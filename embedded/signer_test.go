package embedded

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/stretchr/testify/require"
)

// countingSigner counts the inputs it signs for another signer.
type countingSigner struct {
	signer Signer
	calls  int
}

// SignOutputRaw signs with the wrapped signer.
func (s *countingSigner) SignOutputRaw(ctx context.Context, tx *wire.MsgTx,
	signDesc *input.SignDescriptor) (input.Signature, error) {

	s.calls++

	return s.signer.SignOutputRaw(ctx, tx, signDesc)
}

// spendTx returns a transaction spending prevOut and a fetcher for it.
func spendTx(prevOut *wire.TxOut) (*wire.MsgTx,
	txscript.PrevOutputFetcher) {

	tx := wire.NewMsgTx(2)
	tx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: chainhash.Hash{1}},
	})
	tx.AddTxOut(&wire.TxOut{Value: prevOut.Value - 500, PkScript: []byte{
		txscript.OP_TRUE,
	}})
	fetcher := txscript.NewCannedPrevOutputFetcher(
		prevOut.PkScript, prevOut.Value,
	)

	return tx, fetcher
}

// requireValidSpend runs the script engine on input 0 of tx.
func requireValidSpend(t *testing.T, tx *wire.MsgTx, prevOut *wire.TxOut,
	fetcher txscript.PrevOutputFetcher) {

	t.Helper()

	vm, err := txscript.NewEngine(
		prevOut.PkScript, tx, 0, txscript.StandardVerifyFlags, nil,
		txscript.NewTxSigHashes(tx, fetcher), prevOut.Value, fetcher,
	)
	require.NoError(t, err)
	require.NoError(t, vm.Execute())
}

// TestKeyRingSigner checks that the local signer produces valid Taproot key
// and script spends, and returns signatures without a sighash flag.
func TestKeyRingSigner(t *testing.T) {
	t.Parallel()

	keys := newTestKeyRing(1)
	keyDesc, err := keys.DeriveNextKey(7)
	require.NoError(t, err)
	signer := NewKeyRingSigner(keys)

	leafScript, err := txscript.NewScriptBuilder().
		AddData(schnorr.SerializePubKey(keyDesc.PubKey)).
		AddOp(txscript.OP_CHECKSIG).
		Script()
	require.NoError(t, err)
	leaf := txscript.NewBaseTapLeaf(leafScript)
	tree := txscript.AssembleTaprootScriptTree(leaf)
	rootHash := tree.RootNode.TapHash()
	outputKey := txscript.ComputeTaprootOutputKey(
		keyDesc.PubKey, rootHash[:],
	)
	pkScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)
	prevOut := &wire.TxOut{Value: 10_000, PkScript: pkScript}

	for _, hashType := range []txscript.SigHashType{
		txscript.SigHashDefault, txscript.SigHashAll,
	} {
		tx, fetcher := spendTx(prevOut)
		signDesc := &input.SignDescriptor{
			KeyDesc:           keyDesc,
			TapTweak:          rootHash[:],
			SignMethod:        input.TaprootKeySpendSignMethod,
			Output:            prevOut,
			HashType:          hashType,
			PrevOutputFetcher: fetcher,
		}
		sig, err := signer.SignOutputRaw(t.Context(), tx, signDesc)
		require.NoError(t, err)
		require.Len(t, sig.Serialize(), schnorr.SignatureSize)

		witness := sig.Serialize()
		if hashType != txscript.SigHashDefault {
			witness = append(witness, byte(hashType))
		}
		tx.TxIn[0].Witness = wire.TxWitness{witness}
		requireValidSpend(t, tx, prevOut, fetcher)

		tx, fetcher = spendTx(prevOut)
		signDesc.SignMethod = input.TaprootScriptSpendSignMethod
		signDesc.TapTweak = nil
		signDesc.WitnessScript = leafScript
		sig, err = signer.SignOutputRaw(t.Context(), tx, signDesc)
		require.NoError(t, err)

		witness = sig.Serialize()
		if hashType != txscript.SigHashDefault {
			witness = append(witness, byte(hashType))
		}
		ctrlBlock := tree.LeafMerkleProofs[0].ToControlBlock(
			keyDesc.PubKey,
		)
		ctrlBytes, err := ctrlBlock.ToBytes()
		require.NoError(t, err)
		tx.TxIn[0].Witness = wire.TxWitness{
			witness, leafScript, ctrlBytes,
		}
		requireValidSpend(t, tx, prevOut, fetcher)
	}

	// A non-Taproot output is refused.
	tx, fetcher := spendTx(prevOut)
	_, err = signer.SignOutputRaw(t.Context(), tx, &input.SignDescriptor{
		KeyDesc:           keyDesc,
		SignMethod:        input.TaprootKeySpendSignMethod,
		Output:            &wire.TxOut{Value: 1, PkScript: []byte{1}},
		PrevOutputFetcher: fetcher,
	})
	require.ErrorContains(t, err, "does not spend a Taproot output")
}

// TestSignAnchorPsbt checks that the engine signs its anchor inputs with
// the configured signer and appends a non-default sighash flag.
func TestSignAnchorPsbt(t *testing.T) {
	t.Parallel()

	keys := newTestKeyRing(2)
	keyDesc, err := keys.DeriveNextKey(keychain.KeyFamily(212))
	require.NoError(t, err)
	merkleRoot := bytes.Repeat([]byte{0x42}, 32)
	outputKey := txscript.ComputeTaprootOutputKey(
		keyDesc.PubKey, merkleRoot,
	)
	pkScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)
	prevOut := &wire.TxOut{Value: 10_000, PkScript: pkScript}

	for _, hashType := range []txscript.SigHashType{
		txscript.SigHashDefault, txscript.SigHashAll,
	} {
		tx, fetcher := spendTx(prevOut)
		pkt, err := psbt.NewFromUnsignedTx(tx)
		require.NoError(t, err)

		derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
			keyDesc, 1,
		)
		pIn := &pkt.Inputs[0]
		pIn.WitnessUtxo = prevOut
		pIn.Bip32Derivation = []*psbt.Bip32Derivation{derivation}
		pIn.TaprootBip32Derivation = []*psbt.TaprootBip32Derivation{
			trDerivation,
		}
		pIn.TaprootMerkleRoot = merkleRoot
		pIn.SighashType = hashType

		signer := &countingSigner{signer: NewKeyRingSigner(keys)}
		e := &engine{signer: signer}
		pkt, err = e.signAnchorPsbt(t.Context(), pkt)
		require.NoError(t, err)
		require.Equal(t, 1, signer.calls)

		tx.TxIn[0].Witness = wire.TxWitness{
			pkt.Inputs[0].TaprootKeySpendSig,
		}
		requireValidSpend(t, tx, prevOut, fetcher)
	}
}

// stallingSigner answers only once the call's context is done.
type stallingSigner struct{}

// SignOutputRaw waits for ctx and returns its error.
func (stallingSigner) SignOutputRaw(ctx context.Context, _ *wire.MsgTx,
	_ *input.SignDescriptor) (input.Signature, error) {

	<-ctx.Done()

	return nil, ctx.Err()
}

// TestSigningHonorsDeadline checks that both signing paths hand the signer
// the context of the engine call, so a stalled signer ends at its deadline.
func TestSigningHonorsDeadline(t *testing.T) {
	t.Parallel()

	keys := newTestKeyRing(3)
	keyDesc, err := keys.DeriveNextKey(keychain.KeyFamily(212))
	require.NoError(t, err)
	pkScript, err := txscript.PayToTaprootScript(
		txscript.ComputeTaprootOutputKey(keyDesc.PubKey, nil),
	)
	require.NoError(t, err)
	prevOut := &wire.TxOut{Value: 10_000, PkScript: pkScript}

	tx, _ := spendTx(prevOut)
	pkt, err := psbt.NewFromUnsignedTx(tx)
	require.NoError(t, err)
	derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
		keyDesc, 1,
	)
	pkt.Inputs[0].WitnessUtxo = prevOut
	pkt.Inputs[0].Bip32Derivation = []*psbt.Bip32Derivation{derivation}
	pkt.Inputs[0].TaprootBip32Derivation = []*psbt.TaprootBip32Derivation{
		trDerivation,
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	e := &engine{signer: stallingSigner{}}
	_, err = e.signAnchorPsbt(ctx, pkt)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	signer := &virtualTxSigner{ctx: ctx, signer: stallingSigner{}}
	_, err = signer.SignVirtualTx(&asset.SignDescriptor{
		KeyDesc: keyDesc,
	}, tx, prevOut)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// The local implementations refuse a call whose deadline passed.
	_, err = NewKeyRingSigner(keys).SignOutputRaw(ctx, tx,
		&input.SignDescriptor{Output: prevOut})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = NewLocalKeyRing(keys).DeriveNextKey(ctx, 212)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestConfigRequiresSigner checks that a client needs a signer.
func TestConfigRequiresSigner(t *testing.T) {
	t.Parallel()

	_, err := NewClient(&Config{
		Network: tapsdk.NetworkRegtest,
		Store:   NewMemoryStore(),
		KeyRing: NewLocalKeyRing(newTestKeyRing(1)),
		Chain:   newTestChain(t),
	})
	require.ErrorContains(t, err, "signer is required")
}
