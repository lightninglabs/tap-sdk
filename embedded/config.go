package embedded

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/keychain"
)

const (
	// defaultRPCTimeout bounds a single engine call.
	defaultRPCTimeout = 30 * time.Second

	// Version is the engine version GetInfo reports.
	Version = "embedded-0.1.0"
)

// KeyRing derives the wallet's asset keys. NewLocalKeyRing derives with a
// local key ring.
type KeyRing interface {
	// DeriveNextKey derives the next unused key of the family. ctx
	// carries the deadline of the engine call that needs the key.
	DeriveNextKey(ctx context.Context,
		family keychain.KeyFamily) (keychain.KeyDescriptor, error)
}

// Signer signs transaction inputs with keys the KeyRing derived, so the keys
// can stay in a remote signer such as lnd. NewKeyRingSigner signs with local
// private keys.
type Signer interface {
	// SignOutputRaw signs the input of tx that signDesc describes, as
	// lnd's input.Signer does. ctx carries the deadline of the engine call
	// that needs the signature. The engine only signs Taproot inputs, and
	// expects a Schnorr signature without a sighash flag.
	SignOutputRaw(ctx context.Context, tx *wire.MsgTx,
		signDesc *input.SignDescriptor) (input.Signature, error)
}

// Confirmation is the block that confirms a transaction.
type Confirmation struct {
	// Height is the height of Block on the best chain.
	Height uint32

	// Block is the full confirming block.
	Block *wire.MsgBlock
}

// ConfirmationEvents follows one transaction until its confirmation is
// final.
//
// The engine receives from the three channels in no fixed order. It
// tolerates a confirmation and Done that are ready together, and applies the
// confirmation first. A host must not have Reorged ready together with
// another event.
type ConfirmationEvents struct {
	// Confirmed delivers the confirming block each time the transaction
	// confirms.
	Confirmed <-chan *Confirmation

	// Reorged signals that the confirming block left the best chain.
	Reorged <-chan struct{}

	// Done signals that the confirmation is final. No event follows it.
	Done <-chan struct{}

	// Cancel stops the events and releases their resources.
	Cancel func()
}

// Chain is the engine's view of the Bitcoin chain.
type Chain interface {
	// BestBlock returns the height and hash of the best chain tip.
	BestBlock(ctx context.Context) (uint32, chainhash.Hash, error)

	// BlockHeader returns the header of the best-chain block at height.
	BlockHeader(ctx context.Context,
		height uint32) (*wire.BlockHeader, error)

	// PublishTransaction broadcasts a transaction. Publishing a
	// transaction the network already knows is not an error.
	PublishTransaction(ctx context.Context, tx *wire.MsgTx,
		label string) error

	// RegisterConfirmation follows the confirmation of a transaction
	// that pays to pkScript, starting at heightHint.
	RegisterConfirmation(ctx context.Context, txid chainhash.Hash,
		pkScript []byte, heightHint uint32) (*ConfirmationEvents, error)
}

// Logger receives the engine's background events.
type Logger interface {
	// Infof logs a transfer's progress.
	Infof(format string, args ...any)

	// Errorf logs a failure the engine retries.
	Errorf(format string, args ...any)
}

// ReceiveKeys decrypts and authenticates with keys the KeyRing derived, so
// the keys can stay in a remote signer such as lnd. NewKeyRingReceiveKeys
// uses local private keys.
type ReceiveKeys interface {
	// ECDH returns the SHA-256 of the compressed shared point of the key
	// at locator and pubKey, as lnd's DeriveSharedKey does.
	ECDH(ctx context.Context, locator keychain.KeyLocator,
		pubKey *btcec.PublicKey) ([32]byte, error)

	// SignMessageSchnorr returns the BIP-340 signature of the key at
	// locator over the SHA-256 of msg, as lnd's SignMessage does for a
	// Schnorr signature without a tag or a Taproot tweak.
	SignMessageSchnorr(ctx context.Context, locator keychain.KeyLocator,
		msg []byte) (*schnorr.Signature, error)
}

// ProofKey names one leaf of a courier's universe.
type ProofKey struct {
	// AssetID selects the universe of one asset. It is not read when
	// GroupKey is set.
	AssetID [32]byte

	// GroupKey selects the universe of an asset group. Nil selects the
	// universe of AssetID.
	GroupKey *[33]byte

	// ScriptKey is the compressed script key of the asset output.
	ScriptKey [33]byte

	// Outpoint is the anchor output holding the asset.
	Outpoint wire.OutPoint
}

// AnchorProof shows a courier that the sender of a mailbox message created a
// confirmed Taproot output. A courier stores one message per output.
type AnchorProof struct {
	// Tx is the confirmed transaction.
	Tx *wire.MsgTx

	// BlockHeader is the header of the block that confirms Tx.
	BlockHeader wire.BlockHeader

	// BlockHeight is the height of that block.
	BlockHeight uint32

	// MerkleNodes are the hashes that lead from Tx to the merkle root of
	// the block, lowest level first.
	MerkleNodes []chainhash.Hash

	// MerkleBits tells for each of MerkleNodes whether it is the right
	// sibling.
	MerkleBits []bool

	// Outpoint is the output of Tx the message is about.
	Outpoint wire.OutPoint

	// InternalKey is the compressed Taproot internal key of the output.
	InternalKey [33]byte

	// MerkleRoot is the Taproot merkle root the output commits to.
	MerkleRoot []byte
}

// MailboxMessage is one message of a courier's mailbox.
type MailboxMessage struct {
	// ID is the courier's ID of the message.
	ID uint64

	// Payload is the encrypted message.
	Payload []byte
}

// MessageStream delivers the messages of one mailbox.
type MessageStream interface {
	// Recv blocks until the courier delivers messages. It fails once the
	// stream ends, which the context of Courier.ReceiveMessages controls.
	Recv() ([]MailboxMessage, error)
}

// ChallengeSigner signs the challenge of a mailbox with the key of the
// receiver.
type ChallengeSigner func(ctx context.Context,
	challenge []byte) (*schnorr.Signature, error)

// Courier is a proof courier that runs a universe and an auth mailbox. It
// moves opaque bytes: the engine decrypts the messages and verifies the
// proofs.
type Courier interface {
	// QueryProof returns the encoded proof at a universe leaf, or
	// ErrNotFound when the universe does not hold the leaf.
	QueryProof(ctx context.Context, key ProofKey) ([]byte, error)

	// ReceiveMessages authenticates as the receiver and returns the
	// stream of its mailbox: the stored messages whose transaction
	// confirmed at startBlock or later, then the ones that arrive. sign
	// answers the courier's challenge. The stream ends with ctx.
	ReceiveMessages(ctx context.Context, receiver [33]byte,
		startBlock uint32, sign ChallengeSigner) (MessageStream, error)

	// InsertProof stores an encoded proof at a universe leaf. It returns
	// ErrProofRefused when the universe answers that it does not take the
	// proof, and another error when a later call may succeed.
	InsertProof(ctx context.Context, key ProofKey, proof []byte) error

	// SendMessage stores an encrypted message in the mailbox of a
	// receiver. anchor proves the output the message is about. A courier
	// keeps one message per output: it answers a second message about an
	// output to the same receiver as it answered the first, and stores
	// the first only.
	SendMessage(ctx context.Context, receiver [33]byte, payload []byte,
		anchor *AnchorProof) error

	// Close releases the connection to the courier.
	Close() error
}

// CourierDialer connects to the courier a proof courier address names.
type CourierDialer func(ctx context.Context, addr *url.URL) (Courier, error)

// ReceiveConfig enables Taproot Asset addresses and the receipt of the
// transfers sent to them.
type ReceiveConfig struct {
	// Store persists the wallet's addresses and the transfers to them.
	Store AddressStore

	// Keys decrypts and authenticates with the script keys of the
	// wallet's addresses.
	Keys ReceiveKeys

	// Dial connects to the proof courier of an address.
	Dial CourierDialer

	// DefaultCourier is the proof courier of an address whose request
	// names none, as an authmailbox+universerpc address. Nil makes every
	// request name its courier.
	DefaultCourier *url.URL
}

// validate checks that the receive configuration is complete.
func (c *ReceiveConfig) validate() error {
	switch {
	case c.Store == nil:
		return fmt.Errorf("address store is required")

	case c.Keys == nil:
		return fmt.Errorf("receive keys are required")

	case c.Dial == nil:
		return fmt.Errorf("courier dialer is required")
	}
	if c.DefaultCourier == nil {
		return nil
	}

	if err := validateCourier(c.DefaultCourier); err != nil {
		return fmt.Errorf("default courier: %w", err)
	}

	return nil
}

// Config configures the embedded engine.
type Config struct {
	// Network is the Bitcoin network the wallet runs on.
	Network tapsdk.Network

	// Store persists keys, proofs and transfers.
	Store Store

	// KeyRing derives the wallet's asset keys.
	KeyRing KeyRing

	// Signer signs with the keys KeyRing derives.
	Signer Signer

	// Chain provides headers, broadcast and confirmations.
	Chain Chain

	// GroupKeys are the asset groups the wallet trusts. A proof of a
	// grouped asset verifies only if its group key is listed here or was
	// added with Client.TrustGroupKey.
	GroupKeys []tapsdk.PubKey

	// RPCTimeout bounds a single call. Zero selects 30 seconds.
	RPCTimeout time.Duration

	// Logger receives background events. Nil discards them.
	Logger Logger

	// Receive enables Taproot Asset addresses. Nil leaves the address
	// calls unimplemented, and the client contacts no courier.
	Receive *ReceiveConfig
}

// validate checks that the configuration is complete.
func (c *Config) validate() error {
	switch {
	case c == nil:
		return fmt.Errorf("config is required")

	case c.Store == nil:
		return fmt.Errorf("store is required")

	case c.KeyRing == nil:
		return fmt.Errorf("key ring is required")

	case c.Signer == nil:
		return fmt.Errorf("signer is required")

	case c.Chain == nil:
		return fmt.Errorf("chain is required")
	}

	if c.Receive != nil {
		if err := c.Receive.validate(); err != nil {
			return fmt.Errorf("receive: %w", err)
		}
	}

	return nil
}

// nopLogger discards the engine's background events.
type nopLogger struct{}

// Infof discards the message.
func (nopLogger) Infof(string, ...any) {}

// Errorf discards the message.
func (nopLogger) Errorf(string, ...any) {}
