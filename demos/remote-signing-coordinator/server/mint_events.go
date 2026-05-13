package main

import (
	"context"
	"errors"
	"fmt"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

type mintEventSubscriber interface {
	SubscribeMintEvents(context.Context,
		*tapsdk.SubscribeMintEventsRequest) (
		<-chan *tapsdk.MintEvent, <-chan error, error)
}

type mintFinalityWatcher struct {
	cancel context.CancelFunc
	done   <-chan mintFinalityResult
}

type mintFinalityResult struct {
	event *tapsdk.MintEvent
	err   error
}

func (c *coordinator) startMintFinalityWatcher(ctx context.Context,
	id string) (*mintFinalityWatcher, error) {

	if c.events == nil {
		return nil, nil
	}

	subCtx, cancel := context.WithCancel(ctx)
	events, errs, err := c.events.SubscribeMintEvents(
		subCtx, &tapsdk.SubscribeMintEventsRequest{
			ShortResponse: true,
		},
	)
	if err != nil {
		cancel()
		return nil, err
	}

	done := make(chan mintFinalityResult, 1)
	watcher := &mintFinalityWatcher{
		cancel: cancel,
		done:   done,
	}

	go c.consumeMintEvents(subCtx, id, events, errs, done)

	return watcher, nil
}

func (w *mintFinalityWatcher) Stop() {
	if w == nil || w.cancel == nil {
		return
	}

	w.cancel()
}

func (w *mintFinalityWatcher) Wait(ctx context.Context) (
	*tapsdk.MintEvent, error) {

	if w == nil {
		return nil, nil
	}

	select {
	case result := <-w.done:
		return result.event, result.err

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *coordinator) consumeMintEvents(ctx context.Context, id string,
	events <-chan *tapsdk.MintEvent, errs <-chan error,
	done chan<- mintFinalityResult) {

	var tracked *tapsdk.PubKey

	for events != nil || errs != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}

			if event == nil || event.Batch == nil {
				if event != nil && event.Error != "" {
					done <- mintFinalityResult{
						err: errors.New(event.Error),
					}
					return
				}

				continue
			}

			if tracked == nil {
				key := event.Batch.BatchKey
				tracked = &key
			}
			if event.Batch.BatchKey != *tracked {
				continue
			}

			c.observeMintEvent(id, event)

			if event.Error != "" {
				done <- mintFinalityResult{
					err: fmt.Errorf("mint batch %s: %s",
						event.Batch.BatchKey, event.Error),
				}
				return
			}
			if event.BatchState == tapsdk.BatchStateFinalized {
				done <- mintFinalityResult{event: event}
				return
			}

		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err == nil || errors.Is(err, context.Canceled) {
				continue
			}

			done <- mintFinalityResult{err: err}
			return

		case <-ctx.Done():
			done <- mintFinalityResult{err: ctx.Err()}
			return
		}
	}

	done <- mintFinalityResult{
		err: errors.New("mint event stream closed before finalization"),
	}
}
