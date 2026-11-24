//go:build itest

package itest

import (
	"context"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/embedded"
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

	// The engine signs for the script key Alice derived from the
	// address, and for the anchor output under the address's internal
	// key.
	aliceKeys, err := h.AliceWallet.DeriveKeys(ctx)
	require.NoError(t, err)
	returned := commitEmbeddedTransfer(
		t, ctx, h, node, lnd, minted.Ref, sentProof,
		embeddedITestReceived, aliceKeys, embeddedITestReceived,
	)
	publishEmbeddedTransfer(t, ctx, node, returned)

	h.MineBlocks(t, defaultMineBlocks)
	node.WaitForTransferState(
		t, ctx, returned.txid, embedded.TransferFinal,
	)
	h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

	balance, err = node.Wallet.GetBalance(ctx, minted.Ref)
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
