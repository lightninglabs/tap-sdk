// Package courier connects the embedded engine to proof couriers over gRPC.
// A courier is a tapd that serves its universe and its auth mailbox to the
// public, so the client sends no macaroon.
package courier

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/lightninglabs/tap-sdk/embedded"
	mboxrpc "github.com/lightninglabs/taproot-assets/taprpc/authmailboxrpc"
	unirpc "github.com/lightninglabs/taproot-assets/taprpc/universerpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

const (
	// handshakeTimeout bounds the authentication of a mailbox stream. A
	// courier drops a receiver that has not authenticated after 10
	// seconds.
	handshakeTimeout = 30 * time.Second

	// maxRecvMsgSize is the largest message the client accepts, as tapd
	// sets it for its own courier client. A proof can exceed gRPC's
	// default of 4 MiB: it embeds the proof files of the inputs a
	// transfer merged.
	maxRecvMsgSize = 200 * 1024 * 1024

	// noProofMsg is how a universe reports a leaf it does not hold when
	// it does not answer with the NotFound code.
	noProofMsg = "no universe proof found"
)

// keepaliveParams makes a connection that died silently fail its streams
// within a minute. Without it a mailbox stream blocks forever, for example
// after a phone changed networks.
var keepaliveParams = keepalive.ClientParameters{
	Time:    30 * time.Second,
	Timeout: 20 * time.Second,
}

// Endpoint is how the client reaches one courier.
type Endpoint struct {
	// Target is the host and port to dial in place of the ones the
	// courier address names. Empty dials the address.
	Target string

	// TLS is the TLS configuration of the connection. Nil verifies the
	// courier's certificate against the system's root certificates.
	TLS *tls.Config
}

// NewDialer returns a dialer of gRPC couriers. endpoints holds the couriers
// that are not reached as their address says, keyed by the host and port of
// the address.
func NewDialer(endpoints map[string]Endpoint) embedded.CourierDialer {
	return func(_ context.Context,
		addr *url.URL) (embedded.Courier, error) {

		endpoint := endpoints[addr.Host]

		target := endpoint.Target
		if target == "" {
			target = addr.Host
		}
		tlsConfig := endpoint.TLS
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}

		// The connection is established by the first call.
		conn, err := grpc.NewClient(
			target, grpc.WithTransportCredentials(
				credentials.NewTLS(tlsConfig),
			),
			grpc.WithKeepaliveParams(keepaliveParams),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("connect to courier %v: %w",
				addr.Host, err)
		}

		return newClient(conn), nil
	}
}

// client is a courier reached over one gRPC connection, which carries the
// mailbox stream of every receiver.
type client struct {
	conn     *grpc.ClientConn
	universe unirpc.UniverseClient
	mailbox  mboxrpc.MailboxClient
}

// newClient returns the courier behind a connection, which it owns.
func newClient(conn *grpc.ClientConn) *client {
	return &client{
		conn:     conn,
		universe: unirpc.NewUniverseClient(conn),
		mailbox:  mboxrpc.NewMailboxClient(conn),
	}
}

// QueryProof returns the encoded proof at a universe leaf.
func (c *client) QueryProof(ctx context.Context,
	key embedded.ProofKey) ([]byte, error) {

	universeID := &unirpc.ID{
		Id: &unirpc.ID_AssetId{AssetId: key.AssetID[:]},
	}
	if key.GroupKey != nil {
		universeID.Id = &unirpc.ID_GroupKey{GroupKey: key.GroupKey[:]}
	}

	resp, err := c.universe.QueryProof(ctx, &unirpc.UniverseKey{
		Id: universeID,
		LeafKey: &unirpc.AssetKey{
			Outpoint: &unirpc.AssetKey_Op{
				Op: &unirpc.Outpoint{
					HashStr: key.Outpoint.Hash.String(),
					Index:   int32(key.Outpoint.Index),
				},
			},
			ScriptKey: &unirpc.AssetKey_ScriptKeyBytes{
				ScriptKeyBytes: key.ScriptKey[:],
			},
		},
	})
	switch {
	case isNoProof(err):
		return nil, fmt.Errorf("%w: %s", embedded.ErrNotFound,
			status.Convert(err).Message())

	case err != nil:
		return nil, err
	}

	leaf := resp.GetAssetLeaf()
	if leaf == nil || len(leaf.Proof) == 0 {
		return nil, errors.New("universe returned no proof")
	}

	return leaf.Proof, nil
}

// isNoProof reports whether a universe answered that it holds no proof at a
// leaf.
func isNoProof(err error) bool {
	switch status.Code(err) {
	case codes.NotFound:
		return true

	case codes.Unknown:
		return strings.Contains(
			status.Convert(err).Message(), noProofMsg,
		)

	default:
		return false
	}
}

// ReceiveMessages opens the mailbox stream of a receiver and authenticates
// it: the courier answers the receiver's key with a challenge, and delivers
// messages once it holds the receiver's signature of that challenge.
func (c *client) ReceiveMessages(ctx context.Context, receiver [33]byte,
	startBlock uint32,
	sign embedded.ChallengeSigner) (embedded.MessageStream, error) {

	streamCtx, cancel := context.WithCancel(ctx)
	timeout := time.AfterFunc(handshakeTimeout, cancel)

	stream, err := c.authenticate(streamCtx, receiver, startBlock, sign)
	if err != nil {
		timeout.Stop()
		cancel()

		return nil, err
	}

	// The timer canceled the stream if it fired before it was stopped.
	if !timeout.Stop() {
		return nil, errors.New("mailbox authentication timed out")
	}

	return &messageStream{stream: stream}, nil
}

// authenticate runs the handshake of a new mailbox stream.
func (c *client) authenticate(ctx context.Context, receiver [33]byte,
	startBlock uint32, sign embedded.ChallengeSigner) (
	mboxrpc.Mailbox_ReceiveMessagesClient, error) {

	stream, err := c.mailbox.ReceiveMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("open mailbox stream: %w", err)
	}

	err = stream.Send(&mboxrpc.ReceiveMessagesRequest{
		RequestType: &mboxrpc.ReceiveMessagesRequest_Init{
			Init: &mboxrpc.InitReceive{
				ReceiverId:                receiver[:],
				StartBlockHeightInclusive: startBlock,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("send receiver: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive challenge: %w", err)
	}
	challenge := resp.GetChallenge()
	if challenge == nil {
		return nil, fmt.Errorf("courier sent %T in place of a "+
			"challenge", resp.ResponseType)
	}

	sig, err := sign(ctx, challenge.ChallengeHash)
	if err != nil {
		return nil, fmt.Errorf("sign challenge: %w", err)
	}

	err = stream.Send(&mboxrpc.ReceiveMessagesRequest{
		RequestType: &mboxrpc.ReceiveMessagesRequest_AuthSig{
			AuthSig: &mboxrpc.AuthSignature{
				Signature: sig.Serialize(),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("send signature: %w", err)
	}

	resp, err = stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	if !resp.GetAuthSuccess() {
		return nil, fmt.Errorf("courier sent %T in place of the "+
			"authentication result", resp.ResponseType)
	}

	return stream, nil
}

// Close closes the connection and ends its streams.
func (c *client) Close() error {
	return c.conn.Close()
}

// messageStream is an authenticated mailbox stream.
type messageStream struct {
	stream mboxrpc.Mailbox_ReceiveMessagesClient
}

// Recv returns the next messages of the mailbox, and io.EOF once the courier
// ends the stream.
func (s *messageStream) Recv() ([]embedded.MailboxMessage, error) {
	for {
		resp, err := s.stream.Recv()
		if err != nil {
			return nil, err
		}

		switch t := resp.ResponseType.(type) {
		case *mboxrpc.ReceiveMessagesResponse_Messages:
			rpcMsgs := t.Messages.GetMessages()
			if len(rpcMsgs) == 0 {
				continue
			}

			msgs := make([]embedded.MailboxMessage, len(rpcMsgs))
			for idx, rpcMsg := range rpcMsgs {
				msgs[idx] = embedded.MailboxMessage{
					ID:      rpcMsg.MessageId,
					Payload: rpcMsg.EncryptedPayload,
				}
			}

			return msgs, nil

		case *mboxrpc.ReceiveMessagesResponse_Eos:
			return nil, io.EOF
		}
	}
}
