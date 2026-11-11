//go:build itest

package itest

import (
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/lightninglabs/tap-sdk/embedded"
	"github.com/lightningnetwork/lnd/keychain"
)

// hdKeyRing is an embedded.KeyRing over a BIP-32 seed. A key locator maps to
// m/1017'/coinType'/family'/0/index, the path lnd derives it at.
type hdKeyRing struct {
	// coinKey is the extended key at m/1017'/coinType'.
	coinKey *hdkeychain.ExtendedKey

	mu   sync.Mutex
	next map[keychain.KeyFamily]uint32
}

// A compile-time check that hdKeyRing is a key ring of the engine.
var _ embedded.KeyRing = (*hdKeyRing)(nil)

// newHDKeyRing returns a key ring for the seed on the given network.
func newHDKeyRing(seed []byte, params *chaincfg.Params) (*hdKeyRing, error) {
	master, err := hdkeychain.NewMaster(seed, params)
	if err != nil {
		return nil, err
	}
	purposeKey, err := master.Derive(
		hdkeychain.HardenedKeyStart + keychain.BIP0043Purpose,
	)
	if err != nil {
		return nil, err
	}
	coinKey, err := purposeKey.Derive(
		hdkeychain.HardenedKeyStart + params.HDCoinType,
	)
	if err != nil {
		return nil, err
	}

	return &hdKeyRing{
		coinKey: coinKey,
		next:    make(map[keychain.KeyFamily]uint32),
	}, nil
}

// privKey derives the private key at a key locator.
func (k *hdKeyRing) privKey(
	locator keychain.KeyLocator) (*btcec.PrivateKey, error) {

	familyKey, err := k.coinKey.Derive(
		hdkeychain.HardenedKeyStart + uint32(locator.Family),
	)
	if err != nil {
		return nil, err
	}
	branchKey, err := familyKey.Derive(0)
	if err != nil {
		return nil, err
	}
	indexKey, err := branchKey.Derive(locator.Index)
	if err != nil {
		return nil, err
	}

	return indexKey.ECPrivKey()
}

// DeriveNextKey derives the next unused key of the family.
func (k *hdKeyRing) DeriveNextKey(
	family keychain.KeyFamily) (keychain.KeyDescriptor, error) {

	k.mu.Lock()
	defer k.mu.Unlock()

	locator := keychain.KeyLocator{
		Family: family,
		Index:  k.next[family],
	}
	privKey, err := k.privKey(locator)
	if err != nil {
		return keychain.KeyDescriptor{}, err
	}
	k.next[family]++

	return keychain.KeyDescriptor{
		KeyLocator: locator,
		PubKey:     privKey.PubKey(),
	}, nil
}

// DerivePrivKey derives the private key of a key descriptor. A descriptor
// that names a public key must name the one at its locator.
func (k *hdKeyRing) DerivePrivKey(
	desc keychain.KeyDescriptor) (*btcec.PrivateKey, error) {

	privKey, err := k.privKey(desc.KeyLocator)
	if err != nil {
		return nil, err
	}
	if desc.PubKey != nil && !privKey.PubKey().IsEqual(desc.PubKey) {
		return nil, fmt.Errorf("key %x is not at %d/%d",
			desc.PubKey.SerializeCompressed(), desc.Family,
			desc.Index)
	}

	return privKey, nil
}
