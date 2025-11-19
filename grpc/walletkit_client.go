package client

import (
	"context"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"google.golang.org/grpc"
)

// walletKitClient is a wrapper around the assetwalletrpc.AssetWalletClient.
type walletKitClient struct {
	client       assetwalletrpc.AssetWalletClient
	timeout      time.Duration
	walletKitMac macaroon.SerializedMacaroon
}

// NewWalletKitClient creates a new WalletKit client.
func NewWalletKitClient(conn grpc.ClientConnInterface, timeout time.Duration,
	walletKitMac macaroon.SerializedMacaroon) *walletKitClient {

	return &walletKitClient{
		client:       assetwalletrpc.NewAssetWalletClient(conn),
		timeout:      timeout,
		walletKitMac: walletKitMac,
	}
}

func (m *walletKitClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	assetwalletrpc.AssetWalletClient) {

	return m.walletKitMac.WithMacaroonAuth(parentCtx), m.timeout, m.client
}

// FundTransfer funds a virtual transaction.
func (m *walletKitClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient, inputs []entities.AssetInput) (
	*entities.FundedTransfer, error) {

	// Map inputs to RPC inputs
	rpcInputs := make([]*assetwalletrpc.PrevId, len(inputs))
	for i, input := range inputs {
		rpcInputs[i] = &assetwalletrpc.PrevId{
			Outpoint: &taprpc.OutPoint{
				Txid:        input.OutPoint.Hash[:],
				OutputIndex: input.OutPoint.Index,
			},
			Id:        input.ID,
			ScriptKey: input.ScriptKey,
		}
	}

	// Map recipients to RPC recipients
	rpcRecipients := make([]*taprpc.AddressWithAmount, len(recipients))
	for i, recipient := range recipients {
		rpcRecipients[i] = &taprpc.AddressWithAmount{
			TapAddr: recipient.Address,
			Amount:  recipient.Amount,
		}
	}

	// Create the raw template.
	rawTemplate := &assetwalletrpc.TxTemplate{
		Recipients:           nil, // Deprecated
		Inputs:               rpcInputs,
		AddressesWithAmounts: rpcRecipients,
	}

	req := &assetwalletrpc.FundVirtualPsbtRequest{
		Template: &assetwalletrpc.FundVirtualPsbtRequest_Raw{
			Raw: rawTemplate,
		},
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.FundVirtualPsbt(authCtx, req)
	if err != nil {
		return nil, err
	}

	return &entities.FundedTransfer{
		FundedPsbt:        resp.FundedPsbt,
		PassiveAssetPsbts: resp.PassiveAssetPsbts,
	}, nil
}

// SignVirtualPsbt signs a virtual transaction.
func (m *walletKitClient) SignVirtualPsbt(ctx context.Context,
	fundedPsbt []byte) ([]byte, error) {

	req := &assetwalletrpc.SignVirtualPsbtRequest{
		FundedPsbt: fundedPsbt,
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.SignVirtualPsbt(authCtx, req)
	if err != nil {
		return nil, err
	}

	return resp.SignedPsbt, nil
}

// CommitVirtualPsbts commits virtual transactions.
func (m *walletKitClient) CommitVirtualPsbts(ctx context.Context,
	virtualPsbts [][]byte, passivePsbts [][]byte, satPerVByte uint64) (
	*entities.CommittedTransfer, error) {

	req := &assetwalletrpc.CommitVirtualPsbtsRequest{
		VirtualPsbts:      virtualPsbts,
		PassiveAssetPsbts: passivePsbts,
		Fees: &assetwalletrpc.CommitVirtualPsbtsRequest_SatPerVbyte{
			SatPerVbyte: satPerVByte,
		},
		AnchorChangeOutput: &assetwalletrpc.CommitVirtualPsbtsRequest_Add{
			Add: true,
		},
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.CommitVirtualPsbts(authCtx, req)
	if err != nil {
		return nil, err
	}

	return &entities.CommittedTransfer{
		AnchorPsbt:        resp.AnchorPsbt,
		VirtualPsbts:      resp.VirtualPsbts,
		PassiveAssetPsbts: resp.PassiveAssetPsbts,
	}, nil
}

// PublishAndLogTransfer publishes the anchor transaction and logs the transfer.
func (m *walletKitClient) PublishAndLogTransfer(ctx context.Context,
	anchorPsbt []byte, virtualPsbts [][]byte, passivePsbts [][]byte,
	skipAnchorTxBroadcast bool) (*entities.AssetPacket, error) {

	req := &assetwalletrpc.PublishAndLogRequest{
		AnchorPsbt:            anchorPsbt,
		VirtualPsbts:          virtualPsbts,
		PassiveAssetPsbts:     passivePsbts,
		SkipAnchorTxBroadcast: skipAnchorTxBroadcast,
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.PublishAndLogTransfer(authCtx, req)
	if err != nil {
		return nil, err
	}

	return &entities.AssetPacket{
		AnchorTransaction:        resp.Transfer.AnchorTx,
		VirtualTransactions:      virtualPsbts,
		PassiveAssetTransactions: passivePsbts,
	}, nil
}
