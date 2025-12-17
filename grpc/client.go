package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

	return &Client{
		walletClient:    walletClient,
		walletKitClient: walletKitClient,
		proofClient:     proofClient,
		universeClient:  universeClient,
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
	creds, err := getTLSCredentials(
		cfg.TLSData, cfg.TLSPath, cfg.Insecure, cfg.SystemCert,
	)
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

// getTLSCredentials gets the tls credentials, whether provided as straight-up
// data or a path to a certificate file.
func getTLSCredentials(tlsData, tlsPath string, insecure,
	systemCert bool) (credentials.TransportCredentials, error) {

	// We'll determine if the tls certificate is passed in directly as
	// data, by a path, or try the system's certificate chain, and then
	// load it.
	var creds credentials.TransportCredentials
	switch {
	case tlsPath != "" && tlsData != "":
		return nil, fmt.Errorf("must set only one: TLSPath or TLSData")

	case insecure && systemCert:
		return nil, fmt.Errorf("cannot set insecure and system cert " +
			"at the same time")

	case insecure:
		// If we don't need to use tls, such as if we're connecting to
		// tapd via a bufconn, then we'll skip verification.
		creds = credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true, // nolint:gosec
		})

	case systemCert:
		// Fallback to the system pool. Using an empty tls config is an
		// alternative to x509.SystemCertPool(), which is not supported
		// on Windows.
		creds = credentials.NewTLS(&tls.Config{})

	case tlsData != "":
		tlsBytes := []byte(tlsData)

		block, _ := pem.Decode(tlsBytes)
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

		// Load the specified TLS certificate and build transport
		// credentials.
		creds = credentials.NewClientTLSFromCert(pool, "")

	case tlsPath != "":
		var err error
		creds, err = credentials.NewClientTLSFromFile(tlsPath, "")
		if err != nil {
			return nil, err
		}

	default:
		// If neither tlsData nor tlsPath were set, we'll try the
		// default tls cert path.
		_, err := os.Stat(defaultTLSCertPath)
		if err != nil {
			return nil, fmt.Errorf("couldn't find out if default "+
				"TLS cert at %s exists: %v",
				defaultTLSCertPath, err)
		}
		creds, err = credentials.NewClientTLSFromFile(
			defaultTLSCertPath, "",
		)
		if err != nil {
			return nil, fmt.Errorf("couldn't load default "+
				"TLS cert at %s: %v", defaultTLSCertPath, err)
		}
	}

	return creds, nil
}
