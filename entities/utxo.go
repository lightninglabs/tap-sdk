package entities

// ManagedUtxo represents a UTXO managed by the tapd daemon.
type ManagedUtxo struct {
	// OutPoint is the outpoint of the UTXO.
	OutPoint Outpoint

	// AmtSat is the UTXO amount in satoshis.
	AmtSat int64

	// InternalKey is the internal key used for the on-chain output.
	InternalKey PubKey

	// TaprootAssetRoot is the Taproot Asset root commitment hash.
	TaprootAssetRoot Hash

	// MerkleRoot is the Taproot merkle root hash.
	MerkleRoot Hash

	// Assets are the assets held at this UTXO.
	Assets []*Asset

	// LeaseOwner is the lease owner for this UTXO. Empty if unleased.
	LeaseOwner []byte

	// LeaseExpiryUnix is the unix timestamp when the lease expires.
	// Zero if unleased.
	LeaseExpiryUnix int64
}

// ListUtxosRequest specifies filters for listing managed UTXOs.
type ListUtxosRequest struct {
	// IncludeLeased includes UTXOs that are marked as leased.
	IncludeLeased bool

	// ScriptKeyType filters the assets by script key type.
	ScriptKeyType *ScriptKeyTypeQuery
}

// AssetHumanReadable is a simplified asset representation used in
// group listings.
type AssetHumanReadable struct {
	// ID is the 32-byte asset identifier.
	ID AssetID

	// Amount is the asset amount.
	Amount uint64

	// LockTime is the optional absolute locktime.
	LockTime int32

	// RelativeLockTime is the optional relative locktime.
	RelativeLockTime int32

	// Tag is the name of the asset.
	Tag string

	// MetaHash is the metadata hash.
	MetaHash Hash

	// Type is the asset type.
	Type AssetType

	// Version is the asset version.
	Version uint8
}

// GroupedAssets is a list of assets sharing the same group key.
type GroupedAssets struct {
	// Assets are the assets in this group.
	Assets []*AssetHumanReadable
}
