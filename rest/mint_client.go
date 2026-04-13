package rest

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
)

// mintClient implements tapsdk.MintClient over REST.
type mintClient struct {
	transport *transport
}

func newMintClient(tp *transport) *mintClient {
	return &mintClient{transport: tp}
}

// mintAssetRequest is the low-level RPC request shape for MintAsset.
// It is internal to this package; callers use CreateAssetRequest or
// CreateIssuanceRequest instead.
type mintAssetRequest struct {
	asset         *mintAsset
	shortResponse bool
}

// mintAsset describes a new asset to stage in a minting batch.
type mintAsset struct {
	entities.PendingMintAsset

	groupedAsset            bool
	decimalDisplay          uint32
	externalGroupKey        *entities.ExternalKey
	enableSupplyCommitments bool
}

// jsonMintAssetRequest is the JSON body for MintAsset.
type jsonMintAssetRequest struct {
	Asset         *jsonMintAsset `json:"asset"`
	ShortResponse bool           `json:"short_response,omitempty"`
}

// mintAssetRPC stages a new asset in the pending mint batch using
// the internal request type.
func (m *mintClient) mintAssetRPC(ctx context.Context,
	req *mintAssetRequest) (*entities.MintingBatch, error) {

	body := &jsonMintAssetRequest{}
	if req != nil {
		body.ShortResponse = req.shortResponse
		body.Asset = marshalMintAssetJSON(req.asset)
	}

	var resp jsonMintAssetResponse
	err := m.transport.doPost(
		ctx, "/v1/taproot-assets/assets",
		macaroon.MintServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalMintingBatch(resp.PendingBatch)
}

// CreateAsset stages a brand-new asset in the pending mint batch.
func (m *mintClient) CreateAsset(ctx context.Context,
	req *entities.CreateAssetRequest) (
	*entities.MintingBatch, error) {

	return m.mintAssetRPC(
		ctx, mintAssetRequestFromCreateAsset(req),
	)
}

// CreateIssuance stages an additional issuance in the pending
// mint batch.
func (m *mintClient) CreateIssuance(ctx context.Context,
	req *entities.CreateIssuanceRequest) (
	*entities.MintingBatch, error) {

	mintReq, err := mintAssetRequestFromCreateIssuance(req)
	if err != nil {
		return nil, err
	}

	return m.mintAssetRPC(ctx, mintReq)
}

// jsonFundBatchRequest is the JSON body for FundBatch.
type jsonFundBatchRequest struct {
	ShortResponse bool   `json:"short_response,omitempty"`
	FeeRate       uint32 `json:"fee_rate,omitempty"`
}

// FundBatch funds the current pending mint batch.
func (m *mintClient) FundBatch(ctx context.Context,
	req *entities.FundBatchRequest) (
	*entities.VerboseMintingBatch, error) {

	body := &jsonFundBatchRequest{}
	if req != nil {
		body.ShortResponse = req.ShortResponse
		body.FeeRate = req.FeeRate
	}

	var resp jsonFundBatchResponse
	err := m.transport.doPost(
		ctx, "/v1/taproot-assets/assets/mint/fund",
		macaroon.MintServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalRESTVerboseBatch(resp.Batch)
}

// jsonSealBatchRequest is the JSON body for SealBatch.
type jsonSealBatchRequest struct {
	ShortResponse  bool                `json:"short_response,omitempty"`
	GroupWitnesses []*jsonGroupWitness `json:"group_witnesses,omitempty"`

	SignedGroupVirtualPsbts []string `json:"signed_group_virtual_psbts,omitempty"` //nolint:lll
}

// jsonGroupWitness is the JSON shape of taprpc.GroupWitness.
type jsonGroupWitness struct {
	GenesisID string   `json:"genesis_id"`
	Witness   [][]byte `json:"witness"`
}

// SealBatch seals a funded batch before finalization.
func (m *mintClient) SealBatch(ctx context.Context,
	req *entities.SealBatchRequest) (
	*entities.MintingBatch, error) {

	body := &jsonSealBatchRequest{}
	if req != nil {
		body.ShortResponse = req.ShortResponse

		for _, w := range req.GroupWitnesses {
			gID := base64.StdEncoding.EncodeToString(
				w.GenesisID[:],
			)
			body.GroupWitnesses = append(
				body.GroupWitnesses,
				&jsonGroupWitness{
					GenesisID: gID,
					Witness:   w.Witness,
				},
			)
		}

		body.SignedGroupVirtualPsbts = req.SignedGroupVirtualPSBTs
	}

	var resp jsonSealBatchResponse
	err := m.transport.doPost(
		ctx, "/v1/taproot-assets/assets/mint/seal",
		macaroon.MintServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalMintingBatch(resp.Batch)
}

// jsonFinalizeBatchRequest is the JSON body for FinalizeBatch.
type jsonFinalizeBatchRequest struct {
	ShortResponse bool   `json:"short_response,omitempty"`
	FeeRate       uint32 `json:"fee_rate,omitempty"`
}

// FinalizeBatch finalizes the current pending mint batch.
func (m *mintClient) FinalizeBatch(ctx context.Context,
	req *entities.FinalizeBatchRequest) (
	*entities.MintingBatch, error) {

	body := &jsonFinalizeBatchRequest{}
	if req != nil {
		body.ShortResponse = req.ShortResponse
		body.FeeRate = req.FeeRate
	}

	var resp jsonFinalizeBatchResponse
	err := m.transport.doPost(
		ctx, "/v1/taproot-assets/assets/mint/finalize",
		macaroon.MintServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalMintingBatch(resp.Batch)
}

// CancelBatch cancels the current mint batch.
func (m *mintClient) CancelBatch(
	ctx context.Context) (*entities.CancelBatchResponse, error) {

	var resp jsonCancelBatchResponse
	err := m.transport.doPost(
		ctx, "/v1/taproot-assets/assets/mint/cancel",
		macaroon.MintServiceMac, nil, &resp,
	)
	if err != nil {
		return nil, err
	}

	batchKeyBytes, err := parseHexBytes(resp.BatchKey)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid batch key: %w", err,
		)
	}

	batchKey, err := entities.ParsePubKey(batchKeyBytes)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid batch key: %w", err,
		)
	}

	return &entities.CancelBatchResponse{
		BatchKey: batchKey,
	}, nil
}

// ListBatches lists mint batches known to the daemon.
func (m *mintClient) ListBatches(ctx context.Context,
	req *entities.ListBatchesRequest) (
	[]*entities.VerboseMintingBatch, error) {

	params := url.Values{}
	batchKeyPath := ""
	if req != nil {
		if req.BatchKey != nil {
			batchKeyPath = hex.EncodeToString(
				req.BatchKey[:],
			)
		}
		if req.Verbose {
			params.Set("verbose", "true")
		}
	}

	path := "/v1/taproot-assets/assets/mint/batches/" +
		batchKeyPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonListBatchesResponse
	err := m.transport.doGet(
		ctx, path, macaroon.MintServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	batches := make(
		[]*entities.VerboseMintingBatch, 0,
		len(resp.Batches),
	)
	for _, jsonBatch := range resp.Batches {
		batch, err := unmarshalRESTVerboseBatch(jsonBatch)
		if err != nil {
			return nil, err
		}

		batches = append(batches, batch)
	}

	return batches, nil
}

// unmarshalRESTVerboseBatch converts a JSON verbose batch to the
// entity type.
func unmarshalRESTVerboseBatch(
	b *jsonVerboseBatch) (*entities.VerboseMintingBatch, error) {

	if b == nil {
		return nil, fmt.Errorf("nil verbose batch")
	}

	batch, err := unmarshalMintingBatch(b.Batch)
	if err != nil {
		return nil, err
	}

	return &entities.VerboseMintingBatch{Batch: *batch}, nil
}

func mintAssetRequestFromCreateAsset(
	req *entities.CreateAssetRequest) *mintAssetRequest {

	if req == nil {
		return nil
	}

	asset := req.Asset
	if asset == nil {
		return &mintAssetRequest{
			shortResponse: req.ShortResponse,
		}
	}

	return &mintAssetRequest{
		shortResponse: req.ShortResponse,
		asset: &mintAsset{
			PendingMintAsset: entities.PendingMintAsset{
				AssetVersion:       asset.AssetVersion,
				AssetType:          asset.AssetType,
				Name:               asset.Name,
				AssetMeta:          asset.AssetMeta,
				Amount:             asset.InitialSupply,
				NewGroupedAsset:    asset.AllowIssuance,
				GroupInternalKey:   asset.GroupInternalKey,
				GroupTapscriptRoot: asset.GroupTapscriptRoot,
				ScriptKey:          asset.ScriptKey,
			},
			decimalDisplay:          asset.DecimalDisplay,
			externalGroupKey:        asset.ExternalGroupKey,
			enableSupplyCommitments: asset.EnableSupplyCommitments,
		},
	}
}

func mintAssetRequestFromCreateIssuance(
	req *entities.CreateIssuanceRequest) (*mintAssetRequest,
	error) {

	if req == nil {
		return nil, nil
	}

	issuance := req.Issuance
	if issuance == nil {
		return &mintAssetRequest{
			shortResponse: req.ShortResponse,
		}, nil
	}

	groupKey, ok := issuance.AssetRef.GroupKey()
	if !ok {
		return nil, fmt.Errorf("asset ref must resolve " +
			"to a group key to create an issuance")
	}

	return &mintAssetRequest{
		shortResponse: req.ShortResponse,
		asset: &mintAsset{
			PendingMintAsset: entities.PendingMintAsset{
				AssetVersion: issuance.AssetVersion,
				AssetType:    issuance.AssetType,
				Name:         issuance.Name,
				AssetMeta:    issuance.AssetMeta,
				Amount:       issuance.Amount,
				GroupKey:     &groupKey,
				ScriptKey:    issuance.ScriptKey,
			},
			groupedAsset:     true,
			decimalDisplay:   issuance.DecimalDisplay,
			externalGroupKey: issuance.ExternalGroupKey,
		},
	}, nil
}

// marshalMintAssetJSON converts a mintAsset to its JSON
// representation.
func marshalMintAssetJSON(asset *mintAsset) *jsonMintAsset {
	if asset == nil {
		return nil
	}

	ver := marshalAssetVersionJSON(asset.AssetVersion)
	typ := marshalAssetTypeJSON(asset.AssetType)

	rpcAsset := &jsonMintAsset{
		AssetVersion:    ver,
		AssetType:       typ,
		Name:            asset.Name,
		Amount:          fmt.Sprintf("%d", asset.Amount),
		NewGroupedAsset: asset.NewGroupedAsset,
		GroupedAsset:    asset.groupedAsset,
		GroupAnchor:     asset.GroupAnchor,

		EnableSupplyCommitments: asset.enableSupplyCommitments,
	}

	if asset.GroupKey != nil {
		rpcAsset.GroupKey = hex.EncodeToString(
			asset.GroupKey[:],
		)
	}

	if len(asset.GroupTapscriptRoot) > 0 {
		rpcAsset.GroupTapscriptRoot = hex.EncodeToString(
			asset.GroupTapscriptRoot,
		)
	}

	if asset.ScriptKey != nil {
		rpcAsset.ScriptKey = marshalScriptKeyJSON(
			asset.ScriptKey,
		)
	}

	if asset.AssetMeta != nil {
		rpcAsset.AssetMeta = marshalAssetMetaJSON(
			asset.AssetMeta,
		)
	}

	if asset.GroupInternalKey != nil {
		rpcAsset.GroupInternalKey = marshalKeyDescriptorJSON(
			asset.GroupInternalKey,
		)
	}

	rpcAsset.DecimalDisplay = asset.decimalDisplay

	return rpcAsset
}

// marshalAssetTypeJSON converts an AssetType to a proto JSON
// enum string.
func marshalAssetTypeJSON(t entities.AssetType) string {
	switch t {
	case entities.AssetTypeCollectible:
		return "COLLECTIBLE"
	default:
		return "NORMAL"
	}
}

// marshalAssetMetaJSON converts AssetMeta to JSON.
func marshalAssetMetaJSON(
	meta *entities.AssetMeta) *jsonAssetMeta {

	if meta == nil {
		return nil
	}

	result := &jsonAssetMeta{
		Data: base64.StdEncoding.EncodeToString(meta.Data),
	}

	if meta.MetaHash != (entities.Hash{}) {
		result.MetaHash = hex.EncodeToString(
			meta.MetaHash[:],
		)
	}

	switch meta.Type {
	case entities.AssetMetaTypeJSON:
		result.Type = "META_TYPE_JSON"
	default:
		result.Type = "META_TYPE_OPAQUE"
	}

	return result
}
