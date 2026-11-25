package embedded

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/proof"
)

// receiveHintMargin is how many blocks below the last known block of an
// anchor transaction its confirmation is searched from. A re-org can confirm
// the transaction below the block that left the chain.
const receiveHintMargin = 6

// anchorLookupTimeout bounds the wait for the block of the anchor transaction
// of an earlier proof. A confirmed transaction that spends its output implies
// that block, so the chain source reports it without waiting for a new one.
var anchorLookupTimeout = 30 * time.Second

// confirmationHint returns the height the confirmation of an anchor
// transaction last known at height is searched from.
func confirmationHint(height uint32) uint32 {
	if height > receiveHintMargin {
		return height - receiveHintMargin
	}

	return 1
}

// trackedReceive is a receive the tracker of its anchor output follows: a
// confirmed one, or one whose confirming block left the chain.
type trackedReceive struct {
	record  ReceiveRecord
	payload *receivePayload
}

// trackStored follows every stored receive whose confirmation is not final.
func (r *receiver) trackStored(ctx context.Context) error {
	states := []ReceiveState{ReceiveConfirmed, ReceiveDetected}
	for _, state := range states {
		records, err := r.e.addrs.ListReceives(ctx, ReceiveFilter{
			State: &state,
		})
		if err != nil {
			return fmt.Errorf("list receives: %w", err)
		}

		for idx := range records {
			record := &records[idx]
			if state == ReceiveDetected {
				payload, err := decodeReceivePayload(
					record.Payload,
				)
				if err != nil {
					return err
				}
				if !payload.reorged {
					continue
				}
			}

			r.track(record.Outpoint)
		}
	}

	return nil
}

// track follows the anchor transaction of an output in the background until
// the confirmation of its receives is final. Tracking an output twice has no
// effect.
func (r *receiver) track(outpoint wire.OutPoint) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ctx.Err() != nil {
		return
	}

	// A tracker that is about to end looks at the receives once more.
	if _, ok := r.tracked[outpoint]; ok {
		r.tracked[outpoint] = true

		return
	}
	r.tracked[outpoint] = false

	r.e.wg.Go(func() {
		r.followOutput(r.ctx, outpoint)
	})
}

// followOutput follows the receives of an output until none of them needs a
// tracker. A failed registration is retried.
func (r *receiver) followOutput(ctx context.Context,
	outpoint wire.OutPoint) {

	for {
		done, err := r.followOutputOnce(ctx, outpoint)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.e.log.Errorf("Following receive %v: %v", outpoint, err)
		}

		if done {
			r.mu.Lock()
			again := r.tracked[outpoint]
			if again {
				r.tracked[outpoint] = false
			} else {
				delete(r.tracked, outpoint)
			}
			r.mu.Unlock()

			if !again {
				return
			}

			continue
		}

		select {
		case <-time.After(retryInterval):
		case <-ctx.Done():
			return
		}
	}
}

// followOutputOnce runs one confirmation registration of the anchor
// transaction of an output. It reports whether no receive of the output needs
// further tracking.
func (r *receiver) followOutputOnce(ctx context.Context,
	outpoint wire.OutPoint) (bool, error) {

	tracked, err := r.trackedReceives(ctx, outpoint)
	if err != nil {
		return false, err
	}
	if len(tracked) == 0 {
		return true, nil
	}

	pkScript, height, err := r.anchorOutput(ctx, &tracked[0])
	if err != nil {
		return false, err
	}

	events, err := r.e.chain.RegisterConfirmation(
		ctx, outpoint.Hash, pkScript, confirmationHint(height),
	)
	if err != nil {
		return false, fmt.Errorf("register confirmation: %w", err)
	}
	defer events.Cancel()

	// A block recorded before this registration can have left the best
	// chain while nothing followed the receive, and no event reports
	// that. The events report every later change.
	for idx := range tracked {
		record := &tracked[idx].record
		if record.State != ReceiveConfirmed {
			continue
		}

		onChain, err := r.e.onBestChain(
			ctx, record.BlockHash, record.BlockHeight,
		)
		if err != nil {
			return false, err
		}
		if onChain {
			continue
		}

		if err := r.unconfirm(ctx, outpoint, nil); err != nil {
			return false, err
		}

		break
	}

	// unapplied is a confirmation the receives do not reflect yet.
	var unapplied *Confirmation

	retry := time.NewTicker(retryInterval)
	defer retry.Stop()

	for {
		if unapplied != nil {
			err := r.reconfirm(ctx, outpoint, unapplied)
			switch {
			case err == nil:
				unapplied = nil

			case ctx.Err() != nil:
				return false, nil

			default:
				r.e.log.Errorf("Confirming receive %v: %v",
					outpoint, err)
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
			err := r.unconfirm(ctx, outpoint, nil)
			if err != nil {
				return false, err
			}

		case <-events.Done:
			// The confirmation may be ready along with Done.
			if unapplied == nil {
				select {
				case unapplied = <-events.Confirmed:
				default:
				}
			}
			if unapplied != nil {
				err := r.reconfirm(ctx, outpoint, unapplied)
				if err != nil {
					return false, err
				}
			}
			if err := r.finish(ctx, outpoint); err != nil {
				return false, err
			}

			return true, nil

		// The next round applies a confirmation that failed again.
		case <-retry.C:

		case <-ctx.Done():
			return false, nil
		}
	}
}

// trackedReceives returns the receives of an output that its tracker
// follows, oldest first.
func (r *receiver) trackedReceives(ctx context.Context,
	outpoint wire.OutPoint) ([]trackedReceive, error) {

	records, err := r.e.addrs.ListReceives(ctx, ReceiveFilter{
		Outpoint: &outpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("list receives: %w", err)
	}

	var tracked []trackedReceive
	for idx := range records {
		record := records[idx]
		if record.State != ReceiveConfirmed &&
			record.State != ReceiveDetected {

			continue
		}

		payload, err := decodeReceivePayload(record.Payload)
		if err != nil {
			return nil, err
		}
		if record.State == ReceiveDetected && !payload.reorged {
			continue
		}

		tracked = append(tracked, trackedReceive{
			record:  record,
			payload: payload,
		})
	}

	return tracked, nil
}

// storedProofs returns the stored proof of every asset output of a tracked
// receive.
func (r *receiver) storedProofs(ctx context.Context,
	receive *trackedReceive) ([]AssetProof, error) {

	addr, err := r.address(ctx, receive.record.Address)
	if err != nil {
		return nil, err
	}
	outputs, err := deriveReceiveOutputs(addr, receive.payload.fragment)
	if err != nil {
		return nil, err
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("receive names no asset output")
	}

	outpoint := receive.record.Outpoint
	proofs := make([]AssetProof, len(outputs))
	for idx, output := range outputs {
		stored, err := r.e.store.FetchProofs(
			ctx, output.assetID,
			scriptKeyBytes(output.scriptKey.PubKey), &outpoint,
		)
		if err != nil {
			return nil, fmt.Errorf("fetch proof of asset %v: %w",
				output.assetID, err)
		}
		if len(stored) != 1 {
			return nil, fmt.Errorf("proof of asset %v at %v: %w",
				output.assetID, outpoint, ErrNotFound)
		}
		proofs[idx] = stored[0]
	}

	return proofs, nil
}

// anchorOutput returns the script of the anchor output of a tracked receive
// and the height of the block its stored proofs name.
func (r *receiver) anchorOutput(ctx context.Context,
	receive *trackedReceive) ([]byte, uint32, error) {

	proofs, err := r.storedProofs(ctx, receive)
	if err != nil {
		return nil, 0, err
	}
	file, err := proof.DecodeFile(proofs[0].ProofFile)
	if err != nil {
		return nil, 0, fmt.Errorf("decode proof file: %w", err)
	}
	tip, err := file.LastProof()
	if err != nil {
		return nil, 0, err
	}

	index := receive.record.Outpoint.Index
	if int(index) >= len(tip.AnchorTx.TxOut) {
		return nil, 0, fmt.Errorf("anchor transaction has no output "+
			"%d", index)
	}

	return tip.AnchorTx.TxOut[index].PkScript, tip.BlockHeight, nil
}

// unconfirm returns the confirmed receives of an output whose confirming
// block left the chain to detected: every one, or those that are not
// confirmed in the block of keep. Their asset outputs leave the wallet until
// the anchor transaction confirms again.
func (r *receiver) unconfirm(ctx context.Context, outpoint wire.OutPoint,
	keep *Confirmation) error {

	r.e.transferMu.Lock()
	defer r.e.transferMu.Unlock()

	tracked, err := r.trackedReceives(ctx, outpoint)
	if err != nil {
		return err
	}

	for idx := range tracked {
		receive := &tracked[idx]
		record := &receive.record
		if record.State != ReceiveConfirmed ||
			confirmedIn(record, keep) {

			continue
		}

		// The records stay, so that a spent output stays spent when
		// the receive confirms again.
		proofs, err := r.storedProofs(ctx, receive)
		if err != nil {
			return err
		}
		for proofIdx := range proofs {
			proofs[proofIdx].Owned = false
		}

		receive.payload.reorged = true
		record.State = ReceiveDetected
		record.BlockHash = chainhash.Hash{}
		record.BlockHeight = 0
		record.Payload, err = receive.payload.encode()
		if err != nil {
			return err
		}
		err = r.e.addrs.UpdateReceive(ctx, *record, proofs)
		if err != nil {
			return fmt.Errorf("store receive: %w", err)
		}

		r.e.log.Infof("Receive %v left the best chain", outpoint)
	}

	return nil
}

// confirmedIn reports whether a receive is confirmed in the block of a
// confirmation.
func confirmedIn(record *ReceiveRecord, conf *Confirmation) bool {
	return conf != nil && record.State == ReceiveConfirmed &&
		record.BlockHash == conf.Block.BlockHash() &&
		record.BlockHeight == conf.Height
}

// reconfirm moves the tracked receives of an output to the block that
// confirms its anchor transaction: it writes that block into their proofs,
// with the current block of every earlier proof whose block left the chain,
// verifies the files and stores them as owned asset outputs.
func (r *receiver) reconfirm(ctx context.Context, outpoint wire.OutPoint,
	conf *Confirmation) error {

	if conf == nil || conf.Block == nil {
		return fmt.Errorf("confirmation of %v carries no block",
			outpoint.Hash)
	}

	// A receive confirmed in another block holds proofs of a block that
	// left the chain, whether or not its new proofs verify.
	if err := r.unconfirm(ctx, outpoint, conf); err != nil {
		return err
	}

	// The chain source can take a while to answer, so it is asked before
	// the writes of transfers are held up.
	blocks, err := r.findAnchorBlocks(ctx, outpoint, conf)
	if err != nil {
		return err
	}

	r.e.transferMu.Lock()
	defer r.e.transferMu.Unlock()

	tracked, err := r.trackedReceives(ctx, outpoint)
	if err != nil {
		return err
	}

	for idx := range tracked {
		receive := &tracked[idx]
		record := &receive.record
		if record.State == ReceiveConfirmed {
			continue
		}

		proofs, err := r.restamp(ctx, receive, blocks)
		if err != nil {
			return err
		}

		receive.payload.reorged = false
		record.State = ReceiveConfirmed
		record.BlockHash = conf.Block.BlockHash()
		record.BlockHeight = conf.Height
		record.Payload, err = receive.payload.encode()
		if err != nil {
			return err
		}
		err = r.e.addrs.UpdateReceive(ctx, *record, proofs)
		if err != nil {
			return fmt.Errorf("store received proofs: %w", err)
		}

		r.e.log.Infof("Receive %v confirmed at height %d", outpoint,
			conf.Height)
	}

	return nil
}

// anchorBlockSet holds the blocks to write into the proofs of an output.
type anchorBlockSet struct {
	// seen are the anchor transactions whose block is settled: it is in
	// blocks, or the proofs name a block of the best chain.
	seen map[chainhash.Hash]struct{}

	blocks []proof.VerifiedBlockContext
}

// findAnchorBlocks returns the blocks that the proofs of the unconfirmed
// receives of an output lack: the confirmation of the anchor transaction of
// the output, and the block the chain source reports for the anchor
// transaction of every other proof, those of additional inputs included, that
// names a block which is not on the best chain.
func (r *receiver) findAnchorBlocks(ctx context.Context,
	outpoint wire.OutPoint, conf *Confirmation) (*anchorBlockSet, error) {

	tracked, err := r.trackedReceives(ctx, outpoint)
	if err != nil {
		return nil, err
	}

	found := &anchorBlockSet{seen: make(map[chainhash.Hash]struct{})}
	for idx := range tracked {
		receive := &tracked[idx]
		if receive.record.State == ReceiveConfirmed {
			continue
		}

		proofs, err := r.storedProofs(ctx, receive)
		if err != nil {
			return nil, err
		}
		for proofIdx := range proofs {
			file, err := proof.DecodeFile(
				proofs[proofIdx].ProofFile,
			)
			if err != nil {
				return nil, fmt.Errorf("decode proof file: %w",
					err)
			}

			err = r.addFileBlocks(
				ctx, file, outpoint.Hash, conf, found,
			)
			if err != nil {
				return nil, err
			}
		}
	}

	return found, nil
}

// addFileBlocks adds the blocks that a proof file and the files of its
// additional inputs lack. conf confirms the transaction anchorTxid.
func (r *receiver) addFileBlocks(ctx context.Context, file *proof.File,
	anchorTxid chainhash.Hash, conf *Confirmation,
	found *anchorBlockSet) error {

	for idx := range file.NumProofs() {
		p, err := file.ProofAt(uint32(idx))
		if err != nil {
			return fmt.Errorf("read proof %d: %w", idx, err)
		}

		for inputIdx := range p.AdditionalInputs {
			err := r.addFileBlocks(
				ctx, &p.AdditionalInputs[inputIdx], anchorTxid,
				conf, found,
			)
			if err != nil {
				return err
			}
		}

		txid := p.AnchorTx.TxHash()
		if _, ok := found.seen[txid]; ok {
			continue
		}

		var block proof.VerifiedBlockContext
		if txid == anchorTxid {
			block, err = anchorBlock(&p.AnchorTx, conf)
		} else {
			block, err = r.staleAnchorBlock(ctx, p, conf.Height)
		}
		if err != nil {
			return err
		}

		found.seen[txid] = struct{}{}
		if block != nil {
			found.blocks = append(found.blocks, block)
		}
	}

	return nil
}

// staleAnchorBlock returns the block that confirms the anchor transaction of
// a proof, or nil when the proof names a block of the best chain. A
// transaction that spends the anchor transaction, directly or not, is
// confirmed at spendHeight.
func (r *receiver) staleAnchorBlock(ctx context.Context, p *proof.Proof,
	spendHeight uint32) (proof.VerifiedBlockContext, error) {

	onChain, err := r.e.onBestChain(
		ctx, p.BlockHeader.BlockHash(), p.BlockHeight,
	)
	if err != nil || onChain {
		return nil, err
	}

	txid := p.AnchorTx.TxHash()
	index := p.InclusionProof.OutputIndex
	if int(index) >= len(p.AnchorTx.TxOut) {
		return nil, fmt.Errorf("anchor transaction %v has no output "+
			"%d", txid, index)
	}

	ctx, cancel := context.WithTimeout(ctx, anchorLookupTimeout)
	defer cancel()

	// The transaction confirmed no later than the one that spends it.
	events, err := r.e.chain.RegisterConfirmation(
		ctx, txid, p.AnchorTx.TxOut[index].PkScript,
		confirmationHint(min(p.BlockHeight, spendHeight)),
	)
	if err != nil {
		return nil, fmt.Errorf("register confirmation of %v: %w", txid,
			err)
	}
	defer events.Cancel()

	select {
	case conf, ok := <-events.Confirmed:
		if !ok || conf == nil || conf.Block == nil {
			return nil, fmt.Errorf("confirmation events of %v "+
				"ended", txid)
		}

		return anchorBlock(&p.AnchorTx, conf)

	case <-ctx.Done():
		return nil, fmt.Errorf("anchor transaction %v of an earlier "+
			"proof is not confirmed: %w", txid, ctx.Err())
	}
}

// restamp returns the stored proofs of a tracked receive as owned asset
// outputs, with the given blocks written into their files. A file that does
// not verify with them is not returned.
func (r *receiver) restamp(ctx context.Context, receive *trackedReceive,
	found *anchorBlockSet) ([]AssetProof, error) {

	proofs, err := r.storedProofs(ctx, receive)
	if err != nil {
		return nil, err
	}

	for idx := range proofs {
		assetProof := &proofs[idx]

		file, err := proof.DecodeFile(assetProof.ProofFile)
		if err != nil {
			return nil, fmt.Errorf("decode proof file: %w", err)
		}

		blob := proof.Blob(assetProof.ProofFile)
		for _, block := range found.blocks {
			restamped, err := file.RestampAnchor(block)
			if err != nil {
				return nil, fmt.Errorf("write block into "+
					"proof: %w", err)
			}
			blob = restamped.Blob()
		}

		_, err = file.Verify(ctx, r.e.verifierCtx(ctx))
		if err != nil {
			return nil, fmt.Errorf("verify proof of asset %x: %w",
				assetProof.AssetID[:], err)
		}

		assetProof.ProofFile = blob
		assetProof.Owned = true
	}

	return proofs, nil
}

// anchorBlock returns the proof that a confirmation holds an anchor
// transaction.
func anchorBlock(anchorTx *wire.MsgTx,
	conf *Confirmation) (proof.VerifiedBlockContext, error) {

	txid := anchorTx.TxHash()
	txIndex := slices.IndexFunc(
		conf.Block.Transactions, func(blockTx *wire.MsgTx) bool {
			return blockTx.TxHash() == txid
		},
	)
	if txIndex < 0 {
		return nil, fmt.Errorf("anchor transaction %v is not in "+
			"block %v", txid, conf.Block.BlockHash())
	}

	merkleProof, err := proof.NewTxMerkleProof(
		conf.Block.Transactions, txIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("build merkle proof: %w", err)
	}

	return proof.NewVerifiedBlockContext(
		anchorTx, conf.Block.Header, conf.Height, *merkleProof,
	)
}

// finish marks the confirmed receives of an output as final.
func (r *receiver) finish(ctx context.Context, outpoint wire.OutPoint) error {
	r.e.transferMu.Lock()
	defer r.e.transferMu.Unlock()

	tracked, err := r.trackedReceives(ctx, outpoint)
	if err != nil {
		return err
	}

	for idx := range tracked {
		record := &tracked[idx].record
		if record.State != ReceiveConfirmed {
			return fmt.Errorf("confirmation of %v became final "+
				"before it was applied", outpoint.Hash)
		}

		record.State = ReceiveFinal
		err := r.e.addrs.UpdateReceive(ctx, *record, nil)
		if err != nil {
			return fmt.Errorf("store receive: %w", err)
		}
	}

	return nil
}
