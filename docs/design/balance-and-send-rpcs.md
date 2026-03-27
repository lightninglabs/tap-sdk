# Balance Queries and One-Shot Sends Design

## Overview

This document specifies the design for adding two missing high-priority
TaprootAssets RPC wrappers to `tap-sdk`:

1. `ListBalances`
2. `SendAsset`

These two RPCs cover the most common read and write flows that are still
missing from the root `WalletClient` surface:

- querying confirmed wallet balances without manually aggregating assets
- performing simple address-based sends without stepping through the full
  builder pipeline

## Scope

This design intentionally focuses on the currently available high-priority
TaprootAssets RPCs in `taprpc` v1.0.12.

The original issue text for #15 also mentions `FetchAsset`, but that RPC is
not present in the current upstream `taprootassets.proto`. Rather than invent
an SDK abstraction for a non-existent server method, this work wraps the
high-priority RPCs that do exist today and leaves any future `FetchAsset`
addition to a separate upstream-driven change.

## Goals

- Expose `ListBalances` through SDK-native request and response types
- Expose `SendAsset` through SDK-native request types and return the existing
  `AssetTransfer` type
- Reuse the existing `AssetTransfer` unmarshaling path for send responses
- Extend script-key-type filtering so the SDK can express both `all_types`
  and `explicit_type` queries
- Preserve the SDK rule that no `taprpc` types leak through the public API

## Non-Goals

- Wrapping the remaining medium- and low-priority TaprootAssets RPCs
- Introducing a higher-level `Wallet.Send` convenience method in this change
- Replacing the transaction-builder flow for advanced sends
- Client-side validation beyond lightweight SDK defaults

## SDK Surface

### `entities.ScriptKeyType`

`ListBalances` supports filtering by script key type. The SDK therefore needs
an enum that mirrors the upstream values:

- `Unknown`
- `BIP86`
- `ScriptPathExternal`
- `Burn`
- `Tombstone`
- `Channel`
- `UniquePedersen`

The existing `ScriptKeyTypeQuery` will be extended to support:

- `ExplicitType *ScriptKeyType`
- `AllTypes bool`

This change also benefits `ListAssets`, which already accepts a
`ScriptKeyTypeQuery` but could previously express only `all_types`.

### `entities.ListBalancesRequest`

```go
type ListBalancesRequest struct {
    GroupBy         BalanceGroupBy
    AssetFilter     *AssetID
    GroupKeyFilter  *PubKey
    IncludeLeased   bool
    ScriptKeyType   *ScriptKeyTypeQuery
}
```

`GroupBy` is modeled as an SDK enum rather than a pair of booleans because the
RPC accepts a oneof and the SDK should make the intent explicit.

The zero value defaults to grouping by asset ID, which gives callers a useful
result without requiring extra configuration.

### `entities.ListBalancesResponse`

The response preserves the daemon's grouping semantics by exposing maps keyed
by the same strings returned from `tapd`:

```go
type ListBalancesResponse struct {
    AssetBalances        map[string]*AssetBalance
    AssetGroupBalances   map[string]*AssetGroupBalance
    UnconfirmedTransfers uint64
}
```

This avoids losing information from the upstream response while still hiding
all proto types.

### `entities.SendAssetRequest`

```go
type SendAssetRequest struct {
    TapAddresses                []string
    FeeRate                     uint32
    Label                       string
    SkipProofCourierPingCheck   bool
    Recipients                  []Recipient
}
```

The upstream RPC supports two mutually exclusive input styles:

1. `tap_addrs`, where the address already encodes the amount
2. `addresses_with_amounts`, where the sender supplies the amount explicitly

The SDK models the second form as `[]Recipient` because the shape already
exists and is semantically identical.

If `Recipients` is set, the SDK marshaler will populate
`addresses_with_amounts` and clear `tap_addrs`.

## Interface Changes

`WalletClient` gains two additive methods:

```go
ListBalances(ctx context.Context,
    req *entities.ListBalancesRequest) (*entities.ListBalancesResponse, error)

SendAsset(ctx context.Context,
    req *entities.SendAssetRequest) (*entities.AssetTransfer, error)
```

## gRPC Mapping

| SDK Method | RPC | Request Mapping | Response Mapping |
|------------|-----|-----------------|------------------|
| `ListBalances` | `TaprootAssets.ListBalances` | `entities.ListBalancesRequest` → `taprpc.ListBalancesRequest` | `taprpc.ListBalancesResponse` → `entities.ListBalancesResponse` |
| `SendAsset` | `TaprootAssets.SendAsset` | `entities.SendAssetRequest` → `taprpc.SendAssetRequest` | `taprpc.SendAssetResponse.Transfer` → `entities.AssetTransfer` |

## Marshaling Details

### `ListBalances`

- `GroupByAssetID` maps to `group_by.asset_id = true`
- `GroupByGroupKey` maps to `group_by.group_key = true`
- zero-value `GroupBy` defaults to `asset_id = true`
- `AssetFilter` and `GroupKeyFilter` copy directly to their byte fields
- `ScriptKeyTypeQuery` maps to either `explicit_type` or `all_types`

### `SendAsset`

- `TapAddresses` maps to `tap_addrs`
- `Recipients` maps to `addresses_with_amounts`
- if `Recipients` is non-empty, `tap_addrs` is cleared to preserve the RPC's
  mutual exclusivity requirement
- the RPC response already returns an `AssetTransfer`, so the SDK can reuse
  `unmarshalAssetTransfer`

## Error Handling

This change follows the existing SDK approach:

- transport and server errors are returned as-is from the gRPC layer
- higher-level callers may wrap them in the SDK `Error` type
- request-shape conflicts remain server-validated rather than duplicated in
  the SDK

The only SDK-side defaulting is the `ListBalances` grouping default to asset
ID.

## Testing Strategy

### Unit Tests

Add table-driven tests for:

- `marshalListBalancesRequest`
- `marshalSendAssetRequest`
- `unmarshalAssetBalance`
- `unmarshalAssetGroupBalance`
- `ScriptKeyType` enum alignment with upstream constants

These tests are sufficient for this change because the new RPC methods reuse
existing request/response plumbing patterns already present in the package.

## Migration Notes

This is a purely additive change:

- existing code keeps compiling
- `ListAssets` callers can optionally start using explicit script-key-type
  filters
- SDK consumers now have native access to balance queries and one-shot sends

## References

- `taprpc/taprootassets.proto`
- issue #15
- `grpc/wallet_client.go`
- `clients.go`
