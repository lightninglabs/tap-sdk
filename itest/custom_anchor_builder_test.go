//go:build itest

package itest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

const (
	customAnchorITestVersion  = int32(3)
	customAnchorITestSequence = uint32(0xffff_fffc)
	customAnchorITestFeeSat   = int64(250)
	customAnchorITestChildFee = int64(1_000)
)

// TestCustomAnchorBuilderEndToEnd exercises the exact caller-funded custom
// anchor lifecycle through both SDK transports. In particular, it proves that
// the caller's transaction version, sequence, and fee survive virtual signing,
// commitment, external anchor signing, and publishing unchanged.
func TestCustomAnchorBuilderEndToEnd(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf(
			"custom-anchor-%s", transport,
		))
		minted, err := h.CreateFungibleAndConfirm(
			t, ctx, name, 1_000,
		)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		inputProof, err := h.AliceWallet.ExportProofFile(
			ctx,
			tapsdk.AssetRefFromAssetID(
				minted.Asset.Genesis.IssuanceID,
			),
			minted.Asset.ScriptKey.PubKey, nil,
		)
		require.NoError(t, err)
		require.NotEmpty(t, inputProof.RawProofFile)

		proofFile, err := proof.DecodeFile(inputProof.RawProofFile)
		require.NoError(t, err)
		lastProof, err := proofFile.LastProof()
		require.NoError(t, err)
		require.NotNil(t, lastProof.InclusionProof.InternalKey)

		receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
		require.NoError(t, err)
		require.NotNil(t, receiverKeys)

		anchorPsbt, anchorValue := customAnchorITestPSBT(
			t, lastProof, receiverKeys.InternalKey,
		)
		anchorSigner, err := tapsdk.ParseTaprootPubKey(
			lastProof.InclusionProof.InternalKey.SerializeCompressed(),
		)
		require.NoError(t, err)
		request := &tapsdk.CustomAnchorRequest{
			Inputs: []tapsdk.CustomAssetInput{{
				ID:        "mint-output",
				AssetRef:  minted.Ref,
				Amount:    minted.Asset.Amount,
				ProofFile: inputProof.RawProofFile,
				Witness: tapsdk.CustomAssetWitnessPlan{
					Mode: tapsdk.CustomAssetWitnessBackendSigner,
				},
			}},
			Outputs: []tapsdk.CustomAssetOutput{{
				ID:                "bob-output",
				AssetRef:          minted.Ref,
				Amount:            minted.Asset.Amount,
				AnchorOutputIndex: 0,
				AnchorValueSat:    uint64(anchorValue),
				Script: tapsdk.CustomAssetScriptPlan{
					Mode: tapsdk.CustomAssetScriptExternal,
					External: &tapsdk.CustomAssetExternalScriptPlan{
						ScriptKey: receiverKeys.ScriptKey,
					},
				},
				Anchor: tapsdk.CustomAnchorOutputPlan{
					InternalKey: receiverKeys.InternalKey,
				},
			}},
			AnchorPSBT: anchorPsbt,
			Funding: tapsdk.CustomAnchorFundingPlan{
				Mode:              tapsdk.CustomAnchorFundingCallerFundedExact,
				CallerFundedExact: &tapsdk.CustomAnchorCallerFundedExact{},
			},
			PassiveAssets: tapsdk.CustomAnchorPassiveAssets{
				Policy: tapsdk.CustomAnchorPassiveReject,
			},
			LossPolicy: tapsdk.CustomAnchorLossPolicy{
				Mode: tapsdk.CustomAnchorLossReject,
			},
			SigningPlans: []tapsdk.CustomAnchorInputSigningPlan{{
				InputIndex: 0,
				KeyPath: &tapsdk.CustomAnchorKeyPathSigningPlan{
					Signer: anchorSigner.XOnly(),
				},
			}},
		}

		plan, err := h.AliceWallet.NewCustomAnchorTxBuilder().Build(
			ctx, request,
		)
		require.NoError(t, err)
		verification := plan.Verification()
		require.True(t, verification.Valid())
		require.NotEmpty(t, plan.ActiveVirtualPSBTs())
		requireCustomAnchorTxShape(t, plan.AnchorPSBT(), anchorValue)

		sealed, err := plan.Commit(ctx, tapsdk.CustomAnchorCommitOptions{
			Publish: tapsdk.CustomAnchorPublishMetadata{Label: name},
		})
		require.NoError(t, err)
		require.NoError(t, sealed.Validate())
		require.NotZero(t, sealed.PlanID)
		require.NotZero(t, sealed.CommittedPackageDigest)
		require.NotZero(t, sealed.UnsignedTxDigest)
		require.NotZero(t, sealed.PackageDigest)
		require.Len(t, sealed.Outputs, 1)
		require.Len(t, sealed.ProofUpdates, 1)
		require.NotEmpty(t, sealed.ProofUpdates[0].ProofBlob)
		require.Equal(t, receiverKeys.ScriptKey.PubKey,
			sealed.Outputs[0].ScriptKey)
		requireCustomAnchorTxShape(t, sealed.AnchorPsbt, anchorValue)

		signingRequests, err := sealed.SigningRequests()
		require.NoError(t, err)
		require.Len(t, signingRequests.KeyPath, 1)
		require.Empty(t, signingRequests.MuSig2)
		require.Empty(t, signingRequests.ScriptPath)
		require.Equal(t, anchorSigner.XOnly(),
			signingRequests.KeyPath[0].Signer)

		finalAnchorPsbt, err := newAliceLndAnchorSigner(t)(
			ctx, sealed.AnchorPsbt,
		)
		require.NoError(t, err)
		require.NoError(t,
			sealed.VerifyFinalAnchorPSBT(finalAnchorPsbt))
		requireCustomAnchorTxShape(t, finalAnchorPsbt, anchorValue)

		finalPacket := decodeCustomAnchorITestPSBT(t, finalAnchorPsbt)
		require.NotEmpty(t, finalPacket.Inputs[0].FinalScriptWitness)

		published, err := h.AliceWallet.PublishCustomAnchorTransfer(
			ctx, sealed, finalAnchorPsbt,
		)
		require.NoError(t, err)
		require.NotEmpty(t, published.AnchorTransaction)
		require.NotEmpty(t, published.VirtualTransactions)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		output := sealed.Outputs[0]
		completedProof := h.WaitForProofFile(
			t, ctx, h.AliceWallet,
			tapsdk.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)
		require.NotEmpty(t, completedProof.RawProofFile)

		h.EnableUniverseBootstrap(t, ctx)
		registered, err := h.BobWallet.ImportProofFile(
			ctx, completedProof,
		)
		require.NoError(t, err)
		require.Equal(t, output.IssuanceID, registered.IssuanceID)
		require.Equal(t, output.ScriptKey, registered.ScriptKey)
		require.Equal(t, minted.Asset.Amount, registered.Amount)

		bobBalance := h.WaitForBalance(
			t, ctx, h.BobWallet, minted.Ref, minted.Asset.Amount,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, minted.Asset.Amount, bobBalance)
	})
}

// TestCustomAnchorBuilderExternalP2AEndToEnd proves the external broadcaster
// flow used by hosts that retain ownership of Bitcoin fee management. Tapd
// commits and logs the transfer without broadcasting, while the caller signs,
// finalizes, and broadcasts the v3 parent containing the P2A hook.
func TestCustomAnchorBuilderExternalP2AEndToEnd(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf(
			"custom-anchor-p2a-%s", transport,
		))
		minted, err := h.CreateFungibleAndConfirm(
			t, ctx, name, 1_000,
		)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		inputProof, err := h.AliceWallet.ExportProofFile(
			ctx,
			tapsdk.AssetRefFromAssetID(
				minted.Asset.Genesis.IssuanceID,
			),
			minted.Asset.ScriptKey.PubKey, nil,
		)
		require.NoError(t, err)
		require.NotEmpty(t, inputProof.RawProofFile)

		proofFile, err := proof.DecodeFile(inputProof.RawProofFile)
		require.NoError(t, err)
		lastProof, err := proofFile.LastProof()
		require.NoError(t, err)
		require.NotNil(t, lastProof.InclusionProof.InternalKey)

		receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
		require.NoError(t, err)
		require.NotNil(t, receiverKeys)

		anchorPsbt, anchorValue := customAnchorP2AITestPSBT(
			t, lastProof, receiverKeys.InternalKey,
		)
		anchorSigner, err := tapsdk.ParseTaprootPubKey(
			lastProof.InclusionProof.InternalKey.SerializeCompressed(),
		)
		require.NoError(t, err)
		externalFeeBump := &tapsdk.CustomAnchorExternalP2AFeeBump{
			P2AOutputIndex: 1,
		}
		externalFundingMode :=
			tapsdk.CustomAnchorFundingExternalP2AFeeBump

		request := &tapsdk.CustomAnchorRequest{
			Inputs: []tapsdk.CustomAssetInput{{
				ID:        "mint-output",
				AssetRef:  minted.Ref,
				Amount:    minted.Asset.Amount,
				ProofFile: inputProof.RawProofFile,
				Witness: tapsdk.CustomAssetWitnessPlan{
					Mode: tapsdk.CustomAssetWitnessBackendSigner,
				},
			}},
			Outputs: []tapsdk.CustomAssetOutput{{
				ID:                "bob-output",
				AssetRef:          minted.Ref,
				Amount:            minted.Asset.Amount,
				AnchorOutputIndex: 0,
				AnchorValueSat:    uint64(anchorValue),
				Script: tapsdk.CustomAssetScriptPlan{
					Mode: tapsdk.CustomAssetScriptExternal,
					External: &tapsdk.CustomAssetExternalScriptPlan{
						ScriptKey: receiverKeys.ScriptKey,
					},
				},
				Anchor: tapsdk.CustomAnchorOutputPlan{
					InternalKey: receiverKeys.InternalKey,
				},
			}},
			AnchorPSBT: anchorPsbt,
			Funding: tapsdk.CustomAnchorFundingPlan{
				Mode:               externalFundingMode,
				ExternalP2AFeeBump: externalFeeBump,
			},
			PassiveAssets: tapsdk.CustomAnchorPassiveAssets{
				Policy: tapsdk.CustomAnchorPassiveReject,
			},
			LossPolicy: tapsdk.CustomAnchorLossPolicy{
				Mode: tapsdk.CustomAnchorLossReject,
			},
			SigningPlans: []tapsdk.CustomAnchorInputSigningPlan{{
				InputIndex: 0,
				KeyPath: &tapsdk.CustomAnchorKeyPathSigningPlan{
					Signer: anchorSigner.XOnly(),
				},
			}},
		}

		plan, err := h.AliceWallet.NewCustomAnchorTxBuilder().Build(
			ctx, request,
		)
		require.NoError(t, err)
		verification := plan.Verification()
		require.True(t, verification.Valid())
		require.NotEmpty(t, plan.ActiveVirtualPSBTs())
		requireCustomAnchorP2ATxShape(
			t, plan.AnchorPSBT(), anchorValue,
		)

		sealed, err := plan.Commit(ctx, tapsdk.CustomAnchorCommitOptions{
			Publish: tapsdk.CustomAnchorPublishMetadata{
				SkipAnchorTxBroadcast: true,
				Label:                 name,
				ExternalBroadcast:     true,
			},
		})
		require.NoError(t, err)
		require.NoError(t, sealed.Validate())
		require.True(t, sealed.Publish.SkipAnchorTxBroadcast)
		require.True(t, sealed.Publish.ExternalBroadcast)
		require.Len(t, sealed.Outputs, 1)
		require.Len(t, sealed.ProofUpdates, 1)
		require.NotEmpty(t, sealed.ProofUpdates[0].ProofBlob)
		requireCustomAnchorP2ATxShape(
			t, sealed.AnchorPsbt, anchorValue,
		)

		signingRequests, err := sealed.SigningRequests()
		require.NoError(t, err)
		require.Len(t, signingRequests.KeyPath, 1)
		require.Empty(t, signingRequests.MuSig2)
		require.Empty(t, signingRequests.ScriptPath)
		require.Equal(t, anchorSigner.XOnly(),
			signingRequests.KeyPath[0].Signer)

		finalAnchorPsbt, err := newAliceLndAnchorSigner(t)(
			ctx, sealed.AnchorPsbt,
		)
		require.NoError(t, err)
		require.NoError(t,
			sealed.VerifyFinalAnchorPSBT(finalAnchorPsbt))
		requireCustomAnchorP2ATxShape(
			t, finalAnchorPsbt, anchorValue,
		)

		finalPacket := decodeCustomAnchorITestPSBT(
			t, finalAnchorPsbt,
		)
		require.NotEmpty(t, finalPacket.Inputs[0].FinalScriptWitness)
		require.NotNil(t, finalPacket.Inputs[0].WitnessUtxo)
		var parentOutputValue int64
		for _, txOut := range finalPacket.UnsignedTx.TxOut {
			parentOutputValue += txOut.Value
		}
		require.Equal(t, parentOutputValue,
			finalPacket.Inputs[0].WitnessUtxo.Value,
			"ephemeral P2A parent must pay zero fee")

		logged, err := h.AliceWallet.PublishCustomAnchorTransfer(
			ctx, sealed, finalAnchorPsbt,
		)
		require.NoError(t, err)
		require.NotEmpty(t, logged.AnchorTransaction)
		require.NotEmpty(t, logged.VirtualTransactions)

		// A zero-value ephemeral P2A output requires the parent to pay zero
		// fee. The external fee wallet therefore attaches and signs a v3
		// child, then submits the package outside tapd.
		finalTx, err := psbt.Extract(finalPacket)
		require.NoError(t, err)
		broadcastCustomAnchorP2APackage(t, h, finalTx, 1)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		output := sealed.Outputs[0]
		completedProof := h.WaitForProofFile(
			t, ctx, h.AliceWallet,
			tapsdk.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)
		require.NotEmpty(t, completedProof.RawProofFile)

		h.EnableUniverseBootstrap(t, ctx)
		registered, err := h.BobWallet.ImportProofFile(
			ctx, completedProof,
		)
		require.NoError(t, err)
		require.Equal(t, output.IssuanceID, registered.IssuanceID)
		require.Equal(t, output.ScriptKey, registered.ScriptKey)
		require.Equal(t, minted.Asset.Amount, registered.Amount)

		bobBalance := h.WaitForBalance(
			t, ctx, h.BobWallet, minted.Ref, minted.Asset.Amount,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, minted.Asset.Amount, bobBalance)
	})
}

func customAnchorITestPSBT(t testing.TB, inputProof *proof.Proof,
	receiverInternalKey tapsdk.InternalKey) ([]byte, int64) {

	t.Helper()

	inputOutpoint := inputProof.OutPoint()
	inputTxOut := inputProof.AnchorTx.TxOut[inputOutpoint.Index]
	require.Greater(t, inputTxOut.Value, customAnchorITestFeeSat)

	anchorValue := inputTxOut.Value - customAnchorITestFeeSat
	require.GreaterOrEqual(t, anchorValue, int64(330))

	internalKey, err := btcec.ParsePubKey(receiverInternalKey.PubKey[:])
	require.NoError(t, err)
	outputKey := txscript.ComputeTaprootKeyNoScript(internalKey)
	placeholderScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)

	anchorTx := wire.NewMsgTx(customAnchorITestVersion)
	anchorTx.LockTime = 21
	anchorTx.AddTxIn(wire.NewTxIn(&inputOutpoint, nil, nil))
	anchorTx.TxIn[0].Sequence = customAnchorITestSequence
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    anchorValue,
		PkScript: placeholderScript,
	})

	packet, err := psbt.NewFromUnsignedTx(anchorTx)
	require.NoError(t, err)

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes(), anchorValue
}

func customAnchorP2AITestPSBT(t testing.TB, inputProof *proof.Proof,
	receiverInternalKey tapsdk.InternalKey) ([]byte, int64) {

	t.Helper()

	inputOutpoint := inputProof.OutPoint()
	inputTxOut := inputProof.AnchorTx.TxOut[inputOutpoint.Index]
	require.Greater(t, inputTxOut.Value, customAnchorITestFeeSat)

	anchorValue := inputTxOut.Value
	require.GreaterOrEqual(t, anchorValue, int64(330))

	internalKey, err := btcec.ParsePubKey(receiverInternalKey.PubKey[:])
	require.NoError(t, err)
	outputKey := txscript.ComputeTaprootKeyNoScript(internalKey)
	placeholderScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)

	anchorTx := wire.NewMsgTx(customAnchorITestVersion)
	anchorTx.LockTime = 21
	anchorTx.AddTxIn(wire.NewTxIn(&inputOutpoint, nil, nil))
	anchorTx.TxIn[0].Sequence = customAnchorITestSequence
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    anchorValue,
		PkScript: placeholderScript,
	})
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    0,
		PkScript: customAnchorITestP2AScript(),
	})

	packet, err := psbt.NewFromUnsignedTx(anchorTx)
	require.NoError(t, err)

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes(), anchorValue
}

func requireCustomAnchorTxShape(t testing.TB, rawPsbt []byte,
	anchorValue int64) {

	t.Helper()
	packet := decodeCustomAnchorITestPSBT(t, rawPsbt)
	require.Equal(t, customAnchorITestVersion,
		packet.UnsignedTx.Version)
	require.Equal(t, uint32(21), packet.UnsignedTx.LockTime)
	require.Len(t, packet.UnsignedTx.TxIn, 1)
	require.Equal(t, customAnchorITestSequence,
		packet.UnsignedTx.TxIn[0].Sequence)
	require.Len(t, packet.UnsignedTx.TxOut, 1)
	require.Equal(t, anchorValue, packet.UnsignedTx.TxOut[0].Value)
}

func requireCustomAnchorP2ATxShape(t testing.TB, rawPsbt []byte,
	anchorValue int64) {

	t.Helper()
	packet := decodeCustomAnchorITestPSBT(t, rawPsbt)
	require.Equal(t, customAnchorITestVersion,
		packet.UnsignedTx.Version)
	require.Equal(t, uint32(21), packet.UnsignedTx.LockTime)
	require.Len(t, packet.UnsignedTx.TxIn, 1)
	require.Equal(t, customAnchorITestSequence,
		packet.UnsignedTx.TxIn[0].Sequence)
	require.Len(t, packet.UnsignedTx.TxOut, 2)
	require.Equal(t, anchorValue,
		packet.UnsignedTx.TxOut[0].Value)
	require.Zero(t, packet.UnsignedTx.TxOut[1].Value)
	require.Equal(t, customAnchorITestP2AScript(),
		packet.UnsignedTx.TxOut[1].PkScript)
}

func customAnchorITestP2AScript() []byte {
	return []byte{txscript.OP_1, 0x02, 0x4e, 0x73}
}

func broadcastCustomAnchorP2APackage(t testing.TB, h *TestHarness,
	parent *wire.MsgTx, p2aOutputIndex uint32) {

	t.Helper()
	require.Equal(t, customAnchorITestVersion, parent.Version)
	require.Less(t, p2aOutputIndex, uint32(len(parent.TxOut)))
	p2aOutput := parent.TxOut[p2aOutputIndex]
	require.Zero(t, p2aOutput.Value)
	require.Equal(t, customAnchorITestP2AScript(), p2aOutput.PkScript)

	utxo := customAnchorITestMinerUTXO(t, h)
	utxoAmount, err := btcutil.NewAmount(utxo.Amount)
	require.NoError(t, err)
	require.Greater(t, int64(utxoAmount), customAnchorITestChildFee)

	addressJSON := h.bitcoindRPCWallet(
		t, "miner", "getnewaddress", `""`, `"bech32"`,
	)
	var address string
	require.NoError(t, json.Unmarshal([]byte(addressJSON), &address))
	require.NotEmpty(t, address)

	addressInfoJSON := h.bitcoindRPCWallet(
		t, "miner", "getaddressinfo", fmt.Sprintf("%q", address),
	)
	var addressInfo struct {
		ScriptPubKey string `json:"scriptPubKey"`
	}
	require.NoError(t, json.Unmarshal(
		[]byte(addressInfoJSON), &addressInfo,
	))
	outputScript, err := hex.DecodeString(addressInfo.ScriptPubKey)
	require.NoError(t, err)

	minerHash, err := chainhash.NewHashFromStr(utxo.TxID)
	require.NoError(t, err)
	parentHash := parent.TxHash()
	child := wire.NewMsgTx(customAnchorITestVersion)
	child.AddTxIn(wire.NewTxIn(&wire.OutPoint{
		Hash:  parentHash,
		Index: p2aOutputIndex,
	}, nil, nil))
	child.AddTxIn(wire.NewTxIn(&wire.OutPoint{
		Hash:  *minerHash,
		Index: utxo.Vout,
	}, nil, nil))
	for _, txIn := range child.TxIn {
		txIn.Sequence = customAnchorITestSequence
	}
	child.AddTxOut(&wire.TxOut{
		Value:    int64(utxoAmount) - customAnchorITestChildFee,
		PkScript: outputScript,
	})

	var unsignedChild bytes.Buffer
	require.NoError(t, child.Serialize(&unsignedChild))
	prevOutputs := fmt.Sprintf(
		`[{"txid":%q,"vout":%d,"scriptPubKey":%q,"amount":0}]`,
		parentHash.String(), p2aOutputIndex,
		hex.EncodeToString(customAnchorITestP2AScript()),
	)
	signedChildJSON := h.bitcoindRPCWallet(
		t, "miner", "signrawtransactionwithwallet",
		fmt.Sprintf("%q", hex.EncodeToString(unsignedChild.Bytes())),
		prevOutputs,
	)
	var signedChild struct {
		Hex      string `json:"hex"`
		Complete bool   `json:"complete"`
		Errors   []any  `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(
		[]byte(signedChildJSON), &signedChild,
	))
	require.True(t, signedChild.Complete, "%v", signedChild.Errors)
	require.NotEmpty(t, signedChild.Hex)
	signedChildBytes, err := hex.DecodeString(signedChild.Hex)
	require.NoError(t, err)
	var signedChildTx wire.MsgTx
	require.NoError(t, signedChildTx.Deserialize(
		bytes.NewReader(signedChildBytes),
	))
	require.Equal(t, customAnchorITestVersion, signedChildTx.Version)
	require.Len(t, signedChildTx.TxIn, 2)
	require.Equal(t, parentHash,
		signedChildTx.TxIn[0].PreviousOutPoint.Hash)
	require.Equal(t, p2aOutputIndex,
		signedChildTx.TxIn[0].PreviousOutPoint.Index)
	require.Empty(t, signedChildTx.TxIn[0].SignatureScript)
	require.Empty(t, signedChildTx.TxIn[0].Witness)
	require.NotEmpty(t, signedChildTx.TxIn[1].Witness)

	var parentBuffer bytes.Buffer
	require.NoError(t, parent.Serialize(&parentBuffer))
	packageJSON := h.bitcoindCurlRPC(
		t, "submitpackage", fmt.Sprintf(
			"[%q,%q]", hex.EncodeToString(parentBuffer.Bytes()),
			signedChild.Hex,
		),
	)
	var packageResult struct {
		Message string `json:"package_msg"`
		Results map[string]struct {
			TxID  string `json:"txid"`
			Error string `json:"error"`
		} `json:"tx-results"`
	}
	require.NoError(t, json.Unmarshal(
		[]byte(packageJSON), &packageResult,
	))
	require.Equal(t, "success", packageResult.Message, "%+v",
		packageResult.Results)
	require.Len(t, packageResult.Results, 2)
	wantTxIDs := map[string]struct{}{
		parentHash.String():             {},
		signedChildTx.TxHash().String(): {},
	}
	for _, result := range packageResult.Results {
		require.Empty(t, result.Error)
		_, ok := wantTxIDs[result.TxID]
		require.True(t, ok, "unexpected package txid %s", result.TxID)
		delete(wantTxIDs, result.TxID)
	}
	require.Empty(t, wantTxIDs)
}

type customAnchorMinerUTXO struct {
	TxID   string  `json:"txid"`
	Vout   uint32  `json:"vout"`
	Amount float64 `json:"amount"`
}

func customAnchorITestMinerUTXO(t testing.TB,
	h *TestHarness) customAnchorMinerUTXO {

	t.Helper()
	listJSON := h.bitcoindRPCWallet(
		t, "miner", "listunspent", "1", "9999999",
	)
	var utxos []customAnchorMinerUTXO
	require.NoError(t, json.Unmarshal([]byte(listJSON), &utxos))
	for _, utxo := range utxos {
		amount, err := btcutil.NewAmount(utxo.Amount)
		require.NoError(t, err)
		if int64(amount) > customAnchorITestChildFee {
			return utxo
		}
	}

	require.FailNow(t, "miner wallet has no confirmed fee-bump UTXO")
	return customAnchorMinerUTXO{}
}

func decodeCustomAnchorITestPSBT(t testing.TB,
	rawPsbt []byte) *psbt.Packet {

	t.Helper()
	packet, err := psbt.NewFromRawBytes(bytes.NewReader(rawPsbt), false)
	require.NoError(t, err)

	return packet
}
