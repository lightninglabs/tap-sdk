# Getting Started

This guide shows the common SDK flow: connect to `tapd`, create assets, receive
and send them, inspect balances, export proofs, and query the universe.

## Prerequisites

- Go 1.25.10+
- `tapd` v0.8.0 or newer
- TLS certificate and macaroon for the `tapd` node

For local development, the integration-test stack in [itest](../itest) starts
bitcoind, lnd, and two tapd nodes in regtest.

## Install

```bash
go get github.com/lightninglabs/tap-sdk
```

## Connect

Most applications import the root package and one transport package.

```go
client, err := tapgrpc.NewClient(&tapgrpc.Config{
    Host:     "localhost:10029",
    Network:  tapsdk.NetworkRegtest,
    TLS:      tapgrpc.TLSFromPath("/path/to/tls.cert"),
    Macaroon: tapsdk.MacaroonFromPath("/path/to/admin.macaroon"),
})
if err != nil {
    return err
}
defer client.Close()

wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
```

REST uses the same root SDK types:

```go
client, err := taprest.NewClient(&taprest.Config{
    BaseURL:  "https://localhost:8089",
    Network:  tapsdk.NetworkRegtest,
    TLS:      taprest.TLSFromPath("/path/to/tls.cert"),
    Macaroon: tapsdk.MacaroonFromPath("/path/to/admin.macaroon"),
})
```

## Issue a Fungible Asset

```go
issuer := wallet.NewIssuer()

token, err := issuer.CreateFungible(ctx, tapsdk.FungibleAssetSpec{
    Name:   "example-token",
    Amount: 1_000_000,
})
if err != nil {
    return err
}

_, err = issuer.IssueFungible(ctx, token.AssetRef, 500_000)
if err != nil {
    return err
}
```

Use `token.AssetRef` everywhere else in the SDK. You do not need to choose
between an asset ID and group key.

## Create NFTs and Collections

```go
nft, err := issuer.CreateNFT(ctx, tapsdk.NFTSpec{
    Name: "ticket-001",
})
if err != nil {
    return err
}

created, err := issuer.CreateCollection(ctx, tapsdk.NFTSpec{
    Name: "series-001",
})
if err != nil {
    return err
}

item, err := issuer.MintCollectionItem(
    ctx, created.Collection.AssetRef, tapsdk.NFTSpec{
        Name: "series-002",
    },
)
```

`nft` and `item` are assets. `created.Collection` is a collection.

## Receive and Send

The high-level receive helper defaults to V2 addresses.

```go
addr, err := wallet.NewReceiveAddress(ctx, token.AssetRef)
if err != nil {
    return err
}
```

Send with an explicit amount:

```go
transfer, err := wallet.Send(ctx, addr.Encoded, tapsdk.WithAmount(100))
if err != nil {
    return err
}

fmt.Println(transfer.AnchorTxid)
```

Send multiple recipients of the same asset:

```go
batch, err := wallet.SendMulti(ctx, []tapsdk.Recipient{
    tapsdk.RecipientWithAmount(firstAddr, 100),
    tapsdk.RecipientWithEmbeddedAmount(secondAddr),
})
```

## Balances and Assets

```go
balance, err := wallet.GetBalance(ctx, token.AssetRef)
if err != nil {
    return err
}

assets, err := wallet.ListAssets(ctx, &tapsdk.ListAssetsRequest{})
if err != nil {
    return err
}

collections, err := wallet.ListCollections(
    ctx, &tapsdk.ListCollectionsRequest{},
)
```

`ListAssets` returns fungible assets and concrete NFT item assets.
`ListCollections` returns collection objects.

## Proofs

Export one or more proof entries for an `AssetRef`:

```go
bundle, err := wallet.ExportProof(ctx, token.AssetRef)
if err != nil {
    return err
}
```

Import a proof bundle received out of band:

```go
registered, err := wallet.ImportProof(ctx, bundle)
if err != nil {
    return err
}

fmt.Println(len(registered))
```

When a group `AssetRef` maps to multiple issuances, proof export returns the
set of proof entries needed for that asset.

## Burns

```go
burn, err := wallet.Burn(ctx, token.AssetRef, 10,
    tapsdk.WithBurnNote("redeemed"),
)
if err != nil {
    return err
}

fmt.Println(burn.Amount)
```

## Universe

```go
universe := wallet.NewUniverse()

known, err := universe.HasAsset(ctx, token.AssetRef)
if err != nil {
    return err
}

proofs, err := universe.ListProofs(ctx, token.AssetRef)
if err != nil {
    return err
}
```

Universe sync hosts should come from trusted configuration:

```go
_, err = universe.SyncAsset(ctx, token.AssetRef, "tapd.example:10029")
```

Current tapd versions perform the remote universe dial. Do not pass direct user
input as the sync host.

## Workflow References

The integration suite doubles as executable workflow documentation. Use these
tests when a flow needs a funded regtest stack instead of a short snippet:

- Connect and inspect node info: [`itest/info_test.go`](../itest/info_test.go).
- Issue fungibles, NFTs, and collection items:
  [`itest/issuer_test.go`](../itest/issuer_test.go) and
  [`itest/mint_test.go`](../itest/mint_test.go).
- Receive and send assets: [`itest/send_test.go`](../itest/send_test.go).
- Build interactive transfers and VPSBTs:
  [`itest/builder_test.go`](../itest/builder_test.go).
- Export, import, and verify proofs:
  [`itest/proof_test.go`](../itest/proof_test.go).
- Query universe roots and proofs:
  [`itest/universe_test.go`](../itest/universe_test.go).
- Burn assets and inspect protocol records:
  [`itest/wallet_surface_test.go`](../itest/wallet_surface_test.go).

The [integration test guide](../itest/README.md) explains how to start the
regtest stack and run individual workflow tests.

## Next Steps

- Read the [Asset Model](asset-model.md).
- Configure production [Transports and Auth](transports.md).
- Run the [Integration Tests](../itest/README.md) against a local regtest
  stack.
