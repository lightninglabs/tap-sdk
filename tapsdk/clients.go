package tapsdk

import (
	"context"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
)

// ServiceClient is an interface that all service clients need to implement.
type ServiceClient[T any] interface {
	// RawClientWithMacAuth returns a context with the proper macaroon
	// authentication, the default RPC timeout, and the raw client.
	RawClientWithMacAuth(parentCtx context.Context) (context.Context,
		time.Duration, T)
}

// WalletClient exposes the TaprootAssets service gRPC client.
type WalletClient interface {
	ServiceClient[taprpc.TaprootAssetsClient]

	GetInfo(ctx context.Context) (*entities.Info, error)
}

// WalletKitClient exposes the AssetWalletClient service gRPC client.
type WalletKitClient interface {
	ServiceClient[assetwalletrpc.AssetWalletClient]

	// FundTransfer funds a virtual transaction.
	FundTransfer(ctx context.Context, recipients []entities.Recipient,
		inputs []entities.AssetInput) (*entities.FundedTransfer, error)

	// SignVirtualPsbt signs a virtual transaction.
	SignVirtualPsbt(ctx context.Context, fundedPsbt []byte) ([]byte, error)

	// CommitVirtualPsbts commits virtual transactions.
	CommitVirtualPsbts(ctx context.Context, virtualPsbts [][]byte,
		passivePsbts [][]byte, feeRate uint64) (*entities.CommittedTransfer, error)

	// PublishAndLogTransfer publishes the anchor transaction and logs the
	// transfer.
	PublishAndLogTransfer(ctx context.Context, anchorPsbt []byte,
		virtualPsbts [][]byte, passivePsbts [][]byte,
		skipAnchorTxBroadcast bool) (*entities.AssetPacket, error)
}
