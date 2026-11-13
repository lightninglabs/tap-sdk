package embedded

import (
	"context"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightningnetwork/lnd/keychain"
)

const (
	// defaultRPCTimeout bounds a single engine call.
	defaultRPCTimeout = 30 * time.Second

	// Version is the engine version GetInfo reports.
	Version = "embedded-0.1.0"
)

// KeyRing derives the wallet's asset keys. lnd's keychain.SecretKeyRing
// implements it.
type KeyRing interface {
	// DeriveNextKey derives the next unused key of the family.
	DeriveNextKey(family keychain.KeyFamily) (keychain.KeyDescriptor,
		error)

	// DerivePrivKey derives the private key of a key descriptor.
	DerivePrivKey(desc keychain.KeyDescriptor) (*btcec.PrivateKey, error)
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

// Config configures the embedded engine.
type Config struct {
	// Network is the Bitcoin network the wallet runs on.
	Network tapsdk.Network

	// Store persists keys, proofs and transfers.
	Store Store

	// KeyRing derives and holds the wallet's asset keys.
	KeyRing KeyRing

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

	case c.Chain == nil:
		return fmt.Errorf("chain is required")
	}

	return nil
}

// nopLogger discards the engine's background events.
type nopLogger struct{}

// Infof discards the message.
func (nopLogger) Infof(string, ...any) {}

// Errorf discards the message.
func (nopLogger) Errorf(string, ...any) {}
