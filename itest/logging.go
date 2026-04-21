//go:build itest

package itest

import "testing"

// verboseLogf emits helper progress logs only for explicit verbose test runs.
// The default CI path keeps output focused on the failing assertion instead.
func verboseLogf(t testing.TB, format string, args ...any) {
	t.Helper()

	if testing.Verbose() {
		t.Logf(format, args...)
	}
}

func lastObservation(last string) string {
	if last == "" {
		return "no observation recorded"
	}

	return last
}
