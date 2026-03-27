# tap-sdk

The official Go SDK for building applications on the
[Taproot Assets](https://github.com/lightninglabs/taproot-assets) protocol.

`tap-sdk` wraps the `tapd` gRPC interface and provides typed, composable
building blocks for Taproot Assets workflows. It exposes pure Go types so
consumers never need to import `taprpc` or other `taproot-assets` packages
directly.

## Status

**Pre-v1.0** — The API is evolving. See [DEVELOPMENT_CYCLE.md](DEVELOPMENT_CYCLE.md)
and [CHANGELOG.md](CHANGELOG.md) for details.

## Installation

```bash
go get github.com/lightninglabs/tap-sdk
```

## Quick Start

### Connect to tapd

```go
package main

import (
	"context"
	"fmt"
	"log"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/grpc"
)

func main() {
	// Connect using default tapd paths for TLS and macaroons.
	client, err := grpc.NewClient(&grpc.Config{
		Host:    "localhost:10029",
		Network: entities.NetworkRegtest,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Create the high-level wallet entrypoint.
	wallet := tapsdk.NewWallet(client, entities.NetworkRegtest)

	// Query node info.
	ctx := context.Background()
	info, err := wallet.GetInfo(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("tapd %s on %s (block %d)\n",
		info.Version, info.Network, info.BlockHeight)
}
```

### Send Assets (Address-Based)

Address-based sends use Taproot Asset addresses with automatic proof delivery
via a proof courier.

```go
// Build and execute a transfer.
packet, err := wallet.NewTxBuilder().
	AddRecipient(recipientAddr, 100).
	SetFeeRate(2).
	Execute(ctx, false)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Anchor tx: %x\n", packet.AnchorTransaction)
```

### Send Assets (Interactive)

Interactive sends require the receiver to provide their keys directly. The
sender must deliver the proof file out-of-band after completion.

```go
// Receiver: derive keys and share them with the sender.
keys, err := wallet.DeriveKeys(ctx)
if err != nil {
	log.Fatal(err)
}
// Share `keys` with the sender via your application protocol.

// Sender: build and execute the interactive transfer.
transfer, err := wallet.NewInteractiveTxBuilder().
	SetAsset(assetID, 500).
	SetReceiverKeys(*keys).
	Execute(ctx)
if err != nil {
	log.Fatal(err)
}
// Deliver proof to receiver (transfer contains proof data).
```

### Receive Assets (Interactive)

```go
// Receiver: import the proof file from the sender.
registered, err := wallet.ImportProof(ctx, proofFile)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Received %d units of %s\n",
	registered.Amount, registered.AssetID)
```

### Receive Assets (Address-Based)

```go
// Generate a V2 address to receive any asset from a group.
addr, err := wallet.NewReceiveAddress(ctx, groupKey)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Share this address: %s\n", addr.Encoded)

// Poll for incoming transfers.
events, err := wallet.AddrReceives(ctx,
	&entities.AddressReceivesQuery{
		FilterAddr: addr.Encoded,
	},
)
```

## Architecture

```
tap-sdk/
├── Root package      High-level Wallet, TxBuilder, InteractiveTxBuilder
├── entities/         Pure Go domain types (no proto dependencies)
├── grpc/             gRPC client implementations (internal boundary)
├── vpsbt/            Virtual PSBT encoding for interactive transfers
├── codec/            Cryptographic utilities (alt-leaves, STXO derivation)
├── macaroon/         Authentication helpers
└── docs/
    └── design/       Architecture decision records
```

### Design Principles

1. **SDK types only.** Users interact exclusively with types defined in
   `tap-sdk` and `tap-sdk/entities`. No `taprpc` imports needed.

2. **gRPC is hidden.** All protobuf conversions happen inside `grpc/`.
   The transport layer is an implementation detail.

3. **Wallet is the entrypoint.** Common workflows are accessible through
   `Wallet` methods. Power users can use `Client` interfaces directly.

4. **Builders for complex flows.** `TxBuilder` and `InteractiveTxBuilder`
   guide users through multi-step transfer workflows with compile-time safety.

5. **Opinionated defaults.** The SDK picks sensible defaults (fee rates,
   address versions, key derivation) so the common case is simple.

See [docs/architecture.md](docs/architecture.md) for detailed design.

## Client Interface

The SDK exposes a single `Client` interface that embeds four specialized
clients:

| Interface | Service | Operations |
|-----------|---------|------------|
| `WalletClient` | TaprootAssets | GetInfo, ListAssets, ListTransfers, NewAddr, DecodeAddr, QueryAddrs, AddrReceives |
| `WalletKitClient` | AssetWallet | DeriveScriptKey, DeriveInternalKey, Fund, Sign, Commit, Publish, Anchor |
| `ProofClient` | TaprootAssets | ExportProof, DecodeProof, UnpackProofFile, RegisterTransfer |
| `UniverseClient` | Universe | InsertProof |

## Error Handling

SDK errors wrap gRPC errors with operation context:

```go
transfer, err := wallet.ImportProof(ctx, proofFile)
if err != nil {
	var sdkErr *tapsdk.Error
	if errors.As(err, &sdkErr) {
		fmt.Printf("Operation: %s\n", sdkErr.Op)

		if sdkErr.IsNotFound() {
			fmt.Println("Asset or proof not found")
		}
		if sdkErr.IsUnavailable() {
			fmt.Println("tapd is not reachable")
		}
	}
}
```

## Requirements

- Go 1.25.7+
- A running `tapd` instance with gRPC access
- TLS certificate and macaroon files (or hex-encoded macaroon)

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code style,
and contribution guidelines.

```bash
make build    # Build
make unit     # Run tests
make lint     # Run linter
make fmt      # Format code
```

## License

See [LICENSE](LICENSE).
