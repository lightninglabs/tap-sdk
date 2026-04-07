package grpc

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"github.com/stretchr/testify/require"
)

func TestMarshalBackupMode(t *testing.T) {
	tests := []struct {
		name string
		mode entities.BackupMode
		want assetwalletrpc.BackupMode
	}{
		{
			name: "raw",
			mode: entities.BackupModeRaw,
			want: assetwalletrpc.BackupMode_RAW,
		},
		{
			name: "compact",
			mode: entities.BackupModeCompact,
			want: assetwalletrpc.BackupMode_COMPACT,
		},
		{
			name: "optimistic",
			mode: entities.BackupModeOptimistic,
			want: assetwalletrpc.BackupMode_OPTIMISTIC,
		},
		{
			name: "unknown defaults to raw",
			mode: entities.BackupMode(99),
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
		validate func(*testing.T,
			*entities.VerifyOwnershipResponse)
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
				result *entities.VerifyOwnershipResponse) {

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
				result *entities.VerifyOwnershipResponse) {

				require.False(t, result.Valid)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &entities.VerifyOwnershipResponse{
				Valid:       tc.resp.ValidProof,
				BlockHeight: tc.resp.BlockHeight,
			}

			if tc.resp.Outpoint != nil {
				result.Outpoint = entities.Outpoint{
					Index: tc.resp.Outpoint.OutputIndex,
				}
				copy(
					result.Outpoint.Txid[:],
					tc.resp.Outpoint.Txid,
				)
			}

			if len(tc.resp.BlockHash) == 32 {
				copy(
					result.BlockHash[:],
					tc.resp.BlockHash,
				)
			}

			tc.validate(t, result)
		})
	}
}
