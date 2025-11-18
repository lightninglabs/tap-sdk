package wallet

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"google.golang.org/grpc"
)

// Client exposes high level wallet functionality.
type Client interface {
	// RawClientWithMacAuth returns a context with the proper macaroon
	// authentication, the default RPC timeout, and the raw client.
	RawClientWithMacAuth(parentCtx context.Context) (context.Context,
		time.Duration, taprpc.TaprootAssetsClient)

	GetInfo(ctx context.Context) (*Info, error)
}

type client struct {
	client   taprpc.TaprootAssetsClient
	timeout  time.Duration
	adminMac macaroon.SerializedMacaroon
}

var _ Client = (*client)(nil)

// NewClient creates a new Wallet client.
func NewClient(conn grpc.ClientConnInterface, timeout time.Duration,
	adminMac macaroon.SerializedMacaroon) *client {

	return &client{
		client:   taprpc.NewTaprootAssetsClient(conn),
		timeout:  timeout,
		adminMac: adminMac,
	}
}

// RawClientWithMacAuth returns a context with the proper macaroon
// authentication, the default RPC timeout, and the raw client.
func (s *client) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	taprpc.TaprootAssetsClient) {

	return s.adminMac.WithMacaroonAuth(parentCtx), s.timeout, s.client
}

// Info contains info about the connected tapd instance.
type Info struct {
	// Version is the version that tapd is running.
	Version string

	// LndVersion is the full version string of the LND node tapd is connected to.
	LndVersion string

	// Network is the network tapd is connected to,
	// e.g. "mainnet", "testnet", or any other supported network.
	Network string

	// LndIdentityPubkey is the public key of the LND node tapd is connected to.
	LndIdentityPubkey [33]byte

	// NodeAlias is the alias of the LND node tapd is connected to.
	NodeAlias string

	// BlockHeight is the best block height that tapd has knowledge of.
	BlockHeight uint32

	// BlockHash is the current block hash as seen by the LND node
	// tapd is connected to.
	BlockHash chainhash.Hash

	// SyncedToChain is true if the wallet's view is synced to the main
	// chain.
	SyncedToChain bool
}

func (s *client) GetInfo(ctx context.Context) (*Info, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)
	resp, err := s.client.GetInfo(rpcCtx, &taprpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	pubKey, err := hex.DecodeString(resp.LndIdentityPubkey)
	if err != nil {
		return nil, err
	}

	var pubKeyArray [33]byte
	copy(pubKeyArray[:], pubKey)

	return &Info{
		Version:           resp.Version,
		LndVersion:        resp.LndVersion,
		Network:           resp.Network,
		LndIdentityPubkey: pubKeyArray,
		BlockHeight:       resp.BlockHeight,
		NodeAlias:         resp.NodeAlias,
		SyncedToChain:     resp.SyncToChain,
	}, nil
}
