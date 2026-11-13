//go:build itest

package itest

import (
	"bytes"
	"context"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	"github.com/lightninglabs/tap-sdk/embedded"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

const (
	// embeddedITestSupply is the number of units Alice mints.
	embeddedITestSupply = uint64(1_000)

	// embeddedITestReceived is the number of units Alice sends to the
	// engine.
	embeddedITestReceived = uint64(600)

	// embeddedITestSent is the number of units the engine sends on to Bob
	// before it sends him its change.
	embeddedITestSent = uint64(250)

	// embeddedITestAnchorSat is the value of every asset anchor output of
	// the engine's transfers.
	embeddedITestAnchorSat = int64(1_000)

	// embeddedITestFeeSat is the fee of the engine's anchor transactions.
	embeddedITestFeeSat = int64(1_000)
)

// TestEmbeddedTransfer receives an asset from Alice's tapd into an embedded
// engine and sends it on to Bob's tapd in two transfers, the second of which
// spends the change of the first. lnd-alice pays for the anchor transactions.
func TestEmbeddedTransfer(t *testing.T) {
	h, ctx := newFundedHarness(t)

	t.Run("ungrouped", func(t *testing.T) {
		minted, err := h.MintAssetAndConfirm(t, ctx, &tapsdk.MintAsset{
			AssetType:     tapsdk.AssetTypeFungible,
			Name:          uniqueEventLabel("embedded-ungrouped"),
			InitialSupply: embeddedITestSupply,
		})
		require.NoError(t, err)
		require.True(t, minted.Ref.IsAssetIDRef())

		testEmbeddedTransfer(t, ctx, h, h.NewEmbeddedNode(t), minted)
	})

	t.Run("grouped", func(t *testing.T) {
		minted, err := h.CreateFungibleAndConfirm(
			t, ctx, uniqueEventLabel("embedded-grouped"),
			embeddedITestSupply,
		)
		require.NoError(t, err)
		groupKey, ok := minted.Ref.GroupKey()
		require.True(t, ok)

		node := h.NewEmbeddedNode(t, groupKey)
		testEmbeddedTransfer(t, ctx, h, node, minted)
	})
}

func testEmbeddedTransfer(t *testing.T, ctx context.Context, h *TestHarness,
	node *EmbeddedNode, minted *MintResult) {

	lnd := newAliceLndWalletKit(t)

	received, receivedProof := h.SendToEmbedded(
		t, ctx, node, minted, embeddedITestReceived,
	)
	balance, err := node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, embeddedITestReceived, balance)

	// The engine stores the file Alice exported and reads it as her tapd
	// does.
	requireEmbeddedProofFile(
		t, ctx, node, received.IssuanceID, received.ScriptKey,
		received.Outpoint, receivedProof.RawProofFile,
	)
	requireSameProofAnswers(
		t, ctx, h.AliceClient, node.Client, receivedProof.RawProofFile,
	)

	bobKeys, err := h.BobWallet.DeriveKeys(ctx)
	require.NoError(t, err)
	first := commitEmbeddedTransfer(
		t, ctx, h, node, lnd, minted.Ref, receivedProof,
		embeddedITestReceived, bobKeys, embeddedITestSent,
	)
	publishEmbeddedTransfer(t, ctx, node, first)

	// The spent output leaves the wallet at once. The change arrives
	// with the confirmation.
	balance, err = node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Zero(t, balance)

	h.MineBlocks(t, defaultMineBlocks)
	node.WaitForTransferState(t, ctx, first.txid, embedded.TransferFinal)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	change := embeddedITestReceived - embeddedITestSent
	balance, err = node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, change, balance)

	h.EnableUniverseBootstrap(t, ctx)
	requireBobImportsEmbeddedOutput(
		t, ctx, h, node, first.recipient, embeddedITestSent,
	)
	h.WaitForBalance(
		t, ctx, h.BobWallet, minted.Ref, embeddedITestSent,
		balanceTimeoutFor(minted.Ref),
	)

	// The change proof is a file the engine wrote. Alice's tapd accepts
	// it, and the engine can spend it.
	changeProof, err := node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(first.change.IssuanceID),
		first.change.ScriptKey, &first.change.AnchorOutpoint,
	)
	require.NoError(t, err)
	requireSameProofAnswers(
		t, ctx, h.AliceClient, node.Client, changeProof.RawProofFile,
	)

	bobKeys, err = h.BobWallet.DeriveKeys(ctx)
	require.NoError(t, err)
	second := commitEmbeddedTransfer(
		t, ctx, h, node, lnd, minted.Ref, changeProof, change,
		bobKeys, change,
	)
	publishEmbeddedTransfer(t, ctx, node, second)

	h.MineBlocks(t, defaultMineBlocks)
	node.WaitForTransferState(t, ctx, second.txid, embedded.TransferFinal)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	balance, err = node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Zero(t, balance)

	requireBobImportsEmbeddedOutput(
		t, ctx, h, node, second.recipient, change,
	)
	h.WaitForBalance(
		t, ctx, h.BobWallet, minted.Ref, embeddedITestReceived,
		balanceTimeoutFor(minted.Ref),
	)
	h.WaitForBalance(
		t, ctx, h.AliceWallet, minted.Ref,
		embeddedITestSupply-embeddedITestReceived,
		balanceTimeoutFor(minted.Ref),
	)
}

// TestEmbeddedReorg takes a transfer of an embedded engine through a re-org
// of its confirming block: the change leaves the wallet, and returns with a
// proof of the block that confirms the transfer again. The engine learns to
// trust the asset's group only after it is created.
func TestEmbeddedReorg(t *testing.T) {
	h, ctx := newFundedHarness(t)
	lnd := newAliceLndWalletKit(t)

	minted, err := h.CreateFungibleAndConfirm(
		t, ctx, uniqueEventLabel("embedded-reorg"),
		embeddedITestSupply,
	)
	require.NoError(t, err)
	groupKey, ok := minted.Ref.GroupKey()
	require.True(t, ok)

	// A proof of a group the engine does not trust is refused.
	node := h.NewEmbeddedNode(t)
	_, receivedProof := h.ProofForEmbedded(
		t, ctx, node, minted, embeddedITestReceived,
	)
	verified, err := node.Client.VerifyProof(
		ctx, receivedProof.RawProofFile,
	)
	require.NoError(t, err)
	require.False(t, verified.Valid)
	_, err = node.Wallet.ImportProofFile(ctx, receivedProof)
	require.ErrorContains(t, err, "is not trusted by this wallet")

	node.Client.TrustGroupKey(groupKey)
	registered, err := node.Wallet.ImportProofFile(ctx, receivedProof)
	require.NoError(t, err)
	require.Equal(t, embeddedITestReceived, registered.Amount)

	bobKeys, err := h.BobWallet.DeriveKeys(ctx)
	require.NoError(t, err)
	sent := commitEmbeddedTransfer(
		t, ctx, h, node, lnd, minted.Ref, receivedProof,
		embeddedITestReceived, bobKeys, embeddedITestSent,
	)
	publishEmbeddedTransfer(t, ctx, node, sent)

	change := embeddedITestReceived - embeddedITestSent
	h.MineBlocks(t, 1)
	node.WaitForTransferState(
		t, ctx, sent.txid, embedded.TransferConfirmed,
	)
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, change, defaultWaitTimeout,
	)
	firstBlock := embeddedTransferBlock(t, ctx, node, sent.txid)

	// The confirming block leaves the chain, and the anchor transaction
	// returns to the mempool.
	require.Equal(t, firstBlock, h.invalidateBestBlock(t))
	node.WaitForTransferState(t, ctx, sent.txid, embedded.TransferPending)
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, 0, defaultWaitTimeout,
	)
	_, err = node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(sent.change.IssuanceID),
		sent.change.ScriptKey, &sent.change.AnchorOutpoint,
	)
	require.Error(t, err)

	h.MineBlocks(t, defaultMineBlocks)
	node.WaitForTransferState(t, ctx, sent.txid, embedded.TransferFinal)
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, change, defaultWaitTimeout,
	)
	require.NotEqual(
		t, firstBlock, embeddedTransferBlock(t, ctx, node, sent.txid),
	)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	// Both proofs commit to the new block, which tapd checks against its
	// own chain backend.
	changeProof, err := node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(sent.change.IssuanceID),
		sent.change.ScriptKey, &sent.change.AnchorOutpoint,
	)
	require.NoError(t, err)
	requireSameProofAnswers(
		t, ctx, h.AliceClient, node.Client, changeProof.RawProofFile,
	)

	h.EnableUniverseBootstrap(t, ctx)
	requireBobImportsEmbeddedOutput(
		t, ctx, h, node, sent.recipient, embeddedITestSent,
	)
	h.WaitForBalance(
		t, ctx, h.BobWallet, minted.Ref, embeddedITestSent,
		balanceTimeoutFor(minted.Ref),
	)
}

// embeddedTransfer is a transfer the engine committed and whose funding
// input lnd signed.
type embeddedTransfer struct {
	sealed    *customanchor.TransferPackage
	finalPsbt []byte
	txid      chainhash.Hash

	// recipient is the output of the receiver.
	recipient customanchor.AssetOutputSummary

	// change is the output that returns the surplus to the engine. It is
	// unset for a transfer of the whole input.
	change customanchor.AssetOutputSummary
}

// commitEmbeddedTransfer commits a transfer of amount units to the receiver's
// keys. It spends the engine's asset output at the tip of inputProof, which
// holds inputAmount units, and returns the surplus to new keys of the engine.
//
// The anchor transaction has two inputs. The engine signs the asset anchor
// when the transfer is published. The other is an output of lnd-alice that
// pays the fee and collects the Bitcoin change, and lnd signs it here.
func commitEmbeddedTransfer(t *testing.T, ctx context.Context,
	h *TestHarness, node *EmbeddedNode, lnd *lndWalletKit,
	ref tapsdk.AssetRef, inputProof *tapsdk.ProofFile, inputAmount uint64,
	receiver *tapsdk.DerivedKeys, amount uint64) *embeddedTransfer {

	t.Helper()

	proofFile, err := proof.DecodeFile(inputProof.RawProofFile)
	require.NoError(t, err)
	lastProof, err := proofFile.LastProof()
	require.NoError(t, err)
	inputOutpoint := lastProof.OutPoint()
	inputValue := lastProof.AnchorTx.TxOut[inputOutpoint.Index].Value

	funding := h.fundLndInput(t, ctx, lnd)

	anchorTx := wire.NewMsgTx(2)
	anchorTx.AddTxIn(wire.NewTxIn(&inputOutpoint, nil, nil))
	anchorTx.AddTxIn(wire.NewTxIn(&funding.outpoint, nil, nil))
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    embeddedITestAnchorSat,
		PkScript: embeddedAnchorPlaceholder(t, receiver.InternalKey),
	})
	assetOutput := func(id string, index uint32, units uint64,
		keys *tapsdk.DerivedKeys) customanchor.AssetOutput {

		return customanchor.AssetOutput{
			ID:                id,
			AssetRef:          ref,
			Amount:            units,
			AnchorOutputIndex: index,
			AnchorValueSat:    uint64(embeddedITestAnchorSat),
			Script: customanchor.AssetScriptPlan{
				Mode: customanchor.AssetScriptExternal,
				External: &customanchor.AssetExternalScriptPlan{
					ScriptKey: keys.ScriptKey,
				},
			},
			Anchor: customanchor.OutputPlan{
				InternalKey: keys.InternalKey,
			},
		}
	}
	outputs := []customanchor.AssetOutput{
		assetOutput("recipient", 0, amount, receiver),
	}

	if amount < inputAmount {
		changeKeys, err := node.Wallet.DeriveKeys(ctx)
		require.NoError(t, err)

		anchorTx.AddTxOut(&wire.TxOut{
			Value: embeddedITestAnchorSat,
			PkScript: embeddedAnchorPlaceholder(
				t, changeKeys.InternalKey,
			),
		})
		outputs = append(outputs, assetOutput(
			"change", 1, inputAmount-amount, changeKeys,
		))
	}

	bitcoinChange := inputValue + funding.txOut.Value - embeddedITestFeeSat
	for _, txOut := range anchorTx.TxOut {
		bitcoinChange -= txOut.Value
	}
	anchorTx.AddTxOut(&wire.TxOut{
		Value:    bitcoinChange,
		PkScript: funding.txOut.PkScript,
	})

	packet, err := psbt.NewFromUnsignedTx(anchorTx)
	require.NoError(t, err)
	packet.Inputs[1] = funding.psbtInput()
	packet.Outputs[len(packet.Outputs)-1] = funding.psbtChangeOutput()
	var template bytes.Buffer
	require.NoError(t, packet.Serialize(&template))

	request := &customanchor.Request{
		Inputs: []customanchor.AssetInput{{
			ID:        "input",
			AssetRef:  ref,
			Amount:    inputAmount,
			ProofFile: inputProof.RawProofFile,
			Witness: customanchor.AssetWitnessPlan{
				Mode: customanchor.AssetWitnessBackendSigner,
			},
		}},
		Outputs:    outputs,
		AnchorPSBT: template.Bytes(),
		Funding: customanchor.FundingPlan{
			Mode: customanchor.
				FundingCallerFundedExact,
			CallerFundedExact: &customanchor.CallerFundedExact{},
		},
		PassiveAssets: customanchor.PassiveAssets{
			Policy: customanchor.PassiveReject,
		},
		LossPolicy: customanchor.LossPolicy{
			Mode: customanchor.LossReject,
		},
		SigningPlans: []customanchor.InputSigningPlan{{
			InputIndex:    0,
			BackendSigned: &customanchor.BackendSignedPlan{},
		}, {
			InputIndex:   1,
			CallerSigned: &customanchor.CallerSignedPlan{},
		}},
	}

	plan, err := customanchor.NewTxBuilder(node.Wallet).Build(ctx, request)
	require.NoError(t, err)
	verification := plan.Verification()
	require.True(t, verification.Valid())

	sealed, err := plan.Commit(ctx, customanchor.CommitOptions{
		Publish: customanchor.PublishMetadata{
			Label: uniqueEventLabel("embedded-transfer"),
		},
	})
	require.NoError(t, err)
	require.NoError(t, sealed.Validate())
	require.Len(t, sealed.Outputs, len(outputs))

	// The engine signs its anchor at publish, and lnd's funding input
	// is the caller's to sign, so the package asks for no signature.
	signingRequests, err := sealed.SigningRequests()
	require.NoError(t, err)
	require.Empty(t, signingRequests.KeyPath)
	require.Empty(t, signingRequests.MuSig2)
	require.Empty(t, signingRequests.ScriptPath)

	signedPsbt, err := lnd.signPsbt(ctx, sealed.AnchorPsbt)
	require.NoError(t, err)
	finalPsbt := customAnchorFinalizeKeyPath(t, signedPsbt, 1)
	require.NoError(t, sealed.VerifyFinalAnchorPSBT(finalPsbt))
	finalPacket := decodeCustomAnchorITestPSBT(t, finalPsbt)
	require.Empty(t, finalPacket.Inputs[0].FinalScriptWitness)

	transfer := &embeddedTransfer{
		sealed:    sealed,
		finalPsbt: finalPsbt,
		txid:      finalPacket.UnsignedTx.TxHash(),
	}
	for _, output := range sealed.Outputs {
		switch output.LogicalOutputID {
		case "recipient":
			transfer.recipient = output

		case "change":
			transfer.change = output
		}
	}
	require.Equal(t, receiver.ScriptKey.PubKey,
		transfer.recipient.ScriptKey)
	require.Equal(t, amount, transfer.recipient.Amount)

	return transfer
}

// embeddedAnchorPlaceholder returns the script an asset anchor output has in
// the anchor transaction template, before the commitment replaces it.
func embeddedAnchorPlaceholder(t testing.TB,
	internalKey tapsdk.InternalKey) []byte {

	t.Helper()

	pubKey, err := btcec.ParsePubKey(internalKey.PubKey[:])
	require.NoError(t, err)
	pkScript, err := txscript.PayToTaprootScript(
		txscript.ComputeTaprootKeyNoScript(pubKey),
	)
	require.NoError(t, err)

	return pkScript
}

// publishEmbeddedTransfer publishes a committed transfer through the engine,
// which signs its asset anchor and broadcasts the anchor transaction.
func publishEmbeddedTransfer(t *testing.T, ctx context.Context,
	node *EmbeddedNode, transfer *embeddedTransfer) {

	t.Helper()

	published, err := customanchor.PublishTransfer(
		ctx, node.Wallet, transfer.sealed, transfer.finalPsbt,
	)
	require.NoError(t, err)

	var anchorTx wire.MsgTx
	require.NoError(t, anchorTx.Deserialize(
		bytes.NewReader(published.AnchorTransaction),
	))
	require.Equal(t, transfer.txid, anchorTx.TxHash())
	require.Len(t, anchorTx.TxIn[0].Witness, 1)
	require.Len(t, anchorTx.TxIn[1].Witness, 1)

	node.WaitForTransferState(
		t, ctx, transfer.txid, embedded.TransferPending,
	)
}

// embeddedTransferBlock returns the block the engine recorded as confirming
// a transfer.
func embeddedTransferBlock(t *testing.T, ctx context.Context,
	node *EmbeddedNode, txid chainhash.Hash) chainhash.Hash {

	t.Helper()

	transfers, err := node.Store.ListTransfers(ctx, &txid)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	require.NotZero(t, transfers[0].BlockHash)

	return transfers[0].BlockHash
}

// requireEmbeddedProofFile checks that the engine exports the given proof
// file for an asset output.
func requireEmbeddedProofFile(t *testing.T, ctx context.Context,
	node *EmbeddedNode, issuanceID tapsdk.AssetID, scriptKey tapsdk.PubKey,
	outpoint tapsdk.Outpoint, want []byte) {

	t.Helper()

	exported, err := node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(issuanceID), scriptKey,
		&outpoint,
	)
	require.NoError(t, err)
	require.Equal(t, want, exported.RawProofFile)
}

// requireSameProofAnswers checks that a tapd and the engine verify, unpack
// and decode a valid proof file to the same answers.
func requireSameProofAnswers(t *testing.T, ctx context.Context,
	tapd tapsdk.Client, engine tapsdk.Client, rawProofFile []byte) {

	t.Helper()

	want, err := tapd.VerifyProof(ctx, rawProofFile)
	require.NoError(t, err)
	require.True(t, want.Valid)
	got, err := engine.VerifyProof(ctx, rawProofFile)
	require.NoError(t, err)
	require.Equal(t, want, got)

	wantProofs, err := tapd.UnpackProofFile(ctx, rawProofFile)
	require.NoError(t, err)
	gotProofs, err := engine.UnpackProofFile(ctx, rawProofFile)
	require.NoError(t, err)
	require.Equal(t, wantProofs, gotProofs)

	for _, rawProof := range wantProofs {
		want, err := tapd.DecodeProof(ctx, rawProof)
		require.NoError(t, err)
		got, err := engine.DecodeProof(ctx, rawProof)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

// requireBobImportsEmbeddedOutput exports the proof file of a transfer output
// from the engine and imports it into Bob's tapd.
func requireBobImportsEmbeddedOutput(t *testing.T, ctx context.Context,
	h *TestHarness, node *EmbeddedNode,
	output customanchor.AssetOutputSummary, amount uint64) {

	t.Helper()

	proofFile, err := node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(output.IssuanceID),
		output.ScriptKey, &output.AnchorOutpoint,
	)
	require.NoError(t, err)

	registered, err := h.BobWallet.ImportProofFile(ctx, proofFile)
	require.NoError(t, err)
	require.Equal(t, output.IssuanceID, registered.IssuanceID)
	require.Equal(t, output.ScriptKey, registered.ScriptKey)
	require.Equal(t, output.AnchorOutpoint, registered.Outpoint)
	require.Equal(t, amount, registered.Amount)

	requireSameProofAnswers(
		t, ctx, h.BobClient, node.Client, proofFile.RawProofFile,
	)
}
