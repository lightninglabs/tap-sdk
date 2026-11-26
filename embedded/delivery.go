package embedded

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/rpcutils"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/taprpc"
)

// courierCallTimeout bounds one call to the courier of a recipient.
const courierCallTimeout = 30 * time.Second

// maxDeliveryInterval caps the wait between two attempts to deliver the
// proofs of a transfer, which starts at retryInterval and doubles.
var maxDeliveryInterval = 10 * time.Minute

// delivery is the hand-over of one anchor output of a transfer to the
// recipient whose address it pays: the proofs of its assets go to the
// universe of the address's courier, and an announcement to its mailbox.
type delivery struct {
	// address is the encoded address the output pays.
	address string

	// anchorIndex is the index of the output in the anchor transaction.
	anchorIndex uint32

	// done is set once the courier holds the proofs and the announcement.
	done bool
}

// encode appends the delivery to a transfer payload.
func (d *delivery) encode(buf *bytes.Buffer) {
	writePayloadField(buf, []byte(d.address))

	var anchorIndex [4]byte
	binary.BigEndian.PutUint32(anchorIndex[:], d.anchorIndex)
	buf.Write(anchorIndex[:])

	var done byte
	if d.done {
		done = 1
	}
	buf.WriteByte(done)
}

// decodeDelivery reads a delivery of a transfer payload.
func decodeDelivery(r *bytes.Reader) (*delivery, error) {
	encoded, err := readPayloadField(r)
	if err != nil {
		return nil, err
	}

	var anchorIndex [4]byte
	if _, err := io.ReadFull(r, anchorIndex[:]); err != nil {
		return nil, err
	}
	done, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	return &delivery{
		address:     string(encoded),
		anchorIndex: binary.BigEndian.Uint32(anchorIndex[:]),
		done:        done != 0,
	}, nil
}

// deliveryPending reports whether an encoded transfer payload holds a
// delivery that is not done.
func deliveryPending(payload []byte) bool {
	return len(payload) > 1 && payload[1]&payloadFlagDeliveryPending != 0
}

// deliveryStatus returns the proof delivery status a new transfer lists for
// an output.
func deliveryStatus(vOut *tappsbt.VOutput) taprpc.ProofDeliveryStatus {
	if vOut.Address != nil {
		return rpcutils.ProofDeliveryStatusPending
	}

	return rpcutils.ProofDeliveryStatusNotApplicable
}

// addressDeliveries returns the deliveries of the packets of a transfer: one
// per anchor output that pays an address. It fails for an output the
// recipient of its address could not find or spend.
func addressDeliveries(packets []*tappsbt.VPacket) ([]delivery, error) {
	var (
		deliveries []delivery
		receivers  = make(map[uint32]*btcec.PublicKey)
	)
	for _, vPkt := range packets {
		for _, vOut := range vPkt.Outputs {
			addr := vOut.Address
			if addr == nil {
				continue
			}

			err := validateAddressOutput(vPkt, vOut)
			if err != nil {
				return nil, fmt.Errorf("output at anchor "+
					"output %d: %w", vOut.AnchorOutputIndex,
					err)
			}

			receiver, ok := receivers[vOut.AnchorOutputIndex]
			switch {
			case ok && !receiver.IsEqual(&addr.ScriptKey):
				return nil, fmt.Errorf("anchor output %d pays "+
					"two addresses", vOut.AnchorOutputIndex)

			case ok:
				continue
			}

			encoded, err := addr.EncodeAddress()
			if err != nil {
				return nil, fmt.Errorf("encode address: %w",
					err)
			}
			receivers[vOut.AnchorOutputIndex] = &addr.ScriptKey
			deliveries = append(deliveries, delivery{
				address:     encoded,
				anchorIndex: vOut.AnchorOutputIndex,
			})
		}
	}

	return deliveries, nil
}

// validateAddressOutput checks that an output pays its address as the
// recipient expects: at the script key derived for the asset, in an anchor
// output under the internal key of the address.
func validateAddressOutput(vPkt *tappsbt.VPacket,
	vOut *tappsbt.VOutput) error {

	addr := vOut.Address
	if !addr.UsesSendManifests() {
		return fmt.Errorf("address is of version %d, the embedded "+
			"backend sends to version %d addresses only",
			addr.Version, address.V2)
	}
	if err := validateCourier(&addr.ProofCourierAddr); err != nil {
		return err
	}

	assetID, err := vPkt.AssetID()
	if err != nil {
		return fmt.Errorf("unable to determine asset ID: %w", err)
	}
	scriptKey, err := asset.DeriveUniqueScriptKey(
		addr.ScriptKey, assetID,
		asset.ScriptKeyDerivationUniquePedersen,
	)
	if err != nil {
		return fmt.Errorf("unable to derive unique script key: %w", err)
	}

	switch {
	case vOut.ScriptKey.PubKey == nil ||
		!sameXOnlyKey(vOut.ScriptKey.PubKey, scriptKey.PubKey):

		return errors.New("script key is not the one derived from " +
			"the address")

	case vOut.AnchorOutputInternalKey == nil ||
		!vOut.AnchorOutputInternalKey.IsEqual(&addr.InternalKey):

		return errors.New("anchor output is not under the internal " +
			"key of the address")
	}

	return nil
}

// sender delivers the proofs of the transfers that pay addresses.
type sender struct {
	e    *engine
	dial CourierDialer

	// ctx ends when the engine stops.
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.Mutex

	// running are the transfers that are being delivered. An entry is set
	// when a transfer is to be delivered again while its delivery runs.
	running map[chainhash.Hash]bool
}

// newSender returns the sender of an engine that pays addresses.
func newSender(e *engine, cfg *SendConfig) *sender {
	ctx, cancel := context.WithCancel(context.Background())

	return &sender{
		e:       e,
		dial:    cfg.Dial,
		ctx:     ctx,
		cancel:  cancel,
		running: make(map[chainhash.Hash]bool),
	}
}

// stop ends the deliveries. The caller waits for them to return.
func (s *sender) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cancel()
}

// deliver hands the outputs of a confirmed transfer that pay addresses to
// their recipients, in the background and until every courier took them. A
// transfer that is being delivered is not delivered twice.
func (s *sender) deliver(txid chainhash.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx.Err() != nil {
		return
	}
	if _, ok := s.running[txid]; ok {
		s.running[txid] = true

		return
	}
	s.running[txid] = false

	s.e.wg.Go(func() {
		for {
			s.run(s.ctx, txid)

			s.mu.Lock()
			again := s.running[txid] && s.ctx.Err() == nil
			if !again {
				delete(s.running, txid)
			} else {
				s.running[txid] = false
			}
			s.mu.Unlock()

			if !again {
				return
			}
		}
	})
}

// run attempts the deliveries of a transfer until none is left, with a wait
// between two attempts that doubles up to maxDeliveryInterval.
func (s *sender) run(ctx context.Context, txid chainhash.Hash) {
	wait := retryInterval
	for {
		done, err := s.attempt(ctx, txid)
		if done || ctx.Err() != nil {
			return
		}
		s.e.log.Errorf("Delivering the proofs of transfer %v: %v", txid,
			err)

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
		wait = min(2*wait, maxDeliveryInterval)
	}
}

// attempt works once on every delivery of a transfer that is not done. It
// reports whether none is left to this run. The sender delivers confirmed
// transfers only: a transfer that is pending again has no proofs to deliver,
// and its next confirmation starts a delivery.
func (s *sender) attempt(ctx context.Context, txid chainhash.Hash) (bool,
	error) {

	transfer, payload, err := s.e.loadTransfer(ctx, txid)
	if err != nil {
		return false, err
	}
	if transfer.State == TransferPending {
		return true, nil
	}

	var errs []error
	for idx := range payload.deliveries {
		pending := &payload.deliveries[idx]
		if pending.done {
			continue
		}

		err := s.deliverOutput(ctx, txid, payload.transfer, pending)
		if err == nil {
			err = s.e.markDelivered(ctx, txid, pending.anchorIndex)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("anchor output %d: %w",
				pending.anchorIndex, err))

			continue
		}

		s.e.log.Infof("Delivered the proofs of output %d of transfer "+
			"%v", pending.anchorIndex, txid)
	}

	return len(errs) == 0, errors.Join(errs...)
}

// universeLeaf is one proof to store in the universe of a courier.
type universeLeaf struct {
	key  ProofKey
	blob []byte
}

// leafID identifies a universe leaf by value. The group of an asset follows
// from its ID.
type leafID struct {
	assetID   [32]byte
	scriptKey [33]byte
	outpoint  wire.OutPoint
}

// id returns the identity of the leaf.
func (l *universeLeaf) id() leafID {
	return leafID{
		assetID:   l.key.AssetID,
		scriptKey: l.key.ScriptKey,
		outpoint:  l.key.Outpoint,
	}
}

// deliverOutput stores the proofs of the assets in one anchor output in the
// universe of the courier its address names, and then announces the output in
// the mailbox of the address, as tapd does: a recipient that reads the
// announcement finds the proofs.
func (s *sender) deliverOutput(ctx context.Context, txid chainhash.Hash,
	transfer *taprpc.AssetTransfer, pending *delivery) error {

	addr, err := address.DecodeAddress(pending.address, &s.e.params)
	if err != nil {
		return fmt.Errorf("decode address: %w", err)
	}
	outpoint := wire.OutPoint{Hash: txid, Index: pending.anchorIndex}

	var (
		fragment = &proof.SendFragment{
			Version:  proof.SendFragmentV1,
			OutPoint: outpoint,
			Outputs:  make(map[asset.ID]proof.SendOutput),
		}
		anchor *AnchorProof
		leaves []universeLeaf
		seen   = make(map[leafID][sha256.Size]byte)
	)
	for _, out := range transfer.Outputs {
		if !deliversOutput(out, outpoint) {
			continue
		}

		file, tip, err := s.outputProof(ctx, out, outpoint)
		if err != nil {
			return err
		}

		fragment.BlockHeader = tip.BlockHeader
		fragment.BlockHeight = tip.BlockHeight
		copy(fragment.TaprootAssetRoot[:], out.Anchor.TaprootAssetRoot)
		fragment.Outputs[tip.Asset.ID()] = proof.SendOutput{
			AssetVersion: tip.Asset.Version,
			Amount:       tip.Asset.Amount,
			DerivationMethod: asset.
				ScriptKeyDerivationUniquePedersen,
			ScriptKey: asset.ToSerialized(
				tip.Asset.ScriptKey.PubKey,
			),
		}
		anchor = anchorProof(tip, out.Anchor.MerkleRoot)

		err = collectLeaves(file, func(leaf universeLeaf) error {
			hash := sha256.Sum256(leaf.blob)
			known, ok := seen[leaf.id()]
			switch {
			case ok && known != hash:
				return fmt.Errorf("two proofs for universe "+
					"leaf %v", leaf.key.Outpoint)

			case ok:
				return nil
			}
			seen[leaf.id()] = hash
			leaves = append(leaves, leaf)

			return nil
		})
		if err != nil {
			return err
		}
	}
	if err := fragment.Validate(); err != nil {
		return fmt.Errorf("send fragment: %w", err)
	}

	payload, err := proof.EncryptSendFragment(fragment, &addr.ScriptKey)
	if err != nil {
		return fmt.Errorf("encrypt send fragment: %w", err)
	}

	courier, err := s.dial(ctx, &addr.ProofCourierAddr)
	if err != nil {
		return fmt.Errorf("connect to courier %v: %w",
			addr.ProofCourierAddr.Host, err)
	}
	defer func() {
		if err := courier.Close(); err != nil {
			s.e.log.Errorf("Closing courier %v: %v",
				addr.ProofCourierAddr.Host, err)
		}
	}()

	for _, leaf := range leaves {
		if err := s.insertProof(ctx, courier, leaf); err != nil {
			return err
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, courierCallTimeout)
	defer cancel()

	err = courier.SendMessage(
		callCtx, asset.ToSerialized(&addr.ScriptKey), payload, anchor,
	)
	if err != nil {
		return fmt.Errorf("announce transfer: %w", err)
	}

	return nil
}

// deliversOutput reports whether a transfer output is in the anchor output
// at outpoint and pays an address.
func deliversOutput(out *taprpc.TransferOutput, outpoint wire.OutPoint) bool {
	notApplicable := rpcutils.ProofDeliveryStatusNotApplicable

	return out.Anchor != nil && out.Anchor.Outpoint == outpoint.String() &&
		out.ProofDeliveryStatus != notApplicable
}

// outputProof returns the stored proof file of a transfer output and its
// last proof.
func (s *sender) outputProof(ctx context.Context, out *taprpc.TransferOutput,
	outpoint wire.OutPoint) (*proof.File, *proof.Proof, error) {

	if len(out.AssetId) != len(asset.ID{}) ||
		len(out.ScriptKey) != btcec.PubKeyBytesLenCompressed {

		return nil, nil, errors.New("malformed transfer output")
	}
	scriptKey, err := btcec.ParsePubKey(out.ScriptKey)
	if err != nil {
		return nil, nil, fmt.Errorf("parse script key: %w", err)
	}

	records, err := s.e.store.FetchProofs(
		ctx, asset.ID(out.AssetId), scriptKeyBytes(scriptKey),
		&outpoint,
	)
	if err != nil {
		return nil, nil, err
	}
	if len(records) != 1 {
		return nil, nil, fmt.Errorf("proof of asset %x: %w",
			out.AssetId, ErrNotFound)
	}

	file, err := proof.DecodeFile(records[0].ProofFile)
	if err != nil {
		return nil, nil, fmt.Errorf("decode proof file: %w", err)
	}
	tip, err := file.LastProof()
	if err != nil {
		return nil, nil, fmt.Errorf("decode proof file: %w", err)
	}

	return file, tip, nil
}

// anchorProof returns the proof that the anchor output of a confirmed proof
// exists, with which a mailbox accepts a message about it.
func anchorProof(tip *proof.Proof, merkleRoot []byte) *AnchorProof {
	return &AnchorProof{
		Tx:          &tip.AnchorTx,
		BlockHeader: tip.BlockHeader,
		BlockHeight: tip.BlockHeight,
		MerkleNodes: tip.TxMerkleProof.Nodes,
		MerkleBits:  tip.TxMerkleProof.Bits,
		Outpoint:    tip.OutPoint(),
		InternalKey: asset.ToSerialized(tip.InclusionProof.InternalKey),
		MerkleRoot:  merkleRoot,
	}
}

// collectLeaves visits the universe leaf of every proof of a file. The file
// of an input that a proof merged is visited before that proof, so that a
// universe holds the history of a proof before the proof.
func collectLeaves(file *proof.File, visit func(universeLeaf) error) error {
	for idx := range file.NumProofs() {
		p, err := file.ProofAt(uint32(idx))
		if err != nil {
			return fmt.Errorf("decode proof %d: %w", idx, err)
		}
		blob, err := file.RawProofAt(uint32(idx))
		if err != nil {
			return fmt.Errorf("read proof %d: %w", idx, err)
		}

		for inputIdx := range p.AdditionalInputs {
			input := &p.AdditionalInputs[inputIdx]
			if err := collectLeaves(input, visit); err != nil {
				return err
			}
		}

		leaf := universeLeaf{
			key: ProofKey{
				AssetID: p.Asset.ID(),
				ScriptKey: asset.ToSerialized(
					p.Asset.ScriptKey.PubKey,
				),
				Outpoint: p.OutPoint(),
			},
			blob: blob,
		}
		if p.Asset.GroupKey != nil {
			groupKey := [33]byte(asset.ToSerialized(
				&p.Asset.GroupKey.GroupPubKey,
			))
			leaf.key.GroupKey = &groupKey
		}
		if err := visit(leaf); err != nil {
			return err
		}
	}

	return nil
}

// insertProof stores a proof in the universe of its asset and, for a grouped
// asset, in the universe of the group, where older recipients look for it. A
// recipient finds a proof in either, so a group universe that refuses the
// proof does not stop the delivery.
func (s *sender) insertProof(ctx context.Context, courier Courier,
	leaf universeLeaf) error {

	insert := func(key ProofKey) error {
		callCtx, cancel := context.WithTimeout(ctx, courierCallTimeout)
		defer cancel()

		return courier.InsertProof(callCtx, key, leaf.blob)
	}

	assetLeaf := leaf.key
	assetLeaf.GroupKey = nil
	if err := insert(assetLeaf); err != nil {
		return fmt.Errorf("insert proof of asset %x: %w",
			leaf.key.AssetID[:], err)
	}
	if leaf.key.GroupKey == nil {
		return nil
	}

	err := insert(leaf.key)
	switch {
	case errors.Is(err, ErrProofRefused):
		s.e.log.Infof("Group universe refused the proof of asset %x "+
			"at %v: %v", leaf.key.AssetID[:], leaf.key.Outpoint,
			err)

	case err != nil:
		return fmt.Errorf("insert proof of asset %x into its group "+
			"universe: %w", leaf.key.AssetID[:], err)
	}

	return nil
}

// markDelivered records that the courier holds the proofs and the
// announcement of an anchor output of a transfer.
func (e *engine) markDelivered(ctx context.Context, txid chainhash.Hash,
	anchorIndex uint32) error {

	e.transferMu.Lock()
	defer e.transferMu.Unlock()

	transfer, payload, err := e.loadTransfer(ctx, txid)
	if err != nil {
		return err
	}

	for idx := range payload.deliveries {
		if payload.deliveries[idx].anchorIndex == anchorIndex {
			payload.deliveries[idx].done = true
		}
	}
	outpoint := wire.OutPoint{Hash: txid, Index: anchorIndex}
	for _, out := range payload.transfer.Outputs {
		if deliversOutput(out, outpoint) {
			out.ProofDeliveryStatus =
				rpcutils.ProofDeliveryStatusComplete
		}
	}

	transfer.Payload, err = payload.encode()
	if err != nil {
		return err
	}

	return e.store.UpdateTransfer(ctx, *transfer, nil, nil)
}
