package tapsdk

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProveOwnership_GroupedFungibleAmount(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)

	groupRef := entities.AssetRefFromGroupKey(testKey(t, 21))
	firstID := ownershipAssetID(1)
	secondID := ownershipAssetID(2)
	firstKey := testKey(t, 31)
	secondKey := testKey(t, 32)
	firstOutpoint := ownershipOutpoint(11)
	secondOutpoint := ownershipOutpoint(12)
	challenge := ownershipChallenge()

	mc.On("ListUtxos", ctx, mock.MatchedBy(
		func(req *entities.ListUtxosRequest) bool {
			return req != nil && req.IncludeLeased
		}),
	).Return(map[string]*entities.ManagedUtxo{
		"second": {
			OutPoint: secondOutpoint,
			Assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					groupRef, secondID, entities.AssetTypeFungible,
					75, secondKey,
				),
			},
		},
		"first": {
			OutPoint: firstOutpoint,
			Assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					groupRef, firstID, entities.AssetTypeFungible,
					50, firstKey,
				),
			},
		},
	}, nil).Once()

	expectOwnershipProof(
		mc, ctx, firstID, firstKey, firstOutpoint, challenge,
		[]byte("first-proof"),
	)
	expectOwnershipProof(
		mc, ctx, secondID, secondKey, secondOutpoint, challenge,
		[]byte("second-proof"),
	)

	proofs, err := w.ProveOwnership(
		ctx, groupRef, WithOwnershipChallenge(challenge),
		WithOwnershipAmount(100),
	)
	require.NoError(t, err)
	require.Equal(t, groupRef, proofs.AssetRef)
	require.Len(t, proofs.Proofs, 2)

	require.Equal(t, groupRef, proofs.Proofs[0].AssetRef)
	require.Equal(t, firstID, proofs.Proofs[0].IssuanceID)
	require.Equal(t, firstKey, proofs.Proofs[0].ScriptKey)
	require.Equal(t, firstOutpoint, proofs.Proofs[0].Outpoint)
	require.Equal(t, uint64(50), proofs.Proofs[0].Amount)
	require.Equal(t, []byte("first-proof"),
		proofs.Proofs[0].ProofWithWitness)

	require.Equal(t, secondID, proofs.Proofs[1].IssuanceID)
	require.Equal(t, uint64(75), proofs.Proofs[1].Amount)
	mc.AssertExpectations(t)
}

func TestProveOwnership_InvalidChallenge(t *testing.T) {
	w := NewWallet(new(mockClient), entities.NetworkRegtest)
	ref := entities.AssetRefFromGroupKey(testKey(t, 37))

	tests := []struct {
		name      string
		challenge []byte
	}{
		{
			name:      "explicit nil",
			challenge: nil,
		},
		{
			name:      "short",
			challenge: []byte{1, 2, 3},
		},
		{
			name:      "all zero",
			challenge: make([]byte, 32),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.ProveOwnership(
				context.Background(), ref,
				WithOwnershipChallenge(tc.challenge),
			)
			require.ErrorIs(t, err, ErrInvalidChallenge)
		})
	}
}

func TestProveOwnership_CollectionRef(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 41))
	firstID := ownershipAssetID(41)
	secondID := ownershipAssetID(42)
	firstKey := testKey(t, 51)
	secondKey := testKey(t, 52)
	firstOutpoint := ownershipOutpoint(21)
	secondOutpoint := ownershipOutpoint(22)

	utxos := map[string]*entities.ManagedUtxo{
		"first": {
			OutPoint: firstOutpoint,
			Assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					collectionRef, firstID, entities.AssetTypeNFT,
					1, firstKey,
				),
			},
		},
		"second": {
			OutPoint: secondOutpoint,
			Assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					collectionRef, secondID, entities.AssetTypeNFT,
					1, secondKey,
				),
			},
		},
	}

	mc.On("ListUtxos", ctx, mock.Anything).Return(utxos, nil).Once()
	expectOwnershipProof(
		mc, ctx, firstID, firstKey, firstOutpoint, nil,
		[]byte("first-item"),
	)

	proofs, err := w.ProveOwnership(ctx, collectionRef)
	require.NoError(t, err)
	require.Len(t, proofs.Proofs, 1)
	require.Equal(t, entities.AssetRefFromAssetID(firstID),
		proofs.Proofs[0].AssetRef)

	mc.On("ListUtxos", ctx, mock.Anything).Return(utxos, nil).Once()
	expectOwnershipProof(
		mc, ctx, firstID, firstKey, firstOutpoint, nil,
		[]byte("first-item-again"),
	)
	expectOwnershipProof(
		mc, ctx, secondID, secondKey, secondOutpoint, nil,
		[]byte("second-item"),
	)

	proofs, err = w.ProveOwnership(
		ctx, collectionRef, WithAllOwnedCollectionItems(),
	)
	require.NoError(t, err)
	require.Len(t, proofs.Proofs, 2)
	require.Equal(t, entities.AssetRefFromAssetID(firstID),
		proofs.Proofs[0].AssetRef)
	require.Equal(t, entities.AssetRefFromAssetID(secondID),
		proofs.Proofs[1].AssetRef)
	mc.AssertExpectations(t)
}

func TestProveOwnership_UnknownAndInsufficient(t *testing.T) {
	ctx := context.Background()
	ref := entities.AssetRefFromAssetID(ownershipAssetID(99))

	t.Run("unknown", func(t *testing.T) {
		mc := new(mockClient)
		w := NewWallet(mc, entities.NetworkRegtest)

		mc.On("ListUtxos", ctx, mock.Anything).Return(
			map[string]*entities.ManagedUtxo{}, nil,
		).Once()
		mc.On("ListAssetRecords", ctx, mock.MatchedBy(
			func(req *entities.ListAssetsRequest) bool {
				return req != nil && req.AssetRef != nil &&
					req.AssetRef.Equivalent(ref) &&
					req.IncludeSpent && !req.IncludeLeased &&
					req.ScriptKeyType != nil &&
					req.ScriptKeyType.AllTypes
			}),
		).Return([]*entities.AssetRecord{}, nil).Once()

		_, err := w.ProveOwnership(ctx, ref)
		require.ErrorIs(t, err, ErrAssetUnknown)
		mc.AssertExpectations(t)
	})

	t.Run("insufficient", func(t *testing.T) {
		mc := new(mockClient)
		w := NewWallet(mc, entities.NetworkRegtest)

		id, ok := ref.AssetID()
		require.True(t, ok)
		scriptKey := testKey(t, 61)
		outpoint := ownershipOutpoint(31)
		mc.On("ListUtxos", ctx, mock.Anything).Return(
			map[string]*entities.ManagedUtxo{
				"owned": {
					OutPoint: outpoint,
					Assets: []*entities.AssetRecord{
						ownershipAssetRecord(
							ref, id, entities.AssetTypeFungible,
							10, scriptKey,
						),
					},
				},
			}, nil,
		).Once()

		_, err := w.ProveOwnership(
			ctx, ref, WithOwnershipAmount(11),
		)
		require.ErrorIs(t, err, ErrInsufficientBalance)
		mc.AssertExpectations(t)
	})
}

func TestProveOwnership_SelectionErrors(t *testing.T) {
	ctx := context.Background()
	groupRef := entities.AssetRefFromGroupKey(testKey(t, 81))
	fungibleID := ownershipAssetID(81)
	nftID := ownershipAssetID(82)

	tests := []struct {
		name    string
		ref     entities.AssetRef
		opts    []OwnershipOption
		assets  []*entities.AssetRecord
		wantErr error
	}{
		{
			name: "fungible amount required",
			ref:  groupRef,
			assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					groupRef, fungibleID,
					entities.AssetTypeFungible, 10,
					testKey(t, 82),
				),
			},
			wantErr: ErrOwnershipAmountRequired,
		},
		{
			name: "mixed asset types",
			ref:  groupRef,
			opts: []OwnershipOption{WithOwnershipAmount(1)},
			assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					groupRef, fungibleID,
					entities.AssetTypeFungible, 10,
					testKey(t, 83),
				),
				ownershipAssetRecord(
					groupRef, nftID, entities.AssetTypeNFT, 1,
					testKey(t, 84),
				),
			},
			wantErr: ErrWrongAssetType,
		},
		{
			name: "all collection items on fungible",
			ref:  groupRef,
			opts: []OwnershipOption{WithAllOwnedCollectionItems()},
			assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					groupRef, fungibleID,
					entities.AssetTypeFungible, 10,
					testKey(t, 85),
				),
			},
			wantErr: ErrWrongAssetType,
		},
		{
			name: "nft amount above one",
			ref:  entities.AssetRefFromAssetID(nftID),
			opts: []OwnershipOption{WithOwnershipAmount(2)},
			assets: []*entities.AssetRecord{
				ownershipAssetRecord(
					entities.AssetRefFromAssetID(nftID), nftID,
					entities.AssetTypeNFT, 1, testKey(t, 86),
				),
			},
			wantErr: ErrInsufficientBalance,
		},
	}

	for idx, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mc := new(mockClient)
			w := NewWallet(mc, entities.NetworkRegtest)

			mc.On("ListUtxos", ctx, mock.Anything).Return(
				map[string]*entities.ManagedUtxo{
					"owned": {
						OutPoint: ownershipOutpoint(
							byte(90 + idx),
						),
						Assets: tc.assets,
					},
				}, nil,
			).Once()

			_, err := w.ProveOwnership(ctx, tc.ref, tc.opts...)
			require.ErrorIs(t, err, tc.wantErr)
			mc.AssertExpectations(t)
		})
	}
}

func TestProveOwnership_RPCAndProofErrors(t *testing.T) {
	ctx := context.Background()
	ref := entities.AssetRefFromGroupKey(testKey(t, 91))
	id := ownershipAssetID(91)
	scriptKey := testKey(t, 92)
	outpoint := ownershipOutpoint(91)

	setup := func(mc *mockClient) {
		mc.On("ListUtxos", ctx, mock.Anything).Return(
			map[string]*entities.ManagedUtxo{
				"owned": {
					OutPoint: outpoint,
					Assets: []*entities.AssetRecord{
						ownershipAssetRecord(
							ref, id, entities.AssetTypeFungible,
							10, scriptKey,
						),
					},
				},
			}, nil,
		).Once()
	}

	t.Run("rpc error", func(t *testing.T) {
		mc := new(mockClient)
		w := NewWallet(mc, entities.NetworkRegtest)
		setup(mc)
		mc.On("ProveAssetOwnership", ctx, mock.Anything).Return(
			nil, errors.New("boom"),
		).Once()

		_, err := w.ProveOwnership(
			ctx, ref, WithOwnershipAmount(1),
		)
		require.ErrorContains(t, err, "boom")
		mc.AssertExpectations(t)
	})

	t.Run("empty proof", func(t *testing.T) {
		mc := new(mockClient)
		w := NewWallet(mc, entities.NetworkRegtest)
		setup(mc)
		mc.On("ProveAssetOwnership", ctx, mock.Anything).Return(
			&entities.OwnershipProof{}, nil,
		).Once()

		_, err := w.ProveOwnership(
			ctx, ref, WithOwnershipAmount(1),
		)
		require.ErrorIs(t, err, ErrNoProofs)
		mc.AssertExpectations(t)
	})
}

func TestVerifyOwnership(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)

	proof := []byte("proof-with-witness")
	challenge := ownershipChallenge()
	expected := &entities.VerifyOwnershipResponse{
		Valid: true,
	}

	mc.On("VerifyAssetOwnership", ctx, mock.MatchedBy(
		func(req *entities.VerifyOwnershipRequest) bool {
			return bytes.Equal(req.ProofWithWitness, proof) &&
				bytes.Equal(req.Challenge, challenge)
		}),
	).Return(expected, nil).Once()

	resp, err := w.VerifyOwnership(
		ctx, proof, WithOwnershipChallenge(challenge),
	)
	require.NoError(t, err)
	require.Equal(t, expected, resp)

	_, err = w.VerifyOwnership(ctx, nil)
	require.ErrorIs(t, err, ErrOwnershipProofRequired)

	_, err = w.VerifyOwnership(
		ctx, proof, WithOwnershipChallenge([]byte{1, 2, 3}),
	)
	require.ErrorIs(t, err, ErrInvalidChallenge)

	_, err = w.VerifyOwnership(
		ctx, proof, WithOwnershipChallenge(make([]byte, 32)),
	)
	require.ErrorIs(t, err, ErrInvalidChallenge)

	_, err = w.VerifyOwnership(ctx, proof, WithOwnershipAmount(1))
	require.ErrorIs(t, err, ErrWrongAssetType)

	mc.On("VerifyAssetOwnership", ctx, mock.Anything).Return(
		&entities.VerifyOwnershipResponse{}, nil,
	).Once()
	_, err = w.VerifyOwnership(ctx, proof)
	require.ErrorIs(t, err, ErrOwnershipProofInvalid)

	mc.On("VerifyAssetOwnership", ctx, mock.Anything).Return(
		nil, errors.New("verify failed"),
	).Once()
	_, err = w.VerifyOwnership(ctx, proof)
	require.ErrorContains(t, err, "verify failed")
	mc.AssertExpectations(t)
}

func TestProveOwnership_CollectionAmountRejected(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)

	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 71))
	id := ownershipAssetID(71)
	mc.On("ListUtxos", ctx, mock.Anything).Return(
		map[string]*entities.ManagedUtxo{
			"item": {
				OutPoint: ownershipOutpoint(41),
				Assets: []*entities.AssetRecord{
					ownershipAssetRecord(
						collectionRef, id, entities.AssetTypeNFT,
						1, testKey(t, 72),
					),
				},
			},
		}, nil,
	).Once()

	_, err := w.ProveOwnership(
		ctx, collectionRef, WithOwnershipAmount(1),
	)
	require.ErrorIs(t, err, ErrWrongAssetType)
	mc.AssertExpectations(t)
}

func ownershipAssetRecord(ref entities.AssetRef, id entities.AssetID,
	assetType entities.AssetType, amount uint64,
	scriptKey entities.PubKey) *entities.AssetRecord {

	return &entities.AssetRecord{
		AssetRef: ref,
		Genesis: entities.IssuanceGenesis{
			IssuanceID: id,
			Type:       assetType,
			Tag:        "owned",
		},
		Amount: amount,
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKey,
		},
	}
}

func expectOwnershipProof(mc *mockClient, ctx context.Context,
	id entities.AssetID, scriptKey entities.PubKey, outpoint entities.Outpoint,
	challenge []byte, proof []byte) {

	mc.On("ProveAssetOwnership", ctx, mock.MatchedBy(
		func(req *entities.ProveOwnershipRequest) bool {
			reqID, ok := req.AssetRef.AssetID()
			return ok && reqID == id &&
				req.ScriptKey == scriptKey &&
				req.Outpoint == outpoint &&
				bytes.Equal(req.Challenge, challenge)
		}),
	).Return(&entities.OwnershipProof{
		ProofWithWitness: proof,
	}, nil).Once()
}

func ownershipAssetID(seed byte) entities.AssetID {
	var id entities.AssetID
	for i := range id {
		id[i] = seed + byte(i)
	}

	return id
}

func ownershipOutpoint(seed byte) entities.Outpoint {
	var outpoint entities.Outpoint
	for i := range outpoint.Txid {
		outpoint.Txid[i] = seed + byte(i)
	}

	return outpoint
}

func ownershipChallenge() []byte {
	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(i + 1)
	}

	return challenge
}
