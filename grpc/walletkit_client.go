package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
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
	recipients []entities.Recipient, inputs []entities.PrevID) (
	*entities.FundedTransfer, error) {

	// Map inputs to RPC inputs
	rpcInputs := make([]*assetwalletrpc.PrevId, len(inputs))
	for i, input := range inputs {
		rpcInputs[i] = &assetwalletrpc.PrevId{
			Outpoint: &taprpc.OutPoint{
				Txid:        input.Outpoint.Txid[:],
				OutputIndex: input.Outpoint.Index,
			},
			Id:        input.AssetID[:],
			ScriptKey: input.ScriptKey[:],
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

// DeriveScriptKey derives a new script key for receiving assets.
// The script key includes both the internal key and the tweaked Taproot
// output key.
func (m *walletKitClient) DeriveScriptKey(ctx context.Context) (
	*entities.ScriptKey, error) {

	req := &assetwalletrpc.NextScriptKeyRequest{
		KeyFamily: entities.TaprootAssetsKeyFamily,
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.NextScriptKey(authCtx, req)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("invalid script key response")
	}

	scriptKey, err := unmarshalScriptKey(resp.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal script key: %w", err)
	}

	return scriptKey, nil
}

// DeriveInternalKey derives a new internal key for anchor outputs.
func (m *walletKitClient) DeriveInternalKey(ctx context.Context) (
	*entities.InternalKey, error) {

	req := &assetwalletrpc.NextInternalKeyRequest{
		KeyFamily: entities.TaprootAssetsKeyFamily,
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.NextInternalKey(authCtx, req)
	if err != nil {
		return nil, err
	}

	if resp.InternalKey == nil {
		return nil, fmt.Errorf("invalid internal key response")
	}

	internalKey, err := unmarshalKeyDescriptor(resp.InternalKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal internal key: %w", err)
	}

	return &entities.InternalKey{
		PubKey:     internalKey.RawKeyBytes,
		KeyLocator: internalKey.KeyLocator,
	}, nil
}

// FundInteractivePsbt funds a virtual PSBT for interactive sends.
// The psbt parameter should be a serialized vPacket created for the
// interactive transfer.
func (m *walletKitClient) FundInteractivePsbt(ctx context.Context,
	psbt []byte) (*entities.FundedTransfer, error) {

	req := &assetwalletrpc.FundVirtualPsbtRequest{
		Template: &assetwalletrpc.FundVirtualPsbtRequest_Psbt{
			Psbt: psbt,
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

// AnchorVirtualPsbts anchors signed virtual PSBTs in a single call.
// This combines signing, committing, and publishing into one operation.
// It returns the completed transfer result with transaction details and proofs.
func (m *walletKitClient) AnchorVirtualPsbts(ctx context.Context,
	signedPsbts [][]byte) (*entities.SendResult, error) {

	req := &assetwalletrpc.AnchorVirtualPsbtsRequest{
		VirtualPsbts: signedPsbts,
	}

	authCtx, _, client := m.RawClientWithMacAuth(ctx)
	resp, err := client.AnchorVirtualPsbts(authCtx, req)
	if err != nil {
		return nil, err
	}

	if resp.Transfer == nil {
		return nil, fmt.Errorf("invalid transfer response")
	}

	return unmarshalSendResult(resp.Transfer)
}

// unmarshalSendResult converts an RPC AssetTransfer to an entities.SendResult.
func unmarshalSendResult(transfer *taprpc.AssetTransfer) (
	*entities.SendResult, error) {

	result := &entities.SendResult{
		AnchorTx: transfer.AnchorTx,
		Outputs:  make([]entities.TransferOutput, 0, len(transfer.Outputs)),
	}

	// Copy the transaction hash.
	if len(transfer.AnchorTxHash) == 32 {
		copy(result.TransferTxid[:], transfer.AnchorTxHash)

		var h chainhash.Hash
		copy(h[:], transfer.AnchorTxHash)
		result.AnchorTxid = h.String()
	}

	// Convert each output.
	for _, out := range transfer.Outputs {
		output := entities.TransferOutput{
			Amount:    out.Amount,
			ProofBlob: out.NewProofBlob,
		}

		// Copy the script key.
		if len(out.ScriptKey) == 33 {
			copy(output.ScriptKey[:], out.ScriptKey)
		}

		// Copy the outpoint from anchor.
		if out.Anchor != nil {
			output.Outpoint = out.Anchor.Outpoint

			op, err := wire.NewOutPointFromString(out.Anchor.Outpoint)
			if err != nil {
				return nil, fmt.Errorf("invalid anchor outpoint: %w", err)
			}
			output.AnchorOutpoint = entities.Outpoint{
				Txid:  op.Hash,
				Index: op.Index,
			}

			output.AnchorValue = out.Anchor.Value
		}

		result.Outputs = append(result.Outputs, output)
	}

	return result, nil
}

// unmarshalScriptKey converts an RPC ScriptKey to an entities.ScriptKey.
func unmarshalScriptKey(rpcKey *taprpc.ScriptKey) (*entities.ScriptKey, error) {
	if len(rpcKey.PubKey) != 33 {
		return nil, fmt.Errorf("invalid public key length: %d",
			len(rpcKey.PubKey))
	}

	var pubKey [33]byte
	copy(pubKey[:], rpcKey.PubKey)

	scriptKey := &entities.ScriptKey{
		PubKey:   pubKey,
		TapTweak: rpcKey.TapTweak,
	}

	if rpcKey.KeyDesc != nil {
		keyDesc, err := unmarshalKeyDescriptor(rpcKey.KeyDesc)
		if err != nil {
			return nil, err
		}

		scriptKey.KeyDesc = *keyDesc
	}

	return scriptKey, nil
}

// unmarshalKeyDescriptor converts an RPC KeyDescriptor to an
// entities.KeyDescriptor.
func unmarshalKeyDescriptor(rpcKey *taprpc.KeyDescriptor) (
	*entities.KeyDescriptor, error) {

	if len(rpcKey.RawKeyBytes) != 33 {
		return nil, fmt.Errorf("invalid raw key bytes length: %d",
			len(rpcKey.RawKeyBytes))
	}

	var rawKeyBytes [33]byte
	copy(rawKeyBytes[:], rpcKey.RawKeyBytes)

	keyDesc := &entities.KeyDescriptor{
		RawKeyBytes: rawKeyBytes,
	}

	if rpcKey.KeyLoc != nil {
		keyDesc.KeyLocator = entities.KeyLocator{
			Family: uint32(rpcKey.KeyLoc.KeyFamily),
			Index:  uint32(rpcKey.KeyLoc.KeyIndex),
		}
	}

	return keyDesc, nil
}
