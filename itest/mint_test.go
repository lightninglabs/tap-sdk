//go:build itest

package itest

import (
	"fmt"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestMintAsset verifies the full minting lifecycle for a grouped fungible
// asset across every transport.
func TestMintAsset(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(
			t, ctx, mintGroupedAssetSpec("test-token", 1000),
		)
		require.NoError(t, err)
		require.NotNil(t, minted.Asset)
		require.NotNil(t, minted.Batch)
		require.True(t, minted.Ref.IsGroupRef())
		require.Equal(t, entities.BatchStateFinalized,
			minted.Batch.Batch.State)
		require.True(t, minted.Asset.AssetRef.IsGroupRef())
		require.Equal(t, uint64(1000), minted.Asset.Amount)
		require.Equal(t, "test-token", minted.Asset.Genesis.Tag)

		balance := h.WaitForBalance(t, ctx, h.AliceWallet,
			minted.Ref, 1000,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, uint64(1000), balance)

		// ListBatches must expose the finalized batch without a
		// filter too.
		batches, err := h.AliceClient.ListBatches(
			ctx, &entities.ListBatchesRequest{},
		)
		require.NoError(t, err)
		require.NotEmpty(t, batches)

		// ListAssetGroups must carry our new group.
		groups, err := h.AliceClient.ListAssetGroups(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, groups)
	})
}

// TestMintCollectible verifies the full minting lifecycle for a collectible
// asset across every transport.
func TestMintCollectible(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(
			t, ctx, mintCollectibleAssetSpec("test-nft"),
		)
		require.NoError(t, err)
		require.NotNil(t, minted.Asset)
		require.NotNil(t, minted.Batch)
		require.True(t, minted.Ref.IsAssetIDRef())
		require.Equal(t, entities.BatchStateFinalized,
			minted.Batch.Batch.State)
		require.True(t, minted.Asset.AssetRef.IsAssetIDRef())
		require.Equal(t, uint64(1), minted.Asset.Amount)
		require.Equal(t, "test-nft", minted.Asset.Genesis.Tag)

		balance := h.WaitForBalance(t, ctx, h.AliceWallet,
			minted.Ref, 1,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, uint64(1), balance)
	})
}

// TestMultiAssetBatchLifecycle stages multiple asset types into the same
// pending mint batch, finalizes it, and verifies both logical assets are
// visible through the wallet surface.
func TestMultiAssetBatchLifecycle(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		mintEvents := h.subscribeMintEvents(t, ctx, h.AliceClient)

		tokenName := uniqueEventLabel(
			fmt.Sprintf("batch-token-%s", transport),
		)
		nftName := uniqueEventLabel(
			fmt.Sprintf("batch-nft-%s", transport),
		)

		firstBatch, err := h.AliceClient.MintAsset(ctx,
			&entities.MintAssetRequest{
				Asset: &entities.MintAsset{
					AssetType:     entities.AssetTypeNormal,
					Name:          tokenName,
					InitialSupply: 700,
				},
				ShortResponse: true,
			},
		)
		require.NoError(t, err)
		require.NotNil(t, firstBatch)

		secondBatch, err := h.AliceClient.MintAsset(ctx,
			&entities.MintAssetRequest{
				Asset: &entities.MintAsset{
					AssetType:     entities.AssetTypeCollectible,
					Name:          nftName,
					InitialSupply: 1,
				},
				ShortResponse: true,
			},
		)
		require.NoError(t, err)
		require.Equal(t, firstBatch.BatchKey, secondBatch.BatchKey)

		pending, err := h.AliceClient.ListBatches(
			ctx, &entities.ListBatchesRequest{
				BatchKey: &firstBatch.BatchKey,
			},
		)
		require.NoError(t, err)
		require.Len(t, pending, 1)

		_, err = h.AliceClient.FinalizeBatch(ctx,
			&entities.FinalizeBatchRequest{ShortResponse: true},
		)
		require.NoError(t, err)

		h.MineBlocks(t, defaultMineBlocks)
		waitForMintFinalized(t, mintEvents, firstBatch.BatchKey,
			defaultWaitTimeout)

		finalized, err := h.fetchMintBatch(ctx, firstBatch.BatchKey)
		require.NoError(t, err)
		require.Equal(t, entities.BatchStateFinalized,
			finalized.Batch.State)

		token := h.WaitForAssetByTag(
			t, ctx, h.AliceClient, tokenName, defaultWaitTimeout,
		)
		require.Equal(t, uint64(700), token.Amount)
		require.True(t, token.AssetRef.IsAssetIDRef())

		nft := h.WaitForAssetByTag(
			t, ctx, h.AliceClient, nftName, defaultWaitTimeout,
		)
		require.Equal(t, uint64(1), nft.Amount)
		require.True(t, nft.AssetRef.IsAssetIDRef())
		require.Equal(t, entities.AssetTypeCollectible,
			nft.Genesis.Type)
	})
}

// TestCancelBatch exercises the mint-cancel path.
func TestCancelBatch(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		// Stage a single asset into a fresh batch and cancel it
		// before finalization.
		_, err := h.AliceClient.MintAsset(ctx,
			&entities.MintAssetRequest{
				Asset: &entities.MintAsset{
					AssetType:     entities.AssetTypeNormal,
					Name:          "cancel-token",
					InitialSupply: 10,
				},
				ShortResponse: true,
			},
		)
		require.NoError(t, err)

		cancelled, err := h.AliceClient.CancelBatch(ctx)
		require.NoError(t, err)
		require.NotNil(t, cancelled)
	})
}
