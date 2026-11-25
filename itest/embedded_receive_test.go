//go:build itest

package itest

import (
	"context"
	"testing"

	"github.com/btcsuite/btcd/chainhash/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/embedded"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedAddressReceive pays addresses of an embedded engine from
// Alice's tapd. The engine learns of each transfer from the auth mailbox of
// the address's proof courier and fetches its proofs from the courier's
// universe. It then returns the units to Alice with a custom-anchor transfer,
// which her tapd verifies and imports. lnd-alice pays for that transfer.
func TestEmbeddedAddressReceive(t *testing.T) {
	h, ctx := newFundedHarness(t)
	lnd := newAliceLndWalletKit(t)

	// A courier takes the proofs of a sender only once its universe
	// allows inserts.
	h.EnableUniverseBootstrap(t, ctx)

	tests := []struct {
		name    string
		grouped bool

		// courier is the proof courier the address names. Empty selects
		// the engine's default, Bob's tapd.
		courier string

		// live holds the engine's mailboxes open. Otherwise the test
		// asks the engine for one pass at a time.
		live bool
	}{{
		name: "ungrouped",
	}, {
		name:    "grouped",
		grouped: true,
		live:    true,
	}, {
		// The sender's tapd holds the mailbox and the universe.
		name:    "sender as courier",
		grouped: true,
		courier: "authmailbox+universerpc://" +
			defaultAliceUniverseHost,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				minted *MintResult
				err    error
				name   = uniqueEventLabel("embedded-receive")
			)
			if tc.grouped {
				minted, err = h.CreateFungibleAndConfirm(
					t, ctx, name, embeddedITestSupply,
				)
			} else {
				minted, err = h.MintAssetAndConfirm(
					t, ctx, &tapsdk.MintAsset{
						AssetType:     tapsdk.AssetTypeFungible,
						Name:          name,
						InitialSupply: embeddedITestSupply,
					},
				)
			}
			require.NoError(t, err)
			require.Equal(t, tc.grouped, minted.Ref.IsGroupRef())

			// The engine is told of no group: its address is what
			// makes it accept the group's proofs.
			node := h.NewEmbeddedNode(t)
			if tc.live {
				require.NoError(t, node.Client.ResumeReceives(ctx))
			}
			testEmbeddedAddressReceive(
				t, ctx, h, node, lnd, minted, tc.courier, !tc.live,
			)
		})
	}
}

func testEmbeddedAddressReceive(t *testing.T, ctx context.Context,
	h *TestHarness, node *EmbeddedNode, lnd *lndWalletKit,
	minted *MintResult, courier string, sync bool) {

	addr, err := node.Client.NewAddr(ctx, &tapsdk.NewAddressRequest{
		AssetRef:         minted.Ref,
		ProofCourierAddr: courier,
	})
	require.NoError(t, err)
	if courier != "" {
		require.Equal(t, courier, addr.ProofCourierAddr)
	}

	sent, err := h.AliceWallet.Send(
		ctx, addr.Encoded, tapsdk.WithAmount(embeddedITestReceived),
	)
	require.NoError(t, err)

	var output *tapsdk.TransferOutput
	for idx := range sent.Outputs {
		if sent.Outputs[idx].Amount == embeddedITestReceived {
			output = &sent.Outputs[idx]
		}
	}
	require.NotNil(t, output, "transfer has no output for the address")

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	event := node.WaitForAddressReceive(
		t, ctx, addr, sync, balanceTimeoutFor(minted.Ref),
	)
	require.Equal(t, addr, event.Address)
	require.Equal(t, output.AnchorOutpoint.String(), event.Outpoint)
	require.EqualValues(t, output.AnchorValue, event.UTXOAmountSat)
	require.NotZero(t, event.ConfirmationHeight)
	require.True(t, event.HasProof)

	balance, err := node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Equal(t, embeddedITestReceived, balance)

	// The engine assembled the file its sender holds, one proof at a
	// time, and reads it as the sender's tapd does.
	sentProof := h.WaitForProofFile(
		t, ctx, h.AliceWallet,
		tapsdk.AssetRefFromAssetID(output.IssuanceID),
		output.ScriptKey, &output.AnchorOutpoint,
	)
	requireEmbeddedProofFile(
		t, ctx, node, output.IssuanceID, output.ScriptKey,
		output.AnchorOutpoint, sentProof.RawProofFile,
	)
	requireSameProofAnswers(
		t, ctx, h.AliceClient, node.Client, sentProof.RawProofFile,
	)

	returnEmbeddedReceive(t, ctx, h, node, lnd, minted, sentProof)
}

// returnEmbeddedReceive sends the units the engine received at an address
// back to Alice, whose tapd verifies and imports the transfer. receivedProof
// is the proof file of the received output.
func returnEmbeddedReceive(t *testing.T, ctx context.Context, h *TestHarness,
	node *EmbeddedNode, lnd *lndWalletKit, minted *MintResult,
	receivedProof *tapsdk.ProofFile) {

	t.Helper()

	// The engine signs for the script key Alice derived from the
	// address, and for the anchor output under the address's internal
	// key.
	aliceKeys, err := h.AliceWallet.DeriveKeys(ctx)
	require.NoError(t, err)
	returned := commitEmbeddedTransfer(
		t, ctx, h, node, lnd, minted.Ref, receivedProof,
		embeddedITestReceived, aliceKeys, embeddedITestReceived,
	)
	publishEmbeddedTransfer(t, ctx, node, returned)

	h.MineBlocks(t, defaultMineBlocks)
	node.WaitForTransferState(
		t, ctx, returned.txid, embedded.TransferFinal,
	)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	balance, err := node.Wallet.GetBalance(ctx, minted.Ref)
	require.NoError(t, err)
	require.Zero(t, balance)

	requireTapdImportsEmbeddedOutput(
		t, ctx, h.AliceWallet, h.AliceClient, node, returned.recipient,
		embeddedITestReceived,
	)
	h.WaitForBalance(
		t, ctx, h.AliceWallet, minted.Ref, embeddedITestSupply,
		balanceTimeoutFor(minted.Ref),
	)
}

// TestEmbeddedReceiveReorg takes a transfer to an address of an embedded
// engine through a re-org of its confirming block: the received units leave
// the wallet, and return with a proof of the block that confirms the transfer
// again. The engine then returns the units to Alice, whose tapd verifies the
// proof of the new block.
func TestEmbeddedReceiveReorg(t *testing.T) {
	h, ctx := newFundedHarness(t)
	lnd := newAliceLndWalletKit(t)
	h.EnableUniverseBootstrap(t, ctx)

	minted, err := h.CreateFungibleAndConfirm(
		t, ctx, uniqueEventLabel("embedded-receive-reorg"),
		embeddedITestSupply,
	)
	require.NoError(t, err)

	node := h.NewEmbeddedNode(t)
	require.NoError(t, node.Client.ResumeReceives(ctx))
	addr, err := node.Client.NewAddr(ctx, &tapsdk.NewAddressRequest{
		AssetRef: minted.Ref,
	})
	require.NoError(t, err)

	sent, err := h.AliceWallet.Send(
		ctx, addr.Encoded, tapsdk.WithAmount(embeddedITestReceived),
	)
	require.NoError(t, err)

	var output *tapsdk.TransferOutput
	for idx := range sent.Outputs {
		if sent.Outputs[idx].Amount == embeddedITestReceived {
			output = &sent.Outputs[idx]
		}
	}
	require.NotNil(t, output, "transfer has no output for the address")

	// One block confirms the transfer, so the block the engine receives
	// it in is the best one.
	h.MineBlocks(t, 1)
	timeout := balanceTimeoutFor(minted.Ref)
	node.WaitForAddressReceive(t, ctx, addr, false, timeout)
	first := node.WaitForReceiveState(
		t, ctx, output.AnchorOutpoint, embedded.ReceiveConfirmed,
	)
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, embeddedITestReceived,
		defaultWaitTimeout,
	)

	// The confirming block leaves the chain, and the anchor transaction
	// returns to the mempool.
	require.Equal(t, first.BlockHash, h.invalidateBestBlock(t))
	node.WaitForAddressReceiveStatus(
		t, ctx, addr, false,
		tapsdk.AddressEventStatusTransactionDetected, timeout,
	)
	held, err := node.Wallet.ListAssets(ctx, &tapsdk.ListAssetsRequest{})
	require.NoError(t, err)
	require.Empty(t, held)

	h.MineBlocks(t, defaultMineBlocks)
	final := node.WaitForReceiveState(
		t, ctx, output.AnchorOutpoint, embedded.ReceiveFinal,
	)
	require.NotEqual(t, first.BlockHash, final.BlockHash)
	event := node.WaitForAddressReceive(t, ctx, addr, false, timeout)
	require.Equal(t, final.BlockHeight, event.ConfirmationHeight)
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, embeddedITestReceived,
		defaultWaitTimeout,
	)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	// The proof commits to the new block, which tapd checks against its
	// own chain backend.
	received, err := node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(output.IssuanceID),
		output.ScriptKey, &output.AnchorOutpoint,
	)
	require.NoError(t, err)
	requireSameProofAnswers(
		t, ctx, h.AliceClient, node.Client, received.RawProofFile,
	)

	returnEmbeddedReceive(t, ctx, h, node, lnd, minted, received)
}

// TestEmbeddedReceiveReorgEarlierProof re-organizes the blocks of two
// transfers to addresses of an embedded engine, the second of which spends
// the change of the first. The proof file of the second transfer returns with
// the new block in the proofs of both transfers, and Alice's tapd verifies it
// against its own chain backend.
func TestEmbeddedReceiveReorgEarlierProof(t *testing.T) {
	h, ctx := newFundedHarness(t)
	h.EnableUniverseBootstrap(t, ctx)

	minted, err := h.CreateFungibleAndConfirm(
		t, ctx, uniqueEventLabel("embedded-receive-reorg-earlier"),
		embeddedITestSupply,
	)
	require.NoError(t, err)

	node := h.NewEmbeddedNode(t)
	require.NoError(t, node.Client.ResumeReceives(ctx))
	timeout := balanceTimeoutFor(minted.Ref)

	// received is a transfer of Alice to an address of the engine.
	type received struct {
		addr   *tapsdk.Address
		output tapsdk.TransferOutput
		record *embedded.ReceiveRecord
	}
	const amount = embeddedITestReceived / 2

	// One block confirms each transfer, so the two blocks the engine
	// receives them in are the best ones.
	pay := func() *received {
		addr, err := node.Client.NewAddr(ctx, &tapsdk.NewAddressRequest{
			AssetRef: minted.Ref,
		})
		require.NoError(t, err)

		sent, err := h.AliceWallet.Send(
			ctx, addr.Encoded, tapsdk.WithAmount(amount),
		)
		require.NoError(t, err)

		var output *tapsdk.TransferOutput
		for idx := range sent.Outputs {
			if sent.Outputs[idx].Amount == amount {
				output = &sent.Outputs[idx]
			}
		}
		require.NotNil(t, output, "transfer has no output for the "+
			"address")

		h.MineBlocks(t, 1)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		node.WaitForAddressReceive(t, ctx, addr, false, timeout)

		return &received{
			addr:   addr,
			output: *output,
			record: node.WaitForReceiveState(
				t, ctx, output.AnchorOutpoint,
				embedded.ReceiveConfirmed,
			),
		}
	}
	first := pay()
	second := pay()
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, 2*amount, defaultWaitTimeout,
	)

	// Both confirming blocks leave the chain, and the anchor transactions
	// return to the mempool.
	require.Equal(t, second.record.BlockHash, h.invalidateBestBlock(t))
	require.Equal(t, first.record.BlockHash, h.invalidateBestBlock(t))
	for _, transfer := range []*received{first, second} {
		node.WaitForAddressReceiveStatus(
			t, ctx, transfer.addr, false,
			tapsdk.AddressEventStatusTransactionDetected, timeout,
		)
	}
	held, err := node.Wallet.ListAssets(ctx, &tapsdk.ListAssetsRequest{})
	require.NoError(t, err)
	require.Empty(t, held)

	h.MineBlocks(t, defaultMineBlocks)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
	for _, transfer := range []*received{first, second} {
		final := node.WaitForReceiveState(
			t, ctx, transfer.output.AnchorOutpoint,
			embedded.ReceiveFinal,
		)
		require.NotEqual(t, transfer.record.BlockHash, final.BlockHash)
		transfer.record = final
	}
	h.WaitForBalance(
		t, ctx, node.Wallet, minted.Ref, 2*amount, defaultWaitTimeout,
	)

	// The file of the second transfer ends in the proof of Alice's change
	// of the first transfer and the proof of the second transfer.
	exported, err := node.Wallet.ExportProofFile(
		ctx, tapsdk.AssetRefFromAssetID(second.output.IssuanceID),
		second.output.ScriptKey, &second.output.AnchorOutpoint,
	)
	require.NoError(t, err)
	file, err := proof.DecodeFile(exported.RawProofFile)
	require.NoError(t, err)
	require.GreaterOrEqual(t, file.NumProofs(), 3)

	for depth, transfer := range []*received{second, first} {
		p, err := file.ProofAt(uint32(file.NumProofs() - 1 - depth))
		require.NoError(t, err)
		require.Equal(
			t, chainhash.Hash(transfer.output.AnchorOutpoint.Txid),
			p.AnchorTx.TxHash(),
		)
		require.Equal(
			t, transfer.record.BlockHash, p.BlockHeader.BlockHash(),
		)
		require.Equal(t, transfer.record.BlockHeight, p.BlockHeight)
	}

	for _, transfer := range []*received{first, second} {
		exported, err := node.Wallet.ExportProofFile(
			ctx,
			tapsdk.AssetRefFromAssetID(transfer.output.IssuanceID),
			transfer.output.ScriptKey,
			&transfer.output.AnchorOutpoint,
		)
		require.NoError(t, err)
		requireSameProofAnswers(
			t, ctx, h.AliceClient, node.Client,
			exported.RawProofFile,
		)
	}
}
