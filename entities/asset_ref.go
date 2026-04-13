package entities

import (
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcutil/bech32"
)

// assetRefKind distinguishes the two protocol encodings hidden behind an
// opaque AssetRef string.
type assetRefKind uint8

const (
	assetRefKindAssetID  assetRefKind = 0
	assetRefKindGroupKey assetRefKind = 1
)

const (
	assetRefHRP           = "assetref"
	assetRefFormatVersion = 0
)

// AssetRef is the SDK's user-facing asset identifier.
//
// It is intentionally opaque. Callers should treat it as a stable string to
// store, compare, log, and pass back into SDK methods. Internally it encodes
// either a protocol asset ID or a group key, but callers should not need to
// care which one was used.
type AssetRef string

// AssetRefFromGroupKey creates an AssetRef for an asset identified by a group
// key.
func AssetRefFromGroupKey(groupKey PubKey) AssetRef {
	return encodeAssetRef(assetRefKindGroupKey, groupKey[:])
}

// AssetRefFromAssetID creates an AssetRef for an asset identified by a
// protocol asset ID.
func AssetRefFromAssetID(assetID AssetID) AssetRef {
	return encodeAssetRef(assetRefKindAssetID, assetID[:])
}

// AssetRefFromSpecifier creates an AssetRef from protocol identifiers.
// When both are present, the group key takes precedence as the semantic
// asset identity.
func AssetRefFromSpecifier(assetID *AssetID, groupKey *PubKey) (AssetRef,
	error) {

	switch {
	case groupKey != nil:
		return AssetRefFromGroupKey(*groupKey), nil

	case assetID != nil:
		return AssetRefFromAssetID(*assetID), nil

	default:
		return "", fmt.Errorf("asset ref requires either an asset " +
			"ID or group key")
	}
}

// AssetRefFromAsset returns the semantic asset identifier for an issuance.
// Grouped assets use the group key. Ungrouped assets use the issuance asset ID.
func AssetRefFromAsset(issuanceID AssetID, groupKey *PubKey) AssetRef {
	if groupKey != nil {
		return AssetRefFromGroupKey(*groupKey)
	}

	return AssetRefFromAssetID(issuanceID)
}

// ParseAssetRef validates an encoded asset reference and returns it in
// canonical lowercase form.
func ParseAssetRef(s string) (AssetRef, error) {
	_, _, canonical, err := decodeAssetRefString(s)
	if err != nil {
		return "", err
	}

	return AssetRef(canonical), nil
}

// String returns the encoded asset reference string.
func (r AssetRef) String() string {
	return string(r)
}

// IsZero reports whether the asset reference is unset.
func (r AssetRef) IsZero() bool {
	return r == ""
}

// Validate checks whether the asset reference string is well-formed.
func (r AssetRef) Validate() error {
	_, _, _, err := decodeAssetRefString(string(r))
	return err
}

// IsFungible returns true when the reference resolves to a group key.
func (r AssetRef) IsFungible() bool {
	kind, _, _, err := decodeAssetRefString(string(r))
	return err == nil && kind == assetRefKindGroupKey
}

// IsCollectible returns true when the reference resolves to an asset ID.
func (r AssetRef) IsCollectible() bool {
	kind, _, _, err := decodeAssetRefString(string(r))
	return err == nil && kind == assetRefKindAssetID
}

// GroupKey returns the group key and true when the asset reference resolves to
// a group key.
func (r AssetRef) GroupKey() (PubKey, bool) {
	kind, payload, _, err := decodeAssetRefString(string(r))
	if err != nil || kind != assetRefKindGroupKey {
		return PubKey{}, false
	}

	key, err := ParsePubKey(payload)
	if err != nil {
		return PubKey{}, false
	}

	return key, true
}

// AssetID returns the asset ID and true when the asset reference resolves to
// a protocol asset ID.
func (r AssetRef) AssetID() (AssetID, bool) {
	kind, payload, _, err := decodeAssetRefString(string(r))
	if err != nil || kind != assetRefKindAssetID {
		return AssetID{}, false
	}

	id, err := ParseAssetID(payload)
	if err != nil {
		return AssetID{}, false
	}

	return id, true
}

// Specifier exposes the underlying protocol identifier used by this AssetRef.
// Exactly one of the returned pointers is non-nil.
func (r AssetRef) Specifier() (*AssetID, *PubKey, error) {
	kind, payload, _, err := decodeAssetRefString(string(r))
	if err != nil {
		return nil, nil, err
	}

	switch kind {
	case assetRefKindAssetID:
		assetID, err := ParseAssetID(payload)
		if err != nil {
			return nil, nil, err
		}

		return &assetID, nil, nil

	case assetRefKindGroupKey:
		groupKey, err := ParsePubKey(payload)
		if err != nil {
			return nil, nil, err
		}

		return nil, &groupKey, nil

	default:
		return nil, nil, fmt.Errorf("unknown asset ref kind: %d", kind)
	}
}

func encodeAssetRef(kind assetRefKind, payload []byte) AssetRef {
	base256 := make([]byte, 0, 2+len(payload))
	base256 = append(base256, assetRefFormatVersion, byte(kind))
	base256 = append(base256, payload...)

	base32, err := bech32.ConvertBits(base256, 8, 5, true)
	if err != nil {
		panic(fmt.Sprintf("invalid asset ref payload: %v", err))
	}

	encoded, err := bech32.EncodeM(assetRefHRP, base32)
	if err != nil {
		panic(fmt.Sprintf("failed to encode asset ref: %v", err))
	}

	return AssetRef(encoded)
}

func decodeAssetRefString(s string) (assetRefKind, []byte, string, error) {
	if s == "" {
		return 0, nil, "", fmt.Errorf("asset ref is empty")
	}

	normalized := strings.ToLower(s)

	hrp, data, version, err := bech32.DecodeGeneric(normalized)
	if err != nil {
		return 0, nil, "", fmt.Errorf("invalid asset ref: %w", err)
	}

	if version != bech32.VersionM {
		return 0, nil, "", fmt.Errorf("asset ref must use bech32m encoding")
	}

	if hrp != assetRefHRP {
		return 0, nil, "", fmt.Errorf("asset ref must use %q hrp", assetRefHRP)
	}

	base256, err := bech32.ConvertBits(data, 5, 8, false)
	if err != nil {
		return 0, nil, "", fmt.Errorf("invalid asset ref payload: %w", err)
	}

	if len(base256) < 2 {
		return 0, nil, "", fmt.Errorf("asset ref payload is too short")
	}

	if base256[0] != assetRefFormatVersion {
		return 0, nil, "", fmt.Errorf("unsupported asset ref version: %d", base256[0])
	}

	kind := assetRefKind(base256[1])
	payload := base256[2:]

	switch kind {
	case assetRefKindAssetID:
		if _, err := ParseAssetID(payload); err != nil {
			return 0, nil, "", fmt.Errorf("invalid asset ref asset ID: %w", err)
		}

	case assetRefKindGroupKey:
		if _, err := ParsePubKey(payload); err != nil {
			return 0, nil, "", fmt.Errorf("invalid asset ref group key: %w", err)
		}

	default:
		return 0, nil, "", fmt.Errorf("unknown asset ref kind: %d", kind)
	}

	canonicalBase32, err := bech32.ConvertBits(base256, 8, 5, true)
	if err != nil {
		return 0, nil, "", fmt.Errorf("invalid asset ref payload: %w", err)
	}

	canonical, err := bech32.EncodeM(assetRefHRP, canonicalBase32)
	if err != nil {
		return 0, nil, "", fmt.Errorf("failed to canonicalize asset ref: %w", err)
	}

	return kind, payload, strings.ToLower(canonical), nil
}
