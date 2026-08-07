package tapsdk

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
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
)

const (
	// AssetProofPathMaxDepth bounds the number of unconfirmed transitions
	// retained in a path. Ark transaction trees are shallow, so a larger
	// value is more likely to be hostile input than a useful path.
	AssetProofPathMaxDepth = 64

	// AssetProofPathMaxConfirmedProofSize bounds the confirmed proof file.
	AssetProofPathMaxConfirmedProofSize = 32 * 1024 * 1024

	// AssetProofPathMaxStepSize bounds one serialized transition proof.
	AssetProofPathMaxStepSize = 4 * 1024 * 1024

	// AssetProofPathMaxSize bounds the complete encoded path.
	AssetProofPathMaxSize = 64 * 1024 * 1024

	assetProofPathChecksumSize = sha256.Size
	assetProofPathHeaderSize   = len(assetProofPathMagic) + 2 + 4 + 2
)

var (
	assetProofPathMagic = [8]byte{'T', 'A', 'P', 'P', 'A', 'T', 'H', 0}

	// ErrAssetProofPathInvalid reports malformed or unsafe path content.
	ErrAssetProofPathInvalid = errors.New("invalid asset proof path")

	// ErrAssetProofPathUnknownVersion reports an unsupported path version.
	ErrAssetProofPathUnknownVersion = errors.New(
		"unknown asset proof path version",
	)

	// ErrAssetProofPathUnknownPassiveAssets reports that the confirmed
	// anchor inventory was not proven complete.
	ErrAssetProofPathUnknownPassiveAssets = errors.New(
		"asset proof path passive asset inventory is unknown",
	)

	// ErrAssetProofPathPassiveAssets reports that the confirmed anchor
	// contains assets outside the tracked proof path.
	ErrAssetProofPathPassiveAssets = errors.New(
		"asset proof path contains passive assets",
	)

	// ErrAssetProofPathUnconfirmedAnchor reports that the host did not
	// attest the Bitcoin-level authorization and policy of an unconfirmed
	// anchor transition.
	ErrAssetProofPathUnconfirmedAnchor = errors.New(
		"asset proof path unconfirmed anchor is not verified",
	)
)

// AssetProofPathVersion identifies the stable binary path schema.
type AssetProofPathVersion uint16

const (
	// AssetProofPathVersionV0 is the initial compact proof-path schema.
	AssetProofPathVersionV0 AssetProofPathVersion = 0

	// AssetProofPathVersionV1 extends V0 with additional confirmed base
	// proofs, one per co-input of a multi-input first transition. The
	// selected asset's lineage stays in ConfirmedBaseProof; the
	// additional bases authenticate the prior states merged into the
	// first step.
	AssetProofPathVersionV1 AssetProofPathVersion = 1

	// AssetProofPathMaxAdditionalBases bounds the co-input base proofs
	// of a V1 path.
	AssetProofPathMaxAdditionalBases = 15
)

// AssetProofPath keeps a fully verified confirmed proof file followed by a
// compact sequence of unconfirmed transition proofs.
//
// Derived IDs and summaries are deliberately absent from the persisted DTO.
// They are recomputed from ConfirmedBaseProof and Steps whenever the path is
// decoded or verified.
type AssetProofPath struct {
	// Version is the binary path schema version.
	Version AssetProofPathVersion

	// ConfirmedBaseProof is the complete proof file ending at the last
	// confirmed asset state of the selected lineage.
	ConfirmedBaseProof []byte

	// AdditionalBaseProofs are complete confirmed proof files for the
	// co-inputs of a multi-input first transition (V1 paths). The first
	// step's previous witnesses must reference exactly the outpoints of
	// ConfirmedBaseProof plus these bases, and every base must carry
	// the same asset identity.
	AdditionalBaseProofs [][]byte

	// Steps are ordered from the first unconfirmed child transition to the
	// selected leaf in the transaction tree.
	Steps []AssetProofPathStep
}

// AssetProofPathStep is one witness-complete unconfirmed transition.
// TransitionProof uses the native single-proof encoding. It must contain all
// asset witnesses and commitment proofs needed for local verification.
type AssetProofPathStep struct {
	TransitionProof []byte
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
	PreviousAnchorOutpoint Outpoint

	// AnchorOutpoint is the new asset-bearing output created by the
	// transition.
	AnchorOutpoint Outpoint

	// AnchorTransaction is the complete serialized Bitcoin transaction,
	// including witness data when present.
	AnchorTransaction []byte
}

// UnconfirmedAnchorVerifier is an optional extension that becomes mandatory
// whenever an AssetProofPath contains unconfirmed steps. Swap or Ark hosts
// should use their authoritative graph, prevouts, and signing policy to verify
// every transition before attesting it here.
type UnconfirmedAnchorVerifier interface {
	VerifyUnconfirmedAnchor(ctx context.Context,
		transition UnconfirmedAnchorVerification) error
}

// AssetProofPathSummary is the verified state at the selected path tip.
type AssetProofPathSummary struct {
	// ContentID commits to the complete canonical path.
	ContentID Hash

	// ConfirmedBaseProofID identifies the confirmed proof-file bytes.
	ConfirmedBaseProofID Hash

	// Depth is the number of unconfirmed transitions in the path.
	Depth uint16

	// AssetRef is the SDK-level identity of the selected asset.
	AssetRef AssetRef

	// IssuanceID is the concrete asset issuance or tranche ID.
	IssuanceID AssetID

	// AssetType identifies a fungible or collectible asset.
	AssetType AssetType

	// Amount is the asset amount at the selected path tip.
	Amount uint64

	// ScriptKey locks the selected asset state.
	ScriptKey PubKey

	// AnchorOutpoint locates the selected state in its anchor transaction.
	AnchorOutpoint Outpoint

	// AnchorValueSat is the BTC value of the selected anchor output.
	AnchorValueSat int64
}

// AssetProofPathStepSummary is derived from one transition proof.
type AssetProofPathStepSummary struct {
	// ContentID commits to the serialized transition proof.
	ContentID Hash

	// PreviousAnchorOutpoint is the anchor output consumed by the step.
	PreviousAnchorOutpoint Outpoint

	// AnchorOutpoint is the output containing the resulting asset state.
	AnchorOutpoint Outpoint

	// AnchorValueSat is the BTC value of the resulting anchor output.
	AnchorValueSat int64

	// AssetRef is the SDK-level identity of the resulting asset.
	AssetRef AssetRef

	// IssuanceID is the concrete asset issuance or tranche ID.
	IssuanceID AssetID

	// AssetType identifies a fungible or collectible asset.
	AssetType AssetType

	// Amount is the resulting asset amount.
	Amount uint64

	// ScriptKey locks the resulting asset state.
	ScriptKey PubKey

	// SplitAsset is true when this proof selects a non-root split output.
	SplitAsset bool
}

// Validate checks bounded structural and policy invariants without trusting a
// backend or treating the unconfirmed steps as verified.
func (p *AssetProofPath) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: nil path", ErrAssetProofPathInvalid)
	}
	if p.Version != AssetProofPathVersionV0 &&
		p.Version != AssetProofPathVersionV1 {

		return fmt.Errorf(
			"%w: %d", ErrAssetProofPathUnknownVersion, p.Version,
		)
	}
	if p.Version == AssetProofPathVersionV0 &&
		len(p.AdditionalBaseProofs) != 0 {

		return fmt.Errorf(
			"%w: additional base proofs need a v1 path",
			ErrAssetProofPathInvalid,
		)
	}
	if len(p.AdditionalBaseProofs) > AssetProofPathMaxAdditionalBases {
		return fmt.Errorf(
			"%w: %d additional base proofs exceed %d",
			ErrAssetProofPathInvalid,
			len(p.AdditionalBaseProofs),
			AssetProofPathMaxAdditionalBases,
		)
	}
	if len(p.AdditionalBaseProofs) > 0 && len(p.Steps) == 0 {
		return fmt.Errorf(
			"%w: additional base proofs need the multi-input "+
				"transition they feed",
			ErrAssetProofPathInvalid,
		)
	}
	if len(p.ConfirmedBaseProof) == 0 {
		return fmt.Errorf(
			"%w: confirmed base proof is required",
			ErrAssetProofPathInvalid,
		)
	}
	if len(p.ConfirmedBaseProof) > AssetProofPathMaxConfirmedProofSize {
		return fmt.Errorf(
			"%w: confirmed base proof exceeds %d bytes",
			ErrAssetProofPathInvalid,
			AssetProofPathMaxConfirmedProofSize,
		)
	}
	if len(p.Steps) > AssetProofPathMaxDepth {
		return fmt.Errorf(
			"%w: path depth %d exceeds %d",
			ErrAssetProofPathInvalid, len(p.Steps),
			AssetProofPathMaxDepth,
		)
	}

	baseProof, err := decodeAssetProofPathBase(p.ConfirmedBaseProof)
	if err != nil {
		return err
	}
	if baseProof.Asset.LockTime != 0 ||
		baseProof.Asset.RelativeLockTime != 0 {

		return fmt.Errorf(
			"%w: confirmed asset timelocks are unsupported",
			ErrAssetProofPathInvalid,
		)
	}
	if len(p.Steps) > 0 && baseProof.Asset.Version != asset.V1 {
		return fmt.Errorf(
			"%w: unconfirmed paths require a v1 base asset",
			ErrAssetProofPathInvalid,
		)
	}

	totalSize := uint64(assetProofPathHeaderSize +
		assetProofPathChecksumSize + len(p.ConfirmedBaseProof))

	// Additional bases must decode, stay in bounds, and carry the same
	// asset identity as the selected base: a multi-input transition can
	// only merge identical assets into one output.
	for i, base := range p.AdditionalBaseProofs {
		if len(base) == 0 ||
			len(base) > AssetProofPathMaxConfirmedProofSize {

			return fmt.Errorf(
				"%w: additional base proof %d size is out "+
					"of bounds", ErrAssetProofPathInvalid,
				i,
			)
		}
		totalSize += uint64(4 + len(base))
		if totalSize > AssetProofPathMaxSize {
			return fmt.Errorf(
				"%w: encoded path exceeds %d bytes",
				ErrAssetProofPathInvalid,
				AssetProofPathMaxSize,
			)
		}

		additional, err := decodeAssetProofPathBase(base)
		if err != nil {
			return fmt.Errorf("additional base %d: %w", i, err)
		}
		if additional.Asset.LockTime != 0 ||
			additional.Asset.RelativeLockTime != 0 {

			return fmt.Errorf(
				"%w: confirmed asset timelocks are "+
					"unsupported", ErrAssetProofPathInvalid,
			)
		}
		if err := verifyAssetProofPathAssetIdentity(
			&baseProof.Asset, &additional.Asset,
			"additional base asset",
		); err != nil {
			return fmt.Errorf("additional base %d: %w", i, err)
		}
	}
	for i := range p.Steps {
		stepSize := len(p.Steps[i].TransitionProof)
		totalSize += uint64(4 + stepSize)
		if totalSize > AssetProofPathMaxSize {
			return fmt.Errorf(
				"%w: encoded path exceeds %d bytes",
				ErrAssetProofPathInvalid,
				AssetProofPathMaxSize,
			)
		}

		_, err := decodeAssetProofPathStep(
			&p.Steps[i], p.stepWitnessCount(i),
		)
		if err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}

	return nil
}

// Clone returns a deep copy suitable for persistence or concurrent ownership.
func (p *AssetProofPath) Clone() *AssetProofPath {
	if p == nil {
		return nil
	}

	clone := &AssetProofPath{
		Version:            p.Version,
		ConfirmedBaseProof: cloneBytes(p.ConfirmedBaseProof),
		Steps:              make([]AssetProofPathStep, len(p.Steps)),
	}
	for _, base := range p.AdditionalBaseProofs {
		clone.AdditionalBaseProofs = append(
			clone.AdditionalBaseProofs, cloneBytes(base),
		)
	}
	for i := range p.Steps {
		clone.Steps[i].TransitionProof = cloneBytes(
			p.Steps[i].TransitionProof,
		)
	}

	return clone
}

// ContentID returns a domain-separated commitment to the canonical path.
func (p *AssetProofPath) ContentID() (Hash, error) {
	if err := p.Validate(); err != nil {
		return Hash{}, err
	}

	body, err := p.marshalBody()
	if err != nil {
		return Hash{}, err
	}

	tag := "tapsdk/asset-proof-path/v0"
	if p.Version == AssetProofPathVersionV1 {
		tag = "tapsdk/asset-proof-path/v1"
	}

	return taggedAssetProofPathHash(tag, body), nil
}

// ContentID returns a domain-separated commitment to the transition proof.
func (s *AssetProofPathStep) ContentID() (Hash, error) {
	if _, err := decodeAssetProofPathStep(s, 0); err != nil {
		return Hash{}, err
	}

	return taggedAssetProofPathHash(
		"tapsdk/asset-proof-path-step/v0", s.TransitionProof,
	), nil
}

// Summary decodes and derives the step's identity and resulting asset state.
// It does not cryptographically verify the transition against its predecessor.
func (s *AssetProofPathStep) Summary() (*AssetProofPathStepSummary, error) {
	transition, err := decodeAssetProofPathStep(s, 0)
	if err != nil {
		return nil, err
	}

	return summarizeAssetProofPathStep(s, transition)
}

// MarshalBinary returns the canonical, checksummed path encoding.
func (p *AssetProofPath) MarshalBinary() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

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
func (p *AssetProofPath) UnmarshalBinary(encoded []byte) error {
	if p == nil {
		return fmt.Errorf("%w: nil receiver", ErrAssetProofPathInvalid)
	}
	if len(encoded) > AssetProofPathMaxSize {
		return fmt.Errorf(
			"%w: encoded path exceeds %d bytes",
			ErrAssetProofPathInvalid, AssetProofPathMaxSize,
		)
	}
	if len(encoded) < assetProofPathHeaderSize+
		assetProofPathChecksumSize {

		return fmt.Errorf(
			"%w: truncated encoding", ErrAssetProofPathInvalid,
		)
	}

	bodyEnd := len(encoded) - assetProofPathChecksumSize
	body := encoded[:bodyEnd]
	checksum := sha256.Sum256(body)
	if subtle.ConstantTimeCompare(
		checksum[:], encoded[bodyEnd:],
	) != 1 {

		return fmt.Errorf(
			"%w: checksum mismatch", ErrAssetProofPathInvalid,
		)
	}

	reader := bytes.NewReader(body)
	var magic [len(assetProofPathMagic)]byte
	if _, err := reader.Read(magic[:]); err != nil {
		return fmt.Errorf("%w: read magic: %v", ErrAssetProofPathInvalid,
			err)
	}
	if magic != assetProofPathMagic {
		return fmt.Errorf("%w: invalid magic", ErrAssetProofPathInvalid)
	}

	var version uint16
	if err := binary.Read(reader, binary.BigEndian, &version); err != nil {
		return fmt.Errorf(
			"%w: read version: %v", ErrAssetProofPathInvalid, err,
		)
	}
	if AssetProofPathVersion(version) != AssetProofPathVersionV0 &&
		AssetProofPathVersion(version) != AssetProofPathVersionV1 {

		return fmt.Errorf(
			"%w: %d", ErrAssetProofPathUnknownVersion, version,
		)
	}

	baseProof, err := readAssetProofPathBytes(
		reader, AssetProofPathMaxConfirmedProofSize, "confirmed base proof",
	)
	if err != nil {
		return err
	}

	var additionalBases [][]byte
	if AssetProofPathVersion(version) >= AssetProofPathVersionV1 {
		var baseCount uint16
		err := binary.Read(reader, binary.BigEndian, &baseCount)
		if err != nil {
			return fmt.Errorf(
				"%w: read additional base count: %v",
				ErrAssetProofPathInvalid, err,
			)
		}
		if baseCount > AssetProofPathMaxAdditionalBases {
			return fmt.Errorf(
				"%w: %d additional base proofs exceed %d",
				ErrAssetProofPathInvalid, baseCount,
				AssetProofPathMaxAdditionalBases,
			)
		}
		for i := 0; i < int(baseCount); i++ {
			base, err := readAssetProofPathBytes(
				reader, AssetProofPathMaxConfirmedProofSize,
				"additional base proof",
			)
			if err != nil {
				return fmt.Errorf(
					"additional base %d: %w", i, err,
				)
			}
			additionalBases = append(additionalBases, base)
		}
	}

	var stepCount uint16
	if err := binary.Read(reader, binary.BigEndian, &stepCount); err != nil {
		return fmt.Errorf(
			"%w: read step count: %v", ErrAssetProofPathInvalid, err,
		)
	}
	if stepCount > AssetProofPathMaxDepth {
		return fmt.Errorf(
			"%w: path depth %d exceeds %d",
			ErrAssetProofPathInvalid, stepCount,
			AssetProofPathMaxDepth,
		)
	}

	decoded := AssetProofPath{
		Version:              AssetProofPathVersion(version),
		ConfirmedBaseProof:   baseProof,
		AdditionalBaseProofs: additionalBases,
		Steps: make(
			[]AssetProofPathStep, int(stepCount),
		),
	}
	for i := range decoded.Steps {
		stepProof, err := readAssetProofPathBytes(
			reader, AssetProofPathMaxStepSize, "transition proof",
		)
		if err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		decoded.Steps[i].TransitionProof = stepProof
	}
	if reader.Len() != 0 {
		return fmt.Errorf(
			"%w: trailing bytes", ErrAssetProofPathInvalid,
		)
	}
	if err := decoded.Validate(); err != nil {
		return err
	}

	*p = decoded
	return nil
}

// Verify fully verifies the confirmed base through verifier, then locally
// chains every unconfirmed proof while skipping only block inclusion checks.
func (p *AssetProofPath) Verify(ctx context.Context,
	verifier ConfirmedProofVerifier) (*AssetProofPathSummary, error) {

	if err := p.Validate(); err != nil {
		return nil, err
	}
	if verifier == nil {
		return nil, fmt.Errorf(
			"%w: confirmed proof verifier is required",
			ErrAssetProofPathInvalid,
		)
	}
	var unconfirmedVerifier UnconfirmedAnchorVerifier
	if len(p.Steps) > 0 {
		var ok bool
		unconfirmedVerifier, ok = verifier.(UnconfirmedAnchorVerifier)
		if !ok {
			return nil, fmt.Errorf(
				"%w: verifier extension is required",
				ErrAssetProofPathUnconfirmedAnchor,
			)
		}
	}

	verification, err := verifier.VerifyConfirmedProof(
		ctx, cloneBytes(p.ConfirmedBaseProof),
	)
	if err != nil {
		return nil, fmt.Errorf("verify confirmed base proof: %w", err)
	}
	if verification == nil ||
		!verification.AnchorAssetInventoryComplete {

		return nil, ErrAssetProofPathUnknownPassiveAssets
	}
	if verification.PassiveAssetCount != 0 {
		return nil, fmt.Errorf(
			"%w: %d", ErrAssetProofPathPassiveAssets,
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

	// Additional bases (co-inputs of a multi-input first transition)
	// are verified exactly like the selected base, and the first step
	// must spend precisely the union of all base outpoints — otherwise
	// the extra bases would be unbound decoration.
	baseOutpoints := map[wire.OutPoint]struct{}{
		baseProof.OutPoint(): {},
	}
	for i, base := range p.AdditionalBaseProofs {
		additionalVerification, err := verifier.VerifyConfirmedProof(
			ctx, cloneBytes(base),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"verify additional base proof %d: %w", i, err,
			)
		}
		if additionalVerification == nil ||
			!additionalVerification.AnchorAssetInventoryComplete {

			return nil, ErrAssetProofPathUnknownPassiveAssets
		}
		if additionalVerification.PassiveAssetCount != 0 {
			return nil, fmt.Errorf(
				"%w: %d", ErrAssetProofPathPassiveAssets,
				additionalVerification.PassiveAssetCount,
			)
		}

		additional, err := decodeAssetProofPathBase(base)
		if err != nil {
			return nil, fmt.Errorf("additional base %d: %w", i,
				err)
		}
		baseOutpoints[additional.OutPoint()] = struct{}{}
	}
	if len(baseOutpoints) != len(p.AdditionalBaseProofs)+1 {
		return nil, fmt.Errorf(
			"%w: duplicate base proof outpoints",
			ErrAssetProofPathInvalid,
		)
	}

	var finalProof = baseProof
	for i := range p.Steps {
		transition, err := decodeAssetProofPathStep(
			&p.Steps[i], p.stepWitnessCount(i),
		)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		if i == 0 && len(p.AdditionalBaseProofs) > 0 {
			err := verifyAssetProofPathBaseBinding(
				transition, baseOutpoints,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"verify first step base binding: %w",
					err,
				)
			}
		}
		if err := verifyAssetProofPathIdentity(
			previous.Asset, transition,
		); err != nil {
			return nil, fmt.Errorf(
				"verify unconfirmed step %d identity: %w", i, err,
			)
		}
		stepSummary, err := summarizeAssetProofPathStep(
			&p.Steps[i], transition,
		)
		if err != nil {
			return nil, fmt.Errorf("summarize unconfirmed step %d: %w", i,
				err)
		}
		var anchorTransaction bytes.Buffer
		if err := transition.AnchorTx.Serialize(
			&anchorTransaction,
		); err != nil {
			return nil, fmt.Errorf("serialize unconfirmed step %d anchor: %w",
				i, err)
		}
		if err := unconfirmedVerifier.VerifyUnconfirmedAnchor(
			ctx, UnconfirmedAnchorVerification{
				StepIndex:              uint16(i),
				PreviousAnchorOutpoint: stepSummary.PreviousAnchorOutpoint,
				AnchorOutpoint:         stepSummary.AnchorOutpoint,
				AnchorTransaction:      anchorTransaction.Bytes(),
			},
		); err != nil {
			return nil, fmt.Errorf(
				"%w: step %d: %v",
				ErrAssetProofPathUnconfirmedAnchor, i, err,
			)
		}

		verificationContext := proof.VerifierCtx{
			// The confirmed base establishes the only group key that may
			// appear in this path. The explicit identity check above binds
			// every other immutable group parameter as well.
			GroupVerifier: assetProofPathGroupVerifier(
				previous.Asset.GroupKey,
			),
		}

		previous, err = transition.Verify(
			ctx, previous, assetProofPathChainLookup{},
			verificationContext,
			proof.WithSkipChainVerification(),
		)
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

	derived, err := summarizeAssetProof(finalProof)
	if err != nil {
		return nil, err
	}
	contentID, err := p.ContentID()
	if err != nil {
		return nil, err
	}
	baseProofID := sha256.Sum256(p.ConfirmedBaseProof)

	return &AssetProofPathSummary{
		ContentID:            contentID,
		ConfirmedBaseProofID: Hash(baseProofID),
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

// verifyAssetProofPathIdentity enforces the immutable asset parameters that
// must survive every transition. The native VM verifies the witness PrevID,
// but a matching predecessor ID alone does not bind the child asset's Genesis
// or GroupKey fields. Split leaves also carry a complete root asset that must
// retain the same identity.
func verifyAssetProofPathIdentity(previous *asset.Asset,
	transition *proof.Proof) error {

	if previous == nil || transition == nil {
		return fmt.Errorf(
			"%w: missing asset identity state", ErrAssetProofPathInvalid,
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
			ErrAssetProofPathInvalid,
		)
	}
	rootAsset := &transition.Asset.PrevWitnesses[0].SplitCommitment.RootAsset
	return verifyAssetProofPathAssetIdentity(
		previous, rootAsset, "split root asset",
	)
}

// verifyAssetProofPathBaseBinding requires a multi-base first transition
// to spend exactly the outpoints of the declared base proofs: every
// previous witness resolves to a declared base and every base is spent.
func verifyAssetProofPathBaseBinding(transition *proof.Proof,
	baseOutpoints map[wire.OutPoint]struct{}) error {

	spent := make(map[wire.OutPoint]struct{})
	for i := range transition.Asset.PrevWitnesses {
		prevID := transition.Asset.PrevWitnesses[i].PrevID
		if prevID == nil {
			return fmt.Errorf(
				"%w: transition witness %d misses its "+
					"previous ID", ErrAssetProofPathInvalid,
				i,
			)
		}
		if _, ok := baseOutpoints[prevID.OutPoint]; !ok {
			return fmt.Errorf(
				"%w: transition spends undeclared outpoint "+
					"%v", ErrAssetProofPathInvalid,
				prevID.OutPoint,
			)
		}
		spent[prevID.OutPoint] = struct{}{}
	}
	if len(spent) != len(baseOutpoints) {
		return fmt.Errorf(
			"%w: transition does not spend every declared base",
			ErrAssetProofPathInvalid,
		)
	}

	return nil
}

func verifyAssetProofPathAssetIdentity(previous, next *asset.Asset,
	label string) error {

	if next == nil {
		return fmt.Errorf(
			"%w: %s is missing", ErrAssetProofPathInvalid, label,
		)
	}
	if next.Type != previous.Type {
		return fmt.Errorf(
			"%w: %s type changed", ErrAssetProofPathInvalid, label,
		)
	}
	if next.Genesis != previous.Genesis {
		return fmt.Errorf(
			"%w: %s genesis changed", ErrAssetProofPathInvalid, label,
		)
	}
	if !previous.GroupKey.IsEqual(next.GroupKey) {
		return fmt.Errorf(
			"%w: %s group key changed", ErrAssetProofPathInvalid, label,
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

func (p *AssetProofPath) marshalBody() ([]byte, error) {
	var body bytes.Buffer
	body.Grow(assetProofPathHeaderSize + len(p.ConfirmedBaseProof) +
		4*len(p.Steps))
	body.Write(assetProofPathMagic[:])
	if err := binary.Write(
		&body, binary.BigEndian, uint16(p.Version),
	); err != nil {
		return nil, err
	}
	if err := writeAssetProofPathBytes(&body, p.ConfirmedBaseProof); err != nil {
		return nil, err
	}
	if p.Version >= AssetProofPathVersionV1 {
		err := binary.Write(
			&body, binary.BigEndian,
			uint16(len(p.AdditionalBaseProofs)),
		)
		if err != nil {
			return nil, err
		}
		for _, base := range p.AdditionalBaseProofs {
			err := writeAssetProofPathBytes(&body, base)
			if err != nil {
				return nil, err
			}
		}
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
			"%w: read %s length: %v", ErrAssetProofPathInvalid,
			label, err,
		)
	}
	if length == 0 {
		return nil, fmt.Errorf(
			"%w: %s is empty", ErrAssetProofPathInvalid, label,
		)
	}
	if uint64(length) > uint64(maximum) {
		return nil, fmt.Errorf(
			"%w: %s exceeds %d bytes", ErrAssetProofPathInvalid,
			label, maximum,
		)
	}
	if uint64(length) > uint64(reader.Len()) {
		return nil, fmt.Errorf(
			"%w: truncated %s", ErrAssetProofPathInvalid, label,
		)
	}

	value := make([]byte, int(length))
	if _, err := reader.Read(value); err != nil {
		return nil, fmt.Errorf(
			"%w: read %s: %v", ErrAssetProofPathInvalid, label, err,
		)
	}

	return value, nil
}

func decodeAssetProofPathBase(rawProofFile []byte) (*proof.Proof, error) {
	var proofFile proof.File
	if err := proofFile.Decode(bytes.NewReader(rawProofFile)); err != nil {
		return nil, fmt.Errorf(
			"%w: decode confirmed base proof: %v",
			ErrAssetProofPathInvalid, err,
		)
	}
	baseProof, err := proofFile.LastProof()
	if err != nil {
		return nil, fmt.Errorf(
			"%w: read confirmed base proof tip: %v",
			ErrAssetProofPathInvalid, err,
		)
	}
	if err := baseProof.Asset.Validate(); err != nil {
		return nil, fmt.Errorf(
			"%w: validate confirmed base asset: %v",
			ErrAssetProofPathInvalid, err,
		)
	}

	return baseProof, nil
}

// decodeAssetProofPathStep decodes one step and checks its structure.
// wantWitnesses pins how many asset witnesses the transition must carry;
// zero accepts any complete set, which the per-step helpers use because
// they have no path context to pin the count with. Validate and Verify
// always pin it.
func decodeAssetProofPathStep(step *AssetProofPathStep,
	wantWitnesses int) (*proof.Proof, error) {
	if step == nil {
		return nil, fmt.Errorf("%w: nil step", ErrAssetProofPathInvalid)
	}
	if len(step.TransitionProof) == 0 {
		return nil, fmt.Errorf(
			"%w: transition proof is required",
			ErrAssetProofPathInvalid,
		)
	}
	if len(step.TransitionProof) > AssetProofPathMaxStepSize {
		return nil, fmt.Errorf(
			"%w: transition proof exceeds %d bytes",
			ErrAssetProofPathInvalid, AssetProofPathMaxStepSize,
		)
	}

	transition, err := proof.Decode(step.TransitionProof)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: decode transition proof: %v",
			ErrAssetProofPathInvalid, err,
		)
	}
	if transition.Version != proof.TransitionV1 {
		return nil, fmt.Errorf(
			"%w: transition proof must be v1",
			ErrAssetProofPathInvalid,
		)
	}
	if transition.Asset.Version != asset.V1 {
		return nil, fmt.Errorf(
			"%w: transition asset must be v1",
			ErrAssetProofPathInvalid,
		)
	}
	if err := transition.Asset.Validate(); err != nil {
		return nil, fmt.Errorf(
			"%w: validate transition asset: %v",
			ErrAssetProofPathInvalid, err,
		)
	}
	if transition.Asset.IsGenesisAsset() {
		return nil, fmt.Errorf(
			"%w: unconfirmed genesis proofs are unsupported",
			ErrAssetProofPathInvalid,
		)
	}
	if transition.Asset.LockTime != 0 ||
		transition.Asset.RelativeLockTime != 0 {

		return nil, fmt.Errorf(
			"%w: asset timelocks are unsupported",
			ErrAssetProofPathInvalid,
		)
	}
	if len(transition.AdditionalInputs) != 0 {
		return nil, fmt.Errorf(
			"%w: asset merges and additional input paths are unsupported",
			ErrAssetProofPathInvalid,
		)
	}
	assetPredecessors := 0
	for i, input := range transition.AnchorTx.TxIn {
		if input == nil {
			return nil, fmt.Errorf(
				"%w: anchor input %d is nil",
				ErrAssetProofPathInvalid, i,
			)
		}
		if input.PreviousOutPoint == transition.PrevOut {
			assetPredecessors++
		}
	}
	if assetPredecessors != 1 {
		return nil, fmt.Errorf(
			"%w: transition must spend its asset predecessor exactly "+
				"once, found %d", ErrAssetProofPathInvalid,
			assetPredecessors,
		)
	}
	if transition.ChallengeWitness != nil {
		return nil, fmt.Errorf(
			"%w: ownership proofs are unsupported",
			ErrAssetProofPathInvalid,
		)
	}

	isSplit := transition.Asset.HasSplitCommitmentWitness()
	var witnesses []asset.Witness
	if isSplit {
		if transition.SplitRootProof == nil {
			return nil, fmt.Errorf(
				"%w: split output proof is missing its root proof",
				ErrAssetProofPathInvalid,
			)
		}

		// A split leaf is authorized by the complete transition witness
		// on the root asset embedded in its split commitment proof.
		splitWitness := transition.Asset.PrevWitnesses[0]
		rootAsset := &splitWitness.SplitCommitment.RootAsset
		if rootAsset.Version != asset.V1 {
			return nil, fmt.Errorf(
				"%w: split root asset must be v1",
				ErrAssetProofPathInvalid,
			)
		}
		if rootAsset.LockTime != 0 || rootAsset.RelativeLockTime != 0 {
			return nil, fmt.Errorf(
				"%w: split root asset timelocks are unsupported",
				ErrAssetProofPathInvalid,
			)
		}
		if err := rootAsset.Validate(); err != nil {
			return nil, fmt.Errorf(
				"%w: validate split root asset: %v",
				ErrAssetProofPathInvalid, err,
			)
		}
		witnesses = rootAsset.PrevWitnesses
	} else {
		if transition.SplitRootProof != nil {
			return nil, fmt.Errorf(
				"%w: non-split transition has a split root proof",
				ErrAssetProofPathInvalid,
			)
		}
		witnesses = transition.Asset.PrevWitnesses
	}

	if len(witnesses) == 0 {
		return nil, fmt.Errorf(
			"%w: transition must contain at least one complete "+
				"asset witness", ErrAssetProofPathInvalid,
		)
	}
	if wantWitnesses > 0 && len(witnesses) != wantWitnesses {
		return nil, fmt.Errorf(
			"%w: transition must contain %d complete asset "+
				"witnesses, found %d", ErrAssetProofPathInvalid,
			wantWitnesses, len(witnesses),
		)
	}

	// Every witness must be complete, and exactly one of them must spend
	// the step's declared predecessor. A merging transition may carry
	// more, but each of those is pinned to a declared base outpoint by
	// verifyAssetProofPathBaseBinding, so none can hide an unproven
	// input.
	predecessors := 0
	for i := range witnesses {
		witness := &witnesses[i]
		if witness.PrevID == nil || len(witness.TxWitness) == 0 ||
			witness.SplitCommitment != nil {

			return nil, fmt.Errorf(
				"%w: asset witness %d is incomplete",
				ErrAssetProofPathInvalid, i,
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
			ErrAssetProofPathInvalid, predecessors,
		)
	}

	return transition, nil
}

// stepWitnessCount returns the number of asset witnesses the step at the
// given index must carry. Only a multi-base path's first step merges more
// than one predecessor; every later step spends a single tree node.
func (p *AssetProofPath) stepWitnessCount(index int) int {
	if index == 0 {
		return 1 + len(p.AdditionalBaseProofs)
	}

	return 1
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
		ErrAssetProofPathInvalid,
	)
}

func (assetProofPathChainLookup) MeanBlockTimestamp(context.Context,
	uint32) (time.Time, error) {

	return time.Time{}, fmt.Errorf(
		"%w: absolute timelock lookup is unsupported",
		ErrAssetProofPathInvalid,
	)
}

func verifyAssetProofPathIsolatedOutput(transition *proof.Proof) error {
	if transition == nil ||
		transition.InclusionProof.CommitmentProof == nil {

		return fmt.Errorf(
			"%w: missing inclusion commitment proof",
			ErrAssetProofPathInvalid,
		)
	}
	if transition.InclusionProof.InternalKey == nil {
		return fmt.Errorf(
			"%w: missing anchor internal key",
			ErrAssetProofPathInvalid,
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
			ErrAssetProofPathInvalid, outputIndex,
		)
	}
	anchorOutput := transition.AnchorTx.TxOut[outputIndex]
	if anchorOutput == nil {
		return fmt.Errorf(
			"%w: anchor output %d is nil", ErrAssetProofPathInvalid,
			outputIndex,
		)
	}
	actualScript := anchorOutput.PkScript
	if !bytes.Equal(expectedScript, actualScript) {
		return fmt.Errorf(
			"%w: selected output is not an isolated asset commitment",
			ErrAssetProofPathPassiveAssets,
		)
	}

	return nil
}

type assetProofPathDerivedSummary struct {
	assetRef       AssetRef
	issuanceID     AssetID
	assetType      AssetType
	amount         uint64
	scriptKey      PubKey
	anchorOutpoint Outpoint
	anchorValueSat int64
}

func summarizeAssetProofPathStep(step *AssetProofPathStep,
	transition *proof.Proof) (*AssetProofPathStepSummary, error) {

	derived, err := summarizeAssetProof(transition)
	if err != nil {
		return nil, err
	}
	contentID, err := step.ContentID()
	if err != nil {
		return nil, err
	}

	return &AssetProofPathStepSummary{
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
			"%w: nil asset proof", ErrAssetProofPathInvalid,
		)
	}
	if assetProof.Asset.Type != asset.Normal &&
		assetProof.Asset.Type != asset.Collectible {

		return nil, fmt.Errorf(
			"%w: unknown asset type %d", ErrAssetProofPathInvalid,
			assetProof.Asset.Type,
		)
	}
	if assetProof.Asset.ScriptKey.PubKey == nil {
		return nil, fmt.Errorf(
			"%w: missing asset script key", ErrAssetProofPathInvalid,
		)
	}

	var issuanceID AssetID
	nativeID := assetProof.Asset.ID()
	copy(issuanceID[:], nativeID[:])

	var groupKey *PubKey
	if assetProof.Asset.GroupKey != nil {
		parsedGroupKey, err := ParsePubKey(
			assetProof.Asset.GroupKey.GroupPubKey.SerializeCompressed(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid group key: %v",
				ErrAssetProofPathInvalid, err,
			)
		}
		groupKey = &parsedGroupKey
	}

	assetType := AssetType(assetProof.Asset.Type)
	assetRef := AssetRefFromTypedAsset(
		issuanceID, groupKey, assetType,
	)
	scriptKey, err := ParseTaprootPubKey(
		assetProof.Asset.ScriptKey.PubKey.SerializeCompressed(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid script key: %v", ErrAssetProofPathInvalid,
			err,
		)
	}
	outputIndex := assetProof.InclusionProof.OutputIndex
	if outputIndex >= uint32(len(assetProof.AnchorTx.TxOut)) {
		return nil, fmt.Errorf(
			"%w: anchor output index %d is out of range",
			ErrAssetProofPathInvalid, outputIndex,
		)
	}
	anchorOutput := assetProof.AnchorTx.TxOut[outputIndex]
	if anchorOutput == nil {
		return nil, fmt.Errorf(
			"%w: anchor output %d is nil", ErrAssetProofPathInvalid,
			outputIndex,
		)
	}
	if anchorOutput.Value < 0 {
		return nil, fmt.Errorf(
			"%w: anchor output %d value is negative",
			ErrAssetProofPathInvalid, outputIndex,
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

func sdkOutpoint(outpoint wire.OutPoint) Outpoint {
	var txid [32]byte
	copy(txid[:], outpoint.Hash[:])

	return Outpoint{
		Txid:  txid,
		Index: outpoint.Index,
	}
}

func taggedAssetProofPathHash(tag string, content []byte) Hash {
	tagHash := sha256.Sum256([]byte(tag))
	hasher := sha256.New()
	_, _ = hasher.Write(tagHash[:])
	_, _ = hasher.Write(tagHash[:])
	_, _ = hasher.Write(content)

	var result Hash
	copy(result[:], hasher.Sum(nil))
	return result
}
