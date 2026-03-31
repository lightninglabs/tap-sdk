package tapsdk

// SendOptions configures optional parameters for high-level send operations.
type SendOptions struct {
	// feeRate is the target fee rate in sat/kw for the anchor transaction.
	// Zero uses the daemon default.
	feeRate uint32

	// label is an optional short label for tracking the send.
	label string

	// skipProofCourierPingCheck skips the proof courier connectivity
	// check before sending. Useful when the courier is temporarily
	// unreachable but the send should proceed anyway.
	skipProofCourierPingCheck bool
}

// SendOption is a functional option for configuring Send and SendMulti.
type SendOption func(*SendOptions)

// WithFeeRate sets the target fee rate in sat/kw for the anchor transaction.
func WithFeeRate(feeRate uint32) SendOption {
	return func(o *SendOptions) {
		o.feeRate = feeRate
	}
}

// WithLabel attaches a short label for tracking the send.
func WithLabel(label string) SendOption {
	return func(o *SendOptions) {
		o.label = label
	}
}

// WithSkipProofCourierPingCheck skips the proof courier connectivity check.
func WithSkipProofCourierPingCheck() SendOption {
	return func(o *SendOptions) {
		o.skipProofCourierPingCheck = true
	}
}

// applySendOptions merges functional options into a SendOptions struct.
func applySendOptions(opts []SendOption) *SendOptions {
	o := &SendOptions{}
	for _, opt := range opts {
		opt(o)
	}

	return o
}
