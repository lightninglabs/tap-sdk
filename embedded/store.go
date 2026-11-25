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
	// does not exist, and by a Courier that does not hold a proof.
	ErrNotFound = errors.New("not found")

	// ErrTransferExists is returned by Store.InsertTransfer when a
	// transfer with the same anchor transaction is already recorded.
	ErrTransferExists = errors.New("transfer already exists")

	// ErrAddressExists is returned by AddressStore.InsertAddress when an
	// address with the same script key is already recorded.
	ErrAddressExists = errors.New("address already exists")
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

// AddressRecord is a Taproot Asset address the wallet created.
type AddressRecord struct {
	// ScriptKey is the compressed raw script key of the address. A sender
	// encrypts to it and derives the script key of each asset output from
	// it.
	ScriptKey [33]byte

	// Encoded is the bech32m encoding of the address.
	Encoded string

	// InternalKey is the compressed internal key of the anchor outputs
	// that pay the address.
	InternalKey [33]byte

	// CreationHeight is the chain height at which the address was
	// created. No transfer to the address confirms below it.
	CreationHeight uint32

	// CreatedAt is when the address was created.
	CreatedAt time.Time
}

// ReceiveState is the lifecycle state of a transfer to an address.
type ReceiveState uint8

const (
	// ReceiveDetected is a transfer a sender announced in the mailbox of
	// the address, or a confirmed transfer whose block left the chain. The
	// wallet does not hold its assets.
	ReceiveDetected ReceiveState = iota

	// ReceiveConfirmed is a transfer whose proofs verified and are stored
	// as asset outputs of the wallet.
	ReceiveConfirmed

	// ReceiveFailed is a transfer the wallet took no asset from: its
	// proofs do not verify or do not match the address, or another
	// announcement of its output delivered the assets.
	ReceiveFailed

	// ReceiveFinal is a confirmed transfer whose confirmation the chain
	// source reported as final. The engine does not follow it.
	ReceiveFinal
)

// ReceiveRecord is one announcement of a transfer to an address of the
// wallet. The anchor outpoint, the address and the announcement identify it.
type ReceiveRecord struct {
	// Outpoint is the anchor output that holds the transferred assets.
	Outpoint wire.OutPoint

	// Address is the script key of the address the transfer pays, as in
	// AddressRecord.ScriptKey.
	Address [33]byte

	// Announcement is the engine's hash of what the sender announced.
	// Anyone who knows an address can announce an output, so one output
	// may have several records.
	Announcement [32]byte

	// State is the receive's lifecycle state.
	State ReceiveState

	// BlockHash is the block confirming the anchor transaction. It is
	// zero while the receive is not confirmed.
	BlockHash chainhash.Hash

	// BlockHeight is the height of BlockHash.
	BlockHeight uint32

	// CreatedAt is when the receive was recorded.
	CreatedAt time.Time

	// Payload is the engine's encoding of the receive. A store keeps it
	// as an opaque blob.
	Payload []byte
}

// ReceiveFilter selects receive records. A nil field matches every record.
type ReceiveFilter struct {
	// Outpoint matches the receives of this anchor output.
	Outpoint *wire.OutPoint

	// Address matches the receives of the address with this script key.
	Address *[33]byte

	// State matches the receives in this state.
	State *ReceiveState
}

// AddressStore persists the wallet's Taproot Asset addresses and the
// transfers to them. Every method is atomic. Implementations must be safe for
// concurrent use.
type AddressStore interface {
	// InsertAddress records an address. It returns ErrAddressExists when
	// an address with the same script key is already recorded.
	InsertAddress(ctx context.Context, addr AddressRecord) error

	// ListAddresses returns the recorded addresses, oldest first.
	ListAddresses(ctx context.Context) ([]AddressRecord, error)

	// InsertReceive records a receive and returns it. When a receive with
	// the same outpoint, address and announcement is recorded, it returns
	// that record and changes nothing.
	InsertReceive(ctx context.Context,
		receive ReceiveRecord) (*ReceiveRecord, error)

	// UpdateReceive replaces the state, block and payload of a recorded
	// receive and stores putProofs as Store.InsertProof does, so the
	// engine's Store must read the same data. It returns ErrNotFound for
	// a receive that is not recorded.
	UpdateReceive(ctx context.Context, receive ReceiveRecord,
		putProofs []AssetProof) error

	// ListReceives returns the recorded receives that match the filter,
	// oldest first.
	ListReceives(ctx context.Context,
		filter ReceiveFilter) ([]ReceiveRecord, error)
}
