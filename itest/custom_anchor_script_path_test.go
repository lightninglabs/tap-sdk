//go:build itest

package itest

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

// customAnchorCSVDelay is the relative block delay committed by the timeout
// leaf. It must be small enough to mature quickly in the itest yet large
// enough that the spend is only valid after explicit maturation.
const customAnchorCSVDelay = int64(4)

// TestCustomAnchorBuilderScriptPathTimeoutSweep proves the generic
// custom-anchor capability that Ark-style unilateral exits depend on, without
// any Ark-specific concept: an asset output can commit a tapscript timeout leaf
// as a sibling to the asset commitment, be spent later purely through that
// script path, and yield a valid, non-burned proof. The asset script key is
// OP_TRUE, so the asset witness is regenerated with no counterparty signature;
// the timeout policy lives on the Bitcoin anchor and is satisfied by a single
// key after the relative timelock matures.
//
// It deliberately exercises the ScriptPath signing surface that both existing
// custom-anchor itests assert is empty.
func TestCustomAnchorBuilderScriptPathTimeoutSweep(t *testing.T) {
	h, ctx := newFundedHarnessFor(t, TransportGRPC)

	name := uniqueEventLabel("custom-anchor-script-path")
	minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1_000)
	require.NoError(t, err)
	require.True(t, minted.Ref.IsGroupRef())

	mintProof, err := h.AliceWallet.ExportProofFile(
		ctx,
		tapsdk.AssetRefFromAssetID(minted.Asset.Genesis.IssuanceID),
		minted.Asset.ScriptKey.PubKey, nil,
	)
	require.NoError(t, err)
	require.NotEmpty(t, mintProof.RawProofFile)

	mintFile, err := proof.DecodeFile(mintProof.RawProofFile)
	require.NoError(t, err)
	lastMintProof, err := mintFile.LastProof()
	require.NoError(t, err)

	// The timeout leaf is a standard CSV closure keyed to a single spender
	// key: <delay> OP_CSV OP_DROP <key> OP_CHECKSIG.
	csvKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	csvScript := customAnchorCSVLeafScript(t, customAnchorCSVDelay, csvKey)
	csvLeafHash := customAnchorTapLeafHash(csvScript)

	// The asset-level internal key for the OP_TRUE script key is unique per
	// output and never needs to be wallet-owned.
	opTrueKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	opTrueInternalKey := tapsdk.KeyDescriptor{
		RawKeyBytes: mustCompressedPubKey(t, opTrueKey.PubKey()),
	}

	// The BTC anchor internal key is NUMS, so the timeout leaf is the only
	// spending path.
	numsInternalKey := tapsdk.InternalKey{
		PubKey: mustCompressedPubKey(t, asset.NUMSPubKey),
	}

	lockAnchorPsbt, lockValue := customAnchorITestPSBT(
		t, lastMintProof, numsInternalKey,
	)
	mintAnchorSigner, err := tapsdk.ParseTaprootPubKey(
		lastMintProof.InclusionProof.InternalKey.SerializeCompressed(),
	)
	require.NoError(t, err)

	lockRequest := &tapsdk.CustomAnchorRequest{
		Inputs: []tapsdk.CustomAssetInput{{
			ID:        "mint-output",
			AssetRef:  minted.Ref,
			Amount:    minted.Asset.Amount,
			ProofFile: mintProof.RawProofFile,
			Witness: tapsdk.CustomAssetWitnessPlan{
				Mode: tapsdk.CustomAssetWitnessBackendSigner,
			},
		}},
		Outputs: []tapsdk.CustomAssetOutput{{
			ID:                "timeout-output",
			AssetRef:          minted.Ref,
			Amount:            minted.Asset.Amount,
			AnchorOutputIndex: 0,
			AnchorValueSat:    uint64(lockValue),
			Script: tapsdk.CustomAssetScriptPlan{
				Mode: tapsdk.CustomAssetScriptOPTrue,
				OPTrue: &tapsdk.CustomAssetOPTrueScriptPlan{
					InternalKey: opTrueInternalKey,
				},
			},
			Anchor: tapsdk.CustomAnchorOutputPlan{
				InternalKey: numsInternalKey,
				Tapscript: tapsdk.CustomAnchorTapscriptPlan{
					TapLeaves: []tapsdk.TapLeaf{{
						Script: csvScript,
					}},
				},
			},
		}},
		AnchorPSBT: lockAnchorPsbt,
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
				Signer: mintAnchorSigner.XOnly(),
			},
		}},
	}

	lockPlan, err := h.AliceWallet.NewCustomAnchorTxBuilder().Build(
		ctx, lockRequest,
	)
	require.NoError(t, err)
	lockVerification := lockPlan.Verification()
	require.True(t, lockVerification.Valid())

	lockSealed, err := lockPlan.Commit(ctx, tapsdk.CustomAnchorCommitOptions{
		Publish: tapsdk.CustomAnchorPublishMetadata{Label: name},
	})
	require.NoError(t, err)
	require.NoError(t, lockSealed.Validate())
	require.Len(t, lockSealed.Outputs, 1)
	timeoutOutput := lockSealed.Outputs[0]
	require.NotNil(t, timeoutOutput.OPTrueSpend)
	require.NoError(t, timeoutOutput.OPTrueSpend.Validate(
		timeoutOutput.ScriptKey,
	))

	lockFinal, err := newAliceLndAnchorSigner(t)(ctx, lockSealed.AnchorPsbt)
	require.NoError(t, err)
	require.NoError(t, lockSealed.VerifyFinalAnchorPSBT(lockFinal))

	_, err = h.AliceWallet.PublishCustomAnchorTransfer(
		ctx, lockSealed, lockFinal,
	)
	require.NoError(t, err)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	timeoutProof := h.WaitForProofFile(
		t, ctx, h.AliceWallet,
		tapsdk.AssetRefFromAssetID(timeoutOutput.IssuanceID),
		timeoutOutput.ScriptKey, &timeoutOutput.AnchorOutpoint,
	)
	require.NotEmpty(t, timeoutProof.RawProofFile)

	// Bury the timeout output far enough that the relative timelock is
	// satisfied before the sweep is broadcast.
	h.MineBlocks(t, int(customAnchorCSVDelay))
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	// The sweep spends the timeout output unilaterally: the asset input is
	// authorized by the regenerated OP_TRUE witness (no counterparty), and
	// the BTC anchor is spent through the timeout leaf.
	sweepFile, err := proof.DecodeFile(timeoutProof.RawProofFile)
	require.NoError(t, err)
	sweepInputProof, err := sweepFile.LastProof()
	require.NoError(t, err)

	receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
	require.NoError(t, err)

	sweepAnchorPsbt, sweepValue := customAnchorCSVSweepPSBT(
		t, sweepInputProof, receiverKeys.InternalKey, csvScript,
		timeoutOutput.TaprootAssetRoot, timeoutOutput.TaprootMerkleRoot,
	)

	sweepRequest := &tapsdk.CustomAnchorRequest{
		Inputs: []tapsdk.CustomAssetInput{{
			ID:        "timeout-input",
			AssetRef:  minted.Ref,
			Amount:    minted.Asset.Amount,
			ProofFile: timeoutProof.RawProofFile,
			Witness: tapsdk.CustomAssetWitnessPlan{
				Mode:  tapsdk.CustomAssetWitnessCallerProvided,
				Stack: timeoutOutput.OPTrueSpend.WitnessStack(),
			},
		}},
		Outputs: []tapsdk.CustomAssetOutput{{
			ID:                "bob-output",
			AssetRef:          minted.Ref,
			Amount:            minted.Asset.Amount,
			AnchorOutputIndex: 0,
			AnchorValueSat:    uint64(sweepValue),
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
		AnchorPSBT: sweepAnchorPsbt,
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
			ScriptPath: &tapsdk.CustomAnchorScriptPathSigningPlan{
				LeafHash: csvLeafHash,
				RequiredSigners: []tapsdk.XOnlyPubKey{
					mustXOnlyPubKey(t, csvKey.PubKey()),
				},
			},
		}},
	}

	sweepPlan, err := h.AliceWallet.NewCustomAnchorTxBuilder().Build(
		ctx, sweepRequest,
	)
	require.NoError(t, err)
	sweepVerification := sweepPlan.Verification()
	require.True(t, sweepVerification.Valid())

	sweepSealed, err := sweepPlan.Commit(ctx, tapsdk.CustomAnchorCommitOptions{
		Publish: tapsdk.CustomAnchorPublishMetadata{
			Label: name + "-sweep",
		},
	})
	require.NoError(t, err)
	require.NoError(t, sweepSealed.Validate())

	// This is the assertion the existing custom-anchor itests invert: the
	// unilateral sweep produces exactly one script-path signing request and
	// no key-path request.
	sweepSigning, err := sweepSealed.SigningRequests()
	require.NoError(t, err)
	require.Empty(t, sweepSigning.KeyPath)
	require.Empty(t, sweepSigning.MuSig2)
	require.Len(t, sweepSigning.ScriptPath, 1)
	scriptRequest := sweepSigning.ScriptPath[0]
	require.Equal(t, csvLeafHash, scriptRequest.LeafHash)

	signature, err := schnorr.Sign(csvKey, scriptRequest.Sighash[:])
	require.NoError(t, err)
	signatureBytes := signature.Serialize()
	if scriptRequest.SighashType != uint32(txscript.SigHashDefault) {
		signatureBytes = append(
			signatureBytes, byte(scriptRequest.SighashType),
		)
	}

	witnessed, err := sweepSealed.ApplyScriptPathWitness(
		scriptRequest.ID, [][]byte{signatureBytes},
	)
	require.NoError(t, err)
	require.NoError(t, witnessed.VerifyFinalAnchorPSBT(witnessed.AnchorPsbt))

	sweepFinalPacket := decodeCustomAnchorITestPSBT(t, witnessed.AnchorPsbt)
	require.NotEmpty(t, sweepFinalPacket.Inputs[0].FinalScriptWitness)

	// The sweep pays its own fee, so tapd broadcasts it directly with no
	// external fee-bump child.
	_, err = h.AliceWallet.PublishCustomAnchorTransfer(
		ctx, witnessed, witnessed.AnchorPsbt,
	)
	require.NoError(t, err)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	sweptOutput := sweepSealed.Outputs[0]
	sweptProof := h.WaitForProofFile(
		t, ctx, h.AliceWallet,
		tapsdk.AssetRefFromAssetID(sweptOutput.IssuanceID),
		sweptOutput.ScriptKey, &sweptOutput.AnchorOutpoint,
	)
	require.NotEmpty(t, sweptProof.RawProofFile)

	// The asset survived the unilateral spend: same issuance and amount, a
	// normal transfer rather than a burn.
	h.EnableUniverseBootstrap(t, ctx)
	registered, err := h.BobWallet.ImportProofFile(ctx, sweptProof)
	require.NoError(t, err)
	require.Equal(t, sweptOutput.IssuanceID, registered.IssuanceID)
	require.Equal(t, sweptOutput.ScriptKey, registered.ScriptKey)
	require.Equal(t, minted.Asset.Amount, registered.Amount)

	bobBalance := h.WaitForBalance(
		t, ctx, h.BobWallet, minted.Ref, minted.Asset.Amount,
		balanceTimeoutFor(minted.Ref),
	)
	require.Equal(t, minted.Asset.Amount, bobBalance)
}

// customAnchorCSVLeafScript builds a standard CSV timeout closure keyed to a
// single spender: <delay> OP_CHECKSEQUENCEVERIFY OP_DROP <key> OP_CHECKSIG.
func customAnchorCSVLeafScript(t testing.TB, delay int64,
	key *btcec.PrivateKey) []byte {

	t.Helper()
	builder := txscript.NewScriptBuilder()
	builder.AddInt64(delay)
	builder.AddOp(txscript.OP_CHECKSEQUENCEVERIFY)
	builder.AddOp(txscript.OP_DROP)
	builder.AddData(schnorr.SerializePubKey(key.PubKey()))
	builder.AddOp(txscript.OP_CHECKSIG)
	script, err := builder.Script()
	require.NoError(t, err)

	return script
}

// customAnchorTapLeafHash returns the base-version TapLeaf hash committed by
// the builder for a sibling script.
func customAnchorTapLeafHash(script []byte) tapsdk.Hash {
	leaf := txscript.NewBaseTapLeaf(script)
	hash := leaf.TapHash()

	var out tapsdk.Hash
	copy(out[:], hash[:])

	return out
}

// customAnchorCSVSweepPSBT builds the caller-funded v3 sweep that spends the
// timeout output through its script path and pays its own fee, so tapd can
// broadcast it directly without an external fee-bump child. The input sequence
// encodes the relative timelock, and the spent input carries the reconstructed
// timeout-leaf control block so the builder can derive the script-path signing
// request. The control block is composed from the committed roots exactly as
// an integrating host would. It returns the sweep output value.
func customAnchorCSVSweepPSBT(t testing.TB, inputProof *proof.Proof,
	receiverInternalKey tapsdk.InternalKey, csvScript []byte,
	taprootAssetRoot, taprootMerkleRoot tapsdk.Hash) ([]byte, int64) {

	t.Helper()
	inputOutpoint := inputProof.OutPoint()
	inputTxOut := inputProof.AnchorTx.TxOut[inputOutpoint.Index]
	require.Greater(t, inputTxOut.Value, customAnchorITestFeeSat)
	sweepValue := inputTxOut.Value - customAnchorITestFeeSat
	require.GreaterOrEqual(t, sweepValue, int64(330))

	internalKey, err := btcec.ParsePubKey(receiverInternalKey.PubKey[:])
	require.NoError(t, err)
	outputKey := txscript.ComputeTaprootKeyNoScript(internalKey)
	placeholderScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)

	// Reconstruct the timeout-leaf control block against the NUMS anchor
	// internal key. The asset commitment root is the only sibling, so it is
	// the sole inclusion-proof element.
	anchorOutputKey := txscript.ComputeTaprootOutputKey(
		asset.NUMSPubKey, taprootMerkleRoot[:],
	)
	controlBlock := txscript.ControlBlock{
		InternalKey:     asset.NUMSPubKey,
		OutputKeyYIsOdd: anchorOutputKey.SerializeCompressed()[0] == 0x03,
		LeafVersion:     txscript.BaseLeafVersion,
		InclusionProof:  taprootAssetRoot[:],
	}
	require.Equal(t, taprootMerkleRoot[:], controlBlock.RootHash(csvScript))
	require.Equal(t, inputTxOut.PkScript,
		mustPayToTaproot(t, anchorOutputKey))
	controlBlockBytes, err := controlBlock.ToBytes()
	require.NoError(t, err)

	sweepTx := wire.NewMsgTx(customAnchorITestVersion)
	sweepTx.LockTime = 21
	sweepTx.AddTxIn(wire.NewTxIn(&inputOutpoint, nil, nil))
	sweepTx.TxIn[0].Sequence = uint32(customAnchorCSVDelay)
	sweepTx.AddTxOut(&wire.TxOut{
		Value:    sweepValue,
		PkScript: placeholderScript,
	})

	packet, err := psbt.NewFromUnsignedTx(sweepTx)
	require.NoError(t, err)
	packet.Inputs[0].WitnessUtxo = inputTxOut
	packet.Inputs[0].TaprootInternalKey = schnorr.SerializePubKey(
		asset.NUMSPubKey,
	)
	packet.Inputs[0].TaprootMerkleRoot = taprootMerkleRoot[:]
	packet.Inputs[0].TaprootLeafScript = []*psbt.TaprootTapLeafScript{{
		ControlBlock: controlBlockBytes,
		Script:       csvScript,
		LeafVersion:  txscript.BaseLeafVersion,
	}}

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes(), sweepValue
}

func mustPayToTaproot(t testing.TB, key *btcec.PublicKey) []byte {
	t.Helper()
	script, err := txscript.PayToTaprootScript(key)
	require.NoError(t, err)

	return script
}

func mustCompressedPubKey(t testing.TB, key *btcec.PublicKey) tapsdk.PubKey {
	t.Helper()
	parsed, err := tapsdk.ParsePubKey(key.SerializeCompressed())
	require.NoError(t, err)

	return parsed
}

func mustXOnlyPubKey(t testing.TB, key *btcec.PublicKey) tapsdk.XOnlyPubKey {
	t.Helper()
	parsed, err := tapsdk.ParseXOnlyPubKey(schnorr.SerializePubKey(key))
	require.NoError(t, err)

	return parsed
}
