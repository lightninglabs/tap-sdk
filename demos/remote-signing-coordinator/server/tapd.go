package main

import (
	"fmt"
	"strings"

	tapsdk "github.com/lightninglabs/tap-sdk"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	taprest "github.com/lightninglabs/tap-sdk/rest"
)

func newTapClient(cfg config) (tapsdk.Client, error) {
	switch strings.ToLower(cfg.transport) {
	case "", "grpc":
		tlsSource := tapgrpc.TLSFromPath(cfg.tlsPath)
		if cfg.tlsPath == "" {
			tlsSource = nil
		}
		if cfg.tlsInsecure {
			tlsSource = tapgrpc.TLSInsecure()
		}

		return tapgrpc.NewClient(&tapgrpc.Config{
			Host:     cfg.tapdHost,
			Network:  cfg.network,
			TLS:      tlsSource,
			Macaroon: macaroonSource(cfg.macaroonPath),
		})

	case "rest":
		tlsSource := taprest.TLSFromPath(cfg.tlsPath)
		if cfg.tlsPath == "" {
			tlsSource = nil
		}
		if cfg.tlsInsecure {
			tlsSource = taprest.TLSInsecure()
		}

		return taprest.NewClient(&taprest.Config{
			BaseURL:  cfg.tapdBaseURL,
			Network:  cfg.network,
			TLS:      tlsSource,
			Macaroon: macaroonSource(cfg.macaroonPath),
		})

	default:
		return nil, fmt.Errorf("unsupported transport: %s", cfg.transport)
	}
}

func macaroonSource(path string) tapsdk.MacaroonSource {
	if path == "" {
		return nil
	}

	return tapsdk.MacaroonFromPath(path)
}
