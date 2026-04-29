package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/anchor"
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

func (m *walletKitClient) rawClientWithMacAuth(
	parentCtx context.Context) (context.Context,
	assetwalletrpc.AssetWalletClient) {

	return m.walletKitMac.WithMacaroonAuth(parentCtx), m.client
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
			Id:        input.IssuanceID[:],
			ScriptKey: input.ScriptKey[:],
		}
	}

	// FundVirtualPsbt only exposes AddressesWithAmounts, so every
	// Recipient must carry an explicit amount; there is no
	// embedded-amount wire path at this level.
	rpcRecipients := make([]*taprpc.AddressWithAmount, len(recipients))
	for i, r := range recipients {
		if r.Amount == nil {
			return nil, fmt.Errorf("FundTransfer recipient "+
				"%q requires an explicit Amount", r.Address)
		}
		rpcRecipients[i] = &taprpc.AddressWithAmount{
			TapAddr: r.Address,
			Amount:  *r.Amount,
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

	authCtx, client := m.rawClientWithMacAuth(ctx)
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

	authCtx, client := m.rawClientWithMacAuth(ctx)
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

	anchorPsbt, err := anchor.PreparePsbt(virtualPsbts, passivePsbts)
	if err != nil {
		return nil, fmt.Errorf("prepare anchor PSBT: %w", err)
	}

	req := &assetwalletrpc.CommitVirtualPsbtsRequest{
		VirtualPsbts:      virtualPsbts,
		PassiveAssetPsbts: passivePsbts,
		AnchorPsbt:        anchorPsbt,
		Fees: &assetwalletrpc.CommitVirtualPsbtsRequest_SatPerVbyte{
			SatPerVbyte: satPerVByte,
		},
		AnchorChangeOutput: &assetwalletrpc.CommitVirtualPsbtsRequest_Add{
			Add: true,
		},
	}

	authCtx, client := m.rawClientWithMacAuth(ctx)
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

	authCtx, client := m.rawClientWithMacAuth(ctx)
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

	authCtx, client := m.rawClientWithMacAuth(ctx)
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

	authCtx, client := m.rawClientWithMacAuth(ctx)
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

	authCtx, client := m.rawClientWithMacAuth(ctx)
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
	signedPsbts [][]byte) (*entities.AssetTransfer, error) {

	req := &assetwalletrpc.AnchorVirtualPsbtsRequest{
		VirtualPsbts: signedPsbts,
	}

	authCtx, client := m.rawClientWithMacAuth(ctx)
	resp, err := client.AnchorVirtualPsbts(authCtx, req)
	if err != nil {
		return nil, err
	}

	if resp.Transfer == nil {
		return nil, fmt.Errorf("invalid transfer response")
	}

	return unmarshalAssetTransfer(resp.Transfer)
}

// unmarshalScriptKey converts an RPC ScriptKey to an entities.ScriptKey.
func unmarshalScriptKey(rpcKey *taprpc.ScriptKey) (*entities.ScriptKey, error) {
	pubKey, err := entities.ParseTaprootPubKey(rpcKey.PubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid script key public key: %w", err)
	}

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

	rawKeyBytes, err := entities.ParsePubKey(rpcKey.RawKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid raw key bytes: %w", err)
	}

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

// QueryInternalKey looks up an internal key by its raw public key bytes.
// The input can be 32-byte x-only or 33-byte compressed.
func (m *walletKitClient) QueryInternalKey(ctx context.Context,
	internalKey []byte) (*entities.KeyDescriptor, error) {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	resp, err := client.QueryInternalKey(
		authCtx, &assetwalletrpc.QueryInternalKeyRequest{
			InternalKey: internalKey,
		},
	)
	if err != nil {
		return nil, err
	}

	if resp.InternalKey == nil {
		return nil, fmt.Errorf("nil internal key in response")
	}

	return unmarshalKeyDescriptor(resp.InternalKey)
}

// QueryScriptKey looks up a script key by its tweaked public key bytes.
// The input can be 32-byte x-only or 33-byte compressed.
func (m *walletKitClient) QueryScriptKey(ctx context.Context,
	tweakedScriptKey []byte) (*entities.ScriptKey, error) {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	resp, err := client.QueryScriptKey(
		authCtx, &assetwalletrpc.QueryScriptKeyRequest{
			TweakedScriptKey: tweakedScriptKey,
		},
	)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("nil script key in response")
	}

	return unmarshalScriptKey(resp.ScriptKey)
}

// ProveAssetOwnership generates a proof of ownership for an asset.
func (m *walletKitClient) ProveAssetOwnership(ctx context.Context,
	req *entities.ProveOwnershipRequest) (*entities.OwnershipProof,
	error) {

	if err := req.AssetRef.Validate(); err != nil {
		return nil, err
	}

	assetID, ok := req.AssetRef.AssetID()
	if !ok {
		return nil, fmt.Errorf("prove ownership requires an " +
			"asset-ID ref; group-key refs span multiple " +
			"tranches (see issue #85)")
	}

	authCtx, client := m.rawClientWithMacAuth(ctx)

	rpcReq := &assetwalletrpc.ProveAssetOwnershipRequest{
		AssetId:   assetID[:],
		ScriptKey: req.ScriptKey[:],
		Outpoint: &taprpc.OutPoint{
			Txid:        req.Outpoint.Txid[:],
			OutputIndex: req.Outpoint.Index,
		},
	}

	if len(req.Challenge) > 0 {
		rpcReq.Challenge = req.Challenge
	}

	resp, err := client.ProveAssetOwnership(authCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return &entities.OwnershipProof{
		ProofWithWitness: resp.ProofWithWitness,
	}, nil
}

// VerifyAssetOwnership verifies an asset ownership proof.
func (m *walletKitClient) VerifyAssetOwnership(ctx context.Context,
	req *entities.VerifyOwnershipRequest) (
	*entities.VerifyOwnershipResponse, error) {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	rpcReq := &assetwalletrpc.VerifyAssetOwnershipRequest{
		ProofWithWitness: req.ProofWithWitness,
	}

	if len(req.Challenge) > 0 {
		rpcReq.Challenge = req.Challenge
	}

	resp, err := client.VerifyAssetOwnership(authCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := &entities.VerifyOwnershipResponse{
		Valid:       resp.ValidProof,
		BlockHeight: resp.BlockHeight,
	}

	if resp.Outpoint != nil {
		result.Outpoint = entities.Outpoint{
			Index: resp.Outpoint.OutputIndex,
		}
		copy(result.Outpoint.Txid[:], resp.Outpoint.Txid)
	}

	if len(resp.BlockHash) == 32 {
		copy(result.BlockHash[:], resp.BlockHash)
	}

	return result, nil
}

// RemoveUTXOLease removes a lease on a UTXO.
func (m *walletKitClient) RemoveUTXOLease(ctx context.Context,
	outpoint entities.Outpoint) error {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	_, err := client.RemoveUTXOLease(
		authCtx, &assetwalletrpc.RemoveUTXOLeaseRequest{
			Outpoint: &taprpc.OutPoint{
				Txid:        outpoint.Txid[:],
				OutputIndex: outpoint.Index,
			},
		},
	)

	return err
}

// DeclareScriptKey informs the wallet about an externally derived
// script key.
func (m *walletKitClient) DeclareScriptKey(ctx context.Context,
	req *entities.DeclareScriptKeyRequest) (*entities.ScriptKey,
	error) {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	rpcKey := &taprpc.ScriptKey{
		PubKey:   req.ScriptKey.PubKey[:],
		TapTweak: req.ScriptKey.TapTweak,
	}

	if req.ScriptKey.KeyDesc.RawKeyBytes != (entities.PubKey{}) {
		rpcKey.KeyDesc = &taprpc.KeyDescriptor{
			RawKeyBytes: req.ScriptKey.KeyDesc.RawKeyBytes[:],
			KeyLoc: &taprpc.KeyLocator{
				KeyFamily: int32(
					req.ScriptKey.KeyDesc.KeyLocator.Family,
				),
				KeyIndex: int32(
					req.ScriptKey.KeyDesc.KeyLocator.Index,
				),
			},
		}
	}

	resp, err := client.DeclareScriptKey(
		authCtx, &assetwalletrpc.DeclareScriptKeyRequest{
			ScriptKey: rpcKey,
		},
	)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("nil script key in response")
	}

	return unmarshalScriptKey(resp.ScriptKey)
}

// ExportBackup exports an asset wallet backup blob.
func (m *walletKitClient) ExportBackup(ctx context.Context,
	mode entities.BackupMode) ([]byte, error) {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	resp, err := client.ExportAssetWalletBackup(
		authCtx, &assetwalletrpc.ExportAssetWalletBackupRequest{
			Mode: marshalBackupMode(mode),
		},
	)
	if err != nil {
		return nil, err
	}

	return resp.Backup, nil
}

// ImportBackup imports assets from a previously exported wallet backup blob.
func (m *walletKitClient) ImportBackup(ctx context.Context,
	backup []byte) (uint32, error) {

	authCtx, client := m.rawClientWithMacAuth(ctx)

	resp, err := client.ImportAssetsFromBackup(
		authCtx, &assetwalletrpc.ImportAssetsFromBackupRequest{
			Backup: backup,
		},
	)
	if err != nil {
		return 0, err
	}

	return resp.NumImported, nil
}
