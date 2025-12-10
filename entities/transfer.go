package entities

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

	// AnchorTx is the raw anchor transaction bytes.
	AnchorTx []byte

	// Outputs contains details about each transfer output.
	Outputs []TransferOutput
}

// TransferOutput represents a single output in a transfer.
type TransferOutput struct {
	// Outpoint is the output location in "txid:index" format.
	Outpoint string

	// ScriptKey is the 33-byte compressed public key locking this output.
	ScriptKey [33]byte

	// Amount is the number of asset units in this output.
	Amount uint64

	// ProofBlob is the transition proof for this output.
	// For interactive sends, this must be delivered to the receiver.
	ProofBlob []byte
}
