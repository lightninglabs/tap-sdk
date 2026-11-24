package embedded

import (
	"context"
	"errors"
	"fmt"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	"github.com/lightninglabs/taproot-assets/taprpc"
	wrpc "github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	unirpc "github.com/lightninglabs/taproot-assets/taprpc/universerpc"
)

// errNoReceive is returned by the receive calls of a client whose host set no
// ReceiveConfig.
var errNoReceive = errors.New("embedded: receive is not configured")

// Client is a tapsdk.Client answered by the embedded engine.
type Client struct {
	*tapgrpc.Client

	engine *engine
}

// A compile-time check that Client is a full tap-sdk client that reports
// its custom-anchor capabilities.
var (
	_ tapsdk.Client                      = (*Client)(nil)
	_ tapsdk.CustomAnchorWalletKitClient = (*Client)(nil)
	_ customanchor.CapabilityProvider    = (*Client)(nil)
)

// NewClient creates an embedded client, which answers calls at once. Call
// ResumeTransfers to follow the transfers an earlier run left unconfirmed,
// ResumeReceives to receive at the wallet's addresses, and Close to stop
// both.
func NewClient(cfg *Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("embedded: %w", err)
	}

	e, err := newEngine(cfg)
	if err != nil {
		return nil, fmt.Errorf("embedded: %w", err)
	}

	wallet := &walletServer{engine: e}
	var walletService taprpc.TaprootAssetsServer = wallet
	if cfg.Receive != nil {
		walletService = newAddressServer(wallet, cfg.Receive)
	}

	conn := newLocalConn()
	conn.register(&taprpc.TaprootAssets_ServiceDesc, walletService)
	conn.register(
		&wrpc.AssetWallet_ServiceDesc, &walletKitServer{engine: e},
	)
	conn.register(
		&unirpc.Universe_ServiceDesc, &universeServer{engine: e},
	)

	rpcTimeout := cfg.RPCTimeout
	if rpcTimeout == 0 {
		rpcTimeout = defaultRPCTimeout
	}

	return &Client{
		Client: tapgrpc.NewClientFromConn(conn, rpcTimeout),
		engine: e,
	}, nil
}

// ResumeTransfers follows the transfers an earlier run left unconfirmed or
// not yet final. It broadcasts a pending anchor transaction again and writes
// the final proofs of a transfer once it confirms. A transfer published
// through this client is followed without this call.
func (c *Client) ResumeTransfers(ctx context.Context) error {
	return c.engine.resumeTransfers(ctx)
}

// ResumeReceives holds the mailbox of every address open, so a transfer to an
// address is received when its sender announces it, and works on the receives
// an earlier run left unfinished. A courier delivers its stored messages
// first, which covers the transfers sent while no client ran. The mailbox of
// an address created later is held open without this call.
func (c *Client) ResumeReceives(ctx context.Context) error {
	if c.engine.receive == nil {
		return errNoReceive
	}

	return c.engine.receive.resume(ctx)
}

// SyncReceives reads the stored messages of every mailbox and works once on
// every unfinished receive, then disconnects unless ResumeReceives holds the
// mailboxes open. It suits a host that cannot keep a connection, such as an
// app that returns to the foreground. A transfer whose block or proofs are
// not available yet is left to a later call without an error.
func (c *Client) SyncReceives(ctx context.Context) error {
	if c.engine.receive == nil {
		return errNoReceive
	}

	return c.engine.receive.sync(ctx)
}

// Close stops following transfers and receives. ResumeTransfers and
// ResumeReceives of a later client pick the recorded ones up.
func (c *Client) Close() error {
	c.engine.stopTracking()

	return nil
}

// TrustGroupKey adds an asset group to the groups whose proofs verify.
func (c *Client) TrustGroupKey(groupKey tapsdk.PubKey) {
	c.engine.trustGroupKey(groupKey)
}

// CustomAnchorCapabilities reports what the engine supports of the
// custom-anchor flow. The engine holds no Bitcoin, so it commits to
// caller-funded anchor transactions only.
func (c *Client) CustomAnchorCapabilities(
	ctx context.Context) (*customanchor.Capabilities, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &customanchor.Capabilities{
		BackendMode:              customanchor.BackendLightweight,
		CustomAnchorCommit:       customanchor.CapabilitySupported,
		CallerSuppliedAnchorPSBT: customanchor.CapabilitySupported,
		SkipFunding:              customanchor.CapabilitySupported,
		NoChangeOutput:           customanchor.CapabilitySupported,
		WalletFunding:            customanchor.CapabilityUnsupported,
		SkipBroadcast:            customanchor.CapabilitySupported,
		P2AAnchorOutput:          customanchor.CapabilityUnknown,
		ExternalFeeBump:          customanchor.CapabilityUnknown,
		ProofRegistration:        customanchor.CapabilitySupported,
		LocalVerification:        customanchor.CapabilitySupported,
		BackendVerification:      customanchor.CapabilityUnsupported,
		BackendAnchorSigning:     customanchor.CapabilitySupported,
	}, nil
}
