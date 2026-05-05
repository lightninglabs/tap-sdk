//go:build itest

package itest

import (
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestProtocolRecordSurface covers low-level client operations that expose
// tapd protocol rows. User-flow tests should prefer Wallet and Issuer.
func TestProtocolRecordSurface(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		minted, err := h.MintAssetAndConfirm(t, ctx,
			&entities.MintAsset{
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

		// --- ListAssetRecords honors the AssetRef filter. -----------
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

		filtered, err := h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef:  &minted.Asset.AssetRef,
				MinAmount: minted.Asset.Amount,
				MaxAmount: minted.Asset.Amount,
			},
		)
		require.NoError(t, err)
		require.NotEmpty(t, filtered)

		filtered, err = h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef:  &minted.Asset.AssetRef,
				MinAmount: minted.Asset.Amount + 1,
			},
		)
		require.NoError(t, err)
		require.Empty(t, filtered)

		filtered, err = h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef:  &minted.Asset.AssetRef,
				MaxAmount: minted.Asset.Amount - 1,
			},
		)
		require.NoError(t, err)
		require.Empty(t, filtered)

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
			&entities.MintAsset{
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
		minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 500)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		burn, err := h.AliceWallet.Burn(
			ctx, minted.Ref, 100,
			tapsdk.WithBurnNote("itest group burn"),
		)
		require.NoError(t, err)
		require.NotNil(t, burn)
		require.Equal(t, minted.Ref, burn.AssetRef)
		require.Equal(t, uint64(100), burn.Amount)
		require.NotNil(t, burn.Transfer)

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
		require.Equal(t, minted.Ref, burns[0].AssetRef)
		require.Equal(t, entities.AssetTypeFungible, burns[0].Type)
	})
}

// TestBurnCollectionItemUsesItemAssetRef verifies a burnt collection item is
// keyed by the NFT item AssetRef, not the collection AssetRef.
func TestBurnCollectionItemUsesItemAssetRef(t *testing.T) {
	requireTapdMain(t)

	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel("burn-item-" + string(transport))
		collection, err := h.CreateCollectionAndConfirm(t, ctx, name)
		require.NoError(t, err)
		require.True(t, collection.Ref.IsGroupRef())

		itemRef := entities.AssetRefFromAssetID(
			collection.Asset.Genesis.IssuanceID,
		)

		burn, err := h.AliceWallet.Burn(
			ctx, itemRef, 1,
			tapsdk.WithBurnNote("itest collection item burn"),
		)
		require.NoError(t, err)
		require.NotNil(t, burn)
		require.Equal(t, itemRef, burn.AssetRef)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

		var burns []*entities.BurnRecord
		require.Eventually(t, func() bool {
			burns, err = h.AliceWallet.ListBurns(
				ctx, &entities.ListBurnsRequest{
					AssetRef: &collection.Ref,
				},
			)
			return err == nil && len(burns) > 0
		}, defaultWaitTimeout, time.Second)

		require.Equal(t, itemRef, burns[0].AssetRef)
		require.NotEqual(t, collection.Ref, burns[0].AssetRef)
		require.NotNil(t, burns[0].CollectionRef)
		require.Equal(t, collection.Ref, *burns[0].CollectionRef)
		require.Equal(t, entities.AssetTypeNFT, burns[0].Type)
		require.Equal(t, collection.Asset.Genesis.IssuanceID,
			burns[0].IssuanceID)
	})
}
