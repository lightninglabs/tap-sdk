# tap-sdk

The official Go SDK for building applications on the
[Taproot Assets](https://github.com/lightninglabs/taproot-assets) protocol.

`tap-sdk` wraps the `tapd` gRPC interface and provides typed Go APIs for
common Taproot Assets workflows without exposing `taprpc` types in the public
surface.

## Status

**Pre-v1.0**. The API is evolving.

The current SDK surface requires `tapd` from Taproot Assets v0.8.0 or newer.
Older daemons are unsupported because wallet transfers must include per-row
asset type data for correct `AssetRef` projection, especially for NFT
collections.

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

### Address-based wallet operations

```go
transfer, err := wallet.Send(ctx, recipientAddr, tapsdk.WithAmount(100))
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Anchor tx: %s\n", transfer.AnchorTxid)

batch, err := wallet.SendMulti(ctx, []entities.Recipient{
	entities.RecipientWithAmount(firstRecipientAddr, 100),
	entities.RecipientWithEmbeddedAmount(secondRecipientAddr),
})
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Batch anchor tx: %s\n", batch.AnchorTxid)

burn, err := wallet.Burn(ctx, assetRef, 10, tapsdk.WithBurnNote("cleanup"))
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Burned %d units of %s\n", burn.Amount, burn.AssetRef)

transfers, err := wallet.ListTransfers(ctx, nil)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("wallet has %d outgoing transfers\n", len(transfers))
```

### Issue assets

`Wallet.NewIssuer()` is the preferred minting entrypoint. It hides tapd's
mint batch details and returns SDK business entities keyed by `AssetRef`.
Use `Wallet.Client().MintAsset` and `MintIssuance` only when you need direct
batch control.

```go
issuer := wallet.NewIssuer()

token, err := issuer.CreateFungible(ctx, entities.FungibleAssetSpec{
	Name:   "example-token",
	Amount: 1_000_000,
})
if err != nil {
	log.Fatal(err)
}

_, err = issuer.IssueFungible(ctx, token.AssetRef, 500_000)
if err != nil {
	log.Fatal(err)
}

created, err := issuer.CreateCollection(ctx, entities.NFTSpec{
	Name: "example-collection-001",
})
if err != nil {
	log.Fatal(err)
}

_, err = issuer.MintCollectionItem(ctx, created.Collection.AssetRef, entities.NFTSpec{
	Name: "example-collection-002",
})
if err != nil {
	log.Fatal(err)
}

fmt.Printf("first item: %s\n", created.FirstItem.AssetRef)
```

### Universe proofs and sync

`Wallet.NewUniverse()` is the preferred universe entrypoint. It accepts the
same `AssetRef` values returned by wallet and issuer calls.

```go
universe := wallet.NewUniverse()

known, err := universe.HasAsset(ctx, token.AssetRef)
if err != nil {
	log.Fatal(err)
}
if !known {
	log.Fatal("asset is not known to the local universe")
}

proofs, err := universe.ListProofs(ctx, token.AssetRef)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("local universe has %d proofs\n", len(proofs))

// Use a trusted universe host from configuration. Current tapd versions dial
// remote universe servers without certificate verification during sync.
_, err = universe.SyncAsset(ctx, token.AssetRef, "tapd.example:10029")
if err != nil {
	log.Fatal(err)
}
```

### Interactive receive

```go
registered, err := wallet.ImportProof(ctx, proofBundle)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Imported %d proof entries\n", len(registered))
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
