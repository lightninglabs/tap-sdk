package entities

// ListBalancesRequest specifies filters for querying wallet balances using the
// SDK's semantic asset model.
type ListBalancesRequest struct {
	// AssetRef restricts the result to a single asset.
	AssetRef *AssetRef

	// IncludeLeased includes assets that are currently leased by a pending
	// transfer and therefore unavailable for coin selection.
	IncludeLeased bool

	// ScriptKeyType optionally restricts balances to a specific script key
	// type or all types.
	ScriptKeyType *ScriptKeyTypeQuery
}

// AssetBalance represents the confirmed balance of a semantic asset.
type AssetBalance struct {
	// AssetRef is the SDK's user-facing identifier for the asset.
	AssetRef AssetRef

	// AssetGenesis is the immutable genesis information of a representative
	// issuance for the asset.
	AssetGenesis AssetGenesis

	// Balance is the confirmed amount owned by the wallet.
	Balance uint64

	// GroupKey is the optional protocol group key of the representative
	// issuance.
	GroupKey *PubKey
}

// AssetGroupBalance represents the old raw group-key balance shape exposed by
// tapd. It is kept for protocol-level helpers and backward-compatible tests,
// but the preferred SDK balance surface is ListBalancesResponse.Balances keyed
// by AssetRef.
type AssetGroupBalance struct {
	GroupKey *PubKey
	Balance  uint64
}

// ListBalancesResponse contains wallet balances keyed by AssetRef.
type ListBalancesResponse struct {
	// Balances is keyed by AssetRef.String().
	Balances map[string]*AssetBalance

	// UnconfirmedTransfers counts outgoing transfers that are not yet
	// confirmed on-chain and therefore excluded from the reported balances.
	UnconfirmedTransfers uint64
}
