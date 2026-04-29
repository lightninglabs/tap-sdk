package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestExportProof_GroupRefEnumeratesIssuances(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 31))
	firstID := bundleAssetID(1)
	secondID := bundleAssetID(2)
	firstKey := testKey(t, 32)
	secondKey := testKey(t, 33)

	assets := []*entities.Asset{
		bundleAsset(groupRef, firstID, firstKey, 100,
			entities.AssetTypeNormal),
		bundleAsset(groupRef, secondID, secondKey, 250,
			entities.AssetTypeNormal),
	}

	mc.On("ListAssets", ctx, mock.MatchedBy(
		func(req *entities.ListAssetsRequest) bool {
			return req != nil && req.AssetRef != nil &&
				req.AssetRef.Equivalent(groupRef)
		}),
	).Return(assets, nil).Once()

	firstProof := []byte{0x01, 0x02}
	secondProof := []byte{0x03, 0x04}
	mc.On("ExportProof", ctx, entities.AssetRefFromAssetID(firstID),
		firstKey, (*entities.Outpoint)(nil)).Return(
		&entities.ProofFile{RawProofFile: firstProof}, nil,
	).Once()
	mc.On("ExportProof", ctx, entities.AssetRefFromAssetID(secondID),
		secondKey, (*entities.Outpoint)(nil)).Return(
		&entities.ProofFile{RawProofFile: secondProof}, nil,
	).Once()

	bundle, err := wallet.ExportProof(ctx, groupRef)
	require.NoError(t, err)
	require.Equal(t, groupRef, bundle.AssetRef)
	require.Len(t, bundle.Entries, 2)

	require.Equal(t, groupRef, bundle.Entries[0].AssetRef)
	require.Equal(t, firstID, bundle.Entries[0].IssuanceID)
	require.Equal(t, firstKey, bundle.Entries[0].ScriptKey)
	require.Equal(t, uint64(100), bundle.Entries[0].Amount)
	require.Equal(t, firstProof, bundle.Entries[0].ProofFile)

	require.Equal(t, groupRef, bundle.Entries[1].AssetRef)
	require.Equal(t, secondID, bundle.Entries[1].IssuanceID)
	require.Equal(t, secondKey, bundle.Entries[1].ScriptKey)
	require.Equal(t, uint64(250), bundle.Entries[1].Amount)
	require.Equal(t, secondProof, bundle.Entries[1].ProofFile)

	mc.AssertExpectations(t)
}

func TestExportProof_CollectibleEntryUsesAssetIDRef(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	assetID := bundleAssetID(3)
	assetRef := entities.AssetRefFromAssetID(assetID)
	scriptKey := testKey(t, 34)
	proofBytes := []byte{0x09}

	mc.On("ListAssets", ctx, mock.MatchedBy(
		func(req *entities.ListAssetsRequest) bool {
			return req != nil && req.AssetRef != nil &&
				req.AssetRef.Equivalent(assetRef)
		}),
	).Return([]*entities.Asset{
		bundleAsset(assetRef, assetID, scriptKey, 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	mc.On("ExportProof", ctx, assetRef, scriptKey,
		(*entities.Outpoint)(nil)).Return(
		&entities.ProofFile{RawProofFile: proofBytes}, nil,
	).Once()

	bundle, err := wallet.ExportProof(ctx, assetRef)
	require.NoError(t, err)
	require.Equal(t, assetRef, bundle.AssetRef)
	require.Len(t, bundle.Entries, 1)
	require.Equal(t, assetRef, bundle.Entries[0].AssetRef)
	require.Equal(t, assetID, bundle.Entries[0].IssuanceID)
	require.Equal(t, proofBytes, bundle.Entries[0].ProofFile)

	mc.AssertExpectations(t)
}

func TestExportProof_CollectionGroupEntriesUseAssetIDRefs(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 37))
	firstID := bundleAssetID(7)
	secondID := bundleAssetID(8)
	firstKey := testKey(t, 38)
	secondKey := testKey(t, 39)

	mc.On("ListAssets", ctx, mock.MatchedBy(
		func(req *entities.ListAssetsRequest) bool {
			return req != nil && req.AssetRef != nil &&
				req.AssetRef.Equivalent(groupRef)
		}),
	).Return([]*entities.Asset{
		bundleAsset(groupRef, firstID, firstKey, 1,
			entities.AssetTypeCollectible),
		bundleAsset(groupRef, secondID, secondKey, 1,
			entities.AssetTypeCollectible),
	}, nil).Once()

	firstProof := []byte{0x01}
	secondProof := []byte{0x02}
	mc.On("ExportProof", ctx, entities.AssetRefFromAssetID(firstID),
		firstKey, (*entities.Outpoint)(nil)).Return(
		&entities.ProofFile{RawProofFile: firstProof}, nil,
	).Once()
	mc.On("ExportProof", ctx, entities.AssetRefFromAssetID(secondID),
		secondKey, (*entities.Outpoint)(nil)).Return(
		&entities.ProofFile{RawProofFile: secondProof}, nil,
	).Once()

	bundle, err := wallet.ExportProof(ctx, groupRef)
	require.NoError(t, err)
	require.Equal(t, groupRef, bundle.AssetRef)
	require.Len(t, bundle.Entries, 2)

	require.Equal(t, entities.AssetRefFromAssetID(firstID),
		bundle.Entries[0].AssetRef)
	require.Equal(t, firstID, bundle.Entries[0].IssuanceID)
	require.Equal(t, firstProof, bundle.Entries[0].ProofFile)

	require.Equal(t, entities.AssetRefFromAssetID(secondID),
		bundle.Entries[1].AssetRef)
	require.Equal(t, secondID, bundle.Entries[1].IssuanceID)
	require.Equal(t, secondProof, bundle.Entries[1].ProofFile)

	mc.AssertExpectations(t)
}

func TestExportProof_UnknownAsset(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	ref := entities.AssetRefFromAssetID(bundleAssetID(4))
	mc.On("ListAssets", ctx, mock.Anything).Return(
		[]*entities.Asset{}, nil,
	).Once()

	_, err := wallet.ExportProof(ctx, ref)
	require.ErrorIs(t, err, ErrAssetUnknown)

	mc.AssertExpectations(t)
}

func TestImportProof_ImportsEntries(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	firstRawProof := []byte{0x01}
	secondRawProof := []byte{0x02}
	firstProofFile := []byte{0x0a}
	secondProofFile := []byte{0x0b}
	firstID := bundleAssetID(5)
	secondID := bundleAssetID(6)
	firstKey := testKey(t, 35)
	secondKey := testKey(t, 36)
	firstOutpoint := entities.Outpoint{Index: 1}
	secondOutpoint := entities.Outpoint{Index: 2}
	firstRef := entities.AssetRefFromAssetID(firstID)
	secondRef := entities.AssetRefFromAssetID(secondID)

	firstDecoded := &entities.DecodedProof{
		AssetRef:   firstRef,
		IssuanceID: firstID,
		ScriptKey:  firstKey,
		Outpoint:   firstOutpoint,
	}
	secondDecoded := &entities.DecodedProof{
		AssetRef:   secondRef,
		IssuanceID: secondID,
		ScriptKey:  secondKey,
		Outpoint:   secondOutpoint,
	}

	mc.On("UnpackProofFile", ctx, firstProofFile).Return(
		[][]byte{firstRawProof}, nil,
	).Once()
	mc.On("DecodeProof", ctx, firstRawProof).Return(
		firstDecoded, nil,
	).Once()
	mc.On("InsertProof", ctx, firstRawProof, firstDecoded).Return(nil).Once()
	mc.On("RegisterTransfer", ctx, firstRef, firstKey,
		firstOutpoint).Return(
		&entities.RegisteredAsset{
			AssetRef:   firstRef,
			IssuanceID: firstID,
			ScriptKey:  firstKey,
			Outpoint:   firstOutpoint,
		}, nil,
	).Once()

	mc.On("UnpackProofFile", ctx, secondProofFile).Return(
		[][]byte{secondRawProof}, nil,
	).Once()
	mc.On("DecodeProof", ctx, secondRawProof).Return(
		secondDecoded, nil,
	).Once()
	mc.On("InsertProof", ctx, secondRawProof, secondDecoded).Return(nil).
		Once()
	mc.On("RegisterTransfer", ctx, secondRef, secondKey,
		secondOutpoint).Return(
		&entities.RegisteredAsset{
			AssetRef:   secondRef,
			IssuanceID: secondID,
			ScriptKey:  secondKey,
			Outpoint:   secondOutpoint,
		}, nil,
	).Once()

	registered, err := wallet.ImportProof(ctx, &entities.ProofBundle{
		AssetRef: firstRef,
		Entries: []entities.ProofEntry{
			{ProofFile: firstProofFile},
			{ProofFile: secondProofFile},
		},
	})
	require.NoError(t, err)
	require.Len(t, registered, 2)
	require.Equal(t, firstID, registered[0].IssuanceID)
	require.Equal(t, secondID, registered[1].IssuanceID)

	mc.AssertExpectations(t)
}

func TestImportProof_RejectsIncompleteBundle(t *testing.T) {
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	_, err := wallet.ImportProof(context.Background(), nil)
	require.ErrorIs(t, err, ErrIncompleteProofBundle)

	_, err = wallet.ImportProof(context.Background(),
		&entities.ProofBundle{})
	require.ErrorIs(t, err, ErrIncompleteProofBundle)

	_, err = wallet.ImportProof(context.Background(),
		&entities.ProofBundle{
			Entries: []entities.ProofEntry{{}},
		})
	require.ErrorIs(t, err, ErrIncompleteProofBundle)

	_, err = wallet.ImportProof(context.Background(),
		&entities.ProofBundle{
			Entries: []entities.ProofEntry{
				{ProofFile: []byte{0x01}},
				{},
			},
		})
	require.ErrorIs(t, err, ErrIncompleteProofBundle)

	mc.AssertExpectations(t)
}

func bundleAsset(ref entities.AssetRef, issuanceID entities.AssetID,
	scriptKey entities.PubKey, amount uint64,
	assetType entities.AssetType) *entities.Asset {

	return &entities.Asset{
		AssetRef: ref,
		Genesis: entities.AssetGenesis{
			IssuanceID: issuanceID,
			Type:       assetType,
		},
		Amount: amount,
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKey,
		},
	}
}

func bundleAssetID(seed byte) entities.AssetID {
	var id entities.AssetID
	for idx := range id {
		id[idx] = seed + byte(idx)
	}

	return id
}
