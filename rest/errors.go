package rest

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrUnsupportedNetwork is returned when an unknown network is
// specified in the configuration.
var ErrUnsupportedNetwork = errors.New("unsupported network")

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
	GRPCCode   codes.Code
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

// GRPCStatus exposes the embedded gRPC code so callers can use the same
// status helpers as the native gRPC transport.
func (e *APIError) GRPCStatus() *status.Status {
	return status.New(e.GRPCCode, e.Message)
}
