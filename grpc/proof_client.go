package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/codec"
	"github.com/lightninglabs/tap-sdk/entities"
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

// ExportProof exports a proof file for a specific asset output.
func (p *proofClient) ExportProof(ctx context.Context,
	issuanceID entities.AssetID,
	scriptKey entities.PubKey, outpoint *entities.Outpoint) (*entities.ProofFile,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	rpcCtx = p.proofMac.WithMacaroonAuth(rpcCtx)
	req := &taprpc.ExportProofRequest{
		AssetId:   issuanceID[:],
		ScriptKey: scriptKey[:],
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

	proofFile := &entities.ProofFile{
		RawProofFile: resp.RawProofFile,
	}

	// GenesisPoint is optional - only parse if provided.
	if resp.GenesisPoint != "" {
		genesisPoint, err := entities.NewOutpointFromStr(resp.GenesisPoint)
		if err != nil {
			return nil, fmt.Errorf("invalid genesis point: %v", err)
		}
		proofFile.GenesisPoint = genesisPoint
	}

	return proofFile, nil
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
		copy(result.IssuanceID[:], asset.AssetGenesis.AssetId)
	}

	// Copy script key.
	if len(asset.ScriptKey) == 33 {
		copy(result.ScriptKey[:], asset.ScriptKey)
	}

	result.Amount = asset.Amount

	// Get outpoint from chain anchor.
	if asset.ChainAnchor != nil {
		op, err := entities.NewOutpointFromStr(asset.ChainAnchor.AnchorOutpoint)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor outpoint: %v", err)
		}
		result.Outpoint = op
	}

	// Get group key if present.
	if asset.AssetGroup != nil {
		if len(asset.AssetGroup.TweakedGroupKey) > 0 {
			groupKey, err := entities.ParsePubKey(
				asset.AssetGroup.TweakedGroupKey,
			)
			if err != nil {
				return nil, fmt.Errorf("invalid group key: %w", err)
			}

			result.AssetRef = entities.AssetRefFromGroupKey(groupKey)
		}
	}

	if result.AssetRef.IsZero() {
		result.AssetRef = entities.AssetRefFromAssetID(
			result.IssuanceID,
		)
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

	// Populate prev IDs if present.
	if len(asset.PrevWitnesses) > 0 {
		prevIDs := make([]entities.PrevID, 0, len(asset.PrevWitnesses))
		for idx, witness := range asset.PrevWitnesses {
			if witness == nil || witness.PrevId == nil {
				return nil, fmt.Errorf("missing prev_id for witness %d",
					idx)
			}

			prev := witness.PrevId
			prevOutpoint, err := entities.NewOutpointFromStr(
				prev.AnchorPoint,
			)
			if err != nil {
				return nil, fmt.Errorf("invalid prev_id outpoint for witness %d: %w",
					idx, err)
			}

			if len(prev.AssetId) != 32 {
				return nil, fmt.Errorf("invalid prev_id asset_id length for witness %d: %d",
					idx, len(prev.AssetId))
			}

			if len(prev.ScriptKey) != 33 {
				return nil, fmt.Errorf("invalid prev_id "+
					"script_key length for "+
					"witness %d: %d",
					idx, len(prev.ScriptKey))
			}

			var decodedPrev entities.PrevID
			decodedPrev.Outpoint = prevOutpoint
			copy(decodedPrev.IssuanceID[:], prev.AssetId)
			copy(decodedPrev.ScriptKey[:], prev.ScriptKey)

			prevIDs = append(prevIDs, decodedPrev)
		}

		result.PrevIDs = prevIDs
	}

	return result, nil
}

// RegisterTransfer registers an inbound transfer for an interactive send.
// The proof must already be in the local universe before calling this.
func (p *proofClient) RegisterTransfer(ctx context.Context,
	assetRef entities.AssetRef, scriptKey entities.PubKey,
	outpoint entities.Outpoint) (
	*entities.RegisteredAsset, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	rpcCtx = p.proofMac.WithMacaroonAuth(rpcCtx)
	if err := assetRef.Validate(); err != nil {
		return nil, err
	}

	rpcReq := &taprpc.RegisterTransferRequest{
		ScriptKey: scriptKey[:],
		Outpoint: &taprpc.OutPoint{
			Txid:        outpoint.Txid[:],
			OutputIndex: outpoint.Index,
		},
	}

	if assetID, ok := assetRef.AssetID(); ok {
		rpcReq.AssetId = assetID[:]
	}

	if groupKey, ok := assetRef.GroupKey(); ok {
		rpcReq.GroupKey = groupKey[:]
	}

	resp, err := p.client.RegisterTransfer(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	if resp.RegisteredAsset == nil {
		return nil, fmt.Errorf("invalid registered asset response")
	}

	asset := resp.RegisteredAsset
	result := &entities.RegisteredAsset{
		Amount:   asset.Amount,
		AssetRef: assetRef,
	}

	// Copy asset ID.
	if asset.AssetGenesis != nil && len(asset.AssetGenesis.AssetId) == 32 {
		copy(result.IssuanceID[:], asset.AssetGenesis.AssetId)
	}

	// Copy script key.
	if len(asset.ScriptKey) == 33 {
		copy(result.ScriptKey[:], asset.ScriptKey)
	}

	// Get outpoint from chain anchor.
	if asset.ChainAnchor != nil {
		op, err := entities.NewOutpointFromStr(asset.ChainAnchor.AnchorOutpoint)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor outpoint: %v", err)
		}
		result.Outpoint = op
	}

	return result, nil
}
