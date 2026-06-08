package grpc

import (
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"github.com/stretchr/testify/require"
)

func TestMarshalBackupMode(t *testing.T) {
	tests := []struct {
		name string
		mode tapsdk.BackupMode
		want assetwalletrpc.BackupMode
	}{
		{
			name: "raw",
			mode: tapsdk.BackupModeRaw,
			want: assetwalletrpc.BackupMode_RAW,
		},
		{
			name: "compact",
			mode: tapsdk.BackupModeCompact,
			want: assetwalletrpc.BackupMode_COMPACT,
		},
		{
			name: "optimistic",
			mode: tapsdk.BackupModeOptimistic,
			want: assetwalletrpc.BackupMode_OPTIMISTIC,
		},
		{
			name: "unknown defaults to raw",
			mode: tapsdk.BackupMode(99),
			want: assetwalletrpc.BackupMode_RAW,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, marshalBackupMode(tc.mode))
		})
	}
}

func TestVerifyOwnershipResponseUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		resp     *assetwalletrpc.VerifyAssetOwnershipResponse
		wantErr  string
		validate func(*testing.T,
			*tapsdk.VerifyOwnershipResponse)
	}{
		{
			name: "valid proof",
			resp: &assetwalletrpc.VerifyAssetOwnershipResponse{
				ValidProof: true,
				Outpoint: &taprpc.OutPoint{
					Txid:        testAssetID,
					OutputIndex: 1,
				},
				BlockHash:   testAssetID,
				BlockHeight: 800000,
			},
			validate: func(t *testing.T,
				result *tapsdk.VerifyOwnershipResponse) {

				require.True(t, result.Valid)
				require.Equal(
					t, uint32(1),
					result.Outpoint.Index,
				)
				require.Equal(
					t, uint32(800000),
					result.BlockHeight,
				)
			},
		},
		{
			name: "invalid proof",
			resp: &assetwalletrpc.VerifyAssetOwnershipResponse{
				ValidProof: false,
			},
			validate: func(t *testing.T,
				result *tapsdk.VerifyOwnershipResponse) {

				require.False(t, result.Valid)
			},
		},
		{
			name: "valid proof without outpoint",
			resp: &assetwalletrpc.VerifyAssetOwnershipResponse{
				ValidProof: true,
			},
			wantErr: "missing outpoint",
		},
		{
			name: "invalid outpoint txid length",
			resp: &assetwalletrpc.VerifyAssetOwnershipResponse{
				Outpoint: &taprpc.OutPoint{
					Txid: []byte{1, 2, 3},
				},
			},
			wantErr: "invalid outpoint txid length",
		},
		{
			name: "invalid block hash length",
			resp: &assetwalletrpc.VerifyAssetOwnershipResponse{
				BlockHash: []byte{1, 2, 3},
			},
			wantErr: "invalid block hash length",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := unmarshalVerifyOwnershipResponse(tc.resp)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			tc.validate(t, result)
		})
	}
}

func TestMarshalCommitVirtualPsbtsRequest(t *testing.T) {
	feeRate, err := tapsdk.NewFeeRateSatPerVByte(12)
	require.NoError(t, err)

	req := &tapsdk.CommitVirtualPsbtsRequest{
		AnchorPsbt:        []byte("anchor"),
		VirtualPsbts:      [][]byte{[]byte("virtual")},
		PassiveAssetPsbts: [][]byte{[]byte("passive")},
		Funding: tapsdk.AnchorFundingPlan{
			ChangeOutput: tapsdk.AnchorChangeOutput{
				Mode:                tapsdk.AnchorChangeOutputExisting,
				ExistingOutputIndex: 0,
			},
			Fee: tapsdk.AnchorFee{
				Mode:    tapsdk.AnchorFeeSatPerVByte,
				FeeRate: feeRate,
			},
			CustomLockID:          []byte("lock"),
			LockExpirationSeconds: 42,
		},
	}

	rpcReq, err := marshalCommitVirtualPsbtsRequest(req)
	require.NoError(t, err)
	require.Equal(t, []byte("anchor"), rpcReq.AnchorPsbt)
	require.Equal(t, [][]byte{[]byte("virtual")}, rpcReq.VirtualPsbts)
	require.Equal(
		t, [][]byte{[]byte("passive")}, rpcReq.PassiveAssetPsbts,
	)
	require.Equal(t, int32(0), rpcReq.GetExistingOutputIndex())
	require.Equal(t, uint64(12), rpcReq.GetSatPerVbyte())
	require.Equal(t, []byte("lock"), rpcReq.CustomLockId)
	require.Equal(t, uint64(42), rpcReq.LockExpirationSeconds)
}

func TestMarshalCommitVirtualPsbtsNoNewChange(t *testing.T) {
	req := &tapsdk.CommitVirtualPsbtsRequest{
		AnchorPsbt:   []byte("anchor"),
		VirtualPsbts: [][]byte{[]byte("virtual")},
		Funding: tapsdk.AnchorFundingPlan{
			ChangeOutput: tapsdk.AnchorChangeOutput{
				Mode: tapsdk.AnchorChangeOutputNoNew,
			},
			Fee: tapsdk.AnchorFee{
				Mode:       tapsdk.AnchorFeeTargetConf,
				TargetConf: 6,
			},
		},
	}

	rpcReq, err := marshalCommitVirtualPsbtsRequest(req)
	require.NoError(t, err)
	require.IsType(
		t, &assetwalletrpc.CommitVirtualPsbtsRequest_Add{},
		rpcReq.AnchorChangeOutput,
	)
	require.False(t, rpcReq.GetAdd())
	require.Equal(t, uint32(6), rpcReq.GetTargetConf())
}

func TestUnmarshalCommitVirtualPsbtsResponse(t *testing.T) {
	resp := &assetwalletrpc.CommitVirtualPsbtsResponse{
		AnchorPsbt:        []byte("anchor"),
		VirtualPsbts:      [][]byte{[]byte("virtual")},
		PassiveAssetPsbts: [][]byte{[]byte("passive")},
		ChangeOutputIndex: 2,
		LndLockedUtxos: []*taprpc.OutPoint{{
			Txid:        testAssetID,
			OutputIndex: 7,
		}},
	}

	result, err := unmarshalCommitVirtualPsbtsResponse(resp)
	require.NoError(t, err)
	require.Equal(t, []byte("anchor"), result.AnchorPsbt)
	require.Equal(t, [][]byte{[]byte("virtual")}, result.VirtualPsbts)
	require.Equal(
		t, [][]byte{[]byte("passive")}, result.PassiveAssetPsbts,
	)
	require.Equal(t, int32(2), result.ChangeOutputIndex)
	require.Len(t, result.LockedUTXOs, 1)
	require.Equal(t, testAssetID, result.LockedUTXOs[0].Txid[:])
	require.Equal(t, uint32(7), result.LockedUTXOs[0].Index)
}

func TestMarshalPublishAndLogTransferRequest(t *testing.T) {
	var outpoint tapsdk.Outpoint
	copy(outpoint.Txid[:], testAssetID)
	outpoint.Index = 5

	req := &tapsdk.PublishAndLogTransferRequest{
		AnchorPsbt:            []byte("anchor"),
		VirtualPsbts:          [][]byte{[]byte("virtual")},
		PassiveAssetPsbts:     [][]byte{[]byte("passive")},
		ChangeOutputIndex:     0,
		LockedUTXOs:           []tapsdk.Outpoint{outpoint},
		SkipAnchorTxBroadcast: true,
		Label:                 "custom-label",
	}

	rpcReq, err := marshalPublishAndLogTransferRequest(req)
	require.NoError(t, err)
	require.Equal(t, []byte("anchor"), rpcReq.AnchorPsbt)
	require.Equal(t, int32(0), rpcReq.ChangeOutputIndex)
	require.Len(t, rpcReq.LndLockedUtxos, 1)
	require.Equal(t, testAssetID, rpcReq.LndLockedUtxos[0].Txid)
	require.Equal(t, uint32(5), rpcReq.LndLockedUtxos[0].OutputIndex)
	require.True(t, rpcReq.SkipAnchorTxBroadcast)
	require.Equal(t, "custom-label", rpcReq.Label)
}
