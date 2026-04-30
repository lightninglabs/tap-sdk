package tapsdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestIssuerCreateFungibleMintsGroupedAsset(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 2))
	record := issuerAssetRecord(
		issuerAssetID(1), groupRef, entities.AssetTypeFungible,
		"token", 100,
	)
	batchKey := testKey(t, 3)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{}, nil,
	).Once()
	client.On("MintAsset", ctx, mock.MatchedBy(
		func(req *entities.MintAssetRequest) bool {
			return req != nil &&
				req.ShortResponse &&
				req.Asset != nil &&
				req.Asset.AssetType == entities.AssetTypeFungible &&
				req.Asset.Name == "token" &&
				req.Asset.InitialSupply == 100 &&
				req.Asset.AllowIssuance
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("FinalizeBatch", ctx, mock.MatchedBy(
		func(req *entities.FinalizeBatchRequest) bool {
			return req != nil &&
				req.ShortResponse &&
				req.FeeRate == 250
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{record}, nil,
	).Once()

	asset, err := issuer.CreateFungible(ctx, entities.FungibleAssetSpec{
		Name:   "token",
		Amount: 100,
	}, WithMintFeeRate(250))
	require.NoError(t, err)
	require.Equal(t, groupRef, asset.AssetRef)
	require.Equal(t, entities.AssetTypeFungible, asset.Type)
	require.Equal(t, uint64(100), asset.Amount)

	client.AssertExpectations(t)
}

func TestIssuerIssueFungibleRejectsNFTCollection(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 4))
	record := issuerAssetRecord(
		issuerAssetID(2), collectionRef, entities.AssetTypeNFT,
		"item", 1,
	)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&collectionRef),
	).Return([]*entities.AssetRecord{record}, nil).Once()

	_, err := issuer.IssueFungible(ctx, collectionRef, 50)
	require.ErrorIs(t, err, ErrWrongAssetType)

	client.AssertExpectations(t)
}

func TestIssuerIssueFungibleMintsIssuance(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 8))
	first := issuerAssetRecord(
		issuerAssetID(8), groupRef, entities.AssetTypeFungible,
		"token", 100,
	)
	second := issuerAssetRecord(
		issuerAssetID(9), groupRef, entities.AssetTypeFungible,
		"token", 50,
	)
	batchKey := testKey(t, 9)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&groupRef),
	).Return([]*entities.AssetRecord{first}, nil).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&groupRef),
	).Return([]*entities.AssetRecord{first}, nil).Once()
	client.On("MintIssuance", ctx, mock.MatchedBy(
		func(req *entities.MintIssuanceRequest) bool {
			return req != nil &&
				req.ShortResponse &&
				req.Issuance != nil &&
				req.Issuance.AssetRef == groupRef &&
				req.Issuance.AssetType == entities.AssetTypeFungible &&
				req.Issuance.Name == "token" &&
				req.Issuance.Amount == 50
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("FinalizeBatch", ctx, mock.Anything).Return(
		&entities.MintingBatch{BatchKey: batchKey}, nil,
	).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&groupRef),
	).Return([]*entities.AssetRecord{first, second}, nil).Once()

	issuance, err := issuer.IssueFungible(ctx, groupRef, 50)
	require.NoError(t, err)
	require.Equal(t, groupRef, issuance.AssetRef)
	require.Equal(t, second.Genesis.IssuanceID, issuance.IssuanceID)
	require.Equal(t, uint64(50), issuance.Amount)

	client.AssertExpectations(t)
}

func TestIssuerCreateNFTMintsAsset(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	itemRef := entities.AssetRefFromAssetID(issuerAssetID(10))
	record := issuerAssetRecord(
		issuerAssetID(10), itemRef, entities.AssetTypeNFT,
		"nft", 1,
	)
	batchKey := testKey(t, 10)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{}, nil,
	).Once()
	client.On("MintAsset", ctx, mock.MatchedBy(
		func(req *entities.MintAssetRequest) bool {
			return req != nil &&
				req.ShortResponse &&
				req.Asset != nil &&
				req.Asset.AssetType == entities.AssetTypeNFT &&
				req.Asset.Name == "nft" &&
				req.Asset.InitialSupply == 1 &&
				!req.Asset.AllowIssuance
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("FinalizeBatch", ctx, mock.Anything).Return(
		&entities.MintingBatch{BatchKey: batchKey}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{record}, nil,
	).Once()

	asset, err := issuer.CreateNFT(ctx, entities.NFTSpec{Name: "nft"})
	require.NoError(t, err)
	require.Equal(t, itemRef, asset.AssetRef)
	require.Equal(t, entities.AssetTypeNFT, asset.Type)

	client.AssertExpectations(t)
}

func TestIssuerCreateCollectionReturnsResult(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 11))
	record := issuerAssetRecord(
		issuerAssetID(11), collectionRef, entities.AssetTypeNFT,
		"first", 1,
	)
	batchKey := testKey(t, 12)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{}, nil,
	).Once()
	client.On("MintAsset", ctx, mock.MatchedBy(
		func(req *entities.MintAssetRequest) bool {
			return req != nil &&
				req.ShortResponse &&
				req.Asset != nil &&
				req.Asset.AssetType == entities.AssetTypeNFT &&
				req.Asset.Name == "first" &&
				req.Asset.AllowIssuance
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("FinalizeBatch", ctx, mock.Anything).Return(
		&entities.MintingBatch{BatchKey: batchKey}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{record}, nil,
	).Once()

	result, err := issuer.CreateCollection(
		ctx, entities.NFTSpec{Name: "first"},
	)
	require.NoError(t, err)
	require.Equal(t, collectionRef, result.Collection.AssetRef)
	require.Equal(t, uint64(1), result.Collection.ItemCount)
	require.True(t, result.FirstItem.AssetRef.IsAssetIDRef())
	require.NotNil(t, result.FirstItem.CollectionRef)
	require.Equal(t, collectionRef, *result.FirstItem.CollectionRef)

	client.AssertExpectations(t)
}

func TestIssuerMintCollectionItemMintsCollectibleIssuance(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 5))
	first := issuerAssetRecord(
		issuerAssetID(3), collectionRef, entities.AssetTypeNFT,
		"first", 1,
	)
	second := issuerAssetRecord(
		issuerAssetID(4), collectionRef, entities.AssetTypeNFT,
		"second", 1,
	)
	batchKey := testKey(t, 6)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&collectionRef),
	).Return([]*entities.AssetRecord{first}, nil).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&collectionRef),
	).Return([]*entities.AssetRecord{first}, nil).Once()
	client.On("MintIssuance", ctx, mock.MatchedBy(
		func(req *entities.MintIssuanceRequest) bool {
			return req != nil &&
				req.ShortResponse &&
				req.Issuance != nil &&
				req.Issuance.AssetRef == collectionRef &&
				req.Issuance.AssetType == entities.AssetTypeNFT &&
				req.Issuance.Name == "second" &&
				req.Issuance.Amount == 1
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("FinalizeBatch", ctx, mock.MatchedBy(
		func(req *entities.FinalizeBatchRequest) bool {
			return req != nil && req.ShortResponse
		},
	)).Return(&entities.MintingBatch{BatchKey: batchKey}, nil).Once()
	client.On("ListAssetRecords", ctx,
		includeUnconfirmed(&collectionRef),
	).Return([]*entities.AssetRecord{first, second}, nil).Once()

	asset, err := issuer.MintCollectionItem(ctx, collectionRef,
		entities.NFTSpec{Name: "second"},
	)
	require.NoError(t, err)
	require.True(t, asset.AssetRef.IsAssetIDRef())
	require.NotNil(t, asset.CollectionRef)
	require.Equal(t, collectionRef, *asset.CollectionRef)
	require.Equal(t, "second", asset.Name)

	client.AssertExpectations(t)
}

func TestIssuerRejectsActiveBatch(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{
			{
				Batch: entities.MintingBatch{
					BatchKey: testKey(t, 7),
					State:    entities.BatchStatePending,
				},
			},
		}, nil,
	).Once()

	_, err := issuer.CreateNFT(ctx, entities.NFTSpec{Name: "blocked"})
	require.ErrorIs(t, err, ErrMintBatchActive)

	client.AssertExpectations(t)
}

func TestIssuerResolveTimeoutIsDistinct(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)

	batchKey := testKey(t, 13)

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{}, nil,
	).Once()
	client.On("MintAsset", ctx, mock.Anything).Return(
		&entities.MintingBatch{BatchKey: batchKey}, nil,
	).Once()
	client.On("FinalizeBatch", ctx, mock.Anything).Return(
		&entities.MintingBatch{BatchKey: batchKey}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{}, nil,
	).Maybe()

	_, err := issuer.CreateFungible(
		ctx, entities.FungibleAssetSpec{Name: "slow", Amount: 1},
		WithMintResolveTimeout(time.Nanosecond),
	)
	require.ErrorIs(t, err, ErrMintResolveTimeout)
	require.NotErrorIs(t, err, ErrMintResultNotFound)

	client.AssertExpectations(t)
}

func TestIssuerFinalizeFailureCancelsBatch(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{}
	issuer := NewIssuer(client)
	finalizeErr := errors.New("finalize failed")

	client.On("ListBatches", ctx, mock.Anything).Return(
		[]*entities.VerboseMintingBatch{}, nil,
	).Once()
	client.On("ListAssetRecords", ctx, includeUnconfirmed(nil)).Return(
		[]*entities.AssetRecord{}, nil,
	).Once()
	client.On("MintAsset", ctx, mock.Anything).Return(
		&entities.MintingBatch{BatchKey: testKey(t, 14)}, nil,
	).Once()
	client.On("FinalizeBatch", ctx, mock.Anything).Return(
		nil, finalizeErr,
	).Once()
	client.On("CancelBatch", mock.Anything).Return(
		&entities.CancelBatchResponse{}, nil,
	).Once()

	_, err := issuer.CreateNFT(ctx, entities.NFTSpec{Name: "cancel"})
	require.ErrorIs(t, err, finalizeErr)

	client.AssertExpectations(t)
}

func includeUnconfirmed(
	ref *entities.AssetRef) any {

	return mock.MatchedBy(func(req *entities.ListAssetsRequest) bool {
		if req == nil || !req.IncludeUnconfirmedMints {
			return false
		}
		if ref == nil {
			return req.AssetRef == nil
		}
		if req.AssetRef == nil {
			return false
		}

		return req.AssetRef.Equivalent(*ref)
	})
}

func issuerAssetRecord(id entities.AssetID, ref entities.AssetRef,
	assetType entities.AssetType, name string,
	amount uint64) *entities.AssetRecord {

	return &entities.AssetRecord{
		AssetRef: ref,
		Genesis: entities.IssuanceGenesis{
			Tag:        name,
			IssuanceID: id,
			Type:       assetType,
		},
		Amount: amount,
	}
}

func issuerAssetID(seed byte) entities.AssetID {
	var id entities.AssetID
	for i := range id {
		id[i] = seed + byte(i)
	}

	return id
}
