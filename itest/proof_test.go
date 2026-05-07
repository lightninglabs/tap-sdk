//go:build itest

package itest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestProofOperations verifies proof export, unpack, decode, and verify on
// a minted asset across every transport.
func TestProofOperations(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(t, ctx,
			&entities.MintAsset{
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

// TestWalletOwnershipProofSurface verifies application code can prove ownership
// with AssetRef only, without discovering issuance IDs, script keys, or
// outpoints first.
func TestWalletOwnershipProofSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		challenge := ownershipITestChallenge()

		grouped, err := h.CreateFungibleAndConfirm(
			t, ctx,
			uniqueEventLabel(fmt.Sprintf("own-group-%s", transport)),
			100,
		)
		require.NoError(t, err)
		_, err = h.IssueFungibleAndConfirm(t, ctx, grouped.Ref, 50)
		require.NoError(t, err)

		groupProofs, err := h.AliceWallet.ProveOwnership(
			ctx, grouped.Ref,
			tapsdk.WithOwnershipChallenge(challenge),
			tapsdk.WithOwnershipAmount(125),
		)
		require.NoError(t, err)
		require.Equal(t, grouped.Ref, groupProofs.AssetRef)
		require.Len(t, groupProofs.Proofs, 2)
		requireOwnershipTotalAtLeast(t, groupProofs, 125)
		for _, proof := range groupProofs.Proofs {
			require.Equal(t, grouped.Ref, proof.AssetRef)
			require.NotZero(t, proof.IssuanceID)
			require.NotZero(t, proof.ScriptKey)
			require.NotZero(t, proof.Outpoint)
			require.NotZero(t, proof.Amount)
			require.NotEmpty(t, proof.ProofWithWitness)
			requireOwnershipProofValid(
				t, ctx, h.AliceWallet, proof, challenge,
			)
		}

		badChallenge := append([]byte(nil), challenge...)
		badChallenge[0] ^= 0xff
		_, err = h.AliceWallet.VerifyOwnership(
			ctx, groupProofs.Proofs[0].ProofWithWitness,
			tapsdk.WithOwnershipChallenge(badChallenge),
		)
		require.Error(t, err)

		_, err = h.AliceWallet.ProveOwnership(
			ctx, grouped.Ref, tapsdk.WithOwnershipAmount(10_000),
		)
		require.ErrorIs(t, err, tapsdk.ErrInsufficientBalance)

		nft, err := h.CreateNFTAndConfirm(
			t, ctx,
			uniqueEventLabel(fmt.Sprintf("own-nft-%s", transport)),
		)
		require.NoError(t, err)
		nftProofs, err := h.AliceWallet.ProveOwnership(ctx, nft.Ref)
		require.NoError(t, err)
		require.Len(t, nftProofs.Proofs, 1)
		require.Equal(t, nft.Ref, nftProofs.Proofs[0].AssetRef)
		require.Equal(t, uint64(1), nftProofs.Proofs[0].Amount)
		requireOwnershipProofValid(
			t, ctx, h.AliceWallet, nftProofs.Proofs[0], nil,
		)

		collection, err := h.CreateCollectionAndConfirm(
			t, ctx,
			uniqueEventLabel(fmt.Sprintf("own-collection-%s", transport)),
		)
		require.NoError(t, err)
		secondItem, err := h.MintCollectionItemAndConfirm(
			t, ctx, collection.Ref,
			uniqueEventLabel(fmt.Sprintf("own-item-%s", transport)),
		)
		require.NoError(t, err)

		firstItemRef := entities.AssetRefFromAssetID(
			collection.Asset.Genesis.IssuanceID,
		)
		itemProofs, err := h.AliceWallet.ProveOwnership(
			ctx, secondItem.Ref,
		)
		require.NoError(t, err)
		require.Len(t, itemProofs.Proofs, 1)
		require.Equal(t, secondItem.Ref, itemProofs.Proofs[0].AssetRef)

		collectionProofs, err := h.AliceWallet.ProveOwnership(
			ctx, collection.Ref,
		)
		require.NoError(t, err)
		require.Len(t, collectionProofs.Proofs, 1)
		require.True(t, collectionProofs.Proofs[0].AssetRef.IsAssetIDRef())

		allCollectionProofs, err := h.AliceWallet.ProveOwnership(
			ctx, collection.Ref, tapsdk.WithAllOwnedCollectionItems(),
		)
		require.NoError(t, err)
		require.Len(t, allCollectionProofs.Proofs, 2)
		requireOwnershipProofForRef(t, allCollectionProofs, firstItemRef)
		requireOwnershipProofForRef(t, allCollectionProofs, secondItem.Ref)

		unknownRef := entities.AssetRefFromAssetID(
			ownershipITestAssetID(240),
		)
		_, err = h.AliceWallet.ProveOwnership(ctx, unknownRef)
		require.True(t, errors.Is(err, tapsdk.ErrAssetUnknown), err)
	})
}

// TestGroupedProofExportRequiresIssuanceEnumeration verifies that the
// AssetRef-first Wallet.ExportProof hides issuance/tranche enumeration while
// the low-level ExportProof API remains concrete-issuance oriented.
func TestGroupedProofExportRequiresIssuanceEnumeration(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("proof-group-%s", transport))
		first, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
		require.NoError(t, err)

		second, err := h.IssueFungibleAndConfirm(
			t, ctx, first.Ref, 250,
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

		minted, err := h.CreateNFTAndConfirm(
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
		minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
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

		proof := h.WaitForProofFile(
			t, ctx, h.AliceWallet,
			entities.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)

		h.EnableUniverseBootstrap(t, ctx)
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

func requireOwnershipProofValid(t testing.TB, ctx context.Context,
	wallet *tapsdk.Wallet, proof entities.OwnershipProof,
	challenge []byte) {

	t.Helper()

	opts := []tapsdk.OwnershipOption(nil)
	if len(challenge) > 0 {
		opts = append(opts, tapsdk.WithOwnershipChallenge(challenge))
	}
	verified, err := wallet.VerifyOwnership(
		ctx, proof.ProofWithWitness, opts...,
	)
	require.NoError(t, err)
	require.True(t, verified.Valid)
	require.Equal(t, proof.Outpoint, verified.Outpoint)
	require.NotZero(t, verified.BlockHash)
	require.NotZero(t, verified.BlockHeight)
}

func requireOwnershipTotalAtLeast(t testing.TB,
	proofs *entities.OwnershipProofSet, amount uint64) {

	t.Helper()

	var total uint64
	for _, proof := range proofs.Proofs {
		total += proof.Amount
	}
	require.GreaterOrEqual(t, total, amount)
}

func requireOwnershipProofForRef(t testing.TB,
	proofs *entities.OwnershipProofSet, ref entities.AssetRef) {

	t.Helper()

	for _, proof := range proofs.Proofs {
		if proof.AssetRef == ref {
			require.NotEmpty(t, proof.ProofWithWitness)
			return
		}
	}

	require.FailNowf(t, "ownership proof not found", "ref=%s", ref)
}

func ownershipITestChallenge() []byte {
	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(i + 1)
	}

	return challenge
}

func ownershipITestAssetID(seed byte) entities.AssetID {
	var id entities.AssetID
	for i := range id {
		id[i] = seed + byte(i)
	}

	return id
}
