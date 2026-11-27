//go:build itest

package itest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
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
	"github.com/lightninglabs/tap-sdk/embedded/courier"
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
	log  *embeddedLog
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

	// The engine's addresses name the courier of Bob's V2 addresses.
	bobCourier, err := url.Parse(envOr(
		"TAPD_BOB_PROOF_COURIER_ADDR",
		"authmailbox+universerpc://"+defaultBobProofCourierHost,
	))
	require.NoError(t, err)

	// An address names a tapd by its host in the compose network. The
	// engine runs on the host, where the tapd has another address.
	endpoints := map[string]courier.Endpoint{
		bobCourier.Host:          courierEndpoint(t, bobTapd),
		defaultAliceUniverseHost: courierEndpoint(t, aliceTapd),
	}

	var (
		chain  = newBitcoindChain(h.bitcoindHost)
		store  = embedded.NewMemoryStore()
		logger = &embeddedLog{}
	)
	client, err := embedded.NewClient(&embedded.Config{
		Network:   tapsdk.NetworkRegtest,
		Store:     store,
		KeyRing:   embedded.NewLocalKeyRing(keys),
		Signer:    embedded.NewKeyRingSigner(keys),
		Chain:     chain,
		GroupKeys: groupKeys,
		Logger:    logger,
		Receive: &embedded.ReceiveConfig{
			Store:          store,
			Keys:           embedded.NewKeyRingReceiveKeys(keys),
			Dial:           courier.NewDialer(endpoints),
			DefaultCourier: bobCourier,
		},
		Send: &embedded.SendConfig{
			Dial: courier.NewDialer(endpoints),
		},
	})
	require.NoError(t, err)
	require.NoError(t, client.ResumeTransfers(context.Background()))

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
		log:    logger,
	}
}

// courierEndpoint returns how the engine reaches the courier of a tapd node
// from the host, trusting the node's own certificate.
func courierEndpoint(t testing.TB, node tapdNodeConfig) courier.Endpoint {
	t.Helper()

	certPEM, err := os.ReadFile(node.tlsCertPath(t))
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))

	return courier.Endpoint{
		Target: envOr(node.grpcHostEnv, node.defaultGRPCHost),
		TLS: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
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

// WaitForAddressReceive polls until the engine lists one completed transfer
// to the address. With sync, each poll first runs one pass over the engine's
// mailboxes. Without it the engine must hold its mailboxes open.
func (n *EmbeddedNode) WaitForAddressReceive(t testing.TB,
	ctx context.Context, addr *tapsdk.Address, sync bool,
	timeout time.Duration) *tapsdk.AddressEvent {

	t.Helper()

	return n.WaitForAddressReceiveStatus(
		t, ctx, addr, sync, tapsdk.AddressEventStatusCompleted, timeout,
	)
}

// WaitForAddressReceiveStatus is WaitForAddressReceive for a transfer the
// engine lists with the given status.
func (n *EmbeddedNode) WaitForAddressReceiveStatus(t testing.TB,
	ctx context.Context, addr *tapsdk.Address, sync bool,
	status tapsdk.AddressEventStatus,
	timeout time.Duration) *tapsdk.AddressEvent {

	t.Helper()

	var (
		event      *tapsdk.AddressEvent
		lastStatus string
	)
	require.Eventuallyf(t, func() bool {
		if sync {
			if err := n.Client.SyncReceives(ctx); err != nil {
				lastStatus = fmt.Sprintf("sync failed: %v", err)

				return false
			}
		}

		events, err := n.Client.AddrReceives(
			ctx, &tapsdk.AddressReceivesQuery{
				FilterAddr: addr.Encoded,
			},
		)
		switch {
		case err != nil:
			lastStatus = fmt.Sprintf("receive lookup failed: %v",
				err)

			return false

		case len(events) != 1:
			lastStatus = fmt.Sprintf("%d receives listed",
				len(events))

			return false
		}
		event = events[0]
		lastStatus = fmt.Sprintf("status = %d", event.Status)

		return event.Status == status
	}, timeout, time.Second,
		"embedded address %s never listed a receive with status %d; "+
			"last observation: %s", addr.Encoded, status,
		observed{&lastStatus},
	)

	return event
}

// WaitForReceiveState polls until the engine records one transfer to the
// anchor output in the given state, and returns the record.
func (n *EmbeddedNode) WaitForReceiveState(t testing.TB, ctx context.Context,
	outpoint tapsdk.Outpoint,
	state embedded.ReceiveState) *embedded.ReceiveRecord {

	t.Helper()

	var (
		record     *embedded.ReceiveRecord
		lastStatus string
		anchor     = wire.OutPoint{
			Hash:  chainhash.Hash(outpoint.Txid),
			Index: outpoint.Index,
		}
	)
	require.Eventuallyf(t, func() bool {
		records, err := n.Store.ListReceives(
			ctx, embedded.ReceiveFilter{Outpoint: &anchor},
		)
		switch {
		case err != nil:
			lastStatus = fmt.Sprintf("receive lookup failed: %v",
				err)

			return false

		case len(records) != 1:
			lastStatus = fmt.Sprintf("%d receives recorded",
				len(records))

			return false
		}
		record = &records[0]
		lastStatus = fmt.Sprintf("state = %d", record.State)

		return record.State == state
	}, defaultWaitTimeout, embeddedChainPollInterval,
		"embedded receive at %v never reached state %d; last "+
			"observation: %s", anchor, state, observed{&lastStatus},
	)

	return record
}

// observed is the last status a poll saw. It is read when the failure
// message is formatted, after the poll ended.
type observed struct {
	status *string
}

// String returns the status.
func (o observed) String() string {
	return lastObservation(*o.status)
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
