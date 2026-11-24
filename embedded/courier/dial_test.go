//go:build !js

package courier

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/embedded"
	unirpc "github.com/lightninglabs/taproot-assets/taprpc/universerpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// selfSignedCert returns a certificate for the loopback address and a pool
// that trusts it.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "courier"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(
		rand.Reader, template, template, &privKey.PublicKey, privKey,
	)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  privKey,
	}, pool
}

// TestDialer reaches a courier at the endpoint configured for its address,
// and only with a certificate that endpoint trusts.
func TestDialer(t *testing.T) {
	cert, pool := selfSignedCert(t)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// A proof above gRPC's default limit for a received message.
	proof := bytes.Repeat([]byte{7}, 5<<20)
	server := &testServer{
		proof: &unirpc.AssetProofResponse{
			AssetLeaf: &unirpc.AssetLeaf{Proof: proof},
		},
	}
	server.serve(t, lis, grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})))

	// The address names a host that does not resolve.
	addr, err := url.Parse("authmailbox+universerpc://courier.test:10029")
	require.NoError(t, err)

	query := func(endpoint Endpoint) ([]byte, error) {
		dial := NewDialer(map[string]Endpoint{addr.Host: endpoint})
		client, err := dial(t.Context(), addr)
		require.NoError(t, err)
		defer func() { require.NoError(t, client.Close()) }()

		return client.QueryProof(t.Context(), embedded.ProofKey{})
	}

	blob, err := query(Endpoint{
		Target: lis.Addr().String(),
		TLS: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
	})
	require.NoError(t, err)
	require.Equal(t, proof, blob)

	// The system's root certificates do not vouch for the courier.
	_, err = query(Endpoint{Target: lis.Addr().String()})
	require.Equal(t, codes.Unavailable, status.Code(err), "%v", err)
	require.ErrorContains(t, err, "certificate")
}
