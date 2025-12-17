package codec

import (
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

func TestDeriveBurnKeyVectors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		prevID      entities.PrevID
		expectedKey string
	}{{
		name:        "empty prev ID",
		prevID:      entities.PrevID{},
		expectedKey: "b87da731321c9e90a2f3d525cf81a2f503e04ea49543692951e6b88752a0d72d",
	}, {
		name: "dummy value ID",
		prevID: func() entities.PrevID {
			var txid [32]byte
			txid[0] = 0x77
			txid[1] = 0x88
			txid[2] = 0x99
			txid[3] = 0xaa

			var assetID entities.AssetID
			assetID[0] = 0x01
			assetID[1] = 0x02
			assetID[2] = 0x03
			assetID[3] = 0x04

			var scriptKey entities.PubKey
			scriptKey[0] = 0x02
			scriptKey[1] = 0x03
			scriptKey[2] = 0x04
			scriptKey[3] = 0x05

			return entities.PrevID{
				Outpoint: entities.Outpoint{
					Txid:  txid,
					Index: 123,
				},
				AssetID:   assetID,
				ScriptKey: scriptKey,
			}
		}(),
		expectedKey: "77493dcf8c7e6c1f214824409b2468afe8e4e5faa47e6ae87ddb60226ad4edde",
	}, {
		name: "random value ID",
		prevID: func() entities.PrevID {
			wireOutpoint, err := wire.NewOutPointFromString(
				"c8ca462e6247b1c7d67f9e2b5e371fc9303c3c3e6d690e8fb4a6bb5ca5b78104:354062834",
			)
			require.NoError(t, err)

			var txid [32]byte
			copy(txid[:], wireOutpoint.Hash[:])

			assetIDBytes, err := hex.DecodeString(
				"560982cea2defb7795dda938422b4d7ae5462e64cde32fc68ced4f503f8a5af7",
			)
			require.NoError(t, err)
			require.Len(t, assetIDBytes, 32)

			scriptKeyBytes, err := hex.DecodeString(
				"03c50bfc65dfb20e9b9c1c6d8b435ef91f41eb86434576823eeaf3a69fa7e1fc78",
			)
			require.NoError(t, err)
			require.Len(t, scriptKeyBytes, 33)

			var assetID entities.AssetID
			copy(assetID[:], assetIDBytes)

			var scriptKey entities.PubKey
			copy(scriptKey[:], scriptKeyBytes)

			return entities.PrevID{
				Outpoint: entities.Outpoint{
					Txid:  txid,
					Index: wireOutpoint.Index,
				},
				AssetID:   assetID,
				ScriptKey: scriptKey,
			}
		}(),
		expectedKey: "a76bc68f430c78cfdad6d72abf143de10c8b679842fe00736072361b52ad426c",
	}}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			burnKeyBytes, err := DeriveBurnKey(tc.prevID)
			require.NoError(tt, err)

			pubKey, err := btcec.ParsePubKey(burnKeyBytes[:])
			require.NoError(tt, err)

			burnKeyHex := hex.EncodeToString(schnorr.SerializePubKey(pubKey))
			require.Equal(tt, tc.expectedKey, burnKeyHex)
		})
	}
}

func TestDeriveSTXOAltLeavesDuplicatePrevID(t *testing.T) {
	t.Parallel()

	prevIDs := []entities.PrevID{
		{},
		{},
	}

	_, err := DeriveSTXOAltLeaves(prevIDs)
	require.Error(t, err)
}
