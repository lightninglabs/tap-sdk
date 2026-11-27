//go:build itest

package itest

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/embedded"
	"github.com/stretchr/testify/require"
)

const (
	// embeddedITestSecondIssuance is the number of units Alice issues in
	// the second issuance of a group.
	embeddedITestSecondIssuance = uint64(500)

	// embeddedITestSecondReceived is the number of units Alice sends to
	// the engine from the second issuance.
	embeddedITestSecondReceived = uint64(450)

	// embeddedITestSentAcross is the number of units the engine sends to
	// Bob from two issuances, more than either holds.
	embeddedITestSentAcross = uint64(900)
)

// TestEmbeddedAddressSend pays V2 addresses of Bob's tapd from an embedded
// engine. The engine funds each transfer from units it received at addresses
// of its own, and lnd-alice pays for the anchor transaction. Once it
// confirms, the engine stores the proofs in the universe of the address's
// proof courier and announces the transfer in its mailbox, from which Bob's
// tapd completes the receive.
func TestEmbeddedAddressSend(t *testing.T) {
	h, ctx := newFundedHarness(t)
	lnd := newAliceLndWalletKit(t)

	// A courier takes the proofs of a sender only once its universe
	// allows inserts.
	h.EnableUniverseBootstrap(t, ctx)

	tests := []struct {
		name    string
		grouped bool

		// courier is the proof courier Bob's address names. Empty
		// selects Bob's default, his own tapd.
		courier string

		// issuances is the number of issuances of the group the
		// engine holds and pays from. Zero means one.
		issuances int
	}{{
		name: "ungrouped",
	}, {
		name:    "grouped",
		grouped: true,
	}, {
		// Neither the sender nor the recipient runs the courier.
		name:    "another tapd as courier",
		grouped: true,
		courier: "authmailbox+universerpc://" +
			defaultAliceUniverseHost,
	}, {
		name:      "grouped across two issuances",
		grouped:   true,
		issuances: 2,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				minted *MintResult
				err    error
				name   = uniqueEventLabel("embedded-send")
			)
			if tc.grouped {
				minted, err = h.CreateFungibleAndConfirm(
					t, ctx, name, embeddedITestSupply,
				)
			} else {
				ungrouped := &tapsdk.MintAsset{
					AssetType:     tapsdk.AssetTypeFungible,
					Name:          name,
					InitialSupply: embeddedITestSupply,
				}
				minted, err = h.MintAssetAndConfirm(
					t, ctx, ungrouped,
				)
			}
			require.NoError(t, err)
			require.Equal(t, tc.grouped, minted.Ref.IsGroupRef())

			node := h.NewEmbeddedNode(t)
			testEmbeddedAddressSend(
				t, ctx, h, node, lnd, minted, tc.courier,
				max(tc.issuances, 1),
			)
		})
	}
}

func testEmbeddedAddressSend(t *testing.T, ctx context.Context,
	h *TestHarness, node *EmbeddedNode, lnd *lndWalletKit,
	minted *MintResult, courier string, issuances int) {

	timeout := balanceTimeoutFor(minted.Ref)

	// The engine holds what Alice pays to its addresses.
	var held uint64
	receive := func(amount uint64) {
		own, err := node.Client.NewAddr(ctx, &tapsdk.NewAddressRequest{
			AssetRef: minted.Ref,
		})
		require.NoError(t, err)
		_, err = h.AliceWallet.Send(
			ctx, own.Encoded, tapsdk.WithAmount(amount),
		)
		require.NoError(t, err)
		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		node.WaitForAddressReceive(t, ctx, own, true, timeout)
		held += amount
	}
	receive(embeddedITestReceived)

	// Alice's tapd pays from her largest output that holds the amount,
	// so the second payment comes from the second issuance.
	sent := embeddedITestSent
	if issuances == 2 {
		_, err := h.IssueFungibleAndConfirm(
			t, ctx, minted.Ref, embeddedITestSecondIssuance,
		)
		require.NoError(t, err)
		receive(embeddedITestSecondReceived)
		sent = embeddedITestSentAcross
	}

	// Bob's tapd creates an address only for an asset whose issuances
	// it synced, which CreateReceiveAddress waits for.
	addr := h.CreateReceiveAddress(t, ctx, minted.Ref)
	if courier != "" {
		version := tapsdk.AddressVersionV2
		var err error
		addr, err = h.BobClient.NewAddr(ctx, &tapsdk.NewAddressRequest{
			AssetRef:         addr.AssetRef,
			ProofCourierAddr: courier,
			AddressVersion:   &version,
		})
		require.NoError(t, err)
		require.Equal(t, courier, addr.ProofCourierAddr)
	}

	txid, packet := sendFromEmbedded(
		t, ctx, h, node, lnd, addr.Encoded, sent,
	)
	require.Len(t, packet.VirtualTransactions, issuances)
	h.MineBlocks(t, defaultMineBlocks)
	node.WaitForTransferState(t, ctx, txid, embedded.TransferFinal)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	// Bob's tapd read the announcement in the mailbox of its address and
	// fetched the proofs from the universe.
	event := waitForTapdAddressReceive(t, ctx, h.BobClient, addr, timeout)
	require.Equal(
		t, wire.OutPoint{Hash: txid, Index: 0}.String(), event.Outpoint,
	)
	require.True(t, event.HasProof)
	h.WaitForBalance(t, ctx, h.BobWallet, minted.Ref, sent, timeout)

	balance, err := node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, held-sent, balance)
}

// sendFromEmbedded sends amount units from the engine to a Taproot Asset
// address through TxBuilder and returns the anchor transaction ID and final
// asset packet. The engine funds and signs the virtual transactions and signs
// its asset anchors. An output of lnd-alice pays the fee and collects the
// Bitcoin change.
func sendFromEmbedded(t *testing.T, ctx context.Context, h *TestHarness,
	node *EmbeddedNode, lnd *lndWalletKit, addr string,
	amount uint64) (chainhash.Hash, *tapsdk.AssetPacket) {

	t.Helper()

	var fundingIndex int
	packet, err := node.Wallet.NewTxBuilder().
		AddRecipient(addr, amount).
		SetAnchorFunder(func(ctx context.Context,
			template []byte) ([]byte, error) {

			anchor, err := psbt.NewFromRawBytes(
				bytes.NewReader(template), false,
			)
			if err != nil {
				return nil, err
			}

			funding := h.fundLndInput(t, ctx, lnd)
			fundingIndex = len(anchor.Inputs)
			anchor.UnsignedTx.AddTxIn(
				wire.NewTxIn(&funding.outpoint, nil, nil),
			)
			anchor.Inputs = append(
				anchor.Inputs, funding.psbtInput(),
			)

			bitcoinChange := -embeddedITestFeeSat
			for _, input := range anchor.Inputs {
				bitcoinChange += input.WitnessUtxo.Value
			}
			for _, txOut := range anchor.UnsignedTx.TxOut {
				bitcoinChange -= txOut.Value
			}
			anchor.UnsignedTx.AddTxOut(&wire.TxOut{
				Value:    bitcoinChange,
				PkScript: funding.txOut.PkScript,
			})
			anchor.Outputs = append(
				anchor.Outputs, funding.psbtChangeOutput(),
			)

			var buf bytes.Buffer
			if err := anchor.Serialize(&buf); err != nil {
				return nil, err
			}

			return buf.Bytes(), nil
		}).
		SetAnchorSigner(func(ctx context.Context,
			anchorPsbt []byte) ([]byte, error) {

			signedPsbt, err := lnd.signPsbt(ctx, anchorPsbt)
			if err != nil {
				return nil, err
			}

			return customAnchorFinalizeKeyPath(
				t, signedPsbt, fundingIndex,
			), nil
		}).
		SetSignAnchorInputs(true).
		SetLabel(uniqueEventLabel("embedded-send")).
		Execute(ctx)
	require.NoError(t, err)
	require.Empty(t, packet.PassiveAssetTransactions)

	var anchorTx wire.MsgTx
	require.NoError(
		t, anchorTx.Deserialize(
			bytes.NewReader(packet.AnchorTransaction),
		),
	)
	txid := anchorTx.TxHash()
	node.WaitForTransferState(t, ctx, txid, embedded.TransferPending)

	return txid, packet
}

// TestEmbeddedAddressSendReorg has one embedded engine pay another across a
// re-org of the confirming block: the sender delivers its proofs and
// announcement to Bob's courier after 1 confirmation, the block is invalidated
// before the recipient reads its mailbox, and the recipient recovers the
// transfer in the block that confirms the anchor transaction again.
func TestEmbeddedAddressSendReorg(t *testing.T) {
	h, ctx := newFundedHarness(t)
	lnd := newAliceLndWalletKit(t)
	h.EnableUniverseBootstrap(t, ctx)

	minted, err := h.CreateFungibleAndConfirm(
		t, ctx, uniqueEventLabel("embedded-send-reorg"),
		embeddedITestSupply,
	)
	require.NoError(t, err)

	timeout := balanceTimeoutFor(minted.Ref)
	sender := h.NewEmbeddedNode(t)
	recipient := h.NewEmbeddedNode(t)

	// Fund the sender from Alice.
	own, err := sender.Client.NewAddr(ctx, &tapsdk.NewAddressRequest{
		AssetRef: minted.Ref,
	})
	require.NoError(t, err)
	_, err = h.AliceWallet.Send(
		ctx, own.Encoded, tapsdk.WithAmount(embeddedITestReceived),
	)
	require.NoError(t, err)
	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	sender.WaitForAddressReceive(t, ctx, own, true, timeout)

	// Recipient creates an address but stays offline across the re-org.
	addr, err := recipient.Client.NewAddr(ctx, &tapsdk.NewAddressRequest{
		AssetRef: minted.Ref,
	})
	require.NoError(t, err)

	txid, _ := sendFromEmbedded(
		t, ctx, h, sender, lnd, addr.Encoded, embeddedITestSent,
	)
	h.MineBlocks(t, 1)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)
	sender.WaitForTransferState(t, ctx, txid, embedded.TransferConfirmed)

	// Wait until the sender has uploaded the proofs and mailbox
	// announcement for the first confirming block.
	deliveredLog := fmt.Sprintf(
		"Delivered the proofs of output 0 of transfer %v", txid,
	)
	require.Eventually(t, func() bool {
		return strings.Contains(sender.log.String(), deliveredLog)
	}, timeout, embeddedChainPollInterval)

	transfers, err := sender.Store.ListTransfers(ctx, &txid)
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	firstBlockHash := transfers[0].BlockHash

	// Invalidate the confirming block before the recipient reads its
	// mailbox, then confirm the anchor transaction in a new block.
	require.Equal(t, firstBlockHash, h.invalidateBestBlock(t))
	h.MineBlocks(t, defaultMineBlocks)
	sender.WaitForTransferState(t, ctx, txid, embedded.TransferFinal)
	h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

	// The recipient wakes up, reads the stale-block announcement, and
	// follows the anchor transaction to finality in the new block.
	recipient.WaitForAddressReceive(t, ctx, addr, true, timeout)
	outpoint := tapsdk.Outpoint{Txid: tapsdk.Hash(txid), Index: 0}
	final := recipient.WaitForReceiveState(
		t, ctx, outpoint, embedded.ReceiveFinal,
	)
	require.NotEqual(t, firstBlockHash, final.BlockHash)

	recipientBalance, err := recipient.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, embeddedITestSent, recipientBalance)

	senderBalance, err := sender.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, embeddedITestReceived-embeddedITestSent, senderBalance)
}

// waitForTapdAddressReceive polls until a tapd lists one completed transfer
// to an address it created.
func waitForTapdAddressReceive(t *testing.T, ctx context.Context,
	client tapsdk.Client, addr *tapsdk.Address,
	timeout time.Duration) *tapsdk.AddressEvent {

	t.Helper()

	var (
		event      *tapsdk.AddressEvent
		lastStatus string
	)
	require.Eventuallyf(t, func() bool {
		events, err := client.AddrReceives(
			ctx, &tapsdk.AddressReceivesQuery{
				FilterAddr: addr.Encoded,
			},
		)
		switch {
		case err != nil:
			lastStatus = fmt.Sprintf("receive lookup failed: %v",
				err)

			return false

		case len(events) != 1:
			lastStatus = fmt.Sprintf("%d receives listed",
				len(events))

			return false
		}
		event = events[0]
		lastStatus = fmt.Sprintf("status = %d", event.Status)

		return event.Status == tapsdk.AddressEventStatusCompleted
	}, timeout, time.Second,
		"tapd never listed a completed receive at address %s; last "+
			"observation: %s", addr.Encoded, observed{&lastStatus},
	)

	return event
}
