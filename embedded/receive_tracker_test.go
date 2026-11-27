package embedded

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

// receivedProof returns the wallet's proof record of the output of a send.
func (h *testHarness) receivedProof(send *addressSend) AssetProof {
	h.t.Helper()

	proofs, err := h.store.FetchProofs(
		h.t.Context(), send.assetID,
		scriptKeyBytes(send.scriptKey.PubKey), &send.outpoint,
	)
	require.NoError(h.t, err)
	require.Len(h.t, proofs, 1)

	return proofs[0]
}

// requireReceivedIn checks that the wallet holds the output of a send, with a
// record and a valid proof file that both name the given block.
func (h *testHarness) requireReceivedIn(send *addressSend,
	state ReceiveState, block *wire.MsgBlock, height uint32) {

	h.t.Helper()
	ctx := h.t.Context()

	record := h.requireReceiveState(send, state)
	require.Equal(h.t, block.BlockHash(), record.BlockHash)
	require.Equal(h.t, height, record.BlockHeight)

	stored := h.receivedProof(send)
	require.True(h.t, stored.Owned)

	file, err := proof.DecodeFile(stored.ProofFile)
	require.NoError(h.t, err)
	tip, err := file.LastProof()
	require.NoError(h.t, err)
	require.Equal(h.t, block.BlockHash(), tip.BlockHeader.BlockHash())
	require.Equal(h.t, height, tip.BlockHeight)

	verified, err := h.client.VerifyProof(ctx, stored.ProofFile)
	require.NoError(h.t, err)
	require.True(h.t, verified.Valid)
}

// requireReceiveReorged checks that the wallet's record of a send waits for
// its anchor transaction to confirm again, and that the wallet keeps the
// proof of its output without holding the output.
func (h *testHarness) requireReceiveReorged(send *addressSend) {
	h.t.Helper()

	record := h.requireReceiveState(send, ReceiveDetected)
	require.Zero(h.t, record.BlockHash)
	require.Zero(h.t, record.BlockHeight)

	payload, err := decodeReceivePayload(record.Payload)
	require.NoError(h.t, err)
	require.True(h.t, payload.reorged)

	require.False(h.t, h.receivedProof(send).Owned)
	require.Zero(h.t, h.ownedUnits())
}

// waitReceiveReorged waits until the block of a send left the wallet's
// record of it.
func (h *testHarness) waitReceiveReorged(send *addressSend) {
	h.t.Helper()

	h.waitReceiveState(send, ReceiveDetected)
	h.requireReceiveReorged(send)
}

// waitReceiveFinal reports the confirmation of a send as final until the
// wallet's record of it is. A tracker that registers in the background takes
// the report once it is offered.
func (h *testHarness) waitReceiveFinal(send *addressSend) {
	h.t.Helper()

	require.Eventually(h.t, func() bool {
		h.chain.finalize(send.outpoint.Hash)
		record := h.receiveRecord(send)

		return record != nil && record.State == ReceiveFinal
	}, 10*time.Second, 5*time.Millisecond)
}

// receiveStatuses returns the status of every transfer the wallet lists
// under a status filter, oldest first.
func (h *testHarness) receiveStatuses(
	filter tapsdk.AddressEventStatus) []tapsdk.AddressEventStatus {

	h.t.Helper()

	events, err := h.client.AddrReceives(
		h.t.Context(), &tapsdk.AddressReceivesQuery{
			FilterStatus: filter,
			Direction:    tapsdk.SortAscending,
		},
	)
	require.NoError(h.t, err)

	statuses := make([]tapsdk.AddressEventStatus, len(events))
	for idx, event := range events {
		statuses[idx] = event.Status
	}

	return statuses
}

// anchorTx returns the anchor transaction of a send.
func (s *addressSend) anchorTx(t *testing.T) *wire.MsgTx {
	t.Helper()

	tip, err := s.file.LastProof()
	require.NoError(t, err)

	return &tip.AnchorTx
}

// TestReceiveReorg takes a confirmed receive through a re-org of its
// confirming block: the output leaves the wallet, and returns with a proof of
// the block that confirms the transfer again. The receive is final once the
// chain source reports its confirmation as final, and the output is spent
// with the proof of the new block.
func TestReceiveReorg(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	const (
		anyStatus = tapsdk.AddressEventStatusUnknown
		detected  = tapsdk.AddressEventStatusTransactionDetected
		announced = tapsdk.AddressEventStatusTransactionConfirmed
		completed = tapsdk.AddressEventStatusCompleted
	)
	one := func(
		status tapsdk.AddressEventStatus) []tapsdk.AddressEventStatus {

		return []tapsdk.AddressEventStatus{status}
	}

	minted, addr := h.newReceivingAddress(sender, "reorg", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))

	tip, err := send.file.LastProof()
	require.NoError(t, err)
	first := h.chain.blocks[tip.BlockHeight]
	h.requireReceivedIn(send, ReceiveConfirmed, first, tip.BlockHeight)
	require.EqualValues(t, 600, h.ownedUnits())
	require.Equal(t, one(completed), h.receiveStatuses(completed))

	h.chain.reorg(1)
	h.waitReceiveReorged(send)
	require.Equal(t, one(detected), h.receiveStatuses(anyStatus))
	require.Equal(t, one(detected), h.receiveStatuses(detected))
	require.Empty(t, h.receiveStatuses(announced))
	require.Empty(t, h.receiveStatuses(completed))

	// A pass over the mailbox leaves the receive to its tracker.
	queries, _, _ := h.courier.counts()
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveReorged(send)
	laterQueries, _, _ := h.courier.counts()
	require.Equal(t, queries, laterQueries)

	second, height := h.chain.mine(t)
	require.NotEqual(t, first.BlockHash(), second.BlockHash())
	h.waitReceiveState(send, ReceiveConfirmed)
	h.requireReceivedIn(send, ReceiveConfirmed, second, height)
	require.EqualValues(t, 600, h.ownedUnits())
	require.Equal(t, one(completed), h.receiveStatuses(anyStatus))

	h.waitReceiveFinal(send)
	h.requireReceivedIn(send, ReceiveFinal, second, height)
	require.Equal(t, one(completed), h.receiveStatuses(completed))
	require.Eventually(t, func() bool {
		receive := h.client.engine.receive
		receive.mu.Lock()
		defer receive.mu.Unlock()

		return len(receive.tracked) == 0
	}, 10*time.Second, 5*time.Millisecond)

	spend := h.commitTransfer(&mintedAsset{
		ref:       minted.ref,
		assetID:   send.assetID,
		proofFile: h.receivedProof(send).ProofFile,
		outpoint:  send.outpoint,
	}, 600, 250, 9)
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, spend.transfer, spend.finalPSBT,
	)
	require.NoError(t, err)
	h.chain.mine(t)
	h.waitTransfer(spend.txid, TransferConfirmed)
	require.EqualValues(t, 350, h.ownedUnits())
}

// TestReceiveReorgSpentOutput re-organizes the block of a receive and the
// block of the transfer that spends its output. The transfer confirms again
// on the proof of the new block, and the received output stays spent.
func TestReceiveReorgSpentOutput(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "spent", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)

	spend := h.commitTransfer(&mintedAsset{
		ref:       minted.ref,
		assetID:   send.assetID,
		proofFile: h.receivedProof(send).ProofFile,
		outpoint:  send.outpoint,
	}, 600, 250, 9)
	_, err := customanchor.PublishTransfer(
		ctx, h.wallet, spend.transfer, spend.finalPSBT,
	)
	require.NoError(t, err)
	h.chain.mine(t)
	h.waitTransfer(spend.txid, TransferConfirmed)
	require.EqualValues(t, 350, h.ownedUnits())

	h.chain.reorg(2)
	h.waitTransfer(spend.txid, TransferPending)
	h.waitReceiveReorged(send)
	require.True(t, h.receivedProof(send).Spent)

	// One block confirms both transactions again.
	block, height := h.chain.mine(t)
	h.waitReceiveState(send, ReceiveConfirmed)
	h.requireReceivedIn(send, ReceiveConfirmed, block, height)
	require.True(t, h.receivedProof(send).Spent)

	h.waitTransfer(spend.txid, TransferConfirmed)
	require.EqualValues(t, 350, h.ownedUnits())
	changeProof, err := h.client.ExportProof(
		ctx, minted.ref, spend.change.PubKey, nil,
	)
	require.NoError(t, err)
	verified, err := h.client.VerifyProof(ctx, changeProof.RawProofFile)
	require.NoError(t, err)
	require.True(t, verified.Valid)
}

// TestReceiveResumeStaleConfirmation starts a wallet whose confirmed receive
// lost its block while nothing followed it. No re-org event reports that
// block, so the tracker finds it gone and returns the receive to detected
// until its anchor transaction confirms again.
func TestReceiveResumeStaleConfirmation(t *testing.T) {
	tests := []struct {
		name  string
		start func(ctx context.Context, client *Client) error
	}{{
		name: "resume",
		start: func(ctx context.Context, client *Client) error {
			return client.ResumeReceives(ctx)
		},
	}, {
		name: "sync",
		start: func(ctx context.Context, client *Client) error {
			return client.SyncReceives(ctx)
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fastRetries(t)

			h := newAddressHarness(t, testCourier)
			sender := h.peer(2)
			ctx := t.Context()

			minted, addr := h.newReceivingAddress(
				sender, "stale", nil,
			)
			send := sender.payAddress(addr.Encoded, minted, 600)
			h.courier.deliver(t, send)
			require.NoError(t, h.client.SyncReceives(ctx))
			h.requireReceiveState(send, ReceiveConfirmed)

			// While the wallet is down, another block replaces the
			// confirming one and the network forgets the
			// transaction. Nothing broadcasts it again.
			require.NoError(t, h.client.Close())
			require.NoError(t, sender.client.Close())
			h.chain.reorg(1)
			h.chain.mu.Lock()
			h.chain.mempool = nil
			h.chain.mu.Unlock()
			h.chain.mine(t)

			h.restart()
			h.requireReceiveState(send, ReceiveConfirmed)
			require.NoError(t, tc.start(ctx, h.client))
			h.waitReceiveReorged(send)

			second, height := h.chain.mine(t, send.anchorTx(t))
			h.waitReceiveState(send, ReceiveConfirmed)
			h.requireReceivedIn(
				send, ReceiveConfirmed, second, height,
			)
			require.EqualValues(t, 600, h.ownedUnits())

			// A restart on the same chain keeps the confirmation.
			h.restart()
			require.NoError(t, tc.start(ctx, h.client))
			h.waitReceiveFinal(send)
			h.requireReceivedIn(send, ReceiveFinal, second, height)
			require.EqualValues(t, 600, h.ownedUnits())
		})
	}
}

// TestReceiveReorgAtRegistration re-organizes the block of a confirmed
// receive right before its tracker registers for the confirmation. The
// registration reports no event for that block, and the tracker still finds
// it gone.
func TestReceiveReorgAtRegistration(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "register", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)

	require.NoError(t, sender.client.Close())
	h.restart()

	var once sync.Once
	h.chain.mu.Lock()
	h.chain.beforeRegister = func() {
		once.Do(func() { h.chain.reorg(1) })
	}
	h.chain.mu.Unlock()

	require.NoError(t, h.client.ResumeReceives(ctx))
	h.waitReceiveReorged(send)

	block, height := h.chain.mine(t)
	h.waitReceiveState(send, ReceiveConfirmed)
	h.requireReceivedIn(send, ReceiveConfirmed, block, height)
}

// TestReceiveResumeFinal starts a wallet whose receive confirmed again in
// another block while nothing followed it, on a chain that holds the
// confirmation and Done at registration. The receive ends final with a proof
// of the new block, whichever event the tracker receives first.
func TestReceiveResumeFinal(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "final", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)

	require.NoError(t, h.client.Close())
	require.NoError(t, sender.client.Close())
	h.chain.reorg(1)
	block, height := h.chain.mine(t)

	h.chain.mu.Lock()
	h.chain.finalAtRegistration = true
	h.chain.mu.Unlock()

	// The tracker receives Done first about half of the time, so the
	// receive is resumed often enough to see both orders.
	confirmed := h.store
	for range 32 {
		h.store = confirmed.clone()
		h.addresses.Store = h.store
		h.restart()
		require.NoError(t, h.client.ResumeReceives(ctx))

		h.waitReceiveState(send, ReceiveFinal)
		h.requireReceivedIn(send, ReceiveFinal, block, height)
		require.EqualValues(t, 600, h.ownedUnits())
	}
}

// flakyReceives is an address store that fails every second update of a
// receive, as a store that stops between the updates of two receives does.
type flakyReceives struct {
	AddressStore

	mu      sync.Mutex
	updates int
}

// UpdateReceive fails on every second call.
func (f *flakyReceives) UpdateReceive(ctx context.Context,
	receive ReceiveRecord, putProofs []AssetProof) error {

	f.mu.Lock()
	f.updates++
	fail := f.updates%2 == 0
	f.mu.Unlock()

	if fail {
		return errors.New("store is down")
	}

	return f.AddressStore.UpdateReceive(ctx, receive, putProofs)
}

// TestReceiveReorgAnnouncements follows an output that three announcements
// name: two confirmed ones and a failed one. The confirmed ones move through
// a re-org together, with a store that fails between their updates, and the
// failed one stays as it is.
func TestReceiveReorgAnnouncements(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "announced", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))

	failed := send.announcedAs(func(fragment *proof.SendFragment) {
		fragment.TaprootAssetRoot[0] ^= 1
	})
	h.courier.announce(t, failed)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveFailure(failed, errAnnouncedAgain.Error())

	// A second confirmed announcement of the asset, as an announcement of
	// a part of a transfer and one of the whole leave behind.
	second := send.announcedAs(func(fragment *proof.SendFragment) {
		fragment.TaprootAssetRoot[1] ^= 1
	})
	record := *h.requireReceiveState(send, ReceiveConfirmed)
	payload, err := decodeReceivePayload(record.Payload)
	require.NoError(t, err)
	payload.fragment = second.fragment
	record.Announcement, err = announcementOf(second.fragment)
	require.NoError(t, err)
	record.Payload, err = payload.encode()
	require.NoError(t, err)
	_, err = h.store.InsertReceive(ctx, record)
	require.NoError(t, err)

	h.addresses.Store = &flakyReceives{AddressStore: h.store}
	h.restart()
	require.NoError(t, h.client.ResumeReceives(ctx))

	h.chain.reorg(1)
	h.waitReceiveReorged(send)
	h.waitReceiveReorged(second)
	h.requireReceiveFailure(failed, errAnnouncedAgain.Error())

	block, height := h.chain.mine(t)
	for _, confirmed := range []*addressSend{send, second} {
		h.waitReceiveState(confirmed, ReceiveConfirmed)
		h.requireReceivedIn(confirmed, ReceiveConfirmed, block, height)
	}
	require.EqualValues(t, 600, h.ownedUnits())

	h.waitReceiveFinal(send)
	h.waitReceiveFinal(second)
	h.requireReceivedIn(send, ReceiveFinal, block, height)
	h.requireReceivedIn(second, ReceiveFinal, block, height)
	h.requireReceiveFailure(failed, errAnnouncedAgain.Error())
	require.EqualValues(t, 600, h.ownedUnits())

	// A later announcement of the asset finds it held.
	third := send.announcedAs(func(fragment *proof.SendFragment) {
		fragment.TaprootAssetRoot[2] ^= 1
	})
	h.courier.announce(t, third)
	h.waitReceiveState(third, ReceiveFailed)
	h.requireReceiveFailure(third, errAnnouncedAgain.Error())
}

// TestReceiveReorgEarlierProof re-organizes the block of a receive and the
// block of an earlier proof of its file. The tracker writes the new block
// into the last proof only, so the file does not verify and the output stays
// out of the wallet.
func TestReceiveReorgEarlierProof(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	// The asset is issued in the block before the one of the transfer.
	minted, addr := h.newReceivingAddress(sender, "earlier", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveState(send, ReceiveConfirmed)

	require.NoError(t, sender.client.Close())
	h.chain.reorg(2)
	h.waitReceiveReorged(send)

	block, height := h.chain.mine(t)
	require.Len(t, block.Transactions, 3)
	require.Never(t, func() bool {
		return h.receiveRecord(send).State != ReceiveDetected
	}, 200*time.Millisecond, 5*time.Millisecond)
	h.requireReceiveReorged(send)

	// A confirmation that no re-org event preceded leaves no receive
	// confirmed in the block that left the chain.
	h.restart()
	record := *h.requireReceiveState(send, ReceiveDetected)
	payload, err := decodeReceivePayload(record.Payload)
	require.NoError(t, err)
	payload.reorged = false
	record.State = ReceiveConfirmed
	record.BlockHash = chainhash.Hash{1}
	record.BlockHeight = height
	record.Payload, err = payload.encode()
	require.NoError(t, err)
	stored := h.receivedProof(send)
	stored.Owned = true
	require.NoError(
		t, h.store.UpdateReceive(ctx, record, []AssetProof{stored}),
	)
	require.EqualValues(t, 600, h.ownedUnits())

	err = h.client.engine.receive.reconfirm(
		ctx, send.outpoint, &Confirmation{Height: height, Block: block},
	)
	require.ErrorContains(t, err, "is not on the best chain")
	h.requireReceiveReorged(send)
}

// replaceSendBlock closes the sender, so that it broadcasts nothing again,
// and replaces the block of its send, the best one, with another block. The
// new block holds the anchor transaction only when keepTx is set.
func (h *testHarness) replaceSendBlock(sender *testHarness,
	keepTx bool) (*wire.MsgBlock, uint32) {

	h.t.Helper()

	require.NoError(h.t, sender.client.Close())
	h.chain.reorg(1)
	if !keepTx {
		h.chain.mu.Lock()
		h.chain.mempool = nil
		h.chain.mu.Unlock()
	}

	return h.chain.mine(h.t)
}

// TestReceiveStaleBlock works on an announcement whose block left the chain
// before the wallet read it. The wallet holds the output once the anchor
// transaction confirms in another block, with a proof of that block, whether
// it confirms before or after the wallet reads the announcement.
func TestReceiveStaleBlock(t *testing.T) {
	for _, confirmedFirst := range []bool{true, false} {
		name := "confirmed after"
		if confirmedFirst {
			name = "confirmed before"
		}

		t.Run(name, func(t *testing.T) {
			fastRetries(t)

			h := newAddressHarness(t, testCourier)
			sender := h.peer(2)
			ctx := t.Context()

			minted, addr := h.newReceivingAddress(
				sender, "stale", nil,
			)
			send := sender.payAddress(addr.Encoded, minted, 600)
			h.courier.deliver(t, send)

			block, height := h.replaceSendBlock(
				sender, confirmedFirst,
			)
			require.NoError(t, h.client.SyncReceives(ctx))
			if !confirmedFirst {
				h.requireReceiveReorged(send)
				block, height = h.chain.mine(
					t, send.anchorTx(t),
				)
			}

			h.waitReceiveState(send, ReceiveConfirmed)
			h.requireReceivedIn(
				send, ReceiveConfirmed, block, height,
			)
			require.NotEqual(
				t, send.fragment.BlockHeader.BlockHash(),
				block.BlockHash(),
			)
			require.EqualValues(t, 600, h.ownedUnits())

			h.waitReceiveFinal(send)
			h.requireReceivedIn(send, ReceiveFinal, block, height)
		})
	}
}

// TestReceiveStaleBlockUnconfirmed works on an announcement whose block left
// the chain before the wallet read it, and whose anchor transaction does not
// confirm again. The receive stays detected with its proofs kept as not
// owned, and a pass over the mailbox leaves it to its tracker.
func TestReceiveStaleBlockUnconfirmed(t *testing.T) {
	fastRetries(t)

	h := newAddressHarness(t, testCourier)
	sender := h.peer(2)
	ctx := t.Context()

	minted, addr := h.newReceivingAddress(sender, "unconfirmed", nil)
	send := sender.payAddress(addr.Encoded, minted, 600)
	h.courier.deliver(t, send)

	h.replaceSendBlock(sender, false)
	require.NoError(t, h.client.SyncReceives(ctx))
	h.requireReceiveReorged(send)

	queries, _, _ := h.courier.counts()
	h.chain.mine(t)
	require.NoError(t, h.client.SyncReceives(ctx))
	require.Never(t, func() bool {
		return h.receiveRecord(send).State != ReceiveDetected
	}, 200*time.Millisecond, 5*time.Millisecond)
	h.requireReceiveReorged(send)
	laterQueries, _, _ := h.courier.counts()
	require.Equal(t, queries, laterQueries)

	receive := h.client.engine.receive
	receive.mu.Lock()
	_, tracked := receive.tracked[send.outpoint]
	receive.mu.Unlock()
	require.True(t, tracked)
}
