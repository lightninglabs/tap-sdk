//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestMintAsset verifies the full minting lifecycle for a grouped fungible
// asset.
func TestMintAsset(t *testing.T) {
	h, ctx := newFundedHarness(t)

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
}

// TestMintCollectible verifies the full minting lifecycle for a collectible
// asset.
func TestMintCollectible(t *testing.T) {
	h, ctx := newFundedHarness(t)

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
}
