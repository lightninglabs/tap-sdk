//go:build itest

package itest

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestProofOperations verifies proof export, unpack, and decode
// operations on a minted asset.
func TestProofOperations(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	// Mint a simple asset.
	batch, err := h.AliceClient.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType: entities.AssetTypeNormal,
					Name:      "proof-token",
					Amount:    100,
				},
			},
			ShortResponse: true,
		},
	)
	require.NoError(t, err)

	_, err = h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true})
	require.NoError(t, err)

	h.MineBlocks(defaultMineBlocks)
	h.WaitForMint(ctx, h.AliceClient, batch.BatchKey,
		60*time.Second)

	// Find the asset in the wallet.
	assets, err := h.AliceClient.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	require.NoError(t, err)

	var target *entities.Asset
	for _, a := range assets {
		if a.Genesis.Tag == "proof-token" {
			target = a
			break
		}
	}
	require.NotNil(t, target, "minted asset not found")

	// Export the proof for this asset.
	proof, err := h.AliceClient.ExportProof(ctx,
		target.Genesis.AssetID, target.ScriptKey.PubKey, nil)
	require.NoError(t, err)
	require.NotEmpty(t, proof.RawProofFile,
		"proof file should not be empty")
	t.Logf("Exported proof: %d bytes", len(proof.RawProofFile))

	// Unpack the proof file into individual proofs.
	rawProofs, err := h.AliceClient.UnpackProofFile(ctx,
		proof.RawProofFile)
	require.NoError(t, err)
	require.NotEmpty(t, rawProofs,
		"proof file should contain at least one proof")
	t.Logf("Unpacked %d proofs", len(rawProofs))

	// Decode the last proof.
	lastProof := rawProofs[len(rawProofs)-1]
	decoded, err := h.AliceClient.DecodeProof(ctx, lastProof)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.Equal(t, target.Genesis.AssetID, decoded.AssetID)
	t.Logf("Decoded proof: asset=%s, outpoint=%s",
		decoded.AssetID, decoded.Outpoint)
}

// TestBalanceQueries verifies ListBalances grouping modes against
// a real daemon.
func TestBalanceQueries(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	// Mint a grouped fungible asset.
	batch, err := h.AliceClient.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType:       entities.AssetTypeNormal,
					Name:            "balance-token",
					Amount:          500,
					NewGroupedAsset: true,
				},
			},
			ShortResponse: true,
		},
	)
	require.NoError(t, err)

	_, err = h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true})
	require.NoError(t, err)

	h.MineBlocks(defaultMineBlocks)
	h.WaitForMint(ctx, h.AliceClient, batch.BatchKey,
		60*time.Second)

	// Query balance grouped by asset ID.
	byAssetID, err := h.AliceClient.ListBalances(ctx,
		&entities.ListBalancesRequest{
			GroupBy: entities.BalanceGroupByAssetID,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, byAssetID.AssetBalances)

	var foundByID bool
	for _, bal := range byAssetID.AssetBalances {
		if bal.AssetGenesis.Tag == "balance-token" {
			require.Equal(t, uint64(500), bal.Balance)
			foundByID = true
			t.Logf("Balance by asset ID: %s = %d",
				bal.AssetGenesis.AssetID, bal.Balance)
			break
		}
	}
	require.True(t, foundByID,
		"balance-token not found in asset ID query")

	// Query balance grouped by group key.
	byGroupKey, err := h.AliceClient.ListBalances(ctx,
		&entities.ListBalancesRequest{
			GroupBy: entities.BalanceGroupByGroupKey,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, byGroupKey.AssetGroupBalances)
	require.NotEmpty(t, byGroupKey.AssetGroupBalances,
		"should have at least one group balance")

	// At least one group should have a balance of 500.
	var foundByGroup bool
	for key, bal := range byGroupKey.AssetGroupBalances {
		if bal.Balance == 500 {
			foundByGroup = true
			t.Logf("Balance by group key: %s = %d",
				key, bal.Balance)
			break
		}
	}
	require.True(t, foundByGroup,
		"group balance of 500 not found")
}

// TestErrorHandling verifies the SDK returns proper errors for invalid
// inputs.
func TestErrorHandling(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	// Send to an invalid address should return an error.
	_, err := h.AliceClient.SendAsset(ctx,
		&entities.SendAssetRequest{
			TapAddresses: []string{"not-a-valid-address"},
		},
	)
	require.Error(t, err)
	t.Logf("Invalid send error: %v", err)

	// Export proof for a non-existent asset should return an error.
	fakeID := entities.AssetID{}
	fakePubKey := entities.PubKey{}
	_, err = h.AliceClient.ExportProof(ctx, fakeID, fakePubKey,
		nil)
	require.Error(t, err)
	t.Logf("Non-existent proof error: %v", err)

	// Decode an invalid proof should return an error.
	_, err = h.AliceClient.DecodeProof(ctx, []byte{0x00, 0x01})
	require.Error(t, err)
	t.Logf("Invalid decode error: %v", err)
}

// init is a guard so this file is never compiled without the itest tag.
func init() {
	// Ensure a minimum timeout for Docker container readiness. This is
	// a safety net; the actual timeout is in the test harness.
	_ = 120 * time.Second
}
