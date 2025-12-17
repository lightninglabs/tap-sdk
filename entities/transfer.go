package entities

import (
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// FundedTransfer represents the result of funding a virtual transaction.
type FundedTransfer struct {
	// FundedPsbt is the funded virtual transaction PSBT.
	FundedPsbt []byte

	// PassiveAssetPsbts are the PSBTs for passive assets that need to be
	// re-signed.
	PassiveAssetPsbts [][]byte
}

// CommittedTransfer represents the result of committing virtual transactions.
type CommittedTransfer struct {
	// AnchorPsbt is the PSBT of the anchor transaction.
	AnchorPsbt []byte

	// VirtualPsbts are the committed virtual transaction PSBTs.
	VirtualPsbts [][]byte

	// PassiveAssetPsbts are the updated passive asset PSBTs.
	PassiveAssetPsbts [][]byte
}

// Recipient represents a recipient of an asset transfer.
type Recipient struct {
	// Address is the Taproot Asset address of the recipient.
	Address string

	// Amount is the amount of asset units to send.
	Amount uint64
}

// InteractiveSendRequest represents a request to send assets interactively.
type InteractiveSendRequest struct {
	// AssetID is the 32-byte asset identifier.
	AssetID [32]byte

	// Amount is the number of asset units to send.
	Amount uint64

	// ReceiverKeys contains the keys derived by the receiver.
	ReceiverKeys DerivedKeys
}

// SendResult represents the result of a completed send operation.
type SendResult struct {
	// TransferTxid is the anchor transaction ID (32 bytes, not reversed).
	TransferTxid [32]byte

	// AnchorTxid is the display-order transaction ID (string form).
	AnchorTxid string

	// AnchorTx is the raw anchor transaction bytes.
	AnchorTx []byte

	// Outputs contains details about each transfer output.
	Outputs []TransferOutput
}

// TransferOutput represents a single output in a transfer.
type TransferOutput struct {
	// Outpoint is the output location in "txid:index" format.
	Outpoint string

	// AnchorOutpoint is the on-chain outpoint.
	AnchorOutpoint Outpoint

	// AnchorValue is the BTC value of the anchor output in sats.
	AnchorValue int64

	// ScriptKey is the 33-byte compressed public key locking this output.
	ScriptKey [33]byte

	// Amount is the number of asset units in this output.
	Amount uint64

	// ProofBlob is the transition proof for this output.
	// For interactive sends, this must be delivered to the receiver.
	// This is the standard way to retrieve proofs for the transfer.
	ProofBlob []byte

	// AltLeaves are auxiliary Taproot leaves (if present) decoded from proofs.
	AltLeaves [][]byte
}

// TransferInput represents a single input in a transfer.
type TransferInput struct {
	// AnchorPoint is the old/current location of the Taproot Asset commitment
	// that was spent as an input, in "txid:index" format.
	AnchorPoint string

	// AssetID is the 32-byte asset identifier of the asset that was spent.
	AssetID [32]byte

	// ScriptKey is the 33-byte script key of the asset that was spent.
	ScriptKey [33]byte

	// Amount is the number of asset units spent.
	Amount uint64
}

// AssetTransfer represents a wallet-recorded outgoing transfer.
type AssetTransfer struct {
	// TransferTimestamp is the timestamp of the transfer in UTC Unix time
	// seconds.
	TransferTimestamp int64

	// TransferTxid is the anchor transaction ID (32 bytes, not reversed).
	TransferTxid [32]byte

	// AnchorTxid is the display-order transaction ID (string form).
	AnchorTxid string

	// AnchorTxHeightHint is the height hint of the anchor transaction.
	AnchorTxHeightHint uint32

	// AnchorTxChainFees is the total fees paid by the anchor transaction in
	// satoshis.
	AnchorTxChainFees int64

	// Inputs describes the set of spent assets.
	Inputs []TransferInput

	// Outputs describes the set of newly created asset outputs.
	Outputs []TransferOutput

	// AnchorTxBlockHash is the block hash of the blockchain block that contains
	// the anchor transaction (if confirmed).
	AnchorTxBlockHash [32]byte

	// AnchorTxBlockHashStr is the byte-reversed hash as a hex string (if
	// confirmed).
	AnchorTxBlockHashStr string

	// AnchorTxBlockHeight is the block height of the blockchain block that
	// contains the anchor transaction (0 if unconfirmed).
	AnchorTxBlockHeight uint32

	// Label is an optional short label for the transfer.
	Label string

	// AnchorTx is the raw anchor transaction bytes.
	AnchorTx []byte
}

// Outpoint represents a Bitcoin transaction outpoint.
type Outpoint struct {
	// Txid is the 32-byte transaction hash.
	Txid [32]byte

	// Index is the output index within the transaction.
	Index uint32
}

// String returns the string representation of the outpoint in "txid:index" format.
func (o Outpoint) String() string {
	hash, _ := chainhash.NewHash(o.Txid[:])
	return fmt.Sprintf("%v:%d", hash, o.Index)
}

// NewOutpointFromString parses an outpoint from a string in "txid:index" format.
func NewOutpointFromString(s string) (Outpoint, error) {
	op, err := wire.NewOutPointFromString(s)
	if err != nil {
		return Outpoint{}, err
	}

	return Outpoint{
		Txid:  op.Hash,
		Index: op.Index,
	}, nil
}
