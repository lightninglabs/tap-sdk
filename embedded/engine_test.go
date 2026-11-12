package embedded

import (
	"bytes"
	"context"
	"encoding/hex"
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
	"github.com/lightninglabs/taproot-assets/taprpc"
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
	ctx := h.t.Context()

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

	funding := newFundingInput(h.t, seed, 100_000)

	tx := wire.NewMsgTx(2)
	tx.AddTxIn(wire.NewTxIn(&source.outpoint, nil, nil))
	tx.AddTxIn(wire.NewTxIn(&funding.outpoint, nil, nil))
	placeholder := []byte{txscript.OP_1, txscript.OP_DATA_32}
	placeholder = append(placeholder, make([]byte, 32)...)
	tx.AddTxOut(&wire.TxOut{Value: 1_000, PkScript: placeholder})

	request := &customanchor.Request{
		Inputs: []customanchor.AssetInput{{
			ID:        "input-0",
			AssetRef:  source.ref,
			Amount:    total,
			ProofFile: source.proofFile,
			Witness: customanchor.AssetWitnessPlan{
				Mode: customanchor.AssetWitnessBackendSigner,
			},
		}},
		Outputs: []customanchor.AssetOutput{{
			ID:                "recipient",
			AssetRef:          source.ref,
			Amount:            amount,
			AnchorOutputIndex: 0,
			AnchorValueSat:    1_000,
			Script: customanchor.AssetScriptPlan{
				Mode: customanchor.AssetScriptExternal,
				External: &customanchor.AssetExternalScriptPlan{
					ScriptKey: recipient,
				},
			},
			Anchor: customanchor.OutputPlan{
				InternalKey: tapsdk.InternalKey{
					PubKey: sdkPubKey(
						h.t, recipientAnchor.PubKey,
					),
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
		SigningPlans: []customanchor.InputSigningPlan{{
			InputIndex:    0,
			BackendSigned: &customanchor.BackendSignedPlan{},
		}, {
			InputIndex:   1,
			CallerSigned: &customanchor.CallerSignedPlan{},
		}},
	}

	result := &testTransfer{recipient: recipient}
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
				AssetRef:          source.ref,
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
	packet.Inputs[1].WitnessUtxo = funding.txOut
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

	client, err := NewClient(&Config{
		Network:   tapsdk.NetworkRegtest,
		Store:     NewMemoryStore(),
		KeyRing:   newTestKeyRing(1),
		Chain:     newTestChain(t),
		GroupKeys: []tapsdk.PubKey{sdkPubKey(t, configured)},
	})
	require.NoError(t, err)

	require.NoError(t, client.engine.verifyGroupKey(configured))
	require.ErrorContains(
		t, client.engine.verifyGroupKey(added), "is not trusted",
	)

	client.TrustGroupKey(sdkPubKey(t, added))
	require.NoError(t, client.engine.verifyGroupKey(added))
}
