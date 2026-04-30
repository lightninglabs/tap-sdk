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

// TestMultiTrancheGroup exercises a second issuance into an existing fungible
// group and verifies the SDK reports the semantic asset as one AssetRef while
// still exposing both issuance tranches in ListAssetGroups.
func TestMultiTrancheGroup(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("tranche-token-%s", transport))
		first, err := h.MintGroupedAsset(t, ctx, name, 1000)
		require.NoError(t, err)
		require.True(t, first.Ref.IsGroupRef())

		second, err := h.IssueMoreAndConfirm(
			t, ctx, first.Ref, name, 250,
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

// IssueMoreAndConfirm stages and confirms an additional issuance/tranche in an
// existing group.
func (h *TestHarness) IssueMoreAndConfirm(t testing.TB,
	ctx context.Context, ref entities.AssetRef, name string,
	amount uint64) (*MintResult, error) {

	t.Helper()

	mintEvents := h.subscribeMintEvents(t, ctx, h.AliceClient)

	batch, err := h.AliceClient.CreateIssuance(ctx,
		&entities.CreateIssuanceRequest{
			Issuance: &entities.CreateIssuance{
				AssetRef:  ref,
				Name:      name,
				AssetType: entities.AssetTypeNormal,
				Amount:    amount,
			},
			ShortResponse: true,
		},
	)
	if err != nil {
		return nil, err
	}

	_, err = h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true},
	)
	if err != nil {
		return nil, err
	}

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultWaitTimeout)

	waitForMintFinalized(t, mintEvents, batch.BatchKey,
		defaultWaitTimeout)

	finalized, err := h.fetchMintBatch(ctx, batch.BatchKey)
	if err != nil {
		return nil, err
	}

	var resultAsset *entities.AssetRecord
	require.Eventually(t, func() bool {
		assets, err := h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &ref,
			},
		)
		if err != nil {
			return false
		}

		for _, asset := range assets {
			if asset != nil && asset.Amount == amount &&
				asset.Genesis.Tag == name {

				resultAsset = asset
				return true
			}
		}

		return false
	}, defaultWaitTimeout, time.Second)

	return &MintResult{
		Asset: resultAsset,
		Batch: finalized,
		Ref:   ref,
	}, nil
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
