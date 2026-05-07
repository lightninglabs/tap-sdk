package tapsdk

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/lightninglabs/tap-sdk/entities"
)

type ownershipOptions struct {
	challenge               []byte
	challengeSet            bool
	amount                  uint64
	hasAmount               bool
	allOwnedCollectionItems bool
}

// OwnershipOption configures Wallet ownership proof calls.
type OwnershipOption func(*ownershipOptions)

// WithOwnershipChallenge binds an ownership proof to a 32-byte challenge. The
// same challenge must be supplied when verifying the proof.
func WithOwnershipChallenge(challenge []byte) OwnershipOption {
	return func(o *ownershipOptions) {
		o.challengeSet = true
		if len(challenge) == 32 {
			o.challenge = append([]byte(nil), challenge...)
			return
		}

		o.challenge = challenge
	}
}

// WithOwnershipAmount asks ProveOwnership to return enough fungible output
// proofs to cover at least the requested amount.
func WithOwnershipAmount(amount uint64) OwnershipOption {
	return func(o *ownershipOptions) {
		o.amount = amount
		o.hasAmount = true
	}
}

// WithAllOwnedCollectionItems asks ProveOwnership to return a proof for every
// wallet-owned NFT item in the requested collection. Without this option, a
// collection AssetRef proves ownership of one wallet-owned collection item.
func WithAllOwnedCollectionItems() OwnershipOption {
	return func(o *ownershipOptions) {
		o.allOwnedCollectionItems = true
	}
}

func applyOwnershipOptions(opts []OwnershipOption) *ownershipOptions {
	o := &ownershipOptions{}
	for _, opt := range opts {
		opt(o)
	}

	return o
}

func validateOwnershipChallenge(challenge []byte, explicit bool) error {
	if !explicit && len(challenge) == 0 {
		return nil
	}

	if explicit && len(challenge) == 0 {
		return fmt.Errorf(
			"%w: challenge must be 32 non-zero bytes",
			ErrInvalidChallenge,
		)
	}

	if err := entities.ValidateOwnershipChallenge(challenge); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}

	return nil
}

// ProveOwnership proves wallet ownership of the asset identified by AssetRef.
//
// Fungible refs must include WithOwnershipAmount, and are resolved to the owned
// issuance outputs needed by tapd's low-level ownership RPC. NFT item refs
// prove that specific item. A collection ref proves one owned item by default,
// or all owned items when WithAllOwnedCollectionItems is set.
func (s *Wallet) ProveOwnership(ctx context.Context, ref entities.AssetRef,
	opts ...OwnershipOption) (*entities.OwnershipProofSet, error) {

	if err := ref.Validate(); err != nil {
		return nil, wrapErr("ProveOwnership", err)
	}

	o := applyOwnershipOptions(opts)
	if err := validateOwnershipChallenge(
		o.challenge, o.challengeSet,
	); err != nil {
		return nil, wrapErr("ProveOwnership", err)
	}
	if o.hasAmount && o.amount == 0 {
		return nil, wrapErr("ProveOwnership", ErrZeroAmount)
	}

	candidates, err := s.ownershipCandidates(ctx, ref)
	if err != nil {
		return nil, wrapErr("ProveOwnership", err)
	}
	if len(candidates) == 0 {
		return nil, s.ownershipNoCandidatesErr(ctx, ref)
	}

	selected, err := selectOwnershipCandidates(ref, candidates, o)
	if err != nil {
		return nil, wrapErr("ProveOwnership", err)
	}

	result := &entities.OwnershipProofSet{
		AssetRef: ref,
		Proofs:   make([]entities.OwnershipProof, 0, len(selected)),
	}

	for _, candidate := range selected {
		issuanceID := candidate.asset.Genesis.IssuanceID
		proof, err := s.client.ProveAssetOwnership(
			ctx, &entities.ProveOwnershipRequest{
				AssetRef:  entities.AssetRefFromAssetID(issuanceID),
				ScriptKey: candidate.asset.ScriptKey.PubKey,
				Outpoint:  candidate.outpoint,
				Challenge: o.challenge,
			},
		)
		if err != nil {
			return nil, wrapErr("ProveOwnership", err)
		}
		if proof == nil || len(proof.ProofWithWitness) == 0 {
			return nil, wrapErr("ProveOwnership", ErrNoProofs)
		}

		result.Proofs = append(result.Proofs, entities.OwnershipProof{
			AssetRef:         ownershipProofAssetRef(ref, candidate.asset),
			IssuanceID:       issuanceID,
			ScriptKey:        candidate.asset.ScriptKey.PubKey,
			Outpoint:         candidate.outpoint,
			Amount:           candidate.asset.Amount,
			ProofWithWitness: proof.ProofWithWitness,
		})
	}

	return result, nil
}

// VerifyOwnership verifies an ownership proof produced by ProveOwnership or
// by another Taproot Assets wallet.
func (s *Wallet) VerifyOwnership(ctx context.Context, proof []byte,
	opts ...OwnershipOption) (*entities.VerifyOwnershipResponse, error) {

	if len(proof) == 0 {
		return nil, wrapErr("VerifyOwnership", ErrOwnershipProofRequired)
	}

	o := applyOwnershipOptions(opts)
	if o.hasAmount || o.allOwnedCollectionItems {
		return nil, wrapErr("VerifyOwnership", ErrWrongAssetType)
	}
	if err := validateOwnershipChallenge(
		o.challenge, o.challengeSet,
	); err != nil {
		return nil, wrapErr("VerifyOwnership", err)
	}

	resp, err := s.client.VerifyAssetOwnership(
		ctx, &entities.VerifyOwnershipRequest{
			ProofWithWitness: proof,
			Challenge:        o.challenge,
		},
	)
	if err != nil {
		return nil, wrapErr("VerifyOwnership", err)
	}
	if resp == nil || !resp.Valid {
		return nil, wrapErr("VerifyOwnership", ErrOwnershipProofInvalid)
	}

	return resp, nil
}

type ownershipCandidate struct {
	asset    *entities.AssetRecord
	outpoint entities.Outpoint
}

func (s *Wallet) ownershipCandidates(ctx context.Context,
	ref entities.AssetRef) ([]ownershipCandidate, error) {

	utxos, err := s.client.ListUtxos(ctx, &entities.ListUtxosRequest{
		IncludeLeased: true,
	})
	if err != nil {
		return nil, err
	}

	candidates := make([]ownershipCandidate, 0)
	for _, utxo := range utxos {
		if utxo == nil {
			continue
		}

		for _, asset := range utxo.Assets {
			if asset == nil || !ownershipRecordMatchesRef(asset, ref) {
				continue
			}

			candidates = append(candidates, ownershipCandidate{
				asset:    asset,
				outpoint: utxo.OutPoint,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if cmp := bytes.Compare(
			left.outpoint.Txid[:], right.outpoint.Txid[:],
		); cmp != 0 {
			return cmp < 0
		}
		if left.outpoint.Index != right.outpoint.Index {
			return left.outpoint.Index < right.outpoint.Index
		}

		return bytes.Compare(
			left.asset.Genesis.IssuanceID[:],
			right.asset.Genesis.IssuanceID[:],
		) < 0
	})

	return candidates, nil
}

func (s *Wallet) ownershipNoCandidatesErr(ctx context.Context,
	ref entities.AssetRef) error {

	known, err := s.ownershipRefKnown(ctx, ref)
	if err != nil {
		return wrapErr("ProveOwnership", err)
	}
	if known {
		return wrapErr("ProveOwnership", fmt.Errorf(
			"%w: %s", ErrInsufficientBalance, ref,
		))
	}

	return wrapErr("ProveOwnership", fmt.Errorf(
		"%w: %s", ErrAssetUnknown, ref,
	))
}

func (s *Wallet) ownershipRefKnown(ctx context.Context,
	ref entities.AssetRef) (bool, error) {

	allTypes := &entities.ScriptKeyTypeQuery{AllTypes: true}
	records, err := s.listAssetRecords(ctx, &entities.ListAssetsRequest{
		AssetRef:      &ref,
		IncludeSpent:  true,
		ScriptKeyType: allTypes,
	})
	if err != nil {
		return false, err
	}

	return len(records) > 0, nil
}

func selectOwnershipCandidates(ref entities.AssetRef,
	candidates []ownershipCandidate,
	o *ownershipOptions) ([]ownershipCandidate, error) {

	collectionRef := ref.IsGroupRef() &&
		candidates[0].asset.Genesis.Type == entities.AssetTypeCollectible

	if err := validateOwnershipCandidateTypes(
		ref, candidates, collectionRef,
	); err != nil {
		return nil, err
	}

	if collectionRef {
		if o.hasAmount {
			return nil, ErrWrongAssetType
		}
		if o.allOwnedCollectionItems {
			return candidates, nil
		}

		return candidates[:1], nil
	}

	if o.allOwnedCollectionItems {
		return nil, ErrWrongAssetType
	}

	if candidates[0].asset.Genesis.Type == entities.AssetTypeCollectible {
		if o.hasAmount && o.amount > 1 {
			return nil, fmt.Errorf(
				"%w: NFT ownership proves one item",
				ErrInsufficientBalance,
			)
		}

		return candidates[:1], nil
	}

	if !o.hasAmount {
		return nil, ErrOwnershipAmountRequired
	}

	var total uint64
	for idx, candidate := range candidates {
		total = addSaturatingUint64(total, candidate.asset.Amount)
		if total >= o.amount {
			return candidates[:idx+1], nil
		}
	}

	return nil, fmt.Errorf(
		"%w: owned amount %d below requested %d",
		ErrInsufficientBalance, total, o.amount,
	)
}

func validateOwnershipCandidateTypes(ref entities.AssetRef,
	candidates []ownershipCandidate, collectionRef bool) error {

	firstType := candidates[0].asset.Genesis.Type
	for _, candidate := range candidates[1:] {
		if candidate.asset.Genesis.Type != firstType {
			return fmt.Errorf(
				"%w: asset ref resolves to multiple asset types",
				ErrWrongAssetType,
			)
		}
	}

	if collectionRef || !ref.IsGroupRef() {
		return nil
	}

	if firstType != entities.AssetTypeNormal {
		return fmt.Errorf(
			"%w: group ref is not a fungible asset or collection",
			ErrWrongAssetType,
		)
	}

	return nil
}

func ownershipRecordMatchesRef(asset *entities.AssetRecord,
	ref entities.AssetRef) bool {

	if ref.IsGroupRef() {
		return asset.AssetRef.Equivalent(ref)
	}

	assetID, ok := ref.AssetID()
	if !ok {
		return false
	}

	return asset.Genesis.IssuanceID == assetID
}

func ownershipProofAssetRef(ref entities.AssetRef,
	asset *entities.AssetRecord) entities.AssetRef {

	if asset.Genesis.Type == entities.AssetTypeCollectible {
		return entities.AssetRefFromAssetID(asset.Genesis.IssuanceID)
	}

	if ref.IsGroupRef() {
		return ref
	}

	return entities.AssetRefFromAssetID(asset.Genesis.IssuanceID)
}
