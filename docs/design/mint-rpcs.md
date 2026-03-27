# Mint RPC wrappers

## Context

Issue #17 adds minting support to `tap-sdk`.

The Mint subserver is the first service in the SDK that is not part of the
main `TaprootAssets` or `AssetWallet` APIs. That means we need a new client
surface, new SDK-native entities, and a clear boundary for optional mint-only
concepts such as batch siblings and external group keys.

## Goals

- Add a `MintClient` to the public SDK surface.
- Wrap the high-priority Mint RPCs first:
  - `MintAsset`
  - `FinalizeBatch`
- Keep all `mintrpc` and `taprpc` types inside `grpc/`.
- Model mint batches with SDK-native entities that can be reused by later
  Mint RPCs.
- Cover all new marshal and unmarshal paths with unit tests.

## Non-goals

- No `MintBuilder` yet.
- No event streaming, batch listing, or cancellation in this change.
- No new high-level wallet sugar beyond the promoted client methods.

## API shape

### Client surface

`Client` will embed a new `MintClient` interface:

```go
type MintClient interface {
    MintAsset(ctx context.Context,
        req *entities.MintAssetRequest) (*entities.MintingBatch, error)

    FinalizeBatch(ctx context.Context,
        req *entities.FinalizeBatchRequest) (*entities.MintingBatch, error)
}
```

This keeps the SDK shape consistent with the existing wallet, proof, and
universe sub-clients. The high-level `Wallet` already embeds `Client`, so the
new methods are available there automatically without introducing another
facade layer.

### Entities

Add `entities/mint.go` with:

- `MintAssetRequest`
- `FinalizeBatchRequest`
- `MintAsset`
- `PendingMintAsset`
- `MintingBatch`
- `BatchState`
- `AssetMeta`
- `ExternalKey`
- `BatchSibling`, `TapscriptFullTree`, `TapLeaf`, `TapBranch`

The split between `MintAsset` and `PendingMintAsset` matters because the RPC
request supports fields, such as `grouped_asset`, `decimal_display`, and
`external_group_key`, that are not present in a pending batch response.

## gRPC boundary

Add `grpc/mint_client.go` backed by `mintrpc.MintClient`.

### Authentication

Mint RPCs require `mint` permissions. Today the SDK only manages the standard
service macaroons. The admin macaroon already carries the needed permissions,
so the mint client will use the admin macaroon for now.

### Request mapping

- `entities.MintAssetRequest` maps to `mintrpc.MintAssetRequest`.
- `entities.FinalizeBatchRequest` maps to
  `mintrpc.FinalizeBatchRequest`.
- Batch sibling oneofs are represented as a small SDK union:
  `BatchSibling{FullTree|Branch}`.

### Response mapping

Both wrapped RPCs return a `MintingBatch`. Returning the batch directly is more
useful to SDK consumers than exposing thin response wrappers that only contain a
single field.

## Testing

Add table-driven unit tests for:

- mint request marshaling
- finalize request marshaling
- batch unmarshaling
- invalid key and hash lengths on mint batch decoding

## Follow-up work

Once this lands, the next Mint slices can reuse the same entity set for:

- `FundBatch`
- `CancelBatch`
- `ListBatches`
- `SubscribeMintEvents`
