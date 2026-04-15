//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestAddressSend verifies the full V2 address send flow using the opinionated
// Wallet helpers.
func TestAddressSend(t *testing.T) {
	h, ctx := newFundedHarness(t)

	minted, err := h.MintGroupedAsset(t, ctx, "send-token", 5000)
	require.NoError(t, err)
	require.True(t, minted.Ref.IsGroupRef())

	// TestBalanceQueries already covers the semantic balance surface, so this
	// send-path test should not block on a redundant pre-send balance poll.
	bobAddr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)
	require.NotEmpty(t, bobAddr.Encoded)
	require.Equal(t, minted.Ref, bobAddr.AssetRef)
	require.Equal(t, entities.AddressVersionV2, bobAddr.AddressVersion)
	t.Logf("Bob address: %s", bobAddr.Encoded)

	transfer, err := h.AliceWallet.Send(ctx, bobAddr.Encoded, 200)
	require.NoError(t, err)
	require.NotEmpty(t, transfer.AnchorTxid)
	t.Logf("Send tx: %s", transfer.AnchorTxid)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	bobBalance := h.WaitForBalance(t, ctx, h.BobWallet,
		minted.Ref, 200,
		balanceTimeoutFor(minted.Ref))
	t.Logf("Bob balance for %s: %d", minted.Ref, bobBalance)

	aliceBalance := h.WaitForBalance(t, ctx, h.AliceWallet,
		minted.Ref, 4800,
		balanceTimeoutFor(minted.Ref))
	t.Logf("Alice balance for %s: %d", minted.Ref, aliceBalance)
}
