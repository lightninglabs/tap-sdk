package client

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"google.golang.org/grpc"
)

type walletClient struct {
	client   taprpc.TaprootAssetsClient
	timeout  time.Duration
	adminMac macaroon.SerializedMacaroon
}

// NewWalletClient creates a new Wallet client.
func NewWalletClient(conn grpc.ClientConnInterface, timeout time.Duration,
	adminMac macaroon.SerializedMacaroon) *walletClient {

	return &walletClient{
		client:   taprpc.NewTaprootAssetsClient(conn),
		timeout:  timeout,
		adminMac: adminMac,
	}
}

// RawClientWithMacAuth returns a context with the proper macaroon
// authentication, the default RPC timeout, and the raw client.
func (s *walletClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	taprpc.TaprootAssetsClient) {

	return s.adminMac.WithMacaroonAuth(parentCtx), s.timeout, s.client
}

func (s *walletClient) GetInfo(ctx context.Context) (*entities.Info, error) {
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

	return &entities.Info{
		Version:           resp.Version,
		LndVersion:        resp.LndVersion,
		Network:           resp.Network,
		LndIdentityPubkey: pubKeyArray,
		BlockHeight:       resp.BlockHeight,
		NodeAlias:         resp.NodeAlias,
		SyncedToChain:     resp.SyncToChain,
	}, nil
}
