package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/mintrpc"
	"google.golang.org/grpc"
)

type eventClient struct {
	taprpcClient taprpc.TaprootAssetsClient
	mintClient   mintrpc.MintClient
	timeout      time.Duration
	adminMac     macaroon.SerializedMacaroon
	mintMac      macaroon.SerializedMacaroon
}

// NewEventClient creates a new event subscription client.
func NewEventClient(conn grpc.ClientConnInterface,
	timeout time.Duration,
	adminMac macaroon.SerializedMacaroon,
	mintMac macaroon.SerializedMacaroon) *eventClient {

	return &eventClient{
		taprpcClient: taprpc.NewTaprootAssetsClient(conn),
		mintClient:   mintrpc.NewMintClient(conn),
		timeout:      timeout,
		adminMac:     adminMac,
		mintMac:      mintMac,
	}
}

// SubscribeReceiveEvents opens a server-streaming subscription for
// incoming asset transfer events. It returns a channel of events and
// an error channel. The error channel receives exactly one value when
// the stream terminates.
func (c *eventClient) SubscribeReceiveEvents(ctx context.Context,
	req *entities.SubscribeReceiveEventsRequest) (
	<-chan *entities.ReceiveEventRecord, <-chan error, error) {

	rpcCtx := c.adminMac.WithMacaroonAuth(ctx)

	rpcReq := &taprpc.SubscribeReceiveEventsRequest{
		FilterAddr:     req.FilterAddr,
		StartTimestamp: req.StartTimestamp,
	}

	stream, err := c.taprpcClient.SubscribeReceiveEvents(
		rpcCtx, rpcReq,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("subscribe receive "+
			"events: %w", err)
	}

	eventCh := make(chan *entities.ReceiveEventRecord)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		for {
			rpcEvent, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}

			event, err := unmarshalReceiveEvent(rpcEvent)
			if err != nil {
				errCh <- fmt.Errorf("unmarshal "+
					"receive event: %w", err)
				return
			}

			select {
			case eventCh <- event:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	return eventCh, errCh, nil
}

// SubscribeSendEvents opens a server-streaming subscription for
// outgoing asset transfer events.
func (c *eventClient) SubscribeSendEvents(ctx context.Context,
	req *entities.SubscribeSendEventsRequest) (
	<-chan *entities.SendEventRecord, <-chan error, error) {

	rpcCtx := c.adminMac.WithMacaroonAuth(ctx)

	rpcReq := &taprpc.SubscribeSendEventsRequest{
		FilterScriptKey: req.FilterScriptKey,
		FilterLabel:     req.FilterLabel,
		StartTimestamp:  req.StartTimestamp,
	}

	stream, err := c.taprpcClient.SubscribeSendEvents(
		rpcCtx, rpcReq,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("subscribe send "+
			"events: %w", err)
	}

	eventCh := make(chan *entities.SendEventRecord)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		for {
			rpcEvent, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}

			event, err := unmarshalSendEvent(rpcEvent)
			if err != nil {
				errCh <- fmt.Errorf("unmarshal "+
					"send event: %w", err)
				return
			}

			select {
			case eventCh <- event:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	return eventCh, errCh, nil
}

// SubscribeMintEvents opens a server-streaming subscription for
// minting batch lifecycle events.
func (c *eventClient) SubscribeMintEvents(ctx context.Context,
	req *entities.SubscribeMintEventsRequest) (
	<-chan *entities.MintEvent, <-chan error, error) {

	rpcCtx := c.mintMac.WithMacaroonAuth(ctx)

	rpcReq := &mintrpc.SubscribeMintEventsRequest{
		ShortResponse: req.ShortResponse,
	}

	stream, err := c.mintClient.SubscribeMintEvents(
		rpcCtx, rpcReq,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("subscribe mint "+
			"events: %w", err)
	}

	eventCh := make(chan *entities.MintEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		for {
			rpcEvent, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}

			event, err := unmarshalMintEvent(rpcEvent)
			if err != nil {
				errCh <- fmt.Errorf("unmarshal "+
					"mint event: %w", err)
				return
			}

			select {
			case eventCh <- event:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	return eventCh, errCh, nil
}
