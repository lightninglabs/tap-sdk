//go:build itest

package itest

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestAddressSend verifies the full V2 address send flow using the opinionated
// Wallet helpers.
func TestAddressSend(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	minted, err := h.MintGroupedAsset(ctx, "send-token", 5000)
	require.NoError(t, err)
	require.True(t, minted.Asset.AssetRef.IsGroupRef())

	h.EnableUniverseBootstrap(ctx)

	bobAddr := h.WaitForReceiveAddress(ctx, h.BobWallet,
		minted.Asset.AssetRef, 60*time.Second)
	require.NotEmpty(t, bobAddr.Encoded)
	require.Equal(t, minted.Asset.AssetRef, bobAddr.AssetRef)
	require.Equal(t, entities.AddressVersionV2, bobAddr.AddressVersion)
	t.Logf("Bob address: %s", bobAddr.Encoded)

	transfer, err := h.AliceWallet.Send(ctx, bobAddr.Encoded, 200)
	require.NoError(t, err)
	require.NotEmpty(t, transfer.AnchorTxid)
	t.Logf("Send tx: %s", transfer.AnchorTxid)

	h.MineBlocks(defaultMineBlocks)
	h.WaitForSync(h.AliceClient, 30*time.Second)
	h.WaitForSync(h.BobClient, 30*time.Second)

	bobBalance := h.WaitForBalance(ctx, h.BobWallet,
		minted.Asset.AssetRef, 200, 120*time.Second)
	t.Logf("Bob balance for %s: %d", minted.Asset.AssetRef, bobBalance)

	aliceBalance := h.WaitForBalance(ctx, h.AliceWallet,
		minted.Asset.AssetRef, 4800, 120*time.Second)
	t.Logf("Alice balance for %s: %d", minted.Asset.AssetRef,
		aliceBalance)
}
