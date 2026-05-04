//go:build itest

package itest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// eventSubscription is the test-side handle to a tapd event stream.
// events is intentionally unbuffered: the gRPC and REST client drainers
// block on send, which forces the matched terminal event to be consumed
// by waitForEvent BEFORE the close-and-error sequence can race in. Do
// not add a buffer here without revisiting failClosedEventStream.
type eventSubscription[T any] struct {
	events <-chan *T
	errs   <-chan error
	cancel context.CancelFunc
}

func (s *eventSubscription[T]) Stop() {
	s.cancel()
}

// mintSubscriber is the minimal client surface needed to open a mint
// stream. Both grpc.Client and rest.Client satisfy it, so harness tests
// can subscribe on either Alice or Bob.
type mintSubscriber interface {
	SubscribeMintEvents(context.Context,
		*entities.SubscribeMintEventsRequest) (
		<-chan *entities.MintEvent, <-chan error, error)
}

// receiveSubscriber is the minimal client surface needed to open a
// receive stream.
type receiveSubscriber interface {
	SubscribeReceiveEvents(context.Context,
		*entities.SubscribeReceiveEventsRequest) (
		<-chan *entities.ReceiveEventRecord, <-chan error, error)
}

// sendSubscriber is the minimal client surface needed to open a send
// stream.
type sendSubscriber interface {
	SubscribeSendEvents(context.Context,
		*entities.SubscribeSendEventsRequest) (
		<-chan *entities.SendEventRecord, <-chan error, error)
}

func (h *TestHarness) subscribeMintEvents(t testing.TB,
	ctx context.Context,
	client mintSubscriber) *eventSubscription[entities.MintEvent] {

	t.Helper()

	subCtx, cancel := context.WithCancel(ctx)
	events, errs, err := client.SubscribeMintEvents(
		subCtx, &entities.SubscribeMintEventsRequest{},
	)
	require.NoError(t, err)

	sub := &eventSubscription[entities.MintEvent]{
		events: events,
		errs:   errs,
		cancel: cancel,
	}
	t.Cleanup(sub.Stop)

	return sub
}

// subscribeReceiveEvents opens a receive event stream on the given
// client. Pass the wallet that will receive the asset.
//
// StartTimestamp is left at 0 deliberately: tapd's handleEvents
// hardcodes deliverExisting=false, so SubscribeReceiveEvents currently
// ignores StartTimestamp and never replays historical events. Callers
// must subscribe before the sender broadcasts.
func (h *TestHarness) subscribeReceiveEvents(t testing.TB,
	ctx context.Context, client receiveSubscriber,
	addr string) *eventSubscription[entities.ReceiveEventRecord] {

	t.Helper()

	subCtx, cancel := context.WithCancel(ctx)
	events, errs, err := client.SubscribeReceiveEvents(
		subCtx, &entities.SubscribeReceiveEventsRequest{
			FilterAddr: addr,
		},
	)
	require.NoError(t, err)

	sub := &eventSubscription[entities.ReceiveEventRecord]{
		events: events,
		errs:   errs,
		cancel: cancel,
	}
	t.Cleanup(sub.Stop)

	return sub
}

// subscribeSendEvents opens a send event stream on the given client.
// Pass the wallet that issued the send.
//
// startTimestamp is honored by tapd: SubscribeSendEvents replays
// completed parcels (anchor-confirmed, filtered by label) from the DB
// before joining the live stream, so the test can subscribe AFTER
// calling Send and still observe the terminal SendStateComplete event.
func (h *TestHarness) subscribeSendEvents(t testing.TB,
	ctx context.Context, client sendSubscriber, label string,
	startTimestamp int64) *eventSubscription[entities.SendEventRecord] {

	t.Helper()

	subCtx, cancel := context.WithCancel(ctx)
	events, errs, err := client.SubscribeSendEvents(
		subCtx, &entities.SubscribeSendEventsRequest{
			FilterLabel:    label,
			StartTimestamp: startTimestamp,
		},
	)
	require.NoError(t, err)

	sub := &eventSubscription[entities.SendEventRecord]{
		events: events,
		errs:   errs,
		cancel: cancel,
	}
	t.Cleanup(sub.Stop)

	return sub
}

func waitForEvent[T any](t testing.TB, sub *eventSubscription[T],
	timeout time.Duration, desc string,
	matches func(*T) (bool, string)) *T {

	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var lastObservation string
	for {
		select {
		case event, ok := <-sub.events:
			if !ok {
				failClosedEventStream(t, sub, desc, lastObservation)
			}

			matched, observation := matches(event)
			if observation != "" {
				lastObservation = observation
			}
			if matched {
				return event
			}

		case err, ok := <-sub.errs:
			if ok && err != nil {
				require.NoErrorf(t, err, "%s stream error", desc)
			}

			require.FailNowf(
				t, "event stream closed",
				"%s before match; last observation: %s",
				desc, lastObservation,
			)

		case <-timer.C:
			require.FailNowf(
				t, "timed out waiting for event",
				"%s; last observation: %s",
				desc, lastObservation,
			)
		}
	}
}

func failClosedEventStream[T any](t testing.TB, sub *eventSubscription[T],
	desc, lastObservation string) {

	t.Helper()

	select {
	case err, ok := <-sub.errs:
		if ok && err != nil {
			require.NoErrorf(t, err, "%s stream error", desc)
		}
	default:
	}

	require.FailNowf(
		t, "event stream closed",
		"%s before match; last observation: %s",
		desc, lastObservation,
	)
}

func waitForMintFinalized(t testing.TB,
	sub *eventSubscription[entities.MintEvent], batchKey entities.PubKey,
	timeout time.Duration) *entities.MintEvent {

	t.Helper()

	return waitForEvent(t, sub, timeout, "mint batch finalized",
		func(event *entities.MintEvent) (bool, string) {
			if event == nil || event.Batch == nil {
				return false, "nil mint event or batch"
			}

			if event.Batch.BatchKey != batchKey {
				return false, fmt.Sprintf(
					"unrelated mint batch %x at state %d",
					event.Batch.BatchKey, event.BatchState,
				)
			}

			require.Emptyf(t, event.Error,
				"mint event for batch %x failed", batchKey)

			observation := fmt.Sprintf(
				"batch %x at state %d", batchKey,
				event.BatchState,
			)

			return event.BatchState == entities.BatchStateFinalized,
				observation
		},
	)
}

func waitForReceiveCompleted(t testing.TB,
	sub *eventSubscription[entities.ReceiveEventRecord], addr string,
	timeout time.Duration) *entities.ReceiveEventRecord {

	t.Helper()

	return waitForEvent(t, sub, timeout, "receive completed",
		func(event *entities.ReceiveEventRecord) (bool, string) {
			if event == nil {
				return false, "nil receive event"
			}

			require.Emptyf(t, event.Error,
				"receive event for %s failed", addr)

			eventAddr := ""
			if event.Address != nil {
				eventAddr = event.Address.Encoded
			}

			observation := fmt.Sprintf(
				"receive addr=%s status=%d", eventAddr,
				event.Status,
			)

			return eventAddr == addr &&
					event.Status ==
						entities.AddressEventStatusCompleted,
				observation
		},
	)
}

func waitForSendCompleted(t testing.TB,
	sub *eventSubscription[entities.SendEventRecord], label string,
	timeout time.Duration) *entities.SendEventRecord {

	t.Helper()

	return waitForEvent(t, sub, timeout, "send completed",
		func(event *entities.SendEventRecord) (bool, string) {
			if event == nil {
				return false, "nil send event"
			}

			if event.TransferLabel != label {
				return false, fmt.Sprintf(
					"unrelated send label=%s state=%s",
					event.TransferLabel, event.SendState,
				)
			}

			require.Emptyf(t, event.Error,
				"send event for label %s failed", label)

			observation := fmt.Sprintf(
				"send label=%s state=%s next=%s",
				event.TransferLabel, event.SendState,
				event.NextSendState,
			)

			return event.SendState == entities.SendStateComplete,
				observation
		},
	)
}

// The listener API delivers every matching event via callbacks and does
// not expose terminal-state filters, so the callback-based itest keeps
// the observed events in memory and checks for the specific terminal
// status it cares about. Events that reached the terminal state but
// also carry an Error are skipped — they represent a failed final
// transition, not a successful completion.
func hasFinalizedMint(events []*entities.MintEvent,
	batchKey entities.PubKey) bool {

	for _, event := range events {
		if event == nil || event.Batch == nil || event.Error != "" {
			continue
		}

		if event.Batch.BatchKey == batchKey &&
			event.BatchState == entities.BatchStateFinalized {

			return true
		}
	}

	return false
}

func hasCompletedSend(events []*entities.SendEvent, label string) bool {
	return findCompletedSend(events, label) != nil
}

func findCompletedSend(events []*entities.SendEvent,
	label string) *entities.SendEvent {

	for _, event := range events {
		if event == nil || event.Error != "" {
			continue
		}

		if event.TransferLabel == label &&
			event.SendState == entities.SendStateComplete {

			return event
		}
	}

	return nil
}

// hasCompletedReceive checks whether any of the high-level receive events
// emitted by EventListener match the given AssetRef and reached the
// completed status. The post-projection event no longer carries the raw
// address string, so callers identify the receive by its user-facing
// AssetRef instead.
func hasCompletedReceive(events []*entities.ReceiveEvent,
	ref entities.AssetRef) bool {

	return findCompletedReceive(events, ref) != nil
}

func findCompletedReceive(events []*entities.ReceiveEvent,
	ref entities.AssetRef) *entities.ReceiveEvent {

	for _, event := range events {
		if event == nil || event.Error != "" {
			continue
		}

		if event.AssetRef.Equivalent(ref) &&
			event.Status == entities.AddressEventStatusCompleted {

			return event
		}
	}

	return nil
}

func uniqueEventLabel(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func eventStartTimestamp() int64 {
	// Give tapd's timestamp filter a small cushion so a send that
	// starts in the same wall-clock instant is still replayable.
	return time.Now().Add(-time.Second).UnixMicro()
}
