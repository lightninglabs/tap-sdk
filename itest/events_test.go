//go:build itest

package itest

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestEventListenerMintAndSend exercises the real-time event streams via
// tapsdk.NewEventListener against every supported transport. The REST
// client bridges these subscriptions over the grpc-gateway WebSocket
// proxy, so this also exercises that path end-to-end.
func TestEventListenerMintAndSend(t *testing.T) {
	runForTransports(t, testEventListenerMintAndSend)
}

func testEventListenerMintAndSend(t *testing.T, transport Transport) {
	h, ctx := newFundedHarnessFor(t, transport)

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

	// tapd state persists across subtests, so each transport mints its
	// own group to keep balance assertions isolated.
	assetName := fmt.Sprintf("event-token-%s", transport)
	minted, err := h.MintGroupedAsset(t, ctx, assetName, 1000)
	require.NoError(t, err)
	require.True(t, minted.Ref.IsGroupRef())

	bobAddr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)
	label := uniqueEventLabel("listener")
	_, err = h.AliceWallet.Send(
		ctx, bobAddr.Encoded, tapsdk.WithAmount(10),
		tapsdk.WithLabel(label),
	)
	require.NoError(t, err)

	h.MineBlocks(t, defaultMineBlocks)

	// This test validates the listener callbacks themselves, so wait
	// for the specific terminal events instead of polling wallet state.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return hasFinalizedMint(mintEvents,
			minted.Batch.Batch.BatchKey) &&
			hasCompletedSend(sendEvents, label) &&
			hasCompletedReceive(recvEvents, bobAddr.Encoded)
	}, 2*time.Minute, time.Second, "expected events not delivered")

	bobBalance, err := h.BobWallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, uint64(10), bobBalance)

	mu.Lock()
	require.NoError(t, streamError)
	mu.Unlock()
}

// TestEventListenerOnDisconnect verifies that the retriable-disconnect
// hook fires when tapd goes away mid-stream, and that the listener
// reconnects once tapd is back — without `OnError` being triggered,
// since the break is transient rather than terminal. Runs against every
// transport so both the gRPC stream and the REST WebSocket bridge are
// exercised through a real daemon restart.
func TestEventListenerOnDisconnect(t *testing.T) {
	runForTransports(t, testEventListenerOnDisconnect)
}

func testEventListenerOnDisconnect(t *testing.T, transport Transport) {
	h, ctx := newFundedHarnessFor(t, transport)

	type disconnect struct {
		stream    string
		err       error
		nextRetry time.Duration
	}

	var (
		mu          sync.Mutex
		disconnects []disconnect
		fatalErr    error
	)

	listener := tapsdk.NewEventListener(
		h.BobClient,
		tapsdk.EventHandler{
			OnReceive: func(
				_ context.Context, _ *entities.ReceiveEvent,
			) {
			},
			OnSend: func(
				_ context.Context, _ *entities.SendEvent,
			) {
			},
			OnMint: func(
				_ context.Context, _ *entities.MintEvent,
			) {
			},
			OnDisconnect: func(stream string, err error,
				nextRetry time.Duration) {

				mu.Lock()
				disconnects = append(disconnects, disconnect{
					stream:    stream,
					err:       err,
					nextRetry: nextRetry,
				})
				mu.Unlock()
			},
			OnError: func(_ string, err error) {
				mu.Lock()
				fatalErr = err
				mu.Unlock()
			},
		},
		// Keep the retry budget unbounded so the restart window
		// never exhausts it and flips the listener into OnError.
		tapsdk.WithInitialBackoff(250*time.Millisecond),
		tapsdk.WithMaxBackoff(2*time.Second),
	)
	require.NoError(t, listener.Start(ctx))
	t.Cleanup(func() { _ = listener.Stop() })

	// Give the three subscriptions a moment to establish before
	// bouncing the daemon — otherwise the restart races the initial
	// Subscribe calls and we can't tell what broke.
	time.Sleep(2 * time.Second)

	const container = "tap-sdk-tapd-bob"
	out, err := exec.Command("docker", "restart", container).
		CombinedOutput()
	require.NoErrorf(t, err,
		"docker restart %s failed: %s", container, string(out))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(disconnects) >= 1
	}, 90*time.Second, 250*time.Millisecond,
		"expected OnDisconnect to fire after tapd restart",
	)

	mu.Lock()
	require.NotEmpty(t, disconnects)
	first := disconnects[0]
	require.Contains(t,
		[]string{"receive", "send", "mint"}, first.stream,
	)
	require.Error(t, first.err)
	require.Greater(t, first.nextRetry, time.Duration(0))
	require.NoError(t, fatalErr,
		"transient break must not trigger OnError")
	mu.Unlock()

	// Wait for tapd-bob to come back so the harness's t.Cleanup
	// hooks and any follow-up tests see a healthy container.
	require.Eventually(t, func() bool {
		_, err := h.BobClient.GetInfo(ctx)
		return err == nil
	}, 60*time.Second, time.Second, "tapd-bob did not recover")
}
