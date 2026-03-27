package entities

// The mint entities in this file intentionally model the low-level Mint RPC
// building blocks.
//
// The higher-level mint UX of the SDK will be designed separately around the
// semantic distinction between fungible assets and collectibles instead of
// simply mirroring the raw RPC request shapes.

// AssetMetaType describes how asset metadata should be interpreted.
type AssetMetaType uint8

const (
	// AssetMetaTypeOpaque is unstructured opaque metadata.
	AssetMetaTypeOpaque AssetMetaType = 0

	// AssetMetaTypeJSON is metadata encoded as JSON.
	AssetMetaTypeJSON AssetMetaType = 1
)

// AssetMeta contains the metadata committed to an asset's genesis.
type AssetMeta struct {
	// Data is the raw metadata payload.
	Data []byte

	// Type describes how Data should be interpreted.
	Type AssetMetaType

	// MetaHash is the TLV hash of the metadata.
	MetaHash Hash
}

// ExternalKey describes an externally managed BIP-86 key derivation.
type ExternalKey struct {
	// XPub is the account-level extended public key.
	XPub string

	// MasterFingerprint identifies the master key that derived XPub.
	MasterFingerprint [4]byte

	// DerivationPath is the full child derivation path.
	DerivationPath string
}

// PendingMintAsset represents an asset staged in a minting batch.
type PendingMintAsset struct {
	// AssetVersion is the asset encoding version.
	AssetVersion AssetVersion

	// AssetType is the type of asset to mint.
	AssetType AssetType

	// Name is the asset tag.
	Name string

	// AssetMeta is the optional metadata committed to the genesis.
	AssetMeta *AssetMeta

	// Amount is the number of units to mint.
	Amount uint64

	// NewGroupedAsset creates a new asset group for future issuance.
	NewGroupedAsset bool

	// GroupKey joins an existing group by explicit key.
	GroupKey *PubKey

	// GroupAnchor joins the group created by another asset in the batch.
	GroupAnchor string

	// GroupInternalKey is the internal key for a new group.
	GroupInternalKey *KeyDescriptor

	// GroupTapscriptRoot is the optional tapscript root for a new group.
	GroupTapscriptRoot []byte

	// ScriptKey is the optional custom script key for the asset.
	ScriptKey *ScriptKey
}

// MintAsset describes a new asset to stage in a minting batch.
type MintAsset struct {
	PendingMintAsset

	// GroupedAsset allows minting into an existing group.
	GroupedAsset bool

	// DecimalDisplay is the wallet display precision for the asset.
	DecimalDisplay uint32

	// ExternalGroupKey enables external signing for group issuance.
	ExternalGroupKey *ExternalKey

	// EnableSupplyCommitments enables supply commitment support.
	EnableSupplyCommitments bool
}

// BatchState is the lifecycle state of a minting batch.
type BatchState uint8

const (
	// BatchStateUnknown is an unknown batch state.
	BatchStateUnknown BatchState = 0

	// BatchStatePending means the batch is still collecting assets.
	BatchStatePending BatchState = 1

	// BatchStateFrozen means the batch is sealed and no longer mutable.
	BatchStateFrozen BatchState = 2

	// BatchStateCommitted means the genesis transaction was built.
	BatchStateCommitted BatchState = 3

	// BatchStateBroadcast means the genesis transaction was broadcast.
	BatchStateBroadcast BatchState = 4

	// BatchStateConfirmed means the genesis transaction confirmed.
	BatchStateConfirmed BatchState = 5

	// BatchStateFinalized means the batch completed fully.
	BatchStateFinalized BatchState = 6

	// BatchStateSeedlingCancelled means an unfunded batch was cancelled.
	BatchStateSeedlingCancelled BatchState = 7

	// BatchStateSproutCancelled means a funded batch was cancelled.
	BatchStateSproutCancelled BatchState = 8
)

// TapLeaf represents a single leaf in a tapscript tree.
type TapLeaf struct {
	// Script is the tapscript leaf program.
	Script []byte
}

// TapscriptFullTree describes a full ordered tapscript tree.
type TapscriptFullTree struct {
	// Leaves is the ordered list of tapscript leaves.
	Leaves []TapLeaf
}

// TapBranch describes a tapscript tree by its root children.
type TapBranch struct {
	// LeftTapHash is the tap hash of the left child.
	LeftTapHash Hash

	// RightTapHash is the tap hash of the right child.
	RightTapHash Hash
}

// BatchSibling selects the tapscript sibling used for a mint batch.
type BatchSibling struct {
	// FullTree provides the full tapscript tree.
	FullTree *TapscriptFullTree

	// Branch provides only the root branch hashes.
	Branch *TapBranch
}

// MintingBatch is the daemon's view of a minting batch.
type MintingBatch struct {
	// BatchKey uniquely identifies the batch.
	BatchKey PubKey

	// BatchTxid is the finalized genesis transaction ID, if any.
	BatchTxid string

	// State is the current lifecycle state.
	State BatchState

	// Assets are the assets currently staged in the batch.
	Assets []PendingMintAsset

	// CreatedAt is the batch creation time as a Unix timestamp.
	CreatedAt int64

	// HeightHint is the chain height at batch creation time.
	HeightHint uint32

	// BatchPSBT is the genesis transaction PSBT, if available.
	BatchPSBT []byte
}

// MintAssetRequest adds an asset to the pending mint batch.
type MintAssetRequest struct {
	// Asset is the asset to add to the batch.
	Asset *MintAsset

	// ShortResponse asks the daemon to omit existing batch assets.
	ShortResponse bool
}

// FinalizeBatchRequest finalizes the current pending mint batch.
type FinalizeBatchRequest struct {
	// ShortResponse asks the daemon to omit batch asset details.
	ShortResponse bool

	// FeeRate is the optional fee rate in sat/kw.
	FeeRate uint32

	// BatchSibling is the optional tapscript sibling for the batch output.
	BatchSibling *BatchSibling
}
