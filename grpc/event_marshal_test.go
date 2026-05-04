package grpc

import (
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/mintrpc"
	"github.com/stretchr/testify/require"
)

var (
	// testTxID is a 32-byte transaction ID for tests.
	testTxID = []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}
)

func TestUnmarshalReceiveEvent(t *testing.T) {
	tests := []struct {
		name     string
		rpcEvent *taprpc.ReceiveEvent
		wantErr  string
		validate func(*testing.T, *entities.ReceiveEventRecord)
	}{
		{
			name:     "nil event",
			rpcEvent: nil,
			wantErr:  "nil receive event",
		},
		{
			name: "minimal event",
			rpcEvent: &taprpc.ReceiveEvent{
				Timestamp: 1711627200000000,
				Outpoint:  "abc123:0",
				Status:    taprpc.AddrEventStatus_ADDR_EVENT_STATUS_TRANSACTION_DETECTED,
			},
			validate: func(t *testing.T, e *entities.ReceiveEventRecord) {
				require.Equal(t,
					int64(1711627200000000),
					e.Timestamp,
				)
				require.Equal(t, "abc123:0", e.Outpoint)
				require.Equal(t,
					entities.AddressEventStatusTransactionDetected,
					e.Status,
				)
				require.Nil(t, e.Address)
				require.Empty(t, e.Error)
				require.Zero(t, e.ConfirmationHeight)
			},
		},
		{
			name: "full event with address",
			rpcEvent: &taprpc.ReceiveEvent{
				Timestamp: 1711627200000000,
				Address: &taprpc.Addr{
					AssetId:          testAssetID,
					ScriptKey:        testPubKey,
					InternalKey:      testPubKey,
					TaprootOutputKey: testXOnlyPubKey,
					Amount:           100,
				},
				Outpoint:           "abc123:1",
				Status:             taprpc.AddrEventStatus_ADDR_EVENT_STATUS_COMPLETED,
				ConfirmationHeight: 800000,
				Error:              "",
			},
			validate: func(t *testing.T, e *entities.ReceiveEventRecord) {
				require.Equal(t,
					int64(1711627200000000),
					e.Timestamp,
				)
				require.NotNil(t, e.Address)
				require.Equal(t,
					uint64(100), e.Address.Amount,
				)
				require.Equal(t, "abc123:1", e.Outpoint)
				require.Equal(t,
					entities.AddressEventStatusCompleted,
					e.Status,
				)
				require.Equal(t,
					uint32(800000),
					e.ConfirmationHeight,
				)
			},
		},
		{
			name: "event with error",
			rpcEvent: &taprpc.ReceiveEvent{
				Timestamp: 1711627200000000,
				Status:    taprpc.AddrEventStatus_ADDR_EVENT_STATUS_PROOF_RECEIVED,
				Error:     "proof validation failed",
			},
			validate: func(t *testing.T, e *entities.ReceiveEventRecord) {
				require.Equal(t,
					entities.AddressEventStatusProofReceived,
					e.Status,
				)
				require.Equal(t,
					"proof validation failed", e.Error,
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, err := unmarshalReceiveEvent(tc.rpcEvent)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.validate(t, event)
		})
	}
}

func TestUnmarshalSendEvent(t *testing.T) {
	tests := []struct {
		name     string
		rpcEvent *taprpc.SendEvent
		wantErr  string
		validate func(*testing.T, *entities.SendEventRecord)
	}{
		{
			name:     "nil event",
			rpcEvent: nil,
			wantErr:  "nil send event",
		},
		{
			name: "minimal event",
			rpcEvent: &taprpc.SendEvent{
				// tapd populates send_state on the wire via
				// the String() method of its internal
				// tapfreighter.SendState enum, not the proto
				// enum. The fixtures must match those exact
				// strings — see tapfreighter/parcel.go.
				Timestamp:  1711627200000000,
				SendState:  string(entities.SendStateAnchorSign),
				ParcelType: taprpc.ParcelType_PARCEL_TYPE_ADDRESS,
			},
			validate: func(t *testing.T, e *entities.SendEventRecord) {
				require.Equal(t,
					int64(1711627200000000),
					e.Timestamp,
				)
				require.Equal(t,
					entities.SendStateAnchorSign,
					e.SendState,
				)
				require.Equal(t,
					entities.ParcelTypeAddress,
					e.ParcelType,
				)
				require.Nil(t, e.Addresses)
				require.Nil(t, e.AnchorTransaction)
				require.Nil(t, e.Transfer)
			},
		},
		{
			name: "event with virtual packets",
			rpcEvent: &taprpc.SendEvent{
				Timestamp:  1711627200000000,
				SendState:  string(entities.SendStateVirtualSign),
				ParcelType: taprpc.ParcelType_PARCEL_TYPE_PRE_SIGNED,
				VirtualPackets: [][]byte{
					{0x01, 0x02},
					{0x03, 0x04},
				},
				PassiveVirtualPackets: [][]byte{
					{0x05, 0x06},
				},
			},
			validate: func(t *testing.T, e *entities.SendEventRecord) {
				require.Equal(t,
					entities.ParcelTypePreSigned,
					e.ParcelType,
				)
				require.Len(t, e.VirtualPackets, 2)
				require.Len(t, e.PassiveVirtualPackets, 1)
			},
		},
		{
			name: "event with anchor transaction",
			rpcEvent: &taprpc.SendEvent{
				Timestamp: 1711627200000000,
				SendState: string(entities.SendStateAnchorSign),
				AnchorTransaction: &taprpc.AnchorTransaction{
					AnchorPsbt:         []byte{0xaa, 0xbb},
					ChangeOutputIndex:  1,
					ChainFeesSats:      500,
					TargetFeeRateSatKw: 12500,
					LndLockedUtxos: []*taprpc.OutPoint{
						{
							Txid:        testTxID,
							OutputIndex: 0,
						},
					},
					FinalTx: []byte{0xcc, 0xdd},
				},
			},
			validate: func(t *testing.T, e *entities.SendEventRecord) {
				require.NotNil(t, e.AnchorTransaction)
				require.Equal(t,
					[]byte{0xaa, 0xbb},
					e.AnchorTransaction.AnchorPsbt,
				)
				require.Equal(t,
					int32(1),
					e.AnchorTransaction.ChangeOutputIndex,
				)
				require.Equal(t,
					int64(500),
					e.AnchorTransaction.ChainFeesSats,
				)
				require.Equal(t,
					int32(12500),
					e.AnchorTransaction.TargetFeeRateSatKw,
				)
				require.Len(t, e.AnchorTransaction.LndLockedUtxos, 1)
				require.Equal(t,
					testTxID,
					e.AnchorTransaction.LndLockedUtxos[0].Txid[:],
				)
				require.Equal(t,
					uint32(0),
					e.AnchorTransaction.LndLockedUtxos[0].Index,
				)
				require.Equal(t,
					[]byte{0xcc, 0xdd},
					e.AnchorTransaction.FinalTx,
				)
			},
		},
		{
			name: "event with labels",
			rpcEvent: &taprpc.SendEvent{
				Timestamp: 1711627200000000,
				SendState: string(
					entities.SendStateStorePreBroadcast,
				),
				TransferLabel: "payment-42",
				NextSendState: string(entities.SendStateComplete),
				Error:         "timeout",
			},
			validate: func(t *testing.T, e *entities.SendEventRecord) {
				require.Equal(t,
					"payment-42", e.TransferLabel,
				)
				require.Equal(t,
					entities.SendStateComplete,
					e.NextSendState,
				)
				require.Equal(t, "timeout", e.Error)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, err := unmarshalSendEvent(tc.rpcEvent)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.validate(t, event)
		})
	}
}

func TestUnmarshalAnchorTransaction(t *testing.T) {
	tests := []struct {
		name     string
		rpcTx    *taprpc.AnchorTransaction
		wantErr  string
		validate func(*testing.T, *entities.AnchorTransaction)
	}{
		{
			name:    "nil transaction",
			rpcTx:   nil,
			wantErr: "nil anchor transaction",
		},
		{
			name:  "empty transaction",
			rpcTx: &taprpc.AnchorTransaction{},
			validate: func(t *testing.T, tx *entities.AnchorTransaction) {
				require.Nil(t, tx.AnchorPsbt)
				require.Zero(t, tx.ChangeOutputIndex)
				require.Zero(t, tx.ChainFeesSats)
				require.Nil(t, tx.LndLockedUtxos)
			},
		},
		{
			name: "full transaction",
			rpcTx: &taprpc.AnchorTransaction{
				AnchorPsbt:         []byte{0x70, 0x73, 0x62, 0x74},
				ChangeOutputIndex:  2,
				ChainFeesSats:      1000,
				TargetFeeRateSatKw: 25000,
				LndLockedUtxos: []*taprpc.OutPoint{
					{
						Txid:        testTxID,
						OutputIndex: 0,
					},
					{
						Txid:        testTxID,
						OutputIndex: 1,
					},
				},
				FinalTx: []byte{0x01, 0x00, 0x00, 0x00},
			},
			validate: func(t *testing.T, tx *entities.AnchorTransaction) {
				require.Equal(t,
					[]byte{0x70, 0x73, 0x62, 0x74},
					tx.AnchorPsbt,
				)
				require.Equal(t, int32(2), tx.ChangeOutputIndex)
				require.Equal(t, int64(1000), tx.ChainFeesSats)
				require.Equal(t,
					int32(25000), tx.TargetFeeRateSatKw,
				)
				require.Len(t, tx.LndLockedUtxos, 2)
				require.Equal(t,
					uint32(0), tx.LndLockedUtxos[0].Index,
				)
				require.Equal(t,
					uint32(1), tx.LndLockedUtxos[1].Index,
				)
				require.Equal(t,
					[]byte{0x01, 0x00, 0x00, 0x00},
					tx.FinalTx,
				)
			},
		},
		{
			name: "skips nil outpoints",
			rpcTx: &taprpc.AnchorTransaction{
				LndLockedUtxos: []*taprpc.OutPoint{
					nil,
					{
						Txid:        testTxID,
						OutputIndex: 5,
					},
				},
			},
			validate: func(t *testing.T, tx *entities.AnchorTransaction) {
				require.Len(t, tx.LndLockedUtxos, 1)
				require.Equal(t,
					uint32(5),
					tx.LndLockedUtxos[0].Index,
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := unmarshalAnchorTransaction(tc.rpcTx)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.validate(t, tx)
		})
	}
}

func TestUnmarshalOutPoint(t *testing.T) {
	tests := []struct {
		name     string
		rpcOp    *taprpc.OutPoint
		wantErr  string
		validate func(*testing.T, entities.Outpoint)
	}{
		{
			name:    "nil outpoint",
			rpcOp:   nil,
			wantErr: "nil outpoint",
		},
		{
			name: "invalid txid length",
			rpcOp: &taprpc.OutPoint{
				Txid:        []byte{0x01, 0x02},
				OutputIndex: 0,
			},
			wantErr: "invalid txid length",
		},
		{
			name: "valid outpoint",
			rpcOp: &taprpc.OutPoint{
				Txid:        testTxID,
				OutputIndex: 3,
			},
			validate: func(t *testing.T, op entities.Outpoint) {
				require.Equal(t, testTxID, op.Txid[:])
				require.Equal(t, uint32(3), op.Index)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			op, err := unmarshalOutPoint(tc.rpcOp)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.validate(t, op)
		})
	}
}

func TestUnmarshalMintEvent(t *testing.T) {
	tests := []struct {
		name     string
		rpcEvent *mintrpc.MintEvent
		wantErr  string
		validate func(*testing.T, *entities.MintEvent)
	}{
		{
			name:     "nil event",
			rpcEvent: nil,
			wantErr:  "nil mint event",
		},
		{
			name: "minimal event",
			rpcEvent: &mintrpc.MintEvent{
				Timestamp:  1711627200000000,
				BatchState: mintrpc.BatchState_BATCH_STATE_PENDING,
			},
			validate: func(t *testing.T, e *entities.MintEvent) {
				require.Equal(t,
					int64(1711627200000000),
					e.Timestamp,
				)
				require.Equal(t,
					entities.BatchStatePending,
					e.BatchState,
				)
				require.Nil(t, e.Batch)
				require.Empty(t, e.Error)
			},
		},
		{
			name: "event with error",
			rpcEvent: &mintrpc.MintEvent{
				Timestamp:  1711627200000000,
				BatchState: mintrpc.BatchState_BATCH_STATE_BROADCAST,
				Error:      "broadcast failed",
			},
			validate: func(t *testing.T, e *entities.MintEvent) {
				require.Equal(t,
					entities.BatchStateBroadcast,
					e.BatchState,
				)
				require.Equal(t,
					"broadcast failed", e.Error,
				)
			},
		},
		{
			name: "event with batch",
			rpcEvent: func() *mintrpc.MintEvent {
				_, pubKey := btcec.PrivKeyFromBytes(
					testAssetID,
				)
				return &mintrpc.MintEvent{
					Timestamp:  1711627200000000,
					BatchState: mintrpc.BatchState_BATCH_STATE_CONFIRMED,
					Batch: &mintrpc.MintingBatch{
						BatchKey: pubKey.SerializeCompressed(),
						State:    mintrpc.BatchState_BATCH_STATE_CONFIRMED,
					},
				}
			}(),
			validate: func(t *testing.T, e *entities.MintEvent) {
				require.Equal(t,
					entities.BatchStateConfirmed,
					e.BatchState,
				)
				require.NotNil(t, e.Batch)
				require.Equal(t,
					entities.BatchStateConfirmed,
					e.Batch.State,
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, err := unmarshalMintEvent(tc.rpcEvent)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.validate(t, event)
		})
	}
}
