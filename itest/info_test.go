//go:build itest

package itest

import (
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/stretchr/testify/require"
)

// TestGetInfo verifies that we can connect to both tapd instances and
// retrieve valid node information, across every transport.
func TestGetInfo(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newHarnessContextFor(t, transport)

		assertNode := func(
			name string, client tapsdk.Client,
		) *tapsdk.Info {

			t.Helper()

			info, err := client.GetInfo(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, info.Version)
			require.Equal(t, "regtest", info.Network)
			require.NotEmpty(t, info.LndVersion)

			verboseLogf(t, "%s tapd (%s): version=%s, lnd=%s, "+
				"block=%d",
				name, transport, info.Version,
				info.LndVersion, info.BlockHeight)

			return info
		}

		aliceInfo := assertNode("Alice", h.AliceClient)
		bobInfo := assertNode("Bob", h.BobClient)

		require.Equal(t, aliceInfo.BlockHeight, bobInfo.BlockHeight)
	})
}
