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
	Asset *entities.AssetRecord
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

	// Subscribe before finalization: SubscribeMintEvents has no
	// StartTimestamp option (#81 item 6) and tapd's planter does not
	// replay historical batch transitions for late subscribers, so a
	// terminal-state-during-handshake race could lose the event.
	mintEvents := h.subscribeMintEvents(t, ctx, h.AliceClient)

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

	waitForMintFinalized(t, mintEvents, batch.BatchKey,
		defaultWaitTimeout)

	// The event is the readiness signal. We still query once for the
	// verbose batch shape returned by the harness helper.
	finalized, err := h.fetchMintBatch(ctx, batch.BatchKey)
	if err != nil {
		return nil, err
	}

	// tapd has no asset-visibility or group-discovery event yet.
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

func (h *TestHarness) fetchMintBatch(ctx context.Context,
	batchKey entities.PubKey) (*entities.VerboseMintingBatch, error) {

	batches, err := h.AliceClient.ListBatches(ctx,
		&entities.ListBatchesRequest{
			BatchKey: &batchKey,
		},
	)
	if err != nil {
		return nil, err
	}

	if len(batches) != 1 || batches[0] == nil {
		return nil, fmt.Errorf("mint batch %x not found", batchKey)
	}

	return batches[0], nil
}

// WaitForSemanticAssetRef resolves the user-facing AssetRef to use for a
// minted asset. Fungible assets use the canonical group key exposed by
// ListAssetGroups, while collectibles keep their issuance asset ID.
func (h *TestHarness) WaitForSemanticAssetRef(t testing.TB,
	ctx context.Context, asset *entities.AssetRecord,
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
		groups, err := h.AliceClient.ListAssetGroups(ctx)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"group lookup not ready for %s: %v",
				asset.Genesis.Tag, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		for _, group := range groups {
			for _, candidate := range group.Members {
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

// MintCollectibleCollection mints the first NFT item in a new collection. The
// returned Ref is the collection AssetRef; the concrete item AssetRef is derived
// from result.Asset.Genesis.IssuanceID.
func (h *TestHarness) MintCollectibleCollection(t testing.TB,
	ctx context.Context, name string) (*MintResult, error) {

	return h.MintAssetAndConfirm(t, ctx, &entities.CreateAsset{
		AssetType:     entities.AssetTypeCollectible,
		Name:          name,
		InitialSupply: 1,
		AllowIssuance: true,
	})
}

// IssueCollectionItemAndConfirm mints another NFT item into an existing
// collection and returns the concrete item AssetRef.
func (h *TestHarness) IssueCollectionItemAndConfirm(t testing.TB,
	ctx context.Context, collectionRef entities.AssetRef,
	name string) (*MintResult, error) {

	t.Helper()

	mintEvents := h.subscribeMintEvents(t, ctx, h.AliceClient)

	batch, err := h.AliceClient.CreateIssuance(ctx,
		&entities.CreateIssuanceRequest{
			Issuance: &entities.CreateIssuance{
				AssetRef:  collectionRef,
				Name:      name,
				AssetType: entities.AssetTypeCollectible,
				Amount:    1,
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

	resultAsset := h.WaitForAssetByTag(t, ctx, h.AliceClient,
		name, defaultWaitTimeout)
	if resultAsset == nil {
		return nil, fmt.Errorf("collection item %q not found", name)
	}

	return &MintResult{
		Asset: resultAsset,
		Batch: finalized,
		Ref: entities.AssetRefFromAssetID(
			resultAsset.Genesis.IssuanceID,
		),
	}, nil
}
