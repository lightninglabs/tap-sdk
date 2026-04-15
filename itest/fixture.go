//go:build itest

package itest

import (
	"context"
	"testing"
)

// newHarnessContext creates a harness plus background context for tests that
// only need connected clients.
func newHarnessContext(t testing.TB) (*TestHarness, context.Context) {
	t.Helper()

	return NewTestHarness(t), context.Background()
}

// newFundedHarness creates a harness and funds Alice's LND wallet so tests can
// focus on the asset flow they actually care about.
func newFundedHarness(t testing.TB) (*TestHarness, context.Context) {
	t.Helper()

	h, ctx := newHarnessContext(t)
	h.FundLndWallet(t, ctx)

	return h, ctx
}
