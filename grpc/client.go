package grpc

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Client holds the connection to the tapd daemon and the sub-clients.
type Client struct {
	*walletClient
	*walletKitClient
	*proofClient
	*universeClient
	*mintClient

	grpcConn  *grpc.ClientConn
	macaroons macaroon.Pouch
}

// NewClient creates a new Client instance.
func NewClient(cfg *Config) (*Client, error) {
	// Of the macaroon directory, the custom macaroon path, and the custom
	// macaroon hex, we only allow one to be set at once. If all are empty,
	// that's fine, the default behavior is to use tapd's default directory
	// to try to locate the macaroons.
	macaroonOptions := []string{
		cfg.MacaroonDir,
		cfg.MacaroonPath,
		cfg.MacaroonHex,
	}
	macOptionCount := 0
	for _, option := range macaroonOptions {
		if option != "" {
			macOptionCount++
		}
	}
	if macOptionCount > 1 {
		return nil, fmt.Errorf("must set only one: MacaroonDir, " +
			"MacaroonPath, or MacaroonHex")
	}

	// Based on the network, if the macaroon directory isn't set, then
	// we'll use the expected default locations.
	macaroonDir := cfg.MacaroonDir
	if macaroonDir == "" {
		switch cfg.Network {
		case entities.NetworkTestnet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "testnet",
			)

		case entities.NetworkTestnet4:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "testnet4",
			)

		case entities.NetworkMainnet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "mainnet",
			)

		case entities.NetworkSimnet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "simnet",
			)

		case entities.NetworkSignet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "signet",
			)

		case entities.NetworkRegtest:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "regtest",
			)

		default:
			return nil, fmt.Errorf("unsupported network: %v",
				cfg.Network)
		}
	}

	conn, err := getClientConn(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.RPCTimeout == 0 {
		cfg.RPCTimeout = defaultRPCTimeout
	}

	macaroons, err := macaroon.NewPouch(
		macaroonDir, cfg.MacaroonPath, cfg.MacaroonHex,
	)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read macaroons: %v", err)
	}

	walletClient := NewWalletClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.AdminServiceMac],
	)
	walletKitClient := NewWalletKitClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.WalletKitServiceMac],
	)
	proofClient := NewProofClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.ProofServiceMac],
	)
	universeClient := NewUniverseClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.UniverseServiceMac],
	)
	mintClient := NewMintClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.MintServiceMac],
	)

	return &Client{
		walletClient:    walletClient,
		walletKitClient: walletKitClient,
		proofClient:     proofClient,
		universeClient:  universeClient,
		mintClient:      mintClient,
		grpcConn:        conn,
		macaroons:       macaroons,
	}, nil
}

// Close tears down the underlying gRPC connection.
func (c *Client) Close() error {
	if c.grpcConn != nil {
		return c.grpcConn.Close()
	}

	return nil
}

// getClientConn gets a client connection to the tapd host.
func getClientConn(cfg *Config) (*grpc.ClientConn, error) {
	creds, err := getTLSCredentials(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to get tls creds: %v", err)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(maxMsgRecvSize),
	}

	conn, err := grpc.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// defaultTLSMinVersion is the minimum TLS version the client will
// accept when no explicit version is configured.
const defaultTLSMinVersion = tls.VersionTLS12

// getTLSCredentials builds the gRPC transport credentials from the
// client configuration. It enforces a minimum TLS version floor and
// optionally pins the server certificate by SHA-256 fingerprint.
func getTLSCredentials(
	cfg *Config) (credentials.TransportCredentials, error) {

	tlsData := cfg.TLSData
	tlsPath := cfg.TLSPath
	insecure := cfg.Insecure
	systemCert := cfg.SystemCert

	minVersion := cfg.TLSMinVersion
	if minVersion == 0 {
		minVersion = defaultTLSMinVersion
	}

	// We'll determine if the tls certificate is passed in directly as
	// data, by a path, or try the system's certificate chain, and then
	// load it.
	var tlsCfg *tls.Config
	switch {
	case tlsPath != "" && tlsData != "":
		return nil, fmt.Errorf("must set only one: TLSPath or " +
			"TLSData")

	case insecure && systemCert:
		return nil, fmt.Errorf("cannot set insecure and system " +
			"cert at the same time")

	case insecure:
		// If we don't need to use tls, such as if we're connecting
		// to tapd via a bufconn, then we'll skip verification.
		tlsCfg = &tls.Config{
			InsecureSkipVerify: true, // nolint:gosec
			MinVersion:         minVersion,
		}

	case systemCert:
		// Fallback to the system pool. Using an empty tls config
		// is an alternative to x509.SystemCertPool(), which is
		// not supported on Windows.
		tlsCfg = &tls.Config{
			MinVersion: minVersion,
		}

	case tlsData != "":
		pool, err := certPoolFromPEM([]byte(tlsData))
		if err != nil {
			return nil, err
		}

		tlsCfg = &tls.Config{
			RootCAs:    pool,
			MinVersion: minVersion,
		}

	case tlsPath != "":
		pool, err := certPoolFromFile(tlsPath)
		if err != nil {
			return nil, err
		}

		tlsCfg = &tls.Config{
			RootCAs:    pool,
			MinVersion: minVersion,
		}

	default:
		// If neither tlsData nor tlsPath were set, we'll try the
		// default tls cert path.
		pool, err := certPoolFromFile(defaultTLSCertPath)
		if err != nil {
			return nil, fmt.Errorf("couldn't load default "+
				"TLS cert at %s: %v",
				defaultTLSCertPath, err)
		}

		tlsCfg = &tls.Config{
			RootCAs:    pool,
			MinVersion: minVersion,
		}
	}

	// When a pinned certificate fingerprint is provided, install a
	// VerifyPeerCertificate callback that compares the SHA-256
	// digest of the leaf certificate against the expected value.
	if cfg.TLSPinnedCertFingerprint != "" {
		expected := strings.ToLower(strings.ReplaceAll(
			cfg.TLSPinnedCertFingerprint, ":", "",
		))

		if _, err := hex.DecodeString(expected); err != nil ||
			len(expected) != 64 {

			return nil, fmt.Errorf("TLSPinnedCertFingerprint "+
				"must be a 64-char hex SHA-256 digest, "+
				"got %q", cfg.TLSPinnedCertFingerprint)
		}

		tlsCfg.VerifyPeerCertificate = func(
			rawCerts [][]byte,
			_ [][]*x509.Certificate) error {

			if len(rawCerts) == 0 {
				return errors.New("server presented " +
					"no certificates")
			}

			digest := sha256.Sum256(rawCerts[0])
			actual := hex.EncodeToString(digest[:])

			if actual != expected {
				return fmt.Errorf("certificate "+
					"fingerprint mismatch: "+
					"got %s, want %s",
					actual, expected)
			}

			return nil
		}
	}

	return credentials.NewTLS(tlsCfg), nil
}

// certPoolFromPEM decodes a PEM-encoded certificate and returns a
// certificate pool containing it.
func certPoolFromPEM(pemData []byte) (*x509.CertPool, error) {
	block, _ := pem.Decode(pemData)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("failed to decode PEM block " +
			"containing tls certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return pool, nil
}

// certPoolFromFile reads a PEM certificate file and returns a
// certificate pool.
func certPoolFromFile(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read TLS cert at %s: "+
			"%v", path, err)
	}

	return certPoolFromPEM(data)
}
