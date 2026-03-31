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

	// ErrNoAssetID is returned when attempting to execute a transfer
	// without specifying the asset ID.
	ErrNoAssetID = errors.New("asset ID not set")

	// ErrZeroAmount is returned when attempting to execute a transfer
	// with zero amount.
	ErrZeroAmount = errors.New("amount must be greater than zero")

	// ErrGroupKeyNotSupported is returned when a fungible
	// (group-key) AssetRef is used in a context that requires a
	// specific asset ID, such as interactive transfers.
	ErrGroupKeyNotSupported = errors.New(
		"group-key AssetRef not supported; use " +
			"AssetRefFromAssetID for interactive " +
			"transfers",
	)
)
