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
