//go:build itest

package itest

import (
	"context"
	"testing"
)

// newFundedHarness creates a harness and funds Alice's LND wallet so tests can
// focus on the asset flow they actually care about.
func newFundedHarness(t testing.TB) (*TestHarness, context.Context) {
	t.Helper()

	h := NewTestHarness(t)
	ctx := context.Background()
	h.FundLndWallet(t, ctx)

	return h, ctx
}
