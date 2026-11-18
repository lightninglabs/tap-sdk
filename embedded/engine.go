package embedded

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightningnetwork/lnd/keychain"
)

// maxStagedProofs bounds the proofs InsertProof holds for a later
// RegisterTransfer.
const maxStagedProofs = 4096

// engine holds the state shared by the embedded services.
type engine struct {
	params address.ChainParams
	store  Store
	keys   KeyRing
	signer Signer
	chain  Chain
	log    Logger

	// keyMu serializes key derivation with the store insert that makes
	// the key known.
	keyMu sync.Mutex

	groupMu   sync.RWMutex
	groupKeys map[asset.SerializedKey]struct{}

	// heldGroupsLoaded is set once the groups of the assets the wallet
	// holds are in groupKeys.
	heldGroupsLoaded bool

	stagedMu sync.Mutex
	staged   map[ProofLocator][]byte

	// transferMu serializes writes of transfers and their proofs.
	transferMu sync.Mutex

	trackMu  sync.Mutex
	tracking map[chainhash.Hash]context.CancelFunc
	wg       sync.WaitGroup
	quit     chan struct{}
	stopOnce sync.Once
}

// newEngine builds the engine for a validated configuration.
func newEngine(cfg *Config) (*engine, error) {
	params, err := chainParams(cfg.Network)
	if err != nil {
		return nil, err
	}

	logger := cfg.Logger
	if logger == nil {
		logger = nopLogger{}
	}

	e := &engine{
		params:    params,
		store:     cfg.Store,
		keys:      cfg.KeyRing,
		signer:    cfg.Signer,
		chain:     cfg.Chain,
		log:       logger,
		groupKeys: make(map[asset.SerializedKey]struct{}),
		staged:    make(map[ProofLocator][]byte),
		tracking:  make(map[chainhash.Hash]context.CancelFunc),
		quit:      make(chan struct{}),
	}
	for _, groupKey := range cfg.GroupKeys {
		e.groupKeys[asset.SerializedKey(groupKey)] = struct{}{}
	}

	return e, nil
}

// chainParams maps an SDK network to tapd's chain parameters.
func chainParams(network tapsdk.Network) (address.ChainParams, error) {
	switch network {
	case tapsdk.NetworkMainnet:
		return address.MainNetTap, nil

	case tapsdk.NetworkTestnet:
		return address.TestNet3Tap, nil

	case tapsdk.NetworkTestnet4:
		return address.TestNet4Tap, nil

	case tapsdk.NetworkRegtest:
		return address.RegressionNetTap, nil

	case tapsdk.NetworkSignet:
		return address.SigNetTap, nil

	case tapsdk.NetworkSimnet:
		// lnd derives simnet keys under the testnet coin type.
		simNet := address.SimNetTap
		simNet.HDCoinType = address.TestNet3Tap.HDCoinType

		return simNet, nil

	default:
		return address.ChainParams{}, fmt.Errorf("unsupported "+
			"network %q", network)
	}
}

// trustGroupKey adds a group key to the trusted set.
func (e *engine) trustGroupKey(groupKey tapsdk.PubKey) {
	e.groupMu.Lock()
	defer e.groupMu.Unlock()

	e.groupKeys[asset.SerializedKey(groupKey)] = struct{}{}
}

// isGroupTrusted reports whether a group key is in the trusted set.
func (e *engine) isGroupTrusted(groupKey asset.SerializedKey) bool {
	e.groupMu.RLock()
	defer e.groupMu.RUnlock()

	_, ok := e.groupKeys[groupKey]

	return ok
}

// trustHeldGroups trusts the group of every asset output the wallet holds.
// The store is read once, since an asset received later is of a group that
// was trusted when its proof verified.
func (e *engine) trustHeldGroups(ctx context.Context) error {
	e.groupMu.RLock()
	loaded := e.heldGroupsLoaded
	e.groupMu.RUnlock()

	if loaded {
		return nil
	}

	records, err := e.store.ListOwnedProofs(ctx, true)
	if err != nil {
		return fmt.Errorf("list owned assets: %w", err)
	}

	var held []asset.SerializedKey
	for idx := range records {
		file, err := proof.DecodeFile(records[idx].ProofFile)
		if err != nil {
			return fmt.Errorf("decode proof of %v: %w",
				records[idx].Outpoint, err)
		}
		tip, err := file.LastProof()
		if err != nil {
			return fmt.Errorf("decode proof of %v: %w",
				records[idx].Outpoint, err)
		}
		if tip.Asset.GroupKey == nil {
			continue
		}

		held = append(held, asset.ToSerialized(
			&tip.Asset.GroupKey.GroupPubKey,
		))
	}

	e.groupMu.Lock()
	defer e.groupMu.Unlock()

	for _, groupKey := range held {
		e.groupKeys[groupKey] = struct{}{}
	}
	e.heldGroupsLoaded = true

	return nil
}

// verifyGroupKey accepts the group keys the wallet trusts: the configured
// and added ones, and the group of every asset the wallet holds, whose proof
// verified when it was received.
func (e *engine) verifyGroupKey(ctx context.Context,
	groupKey *btcec.PublicKey) error {

	serialized := asset.ToSerialized(groupKey)
	if e.isGroupTrusted(serialized) {
		return nil
	}

	if err := e.trustHeldGroups(ctx); err != nil {
		return err
	}
	if e.isGroupTrusted(serialized) {
		return nil
	}

	return fmt.Errorf("asset group %x is not trusted by this wallet",
		serialized[:])
}

// verifyHeader checks that a block header is the best-chain header at its
// claimed height.
func (e *engine) verifyHeader(ctx context.Context, header wire.BlockHeader,
	height uint32) error {

	known, err := e.chain.BlockHeader(ctx, height)
	if err != nil {
		return fmt.Errorf("fetch block header at height %d: %w", height,
			err)
	}
	if known == nil || known.BlockHash() != header.BlockHash() {
		return fmt.Errorf("block %v is not on the best chain at "+
			"height %d", header.BlockHash(), height)
	}

	return nil
}

// verifierCtx returns the context proofs are verified in.
func (e *engine) verifierCtx(ctx context.Context) proof.VerifierCtx {
	return proof.VerifierCtx{
		HeaderVerifier: func(header wire.BlockHeader,
			height uint32) error {

			return e.verifyHeader(ctx, header, height)
		},
		MerkleVerifier: proof.DefaultMerkleVerifier,
		GroupVerifier: func(groupKey *btcec.PublicKey) error {
			return e.verifyGroupKey(ctx, groupKey)
		},
		ChainLookupGen: &chainLookupGen{chain: e.chain},
	}
}

// IsLocalKey reports whether the wallet derived the key. It makes the engine
// the key lookup tapd's marshalling asks for local keys.
func (e *engine) IsLocalKey(ctx context.Context,
	desc keychain.KeyDescriptor) bool {

	if desc.PubKey == nil {
		return false
	}

	_, err := e.store.FetchInternalKey(
		ctx, asset.ToSerialized(desc.PubKey),
	)

	return err == nil
}

// fetchKeyDescriptor returns the descriptor of a key the wallet derived.
func (e *engine) fetchKeyDescriptor(ctx context.Context,
	pubKey *btcec.PublicKey) (keychain.KeyDescriptor, error) {

	record, err := e.store.FetchInternalKey(
		ctx, asset.ToSerialized(pubKey),
	)
	if err != nil {
		return keychain.KeyDescriptor{}, err
	}

	return keyDescriptor(*record)
}

// keyDescriptor converts a stored key to a key descriptor.
func keyDescriptor(record KeyRecord) (keychain.KeyDescriptor, error) {
	pubKey, err := btcec.ParsePubKey(record.PubKey[:])
	if err != nil {
		return keychain.KeyDescriptor{}, fmt.Errorf("parse stored "+
			"key: %w", err)
	}

	return keychain.KeyDescriptor{
		PubKey: pubKey,
		KeyLocator: keychain.KeyLocator{
			Family: keychain.KeyFamily(record.Family),
			Index:  record.Index,
		},
	}, nil
}

// keyRecord converts a key descriptor to a stored key.
func keyRecord(desc keychain.KeyDescriptor) KeyRecord {
	return KeyRecord{
		PubKey: asset.ToSerialized(desc.PubKey),
		Family: uint32(desc.Family),
		Index:  desc.Index,
	}
}

// fetchScriptKey returns the full script key of a tweaked key the wallet
// knows, or address.ErrScriptKeyNotFound.
func (e *engine) fetchScriptKey(ctx context.Context,
	tweakedKey *btcec.PublicKey) (*asset.TweakedScriptKey, error) {

	record, err := e.store.FetchScriptKey(ctx, scriptKeyBytes(tweakedKey))
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, address.ErrScriptKeyNotFound

	case err != nil:
		return nil, err
	}

	rawKey, err := keyDescriptor(record.RawKey)
	if err != nil {
		return nil, err
	}

	return &asset.TweakedScriptKey{
		RawKey: rawKey,
		Tweak:  record.Tweak,
		Type:   asset.ScriptKeyType(record.Type),
	}, nil
}

// insertScriptKey records a script key under its tweaked key.
func (e *engine) insertScriptKey(ctx context.Context,
	scriptKey asset.ScriptKey) error {

	if scriptKey.PubKey == nil || scriptKey.TweakedScriptKey == nil ||
		scriptKey.RawKey.PubKey == nil {

		return fmt.Errorf("script key is incomplete")
	}

	return e.store.InsertScriptKey(ctx, ScriptKeyRecord{
		TweakedKey: scriptKeyBytes(scriptKey.PubKey),
		RawKey:     keyRecord(scriptKey.RawKey),
		Tweak:      scriptKey.Tweak,
		Type:       uint8(scriptKey.Type),
	})
}
