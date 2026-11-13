package embedded

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

// retryInterval is how long the tracker waits before it retries a failed
// step or broadcasts a pending anchor transaction again.
var retryInterval = 30 * time.Second

// resumeTransfers starts following every recorded transfer that is not
// final.
func (e *engine) resumeTransfers(ctx context.Context) error {
	transfers, err := e.store.ListTransfers(ctx, nil)
	if err != nil {
		return fmt.Errorf("list transfers: %w", err)
	}

	for idx := range transfers {
		if transfers[idx].State == TransferFinal {
			continue
		}
		e.track(transfers[idx].AnchorTxid, true)
	}

	return nil
}

// track follows a recorded transfer in the background until its
// confirmation is final, first broadcasting a pending anchor transaction
// again when asked to. Tracking a transfer twice has no effect.
func (e *engine) track(txid chainhash.Hash, rebroadcast bool) {
	e.trackMu.Lock()
	defer e.trackMu.Unlock()

	select {
	case <-e.quit:
		return
	default:
	}
	if _, ok := e.tracking[txid]; ok {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.tracking[txid] = cancel

	e.wg.Go(func() {
		defer func() {
			cancel()

			e.trackMu.Lock()
			delete(e.tracking, txid)
			e.trackMu.Unlock()
		}()

		e.follow(ctx, txid, rebroadcast)
	})
}

// stopTracking cancels every tracker and waits for them to return.
func (e *engine) stopTracking() {
	e.stopOnce.Do(func() {
		e.trackMu.Lock()
		close(e.quit)
		for _, cancel := range e.tracking {
			cancel()
		}
		e.trackMu.Unlock()

		e.wg.Wait()
	})
}

// follow registers for the confirmation of a transfer and applies its
// events until the confirmation is final. A failed registration is retried.
func (e *engine) follow(ctx context.Context, txid chainhash.Hash,
	rebroadcast bool) {

	for {
		done, err := e.followOnce(ctx, txid, rebroadcast)
		if done || ctx.Err() != nil {
			return
		}
		if err != nil {
			e.log.Errorf("Following transfer %v: %v", txid, err)
		}
		rebroadcast = true

		select {
		case <-time.After(retryInterval):
		case <-ctx.Done():
			return
		}
	}
}

// followOnce runs one confirmation registration of a transfer. It reports
// whether the transfer needs no further tracking.
func (e *engine) followOnce(ctx context.Context, txid chainhash.Hash,
	rebroadcast bool) (bool, error) {

	transfer, payload, err := e.loadTransfer(ctx, txid)
	if err != nil {
		return false, err
	}
	if transfer.State == TransferFinal {
		return true, nil
	}

	var anchorTx wire.MsgTx
	err = anchorTx.Deserialize(bytes.NewReader(payload.transfer.AnchorTx))
	if err != nil {
		return false, fmt.Errorf("decode anchor transaction: %w", err)
	}
	if len(anchorTx.TxOut) == 0 {
		return false, fmt.Errorf("anchor transaction has no outputs")
	}

	events, err := e.chain.RegisterConfirmation(
		ctx, txid, anchorTx.TxOut[0].PkScript, transfer.HeightHint,
	)
	if err != nil {
		return false, fmt.Errorf("register confirmation: %w", err)
	}
	defer events.Cancel()

	var (
		// unapplied is a confirmation whose proofs are not written
		// yet.
		unapplied *Confirmation

		pending   = transfer.State == TransferPending
		broadcast = !payload.skipBroadcast
	)
	publish := func() {
		err := e.chain.PublishTransaction(
			ctx, &anchorTx, payload.transfer.Label,
		)
		if err != nil {
			e.log.Errorf("Broadcasting transfer %v: %v", txid, err)
		}
	}
	if rebroadcast && pending && broadcast {
		publish()
	}

	retry := time.NewTicker(retryInterval)
	defer retry.Stop()

	for {
		if unapplied != nil {
			err := e.confirmTransfer(ctx, txid, unapplied)
			switch {
			case err == nil:
				e.log.Infof("Transfer %v confirmed at height "+
					"%d", txid, unapplied.Height)
				unapplied = nil
				pending = false

			case ctx.Err() != nil:
				return false, nil

			default:
				e.log.Errorf("Writing final proofs of "+
					"transfer %v: %v", txid, err)
			}
		}

		select {
		case conf, ok := <-events.Confirmed:
			if !ok {
				return false, fmt.Errorf("confirmation " +
					"events ended")
			}
			unapplied = conf

		case _, ok := <-events.Reorged:
			if !ok {
				return false, fmt.Errorf("confirmation " +
					"events ended")
			}

			unapplied = nil
			if err := e.unconfirmTransfer(ctx, txid); err != nil {
				return false, err
			}
			e.log.Infof("Transfer %v left the best chain", txid)
			pending = true

		case <-events.Done:
			if unapplied != nil || pending {
				return false, fmt.Errorf("confirmation of %v "+
					"became final before it was applied",
					txid)
			}
			if err := e.finishTransfer(ctx, txid); err != nil {
				return false, err
			}

			return true, nil

		case <-retry.C:
			if pending && broadcast && unapplied == nil {
				publish()
			}

		case <-ctx.Done():
			return false, nil
		}
	}
}
