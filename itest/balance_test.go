//go:build itest

package itest

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestBalanceQueries verifies the opinionated balance surface for grouped
// fungible assets and standalone NFTs across every transport.
func TestBalanceQueries(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		tests := []struct {
			name string
			mint func(
				testing.TB, *TestHarness, context.Context,
			) (*MintResult, error)
			wantAmount uint64
			wantRef    func(entities.AssetRef) bool
		}{
			{
				name: "grouped fungible asset",
				mint: func(t testing.TB, h *TestHarness,
					ctx context.Context) (*MintResult, error) {

					name := uniqueEventLabel(
						"balance-token-" + string(transport),
					)
					return h.MintGroupedAsset(
						t, ctx, name, 500,
					)
				},
				wantAmount: 500,
				wantRef: func(ref entities.AssetRef) bool {
					return ref.IsGroupRef()
				},
			},
			{
				name: "standalone NFT",
				mint: func(t testing.TB, h *TestHarness,
					ctx context.Context) (*MintResult, error) {

					name := uniqueEventLabel(
						"balance-nft-" + string(transport),
					)
					return h.MintCollectibleAsset(
						t, ctx, name,
					)
				},
				wantAmount: 1,
				wantRef: func(ref entities.AssetRef) bool {
					return ref.IsAssetIDRef()
				},
			},
		}

		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				h, ctx := newFundedHarnessFor(t, transport)

				minted, err := tc.mint(t, h, ctx)
				require.NoError(t, err)
				require.NotNil(t, minted.Asset)
				require.True(t, tc.wantRef(minted.Ref))

				balance := h.WaitForBalance(
					t,
					ctx,
					h.AliceWallet,
					minted.Ref,
					tc.wantAmount,
					balanceTimeoutFor(minted.Ref),
				)
				require.Equal(t, tc.wantAmount, balance)

				balances, err := h.AliceWallet.ListBalances(
					ctx, &entities.ListBalancesRequest{
						AssetRef: &minted.Ref,
					},
				)
				require.NoError(t, err)
				requireBalanceEntry(
					t, balances, minted.Ref, tc.wantAmount,
				)
			})
		}
	})
}

// TestListBalancesMultiIssuanceFungible verifies a grouped fungible asset is
// listed as one semantic balance even when wallet funds span multiple
// issuances.
func TestListBalancesMultiIssuanceFungible(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(
			"multi-balance-token-" + string(transport),
		)
		first, err := h.MintGroupedAsset(
			t, ctx, name, 500,
		)
		require.NoError(t, err)
		require.True(t, first.Ref.IsGroupRef())

		_, err = h.IssueMoreAndConfirm(
			t, ctx, first.Ref, name, 250,
		)
		require.NoError(t, err)

		h.WaitForBalance(
			t, ctx, h.AliceWallet, first.Ref, 750,
			balanceTimeoutFor(first.Ref),
		)

		balances, err := h.AliceWallet.ListBalances(
			ctx, &entities.ListBalancesRequest{
				AssetRef: &first.Ref,
			},
		)
		require.NoError(t, err)
		requireBalanceEntry(t, balances, first.Ref, 750)
	})
}

// TestListBalancesCollectionItems verifies collection members are balance
// entries keyed by their NFT item AssetRefs. The collection AssetRef is a
// grouping handle, not a balance-bearing asset.
func TestListBalancesCollectionItems(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		firstName := uniqueEventLabel(
			"balance-collection-one-" + string(transport),
		)
		first, err := h.MintCollectibleCollection(
			t, ctx, firstName,
		)
		require.NoError(t, err)
		require.True(t, first.Ref.IsGroupRef())

		secondName := uniqueEventLabel(
			"balance-collection-two-" + string(transport),
		)
		second, err := h.IssueCollectionItemAndConfirm(
			t, ctx, first.Ref, secondName,
		)
		require.NoError(t, err)
		require.True(t, second.Ref.IsAssetIDRef())

		firstItemRef := entities.AssetRefFromAssetID(
			first.Asset.Genesis.IssuanceID,
		)

		h.WaitForBalance(
			t, ctx, h.AliceWallet, firstItemRef, 1,
			balanceTimeoutFor(firstItemRef),
		)
		h.WaitForBalance(
			t, ctx, h.AliceWallet, second.Ref, 1,
			balanceTimeoutFor(second.Ref),
		)

		balances, err := h.AliceWallet.ListBalances(ctx, nil)
		require.NoError(t, err)
		requireBalanceEntry(t, balances, firstItemRef, 1)
		requireBalanceEntry(t, balances, second.Ref, 1)
		require.NotContains(t, balances.Balances, first.Ref.String())
	})
}

// TestGetBalance_UnknownAsset verifies that a random asset ref the
// wallet has never seen surfaces ErrAssetUnknown end-to-end. This pins
// the server-side assumption that QueryAssetRoots returns no roots and
// ListAssets returns no ownership records for refs tapd has no history
// of.
func TestGetBalance_UnknownAsset(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		// An unrelated mint makes the "no record at all" outcome
		// non-trivial: tapd has assets, just not this ref.
		_, err := h.MintCollectibleAsset(t, ctx, "unknown-guard")
		require.NoError(t, err)

		ref := randomAssetIDRef(t)

		_, err = h.AliceWallet.ListBalances(
			ctx, &entities.ListBalancesRequest{
				AssetRef: &ref,
			},
		)
		require.Error(t, err)
		require.True(t, errors.Is(err, tapsdk.ErrAssetUnknown),
			"expected ErrAssetUnknown, got %v", err)

		_, err = h.AliceWallet.GetBalance(ctx, ref)
		require.Error(t, err)
		require.True(t, errors.Is(err, tapsdk.ErrAssetUnknown),
			"expected ErrAssetUnknown, got %v", err)
	})
}

// TestGetBalance_ZeroAfterUniverseBootstrap is the motivating scenario
// from issue #69: Bob learns about a fungible group by syncing Alice's
// issuance root and then queries GetBalance before any units have been
// received. The result must be (0, nil) — Bob knows the asset, he just
// owns nothing yet.
func TestGetBalance_ZeroAfterUniverseBootstrap(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintGroupedAsset(
			t, ctx, "bootstrap-token", 1000,
		)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		// Bootstrap Bob via universe sync but do not send any units.
		h.EnableUniverseBootstrap(t, ctx)
		issuanceID := entities.UniverseIDFromRef(
			minted.Ref, entities.ProofTypeIssuance,
		)
		require.Eventually(t, func() bool {
			return h.syncUniverseTarget(ctx, issuanceID) == nil
		}, defaultWaitTimeout, time.Second)

		balances, err := h.BobWallet.ListBalances(
			ctx, &entities.ListBalancesRequest{
				AssetRef: &minted.Ref,
			},
		)
		require.NoError(t, err,
			"Bob knows the group via universe but holds zero, "+
				"expected a zero balance entry; got %v", err)
		requireBalanceEntry(t, balances, minted.Ref, 0)

		balance, err := h.BobWallet.GetBalance(ctx, minted.Ref)
		require.NoError(t, err,
			"Bob knows the group via universe but holds zero, "+
				"expected (0, nil); got %v", err)
		require.Zero(t, balance)
	})
}

// randomAssetIDRef builds a 32-byte random AssetID and wraps it in an
// AssetRef. Collision with a live asset is astronomically unlikely in
// regtest.
func randomAssetIDRef(t testing.TB) entities.AssetRef {
	t.Helper()

	var raw [32]byte
	_, err := rand.Read(raw[:])
	require.NoError(t, err)

	id, err := entities.ParseAssetID(raw[:])
	require.NoError(t, err)

	return entities.AssetRefFromAssetID(id)
}

func requireBalanceEntry(t testing.TB,
	resp *entities.ListBalancesResponse, ref entities.AssetRef,
	amount uint64) {

	t.Helper()

	require.NotNil(t, resp)
	require.Contains(t, resp.Balances, ref.String())
	require.Equal(t, ref, resp.Balances[ref.String()].AssetRef)
	require.Equal(t, amount, resp.Balances[ref.String()].Balance)
}
