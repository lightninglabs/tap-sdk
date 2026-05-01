package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestListTransfersSummarizesAssetRefs(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	issuanceID := testAssetID()
	groupRef := entities.AssetRefFromGroupKey(testKey(t, 91))
	req := &entities.ListTransfersRequest{AnchorTxid: "abc"}
	raw := []*entities.AssetTransfer{{
		TransferTimestamp:   123,
		AnchorTxid:          "abc",
		AnchorTxBlockHeight: 42,
		AnchorTxChainFees:   250,
		Label:               "label",
		Inputs: []entities.TransferInput{{
			IssuanceID: issuanceID,
			Amount:     300,
			ScriptKey:  testKey(t, 11),
		}},
		Outputs: []entities.TransferOutput{{
			IssuanceID: issuanceID,
			Amount:     200,
			ScriptKey:  testKey(t, 12),
		}},
	}}
	records := []*entities.AssetRecord{{
		AssetRef: groupRef,
		Genesis: entities.IssuanceGenesis{
			IssuanceID: issuanceID,
			Type:       entities.AssetTypeNormal,
		},
	}}

	mc.On("ListTransfers", ctx, req).Return(raw, nil)
	mc.On("ListAssetRecords", ctx, mock.MatchedBy(
		func(req *entities.ListAssetsRequest) bool {
			return req.IncludeSpent &&
				!req.IncludeLeased &&
				req.ScriptKeyType != nil &&
				req.ScriptKeyType.AllTypes
		}),
	).Return(records, nil)

	transfers, err := w.ListTransfers(ctx, req)
	require.NoError(t, err)
	require.Len(t, transfers, 1)

	transfer := transfers[0]
	require.Equal(t, raw[0].TransferTimestamp,
		transfer.TransferTimestamp)
	require.Equal(t, raw[0].AnchorTxid, transfer.AnchorTxid)
	require.Equal(t, raw[0].AnchorTxBlockHeight,
		transfer.AnchorTxBlockHeight)
	require.Equal(t, raw[0].AnchorTxChainFees,
		transfer.AnchorTxChainFees)
	require.Equal(t, raw[0].Label, transfer.Label)

	require.Len(t, transfer.Inputs, 1)
	require.True(t, transfer.Inputs[0].AssetRef.Equivalent(groupRef))
	require.Equal(t, issuanceID, transfer.Inputs[0].IssuanceID)
	require.Equal(t, uint64(300), transfer.Inputs[0].Amount)

	require.Len(t, transfer.Outputs, 1)
	require.True(t, transfer.Outputs[0].AssetRef.Equivalent(groupRef))
	require.Equal(t, issuanceID, transfer.Outputs[0].IssuanceID)
	require.Equal(t, uint64(200), transfer.Outputs[0].Amount)

	mc.AssertExpectations(t)
}

func TestListTransfersUsesAssetIDRefForCollectibles(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	issuanceID := testAssetID()
	collectionRef := entities.AssetRefFromGroupKey(testKey(t, 92))
	raw := []*entities.AssetTransfer{{
		Inputs: []entities.TransferInput{{
			IssuanceID: issuanceID,
			Amount:     1,
		}},
	}}
	records := []*entities.AssetRecord{{
		AssetRef: collectionRef,
		Genesis: entities.IssuanceGenesis{
			IssuanceID: issuanceID,
			Type:       entities.AssetTypeCollectible,
		},
	}}

	mc.On("ListTransfers", ctx, (*entities.ListTransfersRequest)(nil)).
		Return(raw, nil)
	mc.On("ListAssetRecords", ctx, mock.Anything).Return(records, nil)

	transfers, err := w.ListTransfers(ctx, nil)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	require.Len(t, transfers[0].Inputs, 1)
	require.True(t, transfers[0].Inputs[0].AssetRef.IsAssetIDRef())
	require.True(t, transfers[0].Inputs[0].AssetRef.Equivalent(
		entities.AssetRefFromAssetID(issuanceID),
	))

	mc.AssertExpectations(t)
}

