package entities

// PrevID represents a previous asset input to be spent. It is the Taproot
// Assets protocol-level identifier for an input asset.
type PrevID struct {
	// Outpoint is the bitcoin anchor output on chain.
	Outpoint Outpoint

	// AssetID is the 32-byte asset identifier of the previous asset tree.
	AssetID [32]byte

	// ScriptKey is the tweaked Taproot output key.
	ScriptKey [33]byte
}

// AssetPacket represents a fully constructed and signed asset transfer packet
// ready for broadcasting.
type AssetPacket struct {
	// AnchorTransaction is the raw bytes of the final Bitcoin anchor transaction.
	AnchorTransaction []byte

	// VirtualTransactions are the signed virtual asset transactions.
	VirtualTransactions [][]byte

	// PassiveAssetTransactions are the signed passive asset transactions.
	PassiveAssetTransactions [][]byte
}

// AssetType represents the type of asset.
type AssetType uint8

const (
	// AssetTypeNormal is a normal asset.
	AssetTypeNormal AssetType = 0

	// AssetTypeCollectible is a collectible asset.
	AssetTypeCollectible AssetType = 1
)

// AssetGenesis represents the genesis of an asset.
type AssetGenesis struct {
	// FirstPrevOut is the outpoint of the first input of the genesis transaction.
	FirstPrevOut Outpoint

	// Tag is the asset tag.
	Tag string

	// MetaHash is the meta data hash.
	MetaHash [32]byte

	// AssetID is the unique 32-byte asset identifier.
	AssetID [32]byte

	// OutputIndex is the output index of the genesis transaction.
	OutputIndex uint32

	// Type is the asset type.
	Type AssetType
}

// GroupKey represents a group key.
type GroupKey struct {
	// RawKey is the raw group key bytes.
	RawKey [33]byte

	// TapscriptRoot is the tapscript root of the group key.
	TapscriptRoot []byte
}

// AltLeaf represents an auxiliary leaf in the Taproot tree.
type AltLeaf struct {
	// Value is the raw bytes of the leaf.
	Value []byte
}

// Asset represents a Taproot Asset.
type Asset struct {
	// Version is the asset version.
	Version uint8

	// Genesis is the asset genesis definition.
	Genesis AssetGenesis

	// Amount is the asset amount.
	Amount uint64

	// LockTime is the CLTV lock time.
	LockTime uint64

	// RelativeLockTime is the CSV lock time.
	RelativeLockTime uint64

	// ScriptVersion is the script version.
	ScriptVersion uint16

	// ScriptKey is the script key.
	ScriptKey ScriptKey

	// GroupKey is the optional group key.
	GroupKey *GroupKey

	// AltLeaves are the auxiliary leaves.
	AltLeaves []AltLeaf
}
