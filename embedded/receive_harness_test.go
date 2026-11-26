package embedded

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/customanchor"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

func init() {
	// The test courier delivers its stored messages at once.
	mailboxQuiet = 50 * time.Millisecond
}

// memCourier is a proof courier in memory: a universe of proofs and a
// mailbox per receiver. Its state outlives the clients that dial it.
type memCourier struct {
	mu sync.Mutex

	// leaves are the proofs of the universe by leaf.
	leaves map[string][]byte

	// queryErr fails every proof query.
	queryErr error

	// queries counts the proof queries.
	queries int

	// querying and maxQuerying count the queries in flight. A query
	// blocks on hold when it is set.
	querying    int
	maxQuerying int
	hold        chan struct{}

	// mailboxes are the stored messages by receiver.
	mailboxes map[[33]byte][]MailboxMessage
	streams   []*memStream
	nextMsgID uint64

	// startBlocks is the start block of the last subscription of each
	// receiver.
	startBlocks map[[33]byte]uint32

	// subscriptions counts the authenticated mailbox streams.
	subscriptions int

	// dialed are the dialed courier addresses, and open counts the
	// connections that are not closed.
	dialed []string
	open   int

	// insertErr fails a proof insert when it returns an error.
	insertErr func(key ProofKey) error

	// sendErr fails every message a sender posts.
	sendErr error

	// uploads lists the leaves senders inserted and the outputs they
	// announced, in the order of the calls.
	uploads []string

	// announced are the receivers of the posted messages by the output
	// each message is about.
	announced map[wire.OutPoint][33]byte

	// chain is the chain the anchor proof of a posted message is checked
	// against.
	chain *testChain
}

// newMemCourier returns an empty courier.
func newMemCourier() *memCourier {
	return &memCourier{
		leaves:      make(map[string][]byte),
		mailboxes:   make(map[[33]byte][]MailboxMessage),
		startBlocks: make(map[[33]byte]uint32),
		announced:   make(map[wire.OutPoint][33]byte),
	}
}

// leafName names a universe leaf.
func leafName(key ProofKey) string {
	universe := fmt.Sprintf("asset-%x", key.AssetID[:])
	if key.GroupKey != nil {
		universe = fmt.Sprintf("group-%x", key.GroupKey[1:])
	}

	return fmt.Sprintf("%s/%v/%x", universe, key.Outpoint,
		key.ScriptKey[1:])
}

// dial connects to the courier.
func (c *memCourier) dial(_ context.Context, addr *url.URL) (Courier,
	error) {

	c.mu.Lock()
	defer c.mu.Unlock()

	c.dialed = append(c.dialed, addr.String())
	c.open++

	ctx, cancel := context.WithCancel(context.Background())

	return &memCourierConn{courier: c, ctx: ctx, cancel: cancel}, nil
}

// publish adds every proof of a file to the universe, as the sender of a
// transfer does: in the universe of the asset's group, or of the asset when
// it has none.
func (c *memCourier) publish(t *testing.T, file *proof.File) {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	for idx := range file.NumProofs() {
		blob, err := file.RawProofAt(uint32(idx))
		require.NoError(t, err)
		p, err := file.ProofAt(uint32(idx))
		require.NoError(t, err)

		key := ProofKey{
			AssetID:   p.Asset.ID(),
			ScriptKey: asset.ToSerialized(p.Asset.ScriptKey.PubKey),
			Outpoint:  p.OutPoint(),
		}
		if p.Asset.GroupKey != nil {
			groupKey := [33]byte(asset.ToSerialized(
				&p.Asset.GroupKey.GroupPubKey,
			))
			key.GroupKey = &groupKey
		}
		c.leaves[leafName(key)] = blob
	}
}

// post stores a message for a receiver and delivers it to the open streams
// of that receiver.
func (c *memCourier) post(receiver *btcec.PublicKey, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextMsgID++
	msg := MailboxMessage{ID: c.nextMsgID, Payload: payload}

	id := [33]byte(asset.ToSerialized(receiver))
	c.mailboxes[id] = append(c.mailboxes[id], msg)

	for _, stream := range c.streams {
		if stream.receiver == id && stream.ctx.Err() == nil {
			stream.batches <- []MailboxMessage{msg}
		}
	}
}

// clearMailboxes removes every stored message.
func (c *memCourier) clearMailboxes() {
	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.mailboxes)
}

// counts returns the number of proof queries, of authenticated mailbox
// streams and of open connections.
func (c *memCourier) counts() (int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.queries, c.subscriptions, c.open
}

// memCourierConn is one connection to a test courier.
type memCourierConn struct {
	courier *memCourier
	ctx     context.Context
	cancel  context.CancelFunc
}

// QueryProof returns the proof at a universe leaf.
func (c *memCourierConn) QueryProof(ctx context.Context,
	key ProofKey) ([]byte, error) {

	courier := c.courier

	courier.mu.Lock()
	courier.queries++
	courier.querying++
	courier.maxQuerying = max(courier.maxQuerying, courier.querying)
	hold := courier.hold
	courier.mu.Unlock()

	defer func() {
		courier.mu.Lock()
		courier.querying--
		courier.mu.Unlock()
	}()

	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	courier.mu.Lock()
	defer courier.mu.Unlock()

	if courier.queryErr != nil {
		return nil, courier.queryErr
	}
	blob, ok := courier.leaves[leafName(key)]
	if !ok {
		return nil, fmt.Errorf("%w: no universe proof", ErrNotFound)
	}

	return blob, nil
}

// ReceiveMessages challenges the receiver as a tapd does, whose lnd verifies
// a Schnorr signature over the SHA-256 of the challenge with the receiver's
// x-only key, and then delivers the stored messages of the receiver.
func (c *memCourierConn) ReceiveMessages(ctx context.Context,
	receiver [33]byte, startBlock uint32,
	sign ChallengeSigner) (MessageStream, error) {

	courier := c.courier

	courier.mu.Lock()
	nonce := sha256.Sum256([]byte{byte(courier.subscriptions)})
	courier.mu.Unlock()

	challenge := sha256.Sum256(append(receiver[:], nonce[:]...))
	sig, err := sign(ctx, challenge[:])
	if err != nil {
		return nil, err
	}
	receiverKey, err := schnorr.ParsePubKey(receiver[1:])
	if err != nil {
		return nil, err
	}
	if !sig.Verify(chainhash.HashB(challenge[:]), receiverKey) {
		return nil, errors.New("signature not valid for receiver")
	}
	if startBlock == 0 {
		return nil, errors.New("no stored messages without a start")
	}

	courier.mu.Lock()
	defer courier.mu.Unlock()

	courier.subscriptions++
	courier.startBlocks[receiver] = startBlock

	streamCtx, cancel := context.WithCancel(ctx)
	context.AfterFunc(c.ctx, cancel)

	stream := &memStream{
		ctx:      streamCtx,
		receiver: receiver,
		batches:  make(chan []MailboxMessage, 64),
	}
	if stored := courier.mailboxes[receiver]; len(stored) > 0 {
		stream.batches <- append([]MailboxMessage(nil), stored...)
	}
	courier.streams = append(courier.streams, stream)

	return stream, nil
}

// InsertProof stores a proof at a universe leaf, as a universe does: it reads
// the asset of the leaf from the proof.
func (c *memCourierConn) InsertProof(_ context.Context, key ProofKey,
	blob []byte) error {

	courier := c.courier

	courier.mu.Lock()
	defer courier.mu.Unlock()

	if courier.insertErr != nil {
		if err := courier.insertErr(key); err != nil {
			return err
		}
	}
	if proofAssetID(blob) != key.AssetID {
		return fmt.Errorf("%w: proof is of another asset",
			ErrProofRefused)
	}

	courier.leaves[leafName(key)] = blob
	courier.uploads = append(courier.uploads, leafName(key))

	return nil
}

// SendMessage stores a message for a receiver, as a mailbox does: the anchor
// proof must show a confirmed Taproot output, and one message is stored per
// output.
func (c *memCourierConn) SendMessage(ctx context.Context, receiver [33]byte,
	payload []byte, anchor *AnchorProof) error {

	courier := c.courier

	receiverKey, err := btcec.ParsePubKey(receiver[:])
	if err != nil {
		return err
	}
	internalKey, err := btcec.ParsePubKey(anchor.InternalKey[:])
	if err != nil {
		return err
	}
	txProof := proof.TxProof{
		MsgTx:       *anchor.Tx,
		BlockHeader: anchor.BlockHeader,
		BlockHeight: anchor.BlockHeight,
		MerkleProof: proof.TxMerkleProof{
			Nodes: anchor.MerkleNodes,
			Bits:  anchor.MerkleBits,
		},
		ClaimedOutPoint: anchor.Outpoint,
		InternalKey:     *internalKey,
		MerkleRoot:      anchor.MerkleRoot,
	}
	err = txProof.Verify(func(header wire.BlockHeader,
		height uint32) error {

		known, err := courier.chain.BlockHeader(ctx, height)
		if err != nil {
			return err
		}
		if known.BlockHash() != header.BlockHash() {
			return errors.New("block is not on the chain")
		}

		return nil
	}, proof.DefaultMerkleVerifier)
	if err != nil {
		return fmt.Errorf("error validating proof: %w", err)
	}

	courier.mu.Lock()
	if courier.sendErr != nil {
		courier.mu.Unlock()

		return courier.sendErr
	}
	known, ok := courier.announced[anchor.Outpoint]
	if !ok {
		courier.announced[anchor.Outpoint] = receiver
		courier.uploads = append(
			courier.uploads, "message/"+anchor.Outpoint.String(),
		)
	}
	courier.mu.Unlock()

	switch {
	case !ok:
		courier.post(receiverKey, payload)

		return nil

	case known == receiver:
		return nil

	default:
		return errors.New("outpoint in proof already in use")
	}
}

// Close ends the streams of the connection.
func (c *memCourierConn) Close() error {
	c.cancel()

	c.courier.mu.Lock()
	defer c.courier.mu.Unlock()

	c.courier.open--

	return nil
}

// memStream is a mailbox stream of a test courier.
type memStream struct {
	ctx      context.Context
	receiver [33]byte
	batches  chan []MailboxMessage
}

// Recv returns the next messages.
func (s *memStream) Recv() ([]MailboxMessage, error) {
	select {
	case msgs := <-s.batches:
		return msgs, nil

	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// addressSend is a confirmed transfer to an address, as its sender holds it.
type addressSend struct {
	addr      *address.Tap
	assetID   asset.ID
	amount    uint64
	outpoint  wire.OutPoint
	scriptKey asset.ScriptKey

	// file is the proof file of the output that pays the address.
	file      *proof.File
	proofFile []byte

	// fragment is what the sender posts to the mailbox of the address.
	fragment *proof.SendFragment
}

// anchorPlan changes the anchor output of a send to an address.
type anchorPlan struct {
	// internalKey replaces the internal key of the address.
	internalKey *btcec.PublicKey

	// leaves are tapscript leaves the output commits to.
	leaves []tapsdk.TapLeaf
}

// payAddress has the wallet h send amount units of an asset it holds to an
// address, as a tapd does: to the script key derived from the address for
// the asset, in an anchor output under the internal key of the address,
// unless a plan changes that output. The transfer is confirmed. Nothing is
// published on the courier.
func (h *testHarness) payAddress(encoded string, minted *mintedAsset,
	amount uint64, plans ...anchorPlan) *addressSend {

	h.t.Helper()
	ctx := h.t.Context()

	addr, err := address.DecodeAddress(encoded, &address.RegressionNetTap)
	require.NoError(h.t, err)

	var plan anchorPlan
	if len(plans) > 0 {
		plan = plans[0]
	}
	anchorKey := &addr.InternalKey
	if plan.internalKey != nil {
		anchorKey = plan.internalKey
	}

	scriptKey, err := asset.DeriveUniqueScriptKey(
		addr.ScriptKey, minted.assetID,
		asset.ScriptKeyDerivationUniquePedersen,
	)
	require.NoError(h.t, err)

	// Each send spends a funding coin of its own.
	h.sends++
	recipient := tapsdk.ScriptKey{
		PubKey: sdkPubKey(h.t, scriptKey.PubKey),
	}
	sent := h.commitTransferTo(
		minted, receivedUnits, amount, h.sends,
		customanchor.AssetScriptPlan{
			Mode: customanchor.AssetScriptExternal,
			External: &customanchor.AssetExternalScriptPlan{
				ScriptKey: recipient,
			},
		}, anchorKey, plan.leaves...,
	)
	_, err = customanchor.PublishTransfer(
		ctx, h.wallet, sent.transfer, sent.finalPSBT,
	)
	require.NoError(h.t, err)
	h.chain.mine(h.t)
	h.waitTransfer(sent.txid, TransferConfirmed)

	outpoint := wire.OutPoint{Hash: sent.txid, Index: 0}
	exported, err := h.client.ExportProof(
		ctx, tapsdk.AssetRefFromAssetID(tapsdk.AssetID(minted.assetID)),
		sdkPubKey(h.t, scriptKey.PubKey), &tapsdk.Outpoint{
			Txid:  outpoint.Hash,
			Index: outpoint.Index,
		},
	)
	require.NoError(h.t, err)

	file, err := proof.DecodeFile(exported.RawProofFile)
	require.NoError(h.t, err)
	tip, err := file.LastProof()
	require.NoError(h.t, err)
	require.Equal(h.t, amount, tip.Asset.Amount)

	return &addressSend{
		addr:      addr,
		assetID:   minted.assetID,
		amount:    amount,
		outpoint:  outpoint,
		scriptKey: scriptKey,
		file:      file,
		proofFile: exported.RawProofFile,
		fragment: &proof.SendFragment{
			Version:     proof.SendFragmentV1,
			BlockHeader: tip.BlockHeader,
			BlockHeight: tip.BlockHeight,
			OutPoint:    outpoint,
			Outputs: map[asset.ID]proof.SendOutput{
				minted.assetID: {
					AssetVersion: tip.Asset.Version,
					Amount:       amount,
					ScriptKey: asset.ToSerialized(
						scriptKey.PubKey,
					),
				},
			},
		},
	}
}

// announcedAs returns the send with a changed copy of its fragment, as
// someone who knows the address and the output may announce it.
func (s *addressSend) announcedAs(
	change func(*proof.SendFragment)) *addressSend {

	fragment := *s.fragment
	fragment.Outputs = maps.Clone(s.fragment.Outputs)
	change(&fragment)

	announced := *s
	announced.fragment = &fragment

	return &announced
}

// announce posts the fragment of a send to the mailbox of its address.
func (c *memCourier) announce(t *testing.T, send *addressSend) {
	t.Helper()

	payload, err := proof.EncryptSendFragment(
		send.fragment, &send.addr.ScriptKey,
	)
	require.NoError(t, err)

	c.post(&send.addr.ScriptKey, payload)
}

// deliver publishes the proofs of a send and announces it, as a sender does
// once its transfer is confirmed.
func (c *memCourier) deliver(t *testing.T, send *addressSend) {
	t.Helper()

	c.publish(t, send.file)
	c.announce(t, send)
}

// receiveRecord returns the wallet's record of the announcement of a send, or
// nil.
func (h *testHarness) receiveRecord(send *addressSend) *ReceiveRecord {
	h.t.Helper()

	announcement, err := announcementOf(send.fragment)
	require.NoError(h.t, err)

	address := [33]byte(asset.ToSerialized(&send.addr.ScriptKey))
	records, err := h.store.ListReceives(h.t.Context(), ReceiveFilter{
		Outpoint: &send.outpoint,
		Address:  &address,
	})
	require.NoError(h.t, err)
	for idx := range records {
		if records[idx].Announcement == announcement {
			return &records[idx]
		}
	}

	return nil
}

// requireReceiveFailure checks that the wallet's record of a send failed for
// the given reason.
func (h *testHarness) requireReceiveFailure(send *addressSend,
	errText string) {

	h.t.Helper()

	record := h.requireReceiveState(send, ReceiveFailed)
	require.Zero(h.t, record.BlockHash)
	payload, err := decodeReceivePayload(record.Payload)
	require.NoError(h.t, err)
	require.Contains(h.t, payload.failure, errText)
}

// requireReceiveState checks the state of the wallet's record of a send.
func (h *testHarness) requireReceiveState(send *addressSend,
	state ReceiveState) *ReceiveRecord {

	h.t.Helper()

	record := h.receiveRecord(send)
	require.NotNil(h.t, record, "send is not recorded")
	require.Equal(h.t, state, record.State)

	return record
}

// waitReceiveState waits until the wallet's record of a send reaches a
// state.
func (h *testHarness) waitReceiveState(send *addressSend,
	state ReceiveState) {

	h.t.Helper()

	require.Eventually(h.t, func() bool {
		record := h.receiveRecord(send)

		return record != nil && record.State == state
	}, 10*time.Second, 5*time.Millisecond)
}

// fastRetries shortens the delay between the attempts of background work. It
// must be called before the clients of the test are created, so that they
// are closed before the delay is restored.
func fastRetries(t *testing.T) {
	t.Helper()

	previous, previousReceive := retryInterval, receiveRetryInterval
	retryInterval, receiveRetryInterval = time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		retryInterval, receiveRetryInterval = previous, previousReceive
	})
}
