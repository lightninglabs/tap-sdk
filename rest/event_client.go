package rest

import (
	"context"

	"github.com/lightninglabs/tap-sdk/entities"
)

// NOTE: Event subscriptions over REST require WebSocket support, which
// is planned but not yet implemented. These stubs satisfy the
// tapsdk.EventClient interface so the REST Client compiles against
// the full tapsdk.Client interface. A future PR will implement
// WebSocket-based streaming for REST.

// SubscribeReceiveEvents is not yet implemented for REST.
func (c *Client) SubscribeReceiveEvents(_ context.Context,
	_ *entities.SubscribeReceiveEventsRequest) (
	<-chan *entities.ReceiveEvent, <-chan error, error) {

	return nil, nil, errNotImplemented(
		"SubscribeReceiveEvents (requires WebSocket)",
	)
}

// SubscribeSendEvents is not yet implemented for REST.
func (c *Client) SubscribeSendEvents(_ context.Context,
	_ *entities.SubscribeSendEventsRequest) (
	<-chan *entities.SendEvent, <-chan error, error) {

	return nil, nil, errNotImplemented(
		"SubscribeSendEvents (requires WebSocket)",
	)
}

// SubscribeMintEvents is not yet implemented for REST.
func (c *Client) SubscribeMintEvents(_ context.Context,
	_ *entities.SubscribeMintEventsRequest) (
	<-chan *entities.MintEvent, <-chan error, error) {

	return nil, nil, errNotImplemented(
		"SubscribeMintEvents (requires WebSocket)",
	)
}
