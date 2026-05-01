//go:build itest

package itest

import (
	"errors"
	"fmt"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestIssuerFungibleSurface verifies the high-level issuer creates grouped
// fungible assets and additional issuances without exposing tapd tranche
// mechanics to the caller.
func TestIssuerFungibleSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)
		issuer := h.AliceWallet.NewIssuer()

		name := uniqueEventLabel(
			fmt.Sprintf("issuer-token-%s", transport),
		)
		asset, err := issuer.CreateFungible(ctx,
			entities.FungibleAssetSpec{
				Name:   name,
				Amount: 1000,
			},
		)
		require.NoError(t, err)
		require.Equal(t, name, asset.Name)
		require.Equal(t, entities.AssetTypeFungible, asset.Type)
		require.Equal(t, uint64(1000), asset.Amount)
		require.True(t, asset.AssetRef.IsGroupRef())

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForNoActiveMintBatch(
			t, ctx, h.AliceClient, defaultWaitTimeout,
		)

		balance := h.WaitForBalance(
			t, ctx, h.AliceWallet, asset.AssetRef, 1000,
			balanceTimeoutFor(asset.AssetRef),
		)
		require.Equal(t, uint64(1000), balance)

		_, err = issuer.MintCollectionItem(ctx, asset.AssetRef,
			entities.NFTSpec{Name: name + "-wrong-kind"},
		)
		require.Error(t, err)
		require.True(t, errors.Is(err, tapsdk.ErrWrongAssetType))

		issuance, err := issuer.IssueFungible(
			ctx, asset.AssetRef, 250,
		)
		require.NoError(t, err)
		require.Equal(t, asset.AssetRef, issuance.AssetRef)
		require.Equal(t, uint64(250), issuance.Amount)
		require.NotZero(t, issuance.IssuanceID)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForNoActiveMintBatch(
			t, ctx, h.AliceClient, defaultWaitTimeout,
		)

		balance = h.WaitForBalance(
			t, ctx, h.AliceWallet, asset.AssetRef, 1250,
			balanceTimeoutFor(asset.AssetRef),
		)
		require.Equal(t, uint64(1250), balance)

		assets, err := h.AliceWallet.ListAssets(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &asset.AssetRef,
			},
		)
		require.NoError(t, err)
		require.Len(t, assets, 1)
		require.Equal(t, asset.AssetRef, assets[0].AssetRef)
		require.Equal(t, entities.AssetTypeFungible, assets[0].Type)
		require.GreaterOrEqual(t, assets[0].Amount, uint64(1250))

		issuances, err := h.AliceWallet.ListIssuances(ctx,
			&entities.ListIssuancesRequest{
				AssetRef: &asset.AssetRef,
			},
		)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(issuances), 2)
	})
}

// TestIssuerNFTCollectionSurface verifies standalone NFTs and NFT collections
// are separate high-level entities.
func TestIssuerNFTCollectionSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)
		issuer := h.AliceWallet.NewIssuer()

		nftName := uniqueEventLabel(
			fmt.Sprintf("issuer-nft-%s", transport),
		)
		nft, err := issuer.CreateNFT(ctx, entities.NFTSpec{
			Name: nftName,
		})
		require.NoError(t, err)
		require.Equal(t, nftName, nft.Name)
		require.Equal(t, entities.AssetTypeNFT, nft.Type)
		require.True(t, nft.AssetRef.IsAssetIDRef())
		require.Nil(t, nft.CollectionRef)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForNoActiveMintBatch(
			t, ctx, h.AliceClient, defaultWaitTimeout,
		)
		h.WaitForBalance(
			t, ctx, h.AliceWallet, nft.AssetRef, 1,
			balanceTimeoutFor(nft.AssetRef),
		)

		firstName := uniqueEventLabel(
			fmt.Sprintf("issuer-collection-one-%s", transport),
		)
		created, err := issuer.CreateCollection(
			ctx, entities.NFTSpec{Name: firstName},
		)
		require.NoError(t, err)
		collection := created.Collection
		firstItem := created.FirstItem
		require.True(t, collection.AssetRef.IsGroupRef())
		require.Equal(t, uint64(1), collection.ItemCount)
		require.True(t, firstItem.AssetRef.IsAssetIDRef())
		require.NotNil(t, firstItem.CollectionRef)
		require.Equal(t, collection.AssetRef, *firstItem.CollectionRef)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForNoActiveMintBatch(
			t, ctx, h.AliceClient, defaultWaitTimeout,
		)
		h.WaitForBalance(
			t, ctx, h.AliceWallet, firstItem.AssetRef, 1,
			balanceTimeoutFor(firstItem.AssetRef),
		)

		_, err = issuer.IssueFungible(ctx, collection.AssetRef, 10)
		require.Error(t, err)
		require.True(t, errors.Is(err, tapsdk.ErrWrongAssetType))

		secondName := uniqueEventLabel(
			fmt.Sprintf("issuer-collection-two-%s", transport),
		)
		secondItem, err := issuer.MintCollectionItem(
			ctx, collection.AssetRef,
			entities.NFTSpec{Name: secondName},
		)
		require.NoError(t, err)
		require.Equal(t, secondName, secondItem.Name)
		require.True(t, secondItem.AssetRef.IsAssetIDRef())
		require.NotNil(t, secondItem.CollectionRef)
		require.Equal(t, collection.AssetRef, *secondItem.CollectionRef)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForNoActiveMintBatch(
			t, ctx, h.AliceClient, defaultWaitTimeout,
		)
		h.WaitForBalance(
			t, ctx, h.AliceWallet, secondItem.AssetRef, 1,
			balanceTimeoutFor(secondItem.AssetRef),
		)

		collections, err := h.AliceWallet.ListCollections(
			ctx, &entities.ListCollectionsRequest{
				AssetRef: &collection.AssetRef,
			},
		)
		require.NoError(t, err)
		require.Len(t, collections, 1)
		require.Equal(t, collection.AssetRef, collections[0].AssetRef)
		require.Equal(t, uint64(2), collections[0].ItemCount)

		items, err := h.AliceWallet.ListCollectionItems(
			ctx, &entities.ListCollectionItemsRequest{
				CollectionRef: &collection.AssetRef,
			},
		)
		require.NoError(t, err)
		require.Len(t, items, 2)
		requireNFTItem(t, items, collection.AssetRef, firstItem.AssetRef)
		requireNFTItem(t, items, collection.AssetRef, secondItem.AssetRef)
	})
}

// TestIssuerPendingBatchConflict verifies the high-level issuer refuses to
// attach itself to an existing low-level mint batch.
func TestIssuerPendingBatchConflict(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(
			fmt.Sprintf("issuer-pending-%s", transport),
		)
		_, err := h.AliceClient.MintAsset(ctx,
			&entities.MintAssetRequest{
				Asset: &entities.MintAsset{
					AssetType:     entities.AssetTypeFungible,
					Name:          name,
					InitialSupply: 1,
				},
				ShortResponse: true,
			},
		)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = h.AliceClient.CancelBatch(ctx)
		})

		_, err = h.AliceWallet.NewIssuer().CreateNFT(
			ctx, entities.NFTSpec{Name: name + "-blocked"},
		)
		require.Error(t, err)
		require.True(t, errors.Is(err, tapsdk.ErrMintBatchActive))

		_, err = h.AliceClient.CancelBatch(ctx)
		require.NoError(t, err)
	})
}
