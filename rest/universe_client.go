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
