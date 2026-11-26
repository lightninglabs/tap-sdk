package embedded

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/rpcutils"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sendToAddress funds a transfer of amount units to an address and publishes
// it. The anchor transaction is not confirmed.
func (h *testHarness) sendToAddress(addr string, amount uint64) chainhash.Hash {
	h.t.Helper()

	funded, _, _ := h.fundSend(addr, amount)
	h.sends++
	committed := h.commitSend(funded, h.sends)

	_, err := h.client.PublishAndLogTransferWithRequest(
		h.t.Context(), committed.request,
	)
	require.NoError(h.t, err)

	return committed.txid
}

// transferPayload returns the stored payload of a transfer.
func (h *testHarness) transferPayload(txid chainhash.Hash) *transferPayload {
	h.t.Helper()

	transfers, err := h.store.ListTransfers(h.t.Context(), &txid)
	require.NoError(h.t, err)
	require.Len(h.t, transfers, 1)

	payload, err := decodeTransferPayload(transfers[0].Payload)
	require.NoError(h.t, err)

	return payload
}

// waitDelivered waits until every delivery of a transfer is done.
func (h *testHarness) waitDelivered(txid chainhash.Hash) {
	h.t.Helper()

	require.Eventually(h.t, func() bool {
		transfers, err := h.store.ListTransfers(h.t.Context(), &txid)

		return err == nil && len(transfers) == 1 &&
			transfers[0].State != TransferPending &&
			!deliveryPending(transfers[0].Payload)
	}, 10*time.Second, 5*time.Millisecond)

	payload := h.transferPayload(txid)
	require.NotEmpty(h.t, payload.deliveries)
	require.False(h.t, payload.deliveryPending())
}

// requireUndelivered checks that a delivery of a transfer is not done, and
// that no courier holds an announcement of it.
func (h *testHarness) requireUndelivered(txid chainhash.Hash) {
	h.t.Helper()

	require.True(h.t, h.transferPayload(txid).deliveryPending())
	require.Empty(h.t, h.courier.messagesAbout(txid))
}

// messagesAbout returns the uploads that announce an output of a transaction.
func (c *memCourier) messagesAbout(txid chainhash.Hash) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var messages []string
	for _, upload := range c.uploads {
		if strings.HasPrefix(upload, "message/"+txid.String()) {
			messages = append(messages, upload)
		}
	}

	return messages
}

// failInserts makes the courier fail its proof inserts with the error that
// fail returns. Nil ends the failure.
func (c *memCourier) failInserts(fail func(key ProofKey) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.insertErr = fail
}

// deliveryStatuses returns the proof delivery status of each output of a
// transfer by the index of its anchor output.
func (h *testHarness) deliveryStatuses(
	txid chainhash.Hash) map[uint32]taprpc.ProofDeliveryStatus {

	h.t.Helper()

	statuses := make(map[uint32]taprpc.ProofDeliveryStatus)
	for _, out := range h.transferPayload(txid).transfer.Outputs {
		outpoint, err := wire.NewOutPointFromString(out.Anchor.Outpoint)
		require.NoError(h.t, err)
		statuses[outpoint.Index] = out.ProofDeliveryStatus
	}

	return statuses
}

// requireReceived syncs the mailboxes of a wallet and checks the units it
// holds.
func (h *testHarness) requireReceived(units uint64) {
	h.t.Helper()

	require.NoError(h.t, h.client.SyncReceives(h.t.Context()))
	require.Equal(h.t, units, h.ownedUnits())
}

// TestSendToAddress pays an address of an asset and of a group: once the
// anchor transaction confirms, the courier of the address holds the proofs
// and then the announcement, and the recipient receives the units and pays
// part of them back.
func TestSendToAddress(t *testing.T) {
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
			sender, recipient := newSendHarness(t)
			ctx := t.Context()
			courier := sender.courier

			minted, addr := recipient.newReceivingAddress(
				sender, "send-"+tc.name, tc.groupPriv,
			)
			txid := sender.sendToAddress(addr.Encoded, 300)

			// Nothing reaches the courier before the transfer
			// confirms.
			require.Equal(t, map[uint32]taprpc.ProofDeliveryStatus{
				0: rpcutils.ProofDeliveryStatusPending,
				1: rpcutils.ProofDeliveryStatusNotApplicable,
			}, sender.deliveryStatuses(txid))
			require.Empty(t, courier.uploads)

			sender.chain.mine(t)
			sender.waitTransfer(txid, TransferConfirmed)
			sender.waitDelivered(txid)
			require.Equal(t, map[uint32]taprpc.ProofDeliveryStatus{
				0: rpcutils.ProofDeliveryStatusComplete,
				1: rpcutils.ProofDeliveryStatusNotApplicable,
			}, sender.deliveryStatuses(txid))

			// The issuance and the transfer are in the universe of
			// the asset, and for a grouped asset in the universe of
			// the group, before the announcement.
			wantUploads := 2
			if tc.groupPriv != nil {
				wantUploads = 4
			}
			uploads := slices.Clone(courier.uploads)
			require.Len(t, uploads, wantUploads+1)
			for _, upload := range uploads[:wantUploads] {
				require.NotContains(t, upload, "message/")
			}
			paid := wire.OutPoint{Hash: txid}
			require.Equal(
				t, "message/"+paid.String(),
				uploads[wantUploads],
			)

			recipient.requireReceived(300)
			require.EqualValues(
				t, receivedUnits-300, sender.ownedUnits(),
			)

			// The recipient spends what it received.
			back, err := sender.wallet.NewReceiveAddress(
				ctx, minted.ref,
			)
			require.NoError(t, err)
			backTxid := recipient.sendToAddress(back.Encoded, 100)
			recipient.chain.mine(t)
			recipient.waitTransfer(backTxid, TransferConfirmed)
			recipient.waitDelivered(backTxid)

			sender.requireReceived(receivedUnits - 200)
			require.EqualValues(t, 200, recipient.ownedUnits())
		})
	}
}

// TestSendToAddressIssuances pays an address of a group with units of two
// issuances of the group, which no issuance holds alone. The recipient reads
// one announcement that names both issuances and receives every unit.
func TestSendToAddressIssuances(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	groupPriv := newTestKeyRing(4).privKey(1, 0)
	first := sender.receiveIssuances(groupPriv, 100, 50)[0]

	addr, err := recipient.wallet.NewReceiveAddress(ctx, first.ref)
	require.NoError(t, err)
	txid := sender.sendToAddress(addr.Encoded, 120)
	sender.chain.mine(t)
	sender.waitDelivered(txid)
	require.Len(t, sender.courier.messagesAbout(txid), 1)

	recipient.requireReceived(120)
	receives, err := recipient.store.ListReceives(ctx, ReceiveFilter{})
	require.NoError(t, err)
	require.Len(t, receives, 1)
	payload, err := decodeReceivePayload(receives[0].Payload)
	require.NoError(t, err)
	require.Len(t, payload.fragment.Outputs, 2)

	balance, err := recipient.wallet.GetBalance(ctx, first.ref)
	require.NoError(t, err)
	require.EqualValues(t, 120, balance)
	balance, err = sender.wallet.GetBalance(ctx, first.ref)
	require.NoError(t, err)
	require.EqualValues(t, 30, balance)
}

// TestSendMergedInputs pays an address from two outputs of one grouped asset
// whose proof files share their issuance. The proof of the recipient's output
// holds both files, and the courier stores each of their proofs once.
func TestSendMergedInputs(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	groupPriv := newTestKeyRing(4).privKey(1, 0)
	minted := sender.receiveOf("merged", groupPriv)

	// A payment to an address of the wallet leaves it two outputs of the
	// asset, the received one and the change.
	own, err := sender.wallet.NewReceiveAddress(ctx, minted.ref)
	require.NoError(t, err)
	split := sender.sendToAddress(own.Encoded, 300)
	sender.chain.mine(t)
	sender.waitDelivered(split)
	sender.requireReceived(receivedUnits)

	addr, err := recipient.wallet.NewReceiveAddress(ctx, minted.ref)
	require.NoError(t, err)
	_, active, _ := sender.fundSendOne(addr.Encoded, receivedUnits)
	require.Len(t, active.Inputs, 2)

	sender.courier.mu.Lock()
	before := len(sender.courier.uploads)
	sender.courier.mu.Unlock()

	txid := sender.sendToAddress(addr.Encoded, receivedUnits)
	sender.chain.mine(t)
	sender.waitDelivered(txid)

	// The issuance, the two outputs of the first transfer and the
	// output of this one, in the universes of the asset and of the
	// group, and the announcement.
	sender.courier.mu.Lock()
	uploads := slices.Clone(sender.courier.uploads[before:])
	sender.courier.mu.Unlock()
	require.Len(t, uploads, 4*2+1)
	slices.Sort(uploads)
	require.Len(t, slices.Compact(uploads), len(uploads))

	recipient.requireReceived(receivedUnits)
}

// TestSendToOwnAddress pays an address of the sending wallet, which holds the
// output from the confirmation on and lists the receive once it read its
// mailbox.
func TestSendToOwnAddress(t *testing.T) {
	sender, _ := newSendHarness(t)
	ctx := t.Context()

	minted := sender.receive("own")
	addr, err := sender.wallet.NewReceiveAddress(ctx, minted.ref)
	require.NoError(t, err)

	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitDelivered(txid)
	require.EqualValues(t, receivedUnits, sender.ownedUnits())

	sender.requireReceived(receivedUnits)
	receives, err := sender.client.AddrReceives(
		ctx, &tapsdk.AddressReceivesQuery{},
	)
	require.NoError(t, err)
	require.Len(t, receives, 1)
	require.Equal(t, tapsdk.AddressEventStatusCompleted, receives[0].Status)
}

// TestSendDeliveryRetries keeps delivering to a courier that refuses the
// proofs at first: the announcement follows the last proof, and is posted
// once.
func TestSendDeliveryRetries(t *testing.T) {
	fastRetries(t)

	sender, recipient := newSendHarness(t)
	courier := sender.courier

	_, addr := recipient.newReceivingAddress(sender, "retries", nil)

	// The universe takes the issuance proof only on the fourth call, and
	// the transfer proof on the call after.
	var calls atomic.Int32
	courier.failInserts(func(ProofKey) error {
		if calls.Add(1) < 4 {
			return errors.New("proof insert disabled for universe")
		}

		return nil
	})

	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitDelivered(txid)

	require.EqualValues(t, 5, calls.Load())
	require.Len(t, courier.uploads, 3)
	require.Equal(t, []string{
		"message/" + wire.OutPoint{Hash: txid}.String(),
	}, courier.uploads[2:])

	recipient.requireReceived(300)
}

// TestSendDeliveryMailboxFailure delivers again when the mailbox did not
// take the announcement: the proofs are inserted again, and the recipient
// reads one message.
func TestSendDeliveryMailboxFailure(t *testing.T) {
	fastRetries(t)

	sender, recipient := newSendHarness(t)
	courier := sender.courier

	_, addr := recipient.newReceivingAddress(sender, "mailbox", nil)

	courier.mu.Lock()
	courier.sendErr = errors.New("mailbox is down")
	courier.mu.Unlock()

	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitTransfer(txid, TransferConfirmed)
	require.Eventually(t, func() bool {
		courier.mu.Lock()
		defer courier.mu.Unlock()

		return len(courier.uploads) >= 4
	}, 10*time.Second, time.Millisecond)
	sender.requireUndelivered(txid)

	courier.mu.Lock()
	courier.sendErr = nil
	courier.mu.Unlock()

	sender.waitDelivered(txid)
	require.Len(t, courier.messagesAbout(txid), 1)
	recipient.requireReceived(300)
}

// deliveryRunning reports whether the client runs a delivery of a transfer.
func (h *testHarness) deliveryRunning(txid chainhash.Hash) bool {
	send := h.client.engine.send

	send.mu.Lock()
	defer send.mu.Unlock()

	_, ok := send.running[txid]

	return ok
}

// TestSendDeliveryPending stops delivering a transfer that is pending again
// while its courier refuses the proofs, and delivers it as soon as it confirms
// again.
func TestSendDeliveryPending(t *testing.T) {
	fastRetries(t)

	sender, recipient := newSendHarness(t)
	courier := sender.courier

	_, addr := recipient.newReceivingAddress(sender, "pending", nil)

	var refused atomic.Int32
	courier.failInserts(func(ProofKey) error {
		refused.Add(1)

		return errors.New("courier is down")
	})
	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitTransfer(txid, TransferConfirmed)
	require.Eventually(t, func() bool {
		return refused.Load() >= 2
	}, 10*time.Second, time.Millisecond)

	sender.chain.reorg(1)
	sender.waitTransfer(txid, TransferPending)
	require.Eventually(t, func() bool {
		return !sender.deliveryRunning(txid)
	}, 10*time.Second, time.Millisecond)
	sender.requireUndelivered(txid)

	courier.failInserts(nil)
	sender.chain.mine(t)
	sender.waitDelivered(txid)
	recipient.requireReceived(300)
}

// TestSendDeliveryRestart delivers the proofs of a transfer after a restart:
// one that confirms while no client runs, one whose courier was down, and one
// whose confirmation is final.
func TestSendDeliveryRestart(t *testing.T) {
	down := errors.New("courier is down")

	tests := []struct {
		name string

		// stop runs between the broadcast and the restart.
		stop func(t *testing.T, sender *testHarness,
			txid chainhash.Hash)
	}{{
		name: "confirmed while stopped",
		stop: func(t *testing.T, sender *testHarness,
			_ chainhash.Hash) {

			require.NoError(t, sender.client.Close())
			sender.chain.mine(t)
		},
	}, {
		name: "courier down",
		stop: func(t *testing.T, sender *testHarness,
			txid chainhash.Hash) {

			sender.courier.failInserts(func(ProofKey) error {
				return down
			})
			sender.chain.mine(t)
			sender.waitTransfer(txid, TransferConfirmed)
			require.NoError(t, sender.client.Close())
		},
	}, {
		name: "final",
		stop: func(t *testing.T, sender *testHarness,
			txid chainhash.Hash) {

			sender.courier.failInserts(func(ProofKey) error {
				return down
			})
			sender.chain.mine(t)
			sender.waitTransfer(txid, TransferConfirmed)
			sender.chain.finalize(txid)
			sender.waitTransfer(txid, TransferFinal)
			require.NoError(t, sender.client.Close())
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender, recipient := newSendHarness(t)

			_, addr := recipient.newReceivingAddress(
				sender, "restart", nil,
			)
			txid := sender.sendToAddress(addr.Encoded, 300)

			tc.stop(t, sender, txid)
			sender.requireUndelivered(txid)

			sender.courier.failInserts(nil)
			sender.restart()
			sender.waitDelivered(txid)

			// A later restart delivers nothing again.
			uploads := len(sender.courier.uploads)
			sender.restart()
			recipient.requireReceived(300)
			require.Len(t, sender.courier.uploads, uploads)
		})
	}
}

// TestSendDeliveryGroupUniverse completes a delivery when the universe of the
// group refuses a proof that the universe of the asset took, and retries
// when the universe of the group fails for another reason.
func TestSendDeliveryGroupUniverse(t *testing.T) {
	fastRetries(t)

	sender, recipient := newSendHarness(t)
	courier := sender.courier

	groupPriv := newTestKeyRing(4).privKey(1, 0)
	_, addr := recipient.newReceivingAddress(sender, "group", groupPriv)

	var failed atomic.Int32
	courier.failInserts(func(key ProofKey) error {
		switch {
		case key.GroupKey == nil:
			return nil

		case failed.Add(1) == 1:
			return errors.New("universe is busy")

		default:
			return ErrProofRefused
		}
	})

	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitDelivered(txid)

	// The first attempt stopped at the group universe, and the second
	// one inserted both proofs into the universe of the asset alone.
	require.EqualValues(t, 3, failed.Load())
	for _, upload := range courier.uploads[:len(courier.uploads)-1] {
		require.True(t, strings.HasPrefix(upload, "asset-"), upload)
	}
	require.Len(t, courier.messagesAbout(txid), 1)

	recipient.requireReceived(300)
}

// TestSendNeedsConfig refuses to publish a transfer to an address for a host
// that set no SendConfig, and records nothing.
func TestSendNeedsConfig(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	sender.sending = nil
	sender.restart()

	_, addr := recipient.newReceivingAddress(sender, "config", nil)
	funded, _, _ := sender.fundSend(addr.Encoded, 300)
	committed := sender.commitSend(funded, 1)

	_, err := sender.client.PublishAndLogTransferWithRequest(
		ctx, committed.request,
	)
	require.Equal(t, codes.Unimplemented, status.Code(err))

	transfers, err := sender.store.ListTransfers(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, transfers)
	require.EqualValues(t, receivedUnits, sender.ownedUnits())
	require.Empty(t, sender.courier.dialed)

	_, err = NewClient(&Config{
		Network: tapsdk.NetworkRegtest,
		Store:   sender.store,
		KeyRing: NewLocalKeyRing(sender.keys),
		Signer:  NewKeyRingSigner(sender.keys),
		Chain:   sender.chain,
		Send:    &SendConfig{},
	})
	require.ErrorContains(t, err, "send: courier dialer is required")
}

// TestAddressDeliveries derives the deliveries of a transfer from its
// packets, and refuses an output its recipient could not find or spend.
func TestAddressDeliveries(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	minted, first := recipient.newReceivingAddress(sender, "outputs", nil)
	second, err := recipient.wallet.NewReceiveAddress(ctx, minted.ref)
	require.NoError(t, err)

	fund := func() []delivery {
		t.Helper()

		funded, err := sender.client.FundTransfer(
			ctx, []tapsdk.Recipient{
				tapsdk.RecipientWithAmount(first.Encoded, 100),
				tapsdk.RecipientWithAmount(second.Encoded, 200),
			}, nil,
		)
		require.NoError(t, err)
		packets, err := decodeVirtualPackets(funded.FundedPsbts)
		require.NoError(t, err)

		deliveries, err := addressDeliveries(packets)
		require.NoError(t, err)

		return deliveries
	}

	deliveries := fund()
	require.Len(t, deliveries, 2)
	for idx, want := range []*tapsdk.Address{first, second} {
		require.EqualValues(t, idx, deliveries[idx].anchorIndex)
		require.False(t, deliveries[idx].done)

		paid, err := address.DecodeAddress(
			deliveries[idx].address, &address.RegressionNetTap,
		)
		require.NoError(t, err)
		require.Equal(
			t, want.ScriptKey[:],
			paid.ScriptKey.SerializeCompressed(),
		)
	}

	otherKey := newTestKeyRing(9).privKey(1, 0).PubKey()

	tests := []struct {
		name    string
		change  func(t *testing.T, paid *tappsbt.VOutput)
		errText string
	}{{
		name: "another script key",
		change: func(_ *testing.T, paid *tappsbt.VOutput) {
			paid.ScriptKey = asset.NewScriptKey(otherKey)
		},
		errText: "script key is not the one derived",
	}, {
		name: "another internal key",
		change: func(_ *testing.T, paid *tappsbt.VOutput) {
			paid.AnchorOutputInternalKey = otherKey
		},
		errText: "not under the internal key",
	}, {
		name: "version 1 address",
		change: func(_ *testing.T, paid *tappsbt.VOutput) {
			addr := *paid.Address
			addr.Version = address.V1
			paid.Address = &addr
		},
		errText: "version 2 addresses only",
	}, {
		name: "two addresses in one anchor output",
		change: func(_ *testing.T, paid *tappsbt.VOutput) {
			paid.AnchorOutputIndex = 1
		},
		errText: "pays two addresses",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			funded, active, _ := sender.fundSendOne(
				first.Encoded, 100,
			)
			require.NotNil(t, funded)

			packets := []*tappsbt.VPacket{active}
			if tc.name == "two addresses in one anchor output" {
				_, other, _ := sender.fundSendOne(
					second.Encoded, 100,
				)
				for _, vOut := range other.Outputs {
					if vOut.Address != nil {
						vOut.AnchorOutputIndex = 1
					}
				}
				packets = append(packets, other)
			}
			for _, vOut := range active.Outputs {
				if vOut.Address != nil {
					tc.change(t, vOut)
				}
			}

			_, err := addressDeliveries(packets)
			require.ErrorContains(t, err, tc.errText)
		})
	}
}

// TestTransferPayloadDeliveries encodes the deliveries of a transfer, marks
// a payload with a delivery that is not done, and reads a payload that was
// written without deliveries.
func TestTransferPayloadDeliveries(t *testing.T) {
	payload := &transferPayload{
		transfer:        &taprpc.AssetTransfer{Label: "payload"},
		skipBroadcast:   true,
		passiveSuffixes: [][]byte{{1, 2}, {3}},
		deliveries: []delivery{{
			address:     "taprt1first",
			anchorIndex: 2,
			done:        true,
		}, {
			address:     "taprt1second",
			anchorIndex: 7,
		}},
	}

	encoded, err := payload.encode()
	require.NoError(t, err)
	require.EqualValues(t, transferPayloadVersion, encoded[0])
	require.True(t, deliveryPending(encoded))

	decoded, err := decodeTransferPayload(encoded)
	require.NoError(t, err)
	require.Equal(t, payload.deliveries, decoded.deliveries)
	require.Equal(t, payload.passiveSuffixes, decoded.passiveSuffixes)
	require.True(t, decoded.skipBroadcast)
	require.Equal(t, "payload", decoded.transfer.Label)

	decoded.deliveries[1].done = true
	encoded, err = decoded.encode()
	require.NoError(t, err)
	require.False(t, deliveryPending(encoded))

	// A payload without deliveries is written in the version that ends
	// after the passive suffixes.
	withDeliveries := len(encoded)
	payload.deliveries = nil
	encoded, err = payload.encode()
	require.NoError(t, err)
	require.EqualValues(t, transferPayloadNoDeliveries, encoded[0])
	require.Less(t, len(encoded), withDeliveries)

	decoded, err = decodeTransferPayload(encoded)
	require.NoError(t, err)
	require.Empty(t, decoded.deliveries)
	require.Equal(t, payload.passiveSuffixes, decoded.passiveSuffixes)
	require.False(t, deliveryPending(encoded))
	require.False(t, deliveryPending(nil))

	// The same bytes do not decode as the version with deliveries.
	truncated := bytes.Clone(encoded)
	truncated[0] = transferPayloadVersion
	_, err = decodeTransferPayload(truncated)
	require.Error(t, err)

	// A payload that ends within a delivery does not decode.
	payload.deliveries = []delivery{{address: "taprt1first"}}
	encoded, err = payload.encode()
	require.NoError(t, err)
	_, err = decodeTransferPayload(encoded[:len(encoded)-1])
	require.Error(t, err)

	encoded[0] = transferPayloadVersion + 1
	_, err = decodeTransferPayload(encoded)
	require.ErrorContains(t, err, "unknown transfer payload version")
}

// TestCollectLeaves visits the proofs of a file in the order of the file,
// each with its universe leaf.
func TestCollectLeaves(t *testing.T) {
	sender, recipient := newSendHarness(t)

	groupPriv := newTestKeyRing(4).privKey(1, 0)
	minted, addr := recipient.newReceivingAddress(
		sender, "leaves", groupPriv,
	)
	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitDelivered(txid)

	records, err := sender.store.FetchProofs(
		t.Context(), minted.assetID, [33]byte{}, nil,
	)
	require.NoError(t, err)
	require.Empty(t, records)

	outpoint := wire.OutPoint{Hash: txid}
	payload := sender.transferPayload(txid)
	var file *proof.File
	for _, out := range payload.transfer.Outputs {
		if out.Anchor.Outpoint != outpoint.String() {
			continue
		}
		scriptKey, err := btcec.ParsePubKey(out.ScriptKey)
		require.NoError(t, err)
		stored, err := sender.store.FetchProofs(
			t.Context(), minted.assetID,
			scriptKeyBytes(scriptKey), &outpoint,
		)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		require.False(t, stored[0].Owned)

		file, err = proof.DecodeFile(stored[0].ProofFile)
		require.NoError(t, err)
	}
	require.NotNil(t, file)

	var leaves []universeLeaf
	require.NoError(t, collectLeaves(file, func(leaf universeLeaf) error {
		leaves = append(leaves, leaf)

		return nil
	}))
	require.Len(t, leaves, 2)
	require.Equal(t, minted.outpoint, leaves[0].key.Outpoint)
	require.Equal(t, outpoint, leaves[1].key.Outpoint)
	for idx, leaf := range leaves {
		require.Equal(t, [32]byte(minted.assetID), leaf.key.AssetID)
		require.NotNil(t, leaf.key.GroupKey)

		raw, err := file.RawProofAt(uint32(idx))
		require.NoError(t, err)
		require.Equal(t, raw, leaf.blob)
	}

	stop := errors.New("stop")
	err = collectLeaves(file, func(universeLeaf) error { return stop })
	require.ErrorIs(t, err, stop)
}

// TestSendKeepsPassiveAsset pays an address from an anchor output that holds
// a second asset. The wallet holds that asset in the anchor output of the
// change once the transfer confirms, and spends it from there.
func TestSendKeepsPassiveAsset(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	pair := sender.receivePair("spent", "kept")
	addr, err := recipient.wallet.NewReceiveAddress(ctx, pair[0].ref)
	require.NoError(t, err)

	txid := sender.sendToAddress(addr.Encoded, 300)
	sender.chain.mine(t)
	sender.waitDelivered(txid)
	recipient.requireReceived(300)
	require.EqualValues(t, 2*receivedUnits-300, sender.ownedUnits())

	utxos, err := sender.client.ListUtxos(ctx, &tapsdk.ListUtxosRequest{})
	require.NoError(t, err)
	require.Len(t, utxos, 1)
	change := wire.OutPoint{Hash: txid, Index: 1}
	for outpoint, utxo := range utxos {
		require.Equal(t, change.String(), outpoint)
		require.Len(t, utxo.Assets, 2)
	}

	// The change of the first transfer shares the anchor output, and
	// moves to an anchor output of its own.
	keptAddr, err := recipient.wallet.NewReceiveAddress(ctx, pair[1].ref)
	require.NoError(t, err)
	keptTxid := sender.sendToAddress(keptAddr.Encoded, receivedUnits)
	sender.chain.mine(t)
	sender.waitDelivered(keptTxid)

	recipient.requireReceived(300 + receivedUnits)
	require.EqualValues(t, receivedUnits-300, sender.ownedUnits())

	balance, err := sender.wallet.GetBalance(ctx, pair[0].ref)
	require.NoError(t, err)
	require.EqualValues(t, receivedUnits-300, balance)
}
