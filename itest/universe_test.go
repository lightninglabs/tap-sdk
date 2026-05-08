//go:build itest

package itest

import (
	"context"
	"fmt"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/stretchr/testify/require"
)

// TestUniverseAssetRefSurface covers the high-level universe facade using the
// same AssetRefs returned by Wallet and Issuer calls.
func TestUniverseAssetRefSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)
		h.EnableUniverseBootstrap(t, ctx)

		aliceUniverse := h.AliceWallet.NewUniverse()
		bobUniverse := h.BobWallet.NewUniverse()

		fungibleName := uniqueEventLabel(
			fmt.Sprintf("universe-fungible-%s", transport),
		)
		fungible, err := h.CreateFungibleAndConfirm(
			t, ctx, fungibleName, 1000,
		)
		require.NoError(t, err)

		assertUniverseRefUsable(
			t, ctx, aliceUniverse, fungible.Ref, fungibleName,
		)
		assertUniverseSyncsAsset(
			t, ctx, bobUniverse, fungible.Ref,
		)

		nftName := uniqueEventLabel(
			fmt.Sprintf("universe-nft-%s", transport),
		)
		nft, err := h.CreateNFTAndConfirm(t, ctx, nftName)
		require.NoError(t, err)
		assertUniverseRefUsable(t, ctx, aliceUniverse, nft.Ref, nftName)

		collectionName := uniqueEventLabel(
			fmt.Sprintf("universe-collection-%s", transport),
		)
		collection, err := h.CreateCollectionAndConfirm(
			t, ctx, collectionName,
		)
		require.NoError(t, err)
		assertUniverseRefUsable(
			t, ctx, aliceUniverse, collection.Ref, collectionName,
		)

		itemRef := collection.Asset.AssetRef
		assertUniverseRefUsable(
			t, ctx, aliceUniverse, itemRef, collectionName,
		)

		unknown := tapsdk.AssetRefFromAssetID(itestAssetID(99))
		ok, err := aliceUniverse.HasAsset(ctx, unknown)
		require.NoError(t, err)
		require.False(t, ok)

		_, err = aliceUniverse.GetRoots(ctx, unknown)
		require.ErrorIs(t, err, tapsdk.ErrAssetUnknown)
	})
}

// TestUniverseProtocolReadSurface covers the low-level universe RPC mapping.
func TestUniverseProtocolReadSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)
		h.EnableUniverseBootstrap(t, ctx)

		name := uniqueEventLabel(fmt.Sprintf("universe-token-%s", transport))
		minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
		require.NoError(t, err)

		id := tapsdk.UniverseIDFromRef(
			minted.Ref, tapsdk.ProofTypeIssuance,
		)

		root := h.WaitForUniverseRoot(t, ctx, id, defaultWaitTimeout)
		require.Equal(t, name, root.AssetName)
		require.NotNil(t, root.MSSMTRoot)

		listedRoots, err := h.AliceClient.AssetRoots(ctx,
			&tapsdk.AssetRootRequest{
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
			&tapsdk.AssetLeafKeysRequest{
				ID: id,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, keys)

		leaves, err := h.AliceClient.AssetLeaves(ctx, &id)
		require.NoError(t, err)
		require.NotEmpty(t, leaves)

		proof, err := h.AliceClient.QueryProof(ctx,
			&tapsdk.UniverseKey{
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
			&tapsdk.AssetStatsQuery{
				AssetNameFilter: name,
				AssetTypeFilter: tapsdk.FilterAssetNormal,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, assetStats)
		require.Truef(t, hasStatsForRef(assetStats, minted.Ref),
			"missing stats for %s in %+v", minted.Ref, assetStats)
	})
}

func assertUniverseRefUsable(t testing.TB, ctx context.Context,
	universe *tapsdk.Universe, ref tapsdk.AssetRef, name string) {

	t.Helper()

	var (
		roots  *tapsdk.UniverseRoots
		proofs []*tapsdk.UniverseProof
	)
	require.Eventually(t, func() bool {
		var err error
		roots, err = universe.GetRoots(ctx, ref)
		if err != nil {
			return false
		}
		if roots.IssuanceRoot == nil {
			return false
		}

		proofs, err = universe.ListProofs(
			ctx, ref,
			tapsdk.WithUniverseProofType(
				tapsdk.ProofTypeIssuance,
			),
		)
		return err == nil && len(proofs) > 0
	}, defaultWaitTimeout, time.Second)

	require.NotNil(t, roots)
	require.NotEmpty(t, proofs)
	require.True(t, roots.HasRoots())
	require.Equal(t, name, roots.IssuanceRoot.AssetName)
	require.NotEmpty(t, proofs[0].Proof)

	proof, err := universe.GetProof(
		ctx, ref, proofs[0].LeafKey,
		tapsdk.WithUniverseProofType(proofs[0].ProofType),
	)
	require.NoError(t, err)
	require.Equal(t, proofs[0].LeafKey, proof.LeafKey)
	require.Equal(t, proofs[0].ProofType, proof.ProofType)
	require.NotEmpty(t, proof.Proof)
}

func assertUniverseSyncsAsset(t testing.TB, ctx context.Context,
	universe *tapsdk.Universe, ref tapsdk.AssetRef) {

	t.Helper()

	host := envOr("TAPD_ALICE_UNIVERSE_HOST", defaultAliceUniverseHost)
	require.Eventually(t, func() bool {
		_, err := universe.SyncAsset(
			ctx, ref, host,
			tapsdk.WithUniverseSyncMode(tapsdk.SyncIssuanceOnly),
		)
		if err != nil {
			return false
		}

		ok, err := universe.HasAsset(ctx, ref)
		return err == nil && ok
	}, defaultWaitTimeout, time.Second)
}

func itestAssetID(seed byte) tapsdk.AssetID {
	var id tapsdk.AssetID
	for i := range id {
		id[i] = seed + byte(i)
	}

	return id
}

func (h *TestHarness) WaitForUniverseRoot(t testing.TB,
	ctx context.Context, id tapsdk.UniverseID,
	timeout time.Duration) *tapsdk.UniverseRoot {

	t.Helper()

	var root *tapsdk.UniverseRoot
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

func hasStatsForRef(stats []tapsdk.AssetStatsSnapshot,
	ref tapsdk.AssetRef) bool {

	for _, stat := range stats {
		if stat.AssetRef.Equivalent(ref) {
			return true
		}
	}

	return false
}
