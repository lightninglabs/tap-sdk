//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestMintAsset verifies the full minting lifecycle for a grouped fungible
// asset across every transport.
func TestMintAsset(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintGroupedAsset(t, ctx, "test-token", 1000)
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

		// ListGroups must carry our new group.
		groups, err := h.AliceClient.ListGroups(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, groups)
	})
}

// TestMintCollectible verifies the full minting lifecycle for a collectible
// asset across every transport.
func TestMintCollectible(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintCollectibleAsset(t, ctx, "test-nft")
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

// TestCancelBatch exercises the mint-cancel path.
func TestCancelBatch(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		// Stage a single asset into a fresh batch and cancel it
		// before finalization.
		_, err := h.AliceClient.CreateAsset(ctx,
			&entities.CreateAssetRequest{
				Asset: &entities.CreateAsset{
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
