package embedded

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/mssmt"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/rpcutils"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/tapsend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// transferPayloadVersion is the version of the transfer payload
	// encoding.
	transferPayloadVersion = 0

	// payloadFlagSkipBroadcast marks a transfer whose anchor transaction
	// the caller broadcasts.
	payloadFlagSkipBroadcast = 1 << 0

	// maxPayloadField bounds one length-prefixed field of a payload.
	maxPayloadField = 512 * 1024 * 1024
)

// shipment is a committed and signed transfer ready to be recorded.
type shipment struct {
	anchorPkt      *psbt.Packet
	anchorTx       *wire.MsgTx
	chainFees      int64
	activePackets  []*tappsbt.VPacket
	passivePackets []*tappsbt.VPacket
	label          string
	skipBroadcast  bool
}

// transferPayload is what the engine stores with a recorded transfer.
type transferPayload struct {
	// transfer is the transfer as ListTransfers returns it.
	transfer *taprpc.AssetTransfer

	// skipBroadcast is set when the caller broadcasts the anchor
	// transaction.
	skipBroadcast bool

	// passiveSuffixes are the encoded proof suffixes of the passive
	// assets the transfer re-anchors.
	passiveSuffixes [][]byte
}

// encode serializes the payload.
func (p *transferPayload) encode() ([]byte, error) {
	transferBytes, err := proto.Marshal(p.transfer)
	if err != nil {
		return nil, fmt.Errorf("encode transfer: %w", err)
	}

	var flags byte
	if p.skipBroadcast {
		flags |= payloadFlagSkipBroadcast
	}

	var buf bytes.Buffer
	buf.WriteByte(transferPayloadVersion)
	buf.WriteByte(flags)
	writePayloadField(&buf, transferBytes)

	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(p.passiveSuffixes)))
	buf.Write(count[:])
	for _, suffix := range p.passiveSuffixes {
		writePayloadField(&buf, suffix)
	}

	return buf.Bytes(), nil
}

// writePayloadField writes a length-prefixed field.
func writePayloadField(buf *bytes.Buffer, field []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(field)))
	buf.Write(length[:])
	buf.Write(field)
}

// readPayloadField reads a length-prefixed field.
func readPayloadField(r *bytes.Reader) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint32(length[:])
	if size > maxPayloadField || int64(size) > int64(r.Len()) {
		return nil, fmt.Errorf("payload field of %d bytes exceeds "+
			"the payload", size)
	}

	field := make([]byte, size)
	if _, err := io.ReadFull(r, field); err != nil {
		return nil, err
	}

	return field, nil
}

// decodeTransferPayload parses a stored transfer payload.
func decodeTransferPayload(encoded []byte) (*transferPayload, error) {
	r := bytes.NewReader(encoded)

	version, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("decode transfer payload: %w", err)
	}
	if version != transferPayloadVersion {
		return nil, fmt.Errorf("unknown transfer payload version %d",
			version)
	}
	flags, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("decode transfer payload: %w", err)
	}

	transferBytes, err := readPayloadField(r)
	if err != nil {
		return nil, fmt.Errorf("decode transfer payload: %w", err)
	}
	transfer := &taprpc.AssetTransfer{}
	if err := proto.Unmarshal(transferBytes, transfer); err != nil {
		return nil, fmt.Errorf("decode transfer: %w", err)
	}

	var count [4]byte
	if _, err := io.ReadFull(r, count[:]); err != nil {
		return nil, fmt.Errorf("decode transfer payload: %w", err)
	}
	numPassive := binary.BigEndian.Uint32(count[:])
	if int64(numPassive) > int64(r.Len()) {
		return nil, fmt.Errorf("transfer payload claims %d passive "+
			"assets", numPassive)
	}

	payload := &transferPayload{
		transfer:      transfer,
		skipBroadcast: flags&payloadFlagSkipBroadcast != 0,
	}
	for range numPassive {
		suffix, err := readPayloadField(r)
		if err != nil {
			return nil, fmt.Errorf("decode transfer payload: %w",
				err)
		}
		payload.passiveSuffixes = append(
			payload.passiveSuffixes, suffix,
		)
	}

	return payload, nil
}

// shipTransfer records a transfer, broadcasts its anchor transaction unless
// the caller does, and follows it to confirmation. A transfer with a recorded
// anchor transaction is returned as recorded and broadcast again.
func (e *engine) shipTransfer(ctx context.Context,
	s *shipment) (*taprpc.AssetTransfer, error) {

	txid := s.anchorTx.TxHash()

	rpcTransfer, pending, err := e.recordTransfer(ctx, s)
	if err != nil {
		return nil, err
	}

	// The transfer is recorded: from here on a failure leaves it to be
	// published again, which the status tells the caller.
	if pending && !s.skipBroadcast {
		err := e.chain.PublishTransaction(ctx, s.anchorTx, s.label)
		if err != nil {
			e.track(txid, false)

			return nil, status.Errorf(codes.Unavailable, "unable "+
				"to broadcast transaction %v: %v", txid, err)
		}
	}
	e.track(txid, false)

	return rpcTransfer, nil
}

// recordTransfer verifies a transfer and stores it as pending. It returns the
// recorded transfer and whether it still awaits confirmation.
func (e *engine) recordTransfer(ctx context.Context,
	s *shipment) (*taprpc.AssetTransfer, bool, error) {

	e.transferMu.Lock()
	defer e.transferMu.Unlock()

	txid := s.anchorTx.TxHash()
	recorded, err := e.store.ListTransfers(ctx, &txid)
	if err != nil {
		return nil, false, err
	}
	if len(recorded) > 0 {
		payload, err := decodeTransferPayload(recorded[0].Payload)
		if err != nil {
			return nil, false, err
		}

		return payload.transfer,
			recorded[0].State == TransferPending, nil
	}

	allPackets := append([]*tappsbt.VPacket{}, s.activePackets...)
	allPackets = append(allPackets, s.passivePackets...)
	if err := tapsend.AssertInputsUnique(allPackets); err != nil {
		return nil, false, invalidTransfer(err)
	}

	// Every input must have its proof file in the store: the final
	// proofs of the outputs extend those files.
	var (
		inputFiles = make(map[asset.PrevID]*proof.File)
		spends     []ProofLocator
	)
	for _, vPkt := range allPackets {
		for _, vIn := range vPkt.Inputs {
			record, file, err := e.fetchInputProof(ctx, vIn.PrevID)
			if err != nil {
				return nil, false, err
			}
			if record.Owned && record.Spent {
				return nil, false, status.Errorf(
					codes.FailedPrecondition, "asset "+
						"input %v is spent by a "+
						"recorded transfer",
					vIn.PrevID.OutPoint,
				)
			}
			if record.Owned {
				spends = append(spends, record.ProofLocator)
			}
			inputFiles[vIn.PrevID] = file
		}
	}

	err = e.verifyPacketsPreBroadcast(ctx, allPackets, inputFiles)
	if err != nil {
		return nil, false, invalidTransfer(err)
	}

	heightHint, _, err := e.chain.BestBlock(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("unable to get current "+
			"height: %w", err)
	}

	now := time.Now()
	rpcTransfer, err := e.marshalShipment(ctx, s, heightHint, now)
	if err != nil {
		return nil, false, invalidTransfer(err)
	}

	payload := &transferPayload{
		transfer:      rpcTransfer,
		skipBroadcast: s.skipBroadcast,
	}
	for _, vPkt := range s.passivePackets {
		for _, vOut := range vPkt.Outputs {
			if vOut.ProofSuffix == nil {
				return nil, false, invalidTransfer(fmt.Errorf(
					"no proof suffix for passive asset",
				))
			}
			suffix, err := vOut.ProofSuffix.Bytes()
			if err != nil {
				return nil, false, err
			}
			payload.passiveSuffixes = append(
				payload.passiveSuffixes, suffix,
			)
		}
	}
	encoded, err := payload.encode()
	if err != nil {
		return nil, false, err
	}

	err = e.store.InsertTransfer(ctx, Transfer{
		AnchorTxid: txid,
		State:      TransferPending,
		HeightHint: heightHint,
		CreatedAt:  now,
		Payload:    encoded,
	}, spends)
	if err != nil {
		return nil, false, fmt.Errorf("unable to record transfer: %w",
			err)
	}

	return rpcTransfer, true, nil
}

// invalidTransfer marks an error that rejected a transfer before anything was
// recorded.
func invalidTransfer(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}

	return status.Error(codes.InvalidArgument, err.Error())
}

// fetchInputProof returns the stored proof file of a transfer input.
func (e *engine) fetchInputProof(ctx context.Context,
	prevID asset.PrevID) (*AssetProof, *proof.File, error) {

	locator, err := prevIDLocator(prevID)
	if err != nil {
		return nil, nil, invalidTransfer(err)
	}

	records, err := e.store.FetchProofs(
		ctx, locator.AssetID, locator.ScriptKey, &locator.Outpoint,
	)
	if err != nil {
		return nil, nil, err
	}
	if len(records) != 1 {
		return nil, nil, status.Errorf(codes.FailedPrecondition,
			"no proof stored for asset input %v, register it "+
				"with RegisterTransfer first", prevID.OutPoint)
	}

	file, err := proof.DecodeFile(records[0].ProofFile)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode input proof "+
			"file: %w", err)
	}

	return &records[0], file, nil
}

// verifyPacketsPreBroadcast checks the packets as far as an unconfirmed
// transfer allows: the inclusion of every input, the split commitment
// witnesses and every output's proof suffix against its input files.
func (e *engine) verifyPacketsPreBroadcast(ctx context.Context,
	packets []*tappsbt.VPacket,
	inputFiles map[asset.PrevID]*proof.File) error {

	vCtx := e.verifierCtx(ctx)
	verifier := &proof.BaseVerifier{}

	for pktIdx, vPkt := range packets {
		for inputIdx, vIn := range vPkt.Inputs {
			if vIn.Proof == nil {
				return fmt.Errorf("packet input proof is nil "+
					"(vpkt_idx=%d, input_idx=%d)", pktIdx,
					inputIdx)
			}

			_, err := vIn.Proof.VerifyProofIntegrity(ctx, vCtx)
			if err != nil {
				return fmt.Errorf("unable to verify inclusion "+
					"proof for packet input (vpkt_idx=%d, "+
					"input_idx=%d): %w", pktIdx, inputIdx,
					err)
			}
		}

		err := verifySplitCommitmentWitnesses(vPkt)
		if err != nil {
			return fmt.Errorf("verify split commitment witnesses "+
				"(vpkt_idx=%d): %w", pktIdx, err)
		}

		for outIdx, vOut := range vPkt.Outputs {
			if vOut.ProofSuffix == nil || vOut.Asset == nil {
				return fmt.Errorf("output asset or proof "+
					"suffix is nil (vpkt_idx=%d, "+
					"output_idx=%d)", pktIdx, outIdx)
			}

			files := outputInputFiles(vPkt, vOut, inputFiles)
			if len(files) == 0 {
				return fmt.Errorf("no inputs matched output "+
					"witnesses (vpkt_idx=%d, "+
					"output_idx=%d)", pktIdx, outIdx)
			}

			_, err := proof.VerifyProofSuffix(
				ctx, vOut.ProofSuffix, files, verifier, vCtx,
			)
			if err != nil {
				return fmt.Errorf("output proof verification "+
					"failed (vpkt_idx=%d, output_idx=%d): "+
					"%w", pktIdx, outIdx, err)
			}
		}
	}

	return nil
}

// outputInputFiles returns the proof files of the packet inputs an output's
// asset spends.
func outputInputFiles(vPkt *tappsbt.VPacket, vOut *tappsbt.VOutput,
	inputFiles map[asset.PrevID]*proof.File) map[asset.PrevID]*proof.File {

	spent := make(map[asset.PrevID]struct{})
	for _, witness := range vOut.Asset.Witnesses() {
		if witness.PrevID != nil {
			spent[*witness.PrevID] = struct{}{}
		}
	}

	files := make(map[asset.PrevID]*proof.File)
	for _, vIn := range vPkt.Inputs {
		if _, ok := spent[vIn.PrevID]; ok {
			files[vIn.PrevID] = inputFiles[vIn.PrevID]
		}
	}

	return files
}

// verifySplitCommitmentWitnesses checks that every split output embeds a
// split root that carries a witness.
func verifySplitCommitmentWitnesses(vPkt *tappsbt.VPacket) error {
	for outIdx, vOut := range vPkt.Outputs {
		if vOut.Asset == nil ||
			!vOut.Asset.HasSplitCommitmentWitness() {

			continue
		}

		splitCommitment := vOut.Asset.PrevWitnesses[0].SplitCommitment
		if splitCommitment == nil {
			return fmt.Errorf("output missing split commitment "+
				"(output_idx=%d)", outIdx)
		}

		root := &splitCommitment.RootAsset
		hasWitness := fn.Any(
			root.PrevWitnesses, func(wit asset.Witness) bool {
				return len(wit.TxWitness) > 0
			},
		)
		if !hasWitness {
			return fmt.Errorf("output split root witness empty "+
				"(output_idx=%d)", outIdx)
		}
	}

	return nil
}

// isLocalScriptKey reports whether an output's script key is one the wallet
// can sign for.
func (e *engine) isLocalScriptKey(ctx context.Context,
	scriptKey asset.ScriptKey) (bool, error) {

	stored, err := e.fetchScriptKey(ctx, scriptKey.PubKey)
	switch {
	case errors.Is(err, address.ErrScriptKeyNotFound):
		return scriptKey.TweakedScriptKey != nil &&
			e.IsLocalKey(ctx, scriptKey.RawKey), nil

	case err != nil:
		return false, fmt.Errorf("error fetching script key: %w", err)
	}

	return e.IsLocalKey(ctx, stored.RawKey), nil
}

// marshalShipment builds the transfer ListTransfers returns for a shipment.
// Passive assets are not listed, as in tapd.
func (e *engine) marshalShipment(ctx context.Context, s *shipment,
	heightHint uint32, now time.Time) (*taprpc.AssetTransfer, error) {

	txid := s.anchorTx.TxHash()

	var numPassive uint32
	for _, vPkt := range s.passivePackets {
		numPassive += uint32(len(vPkt.Outputs))
	}
	passiveAnchorIndex := func() (uint32, bool) {
		if len(s.passivePackets) == 0 ||
			len(s.passivePackets[0].Outputs) == 0 {

			return 0, false
		}

		return s.passivePackets[0].Outputs[0].AnchorOutputIndex, true
	}

	var rpcInputs []*taprpc.TransferInput
	for _, vPkt := range s.activePackets {
		for _, vIn := range vPkt.Inputs {
			if vIn.PrevID == (asset.PrevID{}) {
				return nil, fmt.Errorf("invalid input, prev " +
					"id missing")
			}

			inAsset := vIn.Asset()
			rpcInputs = append(rpcInputs, &taprpc.TransferInput{
				AnchorPoint: vIn.PrevID.OutPoint.String(),
				AssetId:     vIn.PrevID.ID[:],
				ScriptKey:   vIn.PrevID.ScriptKey[:],
				Amount:      inAsset.Amount,
				GroupKey:    groupKeyBytes(inAsset),
				AssetType:   taprpc.AssetType(inAsset.Type),
			})
		}
	}

	var rpcOutputs []*taprpc.TransferOutput
	for _, vPkt := range s.activePackets {
		for vOutIdx, vOut := range vPkt.Outputs {
			if vOut.Asset == nil || vOut.ProofSuffix == nil {
				return nil, fmt.Errorf("invalid output %d, "+
					"asset or proof missing", vOutIdx)
			}
			if int(vOut.AnchorOutputIndex) >=
				len(s.anchorTx.TxOut) {

				return nil, fmt.Errorf("invalid output %d, "+
					"anchor output out of range", vOutIdx)
			}

			proofSuffix, err := vOut.ProofSuffix.Bytes()
			if err != nil {
				return nil, fmt.Errorf("unable to encode "+
					"proof %d: %w", vOutIdx, err)
			}

			sibling, _, err := commitment.
				MaybeEncodeTapscriptPreimage(
					vOut.AnchorOutputTapscriptSibling,
				)
			if err != nil {
				return nil, fmt.Errorf("unable to encode "+
					"tapscript preimage: %w", err)
			}

			anchorOut := &s.anchorPkt.Outputs[vOut.AnchorOutputIndex]
			merkleRoot := tappsbt.ExtractCustomField(
				anchorOut.Unknowns,
				tappsbt.PsbtKeyTypeOutputTaprootMerkleRoot,
			)
			assetRoot := tappsbt.ExtractCustomField(
				anchorOut.Unknowns,
				tappsbt.PsbtKeyTypeOutputAssetRoot,
			)

			var passiveHere uint32
			idx, ok := passiveAnchorIndex()
			if ok && idx == vOut.AnchorOutputIndex {
				passiveHere = numPassive
			}

			txOut := s.anchorTx.TxOut[vOut.AnchorOutputIndex]
			rpcAnchor := &taprpc.TransferOutputAnchor{
				Outpoint: wire.OutPoint{
					Hash:  txid,
					Index: vOut.AnchorOutputIndex,
				}.String(),
				Value: txOut.Value,
				InternalKey: vOut.AnchorOutputInternalKey.
					SerializeCompressed(),
				TaprootAssetRoot: assetRoot,
				MerkleRoot:       merkleRoot,
				TapscriptSibling: sibling,
				NumPassiveAssets: passiveHere,
				PkScript:         txOut.PkScript,
			}

			var splitCommitRoot []byte
			if root := vOut.Asset.SplitCommitmentRoot; root != nil {
				hash := root.NodeHash()
				if hash != mssmt.ZeroNodeHash {
					splitCommitRoot = hash[:]
				}
			}

			rpcOutType, err := marshalOutputType(vOut.Type)
			if err != nil {
				return nil, err
			}
			assetVersion, err := rpcutils.MarshalAssetVersion(
				vOut.AssetVersion,
			)
			if err != nil {
				return nil, err
			}

			local, err := e.isLocalScriptKey(ctx, vOut.ScriptKey)
			if err != nil {
				return nil, err
			}

			outAssetID := vOut.Asset.ID()
			scriptKey := vOut.ScriptKey.PubKey
			rpcOutputs = append(rpcOutputs, &taprpc.TransferOutput{
				Anchor:              rpcAnchor,
				ScriptKey:           scriptKey.SerializeCompressed(),
				ScriptKeyIsLocal:    local,
				Amount:              vOut.Amount,
				LockTime:            vOut.Asset.LockTime,
				RelativeLockTime:    vOut.Asset.RelativeLockTime,
				NewProofBlob:        proofSuffix,
				SplitCommitRootHash: splitCommitRoot,
				OutputType:          rpcOutType,
				AssetVersion:        assetVersion,
				ProofDeliveryStatus: rpcutils.
					ProofDeliveryStatusNotApplicable,
				AssetId:   outAssetID[:],
				GroupKey:  groupKeyBytes(vOut.Asset),
				AssetType: taprpc.AssetType(vOut.Asset.Type),
			})
		}
	}

	anchorTxBytes, err := fn.Serialize(s.anchorTx)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize anchor tx: %w",
			err)
	}

	return &taprpc.AssetTransfer{
		TransferTimestamp:  now.Unix(),
		AnchorTxHash:       txid[:],
		AnchorTxHeightHint: heightHint,
		AnchorTxChainFees:  s.chainFees,
		Inputs:             rpcInputs,
		Outputs:            rpcOutputs,
		Label:              s.label,
		AnchorTx:           anchorTxBytes,
	}, nil
}

// groupKeyBytes returns the serialized group key of an asset, if it has one.
func groupKeyBytes(a *asset.Asset) []byte {
	if a == nil || a.GroupKey == nil {
		return nil
	}

	return a.GroupKey.GroupPubKey.SerializeCompressed()
}

// marshalOutputType turns the transfer output type into the RPC counterpart.
func marshalOutputType(outputType tappsbt.VOutputType) (taprpc.OutputType,
	error) {

	switch outputType {
	case tappsbt.TypeSimple:
		return taprpc.OutputType_OUTPUT_TYPE_SIMPLE, nil

	case tappsbt.TypeSplitRoot:
		return taprpc.OutputType_OUTPUT_TYPE_SPLIT_ROOT, nil

	default:
		return 0, fmt.Errorf("unknown output type: %d", outputType)
	}
}

// transferInputs returns the previous IDs of a recorded transfer's inputs, in
// transfer order.
func transferInputs(transfer *taprpc.AssetTransfer) ([]asset.PrevID, error) {
	prevIDs := make([]asset.PrevID, 0, len(transfer.Inputs))
	for _, in := range transfer.Inputs {
		op, err := wire.NewOutPointFromString(in.AnchorPoint)
		if err != nil {
			return nil, fmt.Errorf("decode input anchor point: %w",
				err)
		}
		if len(in.AssetId) != len(asset.ID{}) ||
			len(in.ScriptKey) != btcec.PubKeyBytesLenCompressed {

			return nil, fmt.Errorf("malformed transfer input")
		}

		prevID := asset.PrevID{OutPoint: *op}
		copy(prevID.ID[:], in.AssetId)
		copy(prevID.ScriptKey[:], in.ScriptKey)
		prevIDs = append(prevIDs, prevID)
	}

	return prevIDs, nil
}

// confirmTransfer writes the final proofs of a transfer whose anchor
// transaction confirmed and marks it confirmed.
func (e *engine) confirmTransfer(ctx context.Context, txid chainhash.Hash,
	conf *Confirmation) error {

	if conf == nil || conf.Block == nil {
		return fmt.Errorf("confirmation of %v carries no block", txid)
	}

	e.transferMu.Lock()
	defer e.transferMu.Unlock()

	transfer, payload, err := e.loadTransfer(ctx, txid)
	if err != nil {
		return err
	}
	if transfer.State == TransferFinal {
		return nil
	}

	var anchorTx wire.MsgTx
	err = anchorTx.Deserialize(bytes.NewReader(payload.transfer.AnchorTx))
	if err != nil {
		return fmt.Errorf("decode anchor transaction: %w", err)
	}

	txIndex := -1
	for idx, blockTx := range conf.Block.Transactions {
		if blockTx.TxHash() == txid {
			txIndex = idx

			break
		}
	}
	if txIndex < 0 {
		return fmt.Errorf("anchor transaction %v is not in block %v",
			txid, conf.Block.BlockHash())
	}
	merkleProof, err := proof.NewTxMerkleProof(
		conf.Block.Transactions, txIndex,
	)
	if err != nil {
		return fmt.Errorf("build merkle proof: %w", err)
	}

	inputs, err := transferInputs(payload.transfer)
	if err != nil {
		return err
	}

	// An input file must end in a block of the best chain. After a
	// re-org, the transfer that created the input rewrites the file once
	// it confirms again, so a stale file is retried rather than extended.
	fetchInput := func(prevID asset.PrevID) (*proof.File, error) {
		_, file, err := e.fetchInputProof(ctx, prevID)
		if err != nil {
			return nil, err
		}
		tip, err := file.LastProof()
		if err != nil {
			return nil, err
		}
		err = e.verifyHeader(ctx, tip.BlockHeader, tip.BlockHeight)
		if err != nil {
			return nil, fmt.Errorf("input proof of %v is stale: "+
				"%w", prevID.OutPoint, err)
		}

		return file, nil
	}

	finalize := func(rawSuffix []byte, candidates []asset.PrevID,
		owned bool) (*AssetProof, error) {

		suffix, err := proof.Decode(rawSuffix)
		if err != nil {
			return nil, fmt.Errorf("decode proof suffix: %w", err)
		}
		if candidates == nil {
			for _, witness := range suffix.Asset.Witnesses() {
				if witness.PrevID != nil {
					candidates = append(
						candidates, *witness.PrevID,
					)
				}
			}
		}

		file, confirmed, err := proof.AppendConfirmedSuffix(
			suffix, &anchorTx, conf.Block.Header, conf.Height,
			*merkleProof, candidates, fetchInput,
		)
		if err != nil {
			return nil, err
		}

		var blob bytes.Buffer
		if err := file.Encode(&blob); err != nil {
			return nil, fmt.Errorf("encode proof file: %w", err)
		}

		return &AssetProof{
			ProofLocator: proofLocator(confirmed),
			ProofFile:    blob.Bytes(),
			Owned:        owned,
		}, nil
	}

	var putProofs []AssetProof
	for _, out := range payload.transfer.Outputs {
		if len(out.NewProofBlob) == 0 {
			continue
		}

		final, err := finalize(
			out.NewProofBlob, inputs, out.ScriptKeyIsLocal,
		)
		if err != nil {
			return err
		}
		putProofs = append(putProofs, *final)
	}
	for _, rawSuffix := range payload.passiveSuffixes {
		final, err := finalize(rawSuffix, nil, true)
		if err != nil {
			return err
		}
		putProofs = append(putProofs, *final)
	}

	blockHash := conf.Block.BlockHash()
	payload.transfer.AnchorTxBlockHash = &taprpc.ChainHash{
		Hash:    blockHash[:],
		HashStr: blockHash.String(),
	}
	payload.transfer.AnchorTxBlockHeight = conf.Height

	encoded, err := payload.encode()
	if err != nil {
		return err
	}

	transfer.State = TransferConfirmed
	transfer.BlockHash = blockHash
	transfer.BlockHeight = conf.Height
	transfer.Payload = encoded

	return e.store.UpdateTransfer(ctx, *transfer, putProofs, nil)
}

// unconfirmTransfer returns a transfer whose confirming block left the chain
// to pending. Its unspent outputs leave the wallet until it confirms again.
func (e *engine) unconfirmTransfer(ctx context.Context,
	txid chainhash.Hash) error {

	e.transferMu.Lock()
	defer e.transferMu.Unlock()

	transfer, payload, err := e.loadTransfer(ctx, txid)
	if err != nil {
		return err
	}
	if transfer.State != TransferConfirmed {
		return nil
	}

	var suffixes [][]byte
	for _, out := range payload.transfer.Outputs {
		if len(out.NewProofBlob) != 0 {
			suffixes = append(suffixes, out.NewProofBlob)
		}
	}
	suffixes = append(suffixes, payload.passiveSuffixes...)

	// A spent output keeps its record, so that it stays spent when the
	// transfer confirms again.
	var deleteProofs []ProofLocator
	for _, rawSuffix := range suffixes {
		suffix, err := proof.Decode(rawSuffix)
		if err != nil {
			return fmt.Errorf("decode proof suffix: %w", err)
		}
		locator := proofLocator(suffix)

		records, err := e.store.FetchProofs(
			ctx, locator.AssetID, locator.ScriptKey,
			&locator.Outpoint,
		)
		if err != nil {
			return err
		}
		if len(records) == 1 && !records[0].Spent {
			deleteProofs = append(deleteProofs, locator)
		}
	}

	payload.transfer.AnchorTxBlockHash = nil
	payload.transfer.AnchorTxBlockHeight = 0
	encoded, err := payload.encode()
	if err != nil {
		return err
	}

	transfer.State = TransferPending
	transfer.BlockHash = chainhash.Hash{}
	transfer.BlockHeight = 0
	transfer.Payload = encoded

	return e.store.UpdateTransfer(ctx, *transfer, nil, deleteProofs)
}

// finishTransfer marks a confirmed transfer as final.
func (e *engine) finishTransfer(ctx context.Context,
	txid chainhash.Hash) error {

	e.transferMu.Lock()
	defer e.transferMu.Unlock()

	transfer, _, err := e.loadTransfer(ctx, txid)
	if err != nil {
		return err
	}
	if transfer.State != TransferConfirmed {
		return fmt.Errorf("transfer %v is not confirmed", txid)
	}
	transfer.State = TransferFinal

	return e.store.UpdateTransfer(ctx, *transfer, nil, nil)
}

// loadTransfer returns a recorded transfer and its decoded payload.
func (e *engine) loadTransfer(ctx context.Context,
	txid chainhash.Hash) (*Transfer, *transferPayload, error) {

	transfers, err := e.store.ListTransfers(ctx, &txid)
	if err != nil {
		return nil, nil, err
	}
	if len(transfers) != 1 {
		return nil, nil, fmt.Errorf("transfer %v: %w", txid,
			ErrNotFound)
	}

	payload, err := decodeTransferPayload(transfers[0].Payload)
	if err != nil {
		return nil, nil, err
	}

	return &transfers[0], payload, nil
}
