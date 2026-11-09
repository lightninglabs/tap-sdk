package customanchor

import (
	"bytes"
	"fmt"
	"math/bits"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/internal/anchor"
)

// Request contains the SDK-owned inputs needed to build an
// advanced custom-anchor asset transaction.
type Request struct {
	// Inputs are the proof-selected asset inputs to spend.
	Inputs []AssetInput

	// Outputs are the logical asset outputs to create.
	Outputs []AssetOutput

	// AnchorPSBT is the caller-visible BTC anchor transaction template.
	AnchorPSBT []byte

	// Funding selects exactly one high-level anchor funding mode.
	Funding FundingPlan

	// PassiveAssets controls how co-anchored passive assets are handled.
	PassiveAssets PassiveAssets

	// LossPolicy bounds explicitly requested protocol burn outputs. Asset
	// amounts must otherwise be conserved exactly.
	LossPolicy LossPolicy

	// SigningPlans select the trusted external spend path for each BTC anchor
	// input. PSBT-bound validation occurs after the anchor template is decoded.
	SigningPlans []InputSigningPlan
}

// AssetInput identifies exact asset units selected by a proof file.
type AssetInput struct {
	// ID is a stable caller-defined identifier for this logical input.
	ID string

	// AssetRef is the semantic asset identity expected at the selected proof
	// source tip.
	AssetRef tapsdk.AssetRef

	// Amount must exactly match the amount at the selected proof source tip.
	Amount uint64

	// ProofFile is a complete confirmed proof file. Exactly one of ProofFile
	// and ProofPath must be set.
	ProofFile []byte

	// ProofPath is a verified confirmed base plus zero or more compact
	// unconfirmed transitions. It requires a ConfirmedProofVerifier on the
	// builder, its UnconfirmedAnchorVerifier extension when steps are present,
	// and caller-provided asset witnesses.
	ProofPath *ProofPath

	// Witness selects how the virtual asset input is authorized.
	Witness AssetWitnessPlan
}

// AssetWitnessMode selects who supplies the virtual asset witness.
type AssetWitnessMode uint8

const (
	// AssetWitnessUnspecified is not a usable witness mode.
	AssetWitnessUnspecified AssetWitnessMode = iota

	// AssetWitnessBackendSigner asks the backend to sign the asset
	// input. No caller witness stack may be present.
	AssetWitnessBackendSigner

	// AssetWitnessCallerProvided uses the exact caller-provided asset
	// witness stack.
	AssetWitnessCallerProvided
)

// AssetWitnessPlan describes how a virtual asset input is authorized.
type AssetWitnessPlan struct {
	// Mode selects backend signing or a caller-provided stack.
	Mode AssetWitnessMode

	// Stack is the final witness stack for caller-provided mode.
	Stack [][]byte
}

// AssetOutput is one logical asset output in a custom-anchor plan.
type AssetOutput struct {
	// ID is a stable caller-defined identifier used across planning, signing,
	// proof delivery, and persistence.
	ID string

	// AssetRef is the semantic identity of the asset being created.
	AssetRef tapsdk.AssetRef

	// Amount is the number of asset units assigned to this output.
	Amount uint64

	// AnchorOutputIndex is the output index in the caller anchor PSBT.
	AnchorOutputIndex uint32

	// AnchorValueSat is the exact BTC value required at the anchor output.
	AnchorValueSat uint64

	// Script selects exactly one asset script construction mode.
	Script AssetScriptPlan

	// Anchor describes the anchor internal key and BTC tapscript sibling.
	Anchor OutputPlan

	// ProofDelivery contains opaque receiver proof-delivery metadata.
	ProofDelivery AssetProofDelivery

	// Timelocks contains asset-level lock times. Nonzero values are rejected
	// by the first custom-anchor implementation.
	Timelocks AssetTimelocks
}

// AssetScriptMode identifies an asset output script construction mode.
type AssetScriptMode uint8

const (
	// AssetScriptUnspecified is not a usable script mode.
	AssetScriptUnspecified AssetScriptMode = iota

	// AssetScriptWallet derives a script key from the backend wallet.
	AssetScriptWallet

	// AssetScriptExternal uses an externally supplied script key.
	AssetScriptExternal

	// AssetScriptOPTrue creates a unique OP_TRUE asset tapscript.
	AssetScriptOPTrue

	// AssetScriptBurn creates an explicitly unspendable burn output.
	AssetScriptBurn
)

// AssetScriptPlan is a tagged asset script construction plan. Exactly
// one variant must be non-nil and must match Mode.
type AssetScriptPlan struct {
	// Mode selects the active script variant.
	Mode AssetScriptMode

	// Wallet selects a backend wallet-derived script key.
	Wallet *AssetWalletScriptPlan

	// External selects a caller-supplied script key.
	External *AssetExternalScriptPlan

	// OPTrue selects a unique OP_TRUE asset tapscript.
	OPTrue *AssetOPTrueScriptPlan

	// Burn selects an explicitly unspendable asset output.
	Burn *AssetBurnScriptPlan
}

// AssetWalletScriptPlan asks the backend wallet for an asset script key.
type AssetWalletScriptPlan struct {
	// KeyLocator optionally requests a specific wallet key. Nil asks the
	// backend to derive a fresh key using its normal asset key family.
	KeyLocator *tapsdk.KeyLocator
}

// AssetExternalScriptPlan uses a complete caller-supplied script key.
type AssetExternalScriptPlan struct {
	// ScriptKey is the external Taproot Asset script key.
	ScriptKey tapsdk.ScriptKey
}

// AssetOPTrueScriptPlan describes a unique OP_TRUE asset tapscript.
type AssetOPTrueScriptPlan struct {
	// InternalKey is unique to this logical output. Reusing it can produce a
	// duplicate asset commitment key and is rejected by the builder.
	InternalKey tapsdk.KeyDescriptor
}

// AssetOPTrueSpendInfo contains the complete Taproot script-path data
// needed to spend an SDK-built OP_TRUE asset output. It deliberately uses only
// SDK and primitive types so applications do not need taproot-assets imports.
type AssetOPTrueSpendInfo struct {
	// LeafScript is the OP_TRUE tapscript program.
	LeafScript []byte `json:"leaf_script"`

	// LeafVersion is the tapscript leaf version committed by ControlBlock.
	LeafVersion uint8 `json:"leaf_version"`

	// ControlBlock proves LeafScript against the output asset script key.
	ControlBlock []byte `json:"control_block"`

	// InternalKey is the Taproot internal key committed by ControlBlock.
	InternalKey tapsdk.PubKey `json:"internal_key"`
}

// Validate checks that the spend data reveals the expected OP_TRUE leaf and
// commits to scriptKey. This lets a persisted package be checked without
// importing taproot-assets implementation packages.
func (i *AssetOPTrueSpendInfo) Validate(scriptKey tapsdk.PubKey) error {
	if i == nil {
		return fmt.Errorf("nil OP_TRUE spend info")
	}
	if !bytes.Equal(i.LeafScript, []byte{txscript.OP_TRUE}) {
		return fmt.Errorf("OP_TRUE spend leaf must be OP_TRUE")
	}
	if i.LeafVersion != uint8(txscript.BaseLeafVersion) {
		return fmt.Errorf("unsupported OP_TRUE spend leaf version %d",
			i.LeafVersion)
	}
	if err := validateCustomAnchorPubKey(
		"OP_TRUE spend internal key", i.InternalKey,
	); err != nil {
		return err
	}
	if err := validateCustomAnchorPubKey(
		"OP_TRUE asset script key", scriptKey,
	); err != nil {
		return err
	}

	controlBlock, err := txscript.ParseControlBlock(i.ControlBlock)
	if err != nil {
		return fmt.Errorf("parse OP_TRUE spend control block: %w", err)
	}
	if uint8(controlBlock.LeafVersion) != i.LeafVersion {
		return fmt.Errorf("OP_TRUE spend leaf version does not match " +
			"control block")
	}
	internalKey, err := schnorr.ParsePubKey(i.InternalKey[1:])
	if err != nil {
		return fmt.Errorf("parse OP_TRUE spend internal key: %w", err)
	}
	if !bytes.Equal(
		schnorr.SerializePubKey(controlBlock.InternalKey),
		schnorr.SerializePubKey(internalKey),
	) {

		return fmt.Errorf("OP_TRUE spend internal key does not match " +
			"control block")
	}

	outputKey, err := schnorr.ParsePubKey(scriptKey[1:])
	if err != nil {
		return fmt.Errorf("parse OP_TRUE asset script key: %w", err)
	}
	if err := txscript.VerifyTaprootLeafCommitment(
		controlBlock, schnorr.SerializePubKey(outputKey), i.LeafScript,
	); err != nil {
		return fmt.Errorf("verify OP_TRUE spend commitment: %w", err)
	}

	return nil
}

// Clone returns a deep copy whose script and control block do not alias the
// source spend information.
func (i *AssetOPTrueSpendInfo) Clone() *AssetOPTrueSpendInfo {
	if i == nil {
		return nil
	}

	clone := *i
	clone.LeafScript = bytes.Clone(i.LeafScript)
	clone.ControlBlock = bytes.Clone(i.ControlBlock)

	return &clone
}

// WitnessStack returns the complete zero-argument asset witness for the
// OP_TRUE leaf. The returned byte slices never alias the spend information.
func (i *AssetOPTrueSpendInfo) WitnessStack() [][]byte {
	if i == nil {
		return nil
	}

	return [][]byte{
		bytes.Clone(i.LeafScript),
		bytes.Clone(i.ControlBlock),
	}
}

// AssetBurnScriptPlan marks an explicitly unspendable asset output.
type AssetBurnScriptPlan struct{}

// OutputPlan describes an asset-bearing BTC output.
type OutputPlan struct {
	// InternalKey is the Taproot internal key for the BTC anchor output.
	InternalKey tapsdk.InternalKey

	// Tapscript optionally supplies an inspectable or opaque sibling form.
	Tapscript TapscriptPlan
}

// TapscriptPlan represents the non-asset sibling committed at a
// BTC anchor output. At most one representation may be populated; leaving
// both empty selects a direct/BIP86-style anchor policy.
type TapscriptPlan struct {
	// TapLeaves is an ordered set of SDK tapscript leaves.
	TapLeaves []tapsdk.TapLeaf

	// SerializedSibling is an opaque serialized tapscript sibling.
	SerializedSibling []byte
}

// AssetProofDelivery stores host-owned proof-delivery metadata.
type AssetProofDelivery struct {
	// RecipientID is an optional stable recipient or delivery identifier.
	RecipientID string `json:"recipient_id"`

	// CourierAddress is an optional proof courier destination.
	CourierAddress string `json:"courier_address"`

	// OpaqueMetadata is preserved for the host application's delivery flow.
	OpaqueMetadata []byte `json:"opaque_metadata"`
}

// AssetTimelocks contains asset-level absolute and relative lock times.
type AssetTimelocks struct {
	// Absolute is the asset-level absolute lock time.
	Absolute uint64

	// Relative is the asset-level relative lock time.
	Relative uint64
}

// FundingMode selects a high-level anchor funding policy.
type FundingMode uint8

const (
	// FundingUnspecified is not a usable funding mode.
	FundingUnspecified FundingMode = iota

	// FundingWalletFunded lets the backend wallet fund and add
	// change to the anchor transaction.
	FundingWalletFunded

	// FundingCallerFundedExact preserves the exact caller-funded
	// anchor template without backend funding.
	FundingCallerFundedExact

	// FundingExternalP2AFeeBump preserves an externally funded
	// parent containing a caller-selected P2A output.
	FundingExternalP2AFeeBump
)

// FundingPlan is a tagged high-level funding plan. Exactly one
// variant must be non-nil and must match Mode.
type FundingPlan struct {
	// Mode selects the active funding variant.
	Mode FundingMode

	// WalletFunded configures backend wallet funding.
	WalletFunded *WalletFunding

	// CallerFundedExact selects an exact caller-funded template.
	CallerFundedExact *CallerFundedExact

	// ExternalP2AFeeBump selects an external P2A fee-bump plan.
	ExternalP2AFeeBump *ExternalP2AFeeBump
}

// WalletFunding configures backend wallet anchor funding.
type WalletFunding struct {
	// ChangeOutput selects backend change handling.
	ChangeOutput tapsdk.AnchorChangeOutput

	// Fee selects the backend fee policy.
	Fee tapsdk.AnchorFee

	// MaxFeeSat is the hard absolute fee ceiling for the committed anchor.
	// It is checked locally before any Bitcoin input is signed. This remains
	// required even when Fee uses a confirmation target because the backend
	// fee estimate is not itself a safe spending limit.
	MaxFeeSat uint64

	// CustomLockID is the required deterministic 32-byte identifier for
	// backend wallet UTXO locks and ambiguous-commit recovery.
	CustomLockID []byte

	// LockExpirationSeconds optionally overrides the backend lock duration.
	LockExpirationSeconds uint64
}

// CallerFundedExact selects an exact caller-funded anchor PSBT.
type CallerFundedExact struct{}

// ExternalP2AFeeBump identifies the caller's P2A output.
type ExternalP2AFeeBump struct {
	// P2AOutputIndex is the zero-based P2A output index in AnchorPSBT.
	P2AOutputIndex uint32
}

// PassivePolicy selects how co-anchored passive assets are handled.
type PassivePolicy uint8

const (
	// PassiveReject rejects any discovered passive asset. This is
	// the safe zero-value default.
	PassiveReject PassivePolicy = iota

	// PassivePreserve asks the backend to preserve every passive
	// asset it discovers.
	PassivePreserve

	// PassiveCallerReanchor uses caller-supplied passive packets.
	PassiveCallerReanchor
)

// PassiveAssets describes passive-asset handling for a plan.
type PassiveAssets struct {
	// Policy selects reject, backend preserve, or caller re-anchor behavior.
	Policy PassivePolicy

	// Packets are required only for caller re-anchor behavior.
	Packets []PassivePacket
}

// PassivePacket is a caller-supplied passive virtual packet.
type PassivePacket struct {
	// ID is a stable caller-defined identifier for the passive packet.
	ID string

	// AssetRef is the semantic identity of the passive asset.
	AssetRef tapsdk.AssetRef

	// Amount is the exact passive asset amount represented by the packet.
	Amount uint64

	// VirtualPSBT is the serialized passive virtual asset PSBT.
	VirtualPSBT []byte

	// ProofFile is the complete confirmed proof that selects the passive input.
	ProofFile []byte

	// ProofDelivery contains host-owned delivery metadata for the reanchored
	// passive proof.
	ProofDelivery AssetProofDelivery
}

// LossMode identifies whether irreversible asset loss is allowed.
type LossMode uint8

const (
	// LossReject rejects burn outputs and any amount imbalance.
	// This is the safe zero-value default.
	LossReject LossMode = iota

	// LossBurn permits only confirmed, per-asset bounded burns.
	LossBurn
)

// LossPolicy bounds explicitly requested asset destruction.
type LossPolicy struct {
	// Mode selects reject or explicitly bounded burn behavior.
	Mode LossMode

	// Allowances contains the maximum explicit burn for each asset.
	Allowances []LossAllowance

	// ConfirmIrreversibleLoss must be true before any burn is allowed.
	ConfirmIrreversibleLoss bool
}

// LossAllowance caps explicit burns for one semantic asset.
type LossAllowance struct {
	// AssetRef identifies the asset whose burn is being authorized.
	AssetRef tapsdk.AssetRef

	// MaxAmount is the maximum number of units that may be destroyed.
	MaxAmount uint64
}

// VerificationOrigin identifies where a check was performed.
type VerificationOrigin uint8

const (
	// VerificationOriginUnknown is not a trusted origin.
	VerificationOriginUnknown VerificationOrigin = iota

	// VerificationOriginLocal identifies an SDK-local check.
	VerificationOriginLocal

	// VerificationOriginBackend identifies a backend-trusted
	// check.
	VerificationOriginBackend

	// VerificationOriginHost identifies a check performed by the
	// integrating host through an injected verifier. It is trusted by the
	// host application, not independently reproduced by the SDK.
	VerificationOriginHost
)

// VerificationSeverity describes the impact of an issue.
type VerificationSeverity uint8

const (
	// VerificationSeverityUnknown is not a usable severity.
	VerificationSeverityUnknown VerificationSeverity = iota

	// VerificationSeverityInfo is informational.
	VerificationSeverityInfo

	// VerificationSeverityWarning does not invalidate a plan.
	VerificationSeverityWarning

	// VerificationSeverityError invalidates a plan.
	VerificationSeverityError
)

// VerificationScope identifies the subject of a check or issue.
type VerificationScope string

const (
	// VerificationScopeRequest covers the complete request.
	VerificationScopeRequest VerificationScope = "request"

	// VerificationScopeInputProof covers proof-selected inputs.
	VerificationScopeInputProof = VerificationScope(
		"input_proof",
	)

	// VerificationScopeAssetIdentity covers semantic and concrete
	// asset identity.
	VerificationScopeAssetIdentity = VerificationScope(
		"asset_identity",
	)

	// VerificationScopeAmount covers asset conservation.
	VerificationScopeAmount VerificationScope = "amount"

	// VerificationScopeOutputCommitment covers output asset
	// commitments.
	VerificationScopeOutputCommitment = VerificationScope(
		"output_commitment",
	)

	// VerificationScopeAnchorOutput covers the BTC anchor template.
	VerificationScopeAnchorOutput = VerificationScope(
		"anchor_output",
	)

	// VerificationScopePassiveAssets covers passive-asset policy.
	VerificationScopePassiveAssets = VerificationScope(
		"passive_assets",
	)

	// VerificationScopeLossPolicy covers burn and loss bounds.
	VerificationScopeLossPolicy = VerificationScope(
		"loss_policy",
	)

	// VerificationScopeCapability covers backend capabilities.
	VerificationScopeCapability = VerificationScope(
		"capability",
	)

	// VerificationScopeBackendTrust covers backend-only checks.
	VerificationScopeBackendTrust = VerificationScope(
		"backend_trust",
	)
)

// VerificationCode is a stable machine-readable check or issue
// code. Concrete verifiers own their code vocabulary.
type VerificationCode string

// VerificationCheck records one verification checkpoint.
type VerificationCheck struct {
	// Code is the stable machine-readable check code.
	Code VerificationCode

	// Scope identifies the subject being checked.
	Scope VerificationScope

	// Origin identifies whether the SDK, backend, or integrating host
	// performed the check.
	Origin VerificationOrigin

	// Passed reports whether the check succeeded.
	Passed bool

	// InputIndex optionally identifies an asset input.
	InputIndex *uint32

	// OutputIndex optionally identifies an anchor output.
	OutputIndex *uint32

	// OutputID optionally identifies a logical asset output.
	OutputID string

	// Message is diagnostic text and is not a stable API value.
	Message string
}

// VerificationIssue records a structured verification finding.
type VerificationIssue struct {
	// Code is the stable machine-readable issue code.
	Code VerificationCode

	// Scope identifies the affected part of the plan or package.
	Scope VerificationScope

	// Origin identifies whether the SDK, backend, or integrating host reported
	// the issue.
	Origin VerificationOrigin

	// Severity determines whether the issue invalidates the result.
	Severity VerificationSeverity

	// InputIndex optionally identifies an asset input.
	InputIndex *uint32

	// OutputIndex optionally identifies an anchor output.
	OutputIndex *uint32

	// OutputID optionally identifies a logical asset output.
	OutputID string

	// Message is diagnostic text and is not a stable API value.
	Message string
}

// VerificationResult contains inspectable checks and issues.
type VerificationResult struct {
	// Checks are the verification checkpoints that were attempted.
	Checks []VerificationCheck

	// Issues are structured informational, warning, and error findings.
	Issues []VerificationIssue
}

// Validate checks that the request is internally consistent before proof or
// PSBT decoding begins.
func (r *Request) Validate() error {
	if r == nil {
		return fmt.Errorf("nil custom anchor request")
	}
	if len(r.AnchorPSBT) == 0 {
		return fmt.Errorf("anchor PSBT is required")
	}
	if len(r.Inputs) == 0 {
		return fmt.Errorf("at least one asset input is required")
	}
	if len(r.Outputs) == 0 {
		return fmt.Errorf("at least one asset output is required")
	}

	if err := r.Funding.Validate(); err != nil {
		return fmt.Errorf("funding plan: %w", err)
	}
	if err := r.PassiveAssets.Validate(); err != nil {
		return fmt.Errorf("passive assets: %w", err)
	}
	if err := r.LossPolicy.Validate(); err != nil {
		return fmt.Errorf("loss policy: %w", err)
	}

	inputIDs := make(map[string]struct{}, len(r.Inputs))
	for idx := range r.Inputs {
		input := &r.Inputs[idx]
		if err := input.Validate(); err != nil {
			return fmt.Errorf("input %d: %w", idx, err)
		}
		if _, ok := inputIDs[input.ID]; ok {
			return fmt.Errorf("input %d has duplicate ID %q", idx,
				input.ID)
		}

		inputIDs[input.ID] = struct{}{}
	}

	outputIDs := make(map[string]struct{}, len(r.Outputs))
	anchorOutputs := make(map[uint32]AssetOutput)
	for idx := range r.Outputs {
		output := &r.Outputs[idx]
		if err := output.Validate(); err != nil {
			return fmt.Errorf("output %d: %w", idx, err)
		}
		if _, ok := outputIDs[output.ID]; ok {
			return fmt.Errorf("output %d has duplicate ID %q", idx,
				output.ID)
		}
		outputIDs[output.ID] = struct{}{}

		existing, ok := anchorOutputs[output.AnchorOutputIndex]
		if ok && !sameCustomAnchorOutput(existing, *output) {
			return fmt.Errorf("output %d conflicts with anchor output %d",
				idx, output.AnchorOutputIndex)
		}
		anchorOutputs[output.AnchorOutputIndex] = *output
	}

	return r.validateAmounts()
}

// Validate checks a proof-file asset input before proof decoding.
func (i *AssetInput) Validate() error {
	if i == nil {
		return fmt.Errorf("nil asset input")
	}
	if i.ID == "" {
		return fmt.Errorf("input ID is required")
	}
	if err := i.AssetRef.Validate(); err != nil {
		return fmt.Errorf("asset ref: %w", err)
	}
	if i.Amount == 0 {
		return fmt.Errorf("amount is required")
	}
	if (len(i.ProofFile) == 0) == (i.ProofPath == nil) {
		return fmt.Errorf("exactly one proof file or proof path is required")
	}
	if i.ProofPath != nil {
		if err := i.ProofPath.Validate(); err != nil {
			return fmt.Errorf("proof path: %w", err)
		}
		if i.Witness.Mode == AssetWitnessBackendSigner {
			return fmt.Errorf("proof path inputs require caller-provided " +
				"witnesses")
		}
	}

	return i.Witness.Validate()
}

// Validate checks that exactly one supported asset witness mode is selected.
func (p *AssetWitnessPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("nil asset witness plan")
	}

	switch p.Mode {
	case AssetWitnessBackendSigner:
		if len(p.Stack) != 0 {
			return fmt.Errorf("backend signer mode cannot include a " +
				"caller witness stack")
		}

		return nil

	case AssetWitnessCallerProvided:
		if len(p.Stack) == 0 {
			return fmt.Errorf("caller-provided witness stack is required")
		}

		return nil

	default:
		return fmt.Errorf("asset witness mode is required")
	}
}

// Validate checks a logical asset output before proof or PSBT decoding.
func (o *AssetOutput) Validate() error {
	if o == nil {
		return fmt.Errorf("nil asset output")
	}
	if o.ID == "" {
		return fmt.Errorf("output ID is required")
	}
	if err := o.AssetRef.Validate(); err != nil {
		return fmt.Errorf("asset ref: %w", err)
	}
	if o.Amount == 0 {
		return fmt.Errorf("amount is required")
	}
	if err := o.Script.Validate(); err != nil {
		return fmt.Errorf("script plan: %w", err)
	}
	if o.AnchorValueSat == 0 {
		return fmt.Errorf("anchor value is required")
	}
	if err := o.Anchor.Validate(); err != nil {
		return fmt.Errorf("anchor plan: %w", err)
	}
	if o.Timelocks.Absolute != 0 || o.Timelocks.Relative != 0 {
		return fmt.Errorf("asset timelocks are not supported")
	}

	return nil
}

// Validate checks that exactly one asset script variant is selected.
func (p *AssetScriptPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("nil asset script plan")
	}

	variantCount := countCustomAnchorVariants(
		p.Wallet != nil, p.External != nil, p.OPTrue != nil,
		p.Burn != nil,
	)
	if variantCount != 1 {
		return fmt.Errorf("asset script plan requires exactly one variant")
	}

	switch p.Mode {
	case AssetScriptWallet:
		if p.Wallet == nil {
			return fmt.Errorf("wallet script mode requires wallet variant")
		}

	case AssetScriptExternal:
		if p.External == nil {
			return fmt.Errorf("external script mode requires external variant")
		}
		if err := validateCustomAnchorPubKey(
			"external script key", p.External.ScriptKey.PubKey,
		); err != nil {
			return err
		}

	case AssetScriptOPTrue:
		if p.OPTrue == nil {
			return fmt.Errorf("OP_TRUE script mode requires OP_TRUE variant")
		}
		if err := validateCustomAnchorPubKey(
			"OP_TRUE internal key", p.OPTrue.InternalKey.RawKeyBytes,
		); err != nil {
			return err
		}

	case AssetScriptBurn:
		if p.Burn == nil {
			return fmt.Errorf("burn script mode requires burn variant")
		}

	default:
		return fmt.Errorf("asset script mode is required")
	}

	return nil
}

// Validate checks the anchor internal key and sibling representation.
func (p *OutputPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("nil anchor output plan")
	}
	if err := validateCustomAnchorPubKey(
		"anchor internal key", p.InternalKey.PubKey,
	); err != nil {
		return err
	}

	return p.Tapscript.Validate()
}

// Validate checks that at most one anchor sibling representation is present.
func (p *TapscriptPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("nil anchor tapscript plan")
	}

	hasLeaves := len(p.TapLeaves) != 0
	hasSerialized := len(p.SerializedSibling) != 0
	if hasLeaves && hasSerialized {
		return fmt.Errorf("anchor tapscript cannot contain both tap leaves " +
			"and a serialized sibling")
	}

	for idx, leaf := range p.TapLeaves {
		if len(leaf.Script) == 0 {
			return fmt.Errorf("tap leaf %d script is empty", idx)
		}
	}

	return nil
}

// Validate checks that exactly one high-level funding variant is selected.
func (p *FundingPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("nil custom anchor funding plan")
	}

	variantCount := countCustomAnchorVariants(
		p.WalletFunded != nil, p.CallerFundedExact != nil,
		p.ExternalP2AFeeBump != nil,
	)
	if variantCount != 1 {
		return fmt.Errorf("funding plan requires exactly one variant")
	}

	switch p.Mode {
	case FundingWalletFunded:
		if p.WalletFunded == nil {
			return fmt.Errorf("wallet-funded mode requires wallet variant")
		}
		if err := p.WalletFunded.ChangeOutput.Validate(); err != nil {
			return err
		}
		if err := p.WalletFunded.Fee.Validate(); err != nil {
			return err
		}
		if p.WalletFunded.MaxFeeSat == 0 {
			return fmt.Errorf("wallet-funded maximum fee is required")
		}
		if len(p.WalletFunded.CustomLockID) == 0 {
			return fmt.Errorf("wallet-funded custom lock ID is required")
		}
		if err := anchor.ValidateLockID(
			p.WalletFunded.CustomLockID,
		); err != nil {
			return err
		}
		if err := anchor.ValidateLockExpiration(
			p.WalletFunded.LockExpirationSeconds,
		); err != nil {
			return err
		}

	case FundingCallerFundedExact:
		if p.CallerFundedExact == nil {
			return fmt.Errorf("caller-funded mode requires exact variant")
		}

	case FundingExternalP2AFeeBump:
		if p.ExternalP2AFeeBump == nil {
			return fmt.Errorf("external P2A mode requires fee-bump variant")
		}

	default:
		return fmt.Errorf("custom anchor funding mode is required")
	}

	return nil
}

// Validate checks passive-asset policy and caller packet consistency.
func (p *PassiveAssets) Validate() error {
	if p == nil {
		return fmt.Errorf("nil passive-asset plan")
	}

	switch p.Policy {
	case PassiveReject, PassivePreserve:
		if len(p.Packets) != 0 {
			return fmt.Errorf("passive packets require caller re-anchor " +
				"policy")
		}

		return nil

	case PassiveCallerReanchor:
		if len(p.Packets) == 0 {
			return fmt.Errorf("caller re-anchor requires passive packets")
		}

	default:
		return fmt.Errorf("unknown passive-asset policy %d", p.Policy)
	}

	ids := make(map[string]struct{}, len(p.Packets))
	amounts := make(map[string]uint64)
	for idx := range p.Packets {
		packet := &p.Packets[idx]
		if packet.ID == "" {
			return fmt.Errorf("passive packet %d ID is required", idx)
		}
		if _, ok := ids[packet.ID]; ok {
			return fmt.Errorf("passive packet %d has duplicate ID %q",
				idx, packet.ID)
		}
		ids[packet.ID] = struct{}{}

		if err := packet.AssetRef.Validate(); err != nil {
			return fmt.Errorf("passive packet %d asset ref: %w", idx,
				err)
		}
		if packet.Amount == 0 {
			return fmt.Errorf("passive packet %d amount is required", idx)
		}
		if len(packet.VirtualPSBT) == 0 {
			return fmt.Errorf("passive packet %d virtual PSBT is required",
				idx)
		}
		if len(packet.ProofFile) == 0 {
			return fmt.Errorf("passive packet %d proof file is required", idx)
		}

		key := customAnchorAssetKey(packet.AssetRef)
		amount, err := checkedCustomAnchorAdd(
			amounts[key], packet.Amount,
		)
		if err != nil {
			return fmt.Errorf("passive packet amount for %q: %w",
				packet.AssetRef, err)
		}
		amounts[key] = amount
	}

	return nil
}

// Validate checks that any irreversible loss is explicit and bounded.
func (p *LossPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("nil custom anchor loss policy")
	}

	switch p.Mode {
	case LossReject:
		if len(p.Allowances) != 0 || p.ConfirmIrreversibleLoss {
			return fmt.Errorf("reject loss mode cannot include allowances " +
				"or confirmation")
		}

		return nil

	case LossBurn:
		if !p.ConfirmIrreversibleLoss {
			return fmt.Errorf("irreversible asset loss is not confirmed")
		}
		if len(p.Allowances) == 0 {
			return fmt.Errorf("at least one loss allowance is required")
		}

	default:
		return fmt.Errorf("unknown custom anchor loss mode %d", p.Mode)
	}

	seen := make(map[string]struct{}, len(p.Allowances))
	var total uint64
	for idx, allowance := range p.Allowances {
		if err := allowance.AssetRef.Validate(); err != nil {
			return fmt.Errorf("loss allowance %d asset ref: %w", idx,
				err)
		}
		if allowance.MaxAmount == 0 {
			return fmt.Errorf("loss allowance %d amount is required", idx)
		}

		key := customAnchorAssetKey(allowance.AssetRef)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("loss allowance %d duplicates asset %q",
				idx, allowance.AssetRef)
		}
		seen[key] = struct{}{}

		var err error
		total, err = checkedCustomAnchorAdd(total, allowance.MaxAmount)
		if err != nil {
			return fmt.Errorf("loss allowances: %w", err)
		}
	}

	return nil
}

// Clone returns a deep copy whose byte slices, witness stacks, variants, and
// metadata do not alias the request.
func (r *Request) Clone() *Request {
	if r == nil {
		return nil
	}

	clone := *r
	clone.AnchorPSBT = bytes.Clone(r.AnchorPSBT)

	if r.Inputs != nil {
		clone.Inputs = make([]AssetInput, len(r.Inputs))
		for idx := range r.Inputs {
			clone.Inputs[idx] = r.Inputs[idx]
			clone.Inputs[idx].ProofFile = bytes.Clone(
				r.Inputs[idx].ProofFile,
			)
			clone.Inputs[idx].ProofPath = r.Inputs[idx].ProofPath.Clone()
			clone.Inputs[idx].Witness.Stack =
				cloneCustomAnchorByteSlices(
					r.Inputs[idx].Witness.Stack,
				)
		}
	}

	if r.Outputs != nil {
		clone.Outputs = make([]AssetOutput, len(r.Outputs))
		for idx := range r.Outputs {
			clone.Outputs[idx] = cloneCustomAssetOutput(r.Outputs[idx])
		}
	}

	clone.Funding = cloneCustomAnchorFunding(r.Funding)
	if r.PassiveAssets.Packets != nil {
		clone.PassiveAssets.Packets = make(
			[]PassivePacket, len(r.PassiveAssets.Packets),
		)
		for idx := range r.PassiveAssets.Packets {
			clone.PassiveAssets.Packets[idx] =
				r.PassiveAssets.Packets[idx]
			clone.PassiveAssets.Packets[idx].VirtualPSBT = bytes.Clone(
				r.PassiveAssets.Packets[idx].VirtualPSBT,
			)
			clone.PassiveAssets.Packets[idx].ProofFile = bytes.Clone(
				r.PassiveAssets.Packets[idx].ProofFile,
			)
			clone.PassiveAssets.Packets[idx].ProofDelivery.OpaqueMetadata =
				bytes.Clone(
					r.PassiveAssets.Packets[idx].ProofDelivery.OpaqueMetadata,
				)
		}
	}
	clone.LossPolicy.Allowances = append(
		[]LossAllowance(nil), r.LossPolicy.Allowances...,
	)
	clone.SigningPlans = cloneCustomAnchorSigningPlans(r.SigningPlans)

	return &clone
}

// Validate checks the structure of a verification result. An empty result is
// invalid so an uninitialized verifier cannot be mistaken for success.
func (r *VerificationResult) Validate() error {
	if r == nil {
		return fmt.Errorf("nil custom anchor verification result")
	}
	if len(r.Checks) == 0 {
		return fmt.Errorf("at least one verification check is required")
	}

	for idx := range r.Checks {
		check := &r.Checks[idx]
		if check.Code == "" {
			return fmt.Errorf("verification check %d code is required", idx)
		}
		if !check.Scope.valid() {
			return fmt.Errorf("verification check %d has unknown scope %q",
				idx, check.Scope)
		}
		if !check.Origin.valid() {
			return fmt.Errorf("verification check %d has unknown origin %d",
				idx, check.Origin)
		}
	}

	for idx := range r.Issues {
		issue := &r.Issues[idx]
		if issue.Code == "" {
			return fmt.Errorf("verification issue %d code is required", idx)
		}
		if !issue.Scope.valid() {
			return fmt.Errorf("verification issue %d has unknown scope %q",
				idx, issue.Scope)
		}
		if !issue.Origin.valid() {
			return fmt.Errorf("verification issue %d has unknown origin %d",
				idx, issue.Origin)
		}
		if !issue.Severity.valid() {
			return fmt.Errorf("verification issue %d has unknown severity %d",
				idx, issue.Severity)
		}
	}

	return nil
}

// Valid reports whether the result is well-formed, every check passed, and no
// error-severity issue was reported.
func (r *VerificationResult) Valid() bool {
	if err := r.Validate(); err != nil {
		return false
	}

	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	for _, issue := range r.Issues {
		if issue.Severity == VerificationSeverityError {
			return false
		}
	}

	return true
}

// Clone returns a deep copy of the verification result and optional indices.
//
//nolint:lll // Keeping the exported receiver and return type explicit is clearer.
func (r *VerificationResult) Clone() *VerificationResult {
	if r == nil {
		return nil
	}

	clone := *r
	clone.Checks = append(
		[]VerificationCheck(nil), r.Checks...,
	)
	for idx := range clone.Checks {
		clone.Checks[idx].InputIndex = cloneUint32(
			r.Checks[idx].InputIndex,
		)
		clone.Checks[idx].OutputIndex = cloneUint32(
			r.Checks[idx].OutputIndex,
		)
	}

	clone.Issues = append(
		[]VerificationIssue(nil), r.Issues...,
	)
	for idx := range clone.Issues {
		clone.Issues[idx].InputIndex = cloneUint32(
			r.Issues[idx].InputIndex,
		)
		clone.Issues[idx].OutputIndex = cloneUint32(
			r.Issues[idx].OutputIndex,
		)
	}

	return &clone
}

func (r *Request) validateAmounts() error {
	totals := make(map[string]*customAnchorAmountTotals)
	for idx := range r.Inputs {
		input := &r.Inputs[idx]
		entry := customAnchorTotals(totals, input.AssetRef)

		amount, err := checkedCustomAnchorAdd(entry.Inputs, input.Amount)
		if err != nil {
			return fmt.Errorf("input amount for asset %q: %w",
				input.AssetRef, err)
		}
		entry.Inputs = amount
	}

	for idx := range r.Outputs {
		output := &r.Outputs[idx]
		entry := customAnchorTotals(totals, output.AssetRef)

		amount, err := checkedCustomAnchorAdd(entry.Outputs, output.Amount)
		if err != nil {
			return fmt.Errorf("output amount for asset %q: %w",
				output.AssetRef, err)
		}
		entry.Outputs = amount

		if output.Script.Mode == AssetScriptBurn {
			amount, err = checkedCustomAnchorAdd(entry.Burned, output.Amount)
			if err != nil {
				return fmt.Errorf("burn amount for asset %q: %w",
					output.AssetRef, err)
			}
			entry.Burned = amount
		}
	}

	seenOutputs := make(map[string]struct{}, len(r.Outputs))
	for idx := range r.Outputs {
		output := &r.Outputs[idx]
		key := customAnchorAssetKey(output.AssetRef)
		if _, ok := seenOutputs[key]; ok {
			continue
		}
		seenOutputs[key] = struct{}{}

		entry := totals[key]
		if entry.Outputs != entry.Inputs {
			if entry.Outputs < entry.Inputs {
				return fmt.Errorf("asset %q has %d unallocated units; "+
					"implicit asset loss is unsupported, use an explicit "+
					"burn output", entry.Ref, entry.Inputs-entry.Outputs)
			}

			return fmt.Errorf("asset %q output amount %d exceeds input "+
				"amount %d", entry.Ref, entry.Outputs, entry.Inputs)
		}
	}

	seenInputs := make(map[string]struct{}, len(r.Inputs))
	for idx := range r.Inputs {
		input := &r.Inputs[idx]
		key := customAnchorAssetKey(input.AssetRef)
		if _, ok := seenInputs[key]; ok {
			continue
		}
		seenInputs[key] = struct{}{}

		entry := totals[key]

		loss := entry.Burned
		if loss == 0 {
			continue
		}

		allowance := r.LossPolicy.allowance(entry.Ref)
		if loss > allowance {
			return fmt.Errorf("asset %q irreversible loss %d exceeds "+
				"allowance %d", entry.Ref, loss, allowance)
		}
	}

	return nil
}

type customAnchorAmountTotals struct {
	Ref     tapsdk.AssetRef
	Inputs  uint64
	Outputs uint64
	Burned  uint64
}

func customAnchorTotals(totals map[string]*customAnchorAmountTotals,
	ref tapsdk.AssetRef) *customAnchorAmountTotals {

	key := customAnchorAssetKey(ref)
	entry, ok := totals[key]
	if ok {
		return entry
	}

	entry = &customAnchorAmountTotals{
		Ref: ref,
	}
	totals[key] = entry

	return entry
}

func (p *LossPolicy) allowance(ref tapsdk.AssetRef) uint64 {
	if p.Mode != LossBurn || !p.ConfirmIrreversibleLoss {
		return 0
	}

	key := customAnchorAssetKey(ref)
	for _, allowance := range p.Allowances {
		if customAnchorAssetKey(allowance.AssetRef) == key {
			return allowance.MaxAmount
		}
	}

	return 0
}

func cloneCustomAssetOutput(output AssetOutput) AssetOutput {
	clone := output
	clone.Script = cloneCustomAssetScript(output.Script)
	if output.Anchor.Tapscript.TapLeaves != nil {
		clone.Anchor.Tapscript.TapLeaves = make(
			[]tapsdk.TapLeaf, len(output.Anchor.Tapscript.TapLeaves),
		)
		for idx, leaf := range output.Anchor.Tapscript.TapLeaves {
			clone.Anchor.Tapscript.TapLeaves[idx] = leaf
			clone.Anchor.Tapscript.TapLeaves[idx].Script = bytes.Clone(
				leaf.Script,
			)
		}
	}
	clone.Anchor.Tapscript.SerializedSibling = bytes.Clone(
		output.Anchor.Tapscript.SerializedSibling,
	)
	clone.ProofDelivery.OpaqueMetadata = bytes.Clone(
		output.ProofDelivery.OpaqueMetadata,
	)

	return clone
}

func cloneCustomAssetScript(
	plan AssetScriptPlan) AssetScriptPlan {

	clone := plan
	if plan.Wallet != nil {
		wallet := *plan.Wallet
		wallet.KeyLocator = cloneKeyLocator(plan.Wallet.KeyLocator)
		clone.Wallet = &wallet
	}
	if plan.External != nil {
		external := *plan.External
		external.ScriptKey.TapTweak = bytes.Clone(
			plan.External.ScriptKey.TapTweak,
		)
		clone.External = &external
	}
	if plan.OPTrue != nil {
		opTrue := *plan.OPTrue
		clone.OPTrue = &opTrue
	}
	if plan.Burn != nil {
		burn := *plan.Burn
		clone.Burn = &burn
	}

	return clone
}

func cloneCustomAnchorFunding(
	plan FundingPlan) FundingPlan {

	clone := plan
	if plan.WalletFunded != nil {
		wallet := *plan.WalletFunded
		wallet.CustomLockID = bytes.Clone(plan.WalletFunded.CustomLockID)
		clone.WalletFunded = &wallet
	}
	if plan.CallerFundedExact != nil {
		caller := *plan.CallerFundedExact
		clone.CallerFundedExact = &caller
	}
	if plan.ExternalP2AFeeBump != nil {
		external := *plan.ExternalP2AFeeBump
		clone.ExternalP2AFeeBump = &external
	}

	return clone
}

func cloneCustomAnchorByteSlices(values [][]byte) [][]byte {
	if values == nil {
		return nil
	}

	clone := make([][]byte, len(values))
	for idx := range values {
		clone[idx] = bytes.Clone(values[idx])
	}

	return clone
}

func cloneKeyLocator(locator *tapsdk.KeyLocator) *tapsdk.KeyLocator {
	if locator == nil {
		return nil
	}

	clone := *locator
	return &clone
}

func cloneUint32(value *uint32) *uint32 {
	if value == nil {
		return nil
	}

	clone := *value
	return &clone
}

func sameCustomAnchorOutput(left, right AssetOutput) bool {
	if left.AnchorValueSat != right.AnchorValueSat ||
		left.Anchor.InternalKey != right.Anchor.InternalKey {

		return false
	}

	return sameCustomAnchorTapscript(
		left.Anchor.Tapscript, right.Anchor.Tapscript,
	)
}

func sameCustomAnchorTapscript(left,
	right TapscriptPlan) bool {

	if !bytes.Equal(left.SerializedSibling, right.SerializedSibling) ||
		len(left.TapLeaves) != len(right.TapLeaves) {

		return false
	}

	for idx := range left.TapLeaves {
		if !bytes.Equal(
			left.TapLeaves[idx].Script,
			right.TapLeaves[idx].Script,
		) {

			return false
		}
	}

	return true
}

func customAnchorAssetKey(ref tapsdk.AssetRef) string {
	if id, ok := ref.AssetID(); ok {
		return "asset:" + string(id[:])
	}
	if groupKey, ok := ref.GroupKey(); ok {
		xOnly := groupKey.XOnly()
		return "group:" + string(xOnly[:])
	}

	return "invalid:" + ref.String()
}

func checkedCustomAnchorAdd(left, right uint64) (uint64, error) {
	sum, carry := bits.Add64(left, right, 0)
	if carry != 0 {
		return 0, fmt.Errorf("amount overflows uint64")
	}

	return sum, nil
}

func countCustomAnchorVariants(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}

	return count
}

func validateCustomAnchorPubKey(name string, key tapsdk.PubKey) error {
	if _, err := tapsdk.ParsePubKey(key[:]); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}

func (o VerificationOrigin) valid() bool {
	return o == VerificationOriginLocal ||
		o == VerificationOriginBackend ||
		o == VerificationOriginHost
}

func (s VerificationSeverity) valid() bool {
	switch s {
	case VerificationSeverityInfo,
		VerificationSeverityWarning,
		VerificationSeverityError:

		return true

	default:
		return false
	}
}

func (s VerificationScope) valid() bool {
	switch s {
	case VerificationScopeRequest,
		VerificationScopeInputProof,
		VerificationScopeAssetIdentity,
		VerificationScopeAmount,
		VerificationScopeOutputCommitment,
		VerificationScopeAnchorOutput,
		VerificationScopePassiveAssets,
		VerificationScopeLossPolicy,
		VerificationScopeCapability,
		VerificationScopeBackendTrust:

		return true

	default:
		return false
	}
}
