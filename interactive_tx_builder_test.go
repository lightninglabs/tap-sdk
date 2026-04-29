package tapsdk

import (
	"bytes"
	"context"
	"testing"

	btcpsbt "github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/lightninglabs/tap-sdk/codec"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInteractiveTxBuilder_Execute(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()

	// Test data
	var assetID entities.AssetID
	copy(assetID[:], []byte("asset_id_32_bytes_long_enough!!!"))

	var scriptKeyPubKey entities.PubKey
	copy(scriptKeyPubKey[:], []byte("script_key_pubkey_33_bytes_long"))

	var internalKeyPubKey entities.PubKey
	copy(internalKeyPubKey[:], []byte("internal_key_pubkey_33_bytes_lo"))

	receiverKeys := entities.DerivedKeys{
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKeyPubKey,
		},
		InternalKey: entities.InternalKey{
			PubKey: internalKeyPubKey,
			KeyLocator: entities.KeyLocator{
				Family: 212,
				Index:  5,
			},
		},
	}

	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")

	var transferTxid [32]byte
	copy(transferTxid[:], []byte("txid_hash_32_bytes_long_enough!!"))

	expectedResult := &entities.AssetTransfer{
		TransferTxid: transferTxid,
		AnchorTx:     []byte("anchor_tx_bytes"),
		Outputs: []entities.TransferOutput{
			{
				AnchorOutpoint: entities.Outpoint{Txid: transferTxid, Index: 0},
				AnchorValue:    10,
				ScriptKey:      scriptKeyPubKey,
				Amount:         1000,
				ProofBlob:      []byte("proof_blob"),
			},
		},
	}

	// 1. Fund interactive PSBT (matches any PSBT since we build it internally)
	mockWalletKit.On("FundInteractivePsbt", ctx, mock.Anything).Return(
		&entities.FundedTransfer{
			FundedPsbt: fundedPsbt,
		}, nil)

	// 2. Sign
	mockWalletKit.On("SignVirtualPsbt", ctx, fundedPsbt).Return(
		signedPsbt, nil)

	// 3. Anchor (Complete)
	mockWalletKit.On("AnchorVirtualPsbts", ctx, [][]byte{signedPsbt}).Return(
		expectedResult, nil)

	// Execute interactive send
	builder := newInteractiveTxBuilder(mockWalletKit, "tapassetr", 1)
	builder.SetAsset(entities.AssetRefFromAssetID(assetID), 1000).
		SetReceiverKeys(receiverKeys)

	result, err := builder.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedResult.TransferTxid, result.TransferTxid)
	require.Equal(t, expectedResult.AnchorTx, result.AnchorTx)
	require.Len(t, result.Outputs, 1)
	require.Equal(t,
		expectedResult.Outputs[0].AnchorOutpoint,
		result.Outputs[0].AnchorOutpoint,
	)
	require.Equal(t,
		expectedResult.Outputs[0].AnchorValue,
		result.Outputs[0].AnchorValue,
	)

	mockWalletKit.AssertExpectations(t)
}

type interactiveResolverMock struct {
	*MockWalletKitClient

	assets []*entities.Asset
	err    error
	gotReq *entities.ListAssetsRequest
}

func (m *interactiveResolverMock) ListAssets(_ context.Context,
	req *entities.ListAssetsRequest) ([]*entities.Asset, error) {

	m.gotReq = req

	return m.assets, m.err
}

func TestInteractiveTxBuilder_GroupRefResolvesSpendableIssuance(
	t *testing.T) {

	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()

	var smallID entities.AssetID
	copy(smallID[:], []byte("small_asset_id_32_bytes_long!!!!"))

	var selectedID entities.AssetID
	copy(selectedID[:], []byte("selected_asset_id_32_bytes_long!"))

	groupRef := entities.AssetRefFromGroupKey(testRefGroupKey(t))
	resolver := &interactiveResolverMock{
		MockWalletKitClient: mockWalletKit,
		assets: []*entities.Asset{
			{
				AssetRef: groupRef,
				Genesis: entities.AssetGenesis{
					IssuanceID: smallID,
				},
				Amount: 250,
			},
			{
				AssetRef: groupRef,
				Genesis: entities.AssetGenesis{
					IssuanceID: selectedID,
				},
				Amount: 1000,
			},
		},
	}

	var scriptKeyPubKey entities.PubKey
	copy(scriptKeyPubKey[:], []byte("script_key_pubkey_33_bytes_long"))

	var internalKeyPubKey entities.PubKey
	copy(internalKeyPubKey[:], []byte("internal_key_pubkey_33_bytes_lo"))

	receiverKeys := entities.DerivedKeys{
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKeyPubKey,
		},
		InternalKey: entities.InternalKey{
			PubKey: internalKeyPubKey,
			KeyLocator: entities.KeyLocator{
				Family: 212,
				Index:  5,
			},
		},
	}

	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")
	expectedResult := &entities.AssetTransfer{
		AnchorTx: []byte("anchor_tx_bytes"),
	}

	var capturedPsbt []byte
	mockWalletKit.On("FundInteractivePsbt", ctx, mock.Anything).Run(
		func(args mock.Arguments) {
			psbtBytes := args.Get(1).([]byte)
			capturedPsbt = append([]byte(nil), psbtBytes...)
		},
	).Return(&entities.FundedTransfer{
		FundedPsbt: fundedPsbt,
	}, nil)
	mockWalletKit.On("SignVirtualPsbt", ctx, fundedPsbt).Return(
		signedPsbt, nil)
	mockWalletKit.On("AnchorVirtualPsbts", ctx, [][]byte{signedPsbt}).Return(
		expectedResult, nil)

	builder := newInteractiveTxBuilder(resolver, "tapassetr", 1)
	builder.SetAsset(groupRef, 500).SetReceiverKeys(receiverKeys)

	result, err := builder.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedResult, result)
	require.NotNil(t, resolver.gotReq)
	require.Equal(t, groupRef, *resolver.gotReq.AssetRef)

	requireInteractiveInputAssetID(t, capturedPsbt, selectedID)

	mockWalletKit.AssertExpectations(t)
}

func TestInteractiveTxBuilder_WithAltLeaves(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()

	var assetID entities.AssetID
	copy(assetID[:], []byte("asset_id_32_bytes_long_enough!!!"))

	var scriptKeyPubKey entities.PubKey
	copy(scriptKeyPubKey[:], []byte("script_key_pubkey_33_bytes_long"))

	var internalKeyPubKey entities.PubKey
	copy(internalKeyPubKey[:], []byte("internal_key_pubkey_33_bytes_lo"))

	receiverKeys := entities.DerivedKeys{
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKeyPubKey,
		},
		InternalKey: entities.InternalKey{
			PubKey: internalKeyPubKey,
			KeyLocator: entities.KeyLocator{
				Family: 212,
				Index:  5,
			},
		},
	}

	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")

	var transferTxid [32]byte
	copy(transferTxid[:], []byte("txid_hash_32_bytes_long_enough!!"))

	expectedResult := &entities.AssetTransfer{
		TransferTxid: transferTxid,
		AnchorTx:     []byte("anchor_tx_bytes"),
		Outputs: []entities.TransferOutput{
			{
				AnchorOutpoint: entities.Outpoint{Txid: transferTxid, Index: 0},
				AnchorValue:    10,
				ScriptKey:      scriptKeyPubKey,
				Amount:         1000,
				ProofBlob:      []byte("proof_blob"),
			},
		},
	}

	altLeaves := [][]byte{
		[]byte("alt_leaf_1"),
		[]byte("alt_leaf_2"),
	}

	var capturedPsbt []byte

	// 1. Fund interactive PSBT and capture the vPSBT bytes to inspect alt leaves.
	mockWalletKit.On("FundInteractivePsbt", ctx, mock.Anything).Run(
		func(args mock.Arguments) {
			psbtBytes := args.Get(1).([]byte)
			capturedPsbt = append([]byte(nil), psbtBytes...)
		},
	).Return(&entities.FundedTransfer{
		FundedPsbt: fundedPsbt,
	}, nil)

	// 2. Sign
	mockWalletKit.On("SignVirtualPsbt", ctx, fundedPsbt).Return(
		signedPsbt, nil)

	// 3. Anchor (Complete)
	mockWalletKit.On("AnchorVirtualPsbts", ctx, [][]byte{signedPsbt}).Return(
		expectedResult, nil)

	builder := newInteractiveTxBuilder(mockWalletKit, "tapassetr", 1)
	builder.SetAsset(entities.AssetRefFromAssetID(assetID), 1000).
		SetReceiverKeys(receiverKeys).
		WithAltLeaves(receiverKeys.ScriptKey.PubKey, altLeaves)

	result, err := builder.Execute(ctx)
	require.NoError(t, err)

	// Validate AssetTransfer passthrough.
	require.Equal(t, expectedResult.TransferTxid, result.TransferTxid)
	require.Equal(t, expectedResult.AnchorTx, result.AnchorTx)
	require.Len(t, result.Outputs, 1)
	require.Equal(t,
		expectedResult.Outputs[0].AnchorOutpoint,
		result.Outputs[0].AnchorOutpoint,
	)
	require.Equal(t,
		expectedResult.Outputs[0].AnchorValue,
		result.Outputs[0].AnchorValue,
	)

	// Validate alt leaves were encoded into the vPSBT.
	require.NotEmpty(t, capturedPsbt)

	packet, err := btcpsbt.NewFromRawBytes(bytes.NewReader(capturedPsbt), false)
	require.NoError(t, err)
	require.Len(t, packet.Outputs, 1)

	var encodedAltLeaves []byte
	for _, u := range packet.Outputs[0].Unknowns {
		if bytes.Equal(u.Key, []byte{0x7e}) {
			encodedAltLeaves = u.Value
			break
		}
	}

	require.NotNil(t, encodedAltLeaves)

	decodedLeaves, err := codec.DecodeAltLeaves(encodedAltLeaves)
	require.NoError(t, err)
	require.Equal(t, altLeaves, decodedLeaves)

	mockWalletKit.AssertExpectations(t)
}

func requireInteractiveInputAssetID(t *testing.T, rawPSBT []byte,
	want entities.AssetID) {

	t.Helper()

	packet, err := btcpsbt.NewFromRawBytes(bytes.NewReader(rawPSBT), false)
	require.NoError(t, err)
	require.Len(t, packet.Inputs, 1)

	var prevID []byte
	for _, u := range packet.Inputs[0].Unknowns {
		if bytes.Equal(u.Key, []byte{0x70}) {
			prevID = u.Value
			break
		}
	}

	require.NotNil(t, prevID)
	require.GreaterOrEqual(t, len(prevID), 68)

	assetID, err := entities.ParseAssetID(prevID[36:68])
	require.NoError(t, err)
	require.Equal(t, want, assetID)
}

func TestInteractiveTxBuilder_Validation(t *testing.T) {
	services := &Wallet{
		networkHRP: "tapassetr",
		coinType:   1,
	}

	ctx := context.Background()

	// Test missing receiver keys
	builder := services.NewInteractiveTxBuilder()
	builder.SetAsset(entities.AssetRefFromAssetID(entities.AssetID{1, 2, 3}), 1000)

	_, err := builder.Execute(ctx)
	require.ErrorIs(t, err, ErrNoReceiverKeys)

	// Test missing asset ID
	builder2 := services.NewInteractiveTxBuilder()
	builder2.SetReceiverKeys(entities.DerivedKeys{})

	_, err = builder2.Execute(ctx)
	require.ErrorIs(t, err, ErrNoAssetID)

	// Test zero amount
	var assetID entities.AssetID
	copy(assetID[:], []byte("asset_id_32_bytes_long_enough!!!"))

	builder3 := services.NewInteractiveTxBuilder()
	builder3.SetAsset(entities.AssetRefFromAssetID(assetID), 0).
		SetReceiverKeys(entities.DerivedKeys{
			ScriptKey: entities.ScriptKey{
				PubKey: entities.PubKey{1},
			},
			InternalKey: entities.InternalKey{
				PubKey: entities.PubKey{1},
			},
		})

	_, err = builder3.Execute(ctx)
	require.ErrorIs(t, err, ErrZeroAmount)

	builder4 := newInteractiveTxBuilder(
		new(MockWalletKitClient), "tapassetr", 1,
	)
	builder4.SetAsset(
		entities.AssetRefFromGroupKey(testRefGroupKey(t)), 1000,
	).SetReceiverKeys(entities.DerivedKeys{
		ScriptKey: entities.ScriptKey{
			PubKey: entities.PubKey{1},
		},
		InternalKey: entities.InternalKey{
			PubKey: entities.PubKey{1},
		},
	})

	_, err = builder4.Execute(ctx)
	require.ErrorIs(t, err, ErrGroupKeyNotSupported)
}

func TestInteractiveTxBuilder_AlreadyFinished(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()

	var assetID entities.AssetID
	copy(assetID[:], []byte("asset_id_32_bytes_long_enough!!!"))

	var scriptKeyPubKey entities.PubKey
	copy(scriptKeyPubKey[:], []byte("script_key_pubkey_33_bytes_long"))

	var internalKeyPubKey entities.PubKey
	copy(internalKeyPubKey[:], []byte("internal_key_pubkey_33_bytes_lo"))

	receiverKeys := entities.DerivedKeys{
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKeyPubKey,
		},
		InternalKey: entities.InternalKey{
			PubKey:     internalKeyPubKey,
			KeyLocator: entities.KeyLocator{Family: 212, Index: 5},
		},
	}

	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")

	expectedResult := &entities.AssetTransfer{
		AnchorTx: []byte("anchor_tx_bytes"),
	}

	mockWalletKit.On("FundInteractivePsbt", ctx, mock.Anything).Return(
		&entities.FundedTransfer{FundedPsbt: fundedPsbt}, nil)
	mockWalletKit.On("SignVirtualPsbt", ctx, fundedPsbt).Return(
		signedPsbt, nil)
	mockWalletKit.On("AnchorVirtualPsbts", ctx, [][]byte{signedPsbt}).Return(
		expectedResult, nil)

	builder := newInteractiveTxBuilder(mockWalletKit, "tapassetr", 1)
	builder.SetAsset(
		entities.AssetRefFromAssetID(assetID), 1000,
	).SetReceiverKeys(receiverKeys)

	// First execution succeeds
	_, err := builder.Execute(ctx)
	require.NoError(t, err)

	// Second execution fails
	_, err = builder.Execute(ctx)
	require.ErrorIs(t, err, ErrBuilderFinished)

	mockWalletKit.AssertExpectations(t)
}
