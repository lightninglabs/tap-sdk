//go:build itest

package itest

import (
	"context"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/status"
)

// TestErrorHandling verifies that the REST transport produces the same
// typed gRPC status code as the native gRPC transport for invalid input.
// The PR that introduced APIError.GRPCStatus claims REST-gRPC parity —
// the most direct way to test that is to run each failure case over
// both transports and require the codes match.
func TestErrorHandling(t *testing.T) {
	grpcH, ctx := newHarnessContextFor(t, TransportGRPC)
	restH, _ := newHarnessContextFor(t, TransportREST)

	tests := []struct {
		name string
		run  func(tapsdk.Client, context.Context) error
	}{
		{
			name: "non-existent proof",
			run: func(c tapsdk.Client, ctx context.Context) error {
				fakeRef := entities.AssetRefFromAssetID(
					entities.AssetID{},
				)
				fakePubKey := entities.PubKey{}
				_, err := c.ExportProof(
					ctx, fakeRef, fakePubKey, nil,
				)
				return err
			},
		},
		{
			name: "invalid proof decode",
			run: func(c tapsdk.Client, ctx context.Context) error {
				_, err := c.DecodeProof(
					ctx, []byte{0x00, 0x01},
				)
				return err
			},
		},
		{
			name: "invalid tap address decode",
			run: func(c tapsdk.Client, ctx context.Context) error {
				_, err := c.DecodeAddr(ctx, "not-a-tap-addr")
				return err
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			grpcErr := tc.run(grpcH.AliceClient, ctx)
			restErr := tc.run(restH.AliceClient, ctx)

			require.Error(t, grpcErr, "gRPC did not error")
			require.Error(t, restErr, "REST did not error")

			// status.FromError walks the error chain via
			// errors.As; both transports must satisfy it or
			// callers cannot branch on the code uniformly.
			grpcSt, grpcOK := status.FromError(grpcErr)
			restSt, restOK := status.FromError(restErr)
			require.True(t, grpcOK,
				"gRPC error not typed: %v", grpcErr)
			require.True(t, restOK,
				"REST error not typed: %v", restErr)

			require.Equal(t, grpcSt.Code(), restSt.Code(),
				"REST code %s != gRPC code %s; "+
					"gRPC err: %v; REST err: %v",
				restSt.Code(), grpcSt.Code(),
				grpcErr, restErr)
		})
	}
}
