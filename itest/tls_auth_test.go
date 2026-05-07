//go:build itest

package itest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
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
			Host:     defaultAliceHost,
			Network:  tapsdk.NetworkRegtest,
			TLS:      tapgrpc.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromPath(macPath),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		info, err := client.GetInfo(t.Context())
		require.NoError(t, err)
		require.Equal(t, "regtest", info.Network)
	})

	t.Run("grpc wrong tls cert rejected", func(t *testing.T) {
		// A real but unrelated self-signed cert that tapd did not
		// issue.
		bogusTLS := writeTempFile(t, "bogus-tls.cert",
			bogusTLSPEM(t),
		)

		client, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:     defaultAliceHost,
			Network:  tapsdk.NetworkRegtest,
			TLS:      tapgrpc.TLSFromPath(bogusTLS),
			Macaroon: tapsdk.MacaroonFromPath(macPath),
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
			Network: tapsdk.NetworkRegtest,
			TLS:     tapgrpc.TLSFromPath(tlsPath),
		})
		require.Error(t, err,
			"expected NewClient to fail without macaroon material")
	})

	t.Run("grpc invalid macaroon rejected", func(t *testing.T) {
		client, err := tapgrpc.NewClient(&tapgrpc.Config{
			Host:     defaultAliceHost,
			Network:  tapsdk.NetworkRegtest,
			TLS:      tapgrpc.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromHex("00"),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.GetInfo(t.Context())
		require.Error(t, err,
			"expected tapd rejection on invalid macaroon")
	})

	t.Run("rest good credentials connect", func(t *testing.T) {
		client, err := taprest.NewClient(&taprest.Config{
			BaseURL:  defaultAliceRestHost,
			Network:  tapsdk.NetworkRegtest,
			TLS:      taprest.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromPath(macPath),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		info, err := client.GetInfo(t.Context())
		require.NoError(t, err)
		require.Equal(t, "regtest", info.Network)
	})

	t.Run("rest wrong tls cert rejected", func(t *testing.T) {
		bogusTLS := writeTempFile(t, "bogus-tls.cert",
			bogusTLSPEM(t),
		)

		client, err := taprest.NewClient(&taprest.Config{
			BaseURL:  defaultAliceRestHost,
			Network:  tapsdk.NetworkRegtest,
			TLS:      taprest.TLSFromPath(bogusTLS),
			Macaroon: tapsdk.MacaroonFromPath(macPath),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.GetInfo(t.Context())
		require.Error(t, err,
			"expected rest transport to reject wrong trust root")
	})

	t.Run("rest missing macaroon rejected", func(t *testing.T) {
		_, err := taprest.NewClient(&taprest.Config{
			BaseURL: defaultAliceRestHost,
			Network: tapsdk.NetworkRegtest,
			TLS:     taprest.TLSFromPath(tlsPath),
		})
		require.Error(t, err,
			"expected NewClient to fail without macaroon material")
	})

	t.Run("rest invalid macaroon rejected", func(t *testing.T) {
		client, err := taprest.NewClient(&taprest.Config{
			BaseURL:  defaultAliceRestHost,
			Network:  tapsdk.NetworkRegtest,
			TLS:      taprest.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromHex("00"),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.GetInfo(t.Context())
		require.Error(t, err,
			"expected rest rejection on invalid macaroon")
	})
}

func writeTempFile(t *testing.T, name string, contents []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

// bogusTLSPEM returns a freshly-generated self-signed certificate PEM
// that tapd does not trust, so the SDK is forced to refuse the
// handshake.
func bogusTLSPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "itest-bogus"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage: x509.KeyUsageCertSign |
			x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(
		rand.Reader, template, template, &key.PublicKey, key,
	)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})
}
