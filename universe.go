package tapsdk

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/lightninglabs/tap-sdk/entities"
)

// Universe is the high-level AssetRef-first universe surface.
//
// It exposes common asset discovery, proof lookup, and sync operations without
// requiring application code to build protocol-shaped UniverseID values.
// UniverseClient remains available for callers that need direct access to the
// tapd universe RPC model.
type Universe struct {
	client UniverseClient
}

// NewUniverse creates a high-level universe facade backed by the given client.
func NewUniverse(client UniverseClient) *Universe {
	return &Universe{
		client: client,
	}
}

// NewUniverse returns a high-level universe facade backed by the wallet's
// client.
func (s *Wallet) NewUniverse() *Universe {
	return NewUniverse(s.client)
}

// UniverseProofOption configures high-level proof queries.
type UniverseProofOption func(*universeProofOptions)

type universeProofOptions struct {
	proofType    entities.ProofType
	proofTypeSet bool
	offset       int32
	limit        int32
	pageSet      bool
	direction    entities.SortDirection
}

// WithUniverseProofType limits a proof query to issuance or transfer proofs.
// The default queries all locally known proof types for the AssetRef.
func WithUniverseProofType(proofType entities.ProofType) UniverseProofOption {
	return func(o *universeProofOptions) {
		o.proofType = proofType
		o.proofTypeSet = true
	}
}

// WithUniverseProofPage sets the offset and limit for ListProofs. Pagination
// requires WithUniverseProofType so one page maps to one proof stream.
func WithUniverseProofPage(offset, limit int32) UniverseProofOption {
	return func(o *universeProofOptions) {
		o.offset = offset
		o.limit = limit
		o.pageSet = true
	}
}

// WithUniverseProofDirection sets the leaf-key sort direction for ListProofs.
// The default is descending.
func WithUniverseProofDirection(
	direction entities.SortDirection) UniverseProofOption {

	return func(o *universeProofOptions) {
		o.direction = direction
	}
}

// UniverseSyncOption configures high-level universe sync calls.
type UniverseSyncOption func(*universeSyncOptions)

type universeSyncOptions struct {
	mode entities.UniverseSyncMode
}

// WithUniverseSyncMode sets the tapd sync scope. The default is SyncFull.
func WithUniverseSyncMode(mode entities.UniverseSyncMode) UniverseSyncOption {
	return func(o *universeSyncOptions) {
		o.mode = mode
	}
}

// HasAsset reports whether the local universe knows issuance or transfer roots
// for the AssetRef. It returns false with a nil error only when tapd
// successfully reports no roots; RPC and authentication failures are returned
// as errors.
func (u *Universe) HasAsset(ctx context.Context,
	ref entities.AssetRef) (bool, error) {

	ok, err := u.hasAsset(ctx, ref)
	if err != nil {
		return false, wrapErr("HasAsset", err)
	}

	return ok, nil
}

// GetRoots returns the locally known universe roots for the AssetRef.
func (u *Universe) GetRoots(ctx context.Context,
	ref entities.AssetRef) (*entities.UniverseRoots, error) {

	roots, err := u.getRoots(ctx, ref)
	if err != nil {
		return nil, wrapErr("GetRoots", err)
	}

	return roots, nil
}

// ListProofs returns locally known universe proofs for the AssetRef. By
// default it returns all locally known proof types. Use WithUniverseProofType
// to request only issuance or transfer proofs. The method returns one tapd
// page per queried proof type, not a full auto-paginated scan.
func (u *Universe) ListProofs(ctx context.Context,
	ref entities.AssetRef,
	opts ...UniverseProofOption) ([]*entities.UniverseProof, error) {

	proofs, err := u.listProofs(ctx, ref, opts...)
	if err != nil {
		return nil, wrapErr("ListProofs", err)
	}

	return proofs, nil
}

// GetProof returns one universe proof by AssetRef and leaf key. By default it
// tries all locally known proof types. Use WithUniverseProofType when the proof
// type is already known.
func (u *Universe) GetProof(ctx context.Context, ref entities.AssetRef,
	leafKey entities.AssetLeafKey,
	opts ...UniverseProofOption) (*entities.UniverseProof, error) {

	proof, err := u.getProof(ctx, ref, leafKey, opts...)
	if err != nil {
		return nil, wrapErr("GetProof", err)
	}

	return proof, nil
}

// SyncAsset syncs universe roots and leaves for one AssetRef from a trusted
// remote universe server. The host is forwarded to tapd, which currently dials
// remote universe servers without certificate verification, so it must come
// from trusted configuration and not direct user input.
func (u *Universe) SyncAsset(ctx context.Context, ref entities.AssetRef,
	host string, opts ...UniverseSyncOption) (*entities.UniverseSyncResult,
	error) {

	results, err := u.syncAssets(ctx, []entities.AssetRef{ref}, host, opts...)
	if err != nil {
		return nil, wrapErr("SyncAsset", err)
	}

	return results[0], nil
}

// SyncAssets syncs universe roots and leaves for multiple AssetRefs from a
// trusted remote universe server. The host is forwarded to tapd, which
// currently dials remote universe servers without certificate verification, so
// it must come from trusted configuration and not direct user input.
func (u *Universe) SyncAssets(ctx context.Context, refs []entities.AssetRef,
	host string, opts ...UniverseSyncOption) ([]*entities.UniverseSyncResult,
	error) {

	results, err := u.syncAssets(ctx, refs, host, opts...)
	if err != nil {
		return nil, wrapErr("SyncAssets", err)
	}

	return results, nil
}

func (u *Universe) hasAsset(ctx context.Context,
	ref entities.AssetRef) (bool, error) {

	roots, err := u.getRoots(ctx, ref)
	if errors.Is(err, ErrAssetUnknown) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return roots.HasRoots(), nil
}

func (u *Universe) getRoots(ctx context.Context,
	ref entities.AssetRef) (*entities.UniverseRoots, error) {

	if err := validateUniverseAssetRef(ref); err != nil {
		return nil, err
	}

	roots, err := u.client.QueryAssetRoots(
		ctx, universeID(ref, entities.ProofTypeIssuance),
	)
	if err != nil {
		return nil, err
	}

	result := &entities.UniverseRoots{
		AssetRef: ref,
	}
	if roots != nil {
		result.IssuanceRoot = nonEmptyUniverseRoot(roots.IssuanceRoot)
		result.TransferRoot = nonEmptyUniverseRoot(roots.TransferRoot)
	}
	if !result.HasRoots() {
		return nil, fmt.Errorf("%w: %s", ErrAssetUnknown, ref)
	}

	return result, nil
}

func (u *Universe) listProofs(ctx context.Context, ref entities.AssetRef,
	opts ...UniverseProofOption) ([]*entities.UniverseProof, error) {

	options := applyUniverseProofOptions(opts)
	if err := validateUniverseListProofOptions(options); err != nil {
		return nil, err
	}

	roots, err := u.getRoots(ctx, ref)
	if err != nil {
		return nil, err
	}

	proofTypes, err := proofTypesForRoots(roots, options.proofType)
	if err != nil {
		return nil, err
	}

	var proofs []*entities.UniverseProof
	for _, proofType := range proofTypes {
		keys, err := u.client.AssetLeafKeys(
			ctx, &entities.AssetLeafKeysRequest{
				ID:        *universeID(ref, proofType),
				Offset:    options.offset,
				Limit:     options.limit,
				Direction: options.direction,
			},
		)
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			proof, err := u.queryProof(ctx, ref, proofType, key)
			if err != nil {
				return nil, err
			}

			proofs = append(proofs, proof)
		}
	}
	if len(proofs) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoProofs, ref)
	}

	return proofs, nil
}

func (u *Universe) getProof(ctx context.Context, ref entities.AssetRef,
	leafKey entities.AssetLeafKey,
	opts ...UniverseProofOption) (*entities.UniverseProof, error) {

	options := applyUniverseProofOptions(opts)
	if err := validateUniverseProofType(options); err != nil {
		return nil, err
	}

	roots, err := u.getRoots(ctx, ref)
	if err != nil {
		return nil, err
	}

	proofTypes, err := proofTypesForRoots(roots, options.proofType)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, proofType := range proofTypes {
		proof, err := u.queryProof(ctx, ref, proofType, leafKey)
		if err == nil {
			return proof, nil
		}

		normalized := normalizeErr("GetProof", err)
		if !errors.Is(normalized, ErrProofNotFound) &&
			!errors.Is(normalized, ErrAssetUnknown) {

			return nil, normalized
		}

		lastErr = normalized
	}
	if lastErr != nil {
		if errors.Is(lastErr, ErrAssetUnknown) {
			return nil, lastErr
		}

		return nil, fmt.Errorf("%w: %w", ErrProofNotFound, lastErr)
	}

	return nil, fmt.Errorf("%w: %s", ErrProofNotFound, ref)
}

func (u *Universe) queryProof(ctx context.Context, ref entities.AssetRef,
	proofType entities.ProofType,
	leafKey entities.AssetLeafKey) (*entities.UniverseProof, error) {

	resp, err := u.client.QueryProof(ctx, &entities.UniverseKey{
		ID:      *universeID(ref, proofType),
		LeafKey: leafKey,
	})
	if err != nil {
		return nil, err
	}

	return universeProofFromResponse(ref, proofType, leafKey, resp), nil
}

func (u *Universe) syncAssets(ctx context.Context, refs []entities.AssetRef,
	host string, opts ...UniverseSyncOption) ([]*entities.UniverseSyncResult,
	error) {

	if len(refs) == 0 {
		return nil, ErrNoAssetRef
	}
	for _, ref := range refs {
		if err := validateUniverseAssetRef(ref); err != nil {
			return nil, err
		}
	}
	if err := validateDistinctAssetRefs(refs); err != nil {
		return nil, err
	}
	if err := validateUniverseSyncHost(host); err != nil {
		return nil, err
	}

	options := applyUniverseSyncOptions(opts)
	results := make([]*entities.UniverseSyncResult, 0, len(refs))
	targets := make([]entities.SyncTarget, 0, len(refs)*2)

	for _, ref := range refs {
		result := &entities.UniverseSyncResult{AssetRef: ref}
		results = append(results, result)

		for _, proofType := range syncProofTypes(options.mode) {
			targets = append(targets, entities.SyncTarget{
				ID: *universeID(ref, proofType),
			})
		}
	}

	diffs, err := u.client.SyncUniverse(ctx, &entities.SyncRequest{
		UniverseHost: host,
		SyncMode:     options.mode,
		SyncTargets:  targets,
	})
	if err != nil {
		return nil, err
	}

	for i := range diffs {
		diff := diffs[i]
		ref, proofType, ok := syncedUniverseID(&diff)
		if !ok {
			continue
		}

		result := findUniverseSyncResult(results, ref)
		if result == nil {
			continue
		}

		switch proofType {
		case entities.ProofTypeIssuance:
			result.Issuance = &diff

		case entities.ProofTypeTransfer:
			result.Transfer = &diff
		}
	}

	return results, nil
}

func findUniverseSyncResult(results []*entities.UniverseSyncResult,
	ref entities.AssetRef) *entities.UniverseSyncResult {

	for _, result := range results {
		if result.AssetRef.Equivalent(ref) {
			return result
		}
	}

	return nil
}

func applyUniverseProofOptions(
	opts []UniverseProofOption) universeProofOptions {

	options := universeProofOptions{
		proofType: entities.ProofTypeUnspecified,
	}
	for _, opt := range opts {
		opt(&options)
	}

	return options
}

func applyUniverseSyncOptions(opts []UniverseSyncOption) universeSyncOptions {
	options := universeSyncOptions{
		mode: entities.SyncFull,
	}
	for _, opt := range opts {
		opt(&options)
	}

	return options
}

func validateUniverseAssetRef(ref entities.AssetRef) error {
	if ref.IsZero() {
		return ErrNoAssetRef
	}
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAssetRef, err)
	}

	return nil
}

func validateDistinctAssetRefs(refs []entities.AssetRef) error {
	for i := range refs {
		for j := i + 1; j < len(refs); j++ {
			if !refs[i].Equivalent(refs[j]) {
				continue
			}

			return fmt.Errorf(
				"%w: %s", ErrDuplicateAssetRef, refs[i],
			)
		}
	}

	return nil
}

func validateUniverseSyncHost(host string) error {
	if host == "" {
		return ErrUniverseHostRequired
	}
	if strings.ContainsAny(host, "\r\n\x00 \t/@") {
		return fmt.Errorf(
			"%w: host must be a host:port address",
			ErrInvalidUniverseHost,
		)
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		return fmt.Errorf(
			"%w: host must be a host:port address",
			ErrInvalidUniverseHost,
		)
	}

	return nil
}

func universeID(ref entities.AssetRef,
	proofType entities.ProofType) *entities.UniverseID {

	id := entities.UniverseIDFromRef(ref, proofType)
	return &id
}

func nonEmptyUniverseRoot(root *entities.UniverseRoot) *entities.UniverseRoot {
	if root == nil {
		return nil
	}
	if root.ID.AssetRef.IsZero() && root.MSSMTRoot == nil {
		return nil
	}

	return root
}

func proofTypesForRoots(roots *entities.UniverseRoots,
	proofType entities.ProofType) ([]entities.ProofType, error) {

	switch proofType {
	case entities.ProofTypeIssuance:
		if roots.IssuanceRoot == nil {
			return nil, ErrNoProofs
		}

		return []entities.ProofType{entities.ProofTypeIssuance}, nil

	case entities.ProofTypeTransfer:
		if roots.TransferRoot == nil {
			return nil, ErrNoProofs
		}

		return []entities.ProofType{entities.ProofTypeTransfer}, nil

	case entities.ProofTypeUnspecified:
		var proofTypes []entities.ProofType
		if roots.IssuanceRoot != nil {
			proofTypes = append(
				proofTypes, entities.ProofTypeIssuance,
			)
		}
		if roots.TransferRoot != nil {
			proofTypes = append(
				proofTypes, entities.ProofTypeTransfer,
			)
		}
		if len(proofTypes) == 0 {
			return nil, ErrNoProofs
		}

		return proofTypes, nil

	default:
		return nil, fmt.Errorf("unknown proof type: %d", proofType)
	}
}

func validateUniverseListProofOptions(options universeProofOptions) error {
	if err := validateUniverseProofType(options); err != nil {
		return err
	}
	if options.offset < 0 || options.limit < 0 {
		return ErrInvalidPagination
	}
	if options.pageSet && options.proofType == entities.ProofTypeUnspecified {
		return ErrUniverseProofTypeRequired
	}

	return nil
}

func validateUniverseProofType(options universeProofOptions) error {
	if options.proofTypeSet &&
		options.proofType == entities.ProofTypeUnspecified {

		return ErrUniverseProofTypeRequired
	}

	return nil
}

func syncProofTypes(mode entities.UniverseSyncMode) []entities.ProofType {
	if mode == entities.SyncIssuanceOnly {
		return []entities.ProofType{entities.ProofTypeIssuance}
	}

	return []entities.ProofType{
		entities.ProofTypeIssuance,
		entities.ProofTypeTransfer,
	}
}

func universeProofFromResponse(ref entities.AssetRef,
	proofType entities.ProofType, leafKey entities.AssetLeafKey,
	resp *entities.AssetProofResponse) *entities.UniverseProof {

	proof := &entities.UniverseProof{
		AssetRef:  ref,
		ProofType: proofType,
		LeafKey:   leafKey,
	}
	if resp == nil {
		return proof
	}

	proof.UniverseRoot = resp.UniverseRoot
	proof.UniverseInclusionProof = resp.UniverseInclusionProof
	proof.MultiverseRoot = resp.MultiverseRoot
	proof.MultiverseInclusionProof = resp.MultiverseInclusionProof

	if resp.AssetLeaf != nil {
		proof.Asset = resp.AssetLeaf.Asset
		proof.Proof = resp.AssetLeaf.Proof
	}

	return proof
}

func syncedUniverseID(sync *entities.SyncedUniverse) (
	entities.AssetRef, entities.ProofType, bool) {

	root := sync.NewAssetRoot
	if root == nil {
		root = sync.OldAssetRoot
	}
	if root == nil {
		return "", entities.ProofTypeUnspecified, false
	}

	return root.ID.AssetRef, root.ID.ProofType, true
}
