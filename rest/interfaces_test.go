package rest_test

import (
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/rest"
)

// TestClientSatisfiesInterfaces verifies at compile time that
// rest.Client satisfies all tap-sdk client interfaces.
func TestClientSatisfiesInterfaces(t *testing.T) {
	var _ tapsdk.Client = (*rest.Client)(nil)
}
