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

	// Batch is populated by MintAssetAndConfirm for tests that assert the
	// low-level mint batch lifecycle.
	Batch *entities.VerboseMintingBatch

	Ref entities.AssetRef
}

// MintAssetAndConfirm adds a low-level asset to a mint batch, finalizes it,
// mines it, and waits for it to become visible in Alice's wallet.
func (h *TestHarness) MintAssetAndConfirm(t testing.TB,
	ctx context.Context, asset *entities.MintAsset) (*MintResult, error) {

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

	batch, err := h.AliceClient.MintAsset(ctx, &entities.MintAssetRequest{
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

func (h *TestHarness) confirmIssuerMint(t testing.TB,
	ctx context.Context) {

	t.Helper()

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	h.WaitForNoActiveMintBatch(
		t, ctx, h.AliceClient, defaultWaitTimeout,
	)
}

func (h *TestHarness) waitForMintRecord(t testing.TB,
	ctx context.Context, ref entities.AssetRef, tag string, amount uint64,
	timeout time.Duration) *entities.AssetRecord {

	t.Helper()

	var (
		found      *entities.AssetRecord
		lastStatus string
	)

	assetID, hasAssetID := ref.AssetID()
	require.Eventuallyf(t, func() bool {
		assets, err := h.AliceClient.ListAssetRecords(ctx,
			&entities.ListAssetsRequest{
				AssetRef: &ref,
			},
		)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"list assets for %s failed: %v", ref, err,
			)
			return false
		}

		for _, candidate := range assets {
			if candidate == nil {
				continue
			}
			if tag != "" && candidate.Genesis.Tag != tag {
				continue
			}
			if amount != 0 && candidate.Amount != amount {
				continue
			}
			if hasAssetID &&
				candidate.Genesis.IssuanceID != assetID {

				continue
			}
			if ref.IsGroupRef() &&
				!candidate.AssetRef.Equivalent(ref) {

				continue
			}

			found = candidate
			return true
		}

		lastStatus = fmt.Sprintf(
			"asset record %s tag=%q amount=%d not visible",
			ref, tag, amount,
		)
		return false
	}, timeout, time.Second,
		"asset record never became visible; last observation: %s",
		lastObservation(lastStatus),
	)

	return found
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

// CreateFungibleAndConfirm creates a fungible asset and confirms the mint.
// The returned Ref is the asset's group AssetRef.
func (h *TestHarness) CreateFungibleAndConfirm(t testing.TB, ctx context.Context,
	name string, amount uint64) (*MintResult, error) {

	t.Helper()

	asset, err := h.AliceWallet.NewIssuer().CreateFungible(
		ctx, entities.FungibleAssetSpec{
			Name:   name,
			Amount: amount,
		},
	)
	if err != nil {
		return nil, err
	}

	h.confirmIssuerMint(t, ctx)

	record := h.waitForMintRecord(
		t, ctx, asset.AssetRef, name, amount, defaultWaitTimeout,
	)

	return &MintResult{
		Asset: record,
		Ref:   asset.AssetRef,
	}, nil
}

// CreateNFTAndConfirm creates a standalone NFT and confirms the mint.
func (h *TestHarness) CreateNFTAndConfirm(t testing.TB, ctx context.Context,
	name string) (*MintResult, error) {

	t.Helper()

	asset, err := h.AliceWallet.NewIssuer().CreateNFT(
		ctx, entities.NFTSpec{Name: name},
	)
	if err != nil {
		return nil, err
	}

	h.confirmIssuerMint(t, ctx)

	record := h.waitForMintRecord(
		t, ctx, asset.AssetRef, name, 1, defaultWaitTimeout,
	)

	return &MintResult{
		Asset: record,
		Ref:   asset.AssetRef,
	}, nil
}

// CreateCollectionAndConfirm creates a collection and confirms the first item
// mint. The returned Ref is the collection AssetRef; the concrete item AssetRef
// is derived from result.Asset.Genesis.IssuanceID.
func (h *TestHarness) CreateCollectionAndConfirm(t testing.TB,
	ctx context.Context, name string) (*MintResult, error) {

	t.Helper()

	result, err := h.AliceWallet.NewIssuer().
		CreateCollection(ctx, entities.NFTSpec{Name: name})
	if err != nil {
		return nil, err
	}

	h.confirmIssuerMint(t, ctx)

	record := h.waitForMintRecord(
		t, ctx, result.FirstItem.AssetRef, name, 1, defaultWaitTimeout,
	)

	return &MintResult{
		Asset: record,
		Ref:   result.Collection.AssetRef,
	}, nil
}

// MintCollectionItemAndConfirm mints another NFT item into an existing
// collection and returns the concrete item AssetRef.
func (h *TestHarness) MintCollectionItemAndConfirm(t testing.TB,
	ctx context.Context, collectionRef entities.AssetRef,
	name string) (*MintResult, error) {

	t.Helper()

	asset, err := h.AliceWallet.NewIssuer().MintCollectionItem(
		ctx, collectionRef, entities.NFTSpec{Name: name},
	)
	if err != nil {
		return nil, err
	}

	h.confirmIssuerMint(t, ctx)

	record := h.waitForMintRecord(
		t, ctx, asset.AssetRef, name, 1, defaultWaitTimeout,
	)

	return &MintResult{
		Asset: record,
		Ref:   asset.AssetRef,
	}, nil
}

// IssueFungibleAndConfirm mints another issuance into an existing fungible
// asset and returns the issuance's concrete wallet record.
func (h *TestHarness) IssueFungibleAndConfirm(t testing.TB,
	ctx context.Context, ref entities.AssetRef, amount uint64) (*MintResult,
	error) {

	t.Helper()

	issuance, err := h.AliceWallet.NewIssuer().IssueFungible(
		ctx, ref, amount,
	)
	if err != nil {
		return nil, err
	}

	h.confirmIssuerMint(t, ctx)

	issuanceRef := entities.AssetRefFromAssetID(issuance.IssuanceID)
	record := h.waitForMintRecord(
		t, ctx, issuanceRef, issuance.Name, amount, defaultWaitTimeout,
	)

	return &MintResult{
		Asset: record,
		Ref:   ref,
	}, nil
}

func mintGroupedAssetSpec(name string, amount uint64) *entities.MintAsset {
	return &entities.MintAsset{
		AssetType:     entities.AssetTypeFungible,
		Name:          name,
		InitialSupply: amount,
		AllowIssuance: true,
	}
}

func mintCollectibleAssetSpec(name string) *entities.MintAsset {
	return &entities.MintAsset{
		AssetType:     entities.AssetTypeNFT,
		Name:          name,
		InitialSupply: 1,
	}
}
