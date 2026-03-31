package rest

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsupportedNetwork is returned when an unknown network is
	// specified in the configuration.
	ErrUnsupportedNetwork = errors.New("unsupported network")

	// ErrMacaroonConflict is returned when more than one macaroon
	// source is specified (directory, path, or hex).
	ErrMacaroonConflict = errors.New(
		"must set only one: MacaroonDir, MacaroonPath, or MacaroonHex",
	)

	// ErrTLSConflict is returned when both TLSPath and TLSData are
	// set.
	ErrTLSConflict = errors.New("must set only one: TLSPath or TLSData")

	// ErrInsecureSystemCert is returned when both Insecure and
	// SystemCert are set.
	ErrInsecureSystemCert = errors.New(
		"cannot set insecure and system cert at the same time",
	)
)

// ErrNotImplemented is the base error for methods that haven't
// been implemented yet in the REST transport.
var ErrNotImplemented = errors.New("not implemented in REST transport")

// errNotImplemented returns an error indicating that a given REST
// method is not yet implemented.
func errNotImplemented(method string) error {
	return fmt.Errorf("%s: %w", method, ErrNotImplemented)
}

// APIError represents an error returned by the tapd REST API.
type APIError struct {
	StatusCode int
	Message    string
	Details    string
}

func (e *APIError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("tapd REST API error %d: %s (%s)",
			e.StatusCode, e.Message, e.Details)
	}

	return fmt.Sprintf("tapd REST API error %d: %s",
		e.StatusCode, e.Message)
}
