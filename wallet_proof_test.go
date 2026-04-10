package tapsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

func testAssetID(fill byte) entities.AssetID {
	var id entities.AssetID
	for i := range id {
		id[i] = fill
	}

	return id
}

func testPubKey(fill byte) entities.PubKey {
	var key entities.PubKey
	key[0] = 0x02
	for i := 1; i < len(key); i++ {
		key[i] = fill
	}

	return key
}

func testOutpoint(fill byte, index uint32) entities.Outpoint {
	var txid [32]byte
	for i := range txid {
		txid[i] = fill
	}

	return entities.Outpoint{
		Txid:  txid,
		Index: index,
	}
}

func TestImportProof(t *testing.T) {
	type decodeStep struct {
		rawProof []byte
		decoded  *entities.DecodedProof
		err      error
	}

	type insertStep struct {
		rawProof []byte
		decoded  *entities.DecodedProof
		err      error
	}

	groupKey := testPubKey(0x33)
	decodedOne := &entities.DecodedProof{
		AssetID:   testAssetID(0x11),
		ScriptKey: testPubKey(0x21),
		Amount:    5,
		Outpoint:  testOutpoint(0x41, 0),
	}
	decodedTwo := &entities.DecodedProof{
		AssetID:   testAssetID(0x12),
		GroupKey:  &groupKey,
		ScriptKey: testPubKey(0x22),
		Amount:    15,
		Outpoint:  testOutpoint(0x42, 1),
	}
	registered := &entities.RegisteredAsset{
		AssetID:   decodedTwo.AssetID,
		ScriptKey: decodedTwo.ScriptKey,
		Amount:    decodedTwo.Amount,
		Outpoint:  decodedTwo.Outpoint,
	}

	proofFile := &entities.ProofFile{
		RawProofFile: []byte{0xaa, 0xbb, 0xcc},
	}
	proofOne := []byte{0x01}
	proofTwo := []byte{0x02}

	decodeErr := errors.New("decode failed")
	insertErr := errors.New("insert failed")
	registerErr := errors.New("register failed")
	unpackErr := errors.New("unpack failed")

	tests := []struct {
		name           string
		unpackProofs   [][]byte
		unpackErr      error
		decodeSteps    []decodeStep
		insertSteps    []insertStep
		registerResult *entities.RegisteredAsset
		registerErr    error
		wantResult     *entities.RegisteredAsset
		wantErr        string
	}{
		{
			name:         "success",
			unpackProofs: [][]byte{proofOne, proofTwo},
			decodeSteps: []decodeStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					decoded:  decodedTwo,
				},
			},
			insertSteps: []insertStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					decoded:  decodedTwo,
				},
			},
			registerResult: registered,
			wantResult:     registered,
		},
		{
			name:      "unpack error",
			unpackErr: unpackErr,
			wantErr:   "unpack failed",
		},
		{
			name:         "empty proof file",
			unpackProofs: [][]byte{},
			wantErr:      "proof file contains no proofs",
		},
		{
			name:         "decode error",
			unpackProofs: [][]byte{proofOne, proofTwo},
			decodeSteps: []decodeStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					err:      decodeErr,
				},
			},
			insertSteps: []insertStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
			},
			wantErr: "decode failed",
		},
		{
			name:         "insert error",
			unpackProofs: [][]byte{proofOne, proofTwo},
			decodeSteps: []decodeStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					decoded:  decodedTwo,
				},
			},
			insertSteps: []insertStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					decoded:  decodedTwo,
					err:      insertErr,
				},
			},
			wantErr: "insert failed",
		},
		{
			name:         "register error",
			unpackProofs: [][]byte{proofOne, proofTwo},
			decodeSteps: []decodeStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					decoded:  decodedTwo,
				},
			},
			insertSteps: []insertStep{
				{
					rawProof: proofOne,
					decoded:  decodedOne,
				},
				{
					rawProof: proofTwo,
					decoded:  decodedTwo,
				},
			},
			registerErr: registerErr,
			wantErr:     "register failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mc := new(mockClient)
			w := NewWallet(mc, entities.NetworkRegtest)
			ctx := context.Background()

			mc.On("UnpackProofFile", ctx, proofFile.RawProofFile).Return(
				test.unpackProofs, test.unpackErr,
			).Once()

			for _, step := range test.decodeSteps {
				mc.On("DecodeProof", ctx, step.rawProof).Return(
					step.decoded, step.err,
				).Once()
			}

			for _, step := range test.insertSteps {
				mc.On("InsertProof", ctx, step.rawProof, step.decoded).Return(
					step.err,
				).Once()
			}

			canRegister := test.unpackErr == nil &&
				len(test.decodeSteps) > 0 &&
				len(test.insertSteps) == len(test.decodeSteps) &&
				test.decodeSteps[len(test.decodeSteps)-1].err == nil &&
				test.insertSteps[len(test.insertSteps)-1].err == nil
			if canRegister {
				lastDecoded := test.decodeSteps[len(test.decodeSteps)-1].decoded
				mc.On(
					"RegisterTransfer", ctx,
					lastDecoded.AssetID,
					lastDecoded.GroupKey,
					lastDecoded.ScriptKey,
					lastDecoded.Outpoint,
				).Return(test.registerResult, test.registerErr).Once()
			}

			result, err := w.ImportProof(ctx, proofFile)
			if test.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, test.wantErr)

				var sdkErr *Error
				require.ErrorAs(t, err, &sdkErr)
				require.Equal(t, "ImportProof", sdkErr.Op)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantResult, result)
			}

			mc.AssertExpectations(t)
		})
	}
}
