package embedded

import (
	"context"
	"sort"
	"sync"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

// MemoryStore is a Store that keeps everything in memory. It suits tests and
// wallets that verify proofs without holding assets.
type MemoryStore struct {
	mu sync.Mutex

	internalKeys map[[33]byte]KeyRecord
	scriptKeys   map[[33]byte]ScriptKeyRecord
	proofs       map[ProofLocator]AssetProof
	transfers    map[chainhash.Hash]Transfer
	order        []chainhash.Hash
}

// A compile-time check that MemoryStore is a Store.
var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		internalKeys: make(map[[33]byte]KeyRecord),
		scriptKeys:   make(map[[33]byte]ScriptKeyRecord),
		proofs:       make(map[ProofLocator]AssetProof),
		transfers:    make(map[chainhash.Hash]Transfer),
	}
}

// InsertInternalKey records a derived key.
func (m *MemoryStore) InsertInternalKey(_ context.Context,
	key KeyRecord) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	m.internalKeys[key.PubKey] = key

	return nil
}

// FetchInternalKey returns the record of a derived key.
func (m *MemoryStore) FetchInternalKey(_ context.Context,
	pubKey [33]byte) (*KeyRecord, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.internalKeys[pubKey]
	if !ok {
		return nil, ErrNotFound
	}

	return &key, nil
}

// InsertScriptKey records a script key.
func (m *MemoryStore) InsertScriptKey(_ context.Context,
	key ScriptKeyRecord) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	key.Tweak = append([]byte(nil), key.Tweak...)
	m.scriptKeys[key.TweakedKey] = key

	return nil
}

// FetchScriptKey returns the record of a tweaked script key.
func (m *MemoryStore) FetchScriptKey(_ context.Context,
	tweakedKey [33]byte) (*ScriptKeyRecord, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.scriptKeys[tweakedKey]
	if !ok {
		return nil, ErrNotFound
	}
	key.Tweak = append([]byte(nil), key.Tweak...)

	return &key, nil
}

// InsertProof stores a proof file.
func (m *MemoryStore) InsertProof(_ context.Context, proof AssetProof) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.putProof(proof)

	return nil
}

// putProof stores a proof file, keeping the spent flag of a known record.
func (m *MemoryStore) putProof(proof AssetProof) {
	proof.ProofFile = append([]byte(nil), proof.ProofFile...)
	if known, ok := m.proofs[proof.ProofLocator]; ok {
		proof.Spent = known.Spent
	}
	m.proofs[proof.ProofLocator] = proof
}

// FetchProofs returns the proofs of an asset and script key.
func (m *MemoryStore) FetchProofs(_ context.Context, assetID [32]byte,
	scriptKey [33]byte, outpoint *wire.OutPoint) ([]AssetProof, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	var matches []AssetProof
	for locator, proof := range m.proofs {
		if locator.AssetID != assetID ||
			locator.ScriptKey != scriptKey {

			continue
		}
		if outpoint != nil && locator.Outpoint != *outpoint {
			continue
		}
		matches = append(matches, copyProof(proof))
	}
	sortProofs(matches)

	return matches, nil
}

// ListOwnedProofs returns the owned asset outputs.
func (m *MemoryStore) ListOwnedProofs(_ context.Context,
	includeSpent bool) ([]AssetProof, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	var owned []AssetProof
	for _, proof := range m.proofs {
		if !proof.Owned || (proof.Spent && !includeSpent) {
			continue
		}
		owned = append(owned, copyProof(proof))
	}
	sortProofs(owned)

	return owned, nil
}

// InsertTransfer records a transfer and marks the outputs it spends.
func (m *MemoryStore) InsertTransfer(_ context.Context, transfer Transfer,
	spends []ProofLocator) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.transfers[transfer.AnchorTxid]; ok {
		return ErrTransferExists
	}

	for _, locator := range spends {
		proof, ok := m.proofs[locator]
		if !ok {
			return ErrNotFound
		}
		proof.Spent = true
		m.proofs[locator] = proof
	}

	transfer.Payload = append([]byte(nil), transfer.Payload...)
	m.transfers[transfer.AnchorTxid] = transfer
	m.order = append(m.order, transfer.AnchorTxid)

	return nil
}

// UpdateTransfer replaces a recorded transfer and applies its proof changes.
func (m *MemoryStore) UpdateTransfer(_ context.Context, transfer Transfer,
	putProofs []AssetProof, deleteProofs []ProofLocator) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	known, ok := m.transfers[transfer.AnchorTxid]
	if !ok {
		return ErrNotFound
	}

	transfer.CreatedAt = known.CreatedAt
	transfer.HeightHint = known.HeightHint
	transfer.Payload = append([]byte(nil), transfer.Payload...)
	m.transfers[transfer.AnchorTxid] = transfer

	for _, proof := range putProofs {
		m.putProof(proof)
	}
	for _, locator := range deleteProofs {
		delete(m.proofs, locator)
	}

	return nil
}

// ListTransfers returns the recorded transfers, oldest first.
func (m *MemoryStore) ListTransfers(_ context.Context,
	anchorTxid *chainhash.Hash) ([]Transfer, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	var transfers []Transfer
	for _, txid := range m.order {
		if anchorTxid != nil && txid != *anchorTxid {
			continue
		}

		transfer := m.transfers[txid]
		transfer.Payload = append([]byte(nil), transfer.Payload...)
		transfers = append(transfers, transfer)
	}

	return transfers, nil
}

// copyProof returns a proof whose file is not shared with the store.
func copyProof(proof AssetProof) AssetProof {
	proof.ProofFile = append([]byte(nil), proof.ProofFile...)

	return proof
}

// sortProofs orders proofs by anchor outpoint, asset and script key.
func sortProofs(proofs []AssetProof) {
	sort.Slice(proofs, func(i, j int) bool {
		a, b := proofs[i].ProofLocator, proofs[j].ProofLocator
		if a.Outpoint != b.Outpoint {
			return a.Outpoint.String() < b.Outpoint.String()
		}
		if a.AssetID != b.AssetID {
			return string(a.AssetID[:]) < string(b.AssetID[:])
		}

		return string(a.ScriptKey[:]) < string(b.ScriptKey[:])
	})
}
