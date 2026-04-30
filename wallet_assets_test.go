package tapsdk

import (
	"context"
	"math"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

func TestListAssets_AggregatesGroupedFungibleIssuances(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 41))
	firstID := bundleAssetID(11)
	secondID := bundleAssetID(12)
	req := &entities.ListAssetsRequest{AssetRef: &groupRef}

	mc.On("ListAssetRecords", ctx, req).Return([]*entities.AssetRecord{
		bundleAsset(groupRef, firstID, testKey(t, 42), 100,
			entities.AssetTypeNormal),
		bundleAsset(groupRef, secondID, testKey(t, 43), 250,
			entities.AssetTypeNormal),
	}, nil).Once()

	assets, err := wallet.ListAssets(ctx, req)
	require.NoError(t, err)
	require.Len(t, assets, 1)

	asset := assets[0]
	require.Equal(t, groupRef, asset.AssetRef)
	require.Equal(t, entities.AssetTypeNormal, asset.Type)
	require.Equal(t, uint64(350), asset.Amount)
	require.Nil(t, asset.CollectionRef)

	mc.AssertExpectations(t)
}

func TestListAssets_SingleCollectible(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	assetID := bundleAssetID(13)
	assetRef := entities.AssetRefFromAssetID(assetID)
	req := &entities.ListAssetsRequest{AssetRef: &assetRef}

	mc.On("ListAssetRecords", ctx, req).Return([]*entities.AssetRecord{
		bundleAsset(assetRef, assetID, testKey(t, 44), 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	assets, err := wallet.ListAssets(ctx, req)
	require.NoError(t, err)
	require.Len(t, assets, 1)

	asset := assets[0]
	require.Equal(t, assetRef, asset.AssetRef)
	require.Equal(t, entities.AssetTypeCollectible, asset.Type)
	require.Equal(t, uint64(1), asset.Amount)
	require.Nil(t, asset.CollectionRef)

	mc.AssertExpectations(t)
}

func TestListAssets_ReturnsCollectionItemsAsNFTAssets(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 45))
	firstID := bundleAssetID(14)
	secondID := bundleAssetID(15)
	req := &entities.ListAssetsRequest{AssetRef: &collectionRef}

	mc.On("ListAssetRecords", ctx, req).Return([]*entities.AssetRecord{
		bundleAsset(collectionRef, firstID, testKey(t, 46), 1,
			entities.AssetTypeCollectible),
		bundleAsset(collectionRef, secondID, testKey(t, 47), 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	assets, err := wallet.ListAssets(ctx, req)
	require.NoError(t, err)
	require.Len(t, assets, 2)

	require.Equal(t, entities.AssetRefFromAssetID(firstID), assets[0].AssetRef)
	require.Equal(t, entities.AssetTypeCollectible, assets[0].Type)
	require.Equal(t, uint64(1), assets[0].Amount)
	require.NotNil(t, assets[0].CollectionRef)
	require.Equal(t, collectionRef, *assets[0].CollectionRef)
	require.Equal(t, entities.AssetRefFromAssetID(secondID), assets[1].AssetRef)
	require.NotNil(t, assets[1].CollectionRef)
	require.Equal(t, collectionRef, *assets[1].CollectionRef)

	mc.AssertExpectations(t)
}

func TestListCollections_AggregatesCollectionItems(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 54))
	req := &entities.ListCollectionsRequest{AssetRef: &collectionRef}

	mc.On("ListAssetRecords", ctx, &entities.ListAssetsRequest{
		AssetRef: &collectionRef,
	}).Return([]*entities.AssetRecord{
		bundleAsset(collectionRef, bundleAssetID(20), testKey(t, 55), 1,
			entities.AssetTypeCollectible),
		bundleAsset(collectionRef, bundleAssetID(21), testKey(t, 56), 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	collections, err := wallet.ListCollections(ctx, req)
	require.NoError(t, err)
	require.Len(t, collections, 1)
	require.Equal(t, collectionRef, collections[0].AssetRef)
	require.Equal(t, uint64(2), collections[0].ItemCount)

	mc.AssertExpectations(t)
}

func TestListAssets_DeduplicatesCollectibleRows(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	assetID := bundleAssetID(23)
	assetRef := entities.AssetRefFromAssetID(assetID)
	req := &entities.ListAssetsRequest{
		AssetRef:     &assetRef,
		IncludeSpent: true,
	}

	mc.On("ListAssetRecords", ctx, req).Return([]*entities.AssetRecord{
		bundleAsset(assetRef, assetID, testKey(t, 59), 1,
			entities.AssetTypeCollectible),
		bundleAsset(assetRef, assetID, testKey(t, 60), 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	assets, err := wallet.ListAssets(ctx, req)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	require.Equal(t, assetRef, assets[0].AssetRef)
	require.Equal(t, uint64(1), assets[0].Amount)

	mc.AssertExpectations(t)
}

func TestListAssets_SaturatesFungibleOverflow(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 61))
	req := &entities.ListAssetsRequest{AssetRef: &groupRef}

	mc.On("ListAssetRecords", ctx, req).Return([]*entities.AssetRecord{
		bundleAsset(groupRef, bundleAssetID(24), testKey(t, 62),
			math.MaxUint64-5, entities.AssetTypeNormal),
		bundleAsset(groupRef, bundleAssetID(25), testKey(t, 63),
			10, entities.AssetTypeNormal),
	}, nil).Once()

	assets, err := wallet.ListAssets(ctx, req)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	require.Equal(t, uint64(math.MaxUint64), assets[0].Amount)

	mc.AssertExpectations(t)
}

func TestListIssuances_SaturatesOverflow(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 64))
	issuanceID := bundleAssetID(26)
	req := &entities.ListIssuancesRequest{AssetRef: &groupRef}

	mc.On("ListAssetRecords", ctx, &entities.ListAssetsRequest{
		AssetRef: &groupRef,
	}).Return([]*entities.AssetRecord{
		bundleAsset(groupRef, issuanceID, testKey(t, 65),
			math.MaxUint64-5, entities.AssetTypeNormal),
		bundleAsset(groupRef, issuanceID, testKey(t, 66),
			10, entities.AssetTypeNormal),
	}, nil).Once()

	issuances, err := wallet.ListIssuances(ctx, req)
	require.NoError(t, err)
	require.Len(t, issuances, 1)
	require.Equal(t, issuanceID, issuances[0].IssuanceID)
	require.Equal(t, entities.AssetRefFromAssetID(issuanceID),
		issuances[0].Ref())
	require.Equal(t, uint64(math.MaxUint64), issuances[0].Amount)

	mc.AssertExpectations(t)
}

func TestListIssuances_FiltersCollectibles(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 48))
	nftRef := entities.AssetRefFromAssetID(bundleAssetID(16))

	mc.On("ListAssetRecords", ctx, (*entities.ListAssetsRequest)(nil)).Return(
		[]*entities.AssetRecord{
			bundleAsset(groupRef, bundleAssetID(17), testKey(t, 49),
				100, entities.AssetTypeNormal),
			bundleAsset(nftRef, bundleAssetID(16), testKey(t, 50),
				1, entities.AssetTypeCollectible),
		}, nil,
	).Once()

	issuances, err := wallet.ListIssuances(ctx, nil)
	require.NoError(t, err)
	require.Len(t, issuances, 1)
	require.Equal(t, groupRef, issuances[0].AssetRef)
	require.Equal(t, bundleAssetID(17), issuances[0].IssuanceID)
	require.Equal(t, uint64(100), issuances[0].Amount)

	mc.AssertExpectations(t)
}

func TestListCollectionItems_UsesConcreteItemRefs(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 51))
	firstID := bundleAssetID(18)
	secondID := bundleAssetID(19)
	req := &entities.ListCollectionItemsRequest{CollectionRef: &collectionRef}

	mc.On("ListAssetRecords", ctx, &entities.ListAssetsRequest{
		AssetRef: &collectionRef,
	}).Return([]*entities.AssetRecord{
		bundleAsset(collectionRef, firstID, testKey(t, 52), 1,
			entities.AssetTypeCollectible),
		bundleAsset(collectionRef, secondID, testKey(t, 53), 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	items, err := wallet.ListCollectionItems(ctx, req)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, entities.AssetRefFromAssetID(firstID),
		items[0].AssetRef)
	require.Equal(t, entities.AssetRefFromAssetID(secondID),
		items[1].AssetRef)
	require.NotNil(t, items[0].CollectionRef)
	require.Equal(t, collectionRef, *items[0].CollectionRef)

	mc.AssertExpectations(t)
}

func TestListCollectionItems_ByItemAssetRef(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 57))
	itemID := bundleAssetID(22)
	itemRef := entities.AssetRefFromAssetID(itemID)
	req := &entities.ListCollectionItemsRequest{AssetRef: &itemRef}

	mc.On("ListAssetRecords", ctx, &entities.ListAssetsRequest{
		AssetRef: &itemRef,
	}).Return([]*entities.AssetRecord{
		bundleAsset(collectionRef, itemID, testKey(t, 58), 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	items, err := wallet.ListCollectionItems(ctx, req)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, itemRef, items[0].AssetRef)
	require.NotNil(t, items[0].CollectionRef)
	require.Equal(t, collectionRef, *items[0].CollectionRef)

	mc.AssertExpectations(t)
}
