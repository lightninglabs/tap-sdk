package grpc

import (
	"context"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"github.com/stretchr/testify/require"
)

func TestWalletKitCustomAnchorCapabilities(t *testing.T) {
	client := &walletKitClient{}

	caps, err := client.CustomAnchorCapabilities(context.Background())
	require.NoError(t, err)
	require.Equal(t, tapsdk.DefaultTapdCustomAnchorCapabilities(), *caps)
}

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
