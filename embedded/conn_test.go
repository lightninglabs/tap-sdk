package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestLocalConnMetadata checks that a handler sees the caller's metadata
// as incoming, as over a network, and has none to forward.
func TestLocalConnMetadata(t *testing.T) {
	t.Parallel()

	var incoming, outgoing metadata.MD
	conn := newLocalConn()
	conn.register(&grpc.ServiceDesc{
		ServiceName: "test.Service",
		Methods: []grpc.MethodDesc{{
			MethodName: "Call",
			Handler: func(_ any, ctx context.Context,
				_ func(any) error,
				_ grpc.UnaryServerInterceptor) (any, error) {

				incoming, _ = metadata.FromIncomingContext(ctx)
				outgoing, _ = metadata.FromOutgoingContext(ctx)

				return &emptypb.Empty{}, nil
			},
		}},
	}, nil)

	ctx := metadata.AppendToOutgoingContext(
		t.Context(), "macaroon", "",
	)
	err := conn.Invoke(
		ctx, "/test.Service/Call", &emptypb.Empty{}, &emptypb.Empty{},
	)
	require.NoError(t, err)

	require.Equal(t, []string{""}, incoming.Get("macaroon"))
	require.Empty(t, outgoing)
}
