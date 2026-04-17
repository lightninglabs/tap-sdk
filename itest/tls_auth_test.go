//go:build itest

package itest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	taprest "github.com/lightninglabs/tap-sdk/rest"
	"github.com/stretchr/testify/require"
)

// TestTLSAndMacaroonGuards locks in the failure modes we expect from the
// SDK when TLS or macaroon material is wrong. These tests run against a
// live tapd so we catch regressions on the real wire format.
func TestTLSAndMacaroonGuards(t *testing.T) {
	// Extract the real credentials once — we compose a baseline cfg from
	// them and then sabotage individual fields.
	tlsPath := extractDockerFile(t, "tap-sdk-tapd-alice",
		"/root/.tapd/tls.cert")
	macPath := extractDockerFile(t, "tap-sdk-tapd-alice",
		"/root/.tapd/data/regtest/admin.macaroon")

	t.Run("grpc good credentials connect", func(t *testing.T) {
		client, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:         defaultAliceHost,
			Network:      entities.NetworkRegtest,
			TLSPath:      tlsPath,
			MacaroonPath: macPath,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		info, err := client.GetInfo(t.Context())
		require.NoError(t, err)
		require.Equal(t, "regtest", info.Network)
	})

	t.Run("grpc wrong tls cert rejected", func(t *testing.T) {
		// A self-signed cert that does not match tapd's leaf.
		bogusTLS := writeTempFile(t, "bogus-tls.cert",
			[]byte(bogusTLSPEM),
		)

		client, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:         defaultAliceHost,
			Network:      entities.NetworkRegtest,
			TLSPath:      bogusTLS,
			MacaroonPath: macPath,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.GetInfo(t.Context())
		require.Error(t, err,
			"expected tapd rejection on wrong TLS trust root")
	})

	t.Run("grpc missing macaroon rejected", func(t *testing.T) {
		_, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:    defaultAliceHost,
			Network: entities.NetworkRegtest,
			TLSPath: tlsPath,
		})
		require.Error(t, err,
			"expected NewClient to fail without macaroon material")
	})

	t.Run("grpc conflicting macaroon opts rejected", func(t *testing.T) {
		_, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:         defaultAliceHost,
			Network:      entities.NetworkRegtest,
			TLSPath:      tlsPath,
			MacaroonPath: macPath,
			MacaroonHex:  "deadbeef",
		})
		require.Error(t, err,
			"expected NewClient to reject multiple macaroon inputs")
	})

	t.Run("rest good credentials connect", func(t *testing.T) {
		client, err := taprest.NewClient(&taprest.Config{
			BaseURL:      defaultAliceRestHost,
			Network:      entities.NetworkRegtest,
			TLSPath:      tlsPath,
			MacaroonPath: macPath,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		info, err := client.GetInfo(t.Context())
		require.NoError(t, err)
		require.Equal(t, "regtest", info.Network)
	})

	t.Run("rest wrong tls cert rejected", func(t *testing.T) {
		bogusTLS := writeTempFile(t, "bogus-tls.cert",
			[]byte(bogusTLSPEM),
		)

		client, err := taprest.NewClient(&taprest.Config{
			BaseURL:      defaultAliceRestHost,
			Network:      entities.NetworkRegtest,
			TLSPath:      bogusTLS,
			MacaroonPath: macPath,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.GetInfo(t.Context())
		require.Error(t, err,
			"expected rest transport to reject wrong trust root")
	})
}

func writeTempFile(t *testing.T, name string, contents []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

// bogusTLSPEM is an unrelated self-signed cert generated solely for this
// test. It must not be trusted by tapd, so the SDK has to refuse the
// handshake.
const bogusTLSPEM = `-----BEGIN CERTIFICATE-----
MIIBWDCB/qADAgECAhEAuL7uMTgzX6vNlVbUK8zt0jAKBggqhkjOPQQDAjASMRAw
DgYDVQQDEwdpbnZhbGlkMB4XDTI1MDEwMTAwMDAwMFoXDTM1MDEwMTAwMDAwMFow
EjEQMA4GA1UEAxMHaW52YWxpZDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABDaW
9Ae+lXLeWJp39v+XoFl/B2FyGuPnnlMFVWPUq2PkKS7WPIZkAKjKH45dySEyrS01
R+gXv2X2+9q6gtQxexmjMjAwMA4GA1UdDwEB/wQEAwIChDAPBgNVHRMBAf8EBTAD
AQH/MA0GA1UdDgQGBAQEBAQEMAoGCCqGSM49BAMCA0gAMEUCIQDjDsvoW3fB/AYG
zQP2vKdg88m4ezlG/Mrnlff7ii4BhAIgKjlPMVfR4RFMDhhnlzuvabgeIHZ1K/r3
cs3r7yQ/C6Y=
-----END CERTIFICATE-----
`
