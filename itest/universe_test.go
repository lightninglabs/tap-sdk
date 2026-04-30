//go:build itest

package itest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestUniverseReadSurface covers the read-only universe RPCs that applications
// use to bootstrap and inspect asset state without falling back to taprpc.
func TestUniverseReadSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)
		h.EnableUniverseBootstrap(t, ctx)

		name := uniqueEventLabel(fmt.Sprintf("universe-token-%s", transport))
		minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
		require.NoError(t, err)

		id := entities.UniverseIDFromRef(
			minted.Ref, entities.ProofTypeIssuance,
		)

		root := h.WaitForUniverseRoot(t, ctx, id, defaultWaitTimeout)
		require.Equal(t, name, root.AssetName)
		require.NotNil(t, root.MSSMTRoot)

		listedRoots, err := h.AliceClient.AssetRoots(ctx,
			&entities.AssetRootRequest{
				WithAmountsByID: true,
			},
		)
		require.NoError(t, err)
		require.NotNil(t, listedRoots)

		queriedRoots, err := h.AliceClient.QueryAssetRoots(ctx, &id)
		require.NoError(t, err)
		require.NotNil(t, queriedRoots.IssuanceRoot)
		require.True(t, minted.Ref.Equivalent(
			queriedRoots.IssuanceRoot.ID.AssetRef,
		))

		keys, err := h.AliceClient.AssetLeafKeys(ctx,
			&entities.AssetLeafKeysRequest{
				ID: id,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, keys)

		leaves, err := h.AliceClient.AssetLeaves(ctx, &id)
		require.NoError(t, err)
		require.NotEmpty(t, leaves)

		proof, err := h.AliceClient.QueryProof(ctx,
			&entities.UniverseKey{
				ID:      id,
				LeafKey: keys[0],
			},
		)
		require.NoError(t, err)
		require.NotNil(t, proof)
		require.NotNil(t, proof.UniverseRoot)
		require.NotNil(t, proof.AssetLeaf)
		require.NotEmpty(t, proof.AssetLeaf.Proof)

		stats, err := h.AliceClient.UniverseStats(ctx)
		require.NoError(t, err)
		require.Greater(t, stats.NumTotalAssets, int64(0))
		require.Greater(t, stats.NumTotalProofs, int64(0))

		assetStats, err := h.AliceClient.QueryAssetStats(ctx,
			&entities.AssetStatsQuery{
				AssetNameFilter: name,
				AssetTypeFilter: entities.FilterAssetNormal,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, assetStats)
		require.Truef(t, hasStatsForRef(assetStats, minted.Ref),
			"missing stats for %s in %+v", minted.Ref, assetStats)
	})
}

func (h *TestHarness) WaitForUniverseRoot(t testing.TB,
	ctx context.Context, id entities.UniverseID,
	timeout time.Duration) *entities.UniverseRoot {

	t.Helper()

	var root *entities.UniverseRoot
	require.Eventually(t, func() bool {
		roots, err := h.AliceClient.QueryAssetRoots(ctx, &id)
		if err != nil {
			return false
		}
		if roots == nil {
			return false
		}

		candidate := roots.IssuanceRoot
		if candidate != nil &&
			candidate.ID.ProofType == id.ProofType &&
			candidate.ID.AssetRef.Equivalent(id.AssetRef) {

			root = candidate
			return true
		}

		return false
	}, timeout, time.Second)

	return root
}

func hasStatsForRef(stats []entities.AssetStatsSnapshot,
	ref entities.AssetRef) bool {

	for _, stat := range stats {
		if stat.AssetRef.Equivalent(ref) {
			return true
		}
	}

	return false
}
