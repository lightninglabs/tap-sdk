package embedded

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/stretchr/testify/require"
)

// testKeyRing derives keys deterministically from a seed.
type testKeyRing struct {
	mu   sync.Mutex
	seed byte
	next map[keychain.KeyFamily]uint32
}

// newTestKeyRing returns a key ring whose keys depend on seed.
func newTestKeyRing(seed byte) *testKeyRing {
	return &testKeyRing{
		seed: seed,
		next: make(map[keychain.KeyFamily]uint32),
	}
}

// privKey returns the private key at a derivation path.
func (k *testKeyRing) privKey(family keychain.KeyFamily,
	index uint32) *btcec.PrivateKey {

	var preimage [9]byte
	preimage[0] = k.seed
	binary.BigEndian.PutUint32(preimage[1:5], uint32(family))
	binary.BigEndian.PutUint32(preimage[5:9], index)
	digest := sha256.Sum256(preimage[:])
	privKey, _ := btcec.PrivKeyFromBytes(digest[:])

	return privKey
}

// DeriveNextKey derives the next key of the family.
func (k *testKeyRing) DeriveNextKey(
	family keychain.KeyFamily) (keychain.KeyDescriptor, error) {

	k.mu.Lock()
	defer k.mu.Unlock()

	index := k.next[family]
	k.next[family]++

	return keychain.KeyDescriptor{
		PubKey: k.privKey(family, index).PubKey(),
		KeyLocator: keychain.KeyLocator{
			Family: family,
			Index:  index,
		},
	}, nil
}

// DerivePrivKey returns the private key of a derived key.
func (k *testKeyRing) DerivePrivKey(
	desc keychain.KeyDescriptor) (*btcec.PrivateKey, error) {

	privKey := k.privKey(desc.Family, desc.Index)
	if desc.PubKey != nil && !privKey.PubKey().IsEqual(desc.PubKey) {
		return nil, fmt.Errorf("key %x is not at %d/%d",
			desc.PubKey.SerializeCompressed(), desc.Family,
			desc.Index)
	}

	return privKey, nil
}

// confSubscription is one confirmation registration of the test chain.
type confSubscription struct {
	txid      chainhash.Hash
	confirmed chan *Confirmation
	reorged   chan struct{}
	done      chan struct{}
	canceled  bool
}

// testChain is an in-memory best chain with a mempool.
type testChain struct {
	mu      sync.Mutex
	blocks  []*wire.MsgBlock
	mempool []*wire.MsgTx
	subs    []*confSubscription

	// publishErr fails PublishTransaction when set.
	publishErr error
}

// newTestChain returns a chain with a few empty blocks.
func newTestChain(t *testing.T) *testChain {
	t.Helper()

	c := &testChain{}
	for range 12 {
		c.mine(t)
	}

	return c
}

// BestBlock returns the height and hash of the chain tip.
func (c *testChain) BestBlock(context.Context) (uint32, chainhash.Hash,
	error) {

	c.mu.Lock()
	defer c.mu.Unlock()

	tip := c.blocks[len(c.blocks)-1]

	return uint32(len(c.blocks) - 1), tip.BlockHash(), nil
}

// BlockHeader returns the header at height.
func (c *testChain) BlockHeader(_ context.Context,
	height uint32) (*wire.BlockHeader, error) {

	c.mu.Lock()
	defer c.mu.Unlock()

	if int(height) >= len(c.blocks) {
		return nil, fmt.Errorf("no block at height %d", height)
	}
	header := c.blocks[height].Header

	return &header, nil
}

// PublishTransaction adds a transaction to the mempool.
func (c *testChain) PublishTransaction(_ context.Context, tx *wire.MsgTx,
	_ string) error {

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.publishErr != nil {
		return c.publishErr
	}

	txid := tx.TxHash()
	for _, known := range c.mempool {
		if known.TxHash() == txid {
			return nil
		}
	}
	if _, _, ok := c.findTx(txid); ok {
		return nil
	}
	c.mempool = append(c.mempool, tx.Copy())

	return nil
}

// findTx returns the height and block of a confirmed transaction.
func (c *testChain) findTx(txid chainhash.Hash) (uint32, *wire.MsgBlock,
	bool) {

	for height, block := range c.blocks {
		for _, tx := range block.Transactions {
			if tx.TxHash() == txid {
				return uint32(height), block, true
			}
		}
	}

	return 0, nil, false
}

// RegisterConfirmation follows the confirmation of a transaction.
func (c *testChain) RegisterConfirmation(_ context.Context,
	txid chainhash.Hash, _ []byte, _ uint32) (*ConfirmationEvents,
	error) {

	c.mu.Lock()
	defer c.mu.Unlock()

	sub := &confSubscription{
		txid:      txid,
		confirmed: make(chan *Confirmation, 16),
		reorged:   make(chan struct{}, 16),
		done:      make(chan struct{}, 1),
	}
	c.subs = append(c.subs, sub)

	if height, block, ok := c.findTx(txid); ok {
		sub.confirmed <- &Confirmation{Height: height, Block: block}
	}

	return &ConfirmationEvents{
		Confirmed: sub.confirmed,
		Reorged:   sub.reorged,
		Done:      sub.done,
		Cancel: func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			sub.canceled = true
		},
	}, nil
}

// mine adds a block holding the mempool and the given transactions, and
// returns it with its height.
func (c *testChain) mine(t *testing.T, txs ...*wire.MsgTx) (*wire.MsgBlock,
	uint32) {

	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	height := uint32(len(c.blocks))

	// The coinbase makes every block unique.
	coinbase := wire.NewMsgTx(2)
	var heightScript [5]byte
	heightScript[0] = 4
	binary.LittleEndian.PutUint32(heightScript[1:], height)
	coinbase.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Index: wire.MaxPrevOutIndex},
		SignatureScript:  heightScript[:],
		Sequence:         uint32(time.Now().UnixNano()),
	})
	coinbase.AddTxOut(&wire.TxOut{Value: 50, PkScript: []byte{0x51}})

	blockTxs := []*wire.MsgTx{coinbase}
	blockTxs = append(blockTxs, c.mempool...)
	blockTxs = append(blockTxs, txs...)
	c.mempool = nil

	utilTxs := make([]*btcutil.Tx, len(blockTxs))
	for idx, tx := range blockTxs {
		utilTxs[idx] = btcutil.NewTx(tx)
	}
	merkleTree := blockchain.BuildMerkleTreeStore(utilTxs, false)

	prevHash := chaincfg.RegressionNetParams.GenesisHash
	if height > 0 {
		hash := c.blocks[height-1].BlockHash()
		prevHash = &hash
	}
	header := wire.NewBlockHeader(
		2, prevHash, merkleTree[len(merkleTree)-1], 0, 0,
	)
	header.Timestamp = time.Unix(1_700_000_000+int64(height)*600, 0)

	block := &wire.MsgBlock{Header: *header, Transactions: blockTxs}
	c.blocks = append(c.blocks, block)

	for _, sub := range c.subs {
		if sub.canceled {
			continue
		}
		for _, tx := range blockTxs {
			if tx.TxHash() == sub.txid {
				sub.confirmed <- &Confirmation{
					Height: height,
					Block:  block,
				}
			}
		}
	}

	return block, height
}

// reorg removes the last depth blocks. Their transactions return to the
// mempool, and their confirmations are reported as reorged.
func (c *testChain) reorg(depth int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := c.blocks[len(c.blocks)-depth:]
	c.blocks = c.blocks[:len(c.blocks)-depth]

	for _, block := range removed {
		for _, tx := range block.Transactions[1:] {
			c.mempool = append(c.mempool, tx)

			for _, sub := range c.subs {
				if !sub.canceled && sub.txid == tx.TxHash() {
					sub.reorged <- struct{}{}
				}
			}
		}
	}
}

// finalize reports the confirmation of a transaction as final.
func (c *testChain) finalize(txid chainhash.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, sub := range c.subs {
		if !sub.canceled && sub.txid == txid {
			select {
			case sub.done <- struct{}{}:
			default:
			}
		}
	}
}

// testHarness is an embedded client on a test chain.
type testHarness struct {
	t      *testing.T
	chain  *testChain
	keys   *testKeyRing
	store  *MemoryStore
	client *Client
	wallet *tapsdk.Wallet
}

// newTestHarness starts an embedded client with an empty store.
func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	h := &testHarness{
		t:     t,
		chain: newTestChain(t),
		keys:  newTestKeyRing(1),
		store: NewMemoryStore(),
	}
	h.restart()

	return h
}

// restart replaces the client with a new one over the same store.
func (h *testHarness) restart() {
	h.t.Helper()

	if h.client != nil {
		require.NoError(h.t, h.client.Close())
	}

	client, err := NewClient(&Config{
		Network: tapsdk.NetworkRegtest,
		Store:   h.store,
		KeyRing: h.keys,
		Chain:   h.chain,
	})
	require.NoError(h.t, err)
	require.NoError(h.t, client.Start(h.t.Context()))
	h.t.Cleanup(func() { _ = client.Close() })

	h.client = client
	h.wallet = tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
}

// mintedAsset is an asset issued straight into a wallet's keys.
type mintedAsset struct {
	ref       tapsdk.AssetRef
	assetID   asset.ID
	proofFile []byte
	outpoint  wire.OutPoint
}

// mintTo issues units of a new asset to a script key in an anchor output
// under an internal key, confirms the issuance and returns its proof file.
func (h *testHarness) mintTo(scriptKey *tapsdk.ScriptKey,
	internalKey *tapsdk.InternalKey, amount uint64,
	tag string) *mintedAsset {

	h.t.Helper()

	scriptPubKey, err := btcec.ParsePubKey(scriptKey.PubKey[:])
	require.NoError(h.t, err)
	anchorKey, err := btcec.ParsePubKey(internalKey.PubKey[:])
	require.NoError(h.t, err)

	genesisPoint := wire.OutPoint{
		Hash:  sha256.Sum256([]byte("genesis-" + tag)),
		Index: 0,
	}
	genesis := asset.Genesis{
		FirstPrevOut: genesisPoint,
		Tag:          tag,
		OutputIndex:  0,
		Type:         asset.Normal,
	}
	newAsset, err := asset.New(
		genesis, amount, 0, 0, asset.NewScriptKey(scriptPubKey), nil,
	)
	require.NoError(h.t, err)

	version := commitment.TapCommitmentV2
	tapCommitment, err := commitment.FromAssets(&version, newAsset)
	require.NoError(h.t, err)

	tapscriptRoot := tapCommitment.TapscriptRoot(nil)
	taprootKey := txscript.ComputeTaprootOutputKey(
		anchorKey, tapscriptRoot[:],
	)
	pkScript, err := txscript.PayToTaprootScript(taprootKey)
	require.NoError(h.t, err)

	genesisTx := wire.NewMsgTx(2)
	genesisTx.AddTxIn(&wire.TxIn{PreviousOutPoint: genesisPoint})
	genesisTx.AddTxOut(&wire.TxOut{Value: 1_000, PkScript: pkScript})

	block, height := h.chain.mine(h.t, genesisTx)

	proofs, err := proof.NewMintingBlobs(&proof.MintParams{
		BaseProofParams: proof.BaseProofParams{
			Block:            block,
			BlockHeight:      height,
			Tx:               genesisTx,
			TxIndex:          len(block.Transactions) - 1,
			OutputIndex:      0,
			InternalKey:      anchorKey,
			TaprootAssetRoot: tapCommitment,
		},
		GenesisPoint: genesisPoint,
	}, h.client.engine.verifierCtx(h.t.Context()))
	require.NoError(h.t, err)

	genesisProof := proofs[asset.ToSerialized(newAsset.ScriptKey.PubKey)]
	require.NotNil(h.t, genesisProof)

	file, err := proof.NewFile(proof.V0, *genesisProof)
	require.NoError(h.t, err)
	var encoded bytes.Buffer
	require.NoError(h.t, file.Encode(&encoded))

	assetID := newAsset.ID()

	return &mintedAsset{
		ref:       tapsdk.AssetRefFromAssetID(tapsdk.AssetID(assetID)),
		assetID:   assetID,
		proofFile: encoded.Bytes(),
		outpoint: wire.OutPoint{
			Hash:  genesisTx.TxHash(),
			Index: 0,
		},
	}
}

// receivedUnits is how many units receive issues to the wallet.
const receivedUnits = 1_000

// receive derives wallet keys, issues receivedUnits of a new asset to them
// and imports its proof into the wallet.
func (h *testHarness) receive(tag string) *mintedAsset {
	h.t.Helper()

	ctx := h.t.Context()
	scriptKey, err := h.client.DeriveScriptKey(ctx)
	require.NoError(h.t, err)
	internalKey, err := h.client.DeriveInternalKey(ctx)
	require.NoError(h.t, err)

	minted := h.mintTo(scriptKey, internalKey, receivedUnits, tag)
	registered, err := h.wallet.ImportProofFile(
		ctx, &tapsdk.ProofFile{RawProofFile: minted.proofFile},
	)
	require.NoError(h.t, err)
	require.EqualValues(h.t, receivedUnits, registered.Amount)

	return minted
}

// fundingInput is a key-spend Taproot coin that pays for an anchor
// transaction in tests.
type fundingInput struct {
	privKey  *btcec.PrivateKey
	outpoint wire.OutPoint
	txOut    *wire.TxOut
}

// newFundingInput returns a coin of the given value.
func newFundingInput(t *testing.T, seed byte, value int64) *fundingInput {
	t.Helper()

	digest := sha256.Sum256([]byte{0xfe, seed})
	privKey, _ := btcec.PrivKeyFromBytes(digest[:])
	outputKey := txscript.ComputeTaprootKeyNoScript(privKey.PubKey())
	pkScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)

	return &fundingInput{
		privKey: privKey,
		outpoint: wire.OutPoint{
			Hash:  sha256.Sum256([]byte{0xfd, seed}),
			Index: 1,
		},
		txOut: &wire.TxOut{Value: value, PkScript: pkScript},
	}
}

// sign adds the coin's final witness to the input of the packet that spends
// it.
func (f *fundingInput) sign(t *testing.T, packet *psbt.Packet) {
	t.Helper()

	prevOuts := make(map[wire.OutPoint]*wire.TxOut)
	index := -1
	for idx, txIn := range packet.UnsignedTx.TxIn {
		prevOuts[txIn.PreviousOutPoint] = packet.Inputs[idx].WitnessUtxo
		if txIn.PreviousOutPoint == f.outpoint {
			index = idx
		}
	}
	require.GreaterOrEqual(t, index, 0)

	fetcher := txscript.NewMultiPrevOutFetcher(prevOuts)
	sigHashes := txscript.NewTxSigHashes(packet.UnsignedTx, fetcher)
	sig, err := txscript.RawTxInTaprootSignature(
		packet.UnsignedTx, sigHashes, index, f.txOut.Value,
		f.txOut.PkScript, nil, txscript.SigHashDefault, f.privKey,
	)
	require.NoError(t, err)

	var witness bytes.Buffer
	require.NoError(t, psbt.WriteTxWitness(&witness, [][]byte{sig}))
	packet.Inputs[index].FinalScriptWitness = witness.Bytes()
}
