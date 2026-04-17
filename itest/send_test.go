//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestAddressSend verifies the full V2 address send flow using the
// opinionated Wallet helpers across every transport.
func TestAddressSend(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintGroupedAsset(t, ctx, "send-token", 5000)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		bobAddr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)
		require.NotEmpty(t, bobAddr.Encoded)
		require.Equal(t, minted.Ref, bobAddr.AssetRef)
		require.Equal(t, entities.AddressVersionV2,
			bobAddr.AddressVersion)

		// Bob's wallet should round-trip the address string through
		// DecodeAddr unchanged.
		decoded, err := h.BobClient.DecodeAddr(ctx, bobAddr.Encoded)
		require.NoError(t, err)
		require.Equal(t, bobAddr.Encoded, decoded.Encoded)

		// Bob should also be able to look up the address via
		// QueryAddrs.
		queried, err := h.BobClient.QueryAddrs(
			ctx, &entities.AddressQuery{},
		)
		require.NoError(t, err)
		require.NotEmpty(t, queried)

		transfer, err := h.AliceWallet.Send(ctx, bobAddr.Encoded, 200)
		require.NoError(t, err)
		require.NotEmpty(t, transfer.AnchorTxid)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		bobBalance := h.WaitForBalance(t, ctx, h.BobWallet,
			minted.Ref, 200,
			balanceTimeoutFor(minted.Ref))
		require.Equal(t, uint64(200), bobBalance)

		aliceBalance := h.WaitForBalance(t, ctx, h.AliceWallet,
			minted.Ref, 4800,
			balanceTimeoutFor(minted.Ref))
		require.Equal(t, uint64(4800), aliceBalance)

		// ListTransfers must surface the anchor transaction on Alice's
		// side once confirmed.
		transfers, err := h.AliceClient.ListTransfers(ctx,
			&entities.ListTransfersRequest{
				AnchorTxid: transfer.AnchorTxid,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, transfers)

		// Bob should observe the incoming transfer through
		// AddrReceives.
		events, err := h.BobClient.AddrReceives(ctx,
			&entities.AddressReceivesQuery{},
		)
		require.NoError(t, err)
		require.NotEmpty(t, events)
	})
}
