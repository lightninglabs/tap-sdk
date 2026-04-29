# tap-sdk

The official Go SDK for building applications on the
[Taproot Assets](https://github.com/lightninglabs/taproot-assets) protocol.

`tap-sdk` wraps the `tapd` gRPC interface and provides typed Go APIs for
common Taproot Assets workflows without exposing `taprpc` types in the public
surface.

## Status

**Pre-v1.0**. The API is evolving.

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
	client, err := grpc.NewClient(&grpc.Config{
		Host:    "localhost:10029",
		Network: entities.NetworkRegtest,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	wallet := tapsdk.NewWallet(client, entities.NetworkRegtest)

	ctx := context.Background()
	info, err := wallet.Client().GetInfo(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("tapd %s on %s (block %d)\n",
		info.Version, info.Network, info.BlockHeight)
}
```

### Address-based send

```go
packet, err := wallet.NewTxBuilder().
	AddRecipient(recipientAddr, 100).
	SetFeeRate(2).
	Execute(ctx, false)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Anchor tx: %x\n", packet.AnchorTransaction)
```

### Interactive receive

```go
registered, err := wallet.ImportProofFile(ctx, proofFile)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Received %d units\n", registered.Amount)
```

## Documentation

- [CHANGELOG.md](CHANGELOG.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [docs/architecture.md](docs/architecture.md)
- [DEVELOPMENT_CYCLE.md](DEVELOPMENT_CYCLE.md)

## Development

```bash
make build
make unit
make lint
make fmt
```

## License

See [LICENSE](LICENSE).
