package rest

import (
	"fmt"

	"github.com/lightninglabs/tap-sdk/macaroon"
)

// Client holds the HTTP transport and sub-clients for communicating with
// the tapd REST API. It satisfies the same tapsdk.Client interface as
// grpc.Client.
type Client struct {
	*walletClient
	*walletKitClient
	*proofClient
	*universeClient
	*mintClient

	transport *transport
}

// NewClient creates a REST-based Client that satisfies all tap-sdk
// sub-client interfaces. The Config mirrors grpc.Config but points at
// the REST endpoint (https://host:8089 by default).
func NewClient(cfg *Config) (*Client, error) {
	// Validate mutual-exclusivity constraints.
	macOptions := []string{
		cfg.MacaroonDir, cfg.MacaroonPath, cfg.MacaroonHex,
	}
	macCount := 0
	for _, opt := range macOptions {
		if opt != "" {
			macCount++
		}
	}

	if macCount > 1 {
		return nil, ErrMacaroonConflict
	}

	macDir, err := cfg.macaroonDir()
	if err != nil && cfg.MacaroonPath == "" && cfg.MacaroonHex == "" {
		return nil, err
	}

	macaroons, err := macaroon.NewPouch(
		macDir, cfg.MacaroonPath, cfg.MacaroonHex,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read macaroons: %w", err)
	}

	tp, err := newTransport(cfg, macaroons)
	if err != nil {
		return nil, err
	}

	return &Client{
		walletClient:    newWalletClient(tp),
		walletKitClient: newWalletKitClient(tp),
		proofClient:     newProofClient(tp),
		universeClient:  newUniverseClient(tp),
		mintClient:      newMintClient(tp),
		transport:       tp,
	}, nil
}

// Close is a no-op for REST clients (no persistent connection to
// close). It exists to satisfy the tapsdk.Client interface.
func (c *Client) Close() error {
	return nil
}
