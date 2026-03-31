// Package internal provides shared helpers for tap-sdk examples.
//
// This is NOT part of the public SDK surface — it exists only to
// reduce boilerplate across example programs.
package internal

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/tap-sdk/entities"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
)

// EnvOr returns the value of the environment variable key, or
// fallback if unset.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ParseNetwork maps a string to an entities.Network value.
func ParseNetwork(s string) entities.Network {
	switch s {
	case "mainnet":
		return entities.NetworkMainnet
	case "testnet":
		return entities.NetworkTestnet
	case "testnet4":
		return entities.NetworkTestnet4
	case "signet":
		return entities.NetworkSignet
	case "simnet":
		return entities.NetworkSimnet
	default:
		return entities.NetworkRegtest
	}
}

// DefaultMacaroonPath returns the default admin macaroon path for
// the given network.
func DefaultMacaroonPath(network string) string {
	tapdDir := btcutil.AppDataDir("tapd", false)
	return filepath.Join(
		tapdDir, "data", network, "admin.macaroon",
	)
}

// ConfigFromEnv builds a grpc.Config from environment variables.
func ConfigFromEnv() *tapgrpc.Config {
	network := EnvOr("TAPD_NETWORK", "regtest")
	return &tapgrpc.Config{
		Host:         EnvOr("TAPD_HOST", "localhost:10029"),
		Network:      ParseNetwork(network),
		TLSPath:      EnvOr("TAPD_TLS_PATH", ""),
		MacaroonPath: EnvOr("TAPD_MACAROON_PATH", DefaultMacaroonPath(network)),
	}
}

// MustConnect creates a gRPC client using ConfigFromEnv or exits
// on failure.
func MustConnect() *tapgrpc.Client {
	cfg := ConfigFromEnv()

	client, err := tapgrpc.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Failed to connect to tapd at %s: %v\n",
			cfg.Host, err)
		os.Exit(1)
	}

	return client
}
