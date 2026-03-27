package entities

// BurnAssetRequest specifies parameters for burning asset units.
type BurnAssetRequest struct {
	// AssetID is the 32-byte asset identifier to burn. Either this or
	// AssetIDStr must be set.
	AssetID *AssetID

	// AssetIDStr is the hex-encoded asset ID to burn. Either this or
	// AssetID must be set.
	AssetIDStr string

	// AmountToBurn is the number of asset units to burn. Must be
	// greater than zero.
	AmountToBurn uint64

	// ConfirmationText must be set to "assets will be destroyed" for
	// the burn to succeed. This is a safety check.
	ConfirmationText string

	// Note is optional user-defined metadata for this burn.
	Note string
}

// BurnAssetResponse contains the result of a burn operation.
type BurnAssetResponse struct {
	// BurnTransfer is the asset transfer containing the burn output.
	BurnTransfer *AssetTransfer

	// BurnProof is the transition proof for the burn output.
	BurnProof *DecodedProof
}

// ListBurnsRequest specifies filters for listing asset burns.
type ListBurnsRequest struct {
	// AssetID filters by the asset id of the burnt asset.
	AssetID *AssetID

	// TweakedGroupKey filters by the tweaked group key.
	TweakedGroupKey *PubKey

	// AnchorTxid filters by the anchor transaction id.
	AnchorTxid *Hash
}

// AssetBurn represents a single asset burn event.
type AssetBurn struct {
	// Note is user-defined metadata for the burn.
	Note string

	// AssetID is the asset id of the burnt asset.
	AssetID AssetID

	// TweakedGroupKey is the tweaked group key.
	TweakedGroupKey *PubKey

	// Amount is the number of burnt units.
	Amount uint64

	// AnchorTxid is the txid of the anchor transaction.
	AnchorTxid Hash
}
