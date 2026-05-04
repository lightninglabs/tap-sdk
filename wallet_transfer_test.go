package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

func TestListTransfersUsesGroupKeyForAssetRef(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	issuanceID := testAssetID()
	groupKey := testKey(t, 91)
	groupRef := entities.AssetRefFromGroupKey(groupKey)
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
			GroupKey:   &groupKey,
		}},
		Outputs: []entities.TransferOutput{{
			IssuanceID: issuanceID,
			Amount:     200,
			ScriptKey:  testKey(t, 12),
			GroupKey:   &groupKey,
		}},
	}}

	mc.On("ListTransfers", ctx, req).Return(raw, nil)

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

func TestListTransfersUsesAssetIDRefWhenNoGroup(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	issuanceID := testAssetID()
	raw := []*entities.AssetTransfer{{
		Inputs: []entities.TransferInput{{
			IssuanceID: issuanceID,
			Amount:     1,
		}},
	}}

	mc.On("ListTransfers", ctx, (*entities.ListTransfersRequest)(nil)).
		Return(raw, nil)

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

// TestNewSendEvent verifies that the entities-level projection of a raw send
// event record into a high-level SendEvent populates AssetRefs from the
// recipient addresses, falls back to the embedded transfer's inputs/outputs
// when no addresses are present, and rebuilds the Transfer summary using
// the embedded GroupKey on each input/output.
func TestNewSendEvent(t *testing.T) {
	issuanceID := testAssetID()
	groupKey := testKey(t, 93)
	ref := entities.AssetRefFromGroupKey(groupKey)
	record := &entities.SendEventRecord{
		Timestamp:     456,
		SendState:     entities.SendStateComplete,
		NextSendState: entities.SendStateComplete,
		TransferLabel: "send-label",
		Addresses: []*entities.Address{
			{AssetRef: ref},
			{AssetRef: ref},
		},
		Transfer: &entities.AssetTransfer{
			Outputs: []entities.TransferOutput{{
				IssuanceID: issuanceID,
				Amount:     10,
				GroupKey:   &groupKey,
			}},
		},
	}

	event := entities.NewSendEvent(record)
	require.NotNil(t, event)
	require.Equal(t, record.Timestamp, event.Timestamp)
	require.Equal(t, record.SendState, event.SendState)
	require.Equal(t, record.TransferLabel, event.TransferLabel)
	require.Len(t, event.AssetRefs, 1)
	require.True(t, event.AssetRefs[0].Equivalent(ref))
	require.NotNil(t, event.Transfer)
	require.Len(t, event.Transfer.Outputs, 1)
	require.True(t, event.Transfer.Outputs[0].AssetRef.Equivalent(ref))
}

// TestNewReceiveEvent verifies that the entities-level projection of a raw
// receive event record copies AssetRef and Amount from the embedded address.
func TestNewReceiveEvent(t *testing.T) {
	ref := entities.AssetRefFromAssetID(testAssetID())
	record := &entities.ReceiveEventRecord{
		Timestamp:          789,
		Status:             entities.AddressEventStatusCompleted,
		Outpoint:           "abc:0",
		ConfirmationHeight: 100,
		Address: &entities.Address{
			AssetRef: ref,
			Amount:   25,
		},
	}

	event := entities.NewReceiveEvent(record)
	require.NotNil(t, event)
	require.Equal(t, record.Timestamp, event.Timestamp)
	require.True(t, event.AssetRef.Equivalent(ref))
	require.Equal(t, uint64(25), event.Amount)
	require.Equal(t, record.Status, event.Status)
	require.Equal(t, record.Outpoint, event.Outpoint)
	require.Equal(t, record.ConfirmationHeight, event.ConfirmationHeight)
}
