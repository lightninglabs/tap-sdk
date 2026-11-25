package embedded

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/tapscript"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testTransfer is a committed transfer and what the test needs to publish
// and check it.
type testTransfer struct {
	transfer  *customanchor.TransferPackage
	finalPSBT []byte
	txid      chainhash.Hash
	recipient tapsdk.ScriptKey
	change    *tapsdk.ScriptKey
}

// commitTransfer commits a transfer of amount units of the wallet's asset
// held at source to a recipient outside the wallet. The surplus returns to
// the wallet. The anchor transaction spends the wallet's anchor and a
// caller-owned coin, and only the caller's input is signed.
func (h *testHarness) commitTransfer(source *mintedAsset, total,
	amount uint64, seed byte) *testTransfer {

	h.t.Helper()

	recipientKeys := newTestKeyRing(seed)
	recipientRaw, err := recipientKeys.DeriveNextKey(1)
	require.NoError(h.t, err)
	recipientAnchor, err := recipientKeys.DeriveNextKey(1)
	require.NoError(h.t, err)
	recipientOutput := txscript.ComputeTaprootKeyNoScript(
		recipientRaw.PubKey,
	)
	recipient := tapsdk.ScriptKey{
		PubKey: sdkPubKey(h.t, evenKey(recipientOutput)),
		KeyDesc: tapsdk.KeyDescriptor{
			RawKeyBytes: sdkPubKey(h.t, recipientRaw.PubKey),
		},
	}

	result := h.commitTransferTo(
		source, total, amount, seed, customanchor.AssetScriptPlan{
			Mode: customanchor.AssetScriptExternal,
			External: &customanchor.AssetExternalScriptPlan{
				ScriptKey: recipient,
			},
		}, recipientAnchor.PubKey,
	)
	result.recipient = recipient

	return result
}

// commitTransferTo is commitTransfer to a recipient with the given asset
// script, in an anchor output under the given internal key.
func (h *testHarness) commitTransferTo(source *mintedAsset, total,
	amount uint64, seed byte, recipientScript customanchor.AssetScriptPlan,
	recipientAnchor *btcec.PublicKey) *testTransfer {

	h.t.Helper()

	return h.commitTransferFrom(
		[]assetSource{{asset: source, units: total}}, amount, seed,
		recipientScript, recipientAnchor,
	)
}

// assetSource is an asset output of the wallet that a transfer spends.
type assetSource struct {
	asset *mintedAsset
	units uint64
}

// commitTransferFrom is commitTransferTo for a transfer that spends one or
// more outputs of an asset. The anchor output of the recipient commits to the
// given tapscript leaves.
func (h *testHarness) commitTransferFrom(sources []assetSource,
	amount uint64, seed byte, recipientScript customanchor.AssetScriptPlan,
	recipientAnchor *btcec.PublicKey,
	anchorLeaves ...tapsdk.TapLeaf) *testTransfer {

	h.t.Helper()
	ctx := h.t.Context()

	funding := newFundingInput(h.t, seed, 100_000)
	fundingIndex := len(sources)

	var (
		tx       = wire.NewMsgTx(2)
		ref      = sources[0].asset.ref
		total    uint64
		inputs   []customanchor.AssetInput
		signings []customanchor.InputSigningPlan
	)
	for idx, source := range sources {
		tx.AddTxIn(wire.NewTxIn(&source.asset.outpoint, nil, nil))
		total += source.units

		inputs = append(inputs, customanchor.AssetInput{
			ID:        fmt.Sprintf("input-%d", idx),
			AssetRef:  ref,
			Amount:    source.units,
			ProofFile: source.asset.proofFile,
			Witness: customanchor.AssetWitnessPlan{
				Mode: customanchor.AssetWitnessBackendSigner,
			},
		})
		signings = append(signings, customanchor.InputSigningPlan{
			InputIndex:    uint32(idx),
			BackendSigned: &customanchor.BackendSignedPlan{},
		})
	}
	signings = append(signings, customanchor.InputSigningPlan{
		InputIndex:   uint32(fundingIndex),
		CallerSigned: &customanchor.CallerSignedPlan{},
	})

	tx.AddTxIn(wire.NewTxIn(&funding.outpoint, nil, nil))
	placeholder := []byte{txscript.OP_1, txscript.OP_DATA_32}
	placeholder = append(placeholder, make([]byte, 32)...)
	tx.AddTxOut(&wire.TxOut{Value: 1_000, PkScript: placeholder})

	request := &customanchor.Request{
		Inputs: inputs,
		Outputs: []customanchor.AssetOutput{{
			ID:                "recipient",
			AssetRef:          ref,
			Amount:            amount,
			AnchorOutputIndex: 0,
			AnchorValueSat:    1_000,
			Script:            recipientScript,
			Anchor: customanchor.OutputPlan{
				InternalKey: tapsdk.InternalKey{
					PubKey: sdkPubKey(h.t, recipientAnchor),
				},
				Tapscript: customanchor.TapscriptPlan{
					TapLeaves: anchorLeaves,
				},
			},
		}},
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
		SigningPlans: signings,
	}

	result := &testTransfer{}
	if amount < total {
		changeKey, err := h.client.DeriveScriptKey(ctx)
		require.NoError(h.t, err)
		changeAnchor, err := h.client.DeriveInternalKey(ctx)
		require.NoError(h.t, err)
		result.change = changeKey

		tx.AddTxOut(&wire.TxOut{Value: 1_000, PkScript: placeholder})
		request.Outputs = append(
			request.Outputs, customanchor.AssetOutput{
				ID:                "change",
				AssetRef:          ref,
				Amount:            total - amount,
				AnchorOutputIndex: 1,
				AnchorValueSat:    1_000,
				Script: customanchor.AssetScriptPlan{
					Mode: customanchor.AssetScriptExternal,
					External: &customanchor.
						AssetExternalScriptPlan{
						ScriptKey: *changeKey,
					},
				},
				Anchor: customanchor.OutputPlan{
					InternalKey: *changeAnchor,
				},
			},
		)
	}

	packet, err := psbt.NewFromUnsignedTx(tx)
	require.NoError(h.t, err)
	packet.Inputs[fundingIndex].WitnessUtxo = funding.txOut
	var template bytes.Buffer
	require.NoError(h.t, packet.Serialize(&template))
	request.AnchorPSBT = template.Bytes()

	plan, err := customanchor.NewTxBuilder(h.wallet).Build(ctx, request)
	require.NoError(h.t, err)
	transfer, err := plan.Commit(ctx, customanchor.CommitOptions{
		Publish: customanchor.PublishMetadata{Label: "test-transfer"},
	})
	require.NoError(h.t, err)

	committed, err := psbt.NewFromRawBytes(
		bytes.NewReader(transfer.AnchorPsbt), false,
	)
	require.NoError(h.t, err)
	funding.sign(h.t, committed)
	var final bytes.Buffer
	require.NoError(h.t, committed.Serialize(&final))
	require.NoError(h.t, transfer.VerifyFinalAnchorPSBT(final.Bytes()))

	result.transfer = transfer
	result.finalPSBT = final.Bytes()
	result.txid = committed.UnsignedTx.TxHash()

	return result
}

// lockedAsset is a confirmed asset output under an OP_TRUE script key, in an
// anchor output whose key no wallet holds. It stands for an output a
// contract holds, which whoever satisfies the contract spends.
type lockedAsset struct {
	ref       tapsdk.AssetRef
	amount    uint64
	scriptKey tapsdk.PubKey
	witness   [][]byte
	proofFile []byte
	anchor    *fundingInput
}

// lockAsset moves amount units of the wallet's asset held at source into a
// locked output and confirms the transfer. The surplus returns to the
// wallet.
func (h *testHarness) lockAsset(source *mintedAsset, total, amount uint64,
	seed byte) *lockedAsset {

	h.t.Helper()
	ctx := h.t.Context()

	keys := newTestKeyRing(seed)
	anchorKey := keys.privKey(1, 0)
	scriptInternalKey := keys.privKey(1, 1).PubKey()
	sent := h.commitTransferTo(
		source, total, amount, seed, customanchor.AssetScriptPlan{
			Mode: customanchor.AssetScriptOPTrue,
			OPTrue: &customanchor.AssetOPTrueScriptPlan{
				InternalKey: tapsdk.KeyDescriptor{
					RawKeyBytes: sdkPubKey(
						h.t, scriptInternalKey,
					),
				},
			},
		}, anchorKey.PubKey(),
	)
	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(h.t, err)
	h.chain.mine(h.t)
	h.waitTransfer(sent.txid, TransferConfirmed)

	var output *customanchor.AssetOutputSummary
	for idx := range sent.transfer.Outputs {
		if sent.transfer.Outputs[idx].OPTrueSpend != nil {
			output = &sent.transfer.Outputs[idx]
		}
	}
	require.NotNil(h.t, output)

	exported, err := h.client.ExportProof(
		ctx, source.ref, output.ScriptKey, nil,
	)
	require.NoError(h.t, err)

	anchorPacket, err := psbt.NewFromRawBytes(
		bytes.NewReader(sent.finalPSBT), false,
	)
	require.NoError(h.t, err)
	anchorIndex := output.AnchorOutputIndex

	return &lockedAsset{
		ref:       source.ref,
		amount:    amount,
		scriptKey: output.ScriptKey,
		witness:   output.OPTrueSpend.WitnessStack(),
		proofFile: exported.RawProofFile,
		anchor: &fundingInput{
			privKey: anchorKey,
			outpoint: wire.OutPoint{
				Hash:  sent.txid,
				Index: anchorIndex,
			},
			txOut:      anchorPacket.UnsignedTx.TxOut[anchorIndex],
			merkleRoot: output.TaprootMerkleRoot[:],
		},
	}
}

// commitClaim commits a transfer of a locked asset to keys the wallet
// derives. The anchor transaction spends only the locked output, at its full
// value, and the key of that output signs it.
func (h *testHarness) commitClaim(locked *lockedAsset) *testTransfer {
	h.t.Helper()
	ctx := h.t.Context()

	scriptKey, err := h.client.DeriveScriptKey(ctx)
	require.NoError(h.t, err)
	internalKey, err := h.client.DeriveInternalKey(ctx)
	require.NoError(h.t, err)

	tx := wire.NewMsgTx(2)
	tx.AddTxIn(wire.NewTxIn(&locked.anchor.outpoint, nil, nil))
	placeholder := []byte{txscript.OP_1, txscript.OP_DATA_32}
	placeholder = append(placeholder, make([]byte, 32)...)
	tx.AddTxOut(&wire.TxOut{
		Value:    locked.anchor.txOut.Value,
		PkScript: placeholder,
	})

	packet, err := psbt.NewFromUnsignedTx(tx)
	require.NoError(h.t, err)
	packet.Inputs[0].WitnessUtxo = locked.anchor.txOut
	var template bytes.Buffer
	require.NoError(h.t, packet.Serialize(&template))

	request := &customanchor.Request{
		Inputs: []customanchor.AssetInput{{
			ID:        "locked",
			AssetRef:  locked.ref,
			Amount:    locked.amount,
			ProofFile: locked.proofFile,
			Witness: customanchor.AssetWitnessPlan{
				Mode:  customanchor.AssetWitnessCallerProvided,
				Stack: locked.witness,
			},
		}},
		Outputs: []customanchor.AssetOutput{{
			ID:                "claimed",
			AssetRef:          locked.ref,
			Amount:            locked.amount,
			AnchorOutputIndex: 0,
			AnchorValueSat:    uint64(locked.anchor.txOut.Value),
			Script: customanchor.AssetScriptPlan{
				Mode: customanchor.AssetScriptExternal,
				External: &customanchor.AssetExternalScriptPlan{
					ScriptKey: *scriptKey,
				},
			},
			Anchor: customanchor.OutputPlan{
				InternalKey: *internalKey,
			},
		}},
		AnchorPSBT: template.Bytes(),
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
			InputIndex:   0,
			CallerSigned: &customanchor.CallerSignedPlan{},
		}},
	}

	plan, err := customanchor.NewTxBuilder(h.wallet).Build(ctx, request)
	require.NoError(h.t, err)
	transfer, err := plan.Commit(ctx, customanchor.CommitOptions{
		Publish: customanchor.PublishMetadata{Label: "test-claim"},
	})
	require.NoError(h.t, err)

	committed, err := psbt.NewFromRawBytes(
		bytes.NewReader(transfer.AnchorPsbt), false,
	)
	require.NoError(h.t, err)
	locked.anchor.sign(h.t, committed)
	var final bytes.Buffer
	require.NoError(h.t, committed.Serialize(&final))
	require.NoError(h.t, transfer.VerifyFinalAnchorPSBT(final.Bytes()))

	return &testTransfer{
		transfer:  transfer,
		finalPSBT: final.Bytes(),
		txid:      committed.UnsignedTx.TxHash(),
		change:    scriptKey,
	}
}

// inputRecords returns the wallet's proof records of a locked asset.
func (h *testHarness) inputRecords(locked *lockedAsset) []AssetProof {
	h.t.Helper()

	assetID, ok := locked.ref.AssetID()
	require.True(h.t, ok)
	records, err := h.store.FetchProofs(
		h.t.Context(), assetID, locked.scriptKey,
		&locked.anchor.outpoint,
	)
	require.NoError(h.t, err)

	return records
}

// sdkPubKey converts a public key to the SDK's compressed form.
func sdkPubKey(t *testing.T, key *btcec.PublicKey) tapsdk.PubKey {
	t.Helper()

	pubKey, err := tapsdk.ParsePubKey(key.SerializeCompressed())
	require.NoError(t, err)

	return pubKey
}

// evenKey returns the key with an even Y coordinate, the form of an asset
// script key.
func evenKey(key *btcec.PublicKey) *btcec.PublicKey {
	serialized := scriptKeyBytes(key)
	even, _ := btcec.ParsePubKey(serialized[:])

	return even
}

// waitTransfer waits until the transfer anchored by txid reaches a state.
func (h *testHarness) waitTransfer(txid chainhash.Hash,
	state TransferState) {

	h.t.Helper()

	require.Eventually(h.t, func() bool {
		transfers, err := h.store.ListTransfers(h.t.Context(), &txid)

		return err == nil && len(transfers) == 1 &&
			transfers[0].State == state
	}, 10*time.Second, 10*time.Millisecond)
}

// ownedUnits sums the units of the wallet's unspent asset outputs.
func (h *testHarness) ownedUnits() uint64 {
	h.t.Helper()

	utxos, err := h.client.ListUtxos(
		h.t.Context(), &tapsdk.ListUtxosRequest{},
	)
	require.NoError(h.t, err)

	var units uint64
	for _, utxo := range utxos {
		for _, held := range utxo.Assets {
			units += held.Amount
		}
	}

	return units
}

// TestReceive imports a proof file through the SDK wallet and lists the
// asset it adds.
func TestReceive(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("receive")

	verified, err := h.client.VerifyProof(ctx, minted.proofFile)
	require.NoError(t, err)
	require.True(t, verified.Valid)
	require.EqualValues(t, 1_000, verified.DecodedProof.Amount)

	utxos, err := h.client.ListUtxos(ctx, &tapsdk.ListUtxosRequest{})
	require.NoError(t, err)
	require.Len(t, utxos, 1)
	utxo := utxos[minted.outpoint.String()]
	require.NotNil(t, utxo)
	require.EqualValues(t, 1_000, utxo.AmtSat)
	require.Len(t, utxo.Assets, 1)
	require.EqualValues(t, 1_000, utxo.Assets[0].Amount)
	require.True(t, utxo.Assets[0].AssetRef.Equivalent(minted.ref))

	exported, err := h.client.ExportProof(
		ctx, minted.ref, utxo.Assets[0].ScriptKey.PubKey, nil,
	)
	require.NoError(t, err)
	require.Equal(t, minted.proofFile, exported.RawProofFile)

	// Importing the same file again changes nothing.
	_, err = h.wallet.ImportProofFile(
		ctx, &tapsdk.ProofFile{RawProofFile: minted.proofFile},
	)
	require.NoError(t, err)
	require.EqualValues(t, 1_000, h.ownedUnits())
}

// TestListUniquePedersenKeys lists an asset under a script key that a sender
// derived from a wallet key for the asset, as for a BIP-86 key.
func TestListUniquePedersenKeys(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	rawKey, err := h.client.DeriveInternalKey(ctx)
	require.NoError(t, err)
	rawPubKey, err := btcec.ParsePubKey(rawKey.PubKey[:])
	require.NoError(t, err)
	internalKey, err := h.client.DeriveInternalKey(ctx)
	require.NoError(t, err)

	scriptKey, err := asset.DeriveUniqueScriptKey(
		*rawPubKey, testGenesis("pedersen").ID(),
		asset.ScriptKeyDerivationUniquePedersen,
	)
	require.NoError(t, err)
	scriptKey.RawKey, err = h.client.engine.fetchKeyDescriptor(
		ctx, rawPubKey,
	)
	require.NoError(t, err)
	require.NoError(t, h.client.engine.insertScriptKey(ctx, scriptKey))

	minted := h.mintTo(&tapsdk.ScriptKey{
		PubKey: sdkPubKey(t, scriptKey.PubKey),
	}, internalKey, 500, "pedersen")
	_, err = h.wallet.ImportProofFile(
		ctx, &tapsdk.ProofFile{RawProofFile: minted.proofFile},
	)
	require.NoError(t, err)

	require.EqualValues(t, 500, h.ownedUnits())
	balance, err := h.wallet.GetBalance(ctx, minted.ref)
	require.NoError(t, err)
	require.EqualValues(t, 500, balance)

	// A query for another type leaves the asset out.
	listed, err := h.client.engine.ownedAssets(
		ctx, false, fn.Some(asset.ScriptKeyScriptPathExternal),
	)
	require.NoError(t, err)
	require.Empty(t, listed)
	listed, err = h.client.engine.ownedAssets(
		ctx, false, fn.Some(asset.ScriptKeyUniquePedersen),
	)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

// TestReceiveUnknownScriptKey refuses a proof whose asset the wallet could
// not spend.
func TestReceiveUnknownScriptKey(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	foreign := newTestKeyRing(9)
	rawKey, err := foreign.DeriveNextKey(1)
	require.NoError(t, err)
	anchorKey, err := foreign.DeriveNextKey(1)
	require.NoError(t, err)

	outputKey := txscript.ComputeTaprootKeyNoScript(rawKey.PubKey)
	minted := h.mintTo(&tapsdk.ScriptKey{
		PubKey: sdkPubKey(t, evenKey(outputKey)),
	}, &tapsdk.InternalKey{
		PubKey: sdkPubKey(t, anchorKey.PubKey),
	}, 500, "foreign")

	_, err = h.wallet.ImportProofFile(
		ctx, &tapsdk.ProofFile{RawProofFile: minted.proofFile},
	)
	require.ErrorContains(t, err, "error fetching script key")
	require.Zero(t, h.ownedUnits())
}

// TestTransferLifecycle sends part of a received asset through the
// custom-anchor flow and follows the transfer to its final proofs.
func TestTransferLifecycle(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("lifecycle")
	sent := h.commitTransfer(minted, 1_000, 600, 7)

	caps, err := customanchor.QueryCapabilities(ctx, h.wallet)
	require.NoError(t, err)
	require.NoError(t, caps.Require(
		customanchor.CapabilityBackendAnchorSigning,
		customanchor.CapabilitySkipFunding,
	))

	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(t, err)

	// The spent asset leaves the wallet at once; the change arrives with
	// the confirmation.
	require.Zero(t, h.ownedUnits())
	transfers, err := h.client.ListTransfers(
		ctx, &tapsdk.ListTransfersRequest{
			AnchorTxid: sent.txid.String(),
		},
	)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	require.Equal(t, [32]byte{}, transfers[0].AnchorTxBlockHash)
	require.Len(t, transfers[0].Outputs, 2)

	// Publishing the same transfer again returns the recorded one.
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(t, err)

	block, height := h.chain.mine(t)
	h.waitTransfer(sent.txid, TransferConfirmed)
	require.EqualValues(t, 400, h.ownedUnits())

	transfers, err = h.client.ListTransfers(
		ctx, &tapsdk.ListTransfersRequest{
			AnchorTxid: sent.txid.String(),
		},
	)
	require.NoError(t, err)
	require.Equal(
		t, [32]byte(block.BlockHash()), transfers[0].AnchorTxBlockHash,
	)
	require.Equal(t, height, transfers[0].AnchorTxBlockHeight)

	// Both outputs have a proof file that verifies against the chain.
	for _, output := range transfers[0].Outputs {
		exported, err := h.client.ExportProof(
			ctx, tapsdk.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)
		require.NoError(t, err)

		verified, err := h.client.VerifyProof(
			ctx, exported.RawProofFile,
		)
		require.NoError(t, err)
		require.True(t, verified.Valid)
		require.Equal(t, output.Amount, verified.DecodedProof.Amount)
	}

	h.chain.finalize(sent.txid)
	h.waitTransfer(sent.txid, TransferFinal)
}

// TestTransferSpendsChange spends the change of a confirmed transfer, which
// extends the proof file the engine wrote for it.
func TestTransferSpendsChange(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("change")
	first := h.commitTransfer(minted, 1_000, 600, 7)
	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, first.transfer, first.finalPSBT,
	)
	require.NoError(t, err)
	h.chain.mine(t)
	h.waitTransfer(first.txid, TransferConfirmed)

	changeProof, err := h.client.ExportProof(
		ctx, minted.ref, first.change.PubKey, nil,
	)
	require.NoError(t, err)

	second := h.commitTransfer(&mintedAsset{
		ref:       minted.ref,
		assetID:   minted.assetID,
		proofFile: changeProof.RawProofFile,
		outpoint:  wire.OutPoint{Hash: first.txid, Index: 1},
	}, 400, 400, 8)
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, second.transfer, second.finalPSBT,
	)
	require.NoError(t, err)
	h.chain.mine(t)
	h.waitTransfer(second.txid, TransferConfirmed)
	require.Zero(t, h.ownedUnits())

	recipientProof, err := h.client.ExportProof(
		ctx, minted.ref, second.recipient.PubKey, nil,
	)
	require.NoError(t, err)
	verified, err := h.client.VerifyProof(ctx, recipientProof.RawProofFile)
	require.NoError(t, err)
	require.True(t, verified.Valid)
	require.EqualValues(t, 400, verified.DecodedProof.Amount)
}

// TestTransferSpendsTweakedOutput sends an asset to a script key tweaked
// from a key the wallet derived, as each issuance of a grouped output is.
// The wallet never derived that script key, so it records the key with the
// transfer, credits the output and signs its spend.
func TestTransferSpendsTweakedOutput(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("tweaked")

	base, err := h.client.DeriveScriptKey(ctx)
	require.NoError(t, err)
	rawKey, err := btcec.ParsePubKey(base.KeyDesc.RawKeyBytes[:])
	require.NoError(t, err)
	unique, err := asset.DeriveUniqueScriptKey(
		*rawKey, minted.assetID,
		asset.ScriptKeyDerivationUniquePedersen,
	)
	require.NoError(t, err)
	recipient := tapsdk.ScriptKey{
		PubKey:   sdkPubKey(t, evenKey(unique.PubKey)),
		KeyDesc:  base.KeyDesc,
		TapTweak: unique.Tweak,
	}
	anchor, err := h.client.DeriveInternalKey(ctx)
	require.NoError(t, err)
	anchorKey, err := btcec.ParsePubKey(anchor.PubKey[:])
	require.NoError(t, err)

	first := h.commitTransferTo(
		minted, 1_000, 1_000, 7, customanchor.AssetScriptPlan{
			Mode: customanchor.AssetScriptExternal,
			External: &customanchor.AssetExternalScriptPlan{
				ScriptKey: recipient,
			},
		}, anchorKey,
	)
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, first.transfer, first.finalPSBT,
	)
	require.NoError(t, err)
	h.chain.mine(t)
	h.waitTransfer(first.txid, TransferConfirmed)

	// The wallet lists the output under the type of its key.
	keyType := tapsdk.ScriptKeyTypeUniquePedersen
	uniqueKeys := &tapsdk.ListUtxosRequest{
		ScriptKeyType: &tapsdk.ScriptKeyTypeQuery{
			ExplicitType: &keyType,
		},
	}
	held, err := h.client.ListUtxos(ctx, uniqueKeys)
	require.NoError(t, err)
	require.Len(t, held, 1)

	received, err := h.client.ExportProof(
		ctx, minted.ref, recipient.PubKey, nil,
	)
	require.NoError(t, err)
	second := h.commitTransfer(&mintedAsset{
		ref:       minted.ref,
		assetID:   minted.assetID,
		proofFile: received.RawProofFile,
		outpoint:  wire.OutPoint{Hash: first.txid, Index: 0},
	}, 1_000, 1_000, 8)
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, second.transfer, second.finalPSBT,
	)
	require.NoError(t, err)
	h.chain.mine(t)
	h.waitTransfer(second.txid, TransferConfirmed)
	held, err = h.client.ListUtxos(ctx, uniqueKeys)
	require.NoError(t, err)
	require.Empty(t, held)
}

// TestTransferReorg takes a confirmed transfer through a re-org: its change
// leaves the wallet, and returns with proofs of the new block.
func TestTransferReorg(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("reorg")
	sent := h.commitTransfer(minted, 1_000, 600, 7)
	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(t, err)

	first, _ := h.chain.mine(t)
	h.waitTransfer(sent.txid, TransferConfirmed)
	require.EqualValues(t, 400, h.ownedUnits())

	h.chain.reorg(1)
	h.waitTransfer(sent.txid, TransferPending)
	require.Zero(t, h.ownedUnits())

	second, _ := h.chain.mine(t)
	require.NotEqual(t, first.BlockHash(), second.BlockHash())
	h.waitTransfer(sent.txid, TransferConfirmed)
	require.EqualValues(t, 400, h.ownedUnits())

	changeProof, err := h.client.ExportProof(
		ctx, minted.ref, sent.change.PubKey, nil,
	)
	require.NoError(t, err)
	verified, err := h.client.VerifyProof(ctx, changeProof.RawProofFile)
	require.NoError(t, err)
	require.True(t, verified.Valid)
}

// TestTransferResume records a transfer whose broadcast fails, and publishes
// and confirms it after a restart.
func TestTransferResume(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("resume")
	sent := h.commitTransfer(minted, 1_000, 600, 7)

	h.chain.mu.Lock()
	h.chain.publishErr = context.DeadlineExceeded
	h.chain.mu.Unlock()

	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	var attempt *customanchor.PublishAttemptError
	require.ErrorAs(t, err, &attempt)
	require.True(t, attempt.OutcomeUnknown)
	require.Equal(t, codes.Unavailable, status.Code(err))
	h.waitTransfer(sent.txid, TransferPending)

	h.chain.mu.Lock()
	h.chain.publishErr = nil
	h.chain.mu.Unlock()

	h.restart()
	require.Eventually(t, func() bool {
		h.chain.mu.Lock()
		defer h.chain.mu.Unlock()

		return len(h.chain.mempool) == 1
	}, 10*time.Second, 10*time.Millisecond)

	h.chain.mine(t)
	h.waitTransfer(sent.txid, TransferConfirmed)
	require.EqualValues(t, 400, h.ownedUnits())
}

// TestTransferResumeStaleConfirmation resumes a transfer whose confirming
// block left the chain while nothing followed it. No re-org event reports
// that block, so the tracker finds it gone, returns the transfer to pending
// and broadcasts it again.
func TestTransferResumeStaleConfirmation(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("stale")
	sent := h.commitTransfer(minted, 1_000, 600, 7)
	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(t, err)

	first, height := h.chain.mine(t)
	h.waitTransfer(sent.txid, TransferConfirmed)
	require.EqualValues(t, 400, h.ownedUnits())

	// While the wallet is down, another block replaces the confirming
	// one and the network forgets the transaction.
	require.NoError(t, h.client.Close())
	h.chain.reorg(1)
	h.chain.mu.Lock()
	h.chain.mempool = nil
	h.chain.mu.Unlock()
	replaced, replacedHeight := h.chain.mine(t)
	require.Equal(t, height, replacedHeight)
	require.NotEqual(t, first.BlockHash(), replaced.BlockHash())

	h.restart()
	h.waitTransfer(sent.txid, TransferPending)
	require.Zero(t, h.ownedUnits())
	require.Eventually(t, func() bool {
		h.chain.mu.Lock()
		defer h.chain.mu.Unlock()

		return len(h.chain.mempool) == 1
	}, 10*time.Second, 10*time.Millisecond)

	second, _ := h.chain.mine(t)
	h.waitTransfer(sent.txid, TransferConfirmed)
	require.EqualValues(t, 400, h.ownedUnits())

	transfers, err := h.store.ListTransfers(ctx, &sent.txid)
	require.NoError(t, err)
	require.Equal(t, second.BlockHash(), transfers[0].BlockHash)

	// A restart on the same chain keeps the confirmation. The new
	// tracker registers in the background, so Done is offered until it
	// is taken.
	h.restart()
	require.Eventually(t, func() bool {
		h.chain.finalize(sent.txid)
		transfers, err := h.store.ListTransfers(ctx, &sent.txid)

		return err == nil && transfers[0].State == TransferFinal
	}, 10*time.Second, 10*time.Millisecond)
	transfers, err = h.store.ListTransfers(ctx, &sent.txid)
	require.NoError(t, err)
	require.Equal(t, second.BlockHash(), transfers[0].BlockHash)
	require.EqualValues(t, 400, h.ownedUnits())
}

// TestTransferResumeFinal resumes a transfer that confirmed while nothing
// followed it, on a chain that holds its confirmation and Done at
// registration. The transfer ends final with its proofs written, whichever
// event the tracker receives first.
func TestTransferResumeFinal(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("final")
	sent := h.commitTransfer(minted, 1_000, 600, 7)
	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(t, err)

	require.NoError(t, h.client.Close())
	block, height := h.chain.mine(t)

	h.chain.mu.Lock()
	h.chain.finalAtRegistration = true
	h.chain.mu.Unlock()

	// The tracker receives Done first about half of the time, so the
	// pending transfer is resumed often enough to see both orders.
	pending := h.store
	for range 32 {
		h.store = pending.clone()
		h.restart()
		h.waitTransfer(sent.txid, TransferFinal)

		transfers, err := h.store.ListTransfers(ctx, &sent.txid)
		require.NoError(t, err)
		require.Equal(t, block.BlockHash(), transfers[0].BlockHash)
		require.Equal(t, height, transfers[0].BlockHeight)
		require.EqualValues(t, 400, h.ownedUnits())

		for _, scriptKey := range []tapsdk.PubKey{
			sent.recipient.PubKey, sent.change.PubKey,
		} {
			exported, err := h.client.ExportProof(
				ctx, minted.ref, scriptKey, nil,
			)
			require.NoError(t, err)

			verified, err := h.client.VerifyProof(
				ctx, exported.RawProofFile,
			)
			require.NoError(t, err)
			require.True(t, verified.Valid)
		}
	}
}

// TestTransferRejectsSpentInput refuses a second transfer of an asset a
// recorded transfer spends, before anything is recorded.
func TestTransferRejectsSpentInput(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("double")
	first := h.commitTransfer(minted, 1_000, 600, 7)
	second := h.commitTransfer(minted, 1_000, 500, 8)

	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, first.transfer, first.finalPSBT,
	)
	require.NoError(t, err)

	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, second.transfer, second.finalPSBT,
	)
	var attempt *customanchor.PublishAttemptError
	require.ErrorAs(t, err, &attempt)
	require.False(t, attempt.OutcomeUnknown)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	transfers, err := h.client.ListTransfers(
		ctx, &tapsdk.ListTransfersRequest{},
	)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
}

// TestTransferInputProofFiles publishes a transfer that spends an asset
// output the wallet does not hold, with the output's proof file supplied at
// publish. The wallet keeps the file without owning the output, and extends
// it once the transfer confirms.
func TestTransferInputProofFiles(t *testing.T) {
	tests := []struct {
		name string

		// own makes the wallet that locked the asset spend it. That
		// wallet already keeps the locked output's proof.
		own bool

		wantUnits uint64
	}{{
		name:      "output of another wallet",
		wantUnits: 600,
	}, {
		name:      "output of an own transfer",
		own:       true,
		wantUnits: receivedUnits,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			holder := newTestHarness(t)
			ctx := t.Context()

			minted := holder.receive("locked")
			locked := holder.lockAsset(
				minted, receivedUnits, 600, 7,
			)

			h := holder
			if !tc.own {
				h = holder.peer(2)
				require.Empty(t, h.inputRecords(locked))
			}
			before := h.ownedUnits()

			claim := h.commitClaim(locked)
			_, err := customanchor.PublishTransfer(
				ctx, h.wallet, claim.transfer, claim.finalPSBT,
				customanchor.WithInputProofFiles(
					locked.proofFile,
				),
			)
			require.NoError(t, err)

			records := h.inputRecords(locked)
			require.Len(t, records, 1)
			require.False(t, records[0].Owned)
			require.Equal(t, locked.proofFile, records[0].ProofFile)
			require.Equal(t, before, h.ownedUnits())

			h.chain.mine(t)
			h.waitTransfer(claim.txid, TransferConfirmed)
			require.Equal(t, tc.wantUnits, h.ownedUnits())

			// The claimed output's file is the supplied one plus
			// the claim.
			exported, err := h.client.ExportProof(
				ctx, locked.ref, claim.change.PubKey, nil,
			)
			require.NoError(t, err)
			verified, err := h.client.VerifyProof(
				ctx, exported.RawProofFile,
			)
			require.NoError(t, err)
			require.True(t, verified.Valid)
			require.Equal(
				t, locked.amount, verified.DecodedProof.Amount,
			)

			inputFile, err := proof.DecodeFile(locked.proofFile)
			require.NoError(t, err)
			claimedFile, err := proof.DecodeFile(
				exported.RawProofFile,
			)
			require.NoError(t, err)
			require.Equal(
				t, inputFile.NumProofs()+1,
				claimedFile.NumProofs(),
			)
		})
	}
}

// TestTransferRejectsInputProofFiles refuses a transfer whose supplied proof
// files do not prove its inputs, before anything is recorded.
func TestTransferRejectsInputProofFiles(t *testing.T) {
	holder := newTestHarness(t)
	ctx := t.Context()

	minted := holder.receive("locked")
	locked := holder.lockAsset(minted, receivedUnits, 600, 7)
	other := holder.receive("other")

	// A file that holds only the locked output's last proof, as a proof
	// of ownership, shows who controls the output but not that the asset
	// was ever issued.
	lockedFile, err := proof.DecodeFile(locked.proofFile)
	require.NoError(t, err)
	tip, err := lockedFile.LastProof()
	require.NoError(t, err)
	tip.ChallengeWitness = locked.witness
	tipFile, err := proof.NewFile(proof.V0, *tip)
	require.NoError(t, err)
	var tipOnly bytes.Buffer
	require.NoError(t, tipFile.Encode(&tipOnly))

	h := holder.peer(2)
	claim := h.commitClaim(locked)

	tests := []struct {
		name    string
		files   [][]byte
		code    codes.Code
		wantErr string
	}{{
		name:    "no file",
		code:    codes.FailedPrecondition,
		wantErr: "no proof stored for asset input",
	}, {
		name:    "undecodable file",
		files:   [][]byte{{0x01}},
		code:    codes.InvalidArgument,
		wantErr: "error decoding input proof file 0",
	}, {
		name:    "duplicate file",
		files:   [][]byte{locked.proofFile, locked.proofFile},
		code:    codes.InvalidArgument,
		wantErr: "duplicate input proof file",
	}, {
		name:    "file of another asset",
		files:   [][]byte{locked.proofFile, other.proofFile},
		code:    codes.InvalidArgument,
		wantErr: "does not match a transfer input",
	}, {
		name:    "file without its history",
		files:   [][]byte{tipOnly.Bytes()},
		code:    codes.InvalidArgument,
		wantErr: "error verifying input proofs",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := customanchor.PublishTransfer(
				ctx, h.wallet, claim.transfer, claim.finalPSBT,
				customanchor.WithInputProofFiles(tc.files...),
			)
			var attempt *customanchor.PublishAttemptError
			require.ErrorAs(t, err, &attempt)
			require.False(t, attempt.OutcomeUnknown)
			require.Equal(t, tc.code, status.Code(err))
			require.ErrorContains(t, err, tc.wantErr)

			transfers, err := h.client.ListTransfers(
				ctx, &tapsdk.ListTransfersRequest{},
			)
			require.NoError(t, err)
			require.Empty(t, transfers)
			require.Empty(t, h.inputRecords(locked))
		})
	}

	// The refused transfer publishes with the locked output's file.
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, claim.transfer, claim.finalPSBT,
		customanchor.WithInputProofFiles(locked.proofFile),
	)
	require.NoError(t, err)
}

// TestBalances follows the balance of an asset through a transfer, and tells
// an asset the wallet has spent from one it never held.
func TestBalances(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	minted := h.receive("balance")
	balance, err := h.wallet.GetBalance(ctx, minted.ref)
	require.NoError(t, err)
	require.EqualValues(t, receivedUnits, balance)

	sent := h.commitTransfer(minted, receivedUnits, 600, 7)
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(t, err)

	// The change is unconfirmed, so the wallet holds nothing of an asset
	// it still knows.
	filtered, err := h.wallet.ListBalances(ctx, &tapsdk.ListBalancesRequest{
		AssetRef: &minted.ref,
	})
	require.NoError(t, err)
	require.Len(t, filtered.Balances, 1)
	require.Zero(t, filtered.Balances[minted.ref.String()].Balance)
	require.EqualValues(t, 1, filtered.UnconfirmedTransfers)

	h.chain.mine(t)
	h.waitTransfer(sent.txid, TransferConfirmed)

	all, err := h.wallet.ListBalances(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all.Balances, 1)
	require.EqualValues(t, 400, all.Balances[minted.ref.String()].Balance)
	require.Zero(t, all.UnconfirmedTransfers)

	// A ref the wallet never held is unknown, by asset ID and by group
	// key.
	groupKey := newTestKeyRing(4).privKey(1, 0).PubKey()
	for _, neverHeld := range []tapsdk.AssetRef{
		tapsdk.AssetRefFromAssetID(tapsdk.AssetID{1}),
		tapsdk.AssetRefFromGroupKey(sdkPubKey(t, groupKey)),
	} {
		_, err = h.wallet.GetBalance(ctx, neverHeld)
		require.ErrorIs(t, err, tapsdk.ErrAssetUnknown)
	}

	// A query has to name its grouping.
	server := &walletServer{engine: h.client.engine}
	_, err = server.ListBalances(ctx, &taprpc.ListBalancesRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestBalanceSums sums asset outputs by asset ID and by group key.
func TestBalanceSums(t *testing.T) {
	groupKey := newTestKeyRing(4).privKey(1, 0).PubKey()
	otherKey := newTestKeyRing(4).privKey(1, 1).PubKey()

	newHolding := func(tag string, amount uint64,
		group *btcec.PublicKey) *holding {

		held := &asset.Asset{
			Genesis: asset.Genesis{Tag: tag},
			Amount:  amount,
		}
		if group != nil {
			held.GroupKey = &asset.GroupKey{GroupPubKey: *group}
		}

		return &holding{chainAsset: asset.ChainAsset{Asset: held}}
	}
	held := []*holding{
		newHolding("plain", 5, nil),
		newHolding("plain", 7, nil),
		newHolding("tranche-1", 11, groupKey),
		newHolding("tranche-2", 13, groupKey),
	}
	assetKey := func(h *holding) string {
		id := h.chainAsset.ID()

		return hex.EncodeToString(id[:])
	}
	groupHex := hex.EncodeToString(groupKey.SerializeCompressed())

	byAsset, err := balancesByAsset(held, nil)
	require.NoError(t, err)
	require.Len(t, byAsset, 3)
	plain := byAsset[assetKey(held[0])]
	require.EqualValues(t, 12, plain.Balance)
	require.Equal(t, "plain", plain.AssetGenesis.Name)
	require.Empty(t, plain.GroupKey)
	tranche := byAsset[assetKey(held[2])]
	require.EqualValues(t, 11, tranche.Balance)
	require.Equal(t, groupKey.SerializeCompressed(), tranche.GroupKey)

	trancheID := held[3].chainAsset.ID()
	byAsset, err = balancesByAsset(held, trancheID[:])
	require.NoError(t, err)
	require.Len(t, byAsset, 1)
	require.EqualValues(t, 13, byAsset[assetKey(held[3])].Balance)

	_, err = balancesByAsset(held, []byte{1, 2, 3})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Assets without a group are in no group balance.
	byGroup, err := balancesByGroup(held, nil)
	require.NoError(t, err)
	require.Len(t, byGroup, 1)
	require.EqualValues(t, 24, byGroup[groupHex].Balance)

	byGroup, err = balancesByGroup(held, groupKey.SerializeCompressed())
	require.NoError(t, err)
	require.EqualValues(t, 24, byGroup[groupHex].Balance)

	byGroup, err = balancesByGroup(held, otherKey.SerializeCompressed())
	require.NoError(t, err)
	require.Empty(t, byGroup)

	_, err = balancesByGroup(held, []byte{1, 2, 3})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestUnimplementedCalls reports calls outside the engine's scope with the
// status a tapd without the service would return.
func TestUnimplementedCalls(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	_, err := h.client.ListBatches(ctx, &tapsdk.ListBatchesRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = h.client.SendAsset(ctx, &tapsdk.SendAssetRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, _, err = h.client.SubscribeSendEvents(
		ctx, &tapsdk.SubscribeSendEventsRequest{},
	)
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

// TestGetInfo reports the engine's network and chain tip.
func TestGetInfo(t *testing.T) {
	h := newTestHarness(t)

	info, err := h.client.GetInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "regtest", info.Network)
	require.EqualValues(t, 11, info.BlockHeight)
	require.True(t, info.SyncedToChain)
}

// TestGroupKeyTrust verifies grouped assets only for configured or added
// group keys.
func TestGroupKeyTrust(t *testing.T) {
	configured := newTestKeyRing(3).privKey(1, 0).PubKey()
	added := newTestKeyRing(3).privKey(1, 1).PubKey()

	keys := newTestKeyRing(1)
	client, err := NewClient(&Config{
		Network:   tapsdk.NetworkRegtest,
		Store:     NewMemoryStore(),
		KeyRing:   NewLocalKeyRing(keys),
		Signer:    NewKeyRingSigner(keys),
		Chain:     newTestChain(t),
		GroupKeys: []tapsdk.PubKey{sdkPubKey(t, configured)},
	})
	require.NoError(t, err)

	ctx := t.Context()
	require.NoError(t, client.engine.verifyGroupKey(ctx, configured))
	require.ErrorContains(
		t, client.engine.verifyGroupKey(ctx, added), "is not trusted",
	)

	client.TrustGroupKey(sdkPubKey(t, added))
	require.NoError(t, client.engine.verifyGroupKey(ctx, added))
}

// TestGroupKeyTrustHeldAsset verifies grouped assets for the group of an
// asset the wallet holds, which no configuration names.
func TestGroupKeyTrustHeldAsset(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	held := newTestKeyRing(3).privKey(1, 0).PubKey()
	other := newTestKeyRing(3).privKey(1, 1).PubKey()

	// The wallet holds an asset of the group.
	h.receive("held")
	records, err := h.store.ListOwnedProofs(ctx, true)
	require.NoError(t, err)
	require.Len(t, records, 1)

	file, err := proof.DecodeFile(records[0].ProofFile)
	require.NoError(t, err)
	tip, err := file.LastProof()
	require.NoError(t, err)
	tip.Asset.GroupKey = &asset.GroupKey{GroupPubKey: *held}
	require.NoError(t, file.ReplaceLastProof(*tip))

	var encoded bytes.Buffer
	require.NoError(t, file.Encode(&encoded))
	records[0].ProofFile = encoded.Bytes()
	require.NoError(t, h.store.InsertProof(ctx, records[0]))

	// A new client over the store trusts the group before any other
	// call.
	client, err := NewClient(&Config{
		Network: tapsdk.NetworkRegtest,
		Store:   h.store,
		KeyRing: NewLocalKeyRing(h.keys),
		Signer:  NewKeyRingSigner(h.keys),
		Chain:   h.chain,
	})
	require.NoError(t, err)

	require.NoError(t, client.engine.verifyGroupKey(ctx, held))
	require.ErrorContains(
		t, client.engine.verifyGroupKey(ctx, other), "is not trusted",
	)
}

// TestGroupKeyTrustAddress verifies grouped assets for the group of an
// address the wallet created, before and after a restart.
func TestGroupKeyTrustAddress(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	ctx := t.Context()

	group := newTestKeyRing(3).privKey(1, 0).PubKey()
	other := newTestKeyRing(3).privKey(1, 1).PubKey()

	// The address is the only source of trust in the group.
	require.ErrorContains(
		t, h.client.engine.verifyGroupKey(ctx, group), "is not trusted",
	)
	_, err := h.wallet.NewReceiveAddress(
		ctx, tapsdk.AssetRefFromGroupKey(sdkPubKey(t, group)),
	)
	require.NoError(t, err)
	_, err = h.wallet.NewReceiveAddress(
		ctx, tapsdk.AssetRefFromAssetID(tapsdk.AssetID{1}),
	)
	require.NoError(t, err)

	requireTrust := func() {
		t.Helper()

		// The address may name the key with the other parity than
		// the proofs of the group do.
		engine := h.client.engine
		flipped := tapscript.FlipParity(group)
		require.NoError(t, engine.verifyGroupKey(ctx, group))
		require.NoError(t, engine.verifyGroupKey(ctx, flipped))
		require.ErrorContains(
			t, engine.verifyGroupKey(ctx, other), "is not trusted",
		)
	}
	requireTrust()

	h.restart()
	requireTrust()

	// A wallet without addresses reads no address store.
	h.addresses = nil
	h.restart()
	require.ErrorContains(
		t, h.client.engine.verifyGroupKey(ctx, group), "is not trusted",
	)
}
