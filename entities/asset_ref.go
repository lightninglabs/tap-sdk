package entities

import (
	"encoding/hex"
	"fmt"
)

// assetRefKind distinguishes the two forms of asset reference.
type assetRefKind uint8

const (
	assetRefKindAssetID  assetRefKind = 0
	assetRefKindGroupKey assetRefKind = 1
)

// AssetRef is a unified reference to a Taproot Asset. It abstracts over
// the protocol-level distinction between asset ID and group key, providing
// a single identifier type across the high-level SDK surface.
//
// Use AssetRefFromGroupKey for fungible assets (the group key identifies
// the asset regardless of which tranche issued it) and AssetRefFromAssetID
// for collectibles / non-fungible assets (where the per-mint asset ID is
// the canonical identifier).
//
// Low-level gRPC wrappers continue to accept raw AssetID and PubKey values
// directly. AssetRef is intended for the opinionated Wallet surface.
type AssetRef struct {
	kind     assetRefKind
	assetID  AssetID
	groupKey PubKey
}

// AssetRefFromGroupKey creates an AssetRef for a fungible asset identified
// by its group key.
func AssetRefFromGroupKey(groupKey PubKey) AssetRef {
	return AssetRef{
		kind:     assetRefKindGroupKey,
		groupKey: groupKey,
	}
}

// AssetRefFromAssetID creates an AssetRef for a collectible (non-fungible)
// asset identified by its unique asset ID.
func AssetRefFromAssetID(assetID AssetID) AssetRef {
	return AssetRef{
		kind:    assetRefKindAssetID,
		assetID: assetID,
	}
}

// IsFungible returns true when the reference identifies a fungible asset
// by group key.
func (r AssetRef) IsFungible() bool {
	return r.kind == assetRefKindGroupKey
}

// IsCollectible returns true when the reference identifies a non-fungible
// asset by asset ID.
func (r AssetRef) IsCollectible() bool {
	return r.kind == assetRefKindAssetID
}

// GroupKey returns the group key and true for fungible references, or
// the zero value and false for collectible references.
func (r AssetRef) GroupKey() (PubKey, bool) {
	if r.kind == assetRefKindGroupKey {
		return r.groupKey, true
	}

	return PubKey{}, false
}

// AssetID returns the asset ID and true for collectible references, or
// the zero value and false for fungible references.
func (r AssetRef) AssetID() (AssetID, bool) {
	if r.kind == assetRefKindAssetID {
		return r.assetID, true
	}

	return AssetID{}, false
}

// String returns a human-readable representation of the asset reference.
// Fungible references are prefixed with "group:" and collectible references
// with "asset:".
func (r AssetRef) String() string {
	switch r.kind {
	case assetRefKindGroupKey:
		return "group:" + hex.EncodeToString(r.groupKey[:])

	case assetRefKindAssetID:
		return "asset:" + r.assetID.String()

	default:
		return "unknown"
	}
}

// ParseAssetRef parses a string produced by AssetRef.String back into an
// AssetRef. It accepts both "group:<hex>" and "asset:<hex>" formats.
func ParseAssetRef(s string) (AssetRef, error) {
	if len(s) < 7 {
		return AssetRef{}, fmt.Errorf("invalid asset ref: %q", s)
	}

	switch {
	case len(s) > 6 && s[:6] == "group:":
		key, err := ParsePubKeyHex(s[6:])
		if err != nil {
			return AssetRef{}, fmt.Errorf(
				"invalid group key in asset ref: %w", err,
			)
		}

		return AssetRefFromGroupKey(key), nil

	case len(s) > 6 && s[:6] == "asset:":
		id, err := ParseAssetIDHex(s[6:])
		if err != nil {
			return AssetRef{}, fmt.Errorf(
				"invalid asset ID in asset ref: %w", err,
			)
		}

		return AssetRefFromAssetID(id), nil

	default:
		return AssetRef{}, fmt.Errorf(
			"asset ref must start with \"group:\" or "+
				"\"asset:\": %q", s,
		)
	}
}
