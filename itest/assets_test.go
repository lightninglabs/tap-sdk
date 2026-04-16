//go:build itest

package itest

import (
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestParseGroupRefKeyPreservesCompressedKey ensures grouped asset refs keep
// the exact compressed group key bytes returned by ListGroups. Odd-Y keys must
// not be normalized through x-only parsing, or semantic grouped lookups break.
func TestParseGroupRefKeyPreservesCompressedKey(t *testing.T) {
	t.Parallel()

	oddKey := oddCompressedPubKey(t)
	hexKey := hex.EncodeToString(oddKey[:])
	assetRef := entities.AssetRefFromGroupKey(oddKey)

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "asset ref",
			input: assetRef.String(),
		},
		{
			name:  "compressed hex",
			input: hexKey,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseGroupRefKey(test.input)
			require.NoError(t, err)

			require.Equal(t, oddKey, parsed)
		})
	}
}

// TestParseGroupRefKeyXOnlyFallback ensures the helper still accepts x-only
// encodings when they appear on taproot-facing paths.
func TestParseGroupRefKeyXOnlyFallback(t *testing.T) {
	t.Parallel()

	oddKey := oddCompressedPubKey(t)
	xOnlyHex := hex.EncodeToString(oddKey[1:])
	expected, err := entities.ParseTaprootPubKey(oddKey[1:])
	require.NoError(t, err)

	parsed, err := parseGroupRefKey(xOnlyHex)
	require.NoError(t, err)
	require.Equal(t, expected, parsed)
}

func oddCompressedPubKey(t *testing.T) entities.PubKey {
	t.Helper()

	for i := byte(1); i < 64; i++ {
		privKey, _ := btcec.PrivKeyFromBytes([]byte{i})
		compressed := privKey.PubKey().SerializeCompressed()
		if compressed[0] != 0x03 {
			continue
		}

		parsed, err := entities.ParsePubKey(compressed)
		require.NoError(t, err)

		xOnly := schnorr.SerializePubKey(privKey.PubKey())
		require.Equal(t, xOnly, parsed[1:])

		return parsed
	}

	t.Fatal("failed to derive odd compressed pubkey")
	return entities.PubKey{}
}
