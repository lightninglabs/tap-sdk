//go:build itest

package itest

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/embedded"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/stretchr/testify/require"
)

// EmbeddedNode is an embedded engine wallet that runs next to the tapd nodes
// of the harness. It keeps its state in memory, derives its keys from a seed
// of its own and reads the chain from the harness bitcoind.
type EmbeddedNode struct {
	Client *embedded.Client
	Wallet *tapsdk.Wallet
	Store  *embedded.MemoryStore

	keys *hdKeyRing
}

// NewEmbeddedNode starts an embedded engine that trusts the given asset
// groups.
func (h *TestHarness) NewEmbeddedNode(t testing.TB,
	groupKeys ...tapsdk.PubKey) *EmbeddedNode {

	t.Helper()

	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	require.NoError(t, err)
	keys, err := newHDKeyRing(seed, &chaincfg.RegressionNetParams)
	require.NoError(t, err)

	var (
		chain  = newBitcoindChain(h.bitcoindHost)
		store  = embedded.NewMemoryStore()
		logger = &embeddedLog{}
	)
	client, err := embedded.NewClient(&embedded.Config{
		Network:   tapsdk.NetworkRegtest,
		Store:     store,
		KeyRing:   keys,
		Chain:     chain,
		GroupKeys: groupKeys,
		Logger:    logger,
	})
	require.NoError(t, err)
	require.NoError(t, client.Start(context.Background()))

	t.Cleanup(func() {
		_ = client.Close()
		chain.Stop()

		if t.Failed() {
			t.Logf("embedded engine log:\n%s", logger.String())
		}
	})

	return &EmbeddedNode{
		Client: client,
		Wallet: tapsdk.NewWallet(client, tapsdk.NetworkRegtest),
		Store:  store,
		keys:   keys,
	}
}

// embeddedLog keeps the background events of an engine for the report of a
// failed test.
type embeddedLog struct {
	mu    sync.Mutex
	lines []string
}

// Infof records a transfer's progress.
func (l *embeddedLog) Infof(format string, args ...any) {
	l.add("INF", format, args...)
}

// Errorf records a failure the engine retries.
func (l *embeddedLog) Errorf(format string, args ...any) {
	l.add("ERR", format, args...)
}

// add records one line.
func (l *embeddedLog) add(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lines = append(l.lines, fmt.Sprintf(
		"%s [%s] %s", time.Now().Format(time.StampMilli), level,
		fmt.Sprintf(format, args...),
	))
}

// String returns the recorded lines.
func (l *embeddedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return strings.Join(l.lines, "\n")
}

// WaitForTransferState polls until the transfer anchored by txid is recorded
// in the given state.
func (n *EmbeddedNode) WaitForTransferState(t testing.TB,
	ctx context.Context, txid chainhash.Hash,
	state embedded.TransferState) {

	t.Helper()

	var lastStatus string
	require.Eventuallyf(t, func() bool {
		transfers, err := n.Store.ListTransfers(ctx, &txid)
		switch {
		case err != nil:
			lastStatus = fmt.Sprintf("transfer lookup failed: %v",
				err)

			return false

		case len(transfers) != 1:
			lastStatus = fmt.Sprintf("%d transfers recorded",
				len(transfers))

			return false
		}
		lastStatus = fmt.Sprintf("state = %d", transfers[0].State)

		return transfers[0].State == state
	}, defaultWaitTimeout, embeddedChainPollInterval,
		"embedded transfer %v never reached state %d; last "+
			"observation: %s", txid, state,
		lastObservation(lastStatus),
	)
}

// SignAnchorPsbt signs and finalizes the Taproot key spend of every anchor
// input that carries the derivation path of a key of the engine's key ring.
// It is to the engine what lnd's PSBT signer is to a tapd.
func (n *EmbeddedNode) SignAnchorPsbt(_ context.Context,
	anchorPsbt []byte) ([]byte, error) {

	packet, err := psbt.NewFromRawBytes(bytes.NewReader(anchorPsbt), false)
	if err != nil {
		return nil, err
	}

	prevOuts := txscript.NewMultiPrevOutFetcher(nil)
	for idx, txIn := range packet.UnsignedTx.TxIn {
		if packet.Inputs[idx].WitnessUtxo == nil {
			return nil, fmt.Errorf("input %d has no witness UTXO",
				idx)
		}
		prevOuts.AddPrevOut(
			txIn.PreviousOutPoint, packet.Inputs[idx].WitnessUtxo,
		)
	}
	sigHashes := txscript.NewTxSigHashes(packet.UnsignedTx, prevOuts)

	for idx := range packet.Inputs {
		pIn := &packet.Inputs[idx]
		if len(pIn.Bip32Derivation) == 0 ||
			len(pIn.FinalScriptWitness) != 0 {

			continue
		}

		keyDesc, err := tappsbt.KeyDescFromBip32Derivation(
			pIn.Bip32Derivation[0],
		)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", idx, err)
		}
		privKey, err := n.keys.DerivePrivKey(keyDesc)
		if err != nil {
			// The key belongs to another signer.
			continue
		}

		sig, err := txscript.RawTxInTaprootSignature(
			packet.UnsignedTx, sigHashes, idx,
			pIn.WitnessUtxo.Value, pIn.WitnessUtxo.PkScript,
			pIn.TaprootMerkleRoot, pIn.SighashType, privKey,
		)
		if err != nil {
			return nil, fmt.Errorf("input %d: sign: %w", idx, err)
		}

		var witness bytes.Buffer
		err = psbt.WriteTxWitness(&witness, wire.TxWitness{sig})
		if err != nil {
			return nil, err
		}
		pIn.FinalScriptWitness = witness.Bytes()
	}

	var signed bytes.Buffer
	if err := packet.Serialize(&signed); err != nil {
		return nil, err
	}

	return signed.Bytes(), nil
}

// ProofForEmbedded moves amount units of an asset Alice minted from her tapd
// to new keys of the engine with an interactive transfer and confirms it. The
// engine's output keeps the asset version of the mint. It returns that output
// and the proof file Alice exports for it.
func (h *TestHarness) ProofForEmbedded(t testing.TB, ctx context.Context,
	node *EmbeddedNode, minted *MintResult,
	amount uint64) (*tapsdk.TransferOutput, *tapsdk.ProofFile) {

	t.Helper()

	keys, err := node.Wallet.DeriveKeys(ctx)
	require.NoError(t, err)

	transfer, err := h.AliceWallet.NewInteractiveTxBuilder().
		SetAsset(minted.Ref, amount).
		SetAssetVersion(tapsdk.AssetVersion(minted.Asset.Version)).
		SetReceiverKeys(*keys).
		Execute(ctx)
	require.NoError(t, err)

	var output *tapsdk.TransferOutput
	for idx := range transfer.Outputs {
		if transfer.Outputs[idx].ScriptKey == keys.ScriptKey.PubKey {
			output = &transfer.Outputs[idx]
		}
	}
	require.NotNil(t, output, "transfer has no output for the engine")
	require.Equal(t, amount, output.Amount)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	proofFile := h.WaitForProofFile(
		t, ctx, h.AliceWallet,
		tapsdk.AssetRefFromAssetID(output.IssuanceID),
		output.ScriptKey, &output.AnchorOutpoint,
	)

	return output, proofFile
}

// SendToEmbedded moves amount units of an asset Alice minted from her tapd
// into the engine: it runs ProofForEmbedded and imports the proof file. It
// returns the asset output the engine registered and its proof file.
func (h *TestHarness) SendToEmbedded(t testing.TB, ctx context.Context,
	node *EmbeddedNode, minted *MintResult,
	amount uint64) (*tapsdk.RegisteredAsset, *tapsdk.ProofFile) {

	t.Helper()

	output, proofFile := h.ProofForEmbedded(t, ctx, node, minted, amount)

	registered, err := node.Wallet.ImportProofFile(ctx, proofFile)
	require.NoError(t, err)
	require.Equal(t, output.IssuanceID, registered.IssuanceID)
	require.Equal(t, output.ScriptKey, registered.ScriptKey)
	require.Equal(t, output.AnchorOutpoint, registered.Outpoint)
	require.Equal(t, amount, registered.Amount)

	return registered, proofFile
}
