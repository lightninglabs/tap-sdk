//go:build itest

package itest

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestMintAsset verifies the full minting lifecycle for a grouped fungible
// asset.
func TestMintAsset(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	minted, err := h.MintGroupedAsset(ctx, "test-token", 1000)
	require.NoError(t, err)
	require.NotNil(t, minted.Asset)
	require.NotNil(t, minted.Batch)
	require.Equal(t, entities.BatchStateFinalized,
		minted.Batch.Batch.State)
	require.True(t, minted.Asset.AssetRef.IsGroupRef())
	require.Equal(t, uint64(1000), minted.Asset.Amount)
	require.Equal(t, "test-token", minted.Asset.Genesis.Tag)

	balance := h.WaitForBalance(ctx, h.AliceWallet,
		minted.Asset.AssetRef, 1000,
		balanceTimeoutFor(minted.Asset.AssetRef),
	)
	require.Equal(t, uint64(1000), balance)
}

// TestMintCollectible verifies the full minting lifecycle for a collectible
// asset.
func TestMintCollectible(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	minted, err := h.MintCollectibleAsset(ctx, "test-nft")
	require.NoError(t, err)
	require.NotNil(t, minted.Asset)
	require.NotNil(t, minted.Batch)
	require.Equal(t, entities.BatchStateFinalized,
		minted.Batch.Batch.State)
	require.True(t, minted.Asset.AssetRef.IsAssetIDRef())
	require.Equal(t, uint64(1), minted.Asset.Amount)
	require.Equal(t, "test-nft", minted.Asset.Genesis.Tag)

	balance := h.WaitForBalance(ctx, h.AliceWallet,
		minted.Asset.AssetRef, 1,
		balanceTimeoutFor(minted.Asset.AssetRef),
	)
	require.Equal(t, uint64(1), balance)
}
