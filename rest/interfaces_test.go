package rest

import (
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

// TestClientSatisfiesInterfaces verifies at compile time that
// rest.Client satisfies all tap-sdk client interfaces.
func TestClientSatisfiesInterfaces(t *testing.T) {
	var _ tapsdk.Client = (*Client)(nil)
	var _ tapsdk.WalletClient = (*walletClient)(nil)
	var _ tapsdk.ProofClient = (*proofClient)(nil)
	var _ tapsdk.WalletKitClient = (*walletKitClient)(nil)
	var _ tapsdk.UniverseClient = (*universeClient)(nil)
	var _ tapsdk.MintClient = (*mintClient)(nil)
}
