//go:build itest

package itest

import (
	"fmt"
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

// TestGroupedProofExportRequiresIssuanceEnumeration documents the current
// group proof surface: callers still need to enumerate concrete
// issuances/tranches when exporting complete proof files. A future ProofBundle
// API should hide this enumeration from application code.
func TestGroupedProofExportRequiresIssuanceEnumeration(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("proof-group-%s", transport))
		first, err := h.MintGroupedAsset(t, ctx, name, 1000)
		require.NoError(t, err)

		second, err := h.IssueMoreAndConfirm(
			t, ctx, first.Ref, name, 250,
		)
		require.NoError(t, err)

		expected := map[entities.AssetID]bool{
			first.Asset.Genesis.IssuanceID:  false,
			second.Asset.Genesis.IssuanceID: false,
		}

		group := h.RequireGroup(t, ctx, first.Ref)
		require.GreaterOrEqual(t, len(group.Assets), 2)

		walletAssets, err := h.AliceClient.ListAssets(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)

		assetsByIssuance := make(
			map[entities.AssetID]*entities.Asset, len(walletAssets),
		)
		for _, asset := range walletAssets {
			if asset == nil {
				continue
			}

			assetsByIssuance[asset.Genesis.IssuanceID] = asset
		}

		for _, groupedAsset := range group.Assets {
			if groupedAsset == nil {
				continue
			}

			if _, ok := expected[groupedAsset.IssuanceID]; !ok {
				continue
			}

			asset := assetsByIssuance[groupedAsset.IssuanceID]
			require.NotNil(t, asset)

			_, err := h.AliceClient.ExportProof(
				ctx, first.Ref, asset.ScriptKey.PubKey, nil,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(),
				"export proof requires an asset-ID ref")

			proof, err := h.AliceClient.ExportProof(
				ctx,
				entities.AssetRefFromAssetID(groupedAsset.IssuanceID),
				asset.ScriptKey.PubKey, nil,
			)
			require.NoError(t, err)
			require.NotEmpty(t, proof.RawProofFile)

			rawProofs, err := h.AliceClient.UnpackProofFile(
				ctx, proof.RawProofFile,
			)
			require.NoError(t, err)
			require.NotEmpty(t, rawProofs)

			decoded, err := h.AliceClient.DecodeProof(
				ctx, rawProofs[len(rawProofs)-1],
			)
			require.NoError(t, err)
			require.True(t, decoded.AssetRef.Equivalent(first.Ref))
			require.Equal(t, groupedAsset.IssuanceID,
				decoded.IssuanceID)

			expected[groupedAsset.IssuanceID] = true
		}

		for issuanceID, saw := range expected {
			require.Truef(t, saw,
				"missing exported proof for issuance %s",
				issuanceID)
		}
	})
}
