//go:build itest

package itest

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
)

// MintResult captures a confirmed mint together with the wallet-visible asset.
type MintResult struct {
	Asset *entities.Asset
	Batch *entities.VerboseMintingBatch
}

// MintAssetAndConfirm stages, finalizes, mines, and waits for a mint to become
// visible in Alice's wallet.
func (h *TestHarness) MintAssetAndConfirm(ctx context.Context,
	asset *entities.CreateAsset) (*MintResult, error) {

	h.t.Helper()

	if asset == nil {
		return nil, fmt.Errorf("mint asset is nil")
	}

	if asset.Name == "" {
		return nil, fmt.Errorf("mint asset name is required")
	}

	batch, err := h.AliceClient.CreateAsset(ctx, &entities.CreateAssetRequest{
		Asset:         asset,
		ShortResponse: true,
	})
	if err != nil {
		return nil, err
	}

	_, err = h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true},
	)
	if err != nil {
		return nil, err
	}

	h.MineBlocks(defaultMineBlocks)
	h.WaitForSync(h.AliceClient, 60*time.Second)

	finalized := h.WaitForMint(ctx, h.AliceClient, batch.BatchKey,
		60*time.Second)
	resultAsset := h.WaitForAssetByTag(ctx, h.AliceClient,
		asset.Name, 60*time.Second)
	if resultAsset == nil {
		return nil, fmt.Errorf("minted asset %q not found", asset.Name)
	}

	h.WaitForBalance(ctx, h.AliceWallet, resultAsset.AssetRef,
		resultAsset.Amount, 60*time.Second)

	return &MintResult{
		Asset: resultAsset,
		Batch: finalized,
	}, nil
}

// MintGroupedAsset mints a fungible asset that uses the canonical group key as
// the user-facing identifier.
func (h *TestHarness) MintGroupedAsset(ctx context.Context, name string,
	amount uint64) (*MintResult, error) {

	return h.MintAssetAndConfirm(ctx, &entities.CreateAsset{
		AssetType:     entities.AssetTypeNormal,
		Name:          name,
		InitialSupply: amount,
		AllowIssuance: true,
	})
}

// MintCollectibleAsset mints a collectible that uses the issuance asset ID as
// the user-facing identifier.
func (h *TestHarness) MintCollectibleAsset(ctx context.Context,
	name string) (*MintResult, error) {

	return h.MintAssetAndConfirm(ctx, &entities.CreateAsset{
		AssetType:     entities.AssetTypeCollectible,
		Name:          name,
		InitialSupply: 1,
	})
}
