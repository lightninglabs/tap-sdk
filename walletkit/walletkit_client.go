package walletkit

import (
	"context"
	"time"

	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"google.golang.org/grpc"
)

// Client exposes asset wallet functionality.
type Client interface {
	// RawClientWithMacAuth returns a context with the proper macaroon
	// authentication, the default RPC timeout, and the raw client.
	RawClientWithMacAuth(parentCtx context.Context) (context.Context,
		time.Duration, assetwalletrpc.AssetWalletClient)
}

// client is a wrapper around the assetwalletrpc.AssetWalletClient.
type client struct {
	client       assetwalletrpc.AssetWalletClient
	timeout      time.Duration
	walletKitMac macaroon.SerializedMacaroon
}

var _ Client = (*client)(nil)

// NewClient creates a new WalletKit client.
func NewClient(conn grpc.ClientConnInterface,
	timeout time.Duration, walletKitMac macaroon.SerializedMacaroon) *client {

	return &client{
		client:       assetwalletrpc.NewAssetWalletClient(conn),
		timeout:      timeout,
		walletKitMac: walletKitMac,
	}
}

func (m *client) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	assetwalletrpc.AssetWalletClient) {

	return m.walletKitMac.WithMacaroonAuth(parentCtx), m.timeout, m.client
}
