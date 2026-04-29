//go:build itest

package itest

import (
	"fmt"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestTxBuilderEndToEnd drives the explicit Fund -> Sign -> Commit -> Finish
// pipeline against a real tapd so the lower-level builder stays mapped to the
// daemon's wallet RPCs.
func TestTxBuilderEndToEnd(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("builder-token-%s", transport))
		minted, err := h.MintGroupedAsset(t, ctx, name, 5000)
		require.NoError(t, err)

		addr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)

		const amount = uint64(125)
		builder := h.AliceWallet.NewTxBuilder().
			AddRecipient(addr.Encoded, amount).
			SetFeeRate(2).
			SetAnchorSigner(newAliceLndAnchorSigner(t))

		funded, err := builder.Fund(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, funded.FundedPsbt)

		signed, err := builder.Sign(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, signed)

		committed, err := builder.Commit(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, committed.AnchorPsbt)
		require.NotEmpty(t, committed.VirtualPsbts)

		packet, err := builder.Finish(ctx, false)
		require.NoError(t, err)
		require.NotEmpty(t, packet.AnchorTransaction)
		require.NotEmpty(t, packet.VirtualTransactions)

		_, err = builder.Finish(ctx, false)
		require.ErrorIs(t, err, tapsdk.ErrBuilderFinished)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		bobBalance := h.WaitForBalance(
			t, ctx, h.BobWallet, minted.Ref, amount,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, amount, bobBalance)
	})
}

// TestInteractiveTxBuilderEndToEnd covers the marquee interactive flow:
// receiver derives keys, sender builds with those keys, sender returns the
// proof, and the receiver imports it into their wallet.
func TestInteractiveTxBuilderEndToEnd(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(
			fmt.Sprintf("interactive-token-%s", transport),
		)
		minted, err := h.MintGroupedAsset(t, ctx, name, 1000)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsGroupRef())

		receiverKeys, err := h.BobWallet.DeriveKeys(ctx)
		require.NoError(t, err)
		require.NotNil(t, receiverKeys)

		const amount = uint64(75)
		transfer, err := h.AliceWallet.NewInteractiveTxBuilder().
			SetAsset(minted.Ref, amount).
			SetReceiverKeys(*receiverKeys).
			Execute(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, transfer.AnchorTxid)

		output := transferOutputForScriptKey(
			t, transfer, receiverKeys.ScriptKey.PubKey,
		)
		require.NotZero(t, output.IssuanceID)

		h.MineBlocks(t, defaultMineBlocks)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)
		h.WaitForSync(t, ctx, h.BobClient, defaultSyncTimeout)

		proof, err := h.AliceWallet.ExportProof(
			ctx, entities.AssetRefFromAssetID(output.IssuanceID),
			output.ScriptKey, &output.AnchorOutpoint,
		)
		require.NoError(t, err)
		require.NotEmpty(t, proof.RawProofFile)

		registered, err := h.BobWallet.ImportProof(ctx, proof)
		require.NoError(t, err)
		require.NotNil(t, registered)
		require.Equal(t, receiverKeys.ScriptKey.PubKey,
			registered.ScriptKey)

		bobBalance := h.WaitForBalance(
			t, ctx, h.BobWallet, minted.Ref, amount,
			balanceTimeoutFor(minted.Ref),
		)
		require.Equal(t, amount, bobBalance)
	})
}

func transferOutputForScriptKey(t testing.TB,
	transfer *entities.AssetTransfer,
	scriptKey entities.PubKey) entities.TransferOutput {

	t.Helper()

	for _, output := range transfer.Outputs {
		if output.ScriptKey == scriptKey {
			return output
		}
	}

	require.FailNow(t, "receiver output not found")
	return entities.TransferOutput{}
}
