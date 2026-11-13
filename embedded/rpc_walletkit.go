package embedded

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/rpcutils"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/taprpc"
	wrpc "github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"github.com/lightninglabs/taproot-assets/tapscript"
	"github.com/lightninglabs/taproot-assets/tapsend"
	"github.com/lightningnetwork/lnd/keychain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// walletKitServer answers the calls of tapd's AssetWallet service that the
// engine supports.
type walletKitServer struct {
	wrpc.UnimplementedAssetWalletServer

	engine *engine
}

// NextInternalKey derives the next key of a family and records it as a
// wallet key.
func (s *walletKitServer) NextInternalKey(ctx context.Context,
	req *wrpc.NextInternalKeyRequest) (*wrpc.NextInternalKeyResponse,
	error) {

	if req.KeyFamily == 0 {
		return nil, fmt.Errorf("key family must be set to a non-zero " +
			"value")
	}

	keyDesc, err := s.engine.nextInternalKey(
		ctx, keychain.KeyFamily(req.KeyFamily),
	)
	if err != nil {
		return nil, fmt.Errorf("error inserting internal key: %w", err)
	}

	return &wrpc.NextInternalKeyResponse{
		InternalKey: rpcutils.MarshalKeyDescriptor(keyDesc),
	}, nil
}

// NextScriptKey derives the next key of a family and records it and its
// BIP-86 script key as wallet keys.
func (s *walletKitServer) NextScriptKey(ctx context.Context,
	req *wrpc.NextScriptKeyRequest) (*wrpc.NextScriptKeyResponse, error) {

	if req.KeyFamily == 0 {
		return nil, fmt.Errorf("key family must be set to a non-zero " +
			"value")
	}

	keyDesc, err := s.engine.nextInternalKey(
		ctx, keychain.KeyFamily(req.KeyFamily),
	)
	if err != nil {
		return nil, fmt.Errorf("error inserting internal key: %w", err)
	}

	scriptKey := asset.NewScriptKeyBip86(keyDesc)
	if err := s.engine.insertScriptKey(ctx, scriptKey); err != nil {
		return nil, fmt.Errorf("error inserting script key: %w", err)
	}

	return &wrpc.NextScriptKeyResponse{
		ScriptKey: rpcutils.MarshalScriptKey(scriptKey),
	}, nil
}

// nextInternalKey derives the next key of the family and stores it.
func (e *engine) nextInternalKey(ctx context.Context,
	family keychain.KeyFamily) (keychain.KeyDescriptor, error) {

	e.keyMu.Lock()
	defer e.keyMu.Unlock()

	keyDesc, err := e.keys.DeriveNextKey(family)
	if err != nil {
		return keychain.KeyDescriptor{}, fmt.Errorf("derive key: %w",
			err)
	}
	if keyDesc.PubKey == nil {
		return keychain.KeyDescriptor{}, fmt.Errorf("key ring " +
			"derived no key")
	}

	if err := e.store.InsertInternalKey(ctx, keyRecord(keyDesc)); err != nil {
		return keychain.KeyDescriptor{}, err
	}

	return keyDesc, nil
}

// QueryInternalKey returns the key descriptor of a wallet key.
func (s *walletKitServer) QueryInternalKey(ctx context.Context,
	req *wrpc.QueryInternalKeyRequest) (*wrpc.QueryInternalKeyResponse,
	error) {

	var (
		keyDesc keychain.KeyDescriptor
		err     error
	)
	switch len(req.InternalKey) {
	case btcec.PubKeyBytesLenCompressed:
		internalKey, parseErr := btcec.ParsePubKey(req.InternalKey)
		if parseErr != nil {
			return nil, fmt.Errorf("error parsing internal key: %w",
				parseErr)
		}

		keyDesc, err = s.engine.internalKeyDescriptor(ctx, internalKey)
		if err != nil {
			return nil, fmt.Errorf("error fetching internal key: "+
				"%w", err)
		}

	case schnorr.PubKeyBytesLen:
		keyDesc, err = s.engine.schnorrInternalKey(
			ctx, req.InternalKey,
		)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("invalid internal key length")
	}

	return &wrpc.QueryInternalKeyResponse{
		InternalKey: rpcutils.MarshalKeyDescriptor(keyDesc),
	}, nil
}

// internalKeyDescriptor returns the descriptor of a wallet key, or
// address.ErrInternalKeyNotFound.
func (e *engine) internalKeyDescriptor(ctx context.Context,
	pubKey *btcec.PublicKey) (keychain.KeyDescriptor, error) {

	keyDesc, err := e.fetchKeyDescriptor(ctx, pubKey)
	if errors.Is(err, ErrNotFound) {
		return keyDesc, address.ErrInternalKeyNotFound
	}

	return keyDesc, err
}

// schnorrInternalKey returns the descriptor of a wallet key given as an
// x-only key, trying both parities.
func (e *engine) schnorrInternalKey(ctx context.Context,
	schnorrKey []byte) (keychain.KeyDescriptor, error) {

	internalKey, err := schnorr.ParsePubKey(schnorrKey)
	if err != nil {
		return keychain.KeyDescriptor{}, fmt.Errorf("error parsing "+
			"internal key: %w", err)
	}

	keyDesc, err := e.internalKeyDescriptor(ctx, internalKey)
	if errors.Is(err, address.ErrInternalKeyNotFound) {
		keyDesc, err = e.internalKeyDescriptor(
			ctx, tapscript.FlipParity(internalKey),
		)
	}
	if err != nil {
		return keychain.KeyDescriptor{}, fmt.Errorf("error fetching "+
			"internal key: %w", err)
	}

	return keyDesc, nil
}

// QueryScriptKey returns the full script key of a tweaked key the wallet
// knows.
func (s *walletKitServer) QueryScriptKey(ctx context.Context,
	req *wrpc.QueryScriptKeyRequest) (*wrpc.QueryScriptKeyResponse, error) {

	var (
		scriptKey *btcec.PublicKey
		err       error
	)
	switch len(req.TweakedScriptKey) {
	case btcec.PubKeyBytesLenCompressed:
		scriptKey, err = btcec.ParsePubKey(req.TweakedScriptKey)

	case schnorr.PubKeyBytesLen:
		scriptKey, err = schnorr.ParsePubKey(req.TweakedScriptKey)

	default:
		return nil, fmt.Errorf("invalid script key length")
	}
	if err != nil {
		return nil, fmt.Errorf("error parsing script key: %w", err)
	}

	tweakedKey, err := s.engine.fetchScriptKey(ctx, scriptKey)
	if err != nil {
		return nil, fmt.Errorf("error fetching script key: %w", err)
	}

	return &wrpc.QueryScriptKeyResponse{
		ScriptKey: rpcutils.MarshalScriptKey(asset.ScriptKey{
			PubKey:           scriptKey,
			TweakedScriptKey: tweakedKey,
		}),
	}, nil
}

// DeclareScriptKey records a script key that was derived outside the engine.
func (s *walletKitServer) DeclareScriptKey(ctx context.Context,
	req *wrpc.DeclareScriptKeyRequest) (*wrpc.DeclareScriptKeyResponse,
	error) {

	if req.ScriptKey == nil {
		return nil, status.Error(codes.InvalidArgument,
			"script key is required")
	}
	scriptKey, err := rpcutils.UnmarshalScriptKey(req.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling script key: %w",
			err)
	}
	if scriptKey.RawKey.PubKey == nil {
		return nil, status.Error(codes.InvalidArgument,
			"script key descriptor is required")
	}

	scriptKey.Type = scriptKey.DetermineType(nil)
	if scriptKey.Type == asset.ScriptKeyUnknown {
		return nil, fmt.Errorf("script key type must be set")
	}

	if err := s.engine.insertScriptKey(ctx, *scriptKey); err != nil {
		return nil, fmt.Errorf("error inserting script key: %w", err)
	}

	return &wrpc.DeclareScriptKeyResponse{
		ScriptKey: rpcutils.MarshalScriptKey(*scriptKey),
	}, nil
}

// SignVirtualPsbt signs the asset inputs of a virtual transaction with the
// wallet's script keys.
func (s *walletKitServer) SignVirtualPsbt(ctx context.Context,
	req *wrpc.SignVirtualPsbtRequest) (*wrpc.SignVirtualPsbtResponse,
	error) {

	if req.FundedPsbt == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	vPkt, err := tappsbt.Decode(req.FundedPsbt)
	if err != nil {
		return nil, fmt.Errorf("error decoding packet: %w", err)
	}

	e := s.engine
	vCtx := e.verifierCtx(ctx)
	for _, input := range vPkt.Inputs {
		// Before signing, the input must be committed in its anchor
		// transaction.
		if input.Proof == nil {
			return nil, fmt.Errorf("input proof is nil")
		}
		_, err := input.Proof.VerifyProofIntegrity(ctx, vCtx)
		if err != nil {
			return nil, fmt.Errorf("unable to verify inclusion "+
				"proof: %w", err)
		}

		if len(input.Bip32Derivation) > 0 &&
			len(input.TaprootBip32Derivation) > 0 {

			continue
		}

		// The derivation of the signing key comes from the wallet's
		// record of the script key.
		scriptKey := input.Asset().ScriptKey
		if scriptKey.TweakedScriptKey == nil {
			tweaked, err := e.fetchScriptKey(ctx, scriptKey.PubKey)
			if err != nil {
				return nil, fmt.Errorf("error fetching "+
					"script key: %w", err)
			}
			scriptKey.TweakedScriptKey = tweaked
		}

		derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
			scriptKey.RawKey, e.params.HDCoinType,
		)
		input.Bip32Derivation = []*psbt.Bip32Derivation{derivation}
		input.TaprootBip32Derivation = []*psbt.TaprootBip32Derivation{
			trDerivation,
		}
	}

	err = tapsend.SignVirtualTransaction(
		vPkt, &keyRingSigner{keys: e.keys}, witnessValidator{},
	)
	if err != nil {
		return nil, fmt.Errorf("error signing packet: unable to "+
			"generate Taproot Asset witness data: %w", err)
	}

	signedInputs := make([]uint32, len(vPkt.Inputs))
	for idx := range vPkt.Inputs {
		signedInputs[idx] = uint32(idx)
	}

	signedPsbtBytes, err := fn.Serialize(vPkt)
	if err != nil {
		return nil, fmt.Errorf("error serializing packet: %w", err)
	}

	return &wrpc.SignVirtualPsbtResponse{
		SignedPsbt:   signedPsbtBytes,
		SignedInputs: signedInputs,
	}, nil
}

// CommitVirtualPsbts commits virtual transactions to a caller-funded anchor
// transaction and creates their transition proofs.
func (s *walletKitServer) CommitVirtualPsbts(ctx context.Context,
	req *wrpc.CommitVirtualPsbtsRequest) (*wrpc.CommitVirtualPsbtsResponse,
	error) {

	proofOpts, err := transitionProofOptions(req.TransitionProofVersion)
	if err != nil {
		return nil, err
	}
	if len(req.VirtualPsbts) == 0 {
		return nil, fmt.Errorf("no virtual PSBTs specified")
	}

	// The engine holds no Bitcoin, so the caller funds the anchor.
	if !req.SkipFunding {
		return nil, status.Error(codes.FailedPrecondition,
			"the embedded backend commits caller-funded anchor "+
				"transactions only")
	}

	pkt, err := psbt.NewFromRawBytes(bytes.NewReader(req.AnchorPsbt), false)
	if err != nil {
		return nil, fmt.Errorf("error decoding packet: %w", err)
	}
	activePackets, err := decodeVirtualPackets(req.VirtualPsbts)
	if err != nil {
		return nil, fmt.Errorf("error decoding active packets: %w", err)
	}
	passivePackets, err := decodeVirtualPackets(req.PassiveAssetPsbts)
	if err != nil {
		return nil, fmt.Errorf("error decoding passive packets: %w",
			err)
	}

	allPackets := append([]*tappsbt.VPacket{}, activePackets...)
	allPackets = append(allPackets, passivePackets...)
	err = s.engine.validateInputAssets(ctx, pkt, allPackets)
	if err != nil {
		return nil, fmt.Errorf("error validating input assets: %w", err)
	}

	err = tapsend.CommitPackets(pkt, allPackets, proofOpts...)
	if err != nil {
		return nil, err
	}

	response := &wrpc.CommitVirtualPsbtsResponse{
		ChangeOutputIndex: -1,
	}
	response.AnchorPsbt, err = fn.Serialize(pkt)
	if err != nil {
		return nil, fmt.Errorf("error serializing packet: %w", err)
	}
	response.VirtualPsbts, err = encodeVirtualPackets(activePackets)
	if err != nil {
		return nil, fmt.Errorf("error encoding active packets: %w", err)
	}
	response.PassiveAssetPsbts, err = encodeVirtualPackets(passivePackets)
	if err != nil {
		return nil, fmt.Errorf("error encoding passive packets: %w",
			err)
	}

	return response, nil
}

// validateInputAssets checks that the packets spend exactly the assets
// committed in the anchor transaction's inputs, and adds the derivation path
// of every anchor input key the wallet holds.
func (e *engine) validateInputAssets(ctx context.Context, btcPkt *psbt.Packet,
	vPackets []*tappsbt.VPacket) error {

	err := tapsend.ValidateVPacketVersions(vPackets)
	if err != nil {
		return err
	}

	for idx := range btcPkt.Inputs {
		pIn := &btcPkt.Inputs[idx]

		// Asset anchors always specify a Taproot merkle root and an
		// internal key.
		if len(pIn.TaprootMerkleRoot) == 0 ||
			len(pIn.TaprootInternalKey) != schnorr.PubKeyBytesLen ||
			len(pIn.TaprootBip32Derivation) > 0 {

			continue
		}

		keyDesc, err := e.schnorrInternalKey(
			ctx, pIn.TaprootInternalKey,
		)
		switch {
		// An unknown internal key belongs to another party.
		case errors.Is(err, address.ErrInternalKeyNotFound):
			continue

		case err != nil:
			return fmt.Errorf("error querying internal key: %w",
				err)
		}

		derivation, trDerivation := tappsbt.Bip32DerivationFromKeyDesc(
			keyDesc, e.params.HDCoinType,
		)
		pIn.Bip32Derivation = []*psbt.Bip32Derivation{derivation}
		pIn.TaprootBip32Derivation = []*psbt.TaprootBip32Derivation{
			trDerivation,
		}
	}

	// The engine stores no un-spendable assets, so the packets must carry
	// every asset of the anchors they spend.
	return tapsend.ValidateAnchoredPackets(btcPkt, vPackets, nil)
}

// PublishAndLogTransfer records a committed transfer, signs the anchor inputs
// the wallet owns when asked to, broadcasts the anchor transaction and
// follows it until its final proofs are stored.
func (s *walletKitServer) PublishAndLogTransfer(ctx context.Context,
	req *wrpc.PublishAndLogRequest) (*taprpc.SendAssetResponse, error) {

	if len(req.VirtualPsbts) == 0 {
		return nil, fmt.Errorf("no virtual PSBTs specified")
	}

	pkt, err := psbt.NewFromRawBytes(bytes.NewReader(req.AnchorPsbt), false)
	if err != nil {
		return nil, fmt.Errorf("error decoding packet: %w", err)
	}
	activePackets, err := decodeVirtualPackets(req.VirtualPsbts)
	if err != nil {
		return nil, fmt.Errorf("error decoding active packets: %w", err)
	}
	passivePackets, err := decodeVirtualPackets(req.PassiveAssetPsbts)
	if err != nil {
		return nil, fmt.Errorf("error decoding passive packets: %w",
			err)
	}

	e := s.engine
	allPackets := append([]*tappsbt.VPacket{}, activePackets...)
	allPackets = append(allPackets, passivePackets...)
	err = e.validateInputAssets(ctx, pkt, allPackets)
	if err != nil {
		return nil, fmt.Errorf("error validating input assets: %w", err)
	}
	err = tapsend.ValidateAnchorOutputs(pkt, allPackets, true)
	if err != nil {
		return nil, fmt.Errorf("error validating anchor outputs: %w",
			err)
	}

	if req.SignAnchorInputs {
		err = e.signAnchorInputs(ctx, pkt, allPackets)
		if err != nil {
			return nil, err
		}
	}

	chainFees, err := pkt.GetTxFee()
	if err != nil {
		return nil, fmt.Errorf("error calculating transaction fees: %w",
			err)
	}
	finalTx, err := psbt.Extract(pkt)
	if err != nil {
		return nil, fmt.Errorf("error extracting final anchor "+
			"transaction: %w", err)
	}

	label := req.Label
	if label == "" {
		label = fmt.Sprintf("pubandlog-%s", finalTx.TxHash())
	}

	transfer, err := e.shipTransfer(ctx, &shipment{
		anchorPkt:      pkt,
		anchorTx:       finalTx,
		chainFees:      int64(chainFees),
		activePackets:  activePackets,
		passivePackets: passivePackets,
		label:          label,
		skipBroadcast:  req.SkipAnchorTxBroadcast,
	})
	if err != nil {
		return nil, fmt.Errorf("error requesting delivery: %w", err)
	}

	return &taprpc.SendAssetResponse{Transfer: transfer}, nil
}

// signAnchorInputs signs and finalizes the unsigned inputs of the anchor
// transaction that spend anchor outputs of the wallet.
func (e *engine) signAnchorInputs(ctx context.Context, pkt *psbt.Packet,
	vPackets []*tappsbt.VPacket) error {

	unsigned, err := tapsend.UnsignedAnchorInputs(pkt, vPackets)
	if err != nil {
		return err
	}
	if len(unsigned) == 0 {
		return nil
	}

	held, err := e.ownedAssets(ctx, true, fn.None[asset.ScriptKeyType]())
	if err != nil {
		return err
	}

	anchors := make([]tapsend.OwnedAnchorInput, 0, len(unsigned))
	for _, idx := range unsigned {
		op := pkt.UnsignedTx.TxIn[idx].PreviousOutPoint

		var anchor *holding
		for _, h := range held {
			if h.chainAsset.AnchorOutpoint == op {
				anchor = h

				break
			}
		}
		if anchor == nil {
			return fmt.Errorf("anchor input %d (%v) is not "+
				"managed by this wallet", idx, op)
		}

		keyDesc, err := e.internalKeyDescriptor(
			ctx, anchor.chainAsset.AnchorInternalKey,
		)
		if err != nil {
			return fmt.Errorf("anchor input %d (%v) is not owned "+
				"by this wallet: %w", idx, op, err)
		}

		anchors = append(anchors, tapsend.OwnedAnchorInput{
			Index:       idx,
			InternalKey: keyDesc,
			MerkleRoot:  anchor.chainAsset.AnchorMerkleRoot,
			Value:       btcutilAmount(anchor.anchorValue),
		})
	}

	return tapsend.SignAnchorInputs(
		ctx, pkt, anchors, e.params.HDCoinType, e.signAnchorPsbt,
	)
}

// RemoveUTXOLease succeeds without doing anything: the engine leases no
// anchor outputs.
func (s *walletKitServer) RemoveUTXOLease(context.Context,
	*wrpc.RemoveUTXOLeaseRequest) (*wrpc.RemoveUTXOLeaseResponse, error) {

	return &wrpc.RemoveUTXOLeaseResponse{}, nil
}

// transitionProofOptions maps the requested transition proof version to proof
// generation options. The enum's zero value selects the default version.
func transitionProofOptions(
	version wrpc.TransitionProofVersion) ([]proof.GenOption, error) {

	switch version {
	case wrpc.TransitionProofVersion_TRANSITION_PROOF_VERSION_V0:
		return nil, nil

	case wrpc.TransitionProofVersion_TRANSITION_PROOF_VERSION_V1:
		return []proof.GenOption{
			proof.WithVersion(proof.TransitionV1),
		}, nil

	default:
		return nil, status.Errorf(codes.InvalidArgument,
			"unsupported transition proof version: %v", version)
	}
}

// decodeVirtualPackets decodes serialized virtual packets.
func decodeVirtualPackets(rawPackets [][]byte) ([]*tappsbt.VPacket, error) {
	packets := make([]*tappsbt.VPacket, len(rawPackets))
	for idx := range rawPackets {
		var err error
		packets[idx], err = tappsbt.Decode(rawPackets[idx])
		if err != nil {
			return nil, fmt.Errorf("error decoding virtual packet "+
				"at index %d: %w", idx, err)
		}
	}

	return packets, nil
}

// encodeVirtualPackets serializes virtual packets.
func encodeVirtualPackets(packets []*tappsbt.VPacket) ([][]byte, error) {
	rawPackets := make([][]byte, len(packets))
	for idx := range packets {
		var err error
		rawPackets[idx], err = tappsbt.Encode(packets[idx])
		if err != nil {
			return nil, fmt.Errorf("error serializing packet: %w",
				err)
		}
	}

	return rawPackets, nil
}
