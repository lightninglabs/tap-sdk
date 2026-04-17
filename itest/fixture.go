//go:build itest

package itest

import (
	"context"
	"os"
	"strings"
	"testing"
)

// allTransports is the set of SDK client transports a parametrised test
// runs against by default.
var allTransports = []Transport{TransportGRPC, TransportREST}

// selectedTransports returns the transports the harness should exercise.
// The TAP_SDK_TRANSPORTS env var overrides the default (comma-separated).
func selectedTransports(t testing.TB) []Transport {
	t.Helper()

	raw := os.Getenv("TAP_SDK_TRANSPORTS")
	if raw == "" {
		return allTransports
	}

	var out []Transport
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(strings.ToLower(tok))
		switch Transport(tok) {
		case TransportGRPC, TransportREST:
			out = append(out, Transport(tok))
		}
	}

	if len(out) == 0 {
		return allTransports
	}

	return out
}

// runForTransports runs fn for every selected transport as a named
// subtest. Tests that need transport-specific behavior should gate on
// harness.Transport.
func runForTransports(t *testing.T, fn func(*testing.T, Transport)) {
	t.Helper()

	for _, tr := range selectedTransports(t) {
		tr := tr
		t.Run(string(tr), func(t *testing.T) {
			fn(t, tr)
		})
	}
}

// newHarnessContext creates a harness plus background context for tests
// that only need connected clients over the default gRPC transport.
func newHarnessContext(t testing.TB) (*TestHarness, context.Context) {
	t.Helper()

	return NewTestHarness(t), context.Background()
}

// newHarnessContextFor builds a harness for the requested transport.
func newHarnessContextFor(t testing.TB,
	transport Transport) (*TestHarness, context.Context) {

	t.Helper()

	return NewTestHarnessWithTransport(t, transport), context.Background()
}

// newFundedHarness creates a harness and funds Alice's LND wallet so tests
// can focus on the asset flow they actually care about.
func newFundedHarness(t testing.TB) (*TestHarness, context.Context) {
	t.Helper()

	return newFundedHarnessFor(t, TransportGRPC)
}

// newFundedHarnessFor builds and funds a harness for the given transport.
func newFundedHarnessFor(t testing.TB,
	transport Transport) (*TestHarness, context.Context) {

	t.Helper()

	h, ctx := newHarnessContextFor(t, transport)
	h.FundLndWallet(t, ctx)

	return h, ctx
}
