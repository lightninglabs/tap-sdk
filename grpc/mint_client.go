package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/mintrpc"
	"google.golang.org/grpc"
)

// mintClient is a wrapper around the mintrpc.MintClient.
type mintClient struct {
	client   mintrpc.MintClient
	timeout  time.Duration
	adminMac macaroon.SerializedMacaroon
}

// NewMintClient creates a new Mint client.
func NewMintClient(conn grpc.ClientConnInterface, timeout time.Duration,
	adminMac macaroon.SerializedMacaroon) *mintClient {

	return &mintClient{
		client:   mintrpc.NewMintClient(conn),
		timeout:  timeout,
		adminMac: adminMac,
	}
}

// RawClientWithMacAuth returns a context with the proper macaroon
// authentication, the default RPC timeout, and the raw client.
func (m *mintClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	mintrpc.MintClient) {

	return m.adminMac.WithMacaroonAuth(parentCtx), m.timeout, m.client
}

// MintAsset adds an asset to the pending minting batch.
func (m *mintClient) MintAsset(ctx context.Context,
	req *entities.MintAssetRequest) (*entities.MintingBatch, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	rpcCtx = m.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalMintAssetRequest(req)
	resp, err := m.client.MintAsset(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return unmarshalMintingBatch(resp.PendingBatch)
}

// FinalizeBatch finalizes the current pending minting batch.
func (m *mintClient) FinalizeBatch(ctx context.Context,
	req *entities.FinalizeBatchRequest) (*entities.MintingBatch, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	rpcCtx = m.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq, err := marshalFinalizeBatchRequest(req)
	if err != nil {
		return nil, err
	}

	resp, err := m.client.FinalizeBatch(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return unmarshalMintingBatch(resp.Batch)
}

func marshalMintAssetRequest(
	req *entities.MintAssetRequest) *mintrpc.MintAssetRequest {

	if req == nil {
		return &mintrpc.MintAssetRequest{}
	}

	return &mintrpc.MintAssetRequest{
		Asset:         marshalMintAsset(req.Asset),
		ShortResponse: req.ShortResponse,
	}
}

func marshalMintAsset(asset *entities.MintAsset) *mintrpc.MintAsset {
	if asset == nil {
		return nil
	}

	rpcAsset := &mintrpc.MintAsset{
		AssetVersion:             taprpc.AssetVersion(asset.AssetVersion),
		AssetType:                taprpc.AssetType(asset.AssetType),
		Name:                     asset.Name,
		Amount:                   asset.Amount,
		NewGroupedAsset:          asset.NewGroupedAsset,
		GroupedAsset:             asset.GroupedAsset,
		GroupAnchor:              asset.GroupAnchor,
		GroupTapscriptRoot:       asset.GroupTapscriptRoot,
		ScriptKey:                marshalScriptKey(asset.ScriptKey),
		DecimalDisplay:           asset.DecimalDisplay,
		ExternalGroupKey:         marshalExternalKey(asset.ExternalGroupKey),
		EnableSupplyCommitments:  asset.EnableSupplyCommitments,
		GroupInternalKey:         marshalKeyDescriptor(asset.GroupInternalKey),
		AssetMeta:                marshalAssetMeta(asset.AssetMeta),
	}

	if asset.GroupKey != nil {
		rpcAsset.GroupKey = asset.GroupKey[:]
	}

	return rpcAsset
}

func marshalAssetMeta(meta *entities.AssetMeta) *taprpc.AssetMeta {
	if meta == nil {
		return nil
	}

	rpcMeta := &taprpc.AssetMeta{
		Data: meta.Data,
		Type: taprpc.AssetMetaType(meta.Type),
	}

	if meta.MetaHash != (entities.Hash{}) {
		rpcMeta.MetaHash = meta.MetaHash[:]
	}

	return rpcMeta
}

func marshalScriptKey(key *entities.ScriptKey) *taprpc.ScriptKey {
	if key == nil {
		return nil
	}

	rpcKey := &taprpc.ScriptKey{
		PubKey:   key.PubKey[:],
		TapTweak: key.TapTweak,
	}

	if key.KeyDesc != (entities.KeyDescriptor{}) {
		rpcKey.KeyDesc = marshalKeyDescriptor(&key.KeyDesc)
	}

	return rpcKey
}

func marshalKeyDescriptor(desc *entities.KeyDescriptor) *taprpc.KeyDescriptor {
	if desc == nil {
		return nil
	}

	return &taprpc.KeyDescriptor{
		RawKeyBytes: desc.RawKeyBytes[:],
		KeyLoc: &taprpc.KeyLocator{
			KeyFamily: int32(desc.KeyLocator.Family),
			KeyIndex:  int32(desc.KeyLocator.Index),
		},
	}
}

func marshalExternalKey(key *entities.ExternalKey) *taprpc.ExternalKey {
	if key == nil {
		return nil
	}

	return &taprpc.ExternalKey{
		Xpub:              key.XPub,
		MasterFingerprint: key.MasterFingerprint[:],
		DerivationPath:    key.DerivationPath,
	}
}

func marshalFinalizeBatchRequest(
	req *entities.FinalizeBatchRequest) (*mintrpc.FinalizeBatchRequest, error) {

	if req == nil {
		return &mintrpc.FinalizeBatchRequest{}, nil
	}

	rpcReq := &mintrpc.FinalizeBatchRequest{
		ShortResponse: req.ShortResponse,
		FeeRate:       req.FeeRate,
	}

	if req.BatchSibling == nil {
		return rpcReq, nil
	}

	if req.BatchSibling.FullTree != nil && req.BatchSibling.Branch != nil {
		return nil, fmt.Errorf("batch sibling must set exactly one variant")
	}

	if req.BatchSibling.FullTree != nil {
		leaves := make(
			[]*taprpc.TapLeaf, 0, len(req.BatchSibling.FullTree.Leaves),
		)
		for _, leaf := range req.BatchSibling.FullTree.Leaves {
			leaves = append(leaves, &taprpc.TapLeaf{
				Script: leaf.Script,
			})
		}

		rpcReq.BatchSibling = &mintrpc.FinalizeBatchRequest_FullTree{
			FullTree: &taprpc.TapscriptFullTree{
				AllLeaves: leaves,
			},
		}

		return rpcReq, nil
	}

	if req.BatchSibling.Branch != nil {
		rpcReq.BatchSibling = &mintrpc.FinalizeBatchRequest_Branch{
			Branch: &taprpc.TapBranch{
				LeftTaphash:  req.BatchSibling.Branch.LeftTapHash[:],
				RightTaphash: req.BatchSibling.Branch.RightTapHash[:],
			},
		}
	}

	return rpcReq, nil
}

func unmarshalMintingBatch(
	rpcBatch *mintrpc.MintingBatch) (*entities.MintingBatch, error) {

	if rpcBatch == nil {
		return nil, fmt.Errorf("nil minting batch")
	}

	batch := &entities.MintingBatch{
		BatchTxid:  rpcBatch.BatchTxid,
		State:      entities.BatchState(rpcBatch.State),
		CreatedAt:  rpcBatch.CreatedAt,
		HeightHint: rpcBatch.HeightHint,
		BatchPSBT:  rpcBatch.BatchPsbt,
	}

	if len(rpcBatch.BatchKey) != 0 {
		batchKey, err := entities.ParsePubKey(rpcBatch.BatchKey)
		if err != nil {
			return nil, fmt.Errorf("invalid batch key: %w", err)
		}

		batch.BatchKey = batchKey
	}

	batch.Assets = make([]entities.PendingMintAsset, 0, len(rpcBatch.Assets))
	for _, rpcAsset := range rpcBatch.Assets {
		asset, err := unmarshalPendingMintAsset(rpcAsset)
		if err != nil {
			return nil, err
		}

		batch.Assets = append(batch.Assets, *asset)
	}

	return batch, nil
}

func unmarshalPendingMintAsset(
	rpcAsset *mintrpc.PendingAsset) (*entities.PendingMintAsset, error) {

	if rpcAsset == nil {
		return nil, fmt.Errorf("nil pending mint asset")
	}

	asset := &entities.PendingMintAsset{
		AssetVersion:       entities.AssetVersion(rpcAsset.AssetVersion),
		AssetType:          entities.AssetType(rpcAsset.AssetType),
		Name:               rpcAsset.Name,
		Amount:             rpcAsset.Amount,
		NewGroupedAsset:    rpcAsset.NewGroupedAsset,
		GroupAnchor:        rpcAsset.GroupAnchor,
		GroupTapscriptRoot: rpcAsset.GroupTapscriptRoot,
	}

	if rpcAsset.AssetMeta != nil {
		meta, err := unmarshalAssetMeta(rpcAsset.AssetMeta)
		if err != nil {
			return nil, err
		}

		asset.AssetMeta = meta
	}

	if len(rpcAsset.GroupKey) != 0 {
		groupKey, err := entities.ParsePubKey(rpcAsset.GroupKey)
		if err != nil {
			return nil, fmt.Errorf("invalid group key: %w", err)
		}

		asset.GroupKey = &groupKey
	}

	if rpcAsset.GroupInternalKey != nil {
		groupInternalKey, err := unmarshalKeyDescriptor(
			rpcAsset.GroupInternalKey,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid group internal key: %w", err)
		}

		asset.GroupInternalKey = groupInternalKey
	}

	if rpcAsset.ScriptKey != nil {
		scriptKey, err := unmarshalScriptKey(rpcAsset.ScriptKey)
		if err != nil {
			return nil, fmt.Errorf("invalid script key: %w", err)
		}

		asset.ScriptKey = scriptKey
	}

	return asset, nil
}

func unmarshalAssetMeta(
	rpcMeta *taprpc.AssetMeta) (*entities.AssetMeta, error) {
	if rpcMeta == nil {
		return nil, fmt.Errorf("nil asset meta")
	}

	metaHash, err := entities.ParseHash(rpcMeta.MetaHash)
	if err != nil {
		return nil, fmt.Errorf("invalid asset meta hash: %w", err)
	}

	return &entities.AssetMeta{
		Data:     rpcMeta.Data,
		Type:     entities.AssetMetaType(rpcMeta.Type),
		MetaHash: metaHash,
	}, nil
}
