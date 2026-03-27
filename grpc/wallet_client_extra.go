package grpc

import (
	"context"
	"fmt"
	"maps"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
)

// ListUtxos lists managed UTXOs with optional filtering.
func (s *walletClient) ListUtxos(ctx context.Context,
	req *entities.ListUtxosRequest) (map[string]*entities.ManagedUtxo,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.ListUtxosRequest{}
	if req != nil {
		rpcReq.IncludeLeased = req.IncludeLeased
		rpcReq.ScriptKeyType = marshalScriptKeyTypeQuery(
			req.ScriptKeyType,
		)
	}

	resp, err := s.client.ListUtxos(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := make(
		map[string]*entities.ManagedUtxo, len(resp.ManagedUtxos),
	)

	for key, rpcUtxo := range resp.ManagedUtxos {
		utxo, err := unmarshalManagedUtxo(rpcUtxo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"utxo %s: %w", key, err)
		}

		result[key] = utxo
	}

	return result, nil
}

// ListGroups lists all known asset groups.
func (s *walletClient) ListGroups(
	ctx context.Context) (map[string]*entities.GroupedAssets, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	resp, err := s.client.ListGroups(
		rpcCtx, &taprpc.ListGroupsRequest{},
	)
	if err != nil {
		return nil, err
	}

	result := make(
		map[string]*entities.GroupedAssets, len(resp.Groups),
	)

	for key, rpcGroup := range resp.Groups {
		group, err := unmarshalGroupedAssets(rpcGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"group %s: %w", key, err)
		}

		result[key] = group
	}

	return result, nil
}

// BurnAsset burns asset units. The confirmation text must be set to
// "assets will be destroyed" for the burn to succeed.
func (s *walletClient) BurnAsset(ctx context.Context,
	req *entities.BurnAssetRequest) (*entities.BurnAssetResponse,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalBurnAssetRequest(req)

	resp, err := s.client.BurnAsset(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := &entities.BurnAssetResponse{}

	if resp.BurnTransfer != nil {
		transfer, err := unmarshalAssetTransfer(resp.BurnTransfer)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"burn transfer: %w", err)
		}

		result.BurnTransfer = transfer
	}

	if resp.BurnProof != nil {
		proof, err := unmarshalDecodedProof(resp.BurnProof)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"burn proof: %w", err)
		}

		result.BurnProof = proof
	}

	return result, nil
}

// ListBurns lists asset burns with optional filtering.
func (s *walletClient) ListBurns(ctx context.Context,
	req *entities.ListBurnsRequest) ([]*entities.AssetBurn, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.ListBurnsRequest{}
	if req != nil {
		if req.AssetID != nil {
			rpcReq.AssetId = req.AssetID[:]
		}
		if req.TweakedGroupKey != nil {
			rpcReq.TweakedGroupKey = req.TweakedGroupKey[:]
		}
		if req.AnchorTxid != nil {
			rpcReq.AnchorTxid = req.AnchorTxid[:]
		}
	}

	resp, err := s.client.ListBurns(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	burns := make([]*entities.AssetBurn, 0, len(resp.Burns))
	for _, rpcBurn := range resp.Burns {
		burn, err := unmarshalAssetBurn(rpcBurn)
		if err != nil {
			return nil, err
		}

		burns = append(burns, burn)
	}

	return burns, nil
}

// FetchAssetMeta fetches the metadata for an asset by ID or meta hash.
func (s *walletClient) FetchAssetMeta(ctx context.Context,
	req *entities.FetchAssetMetaRequest) (*entities.AssetMeta, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalFetchAssetMetaRequest(req)

	resp, err := s.client.FetchAssetMeta(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return unmarshalFetchAssetMetaResponse(resp)
}

// VerifyProof verifies a proof file and returns the decoded last proof
// if valid.
func (s *walletClient) VerifyProof(ctx context.Context,
	rawProofFile []byte) (*entities.VerifyProofResponse, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	resp, err := s.client.VerifyProof(rpcCtx, &taprpc.ProofFile{
		RawProofFile: rawProofFile,
	})
	if err != nil {
		return nil, err
	}

	result := &entities.VerifyProofResponse{
		Valid: resp.Valid,
	}

	if resp.DecodedProof != nil {
		decoded, err := unmarshalDecodedProof(resp.DecodedProof)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"decoded proof: %w", err)
		}

		result.DecodedProof = decoded
	}

	return result, nil
}

// unmarshalManagedUtxo converts an RPC ManagedUtxo to an
// entities.ManagedUtxo.
func unmarshalManagedUtxo(
	rpcUtxo *taprpc.ManagedUtxo) (*entities.ManagedUtxo, error) {

	if rpcUtxo == nil {
		return nil, fmt.Errorf("nil managed utxo")
	}

	outPoint, err := entities.NewOutpointFromStr(rpcUtxo.OutPoint)
	if err != nil {
		return nil, fmt.Errorf("invalid outpoint: %w", err)
	}

	internalKey, err := entities.ParsePubKey(rpcUtxo.InternalKey)
	if err != nil {
		return nil, fmt.Errorf("invalid internal key: %w", err)
	}

	taprootAssetRoot, err := entities.ParseHash(
		rpcUtxo.TaprootAssetRoot,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid taproot asset root: %w",
			err)
	}

	merkleRoot, err := entities.ParseHash(rpcUtxo.MerkleRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid merkle root: %w", err)
	}

	assets := make([]*entities.Asset, 0, len(rpcUtxo.Assets))
	for _, rpcAsset := range rpcUtxo.Assets {
		asset, err := unmarshalAsset(rpcAsset)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"asset: %w", err)
		}

		assets = append(assets, asset)
	}

	return &entities.ManagedUtxo{
		OutPoint:         outPoint,
		AmtSat:           rpcUtxo.AmtSat,
		InternalKey:      internalKey,
		TaprootAssetRoot: taprootAssetRoot,
		MerkleRoot:       merkleRoot,
		Assets:           assets,
		LeaseOwner:       rpcUtxo.LeaseOwner,
		LeaseExpiryUnix:  rpcUtxo.LeaseExpiryUnix,
	}, nil
}

// unmarshalGroupedAssets converts an RPC GroupedAssets to an
// entities.GroupedAssets.
func unmarshalGroupedAssets(
	rpcGroup *taprpc.GroupedAssets) (*entities.GroupedAssets, error) {

	if rpcGroup == nil {
		return nil, fmt.Errorf("nil grouped assets")
	}

	assets := make(
		[]*entities.AssetHumanReadable, 0, len(rpcGroup.Assets),
	)

	for _, rpcAsset := range rpcGroup.Assets {
		if rpcAsset == nil {
			return nil, fmt.Errorf("nil asset in group")
		}

		assetID, err := entities.ParseAssetID(rpcAsset.Id)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID: %w",
				err)
		}

		metaHash, err := entities.ParseHash(rpcAsset.MetaHash)
		if err != nil {
			return nil, fmt.Errorf("invalid meta hash: %w",
				err)
		}

		assets = append(assets, &entities.AssetHumanReadable{
			ID:               assetID,
			Amount:           rpcAsset.Amount,
			LockTime:         rpcAsset.LockTime,
			RelativeLockTime: rpcAsset.RelativeLockTime,
			Tag:              rpcAsset.Tag,
			MetaHash:         metaHash,
			Type:             entities.AssetType(rpcAsset.Type),
			Version:          uint8(rpcAsset.Version),
		})
	}

	return &entities.GroupedAssets{
		Assets: assets,
	}, nil
}

// unmarshalAssetBurn converts an RPC AssetBurn to an entities.AssetBurn.
func unmarshalAssetBurn(
	rpcBurn *taprpc.AssetBurn) (*entities.AssetBurn, error) {

	if rpcBurn == nil {
		return nil, fmt.Errorf("nil asset burn")
	}

	assetID, err := entities.ParseAssetID(rpcBurn.AssetId)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	anchorTxid, err := entities.ParseHash(rpcBurn.AnchorTxid)
	if err != nil {
		return nil, fmt.Errorf("invalid anchor txid: %w", err)
	}

	burn := &entities.AssetBurn{
		Note:        rpcBurn.Note,
		AssetID:     assetID,
		Amount:      rpcBurn.Amount,
		AnchorTxid:  anchorTxid,
	}

	if len(rpcBurn.TweakedGroupKey) > 0 {
		groupKey, err := entities.ParsePubKey(
			rpcBurn.TweakedGroupKey,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid tweaked group "+
				"key: %w", err)
		}

		burn.TweakedGroupKey = &groupKey
	}

	return burn, nil
}

// marshalBurnAssetRequest converts a BurnAssetRequest to an RPC request.
func marshalBurnAssetRequest(
	req *entities.BurnAssetRequest) *taprpc.BurnAssetRequest {

	rpcReq := &taprpc.BurnAssetRequest{
		AmountToBurn:     req.AmountToBurn,
		ConfirmationText: req.ConfirmationText,
		Note:             req.Note,
	}

	if req.AssetID != nil {
		rpcReq.Asset = &taprpc.BurnAssetRequest_AssetId{
			AssetId: req.AssetID[:],
		}
	} else if req.AssetIDStr != "" {
		rpcReq.Asset = &taprpc.BurnAssetRequest_AssetIdStr{
			AssetIdStr: req.AssetIDStr,
		}
	}

	return rpcReq
}

// marshalFetchAssetMetaRequest converts a FetchAssetMetaRequest to an
// RPC request.
func marshalFetchAssetMetaRequest(
	req *entities.FetchAssetMetaRequest) *taprpc.FetchAssetMetaRequest {

	rpcReq := &taprpc.FetchAssetMetaRequest{}

	if req.AssetID != nil {
		rpcReq.Asset = &taprpc.FetchAssetMetaRequest_AssetId{
			AssetId: req.AssetID[:],
		}
	} else if req.MetaHash != nil {
		rpcReq.Asset = &taprpc.FetchAssetMetaRequest_MetaHash{
			MetaHash: req.MetaHash[:],
		}
	}

	return rpcReq
}

// unmarshalFetchAssetMetaResponse converts an RPC FetchAssetMetaResponse
// to an entities.AssetMeta.
func unmarshalFetchAssetMetaResponse(
	resp *taprpc.FetchAssetMetaResponse) (*entities.AssetMeta, error) {

	if resp == nil {
		return nil, fmt.Errorf("nil asset meta response")
	}

	metaHash, err := entities.ParseHash(resp.MetaHash)
	if err != nil {
		return nil, fmt.Errorf("invalid meta hash: %w", err)
	}

	unknownOddTypes := make(
		map[uint64][]byte, len(resp.UnknownOddTypes),
	)
	maps.Copy(unknownOddTypes, resp.UnknownOddTypes)

	return &entities.AssetMeta{
		Data:                  resp.Data,
		Type:                  entities.AssetMetaType(resp.Type),
		MetaHash:              metaHash,
		UnknownOddTypes:       unknownOddTypes,
		DecimalDisplay:        resp.DecimalDisplay,
		UniverseCommitments:   resp.UniverseCommitments,
		CanonicalUniverseURLs: resp.CanonicalUniverseUrls,
		DelegationKey:         resp.DelegationKey,
	}, nil
}

// unmarshalDecodedProof converts an RPC DecodedProof to an
// entities.DecodedProof. This is extracted from proof_client.go for
// reuse in BurnAsset and VerifyProof.
func unmarshalDecodedProof(
	rpcProof *taprpc.DecodedProof) (*entities.DecodedProof, error) {

	if rpcProof == nil {
		return nil, fmt.Errorf("nil decoded proof")
	}

	if rpcProof.Asset == nil {
		return nil, fmt.Errorf("nil proof asset")
	}

	assetID, err := entities.ParseAssetID(
		rpcProof.Asset.AssetGenesis.AssetId,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	scriptKey, err := entities.ParseTaprootPubKey(
		rpcProof.Asset.ScriptKey,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid script key: %w", err)
	}

	proof := &entities.DecodedProof{
		ProofAtDepth:   rpcProof.ProofAtDepth,
		NumberOfProofs: rpcProof.NumberOfProofs,
		AssetID:        assetID,
		ScriptKey:      scriptKey,
		Amount:         rpcProof.Asset.Amount,
	}

	if rpcProof.Asset.ChainAnchor != nil {
		op, err := entities.NewOutpointFromStr(
			rpcProof.Asset.ChainAnchor.AnchorOutpoint,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor "+
				"outpoint: %w", err)
		}

		proof.Outpoint = op
	}

	if rpcProof.Asset.AssetGroup != nil &&
		len(rpcProof.Asset.AssetGroup.TweakedGroupKey) > 0 {

		groupKey, err := entities.ParsePubKey(
			rpcProof.Asset.AssetGroup.TweakedGroupKey,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid group key: %w",
				err)
		}

		proof.GroupKey = &groupKey
	}

	return proof, nil
}
