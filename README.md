<div align="center">
  <h1>tap-sdk</h1>

  <p>
    <strong>Build Taproot Assets applications</strong>
  </p>

  <p>
    <a href="https://pkg.go.dev/github.com/lightninglabs/tap-sdk"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/lightninglabs/tap-sdk.svg"/></a>
    <a href="https://github.com/lightninglabs/tap-sdk/actions"><img alt="CI" src="https://github.com/lightninglabs/tap-sdk/actions/workflows/main.yml/badge.svg"/></a>
    <a href="LICENSE"><img alt="MIT Licensed" src="https://img.shields.io/badge/license-MIT-blue.svg"/></a>
    <a href="go.mod"><img alt="Go 1.26.0+" src="https://img.shields.io/badge/go-1.26.0%2B-lightgrey.svg"/></a>
  </p>
</div>

`tap-sdk` is the application SDK for
[Taproot Assets](https://github.com/lightninglabs/taproot-assets). It wraps a
running `tapd` node with a typed, developer-facing API for issuing, receiving,
sending, proving, burning, and discovering assets.

The SDK intentionally does not mirror tapd one-to-one. It exposes the asset
model developers usually want:

- `AssetRef` as the stable handle for assets across wallet, issuer, proof,
  burn, balance, event, and universe flows.
- `Asset`, `Collection`, and `Issuance` as distinct business concepts.
- High-level `Wallet`, `Issuer`, and `Universe` surfaces for common workflows.
- Direct `grpc` and `rest` transport packages for connection setup and advanced RPC-shaped access.

The Go package is the first implementation. The API model is designed to be
portable to TypeScript, Rust, Python, Kotlin, and Swift bindings over time.

## Install

```bash
go get github.com/lightninglabs/tap-sdk
```

## Compatibility

| tap-sdk | tapd / Taproot Assets | lnd | Go |
|---------|------------------------|-----|----|
| `main` | tapd `main` after v0.8.0 | v0.21.0-beta or newer | 1.26.0+ |
| `v0.1.x` | v0.8.0 or newer | v0.21.0-beta or newer | 1.25.10+ |

Older `tapd` versions are unsupported. See [Compatibility](docs/compatibility.md) for the detailed matrix.

The first public SDK tag is `v0.1.0`. The SDK remains pre-v1 because some
Taproot Assets workflows are intentionally still outside the current surface
and the API has not yet been broadly exercised by external developers.

## Quick Start

```go
package main

import (
	"context"
	"log"

	tapsdk "github.com/lightninglabs/tap-sdk"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
)

func main() {
	ctx := context.Background()

	client, err := tapgrpc.NewClient(&tapgrpc.Config{
		Host:     "localhost:10029",
		Network:  tapsdk.NetworkRegtest,
		TLS:      tapgrpc.TLSFromPath("/path/to/tls.cert"),
		Macaroon: tapsdk.MacaroonFromPath("/path/to/admin.macaroon"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
	issuer := wallet.NewIssuer()

	token, err := issuer.CreateFungible(ctx, tapsdk.FungibleAssetSpec{
		Name:   "example-token",
		Amount: 1_000_000,
	})
	if err != nil {
		log.Fatal(err)
	}

	balance, err := wallet.GetBalance(ctx, token.AssetRef)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("asset=%s balance=%d", token.AssetRef, balance)
}
```

## Packages

| Package | Role |
|---------|------|
| `github.com/lightninglabs/tap-sdk` | Public asset model, `Wallet`, `Issuer`, `Universe`, builders, errors, and all business types |
| `github.com/lightninglabs/tap-sdk/grpc` | gRPC transport, TLS config, macaroon auth, tapd marshal/unmarshal |
| `github.com/lightninglabs/tap-sdk/rest` | REST transport, TLS config, macaroon auth, WebSocket event streams |
| `github.com/lightninglabs/tap-sdk/macaroon` | Low-level macaroon source helpers |

Most application code imports the root package plus one transport package.
Advanced integrations can use `wallet.Client()` to reach low-level methods
without importing `taprpc`.

## What You Can Build Today

- Wallet apps that issue, receive, send, burn, and list Taproot Assets.
- Services that mint fungibles, standalone NFTs, and NFT collections.
- Indexing and discovery tools backed by universe roots and proofs.
- Proof import/export flows for out-of-band delivery.
- Ownership proof flows for proving wallet control of assets.
- Regtest-backed test suites that exercise both gRPC and REST transports.

Lightning-native Taproot Assets flows such as RFQ, price oracles, asset
channels, and Portfolio Pilot are intentionally outside the current SDK
surface.

## Demos

- [Remote Signing Coordinator](demos/remote-signing-coordinator/README.md) -
  runnable regtest demo for reviewing and signing external Issuance requests
  through a Go coordinator and Next.js dashboard.

## Documentation

- [Getting Started](docs/getting-started.md)
- [Asset Model](docs/asset-model.md)
- [Transports and Auth](docs/transports.md)
- [Compatibility](docs/compatibility.md)
- [Architecture](docs/architecture.md)
- [Design Decisions](docs/design/README.md)
- [Integration Tests](itest/README.md)
- [Contributing](CONTRIBUTING.md)

## License

Licensed under the [MIT License](LICENSE).
