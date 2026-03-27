package entities

// BalanceGroupBy specifies how ListBalances groups the returned balances.
type BalanceGroupBy uint8

const (
	// BalanceGroupByDefault groups balances by asset ID. This matches the
	// SDK default when no explicit grouping is requested.
	BalanceGroupByDefault BalanceGroupBy = 0

	// BalanceGroupByAssetID groups balances by asset ID.
	BalanceGroupByAssetID BalanceGroupBy = 1

	// BalanceGroupByGroupKey groups balances by group key.
	BalanceGroupByGroupKey BalanceGroupBy = 2
)

// ListBalancesRequest specifies filters for querying wallet balances through
// the raw TaprootAssets balance RPC.
//
// This type mirrors the daemon's grouping modes for advanced callers. The
// higher-level SDK surface should still present fungible assets by group key
// and collectibles by asset ID.
type ListBalancesRequest struct {
	// GroupBy selects whether balances are grouped by asset ID or group key.
	// The zero value defaults to grouping by asset ID.
	GroupBy BalanceGroupBy

	// AssetFilter restricts asset-ID grouped queries to a specific asset.
	AssetFilter *AssetID

	// GroupKeyFilter restricts group-key grouped queries to a specific group.
	GroupKeyFilter *PubKey

	// IncludeLeased includes assets that are currently leased by a pending
	// transfer and therefore unavailable for coin selection.
	IncludeLeased bool

	// ScriptKeyType optionally restricts balances to a specific script key
	// type or all types.
	ScriptKeyType *ScriptKeyTypeQuery
}

// AssetBalance represents the confirmed balance of a specific asset.
type AssetBalance struct {
	// AssetGenesis is the immutable genesis information of the asset.
	AssetGenesis AssetGenesis

	// Balance is the confirmed amount owned by the wallet.
	Balance uint64

	// GroupKey is the optional group key of the asset.
	GroupKey *PubKey
}

// AssetGroupBalance represents the aggregate confirmed balance of an asset
// group.
type AssetGroupBalance struct {
	// GroupKey is the group key the balance belongs to. A nil value
	// represents assets that do not belong to a group.
	GroupKey *PubKey

	// Balance is the confirmed amount owned by the wallet for the group.
	Balance uint64
}

// ListBalancesResponse contains wallet balances grouped by either asset ID or
// group key.
type ListBalancesResponse struct {
	// AssetBalances is populated for asset-ID grouped queries. The map key is
	// the hex-encoded asset ID returned by tapd.
	AssetBalances map[string]*AssetBalance

	// AssetGroupBalances is populated for group-key grouped queries. The map
	// key is the hex-encoded group key returned by tapd.
	AssetGroupBalances map[string]*AssetGroupBalance

	// UnconfirmedTransfers counts outgoing transfers that are not yet
	// confirmed on-chain and therefore excluded from the reported balances.
	UnconfirmedTransfers uint64
}
