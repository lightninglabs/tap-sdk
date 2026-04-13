//go:build itest

package itest

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestBalanceQueries verifies the opinionated balance surface for fungible and
// collectible assets.
func TestBalanceQueries(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	h.FundLndWallet()

	tests := []struct {
		name       string
		mint       func(context.Context) (*MintResult, error)
		wantAmount uint64
		wantRef    func(entities.AssetRef) bool
	}{
		{
			name: "grouped fungible asset",
			mint: func(ctx context.Context) (*MintResult, error) {
				return h.MintGroupedAsset(ctx, "balance-token", 500)
			},
			wantAmount: 500,
			wantRef: func(ref entities.AssetRef) bool {
				return ref.IsGroupRef()
			},
		},
		{
			name: "collectible asset",
			mint: func(ctx context.Context) (*MintResult, error) {
				return h.MintCollectibleAsset(ctx, "balance-nft")
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
			minted, err := tc.mint(ctx)
			require.NoError(t, err)
			require.NotNil(t, minted.Asset)
			require.True(t, tc.wantRef(minted.Asset.AssetRef))

			balance := h.WaitForBalance(
				ctx,
				h.AliceWallet,
				minted.Asset.AssetRef,
				tc.wantAmount,
				30*time.Second,
			)
			require.Equal(t, tc.wantAmount, balance)
		})
	}
}
