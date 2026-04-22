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

// MintResult captures a confirmed mint together with the semantic AssetRef to
// use for high-level wallet operations.
type MintResult struct {
	Asset *entities.Asset
	Batch *entities.VerboseMintingBatch
	Ref   entities.AssetRef
}

// MintAssetAndConfirm stages, finalizes, mines, and waits for a mint to become
// visible in Alice's wallet.
func (h *TestHarness) MintAssetAndConfirm(t testing.TB,
	ctx context.Context, asset *entities.CreateAsset) (*MintResult, error) {

	t.Helper()

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

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultWaitTimeout)

	finalized := h.WaitForMint(t, ctx, h.AliceClient, batch.BatchKey,
		defaultWaitTimeout)
	resultAsset := h.WaitForAssetByTag(t, ctx, h.AliceClient,
		asset.Name, defaultWaitTimeout)
	if resultAsset == nil {
		return nil, fmt.Errorf("minted asset %q not found", asset.Name)
	}

	semanticRef := h.WaitForSemanticAssetRef(
		t, ctx, resultAsset, defaultWaitTimeout,
	)

	return &MintResult{
		Asset: resultAsset,
		Batch: finalized,
		Ref:   semanticRef,
	}, nil
}

// WaitForSemanticAssetRef resolves the user-facing AssetRef to use for a
// minted asset. Fungible assets use the canonical group key exposed by
// ListGroups, while collectibles keep their issuance asset ID.
func (h *TestHarness) WaitForSemanticAssetRef(t testing.TB,
	ctx context.Context, asset *entities.Asset,
	timeout time.Duration) entities.AssetRef {

	t.Helper()

	if asset == nil {
		require.FailNow(t, "minted asset is nil")
	}

	if asset.AssetRef.IsAssetIDRef() {
		return asset.AssetRef
	}

	var ref entities.AssetRef
	var lastStatus string
	require.Eventuallyf(t, func() bool {
		groups, err := h.AliceClient.ListGroups(ctx)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"group lookup not ready for %s: %v",
				asset.Genesis.Tag, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		for _, group := range groups {
			for _, candidate := range group.Assets {
				if candidate == nil ||
					candidate.IssuanceID != asset.Genesis.IssuanceID {

					continue
				}

				ref = group.AssetRef
				return true
			}
		}

		lastStatus = fmt.Sprintf(
			"group key for %s not visible yet",
			asset.Genesis.Tag,
		)
		return false
	}, timeout, time.Second,
		"semantic asset ref for %s never became available; "+
			"last observation: %s",
		asset.Genesis.Tag, lastObservation(lastStatus),
	)

	return ref
}

// MintGroupedAsset mints a fungible asset that uses the canonical group key as
// the user-facing identifier.
func (h *TestHarness) MintGroupedAsset(t testing.TB, ctx context.Context,
	name string, amount uint64) (*MintResult, error) {

	return h.MintAssetAndConfirm(t, ctx, &entities.CreateAsset{
		AssetType:     entities.AssetTypeNormal,
		Name:          name,
		InitialSupply: amount,
		AllowIssuance: true,
	})
}

// MintCollectibleAsset mints a collectible that uses the issuance asset ID as
// the user-facing identifier.
func (h *TestHarness) MintCollectibleAsset(t testing.TB, ctx context.Context,
	name string) (*MintResult, error) {

	return h.MintAssetAndConfirm(t, ctx, &entities.CreateAsset{
		AssetType:     entities.AssetTypeCollectible,
		Name:          name,
		InitialSupply: 1,
	})
}
