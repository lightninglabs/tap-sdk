//go:build itest

package itest

import (
	"context"
	"fmt"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestMultiTrancheGroup exercises a second issuance into an existing fungible
// group and verifies the SDK reports the semantic asset as one AssetRef while
// still exposing both issuance tranches in ListAssetGroups.
func TestMultiTrancheGroup(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("tranche-token-%s", transport))
		first, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
		require.NoError(t, err)
		require.True(t, first.Ref.IsGroupRef())

		second, err := h.IssueFungibleAndConfirm(
			t, ctx, first.Ref, 250,
		)
		require.NoError(t, err)
		require.NotNil(t, second)
		require.Equal(t, first.Ref, second.Ref)

		balance := h.WaitForBalance(
			t, ctx, h.AliceWallet, first.Ref, 1250,
			balanceTimeoutFor(first.Ref),
		)
		require.Equal(t, uint64(1250), balance)

		assets, err := h.AliceWallet.ListAssets(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		require.Len(t, assets, 1)
		require.Equal(t, first.Ref, assets[0].AssetRef)
		require.Equal(t, entities.AssetTypeNormal, assets[0].Type)
		require.GreaterOrEqual(t, assets[0].Amount, uint64(1250))

		issuances, err := h.AliceWallet.ListIssuances(ctx,
			&entities.ListIssuancesRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(issuances), 2)

		group := h.RequireGroup(t, ctx, first.Ref)
		require.GreaterOrEqual(t, len(group.Members), 2)

		var (
			sawFirst  bool
			sawSecond bool
			total     uint64
		)
		for _, asset := range group.Members {
			require.Equal(t, first.Ref, asset.AssetRef)
			total += asset.Amount

			switch asset.IssuanceID {
			case first.Asset.Genesis.IssuanceID:
				sawFirst = true
			case second.Asset.Genesis.IssuanceID:
				sawSecond = true
			}
		}

		require.True(t, sawFirst)
		require.True(t, sawSecond)
		require.GreaterOrEqual(t, total, uint64(1250))
	})
}

// RequireGroup returns the grouped-asset row for a semantic group ref.
func (h *TestHarness) RequireGroup(t testing.TB, ctx context.Context,
	ref entities.AssetRef) entities.AssetGroupRecord {

	t.Helper()

	groups, err := h.AliceClient.ListAssetGroups(ctx)
	require.NoError(t, err)

	for _, group := range groups {
		if group.AssetRef == ref {
			return group
		}
	}

	require.FailNowf(t, "group not found", "ref=%s", ref)
	return entities.AssetGroupRecord{}
}
