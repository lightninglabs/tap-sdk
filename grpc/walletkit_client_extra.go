package grpc

import (
	"context"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
)

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

	authCtx, client := m.rawClientWithMacAuth(ctx)

	rpcReq := &assetwalletrpc.ProveAssetOwnershipRequest{
		AssetId:   req.AssetID[:],
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
