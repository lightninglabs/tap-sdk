//go:build itest

package itest

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	taprest "github.com/lightninglabs/tap-sdk/rest"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestReadOnlyMacaroon validates the SDK behavior with a custom macaroon that
// can read daemon and asset state but cannot mutate assets.
func TestReadOnlyMacaroon(t *testing.T) {
	requireTapdMain(t)

	tlsPath := extractDockerFile(t, "tap-sdk-tapd-alice",
		"/root/.tapd/tls.cert")
	adminMacPath := extractDockerFile(t, "tap-sdk-tapd-alice",
		"/root/.tapd/data/regtest/admin.macaroon")

	readMacHex := bakeReadOnlyMacaroon(t, tlsPath, adminMacPath)

	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("readonly-token-%s", transport))
		minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
		require.NoError(t, err)

		readClient := newAliceClientWithMacHex(
			t, transport, tlsPath, readMacHex,
		)

		info, err := readClient.GetInfo(ctx)
		require.NoError(t, err)
		require.Equal(t, "regtest", info.Network)

		balances, err := readClient.ListBalances(ctx,
			&tapsdk.ListBalancesRequest{
				AssetRef: &minted.Ref,
			},
		)
		require.NoError(t, err)
		require.Contains(t, balances.Balances, minted.Ref.String())

		addr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)
		readWallet := tapsdk.NewWallet(
			readClient, tapsdk.NetworkRegtest,
		)
		_, err = readWallet.Send(
			ctx, addr.Encoded, tapsdk.WithAmount(1),
		)
		requirePermissionDenied(t, err)

		_, err = readClient.BurnAsset(ctx, &tapsdk.BurnAssetRequest{
			AssetRef:         minted.Ref,
			AmountToBurn:     1,
			ConfirmationText: "assets will be destroyed",
			Note:             "read-only should fail",
		})
		requirePermissionDenied(t, err)
	})
}

func bakeReadOnlyMacaroon(t testing.TB, tlsPath,
	adminMacPath string) string {

	t.Helper()

	tlsCreds, err := credentials.NewClientTLSFromFile(tlsPath, "")
	require.NoError(t, err)

	conn, err := grpc.NewClient(
		envOr("TAPD_ALICE_HOST", defaultAliceHost),
		grpc.WithTransportCredentials(tlsCreds),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	adminMac, err := os.ReadFile(adminMacPath)
	require.NoError(t, err)

	ctx := metadata.AppendToOutgoingContext(
		context.Background(), "macaroon", hex.EncodeToString(adminMac),
	)

	resp, err := taprpc.NewTaprootAssetsClient(conn).BakeMacaroon(
		ctx, &taprpc.BakeMacaroonRequest{
			Permissions: []*taprpc.MacaroonPermission{
				{
					Entity: "daemon",
					Action: "read",
				},
				{
					Entity: "assets",
					Action: "read",
				},
			},
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Macaroon)

	return resp.Macaroon
}

func newAliceClientWithMacHex(t testing.TB, transport Transport,
	tlsPath string, macHex string) tapsdk.Client {

	t.Helper()

	switch transport {
	case TransportGRPC:
		client, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:     envOr("TAPD_ALICE_HOST", defaultAliceHost),
			Network:  tapsdk.NetworkRegtest,
			TLS:      tapgrpc.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromHex(macHex),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		return client

	case TransportREST:
		client, err := taprest.NewClient(&taprest.Config{
			BaseURL: envOr(
				"TAPD_ALICE_REST", defaultAliceRestHost,
			),
			Network:  tapsdk.NetworkRegtest,
			TLS:      taprest.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromHex(macHex),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		return client

	default:
		require.FailNowf(t, "unsupported transport",
			"transport %q", transport)
		return nil
	}
}

func requirePermissionDenied(t testing.TB, err error) {
	t.Helper()

	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "error does not expose gRPC status: %v", err)

	if st.Code() == codes.PermissionDenied {
		return
	}

	require.Equal(t, codes.Unknown, st.Code())
	require.Contains(t, strings.ToLower(st.Message()), "permission")
}
