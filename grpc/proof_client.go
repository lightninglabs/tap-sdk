package client

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/internal/codec"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"google.golang.org/grpc"
)

// proofClient is a wrapper around the taprpc.TaprootAssetsClient for
// proof-related operations.
type proofClient struct {
	client   taprpc.TaprootAssetsClient
	timeout  time.Duration
	proofMac macaroon.SerializedMacaroon
}

// NewProofClient creates a new Proof client.
func NewProofClient(conn grpc.ClientConnInterface, timeout time.Duration,
	proofMac macaroon.SerializedMacaroon) *proofClient {

	return &proofClient{
		client:   taprpc.NewTaprootAssetsClient(conn),
		timeout:  timeout,
		proofMac: proofMac,
	}
}

// RawClientWithMacAuth returns a context with the proper macaroon
// authentication, the default RPC timeout, and the raw client.
func (p *proofClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	taprpc.TaprootAssetsClient) {

	return p.proofMac.WithMacaroonAuth(parentCtx), p.timeout, p.client
}

// ExportProof exports a proof file for a specific asset output.
func (p *proofClient) ExportProof(ctx context.Context, assetID,
	scriptKey []byte, outpoint *entities.Outpoint) (*entities.ProofFile,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	rpcCtx = p.proofMac.WithMacaroonAuth(rpcCtx)
	req := &taprpc.ExportProofRequest{
		AssetId:   assetID,
		ScriptKey: scriptKey,
	}
	if outpoint != nil {
		req.Outpoint = &taprpc.OutPoint{
			Txid:        outpoint.Txid[:],
			OutputIndex: outpoint.Index,
		}
	}

	resp, err := p.client.ExportProof(rpcCtx, req)
	if err != nil {
		return nil, err
	}

	return &entities.ProofFile{
		RawProofFile: resp.RawProofFile,
		GenesisPoint: resp.GenesisPoint,
	}, nil
}

// UnpackProofFile unpacks a proof file into individual proofs.
func (p *proofClient) UnpackProofFile(ctx context.Context,
	rawProofFile []byte) ([][]byte, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	rpcCtx = p.proofMac.WithMacaroonAuth(rpcCtx)
	resp, err := p.client.UnpackProofFile(rpcCtx, &taprpc.UnpackProofFileRequest{
		RawProofFile: rawProofFile,
	})
	if err != nil {
		return nil, err
	}

	return resp.RawProofs, nil
}

// DecodeProof decodes a raw proof and returns details about it.
func (p *proofClient) DecodeProof(ctx context.Context,
	rawProof []byte) (*entities.DecodedProof, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	rpcCtx = p.proofMac.WithMacaroonAuth(rpcCtx)
	resp, err := p.client.DecodeProof(rpcCtx, &taprpc.DecodeProofRequest{
		RawProof:          rawProof,
		WithMetaReveal:    true,
		WithPrevWitnesses: true,
	})
	if err != nil {
		return nil, err
	}

	if resp.DecodedProof == nil || resp.DecodedProof.Asset == nil {
		return nil, fmt.Errorf("invalid decoded proof response")
	}

	decoded := resp.DecodedProof
	asset := decoded.Asset

	result := &entities.DecodedProof{
		ProofAtDepth:   decoded.ProofAtDepth,
		NumberOfProofs: decoded.NumberOfProofs,
		IsIssuance:     decoded.GenesisReveal != nil,
	}

	// Copy asset ID.
	if asset.AssetGenesis != nil && len(asset.AssetGenesis.AssetId) == 32 {
		copy(result.AssetID[:], asset.AssetGenesis.AssetId)
	}

	// Copy script key.
	if len(asset.ScriptKey) == 33 {
		copy(result.ScriptKey[:], asset.ScriptKey)
	}

	result.Amount = asset.Amount

	// Get outpoint from chain anchor.
	if asset.ChainAnchor != nil {
		result.Outpoint = asset.ChainAnchor.AnchorOutpoint
	}

	// Get group key if present.
	if asset.AssetGroup != nil {
		result.GroupKey = asset.AssetGroup.TweakedGroupKey
	}

	// Decode alt leaves if present.
	if len(decoded.AltLeaves) > 0 {
		altLeaves, err := codec.DecodeAltLeaves(decoded.AltLeaves)
		if err != nil {
			return nil, fmt.Errorf("failed to decode alt leaves: %w",
				err)
		}

		result.AltLeaves = altLeaves
	}

	return result, nil
}

// RegisterTransfer registers an inbound transfer for an interactive send.
// The proof must already be in the local universe before calling this.
func (p *proofClient) RegisterTransfer(ctx context.Context, assetID,
	groupKey, scriptKey []byte, outpoint entities.Outpoint) (
	*entities.RegisteredAsset, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	rpcCtx = p.proofMac.WithMacaroonAuth(rpcCtx)
	resp, err := p.client.RegisterTransfer(rpcCtx, &taprpc.RegisterTransferRequest{
		AssetId:   assetID,
		GroupKey:  groupKey,
		ScriptKey: scriptKey,
		Outpoint: &taprpc.OutPoint{
			Txid:        outpoint.Txid[:],
			OutputIndex: outpoint.Index,
		},
	})
	if err != nil {
		return nil, err
	}

	if resp.RegisteredAsset == nil {
		return nil, fmt.Errorf("invalid registered asset response")
	}

	asset := resp.RegisteredAsset
	result := &entities.RegisteredAsset{
		Amount: asset.Amount,
	}

	// Copy asset ID.
	if asset.AssetGenesis != nil && len(asset.AssetGenesis.AssetId) == 32 {
		copy(result.AssetID[:], asset.AssetGenesis.AssetId)
	}

	// Copy script key.
	if len(asset.ScriptKey) == 33 {
		copy(result.ScriptKey[:], asset.ScriptKey)
	}

	// Get outpoint from chain anchor.
	if asset.ChainAnchor != nil {
		result.Outpoint = asset.ChainAnchor.AnchorOutpoint
	}

	return result, nil
}
