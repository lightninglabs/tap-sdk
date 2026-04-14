//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestProofOperations verifies proof export, unpack, and decode operations on
// a minted asset.
func TestProofOperations(t *testing.T) {
	h, ctx := newFundedHarness(t)

	minted, err := h.MintAssetAndConfirm(t, ctx, &entities.CreateAsset{
		AssetType:     entities.AssetTypeNormal,
		Name:          "proof-token",
		InitialSupply: 100,
	})
	require.NoError(t, err)
	require.True(t, minted.Asset.AssetRef.IsAssetIDRef())

	proof, err := h.AliceClient.ExportProof(ctx,
		minted.Asset.Genesis.IssuanceID, minted.Asset.ScriptKey.PubKey, nil)
	require.NoError(t, err)
	require.NotEmpty(t, proof.RawProofFile,
		"proof file should not be empty")
	t.Logf("Exported proof: %d bytes", len(proof.RawProofFile))

	rawProofs, err := h.AliceClient.UnpackProofFile(ctx,
		proof.RawProofFile)
	require.NoError(t, err)
	require.NotEmpty(t, rawProofs,
		"proof file should contain at least one proof")
	t.Logf("Unpacked %d proofs", len(rawProofs))

	lastProof := rawProofs[len(rawProofs)-1]
	decoded, err := h.AliceClient.DecodeProof(ctx, lastProof)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.Equal(t, minted.Asset.Genesis.IssuanceID, decoded.IssuanceID)
	t.Logf("Decoded proof: asset=%s, outpoint=%s",
		decoded.AssetRef, decoded.Outpoint)
}
