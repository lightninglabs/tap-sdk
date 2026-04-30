//go:build itest

package itest

import (
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestWalletSurface covers read-only wallet operations that are easy to
// reason about once at least one asset has been minted. We run it against
// every transport so gRPC and REST stay in lock-step.
func TestWalletSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(t, ctx,
			&entities.CreateAsset{
				AssetType:     entities.AssetTypeNormal,
				Name:          "surface-token",
				InitialSupply: 100,
				AssetMeta: &entities.AssetMeta{
					Data: []byte(`{"sku":"SURFACE-1"}`),
					Type: entities.AssetMetaTypeJSON,
				},
			},
		)
		require.NoError(t, err)
		require.NotNil(t, minted.Asset)

		// --- ListAssets honors the AssetRef filter. -----------------
		assets, err := h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &minted.Asset.AssetRef,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, assets)
		for _, a := range assets {
			require.Equal(t, minted.Asset.AssetRef, a.AssetRef)
		}

		// --- ListUtxos surfaces managed UTXOs. ----------------------
		utxos, err := h.AliceClient.ListUtxos(ctx,
			&entities.ListUtxosRequest{},
		)
		require.NoError(t, err)
		require.NotEmpty(t, utxos)

		// --- ListAssetGroups must be queryable without a filter. ---------
		groups, err := h.AliceClient.ListAssetGroups(ctx)
		require.NoError(t, err)
		require.NotNil(t, groups)

		// --- FetchAssetMeta round-trips the JSON payload. -----------
		meta, err := h.AliceClient.FetchAssetMeta(ctx,
			&entities.FetchAssetMetaRequest{
				AssetRef: &minted.Asset.AssetRef,
			},
		)
		require.NoError(t, err)
		require.Equal(t, entities.AssetMetaTypeJSON, meta.Type)
		require.JSONEq(t, `{"sku":"SURFACE-1"}`, string(meta.Data))
	})
}

// TestBurnAsset exercises BurnAsset + ListBurns for an asset-id ref.
func TestBurnAsset(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(t, ctx,
			&entities.CreateAsset{
				AssetType: entities.AssetTypeNormal,
				Name: uniqueEventLabel(
					"burn-token-" + string(transport),
				),
				InitialSupply: 500,
			},
		)
		require.NoError(t, err)
		require.True(t, minted.Asset.AssetRef.IsAssetIDRef())

		resp, err := h.AliceClient.BurnAsset(ctx,
			&entities.BurnAssetRequest{
				AssetRef:         minted.Asset.AssetRef,
				AmountToBurn:     100,
				ConfirmationText: "assets will be destroyed",
				Note:             "itest burn",
			},
		)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.BurnTransfer)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

		var burned uint64
		require.Eventually(t, func() bool {
			burns, err := h.AliceClient.ListBurns(ctx,
				&entities.ListBurnsRequest{
					AssetRef: &minted.Asset.AssetRef,
				},
			)
			if err != nil || len(burns) == 0 {
				return false
			}

			burned = 0
			for _, b := range burns {
				burned += b.Amount
			}

			return burned == 100
		}, defaultWaitTimeout, time.Second)
		require.Equal(t, uint64(100), burned)
	})
}

// TestBurnAssetByGroupKey burns units selected through a group-key
// AssetRef. The underlying RPC gained group-key support after v0.7.2, so
// this test is gated on a tapd main build.
func TestBurnAssetByGroupKey(t *testing.T) {
	requireTapdMain(t)

	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel("burn-group-" + string(transport))
		minted, err := h.MintGroupedAsset(t, ctx, name, 500)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		resp, err := h.AliceClient.BurnAsset(ctx,
			&entities.BurnAssetRequest{
				AssetRef:         minted.Ref,
				AmountToBurn:     100,
				ConfirmationText: "assets will be destroyed",
				Note:             "itest group burn",
			},
		)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.BurnTransfer)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

		remaining := h.WaitForBalance(t, ctx, h.AliceWallet,
			minted.Ref, 400, balanceTimeoutFor(minted.Ref))
		require.Equal(t, uint64(400), remaining)

		burns, err := h.AliceClient.ListBurns(ctx,
			&entities.ListBurnsRequest{
				AssetRef: &minted.Ref,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, burns)
	})
}
