package rest

import (
	"path/filepath"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/tap-sdk/entities"
)

const (
	// defaultHTTPTimeout is the default timeout for HTTP requests.
	defaultHTTPTimeout = 30 * time.Second

	// defaultRESTPort is the default REST API port for tapd.
	defaultRESTPort = "8089"
)

var (
	defaultTapdDir         = btcutil.AppDataDir("tapd", false)
	defaultTLSCertFilename = "tls.cert"
	defaultTLSCertPath     = filepath.Join(
		defaultTapdDir, defaultTLSCertFilename,
	)
	defaultDataDir     = "data"
	defaultChainSubDir = "chain"
)

// Config holds configuration for connecting to a tapd REST API.
type Config struct {
	// BaseURL is the base URL of the tapd REST API, e.g.
	// "https://localhost:8089". The scheme must be https unless
	// Insecure is true.
	BaseURL string

	// Network is the bitcoin network we expect the tapd instance to
	// operate on. Used for resolving default macaroon paths.
	Network entities.Network

	// MacaroonDir is the directory where all tapd macaroons can be
	// found. Either this, MacaroonPath, or MacaroonHex should be set,
	// but only one of them.
	MacaroonDir string

	// MacaroonPath is the full path to a custom macaroon file.
	MacaroonPath string

	// MacaroonHex is a hexadecimal encoded macaroon string.
	MacaroonHex string

	// TLSPath is the path to tapd's TLS certificate file.
	TLSPath string

	// TLSData holds the TLS certificate data as a PEM string. Only
	// this or TLSPath can be set, not both.
	TLSData string

	// Insecure disables TLS verification. Use only for local
	// development with bufconn or similar.
	Insecure bool

	// SystemCert uses the system certificate pool for TLS instead of
	// tapd's self-signed certificate.
	SystemCert bool

	// Timeout is an optional custom timeout for HTTP requests. If
	// zero, defaults to 30 seconds.
	Timeout time.Duration
}

// macaroonDir resolves the macaroon directory based on the network.
func (c *Config) macaroonDir() (string, error) {
	if c.MacaroonDir != "" {
		return c.MacaroonDir, nil
	}

	var subDir string
	switch c.Network {
	case entities.NetworkTestnet:
		subDir = "testnet"
	case entities.NetworkTestnet4:
		subDir = "testnet4"
	case entities.NetworkMainnet:
		subDir = "mainnet"
	case entities.NetworkSimnet:
		subDir = "simnet"
	case entities.NetworkSignet:
		subDir = "signet"
	case entities.NetworkRegtest:
		subDir = "regtest"
	default:
		return "", ErrUnsupportedNetwork
	}

	return filepath.Join(
		defaultTapdDir, defaultDataDir, defaultChainSubDir,
		"bitcoin", subDir,
	), nil
}

// timeout returns the configured timeout or the default.
func (c *Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}

	return defaultHTTPTimeout
}
