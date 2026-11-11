//go:build itest

package itest

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc/walletrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const (
	defaultAliceLndHost = "localhost:10009"

	// lndFundingKeyFamily is the lnd key family that holds the Bitcoin
	// inputs the tests fund anchor transactions with. No lnd or tapd
	// subsystem derives keys from it.
	lndFundingKeyFamily keychain.KeyFamily = 9217
)

// lndWalletKit is the wallet kit service of a harness lnd.
type lndWalletKit struct {
	client walletrpc.WalletKitClient
	macHex string
}

// newAliceLndWalletKit connects to the wallet kit service of lnd-alice.
func newAliceLndWalletKit(t testing.TB) *lndWalletKit {
	t.Helper()

	tlsPath := os.Getenv("LND_ALICE_TLS")
	if tlsPath == "" {
		tlsPath = extractDockerFile(
			t, "tap-sdk-lnd-alice", "/root/.lnd/tls.cert",
		)
	}

	macaroonPath := os.Getenv("LND_ALICE_MAC")
	if macaroonPath == "" {
		macaroonPath = extractDockerFile(
			t, "tap-sdk-lnd-alice",
			"/root/.lnd/data/chain/bitcoin/regtest/admin.macaroon",
		)
	}

	tlsCreds, err := credentials.NewClientTLSFromFile(tlsPath, "")
	require.NoError(t, err)

	conn, err := grpc.NewClient(
		envOr("LND_ALICE_HOST", defaultAliceLndHost),
		grpc.WithTransportCredentials(tlsCreds),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	macBytes, err := os.ReadFile(macaroonPath)
	require.NoError(t, err)

	return &lndWalletKit{
		client: walletrpc.NewWalletKitClient(conn),
		macHex: hex.EncodeToString(macBytes),
	}
}

// rpcContext attaches the macaroon of the lnd to an outgoing call.
func (l *lndWalletKit) rpcContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "macaroon", l.macHex)
}

// deriveNextKey derives the next key of a key family of the lnd wallet.
func (l *lndWalletKit) deriveNextKey(ctx context.Context,
	family keychain.KeyFamily) (keychain.KeyDescriptor, error) {

	desc, err := l.client.DeriveNextKey(
		l.rpcContext(ctx), &walletrpc.KeyReq{KeyFamily: int32(family)},
	)
	if err != nil {
		return keychain.KeyDescriptor{}, err
	}
	if desc.KeyLoc == nil {
		return keychain.KeyDescriptor{}, fmt.Errorf("lnd returned " +
			"no key locator")
	}
	pubKey, err := btcec.ParsePubKey(desc.RawKeyBytes)
	if err != nil {
		return keychain.KeyDescriptor{}, err
	}

	return keychain.KeyDescriptor{
		KeyLocator: keychain.KeyLocator{
			Family: keychain.KeyFamily(desc.KeyLoc.KeyFamily),
			Index:  uint32(desc.KeyLoc.KeyIndex),
		},
		PubKey: pubKey,
	}, nil
}

// signPsbt signs the inputs that carry the derivation path of a key of the
// lnd wallet and leaves every input unfinalized.
func (l *lndWalletKit) signPsbt(ctx context.Context,
	anchorPsbt []byte) ([]byte, error) {

	signed, err := l.client.SignPsbt(
		l.rpcContext(ctx), &walletrpc.SignPsbtRequest{
			FundedPsbt: anchorPsbt,
		},
	)
	if err != nil {
		return nil, err
	}
	if len(signed.SignedPsbt) == 0 {
		return nil, fmt.Errorf("lnd returned empty signed PSBT")
	}

	return signed.SignedPsbt, nil
}

// signAndFinalizePsbt signs the inputs of the lnd wallet and finalizes the
// packet. Every input must be signed once lnd has added its signatures.
func (l *lndWalletKit) signAndFinalizePsbt(ctx context.Context,
	anchorPsbt []byte) ([]byte, error) {

	signed, err := l.signPsbt(ctx, anchorPsbt)
	if err != nil {
		return nil, err
	}

	resp, err := l.client.FinalizePsbt(
		l.rpcContext(ctx), &walletrpc.FinalizePsbtRequest{
			FundedPsbt: signed,
		},
	)
	if err != nil {
		return nil, err
	}
	if len(resp.SignedPsbt) == 0 {
		return nil, fmt.Errorf("lnd returned empty signed PSBT")
	}

	return resp.SignedPsbt, nil
}

func newAliceLndAnchorSigner(t testing.TB) tapsdk.AnchorSigner {
	t.Helper()

	return newAliceLndWalletKit(t).signAndFinalizePsbt
}

// lndFundingInput is a confirmed BIP-86 output locked to a key of an lnd
// wallet.
type lndFundingInput struct {
	fundedOutput

	keyDesc keychain.KeyDescriptor
}

// fundLndInput derives a key from the lnd wallet and pays a confirmed BIP-86
// output to it from the bitcoind miner wallet. The lnd wallet does not track
// the output, so tapd cannot select it to fund its own transactions.
func (h *TestHarness) fundLndInput(t testing.TB, ctx context.Context,
	lnd *lndWalletKit) *lndFundingInput {

	t.Helper()

	keyDesc, err := lnd.deriveNextKey(ctx, lndFundingKeyFamily)
	require.NoError(t, err)

	pkScript, err := txscript.PayToTaprootScript(
		txscript.ComputeTaprootKeyNoScript(keyDesc.PubKey),
	)
	require.NoError(t, err)

	return &lndFundingInput{
		fundedOutput: h.fundScript(t, ctx, pkScript),
		keyDesc:      keyDesc,
	}
}

// psbtInput returns the PSBT input fields lnd needs to sign a key spend of
// the output.
func (f *lndFundingInput) psbtInput() psbt.PInput {
	derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
		f.keyDesc, chaincfg.RegressionNetParams.HDCoinType,
	)

	return psbt.PInput{
		WitnessUtxo:     f.txOut,
		Bip32Derivation: []*psbt.Bip32Derivation{derivation},
		TaprootBip32Derivation: []*psbt.TaprootBip32Derivation{
			trDerivation,
		},
		TaprootInternalKey: schnorr.SerializePubKey(f.keyDesc.PubKey),
	}
}

// psbtChangeOutput returns the PSBT output fields of Bitcoin change that
// pays back to the key of the input. The internal key lets the proofs of the
// transfer show that the output commits to no assets.
func (f *lndFundingInput) psbtChangeOutput() psbt.POutput {
	derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
		f.keyDesc, chaincfg.RegressionNetParams.HDCoinType,
	)

	return psbt.POutput{
		Bip32Derivation: []*psbt.Bip32Derivation{derivation},
		TaprootBip32Derivation: []*psbt.TaprootBip32Derivation{
			trDerivation,
		},
		TaprootInternalKey: schnorr.SerializePubKey(f.keyDesc.PubKey),
	}
}
