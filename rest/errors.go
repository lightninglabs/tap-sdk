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
