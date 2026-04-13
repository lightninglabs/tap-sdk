package entities

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/stretchr/testify/require"
)

// testGroupKey returns a deterministic compressed public key for testing.
func testGroupKey(t *testing.T) PubKey {
	t.Helper()

	b, err := hex.DecodeString(
		"02a1633cafcc01ebfb6d78e39f687a1f0995c62fc95f51ead10" +
			"a02ee0be551b5dc",
	)
	require.NoError(t, err)

	key, err := ParsePubKey(b)
	require.NoError(t, err)

	return key
}

// testAssetID returns a deterministic 32-byte asset ID for testing.
func testAssetID() AssetID {
	var id AssetID
	for i := range id {
		id[i] = byte(i)
	}

	return id
}

func TestAssetRefFromGroupKey(t *testing.T) {
	key := testGroupKey(t)
	ref := AssetRefFromGroupKey(key)

	require.False(t, ref.IsZero())
	require.NoError(t, ref.Validate())
	require.True(t, ref.IsFungible())
	require.False(t, ref.IsCollectible())

	gotKey, ok := ref.GroupKey()
	require.True(t, ok)
	require.Equal(t, key, gotKey)

	_, ok = ref.AssetID()
	require.False(t, ok)

	assetID, groupKey, err := ref.Specifier()
	require.NoError(t, err)
	require.Nil(t, assetID)
	require.NotNil(t, groupKey)
	require.Equal(t, key, *groupKey)
}

func TestAssetRefFromAssetID(t *testing.T) {
	id := testAssetID()
	ref := AssetRefFromAssetID(id)

	require.False(t, ref.IsZero())
	require.NoError(t, ref.Validate())
	require.True(t, ref.IsCollectible())
	require.False(t, ref.IsFungible())

	gotID, ok := ref.AssetID()
	require.True(t, ok)
	require.Equal(t, id, gotID)

	_, ok = ref.GroupKey()
	require.False(t, ok)

	assetID, groupKey, err := ref.Specifier()
	require.NoError(t, err)
	require.NotNil(t, assetID)
	require.Nil(t, groupKey)
	require.Equal(t, id, *assetID)
}

func TestAssetRefEncodingUsesBech32M(t *testing.T) {
	refs := []AssetRef{
		AssetRefFromGroupKey(testGroupKey(t)),
		AssetRefFromAssetID(testAssetID()),
	}

	for _, ref := range refs {
		t.Run(ref.String(), func(t *testing.T) {
			hrp, _, version, err := bech32.DecodeGeneric(ref.String())
			require.NoError(t, err)
			require.Equal(t, assetRefHRP, hrp)
			require.Equal(t, bech32.VersionM, version)
			require.True(t, strings.HasPrefix(ref.String(), assetRefHRP+"1"))
		})
	}
}

func TestParseAssetRef(t *testing.T) {
	key := testGroupKey(t)
	id := testAssetID()

	validGroup := AssetRefFromGroupKey(key)
	validAsset := AssetRefFromAssetID(id)
	mixedCase := strings.ToUpper(validAsset.String()[:8]) + validAsset.String()[8:]

	tests := []struct {
		name    string
		input   string
		want    AssetRef
		wantErr bool
	}{
		{
			name:  "valid group key",
			input: validGroup.String(),
			want:  validGroup,
		},
		{
			name:  "valid asset id",
			input: validAsset.String(),
			want:  validAsset,
		},
		{
			name:  "mixed case normalizes to lowercase",
			input: mixedCase,
			want:  validAsset,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "wrong hrp",
			input:   strings.Replace(validAsset.String(), assetRefHRP, "token", 1),
			wantErr: true,
		},
		{
			name:    "bad checksum",
			input:   validAsset.String()[:len(validAsset.String())-1] + "x",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAssetRef(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestAssetRefRoundTrip(t *testing.T) {
	refs := []AssetRef{
		AssetRefFromGroupKey(testGroupKey(t)),
		AssetRefFromAssetID(testAssetID()),
	}

	for _, ref := range refs {
		t.Run(ref.String(), func(t *testing.T) {
			parsed, err := ParseAssetRef(ref.String())
			require.NoError(t, err)
			require.Equal(t, ref, parsed)
		})
	}
}

func TestAssetRefFromSpecifier(t *testing.T) {
	groupKey := testGroupKey(t)
	assetID := testAssetID()

	ref, err := AssetRefFromSpecifier(nil, &groupKey)
	require.NoError(t, err)
	require.Equal(t, AssetRefFromGroupKey(groupKey), ref)

	ref, err = AssetRefFromSpecifier(&assetID, nil)
	require.NoError(t, err)
	require.Equal(t, AssetRefFromAssetID(assetID), ref)

	_, err = AssetRefFromSpecifier(nil, nil)
	require.Error(t, err)

	// When both are set, group key takes precedence.
	ref, err = AssetRefFromSpecifier(&assetID, &groupKey)
	require.NoError(t, err)
	require.Equal(t, AssetRefFromGroupKey(groupKey), ref)
}

func TestUniverseIDFromRef_GroupKey(t *testing.T) {
	gk := testGroupKey(t)
	ref := AssetRefFromGroupKey(gk)

	uid := UniverseIDFromRef(ref, ProofTypeIssuance)
	require.Equal(t, ref, uid.AssetRef)
	require.Equal(t, ProofTypeIssuance, uid.ProofType)
}

func TestUniverseIDFromRef_AssetID(t *testing.T) {
	aid := testAssetID()
	ref := AssetRefFromAssetID(aid)

	uid := UniverseIDFromRef(ref, ProofTypeTransfer)
	require.Equal(t, ref, uid.AssetRef)
	require.Equal(t, ProofTypeTransfer, uid.ProofType)
}
