package tapsdk

import "fmt"

// CustomAnchorTransferPackage is the persistable post-commit recovery
// boundary for an advanced custom-anchor asset transaction.
//
// Callers should persist this package before handing AnchorPsbt to an external
// signer or broadcaster. Publish/log retries after process restart should be
// driven from the persisted package plus the final signed anchor PSBT bytes.
type CustomAnchorTransferPackage struct {
	// AnchorPsbt is the committed BTC-level anchor PSBT before external
	// signing or external broadcast.
	AnchorPsbt []byte

	// ActiveVirtualPsbts are the committed active virtual asset PSBTs.
	ActiveVirtualPsbts [][]byte

	// PassiveVirtualPsbts are committed passive virtual asset PSBTs that
	// must remain paired with the active PSBTs when publishing or retrying.
	PassiveVirtualPsbts [][]byte

	// ChangeOutputIndex is the backend-selected change output index, or -1
	// when no change output exists.
	ChangeOutputIndex int32

	// LockedUTXOs are wallet UTXO locks held for backend-funded anchors.
	LockedUTXOs []CustomAnchorLockedUTXO

	// Inputs summarize the committed asset inputs without exposing
	// taproot-assets implementation types.
	Inputs []CustomAnchorAssetInputSummary

	// Outputs summarize the committed asset outputs without exposing
	// taproot-assets implementation types.
	Outputs []CustomAnchorAssetOutputSummary

	// ProofUpdates carry the proof metadata callers must keep for receiver
	// proof delivery, import, register, export, or retry flows.
	ProofUpdates []CustomAnchorProofUpdate

	// Publish stores publish/log retry metadata that is independent from
	// the final signed anchor PSBT bytes.
	Publish CustomAnchorPublishMetadata
}

// CustomAnchorLockedUTXO describes a BTC UTXO locked while funding an anchor.
type CustomAnchorLockedUTXO struct {
	// Outpoint identifies the locked BTC output.
	Outpoint Outpoint

	// ValueSat is the value of the locked output when known.
	ValueSat uint64

	// LockID is the backend lock identifier when the backend exposes one.
	LockID []byte

	// ExpirationUnixSeconds is the UTC Unix timestamp when the lock
	// expires.
	ExpirationUnixSeconds int64
}

// CustomAnchorAssetInputSummary describes one committed asset input.
type CustomAnchorAssetInputSummary struct {
	// AssetRef is the caller-facing asset identity requested for the input.
	AssetRef AssetRef

	// IssuanceID is the concrete asset issuance/tranche spent by this
	// input.
	IssuanceID AssetID

	// AssetType is the concrete asset type for the input.
	AssetType AssetType

	// AnchorOutpoint is the BTC output that held the spent asset
	// commitment.
	AnchorOutpoint Outpoint

	// ScriptKey is the spent asset script key.
	ScriptKey PubKey

	// Amount is the number of asset units spent.
	Amount uint64

	// ProofFileHash identifies the proof file that authorized this input
	// when the input came from a proof-file source.
	ProofFileHash Hash
}

// CustomAnchorAssetOutputSummary describes one committed asset output.
type CustomAnchorAssetOutputSummary struct {
	// AssetRef is the caller-facing asset identity created at this output.
	AssetRef AssetRef

	// IssuanceID is the concrete asset issuance/tranche created at this
	// output.
	IssuanceID AssetID

	// AssetType is the concrete asset type for the output.
	AssetType AssetType

	// AnchorOutpoint is the BTC output that holds the new asset commitment.
	AnchorOutpoint Outpoint

	// AnchorOutputIndex is the output index in the anchor transaction.
	AnchorOutputIndex uint32

	// AnchorValueSat is the BTC value assigned to the asset-bearing output.
	AnchorValueSat int64

	// ScriptKey is the asset script key for the new output.
	ScriptKey PubKey

	// Amount is the number of asset units created at this output.
	Amount uint64
}

// CustomAnchorProofUpdate describes proof data needed after commit.
type CustomAnchorProofUpdate struct {
	// OutputIndex identifies the committed asset output this proof metadata
	// belongs to.
	OutputIndex int

	// AssetRef is the caller-facing asset identity for the proof update.
	AssetRef AssetRef

	// IssuanceID is the concrete asset issuance/tranche in the proof
	// update.
	IssuanceID AssetID

	// ScriptKey identifies the receiver asset script key.
	ScriptKey PubKey

	// AnchorOutpoint identifies the anchor output referenced by the proof.
	AnchorOutpoint Outpoint

	// ProofBlob is the proof suffix or proof metadata returned by the
	// backend.
	ProofBlob []byte

	// ProofCourierAddr records where the completed proof should be
	// delivered.
	ProofCourierAddr string
}

// CustomAnchorPublishMetadata contains publish/log retry metadata.
type CustomAnchorPublishMetadata struct {
	// SkipAnchorTxBroadcast asks the backend to log without broadcasting
	// the final anchor transaction.
	SkipAnchorTxBroadcast bool

	// Label is an optional backend transfer label.
	Label string

	// ExternalBroadcast records that the caller intends to broadcast, or
	// has already broadcast, the final anchor transaction outside the
	// backend.
	ExternalBroadcast bool
}

// Validate checks that the package contains the fields required to resume a
// publish/log attempt after restart.
func (p *CustomAnchorTransferPackage) Validate() error {
	if p == nil {
		return fmt.Errorf("nil custom anchor transfer package")
	}
	if len(p.AnchorPsbt) == 0 {
		return fmt.Errorf("anchor PSBT is required")
	}
	if len(p.ActiveVirtualPsbts) == 0 {
		return fmt.Errorf(
			"at least one active virtual PSBT is required",
		)
	}
	if p.ChangeOutputIndex < -1 {
		return fmt.Errorf("change output index must be -1 or greater")
	}

	if err := validatePackagePsbts(
		"active virtual PSBT", p.ActiveVirtualPsbts,
	); err != nil {
		return err
	}
	if err := validatePackagePsbts(
		"passive virtual PSBT", p.PassiveVirtualPsbts,
	); err != nil {
		return err
	}

	for i, input := range p.Inputs {
		if err := validatePackageAssetRef(
			"input", i, input.AssetRef,
		); err != nil {
			return err
		}
		if input.Amount == 0 {
			return fmt.Errorf("input %d amount is required", i)
		}
	}

	for i, output := range p.Outputs {
		if err := validatePackageAssetRef(
			"output", i, output.AssetRef,
		); err != nil {
			return err
		}
		if output.Amount == 0 {
			return fmt.Errorf("output %d amount is required", i)
		}
		if output.AnchorValueSat < 0 {
			return fmt.Errorf(
				"output %d anchor value is negative", i,
			)
		}
	}

	for i, update := range p.ProofUpdates {
		if err := validatePackageAssetRef(
			"proof update", i, update.AssetRef,
		); err != nil {
			return err
		}
		if update.OutputIndex < 0 {
			return fmt.Errorf(
				"proof update %d output index is negative", i,
			)
		}
	}

	return nil
}

// Clone returns a deep copy of the package so callers can snapshot a committed
// state before external signing or broadcast mutates host-owned buffers.
func (p *CustomAnchorTransferPackage) Clone() *CustomAnchorTransferPackage {
	if p == nil {
		return nil
	}

	clone := *p
	clone.AnchorPsbt = cloneBytes(p.AnchorPsbt)
	clone.ActiveVirtualPsbts = cloneByteSlices(p.ActiveVirtualPsbts)
	clone.PassiveVirtualPsbts = cloneByteSlices(p.PassiveVirtualPsbts)
	clone.LockedUTXOs = cloneLockedUTXOs(p.LockedUTXOs)
	clone.Inputs = append([]CustomAnchorAssetInputSummary(nil), p.Inputs...)
	clone.Outputs = append(
		[]CustomAnchorAssetOutputSummary(nil), p.Outputs...,
	)
	clone.ProofUpdates = cloneProofUpdates(p.ProofUpdates)

	return &clone
}

func validatePackagePsbts(label string, psbts [][]byte) error {
	for i, psbt := range psbts {
		if len(psbt) == 0 {
			return fmt.Errorf("%s %d is empty", label, i)
		}
	}

	return nil
}

func validatePackageAssetRef(scope string, index int, ref AssetRef) error {
	if ref.IsZero() {
		return fmt.Errorf("%s %d asset ref is required", scope, index)
	}
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%s %d asset ref: %w", scope, index, err)
	}

	return nil
}

func cloneBytes(src []byte) []byte {
	if src == nil {
		return nil
	}

	return append([]byte(nil), src...)
}

func cloneByteSlices(src [][]byte) [][]byte {
	if src == nil {
		return nil
	}

	clone := make([][]byte, len(src))
	for i := range src {
		clone[i] = cloneBytes(src[i])
	}

	return clone
}

func cloneLockedUTXOs(
	src []CustomAnchorLockedUTXO) []CustomAnchorLockedUTXO {

	if src == nil {
		return nil
	}

	clone := append([]CustomAnchorLockedUTXO(nil), src...)
	for i := range clone {
		clone[i].LockID = cloneBytes(src[i].LockID)
	}

	return clone
}

func cloneProofUpdates(
	src []CustomAnchorProofUpdate) []CustomAnchorProofUpdate {

	if src == nil {
		return nil
	}

	clone := append([]CustomAnchorProofUpdate(nil), src...)
	for i := range clone {
		clone[i].ProofBlob = cloneBytes(src[i].ProofBlob)
	}

	return clone
}
