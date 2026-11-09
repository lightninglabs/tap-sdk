package customanchor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
)

const (
	// ProofPathMaxDepth bounds the number of unconfirmed transitions
	// retained in a path. Ark transaction trees are shallow, so a larger
	// value is more likely to be hostile input than a useful path.
	ProofPathMaxDepth = 64

	// ProofPathMaxConfirmedProofSize bounds the confirmed proof file.
	ProofPathMaxConfirmedProofSize = 32 * 1024 * 1024

	// ProofPathMaxStepSize bounds one serialized transition proof.
	ProofPathMaxStepSize = 4 * 1024 * 1024

	// ProofPathMaxSize bounds the complete encoded path, including
	// every recursively embedded co-input path.
	ProofPathMaxSize = 64 * 1024 * 1024

	// ProofPathMaxStepCoPaths bounds the co-input paths of one step,
	// so a merging transition carries at most 16 asset witnesses.
	ProofPathMaxStepCoPaths = 15

	// ProofPathMaxCoPathDepth bounds co-input path nesting below the
	// selected path. The bound is checked before nested content is
	// parsed, so hostile deep encodings fail without recursing into them.
	ProofPathMaxCoPathDepth = 3

	assetProofPathChecksumSize = sha256.Size
	assetProofPathHeaderSize   = len(assetProofPathMagic) + 2 + 4 + 2
	assetProofPathFormat       = uint16(0)
)

var (
	assetProofPathMagic = [8]byte{'T', 'A', 'P', 'P', 'A', 'T', 'H', 0}

	// ErrProofPathInvalid reports malformed or unsafe path content.
	ErrProofPathInvalid = errors.New("invalid asset proof path")

	// ErrProofPathUnknownFormat reports an unsupported path encoding.
	ErrProofPathUnknownFormat = errors.New(
		"unknown asset proof path format",
	)

	// ErrProofPathUnknownPassiveAssets reports that the confirmed
	// anchor inventory was not proven complete.
	ErrProofPathUnknownPassiveAssets = errors.New(
		"asset proof path passive asset inventory is unknown",
	)

	// ErrProofPathPassiveAssets reports that the confirmed anchor
	// contains assets outside the tracked proof path.
	ErrProofPathPassiveAssets = errors.New(
		"asset proof path contains passive assets",
	)

	// ErrProofPathUnconfirmedAnchor reports that the host did not
	// attest the Bitcoin-level authorization and policy of an unconfirmed
	// anchor transition.
	ErrProofPathUnconfirmedAnchor = errors.New(
		"asset proof path unconfirmed anchor is not verified",
	)
)

// ProofPath keeps a fully verified confirmed proof file followed by a
// compact sequence of unconfirmed transition proofs.
//
// Derived IDs and summaries are deliberately absent from the persisted DTO.
// They are recomputed from ConfirmedBaseProof and Steps whenever the path is
// decoded or verified.
type ProofPath struct {
	// ConfirmedBaseProof is the complete proof file ending at the last
	// confirmed asset state of the selected lineage.
	ConfirmedBaseProof []byte

	// Steps are ordered from the first unconfirmed child transition to the
	// selected leaf in the transaction tree.
	Steps []ProofPathStep
}

// ProofPathStep is one witness-complete unconfirmed transition.
// TransitionProof uses the native single-proof encoding. It must contain all
// asset witnesses and commitment proofs needed for local verification.
type ProofPathStep struct {
	TransitionProof []byte

	// CoInputPaths are complete proof paths for the co-inputs merged by
	// this step. The transition must spend exactly the spine tip plus every
	// co-path tip, and every co-path must carry the same asset identity as
	// the selected lineage.
	CoInputPaths []*ProofPath
}

// ConfirmedProofVerification is the non-derivable part of confirmed proof
// verification. The SDK derives asset identity and state from the proof bytes;
// this result only attests whether the anchor's asset inventory is complete.
type ConfirmedProofVerification struct {
	// AnchorAssetInventoryComplete asserts that the verifier examined the
	// full confirmed anchor commitment, not only one inclusion proof.
	AnchorAssetInventoryComplete bool

	// PassiveAssetCount is the number of other assets in the confirmed
	// anchor commitment. Compact paths currently require this to be zero.
	PassiveAssetCount uint32
}

// ConfirmedProofVerifier fully verifies the confirmed base proof against the
// chain and reports whether its complete anchor asset inventory is known.
//
// This interface intentionally uses SDK-only types. A tapd-backed adapter can
// perform header, merkle, proof, and inventory checks without exposing
// taproot-assets implementation types to applications.
type ConfirmedProofVerifier interface {
	VerifyConfirmedProof(ctx context.Context,
		proofFile []byte) (*ConfirmedProofVerification, error)
}

// DeriveConfirmedProofInventory derives the anchor inventory of a confirmed
// proof file from the proof itself. The tip's anchor output must commit to the
// tip asset and its disclosed alternate leaves alone, which proves that the
// anchor holds no passive assets. A verifier that only checks the proof chain,
// such as one for an anchor outside its own wallet, can return this result.
// It does not verify the proof chain.
func DeriveConfirmedProofInventory(
	proofFile []byte) (*ConfirmedProofVerification, error) {

	if len(proofFile) > ProofPathMaxConfirmedProofSize {
		return nil, fmt.Errorf(
			"%w: confirmed proof exceeds %d bytes",
			ErrProofPathInvalid,
			ProofPathMaxConfirmedProofSize,
		)
	}

	tip, err := decodeAssetProofPathBase(proofFile)
	if err != nil {
		return nil, err
	}
	if err := verifyAssetProofPathIsolatedOutput(tip); err != nil {
		return nil, err
	}

	return &ConfirmedProofVerification{
		AnchorAssetInventoryComplete: true,
	}, nil
}

// UnconfirmedAnchorVerification contains the SDK-derived Bitcoin transition
// context that a host must verify against its complete signed transaction
// graph. Native Taproot Asset proof verification cannot prove the values or
// authorization of unrelated Bitcoin inputs, transaction standardness, or
// package-relay policy.
type UnconfirmedAnchorVerification struct {
	// StepIndex is the zero-based transition position in the compact path.
	StepIndex uint16

	// PreviousAnchorOutpoint is the asset-bearing output consumed by the
	// transition.
	PreviousAnchorOutpoint tapsdk.Outpoint

	// AnchorOutpoint is the new asset-bearing output created by the
	// transition.
	AnchorOutpoint tapsdk.Outpoint

	// AnchorTransaction is the complete serialized Bitcoin transaction,
	// including witness data when present.
	AnchorTransaction []byte

	// PreviousAnchorOutpoints lists every asset-bearing outpoint the
	// transition consumes: the spine tip first, then each co-input path
	// tip in declared order. It equals [PreviousAnchorOutpoint] for
	// single-input steps.
	PreviousAnchorOutpoints []tapsdk.Outpoint
}

// UnconfirmedAnchorVerifier is an optional extension that becomes mandatory
// whenever an ProofPath contains unconfirmed steps. Swap or Ark hosts
// should use their authoritative graph, prevouts, and signing policy to verify
// every transition before attesting it here.
type UnconfirmedAnchorVerifier interface {
	VerifyUnconfirmedAnchor(ctx context.Context,
		transition UnconfirmedAnchorVerification) error
}

// ProofPathSummary is the verified state at the selected path tip.
type ProofPathSummary struct {
	// ContentID commits to the complete canonical path.
	ContentID tapsdk.Hash

	// ConfirmedBaseProofID identifies the confirmed proof-file bytes.
	ConfirmedBaseProofID tapsdk.Hash

	// Depth is the number of unconfirmed transitions in the path.
	Depth uint16

	// AssetRef is the SDK-level identity of the selected asset.
	AssetRef tapsdk.AssetRef

	// IssuanceID is the concrete asset issuance or tranche ID.
	IssuanceID tapsdk.AssetID

	// AssetType identifies a fungible or collectible asset.
	AssetType tapsdk.AssetType

	// Amount is the asset amount at the selected path tip.
	Amount uint64

	// ScriptKey locks the selected asset state.
	ScriptKey tapsdk.PubKey

	// AnchorOutpoint locates the selected state in its anchor transaction.
	AnchorOutpoint tapsdk.Outpoint

	// AnchorValueSat is the BTC value of the selected anchor output.
	AnchorValueSat int64
}

// ProofPathStepSummary is derived from one transition proof.
type ProofPathStepSummary struct {
	// ContentID commits to the serialized transition proof.
	ContentID tapsdk.Hash

	// PreviousAnchorOutpoint is the anchor output consumed by the step.
	PreviousAnchorOutpoint tapsdk.Outpoint

	// AnchorOutpoint is the output containing the resulting asset state.
	AnchorOutpoint tapsdk.Outpoint

	// AnchorValueSat is the BTC value of the resulting anchor output.
	AnchorValueSat int64

	// AssetRef is the SDK-level identity of the resulting asset.
	AssetRef tapsdk.AssetRef

	// IssuanceID is the concrete asset issuance or tranche ID.
	IssuanceID tapsdk.AssetID

	// AssetType identifies a fungible or collectible asset.
	AssetType tapsdk.AssetType

	// Amount is the resulting asset amount.
	Amount uint64

	// ScriptKey locks the resulting asset state.
	ScriptKey tapsdk.PubKey

	// SplitAsset is true when this proof selects a non-root split output.
	SplitAsset bool
}

// Validate checks bounded structural and policy invariants without trusting a
// backend or treating the unconfirmed steps as verified. Count, size, and
// depth bounds are enforced over the complete path tree before any proof
// content is decoded, so hostile shapes are rejected cheaply.
func (p *ProofPath) Validate() error {
	var totalSize uint64
	if err := p.validateShape(0, &totalSize); err != nil {
		return err
	}

	return p.validateContent()
}

// validateShape enforces the count, size, and depth bounds of one path level
// and recurses into its co-input paths. totalSize accumulates the encoded size
// of the whole tree against the shared budget. No proof bytes are decoded
// here.
func (p *ProofPath) validateShape(depth int, totalSize *uint64) error {
	if p == nil {
		return fmt.Errorf("%w: nil path", ErrProofPathInvalid)
	}
	if depth > ProofPathMaxCoPathDepth {
		return fmt.Errorf(
			"%w: co-input path depth exceeds %d",
			ErrProofPathInvalid, ProofPathMaxCoPathDepth,
		)
	}
	if len(p.ConfirmedBaseProof) == 0 {
		return fmt.Errorf(
			"%w: confirmed base proof is required",
			ErrProofPathInvalid,
		)
	}
	if len(p.ConfirmedBaseProof) > ProofPathMaxConfirmedProofSize {
		return fmt.Errorf(
			"%w: confirmed base proof exceeds %d bytes",
			ErrProofPathInvalid,
			ProofPathMaxConfirmedProofSize,
		)
	}
	if len(p.Steps) > ProofPathMaxDepth {
		return fmt.Errorf(
			"%w: path depth %d exceeds %d",
			ErrProofPathInvalid, len(p.Steps),
			ProofPathMaxDepth,
		)
	}

	*totalSize += uint64(assetProofPathHeaderSize +
		assetProofPathChecksumSize + len(p.ConfirmedBaseProof))
	if *totalSize > ProofPathMaxSize {
		return fmt.Errorf(
			"%w: encoded path exceeds %d bytes",
			ErrProofPathInvalid, ProofPathMaxSize,
		)
	}

	for i := range p.Steps {
		step := &p.Steps[i]
		if len(step.TransitionProof) > ProofPathMaxStepSize {
			return fmt.Errorf(
				"%w: transition proof exceeds %d bytes",
				ErrProofPathInvalid,
				ProofPathMaxStepSize,
			)
		}
		*totalSize += uint64(4 + len(step.TransitionProof))
		if *totalSize > ProofPathMaxSize {
			return fmt.Errorf(
				"%w: encoded path exceeds %d bytes",
				ErrProofPathInvalid,
				ProofPathMaxSize,
			)
		}

		if len(step.CoInputPaths) > ProofPathMaxStepCoPaths {
			return fmt.Errorf(
				"%w: step %d has %d co-input paths, limit "+
					"is %d", ErrProofPathInvalid, i,
				len(step.CoInputPaths),
				ProofPathMaxStepCoPaths,
			)
		}
		for j, coPath := range step.CoInputPaths {
			*totalSize += 4
			err := coPath.validateShape(depth+1, totalSize)
			if err != nil {
				return fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
		}
	}

	return nil
}

// validateContent decodes and checks the proof content of one path level and
// recurses into its co-input paths. The shape bounds are assumed to hold.
func (p *ProofPath) validateContent() error {
	baseProof, err := decodeAssetProofPathBase(p.ConfirmedBaseProof)
	if err != nil {
		return err
	}
	if baseProof.Asset.LockTime != 0 ||
		baseProof.Asset.RelativeLockTime != 0 {

		return fmt.Errorf(
			"%w: confirmed asset timelocks are unsupported",
			ErrProofPathInvalid,
		)
	}
	if len(p.Steps) > 0 && baseProof.Asset.Version != asset.V1 {
		return fmt.Errorf(
			"%w: unconfirmed paths require a v1 base asset",
			ErrProofPathInvalid,
		)
	}

	for i := range p.Steps {
		_, err := decodeAssetProofPathStep(
			&p.Steps[i], p.stepWitnessCount(i),
		)
		if err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		for j, coPath := range p.Steps[i].CoInputPaths {
			if err := coPath.validateContent(); err != nil {
				return fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
		}
	}

	return nil
}

// Clone returns a deep copy suitable for persistence or concurrent ownership.
func (p *ProofPath) Clone() *ProofPath {
	if p == nil {
		return nil
	}

	clone := &ProofPath{
		ConfirmedBaseProof: cloneBytes(p.ConfirmedBaseProof),
	}
	if len(p.Steps) > 0 {
		clone.Steps = make([]ProofPathStep, len(p.Steps))
	}
	for i := range p.Steps {
		clone.Steps[i].TransitionProof = cloneBytes(
			p.Steps[i].TransitionProof,
		)
		for _, coPath := range p.Steps[i].CoInputPaths {
			clone.Steps[i].CoInputPaths = append(
				clone.Steps[i].CoInputPaths, coPath.Clone(),
			)
		}
	}

	return clone
}

// ContentID returns a domain-separated commitment to the canonical path.
func (p *ProofPath) ContentID() (tapsdk.Hash, error) {
	if err := p.Validate(); err != nil {
		return tapsdk.Hash{}, err
	}

	body, err := p.marshalBody()
	if err != nil {
		return tapsdk.Hash{}, err
	}

	return taggedAssetProofPathHash(
		"tapsdk/asset-proof-path/v0", body,
	), nil
}

// ContentID returns a domain-separated commitment to the transition proof.
func (s *ProofPathStep) ContentID() (tapsdk.Hash, error) {
	if _, err := decodeAssetProofPathStep(s, 0); err != nil {
		return tapsdk.Hash{}, err
	}

	return taggedAssetProofPathHash(
		"tapsdk/asset-proof-path-step/v0", s.TransitionProof,
	), nil
}

// Summary decodes and derives the step's identity and resulting asset state.
// It does not cryptographically verify the transition against its predecessor.
func (s *ProofPathStep) Summary() (*ProofPathStepSummary, error) {
	transition, err := decodeAssetProofPathStep(s, 0)
	if err != nil {
		return nil, err
	}

	return summarizeAssetProofPathStep(s, transition)
}

// MarshalBinary returns the canonical, checksummed path encoding.
func (p *ProofPath) MarshalBinary() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	return p.encode()
}

// encode serializes an already validated path with its trailing checksum.
// Co-input paths embed the same self-describing encoding recursively.
func (p *ProofPath) encode() ([]byte, error) {
	body, err := p.marshalBody()
	if err != nil {
		return nil, err
	}
	checksum := sha256.Sum256(body)

	encoded := make([]byte, 0, len(body)+len(checksum))
	encoded = append(encoded, body...)
	encoded = append(encoded, checksum[:]...)

	return encoded, nil
}

// UnmarshalBinary decodes a canonical path only after all bounds, checksum,
// and structural policy checks pass.
func (p *ProofPath) UnmarshalBinary(encoded []byte) error {
	if p == nil {
		return fmt.Errorf("%w: nil receiver", ErrProofPathInvalid)
	}

	var decoded ProofPath
	if err := decoded.unmarshalBinaryAt(encoded, 0); err != nil {
		return err
	}
	if err := decoded.Validate(); err != nil {
		return err
	}

	*p = decoded
	return nil
}

// unmarshalBinaryAt decodes one self-describing path encoding found at the
// given co-input nesting depth. The depth bound is checked before anything is
// parsed, so a hostile deeply nested blob fails fast without recursion, and
// full validation runs once over the decoded root.
func (p *ProofPath) unmarshalBinaryAt(encoded []byte, depth int) error {
	if depth > ProofPathMaxCoPathDepth {
		return fmt.Errorf(
			"%w: co-input path depth exceeds %d",
			ErrProofPathInvalid, ProofPathMaxCoPathDepth,
		)
	}
	if len(encoded) > ProofPathMaxSize {
		return fmt.Errorf(
			"%w: encoded path exceeds %d bytes",
			ErrProofPathInvalid, ProofPathMaxSize,
		)
	}
	if len(encoded) < assetProofPathHeaderSize+
		assetProofPathChecksumSize {

		return fmt.Errorf(
			"%w: truncated encoding", ErrProofPathInvalid,
		)
	}

	bodyEnd := len(encoded) - assetProofPathChecksumSize
	body := encoded[:bodyEnd]
	checksum := sha256.Sum256(body)
	if subtle.ConstantTimeCompare(
		checksum[:], encoded[bodyEnd:],
	) != 1 {

		return fmt.Errorf(
			"%w: checksum mismatch", ErrProofPathInvalid,
		)
	}

	reader := bytes.NewReader(body)
	var magic [len(assetProofPathMagic)]byte
	if _, err := reader.Read(magic[:]); err != nil {
		return fmt.Errorf("%w: read magic: %v", ErrProofPathInvalid,
			err)
	}
	if magic != assetProofPathMagic {
		return fmt.Errorf("%w: invalid magic", ErrProofPathInvalid)
	}

	var format uint16
	if err := binary.Read(reader, binary.BigEndian, &format); err != nil {
		return fmt.Errorf(
			"%w: read format: %v", ErrProofPathInvalid, err,
		)
	}
	if format != assetProofPathFormat {
		return fmt.Errorf(
			"%w: %d", ErrProofPathUnknownFormat, format,
		)
	}

	baseProof, err := readAssetProofPathBytes(
		reader, ProofPathMaxConfirmedProofSize, "confirmed base proof",
	)
	if err != nil {
		return err
	}

	var stepCount uint16
	if err := binary.Read(reader, binary.BigEndian, &stepCount); err != nil {
		return fmt.Errorf(
			"%w: read step count: %v", ErrProofPathInvalid, err,
		)
	}
	if stepCount > ProofPathMaxDepth {
		return fmt.Errorf(
			"%w: path depth %d exceeds %d",
			ErrProofPathInvalid, stepCount,
			ProofPathMaxDepth,
		)
	}

	decoded := ProofPath{
		ConfirmedBaseProof: baseProof,
	}
	if stepCount > 0 {
		decoded.Steps = make([]ProofPathStep, int(stepCount))
	}
	for i := range decoded.Steps {
		stepProof, err := readAssetProofPathBytes(
			reader, ProofPathMaxStepSize, "transition proof",
		)
		if err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		decoded.Steps[i].TransitionProof = stepProof

		var coPathCount uint16
		err = binary.Read(reader, binary.BigEndian, &coPathCount)
		if err != nil {
			return fmt.Errorf(
				"%w: read step %d co-input path count: %v",
				ErrProofPathInvalid, i, err,
			)
		}
		if coPathCount > ProofPathMaxStepCoPaths {
			return fmt.Errorf(
				"%w: step %d has %d co-input paths, limit "+
					"is %d", ErrProofPathInvalid, i,
				coPathCount, ProofPathMaxStepCoPaths,
			)
		}
		for j := range int(coPathCount) {
			coPathBlob, err := readAssetProofPathBytes(
				reader, ProofPathMaxSize, "co-input path",
			)
			if err != nil {
				return fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
			coPath := &ProofPath{}
			err = coPath.unmarshalBinaryAt(coPathBlob, depth+1)
			if err != nil {
				return fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
			decoded.Steps[i].CoInputPaths = append(
				decoded.Steps[i].CoInputPaths, coPath,
			)
		}
	}
	if reader.Len() != 0 {
		return fmt.Errorf(
			"%w: trailing bytes", ErrProofPathInvalid,
		)
	}

	*p = decoded
	return nil
}

// Verify fully verifies the confirmed base through verifier, then locally
// chains every unconfirmed proof while skipping only block inclusion checks.
// Co-input paths are resolved recursively with the same engine before the
// step that merges them.
func (p *ProofPath) Verify(ctx context.Context,
	verifier ConfirmedProofVerifier) (*ProofPathSummary, error) {

	if err := p.Validate(); err != nil {
		return nil, err
	}
	if verifier == nil {
		return nil, fmt.Errorf(
			"%w: confirmed proof verifier is required",
			ErrProofPathInvalid,
		)
	}
	var unconfirmedVerifier UnconfirmedAnchorVerifier
	if len(p.Steps) > 0 {
		var ok bool
		unconfirmedVerifier, ok = verifier.(UnconfirmedAnchorVerifier)
		if !ok {
			return nil, fmt.Errorf(
				"%w: verifier extension is required",
				ErrProofPathUnconfirmedAnchor,
			)
		}
	}

	finalProof, err := p.verifyChain(ctx, verifier, unconfirmedVerifier)
	if err != nil {
		return nil, err
	}

	derived, err := summarizeAssetProof(finalProof)
	if err != nil {
		return nil, err
	}
	contentID, err := p.ContentID()
	if err != nil {
		return nil, err
	}
	baseProofID := sha256.Sum256(p.ConfirmedBaseProof)

	return &ProofPathSummary{
		ContentID:            contentID,
		ConfirmedBaseProofID: tapsdk.Hash(baseProofID),
		Depth:                uint16(len(p.Steps)),
		AssetRef:             derived.assetRef,
		IssuanceID:           derived.issuanceID,
		AssetType:            derived.assetType,
		Amount:               derived.amount,
		ScriptKey:            derived.scriptKey,
		AnchorOutpoint:       derived.anchorOutpoint,
		AnchorValueSat:       derived.anchorValueSat,
	}, nil
}

// verifyChain is the verification engine for one path. The confirmed base is
// fully verified by the caller's verifier, then every unconfirmed step is
// chained locally: the step's co-input paths are resolved recursively to
// verified tips, the transition's witnesses and its anchor transaction are
// bound to exactly those tips plus the spine tip, and only then does the
// native verifier run with the co-input lineages spliced in. The verified
// tip proof is returned.
func (p *ProofPath) verifyChain(ctx context.Context,
	verifier ConfirmedProofVerifier,
	unconfirmedVerifier UnconfirmedAnchorVerifier) (*proof.Proof, error) {

	verification, err := verifier.VerifyConfirmedProof(
		ctx, cloneBytes(p.ConfirmedBaseProof),
	)
	if err != nil {
		return nil, fmt.Errorf("verify confirmed base proof: %w", err)
	}
	if verification == nil ||
		!verification.AnchorAssetInventoryComplete {

		return nil, ErrProofPathUnknownPassiveAssets
	}
	if verification.PassiveAssetCount != 0 {
		return nil, fmt.Errorf(
			"%w: %d", ErrProofPathPassiveAssets,
			verification.PassiveAssetCount,
		)
	}

	baseProof, err := decodeAssetProofPathBase(p.ConfirmedBaseProof)
	if err != nil {
		return nil, err
	}
	previous := &proof.AssetSnapshot{
		Asset:    baseProof.Asset.Copy(),
		OutPoint: baseProof.OutPoint(),
	}

	var finalProof = baseProof
	for i := range p.Steps {
		transition, err := decodeAssetProofPathStep(
			&p.Steps[i], p.stepWitnessCount(i),
		)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		if unconfirmedVerifier == nil {
			return nil, fmt.Errorf(
				"%w: verifier extension is required",
				ErrProofPathUnconfirmedAnchor,
			)
		}

		// Resolve every co-input path to a verified tip first. The
		// tips define, together with the spine tip, exactly which
		// asset outpoints this step may consume — otherwise the
		// declared co-paths would be unbound decoration.
		coPaths := p.stepCoInputPaths(i)
		expected := map[wire.OutPoint]struct{}{
			previous.OutPoint: {},
		}
		spentOutpoints := []wire.OutPoint{previous.OutPoint}
		coInputs := make([]proof.File, 0, len(coPaths))
		for j, coPath := range coPaths {
			coTip, err := coPath.verifyChain(
				ctx, verifier, unconfirmedVerifier,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
			coOutpoint := coTip.OutPoint()
			if _, ok := expected[coOutpoint]; ok {
				return nil, fmt.Errorf(
					"%w: step %d duplicate co-input "+
						"outpoint %v",
					ErrProofPathInvalid, i,
					coOutpoint,
				)
			}
			expected[coOutpoint] = struct{}{}
			spentOutpoints = append(spentOutpoints, coOutpoint)

			// A merging transition may only consume prior states
			// of the confirmed base's exact asset identity.
			if err := verifyAssetProofPathAssetIdentity(
				previous.Asset, &coTip.Asset, "co-input asset",
			); err != nil {
				return nil, fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}

			// The virtual transaction of a merging transition
			// covers every input asset, so the state machine
			// needs each co-input's lineage. It comes from the
			// path's own declared co-paths, which were just
			// verified, never from the transition proof itself.
			coInput, err := coPath.syntheticProofFile()
			if err != nil {
				return nil, fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
			coInputs = append(coInputs, *coInput)
		}

		err = verifyAssetProofPathStepInputBinding(transition, expected)
		if err != nil {
			return nil, fmt.Errorf(
				"verify step %d input binding: %w", i, err,
			)
		}
		err = verifyAssetProofPathAnchorSpend(transition, expected)
		if err != nil {
			return nil, fmt.Errorf(
				"verify step %d anchor inputs: %w", i, err,
			)
		}
		if err := verifyAssetProofPathIdentity(
			previous.Asset, transition,
		); err != nil {
			return nil, fmt.Errorf(
				"verify unconfirmed step %d identity: %w", i,
				err,
			)
		}
		stepSummary, err := summarizeAssetProofPathStep(
			&p.Steps[i], transition,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"summarize unconfirmed step %d: %w", i, err,
			)
		}
		var anchorTransaction bytes.Buffer
		if err := transition.AnchorTx.Serialize(
			&anchorTransaction,
		); err != nil {
			return nil, fmt.Errorf(
				"serialize unconfirmed step %d anchor: %w", i,
				err,
			)
		}
		if err := unconfirmedVerifier.VerifyUnconfirmedAnchor(
			ctx, UnconfirmedAnchorVerification{
				StepIndex: uint16(i),
				PreviousAnchorOutpoint: stepSummary.
					PreviousAnchorOutpoint,
				AnchorOutpoint:    stepSummary.AnchorOutpoint,
				AnchorTransaction: anchorTransaction.Bytes(),
				PreviousAnchorOutpoints: sdkOutpoints(
					spentOutpoints,
				),
			},
		); err != nil {
			return nil, fmt.Errorf(
				"%w: step %d: %v",
				ErrProofPathUnconfirmedAnchor, i, err,
			)
		}

		verificationContext := proof.VerifierCtx{
			// The confirmed base establishes the only group key
			// that may appear in this path. The explicit identity
			// checks above bind every other immutable group
			// parameter as well.
			GroupVerifier: assetProofPathGroupVerifier(
				previous.Asset.GroupKey,
			),
		}

		// Splice the declared co-inputs in for the merging step so
		// the state machine can build the full virtual transaction.
		// They are cleared again immediately: a path's transition
		// proof must stay compact, and one arriving with inputs of
		// its own is rejected at decode.
		if len(coInputs) > 0 {
			verificationContext = assetProofPathCoInputContext(
				verificationContext,
			)
			transition.AdditionalInputs = coInputs
		}
		previous, err = transition.Verify(
			ctx, previous, assetProofPathChainLookup{},
			verificationContext,
			proof.WithSkipChainVerification(),
		)
		transition.AdditionalInputs = nil
		if err != nil {
			return nil, fmt.Errorf(
				"verify unconfirmed step %d: %w", i, err,
			)
		}
		if err := verifyAssetProofPathIsolatedOutput(transition); err != nil {
			return nil, fmt.Errorf(
				"verify unconfirmed step %d isolation: %w", i,
				err,
			)
		}
		finalProof = transition
	}

	return finalProof, nil
}

// syntheticProofFile assembles the in-memory proof file the native verifier
// consumes as a merging step's additional input: the co-input path's
// confirmed base file with its transition proofs appended without block
// data. Chain facts for those proofs are established by verifyChain; the
// synthetic file only feeds the state machine the co-input lineage under
// the no-op chain context.
func (p *ProofPath) syntheticProofFile() (*proof.File, error) {
	file, err := proof.DecodeFile(p.ConfirmedBaseProof)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: decode co-input base file: %v",
			ErrProofPathInvalid, err,
		)
	}

	for i := range p.Steps {
		transition, err := decodeAssetProofPathStep(
			&p.Steps[i], p.stepWitnessCount(i),
		)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		for j, coPath := range p.stepCoInputPaths(i) {
			coInput, err := coPath.syntheticProofFile()
			if err != nil {
				return nil, fmt.Errorf(
					"step %d co-input path %d: %w", i, j,
					err,
				)
			}
			transition.AdditionalInputs = append(
				transition.AdditionalInputs, *coInput,
			)
		}

		transition.BlockHeader = wire.BlockHeader{}
		transition.BlockHeight = 0
		transition.TxMerkleProof = proof.TxMerkleProof{}
		if err := file.AppendProof(*transition); err != nil {
			return nil, fmt.Errorf(
				"append step %d proof: %w", i, err,
			)
		}
	}

	return file, nil
}

// verifyAssetProofPathIdentity enforces the immutable asset parameters that
// must survive every transition. The native VM verifies the witness PrevID,
// but a matching predecessor ID alone does not bind the child asset's Genesis
// or GroupKey fields. Split leaves also carry a complete root asset that must
// retain the same identity.
func verifyAssetProofPathIdentity(previous *asset.Asset,
	transition *proof.Proof) error {

	if previous == nil || transition == nil {
		return fmt.Errorf(
			"%w: missing asset identity state", ErrProofPathInvalid,
		)
	}
	if err := verifyAssetProofPathAssetIdentity(
		previous, &transition.Asset, "selected asset",
	); err != nil {
		return err
	}

	if !transition.Asset.HasSplitCommitmentWitness() {
		return nil
	}
	if len(transition.Asset.PrevWitnesses) != 1 ||
		transition.Asset.PrevWitnesses[0].SplitCommitment == nil {

		return fmt.Errorf(
			"%w: split asset is missing its root asset",
			ErrProofPathInvalid,
		)
	}
	rootAsset := &transition.Asset.PrevWitnesses[0].SplitCommitment.RootAsset
	return verifyAssetProofPathAssetIdentity(
		previous, rootAsset, "split root asset",
	)
}

// verifyAssetProofPathStepInputBinding requires a step's transition to spend
// exactly the expected asset outpoints: the spine tip plus every declared
// co-input tip. The complete witness set of a split transition lives on the
// root asset, so the binding reads the transition's effective witnesses
// rather than the selected output's.
func verifyAssetProofPathStepInputBinding(transition *proof.Proof,
	expected map[wire.OutPoint]struct{}) error {

	witnesses := transition.Asset.PrevWitnesses
	if transition.Asset.HasSplitCommitmentWitness() {
		if len(witnesses) != 1 || witnesses[0].SplitCommitment == nil {
			return fmt.Errorf(
				"%w: split asset is missing its root asset",
				ErrProofPathInvalid,
			)
		}
		witnesses = witnesses[0].SplitCommitment.RootAsset.PrevWitnesses
	}

	spent := make(map[wire.OutPoint]struct{})
	for i := range witnesses {
		prevID := witnesses[i].PrevID
		if prevID == nil {
			return fmt.Errorf(
				"%w: transition witness %d misses its "+
					"previous ID", ErrProofPathInvalid,
				i,
			)
		}
		if _, ok := expected[prevID.OutPoint]; !ok {
			return fmt.Errorf(
				"%w: transition spends undeclared outpoint "+
					"%v", ErrProofPathInvalid,
				prevID.OutPoint,
			)
		}
		spent[prevID.OutPoint] = struct{}{}
	}
	if len(spent) != len(expected) {
		return fmt.Errorf(
			"%w: transition does not spend every declared "+
				"co-input", ErrProofPathInvalid,
		)
	}

	return nil
}

// verifyAssetProofPathAnchorSpend requires the step's anchor transaction to
// spend every expected asset outpoint exactly once. The native verifier only
// proves the spine predecessor is consumed; without this check a transition
// could claim a co-input its Bitcoin transaction never spends.
func verifyAssetProofPathAnchorSpend(transition *proof.Proof,
	expected map[wire.OutPoint]struct{}) error {

	spends := make(map[wire.OutPoint]int, len(expected))
	for _, input := range transition.AnchorTx.TxIn {
		if _, ok := expected[input.PreviousOutPoint]; ok {
			spends[input.PreviousOutPoint]++
		}
	}
	for outpoint := range expected {
		if spends[outpoint] != 1 {
			return fmt.Errorf(
				"%w: anchor transaction must spend asset "+
					"outpoint %v exactly once, found %d",
				ErrProofPathInvalid, outpoint,
				spends[outpoint],
			)
		}
	}

	return nil
}

func verifyAssetProofPathAssetIdentity(previous, next *asset.Asset,
	label string) error {

	if next == nil {
		return fmt.Errorf(
			"%w: %s is missing", ErrProofPathInvalid, label,
		)
	}
	if next.Type != previous.Type {
		return fmt.Errorf(
			"%w: %s type changed", ErrProofPathInvalid, label,
		)
	}
	if next.Genesis != previous.Genesis {
		return fmt.Errorf(
			"%w: %s genesis changed", ErrProofPathInvalid, label,
		)
	}
	if !previous.GroupKey.IsEqual(next.GroupKey) {
		return fmt.Errorf(
			"%w: %s group key changed", ErrProofPathInvalid, label,
		)
	}

	return nil
}

func assetProofPathGroupVerifier(expected *asset.GroupKey) proof.GroupVerifier {
	return func(groupKey *btcec.PublicKey) error {
		if expected == nil {
			return fmt.Errorf("unexpected asset group key")
		}
		if groupKey == nil || !expected.GroupPubKey.IsEqual(groupKey) {
			return fmt.Errorf("asset group key does not match confirmed base")
		}

		return nil
	}
}

func (p *ProofPath) marshalBody() ([]byte, error) {
	var body bytes.Buffer
	body.Grow(assetProofPathHeaderSize + len(p.ConfirmedBaseProof) +
		4*len(p.Steps))
	body.Write(assetProofPathMagic[:])
	if err := binary.Write(
		&body, binary.BigEndian, assetProofPathFormat,
	); err != nil {
		return nil, err
	}
	if err := writeAssetProofPathBytes(&body, p.ConfirmedBaseProof); err != nil {
		return nil, err
	}
	if err := binary.Write(
		&body, binary.BigEndian, uint16(len(p.Steps)),
	); err != nil {
		return nil, err
	}
	for i := range p.Steps {
		if err := writeAssetProofPathBytes(
			&body, p.Steps[i].TransitionProof,
		); err != nil {
			return nil, err
		}

		err := binary.Write(
			&body, binary.BigEndian,
			uint16(len(p.Steps[i].CoInputPaths)),
		)
		if err != nil {
			return nil, err
		}
		for _, coPath := range p.Steps[i].CoInputPaths {
			encodedCoPath, err := coPath.encode()
			if err != nil {
				return nil, err
			}
			err = writeAssetProofPathBytes(&body, encodedCoPath)
			if err != nil {
				return nil, err
			}
		}
	}

	return body.Bytes(), nil
}

func writeAssetProofPathBytes(buffer *bytes.Buffer, value []byte) error {
	if err := binary.Write(
		buffer, binary.BigEndian, uint32(len(value)),
	); err != nil {
		return err
	}
	_, err := buffer.Write(value)
	return err
}

func readAssetProofPathBytes(reader *bytes.Reader, maximum int,
	label string) ([]byte, error) {

	var length uint32
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf(
			"%w: read %s length: %v", ErrProofPathInvalid,
			label, err,
		)
	}
	if length == 0 {
		return nil, fmt.Errorf(
			"%w: %s is empty", ErrProofPathInvalid, label,
		)
	}
	if uint64(length) > uint64(maximum) {
		return nil, fmt.Errorf(
			"%w: %s exceeds %d bytes", ErrProofPathInvalid,
			label, maximum,
		)
	}
	if uint64(length) > uint64(reader.Len()) {
		return nil, fmt.Errorf(
			"%w: truncated %s", ErrProofPathInvalid, label,
		)
	}

	value := make([]byte, int(length))
	if _, err := reader.Read(value); err != nil {
		return nil, fmt.Errorf(
			"%w: read %s: %v", ErrProofPathInvalid, label, err,
		)
	}

	return value, nil
}

func decodeAssetProofPathBase(rawProofFile []byte) (*proof.Proof, error) {
	var proofFile proof.File
	if err := proofFile.Decode(bytes.NewReader(rawProofFile)); err != nil {
		return nil, fmt.Errorf(
			"%w: decode confirmed base proof: %v",
			ErrProofPathInvalid, err,
		)
	}
	baseProof, err := proofFile.LastProof()
	if err != nil {
		return nil, fmt.Errorf(
			"%w: read confirmed base proof tip: %v",
			ErrProofPathInvalid, err,
		)
	}
	if err := baseProof.Asset.Validate(); err != nil {
		return nil, fmt.Errorf(
			"%w: validate confirmed base asset: %v",
			ErrProofPathInvalid, err,
		)
	}

	return baseProof, nil
}

// decodeAssetProofPathStep decodes one step and checks its structure.
// wantWitnesses pins how many asset witnesses the transition must carry;
// zero accepts any complete set, which the per-step helpers use because
// they have no path context to pin the count with. Validate and Verify
// always pin it.
func decodeAssetProofPathStep(step *ProofPathStep,
	wantWitnesses int) (*proof.Proof, error) {

	if step == nil {
		return nil, fmt.Errorf("%w: nil step", ErrProofPathInvalid)
	}
	if len(step.TransitionProof) == 0 {
		return nil, fmt.Errorf(
			"%w: transition proof is required",
			ErrProofPathInvalid,
		)
	}
	if len(step.TransitionProof) > ProofPathMaxStepSize {
		return nil, fmt.Errorf(
			"%w: transition proof exceeds %d bytes",
			ErrProofPathInvalid, ProofPathMaxStepSize,
		)
	}

	transition, err := proof.Decode(step.TransitionProof)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: decode transition proof: %v",
			ErrProofPathInvalid, err,
		)
	}
	if transition.Version != proof.TransitionV1 {
		return nil, fmt.Errorf(
			"%w: transition proof must be v1",
			ErrProofPathInvalid,
		)
	}
	if transition.Asset.Version != asset.V1 {
		return nil, fmt.Errorf(
			"%w: transition asset must be v1",
			ErrProofPathInvalid,
		)
	}
	if err := transition.Asset.Validate(); err != nil {
		return nil, fmt.Errorf(
			"%w: validate transition asset: %v",
			ErrProofPathInvalid, err,
		)
	}
	if transition.Asset.IsGenesisAsset() {
		return nil, fmt.Errorf(
			"%w: unconfirmed genesis proofs are unsupported",
			ErrProofPathInvalid,
		)
	}
	if err := transition.CheckActivationEvidence(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrProofPathInvalid, err)
	}
	if transition.Asset.LockTime != 0 ||
		transition.Asset.RelativeLockTime != 0 {

		return nil, fmt.Errorf(
			"%w: asset timelocks are unsupported",
			ErrProofPathInvalid,
		)
	}
	if len(transition.AdditionalInputs) != 0 {
		return nil, fmt.Errorf(
			"%w: asset merges and additional input paths are unsupported",
			ErrProofPathInvalid,
		)
	}
	assetPredecessors := 0
	for i, input := range transition.AnchorTx.TxIn {
		if input == nil {
			return nil, fmt.Errorf(
				"%w: anchor input %d is nil",
				ErrProofPathInvalid, i,
			)
		}
		if input.PreviousOutPoint == transition.PrevOut {
			assetPredecessors++
		}
	}
	if assetPredecessors != 1 {
		return nil, fmt.Errorf(
			"%w: transition must spend its asset predecessor exactly "+
				"once, found %d", ErrProofPathInvalid,
			assetPredecessors,
		)
	}
	if transition.ChallengeWitness != nil {
		return nil, fmt.Errorf(
			"%w: ownership proofs are unsupported",
			ErrProofPathInvalid,
		)
	}

	isSplit := transition.Asset.HasSplitCommitmentWitness()
	var witnesses []asset.Witness
	if isSplit {
		if transition.SplitRootProof == nil {
			return nil, fmt.Errorf(
				"%w: split output proof is missing its root proof",
				ErrProofPathInvalid,
			)
		}

		// A split leaf is authorized by the complete transition witness
		// on the root asset embedded in its split commitment proof.
		splitWitness := transition.Asset.PrevWitnesses[0]
		rootAsset := &splitWitness.SplitCommitment.RootAsset
		if rootAsset.Version != asset.V1 {
			return nil, fmt.Errorf(
				"%w: split root asset must be v1",
				ErrProofPathInvalid,
			)
		}
		if rootAsset.LockTime != 0 || rootAsset.RelativeLockTime != 0 {
			return nil, fmt.Errorf(
				"%w: split root asset timelocks are unsupported",
				ErrProofPathInvalid,
			)
		}
		if err := rootAsset.Validate(); err != nil {
			return nil, fmt.Errorf(
				"%w: validate split root asset: %v",
				ErrProofPathInvalid, err,
			)
		}
		witnesses = rootAsset.PrevWitnesses
	} else {
		if transition.SplitRootProof != nil {
			return nil, fmt.Errorf(
				"%w: non-split transition has a split root proof",
				ErrProofPathInvalid,
			)
		}
		witnesses = transition.Asset.PrevWitnesses
	}

	if len(witnesses) == 0 {
		return nil, fmt.Errorf(
			"%w: transition must contain at least one complete "+
				"asset witness", ErrProofPathInvalid,
		)
	}
	if wantWitnesses > 0 && len(witnesses) != wantWitnesses {
		return nil, fmt.Errorf(
			"%w: transition must contain %d complete asset "+
				"witnesses, found %d", ErrProofPathInvalid,
			wantWitnesses, len(witnesses),
		)
	}

	// Every witness must be complete, and exactly one of them must spend
	// the step's declared predecessor. A merging transition may carry
	// more, but each of those is pinned to a declared co-input tip by
	// verifyAssetProofPathStepInputBinding, so none can hide an unproven
	// input.
	predecessors := 0
	for i := range witnesses {
		witness := &witnesses[i]
		if witness.PrevID == nil || len(witness.TxWitness) == 0 ||
			witness.SplitCommitment != nil {

			return nil, fmt.Errorf(
				"%w: asset witness %d is incomplete",
				ErrProofPathInvalid, i,
			)
		}
		if witness.PrevID.OutPoint == transition.PrevOut {
			predecessors++
		}
	}
	if predecessors != 1 {
		return nil, fmt.Errorf(
			"%w: transition must spend its declared predecessor "+
				"exactly once, found %d",
			ErrProofPathInvalid, predecessors,
		)
	}

	return transition, nil
}

// stepCoInputPaths returns the co-input paths merged by the step at the given
// index.
func (p *ProofPath) stepCoInputPaths(index int) []*ProofPath {
	if index < 0 || index >= len(p.Steps) {
		return nil
	}

	return p.Steps[index].CoInputPaths
}

// stepWitnessCount returns the number of asset witnesses the step at the
// given index must carry: one for the spine predecessor plus one per
// declared co-input path.
func (p *ProofPath) stepWitnessCount(index int) int {
	return 1 + len(p.stepCoInputPaths(index))
}

// coInputStats returns the total number of co-input paths in the tree and
// the deepest co-input nesting level.
func (p *ProofPath) coInputStats() (int, int) {
	total, maxDepth := 0, 0
	for i := range p.Steps {
		for _, coPath := range p.stepCoInputPaths(i) {
			nestedTotal, nestedDepth := coPath.coInputStats()
			total += 1 + nestedTotal
			if nestedDepth+1 > maxDepth {
				maxDepth = nestedDepth + 1
			}
		}
	}

	return total, maxDepth
}

// assetProofPathChainLookup lets the VM execute its normal timelock path while
// remaining fail closed. The path policy rejects every non-zero asset
// timelock before verification, so only CurrentHeight is expected to run.
type assetProofPathChainLookup struct{}

func (assetProofPathChainLookup) CurrentHeight(context.Context) (
	uint32, error) {

	return 0, nil
}

func (assetProofPathChainLookup) TxBlockHeight(context.Context,
	chainhash.Hash) (uint32, error) {

	return 0, fmt.Errorf(
		"%w: relative timelock lookup is unsupported",
		ErrProofPathInvalid,
	)
}

func (assetProofPathChainLookup) MeanBlockTimestamp(context.Context,
	uint32) (time.Time, error) {

	return time.Time{}, fmt.Errorf(
		"%w: absolute timelock lookup is unsupported",
		ErrProofPathInvalid,
	)
}

// assetProofPathCoInputContext completes a verifier context so the state
// machine can walk a merging step's co-input lineages. Those files are built
// from the path's own declared co-input paths, and every one of them was
// just verified by the engine — confirmed bases through the caller's
// confirmed-proof verifier, the authority on what is on chain, and
// unconfirmed steps through the recursive chain walk — so re-deriving their
// chain facts here would only repeat a stronger check that already passed,
// and cannot be done offline anyway. What the state machine still needs from
// them is the input assets, which come from the file contents.
func assetProofPathCoInputContext(
	base proof.VerifierCtx) proof.VerifierCtx {

	base.HeaderVerifier = func(wire.BlockHeader, uint32) error {
		return nil
	}
	base.MerkleVerifier = func(*wire.MsgTx, *proof.TxMerkleProof,
		[32]byte) error {

		return nil
	}
	base.ChainLookupGen = assetProofPathChainLookupGen{}

	return base
}

// assetProofPathChainLookupGen hands out the path's fail-closed chain
// lookup for every proof and file.
type assetProofPathChainLookupGen struct{}

func (assetProofPathChainLookupGen) GenFileChainLookup(
	*proof.File) asset.ChainLookup {

	return assetProofPathChainLookup{}
}

func (assetProofPathChainLookupGen) GenProofChainLookup(
	*proof.Proof) (asset.ChainLookup, error) {

	return assetProofPathChainLookup{}, nil
}

func verifyAssetProofPathIsolatedOutput(transition *proof.Proof) error {
	if transition == nil ||
		transition.InclusionProof.CommitmentProof == nil {

		return fmt.Errorf(
			"%w: missing inclusion commitment proof",
			ErrProofPathInvalid,
		)
	}
	if transition.InclusionProof.InternalKey == nil {
		return fmt.Errorf(
			"%w: missing anchor internal key",
			ErrProofPathInvalid,
		)
	}

	// Split commitment proofs aren't part of the asset leaf committed in
	// the selected output. Removing it mirrors the commitment constructor
	// while retaining the root asset and its witness in the proof itself.
	committedAsset := transition.Asset.Copy()
	if committedAsset.HasSplitCommitmentWitness() {
		committedAsset.PrevWitnesses[0].SplitCommitment = nil
	}
	assetCommitment, err := commitment.NewAssetCommitment(committedAsset)
	if err != nil {
		return fmt.Errorf("rebuild asset commitment: %w", err)
	}
	commitmentVersion := transition.InclusionProof.CommitmentProof.
		TaprootAssetProof.Version
	tapCommitment, err := commitment.NewTapCommitment(
		&commitmentVersion, assetCommitment,
	)
	if err != nil {
		return fmt.Errorf("rebuild tap commitment: %w", err)
	}

	// Every allowed non-asset leaf must be disclosed in the proof. Adding
	// exactly those leaves makes the output script comparison a proof of
	// absence for undisclosed live or passive assets.
	if err := asset.ValidAltLeaves(transition.AltLeaves); err != nil {
		return fmt.Errorf("validate alternate leaves: %w", err)
	}
	if err := tapCommitment.MergeAltLeaves(
		transition.AltLeaves,
	); err != nil {
		return fmt.Errorf("merge alternate leaves: %w", err)
	}

	var siblingHash *chainhash.Hash
	sibling := transition.InclusionProof.CommitmentProof.
		TapSiblingPreimage
	if sibling != nil {
		tapHash, err := sibling.TapHash()
		if err != nil {
			return fmt.Errorf("derive tapscript sibling hash: %w", err)
		}
		siblingHash = tapHash
	}
	tapscriptRoot := tapCommitment.TapscriptRoot(siblingHash)
	outputKey := txscript.ComputeTaprootOutputKey(
		transition.InclusionProof.InternalKey, tapscriptRoot[:],
	)
	expectedScript, err := txscript.PayToTaprootScript(outputKey)
	if err != nil {
		return fmt.Errorf("derive isolated anchor output: %w", err)
	}

	outputIndex := transition.InclusionProof.OutputIndex
	if outputIndex >= uint32(len(transition.AnchorTx.TxOut)) {
		return fmt.Errorf(
			"%w: anchor output index %d is out of range",
			ErrProofPathInvalid, outputIndex,
		)
	}
	anchorOutput := transition.AnchorTx.TxOut[outputIndex]
	if anchorOutput == nil {
		return fmt.Errorf(
			"%w: anchor output %d is nil", ErrProofPathInvalid,
			outputIndex,
		)
	}
	actualScript := anchorOutput.PkScript
	if !bytes.Equal(expectedScript, actualScript) {
		return fmt.Errorf(
			"%w: selected output is not an isolated asset commitment",
			ErrProofPathPassiveAssets,
		)
	}

	return nil
}

type assetProofPathDerivedSummary struct {
	assetRef       tapsdk.AssetRef
	issuanceID     tapsdk.AssetID
	assetType      tapsdk.AssetType
	amount         uint64
	scriptKey      tapsdk.PubKey
	anchorOutpoint tapsdk.Outpoint
	anchorValueSat int64
}

func summarizeAssetProofPathStep(step *ProofPathStep,
	transition *proof.Proof) (*ProofPathStepSummary, error) {

	derived, err := summarizeAssetProof(transition)
	if err != nil {
		return nil, err
	}
	contentID, err := step.ContentID()
	if err != nil {
		return nil, err
	}

	return &ProofPathStepSummary{
		ContentID:              contentID,
		PreviousAnchorOutpoint: sdkOutpoint(transition.PrevOut),
		AnchorOutpoint:         derived.anchorOutpoint,
		AnchorValueSat:         derived.anchorValueSat,
		AssetRef:               derived.assetRef,
		IssuanceID:             derived.issuanceID,
		AssetType:              derived.assetType,
		Amount:                 derived.amount,
		ScriptKey:              derived.scriptKey,
		SplitAsset:             transition.Asset.HasSplitCommitmentWitness(),
	}, nil
}

func summarizeAssetProof(
	assetProof *proof.Proof) (*assetProofPathDerivedSummary, error) {

	if assetProof == nil {
		return nil, fmt.Errorf(
			"%w: nil asset proof", ErrProofPathInvalid,
		)
	}
	if assetProof.Asset.Type != asset.Normal &&
		assetProof.Asset.Type != asset.Collectible {

		return nil, fmt.Errorf(
			"%w: unknown asset type %d", ErrProofPathInvalid,
			assetProof.Asset.Type,
		)
	}
	if assetProof.Asset.ScriptKey.PubKey == nil {
		return nil, fmt.Errorf(
			"%w: missing asset script key", ErrProofPathInvalid,
		)
	}

	var issuanceID tapsdk.AssetID
	nativeID := assetProof.Asset.ID()
	copy(issuanceID[:], nativeID[:])

	var groupKey *tapsdk.PubKey
	if assetProof.Asset.GroupKey != nil {
		parsedGroupKey, err := tapsdk.ParsePubKey(
			assetProof.Asset.GroupKey.GroupPubKey.SerializeCompressed(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid group key: %v",
				ErrProofPathInvalid, err,
			)
		}
		groupKey = &parsedGroupKey
	}

	assetType := tapsdk.AssetType(assetProof.Asset.Type)
	assetRef := tapsdk.AssetRefFromTypedAsset(
		issuanceID, groupKey, assetType,
	)
	scriptKey, err := tapsdk.ParseTaprootPubKey(
		assetProof.Asset.ScriptKey.PubKey.SerializeCompressed(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid script key: %v", ErrProofPathInvalid,
			err,
		)
	}
	outputIndex := assetProof.InclusionProof.OutputIndex
	if outputIndex >= uint32(len(assetProof.AnchorTx.TxOut)) {
		return nil, fmt.Errorf(
			"%w: anchor output index %d is out of range",
			ErrProofPathInvalid, outputIndex,
		)
	}
	anchorOutput := assetProof.AnchorTx.TxOut[outputIndex]
	if anchorOutput == nil {
		return nil, fmt.Errorf(
			"%w: anchor output %d is nil", ErrProofPathInvalid,
			outputIndex,
		)
	}
	if anchorOutput.Value < 0 {
		return nil, fmt.Errorf(
			"%w: anchor output %d value is negative",
			ErrProofPathInvalid, outputIndex,
		)
	}

	return &assetProofPathDerivedSummary{
		assetRef:       assetRef,
		issuanceID:     issuanceID,
		assetType:      assetType,
		amount:         assetProof.Asset.Amount,
		scriptKey:      scriptKey,
		anchorOutpoint: sdkOutpoint(assetProof.OutPoint()),
		anchorValueSat: anchorOutput.Value,
	}, nil
}

func sdkOutpoint(outpoint wire.OutPoint) tapsdk.Outpoint {
	var txid [32]byte
	copy(txid[:], outpoint.Hash[:])

	return tapsdk.Outpoint{
		Txid:  txid,
		Index: outpoint.Index,
	}
}

func sdkOutpoints(outpoints []wire.OutPoint) []tapsdk.Outpoint {
	converted := make([]tapsdk.Outpoint, len(outpoints))
	for i := range outpoints {
		converted[i] = sdkOutpoint(outpoints[i])
	}

	return converted
}

func taggedAssetProofPathHash(tag string, content []byte) tapsdk.Hash {
	tagHash := sha256.Sum256([]byte(tag))
	hasher := sha256.New()
	_, _ = hasher.Write(tagHash[:])
	_, _ = hasher.Write(tagHash[:])
	_, _ = hasher.Write(content)

	var result tapsdk.Hash
	copy(result[:], hasher.Sum(nil))
	return result
}
