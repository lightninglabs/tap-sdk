package embedded

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/vm"
)

// keyRingSigner signs asset witnesses with keys of the wallet's key ring.
type keyRingSigner struct {
	keys KeyRing
}

// SignVirtualTx signs a virtual transaction with the key the signing
// descriptor names.
func (s *keyRingSigner) SignVirtualTx(signDesc *asset.SignDescriptor,
	tx *wire.MsgTx, prevOut *wire.TxOut) (*schnorr.Signature, error) {

	privKey, err := s.keys.DerivePrivKey(signDesc.KeyDesc)
	if err != nil {
		return nil, fmt.Errorf("derive signing key: %w", err)
	}

	return asset.SignVirtualTx(privKey, signDesc, tx, prevOut)
}

// witnessValidator validates the witnesses of an asset transfer with the
// Taproot Assets VM.
type witnessValidator struct{}

// ValidateWitnesses validates the created witnesses of an asset transfer.
func (witnessValidator) ValidateWitnesses(newAsset *asset.Asset,
	splitAssets []*commitment.SplitAsset,
	prevAssets commitment.InputSet) error {

	return vm.ValidateWitnesses(newAsset, splitAssets, prevAssets)
}

// signAnchorPsbt adds a Taproot key spend signature to every input of the
// packet that carries the derivation path of a wallet key.
func (e *engine) signAnchorPsbt(_ context.Context,
	pkt *psbt.Packet) (*psbt.Packet, error) {

	prevOuts := make(map[wire.OutPoint]*wire.TxOut, len(pkt.Inputs))
	for idx, txIn := range pkt.UnsignedTx.TxIn {
		pIn := pkt.Inputs[idx]

		switch {
		case pIn.WitnessUtxo != nil:
			prevOuts[txIn.PreviousOutPoint] = pIn.WitnessUtxo

		case pIn.NonWitnessUtxo != nil:
			outIdx := txIn.PreviousOutPoint.Index
			if int(outIdx) >= len(pIn.NonWitnessUtxo.TxOut) {
				return nil, fmt.Errorf("input %d spends a "+
					"missing output", idx)
			}
			prevOuts[txIn.PreviousOutPoint] =
				pIn.NonWitnessUtxo.TxOut[outIdx]

		default:
			return nil, fmt.Errorf("input %d has no UTXO "+
				"information", idx)
		}
	}
	fetcher := txscript.NewMultiPrevOutFetcher(prevOuts)
	sigHashes := txscript.NewTxSigHashes(pkt.UnsignedTx, fetcher)

	for idx := range pkt.Inputs {
		pIn := &pkt.Inputs[idx]
		if len(pIn.TaprootBip32Derivation) == 0 ||
			len(pIn.Bip32Derivation) == 0 {

			continue
		}

		keyDesc, err := tappsbt.KeyDescFromBip32Derivation(
			pIn.Bip32Derivation[0],
		)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", idx, err)
		}
		privKey, err := e.keys.DerivePrivKey(keyDesc)
		if err != nil {
			return nil, fmt.Errorf("input %d: derive signing "+
				"key: %w", idx, err)
		}

		prevOut := prevOuts[pkt.UnsignedTx.TxIn[idx].PreviousOutPoint]
		sig, err := txscript.RawTxInTaprootSignature(
			pkt.UnsignedTx, sigHashes, idx, prevOut.Value,
			prevOut.PkScript, pIn.TaprootMerkleRoot,
			pIn.SighashType, privKey,
		)
		if err != nil {
			return nil, fmt.Errorf("input %d: sign: %w", idx, err)
		}
		pIn.TaprootKeySpendSig = sig
	}

	return pkt, nil
}

// btcutilAmount converts a satoshi value to a btcutil amount.
func btcutilAmount(sats int64) btcutil.Amount {
	return btcutil.Amount(sats)
}
