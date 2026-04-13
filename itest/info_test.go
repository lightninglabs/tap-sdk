//go:build itest

package itest

import (
	"context"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestGetInfo verifies that we can connect to both tapd instances and retrieve
// valid node information.
func TestGetInfo(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	assertNode := func(name string, client tapsdk.Client) *entities.Info {
		t.Helper()

		info, err := client.GetInfo(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, info.Version)
		require.Equal(t, "regtest", info.Network)
		require.NotEmpty(t, info.LndVersion)

		t.Logf("%s tapd: version=%s, lnd=%s, block=%d",
			name, info.Version, info.LndVersion, info.BlockHeight)

		return info
	}

	aliceInfo := assertNode("Alice", h.AliceClient)
	bobInfo := assertNode("Bob", h.BobClient)

	require.Equal(t, aliceInfo.BlockHeight, bobInfo.BlockHeight)
}
