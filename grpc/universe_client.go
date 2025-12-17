package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc/universerpc"
	"google.golang.org/grpc"
)

// universeClient is a wrapper around the universerpc.UniverseClient.
type universeClient struct {
	client      universerpc.UniverseClient
	timeout     time.Duration
	universeMac macaroon.SerializedMacaroon
}

// NewUniverseClient creates a new Universe client.
func NewUniverseClient(conn grpc.ClientConnInterface, timeout time.Duration,
	universeMac macaroon.SerializedMacaroon) *universeClient {

	return &universeClient{
		client:      universerpc.NewUniverseClient(conn),
		timeout:     timeout,
		universeMac: universeMac,
	}
}

// RawClientWithMacAuth returns a context with the proper macaroon
// authentication, the default RPC timeout, and the raw client.
func (u *universeClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	universerpc.UniverseClient) {

	return u.universeMac.WithMacaroonAuth(parentCtx), u.timeout, u.client
}

// InsertProof inserts a proof into the local universe.
// The decoded proof information is used to construct the universe key.
func (u *universeClient) InsertProof(ctx context.Context, rawProof []byte,
	decoded *entities.DecodedProof) error {

	rpcCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	rpcCtx = u.universeMac.WithMacaroonAuth(rpcCtx)

	// Determine proof type based on whether this is issuance or transfer.
	proofType := universerpc.ProofType_PROOF_TYPE_TRANSFER
	if decoded.IsIssuance {
		proofType = universerpc.ProofType_PROOF_TYPE_ISSUANCE
	}

	// Build the universe ID.
	uniID := &universerpc.ID{
		ProofType: proofType,
		Id: &universerpc.ID_AssetId{
			AssetId: decoded.AssetID[:],
		},
	}

	// If there's a group key, use it instead of asset ID.
	if len(decoded.GroupKey) > 0 {
		uniID.Id = &universerpc.ID_GroupKey{
			GroupKey: decoded.GroupKey,
		}
	}

	// Build the leaf key using outpoint and script key.
	leafKey := &universerpc.AssetKey{
		Outpoint: &universerpc.AssetKey_OpStr{
			OpStr: decoded.Outpoint.String(),
		},
		ScriptKey: &universerpc.AssetKey_ScriptKeyBytes{
			ScriptKeyBytes: decoded.ScriptKey[:],
		},
	}

	req := &universerpc.AssetProof{
		Key: &universerpc.UniverseKey{
			Id:      uniID,
			LeafKey: leafKey,
		},
		AssetLeaf: &universerpc.AssetLeaf{
			Proof: rawProof,
		},
	}

	resp, err := u.client.InsertProof(rpcCtx, req)
	if err != nil {
		return err
	}

	if resp == nil {
		return fmt.Errorf("invalid insert proof response")
	}

	return nil
}
