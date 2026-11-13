package embedded

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
)

// medianTimeBlocks is the number of blocks Bitcoin's median time past spans.
const medianTimeBlocks = 11

// errTxNotInProof reports a transaction a proof file does not anchor.
var errTxNotInProof = errors.New("transaction not found in proof file")

// chainLookupGen creates the chain lookups time-lock validation uses.
type chainLookupGen struct {
	chain Chain
}

// GenFileChainLookup returns a chain lookup that resolves transaction heights
// from the proof file.
func (g *chainLookupGen) GenFileChainLookup(f *proof.File) asset.ChainLookup {
	return &chainLookup{chain: g.chain, file: f}
}

// GenProofChainLookup returns a chain lookup that resolves transaction heights
// from a single proof.
func (g *chainLookupGen) GenProofChainLookup(
	p *proof.Proof) (asset.ChainLookup, error) {

	f, err := proof.NewFile(proof.V0, *p)
	if err != nil {
		return nil, err
	}

	return &chainLookup{chain: g.chain, file: f}, nil
}

// chainLookup answers the chain questions of time-lock validation from a
// proof file and the chain source.
type chainLookup struct {
	chain Chain
	file  *proof.File
}

// CurrentHeight returns the height of the best chain tip.
func (l *chainLookup) CurrentHeight(ctx context.Context) (uint32, error) {
	height, _, err := l.chain.BestBlock(ctx)

	return height, err
}

// TxBlockHeight returns the height of the block that confirmed a transaction
// anchoring one of the file's proofs.
func (l *chainLookup) TxBlockHeight(_ context.Context,
	txid chainhash.Hash) (uint32, error) {

	if l.file == nil {
		return 0, errTxNotInProof
	}

	return txHeightInFile(l.file, txid)
}

// txHeightInFile searches a proof file and its additional inputs for the
// height of an anchor transaction.
func txHeightInFile(f *proof.File, txid chainhash.Hash) (uint32, error) {
	for i := f.NumProofs() - 1; i >= 0; i-- {
		p, err := f.ProofAt(uint32(i))
		if err != nil {
			return 0, fmt.Errorf("read proof %d: %w", i, err)
		}

		// A zero height is unset and is not checked against the
		// chain, so it cannot serve as the transaction's height.
		if p.AnchorTx.TxHash() == txid && p.BlockHeight != 0 {
			return p.BlockHeight, nil
		}

		for idx := range p.AdditionalInputs {
			height, err := txHeightInFile(
				&p.AdditionalInputs[idx], txid,
			)
			switch {
			case errors.Is(err, errTxNotInProof):
				continue

			case err != nil:
				return 0, err
			}

			return height, nil
		}
	}

	return 0, errTxNotInProof
}

// MeanBlockTimestamp returns the median time past of the block at height.
func (l *chainLookup) MeanBlockTimestamp(ctx context.Context,
	height uint32) (time.Time, error) {

	timestamps := make([]int64, 0, medianTimeBlocks)
	for i := uint32(0); i < medianTimeBlocks && i <= height; i++ {
		header, err := l.chain.BlockHeader(ctx, height-i)
		if err != nil {
			return time.Time{}, fmt.Errorf("fetch block header "+
				"at height %d: %w", height-i, err)
		}
		if header == nil {
			return time.Time{}, fmt.Errorf("no block header at "+
				"height %d", height-i)
		}

		timestamps = append(timestamps, header.Timestamp.Unix())
	}
	slices.Sort(timestamps)

	return time.Unix(timestamps[len(timestamps)/2], 0), nil
}
