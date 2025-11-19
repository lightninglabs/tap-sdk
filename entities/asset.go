package entities

import "github.com/btcsuite/btcd/wire"

// AssetInput represents a previous asset input to be spent.
type AssetInput struct {
	// OutPoint is the bitcoin anchor output on chain.
	OutPoint wire.OutPoint

	// ID is the asset ID of the previous asset tree.
	ID []byte

	// ScriptKey is the tweaked Taproot output key.
	ScriptKey []byte
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
