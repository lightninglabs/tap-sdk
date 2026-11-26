package courier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/tap-sdk/embedded"
	mboxrpc "github.com/lightninglabs/taproot-assets/taprpc/authmailboxrpc"
	unirpc "github.com/lightninglabs/taproot-assets/taprpc/universerpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// testNonce is the nonce of every challenge of the test courier.
var testNonce = [32]byte{7}

// testServer is a courier that answers from memory. It authenticates a
// mailbox stream as tapd does.
type testServer struct {
	unirpc.UnimplementedUniverseServer
	mboxrpc.UnimplementedMailboxServer

	mu sync.Mutex

	// proof answers the next QueryProof, unless proofErr is set.
	proof    *unirpc.AssetProofResponse
	proofErr error

	// queried is the key of the last QueryProof.
	queried *unirpc.UniverseKey

	// inserted is the last InsertProof, which insertErr fails.
	inserted  *unirpc.AssetProof
	insertErr error

	// sent is the last SendMessage, which sendErr fails.
	sent    *mboxrpc.SendMessageRequest
	sendErr error

	// init is the first message of the last mailbox stream.
	init *mboxrpc.InitReceive

	// skipChallenge makes the courier deliver messages to a receiver that
	// has not authenticated.
	skipChallenge bool

	// stored is delivered once a stream is authenticated, and each batch
	// of arriving after it. The stream then ends.
	stored   []*mboxrpc.MailboxMessage
	arriving chan []*mboxrpc.MailboxMessage
}

// QueryProof records the key and returns the configured answer.
func (s *testServer) QueryProof(_ context.Context,
	key *unirpc.UniverseKey) (*unirpc.AssetProofResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.queried = key
	if s.proofErr != nil {
		return nil, s.proofErr
	}

	return s.proof, nil
}

// InsertProof records the proof and returns the configured answer.
func (s *testServer) InsertProof(_ context.Context,
	req *unirpc.AssetProof) (*unirpc.AssetProofResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.inserted = req
	if s.insertErr != nil {
		return nil, s.insertErr
	}

	return &unirpc.AssetProofResponse{AssetLeaf: req.AssetLeaf}, nil
}

// SendMessage records the message and returns the configured answer.
func (s *testServer) SendMessage(_ context.Context,
	req *mboxrpc.SendMessageRequest) (*mboxrpc.SendMessageResponse,
	error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sent = req
	if s.sendErr != nil {
		return nil, s.sendErr
	}

	return &mboxrpc.SendMessageResponse{MessageId: 1}, nil
}

// ReceiveMessages runs the mailbox handshake and delivers the configured
// messages.
func (s *testServer) ReceiveMessages(
	stream mboxrpc.Mailbox_ReceiveMessagesServer) error {

	req, err := stream.Recv()
	if err != nil {
		return err
	}
	init := req.GetInit()
	if init == nil {
		return status.Error(codes.InvalidArgument, "no init")
	}

	s.mu.Lock()
	s.init = init
	skipChallenge := s.skipChallenge
	stored := s.stored
	s.mu.Unlock()

	if !skipChallenge {
		err := authenticate(stream, init.ReceiverId)
		if err != nil {
			return err
		}
	}

	send := func(msgs []*mboxrpc.MailboxMessage) error {
		return stream.Send(&mboxrpc.ReceiveMessagesResponse{
			ResponseType: &mboxrpc.ReceiveMessagesResponse_Messages{
				Messages: &mboxrpc.MailboxMessages{
					Messages: msgs,
				},
			},
		})
	}
	if err := send(stored); err != nil {
		return err
	}

	for {
		select {
		case msgs, ok := <-s.arriving:
			if !ok {
				eos := &mboxrpc.ReceiveMessagesResponse_Eos{
					Eos: &mboxrpc.EndOfStream{},
				}

				return stream.Send(
					&mboxrpc.ReceiveMessagesResponse{
						ResponseType: eos,
					},
				)
			}
			if err := send(msgs); err != nil {
				return err
			}

		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// authenticate challenges the receiver and checks its answer as tapd does:
// lnd verifies a Schnorr signature over the SHA-256 of the challenge with the
// receiver's x-only key.
func authenticate(stream mboxrpc.Mailbox_ReceiveMessagesServer,
	receiverID []byte) error {

	challenge := sha256.Sum256(append(receiverID, testNonce[:]...))
	err := stream.Send(&mboxrpc.ReceiveMessagesResponse{
		ResponseType: &mboxrpc.ReceiveMessagesResponse_Challenge{
			Challenge: &mboxrpc.Challenge{
				ChallengeHash: challenge[:],
			},
		},
	})
	if err != nil {
		return err
	}

	req, err := stream.Recv()
	if err != nil {
		return err
	}
	authSig := req.GetAuthSig()
	if authSig == nil {
		return status.Error(codes.InvalidArgument, "no signature")
	}

	receiver, err := schnorr.ParsePubKey(receiverID[1:])
	if err != nil {
		return err
	}
	sig, err := schnorr.ParseSignature(authSig.Signature)
	if err != nil {
		return err
	}
	if !sig.Verify(chainhash.HashB(challenge[:]), receiver) {
		return status.Error(codes.PermissionDenied, "invalid signature")
	}

	return stream.Send(&mboxrpc.ReceiveMessagesResponse{
		ResponseType: &mboxrpc.ReceiveMessagesResponse_AuthSuccess{
			AuthSuccess: true,
		},
	})
}

// serve registers the test courier with a gRPC server on the listener.
func (s *testServer) serve(t *testing.T, lis net.Listener,
	opts ...grpc.ServerOption) {

	t.Helper()

	server := grpc.NewServer(opts...)
	unirpc.RegisterUniverseServer(server, s)
	mboxrpc.RegisterMailboxServer(server, s)

	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)
}

// newTestClient returns a courier client connected to a new test courier
// through memory.
func newTestClient(t *testing.T) (*testServer, embedded.Courier) {
	t.Helper()

	server := &testServer{
		arriving: make(chan []*mboxrpc.MailboxMessage),
	}
	lis := bufconn.Listen(1 << 20)
	server.serve(t, lis)

	conn, err := grpc.NewClient(
		"passthrough:///courier", grpc.WithContextDialer(
			func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			},
		),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := newClient(conn)
	t.Cleanup(func() { _ = client.Close() })

	return server, client
}

// testReceiver returns a receiver key with an odd Y coordinate, which a
// mailbox knows by its X coordinate only, and the signer of its challenges.
func testReceiver(t *testing.T) ([33]byte, embedded.ChallengeSigner) {
	t.Helper()

	for seed := byte(1); ; seed++ {
		privKey, pubKey := btcec.PrivKeyFromBytes([]byte{seed})
		compressed := pubKey.SerializeCompressed()
		if compressed[0] != 0x03 {
			continue
		}

		sign := func(_ context.Context,
			challenge []byte) (*schnorr.Signature, error) {

			return schnorr.Sign(privKey, chainhash.HashB(challenge))
		}

		return [33]byte(compressed), sign
	}
}

// TestQueryProof asks for a leaf of an asset's universe and of a group's
// universe, and maps a missing leaf to ErrNotFound.
func TestQueryProof(t *testing.T) {
	var (
		ctx      = t.Context()
		groupKey = [33]byte{2, 9}
		key      = embedded.ProofKey{
			AssetID:   [32]byte{1},
			ScriptKey: [33]byte{2, 3},
			Outpoint: wire.OutPoint{
				Hash:  chainhash.Hash{4},
				Index: 5,
			},
		}
		leafKey = &unirpc.AssetKey{
			Outpoint: &unirpc.AssetKey_Op{
				Op: &unirpc.Outpoint{
					HashStr: key.Outpoint.Hash.String(),
					Index:   5,
				},
			},
			ScriptKey: &unirpc.AssetKey_ScriptKeyBytes{
				ScriptKeyBytes: key.ScriptKey[:],
			},
		}
	)
	groupLeaf := key
	groupLeaf.GroupKey = &groupKey

	tests := []struct {
		name string
		key  embedded.ProofKey

		// resp and respErr are the courier's answer.
		resp    *unirpc.AssetProofResponse
		respErr error

		wantID  *unirpc.ID
		want    []byte
		wantErr error
		errText string
	}{{
		name: "asset universe",
		key:  key,
		resp: &unirpc.AssetProofResponse{
			AssetLeaf: &unirpc.AssetLeaf{Proof: []byte("proof")},
		},
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
		want: []byte("proof"),
	}, {
		name: "group universe",
		key:  groupLeaf,
		resp: &unirpc.AssetProofResponse{
			AssetLeaf: &unirpc.AssetLeaf{Proof: []byte("group")},
		},
		wantID: &unirpc.ID{
			Id: &unirpc.ID_GroupKey{GroupKey: groupKey[:]},
		},
		want: []byte("group"),
	}, {
		name:    "missing leaf",
		key:     key,
		respErr: status.Error(codes.NotFound, "no universe proof"),
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
		wantErr: embedded.ErrNotFound,
		errText: "no universe proof",
	}, {
		name: "missing leaf without the code",
		key:  key,
		respErr: status.Error(
			codes.Unknown, "query failed: no universe proof found",
		),
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
		wantErr: embedded.ErrNotFound,
		errText: "no universe proof found",
	}, {
		name:    "courier failure",
		key:     key,
		respErr: status.Error(codes.Unavailable, "universe busy"),
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
		errText: "universe busy",
	}, {
		name:    "courier failure without a code",
		key:     key,
		respErr: status.Error(codes.Unknown, "database is locked"),
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
		errText: "database is locked",
	}, {
		name: "empty answer",
		key:  key,
		resp: &unirpc.AssetProofResponse{},
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
		errText: "universe returned no proof",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, client := newTestClient(t)
			server.proof, server.proofErr = tc.resp, tc.respErr

			blob, err := client.QueryProof(ctx, tc.key)
			if tc.errText != "" {
				require.ErrorContains(t, err, tc.errText)
				require.Equal(
					t, tc.wantErr != nil,
					errors.Is(err, embedded.ErrNotFound),
				)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, blob)

			queried := server.queried
			require.Equal(t, tc.wantID.Id, queried.Id.Id)
			require.Equal(
				t, leafKey.Outpoint, queried.LeafKey.Outpoint,
			)
			require.Equal(
				t, leafKey.ScriptKey, queried.LeafKey.ScriptKey,
			)
		})
	}
}

// TestReceiveMessages authenticates a mailbox stream and reads the stored
// and the arriving messages until the courier ends the stream.
func TestReceiveMessages(t *testing.T) {
	server, client := newTestClient(t)
	receiver, sign := testReceiver(t)

	server.stored = []*mboxrpc.MailboxMessage{
		{MessageId: 1, EncryptedPayload: []byte("one")},
		{MessageId: 2, EncryptedPayload: []byte("two")},
	}

	stream, err := client.ReceiveMessages(t.Context(), receiver, 21, sign)
	require.NoError(t, err)
	require.Equal(t, receiver[:], server.init.ReceiverId)
	require.EqualValues(t, 21, server.init.StartBlockHeightInclusive)
	require.Zero(t, server.init.StartMessageIdExclusive)
	require.Zero(t, server.init.StartTimestampExclusive)

	msgs, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, []embedded.MailboxMessage{
		{ID: 1, Payload: []byte("one")},
		{ID: 2, Payload: []byte("two")},
	}, msgs)

	// A batch without messages is not delivered.
	server.arriving <- nil
	server.arriving <- []*mboxrpc.MailboxMessage{
		{MessageId: 3, EncryptedPayload: []byte("three")},
	}
	msgs, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, []embedded.MailboxMessage{
		{ID: 3, Payload: []byte("three")},
	}, msgs)

	close(server.arriving)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}

// TestReceiveMessagesEndsWithContext ends a mailbox stream when the context
// it was opened with ends.
func TestReceiveMessagesEndsWithContext(t *testing.T) {
	_, client := newTestClient(t)
	receiver, sign := testReceiver(t)

	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.ReceiveMessages(ctx, receiver, 1, sign)
	require.NoError(t, err)

	cancel()
	_, err = stream.Recv()
	require.Equal(t, codes.Canceled, status.Code(err))
}

// TestReceiveMessagesHandshakeFailure returns no stream when the handshake
// does not complete.
func TestReceiveMessagesHandshakeFailure(t *testing.T) {
	receiver, sign := testReceiver(t)

	tests := []struct {
		name          string
		skipChallenge bool
		sign          embedded.ChallengeSigner
		errText       string
	}{{
		name: "signature of another challenge",
		sign: func(ctx context.Context,
			_ []byte) (*schnorr.Signature, error) {

			return sign(ctx, []byte("other"))
		},
		errText: "invalid signature",
	}, {
		name: "signer failure",
		sign: func(context.Context,
			[]byte) (*schnorr.Signature, error) {

			return nil, errors.New("key is locked")
		},
		errText: "sign challenge: key is locked",
	}, {
		name:          "no challenge",
		skipChallenge: true,
		sign:          sign,
		errText:       "in place of a challenge",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, client := newTestClient(t)
			server.skipChallenge = tc.skipChallenge

			stream, err := client.ReceiveMessages(
				t.Context(), receiver, 1, tc.sign,
			)
			require.ErrorContains(t, err, tc.errText)
			require.Nil(t, stream)
		})
	}
}

// TestInsertProof stores a proof at a leaf of an asset's universe and of a
// group's universe, and tells a universe that refuses a proof from one that
// may take it later.
func TestInsertProof(t *testing.T) {
	var (
		ctx      = t.Context()
		groupKey = [33]byte{2, 9}
		key      = embedded.ProofKey{
			AssetID:   [32]byte{1},
			ScriptKey: [33]byte{2, 3},
			Outpoint: wire.OutPoint{
				Hash:  chainhash.Hash{4},
				Index: 5,
			},
		}
	)
	groupLeaf := key
	groupLeaf.GroupKey = &groupKey

	tests := []struct {
		name      string
		key       embedded.ProofKey
		insertErr error

		wantID  *unirpc.ID
		refused bool
		errText string
	}{{
		name: "asset universe",
		key:  key,
		wantID: &unirpc.ID{
			Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
		},
	}, {
		name: "group universe",
		key:  groupLeaf,
		wantID: &unirpc.ID{
			Id: &unirpc.ID_GroupKey{GroupKey: groupKey[:]},
		},
	}, {
		name:      "inserts disabled",
		key:       key,
		insertErr: errors.New("proof insert disabled for universe"),
		refused:   true,
		errText:   "proof insert disabled for universe",
	}, {
		name: "invalid proof",
		key:  key,
		insertErr: status.Error(
			codes.InvalidArgument, "unmarshal asset leaf",
		),
		refused: true,
		errText: "unmarshal asset leaf",
	}, {
		name:      "rate limit",
		key:       key,
		insertErr: errors.New("rate limiter: context canceled"),
		errText:   "rate limiter",
	}, {
		name:      "unavailable",
		key:       key,
		insertErr: status.Error(codes.Unavailable, "restarting"),
		errText:   "restarting",
	}, {
		name: "resource exhausted",
		key:  key,
		insertErr: status.Error(
			codes.ResourceExhausted, "too many requests",
		),
		errText: "too many requests",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, client := newTestClient(t)
			server.insertErr = tc.insertErr

			err := client.InsertProof(ctx, tc.key, []byte("proof"))
			if tc.errText != "" {
				require.ErrorContains(t, err, tc.errText)

				refused := errors.Is(
					err, embedded.ErrProofRefused,
				)
				require.Equal(t, tc.refused, refused)

				return
			}
			require.NoError(t, err)

			server.mu.Lock()
			defer server.mu.Unlock()

			inserted := server.inserted
			require.Equal(t, tc.wantID.Id, inserted.Key.Id.Id)
			require.Equal(
				t, tc.key.ScriptKey[:],
				inserted.Key.LeafKey.GetScriptKeyBytes(),
			)
			require.Equal(
				t, tc.key.Outpoint.Hash.String(),
				inserted.Key.LeafKey.GetOp().HashStr,
			)
			require.EqualValues(
				t, 5, inserted.Key.LeafKey.GetOp().Index,
			)
			require.Equal(
				t, []byte("proof"), inserted.AssetLeaf.Proof,
			)
		})
	}
}

// TestSendMessage posts a message with the proof of the output it is about,
// and returns the failure of a mailbox.
func TestSendMessage(t *testing.T) {
	ctx := t.Context()
	server, client := newTestClient(t)

	tx := wire.NewMsgTx(2)
	tx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: chainhash.Hash{1}},
	})
	tx.AddTxOut(&wire.TxOut{Value: 1_000, PkScript: []byte{0x51}})

	receiver, _ := testReceiver(t)
	anchor := &embedded.AnchorProof{
		Tx: tx,
		BlockHeader: wire.BlockHeader{
			Version:    2,
			MerkleRoot: chainhash.Hash{6},
		},
		BlockHeight: 144,
		MerkleNodes: []chainhash.Hash{{7}, {8}},
		MerkleBits:  []bool{true, false},
		Outpoint:    wire.OutPoint{Hash: tx.TxHash(), Index: 0},
		InternalKey: [33]byte{2, 5},
		MerkleRoot:  []byte{9},
	}

	err := client.SendMessage(ctx, receiver, []byte("payload"), anchor)
	require.NoError(t, err)

	server.mu.Lock()
	sent := server.sent
	server.mu.Unlock()

	require.Equal(t, receiver[:], sent.ReceiverId)
	require.Equal(t, []byte("payload"), sent.EncryptedPayload)

	txProof := sent.GetTxProof()
	require.NotNil(t, txProof)

	var sentTx wire.MsgTx
	require.NoError(
		t, sentTx.Deserialize(bytes.NewReader(txProof.RawTxData)),
	)
	require.Equal(t, tx.TxHash(), sentTx.TxHash())

	var sentHeader wire.BlockHeader
	require.NoError(t, sentHeader.Deserialize(
		bytes.NewReader(txProof.RawBlockHeaderData),
	))
	require.Equal(t, anchor.BlockHeader.BlockHash(), sentHeader.BlockHash())

	require.EqualValues(t, 144, txProof.BlockHeight)
	require.Equal(t, [][]byte{
		anchor.MerkleNodes[0][:], anchor.MerkleNodes[1][:],
	}, txProof.MerkleProof.SiblingHashes)
	require.Equal(t, anchor.MerkleBits, txProof.MerkleProof.Bits)
	require.Equal(
		t, anchor.Outpoint.Hash[:], txProof.ClaimedOutpoint.Txid,
	)
	require.Zero(t, txProof.ClaimedOutpoint.OutputIndex)
	require.Equal(t, anchor.InternalKey[:], txProof.InternalKey)
	require.Equal(t, anchor.MerkleRoot, txProof.MerkleRoot)

	server.mu.Lock()
	server.sendErr = status.Error(codes.Unknown, "error validating proof")
	server.mu.Unlock()

	err = client.SendMessage(ctx, receiver, []byte("payload"), anchor)
	require.ErrorContains(t, err, "error validating proof")

	err = client.SendMessage(ctx, receiver, []byte("payload"), nil)
	require.ErrorContains(t, err, "anchor proof is required")
}
