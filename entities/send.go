package entities

// SendAssetRequest specifies a low-level one-shot address-based send.
//
// This request type exists for advanced callers that need direct access to
// the daemon's send RPC shape. Higher-level send flows should stay
// opinionated around semantic asset identity and default V2 addresses.
type SendAssetRequest struct {
	// TapAddresses sends to one or more Taproot Asset addresses that already
	// encode their amounts. This is mutually exclusive with Recipients.
	TapAddresses []string

	// FeeRate is the optional target fee rate in sat/kw for the anchor
	// transaction.
	FeeRate uint32

	// Label is an optional short label for tracking the send.
	Label string

	// SkipProofCourierPingCheck skips the proof courier connectivity check.
	SkipProofCourierPingCheck bool

	// Recipients sends to one or more addresses while specifying the amount
	// for each recipient explicitly. This is required for reusable V2
	// addresses that omit the amount in the address itself.
	Recipients []Recipient
}
