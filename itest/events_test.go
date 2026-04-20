//go:build itest

package itest

import (
	"context"
	"sync"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestEventListenerMintAndSend exercises the real-time event streams via
// tapsdk.NewEventListener. REST does not support streaming yet, so this
// test is gRPC-only.
func TestEventListenerMintAndSend(t *testing.T) {
	h, ctx := newFundedHarnessFor(t, TransportGRPC)

	var (
		mu          sync.Mutex
		mintEvents  []*entities.MintEvent
		sendEvents  []*entities.SendEvent
		recvEvents  []*entities.ReceiveEvent
		streamError error
	)

	listener := tapsdk.NewEventListener(
		h.AliceClient,
		tapsdk.EventHandler{
			OnMint: func(
				_ context.Context, e *entities.MintEvent,
			) {
				mu.Lock()
				mintEvents = append(mintEvents, e)
				mu.Unlock()
			},
			OnSend: func(
				_ context.Context, e *entities.SendEvent,
			) {
				mu.Lock()
				sendEvents = append(sendEvents, e)
				mu.Unlock()
			},
			OnError: func(_ string, err error) {
				mu.Lock()
				streamError = err
				mu.Unlock()
			},
		},
	)
	require.NoError(t, listener.Start(ctx))
	t.Cleanup(func() { _ = listener.Stop() })

	// Receiver listens on Bob.
	bobListener := tapsdk.NewEventListener(
		h.BobClient,
		tapsdk.EventHandler{
			OnReceive: func(
				_ context.Context, e *entities.ReceiveEvent,
			) {
				mu.Lock()
				recvEvents = append(recvEvents, e)
				mu.Unlock()
			},
		},
	)
	require.NoError(t, bobListener.Start(ctx))
	t.Cleanup(func() { _ = bobListener.Stop() })

	minted, err := h.MintGroupedAsset(t, ctx, "event-token", 1000)
	require.NoError(t, err)
	require.True(t, minted.Ref.IsGroupRef())

	bobAddr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)
	_, err = h.AliceWallet.Send(ctx, bobAddr.Encoded, 10)
	require.NoError(t, err)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	h.WaitForBalance(t, ctx, h.BobWallet, minted.Ref, 10,
		balanceTimeoutFor(minted.Ref))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(mintEvents) > 0 && len(sendEvents) > 0 &&
			len(recvEvents) > 0
	}, 2*time.Minute, time.Second, "expected events not delivered")

	mu.Lock()
	require.NoError(t, streamError)
	mu.Unlock()
}
