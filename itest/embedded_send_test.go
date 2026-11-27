//go:build itest

package itest

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/embedded"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/tapsend"
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

	txid, funded := sendFromEmbedded(
		t, ctx, h, node, lnd, addr.Encoded, sent,
	)
	require.Len(t, funded.FundedPsbts, issuances)
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
// address and returns the ID of the anchor transaction and the funded
// transfer. The engine funds and signs the virtual transactions and signs its
// asset anchors. An output of lnd-alice pays the fee and collects the Bitcoin
// change.
func sendFromEmbedded(t *testing.T, ctx context.Context, h *TestHarness,
	node *EmbeddedNode, lnd *lndWalletKit, addr string,
	amount uint64) (chainhash.Hash, *tapsdk.FundedTransfer) {

	t.Helper()

	funded, err := node.Client.FundTransfer(
		ctx, []tapsdk.Recipient{
			tapsdk.RecipientWithAmount(addr, amount),
		}, nil,
	)
	require.NoError(t, err)
	require.Empty(t, funded.PassiveAssetPsbts)

	signed := make([][]byte, len(funded.FundedPsbts))
	packets := make([]*tappsbt.VPacket, len(funded.FundedPsbts))
	for idx, fundedPsbt := range funded.FundedPsbts {
		signed[idx], err = node.Client.SignVirtualPsbt(ctx, fundedPsbt)
		require.NoError(t, err)
		packets[idx], err = tappsbt.Decode(signed[idx])
		require.NoError(t, err)
	}

	// The template spends the engine's asset anchors into one output per
	// asset anchor output of the transfer.
	anchor, err := tapsend.PrepareAnchoringTemplate(packets)
	require.NoError(t, err)

	funding := h.fundLndInput(t, ctx, lnd)
	fundingIndex := len(anchor.Inputs)
	anchor.UnsignedTx.AddTxIn(wire.NewTxIn(&funding.outpoint, nil, nil))
	anchor.Inputs = append(anchor.Inputs, funding.psbtInput())

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
	anchor.Outputs = append(anchor.Outputs, funding.psbtChangeOutput())

	var template bytes.Buffer
	require.NoError(t, anchor.Serialize(&template))

	committed, err := node.Client.CommitVirtualPsbtsWithRequest(
		ctx, &tapsdk.CommitVirtualPsbtsRequest{
			AnchorPsbt:   template.Bytes(),
			VirtualPsbts: signed,
			Funding: tapsdk.AnchorFundingPlan{
				SkipFunding: true,
			},
		},
	)
	require.NoError(t, err)

	signedPsbt, err := lnd.signPsbt(ctx, committed.AnchorPsbt)
	require.NoError(t, err)
	finalPsbt := customAnchorFinalizeKeyPath(t, signedPsbt, fundingIndex)
	txid := decodeCustomAnchorITestPSBT(t, finalPsbt).UnsignedTx.TxHash()

	_, err = node.Client.PublishAndLogTransferWithRequest(
		ctx, &tapsdk.PublishAndLogTransferRequest{
			AnchorPsbt:        finalPsbt,
			VirtualPsbts:      committed.VirtualPsbts,
			ChangeOutputIndex: -1,
			SignAnchorInputs:  true,
			Label:             uniqueEventLabel("embedded-send"),
		},
	)
	require.NoError(t, err)
	node.WaitForTransferState(t, ctx, txid, embedded.TransferPending)

	return txid, funded
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
