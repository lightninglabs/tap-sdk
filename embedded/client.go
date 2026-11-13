package embedded

import (
	"context"
	"fmt"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	"github.com/lightninglabs/taproot-assets/taprpc"
	wrpc "github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	unirpc "github.com/lightninglabs/taproot-assets/taprpc/universerpc"
)

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

// NewClient creates an embedded client. Call Start before using calls that
// publish transfers, and Close to stop it.
func NewClient(cfg *Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("embedded: %w", err)
	}

	e, err := newEngine(cfg)
	if err != nil {
		return nil, fmt.Errorf("embedded: %w", err)
	}

	conn := newLocalConn()
	conn.register(
		&taprpc.TaprootAssets_ServiceDesc, &walletServer{engine: e},
	)
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

// Start resumes following the transfers recorded before the last shutdown:
// pending anchor transactions are broadcast again and confirmed transfers
// get their final proofs. It also trusts the group of every asset the wallet
// holds, since those proofs verified when they were received.
func (c *Client) Start(ctx context.Context) error {
	if err := c.engine.trustOwnedGroups(ctx); err != nil {
		return err
	}

	return c.engine.resumeTransfers(ctx)
}

// Close stops following transfers. Recorded transfers resume at the next
// Start.
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
