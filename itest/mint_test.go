//go:build itest

package itest

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestMintAsset verifies the full minting lifecycle: stage an asset in
// a batch, finalize the batch, mine blocks, and verify the minted
// asset appears in the wallet.
func TestMintAsset(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	// Fund Alice's LND wallet so she can pay for the mint tx.
	h.FundLndWallet()

	// Stage a fungible asset in Alice's mint batch.
	batch, err := h.AliceClient.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType:       entities.AssetTypeNormal,
					Name:            "test-token",
					Amount:          1000,
					NewGroupedAsset: true,
				},
			},
			ShortResponse: true,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, batch)
	t.Logf("Batch key: %x", batch.BatchKey[:])

	// Finalize the batch (fund + seal + broadcast in one call).
	finalized, err := h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{
			ShortResponse: true,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, finalized)
	t.Logf("Batch state after finalize: %d", finalized.State)

	// Mine blocks to confirm the genesis transaction.
	h.MineBlocks(defaultMineBlocks)

	// Wait for the batch to reach FINALIZED state.
	batches := h.WaitForMint(ctx, h.AliceClient, batch.BatchKey,
		60*time.Second)
	require.Len(t, batches, 1)
	require.Equal(t, entities.BatchStateFinalized,
		batches[0].Batch.State)

	t.Logf("Mint finalized, txid=%s",
		batches[0].Batch.BatchTxid)

	// Verify the asset appears in Alice's wallet.
	assets, err := h.AliceClient.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, assets)

	var found bool
	for _, a := range assets {
		if a.Genesis.Tag == "test-token" && a.Amount == 1000 {
			found = true
			t.Logf("Found minted asset: id=%s amount=%d",
				a.Genesis.AssetID, a.Amount)
			break
		}
	}
	require.True(t, found, "minted asset not found in wallet")
}

// TestMintCollectible verifies minting a collectible (unique,
// amount=1) asset.
func TestMintCollectible(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	// Stage a collectible.
	batch, err := h.AliceClient.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType: entities.AssetTypeCollectible,
					Name:      "test-nft",
					Amount:    1,
				},
			},
			ShortResponse: true,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, batch)

	// Finalize.
	_, err = h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true})
	require.NoError(t, err)

	h.MineBlocks(defaultMineBlocks)

	// Wait for finalization.
	h.WaitForMint(ctx, h.AliceClient, batch.BatchKey,
		60*time.Second)

	// Verify the collectible is in the wallet.
	assets, err := h.AliceClient.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	require.NoError(t, err)

	var found bool
	for _, a := range assets {
		if a.Genesis.Tag == "test-nft" &&
			a.Genesis.Type == entities.AssetTypeCollectible {

			found = true
			require.Equal(t, uint64(1), a.Amount)
			t.Logf("Found collectible: id=%s",
				a.Genesis.AssetID)
			break
		}
	}
	require.True(t, found, "collectible not found in wallet")
}
