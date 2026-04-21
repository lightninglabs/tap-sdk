package tapsdk

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProveGroupOwnership(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupKey := testRefGroupKey(t)
	ref := entities.AssetRefFromGroupKey(groupKey)
	issuanceA := testRefAssetID()
	issuanceB := mustAssetIDHex(t,
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	)
	scriptKeyA := mustPubKeyHex(t,
		"02b463eb05b6fdb1f38a55d1364c7bbd0cdd6f57d5f7edc75d2c8dbe5285bdcf5d",
	)
	scriptKeyB := mustPubKeyHex(t,
		"03d0c1e2ff5b8fdb7ff95f9df5694fe82f0f06f530d3b0f7a16f8b0ad7d7c22d6f",
	)

	outpointA := mustOutpoint(t,
		"1111111111111111111111111111111111111111111111111111111111111111:1",
	)
	outpointB := mustOutpoint(t,
		"2222222222222222222222222222222222222222222222222222222222222222:0",
	)

	utxos := map[string]*entities.ManagedUtxo{
		"z": {
			OutPoint: outpointB,
			Assets: []*entities.Asset{{
				AssetRef: ref,
				Genesis: entities.AssetGenesis{
					IssuanceID: issuanceB,
				},
				Amount: 75,
				ScriptKey: entities.ScriptKey{
					PubKey: scriptKeyB,
				},
			}},
		},
		"a": {
			OutPoint: outpointA,
			Assets: []*entities.Asset{
				{
					AssetRef: ref,
					Genesis: entities.AssetGenesis{
						IssuanceID: issuanceA,
					},
					Amount: 125,
					ScriptKey: entities.ScriptKey{
						PubKey: scriptKeyA,
					},
				},
				{
					AssetRef: entities.AssetRefFromAssetID(issuanceA),
					GroupKey: &entities.GroupKey{
						TweakedKey: groupKey,
					},
					Genesis: entities.AssetGenesis{
						IssuanceID: issuanceA,
					},
					Amount: 125,
					ScriptKey: entities.ScriptKey{
						PubKey: scriptKeyA,
					},
				},
				{
					AssetRef: entities.AssetRefFromAssetID(issuanceB),
					Genesis: entities.AssetGenesis{
						IssuanceID: issuanceB,
					},
					Amount: 10,
					ScriptKey: entities.ScriptKey{
						PubKey: scriptKeyB,
					},
				},
			},
		},
	}

	mc.On("ListUtxos", ctx, &entities.ListUtxosRequest{}).Return(utxos, nil)
	mc.On("ProveAssetOwnership", ctx, &entities.ProveOwnershipRequest{
		AssetRef:   ref,
		IssuanceID: issuanceA,
		ScriptKey:  scriptKeyA,
		Outpoint:   outpointA,
		Challenge:  []byte("challenge"),
	}).Return(&entities.OwnershipProof{
		ProofWithWitness: []byte("proof-a"),
	}, nil)
	mc.On("ProveAssetOwnership", ctx, &entities.ProveOwnershipRequest{
		AssetRef:   ref,
		IssuanceID: issuanceB,
		ScriptKey:  scriptKeyB,
		Outpoint:   outpointB,
		Challenge:  []byte("challenge"),
	}).Return(&entities.OwnershipProof{
		ProofWithWitness: []byte("proof-b"),
	}, nil)

	proofs, err := wallet.ProveGroupOwnership(ctx, ref, []byte("challenge"))
	require.NoError(t, err)
	require.Equal(t, []entities.GroupOwnershipProof{
		{
			IssuanceID:       issuanceA,
			ScriptKey:        scriptKeyA,
			Outpoint:         outpointA,
			Amount:           125,
			ProofWithWitness: []byte("proof-a"),
		},
		{
			IssuanceID:       issuanceB,
			ScriptKey:        scriptKeyB,
			Outpoint:         outpointB,
			Amount:           75,
			ProofWithWitness: []byte("proof-b"),
		},
	}, proofs)

	mc.AssertExpectations(t)
}

func TestProveGroupOwnership_RequiresGroupRef(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	_, err := wallet.ProveGroupOwnership(ctx,
		entities.AssetRefFromAssetID(testRefAssetID()), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "group-key AssetRef required")

	mc.AssertNotCalled(t, "ListUtxos", mock.Anything, mock.Anything)
}

func TestProveGroupOwnership_ListUtxosError(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)
	ref := entities.AssetRefFromGroupKey(testRefGroupKey(t))

	mc.On("ListUtxos", ctx, &entities.ListUtxosRequest{}).Return(
		nil, fmt.Errorf("utxo lookup failed"),
	)

	_, err := wallet.ProveGroupOwnership(ctx, ref, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "utxo lookup failed")

	mc.AssertExpectations(t)
}

func TestProveGroupOwnership_ProveError(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)
	wallet := NewWallet(mc, entities.NetworkRegtest)

	groupKey := testRefGroupKey(t)
	ref := entities.AssetRefFromGroupKey(groupKey)
	issuance := testRefAssetID()
	scriptKey := mustPubKeyHex(t,
		"02b463eb05b6fdb1f38a55d1364c7bbd0cdd6f57d5f7edc75d2c8dbe5285bdcf5d",
	)
	outpoint := mustOutpoint(t,
		"3333333333333333333333333333333333333333333333333333333333333333:2",
	)

	mc.On("ListUtxos", ctx, &entities.ListUtxosRequest{}).Return(
		map[string]*entities.ManagedUtxo{
			"proof": {
				OutPoint: outpoint,
				Assets: []*entities.Asset{{
					AssetRef: ref,
					Genesis: entities.AssetGenesis{
						IssuanceID: issuance,
					},
					Amount: 10,
					ScriptKey: entities.ScriptKey{
						PubKey: scriptKey,
					},
				}},
			},
		}, nil,
	)
	mc.On("ProveAssetOwnership", ctx, &entities.ProveOwnershipRequest{
		AssetRef:   ref,
		IssuanceID: issuance,
		ScriptKey:  scriptKey,
		Outpoint:   outpoint,
	}).Return(nil, fmt.Errorf("prove failed"))

	_, err := wallet.ProveGroupOwnership(ctx, ref, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prove failed")

	mc.AssertExpectations(t)
}

func mustAssetIDHex(t *testing.T, s string) entities.AssetID {
	t.Helper()

	b, err := hex.DecodeString(s)
	require.NoError(t, err)

	id, err := entities.ParseAssetID(b)
	require.NoError(t, err)

	return id
}

func mustPubKeyHex(t *testing.T, s string) entities.PubKey {
	t.Helper()

	b, err := hex.DecodeString(s)
	require.NoError(t, err)

	key, err := entities.ParsePubKey(b)
	require.NoError(t, err)

	return key
}

func mustOutpoint(t *testing.T, s string) entities.Outpoint {
	t.Helper()

	outpoint, err := entities.NewOutpointFromStr(s)
	require.NoError(t, err)

	return outpoint
}
