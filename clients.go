package tapsdk

import (
	"context"

	"github.com/lightninglabs/tap-sdk/entities"
)

type Client interface {
	WalletClient
	ProofClient
	WalletKitClient
	UniverseClient
}

// WalletClient exposes the TaprootAssets service gRPC client.
type WalletClient interface {
	GetInfo(ctx context.Context) (*entities.Info, error)

	// ListAssets lists wallet assets with optional filtering.
	ListAssets(ctx context.Context,
		req *entities.ListAssetsRequest) ([]*entities.Asset, error)

	// ListTransfers lists outgoing transfers with optional filtering.
	ListTransfers(ctx context.Context,
		req *entities.ListTransfersRequest) ([]*entities.AssetTransfer, error)
}

// ProofClient exposes proof-related operations from the TaprootAssets service.
type ProofClient interface {
	// ExportProof exports a proof file for a specific asset output.
	// If outpoint is nil, then the latest proof for the given asset/script key
	// is exported.
	ExportProof(ctx context.Context, assetID, scriptKey []byte,
		outpoint *entities.Outpoint) (*entities.ProofFile, error)

	// UnpackProofFile unpacks a proof file into individual proofs.
	UnpackProofFile(ctx context.Context, rawProofFile []byte) ([][]byte, error)

	// DecodeProof decodes a raw proof and returns details about it.
	DecodeProof(ctx context.Context, rawProof []byte) (*entities.DecodedProof, error)

	// RegisterTransfer registers an inbound transfer for an interactive send.
	// The proof must already be in the local universe before calling this.
	RegisterTransfer(ctx context.Context, assetID, groupKey, scriptKey []byte,
		outpoint entities.Outpoint) (*entities.RegisteredAsset, error)
}

// WalletKitClient exposes the AssetWalletClient service gRPC client.
type WalletKitClient interface {
	// DeriveScriptKey derives a new script key for receiving assets.
	// The script key includes both the internal key and the tweaked
	// Taproot output key.
	DeriveScriptKey(ctx context.Context) (*entities.ScriptKey, error)

	// DeriveInternalKey derives a new internal key for anchor outputs.
	DeriveInternalKey(ctx context.Context) (*entities.InternalKey, error)

	// FundTransfer funds a virtual transaction using addresses.
	FundTransfer(ctx context.Context, recipients []entities.Recipient,
		inputs []entities.PrevID) (*entities.FundedTransfer, error)

	// FundInteractivePsbt funds a virtual PSBT for interactive sends.
	FundInteractivePsbt(ctx context.Context, psbt []byte) (
		*entities.FundedTransfer, error)

	// SignVirtualPsbt signs a virtual transaction.
	SignVirtualPsbt(ctx context.Context, fundedPsbt []byte) ([]byte, error)

	// CommitVirtualPsbts commits virtual transactions.
	CommitVirtualPsbts(ctx context.Context, virtualPsbts [][]byte,
		passivePsbts [][]byte, feeRate uint64) (*entities.CommittedTransfer, error)

	// AnchorVirtualPsbts anchors signed virtual PSBTs in a single call.
	// This combines committing and publishing into one operation.
	AnchorVirtualPsbts(ctx context.Context, signedPsbts [][]byte) (
		*entities.SendResult, error)

	// PublishAndLogTransfer publishes the anchor transaction and logs the
	// transfer.
	PublishAndLogTransfer(ctx context.Context, anchorPsbt []byte,
		virtualPsbts [][]byte, passivePsbts [][]byte,
		skipAnchorTxBroadcast bool) (*entities.AssetPacket, error)
}

// UniverseClient exposes the Universe service gRPC client.
type UniverseClient interface {
	// InsertProof inserts a proof into the local universe.
	InsertProof(ctx context.Context, rawProof []byte,
		decoded *entities.DecodedProof) error
}
