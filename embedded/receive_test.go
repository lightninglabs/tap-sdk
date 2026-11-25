package embedded

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/stretchr/testify/require"
)

// newReceivingAddress returns a sender's asset and an address of h for it.
func (h *testHarness) newReceivingAddress(sender *testHarness, tag string,
	groupPriv *btcec.PrivateKey) (*mintedAsset, *tapsdk.Address) {

	h.t.Helper()

	minted := sender.receiveOf(tag, groupPriv)
	addr, err := h.wallet.NewReceiveAddress(h.t.Context(), minted.ref)
	require.NoError(h.t, err)

	return minted, addr
}

// requireNothingOwned checks that the wallet holds no asset output.
func (h *testHarness) requireNothingOwned() {
	h.t.Helper()

	owned, err := h.store.ListOwnedProofs(h.t.Context(), true)
	require.NoError(h.t, err)
	require.Empty(h.t, owned)
}

// TestReceiveAtAddress receives a transfer to an address in one pass over
// the mailbox, for an asset ID and for a group key, and spends the received
// output.
func TestReceiveAtAddress(t *testing.T) {
	tests := []struct {
		name      string
		groupPriv *btcec.PrivateKey
	}{{
		name: "asset ID",
	}, {
		name:      "group key",
		groupPriv: newTestKeyRing(4).privKey(1, 0),
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newAddressHarness(t, testCourier)
			sender := h.peer(2)
			ctx := t.Context()

			minted, addr := h.newReceivingAddress(
				sender, "asset", tc.groupPriv,
			)
			send := sender.payAddress(addr.Encoded, minted, 600)
			h.courier.deliver(t, send)

			require.NoError(t, h.client.SyncReceives(ctx))

			tip, err := send.file.LastProof()
			require.NoError(t, err)
			record := h.requireReceiveState(send, ReceiveConfirmed)
			blockHash := tip.BlockHeader.BlockHash()
			require.Equal(t, blockHash, record.BlockHash)
			require.Equal(t, tip.BlockHeight, record.BlockHeight)

			// The wallet holds the output with the file the sender
			// holds, under a key it can sign for.
			require.EqualValues(t, 600, h.ownedUnits())
			balance, err := h.wallet.GetBalance(ctx, minted.ref)
			require.NoError(t, err)
			require.EqualValues(t, 600, balance)

			owned, err := h.store.ListOwnedProofs(ctx, false)
			require.NoError(t, err)
			require.Len(t, owned, 1)
			require.Equal(t, send.proofFile, owned[0].ProofFile)
			require.Equal(t, send.outpoint, owned[0].Outpoint)

			receiver := asset.ToSerialized(&send.addr.ScriptKey)
			scriptKey, err := h.store.FetchScriptKey(
				ctx, owned[0].ScriptKey,
			)
			require.NoError(t, err)
			require.EqualValues(
				t, asset.ScriptKeyUniquePedersen,
				scriptKey.Type,
			)
			require.Equal(t, send.scriptKey.Tweak, scriptKey.Tweak)
			require.Equal(t, KeyRecord{
				PubKey: receiver,
				Family: uint32(asset.TaprootAssetsKeyFamily),
				Index:  0,
			}, scriptKey.RawKey)

			// One pass reads from below the height of the address
			// and leaves no connection behind.
			addrs, err := h.store.ListAddresses(ctx)
			require.NoError(t, err)
			require.Equal(
				t, addrs[0].CreationHeight-mailboxStartMargin,
				h.courier.startBlocks[receiver],
			)
			require.Equal(
				t, []string{testCourier}, h.courier.dialed,
			)
			_, _, open := h.courier.counts()
			require.Zero(t, open)

			// The engine signs for the derived script key and for
			// the anchor output under the address's internal key.
			spend := h.commitTransfer(&mintedAsset{
				ref:       minted.ref,
				assetID:   send.assetID,
				proofFile: owned[0].ProofFile,
				outpoint:  send.outpoint,
			}, 600, 250, 9)
			_, err = customanchor.PublishTransfer(
				ctx, h.wallet, spend.transfer, spend.finalPSBT,
			)
			require.NoError(t, err)
			h.chain.mine(t)
			h.waitTransfer(spend.txid, TransferConfirmed)
			require.EqualValues(t, 350, h.ownedUnits())
		})
	}
}

// TestReceiveReplayedMessage reads a mailbox that keeps its messages again
// and again without a second receive or a second asset output.
func TestReceiveReplayedMessage(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "replayed", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)

	require.NoError(t, h.client.SyncReceives(ctx))
	confirmed := h.requireReceiveState(send, ReceiveConfirmed)
	queries, _, _ := h.courier.counts()

	// The same message twice more: once stored, once posted again.
	h.courier.announce(t, send)
	for range 2 {
		require.NoError(t, h.client.SyncReceives(ctx))
	}

	records, err := h.store.ListReceives(ctx, ReceiveFilter{})
	require.NoError(t, err)
	require.Equal(t, []ReceiveRecord{*confirmed}, records)
	require.EqualValues(t, 600, h.ownedUnits())

	// A confirmed receive fetches no proof again.
	replayQueries, _, _ := h.courier.counts()
	require.Equal(t, queries, replayQueries)
}

// TestReceiveSkipsMalformedMessages receives the transfer of a valid message
// that sits in the mailbox among messages that announce no transfer to the
// address. The wallet records none of those.
func TestReceiveSkipsMalformedMessages(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "malformed", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	receiver := &send.addr.ScriptKey

	// Bytes that are no ciphertext.
	h.courier.post(receiver, []byte("not a send fragment"))
	h.courier.post(receiver, nil)

	// A fragment encrypted to another key.
	other := newTestKeyRing(8).privKey(1, 0).PubKey()
	foreign, err := proof.EncryptSendFragment(send.fragment, other)
	require.NoError(t, err)
	h.courier.post(receiver, foreign)

	// A ciphertext to the address that is no fragment.
	noOutputs := *send.fragment
	noOutputs.Outputs = nil
	invalid, err := proof.EncryptSendFragment(&noOutputs, receiver)
	require.NoError(t, err)
	h.courier.post(receiver, invalid)

	truncated, err := proof.EncryptSendFragment(send.fragment, receiver)
	require.NoError(t, err)
	h.courier.post(receiver, truncated[:len(truncated)-1])

	// Fragments of transfers the address cannot be paid with.
	otherID := asset.ID{7}
	sent := send.fragment.Outputs[send.assetID]
	impossible := []func(*proof.SendFragment){
		func(fragment *proof.SendFragment) {
			delete(fragment.Outputs, send.assetID)
			fragment.Outputs[otherID] = sent
		},
		func(fragment *proof.SendFragment) {
			fragment.Outputs[otherID] = sent
		},
		func(fragment *proof.SendFragment) {
			noUnits := sent
			noUnits.Amount = 0
			fragment.Outputs[send.assetID] = noUnits
		},
		func(fragment *proof.SendFragment) {
			underived := sent
			underived.DerivationMethod = 9
			fragment.Outputs[send.assetID] = underived
		},
	}
	for _, change := range impossible {
		h.courier.announce(t, send.announcedAs(change))
	}

	h.courier.deliver(t, send)

	require.NoError(t, h.client.SyncReceives(ctx))

	records, err := h.store.ListReceives(ctx, ReceiveFilter{})
	require.NoError(t, err)
	require.Len(t, records, 1)
	h.requireReceiveState(send, ReceiveConfirmed)
	require.EqualValues(t, 600, h.ownedUnits())
}

// TestReceiveWaitsForProofs leaves a transfer detected while the courier
// does not hold its proofs, and receives it in a later pass.
func TestReceiveWaitsForProofs(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "late", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.announce(t, send)

	// A proof that is not there yet is no error.
	for range 2 {
		require.NoError(t, h.client.SyncReceives(ctx))
		record := h.requireReceiveState(send, ReceiveDetected)
		require.Zero(t, record.BlockHash)
		h.requireNothingOwned()
	}

	// The last proof alone is not the file.
	tip, err := send.file.RawProofAt(uint32(send.file.NumProofs() - 1))
	require.NoError(t, err)
	partial, err := proof.NewFile(proof.V0)
	require.NoError(t, err)
	require.NoError(t, partial.AppendProofRaw(tip))
	h.courier.publish(t, partial)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveDetected)
	h.requireNothingOwned()

	h.courier.publish(t, send.file)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)
	require.EqualValues(t, 600, h.ownedUnits())
}

// TestReceiveCourierFailure reports a courier that fails, as opposed to one
// that does not hold a proof yet.
func TestReceiveCourierFailure(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "failure", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)

	h.courier.queryErr = errors.New("universe is down")
	require.ErrorContains(
		t, h.client.SyncReceives(ctx), "universe is down",
	)
	h.requireReceiveState(send, ReceiveDetected)

	h.courier.queryErr = nil
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)
}

// TestReceiveWaitsForBlock asks the courier for no proof while the chain
// source does not hold the block a transfer names.
func TestReceiveWaitsForBlock(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "block", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)

	h.chain.reorg(1)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveDetected)
	queries, _, _ := h.courier.counts()
	require.Zero(t, queries)
	h.requireNothingOwned()
}

// TestReceiveFailures fails a transfer whose proofs verify without creating
// the asset output the address and the fragment describe. The wallet takes
// none of its proofs, also after a restart.
func TestReceiveFailures(t *testing.T) {
	foreignKey := newTestKeyRing(8).privKey(1, 0).PubKey()

	tests := []struct {
		name string

		// pay sends to the address of the asset.
		pay func(h, sender *testHarness, minted *mintedAsset,
			addr *tapsdk.Address) *addressSend

		errText string
	}{{
		// The wallet could not sign for the anchor output.
		name: "foreign internal key",
		pay: func(_, sender *testHarness, minted *mintedAsset,
			addr *tapsdk.Address) *addressSend {

			return sender.payAddress(
				addr.Encoded, minted, 600,
				anchorPlan{internalKey: foreignKey},
			)
		},
		errText: "is not under the internal key of the address",
	}, {
		// Whoever can satisfy the script can spend the anchor output.
		name: "anchor script",
		pay: func(_, sender *testHarness, minted *mintedAsset,
			addr *tapsdk.Address) *addressSend {

			leaf := tapsdk.TapLeaf{
				Script: []byte{txscript.OP_TRUE},
			}

			return sender.payAddress(
				addr.Encoded, minted, 600,
				anchorPlan{leaves: []tapsdk.TapLeaf{leaf}},
			)
		},
		errText: "commits to a script the address does not name",
	}, {
		name: "another amount",
		pay: func(_, sender *testHarness, minted *mintedAsset,
			addr *tapsdk.Address) *addressSend {

			send := sender.payAddress(addr.Encoded, minted, 600)
			output := send.fragment.Outputs[send.assetID]
			output.Amount = 601
			send.fragment.Outputs[send.assetID] = output

			return send
		},
		errText: "holds 600 units, not 601",
	}, {
		// The file of another output of the wallet, which the courier
		// serves at the leaf of the announced one.
		name: "proof of another output",
		pay: func(h, sender *testHarness, minted *mintedAsset,
			addr *tapsdk.Address) *addressSend {

			send := sender.payAddress(addr.Encoded, minted, 600)

			// The fragment names an output that holds no asset.
			send.fragment.OutPoint.Index = 1
			tip, err := send.file.RawProofAt(
				uint32(send.file.NumProofs() - 1),
			)
			require.NoError(h.t, err)
			h.courier.leaves[leafName(ProofKey{
				AssetID: send.assetID,
				ScriptKey: asset.ToSerialized(
					send.scriptKey.PubKey,
				),
				Outpoint: send.fragment.OutPoint,
			})] = tip
			send.outpoint = send.fragment.OutPoint

			return send
		},
		errText: "proof is anchored at",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newAddressHarness(t, testCourier)
			sender := h.peer(2)
			ctx := t.Context()

			minted, addr := h.newReceivingAddress(
				sender, "asset", nil,
			)
			send := tc.pay(h, sender, minted, addr)
			h.courier.deliver(t, send)

			require.NoError(t, h.client.SyncReceives(ctx))

			h.requireReceiveFailure(send, tc.errText)
			h.requireNothingOwned()

			// The failure is final: a later pass and a restart
			// fetch nothing for it.
			queries, _, _ := h.courier.counts()
			require.NoError(t, h.client.SyncReceives(ctx))
			h.restart()
			require.NoError(t, h.client.ResumeReceives(ctx))
			require.NoError(t, h.client.SyncReceives(ctx))

			h.requireReceiveState(send, ReceiveFailed)
			h.requireNothingOwned()
			laterQueries, _, _ := h.courier.counts()
			require.Equal(t, queries, laterQueries)
		})
	}
}

// TestReceiveInvalidProof fails a transfer whose proof file does not verify.
func TestReceiveInvalidProof(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "invalid", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)

	// The file claims more units than the transfer created.
	forged, err := proof.DecodeFile(send.proofFile)
	require.NoError(t, err)
	tip, err := forged.LastProof()
	require.NoError(t, err)
	tip.Asset.Amount = 601
	require.NoError(t, forged.ReplaceLastProof(*tip))
	h.courier.publish(t, forged)
	h.courier.announce(t, send)

	require.NoError(t, h.client.SyncReceives(ctx))

	h.requireReceiveFailure(send, "does not verify")
	h.requireNothingOwned()
}

// TestMatchReceiveSpendable refuses a verified output that the transfers of
// the wallet cannot spend.
func TestMatchReceiveSpendable(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "asset", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)

	paid, err := h.client.engine.receive.address(
		ctx, asset.ToSerialized(&send.addr.ScriptKey),
	)
	require.NoError(t, err)
	outputs, err := deriveReceiveOutputs(paid, send.fragment)
	require.NoError(t, err)

	tests := []struct {
		name    string
		change  func(*asset.Asset)
		errText string
	}{{
		name:   "as sent",
		change: func(*asset.Asset) {},
	}, {
		name: "asset version 0",
		change: func(received *asset.Asset) {
			received.Version = asset.V0
		},
		errText: "is of version 0",
	}, {
		name: "lock time",
		change: func(received *asset.Asset) {
			received.LockTime = 1
		},
		errText: "is time locked",
	}, {
		name: "relative lock time",
		change: func(received *asset.Asset) {
			received.RelativeLockTime = 1
		},
		errText: "is time locked",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tip, err := send.file.LastProof()
			require.NoError(t, err)
			tc.change(&tip.Asset)

			err = matchReceive(paid, send.outpoint, outputs[0], tip)
			if tc.errText == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.errText)
		})
	}
}

// TestReceiveForgedAnnouncement receives a transfer whose output someone
// announced before its sender did, with a fragment that fails or that never
// becomes ready.
func TestReceiveForgedAnnouncement(t *testing.T) {
	tests := []struct {
		name  string
		forge func(*proof.SendFragment)

		// state is the state of the forged announcement before the
		// sender announces the transfer, and errText why it fails in
		// the end.
		state   ReceiveState
		errText string
	}{{
		name: "another amount",
		forge: func(fragment *proof.SendFragment) {
			for assetID, sent := range fragment.Outputs {
				sent.Amount++
				fragment.Outputs[assetID] = sent
			}
		},
		state:   ReceiveFailed,
		errText: "holds 600 units, not 601",
	}, {
		name: "unknown block",
		forge: func(fragment *proof.SendFragment) {
			fragment.BlockHeader.Nonce++
		},
		state:   ReceiveDetected,
		errText: errAnnouncedAgain.Error(),
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newAddressHarness(t, testCourier)
			sender := h.peer(2)
			ctx := t.Context()

			minted, addr := h.newReceivingAddress(
				sender, "asset", nil,
			)
			send := sender.payAddress(addr.Encoded, minted, 600)
			forged := send.announcedAs(tc.forge)

			// The proofs are public before the sender announces
			// the transfer.
			h.courier.publish(t, send.file)
			h.courier.announce(t, forged)
			require.NoError(t, h.client.SyncReceives(ctx))
			h.requireReceiveState(forged, tc.state)
			require.Nil(t, h.receiveRecord(send))

			h.courier.announce(t, send)
			for range 2 {
				require.NoError(t, h.client.SyncReceives(ctx))
			}

			h.requireReceiveState(send, ReceiveConfirmed)
			h.requireReceiveFailure(forged, tc.errText)
			require.EqualValues(t, 600, h.ownedUnits())
		})
	}
}

// TestReceiveAnnouncedTwice confirms one receive for an output that two
// fragments describe, whether the second one arrives later or both are
// worked on at once.
func TestReceiveAnnouncedTwice(t *testing.T) {
	// The engine reads nothing of the root a fragment names.
	again := func(fragment *proof.SendFragment) {
		fragment.TaprootAssetRoot[0] ^= 1
	}
	confirmed := ReceiveConfirmed

	t.Run("in turn", func(t *testing.T) {
		h := newAddressHarness(t, testCourier)
		sender := h.peer(2)
		ctx := t.Context()

		minted, addr := h.newReceivingAddress(sender, "asset", nil)
		send := sender.payAddress(addr.Encoded, minted, 600)
		h.courier.deliver(t, send)
		require.NoError(t, h.client.SyncReceives(ctx))
		h.requireReceiveState(send, ReceiveConfirmed)
		queries, _, _ := h.courier.counts()

		second := send.announcedAs(again)
		h.courier.announce(t, second)
		require.NoError(t, h.client.SyncReceives(ctx))

		// The second one fails before a proof is fetched for it.
		h.requireReceiveState(send, ReceiveConfirmed)
		h.requireReceiveFailure(second, errAnnouncedAgain.Error())
		laterQueries, _, _ := h.courier.counts()
		require.Equal(t, queries, laterQueries)
		require.EqualValues(t, 600, h.ownedUnits())
	})

	t.Run("at once", func(t *testing.T) {
		h := newAddressHarness(t, testCourier)
		sender := h.peer(2)
		ctx := t.Context()

		minted, addr := h.newReceivingAddress(sender, "asset", nil)
		send := sender.payAddress(addr.Encoded, minted, 600)
		second := send.announcedAs(again)
		h.courier.deliver(t, send)
		h.courier.announce(t, second)

		// Both fetch their proofs before either is confirmed.
		hold := make(chan struct{})
		h.courier.hold = hold
		done := make(chan error, 1)
		go func() { done <- h.client.SyncReceives(ctx) }()

		require.Eventually(t, func() bool {
			h.courier.mu.Lock()
			defer h.courier.mu.Unlock()

			return h.courier.querying == 2
		}, 10*time.Second, 5*time.Millisecond)
		close(hold)
		require.NoError(t, <-done)

		records, err := h.store.ListReceives(ctx, ReceiveFilter{
			State: &confirmed,
		})
		require.NoError(t, err)
		require.Len(t, records, 1)

		winner, loser := send, second
		if h.receiveRecord(send).State != ReceiveConfirmed {
			winner, loser = second, send
		}
		h.requireReceiveState(winner, ReceiveConfirmed)
		h.requireReceiveFailure(loser, errAnnouncedAgain.Error())
		require.EqualValues(t, 600, h.ownedUnits())
	})
}

// TestAnnouncedAgain tells a fragment that names an asset the wallet does not
// hold at the output from one that names none: an announcement of a part of a
// transfer must not keep the rest of it from the wallet.
func TestAnnouncedAgain(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	ctx := t.Context()

	var (
		assetA = asset.ID{1}
		assetB = asset.ID{2}
		base   = ReceiveRecord{Address: [33]byte{2, 1}}
	)
	fragmentOf := func(assetIDs ...asset.ID) *proof.SendFragment {
		fragment := &proof.SendFragment{
			Version: proof.SendFragmentV1,
			Outputs: make(map[asset.ID]proof.SendOutput),
		}
		for _, assetID := range assetIDs {
			fragment.Outputs[assetID] = proof.SendOutput{Amount: 1}
		}

		return fragment
	}
	insert := func(state ReceiveState,
		fragment *proof.SendFragment) receiveID {

		record := base
		record.State = state

		var err error
		record.Announcement, err = announcementOf(fragment)
		require.NoError(t, err)
		record.Payload, err = (&receivePayload{
			fragment: fragment,
		}).encode()
		require.NoError(t, err)

		_, err = h.store.InsertReceive(ctx, record)
		require.NoError(t, err)

		return idOf(&record)
	}

	// The wallet holds asset A through one announcement, and no asset
	// through one that is not confirmed.
	insert(ReceiveConfirmed, fragmentOf(assetA))
	insert(ReceiveDetected, fragmentOf(assetB))

	tests := []struct {
		name     string
		assetIDs []asset.ID
		want     bool
	}{{
		name:     "held asset",
		assetIDs: []asset.ID{assetA},
		want:     true,
	}, {
		name:     "held asset and another",
		assetIDs: []asset.ID{assetA, assetB},
	}, {
		name:     "another asset",
		assetIDs: []asset.ID{assetB},
	}}
	for idx, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A fragment of its own, so that the record is not
			// one of those above.
			fragment := fragmentOf(tc.assetIDs...)
			fragment.BlockHeight = uint32(idx + 1)
			id := insert(ReceiveDetected, fragment)

			record, held, err := h.client.engine.receive.load(
				ctx, id,
			)
			require.NoError(t, err)
			require.Equal(t, id, idOf(record))
			require.Equal(
				t, tc.want, announcedAgain(held, fragment),
			)
		})
	}
}

// TestReceiveChainFailure leaves a transfer detected when the chain source
// fails while its proofs are verified, which is no verdict on the proofs.
func TestReceiveChainFailure(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "chain", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)

	// The chain source answers for the block of the transfer only.
	tip, err := send.file.LastProof()
	require.NoError(t, err)
	h.chain.failBelow(tip.BlockHeight)
	require.ErrorContains(
		t, h.client.SyncReceives(ctx), "chain source is down",
	)
	h.requireReceiveState(send, ReceiveDetected)
	h.requireNothingOwned()

	h.chain.failBelow(0)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)
}

// failingKeys fails the key operations of the receive path.
type failingKeys struct {
	ReceiveKeys

	err error
}

// ECDH fails while err is set.
func (k *failingKeys) ECDH(ctx context.Context, locator keychain.KeyLocator,
	pubKey *btcec.PublicKey) ([32]byte, error) {

	if k.err != nil {
		return [32]byte{}, k.err
	}

	return k.ReceiveKeys.ECDH(ctx, locator, pubKey)
}

// TestReceiveKeyFailure reads a message again that the host's keys could not
// decrypt, instead of skipping it as a malformed one.
func TestReceiveKeyFailure(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	keys := &failingKeys{
		ReceiveKeys: h.addresses.Keys,
		err:         errors.New("signer is locked"),
	}
	h.addresses.Keys = keys
	h.restart()

	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "keys", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)

	require.ErrorContains(
		t, h.client.SyncReceives(ctx), "signer is locked",
	)
	require.Nil(t, h.receiveRecord(send))

	keys.err = nil
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)
}

// TestReceiveLive holds the mailboxes open: a transfer is received when its
// sender announces it, at an address created before or after the client
// went live, and once its proofs reach the courier.
func TestReceiveLive(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(5)
	ctx := t.Context()

	early, earlyAddr := h.newReceivingAddress(sender, "early", nil)
	require.NoError(t, h.client.ResumeReceives(ctx))
	late, lateAddr := h.newReceivingAddress(sender, "late", nil)

	require.Eventually(t, func() bool {
		_, subscriptions, _ := h.courier.counts()

		return subscriptions == 2
	}, 10*time.Second, 5*time.Millisecond)

	// The announcement arrives before the proofs.
	first := sender.payAddress(earlyAddr.Encoded, early, 600)
	h.courier.announce(t, first)
	h.waitReceiveState(first, ReceiveDetected)
	h.courier.publish(t, first.file)
	h.waitReceiveState(first, ReceiveConfirmed)

	second := sender.payAddress(lateAddr.Encoded, late, 400)
	h.courier.deliver(t, second)
	h.waitReceiveState(second, ReceiveConfirmed)
	require.EqualValues(t, 1_000, h.ownedUnits())

	// One connection serves both mailboxes, and a pass over them leaves
	// it open.
	require.NoError(t, h.client.SyncReceives(ctx))
	require.Equal(t, []string{testCourier}, h.courier.dialed)
	_, _, open := h.courier.counts()
	require.Equal(t, 1, open)

	require.NoError(t, h.client.Close())
	_, _, open = h.courier.counts()
	require.Zero(t, open)
}

// TestReceiveRestart restarts the wallet at each state of a receive. A
// detected receive is resumed from its record, and a finished one is left as
// it is.
func TestReceiveRestart(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "restart", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.announce(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveDetected)

	// The record alone resumes a detected receive: the mailbox holds no
	// message any more.
	h.courier.clearMailboxes()
	h.courier.publish(t, send.file)
	h.restart()
	h.requireReceiveState(send, ReceiveDetected)
	require.NoError(t, h.client.ResumeReceives(ctx))
	h.waitReceiveState(send, ReceiveConfirmed)
	require.EqualValues(t, 600, h.ownedUnits())
	confirmed := h.requireReceiveState(send, ReceiveConfirmed)

	// A confirmed receive stays as it is, with its message replayed.
	h.courier.announce(t, send)
	queries, _, _ := h.courier.counts()
	h.restart()
	require.NoError(t, h.client.ResumeReceives(ctx))
	require.NoError(t, h.client.SyncReceives(ctx))

	records, err := h.store.ListReceives(ctx, ReceiveFilter{})
	require.NoError(t, err)
	require.Equal(t, []ReceiveRecord{*confirmed}, records)
	require.EqualValues(t, 600, h.ownedUnits())
	laterQueries, _, _ := h.courier.counts()
	require.Equal(t, queries, laterQueries)
}

// TestReceiveRetryLimit stops working on a receive whose proofs never reach
// the courier, until the host asks for a pass.
func TestReceiveRetryLimit(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "never", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.announce(t, send)
	require.NoError(t, h.client.ResumeReceives(ctx))

	queries := func() int {
		count, _, _ := h.courier.counts()

		return count
	}
	require.Eventually(t, func() bool {
		return queries() == maxReceiveAttempts
	}, 10*time.Second, 5*time.Millisecond)

	// The message does not start the attempts over when it arrives
	// again.
	h.courier.announce(t, send)
	require.Never(t, func() bool {
		return queries() > maxReceiveAttempts
	}, 100*time.Millisecond, 5*time.Millisecond)
	h.requireReceiveState(send, ReceiveDetected)

	h.courier.publish(t, send.file)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)
}

// TestReceiveFetchLimit fetches the proofs of a few receives at a time.
func TestReceiveFetchLimit(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	const numSends = maxProofFetches + 3

	sends := make([]*addressSend, numSends)
	for idx := range sends {
		minted, addr := h.newReceivingAddress(
			sender, "limit-"+string(rune('a'+idx)), nil,
		)
		sends[idx] = sender.payAddress(addr.Encoded, minted, 100)
		h.courier.deliver(t, sends[idx])
	}

	// Every fetch waits at its first query.
	hold := make(chan struct{})
	h.courier.hold = hold

	done := make(chan error, 1)
	go func() { done <- h.client.SyncReceives(ctx) }()

	inFlight := func() (int, int) {
		h.courier.mu.Lock()
		defer h.courier.mu.Unlock()

		return h.courier.querying, h.courier.maxQuerying
	}
	require.Eventually(t, func() bool {
		querying, _ := inFlight()

		return querying == maxProofFetches
	}, 10*time.Second, 5*time.Millisecond)
	require.Never(t, func() bool {
		querying, _ := inFlight()

		return querying > maxProofFetches
	}, 100*time.Millisecond, 5*time.Millisecond)

	close(hold)
	require.NoError(t, <-done)

	_, maxQuerying := inFlight()
	require.Equal(t, maxProofFetches, maxQuerying)
	for _, send := range sends {
		h.requireReceiveState(send, ReceiveConfirmed)
	}
	require.EqualValues(t, numSends*100, h.ownedUnits())
}

// TestReceiveNeedsConfig contacts no courier for a host that set no
// ReceiveConfig.
func TestReceiveNeedsConfig(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	require.ErrorIs(t, h.client.ResumeReceives(ctx), errNoReceive)
	require.ErrorIs(t, h.client.SyncReceives(ctx), errNoReceive)
	require.Nil(t, h.client.engine.receive)
	require.Nil(t, h.client.engine.addrs)

	// The wallet works as one without addresses does.
	h.receive("plain")
	require.EqualValues(t, receivedUnits, h.ownedUnits())
	require.NoError(t, h.client.Close())
}

// TestQueryProofUniverses asks the universe of a group for the proof of a
// grouped asset, and the universe of the asset when the group's leaf is
// missing or holds another asset, but not when the first query fails.
func TestQueryProofUniverses(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	groupPriv := newTestKeyRing(4).privKey(1, 0)
	minted := sender.receiveOf("grouped", groupPriv)
	other := sender.receive("other")

	file, err := proof.DecodeFile(minted.proofFile)
	require.NoError(t, err)
	tip, err := file.LastProof()
	require.NoError(t, err)
	blob, err := file.RawProofAt(0)
	require.NoError(t, err)

	otherFile, err := proof.DecodeFile(other.proofFile)
	require.NoError(t, err)
	otherBlob, err := otherFile.RawProofAt(0)
	require.NoError(t, err)

	assetID := tip.Asset.ID()
	outpoint := tip.OutPoint()
	loc := proof.Locator{
		AssetID:   &assetID,
		GroupKey:  &tip.Asset.GroupKey.GroupPubKey,
		ScriptKey: *tip.Asset.ScriptKey.PubKey,
		OutPoint:  &outpoint,
	}
	assetLeaf := ProofKey{
		AssetID:   assetID,
		ScriptKey: asset.ToSerialized(&loc.ScriptKey),
		Outpoint:  outpoint,
	}
	groupKey := [33]byte(asset.ToSerialized(loc.GroupKey))
	groupLeaf := assetLeaf
	groupLeaf.GroupKey = &groupKey

	tests := []struct {
		name string

		// group and single are the proofs at the leaf of the group's
		// universe and of the asset's.
		group, single []byte

		// queryErr fails every query.
		queryErr error

		ungrouped   bool
		wantQueries int
		errText     string
	}{{
		name:        "group universe",
		group:       blob,
		wantQueries: 1,
	}, {
		name:        "asset universe after a missing group leaf",
		single:      blob,
		wantQueries: 2,
	}, {
		name:        "asset universe after another asset",
		group:       otherBlob,
		single:      blob,
		wantQueries: 2,
	}, {
		name:        "asset without group",
		group:       otherBlob,
		single:      blob,
		ungrouped:   true,
		wantQueries: 1,
	}, {
		name:        "no leaf",
		wantQueries: 2,
		errText:     "not found",
	}, {
		name:        "group universe fails",
		single:      blob,
		queryErr:    errors.New("universe is down"),
		wantQueries: 1,
		errText:     "universe is down",
	}, {
		name:        "another asset in the asset universe",
		single:      otherBlob,
		wantQueries: 2,
		errText:     "a proof of another asset",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			courier := newMemCourier()
			courier.queryErr = tc.queryErr
			if tc.group != nil {
				courier.leaves[leafName(groupLeaf)] = tc.group
			}
			if tc.single != nil {
				courier.leaves[leafName(assetLeaf)] = tc.single
			}
			conn, err := courier.dial(ctx, &url.URL{})
			require.NoError(t, err)

			query := loc
			if tc.ungrouped {
				query.GroupKey = nil
			}
			got, err := queryProof(ctx, conn, query)
			if tc.errText != "" {
				require.ErrorContains(t, err, tc.errText)
			} else {
				require.NoError(t, err)
				require.Equal(t, proof.Blob(blob), got)
			}

			queries, _, _ := courier.counts()
			require.Equal(t, tc.wantQueries, queries)
		})
	}
}

// TestReceivePayloadVersions decodes a payload of each version of the
// encoding.
func TestReceivePayloadVersions(t *testing.T) {
	fragment := &proof.SendFragment{
		Version:     proof.SendFragmentV1,
		BlockHeight: 7,
		OutPoint:    wire.OutPoint{Hash: chainhash.Hash{1}, Index: 2},
		Outputs: map[asset.ID]proof.SendOutput{
			{3}: {Amount: 4},
		},
	}

	for _, reorged := range []bool{false, true} {
		encoded, err := (&receivePayload{
			fragment:    fragment,
			anchorValue: 1_000,
			failure:     "failure",
			reorged:     reorged,
		}).encode()
		require.NoError(t, err)

		decoded, err := decodeReceivePayload(encoded)
		require.NoError(t, err)
		require.Equal(t, reorged, decoded.reorged)
		require.EqualValues(t, 1_000, decoded.anchorValue)
		require.Equal(t, "failure", decoded.failure)
		require.Equal(t, fragment.OutPoint, decoded.fragment.OutPoint)

		// The first version ends before the re-org marker.
		first := append([]byte{}, encoded[:len(encoded)-1]...)
		first[0] = receivePayloadNoReorg
		decoded, err = decodeReceivePayload(first)
		require.NoError(t, err)
		require.False(t, decoded.reorged)
		require.Equal(t, "failure", decoded.failure)

		_, err = decodeReceivePayload(encoded[:len(encoded)-1])
		require.Error(t, err)

		encoded[0] = receivePayloadVersion + 1
		_, err = decodeReceivePayload(encoded)
		require.ErrorContains(t, err, "unknown receive payload version")
	}
}
