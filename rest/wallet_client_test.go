package rest

import (
	"encoding/hex"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

var (
	restTestPubKey = []byte{
		0x02,
		0x79, 0xbe, 0x66, 0x7e, 0xf9, 0xdc, 0xbb, 0xac,
		0x55, 0xa0, 0x62, 0x95, 0xce, 0x87, 0x0b, 0x07,
		0x02, 0x9b, 0xfc, 0xdb, 0x2d, 0xce, 0x28, 0xd9,
		0x59, 0xf2, 0x81, 0x5b, 0x16, 0xf8, 0x17, 0x98,
	}

	restTestAssetID = []byte{
		0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8,
		0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0,
		0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8,
		0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf, 0xc0,
	}
)

const restZeroOutpoint = "00000000000000000000000000000000" +
	"00000000000000000000000000000000:1"

func TestAssetRecordMatchesRef(t *testing.T) {
	var assetID entities.AssetID
	copy(assetID[:], restTestAssetID)
	itemRef := entities.AssetRefFromAssetID(assetID)

	var groupKey entities.PubKey
	copy(groupKey[:], restTestPubKey)
	collectionRef := entities.AssetRefFromGroupKey(groupKey)

	record := &entities.AssetRecord{
		AssetRef: collectionRef,
		Genesis: entities.IssuanceGenesis{
			IssuanceID: assetID,
		},
	}

	require.True(t, assetRecordMatchesRef(record, collectionRef))
	require.True(t, assetRecordMatchesRef(record, itemRef))
}

func TestBurnAssetRequestBody(t *testing.T) {
	var assetID entities.AssetID
	copy(assetID[:], restTestAssetID)

	var groupKey entities.PubKey
	copy(groupKey[:], restTestPubKey)

	tests := []struct {
		name     string
		req      *entities.BurnAssetRequest
		validate func(*testing.T, map[string]any)
	}{
		{
			name: "asset ID ref uses asset specifier",
			req: &entities.BurnAssetRequest{
				AssetRef:         entities.AssetRefFromAssetID(assetID),
				AmountToBurn:     100,
				ConfirmationText: "assets will be destroyed",
				Note:             "test",
			},
			validate: func(t *testing.T, body map[string]any) {
				specifier, ok := body["asset_specifier"].(map[string]string)
				require.True(t, ok)
				require.Equal(
					t, hex.EncodeToString(restTestAssetID),
					specifier["asset_id_str"],
				)
				require.NotContains(t, body, "asset_id_str")
				require.Equal(t, "100", body["amount_to_burn"])
				require.Equal(t, "test", body["note"])
			},
		},
		{
			name: "group key ref uses asset specifier",
			req: &entities.BurnAssetRequest{
				AssetRef:     entities.AssetRefFromGroupKey(groupKey),
				AmountToBurn: 200,
			},
			validate: func(t *testing.T, body map[string]any) {
				specifier, ok := body["asset_specifier"].(map[string]string)
				require.True(t, ok)
				require.Equal(
					t, hex.EncodeToString(restTestPubKey),
					specifier["group_key_str"],
				)
				require.NotContains(t, body, "asset_id_str")
				require.Equal(t, "200", body["amount_to_burn"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := burnAssetRequestBody(tc.req)
			require.NoError(t, err)

			tc.validate(t, body)
		})
	}
}

func TestUnmarshalAssetTransferGroupKey(t *testing.T) {
	pubKeyHex := hex.EncodeToString(restTestPubKey)
	assetIDHex := hex.EncodeToString(restTestAssetID)

	transfer, err := unmarshalAssetTransfer(&jsonAssetTransfer{
		Inputs: []*jsonTransferInput{{
			AnchorPoint: restZeroOutpoint,
			AssetID:     assetIDHex,
			AssetType:   "NORMAL",
			ScriptKey:   pubKeyHex,
			Amount:      "100",
			GroupKey:    pubKeyHex,
		}},
		Outputs: []*jsonTransferOutput{{
			Amount:    "60",
			AssetID:   assetIDHex,
			AssetType: "NORMAL",
			ScriptKey: pubKeyHex,
			Anchor: &jsonAnchorInfo{
				Outpoint: restZeroOutpoint,
				Value:    "330",
			},
			GroupKey: pubKeyHex,
		}},
	})
	require.NoError(t, err)
	require.Len(t, transfer.Inputs, 1)
	require.Len(t, transfer.Outputs, 1)

	require.NotNil(t, transfer.Inputs[0].GroupKey)
	require.Equal(t, restTestPubKey, transfer.Inputs[0].GroupKey[:])
	require.Equal(t,
		entities.AssetTypeNormal, transfer.Inputs[0].AssetType)
	require.NotNil(t, transfer.Outputs[0].GroupKey)
	require.Equal(t, restTestPubKey, transfer.Outputs[0].GroupKey[:])
	require.Equal(t,
		entities.AssetTypeNormal, transfer.Outputs[0].AssetType)
}

func TestUnmarshalAssetTransferCollectionItem(t *testing.T) {
	pubKeyHex := hex.EncodeToString(restTestPubKey)
	assetIDHex := hex.EncodeToString(restTestAssetID)

	transfer, err := unmarshalAssetTransfer(&jsonAssetTransfer{
		Inputs: []*jsonTransferInput{{
			AnchorPoint: restZeroOutpoint,
			AssetID:     assetIDHex,
			AssetType:   "COLLECTIBLE",
			ScriptKey:   pubKeyHex,
			Amount:      "1",
			GroupKey:    pubKeyHex,
		}},
		Outputs: []*jsonTransferOutput{{
			Amount:    "1",
			AssetID:   assetIDHex,
			AssetType: "COLLECTIBLE",
			ScriptKey: pubKeyHex,
			Anchor: &jsonAnchorInfo{
				Outpoint: restZeroOutpoint,
				Value:    "330",
			},
			GroupKey: pubKeyHex,
		}},
	})
	require.NoError(t, err)

	var assetID entities.AssetID
	copy(assetID[:], restTestAssetID)
	wantRef := entities.AssetRefFromAssetID(assetID)

	highLevel := entities.NewTransfer(transfer)
	require.True(t, highLevel.Inputs[0].AssetRef.Equivalent(wantRef))
	require.True(t, highLevel.Outputs[0].AssetRef.Equivalent(wantRef))
	require.Equal(t,
		entities.AssetTypeCollectible, highLevel.Inputs[0].Type)
}

func TestUnmarshalAddrCollectionItemUsesAssetID(t *testing.T) {
	pubKeyHex := hex.EncodeToString(restTestPubKey)
	assetIDHex := hex.EncodeToString(restTestAssetID)

	addr, err := unmarshalAddr(&jsonAddr{
		Encoded:          "tap1collection",
		AssetID:          assetIDHex,
		AssetType:        "COLLECTIBLE",
		Amount:           "1",
		GroupKey:         pubKeyHex,
		ScriptKey:        pubKeyHex,
		InternalKey:      pubKeyHex,
		TaprootOutputKey: hex.EncodeToString(restTestAssetID),
		AssetVersion:     "ASSET_VERSION_V1",
		AddressVersion:   "ADDR_VERSION_V2",
	})
	require.NoError(t, err)

	var assetID entities.AssetID
	copy(assetID[:], restTestAssetID)
	require.True(t,
		addr.AssetRef.Equivalent(entities.AssetRefFromAssetID(assetID)))
}

func TestUnmarshalAddrCollectionAddressUsesGroupKey(t *testing.T) {
	pubKeyHex := hex.EncodeToString(restTestPubKey)

	addr, err := unmarshalAddr(&jsonAddr{
		Encoded:          "tap1collection",
		AssetID:          hex.EncodeToString(make([]byte, 32)),
		AssetType:        "COLLECTIBLE",
		Amount:           "1",
		GroupKey:         pubKeyHex,
		ScriptKey:        pubKeyHex,
		InternalKey:      pubKeyHex,
		TaprootOutputKey: hex.EncodeToString(restTestAssetID),
		AssetVersion:     "ASSET_VERSION_V1",
		AddressVersion:   "ADDR_VERSION_V2",
	})
	require.NoError(t, err)

	var groupKey entities.PubKey
	copy(groupKey[:], restTestPubKey)
	require.True(t,
		addr.AssetRef.Equivalent(entities.AssetRefFromGroupKey(groupKey)))
}

func TestUnmarshalAddrRejectsMissingAssetRef(t *testing.T) {
	pubKeyHex := hex.EncodeToString(restTestPubKey)

	_, err := unmarshalAddr(&jsonAddr{
		Encoded:          "tap1missingref",
		AssetType:        "NORMAL",
		Amount:           "1",
		ScriptKey:        pubKeyHex,
		InternalKey:      pubKeyHex,
		TaprootOutputKey: hex.EncodeToString(restTestAssetID),
		AssetVersion:     "ASSET_VERSION_V1",
		AddressVersion:   "ADDR_VERSION_V2",
	})
	require.ErrorContains(t, err, "missing asset ID or group key")
}

func TestUnmarshalAddrRejectsZeroAssetIDWithoutGroupKey(t *testing.T) {
	pubKeyHex := hex.EncodeToString(restTestPubKey)

	_, err := unmarshalAddr(&jsonAddr{
		Encoded:          "tap1zeroasset",
		AssetID:          hex.EncodeToString(make([]byte, 32)),
		AssetType:        "COLLECTIBLE",
		Amount:           "1",
		ScriptKey:        pubKeyHex,
		InternalKey:      pubKeyHex,
		TaprootOutputKey: hex.EncodeToString(restTestAssetID),
		AssetVersion:     "ASSET_VERSION_V1",
		AddressVersion:   "ADDR_VERSION_V2",
	})
	require.ErrorContains(t, err, "zero with no group key")
}
