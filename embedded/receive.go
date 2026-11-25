package embedded

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightningnetwork/lnd/keychain"
)

const (
	// receivePayloadVersion is the version of the receive payload
	// encoding.
	receivePayloadVersion = 1

	// receivePayloadNoReorg is the version of the encoding that ends
	// before the re-org marker.
	receivePayloadNoReorg = 0
)

var (
	// errReceiveNotReady marks a receive that waits for its block to
	// reach the chain source or for its proofs to reach the courier.
	errReceiveNotReady = errors.New("receive is not ready")

	// errAnnouncedAgain fails an announcement that names no asset beyond
	// those the wallet holds at the output through other announcements.
	errAnnouncedAgain = errors.New("another announcement of the output " +
		"delivered the assets")
)

// receiveID identifies a receive record.
type receiveID struct {
	outpoint     wire.OutPoint
	address      [33]byte
	announcement [32]byte
}

// idOf returns the ID of a receive record.
func idOf(record *ReceiveRecord) receiveID {
	return receiveID{
		outpoint:     record.Outpoint,
		address:      record.Address,
		announcement: record.Announcement,
	}
}

// String returns the ID as one string.
func (id receiveID) String() string {
	return fmt.Sprintf("%v/%x/%x", id.outpoint, id.address[:],
		id.announcement[:])
}

// announcementOf returns the hash that identifies what a sender announced.
// The same fragment in two mailbox messages is one announcement.
func announcementOf(fragment *proof.SendFragment) ([32]byte, error) {
	var encoded bytes.Buffer
	if err := fragment.Encode(&encoded); err != nil {
		return [32]byte{}, fmt.Errorf("encode send fragment: %w", err)
	}

	return sha256.Sum256(encoded.Bytes()), nil
}

// receivePayload is what the engine stores with a recorded receive.
type receivePayload struct {
	// fragment is the sender's description of the transfer.
	fragment *proof.SendFragment

	// anchorValue is the value of the anchor output in satoshis. It is
	// zero until the receive is confirmed.
	anchorValue int64

	// failure is why the receive failed.
	failure string

	// reorged marks a receive whose confirming block left the chain. Its
	// proofs are stored as not owned, and the tracker of its anchor output
	// confirms it again.
	reorged bool
}

// encode serializes the payload.
func (p *receivePayload) encode() ([]byte, error) {
	var fragment bytes.Buffer
	if err := p.fragment.Encode(&fragment); err != nil {
		return nil, fmt.Errorf("encode send fragment: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteByte(receivePayloadVersion)
	writePayloadField(&buf, fragment.Bytes())

	var anchorValue [8]byte
	binary.BigEndian.PutUint64(anchorValue[:], uint64(p.anchorValue))
	buf.Write(anchorValue[:])
	writePayloadField(&buf, []byte(p.failure))

	var reorged byte
	if p.reorged {
		reorged = 1
	}
	buf.WriteByte(reorged)

	return buf.Bytes(), nil
}

// decodeReceivePayload parses a stored receive payload.
func decodeReceivePayload(encoded []byte) (*receivePayload, error) {
	r := bytes.NewReader(encoded)

	version, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("decode receive payload: %w", err)
	}
	if version > receivePayloadVersion {
		return nil, fmt.Errorf("unknown receive payload version %d",
			version)
	}

	fragmentBytes, err := readPayloadField(r)
	if err != nil {
		return nil, fmt.Errorf("decode receive payload: %w", err)
	}
	fragment, err := proof.DecodeSendFragment(fragmentBytes)
	if err != nil {
		return nil, err
	}
	if err := fragment.Validate(); err != nil {
		return nil, fmt.Errorf("stored send fragment: %w", err)
	}

	var anchorValue [8]byte
	if _, err := io.ReadFull(r, anchorValue[:]); err != nil {
		return nil, fmt.Errorf("decode receive payload: %w", err)
	}
	failure, err := readPayloadField(r)
	if err != nil {
		return nil, fmt.Errorf("decode receive payload: %w", err)
	}

	var reorged byte
	if version != receivePayloadNoReorg {
		reorged, err = r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("decode receive payload: %w",
				err)
		}
	}

	return &receivePayload{
		fragment:    fragment,
		anchorValue: int64(binary.BigEndian.Uint64(anchorValue[:])),
		failure:     string(failure),
		reorged:     reorged != 0,
	}, nil
}

// receiveAddress is a stored address with what the receive flow reads of it.
type receiveAddress struct {
	record AddressRecord
	tap    *address.Tap

	// scriptKey is the address's script key with its derivation path.
	scriptKey keychain.KeyDescriptor
}

// loadAddress decodes a stored address and looks up its script key.
func (r *receiver) loadAddress(ctx context.Context,
	record AddressRecord) (*receiveAddress, error) {

	tap, err := address.DecodeAddress(record.Encoded, &r.e.params)
	if err != nil {
		return nil, fmt.Errorf("decode stored address: %w", err)
	}
	scriptKey, err := r.e.fetchKeyDescriptor(ctx, &tap.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("fetch script key of address: %w", err)
	}

	return &receiveAddress{
		record:    record,
		tap:       tap,
		scriptKey: scriptKey,
	}, nil
}

// address returns the stored address with the given script key.
func (r *receiver) address(ctx context.Context,
	scriptKey [33]byte) (*receiveAddress, error) {

	records, err := r.e.addrs.ListAddresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list addresses: %w", err)
	}
	for idx := range records {
		if records[idx].ScriptKey == scriptKey {
			return r.loadAddress(ctx, records[idx])
		}
	}

	return nil, fmt.Errorf("address %x: %w", scriptKey[:], ErrNotFound)
}

// detect decrypts a mailbox message and records the transfer it announces.
// It returns nil for a message that cannot announce a transfer to the
// address, so that no sender can block the mailbox. A record of the
// announcement that exists is returned as it is.
func (r *receiver) detect(ctx context.Context, addr *receiveAddress,
	msg MailboxMessage) (*ReceiveRecord, error) {

	// A failure of the host's keys says nothing about the message.
	var keyErr error
	fragment, err := proof.DecryptSendFragment(
		ctx, msg.Payload, func(ctx context.Context,
			sender *btcec.PublicKey) ([32]byte, error) {

			var sharedKey [32]byte
			sharedKey, keyErr = r.keys.ECDH(
				ctx, addr.scriptKey.KeyLocator, sender,
			)

			return sharedKey, keyErr
		},
	)
	if err == nil {
		_, err = deriveReceiveOutputs(addr, fragment)
	}
	switch {
	case keyErr != nil:
		return nil, fmt.Errorf("derive shared key: %w", keyErr)

	case err != nil:
		r.e.log.Infof("Skipping mailbox message %d of address %x: %v",
			msg.ID, addr.record.ScriptKey[:], err)

		return nil, nil
	}

	announcement, err := announcementOf(fragment)
	if err != nil {
		return nil, err
	}
	payload, err := (&receivePayload{fragment: fragment}).encode()
	if err != nil {
		return nil, err
	}

	record, err := r.e.addrs.InsertReceive(ctx, ReceiveRecord{
		Outpoint:     fragment.OutPoint,
		Address:      addr.record.ScriptKey,
		Announcement: announcement,
		State:        ReceiveDetected,
		CreatedAt:    time.Now(),
		Payload:      payload,
	})
	if err != nil {
		return nil, fmt.Errorf("record receive: %w", err)
	}

	return record, nil
}

// advance works on a detected receive: it checks the block of the transfer,
// fetches and verifies the proofs and stores them as owned asset outputs. It
// reports whether the receive needs no further work: it is confirmed or
// failed, or its block left the chain and the tracker of its output confirms
// it again. An error leaves the receive detected, for a later call. Concurrent
// calls for one receive share one run.
func (r *receiver) advance(ctx context.Context, id receiveID) (bool, error) {
	done, err, _ := r.runs.Do(id.String(), func() (any, error) {
		return r.advanceOnce(ctx, id)
	})
	if err != nil {
		return false, err
	}

	return done.(bool), nil
}

// load returns the record of a receive, and the assets the wallet holds at
// its output through other announcements.
func (r *receiver) load(ctx context.Context, id receiveID) (*ReceiveRecord,
	map[asset.ID]struct{}, error) {

	records, err := r.e.addrs.ListReceives(ctx, ReceiveFilter{
		Address:  &id.address,
		Outpoint: &id.outpoint,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("fetch receive: %w", err)
	}

	var (
		record *ReceiveRecord
		held   = make(map[asset.ID]struct{})
	)
	for idx := range records {
		known := &records[idx]
		switch {
		case known.Announcement == id.announcement:
			record = known

		case holdsAssets(known.State):
			payload, err := decodeReceivePayload(known.Payload)
			if err != nil {
				return nil, nil, err
			}
			for assetID := range payload.fragment.Outputs {
				held[assetID] = struct{}{}
			}
		}
	}
	if record == nil {
		return nil, nil, fmt.Errorf("receive %v: %w", id.outpoint,
			ErrNotFound)
	}

	return record, held, nil
}

// holdsAssets reports whether the wallet holds the assets of a receive in the
// given state.
func holdsAssets(state ReceiveState) bool {
	return state == ReceiveConfirmed || state == ReceiveFinal
}

// announcedAgain reports whether the wallet holds every asset of a fragment
// through other announcements. Confirming such a fragment would list one
// transfer as two.
func announcedAgain(held map[asset.ID]struct{},
	fragment *proof.SendFragment) bool {

	for assetID := range fragment.Outputs {
		if _, ok := held[assetID]; !ok {
			return false
		}
	}

	return true
}

// advanceOnce is one run of advance.
func (r *receiver) advanceOnce(ctx context.Context, id receiveID) (bool,
	error) {

	record, held, err := r.load(ctx, id)
	if err != nil {
		return false, err
	}
	if record.State != ReceiveDetected {
		return true, nil
	}

	payload, err := decodeReceivePayload(record.Payload)
	if err != nil {
		return false, err
	}
	if payload.reorged {
		return true, nil
	}
	fragment := payload.fragment
	if announcedAgain(held, fragment) {
		return true, r.fail(ctx, record, payload, errAnnouncedAgain)
	}

	addr, err := r.address(ctx, id.address)
	if err != nil {
		return false, err
	}

	// The chain source may not have the block yet, or may not have it
	// any more.
	err = r.e.verifyHeader(ctx, fragment.BlockHeader, fragment.BlockHeight)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errReceiveNotReady, err)
	}

	outputs, err := deriveReceiveOutputs(addr, fragment)
	if err != nil {
		return true, r.fail(ctx, record, payload, err)
	}

	files, err := r.fetchProofs(ctx, addr, fragment.OutPoint, outputs)
	if err != nil {
		return false, err
	}

	var (
		assetProofs = make([]AssetProof, len(outputs))
		tip         *proof.Proof
	)
	for idx, output := range outputs {
		_, err := files[idx].Verify(ctx, r.e.verifierCtx(ctx))
		switch {
		// The proof was not judged.
		case err != nil && (ctx.Err() != nil || isChainFailure(err)):
			return false, fmt.Errorf("verify proof: %w", err)

		case err != nil:
			return true, r.fail(ctx, record, payload, fmt.Errorf(
				"proof of asset %v does not verify: %w",
				output.assetID, err,
			))
		}

		tip, err = files[idx].LastProof()
		if err != nil {
			return false, err
		}
		err = matchReceive(addr, fragment.OutPoint, output, tip)
		if err != nil {
			return true, r.fail(ctx, record, payload, err)
		}

		var blob bytes.Buffer
		if err := files[idx].Encode(&blob); err != nil {
			return false, fmt.Errorf("encode proof file: %w", err)
		}
		assetProofs[idx] = AssetProof{
			ProofLocator: proofLocator(tip),
			ProofFile:    blob.Bytes(),
			Owned:        true,
		}
	}

	r.e.transferMu.Lock()
	defer r.e.transferMu.Unlock()

	// Another announcement of the output may have verified meanwhile.
	_, held, err = r.load(ctx, id)
	if err != nil {
		return false, err
	}
	if announcedAgain(held, fragment) {
		return true, r.fail(ctx, record, payload, errAnnouncedAgain)
	}

	for _, output := range outputs {
		err := r.e.insertScriptKey(ctx, output.scriptKey)
		if err != nil {
			return false, fmt.Errorf("store script key: %w", err)
		}
	}

	// Every proof is anchored in the output of the fragment.
	payload.anchorValue = tip.AnchorTx.TxOut[fragment.OutPoint.Index].Value
	record.State = ReceiveConfirmed
	record.BlockHash = tip.BlockHeader.BlockHash()
	record.BlockHeight = tip.BlockHeight
	record.Payload, err = payload.encode()
	if err != nil {
		return false, err
	}
	err = r.e.addrs.UpdateReceive(ctx, *record, assetProofs)
	if err != nil {
		return false, fmt.Errorf("store received proofs: %w", err)
	}

	r.e.log.Infof("Receive %v confirmed at height %d", id.outpoint,
		record.BlockHeight)
	r.track(id.outpoint)

	return true, nil
}

// fail records why a receive cannot complete. The wallet stores none of its
// proofs.
func (r *receiver) fail(ctx context.Context, record *ReceiveRecord,
	payload *receivePayload, cause error) error {

	payload.failure = cause.Error()

	var err error
	record.State = ReceiveFailed
	record.Payload, err = payload.encode()
	if err != nil {
		return err
	}
	if err := r.e.addrs.UpdateReceive(ctx, *record, nil); err != nil {
		return fmt.Errorf("store failed receive: %w", err)
	}

	r.e.log.Infof("Receive %v failed: %v", record.Outpoint, cause)

	return nil
}

// receiveOutput is one asset output of a transfer to an address.
type receiveOutput struct {
	assetID asset.ID
	amount  uint64

	// scriptKey is the key the sender derived for the asset from the
	// script key of the address.
	scriptKey asset.ScriptKey
}

// deriveReceiveOutputs derives the script key of every asset of a fragment as
// the sender did, in the order of the asset IDs. It fails for a fragment that
// cannot describe a transfer to the address.
func deriveReceiveOutputs(addr *receiveAddress,
	fragment *proof.SendFragment) ([]receiveOutput, error) {

	// An address without a group key is paid in its one asset.
	if addr.tap.GroupKey == nil {
		_, ok := fragment.Outputs[addr.tap.AssetID]
		if !ok || len(fragment.Outputs) != 1 {
			return nil, errors.New("outputs are not of the asset " +
				"of the address")
		}
	}

	outputs := make([]receiveOutput, 0, len(fragment.Outputs))
	for assetID, sent := range fragment.Outputs {
		if sent.Amount == 0 {
			return nil, fmt.Errorf("output of asset %v holds no "+
				"units", assetID)
		}

		scriptKey, err := asset.DeriveUniqueScriptKey(
			addr.tap.ScriptKey, assetID, sent.DerivationMethod,
		)
		if err != nil {
			return nil, fmt.Errorf("derive script key of asset "+
				"%v: %w", assetID, err)
		}
		scriptKey.RawKey = addr.scriptKey

		outputs = append(outputs, receiveOutput{
			assetID:   assetID,
			amount:    sent.Amount,
			scriptKey: scriptKey,
		})
	}
	slices.SortFunc(outputs, func(a, b receiveOutput) int {
		return bytes.Compare(a.assetID[:], b.assetID[:])
	})

	return outputs, nil
}

// fetchProofs downloads the proof file of every output from the courier of
// the address, one proof at a time back to the issuance of the asset.
func (r *receiver) fetchProofs(ctx context.Context, addr *receiveAddress,
	outpoint wire.OutPoint, outputs []receiveOutput) ([]*proof.File,
	error) {

	select {
	case r.fetches <- struct{}{}:
		defer func() { <-r.fetches }()

	case <-ctx.Done():
		return nil, ctx.Err()
	}

	courier, err := r.courier(ctx, &addr.tap.ProofCourierAddr)
	if err != nil {
		return nil, err
	}

	fetch := func(ctx context.Context,
		loc proof.Locator) (proof.Blob, error) {

		return queryProof(ctx, courier, loc)
	}

	files := make([]*proof.File, len(outputs))
	for idx, output := range outputs {
		files[idx], err = proof.FetchProofProvenance(
			ctx, nil, proof.Locator{
				AssetID:   &output.assetID,
				GroupKey:  addr.tap.GroupKey,
				ScriptKey: *output.scriptKey.PubKey,
				OutPoint:  &outpoint,
			}, fetch,
		)
		switch {
		case errors.Is(err, ErrNotFound):
			return nil, fmt.Errorf("%w: fetch proof of asset %v: "+
				"%w", errReceiveNotReady, output.assetID, err)

		case err != nil:
			return nil, fmt.Errorf("fetch proof of asset %v: %w",
				output.assetID, err)
		}
	}

	return files, nil
}

// queryProof fetches one proof of an asset from a courier. The proofs of a
// grouped asset are in the universe of its group, which may lack the leaf or
// hold another asset of the group at it, so the universe of the asset is
// asked next.
func queryProof(ctx context.Context, courier Courier,
	loc proof.Locator) (proof.Blob, error) {

	if loc.AssetID == nil || loc.OutPoint == nil {
		return nil, fmt.Errorf("proof locator is incomplete")
	}
	key := ProofKey{
		AssetID:   *loc.AssetID,
		ScriptKey: asset.ToSerialized(&loc.ScriptKey),
		Outpoint:  *loc.OutPoint,
	}

	if loc.GroupKey != nil {
		groupKey := [33]byte(asset.ToSerialized(loc.GroupKey))
		groupLeaf := key
		groupLeaf.GroupKey = &groupKey

		blob, err := courier.QueryProof(ctx, groupLeaf)
		switch {
		case err == nil && proofAssetID(blob) == key.AssetID:
			return blob, nil

		case err != nil && !errors.Is(err, ErrNotFound):
			return nil, err
		}
	}

	blob, err := courier.QueryProof(ctx, key)
	if err != nil {
		return nil, err
	}
	if proofAssetID(blob) != key.AssetID {
		return nil, fmt.Errorf("courier returned a proof of another "+
			"asset than %v", key.AssetID)
	}

	return blob, nil
}

// proofAssetID returns the asset ID of an encoded proof, or the zero ID for
// bytes that hold no asset.
func proofAssetID(blob []byte) asset.ID {
	var proofAsset asset.Asset
	err := proof.SparseDecode(
		bytes.NewReader(blob), proof.AssetLeafRecord(&proofAsset),
	)
	if err != nil {
		return asset.ID{}
	}

	return proofAsset.ID()
}

// matchReceive checks that a verified proof creates the asset output the
// fragment describes, at the keys of the address. A proof that verifies may
// still be of another output, or of one the wallet cannot spend.
func matchReceive(addr *receiveAddress, outpoint wire.OutPoint,
	output receiveOutput, tip *proof.Proof) error {

	tipAsset := &tip.Asset
	switch {
	case tip.OutPoint() != outpoint:
		return fmt.Errorf("proof is anchored at %v, not at %v",
			tip.OutPoint(), outpoint)

	case tipAsset.ID() != output.assetID:
		return fmt.Errorf("proof is of asset %v, not of %v",
			tipAsset.ID(), output.assetID)

	case !sameXOnlyKey(tipAsset.ScriptKey.PubKey, output.scriptKey.PubKey):
		return fmt.Errorf("proof of asset %v pays another script key "+
			"than the one of the address", output.assetID)

	case tipAsset.Amount != output.amount:
		return fmt.Errorf("proof of asset %v holds %d units, not %d",
			output.assetID, tipAsset.Amount, output.amount)

	// The transfers of the wallet spend neither another asset version nor
	// a time-locked asset.
	case tipAsset.Version != addr.tap.AssetVersion:
		return fmt.Errorf("asset %v is of version %d, not of the "+
			"version %d of the address", output.assetID,
			tipAsset.Version, addr.tap.AssetVersion)

	case tipAsset.LockTime != 0 || tipAsset.RelativeLockTime != 0:
		return fmt.Errorf("asset %v is time locked", output.assetID)
	}

	switch {
	case addr.tap.GroupKey != nil:
		if tipAsset.GroupKey == nil || !sameXOnlyKey(
			&tipAsset.GroupKey.GroupPubKey, addr.tap.GroupKey,
		) {

			return fmt.Errorf("asset %v is not of the group of "+
				"the address", output.assetID)
		}

	case output.assetID != addr.tap.AssetID:
		return fmt.Errorf("asset %v is not the asset of the address",
			output.assetID)
	}

	// The wallet signs for the anchor output with the internal key of the
	// address, and the address names no script the output may commit to.
	inclusion := &tip.InclusionProof
	if !inclusion.InternalKey.IsEqual(&addr.tap.InternalKey) {
		return fmt.Errorf("anchor output of asset %v is not under "+
			"the internal key of the address", output.assetID)
	}
	commitment := inclusion.CommitmentProof
	if commitment != nil && commitment.TapSiblingPreimage != nil {
		return fmt.Errorf("anchor output of asset %v commits to a "+
			"script the address does not name", output.assetID)
	}

	return nil
}

// sameXOnlyKey reports whether two keys have the same X coordinate.
func sameXOnlyKey(a, b *btcec.PublicKey) bool {
	return bytes.Equal(
		schnorr.SerializePubKey(a), schnorr.SerializePubKey(b),
	)
}
