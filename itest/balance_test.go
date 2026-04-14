//go:build itest

package itest

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestBalanceQueries verifies the opinionated balance surface for fungible and
// collectible assets.
func TestBalanceQueries(t *testing.T) {
	tests := []struct {
		name string
		mint func(testing.TB, *TestHarness, context.Context) (*MintResult,
			error)
		wantAmount uint64
		wantRef    func(entities.AssetRef) bool
	}{
		{
			name: "grouped fungible asset",
			mint: func(t testing.TB, h *TestHarness,
				ctx context.Context) (*MintResult, error) {

				return h.MintGroupedAsset(t, ctx,
					"balance-token", 500)
			},
			wantAmount: 500,
			wantRef: func(ref entities.AssetRef) bool {
				return ref.IsGroupRef()
			},
		},
		{
			name: "collectible asset",
			mint: func(t testing.TB, h *TestHarness,
				ctx context.Context) (*MintResult, error) {

				return h.MintCollectibleAsset(t, ctx,
					"balance-nft")
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
			h, ctx := newFundedHarness(t)

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
		})
	}
}
