//go:build itest

package itest

import (
	"context"
	"fmt"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// sendCase captures one Wallet.Send scenario end-to-end.
type sendCase struct {
	name string

	// setup produces the destination address Alice will pay. amount
	// is the number of units Bob should eventually receive.
	setup func(h *TestHarness, ctx context.Context, ref entities.AssetRef,
		amount uint64) *entities.Address

	// opts are passed to Wallet.Send. If this includes WithAmount,
	// the test routes through the explicit-amount wire path.
	opts func(amount uint64) []tapsdk.SendOption
}

// TestSend exercises Wallet.Send end-to-end across transports and
// every combination of address shape x caller intent that the SDK
// actually supports:
//
//   - v2-explicit-amount: V2 address without an embedded amount, caller
//     passes WithAmount. Routes via Recipients (AddressesWithAmounts).
//   - v2-embedded-amount: V2 address that bakes in the amount, no
//     WithAmount. Routes via TapAddrs.
//   - v2-embedded-amount-echoed: V2 address with embedded amount, caller
//     passes a matching WithAmount. Routes via Recipients (caller
//     preserves intent on the wire).
func TestSend(t *testing.T) {
	cases := []sendCase{
		{
			name: "v2-explicit-amount",
			setup: func(h *TestHarness, ctx context.Context,
				ref entities.AssetRef,
				_ uint64) *entities.Address {

				return h.CreateGroupedReceiveAddress(t, ctx, ref)
			},
			opts: func(amount uint64) []tapsdk.SendOption {
				return []tapsdk.SendOption{
					tapsdk.WithAmount(amount),
				}
			},
		},
		{
			name: "v2-embedded-amount",
			setup: func(h *TestHarness, ctx context.Context,
				ref entities.AssetRef,
				amount uint64) *entities.Address {

				return h.CreateV2EmbeddedReceiveAddress(
					t, ctx, ref, amount,
				)
			},
			opts: func(_ uint64) []tapsdk.SendOption { return nil },
		},
		{
			name: "v2-embedded-amount-echoed",
			setup: func(h *TestHarness, ctx context.Context,
				ref entities.AssetRef,
				amount uint64) *entities.Address {

				return h.CreateV2EmbeddedReceiveAddress(
					t, ctx, ref, amount,
				)
			},
			opts: func(amount uint64) []tapsdk.SendOption {
				return []tapsdk.SendOption{
					tapsdk.WithAmount(amount),
				}
			},
		},
	}

	runForTransports(t, func(t *testing.T, transport Transport) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				runSendCase(t, transport, tc)
			})
		}
	})
}

func runSendCase(t *testing.T, transport Transport, tc sendCase) {
	h, ctx := newFundedHarnessFor(t, transport)

	const amount = 175

	// Each subtest mints its own asset so group keys don't collide
	// across cases or transports.
	assetName := fmt.Sprintf("send-%s-%s", tc.name, transport)
	minted, err := h.MintGroupedAsset(t, ctx, assetName, 5000)
	require.NoError(t, err)

	addr := tc.setup(h, ctx, minted.Ref, amount)

	// Receive events do not replay reliably yet, so Bob must listen
	// before Alice sends to this address.
	recvEvents := h.subscribeReceiveEvents(t, ctx, addr.Encoded)

	// Send completion can be replayed by timestamp. The label keeps the
	// replay isolated from other transfers in a reused regtest stack.
	label := uniqueEventLabel("send")
	startTimestamp := eventStartTimestamp()

	opts := append(tc.opts(amount), tapsdk.WithLabel(label))
	transfer, err := h.AliceWallet.Send(
		ctx, addr.Encoded, opts...,
	)
	require.NoError(t, err)
	require.NotEmpty(t, transfer.AnchorTxid)

	sendEvents := h.subscribeSendEvents(t, ctx, label, startTimestamp)
	h.MineBlocks(t, defaultMineBlocks)

	waitForSendCompleted(t, sendEvents, label,
		balanceTimeoutFor(minted.Ref))
	waitForReceiveCompleted(t, recvEvents, addr.Encoded,
		balanceTimeoutFor(minted.Ref))

	// Terminal receive events should make the balance visible; assert
	// that with one read instead of polling.
	bobBalance, err := h.BobWallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, uint64(amount), bobBalance)
}

// sendMultiCase captures one Wallet.SendMulti scenario end-to-end.
type sendMultiCase struct {
	name string

	// recipients builds the []entities.Recipient for the send. addrs
	// is the ordered list of addresses already decoded on Bob's side.
	recipients func(addrs []*entities.Address) []entities.Recipient
}

// TestSendMulti exercises Wallet.SendMulti end-to-end:
//
//   - all-explicit: every Recipient.Amount is non-nil (V2 + WithAmount
//     equivalent for multi).
//   - all-embedded: every Recipient.Amount is nil; addresses all embed
//     their amount. Routes via TapAddrs.
//   - mixed-normalised: one explicit, one embedded — the SDK echoes
//     the embedded value into the request so tapd sees a uniform
//     AddressesWithAmounts shape.
func TestSendMulti(t *testing.T) {
	cases := []sendMultiCase{
		{
			name: "all-explicit",
			recipients: func(addrs []*entities.Address,
			) []entities.Recipient {

				amt1 := uint64(100)
				amt2 := uint64(150)
				return []entities.Recipient{
					{
						Address: addrs[0].Encoded,
						Amount:  &amt1,
					},
					{
						Address: addrs[1].Encoded,
						Amount:  &amt2,
					},
				}
			},
		},
		{
			name: "all-embedded",
			recipients: func(addrs []*entities.Address,
			) []entities.Recipient {

				return []entities.Recipient{
					{Address: addrs[0].Encoded},
					{Address: addrs[1].Encoded},
				}
			},
		},
		{
			name: "mixed-normalised",
			recipients: func(addrs []*entities.Address,
			) []entities.Recipient {

				amt := uint64(100)
				return []entities.Recipient{
					{
						Address: addrs[0].Encoded,
						Amount:  &amt,
					},
					// second recipient nil -> SDK echoes
					// the embedded value into the wire.
					{Address: addrs[1].Encoded},
				}
			},
		},
	}

	runForTransports(t, func(t *testing.T, transport Transport) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				runSendMultiCase(t, transport, tc)
			})
		}
	})
}

func runSendMultiCase(t *testing.T, transport Transport,
	tc sendMultiCase) {

	h, ctx := newFundedHarnessFor(t, transport)

	assetName := fmt.Sprintf("multi-%s-%s", tc.name, transport)
	minted, err := h.MintGroupedAsset(t, ctx, assetName, 5000)
	require.NoError(t, err)

	// All cases send 100 + 150 = 250 units total. "all-embedded" and
	// "mixed-normalised" need addresses with embedded amounts, while
	// "all-explicit" uses amount-less V2 addresses.
	var addrs []*entities.Address
	switch tc.name {
	case "all-explicit":
		addrs = []*entities.Address{
			h.CreateGroupedReceiveAddress(t, ctx, minted.Ref),
			h.CreateGroupedReceiveAddress(t, ctx, minted.Ref),
		}

	case "all-embedded":
		addrs = []*entities.Address{
			h.CreateV2EmbeddedReceiveAddress(
				t, ctx, minted.Ref, 100,
			),
			h.CreateV2EmbeddedReceiveAddress(
				t, ctx, minted.Ref, 150,
			),
		}

	case "mixed-normalised":
		addrs = []*entities.Address{
			h.CreateGroupedReceiveAddress(t, ctx, minted.Ref),
			h.CreateV2EmbeddedReceiveAddress(
				t, ctx, minted.Ref, 150,
			),
		}
	}

	recvEvents := make(
		[]*eventSubscription[entities.ReceiveEvent], 0, len(addrs),
	)
	for _, addr := range addrs {
		// Receive streams are per address and need to exist before the
		// transfer because tapd does not replay them yet.
		recvEvents = append(recvEvents,
			h.subscribeReceiveEvents(t, ctx, addr.Encoded))
	}

	// The send stream can replay completed transfers by label from this
	// cursor, which avoids racing the initial stream setup.
	label := uniqueEventLabel("multi")
	startTimestamp := eventStartTimestamp()

	transfer, err := h.AliceWallet.SendMulti(
		ctx, tc.recipients(addrs), tapsdk.WithLabel(label),
	)
	require.NoError(t, err)
	require.NotEmpty(t, transfer.AnchorTxid)

	sendEvents := h.subscribeSendEvents(t, ctx, label, startTimestamp)
	h.MineBlocks(t, defaultMineBlocks)

	waitForSendCompleted(t, sendEvents, label,
		balanceTimeoutFor(minted.Ref))
	for idx, recvSub := range recvEvents {
		waitForReceiveCompleted(t, recvSub, addrs[idx].Encoded,
			balanceTimeoutFor(minted.Ref))
	}

	// The completed receive events are the readiness signal for this
	// direct balance assertion.
	bobBalance, err := h.BobWallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, uint64(250), bobBalance)
}

// TestSendRejections covers the SDK-side validation errors across both
// Send and SendMulti, against real addresses produced by tapd.
func TestSendRejections(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		assetName := fmt.Sprintf("reject-%s", transport)
		minted, err := h.MintGroupedAsset(t, ctx, assetName, 5000)
		require.NoError(t, err)

		noAmount := h.CreateGroupedReceiveAddress(
			t, ctx, minted.Ref,
		)
		embedded := h.CreateV2EmbeddedReceiveAddress(
			t, ctx, minted.Ref, 50,
		)

		t.Run("Send/amount-required", func(t *testing.T) {
			_, err := h.AliceWallet.Send(ctx, noAmount.Encoded)
			require.ErrorIs(t, err, tapsdk.ErrAmountRequired)
		})

		t.Run("Send/amount-mismatch", func(t *testing.T) {
			_, err := h.AliceWallet.Send(
				ctx, embedded.Encoded, tapsdk.WithAmount(999),
			)
			require.ErrorIs(t, err, tapsdk.ErrAmountMismatch)
		})

		t.Run("SendMulti/amount-required", func(t *testing.T) {
			_, err := h.AliceWallet.SendMulti(ctx,
				[]entities.Recipient{
					{Address: noAmount.Encoded},
				},
			)
			require.ErrorIs(t, err, tapsdk.ErrAmountRequired)
		})

		t.Run("SendMulti/amount-mismatch", func(t *testing.T) {
			amt := uint64(999)
			_, err := h.AliceWallet.SendMulti(ctx,
				[]entities.Recipient{{
					Address: embedded.Encoded,
					Amount:  &amt,
				}},
			)
			require.ErrorIs(t, err, tapsdk.ErrAmountMismatch)
		})
	})
}

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

		// Subscribe before the payment; receive-event replay currently
		// does not cover late subscribers.
		recvEvents := h.subscribeReceiveEvents(
			t, ctx, bobAddr.Encoded,
		)

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

		// Label + timestamp lets the send stream replay the terminal
		// event even if subscription setup overlaps the send RPC.
		label := uniqueEventLabel("address-send")
		startTimestamp := eventStartTimestamp()

		transfer, err := h.AliceWallet.Send(
			ctx, bobAddr.Encoded, tapsdk.WithAmount(200),
			tapsdk.WithLabel(label),
		)
		require.NoError(t, err)
		require.NotEmpty(t, transfer.AnchorTxid)

		sendEvents := h.subscribeSendEvents(
			t, ctx, label, startTimestamp,
		)
		h.MineBlocks(t, defaultMineBlocks)

		waitForSendCompleted(t, sendEvents, label,
			balanceTimeoutFor(minted.Ref))
		waitForReceiveCompleted(t, recvEvents, bobAddr.Encoded,
			balanceTimeoutFor(minted.Ref))

		// Once both terminal events arrive, balances should be visible
		// without a polling loop.
		bobBalance, err := h.BobWallet.GetBalance(ctx, minted.Ref)
		require.NoError(t, err)
		require.Equal(t, uint64(200), bobBalance)

		aliceBalance, err := h.AliceWallet.GetBalance(ctx, minted.Ref)
		require.NoError(t, err)
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
