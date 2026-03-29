package entities

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// testGroupKey returns a deterministic compressed public key for testing.
func testGroupKey(t *testing.T) PubKey {
	t.Helper()

	// Valid compressed secp256k1 public key (02 prefix + 32 bytes).
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

	require.True(t, ref.IsFungible(), "fungible ref must be fungible")
	require.False(t, ref.IsCollectible(),
		"fungible ref must not be collectible")

	gotKey, ok := ref.GroupKey()
	require.True(t, ok, "GroupKey accessor must return true")
	require.Equal(t, key, gotKey)

	_, ok = ref.AssetID()
	require.False(t, ok, "AssetID accessor must return false")
}

func TestAssetRefFromAssetID(t *testing.T) {
	id := testAssetID()
	ref := AssetRefFromAssetID(id)

	require.True(t, ref.IsCollectible(),
		"collectible ref must be collectible")
	require.False(t, ref.IsFungible(),
		"collectible ref must not be fungible")

	gotID, ok := ref.AssetID()
	require.True(t, ok, "AssetID accessor must return true")
	require.Equal(t, id, gotID)

	_, ok = ref.GroupKey()
	require.False(t, ok, "GroupKey accessor must return false")
}

func TestAssetRefString(t *testing.T) {
	tests := []struct {
		name string
		ref  AssetRef
		want string
	}{
		{
			name: "fungible group key",
			ref:  AssetRefFromGroupKey(testGroupKey(t)),
			want: "group:" + hex.EncodeToString(
				testGroupKey(t).Bytes(),
			),
		},
		{
			name: "collectible asset ID",
			ref:  AssetRefFromAssetID(testAssetID()),
			want: "asset:" + testAssetID().String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.ref.String())
		})
	}
}

func TestParseAssetRef(t *testing.T) {
	key := testGroupKey(t)
	id := testAssetID()

	tests := []struct {
		name    string
		input   string
		want    AssetRef
		wantErr bool
	}{
		{
			name:  "valid group key",
			input: AssetRefFromGroupKey(key).String(),
			want:  AssetRefFromGroupKey(key),
		},
		{
			name:  "valid asset ID",
			input: AssetRefFromAssetID(id).String(),
			want:  AssetRefFromAssetID(id),
		},
		{
			name:    "too short",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			input:   "token:abcdef",
			wantErr: true,
		},
		{
			name:    "group with bad hex",
			input:   "group:zzzz",
			wantErr: true,
		},
		{
			name:    "asset with bad hex",
			input:   "asset:zzzz",
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
