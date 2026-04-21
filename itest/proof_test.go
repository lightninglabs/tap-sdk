//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestProofOperations verifies proof export, unpack, decode, and verify on
// a minted asset across every transport.
func TestProofOperations(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(t, ctx,
			&entities.CreateAsset{
				AssetType:     entities.AssetTypeNormal,
				Name:          "proof-token",
				InitialSupply: 100,
			},
		)
		require.NoError(t, err)
		require.True(t, minted.Asset.AssetRef.IsAssetIDRef())

		proof, err := h.AliceClient.ExportProof(ctx,
			entities.AssetRefFromAssetID(
				minted.Asset.Genesis.IssuanceID,
			),
			minted.Asset.ScriptKey.PubKey, nil)
		require.NoError(t, err)
		require.NotEmpty(t, proof.RawProofFile,
			"proof file should not be empty")

		rawProofs, err := h.AliceClient.UnpackProofFile(ctx,
			proof.RawProofFile)
		require.NoError(t, err)
		require.NotEmpty(t, rawProofs,
			"proof file should contain at least one proof")

		lastProof := rawProofs[len(rawProofs)-1]
		decoded, err := h.AliceClient.DecodeProof(ctx, lastProof)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, minted.Asset.Genesis.IssuanceID,
			decoded.IssuanceID)

		// VerifyProof must also accept the exported file.
		verified, err := h.AliceClient.VerifyProof(
			ctx, proof.RawProofFile,
		)
		require.NoError(t, err)
		require.True(t, verified.Valid)
	})
}
