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

// TestGroupedProofExportRequiresIssuanceEnumeration verifies that the
// AssetRef-first ProofBundle API hides issuance/tranche enumeration while the
// low-level ExportProof API remains concrete-issuance oriented.
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

		bundle, err := h.AliceWallet.ExportProof(ctx, first.Ref)
		require.NoError(t, err)
		require.Equal(t, first.Ref, bundle.AssetRef)
		require.GreaterOrEqual(t, len(bundle.Entries), 2)

		expected := map[entities.AssetID]bool{
			first.Asset.Genesis.IssuanceID:  false,
			second.Asset.Genesis.IssuanceID: false,
		}
		for _, entry := range bundle.Entries {
			if _, ok := expected[entry.IssuanceID]; !ok {
				continue
			}

			require.Equal(t, first.Ref, entry.AssetRef)
			require.NotEmpty(t, entry.ProofFile)
			require.NotZero(t, entry.Amount)

			verified, err := h.AliceClient.VerifyProof(
				ctx, entry.ProofFile,
			)
			require.NoError(t, err)
			require.True(t, verified.Valid)

			expected[entry.IssuanceID] = true
		}
		for issuanceID, saw := range expected {
			require.Truef(t, saw,
				"bundle missing proof for issuance %s",
				issuanceID)
		}

		group := h.RequireGroup(t, ctx, first.Ref)
		require.GreaterOrEqual(t, len(group.Members), 2)

		expected = map[entities.AssetID]bool{
			first.Asset.Genesis.IssuanceID:  false,
			second.Asset.Genesis.IssuanceID: false,
		}

		walletAssets, err := h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)

		assetsByIssuance := make(
			map[entities.AssetID]*entities.AssetRecord, len(walletAssets),
		)
		for _, asset := range walletAssets {
			if asset == nil {
				continue
			}

			assetsByIssuance[asset.Genesis.IssuanceID] = asset
		}

		for _, groupedAsset := range group.Members {
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

// TestCollectibleProofExport verifies that a specific NFT/collectible
// asset-ID ref exports as a single-entry proof bundle.
func TestCollectibleProofExport(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintCollectibleAsset(
			t, ctx,
			uniqueEventLabel(fmt.Sprintf("proof-nft-%s", transport)),
		)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsAssetIDRef())

		bundle, err := h.AliceWallet.ExportProof(ctx, minted.Ref)
		require.NoError(t, err)
		require.Equal(t, minted.Ref, bundle.AssetRef)
		require.Len(t, bundle.Entries, 1)

		entry := bundle.Entries[0]
		require.Equal(t, minted.Ref, entry.AssetRef)
		require.Equal(t, minted.Asset.Genesis.IssuanceID,
			entry.IssuanceID)
		require.Equal(t, uint64(1), entry.Amount)
		require.NotEmpty(t, entry.ProofFile)

		verified, err := h.AliceClient.VerifyProof(ctx, entry.ProofFile)
		require.NoError(t, err)
		require.True(t, verified.Valid)
	})
}

// TestProofImportInteractive verifies that receivers can import an
// interactive transfer by passing a ProofBundle instead of manually registering
// issuance/tranche details.
func TestProofImportInteractive(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(
			fmt.Sprintf("bundle-import-%s", transport),
		)
		minted, err := h.MintGroupedAsset(t, ctx, name, 1000)
		require.NoError(t, err)

		receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
		require.NoError(t, err)

		const amount = uint64(44)
		transfer, err := h.AliceWallet.NewInteractiveTxBuilder().
			SetAsset(minted.Ref, amount).
			SetReceiverKeys(*receiverKeys).
			Execute(ctx)
		require.NoError(t, err)

		output := transferOutputForScriptKey(
			t, transfer, receiverKeys.ScriptKey.PubKey,
		)
		require.NotZero(t, output.IssuanceID)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		proof, err := h.AliceWallet.ExportProofFile(
			ctx, entities.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)
		require.NoError(t, err)

		registered, err := h.BobWallet.ImportProof(
			ctx, &entities.ProofBundle{
				AssetRef: minted.Ref,
				Entries: []entities.ProofEntry{{
					AssetRef:   minted.Ref,
					IssuanceID: output.IssuanceID,
					ScriptKey:  output.ScriptKey,
					Outpoint:   &output.AnchorOutpoint,
					Amount:     output.Amount,
					ProofFile:  proof.RawProofFile,
				}},
			},
		)
		require.NoError(t, err)
		require.Len(t, registered, 1)
		require.Equal(t, output.IssuanceID, registered[0].IssuanceID)

		balance := h.WaitForBalance(t, ctx, h.BobWallet,
			minted.Ref, amount, balanceTimeoutFor(minted.Ref))
		require.Equal(t, amount, balance)
	})
}
