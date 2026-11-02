//go:build itest

package itest

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcec/v2/schnorr/musig2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

// TestCustomAnchorBuilderMuSig2KeyPathSweep proves the generic custom-anchor
// capability that Ark-style cooperative exits depend on, without any
// Ark-specific concept: an asset output can be anchored to a MuSig2-aggregated
// internal key and later spent cooperatively through a single aggregate
// key-path signature, yielding a valid, non-burned proof. The asset script key
// is OP_TRUE, so the asset witness is regenerated with no per-party signature;
// the cooperative policy lives on the Bitcoin anchor key.
//
// It exercises the MuSig2 signing surface that both existing custom-anchor
// itests assert is empty.
func TestCustomAnchorBuilderMuSig2KeyPathSweep(t *testing.T) {
	h, ctx := newFundedHarnessFor(t, TransportGRPC)

	name := uniqueEventLabel("custom-anchor-musig2")
	minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1_000)
	require.NoError(t, err)
	require.True(t, minted.Ref.IsGroupRef())

	mintProof, err := h.AliceWallet.ExportProofFile(
		ctx,
		tapsdk.AssetRefFromAssetID(minted.Asset.Genesis.IssuanceID),
		minted.Asset.ScriptKey.PubKey, nil,
	)
	require.NoError(t, err)

	mintFile, err := proof.DecodeFile(mintProof.RawProofFile)
	require.NoError(t, err)
	lastMintProof, err := mintFile.LastProof()
	require.NoError(t, err)

	// Two cooperative signers own the anchor. Their pre-tweaked aggregate
	// key is the anchor internal key exactly as the SDK reconstructs it.
	aliceKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	bobKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	signers := []*btcec.PrivateKey{aliceKey, bobKey}
	participants := []*btcec.PublicKey{aliceKey.PubKey(), bobKey.PubKey()}
	aggregateKey := customAnchorMuSig2AggregateKey(t, participants)
	muSig2InternalKey := tapsdk.InternalKey{
		PubKey: mustCompressedPubKey(t, aggregateKey),
	}

	opTrueKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	opTrueInternalKey := tapsdk.KeyDescriptor{
		RawKeyBytes: mustCompressedPubKey(t, opTrueKey.PubKey()),
	}

	lockAnchorPsbt, lockValue := customAnchorITestPSBT(
		t, lastMintProof, muSig2InternalKey,
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
			ID:                "musig2-output",
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
				InternalKey: muSig2InternalKey,
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
	require.Len(t, lockSealed.Outputs, 1)
	muSig2Output := lockSealed.Outputs[0]
	require.NotNil(t, muSig2Output.OPTrueSpend)

	lockFinal, err := newAliceLndAnchorSigner(t)(ctx, lockSealed.AnchorPsbt)
	require.NoError(t, err)
	require.NoError(t, lockSealed.VerifyFinalAnchorPSBT(lockFinal))

	_, err = h.AliceWallet.PublishCustomAnchorTransfer(
		ctx, lockSealed, lockFinal,
	)
	require.NoError(t, err)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	lockProof := h.WaitForProofFile(
		t, ctx, h.AliceWallet,
		tapsdk.AssetRefFromAssetID(muSig2Output.IssuanceID),
		muSig2Output.ScriptKey, &muSig2Output.AnchorOutpoint,
	)
	require.NotEmpty(t, lockProof.RawProofFile)

	// The cooperative spend re-anchors the asset to Bob: the asset input is
	// authorized by the regenerated OP_TRUE witness, and the Bitcoin anchor
	// is spent by an aggregate MuSig2 key-path signature.
	sweepFile, err := proof.DecodeFile(lockProof.RawProofFile)
	require.NoError(t, err)
	sweepInputProof, err := sweepFile.LastProof()
	require.NoError(t, err)

	receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
	require.NoError(t, err)

	sweepAnchorPsbt, sweepValue := customAnchorMuSig2SweepPSBT(
		t, sweepInputProof, receiverKeys.InternalKey, aggregateKey,
		muSig2Output.TaprootMerkleRoot,
	)

	sweepRequest := &tapsdk.CustomAnchorRequest{
		Inputs: []tapsdk.CustomAssetInput{{
			ID:        "musig2-input",
			AssetRef:  minted.Ref,
			Amount:    minted.Asset.Amount,
			ProofFile: lockProof.RawProofFile,
			Witness: tapsdk.CustomAssetWitnessPlan{
				Mode:  tapsdk.CustomAssetWitnessCallerProvided,
				Stack: muSig2Output.OPTrueSpend.WitnessStack(),
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
			MuSig2: &tapsdk.CustomAnchorMuSig2SigningPlan{
				Participants: []tapsdk.PubKey{
					mustCompressedPubKey(t, aliceKey.PubKey()),
					mustCompressedPubKey(t, bobKey.PubKey()),
				},
				SessionContext: []byte("musig2-cooperative-sweep"),
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
		Publish: tapsdk.CustomAnchorPublishMetadata{Label: name + "-sweep"},
	})
	require.NoError(t, err)
	require.NoError(t, sweepSealed.Validate())

	// The cooperative sweep produces exactly one MuSig2 signing request and
	// no single-signer key-path or script-path request.
	sweepSigning, err := sweepSealed.SigningRequests()
	require.NoError(t, err)
	require.Empty(t, sweepSigning.KeyPath)
	require.Empty(t, sweepSigning.ScriptPath)
	require.Len(t, sweepSigning.MuSig2, 1)
	muSig2Request := sweepSigning.MuSig2[0]
	require.Len(t, muSig2Request.Participants, 2)

	signature := customAnchorMuSig2Sign(
		t, signers, participants, muSig2Request.Sighash,
		muSig2Request.TaprootMerkleRoot,
	)
	if muSig2Request.SighashType != uint32(txscript.SigHashDefault) {
		signature = append(signature, byte(muSig2Request.SighashType))
	}

	witnessed, err := sweepSealed.ApplyKeyPathSignature(
		muSig2Request.ID, signature,
	)
	require.NoError(t, err)

	// Finalize the aggregate key-path signature into a broadcastable witness,
	// the step an external key-path signer such as lnd performs.
	finalAnchorPsbt := customAnchorFinalizeKeyPath(t, witnessed.AnchorPsbt)
	require.NoError(t, witnessed.VerifyFinalAnchorPSBT(finalAnchorPsbt))

	_, err = h.AliceWallet.PublishCustomAnchorTransfer(
		ctx, witnessed, finalAnchorPsbt,
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

// customAnchorFinalizeKeyPath finalizes a Taproot key-path input by promoting
// its aggregate key-spend signature to the final witness stack, then returns
// the serialized PSBT ready for broadcast.
func customAnchorFinalizeKeyPath(t testing.TB, rawPsbt []byte) []byte {
	t.Helper()
	packet := decodeCustomAnchorITestPSBT(t, rawPsbt)
	ok, err := psbt.MaybeFinalize(packet, 0)
	require.NoError(t, err)
	require.True(t, ok)

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes()
}

// customAnchorMuSig2AggregateKey returns the BIP-327 pre-tweaked aggregate key
// for the given ordered participant set, matching how the SDK reconstructs the
// committed internal key (unsorted).
func customAnchorMuSig2AggregateKey(t testing.TB,
	participants []*btcec.PublicKey) *btcec.PublicKey {

	t.Helper()
	aggregate, _, _, err := musig2.AggregateKeys(participants, false)
	require.NoError(t, err)

	return aggregate.PreTweakedKey
}

// customAnchorMuSig2Sign runs a complete in-process MuSig2 signing ceremony
// over the message, applying the BIP-341 taproot tweak for the committed
// script root, and returns the 64-byte aggregate Schnorr signature.
func customAnchorMuSig2Sign(t testing.TB, signers []*btcec.PrivateKey,
	participants []*btcec.PublicKey, sighash tapsdk.Hash,
	scriptRoot []byte) []byte {

	t.Helper()
	var message [32]byte
	copy(message[:], sighash[:])

	nonces := make([]*musig2.Nonces, len(signers))
	pubNonces := make([][musig2.PubNonceSize]byte, len(signers))
	for i, signer := range signers {
		nonce, err := musig2.GenNonces(
			musig2.WithPublicKey(signer.PubKey()),
		)
		require.NoError(t, err)
		nonces[i] = nonce
		pubNonces[i] = nonce.PubNonce
	}

	combinedNonce, err := musig2.AggregateNonces(pubNonces)
	require.NoError(t, err)

	partials := make([]*musig2.PartialSignature, len(signers))
	for i, signer := range signers {
		partial, err := musig2.Sign(
			nonces[i].SecNonce, signer, combinedNonce, participants,
			message, musig2.WithTaprootSignTweak(scriptRoot),
		)
		require.NoError(t, err)
		partials[i] = partial
	}

	finalSig := musig2.CombineSigs(
		partials[0].R, partials,
		musig2.WithTaprootTweakedCombine(
			message, participants, scriptRoot, false,
		),
	)

	return finalSig.Serialize()
}

// customAnchorMuSig2SweepPSBT builds the caller-funded v3 cooperative sweep
// that spends the MuSig2 output through the aggregate key path and pays its own
// fee. The spent input declares the aggregate internal key and committed merkle
// root so the builder can derive the MuSig2 signing request. It returns the
// sweep output value.
func customAnchorMuSig2SweepPSBT(t testing.TB, inputProof *proof.Proof,
	receiverInternalKey tapsdk.InternalKey, aggregateKey *btcec.PublicKey,
	taprootMerkleRoot tapsdk.Hash) ([]byte, int64) {

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

	// Sanity check: the declared aggregate key and committed merkle root must
	// reproduce the actual on-chain output key.
	anchorOutputKey := txscript.ComputeTaprootOutputKey(
		aggregateKey, taprootMerkleRoot[:],
	)
	require.Equal(t, inputTxOut.PkScript,
		mustPayToTaproot(t, anchorOutputKey))

	sweepTx := wire.NewMsgTx(customAnchorITestVersion)
	sweepTx.LockTime = 21
	sweepTx.AddTxIn(wire.NewTxIn(&inputOutpoint, nil, nil))
	sweepTx.TxIn[0].Sequence = customAnchorITestSequence
	sweepTx.AddTxOut(&wire.TxOut{
		Value:    sweepValue,
		PkScript: placeholderScript,
	})

	packet, err := psbt.NewFromUnsignedTx(sweepTx)
	require.NoError(t, err)
	packet.Inputs[0].WitnessUtxo = inputTxOut
	packet.Inputs[0].TaprootInternalKey = schnorr.SerializePubKey(
		aggregateKey,
	)
	packet.Inputs[0].TaprootMerkleRoot = taprootMerkleRoot[:]

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes(), sweepValue
}
