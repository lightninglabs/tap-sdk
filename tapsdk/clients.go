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
}
