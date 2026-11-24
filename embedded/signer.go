package embedded

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/vm"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/keychain"
)

// PrivKeyDeriver derives the private key of a key descriptor. lnd's
// keychain.SecretKeyRing implements it.
type PrivKeyDeriver interface {
	// DerivePrivKey derives the private key of a key descriptor.
	DerivePrivKey(desc keychain.KeyDescriptor) (*btcec.PrivateKey, error)
}

// NextKeyDeriver derives the next unused key of a family. lnd's
// keychain.KeyRing implements it.
type NextKeyDeriver interface {
	// DeriveNextKey derives the next unused key of the family.
	DeriveNextKey(family keychain.KeyFamily) (keychain.KeyDescriptor,
		error)
}

// localKeyRing derives keys with a local key ring.
type localKeyRing struct {
	keys NextKeyDeriver
}

// NewLocalKeyRing returns a KeyRing that derives with a local key ring.
func NewLocalKeyRing(keys NextKeyDeriver) KeyRing {
	return &localKeyRing{keys: keys}
}

// DeriveNextKey derives the next unused key of the family.
func (k *localKeyRing) DeriveNextKey(ctx context.Context,
	family keychain.KeyFamily) (keychain.KeyDescriptor, error) {

	if err := ctx.Err(); err != nil {
		return keychain.KeyDescriptor{}, err
	}

	return k.keys.DeriveNextKey(family)
}

// keyRingSigner signs Taproot inputs with the private keys of a local key
// ring.
type keyRingSigner struct {
	keys PrivKeyDeriver
}

// NewKeyRingSigner returns a Signer that signs with the private keys that
// keys derives.
func NewKeyRingSigner(keys PrivKeyDeriver) Signer {
	return &keyRingSigner{keys: keys}
}

// SignOutputRaw signs a Taproot input with the private key of the
// descriptor's key. Like lnd's signer, it returns the signature without a
// sighash flag.
func (s *keyRingSigner) SignOutputRaw(ctx context.Context, tx *wire.MsgTx,
	signDesc *input.SignDescriptor) (input.Signature, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if signDesc.Output == nil ||
		!txscript.IsPayToTaproot(signDesc.Output.PkScript) {

		return nil, fmt.Errorf("input %d does not spend a Taproot "+
			"output", signDesc.InputIndex)
	}

	privKey, err := s.keys.DerivePrivKey(signDesc.KeyDesc)
	if err != nil {
		return nil, fmt.Errorf("derive signing key: %w", err)
	}

	switch {
	case signDesc.SingleTweak != nil:
		privKey = input.TweakPrivKey(privKey, signDesc.SingleTweak)

	case signDesc.DoubleTweak != nil:
		privKey = input.DeriveRevocationPrivKey(
			privKey, signDesc.DoubleTweak,
		)
	}

	sigHashes := signDesc.SigHashes
	if sigHashes == nil {
		sigHashes = txscript.NewTxSigHashes(
			tx, signDesc.PrevOutputFetcher,
		)
	}

	var rawSig []byte
	switch signDesc.SignMethod {
	case input.TaprootKeySpendBIP0086SignMethod,
		input.TaprootKeySpendSignMethod:

		rawSig, err = txscript.RawTxInTaprootSignature(
			tx, sigHashes, signDesc.InputIndex,
			signDesc.Output.Value, signDesc.Output.PkScript,
			signDesc.TapTweak, signDesc.HashType, privKey,
		)

	case input.TaprootScriptSpendSignMethod:
		rawSig, err = txscript.RawTxInTapscriptSignature(
			tx, sigHashes, signDesc.InputIndex,
			signDesc.Output.Value, signDesc.Output.PkScript,
			txscript.NewBaseTapLeaf(signDesc.WitnessScript),
			signDesc.HashType, privKey,
		)

	default:
		return nil, fmt.Errorf("unsupported sign method %v",
			signDesc.SignMethod)
	}
	if err != nil {
		return nil, err
	}

	sig, err := schnorr.ParseSignature(rawSig[:schnorr.SignatureSize])
	if err != nil {
		return nil, err
	}

	return sig, nil
}

// keyRingReceiveKeys decrypts and authenticates with the private keys of a
// local key ring.
type keyRingReceiveKeys struct {
	keys PrivKeyDeriver
}

// NewKeyRingReceiveKeys returns ReceiveKeys that use the private keys that
// keys derives.
func NewKeyRingReceiveKeys(keys PrivKeyDeriver) ReceiveKeys {
	return &keyRingReceiveKeys{keys: keys}
}

// ECDH returns the SHA-256 of the compressed shared point of the key at
// locator and pubKey.
func (k *keyRingReceiveKeys) ECDH(ctx context.Context,
	locator keychain.KeyLocator, pubKey *btcec.PublicKey) ([32]byte,
	error) {

	if err := ctx.Err(); err != nil {
		return [32]byte{}, err
	}

	privKey, err := k.keys.DerivePrivKey(keychain.KeyDescriptor{
		KeyLocator: locator,
	})
	if err != nil {
		return [32]byte{}, fmt.Errorf("derive key: %w", err)
	}

	return (&keychain.PrivKeyECDH{PrivKey: privKey}).ECDH(pubKey)
}

// SignMessageSchnorr signs the SHA-256 of msg with the key at locator.
func (k *keyRingReceiveKeys) SignMessageSchnorr(ctx context.Context,
	locator keychain.KeyLocator, msg []byte) (*schnorr.Signature, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	privKey, err := k.keys.DerivePrivKey(keychain.KeyDescriptor{
		KeyLocator: locator,
	})
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	return schnorr.Sign(privKey, chainhash.HashB(msg))
}

// virtualTxSigner signs asset witnesses with the host's signer. tapd's
// signing interface carries no context, so it holds the one of the engine
// call it signs for.
type virtualTxSigner struct {
	ctx    context.Context
	signer Signer
}

// SignVirtualTx signs the input of a virtual transaction that signDesc
// describes.
func (s *virtualTxSigner) SignVirtualTx(signDesc *asset.SignDescriptor,
	tx *wire.MsgTx, prevOut *wire.TxOut) (*schnorr.Signature, error) {

	fetcher := txscript.NewCannedPrevOutputFetcher(
		prevOut.PkScript, prevOut.Value,
	)
	sig, err := s.signer.SignOutputRaw(s.ctx, tx, &input.SignDescriptor{
		KeyDesc:           signDesc.KeyDesc,
		SingleTweak:       signDesc.SingleTweak,
		DoubleTweak:       signDesc.DoubleTweak,
		TapTweak:          signDesc.TapTweak,
		WitnessScript:     signDesc.WitnessScript,
		SignMethod:        signDesc.SignMethod,
		Output:            signDesc.Output,
		HashType:          signDesc.HashType,
		SigHashes:         txscript.NewTxSigHashes(tx, fetcher),
		PrevOutputFetcher: fetcher,
		InputIndex:        signDesc.InputIndex,
	})
	if err != nil {
		return nil, fmt.Errorf("sign virtual transaction: %w", err)
	}

	return schnorrSignature(sig)
}

// schnorrSignature returns sig as a Schnorr signature.
func schnorrSignature(sig input.Signature) (*schnorr.Signature, error) {
	if schnorrSig, ok := sig.(*schnorr.Signature); ok {
		return schnorrSig, nil
	}

	schnorrSig, err := schnorr.ParseSignature(sig.Serialize())
	if err != nil {
		return nil, fmt.Errorf("signer returned no Schnorr "+
			"signature: %w", err)
	}

	return schnorrSig, nil
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
func (e *engine) signAnchorPsbt(ctx context.Context,
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
		signMethod := input.TaprootKeySpendSignMethod
		if len(pIn.TaprootMerkleRoot) == 0 {
			signMethod = input.TaprootKeySpendBIP0086SignMethod
		}

		prevOut := prevOuts[pkt.UnsignedTx.TxIn[idx].PreviousOutPoint]
		sig, err := e.signer.SignOutputRaw(
			ctx, pkt.UnsignedTx, &input.SignDescriptor{
				KeyDesc:           keyDesc,
				TapTweak:          pIn.TaprootMerkleRoot,
				SignMethod:        signMethod,
				Output:            prevOut,
				HashType:          pIn.SighashType,
				SigHashes:         sigHashes,
				PrevOutputFetcher: fetcher,
				InputIndex:        idx,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("input %d: sign: %w", idx, err)
		}
		schnorrSig, err := schnorrSignature(sig)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", idx, err)
		}

		sigBytes := schnorrSig.Serialize()
		if pIn.SighashType != txscript.SigHashDefault {
			sigBytes = append(sigBytes, byte(pIn.SighashType))
		}
		pIn.TaprootKeySpendSig = sigBytes
	}

	return pkt, nil
}

// btcutilAmount converts a satoshi value to a btcutil amount.
func btcutilAmount(sats int64) btcutil.Amount {
	return btcutil.Amount(sats)
}
