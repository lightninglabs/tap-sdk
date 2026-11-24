package embedded

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"golang.org/x/sync/singleflight"
)

const (
	// mailboxStartMargin is how many blocks below the creation height of
	// an address its mailbox is read from.
	mailboxStartMargin = 6

	// maxProofFetches caps the receives whose proofs are fetched at the
	// same time. A sender chooses how many proofs a receive needs.
	maxProofFetches = 4

	// maxReceiveAttempts caps how often a client works on one receive on
	// its own. Anyone who knows an address can announce a transfer whose
	// proofs never reach the courier.
	maxReceiveAttempts = 12

	// challengeSize is the size of a mailbox challenge.
	challengeSize = 32
)

var (
	// mailboxQuiet is how long a mailbox must stay silent before one
	// pass over it ends. A courier does not mark the end of its stored
	// messages.
	mailboxQuiet = 2 * time.Second

	// receiveRetryInterval is how long a client waits before it works
	// on a receive again, doubled after every attempt. The first wait is
	// short, since a sender may announce a transfer just before it
	// uploads the proofs.
	receiveRetryInterval = 5 * time.Second
)

// receiver receives the transfers to the wallet's addresses.
type receiver struct {
	e    *engine
	keys ReceiveKeys
	dial CourierDialer

	// ctx ends when the engine stops.
	ctx    context.Context
	cancel context.CancelFunc

	// fetches holds one token per receive that fetches proofs.
	fetches chan struct{}

	// runs shares one run of advance among the callers that work on a
	// receive at the same time.
	runs singleflight.Group

	// syncMu serializes the passes of sync, which close the couriers
	// they dialed.
	syncMu sync.Mutex

	mu sync.Mutex

	// live is set once the client holds the mailboxes open.
	live bool

	// subscribed are the addresses whose mailbox is held open.
	subscribed map[[33]byte]struct{}

	// jobs are the receives the client has worked on by itself.
	jobs map[receiveID]*receiveJob

	// courierMu guards couriers, so that a slow dial blocks no other
	// state.
	courierMu sync.Mutex

	// couriers are the open couriers by address.
	couriers map[string]Courier
}

// receiveJob is the work of a client on one receive.
type receiveJob struct {
	attempts int
	running  bool
}

// newReceiver returns the receiver of an engine with addresses.
func newReceiver(e *engine, cfg *ReceiveConfig) *receiver {
	ctx, cancel := context.WithCancel(context.Background())

	return &receiver{
		e:          e,
		keys:       cfg.Keys,
		dial:       cfg.Dial,
		ctx:        ctx,
		cancel:     cancel,
		fetches:    make(chan struct{}, maxProofFetches),
		subscribed: make(map[[33]byte]struct{}),
		jobs:       make(map[receiveID]*receiveJob),
		couriers:   make(map[string]Courier),
	}
}

// resume holds the mailbox of every stored address open and works on the
// receives an earlier run left unfinished.
func (r *receiver) resume(ctx context.Context) error {
	addrs, err := r.e.addrs.ListAddresses(ctx)
	if err != nil {
		return fmt.Errorf("list addresses: %w", err)
	}
	unfinished, err := r.unfinished(ctx)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.live = true
	r.mu.Unlock()

	for idx := range addrs {
		r.subscribe(addrs[idx])
	}
	for _, id := range unfinished {
		r.follow(id)
	}

	return nil
}

// unfinished returns the receives that are neither confirmed nor failed.
func (r *receiver) unfinished(ctx context.Context) ([]receiveID, error) {
	detected := ReceiveDetected
	records, err := r.e.addrs.ListReceives(ctx, ReceiveFilter{
		State: &detected,
	})
	if err != nil {
		return nil, fmt.Errorf("list receives: %w", err)
	}

	ids := make([]receiveID, len(records))
	for idx := range records {
		ids[idx] = idOf(&records[idx])
	}

	return ids, nil
}

// subscribe holds the mailbox of an address open in the background, once the
// client is live. Subscribing an address twice has no effect.
func (r *receiver) subscribe(record AddressRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.live || r.ctx.Err() != nil {
		return
	}
	if _, ok := r.subscribed[record.ScriptKey]; ok {
		return
	}
	r.subscribed[record.ScriptKey] = struct{}{}

	r.e.wg.Go(func() {
		for {
			err := r.readMailbox(r.ctx, record, false)
			if r.ctx.Err() != nil {
				return
			}
			r.e.log.Errorf("Reading the mailbox of address %x: %v",
				record.ScriptKey[:], err)

			select {
			case <-time.After(retryInterval):
			case <-r.ctx.Done():
				return
			}
		}
	})
}

// follow works on a receive in the background until it is finished, with a
// growing delay between attempts and for at most maxReceiveAttempts of them.
// A receive that is being followed is not followed twice.
func (r *receiver) follow(id receiveID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.live || r.ctx.Err() != nil {
		return
	}
	job, ok := r.jobs[id]
	if !ok {
		job = &receiveJob{}
		r.jobs[id] = job
	}
	if job.running || job.attempts >= maxReceiveAttempts {
		return
	}
	job.running = true

	r.e.wg.Go(func() {
		for {
			done, err := r.advance(r.ctx, id)

			r.mu.Lock()
			job.attempts++
			attempts := job.attempts
			if done {
				delete(r.jobs, id)
			}
			if done || attempts >= maxReceiveAttempts {
				job.running = false
			}
			r.mu.Unlock()

			if done || r.ctx.Err() != nil {
				return
			}
			r.logAttempt(id, err)
			if attempts >= maxReceiveAttempts {
				r.e.log.Infof("Receive %v is left to a later "+
					"sync after %d attempts", id.outpoint,
					attempts)

				return
			}

			delay := receiveRetryInterval << (attempts - 1)
			select {
			case <-time.After(delay):
			case <-r.ctx.Done():
				return
			}
		}
	})
}

// logAttempt logs why an attempt left a receive unfinished. A receive that
// waits is no failure of the wallet.
func (r *receiver) logAttempt(id receiveID, err error) {
	logf := r.e.log.Errorf
	if errors.Is(err, errReceiveNotReady) {
		logf = r.e.log.Infof
	}
	logf("Receive %v: %v", id.outpoint, err)
}

// sync reads the stored messages of every mailbox and works once on every
// unfinished receive. A receive that waits for its block or its proofs is no
// error.
func (r *receiver) sync(ctx context.Context) error {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(r.ctx, cancel)()

	addrs, err := r.e.addrs.ListAddresses(ctx)
	if err != nil {
		return fmt.Errorf("list addresses: %w", err)
	}

	var (
		wg    sync.WaitGroup
		errMu sync.Mutex
		errs  []error
	)
	report := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()

		errs = append(errs, err)
	}

	for idx := range addrs {
		wg.Go(func() {
			err := r.readMailbox(ctx, addrs[idx], true)
			if err != nil {
				report(fmt.Errorf("read mailbox of address "+
					"%x: %w", addrs[idx].ScriptKey[:], err))
			}
		})
	}
	wg.Wait()

	unfinished, err := r.unfinished(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	queue := make(chan receiveID)
	for range maxProofFetches {
		wg.Go(func() {
			for id := range queue {
				_, err := r.advance(ctx, id)
				if errors.Is(err, errReceiveNotReady) {
					r.logAttempt(id, err)

					continue
				}
				if err != nil {
					report(fmt.Errorf("receive %v: %w",
						id.outpoint, err))
				}
			}
		})
	}
	for _, id := range unfinished {
		queue <- id
	}
	close(queue)
	wg.Wait()

	// Nothing else uses the couriers of a client that is not live.
	r.mu.Lock()
	live := r.live
	r.mu.Unlock()
	if !live {
		r.closeCouriers()
	}

	return errors.Join(errs...)
}

// readMailbox reads the mailbox of an address and records the transfers its
// messages describe. One pass returns once the mailbox delivered nothing for
// mailboxQuiet. Otherwise it follows each new receive in the background, and
// returns when the stream fails or ctx ends.
func (r *receiver) readMailbox(ctx context.Context, record AddressRecord,
	onePass bool) error {

	addr, err := r.loadAddress(ctx, record)
	if err != nil {
		return err
	}
	courier, err := r.courier(ctx, &addr.tap.ProofCourierAddr)
	if err != nil {
		return err
	}

	sign := func(ctx context.Context,
		challenge []byte) (*schnorr.Signature, error) {

		if len(challenge) != challengeSize {
			return nil, fmt.Errorf("challenge of %d bytes",
				len(challenge))
		}

		return r.keys.SignMessageSchnorr(
			ctx, addr.scriptKey.KeyLocator, challenge,
		)
	}

	// The filter of a mailbox delivers stored messages only from a
	// height above zero.
	startBlock := uint32(1)
	if record.CreationHeight > mailboxStartMargin {
		startBlock = record.CreationHeight - mailboxStartMargin
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := courier.ReceiveMessages(
		ctx, record.ScriptKey, startBlock, sign,
	)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	type batch struct {
		msgs []MailboxMessage
		err  error
	}
	batches := make(chan batch)
	go func() {
		for {
			msgs, err := stream.Recv()

			select {
			case batches <- batch{msgs: msgs, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var (
		quiet   *time.Timer
		silence <-chan time.Time
	)
	if onePass {
		quiet = time.NewTimer(mailboxQuiet)
		defer quiet.Stop()
		silence = quiet.C
	}
	for {
		select {
		case next := <-batches:
			if next.err != nil {
				return next.err
			}

			for _, msg := range next.msgs {
				receive, err := r.detect(ctx, addr, msg)
				if err != nil {
					return err
				}
				if onePass || receive == nil ||
					receive.State != ReceiveDetected {

					continue
				}

				r.follow(idOf(receive))
			}
			if onePass {
				quiet.Reset(mailboxQuiet)
			}

		case <-silence:
			return nil

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// courier returns the open courier at an address, which it dials on first
// use.
func (r *receiver) courier(ctx context.Context, addr *url.URL) (Courier,
	error) {

	r.courierMu.Lock()
	defer r.courierMu.Unlock()

	key := addr.String()
	if courier, ok := r.couriers[key]; ok {
		return courier, nil
	}
	if r.ctx.Err() != nil {
		return nil, r.ctx.Err()
	}

	courier, err := r.dial(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("connect to courier %v: %w", addr.Host,
			err)
	}
	r.couriers[key] = courier

	return courier, nil
}

// closeCouriers closes every open courier.
func (r *receiver) closeCouriers() {
	r.courierMu.Lock()
	defer r.courierMu.Unlock()

	for key, courier := range r.couriers {
		if err := courier.Close(); err != nil {
			r.e.log.Errorf("Closing courier %v: %v", key, err)
		}
		delete(r.couriers, key)
	}
}

// stop ends the background work. The caller waits for it to return before
// it closes the couriers.
func (r *receiver) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cancel()
}
