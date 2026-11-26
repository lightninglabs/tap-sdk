package embedded

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"slices"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/tapsend"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newSendHarness returns two wallets on one chain that create addresses and
// pay addresses, with one proof courier.
func newSendHarness(t *testing.T) (*testHarness, *testHarness) {
	t.Helper()

	recipient := newAddressHarness(t, testCourier)
	recipient.courier.chain = recipient.chain
	recipient.sending = &SendConfig{Dial: recipient.courier.dial}
	recipient.restart()

	sender := recipient.peer(2)
	sender.courier = recipient.courier
	sender.addresses = &ReceiveConfig{
		Store:          sender.store,
		Keys:           NewKeyRingReceiveKeys(sender.keys),
		Dial:           sender.courier.dial,
		DefaultCourier: recipient.addresses.DefaultCourier,
	}
	sender.sending = &SendConfig{Dial: sender.courier.dial}
	sender.restart()

	return sender, recipient
}

// fundSend funds a transfer of amount units to an address and returns its
// active and passive packets decoded.
func (h *testHarness) fundSend(addr string, amount uint64) (
	*tapsdk.FundedTransfer, []*tappsbt.VPacket, []*tappsbt.VPacket) {

	h.t.Helper()

	funded, err := h.client.FundTransfer(
		h.t.Context(), []tapsdk.Recipient{
			tapsdk.RecipientWithAmount(addr, amount),
		}, nil,
	)
	require.NoError(h.t, err)

	active, err := decodeVirtualPackets(funded.FundedPsbts)
	require.NoError(h.t, err)
	passive, err := decodeVirtualPackets(funded.PassiveAssetPsbts)
	require.NoError(h.t, err)

	return funded, active, passive
}

// fundSendOne is fundSend for a transfer that spends one asset issuance.
func (h *testHarness) fundSendOne(addr string, amount uint64) (
	*tapsdk.FundedTransfer, *tappsbt.VPacket, []*tappsbt.VPacket) {

	h.t.Helper()

	funded, active, passive := h.fundSend(addr, amount)
	require.Len(h.t, active, 1)

	return funded, active[0], passive
}

// committedSend is a funded transfer, signed and committed to an anchor
// transaction that is ready to be published.
type committedSend struct {
	request *tapsdk.PublishAndLogTransferRequest
	txid    chainhash.Hash
}

// commitSend signs the packets of a funded transfer and commits them to an
// anchor transaction that a coin of the caller pays for.
func (h *testHarness) commitSend(funded *tapsdk.FundedTransfer,
	seed byte) *committedSend {

	h.t.Helper()
	ctx := h.t.Context()

	sign := func(unsigned [][]byte) [][]byte {
		signed := make([][]byte, len(unsigned))
		for idx := range unsigned {
			var err error
			signed[idx], err = h.client.SignVirtualPsbt(
				ctx, unsigned[idx],
			)
			require.NoError(h.t, err)
		}

		return signed
	}
	signedActive := sign(funded.FundedPsbts)
	signedPassive := sign(funded.PassiveAssetPsbts)

	packets, err := decodeVirtualPackets(
		append(slices.Clone(signedActive), signedPassive...),
	)
	require.NoError(h.t, err)
	anchor, err := tapsend.PrepareAnchoringTemplate(packets)
	require.NoError(h.t, err)

	funding := newFundingInput(h.t, seed, 100_000)
	anchor.UnsignedTx.AddTxIn(wire.NewTxIn(&funding.outpoint, nil, nil))
	anchor.Inputs = append(anchor.Inputs, psbt.PInput{
		WitnessUtxo: funding.txOut,
	})
	var template bytes.Buffer
	require.NoError(h.t, anchor.Serialize(&template))

	committed, err := h.client.CommitVirtualPsbtsWithRequest(
		ctx, &tapsdk.CommitVirtualPsbtsRequest{
			AnchorPsbt:        template.Bytes(),
			VirtualPsbts:      signedActive,
			PassiveAssetPsbts: signedPassive,
			Funding: tapsdk.AnchorFundingPlan{
				SkipFunding: true,
			},
		},
	)
	require.NoError(h.t, err)

	final, err := psbt.NewFromRawBytes(
		bytes.NewReader(committed.AnchorPsbt), false,
	)
	require.NoError(h.t, err)
	funding.sign(h.t, final)
	var signedAnchor bytes.Buffer
	require.NoError(h.t, final.Serialize(&signedAnchor))

	return &committedSend{
		request: &tapsdk.PublishAndLogTransferRequest{
			AnchorPsbt:        signedAnchor.Bytes(),
			VirtualPsbts:      committed.VirtualPsbts,
			PassiveAssetPsbts: committed.PassiveAssetPsbts,
			ChangeOutputIndex: -1,
			SignAnchorInputs:  true,
			Label:             "test-send",
		},
		txid: final.UnsignedTx.TxHash(),
	}
}

// requireWalletKey checks that the wallet derived a key.
func (h *testHarness) requireWalletKey(key *btcec.PublicKey) {
	h.t.Helper()

	_, err := h.store.FetchInternalKey(
		h.t.Context(), asset.ToSerialized(key),
	)
	require.NoError(h.t, err)
}

// altLeafKey returns the script key of the first alt leaf of an output.
func altLeafKey(vOut *tappsbt.VOutput) asset.SerializedKey {
	leaves := asset.FromAltLeaves(vOut.AltLeaves)

	return asset.ToSerialized(leaves[0].ScriptKey.PubKey)
}

// TestFundAddressSend funds a transfer to an address of an asset and of a
// group: the recipient is paid at the script key derived for the asset, in an
// anchor output under the internal key of the address, and the surplus
// returns to keys of the wallet.
func TestFundAddressSend(t *testing.T) {
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

			minted, addr := recipient.newReceivingAddress(
				sender, "fund-"+tc.name, tc.groupPriv,
			)
			tapAddr, err := address.DecodeAddress(
				addr.Encoded, &address.RegressionNetTap,
			)
			require.NoError(t, err)

			funded, active, passive := sender.fundSendOne(
				addr.Encoded, 300,
			)
			require.Empty(t, passive)

			require.Len(t, active.Inputs, 1)
			input := active.Inputs[0]
			require.Equal(t, minted.outpoint, input.PrevID.OutPoint)
			require.Equal(t, minted.assetID, input.PrevID.ID)
			sender.requireWalletKey(input.Anchor.InternalKey)

			require.Len(t, active.Outputs, 2)
			paid, change := active.Outputs[0], active.Outputs[1]
			if paid.Type == tappsbt.TypeSplitRoot {
				paid, change = change, paid
			}

			scriptKey, err := asset.DeriveUniqueScriptKey(
				tapAddr.ScriptKey, minted.assetID,
				asset.ScriptKeyDerivationUniquePedersen,
			)
			require.NoError(t, err)
			require.EqualValues(t, 300, paid.Amount)
			require.EqualValues(t, 300, paid.Asset.Amount)
			require.True(t, paid.ScriptKey.PubKey.IsEqual(
				scriptKey.PubKey,
			))
			require.True(t, paid.AnchorOutputInternalKey.IsEqual(
				&tapAddr.InternalKey,
			))
			require.Zero(t, paid.AnchorOutputIndex)
			require.Equal(
				t, tapAddr.AssetVersion, paid.AssetVersion,
			)
			require.Len(t, paid.AltLeaves, 1)
			require.NotNil(t, paid.Address)
			require.True(t, paid.Address.ScriptKey.IsEqual(
				&tapAddr.ScriptKey,
			))
			courier := paid.ProofDeliveryAddress.String()
			require.Equal(t, testCourier, courier)

			require.Equal(t, tappsbt.TypeSplitRoot, change.Type)
			require.EqualValues(t, receivedUnits-300, change.Amount)
			require.EqualValues(t, 1, change.AnchorOutputIndex)
			require.Equal(t, asset.V1, change.AssetVersion)
			require.Empty(t, change.AltLeaves)
			require.Nil(t, change.Address)
			sender.requireWalletKey(change.AnchorOutputInternalKey)
			changeKey, err := sender.store.FetchScriptKey(
				ctx, scriptKeyBytes(change.ScriptKey.PubKey),
			)
			require.NoError(t, err)
			require.EqualValues(
				t, asset.ScriptKeyBip86, changeKey.Type,
			)

			// Two payments of the same amount to one address
			// differ in their alt leaf, and so in their anchor
			// output.
			_, again, _ := sender.fundSendOne(addr.Encoded, 300)
			for _, vOut := range again.Outputs {
				if vOut.Address == nil {
					continue
				}
				require.NotEqual(
					t, altLeafKey(paid), altLeafKey(vOut),
				)
			}

			sender.commitSend(funded, 1)
		})
	}
}

// TestFundAddressSendFullValue funds a transfer of every unit of an output,
// which has no change.
func TestFundAddressSendFullValue(t *testing.T) {
	sender, recipient := newSendHarness(t)

	_, addr := recipient.newReceivingAddress(sender, "full", nil)
	funded, active, passive := sender.fundSendOne(
		addr.Encoded, receivedUnits,
	)
	require.Empty(t, passive)

	require.Len(t, active.Outputs, 1)
	require.EqualValues(t, receivedUnits, active.Outputs[0].Amount)
	require.NotNil(t, active.Outputs[0].Address)
	require.Len(t, active.Outputs[0].AltLeaves, 1)

	sender.commitSend(funded, 1)
}

// TestFundAddressSendInputs restricts the funding of a transfer to the
// outputs the request names.
func TestFundAddressSendInputs(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	minted, addr := recipient.newReceivingAddress(sender, "inputs", nil)
	held, err := sender.client.ListUtxos(ctx, &tapsdk.ListUtxosRequest{})
	require.NoError(t, err)
	require.Len(t, held, 1)

	var scriptKey tapsdk.PubKey
	for _, utxo := range held {
		scriptKey = utxo.Assets[0].ScriptKey.PubKey
	}
	recipients := []tapsdk.Recipient{
		tapsdk.RecipientWithAmount(addr.Encoded, 10),
	}

	funded, err := sender.client.FundTransfer(
		ctx, recipients, []tapsdk.PrevID{{
			Outpoint: tapsdk.Outpoint{
				Txid:  minted.outpoint.Hash,
				Index: minted.outpoint.Index,
			},
			IssuanceID: tapsdk.AssetID(minted.assetID),
			ScriptKey:  scriptKey,
		}},
	)
	require.NoError(t, err)
	require.Len(t, funded.FundedPsbts, 1)
	active, err := tappsbt.Decode(funded.FundedPsbts[0])
	require.NoError(t, err)
	require.Equal(t, minted.outpoint, active.Inputs[0].PrevID.OutPoint)

	// An output the wallet does not hold pays for nothing.
	_, err = sender.client.FundTransfer(
		ctx, recipients, []tapsdk.PrevID{{
			Outpoint: tapsdk.Outpoint{
				Txid:  minted.outpoint.Hash,
				Index: minted.outpoint.Index + 1,
			},
			IssuanceID: tapsdk.AssetID(minted.assetID),
			ScriptKey:  scriptKey,
		}},
	)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestFundAddressSendRejects refuses the transfers the engine cannot fund.
func TestFundAddressSendRejects(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	minted, addr := recipient.newReceivingAddress(sender, "rejects", nil)
	_, otherAddr := recipient.newReceivingAddress(sender, "other", nil)

	courier, err := url.Parse(testCourier)
	require.NoError(t, err)
	plainCourier, err := url.Parse("universerpc://courier.test:10029")
	require.NoError(t, err)

	// encode returns an address of the sender's asset that tapd's decoder
	// reads.
	encode := func(change func(*address.Tap)) string {
		tapAddr := &address.Tap{
			Version:          address.V2,
			ChainParams:      &address.RegressionNetTap,
			AssetVersion:     asset.V0,
			AssetID:          minted.assetID,
			ScriptKey:        *recipient.addressKey(0),
			InternalKey:      *recipient.addressKey(1),
			ProofCourierAddr: *courier,
		}
		change(tapAddr)

		encoded, err := tapAddr.EncodeAddress()
		require.NoError(t, err)

		return encoded
	}

	tests := []struct {
		name       string
		recipients []tapsdk.Recipient
		code       codes.Code
		errText    string
	}{{
		name: "more units than the wallet holds",
		recipients: []tapsdk.Recipient{tapsdk.RecipientWithAmount(
			addr.Encoded, receivedUnits+1,
		)},
		code:    codes.FailedPrecondition,
		errText: "insufficient asset balance",
	}, {
		name: "an asset the wallet does not hold",
		recipients: []tapsdk.Recipient{tapsdk.RecipientWithAmount(
			encode(func(tapAddr *address.Tap) {
				tapAddr.AssetID = asset.ID{7}
			}), 1,
		)},
		code:    codes.FailedPrecondition,
		errText: "insufficient asset balance",
	}, {
		name: "version 1 address",
		recipients: []tapsdk.Recipient{tapsdk.RecipientWithAmount(
			encode(func(tapAddr *address.Tap) {
				tapAddr.Version = address.V1
				tapAddr.Amount = 5
				tapAddr.ProofCourierAddr = *plainCourier
			}), 5,
		)},
		code:    codes.InvalidArgument,
		errText: "version 2 addresses only",
	}, {
		name: "courier without a mailbox",
		recipients: []tapsdk.Recipient{tapsdk.RecipientWithAmount(
			encode(func(tapAddr *address.Tap) {
				tapAddr.ProofCourierAddr = *plainCourier
			}), 5,
		)},
		code:    codes.InvalidArgument,
		errText: "proof courier type",
	}, {
		name: "another amount than the address names",
		recipients: []tapsdk.Recipient{tapsdk.RecipientWithAmount(
			encode(func(tapAddr *address.Tap) {
				tapAddr.Amount = 5
			}), 6,
		)},
		code:    codes.InvalidArgument,
		errText: "cannot override amount",
	}, {
		name: "address of another network",
		recipients: []tapsdk.Recipient{tapsdk.RecipientWithAmount(
			encode(func(tapAddr *address.Tap) {
				tapAddr.ChainParams = &address.TestNet3Tap
			}), 5,
		)},
		code: codes.InvalidArgument,
	}, {
		name: "addresses of two assets",
		recipients: []tapsdk.Recipient{
			tapsdk.RecipientWithAmount(addr.Encoded, 5),
			tapsdk.RecipientWithAmount(otherAddr.Encoded, 5),
		},
		code:    codes.InvalidArgument,
		errText: "another asset",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sender.client.FundTransfer(
				ctx, tc.recipients, nil,
			)
			require.Equal(t, tc.code, status.Code(err), "%v", err)
			require.ErrorContains(t, err, tc.errText)
		})
	}

	// The wallet funds no transfer from a packet template.
	_, err = sender.client.FundInteractivePsbt(ctx, []byte{1})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

// TestFundAddressSendTwoRecipients pays two addresses of one asset in one
// transfer, each in an anchor output of its own.
func TestFundAddressSendTwoRecipients(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	minted, first := recipient.newReceivingAddress(sender, "two", nil)
	second, err := recipient.wallet.NewReceiveAddress(ctx, minted.ref)
	require.NoError(t, err)
	require.NotEqual(t, first.Encoded, second.Encoded)

	funded, err := sender.client.FundTransfer(
		ctx, []tapsdk.Recipient{
			tapsdk.RecipientWithAmount(first.Encoded, 100),
			tapsdk.RecipientWithAmount(second.Encoded, 200),
		}, nil,
	)
	require.NoError(t, err)
	require.Len(t, funded.FundedPsbts, 1)
	active, err := tappsbt.Decode(funded.FundedPsbts[0])
	require.NoError(t, err)

	amounts := make(map[uint32]uint64)
	for _, vOut := range active.Outputs {
		amounts[vOut.AnchorOutputIndex] = vOut.Amount
	}
	require.Equal(t, map[uint32]uint64{
		0: 100,
		1: 200,
		2: receivedUnits - 300,
	}, amounts)

	sender.commitSend(funded, 1)
}

// receivePair issues two assets to the wallet in one anchor output, and
// imports their proofs.
func (h *testHarness) receivePair(first, second string) [2]*mintedAsset {
	h.t.Helper()
	ctx := h.t.Context()

	internalKey, err := h.client.DeriveInternalKey(ctx)
	require.NoError(h.t, err)
	anchorKey, err := btcec.ParsePubKey(internalKey.PubKey[:])
	require.NoError(h.t, err)

	// The assets of one issuance share its first input.
	genesisPoint := wire.OutPoint{
		Hash: sha256.Sum256([]byte("genesis-" + first)),
	}
	var (
		tags   = [2]string{first, second}
		assets [2]*asset.Asset
	)
	for idx, tag := range tags {
		scriptKey, err := h.client.DeriveScriptKey(ctx)
		require.NoError(h.t, err)
		scriptPubKey, err := btcec.ParsePubKey(scriptKey.PubKey[:])
		require.NoError(h.t, err)

		assets[idx], err = asset.New(asset.Genesis{
			FirstPrevOut: genesisPoint,
			Tag:          tag,
			Type:         asset.Normal,
		}, receivedUnits, 0, 0, asset.NewScriptKey(scriptPubKey), nil)
		require.NoError(h.t, err)
	}

	version := commitment.TapCommitmentV2
	tapCommitment, err := commitment.FromAssets(
		&version, assets[0], assets[1],
	)
	require.NoError(h.t, err)

	tapscriptRoot := tapCommitment.TapscriptRoot(nil)
	pkScript, err := txscript.PayToTaprootScript(
		txscript.ComputeTaprootOutputKey(anchorKey, tapscriptRoot[:]),
	)
	require.NoError(h.t, err)

	genesisTx := wire.NewMsgTx(2)
	genesisTx.AddTxIn(&wire.TxIn{PreviousOutPoint: genesisPoint})
	genesisTx.AddTxOut(&wire.TxOut{Value: 1_000, PkScript: pkScript})
	block, height := h.chain.mine(h.t, genesisTx)

	vCtx := h.client.engine.verifierCtx(ctx)
	proofs, err := proof.NewMintingBlobs(&proof.MintParams{
		BaseProofParams: proof.BaseProofParams{
			Block:            block,
			BlockHeight:      height,
			Tx:               genesisTx,
			TxIndex:          len(block.Transactions) - 1,
			OutputIndex:      0,
			InternalKey:      anchorKey,
			TaprootAssetRoot: tapCommitment,
		},
		GenesisPoint: genesisPoint,
	}, vCtx)
	require.NoError(h.t, err)

	var minted [2]*mintedAsset
	for idx, issued := range assets {
		issuance := proofs[asset.ToSerialized(issued.ScriptKey.PubKey)]
		require.NotNil(h.t, issuance)

		file, err := proof.NewFile(proof.V0, *issuance)
		require.NoError(h.t, err)
		var encoded bytes.Buffer
		require.NoError(h.t, file.Encode(&encoded))

		_, err = h.wallet.ImportProofFile(
			ctx, &tapsdk.ProofFile{RawProofFile: encoded.Bytes()},
		)
		require.NoError(h.t, err)

		assetID := issued.ID()
		minted[idx] = &mintedAsset{
			ref: tapsdk.AssetRefFromAssetID(
				tapsdk.AssetID(assetID),
			),
			assetID:   assetID,
			proofFile: encoded.Bytes(),
			outpoint: wire.OutPoint{
				Hash: genesisTx.TxHash(),
			},
		}
	}

	return minted
}

// TestFundAddressSendPassiveAsset funds a transfer from an anchor output
// that holds a second asset, which moves to the anchor output of the change
// in a packet of its own.
func TestFundAddressSendPassiveAsset(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	pair := sender.receivePair("spent", "kept")
	addr, err := recipient.wallet.NewReceiveAddress(ctx, pair[0].ref)
	require.NoError(t, err)

	funded, active, passive := sender.fundSendOne(addr.Encoded, 300)
	require.Equal(t, pair[0].assetID, active.Inputs[0].PrevID.ID)
	change, err := active.SplitRootOutput()
	require.NoError(t, err)

	require.Len(t, passive, 1)
	require.Len(t, passive[0].Inputs, 1)
	require.Equal(t, pair[1].assetID, passive[0].Inputs[0].PrevID.ID)
	require.Equal(t, pair[1].outpoint, passive[0].Inputs[0].PrevID.OutPoint)
	require.Len(t, passive[0].Outputs, 1)
	kept := passive[0].Outputs[0]
	require.EqualValues(t, receivedUnits, kept.Amount)
	require.Equal(t, change.AnchorOutputIndex, kept.AnchorOutputIndex)
	require.True(t, kept.AnchorOutputInternalKey.IsEqual(
		change.AnchorOutputInternalKey,
	))
	require.True(t, kept.ScriptKey.PubKey.IsEqual(
		passive[0].Inputs[0].Asset().ScriptKey.PubKey,
	))

	sender.commitSend(funded, 1)

	// Without change, the second asset moves to an anchor output of its
	// own under a key of the wallet.
	funded, active, passive = sender.fundSendOne(
		addr.Encoded, receivedUnits,
	)
	require.Len(t, active.Outputs, 1)
	require.Len(t, passive, 1)
	kept = passive[0].Outputs[0]
	require.EqualValues(t, 1, kept.AnchorOutputIndex)
	sender.requireWalletKey(kept.AnchorOutputInternalKey)

	sender.commitSend(funded, 2)

	// A transfer of a whole output to two recipients has no change, and
	// marks the output of a recipient as the split root of its packet.
	// The second asset moves to an anchor output of the wallet all the
	// same.
	other, err := recipient.wallet.NewReceiveAddress(ctx, pair[0].ref)
	require.NoError(t, err)
	funded, err = sender.client.FundTransfer(ctx, []tapsdk.Recipient{
		tapsdk.RecipientWithAmount(addr.Encoded, 400),
		tapsdk.RecipientWithAmount(other.Encoded, receivedUnits-400),
	}, nil)
	require.NoError(t, err)
	require.Len(t, funded.FundedPsbts, 1)
	active, err = tappsbt.Decode(funded.FundedPsbts[0])
	require.NoError(t, err)
	require.True(t, active.HasSplitRootOutput())
	for _, vOut := range active.Outputs {
		require.NotNil(t, vOut.Address)
	}

	passive, err = decodeVirtualPackets(funded.PassiveAssetPsbts)
	require.NoError(t, err)
	require.Len(t, passive, 1)
	kept = passive[0].Outputs[0]
	require.EqualValues(t, 2, kept.AnchorOutputIndex)
	sender.requireWalletKey(kept.AnchorOutputInternalKey)

	sender.commitSend(funded, 3)
}

// receiveIssuances issues the amounts to keys of the wallet as issuances of
// the group that groupPriv signs for, the first of which starts the group,
// and imports their proofs into the wallet.
func (h *testHarness) receiveIssuances(groupPriv *btcec.PrivateKey,
	amounts ...uint64) []*mintedAsset {

	h.t.Helper()

	ctx := h.t.Context()
	minted := make([]*mintedAsset, len(amounts))
	for idx, amount := range amounts {
		scriptKey, err := h.client.DeriveScriptKey(ctx)
		require.NoError(h.t, err)
		internalKey, err := h.client.DeriveInternalKey(ctx)
		require.NoError(h.t, err)

		minted[idx] = h.mintTranche(
			scriptKey, internalKey, amount,
			fmt.Sprintf("issuance-%d", idx), "issuance-0",
			groupPriv,
		)
		_, err = h.wallet.ImportProofFile(ctx, &tapsdk.ProofFile{
			RawProofFile: minted[idx].proofFile,
		})
		require.NoError(h.t, err)
	}

	return minted
}

// TestFundAddressSendIssuances funds a transfer of a grouped asset that no
// issuance of the group holds alone: one packet per issuance pays the
// recipient in one anchor output, and the change returns to the wallet.
func TestFundAddressSendIssuances(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	groupPriv := newTestKeyRing(4).privKey(1, 0)
	issuances := sender.receiveIssuances(groupPriv, 100, 50)
	first, second := issuances[0], issuances[1]
	require.Equal(t, first.ref, second.ref)
	require.NotEqual(t, first.assetID, second.assetID)

	balance, err := sender.wallet.GetBalance(ctx, first.ref)
	require.NoError(t, err)
	require.EqualValues(t, 150, balance)

	addr, err := recipient.wallet.NewReceiveAddress(ctx, first.ref)
	require.NoError(t, err)
	tapAddr, err := address.DecodeAddress(
		addr.Encoded, &address.RegressionNetTap,
	)
	require.NoError(t, err)

	funded, active, passive := sender.fundSend(addr.Encoded, 120)
	require.Empty(t, passive)
	require.Len(t, active, 2)

	var (
		paid    = make(map[asset.ID]uint64)
		changes []*tappsbt.VOutput
	)
	for _, vPkt := range active {
		assetID, err := vPkt.AssetID()
		require.NoError(t, err)

		for _, vOut := range vPkt.Outputs {
			if vOut.Address == nil {
				changes = append(changes, vOut)

				continue
			}

			scriptKey, err := asset.DeriveUniqueScriptKey(
				tapAddr.ScriptKey, assetID,
				asset.ScriptKeyDerivationUniquePedersen,
			)
			require.NoError(t, err)
			require.True(t, vOut.ScriptKey.PubKey.IsEqual(
				scriptKey.PubKey,
			))
			require.True(t, vOut.AnchorOutputInternalKey.IsEqual(
				&tapAddr.InternalKey,
			))
			require.Zero(t, vOut.AnchorOutputIndex)
			require.Len(t, vOut.AltLeaves, 1)
			paid[assetID] += vOut.Amount
		}
	}
	require.Len(t, paid, 2)
	require.EqualValues(t, 120, paid[first.assetID]+paid[second.assetID])

	require.Len(t, changes, 1)
	require.EqualValues(t, 30, changes[0].Amount)
	require.EqualValues(t, 1, changes[0].AnchorOutputIndex)
	sender.requireWalletKey(changes[0].AnchorOutputInternalKey)

	sender.commitSend(funded, 1)
}

// TestFundAddressSendOddScriptKey funds a transfer from an output whose asset
// leaf names the script key of the wallet with odd parity. tapd's
// constructors make even keys, but a sender that builds the leaf itself can
// pay an address that way.
func TestFundAddressSendOddScriptKey(t *testing.T) {
	sender, recipient := newSendHarness(t)
	ctx := t.Context()

	scriptKey, err := sender.client.DeriveScriptKey(ctx)
	require.NoError(t, err)
	internalKey, err := sender.client.DeriveInternalKey(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0x02, scriptKey.PubKey[0])

	oddKey := *scriptKey
	oddKey.PubKey[0] = 0x03
	minted := sender.mint(&oddKey, internalKey, receivedUnits, "odd", nil)
	_, err = sender.wallet.ImportProofFile(
		ctx, &tapsdk.ProofFile{RawProofFile: minted.proofFile},
	)
	require.NoError(t, err)

	addr, err := recipient.wallet.NewReceiveAddress(ctx, minted.ref)
	require.NoError(t, err)
	funded, active, _ := sender.fundSendOne(addr.Encoded, 300)
	prevID := active.Inputs[0].PrevID
	require.Equal(t, minted.outpoint, prevID.OutPoint)
	require.EqualValues(t, 0x03, prevID.ScriptKey[0])

	sender.commitSend(funded, 1)
}

// TestSelectCoins picks the largest coins of a transfer first, whatever
// issuance of a group they are of.
func TestSelectCoins(t *testing.T) {
	newCoin := func(assetID byte, index uint32, amount uint64) *coin {
		return &coin{
			holding: &holding{chainAsset: asset.ChainAsset{
				Asset: &asset.Asset{Amount: amount},
			}},
			prevID: asset.PrevID{
				OutPoint: wire.OutPoint{Index: index},
				ID:       asset.ID{assetID},
			},
		}
	}

	tests := []struct {
		name   string
		coins  []*coin
		amount uint64

		// picked are the anchor output indexes of the selected coins.
		picked []uint32
		code   codes.Code
	}{{
		name: "largest coin",
		coins: []*coin{
			newCoin(1, 0, 10), newCoin(1, 1, 50), newCoin(1, 2, 30),
		},
		amount: 40,
		picked: []uint32{1},
	}, {
		name: "largest coins first",
		coins: []*coin{
			newCoin(1, 0, 10), newCoin(1, 1, 50), newCoin(1, 2, 30),
		},
		amount: 85,
		picked: []uint32{1, 2, 0},
	}, {
		name: "several issuances",
		coins: []*coin{
			newCoin(1, 0, 40), newCoin(2, 1, 60),
			newCoin(1, 2, 30),
		},
		amount: 110,
		picked: []uint32{1, 0, 2},
	}, {
		name: "equal amounts in a stable order",
		coins: []*coin{
			newCoin(2, 0, 50), newCoin(1, 1, 50),
		},
		amount: 50,
		picked: []uint32{1},
	}, {
		name:   "not enough units",
		coins:  []*coin{newCoin(1, 0, 100), newCoin(2, 1, 60)},
		amount: 161,
		code:   codes.FailedPrecondition,
	}, {
		name:   "no coins",
		amount: 1,
		code:   codes.FailedPrecondition,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := selectCoins(tc.coins, tc.amount)
			require.Equal(t, tc.code, status.Code(err))

			var picked []uint32
			for _, c := range selected {
				picked = append(picked, c.prevID.OutPoint.Index)
			}
			require.Equal(t, tc.picked, picked)
		})
	}
}
