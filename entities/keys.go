package entities

// TaprootAssetsKeyFamily is the key family used to generate internal keys and
// script keys for Taproot Assets. This value must match the constant defined
// in the taproot-assets repository.
const TaprootAssetsKeyFamily = 212

// KeyLocator identifies a key in the key derivation path.
type KeyLocator struct {
	// Family is the key family (derivation purpose).
	Family uint32

	// Index is the specific key index within the family.
	Index uint32
}

// KeyDescriptor contains key bytes and locator information.
type KeyDescriptor struct {
	// RawKeyBytes is the raw public key bytes (33 bytes compressed).
	RawKeyBytes [33]byte

	// KeyLocator identifies the key's derivation path.
	KeyLocator KeyLocator
}

// ScriptKey represents a Taproot Asset script key for receiving assets.
// A script key is composed of an internal key that is optionally tweaked
// with a tap tweak to produce the final public key.
type ScriptKey struct {
	// PubKey is the full Taproot output key the asset is locked to.
	// This is either a BIP-86 key if TapTweak is empty, or a key with
	// the tap tweak applied.
	PubKey [33]byte

	// KeyDesc describes the internal key used to derive PubKey.
	KeyDesc KeyDescriptor

	// TapTweak is the optional Taproot tweak applied to the internal key.
	// If empty, a BIP-86 style tweak is applied.
	TapTweak []byte
}

// InternalKey represents an anchor output internal key.
// This key is used as the internal key for the Taproot output that anchors
// the asset commitment on-chain.
type InternalKey struct {
	// PubKey is the raw public key bytes (33 bytes compressed).
	PubKey [33]byte

	// KeyLocator identifies the key's derivation path.
	KeyLocator KeyLocator
}

// DerivedKeys contains both keys needed for receiving assets in an
// interactive transfer. The receiver derives these keys and shares them
// with the sender.
type DerivedKeys struct {
	// ScriptKey is used to lock the asset to the receiver.
	ScriptKey ScriptKey

	// InternalKey is used as the internal key for the anchor output.
	InternalKey InternalKey
}

