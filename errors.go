package tapsdk

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Error represents an SDK-specific error that wraps underlying RPC errors
// with additional context.
type Error struct {
	// Op is the operation that failed (e.g., "DeriveScriptKey").
	Op string

	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}

	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.Err
}

// IsNotFound returns true if the error indicates a resource was not found.
func (e *Error) IsNotFound() bool {
	if e.Err == nil {
		return false
	}

	st, ok := status.FromError(e.Err)
	if !ok {
		return false
	}

	return st.Code() == codes.NotFound
}

// IsUnavailable returns true if the error indicates the service is unavailable.
func (e *Error) IsUnavailable() bool {
	if e.Err == nil {
		return false
	}

	st, ok := status.FromError(e.Err)
	if !ok {
		return false
	}

	return st.Code() == codes.Unavailable
}

// IsInvalidArgument returns true if the error indicates invalid input.
func (e *Error) IsInvalidArgument() bool {
	if e.Err == nil {
		return false
	}

	st, ok := status.FromError(e.Err)
	if !ok {
		return false
	}

	return st.Code() == codes.InvalidArgument
}

// GRPCCode returns the gRPC status code if the underlying error is a gRPC
// error, or codes.Unknown otherwise.
func (e *Error) GRPCCode() codes.Code {
	if e.Err == nil {
		return codes.OK
	}

	st, ok := status.FromError(e.Err)
	if !ok {
		return codes.Unknown
	}

	return st.Code()
}

// wrapErr wraps an error with operation context. If err is nil, returns nil.
func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}

	return &Error{
		Op:  op,
		Err: err,
	}
}

// Sentinel errors for common SDK error conditions.
var (
	// ErrBuilderFinished is returned when attempting to use a TxBuilder
	// that has already completed its transaction.
	ErrBuilderFinished = errors.New("builder already finished")

	// ErrNotFunded is returned when attempting to sign a transaction
	// that hasn't been funded yet.
	ErrNotFunded = errors.New("transaction not funded")

	// ErrNotSigned is returned when attempting to complete a transaction
	// that hasn't been signed yet.
	ErrNotSigned = errors.New("transaction not signed")

	// ErrNotCommitted is returned when attempting to finish a transaction
	// that hasn't been committed yet.
	ErrNotCommitted = errors.New("transaction not committed")

	// ErrNoRecipients is returned when attempting to fund a transaction
	// with no recipients configured.
	ErrNoRecipients = errors.New("no recipients configured")

	// ErrNoReceiverKeys is returned when attempting to execute an
	// interactive transfer without setting receiver keys.
	ErrNoReceiverKeys = errors.New("receiver keys not set")

	// ErrNoAssetRef is returned when attempting to execute a transfer
	// without specifying a usable asset reference.
	ErrNoAssetRef = errors.New("asset ref not set")

	// ErrZeroAmount is returned when attempting to execute a transfer
	// with zero amount.
	ErrZeroAmount = errors.New("amount must be greater than zero")

	// ErrGroupKeyNotSupported is returned when a group-key AssetRef is
	// used in a context that cannot resolve it to a concrete issuance.
	ErrGroupKeyNotSupported = errors.New(
		"group-key AssetRef requires a wallet-backed asset resolver",
	)

	// ErrInsufficientBalance is returned when the wallet knows the asset
	// ref but cannot select enough spendable units for the requested
	// operation.
	ErrInsufficientBalance = errors.New("insufficient asset balance")

	// ErrAmountRequired is returned when attempting to send to a V2
	// address that does not embed an amount without specifying one.
	ErrAmountRequired = errors.New(
		"amount required for V2 address without an embedded amount",
	)

	// ErrAmountMismatch is returned when the caller provides an
	// explicit amount that does not match the amount embedded in the
	// destination address.
	ErrAmountMismatch = errors.New(
		"amount does not match the address-embedded amount",
	)

	// ErrAssetUnknown is returned by Wallet.GetBalance (and related
	// high-level helpers) when the wallet has no record of the asset
	// ref: no balance entry and no issuance or transfer root in the
	// local universe. Callers should detect it with errors.Is. A known
	// asset with zero confirmed units produces (0, nil) instead.
	ErrAssetUnknown = errors.New("asset ref is unknown to the wallet")

	// ErrNoProofs is returned when a proof bundle export/import path cannot
	// find any proof entries for a known asset ref.
	ErrNoProofs = errors.New("no proofs found for asset ref")

	// ErrIncompleteProofBundle is returned when a proof bundle is missing
	// entries or entry proof bytes.
	ErrIncompleteProofBundle = errors.New("proof bundle is incomplete")
)
