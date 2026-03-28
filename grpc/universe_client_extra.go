package grpc

import (
	"context"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/universerpc"
)

// AssetRoots returns the known universe roots for all assets.
func (u *universeClient) AssetRoots(ctx context.Context,
	req *entities.AssetRootRequest) (
	map[string]*entities.UniverseRoot, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &universerpc.AssetRootRequest{}
	if req != nil {
		rpcReq.WithAmountsById = req.WithAmountsByID
		rpcReq.Offset = req.Offset
		rpcReq.Limit = req.Limit
		rpcReq.Direction = marshalSortDirection(req.Direction)
	}

	resp, err := u.client.AssetRoots(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	roots := make(map[string]*entities.UniverseRoot, len(resp.UniverseRoots))
	for key, rpcRoot := range resp.UniverseRoots {
		root, err := unmarshalUniverseRoot(rpcRoot)
		if err != nil {
			return nil, fmt.Errorf("root %s: %w", key, err)
		}
		roots[key] = root
	}

	return roots, nil
}

// QueryAssetRoots queries the issuance and transfer roots for a specific
// asset.
func (u *universeClient) QueryAssetRoots(ctx context.Context,
	id *entities.UniverseID) (*entities.QueryRootResponse, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &universerpc.AssetRootQuery{
		Id: marshalUniverseID(id),
	}

	resp, err := u.client.QueryAssetRoots(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := &entities.QueryRootResponse{}

	if resp.IssuanceRoot != nil {
		root, err := unmarshalUniverseRoot(resp.IssuanceRoot)
		if err != nil {
			return nil, fmt.Errorf("issuance root: %w", err)
		}
		result.IssuanceRoot = root
	}

	if resp.TransferRoot != nil {
		root, err := unmarshalUniverseRoot(resp.TransferRoot)
		if err != nil {
			return nil, fmt.Errorf("transfer root: %w", err)
		}
		result.TransferRoot = root
	}

	return result, nil
}

// DeleteAssetRoot deletes a universe root for a specific asset.
func (u *universeClient) DeleteAssetRoot(ctx context.Context,
	id *entities.UniverseID) error {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &universerpc.DeleteRootQuery{
		Id: marshalUniverseID(id),
	}

	_, err := u.client.DeleteAssetRoot(rpcCtx, rpcReq)
	return err
}

// AssetLeafKeys returns the set of leaf keys for a universe.
func (u *universeClient) AssetLeafKeys(ctx context.Context,
	req *entities.AssetLeafKeysRequest) ([]entities.AssetLeafKey,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &universerpc.AssetLeafKeysRequest{
		Id: marshalUniverseID(&req.ID),
	}
	if req != nil {
		rpcReq.Offset = req.Offset
		rpcReq.Limit = req.Limit
		rpcReq.Direction = marshalSortDirection(req.Direction)
	}

	resp, err := u.client.AssetLeafKeys(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	keys := make([]entities.AssetLeafKey, 0, len(resp.AssetKeys))
	for _, rpcKey := range resp.AssetKeys {
		key, err := unmarshalAssetLeafKey(rpcKey)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *key)
	}

	return keys, nil
}

// AssetLeaves returns the set of asset leaves for a universe.
func (u *universeClient) AssetLeaves(ctx context.Context,
	id *entities.UniverseID) ([]entities.AssetLeaf, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	resp, err := u.client.AssetLeaves(rpcCtx, marshalUniverseID(id))
	if err != nil {
		return nil, err
	}

	leaves := make([]entities.AssetLeaf, 0, len(resp.Leaves))
	for _, rpcLeaf := range resp.Leaves {
		leaf, err := unmarshalAssetLeaf(rpcLeaf)
		if err != nil {
			return nil, err
		}
		leaves = append(leaves, *leaf)
	}

	return leaves, nil
}

// QueryProof queries a specific proof from the universe.
func (u *universeClient) QueryProof(ctx context.Context,
	key *entities.UniverseKey) (*entities.AssetProofResponse, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcKey := marshalUniverseKey(key)

	resp, err := u.client.QueryProof(rpcCtx, rpcKey)
	if err != nil {
		return nil, err
	}

	return unmarshalAssetProofResponse(resp)
}

// UniverseStats returns aggregate statistics for the universe.
func (u *universeClient) UniverseStats(
	ctx context.Context) (*entities.UniverseStats, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	resp, err := u.client.UniverseStats(
		rpcCtx, &universerpc.StatsRequest{},
	)
	if err != nil {
		return nil, err
	}

	return &entities.UniverseStats{
		NumTotalAssets: resp.NumTotalAssets,
		NumTotalGroups: resp.NumTotalGroups,
		NumTotalSyncs:  resp.NumTotalSyncs,
		NumTotalProofs: resp.NumTotalProofs,
	}, nil
}

// QueryAssetStats returns per-asset statistics.
func (u *universeClient) QueryAssetStats(ctx context.Context,
	req *entities.AssetStatsQuery) ([]entities.AssetStatsSnapshot,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalAssetStatsQuery(req)

	resp, err := u.client.QueryAssetStats(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	snapshots := make(
		[]entities.AssetStatsSnapshot, 0,
		len(resp.AssetStats),
	)
	for _, rpcSnap := range resp.AssetStats {
		snap, err := unmarshalAssetStatsSnapshot(rpcSnap)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, *snap)
	}

	return snapshots, nil
}

// QueryEvents returns daily event counts for a time range.
func (u *universeClient) QueryEvents(ctx context.Context,
	req *entities.QueryEventsRequest) (
	[]entities.GroupedUniverseEvents, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &universerpc.QueryEventsRequest{
		StartTimestamp: req.StartTimestamp,
		EndTimestamp:   req.EndTimestamp,
	}

	resp, err := u.client.QueryEvents(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	events := make(
		[]entities.GroupedUniverseEvents, 0,
		len(resp.Events),
	)
	for _, rpcEvent := range resp.Events {
		events = append(events, entities.GroupedUniverseEvents{
			Date:           rpcEvent.Date,
			SyncEvents:     rpcEvent.SyncEvents,
			NewProofEvents: rpcEvent.NewProofEvents,
		})
	}

	return events, nil
}

// ListFederationServers lists the universe federation peers.
func (u *universeClient) ListFederationServers(
	ctx context.Context) ([]entities.FederationServer, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	resp, err := u.client.ListFederationServers(
		rpcCtx, &universerpc.ListFederationServersRequest{},
	)
	if err != nil {
		return nil, err
	}

	servers := make(
		[]entities.FederationServer, 0,
		len(resp.Servers),
	)
	for _, rpcServer := range resp.Servers {
		servers = append(servers, entities.FederationServer{
			Host: rpcServer.Host,
			ID:   rpcServer.Id,
		})
	}

	return servers, nil
}

// AddFederationServer adds servers to the federation.
func (u *universeClient) AddFederationServer(ctx context.Context,
	servers []entities.FederationServer) error {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcServers := make(
		[]*universerpc.UniverseFederationServer, 0,
		len(servers),
	)
	for _, s := range servers {
		rpcServers = append(
			rpcServers,
			&universerpc.UniverseFederationServer{
				Host: s.Host,
				Id:   s.ID,
			},
		)
	}

	_, err := u.client.AddFederationServer(
		rpcCtx,
		&universerpc.AddFederationServerRequest{
			Servers: rpcServers,
		},
	)
	return err
}

// DeleteFederationServer removes servers from the federation.
func (u *universeClient) DeleteFederationServer(ctx context.Context,
	servers []entities.FederationServer) error {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcServers := make(
		[]*universerpc.UniverseFederationServer, 0,
		len(servers),
	)
	for _, s := range servers {
		rpcServers = append(
			rpcServers,
			&universerpc.UniverseFederationServer{
				Host: s.Host,
				Id:   s.ID,
			},
		)
	}

	_, err := u.client.DeleteFederationServer(
		rpcCtx,
		&universerpc.DeleteFederationServerRequest{
			Servers: rpcServers,
		},
	)
	return err
}

// SetFederationSyncConfig sets the federation sync configuration.
func (u *universeClient) SetFederationSyncConfig(ctx context.Context,
	global []entities.GlobalFederationSyncConfig,
	asset []entities.AssetFederationSyncConfig) error {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcGlobal := make(
		[]*universerpc.GlobalFederationSyncConfig, 0,
		len(global),
	)
	for _, g := range global {
		rpcGlobal = append(
			rpcGlobal,
			&universerpc.GlobalFederationSyncConfig{
				ProofType: marshalProofType(
					g.ProofType,
				),
				AllowSyncInsert: g.AllowSyncInsert,
				AllowSyncExport: g.AllowSyncExport,
			},
		)
	}

	rpcAsset := make(
		[]*universerpc.AssetFederationSyncConfig, 0,
		len(asset),
	)
	for _, a := range asset {
		rpcAsset = append(
			rpcAsset,
			&universerpc.AssetFederationSyncConfig{
				Id:              marshalUniverseID(&a.ID),
				AllowSyncInsert: a.AllowSyncInsert,
				AllowSyncExport: a.AllowSyncExport,
			},
		)
	}

	_, err := u.client.SetFederationSyncConfig(
		rpcCtx,
		&universerpc.SetFederationSyncConfigRequest{
			GlobalSyncConfigs: rpcGlobal,
			AssetSyncConfigs:  rpcAsset,
		},
	)
	return err
}

// QueryFederationSyncConfig queries the federation sync configuration.
func (u *universeClient) QueryFederationSyncConfig(ctx context.Context,
	ids []entities.UniverseID) (*entities.FederationSyncConfig, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcIDs := make([]*universerpc.ID, 0, len(ids))
	for i := range ids {
		rpcIDs = append(rpcIDs, marshalUniverseID(&ids[i]))
	}

	resp, err := u.client.QueryFederationSyncConfig(
		rpcCtx,
		&universerpc.QueryFederationSyncConfigRequest{
			Id: rpcIDs,
		},
	)
	if err != nil {
		return nil, err
	}

	result := &entities.FederationSyncConfig{}

	for _, g := range resp.GlobalSyncConfigs {
		result.GlobalSyncConfigs = append(
			result.GlobalSyncConfigs,
			entities.GlobalFederationSyncConfig{
				ProofType: unmarshalProofType(
					g.ProofType,
				),
				AllowSyncInsert: g.AllowSyncInsert,
				AllowSyncExport: g.AllowSyncExport,
			},
		)
	}

	for _, a := range resp.AssetSyncConfigs {
		uniID, err := unmarshalUniverseID(a.Id)
		if err != nil {
			return nil, fmt.Errorf("asset sync config "+
				"ID: %w", err)
		}
		result.AssetSyncConfigs = append(
			result.AssetSyncConfigs,
			entities.AssetFederationSyncConfig{
				ID:              *uniID,
				AllowSyncInsert: a.AllowSyncInsert,
				AllowSyncExport: a.AllowSyncExport,
			},
		)
	}

	return result, nil
}

// Info returns basic universe server information.
func (u *universeClient) Info(
	ctx context.Context) (*entities.UniverseInfo, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	resp, err := u.client.Info(
		rpcCtx, &universerpc.InfoRequest{},
	)
	if err != nil {
		return nil, err
	}

	return &entities.UniverseInfo{
		RuntimeID: resp.RuntimeId,
	}, nil
}

// SyncUniverse synchronizes with a remote universe server.
func (u *universeClient) SyncUniverse(ctx context.Context,
	req *entities.SyncRequest) ([]entities.SyncedUniverse, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	rpcTargets := make(
		[]*universerpc.SyncTarget, 0,
		len(req.SyncTargets),
	)
	for i := range req.SyncTargets {
		rpcTargets = append(
			rpcTargets, &universerpc.SyncTarget{
				Id: marshalUniverseID(
					&req.SyncTargets[i].ID,
				),
			},
		)
	}

	rpcReq := &universerpc.SyncRequest{
		UniverseHost: req.UniverseHost,
		SyncMode: universerpc.UniverseSyncMode(
			req.SyncMode,
		),
		SyncTargets: rpcTargets,
	}

	resp, err := u.client.SyncUniverse(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	synced := make(
		[]entities.SyncedUniverse, 0,
		len(resp.SyncedUniverses),
	)
	for _, rpcSynced := range resp.SyncedUniverses {
		s, err := unmarshalSyncedUniverse(rpcSynced)
		if err != nil {
			return nil, err
		}
		synced = append(synced, *s)
	}

	return synced, nil
}

// marshalUniverseID converts an SDK UniverseID to the RPC type.
func marshalUniverseID(id *entities.UniverseID) *universerpc.ID {
	rpcID := &universerpc.ID{
		ProofType: marshalProofType(id.ProofType),
	}

	switch {
	case id.GroupKey != nil:
		rpcID.Id = &universerpc.ID_GroupKey{
			GroupKey: id.GroupKey[:],
		}
	case id.AssetID != nil:
		rpcID.Id = &universerpc.ID_AssetId{
			AssetId: id.AssetID[:],
		}
	}

	return rpcID
}

// unmarshalUniverseID converts an RPC universe ID to the SDK type.
func unmarshalUniverseID(
	rpcID *universerpc.ID) (*entities.UniverseID, error) {

	if rpcID == nil {
		return nil, fmt.Errorf("nil universe ID")
	}

	uniID := &entities.UniverseID{
		ProofType: unmarshalProofType(rpcID.ProofType),
	}

	switch v := rpcID.Id.(type) {
	case *universerpc.ID_AssetId:
		assetID, err := entities.ParseAssetID(v.AssetId)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID: %w",
				err)
		}
		uniID.AssetID = &assetID

	case *universerpc.ID_AssetIdStr:
		assetID, err := entities.ParseAssetIDHex(v.AssetIdStr)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID "+
				"string: %w", err)
		}
		uniID.AssetID = &assetID

	case *universerpc.ID_GroupKey:
		groupKey, err := entities.ParsePubKey(v.GroupKey)
		if err != nil {
			return nil, fmt.Errorf("invalid group "+
				"key: %w", err)
		}
		uniID.GroupKey = &groupKey

	case *universerpc.ID_GroupKeyStr:
		groupKey, err := entities.ParsePubKeyHex(
			v.GroupKeyStr,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid group key "+
				"string: %w", err)
		}
		uniID.GroupKey = &groupKey
	}

	return uniID, nil
}

// marshalProofType converts an SDK ProofType to the RPC type.
func marshalProofType(pt entities.ProofType) universerpc.ProofType {
	switch pt {
	case entities.ProofTypeIssuance:
		return universerpc.ProofType_PROOF_TYPE_ISSUANCE
	case entities.ProofTypeTransfer:
		return universerpc.ProofType_PROOF_TYPE_TRANSFER
	default:
		return universerpc.ProofType_PROOF_TYPE_UNSPECIFIED
	}
}

// unmarshalProofType converts an RPC ProofType to the SDK type.
func unmarshalProofType(
	pt universerpc.ProofType) entities.ProofType {

	switch pt {
	case universerpc.ProofType_PROOF_TYPE_ISSUANCE:
		return entities.ProofTypeIssuance
	case universerpc.ProofType_PROOF_TYPE_TRANSFER:
		return entities.ProofTypeTransfer
	default:
		return entities.ProofTypeUnspecified
	}
}

// marshalSortDirection converts an SDK SortDirection to the RPC type.
// NOTE: this follows the same direct-cast approach used in wallet_client.go
// for consistency.
func marshalSortDirection(
	d entities.SortDirection) taprpc.SortDirection {

	return taprpc.SortDirection(d)
}

// unmarshalMerkleSumNode converts an RPC MerkleSumNode to the SDK type.
func unmarshalMerkleSumNode(
	rpcNode *universerpc.MerkleSumNode) (*entities.MerkleSumNode,
	error) {

	if rpcNode == nil {
		return nil, nil
	}

	rootHash, err := entities.ParseHash(rpcNode.RootHash)
	if err != nil {
		return nil, fmt.Errorf("invalid root hash: %w", err)
	}

	return &entities.MerkleSumNode{
		RootHash: rootHash,
		RootSum:  rpcNode.RootSum,
	}, nil
}

// unmarshalUniverseRoot converts an RPC UniverseRoot to the SDK type.
func unmarshalUniverseRoot(
	rpcRoot *universerpc.UniverseRoot) (*entities.UniverseRoot,
	error) {

	if rpcRoot == nil {
		return nil, fmt.Errorf("nil universe root")
	}

	uniID, err := unmarshalUniverseID(rpcRoot.Id)
	if err != nil {
		return nil, err
	}

	mssmtRoot, err := unmarshalMerkleSumNode(rpcRoot.MssmtRoot)
	if err != nil {
		return nil, err
	}

	return &entities.UniverseRoot{
		ID:               *uniID,
		MSSMTRoot:        mssmtRoot,
		AssetName:        rpcRoot.AssetName,
		AmountsByAssetID: rpcRoot.AmountsByAssetId,
	}, nil
}

// unmarshalAssetLeafKey converts an RPC AssetKey to the SDK AssetLeafKey.
func unmarshalAssetLeafKey(
	rpcKey *universerpc.AssetKey) (*entities.AssetLeafKey, error) {

	if rpcKey == nil {
		return nil, fmt.Errorf("nil asset key")
	}

	key := &entities.AssetLeafKey{}

	// Parse the outpoint.
	switch v := rpcKey.Outpoint.(type) {
	case *universerpc.AssetKey_OpStr:
		op, err := entities.NewOutpointFromStr(v.OpStr)
		if err != nil {
			return nil, fmt.Errorf("invalid outpoint: %w",
				err)
		}
		key.Outpoint = op

	case *universerpc.AssetKey_Op:
		if v.Op != nil {
			op, err := entities.NewOutpointFromStr(
				fmt.Sprintf("%s:%d",
					v.Op.HashStr, v.Op.Index),
			)
			if err != nil {
				return nil, fmt.Errorf("invalid "+
					"outpoint: %w", err)
			}
			key.Outpoint = op
		}
	}

	// Parse the script key.
	switch v := rpcKey.ScriptKey.(type) {
	case *universerpc.AssetKey_ScriptKeyBytes:
		scriptKey, err := entities.ParsePubKey(
			v.ScriptKeyBytes,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid script "+
				"key: %w", err)
		}
		key.ScriptKey = scriptKey

	case *universerpc.AssetKey_ScriptKeyStr:
		scriptKey, err := entities.ParsePubKeyHex(
			v.ScriptKeyStr,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid script key "+
				"string: %w", err)
		}
		key.ScriptKey = scriptKey
	}

	return key, nil
}

// unmarshalAssetLeaf converts an RPC AssetLeaf to the SDK type.
func unmarshalAssetLeaf(
	rpcLeaf *universerpc.AssetLeaf) (*entities.AssetLeaf, error) {

	if rpcLeaf == nil {
		return nil, fmt.Errorf("nil asset leaf")
	}

	leaf := &entities.AssetLeaf{
		Proof: rpcLeaf.Proof,
	}

	// The asset field is optional in some contexts.
	if rpcLeaf.Asset != nil {
		asset, err := unmarshalAsset(rpcLeaf.Asset)
		if err != nil {
			return nil, fmt.Errorf("invalid asset in "+
				"leaf: %w", err)
		}
		leaf.Asset = asset
	}

	return leaf, nil
}

// marshalUniverseKey converts an SDK UniverseKey to the RPC type.
func marshalUniverseKey(
	key *entities.UniverseKey) *universerpc.UniverseKey {

	rpcKey := &universerpc.UniverseKey{
		Id: marshalUniverseID(&key.ID),
		LeafKey: &universerpc.AssetKey{
			Outpoint: &universerpc.AssetKey_OpStr{
				OpStr: key.LeafKey.Outpoint.String(),
			},
			ScriptKey: &universerpc.AssetKey_ScriptKeyBytes{
				ScriptKeyBytes: key.LeafKey.ScriptKey[:],
			},
		},
	}

	return rpcKey
}

// unmarshalAssetProofResponse converts an RPC AssetProofResponse to the
// SDK type.
func unmarshalAssetProofResponse(
	resp *universerpc.AssetProofResponse) (
	*entities.AssetProofResponse, error) {

	if resp == nil {
		return nil, fmt.Errorf("nil proof response")
	}

	result := &entities.AssetProofResponse{
		UniverseInclusionProof:   resp.UniverseInclusionProof,
		MultiverseInclusionProof: resp.MultiverseInclusionProof,
	}

	// Unmarshal the key.
	if resp.Req != nil {
		uniKey, err := unmarshalUniverseKeyFromRPC(resp.Req)
		if err != nil {
			return nil, fmt.Errorf("proof key: %w", err)
		}
		result.Key = *uniKey
	}

	// Unmarshal the universe root.
	if resp.UniverseRoot != nil {
		root, err := unmarshalUniverseRoot(resp.UniverseRoot)
		if err != nil {
			return nil, fmt.Errorf("universe root: %w",
				err)
		}
		result.UniverseRoot = root
	}

	// Unmarshal the asset leaf.
	if resp.AssetLeaf != nil {
		leaf, err := unmarshalAssetLeaf(resp.AssetLeaf)
		if err != nil {
			return nil, fmt.Errorf("asset leaf: %w", err)
		}
		result.AssetLeaf = leaf
	}

	// Unmarshal the multiverse root.
	if resp.MultiverseRoot != nil {
		mvRoot, err := unmarshalMerkleSumNode(
			resp.MultiverseRoot,
		)
		if err != nil {
			return nil, fmt.Errorf("multiverse "+
				"root: %w", err)
		}
		result.MultiverseRoot = mvRoot
	}

	return result, nil
}

// unmarshalUniverseKeyFromRPC converts an RPC UniverseKey to the SDK type.
func unmarshalUniverseKeyFromRPC(
	rpcKey *universerpc.UniverseKey) (*entities.UniverseKey, error) {

	if rpcKey == nil {
		return nil, fmt.Errorf("nil universe key")
	}

	key := &entities.UniverseKey{}

	if rpcKey.Id != nil {
		uniID, err := unmarshalUniverseID(rpcKey.Id)
		if err != nil {
			return nil, err
		}
		key.ID = *uniID
	}

	if rpcKey.LeafKey != nil {
		leafKey, err := unmarshalAssetLeafKey(rpcKey.LeafKey)
		if err != nil {
			return nil, err
		}
		key.LeafKey = *leafKey
	}

	return key, nil
}

// marshalAssetStatsQuery converts an SDK AssetStatsQuery to the RPC type.
func marshalAssetStatsQuery(
	req *entities.AssetStatsQuery) *universerpc.AssetStatsQuery {

	rpcReq := &universerpc.AssetStatsQuery{
		AssetNameFilter: req.AssetNameFilter,
		AssetTypeFilter: universerpc.AssetTypeFilter(
			req.AssetTypeFilter,
		),
		SortBy: universerpc.AssetQuerySort(req.SortBy),
		Offset: req.Offset,
		Limit:  req.Limit,
		Direction: marshalSortDirection(
			req.Direction,
		),
	}

	if req.AssetIDFilter != nil {
		rpcReq.AssetIdFilter = req.AssetIDFilter[:]
	}

	return rpcReq
}

// unmarshalAssetStatsSnapshot converts an RPC AssetStatsSnapshot to the
// SDK type.
func unmarshalAssetStatsSnapshot(
	rpcSnap *universerpc.AssetStatsSnapshot) (
	*entities.AssetStatsSnapshot, error) {

	if rpcSnap == nil {
		return nil, fmt.Errorf("nil stats snapshot")
	}

	snap := &entities.AssetStatsSnapshot{
		GroupSupply: rpcSnap.GroupSupply,
		TotalSyncs:  rpcSnap.TotalSyncs,
		TotalProofs: rpcSnap.TotalProofs,
	}

	if len(rpcSnap.GroupKey) > 0 {
		groupKey, err := entities.ParsePubKey(
			rpcSnap.GroupKey,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid group "+
				"key: %w", err)
		}
		snap.GroupKey = &groupKey
	}

	if rpcSnap.GroupAnchor != nil {
		anchor, err := unmarshalAssetStatsAsset(
			rpcSnap.GroupAnchor,
		)
		if err != nil {
			return nil, fmt.Errorf("group anchor: %w",
				err)
		}
		snap.GroupAnchor = anchor
	}

	if rpcSnap.Asset != nil {
		asset, err := unmarshalAssetStatsAsset(rpcSnap.Asset)
		if err != nil {
			return nil, fmt.Errorf("asset: %w", err)
		}
		snap.Asset = asset
	}

	return snap, nil
}

// unmarshalAssetStatsAsset converts an RPC AssetStatsAsset to the SDK type.
func unmarshalAssetStatsAsset(
	rpcAsset *universerpc.AssetStatsAsset) (
	*entities.AssetStatsAsset, error) {

	if rpcAsset == nil {
		return nil, fmt.Errorf("nil stats asset")
	}

	assetID, err := entities.ParseAssetID(rpcAsset.AssetId)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	assetType := entities.AssetTypeNormal
	if rpcAsset.AssetType == taprpc.AssetType_COLLECTIBLE {
		assetType = entities.AssetTypeCollectible
	}

	return &entities.AssetStatsAsset{
		AssetID:          assetID,
		GenesisPoint:     rpcAsset.GenesisPoint,
		TotalSupply:      rpcAsset.TotalSupply,
		AssetName:        rpcAsset.AssetName,
		AssetType:        assetType,
		GenesisHeight:    rpcAsset.GenesisHeight,
		GenesisTimestamp: rpcAsset.GenesisTimestamp,
		AnchorPoint:      rpcAsset.AnchorPoint,
		DecimalDisplay:   rpcAsset.DecimalDisplay,
	}, nil
}

// unmarshalSyncedUniverse converts an RPC SyncedUniverse to the SDK type.
func unmarshalSyncedUniverse(
	rpcSynced *universerpc.SyncedUniverse) (
	*entities.SyncedUniverse, error) {

	if rpcSynced == nil {
		return nil, fmt.Errorf("nil synced universe")
	}

	result := &entities.SyncedUniverse{}

	if rpcSynced.OldAssetRoot != nil {
		root, err := unmarshalUniverseRoot(
			rpcSynced.OldAssetRoot,
		)
		if err != nil {
			return nil, fmt.Errorf("old root: %w", err)
		}
		result.OldAssetRoot = root
	}

	if rpcSynced.NewAssetRoot != nil {
		root, err := unmarshalUniverseRoot(
			rpcSynced.NewAssetRoot,
		)
		if err != nil {
			return nil, fmt.Errorf("new root: %w", err)
		}
		result.NewAssetRoot = root
	}

	for _, rpcLeaf := range rpcSynced.NewAssetLeaves {
		leaf, err := unmarshalAssetLeaf(rpcLeaf)
		if err != nil {
			return nil, fmt.Errorf("synced leaf: %w", err)
		}
		result.NewAssetLeaves = append(
			result.NewAssetLeaves, *leaf,
		)
	}

	return result, nil
}
