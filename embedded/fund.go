package embedded

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/tapsend"
	"github.com/lightningnetwork/lnd/keychain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FundTransfer selects outputs of the wallet that pay version 2 Taproot Asset
// addresses, each with the amount its recipient names, and returns the
// virtual transactions that spend them: one per asset issuance the transfer
// spends, with the surplus in a change output of the wallet. An asset that a
// spent anchor output holds beside the spent ones is returned as a passive
// packet. A non-empty inputs restricts the selection to the outputs it names.
//
// The engine selects among the outputs it signs for by key. It leases no
// output, so a caller funds one transfer at a time.
func (c *Client) FundTransfer(ctx context.Context,
	recipients []tapsdk.Recipient,
	inputs []tapsdk.PrevID) (*tapsdk.FundedTransfer, error) {

	ctx, cancel := context.WithTimeout(ctx, c.rpcTimeout)
	defer cancel()

	funded, err := c.engine.fundTransfer(ctx, recipients, inputs)
	if err != nil {
		return nil, toStatusError(ctx, err)
	}

	return funded, nil
}

// fundTransfer builds and encodes the virtual transactions of a transfer to
// addresses.
func (e *engine) fundTransfer(ctx context.Context,
	recipients []tapsdk.Recipient,
	inputs []tapsdk.PrevID) (*tapsdk.FundedTransfer, error) {

	addrs, err := e.fundingAddresses(recipients)
	if err != nil {
		return nil, err
	}

	prevIDs := make([]asset.PrevID, len(inputs))
	for idx, input := range inputs {
		prevIDs[idx] = asset.PrevID{
			OutPoint: wire.OutPoint{
				Hash:  input.Outpoint.Txid,
				Index: input.Outpoint.Index,
			},
			ID:        asset.ID(input.IssuanceID),
			ScriptKey: asset.SerializedKey(input.ScriptKey),
		}
	}

	active, passive, err := e.fundAddressSend(ctx, addrs, prevIDs)
	if err != nil {
		return nil, fmt.Errorf("error funding address send: %w", err)
	}

	funded := &tapsdk.FundedTransfer{}
	funded.FundedPsbts, err = encodeVirtualPackets(active)
	if err != nil {
		return nil, fmt.Errorf("error serializing packets: %w", err)
	}
	funded.PassiveAssetPsbts, err = encodeVirtualPackets(passive)
	if err != nil {
		return nil, fmt.Errorf("error serializing passive packets: %w",
			err)
	}

	return funded, nil
}

// invalidArgf returns the status of a request the engine cannot read.
func invalidArgf(format string, args ...any) error {
	return status.Errorf(codes.InvalidArgument, format, args...)
}

// fundingAddresses decodes the addresses a transfer pays, each with the amount
// to send to it. The wallet funds one asset or asset group per transfer, and
// sends to version 2 addresses only.
func (e *engine) fundingAddresses(
	recipients []tapsdk.Recipient) ([]*address.Tap, error) {

	if len(recipients) == 0 {
		return nil, invalidArgf("at least one recipient is required")
	}

	addrs := make([]*address.Tap, len(recipients))
	for idx, recipient := range recipients {
		addr, err := e.fundingAddress(recipient)
		if err != nil {
			return nil, invalidArgf("addr %d: %v", idx, err)
		}
		if !sameAddressAsset(addrs[0], addr) {
			return nil, invalidArgf("addr %d is of another asset "+
				"than addr 0, a transfer sends one asset or "+
				"asset group", idx)
		}

		addrs[idx] = addr
	}

	return addrs, nil
}

// fundingAddress decodes the address of a recipient and sets the amount to
// send to it. As on tapd, a funding request names the amount of every
// recipient.
func (e *engine) fundingAddress(
	recipient tapsdk.Recipient) (*address.Tap, error) {

	amount, ok := recipient.Amount()
	switch {
	case recipient.Address == "":
		return nil, errors.New("address must be specified")

	case !ok || amount == 0:
		return nil, errors.New("an explicit amount is required")
	}

	addr, err := address.DecodeAddress(recipient.Address, &e.params)
	if err != nil {
		return nil, err
	}
	if addr.Version != address.V2 {
		return nil, fmt.Errorf("address is of version %d, the "+
			"embedded backend sends to version %d addresses only",
			addr.Version, address.V2)
	}
	if err := validateCourier(&addr.ProofCourierAddr); err != nil {
		return nil, err
	}

	// A version 2 address may leave the amount to the sender.
	switch {
	case addr.Amount > 0 && addr.Amount != amount:
		return nil, errors.New("address has a non-zero amount " +
			"defined, cannot override amount in " +
			"addresses_with_amounts")

	case addr.Amount == 0:
		addr.Amount = amount
	}

	return addr, nil
}

// sameAddressAsset reports whether two addresses name the same asset or asset
// group. A nil first address matches every address.
func sameAddressAsset(first, addr *address.Tap) bool {
	switch {
	case first == nil:
		return true

	case first.GroupKey != nil || addr.GroupKey != nil:
		return first.GroupKey != nil && addr.GroupKey != nil &&
			sameXOnlyKey(first.GroupKey, addr.GroupKey)

	default:
		return first.AssetID == addr.AssetID
	}
}

// coin is an unspent asset output of the wallet that can pay for a transfer.
type coin struct {
	holding *holding
	prevID  asset.PrevID

	// internalKey is the wallet's key of the anchor output.
	internalKey keychain.KeyDescriptor
}

// amount returns the units the coin holds.
func (c *coin) amount() uint64 {
	return c.holding.chainAsset.Amount
}

// fundAddressSend builds the virtual transactions of a transfer to version 2
// addresses of one asset or asset group, as tapd's FundAddressSend does. It
// returns one active packet per asset the transfer spends, and one passive
// packet per asset the spent anchor outputs hold beside those.
func (e *engine) fundAddressSend(ctx context.Context, addrs []*address.Tap,
	prevIDs []asset.PrevID) ([]*tappsbt.VPacket, []*tappsbt.VPacket,
	error) {

	template, err := tappsbt.FromAddresses(addrs, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create virtual "+
			"transaction from addresses: %w", err)
	}
	var amount uint64
	for _, addr := range addrs {
		if addr.Amount > math.MaxUint64-amount {
			return nil, nil, invalidArgf("the amounts of the " +
				"addresses overflow")
		}
		amount += addr.Amount
	}

	// The change of the wallet is of the asset version its other outputs
	// have, whatever version the addresses request.
	template.Outputs = append(template.Outputs, &tappsbt.VOutput{
		AssetVersion:      asset.V1,
		Type:              tappsbt.TypeSplitRoot,
		Interactive:       true,
		AnchorOutputIndex: uint32(len(addrs)),
		ScriptKey:         asset.NUMSScriptKey,
	})

	eligible, err := e.eligibleCoins(ctx, addrs[0], prevIDs)
	if err != nil {
		return nil, nil, err
	}
	selected, err := selectCoins(eligible, amount)
	if err != nil {
		return nil, nil, err
	}

	var (
		inputSum    uint64
		inputProofs = make([]*proof.Proof, len(selected))
		coins       = make(map[asset.PrevID]*coin, len(selected))
	)
	for idx, selectedCoin := range selected {
		inputSum += selectedCoin.amount()
		inputProofs[idx] = selectedCoin.holding.lastProof
		coins[selectedCoin.prevID] = selectedCoin
	}

	allocations, interactive, err := tapsend.AllocationsFromTemplate(
		template, inputSum,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("error extracting allocations: %w",
			err)
	}
	active, err := tapsend.DistributeCoins(
		inputProofs, allocations, &e.params, interactive,
		template.Version,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to distribute coins: %w",
			err)
	}

	for _, vPkt := range active {
		for idx := range vPkt.Inputs {
			prevID := vPkt.Inputs[idx].PrevID
			input, ok := coins[prevID]
			if !ok {
				return nil, nil, fmt.Errorf("input %v was not "+
					"selected", prevID.OutPoint)
			}

			err := e.setFundedInput(vPkt, idx, input)
			if err != nil {
				return nil, nil, err
			}
		}

		if err := e.deriveChangeKey(ctx, vPkt); err != nil {
			return nil, nil, fmt.Errorf("unable to derive change "+
				"output key: %w", err)
		}
	}
	if err := e.deriveOutputAnchorKeys(ctx, active); err != nil {
		return nil, nil, fmt.Errorf("unable to derive output anchor "+
			"internal keys: %w", err)
	}

	for _, vPkt := range active {
		if err := prepareV2Outputs(vPkt); err != nil {
			return nil, nil, err
		}
		if err := tapsend.PrepareOutputAssets(ctx, vPkt); err != nil {
			return nil, nil, fmt.Errorf("unable to prepare "+
				"outputs: %w", err)
		}
	}

	passive, err := e.passivePackets(ctx, active)
	if err != nil {
		return nil, nil, err
	}

	return active, passive, nil
}

// eligibleCoins returns the unspent outputs of the asset or group an address
// names that the wallet can spend on its own: it holds the script key and
// the key of the anchor output. A non-empty prevIDs keeps those outputs only.
func (e *engine) eligibleCoins(ctx context.Context, addr *address.Tap,
	prevIDs []asset.PrevID) ([]*coin, error) {

	held, err := e.ownedAssets(ctx, false, fn.Some(asset.ScriptKeyBip86))
	if err != nil {
		return nil, err
	}

	var coins []*coin
	for _, h := range held {
		chainAsset := h.chainAsset
		switch {
		case addr.GroupKey != nil:
			if chainAsset.GroupKey == nil || !sameXOnlyKey(
				&chainAsset.GroupKey.GroupPubKey, addr.GroupKey,
			) {

				continue
			}

		case chainAsset.ID() != addr.AssetID:
			continue
		}

		prevID := heldPrevID(chainAsset)
		if len(prevIDs) > 0 && !slices.ContainsFunc(
			prevIDs, func(named asset.PrevID) bool {
				return sameOutput(named, prevID)
			},
		) {

			continue
		}

		scriptKey := chainAsset.ScriptKey.TweakedScriptKey
		if scriptKey == nil || !e.IsLocalKey(ctx, scriptKey.RawKey) {
			continue
		}
		anchorKey := schnorr.SerializePubKey(
			chainAsset.AnchorInternalKey,
		)
		internalKey, err := e.schnorrInternalKey(ctx, anchorKey)
		switch {
		case errors.Is(err, address.ErrInternalKeyNotFound):
			continue

		case err != nil:
			return nil, err
		}

		coins = append(coins, &coin{
			holding:     h,
			prevID:      prevID,
			internalKey: internalKey,
		})
	}

	return coins, nil
}

// heldPrevID returns the previous ID of an asset output the wallet holds, with
// the script key as its asset leaf names it, as a virtual input of the output
// names it.
func heldPrevID(chainAsset asset.ChainAsset) asset.PrevID {
	return asset.PrevID{
		OutPoint:  chainAsset.AnchorOutpoint,
		ID:        chainAsset.ID(),
		ScriptKey: asset.ToSerialized(chainAsset.ScriptKey.PubKey),
	}
}

// sameOutput reports whether two previous IDs name the same asset output. A
// caller may name a script key with either parity.
func sameOutput(a, b asset.PrevID) bool {
	return a.OutPoint == b.OutPoint && a.ID == b.ID &&
		bytes.Equal(a.ScriptKey[1:], b.ScriptKey[1:])
}

// selectCoins picks the coins that pay amount, largest first, as tapd does.
// The coins of an asset group may be of several issuances.
func selectCoins(coins []*coin, amount uint64) ([]*coin, error) {
	slices.SortFunc(coins, func(a, b *coin) int {
		return cmp.Or(
			cmp.Compare(b.amount(), a.amount()),
			bytes.Compare(a.prevID.ID[:], b.prevID.ID[:]),
			cmp.Compare(
				a.prevID.OutPoint.String(),
				b.prevID.OutPoint.String(),
			),
		)
	})

	var (
		selected []*coin
		sum      uint64
	)
	for _, c := range coins {
		selected = append(selected, c)
		sum += c.amount()
		if sum >= amount {
			return selected, nil
		}
	}

	return nil, status.Errorf(codes.FailedPrecondition,
		"insufficient asset balance: the wallet can spend %d units, "+
			"the transfer needs %d; if previous transfers are "+
			"unconfirmed, wait for them to confirm", sum, amount)
}

// setFundedInput completes the input of a packet that spends a coin: its
// asset, its proof and the anchor output that holds it.
func (e *engine) setFundedInput(vPkt *tappsbt.VPacket, idx int,
	input *coin) error {

	chainAsset := input.holding.chainAsset
	inputProof := input.holding.lastProof

	outIdx := inputProof.InclusionProof.OutputIndex
	if int(outIdx) >= len(inputProof.AnchorTx.TxOut) {
		return fmt.Errorf("anchor output %d out of range", outIdx)
	}
	anchorOut := inputProof.AnchorTx.TxOut[outIdx]

	derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
		input.internalKey, e.params.HDCoinType,
	)
	vPkt.Inputs[idx] = &tappsbt.VInput{
		PrevID: input.prevID,
		Anchor: tappsbt.Anchor{
			Value:            btcutilAmount(anchorOut.Value),
			PkScript:         anchorOut.PkScript,
			InternalKey:      input.internalKey.PubKey,
			MerkleRoot:       chainAsset.AnchorMerkleRoot,
			TapscriptSibling: chainAsset.AnchorTapscriptSibling,
			Bip32Derivation:  []*psbt.Bip32Derivation{derivation},
			TrBip32Derivation: []*psbt.TaprootBip32Derivation{
				trDerivation,
			},
		},
		Proof: inputProof,
		PInput: psbt.PInput{
			SighashType: txscript.SigHashDefault,
		},
	}
	vPkt.SetInputAsset(idx, chainAsset.Asset)

	return nil
}

// deriveChangeKey gives the change output of a packet a new script key of
// the wallet. A change output that holds no units keeps its un-spendable
// key.
func (e *engine) deriveChangeKey(ctx context.Context,
	vPkt *tappsbt.VPacket) error {

	if !vPkt.HasSplitRootOutput() {
		return nil
	}
	changeOut, err := vPkt.SplitRootOutput()
	if err != nil {
		return err
	}

	unSpendable, err := changeOut.ScriptKey.IsUnSpendable()
	if err != nil {
		return fmt.Errorf("cannot determine if script key is "+
			"spendable: %w", err)
	}
	if !unSpendable || changeOut.Amount == 0 {
		return nil
	}

	rawKey, err := e.nextInternalKey(ctx, asset.TaprootAssetsKeyFamily)
	if err != nil {
		return err
	}
	changeOut.ScriptKey = asset.NewScriptKeyBip86(rawKey)

	return e.insertScriptKey(ctx, changeOut.ScriptKey)
}

// deriveOutputAnchorKeys gives every output without an anchor internal key a
// key of the wallet, the same one for the outputs of one anchor output. The
// outputs that pay an address carry the internal key of that address.
func (e *engine) deriveOutputAnchorKeys(ctx context.Context,
	packets []*tappsbt.VPacket) error {

	anchorKeys := make(map[uint32]*tappsbt.VOutput)
	for _, vPkt := range packets {
		for _, vOut := range vPkt.Outputs {
			if vOut.AnchorOutputInternalKey == nil {
				continue
			}

			known, ok := anchorKeys[vOut.AnchorOutputIndex]
			if ok && !known.AnchorOutputInternalKey.IsEqual(
				vOut.AnchorOutputInternalKey,
			) {

				return fmt.Errorf("anchor output index %d has "+
					"two internal keys",
					vOut.AnchorOutputIndex)
			}
			anchorKeys[vOut.AnchorOutputIndex] = vOut
		}
	}

	for _, vPkt := range packets {
		for _, vOut := range vPkt.Outputs {
			if vOut.AnchorOutputInternalKey != nil {
				continue
			}

			known, ok := anchorKeys[vOut.AnchorOutputIndex]
			if ok {
				copyAnchorKey(vOut, known)

				continue
			}

			internalKey, err := e.nextInternalKey(
				ctx, asset.TaprootAssetsKeyFamily,
			)
			if err != nil {
				return err
			}
			vOut.SetAnchorInternalKey(
				internalKey, e.params.HDCoinType,
			)
			anchorKeys[vOut.AnchorOutputIndex] = vOut
		}
	}

	return nil
}

// copyAnchorKey gives an output the anchor internal key of another output of
// the same anchor output.
func copyAnchorKey(vOut, from *tappsbt.VOutput) {
	vOut.AnchorOutputInternalKey = from.AnchorOutputInternalKey
	vOut.AnchorOutputBip32Derivation = from.AnchorOutputBip32Derivation
	vOut.AnchorOutputTaprootBip32Derivation =
		from.AnchorOutputTaprootBip32Derivation
	vOut.AnchorOutputTapscriptSibling = from.AnchorOutputTapscriptSibling
}

// prepareV2Outputs applies the output rules of a version 2 address to the
// outputs of a packet that pay one, as tapd does: the script key is derived
// from the script key of the address for the asset of the packet, and a
// random alt leaf makes the anchor output of two equal payments differ.
func prepareV2Outputs(vPkt *tappsbt.VPacket) error {
	assetID, err := vPkt.AssetID()
	if err != nil {
		return fmt.Errorf("unable to determine asset ID: %w", err)
	}

	for _, vOut := range vPkt.Outputs {
		if vOut.Address == nil || !vOut.Address.UsesSendManifests() {
			continue
		}

		vOut.ScriptKey, err = asset.DeriveUniqueScriptKey(
			vOut.Address.ScriptKey, assetID,
			asset.ScriptKeyDerivationUniquePedersen,
		)
		if err != nil {
			return fmt.Errorf("unable to derive unique script "+
				"key: %w", err)
		}

		randKey, err := btcec.NewPrivateKey()
		if err != nil {
			return fmt.Errorf("unable to create random private "+
				"key: %w", err)
		}
		altLeaf, err := asset.NewAltLeaf(
			asset.NewScriptKey(randKey.PubKey()), asset.ScriptV0,
		)
		if err != nil {
			return fmt.Errorf("unable to create random alt leaf: "+
				"%w", err)
		}
		vOut.AltLeaves = append(vOut.AltLeaves, altLeaf)
	}

	return nil
}

// passivePackets returns a packet for every asset of the wallet that an
// anchor output spent by the active packets holds and no active packet
// spends. Each moves its asset unchanged to the anchor output of the change,
// or to a new anchor output of the wallet when the transfer has no change.
func (e *engine) passivePackets(ctx context.Context,
	active []*tappsbt.VPacket) ([]*tappsbt.VPacket, error) {

	var (
		spent   = make(map[asset.PrevID]struct{})
		anchors = make(map[wire.OutPoint]*tappsbt.VInput)
	)
	for _, vPkt := range active {
		for _, vIn := range vPkt.Inputs {
			spent[vIn.PrevID] = struct{}{}
			anchors[vIn.PrevID.OutPoint] = vIn
		}
	}

	held, err := e.ownedAssets(ctx, false, fn.None[asset.ScriptKeyType]())
	if err != nil {
		return nil, err
	}

	var (
		passive   []*tappsbt.VPacket
		anchorOut *tappsbt.VOutput
	)
	for _, h := range held {
		chainAsset := h.chainAsset
		anchorIn, ok := anchors[chainAsset.AnchorOutpoint]
		if !ok {
			continue
		}
		prevID := heldPrevID(chainAsset)
		if _, ok := spent[prevID]; ok {
			continue
		}

		if anchorOut == nil {
			anchorOut, err = e.passiveAnchorOutput(ctx, active)
			if err != nil {
				return nil, err
			}
		}

		vPkt := passivePacket(
			h, prevID, anchorIn, anchorOut, &e.params,
			active[0].Version,
		)
		passive = append(passive, vPkt)
	}

	return passive, nil
}

// passiveAnchorOutput returns an output that carries the anchor output the
// passive assets move to: the change of the transfer, or a new anchor output
// under a new key of the wallet. A packet that splits an output over
// recipients and keeps no change marks the output of a recipient as its split
// root, so the change is told apart by its key, as tapd does.
func (e *engine) passiveAnchorOutput(ctx context.Context,
	active []*tappsbt.VPacket) (*tappsbt.VOutput, error) {

	var nextIndex uint32
	for _, vPkt := range active {
		for _, vOut := range vPkt.Outputs {
			nextIndex = max(nextIndex, vOut.AnchorOutputIndex+1)
		}
	}
	for _, vPkt := range active {
		for _, vOut := range vPkt.Outputs {
			if vOut.Type != tappsbt.TypeSplitRoot ||
				vOut.Address != nil {

				continue
			}

			anchorKey := keychain.KeyDescriptor{
				PubKey: vOut.AnchorOutputInternalKey,
			}
			if e.IsLocalKey(ctx, anchorKey) {
				return vOut, nil
			}
		}
	}

	internalKey, err := e.nextInternalKey(
		ctx, asset.TaprootAssetsKeyFamily,
	)
	if err != nil {
		return nil, fmt.Errorf("error deriving anchor internal key "+
			"for passive assets: %w", err)
	}

	anchorOut := &tappsbt.VOutput{AnchorOutputIndex: nextIndex}
	anchorOut.SetAnchorInternalKey(internalKey, e.params.HDCoinType)

	return anchorOut, nil
}

// passivePacket builds the packet that moves an asset the wallet holds, with
// its amount and script key unchanged, from the anchor output of anchorIn to
// the one of anchorOut.
func passivePacket(h *holding, prevID asset.PrevID, anchorIn *tappsbt.VInput,
	anchorOut *tappsbt.VOutput, params *address.ChainParams,
	version tappsbt.VPacketVersion) *tappsbt.VPacket {

	// The asset of the proof lacks the derivation of the script key,
	// which signing needs.
	inputAsset := h.lastProof.Asset.Copy()
	inputAsset.ScriptKey = h.chainAsset.ScriptKey

	vIn := &tappsbt.VInput{
		PrevID: prevID,
		Anchor: anchorIn.Anchor,
		Proof:  h.lastProof,
		PInput: psbt.PInput{
			SighashType: txscript.SigHashDefault,
		},
	}

	outputAsset := inputAsset.CopySpendTemplate()
	outputAsset.PrevWitnesses = []asset.Witness{{
		PrevID: &vIn.PrevID,
	}}
	vOut := &tappsbt.VOutput{
		Amount:            outputAsset.Amount,
		AssetVersion:      outputAsset.Version,
		Interactive:       true,
		AnchorOutputIndex: anchorOut.AnchorOutputIndex,
		ScriptKey:         outputAsset.ScriptKey,
		Asset:             outputAsset,
	}
	copyAnchorKey(vOut, anchorOut)

	vPkt := &tappsbt.VPacket{
		Inputs:      []*tappsbt.VInput{vIn},
		Outputs:     []*tappsbt.VOutput{vOut},
		ChainParams: params,
		Version:     version,
	}
	vPkt.SetInputAsset(0, inputAsset)

	return vPkt
}
