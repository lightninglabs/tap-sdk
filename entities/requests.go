package entities

// ScriptKeyTypeQuery specifies how to filter by script key type.
// If nil in a request, the daemon default is used.
type ScriptKeyTypeQuery struct {
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
	GroupKey                *PubKey
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
