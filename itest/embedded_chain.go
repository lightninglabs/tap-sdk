//go:build itest

package itest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/tap-sdk/embedded"
)

const (
	// bitcoindTxNotFound is the code bitcoind returns for a transaction
	// that is neither in the mempool nor in a block.
	bitcoindTxNotFound = -5

	// bitcoindTxAlreadyConfirmed is the code bitcoind returns when it is
	// asked to broadcast a confirmed transaction.
	bitcoindTxAlreadyConfirmed = -27

	// embeddedChainPollInterval is how often a confirmation registration
	// asks bitcoind for the state of its transaction.
	embeddedChainPollInterval = 200 * time.Millisecond

	// embeddedChainFinalityDepth is the number of confirmations after
	// which the chain reports a confirmation as final.
	embeddedChainFinalityDepth = defaultMineBlocks
)

// bitcoindRPCError is the error object of a bitcoind JSON-RPC response.
type bitcoindRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error returns the code and message bitcoind reported.
func (e *bitcoindRPCError) Error() string {
	return fmt.Sprintf("bitcoind RPC error %d: %s", e.Code, e.Message)
}

// bitcoindChain is an embedded.Chain over the JSON-RPC interface of the
// harness bitcoind. Confirmations are found by polling, which needs the
// transaction index the regtest node runs with.
type bitcoindChain struct {
	url        string
	httpClient *http.Client

	quit context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup
}

// A compile-time check that bitcoindChain is a chain source of the engine.
var _ embedded.Chain = (*bitcoindChain)(nil)

// newBitcoindChain returns a chain source for the bitcoind at host.
func newBitcoindChain(host string) *bitcoindChain {
	quit, stop := context.WithCancel(context.Background())

	return &bitcoindChain{
		url:        fmt.Sprintf("http://%s/", host),
		httpClient: &http.Client{Timeout: defaultSyncTimeout},
		quit:       quit,
		stop:       stop,
	}
}

// Stop ends every confirmation registration and waits for them to return.
func (c *bitcoindChain) Stop() {
	c.stop()
	c.wg.Wait()
}

// call runs one JSON-RPC method and decodes its result into result, which
// may be nil. An error bitcoind reports is returned as a *bitcoindRPCError.
func (c *bitcoindChain) call(ctx context.Context, method string, result any,
	params ...any) error {

	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "1.0",
		"id":      "embedded",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.url, bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.SetBasicAuth(bitcoindUser, bitcoindPass)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bitcoind %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded struct {
		Result json.RawMessage   `json:"result"`
		Error  *bitcoindRPCError `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("bitcoind %s: status %s: %w", method,
			resp.Status, err)
	}
	if decoded.Error != nil {
		return fmt.Errorf("bitcoind %s: %w", method, decoded.Error)
	}
	if result == nil {
		return nil
	}

	return json.Unmarshal(decoded.Result, result)
}

// BestBlock returns the height and hash of the best chain tip.
func (c *bitcoindChain) BestBlock(ctx context.Context) (uint32,
	chainhash.Hash, error) {

	var info struct {
		Blocks        uint32 `json:"blocks"`
		BestBlockHash string `json:"bestblockhash"`
	}
	if err := c.call(ctx, "getblockchaininfo", &info); err != nil {
		return 0, chainhash.Hash{}, err
	}

	hash, err := chainhash.NewHashFromStr(info.BestBlockHash)
	if err != nil {
		return 0, chainhash.Hash{}, err
	}

	return info.Blocks, *hash, nil
}

// BlockHeader returns the header of the best-chain block at height.
func (c *bitcoindChain) BlockHeader(ctx context.Context,
	height uint32) (*wire.BlockHeader, error) {

	var blockHash string
	if err := c.call(ctx, "getblockhash", &blockHash, height); err != nil {
		return nil, err
	}

	var headerHex string
	err := c.call(ctx, "getblockheader", &headerHex, blockHash, false)
	if err != nil {
		return nil, err
	}
	rawHeader, err := hex.DecodeString(headerHex)
	if err != nil {
		return nil, err
	}

	var header wire.BlockHeader
	if err := header.Deserialize(bytes.NewReader(rawHeader)); err != nil {
		return nil, err
	}

	return &header, nil
}

// PublishTransaction broadcasts a transaction through bitcoind. A
// transaction that is already in the mempool or in a block is accepted.
func (c *bitcoindChain) PublishTransaction(ctx context.Context,
	tx *wire.MsgTx, _ string) error {

	var rawTx bytes.Buffer
	if err := tx.Serialize(&rawTx); err != nil {
		return err
	}

	err := c.call(
		ctx, "sendrawtransaction", nil,
		hex.EncodeToString(rawTx.Bytes()),
	)

	var rpcErr *bitcoindRPCError
	if errors.As(err, &rpcErr) &&
		rpcErr.Code == bitcoindTxAlreadyConfirmed {

		return nil
	}

	return err
}

// RegisterConfirmation follows a transaction by polling bitcoind. A block
// that confirms the transaction is reported once, its removal from the best
// chain is reported as a re-org, and the confirmation is final once it is
// embeddedChainFinalityDepth blocks deep.
func (c *bitcoindChain) RegisterConfirmation(_ context.Context,
	txid chainhash.Hash, _ []byte,
	_ uint32) (*embedded.ConfirmationEvents, error) {

	ctx, cancel := context.WithCancel(c.quit)
	watch := &confirmationWatch{
		chain:     c,
		txid:      txid,
		confirmed: make(chan *embedded.Confirmation),
		reorged:   make(chan struct{}),
		done:      make(chan struct{}),
	}
	c.wg.Go(func() { watch.run(ctx) })

	return &embedded.ConfirmationEvents{
		Confirmed: watch.confirmed,
		Reorged:   watch.reorged,
		Done:      watch.done,
		Cancel:    cancel,
	}, nil
}

// confirmationWatch is one confirmation registration.
type confirmationWatch struct {
	chain *bitcoindChain
	txid  chainhash.Hash

	confirmed chan *embedded.Confirmation
	reorged   chan struct{}
	done      chan struct{}

	// reported is the confirming block the engine was last told about.
	reported *chainhash.Hash
}

// run polls until the confirmation is final or the registration is
// canceled. A failed poll is retried at the next interval.
func (w *confirmationWatch) run(ctx context.Context) {
	ticker := time.NewTicker(embeddedChainPollInterval)
	defer ticker.Stop()

	for {
		final, err := w.poll(ctx)
		if final || (err != nil && ctx.Err() != nil) {
			return
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// poll reports the change of the transaction's confirmation since the last
// poll. It returns true once the confirmation is final.
func (w *confirmationWatch) poll(ctx context.Context) (bool, error) {
	blockHash, depth, err := w.chain.confirmingBlock(ctx, w.txid)
	if err != nil {
		return false, err
	}

	// The block the engine knows about left the best chain.
	if w.reported != nil &&
		(blockHash == nil || *blockHash != *w.reported) {

		select {
		case w.reorged <- struct{}{}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		w.reported = nil
	}

	switch {
	case blockHash == nil:
		return false, nil

	case w.reported == nil:
		conf, err := w.chain.confirmation(ctx, *blockHash)
		if err != nil {
			return false, err
		}

		select {
		case w.confirmed <- conf:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		w.reported = blockHash

		return false, nil

	case depth >= embeddedChainFinalityDepth:
		close(w.done)

		return true, nil

	default:
		return false, nil
	}
}

// confirmingBlock returns the best-chain block that holds a transaction and
// its number of confirmations, or nil for a transaction that is unconfirmed
// or unknown.
func (c *bitcoindChain) confirmingBlock(ctx context.Context,
	txid chainhash.Hash) (*chainhash.Hash, int64, error) {

	var tx struct {
		BlockHash     string `json:"blockhash"`
		Confirmations int64  `json:"confirmations"`
	}
	err := c.call(ctx, "getrawtransaction", &tx, txid.String(), true)

	var rpcErr *bitcoindRPCError
	switch {
	case errors.As(err, &rpcErr) && rpcErr.Code == bitcoindTxNotFound:
		return nil, 0, nil

	case err != nil:
		return nil, 0, err

	// The index keeps a transaction of a block that left the best
	// chain, and reports it with no confirmations.
	case tx.BlockHash == "" || tx.Confirmations <= 0:
		return nil, 0, nil
	}

	blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
	if err != nil {
		return nil, 0, err
	}

	return blockHash, tx.Confirmations, nil
}

// confirmation fetches a full block and its height.
func (c *bitcoindChain) confirmation(ctx context.Context,
	blockHash chainhash.Hash) (*embedded.Confirmation, error) {

	var header struct {
		Height uint32 `json:"height"`
	}
	err := c.call(ctx, "getblockheader", &header, blockHash.String(), true)
	if err != nil {
		return nil, err
	}

	var blockHex string
	err = c.call(ctx, "getblock", &blockHex, blockHash.String(), 0)
	if err != nil {
		return nil, err
	}
	rawBlock, err := hex.DecodeString(blockHex)
	if err != nil {
		return nil, err
	}

	var block wire.MsgBlock
	if err := block.Deserialize(bytes.NewReader(rawBlock)); err != nil {
		return nil, err
	}

	return &embedded.Confirmation{
		Height: header.Height,
		Block:  &block,
	}, nil
}
