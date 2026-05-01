package tapsdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
)

const (
	defaultMintResolveTimeout  = 10 * time.Second
	defaultMintResolveInterval = 200 * time.Millisecond
	defaultMintCancelTimeout   = 5 * time.Second
)

var errMintResultNotReady = errors.New("mint result is not visible yet")

// Issuer is the high-level minting surface for SDK business entities.
//
// It creates grouped fungible assets, standalone NFTs, NFT collections, and
// additional supply or collection items while hiding tapd batch mechanics from
// application code. MintClient remains available through Client for callers
// that need direct batch control.
//
// Issuer serializes its own mint calls because tapd has one active mint batch
// per daemon. Issuers returned by the same Wallet share that lock.
type Issuer struct {
	client Client
	mu     *sync.Mutex
}

// NewIssuer creates a high-level issuer backed by the given client.
// Concurrent calls on the returned Issuer are serialized.
func NewIssuer(client Client) *Issuer {
	return &Issuer{
		client: client,
		mu:     &sync.Mutex{},
	}
}

// NewIssuer returns a high-level issuer backed by the wallet's client.
// Issuers returned by the same Wallet share mint-call serialization.
func (s *Wallet) NewIssuer() *Issuer {
	return &Issuer{
		client: s.client,
		mu:     &s.issuerMu,
	}
}

// MintOption configures high-level issuer calls.
type MintOption func(*MintOptions)

// MintOptions contains optional parameters for high-level issuer calls.
type MintOptions struct {
	// feeRate is the target fee rate in sat/kw for the genesis transaction.
	// Zero uses the daemon default.
	feeRate uint32

	// resolveTimeout is how long high-level issuer calls wait for tapd's
	// wallet projection to expose the accepted mint result.
	resolveTimeout time.Duration
}

// WithMintFeeRate sets the target fee rate in sat/kw for the genesis
// transaction. A zero value uses tapd's daemon default.
func WithMintFeeRate(feeRate uint32) MintOption {
	return func(o *MintOptions) {
		o.feeRate = feeRate
	}
}

// WithMintResolveTimeout sets how long high-level issuer calls wait for tapd's
// wallet projection to expose the accepted mint result. If this timeout is hit,
// the call returns ErrMintResolveTimeout and the mint may still have been
// accepted by tapd.
func WithMintResolveTimeout(timeout time.Duration) MintOption {
	return func(o *MintOptions) {
		o.resolveTimeout = timeout
	}
}

// CreateFungible creates a grouped fungible asset and returns the SDK business
// Asset keyed by its group AssetRef.
//
// If the returned error wraps ErrMintResolveTimeout, tapd may already have
// accepted or finalized the mint. Do not blindly retry; inspect wallet assets,
// issuances, or mint batches first to avoid duplicate issuance.
func (i *Issuer) CreateFungible(ctx context.Context,
	spec entities.FungibleAssetSpec,
	opts ...MintOption) (*entities.Asset, error) {

	unlock := i.lock()
	defer unlock()

	asset, err := i.createFungible(ctx, spec, opts...)
	if err != nil {
		return nil, wrapErr("CreateFungible", err)
	}

	return asset, nil
}

// IssueFungible creates an additional issuance for an existing grouped
// fungible asset.
//
// If the returned error wraps ErrMintResolveTimeout, tapd may already have
// accepted or finalized the mint. Do not blindly retry; inspect wallet assets,
// issuances, or mint batches first to avoid duplicate issuance.
func (i *Issuer) IssueFungible(ctx context.Context,
	ref entities.AssetRef, amount uint64,
	opts ...MintOption) (*entities.Issuance, error) {

	unlock := i.lock()
	defer unlock()

	issuance, err := i.issueFungible(ctx, ref, amount, opts...)
	if err != nil {
		return nil, wrapErr("IssueFungible", err)
	}

	return issuance, nil
}

// CreateNFT creates a standalone NFT and returns it as an Asset keyed by its
// asset-ID AssetRef.
//
// If the returned error wraps ErrMintResolveTimeout, tapd may already have
// accepted or finalized the mint. Do not blindly retry; inspect wallet assets
// or mint batches first to avoid duplicate NFTs.
func (i *Issuer) CreateNFT(ctx context.Context, spec entities.NFTSpec,
	opts ...MintOption) (*entities.Asset, error) {

	unlock := i.lock()
	defer unlock()

	asset, err := i.createNFT(ctx, spec, opts...)
	if err != nil {
		return nil, wrapErr("CreateNFT", err)
	}

	return asset, nil
}

// CreateCollection creates a new NFT collection by minting the first item in a
// grouped collectible asset. It returns the collection and first item together
// in a CollectionMintResult.
//
// If the returned error wraps ErrMintResolveTimeout, tapd may already have
// accepted or finalized the mint. Do not blindly retry; inspect wallet assets,
// collections, or mint batches first to avoid duplicate NFTs.
func (i *Issuer) CreateCollection(ctx context.Context,
	firstItem entities.NFTSpec,
	opts ...MintOption) (*entities.CollectionMintResult, error) {

	unlock := i.lock()
	defer unlock()

	result, err := i.createCollection(ctx, firstItem, opts...)
	if err != nil {
		return nil, wrapErr("CreateCollection", err)
	}

	return result, nil
}

// MintCollectionItem mints a new NFT item into an existing collection.
//
// If the returned error wraps ErrMintResolveTimeout, tapd may already have
// accepted or finalized the mint. Do not blindly retry; inspect wallet assets,
// collections, or mint batches first to avoid duplicate NFTs.
func (i *Issuer) MintCollectionItem(ctx context.Context,
	collectionRef entities.AssetRef, item entities.NFTSpec,
	opts ...MintOption) (*entities.Asset, error) {

	unlock := i.lock()
	defer unlock()

	asset, err := i.mintCollectionItem(ctx, collectionRef, item, opts...)
	if err != nil {
		return nil, wrapErr("MintCollectionItem", err)
	}

	return asset, nil
}

func (i *Issuer) createFungible(ctx context.Context,
	spec entities.FungibleAssetSpec,
	opts ...MintOption) (*entities.Asset, error) {

	if err := validateFungibleSpec(spec); err != nil {
		return nil, err
	}

	stage := &entities.MintAsset{
		AssetVersion:            spec.AssetVersion,
		AssetType:               entities.AssetTypeFungible,
		Name:                    spec.Name,
		AssetMeta:               spec.AssetMeta,
		InitialSupply:           spec.Amount,
		AllowIssuance:           true,
		DecimalDisplay:          spec.DecimalDisplay,
		ScriptKey:               spec.ScriptKey,
		EnableSupplyCommitments: spec.EnableSupplyCommitments,
	}

	record, err := i.mintAssetAndResolve(ctx, stage, nil, func(
		record *entities.AssetRecord) bool {

		return isRecord(record, entities.AssetTypeFungible, spec.Name,
			spec.Amount) && record.AssetRef.IsGroupRef()
	}, opts)
	if err != nil {
		return nil, err
	}

	assets := assetsFromRecords([]*entities.AssetRecord{record})
	if len(assets) != 1 {
		return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound,
			spec.Name)
	}

	return assets[0], nil
}

func (i *Issuer) issueFungible(ctx context.Context,
	ref entities.AssetRef, amount uint64,
	opts ...MintOption) (*entities.Issuance, error) {

	if err := validateGroupedRef(ref); err != nil {
		return nil, err
	}
	if amount == 0 {
		return nil, ErrZeroAmount
	}

	mintOpts := applyMintOptions(opts)
	if err := i.ensureReady(ctx); err != nil {
		return nil, err
	}

	base, err := i.resolveFungibleAsset(ctx, ref)
	if err != nil {
		return nil, err
	}

	before, err := i.listMintRecords(ctx, &base.AssetRef)
	if err != nil {
		return nil, err
	}

	stage := &entities.MintIssuance{
		AssetRef:  base.AssetRef,
		Name:      base.Genesis.Tag,
		AssetType: entities.AssetTypeFungible,
		Amount:    amount,
	}

	err = i.mintAndFinalize(ctx, func() (*entities.MintingBatch,
		error) {

		return i.client.MintIssuance(ctx,
			&entities.MintIssuanceRequest{
				Issuance:      stage,
				ShortResponse: true,
			},
		)
	}, mintOpts)
	if err != nil {
		return nil, err
	}

	record, err := i.findNewRecord(ctx, &base.AssetRef, recordIDs(before),
		func(record *entities.AssetRecord) bool {
			return isRecord(
				record, entities.AssetTypeFungible,
				base.Genesis.Tag, amount,
			)
		},
		mintOpts.resolveTimeoutOrDefault(),
	)
	if err != nil {
		return nil, err
	}

	issuances := issuancesFromRecords([]*entities.AssetRecord{record})
	if len(issuances) != 1 {
		return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound, ref)
	}

	return issuances[0], nil
}

func (i *Issuer) createNFT(ctx context.Context, spec entities.NFTSpec,
	opts ...MintOption) (*entities.Asset, error) {

	if err := validateNFTSpec(spec); err != nil {
		return nil, err
	}

	stage := mintNFTAsset(spec, false)
	record, err := i.mintAssetAndResolve(ctx, stage, nil, func(
		record *entities.AssetRecord) bool {

		return isRecord(record, entities.AssetTypeNFT, spec.Name, 1) &&
			record.AssetRef.IsAssetIDRef()
	}, opts)
	if err != nil {
		return nil, err
	}

	assets := assetsFromRecords([]*entities.AssetRecord{record})
	if len(assets) != 1 {
		return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound,
			spec.Name)
	}

	return assets[0], nil
}

func (i *Issuer) createCollection(ctx context.Context,
	firstItem entities.NFTSpec,
	opts ...MintOption) (*entities.CollectionMintResult, error) {

	if err := validateNFTSpec(firstItem); err != nil {
		return nil, err
	}

	stage := mintNFTAsset(firstItem, true)
	record, err := i.mintAssetAndResolve(ctx, stage, nil, func(
		record *entities.AssetRecord) bool {

		return isRecord(
			record, entities.AssetTypeNFT, firstItem.Name, 1,
		) && record.AssetRef.IsGroupRef()
	}, opts)
	if err != nil {
		return nil, err
	}

	collections := collectionsFromRecords([]*entities.AssetRecord{record})
	if len(collections) != 1 {
		return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound,
			firstItem.Name)
	}

	assets := assetsFromRecords([]*entities.AssetRecord{record})
	if len(assets) != 1 {
		return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound,
			firstItem.Name)
	}

	return &entities.CollectionMintResult{
		Collection: collections[0],
		FirstItem:  assets[0],
	}, nil
}

func (i *Issuer) mintCollectionItem(ctx context.Context,
	collectionRef entities.AssetRef, item entities.NFTSpec,
	opts ...MintOption) (*entities.Asset, error) {

	if err := validateGroupedRef(collectionRef); err != nil {
		return nil, err
	}
	if err := validateNFTSpec(item); err != nil {
		return nil, err
	}

	mintOpts := applyMintOptions(opts)
	if err := i.ensureReady(ctx); err != nil {
		return nil, err
	}

	collection, err := i.resolveCollection(ctx, collectionRef)
	if err != nil {
		return nil, err
	}

	before, err := i.listMintRecords(ctx, &collection.AssetRef)
	if err != nil {
		return nil, err
	}

	stage := &entities.MintIssuance{
		AssetRef:     collection.AssetRef,
		Name:         item.Name,
		AssetType:    entities.AssetTypeNFT,
		AssetMeta:    item.AssetMeta,
		Amount:       1,
		AssetVersion: item.AssetVersion,
		ScriptKey:    item.ScriptKey,
	}

	err = i.mintAndFinalize(ctx, func() (*entities.MintingBatch,
		error) {

		return i.client.MintIssuance(ctx,
			&entities.MintIssuanceRequest{
				Issuance:      stage,
				ShortResponse: true,
			},
		)
	}, mintOpts)
	if err != nil {
		return nil, err
	}

	record, err := i.findNewRecord(ctx, &collection.AssetRef,
		recordIDs(before), func(record *entities.AssetRecord) bool {
			return isRecord(
				record, entities.AssetTypeNFT, item.Name, 1,
			) && record.AssetRef.Equivalent(collection.AssetRef)
		},
		mintOpts.resolveTimeoutOrDefault(),
	)
	if err != nil {
		return nil, err
	}

	assets := assetsFromRecords([]*entities.AssetRecord{record})
	if len(assets) != 1 {
		return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound,
			item.Name)
	}

	return assets[0], nil
}

func validateFungibleSpec(spec entities.FungibleAssetSpec) error {
	if spec.Name == "" {
		return ErrAssetNameRequired
	}
	if spec.Amount == 0 {
		return ErrZeroAmount
	}

	return nil
}

func validateNFTSpec(spec entities.NFTSpec) error {
	if spec.Name == "" {
		return ErrAssetNameRequired
	}

	return nil
}

func validateGroupedRef(ref entities.AssetRef) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if !ref.IsGroupRef() {
		return ErrAssetNotIssuable
	}

	return nil
}

func mintNFTAsset(spec entities.NFTSpec, collection bool) *entities.MintAsset {
	return &entities.MintAsset{
		AssetVersion:  spec.AssetVersion,
		AssetType:     entities.AssetTypeNFT,
		Name:          spec.Name,
		AssetMeta:     spec.AssetMeta,
		InitialSupply: 1,
		AllowIssuance: collection,
		ScriptKey:     spec.ScriptKey,
	}
}

func (i *Issuer) lock() func() {
	if i == nil || i.mu == nil {
		return func() {}
	}

	i.mu.Lock()
	return i.mu.Unlock
}

type recordMatcher func(*entities.AssetRecord) bool

func (i *Issuer) mintAssetAndResolve(ctx context.Context,
	stage *entities.MintAsset, ref *entities.AssetRef,
	matches recordMatcher,
	opts []MintOption) (*entities.AssetRecord, error) {

	mintOpts := applyMintOptions(opts)
	if err := i.ensureReady(ctx); err != nil {
		return nil, err
	}

	before, err := i.listMintRecords(ctx, ref)
	if err != nil {
		return nil, err
	}

	err = i.mintAndFinalize(ctx, func() (*entities.MintingBatch,
		error) {

		return i.client.MintAsset(ctx, &entities.MintAssetRequest{
			Asset:         stage,
			ShortResponse: true,
		})
	}, mintOpts)
	if err != nil {
		return nil, err
	}

	return i.findNewRecord(
		ctx, ref, recordIDs(before), matches,
		mintOpts.resolveTimeoutOrDefault(),
	)
}

func (i *Issuer) mintAndFinalize(ctx context.Context,
	stage func() (*entities.MintingBatch, error),
	opts *MintOptions) error {

	if i == nil || i.client == nil {
		return fmt.Errorf("issuer client is nil")
	}
	if opts == nil {
		opts = &MintOptions{}
	}

	if _, err := stage(); err != nil {
		return err
	}

	_, err := i.client.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{
			ShortResponse: true,
			FeeRate:       opts.feeRate,
		},
	)
	if err == nil {
		return nil
	}

	i.cancelBatchBestEffort()

	return err
}

func (i *Issuer) cancelBatchBestEffort() {
	ctx, cancel := context.WithTimeout(
		context.Background(), defaultMintCancelTimeout,
	)
	defer cancel()

	_, _ = i.client.CancelBatch(ctx)
}

func (i *Issuer) ensureReady(ctx context.Context) error {
	if i == nil || i.client == nil {
		return fmt.Errorf("issuer client is nil")
	}

	batches, err := i.client.ListBatches(ctx, &entities.ListBatchesRequest{})
	if err != nil {
		return err
	}

	for _, batch := range batches {
		if batch == nil {
			continue
		}
		if isActiveMintBatch(batch.Batch.State) {
			return fmt.Errorf("%w: %s is in state %s",
				ErrMintBatchActive, batch.Batch.BatchKey,
				batch.Batch.State)
		}
	}

	return nil
}

func isActiveMintBatch(state entities.BatchState) bool {
	switch state {
	case entities.BatchStatePending,
		entities.BatchStateFrozen,
		entities.BatchStateCommitted,
		entities.BatchStateBroadcast:

		return true

	default:
		return false
	}
}

func (i *Issuer) resolveFungibleAsset(ctx context.Context,
	ref entities.AssetRef) (*entities.AssetRecord, error) {

	records, err := i.listMintRecords(ctx, &ref)
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if record == nil {
			continue
		}

		if record.Genesis.Type != entities.AssetTypeFungible {
			return nil, fmt.Errorf("%w: expected fungible asset",
				ErrWrongAssetType)
		}
		if !record.AssetRef.IsGroupRef() {
			return nil, ErrAssetNotIssuable
		}

		return record, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrAssetUnknown, ref)
}

func (i *Issuer) resolveCollection(ctx context.Context,
	ref entities.AssetRef) (*entities.Collection, error) {

	records, err := i.listMintRecords(ctx, &ref)
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if record == nil {
			continue
		}

		if record.Genesis.Type != entities.AssetTypeNFT {
			return nil, fmt.Errorf("%w: expected NFT collection",
				ErrWrongAssetType)
		}
		if !record.AssetRef.IsGroupRef() {
			return nil, fmt.Errorf("%w: expected collection ref",
				ErrWrongAssetType)
		}

		collections := collectionsFromRecords(
			[]*entities.AssetRecord{record},
		)
		if len(collections) != 1 {
			return nil, fmt.Errorf("%w: %s", ErrMintResultNotFound,
				ref)
		}

		return collections[0], nil
	}

	return nil, fmt.Errorf("%w: %s", ErrAssetUnknown, ref)
}

func (i *Issuer) listMintRecords(ctx context.Context,
	ref *entities.AssetRef) ([]*entities.AssetRecord, error) {

	if i == nil || i.client == nil {
		return nil, fmt.Errorf("issuer client is nil")
	}

	req := &entities.ListAssetsRequest{
		IncludeUnconfirmedMints: true,
		AssetRef:                ref,
	}

	return i.client.ListAssetRecords(ctx, req)
}

func (i *Issuer) findNewRecord(ctx context.Context,
	ref *entities.AssetRef, before map[entities.AssetID]struct{},
	matches recordMatcher, timeout time.Duration) (*entities.AssetRecord,
	error) {

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(defaultMintResolveInterval)
	defer ticker.Stop()

	for {
		record, err := i.findNewRecordOnce(ctx, ref, before, matches)
		if err == nil {
			return record, nil
		}
		if !errors.Is(err, errMintResultNotReady) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-deadline.C:
			return nil, fmt.Errorf("%w: %v",
				ErrMintResolveTimeout, err)

		case <-ticker.C:
		}
	}
}

func (i *Issuer) findNewRecordOnce(ctx context.Context,
	ref *entities.AssetRef, before map[entities.AssetID]struct{},
	matches recordMatcher) (*entities.AssetRecord, error) {

	records, err := i.listMintRecords(ctx, ref)
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if record == nil {
			continue
		}

		if _, ok := before[record.Genesis.IssuanceID]; ok {
			continue
		}
		if matches == nil || matches(record) {
			return record, nil
		}
	}

	return nil, errMintResultNotReady
}

func recordIDs(records []*entities.AssetRecord) map[entities.AssetID]struct{} {
	ids := make(map[entities.AssetID]struct{}, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}

		ids[record.Genesis.IssuanceID] = struct{}{}
	}

	return ids
}

func isRecord(record *entities.AssetRecord, assetType entities.AssetType,
	name string, amount uint64) bool {

	if record == nil {
		return false
	}

	return record.Genesis.Type == assetType &&
		record.Genesis.Tag == name &&
		record.Amount == amount
}

func applyMintOptions(opts []MintOption) *MintOptions {
	o := &MintOptions{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		opt(o)
	}

	return o
}

func (o *MintOptions) resolveTimeoutOrDefault() time.Duration {
	if o == nil || o.resolveTimeout <= 0 {
		return defaultMintResolveTimeout
	}

	return o.resolveTimeout
}
