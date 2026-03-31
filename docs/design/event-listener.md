# Design: EventListener — High-Level Event Subscription API

**Author:** Toshi (tap-sdk maintainer)
**Date:** 2026-03-31
**Status:** Proposal
**Issue:** #41 (to be created)
**Related PRs:** #32 (low-level EventClient, merged)

## Problem

PR #32 added `EventClient`, a low-level wrapper around tapd's
server-streaming RPCs (`SubscribeReceiveEvents`, `SubscribeSendEvents`,
`SubscribeMintEvents`). It returns raw Go channels and mirrors the gRPC
stream lifecycle directly. This is appropriate for the `grpc` package
but presents several UX problems for SDK consumers:

1. **No automatic reconnection.** gRPC streams break on network blips,
   server restarts, or idle timeouts. The caller must detect the error,
   back off, and re-subscribe.

2. **Transport coupling.** The channel-based API assumes gRPC
   server-streaming. REST users would need WebSocket handling with a
   completely different pattern.

3. **Boilerplate.** Every consumer writes the same select/error/retry
   loop. The SDK should absorb this complexity.

4. **No lifecycle management.** Starting and stopping subscriptions
   requires careful goroutine and context management that is easy to get
   wrong.

## Prior Art

### lndclient (lightninglabs/lndclient)

Uses the same `(<-chan T, <-chan error, error)` pattern as our current
`EventClient`. No reconnection, no abstraction — consumers handle
retries. This is appropriate for lndclient because it is a low-level
client library, not a high-level SDK.

### LND internal subscriptions (subscribe.Client)

LND's internal `subscribe` package provides a `Client` with a
`Updates()` channel and `Cancel()`. No reconnection (not needed
in-process). Simple but tightly coupled to the LND server.

### taproot-assets itests

Integration tests subscribe in a goroutine, read from the channel, and
fail the test on error. No reconnection needed in test context.

### Kubernetes client-go Informers

A mature event-watching pattern: `Informer` registers event handlers
(`OnAdd`, `OnUpdate`, `OnDelete`), manages the watch stream internally
with automatic reconnection and resource versioning. The user never
touches the underlying HTTP/watch connection.

## Design

### Architecture

```
┌─────────────────────────────────────┐
│           SDK Consumer              │
│   registers EventHandler callbacks  │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│         EventListener               │
│  (tapsdk package, transport-agnostic│
│   reconnection, lifecycle mgmt)     │
└──────────────┬──────────────────────┘
               │ uses EventClient interface
       ┌───────┴────────┐
       ▼                ▼
┌─────────────┐  ┌─────────────┐
│ grpc.Client │  │ rest.Client │
│ (streams)   │  │ (websocket) │
└─────────────┘  └─────────────┘
```

### Interface

```go
package tapsdk

// EventHandler holds optional callbacks for asset events. Nil
// handlers are skipped — the listener will not subscribe to event
// types that have no handler registered.
type EventHandler struct {
	// OnReceive is called for each incoming asset transfer event.
	OnReceive func(ctx context.Context, event *entities.ReceiveEvent)

	// OnSend is called for each outgoing asset transfer event.
	OnSend func(ctx context.Context, event *entities.SendEvent)

	// OnMint is called for each minting batch lifecycle event.
	OnMint func(ctx context.Context, event *entities.MintEvent)

	// OnError is called when a stream encounters a non-recoverable
	// error after all retry attempts are exhausted. If nil, errors
	// are silently dropped and the stream is abandoned.
	OnError func(streamName string, err error)
}

// EventListenerConfig configures reconnection and subscription
// behavior.
type EventListenerConfig struct {
	// MaxRetries is the maximum number of consecutive reconnection
	// attempts per stream before calling OnError. Zero means
	// unlimited retries.
	MaxRetries int

	// InitialBackoff is the delay before the first reconnection
	// attempt. Defaults to 1 second.
	InitialBackoff time.Duration

	// MaxBackoff caps the exponential backoff. Defaults to 60
	// seconds.
	MaxBackoff time.Duration

	// BackoffMultiplier is the factor applied after each retry.
	// Defaults to 2.0.
	BackoffMultiplier float64

	// ReceiveFilter optionally restricts receive events.
	ReceiveFilter *entities.SubscribeReceiveEventsRequest

	// SendFilter optionally restricts send events.
	SendFilter *entities.SubscribeSendEventsRequest

	// MintFilter optionally restricts mint events.
	MintFilter *entities.SubscribeMintEventsRequest
}

// EventListener manages long-lived event subscriptions with automatic
// reconnection. It is transport-agnostic: it works identically over
// gRPC (server-streaming) and REST (WebSocket).
type EventListener interface {
	// Start begins listening for events. It launches background
	// goroutines for each registered handler. Start is non-blocking
	// and returns immediately after the initial subscription
	// succeeds. Returns an error only if the initial connection
	// fails.
	Start(ctx context.Context) error

	// Stop gracefully shuts down all subscriptions and waits for
	// goroutines to exit. Safe to call multiple times.
	Stop() error

	// Running reports whether the listener is currently active.
	Running() bool
}
```

### Construction

```go
// NewEventListener creates an EventListener backed by the given
// client. The client can be either a grpc.Client or rest.Client —
// both satisfy the EventClient interface.
func NewEventListener(
	client EventClient,
	handler EventHandler,
	opts ...EventListenerOption,
) EventListener
```

Functional options (`EventListenerOption`) configure
`EventListenerConfig` fields. Sensible defaults (1s initial backoff,
60s max, multiplier 2.0, unlimited retries) cover the common case:

```go
listener := tapsdk.NewEventListener(client, tapsdk.EventHandler{
	OnReceive: func(ctx context.Context, e *entities.ReceiveEvent) {
		log.Printf("received: %s status=%d", e.Outpoint, e.Status)
	},
	OnError: func(stream string, err error) {
		log.Printf("stream %s failed: %v", stream, err)
	},
})

err := listener.Start(ctx)
// ...
listener.Stop()
```

### Reconnection Strategy

Each stream (receive, send, mint) runs in its own goroutine with
independent reconnection:

```
loop:
  1. Call EventClient.Subscribe*(ctx, filter)
  2. Read from event channel
  3. On event: reset retry counter, call handler
  4. On error from errCh:
     a. If ctx cancelled → exit
     b. If retries exhausted → call OnError, exit
     c. Sleep(backoff), multiply backoff, increment retries
     d. goto 1
```

Key behaviors:
- **Successful event delivery resets the retry counter.** A stream that
  reconnects and delivers one event is considered healthy.
- **Context cancellation is immediate.** No sleeping through a
  cancelled context.
- **Jitter.** Add ±20% jitter to backoff to avoid thundering herd
  when multiple listeners reconnect simultaneously.

### REST / WebSocket Support

The `EventClient` interface (from `clients.go`) is the abstraction
boundary. For REST, the implementation will:

1. Open a WebSocket connection to tapd's grpc-gateway endpoint.
   grpc-gateway exposes server-streaming RPCs as WebSocket at the same
   HTTP path. For example:
   `wss://host:8089/v1/taproot-assets/events/receive`

2. Read JSON-encoded events from the WebSocket frames.

3. Unmarshal into the same `entities.*Event` types.

4. Return `(<-chan *entities.ReceiveEvent, <-chan error, error)` — the
   same signature as the gRPC implementation.

The `EventListener` never knows or cares which transport is underneath.

### Thread Safety

- `Start()` and `Stop()` are protected by a mutex.
- Handlers may be called concurrently (one goroutine per stream).
- Handlers must not block indefinitely — document this clearly.

### Error Classification

Not all errors should trigger reconnection:
- **Retriable:** `io.EOF`, gRPC `Unavailable`, `DeadlineExceeded`,
  connection reset, WebSocket close 1006 (abnormal).
- **Fatal:** `PermissionDenied`, `Unauthenticated`, invalid macaroon,
  `Unimplemented` (tapd doesn't support the RPC).

Fatal errors call `OnError` immediately without retrying.

## Alternatives Considered

### Channel-only API (current EventClient)

Pros: Simple, idiomatic Go, zero allocation overhead.
Cons: No reconnection, no transport abstraction, boilerplate.
Decision: Keep as the low-level API in `grpc` package. The
`EventListener` builds on top.

### Reactive/Observable pattern

Pros: Composable stream operators (filter, map, buffer).
Cons: Adds a reactive library dependency, unfamiliar to most Go
developers, over-engineered for three event types.
Decision: Not worth the complexity.

### Single merged event channel

Return all event types through one `<-chan Event` with a type switch.
Pros: Simpler for consumers who want all events.
Cons: Loses type safety, awkward filtering.
Decision: Keep separate handlers. Can always add a merged helper
later.

## Implementation Plan

1. **Define `EventListener` interface and config** in `tapsdk`
   package.
2. **Implement `eventListener` struct** with reconnection logic,
   backed by `EventClient`.
3. **Add unit tests** using a mock `EventClient` that simulates
   stream breaks and reconnection.
4. **Add REST WebSocket `EventClient`** in `rest` package
   (can be a follow-up PR).
5. **Add integration test** for event listener in the regtest suite.
6. **Document** in architecture.md and examples/.

## Open Questions

1. Should the listener expose per-stream health status (e.g.,
   `StreamStatus(name string) (connected bool, lastEvent time.Time)`)?
   Useful for monitoring but adds API surface.

2. Should we support event buffering / replay from a timestamp?
   tapd's `StartTimestamp` filter enables this, but the listener would
   need to track the last-seen timestamp per stream. Useful for
   crash recovery scenarios.

3. Should `OnReceive` etc. return an error to signal the listener to
   stop? Currently they return nothing (fire-and-forget). Returning
   an error could allow consumer-initiated shutdown on processing
   failure.
