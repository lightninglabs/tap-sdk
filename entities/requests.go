package entities

// ScriptKeyType represents the role of an asset script key.
type ScriptKeyType uint8

const (
	// ScriptKeyTypeUnknown is used when the script key type is not known.
	ScriptKeyTypeUnknown ScriptKeyType = 0

	// ScriptKeyTypeBIP86 is the standard BIP-86 script key type.
	ScriptKeyTypeBIP86 ScriptKeyType = 1

	// ScriptKeyTypeScriptPathExternal represents a user-defined script path.
	ScriptKeyTypeScriptPathExternal ScriptKeyType = 2

	// ScriptKeyTypeBurn represents an unspendable burn key.
	ScriptKeyTypeBurn ScriptKeyType = 3

	// ScriptKeyTypeTombstone represents an unspendable tombstone output.
	ScriptKeyTypeTombstone ScriptKeyType = 4

	// ScriptKeyTypeChannel represents a Taproot Asset channel-related key.
	ScriptKeyTypeChannel ScriptKeyType = 5

	// ScriptKeyTypeUniquePedersen represents a unique Pedersen-based key.
	ScriptKeyTypeUniquePedersen ScriptKeyType = 6
)

// ScriptKeyTypeQuery specifies how to filter by script key type.
type ScriptKeyTypeQuery struct {
	// ExplicitType restricts results to a single script key type.
	ExplicitType *ScriptKeyType

	// AllTypes returns assets of all script key types.
	AllTypes bool
}

// ListAssetsRequest specifies filters for listing wallet assets.
type ListAssetsRequest struct {
	WithWitness             bool
	IncludeSpent            bool
	IncludeLeased           bool
	IncludeUnconfirmedMints bool
	MinAmount               uint64
	MaxAmount               uint64
	AssetRef                *AssetRef
	AnchorOutpoint          *Outpoint
	ScriptKeyType           *ScriptKeyTypeQuery
}

// ListTransfersRequest specifies filters for listing outgoing transfers.
type ListTransfersRequest struct {
	// AnchorTxid specifies the hexadecimal encoded txid string of the anchor
	// transaction for which to retrieve transfers. An empty value indicates
	// that this parameter should be disregarded in transfer selection.
	AnchorTxid string
}
