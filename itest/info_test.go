//go:build itest

package itest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGetInfo verifies that we can connect to both tapd instances and
// retrieve valid node information.
func TestGetInfo(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	// Alice's tapd should respond with valid info.
	aliceInfo, err := h.AliceClient.GetInfo(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, aliceInfo.Version)
	require.Equal(t, "regtest", aliceInfo.Network)
	require.NotEmpty(t, aliceInfo.LndVersion)

	t.Logf("Alice tapd: version=%s, lnd=%s, block=%d",
		aliceInfo.Version, aliceInfo.LndVersion,
		aliceInfo.BlockHeight)

	// Bob's tapd should respond with valid info.
	bobInfo, err := h.BobClient.GetInfo(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, bobInfo.Version)
	require.Equal(t, "regtest", bobInfo.Network)

	t.Logf("Bob tapd: version=%s, lnd=%s, block=%d",
		bobInfo.Version, bobInfo.LndVersion,
		bobInfo.BlockHeight)

	// Both nodes should see the same block height.
	require.Equal(t, aliceInfo.BlockHeight, bobInfo.BlockHeight)
}
