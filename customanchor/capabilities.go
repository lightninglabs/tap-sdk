package customanchor

import (
	"context"
	"errors"
	"fmt"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

const unknownString = "unknown"

// ErrUnsupportedCapability is returned when an advanced custom-anchor flow
// requires a backend capability that is unsupported or unknown.
var ErrUnsupportedCapability = errors.New(
	"custom-anchor capability unsupported",
)

// CapabilityProvider is an optional extension implemented by clients that can
// provide a custom-anchor capability profile. It is kept separate from
// tapsdk.WalletKitClient so existing clients and test doubles aren't forced to
// implement an SDK-specific discovery method that tapd does not expose over
// RPC.
//
// Implementations must distinguish an explicitly unsupported feature from an
// unknown one. Callers fail closed for both states.
type CapabilityProvider interface {
	CustomAnchorCapabilities(ctx context.Context) (*Capabilities, error)
}

// QueryCapabilities reports the optional capability profile exposed by the
// wallet's client. tapd does not currently expose a capability RPC, so a
// client that doesn't implement CapabilityProvider returns an all-unknown
// profile. Unknown capabilities fail closed when Require is used.
func QueryCapabilities(ctx context.Context,
	wallet *tapsdk.Wallet) (*Capabilities, error) {

	provider, ok := wallet.Client().(CapabilityProvider)
	if !ok {
		return &Capabilities{}, nil
	}

	caps, err := provider.CustomAnchorCapabilities(ctx)
	if err != nil {
		return nil, &tapsdk.Error{Op: "CustomAnchorCapabilities", Err: err}
	}
	if caps == nil {
		return &Capabilities{}, nil
	}

	return caps, nil
}

// BackendMode identifies the kind of backend that reported a
// custom-anchor capability set.
type BackendMode string

const (
	// BackendUnknown means the SDK cannot classify the backend.
	BackendUnknown BackendMode = unknownString

	// BackendLocalTapd means the backend is local tapd.
	BackendLocalTapd = BackendMode(
		"local-tapd",
	)

	// BackendRemoteTapd means the backend is remote tapd.
	BackendRemoteTapd = BackendMode(
		"remote-tapd",
	)

	// BackendHosted means the backend is a hosted service.
	BackendHosted BackendMode = "hosted"

	// BackendLightweight means the backend is a smaller service
	// that exposes only the SDK-required wallet, proof, or builder calls.
	BackendLightweight = BackendMode(
		"lightweight",
	)
)

// Capability names a backend feature used by the advanced
// custom-anchor transaction flow.
type Capability string

const (
	// CapabilityCommit commits asset changes into a
	// caller-visible anchor PSBT.
	CapabilityCommit = Capability(
		"custom_anchor_commit",
	)

	// CapabilityCallerAnchorPSBT accepts a caller-supplied BTC
	// anchor PSBT template.
	CapabilityCallerAnchorPSBT = Capability(
		"caller_anchor_psbt",
	)

	// CapabilitySkipFunding skips backend BTC wallet funding.
	CapabilitySkipFunding = Capability(
		"skip_funding",
	)

	// CapabilityNoChangeOutput commits without adding
	// or selecting a BTC change output.
	CapabilityNoChangeOutput = Capability(
		"no_change_output",
	)

	// CapabilityWalletFunding funds the BTC anchor from the
	// backend wallet.
	CapabilityWalletFunding = Capability(
		"wallet_funding",
	)

	// CapabilitySkipBroadcast logs a transfer while leaving BTC
	// anchor broadcast to the caller.
	CapabilitySkipBroadcast = Capability(
		"skip_broadcast",
	)

	// CapabilityP2AAnchorOutput supports pay-to-anchor or a
	// semantically equivalent external fee-bump policy.
	CapabilityP2AAnchorOutput = Capability(
		"p2a_anchor_output",
	)

	// CapabilityExternalFeeBump supports caller fee bumps.
	CapabilityExternalFeeBump = Capability(
		"external_fee_bump",
	)

	// CapabilityProofRegistration registers proof state
	// after the custom transfer is logged.
	CapabilityProofRegistration = Capability(
		"proof_registration",
	)

	// CapabilityLocalVerification verifies the plan or
	// package in SDK/library code without trusting the backend.
	CapabilityLocalVerification = Capability(
		"local_verification",
	)

	// CapabilityBackendVerification verifies the plan or
	// package through the backend.
	CapabilityBackendVerification = Capability(
		"backend_verification",
	)

	// CapabilityBackendAnchorSigning signs the backend-owned asset anchor
	// inputs of a caller-funded transfer at publish.
	CapabilityBackendAnchorSigning = Capability(
		"backend_anchor_signing",
	)
)

// CapabilityStatus describes whether a capability is available.
type CapabilityStatus uint8

const (
	// CapabilityUnknown means the backend did not report
	// a usable answer for the capability.
	CapabilityUnknown CapabilityStatus = iota

	// CapabilityUnsupported means the backend reports that the
	// capability is not available.
	CapabilityUnsupported

	// CapabilitySupported means the backend reports that the
	// capability is available.
	CapabilitySupported
)

// String returns the stable text form of the support status.
func (s CapabilityStatus) String() string {
	switch s {
	case CapabilitySupported:
		return "supported"

	case CapabilityUnsupported:
		return "unsupported"

	default:
		return unknownString
	}
}

// Supported returns true when the capability can be used safely.
func (s CapabilityStatus) Supported() bool {
	return s == CapabilitySupported
}

// Capabilities reports backend support for advanced custom-anchor
// transaction operations. Unknown capabilities are treated as unavailable by
// Require so callers fail before signing.
type Capabilities struct {
	BackendMode BackendMode

	CustomAnchorCommit       CapabilityStatus
	CallerSuppliedAnchorPSBT CapabilityStatus
	SkipFunding              CapabilityStatus
	NoChangeOutput           CapabilityStatus
	WalletFunding            CapabilityStatus
	SkipBroadcast            CapabilityStatus
	P2AAnchorOutput          CapabilityStatus
	ExternalFeeBump          CapabilityStatus
	ProofRegistration        CapabilityStatus
	LocalVerification        CapabilityStatus
	BackendVerification      CapabilityStatus
	BackendAnchorSigning     CapabilityStatus
}

// DefaultTapdCapabilities returns an SDK-version-pinned assumption
// profile for tapd-compatible transports. This is not runtime discovery: tapd
// does not currently expose a capability RPC. Features not proven by the
// pinned API are deliberately left unknown so callers fail closed.
func DefaultTapdCapabilities() Capabilities {
	return Capabilities{
		BackendMode:              BackendUnknown,
		CustomAnchorCommit:       CapabilitySupported,
		CallerSuppliedAnchorPSBT: CapabilitySupported,
		SkipFunding:              CapabilitySupported,
		NoChangeOutput:           CapabilityUnknown,
		WalletFunding:            CapabilitySupported,
		SkipBroadcast:            CapabilitySupported,
		P2AAnchorOutput:          CapabilityUnknown,
		ExternalFeeBump:          CapabilityUnknown,
		ProofRegistration:        CapabilityUnknown,
		LocalVerification:        CapabilityUnsupported,
		BackendVerification:      CapabilityUnknown,
		BackendAnchorSigning:     CapabilityUnknown,
	}
}

// Support returns the reported status for a capability. Unknown capability
// names are treated as unknown rather than supported.
func (c Capabilities) Support(
	capability Capability) CapabilityStatus {

	switch capability {
	case CapabilityCommit:
		return c.CustomAnchorCommit

	case CapabilityCallerAnchorPSBT:
		return c.CallerSuppliedAnchorPSBT

	case CapabilitySkipFunding:
		return c.SkipFunding

	case CapabilityNoChangeOutput:
		return c.NoChangeOutput

	case CapabilityWalletFunding:
		return c.WalletFunding

	case CapabilitySkipBroadcast:
		return c.SkipBroadcast

	case CapabilityP2AAnchorOutput:
		return c.P2AAnchorOutput

	case CapabilityExternalFeeBump:
		return c.ExternalFeeBump

	case CapabilityProofRegistration:
		return c.ProofRegistration

	case CapabilityLocalVerification:
		return c.LocalVerification

	case CapabilityBackendVerification:
		return c.BackendVerification

	case CapabilityBackendAnchorSigning:
		return c.BackendAnchorSigning

	default:
		return CapabilityUnknown
	}
}

// Require returns an unsupported-capability error unless every requested
// capability is explicitly supported.
func (c Capabilities) Require(
	capabilities ...Capability) error {

	for _, capability := range capabilities {
		status := c.Support(capability)
		if !status.Supported() {
			return &UnsupportedCapabilityError{
				BackendMode: c.BackendMode,
				Capability:  capability,
				Status:      status,
			}
		}
	}

	return nil
}

// UnsupportedCapabilityError identifies a missing capability and
// the backend mode that reported it.
type UnsupportedCapabilityError struct {
	BackendMode BackendMode
	Capability  Capability
	Status      CapabilityStatus
}

// Error returns a human-readable unsupported capability message.
func (e *UnsupportedCapabilityError) Error() string {
	backendMode := e.BackendMode
	if backendMode == "" {
		backendMode = BackendUnknown
	}

	capability := e.Capability
	if capability == "" {
		capability = Capability(unknownString)
	}

	return fmt.Sprintf(
		"custom-anchor capability %q is %s for backend mode %q",
		capability, e.Status, backendMode,
	)
}

// Unwrap returns the sentinel error for errors.Is checks.
func (e *UnsupportedCapabilityError) Unwrap() error {
	return ErrUnsupportedCapability
}
