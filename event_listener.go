package tapsdk

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// defaultInitialBackoff is the default delay before the first
	// reconnection attempt.
	defaultInitialBackoff = 1 * time.Second

	// defaultMaxBackoff caps the exponential backoff.
	defaultMaxBackoff = 60 * time.Second

	// defaultBackoffMultiplier is the factor applied after each
	// retry.
	defaultBackoffMultiplier = 2.0

	// jitterFraction is the ±fraction of the backoff added as
	// jitter.
	jitterFraction = 0.2
)

// EventHandler holds optional callbacks for asset events. Nil handlers
// are skipped — the listener will not subscribe to event types that
// have no handler registered.
type EventHandler struct {
	// OnReceive is called for each incoming asset transfer event.
	OnReceive func(ctx context.Context, event *entities.ReceiveEvent)

	// OnSend is called for each outgoing asset transfer event.
	OnSend func(ctx context.Context, event *entities.SendEvent)

	// OnMint is called for each minting batch lifecycle event.
	OnMint func(ctx context.Context, event *entities.MintEvent)

	// OnError is called when a stream encounters a non-recoverable
	// error after all retry attempts are exhausted, or when a fatal
	// (non-retriable) error occurs. If nil, errors are silently
	// dropped and the stream is abandoned.
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

// EventListenerOption is a functional option for configuring
// EventListenerConfig.
type EventListenerOption func(*EventListenerConfig)

// WithMaxRetries sets the maximum consecutive reconnection attempts.
func WithMaxRetries(n int) EventListenerOption {
	return func(c *EventListenerConfig) {
		c.MaxRetries = n
	}
}

// WithInitialBackoff sets the initial reconnection backoff duration.
func WithInitialBackoff(d time.Duration) EventListenerOption {
	return func(c *EventListenerConfig) {
		c.InitialBackoff = d
	}
}

// WithMaxBackoff sets the maximum backoff duration.
func WithMaxBackoff(d time.Duration) EventListenerOption {
	return func(c *EventListenerConfig) {
		c.MaxBackoff = d
	}
}

// WithBackoffMultiplier sets the backoff growth factor.
func WithBackoffMultiplier(m float64) EventListenerOption {
	return func(c *EventListenerConfig) {
		c.BackoffMultiplier = m
	}
}

// WithReceiveFilter sets the receive event subscription filter.
func WithReceiveFilter(
	f *entities.SubscribeReceiveEventsRequest) EventListenerOption {

	return func(c *EventListenerConfig) {
		c.ReceiveFilter = f
	}
}

// WithSendFilter sets the send event subscription filter.
func WithSendFilter(
	f *entities.SubscribeSendEventsRequest) EventListenerOption {

	return func(c *EventListenerConfig) {
		c.SendFilter = f
	}
}

// WithMintFilter sets the mint event subscription filter.
func WithMintFilter(
	f *entities.SubscribeMintEventsRequest) EventListenerOption {

	return func(c *EventListenerConfig) {
		c.MintFilter = f
	}
}

// eventListener implements the EventListener with automatic
// reconnection.
type eventListener struct {
	client  EventClient
	handler EventHandler
	config  EventListenerConfig

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewEventListener creates an EventListener backed by the given
// client. The client can be either a grpc.Client or rest.Client —
// both satisfy the EventClient interface.
func NewEventListener(
	client EventClient,
	handler EventHandler,
	opts ...EventListenerOption,
) *eventListener {

	cfg := EventListenerConfig{
		InitialBackoff:    defaultInitialBackoff,
		MaxBackoff:        defaultMaxBackoff,
		BackoffMultiplier: defaultBackoffMultiplier,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &eventListener{
		client:  client,
		handler: handler,
		config:  cfg,
	}
}

// Start begins listening for events. It launches background goroutines
// for each registered handler. Start is non-blocking and returns
// immediately. Returns an error if the listener is already running.
func (l *eventListener) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return errors.New("event listener already running")
	}

	ctx, l.cancel = context.WithCancel(ctx)
	l.running = true

	if l.handler.OnReceive != nil {
		l.wg.Add(1)
		go l.runStream(ctx, "receive", l.subscribeReceive)
	}

	if l.handler.OnSend != nil {
		l.wg.Add(1)
		go l.runStream(ctx, "send", l.subscribeSend)
	}

	if l.handler.OnMint != nil {
		l.wg.Add(1)
		go l.runStream(ctx, "mint", l.subscribeMint)
	}

	return nil
}

// Stop gracefully shuts down all subscriptions and waits for goroutines
// to exit. Safe to call multiple times.
func (l *eventListener) Stop() error {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.cancel()
	l.mu.Unlock()

	l.wg.Wait()

	l.mu.Lock()
	l.running = false
	l.mu.Unlock()

	return nil
}

// Running reports whether the listener is currently active.
func (l *eventListener) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

// streamFunc is a function that opens a subscription and blocks until
// the stream ends or ctx is cancelled. It returns the terminal error.
type streamFunc func(ctx context.Context) error

// runStream manages reconnection for a single event stream.
func (l *eventListener) runStream(ctx context.Context, name string,
	subscribe streamFunc) {

	defer l.wg.Done()

	backoff := l.config.InitialBackoff
	retries := 0

	for {
		err := subscribe(ctx)

		// Context cancelled: clean shutdown.
		if ctx.Err() != nil {
			return
		}

		// Check if the error is fatal (non-retriable).
		if isFatalError(err) {
			if l.handler.OnError != nil {
				l.handler.OnError(name, err)
			}
			return
		}

		retries++

		// Check retry limit.
		if l.config.MaxRetries > 0 &&
			retries > l.config.MaxRetries {

			if l.handler.OnError != nil {
				l.handler.OnError(name, err)
			}
			return
		}

		// Sleep with jitter.
		jittered := addJitter(backoff)
		select {
		case <-time.After(jittered):
		case <-ctx.Done():
			return
		}

		// Grow backoff.
		backoff = time.Duration(
			float64(backoff) * l.config.BackoffMultiplier,
		)
		if backoff > l.config.MaxBackoff {
			backoff = l.config.MaxBackoff
		}
	}
}

// subscribeReceive opens a receive event stream and delivers events
// to the handler until the stream breaks.
func (l *eventListener) subscribeReceive(ctx context.Context) error {
	filter := l.config.ReceiveFilter
	if filter == nil {
		filter = &entities.SubscribeReceiveEventsRequest{}
	}

	eventCh, errCh, err := l.client.SubscribeReceiveEvents(
		ctx, filter,
	)
	if err != nil {
		return err
	}

	return drainEvents(ctx, eventCh, errCh,
		func(e *entities.ReceiveEvent) {
			l.handler.OnReceive(ctx, e)
		},
	)
}

// subscribeSend opens a send event stream and delivers events.
func (l *eventListener) subscribeSend(ctx context.Context) error {
	filter := l.config.SendFilter
	if filter == nil {
		filter = &entities.SubscribeSendEventsRequest{}
	}

	eventCh, errCh, err := l.client.SubscribeSendEvents(
		ctx, filter,
	)
	if err != nil {
		return err
	}

	return drainEvents(ctx, eventCh, errCh,
		func(e *entities.SendEvent) {
			l.handler.OnSend(ctx, e)
		},
	)
}

// subscribeMint opens a mint event stream and delivers events.
func (l *eventListener) subscribeMint(ctx context.Context) error {
	filter := l.config.MintFilter
	if filter == nil {
		filter = &entities.SubscribeMintEventsRequest{}
	}

	eventCh, errCh, err := l.client.SubscribeMintEvents(
		ctx, filter,
	)
	if err != nil {
		return err
	}

	return drainEvents(ctx, eventCh, errCh,
		func(e *entities.MintEvent) {
			l.handler.OnMint(ctx, e)
		},
	)
}

// drainEvents reads from an event channel and calls handler for each
// event. Returns the terminal error from errCh.
func drainEvents[T any](ctx context.Context,
	eventCh <-chan *T, errCh <-chan error,
	handler func(*T)) error {

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed, read terminal error.
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
			handler(event)

		case err := <-errCh:
			return err

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// isFatalError returns true if the error should not be retried.
func isFatalError(err error) bool {
	if err == nil {
		return false
	}

	// gRPC status codes that are fatal.
	s, ok := status.FromError(err)
	if ok {
		switch s.Code() {
		case codes.PermissionDenied,
			codes.Unauthenticated,
			codes.Unimplemented,
			codes.InvalidArgument:

			return true
		}
	}

	return false
}

// isRetriableError returns true if the error can be retried with a
// reconnection.
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	// io.EOF means the stream ended normally (server closed it).
	if errors.Is(err, io.EOF) {
		return true
	}

	// gRPC retriable codes.
	s, ok := status.FromError(err)
	if ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded,
			codes.Aborted, codes.Internal,
			codes.ResourceExhausted:

			return true
		}
	}

	// Default: retry unknown errors.
	return !isFatalError(err)
}

// addJitter adds ±jitterFraction random jitter to a duration.
func addJitter(d time.Duration) time.Duration {
	jitter := float64(d) * jitterFraction
	offset := (rand.Float64()*2 - 1) * jitter

	return time.Duration(math.Max(
		float64(d)+offset,
		float64(time.Millisecond),
	))
}
