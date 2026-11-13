//go:build itest

package itest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/address/v2"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/mempool"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

const (
	customAnchorITestVersion  = int32(3)
	customAnchorITestSequence = uint32(0xffff_fffc)
	customAnchorITestFeeSat   = int64(250)
	customAnchorITestChildFee = int64(1_000)

	customAnchorITestCallerFeeSat = int64(1_000)
)

// TestCustomAnchorBuilderEndToEnd exercises the exact caller-funded custom
// anchor lifecycle through both SDK transports. In particular, it proves that
// the caller's transaction version, sequence, and fee survive virtual signing,
// commitment, external anchor signing, and publishing unchanged.
func TestCustomAnchorBuilderEndToEnd(t *testing.T) {
	testCustomAnchorBuilderEndToEnd(t, false)
}

// TestCustomAnchorBuilderReceivedAsset exercises backend signing after a
// transfer to a V2 address, whose unique script key requires a Taproot tweak.
func TestCustomAnchorBuilderReceivedAsset(t *testing.T) {
	testCustomAnchorBuilderEndToEnd(t, true)
}

func testCustomAnchorBuilderEndToEnd(t *testing.T, received bool) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf(
			"custom-anchor-%s", transport,
		))
		mintSpec := mintGroupedAssetSpec(name, 1_000)
		mintSpec.AssetVersion = tapsdk.AssetVersionV1
		minted, err := h.MintAssetAndConfirm(t, ctx, mintSpec)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		inputKey := minted.Asset.ScriptKey.PubKey
		var inputPoint *tapsdk.Outpoint
		if received {
			// Move the whole asset to a fresh V2 address in Alice's
			// wallet so the custom spend starts from a received key.
			v1 := tapsdk.AssetVersionV1
			v2 := tapsdk.AddressVersionV2
			addr, err := h.AliceClient.NewAddr(
				ctx, &tapsdk.NewAddressRequest{
					AssetRef:       minted.Ref,
					AssetVersion:   &v1,
					AddressVersion: &v2,
					ProofCourierAddr: envOr(
						"TAPD_ALICE_PROOF_COURIER_ADDR",
						"authmailbox+universerpc://"+
							"tapd-alice:10029",
					),
				},
			)
			require.NoError(t, err)
			transfer, err := h.AliceWallet.Send(
				ctx, addr.Encoded,
				tapsdk.WithAmount(minted.Asset.Amount),
			)
			require.NoError(t, err)
			for _, output := range transfer.Outputs {
				if output.Amount == minted.Asset.Amount &&
					output.IssuanceID ==
						minted.Asset.Genesis.IssuanceID {

					inputKey = output.ScriptKey
					point := output.AnchorOutpoint
					inputPoint = &point
				}
			}
			require.NotNil(t, inputPoint)
			h.MineBlocks(t, defaultMineBlocks)
			h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
			h.WaitForProofFile(
				t, ctx, h.AliceWallet,
				tapsdk.AssetRefFromAssetID(
					minted.Asset.Genesis.IssuanceID,
				),
				inputKey, inputPoint,
			)
			key, err := h.AliceClient.QueryScriptKey(
				ctx, inputKey[1:],
			)
			require.NoError(t, err)
			require.Len(t, key.TapTweak, 32)
		}

		inputProof, err := h.AliceWallet.ExportProofFile(
			ctx,
			tapsdk.AssetRefFromAssetID(
				minted.Asset.Genesis.IssuanceID,
			),
			inputKey, inputPoint,
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
		request := &customanchor.Request{
			Inputs: []customanchor.AssetInput{{
				ID:        "mint-output",
				AssetRef:  minted.Ref,
				Amount:    minted.Asset.Amount,
				ProofFile: inputProof.RawProofFile,
				Witness: customanchor.AssetWitnessPlan{
					Mode: customanchor.AssetWitnessBackendSigner,
				},
			}},
			Outputs: []customanchor.AssetOutput{{
				ID:                "bob-output",
				AssetRef:          minted.Ref,
				Amount:            minted.Asset.Amount,
				AnchorOutputIndex: 0,
				AnchorValueSat:    uint64(anchorValue),
				Script: customanchor.AssetScriptPlan{
					Mode: customanchor.AssetScriptExternal,
					External: &customanchor.AssetExternalScriptPlan{
						ScriptKey: receiverKeys.ScriptKey,
					},
				},
				Anchor: customanchor.OutputPlan{
					InternalKey: receiverKeys.InternalKey,
				},
			}},
			AnchorPSBT: anchorPsbt,
			Funding: customanchor.FundingPlan{
				Mode:              customanchor.FundingCallerFundedExact,
				CallerFundedExact: &customanchor.CallerFundedExact{},
			},
			PassiveAssets: customanchor.PassiveAssets{
				Policy: customanchor.PassiveReject,
			},
			LossPolicy: customanchor.LossPolicy{
				Mode: customanchor.LossReject,
			},
			SigningPlans: []customanchor.InputSigningPlan{{
				InputIndex: 0,
				KeyPath: &customanchor.KeyPathSigningPlan{
					Signer: anchorSigner.XOnly(),
				},
			}},
		}

		plan, err := customanchor.NewTxBuilder(h.AliceWallet).Build(
			ctx, request,
		)
		require.NoError(t, err)
		verification := plan.Verification()
		require.True(t, verification.Valid())
		require.NotEmpty(t, plan.ActiveVirtualPSBTs())
		requireCustomAnchorTxShape(t, plan.AnchorPSBT(), anchorValue)

		sealed, err := plan.Commit(ctx, customanchor.CommitOptions{
			Publish: customanchor.PublishMetadata{Label: name},
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
		require.NotZero(t, sealed.Outputs[0].TaprootAssetRoot)
		require.Equal(t, sealed.Outputs[0].TaprootAssetRoot,
			sealed.Outputs[0].TaprootMerkleRoot)
		require.Equal(t, receiverKeys.ScriptKey.PubKey,
			sealed.Outputs[0].ScriptKey)
		requireCustomAnchorTxShape(t, sealed.AnchorPsbt, anchorValue)

		// Persist the daemon suffix as an unconfirmed path before
		// it gains block inclusion data.
		path := &customanchor.ProofPath{
			ConfirmedBaseProof: inputProof.RawProofFile,
			Steps: []customanchor.ProofPathStep{{
				TransitionProof: sealed.ProofUpdates[0].
					ProofBlob,
			}},
		}
		pathBytes, err := path.MarshalBinary()
		require.NoError(t, err)
		var restored customanchor.ProofPath
		require.NoError(t, restored.UnmarshalBinary(pathBytes))
		step, err := restored.Steps[0].Summary()
		require.NoError(t, err)
		require.Equal(t, sealed.Outputs[0].AnchorOutpoint,
			step.AnchorOutpoint)
		transition, err := proof.Decode(
			restored.Steps[0].TransitionProof,
		)
		require.NoError(t, err)
		require.Equal(t, proof.TransitionV1, transition.Version)
		require.NoError(t, transition.CheckActivationEvidence())

		transition.InclusionProof.CommitmentProof.SpenderProofs = nil
		restored.Steps[0].TransitionProof, err = transition.Bytes()
		require.NoError(t, err)
		require.ErrorIs(t, restored.Validate(),
			proof.ErrMissingSpenderProofs)

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

		published, err := customanchor.PublishTransfer(
			ctx, h.AliceWallet, sealed, finalAnchorPsbt,
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
		externalFeeBump := &customanchor.ExternalP2AFeeBump{
			P2AOutputIndex: 1,
		}
		externalFundingMode :=
			customanchor.FundingExternalP2AFeeBump

		request := &customanchor.Request{
			Inputs: []customanchor.AssetInput{{
				ID:        "mint-output",
				AssetRef:  minted.Ref,
				Amount:    minted.Asset.Amount,
				ProofFile: inputProof.RawProofFile,
				Witness: customanchor.AssetWitnessPlan{
					Mode: customanchor.AssetWitnessBackendSigner,
				},
			}},
			Outputs: []customanchor.AssetOutput{{
				ID:                "bob-output",
				AssetRef:          minted.Ref,
				Amount:            minted.Asset.Amount,
				AnchorOutputIndex: 0,
				AnchorValueSat:    uint64(anchorValue),
				Script: customanchor.AssetScriptPlan{
					Mode: customanchor.AssetScriptExternal,
					External: &customanchor.AssetExternalScriptPlan{
						ScriptKey: receiverKeys.ScriptKey,
					},
				},
				Anchor: customanchor.OutputPlan{
					InternalKey: receiverKeys.InternalKey,
				},
			}},
			AnchorPSBT: anchorPsbt,
			Funding: customanchor.FundingPlan{
				Mode:               externalFundingMode,
				ExternalP2AFeeBump: externalFeeBump,
			},
			PassiveAssets: customanchor.PassiveAssets{
				Policy: customanchor.PassiveReject,
			},
			LossPolicy: customanchor.LossPolicy{
				Mode: customanchor.LossReject,
			},
			SigningPlans: []customanchor.InputSigningPlan{{
				InputIndex: 0,
				KeyPath: &customanchor.KeyPathSigningPlan{
					Signer: anchorSigner.XOnly(),
				},
			}},
		}

		plan, err := customanchor.NewTxBuilder(h.AliceWallet).Build(
			ctx, request,
		)
		require.NoError(t, err)
		verification := plan.Verification()
		require.True(t, verification.Valid())
		require.NotEmpty(t, plan.ActiveVirtualPSBTs())
		requireCustomAnchorP2ATxShape(
			t, plan.AnchorPSBT(), anchorValue,
		)

		sealed, err := plan.Commit(ctx, customanchor.CommitOptions{
			Publish: customanchor.PublishMetadata{
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
		require.NotZero(t, sealed.Outputs[0].TaprootAssetRoot)
		require.Equal(t, sealed.Outputs[0].TaprootAssetRoot,
			sealed.Outputs[0].TaprootMerkleRoot)
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

		logged, err := customanchor.PublishTransfer(
			ctx, h.AliceWallet, sealed, finalAnchorPsbt,
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

// TestCustomAnchorBuilderBackendSignedAnchor exercises a caller-funded exact
// transfer whose BTC funding input and change belong to a key that neither
// tapd nor lnd controls. The caller signs only that input, and tapd signs its
// own asset anchor at publish.
func TestCustomAnchorBuilderBackendSignedAnchor(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newHarnessContextFor(t, transport)
		skipBelowTapdVersion(t, ctx, h.AliceClient, 0, 8, 99)
		h.FundLndWallet(t, ctx)
		wallet := h.AliceWallet

		name := uniqueEventLabel(fmt.Sprintf(
			"custom-anchor-backend-signed-%s", transport,
		))
		mintSpec := mintGroupedAssetSpec(name, 1_000)
		mintSpec.AssetVersion = tapsdk.AssetVersionV1
		minted, err := h.MintAssetAndConfirm(t, ctx, mintSpec)
		require.NoError(t, err)

		inputProof, err := h.AliceWallet.ExportProofFile(
			ctx,
			tapsdk.AssetRefFromAssetID(
				minted.Asset.Genesis.IssuanceID,
			),
			minted.Asset.ScriptKey.PubKey, nil,
		)
		require.NoError(t, err)
		proofFile, err := proof.DecodeFile(inputProof.RawProofFile)
		require.NoError(t, err)
		lastProof, err := proofFile.LastProof()
		require.NoError(t, err)

		receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
		require.NoError(t, err)

		callerKey, err := btcec.NewPrivateKey()
		require.NoError(t, err)
		callerUTXO := fundCustomAnchorCallerUTXO(t, ctx, h, callerKey)
		anchorPsbt, anchorValue := customAnchorCallerFundedITestPSBT(
			t, lastProof, receiverKeys.InternalKey, callerUTXO,
		)
		request := &customanchor.Request{
			Inputs: []customanchor.AssetInput{{
				ID:        "mint-output",
				AssetRef:  minted.Ref,
				Amount:    minted.Asset.Amount,
				ProofFile: inputProof.RawProofFile,
				Witness: customanchor.AssetWitnessPlan{
					Mode: customanchor.AssetWitnessBackendSigner,
				},
			}},
			Outputs: []customanchor.AssetOutput{{
				ID:                "bob-output",
				AssetRef:          minted.Ref,
				Amount:            minted.Asset.Amount,
				AnchorOutputIndex: 0,
				AnchorValueSat:    uint64(anchorValue),
				Script: customanchor.AssetScriptPlan{
					Mode: customanchor.AssetScriptExternal,
					External: &customanchor.AssetExternalScriptPlan{
						ScriptKey: receiverKeys.ScriptKey,
					},
				},
				Anchor: customanchor.OutputPlan{
					InternalKey: receiverKeys.InternalKey,
				},
			}},
			AnchorPSBT: anchorPsbt,
			Funding: customanchor.FundingPlan{
				Mode:              customanchor.FundingCallerFundedExact,
				CallerFundedExact: &customanchor.CallerFundedExact{},
			},
			PassiveAssets: customanchor.PassiveAssets{
				Policy: customanchor.PassiveReject,
			},
			LossPolicy: customanchor.LossPolicy{
				Mode: customanchor.LossReject,
			},
			SigningPlans: []customanchor.InputSigningPlan{
				{
					InputIndex: 0,
					BackendSigned: &customanchor.
						BackendSignedPlan{},
				},
				{
					InputIndex: 1,
					CallerSigned: &customanchor.
						CallerSignedPlan{},
				},
			},
		}

		plan, err := customanchor.NewTxBuilder(wallet).Build(ctx, request)
		require.NoError(t, err)
		sealed, err := plan.Commit(ctx, customanchor.CommitOptions{
			Publish: customanchor.PublishMetadata{Label: name},
		})
		require.NoError(t, err)
		require.Len(t, sealed.Outputs, 1)

		signingRequests, err := sealed.SigningRequests()
		require.NoError(t, err)
		require.Empty(t, signingRequests.KeyPath)
		require.Empty(t, signingRequests.MuSig2)
		require.Empty(t, signingRequests.ScriptPath)

		finalAnchorPsbt := signCustomAnchorCallerInput(
			t, sealed.AnchorPsbt, 1, callerKey,
		)
		require.NoError(t, sealed.VerifyFinalAnchorPSBT(finalAnchorPsbt))
		finalPacket := decodeCustomAnchorITestPSBT(t, finalAnchorPsbt)
		require.Empty(t, finalPacket.Inputs[0].FinalScriptWitness)

		published, err := customanchor.PublishTransfer(
			ctx, wallet, sealed, finalAnchorPsbt,
		)
		require.NoError(t, err)

		// tapd must have added a key-path witness for its anchor and
		// left the caller's witness and the committed txid intact.
		var anchorTx wire.MsgTx
		require.NoError(t, anchorTx.Deserialize(
			bytes.NewReader(published.AnchorTransaction),
		))
		require.Equal(t, finalPacket.UnsignedTx.TxHash(),
			anchorTx.TxHash())
		require.Len(t, anchorTx.TxIn[0].Witness, 1)
		var callerWitness bytes.Buffer
		require.NoError(t, psbt.WriteTxWitness(
			&callerWitness, anchorTx.TxIn[1].Witness,
		))
		require.Equal(t, finalPacket.Inputs[1].FinalScriptWitness,
			callerWitness.Bytes())

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		output := sealed.Outputs[0]
		completedProof := h.WaitForProofFile(
			t, ctx, h.AliceWallet,
			tapsdk.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)
		h.EnableUniverseBootstrap(t, ctx)
		registered, err := h.BobWallet.ImportProofFile(
			ctx, completedProof,
		)
		require.NoError(t, err)
		require.Equal(t, minted.Asset.Amount, registered.Amount)

		bobBalance := h.WaitForBalance(
			t, ctx, h.BobWallet, minted.Ref, minted.Asset.Amount,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, minted.Asset.Amount, bobBalance)
	})
}

// skipBelowTapdVersion skips the test when tapd is older than the given
// version. The pinned CI image lags tapd main, which reports 0.8.99 until the
// next minor release.
func skipBelowTapdVersion(t *testing.T, ctx context.Context,
	client tapsdk.Client, major, minor, patch int) {

	t.Helper()
	info, err := client.GetInfo(ctx)
	require.NoError(t, err)

	var got [3]int
	_, err = fmt.Sscanf(
		info.Version, "%d.%d.%d", &got[0], &got[1], &got[2],
	)
	require.NoError(t, err, "tapd version %q", info.Version)

	want := [3]int{major, minor, patch}
	for idx := range want {
		if got[idx] > want[idx] {
			return
		}
		if got[idx] < want[idx] {
			t.Skipf("tapd %s is older than %d.%d.%d", info.Version,
				major, minor, patch)
		}
	}
}

type customAnchorCallerUTXO struct {
	outpoint wire.OutPoint
	txOut    *wire.TxOut
}

// fundCustomAnchorCallerUTXO pays a confirmed P2WPKH output to key from the
// bitcoind miner wallet.
func fundCustomAnchorCallerUTXO(t testing.TB, ctx context.Context,
	h *TestHarness, key *btcec.PrivateKey) customAnchorCallerUTXO {

	t.Helper()
	pkScript, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_0).
		AddData(address.Hash160(key.PubKey().SerializeCompressed())).
		Script()
	require.NoError(t, err)

	decodedJSON := h.bitcoindRPCWallet(
		t, "miner", "decodescript",
		fmt.Sprintf("%q", hex.EncodeToString(pkScript)),
	)
	var decoded struct {
		Address string `json:"address"`
	}
	require.NoError(t, json.Unmarshal([]byte(decodedJSON), &decoded))
	require.NotEmpty(t, decoded.Address)

	txidJSON := h.bitcoindRPCWallet(
		t, "miner", "sendtoaddress",
		fmt.Sprintf("%q", decoded.Address), "0.001",
	)
	var txid string
	require.NoError(t, json.Unmarshal([]byte(txidJSON), &txid))
	txJSON := h.bitcoindRPCWallet(
		t, "miner", "gettransaction", fmt.Sprintf("%q", txid),
		"true", "true",
	)
	var funding struct {
		Decoded struct {
			Vout []struct {
				Value        float64 `json:"value"`
				N            uint32  `json:"n"`
				ScriptPubKey struct {
					Hex string `json:"hex"`
				} `json:"scriptPubKey"`
			} `json:"vout"`
		} `json:"decoded"`
	}
	require.NoError(t, json.Unmarshal([]byte(txJSON), &funding))

	hash, err := chainhash.NewHashFromStr(txid)
	require.NoError(t, err)
	for _, vout := range funding.Decoded.Vout {
		if vout.ScriptPubKey.Hex != hex.EncodeToString(pkScript) {
			continue
		}
		amount, err := btcutil.NewAmount(vout.Value)
		require.NoError(t, err)

		h.MineBlocks(t, 1)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

		return customAnchorCallerUTXO{
			outpoint: wire.OutPoint{Hash: *hash, Index: vout.N},
			txOut: &wire.TxOut{
				Value:    int64(amount),
				PkScript: pkScript,
			},
		}
	}

	require.FailNow(t, "funding transaction has no caller output")
	return customAnchorCallerUTXO{}
}

// customAnchorCallerFundedITestPSBT spends the asset anchor and the caller
// UTXO. The asset output keeps the anchor value and the caller UTXO pays the
// fee into P2WPKH change back to the caller.
func customAnchorCallerFundedITestPSBT(t testing.TB,
	inputProof *proof.Proof, receiverInternalKey tapsdk.InternalKey,
	caller customAnchorCallerUTXO) ([]byte, int64) {

	t.Helper()

	inputOutpoint := inputProof.OutPoint()
	anchorValue := inputProof.AnchorTx.TxOut[inputOutpoint.Index].Value
	changeValue := caller.txOut.Value - customAnchorITestCallerFeeSat
	require.Positive(t, changeValue)

	internalKey, err := btcec.ParsePubKey(receiverInternalKey.PubKey[:])
	require.NoError(t, err)
	outputKey := txscript.ComputeTaprootKeyNoScript(internalKey)
	placeholderScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)

	anchorTx := wire.NewMsgTx(customAnchorITestVersion)
	anchorTx.AddTxIn(wire.NewTxIn(&inputOutpoint, nil, nil))
	anchorTx.AddTxIn(wire.NewTxIn(&caller.outpoint, nil, nil))
	for _, txIn := range anchorTx.TxIn {
		txIn.Sequence = customAnchorITestSequence
	}
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    anchorValue,
		PkScript: placeholderScript,
	})
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    changeValue,
		PkScript: caller.txOut.PkScript,
	})

	packet, err := psbt.NewFromUnsignedTx(anchorTx)
	require.NoError(t, err)
	packet.Inputs[1].WitnessUtxo = caller.txOut

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes(), anchorValue
}

// signCustomAnchorCallerInput finalizes one P2WPKH caller input outside tapd
// and lnd.
func signCustomAnchorCallerInput(t testing.TB, rawPsbt []byte,
	inputIndex int, key *btcec.PrivateKey) []byte {

	t.Helper()
	packet := decodeCustomAnchorITestPSBT(t, rawPsbt)
	prevOuts := txscript.NewMultiPrevOutFetcher(nil)
	for idx, txIn := range packet.UnsignedTx.TxIn {
		require.NotNil(t, packet.Inputs[idx].WitnessUtxo)
		prevOuts.AddPrevOut(
			txIn.PreviousOutPoint, packet.Inputs[idx].WitnessUtxo,
		)
	}
	sigHashes := txscript.NewTxSigHashes(packet.UnsignedTx, prevOuts)
	prevOut := packet.Inputs[inputIndex].WitnessUtxo
	witness, err := txscript.WitnessSignature(
		packet.UnsignedTx, sigHashes, inputIndex, prevOut.Value,
		prevOut.PkScript, txscript.SigHashAll, key, true,
	)
	require.NoError(t, err)

	var serialized bytes.Buffer
	require.NoError(t, psbt.WriteTxWitness(&serialized, witness))
	packet.Inputs[inputIndex].FinalScriptWitness = serialized.Bytes()

	var buffer bytes.Buffer
	require.NoError(t, packet.Serialize(&buffer))

	return buffer.Bytes()
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

	utxo := customAnchorITestMinerUTXO(t, h, outputScript)
	utxoAmount, err := btcutil.NewAmount(utxo.Amount)
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
	h *TestHarness, outputScript []byte) customAnchorMinerUTXO {

	t.Helper()
	listJSON := h.bitcoindRPCWallet(
		t, "miner", "listunspent", "1", "9999999",
	)
	var utxos []customAnchorMinerUTXO
	require.NoError(t, json.Unmarshal([]byte(listJSON), &utxos))
	for _, utxo := range utxos {
		amount, err := btcutil.NewAmount(utxo.Amount)
		require.NoError(t, err)
		// Earlier tests can leave small miner outputs. The child must
		// cover its fee and leave a standard, non-dust change output.
		change := wire.NewTxOut(
			int64(amount)-customAnchorITestChildFee, outputScript,
		)
		if change.Value > 0 &&
			!mempool.IsDust(change, mempool.DefaultMinRelayTxFee) {

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
