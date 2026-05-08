//go:build itest

package itest

import (
	"fmt"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/stretchr/testify/require"
)

// TestWalletCollectionSurface verifies the high-level wallet surface treats a
// collection as a collection, not as an asset, while still allowing each NFT
// item to be addressed by its concrete asset-ID AssetRef.
func TestWalletCollectionSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		firstName := uniqueEventLabel(
			fmt.Sprintf("collection-one-%s", transport),
		)
		first, err := h.CreateCollectionAndConfirm(t, ctx, firstName)
		require.NoError(t, err)
		require.True(t, first.Ref.IsGroupRef())

		secondName := uniqueEventLabel(
			fmt.Sprintf("collection-two-%s", transport),
		)
		second, err := h.MintCollectionItemAndConfirm(
			t, ctx, first.Ref, secondName,
		)
		require.NoError(t, err)
		require.True(t, second.Ref.IsAssetIDRef())

		firstItemRef := tapsdk.AssetRefFromAssetID(
			first.Asset.Genesis.IssuanceID,
		)
		secondItemRef := second.Ref

		collections, err := h.AliceWallet.ListCollections(
			ctx, &tapsdk.ListCollectionsRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		require.Len(t, collections, 1)
		require.Equal(t, first.Ref, collections[0].AssetRef)
		require.Equal(t, uint64(2), collections[0].ItemCount)

		assets, err := h.AliceWallet.ListAssets(
			ctx, &tapsdk.ListAssetsRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		require.Len(t, assets, 2)
		requireNFTItem(t, assets, first.Ref, firstItemRef)
		requireNFTItem(t, assets, first.Ref, secondItemRef)

		items, err := h.AliceWallet.ListCollectionItems(
			ctx, &tapsdk.ListCollectionItemsRequest{
				CollectionRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		require.Len(t, items, 2)
		requireNFTItem(t, items, first.Ref, firstItemRef)
		requireNFTItem(t, items, first.Ref, secondItemRef)

		itemByRef, err := h.AliceWallet.ListCollectionItems(
			ctx, &tapsdk.ListCollectionItemsRequest{
				AssetRef: &firstItemRef,
			},
		)
		require.NoError(t, err)
		require.Len(t, itemByRef, 1)
		requireNFTItem(t, itemByRef, first.Ref, firstItemRef)

		issuances, err := h.AliceWallet.ListIssuances(
			ctx, &tapsdk.ListIssuancesRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		require.Empty(t, issuances)
	})
}

func requireNFTItem(t testing.TB, assets []*tapsdk.Asset,
	collectionRef, itemRef tapsdk.AssetRef) {

	t.Helper()

	for _, asset := range assets {
		if asset == nil || asset.AssetRef != itemRef {
			continue
		}

		require.Equal(t, tapsdk.AssetTypeNFT, asset.Type)
		require.Equal(t, uint64(1), asset.Amount)
		require.NotNil(t, asset.CollectionRef)
		require.Equal(t, collectionRef, *asset.CollectionRef)
		return
	}

	require.FailNowf(t, "NFT item not found", "item_ref=%s", itemRef)
}
