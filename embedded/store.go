package embedded

import (
	"context"
	"errors"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

var (
	// ErrNotFound is returned by a Store when a key, proof or transfer
	// does not exist.
	ErrNotFound = errors.New("not found")

	// ErrTransferExists is returned by Store.InsertTransfer when a
	// transfer with the same anchor transaction is already recorded.
	ErrTransferExists = errors.New("transfer already exists")
)

// KeyRecord is a wallet key and its derivation path.
type KeyRecord struct {
	// PubKey is the compressed public key.
	PubKey [33]byte

	// Family is the key family the key derives from.
	Family uint32

	// Index is the key's index within Family.
	Index uint32
}

// ScriptKeyRecord is an asset script key the wallet knows.
type ScriptKeyRecord struct {
	// TweakedKey is the compressed key that appears in asset outputs.
	TweakedKey [33]byte

	// RawKey is the key TweakedKey is tweaked from. Family and Index are
	// zero for a key the wallet cannot derive.
	RawKey KeyRecord

	// Tweak is the Taproot tweak applied to RawKey. It is empty for a
	// BIP-86 key.
	Tweak []byte

	// Type is tapd's script key type.
	Type uint8
}

// ProofLocator identifies the proof of one asset output.
type ProofLocator struct {
	// AssetID is the ID of the asset issuance.
	AssetID [32]byte

	// ScriptKey is the compressed, tweaked script key of the output.
	ScriptKey [33]byte

	// Outpoint is the anchor output holding the asset.
	Outpoint wire.OutPoint
}

// AssetProof is the confirmed proof file of one asset output.
type AssetProof struct {
	ProofLocator

	// ProofFile is the encoded proof file whose last proof creates the
	// output.
	ProofFile []byte

	// Owned marks an output the wallet holds, as opposed to one whose
	// proof it only keeps.
	Owned bool

	// Spent marks an owned output that a recorded transfer spends.
	Spent bool
}

// TransferState is the lifecycle state of a recorded transfer.
type TransferState uint8

const (
	// TransferPending is a transfer whose anchor transaction is not
	// confirmed.
	TransferPending TransferState = iota

	// TransferConfirmed is a transfer whose anchor transaction is
	// confirmed and whose final proofs are stored.
	TransferConfirmed

	// TransferFinal is a confirmed transfer buried deep enough that the
	// engine no longer follows it.
	TransferFinal
)

// Transfer is one recorded asset transfer.
type Transfer struct {
	// AnchorTxid is the ID of the transfer's anchor transaction.
	AnchorTxid chainhash.Hash

	// State is the transfer's lifecycle state.
	State TransferState

	// HeightHint is the chain height at which the transfer was recorded.
	HeightHint uint32

	// BlockHash is the block confirming the anchor transaction. It is
	// zero while the transfer is pending.
	BlockHash chainhash.Hash

	// BlockHeight is the height of BlockHash.
	BlockHeight uint32

	// CreatedAt is when the transfer was recorded.
	CreatedAt time.Time

	// Payload is the engine's encoding of the transfer. A Store keeps it
	// as an opaque blob.
	Payload []byte
}

// Store persists the engine's keys, proofs and transfers. Every method is
// atomic. Implementations must be safe for concurrent use.
type Store interface {
	// InsertInternalKey records a derived key. Inserting a known key is
	// not an error.
	InsertInternalKey(ctx context.Context, key KeyRecord) error

	// FetchInternalKey returns the record of a derived key, or
	// ErrNotFound.
	FetchInternalKey(ctx context.Context, pubKey [33]byte) (*KeyRecord,
		error)

	// InsertScriptKey records a script key, replacing a record with the
	// same tweaked key.
	InsertScriptKey(ctx context.Context, key ScriptKeyRecord) error

	// FetchScriptKey returns the record of a tweaked script key, or
	// ErrNotFound.
	FetchScriptKey(ctx context.Context,
		tweakedKey [33]byte) (*ScriptKeyRecord, error)

	// InsertProof stores a proof file, replacing the file and the Owned
	// flag of a record with the same locator. The Spent flag of an
	// existing record is kept.
	InsertProof(ctx context.Context, proof AssetProof) error

	// FetchProofs returns the proofs of an asset and script key. A nil
	// outpoint matches every anchor output.
	FetchProofs(ctx context.Context, assetID [32]byte, scriptKey [33]byte,
		outpoint *wire.OutPoint) ([]AssetProof, error)

	// ListOwnedProofs returns the owned asset outputs. Spent outputs are
	// included only when asked for.
	ListOwnedProofs(ctx context.Context,
		includeSpent bool) ([]AssetProof, error)

	// InsertTransfer records a transfer and marks the owned outputs it
	// spends as spent. It returns ErrTransferExists when the anchor
	// transaction is already recorded.
	InsertTransfer(ctx context.Context, transfer Transfer,
		spends []ProofLocator) error

	// UpdateTransfer replaces the state, block and payload of a recorded
	// transfer, stores putProofs as InsertProof does and deletes
	// deleteProofs.
	UpdateTransfer(ctx context.Context, transfer Transfer,
		putProofs []AssetProof, deleteProofs []ProofLocator) error

	// ListTransfers returns the recorded transfers, oldest first. A nil
	// anchor transaction ID matches every transfer.
	ListTransfers(ctx context.Context,
		anchorTxid *chainhash.Hash) ([]Transfer, error)
}
