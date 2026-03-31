package rest

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
)

// universeClient implements tapsdk.UniverseClient over REST.
type universeClient struct {
	transport *transport
}

func newUniverseClient(tp *transport) *universeClient {
	return &universeClient{transport: tp}
}

// jsonUniverseProof is the JSON body for InsertProof.
type jsonUniverseProof struct {
	Key       *jsonUniverseKey  `json:"key"`
	AssetLeaf *jsonAssetLeafReq `json:"asset_leaf"`
}

// jsonUniverseKey is the JSON shape of universerpc.UniverseKey.
type jsonUniverseKey struct {
	ID      *jsonUniverseID  `json:"id"`
	LeafKey *jsonAssetKeyReq `json:"leaf_key"`
}

// jsonUniverseID is the JSON shape of universerpc.ID.
type jsonUniverseID struct {
	AssetID   string `json:"asset_id,omitempty"`
	GroupKey  string `json:"group_key,omitempty"`
	ProofType string `json:"proof_type"`
}

// jsonAssetKeyReq is the JSON shape of universerpc.AssetKey.
type jsonAssetKeyReq struct {
	OpStr          string `json:"op_str,omitempty"`
	ScriptKeyBytes string `json:"script_key_bytes,omitempty"`
}

// jsonAssetLeafReq is the JSON shape of universerpc.AssetLeaf.
type jsonAssetLeafReq struct {
	Proof string `json:"proof"`
}

// InsertProof inserts a proof into the local universe.
func (u *universeClient) InsertProof(ctx context.Context,
	rawProof []byte,
	decoded *entities.DecodedProof) error {

	proofType := "PROOF_TYPE_TRANSFER"
	if decoded.IsIssuance {
		proofType = "PROOF_TYPE_ISSUANCE"
	}

	uniID := &jsonUniverseID{ProofType: proofType}

	if decoded.GroupKey != nil {
		uniID.GroupKey = hex.EncodeToString(
			decoded.GroupKey[:],
		)
	} else {
		uniID.AssetID = hex.EncodeToString(
			decoded.AssetID[:],
		)
	}

	// Build the REST URL path. InsertProof uses a POST with
	// path parameters for the universe key.
	assetIDStr := hex.EncodeToString(decoded.AssetID[:])

	// The gRPC-gateway REST path uses the human-readable txid
	// (reversed byte order) and output index from the outpoint.
	outpointStr := decoded.Outpoint.String()
	parts := splitOutpoint(outpointStr)
	hashStr := parts[0]
	index := decoded.Outpoint.Index
	scriptKeyStr := hex.EncodeToString(decoded.ScriptKey[:])

	path := fmt.Sprintf(
		"/v1/taproot-assets/universe/proofs/"+
			"asset-id/%s/%s/%d/%s",
		assetIDStr, hashStr, index, scriptKeyStr,
	)

	body := &jsonUniverseProof{
		Key: &jsonUniverseKey{
			ID: uniID,
			LeafKey: &jsonAssetKeyReq{
				OpStr: decoded.Outpoint.String(),
				ScriptKeyBytes: hex.EncodeToString(
					decoded.ScriptKey[:],
				),
			},
		},
		AssetLeaf: &jsonAssetLeafReq{
			Proof: hex.EncodeToString(rawProof),
		},
	}

	var resp jsonInsertProofResponse
	err := u.transport.doPost(
		ctx, path, macaroon.UniverseServiceMac, body, &resp,
	)
	if err != nil {
		return err
	}

	return nil
}

// splitOutpoint splits "txid:index" into its parts, returning
// [txid, index]. If no colon found, returns the original string
// and "0".
func splitOutpoint(s string) [2]string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return [2]string{s[:i], s[i+1:]}
		}
	}

	return [2]string{s, "0"}
}

// AssetRoots returns the known universe roots for all assets.
func (u *universeClient) AssetRoots(ctx context.Context,
	req *entities.AssetRootRequest) (
	map[string]*entities.UniverseRoot, error) {

	return nil, errNotImplemented("AssetRoots")
}

// QueryAssetRoots queries the issuance and transfer roots for a
// specific asset.
func (u *universeClient) QueryAssetRoots(ctx context.Context,
	id *entities.UniverseID) (*entities.QueryRootResponse,
	error) {

	return nil, errNotImplemented("QueryAssetRoots")
}

// DeleteAssetRoot deletes a universe root and all associated data.
func (u *universeClient) DeleteAssetRoot(ctx context.Context,
	id *entities.UniverseID) error {

	return errNotImplemented("DeleteAssetRoot")
}

// AssetLeafKeys returns the set of leaf keys for a universe.
func (u *universeClient) AssetLeafKeys(ctx context.Context,
	req *entities.AssetLeafKeysRequest) (
	[]entities.AssetLeafKey, error) {

	return nil, errNotImplemented("AssetLeafKeys")
}

// AssetLeaves returns the set of asset leaves for a universe.
func (u *universeClient) AssetLeaves(ctx context.Context,
	id *entities.UniverseID) ([]entities.AssetLeaf, error) {

	return nil, errNotImplemented("AssetLeaves")
}

// QueryProof queries a specific proof from the universe.
func (u *universeClient) QueryProof(ctx context.Context,
	key *entities.UniverseKey) (*entities.AssetProofResponse,
	error) {

	return nil, errNotImplemented("QueryProof")
}

// UniverseStats returns aggregate statistics for the universe.
func (u *universeClient) UniverseStats(
	ctx context.Context) (*entities.UniverseStats, error) {

	return nil, errNotImplemented("UniverseStats")
}

// QueryAssetStats returns per-asset statistics.
func (u *universeClient) QueryAssetStats(ctx context.Context,
	req *entities.AssetStatsQuery) (
	[]entities.AssetStatsSnapshot, error) {

	return nil, errNotImplemented("QueryAssetStats")
}

// QueryEvents returns daily sync and proof event counts.
func (u *universeClient) QueryEvents(ctx context.Context,
	req *entities.QueryEventsRequest) (
	[]entities.GroupedUniverseEvents, error) {

	return nil, errNotImplemented("QueryEvents")
}

// ListFederationServers lists the universe federation peers.
func (u *universeClient) ListFederationServers(
	ctx context.Context) ([]entities.FederationServer, error) {

	return nil, errNotImplemented("ListFederationServers")
}

// AddFederationServer adds servers to the federation.
func (u *universeClient) AddFederationServer(ctx context.Context,
	servers []entities.FederationServer) error {

	return errNotImplemented("AddFederationServer")
}

// DeleteFederationServer removes servers from the federation.
func (u *universeClient) DeleteFederationServer(ctx context.Context,
	servers []entities.FederationServer) error {

	return errNotImplemented("DeleteFederationServer")
}

// SetFederationSyncConfig sets the federation sync configuration.
func (u *universeClient) SetFederationSyncConfig(
	ctx context.Context,
	global []entities.GlobalFederationSyncConfig,
	asset []entities.AssetFederationSyncConfig) error {

	return errNotImplemented("SetFederationSyncConfig")
}

// QueryFederationSyncConfig queries the federation sync config.
func (u *universeClient) QueryFederationSyncConfig(
	ctx context.Context,
	ids []entities.UniverseID) (
	*entities.FederationSyncConfig, error) {

	return nil, errNotImplemented("QueryFederationSyncConfig")
}

// Info returns basic universe server information.
func (u *universeClient) Info(
	ctx context.Context) (*entities.UniverseInfo, error) {

	return nil, errNotImplemented("Info")
}

// SyncUniverse synchronizes with a remote universe server.
func (u *universeClient) SyncUniverse(ctx context.Context,
	req *entities.SyncRequest) (
	[]entities.SyncedUniverse, error) {

	return nil, errNotImplemented("SyncUniverse")
}
