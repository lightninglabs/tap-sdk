# Taproot Asset Address Operations Design

## Overview

This document specifies the design for adding Taproot Asset address operations
to the tap-sdk. Address operations enable SDK users to:

1. Generate addresses for receiving assets
2. Decode existing Taproot Asset addresses
3. Query previously created addresses
4. Track incoming asset transfers to addresses

All address operations are part of the TaprootAssets gRPC service and will be
added to the existing `WalletClient` interface.

## Goals

- Expose address operations through SDK-native types only
- Hide all `taprpc` types behind the SDK boundary
- Support all address versions (V0, V1, V2) with appropriate constraints
- Enable complete receive flows without direct RPC knowledge
- Maintain consistency with existing SDK patterns and code style

## Non-Goals

- Subscription-based address event streaming (future work)
- Address persistence or local caching (tapd handles this)
- Custom address derivation outside of tapd's wallet

## SDK Types

### Address

The `Address` type represents a decoded Taproot Asset address. It encapsulates
all fields from the underlying `taprpc.Addr` message.

```go
// Address represents a Taproot Asset address for receiving assets.
type Address struct {
    // Encoded is the bech32m encoded Taproot Asset address string.
    // This is the canonical representation used for sharing with senders.
    Encoded string

    // AssetID is the 32-byte asset identifier this address can receive.
    // For V2 addresses with a group key, this may be empty (zero value).
    AssetID AssetID

    // AssetType indicates whether this is a normal or collectible asset.
    AssetType AssetType

    // Amount is the number of asset units expected at this address.
    // For V2 addresses, this may be zero to allow sender-chosen amounts.
    Amount uint64

    // GroupKey is the optional group key for receiving any asset in a group.
    // Only set for V2 addresses. If set, AssetID must be empty.
    GroupKey *PubKey

    // ScriptKey is the Taproot output key the asset will be locked to.
    ScriptKey PubKey

    // InternalKey is the internal key for the on-chain anchor output.
    InternalKey PubKey

    // TapscriptSibling is the optional serialized tapscript sibling preimage.
    // Used when additional script paths exist alongside the asset commitment.
    TapscriptSibling []byte

    // TaprootOutputKey is the tweaked key for the on-chain Bitcoin output.
    TaprootOutputKey XOnlyPubKey

    // ProofCourierAddr is the address of the proof courier service.
    // For V2 addresses, this is mandatory and must be an auth mailbox URL.
    ProofCourierAddr string

    // AssetVersion is the asset version for transfers to this address.
    AssetVersion AssetVersion

    // AddressVersion is the address format version (V0, V1, or V2).
    AddressVersion AddressVersion
}
```

### AddressVersion

```go
// AddressVersion represents the version of a Taproot Asset address format.
// Values match taprpc.AddrVersion enum (0 is unspecified).
type AddressVersion uint8

const (
    // AddressVersionV0 is the initial address version using asset ID only.
    AddressVersionV0 AddressVersion = 1

    // AddressVersionV1 is the address version with improved encoding.
    AddressVersionV1 AddressVersion = 2

    // AddressVersionV2 supports group keys and optional amounts.
    AddressVersionV2 AddressVersion = 3
)
```

### AssetVersion

```go
// AssetVersion represents the version of asset encoding.
type AssetVersion uint8

const (
    // AssetVersionV0 is the initial asset version.
    AssetVersionV0 AssetVersion = 0

    // AssetVersionV1 is the asset version with witness stripping support.
    AssetVersionV1 AssetVersion = 1
)
```

### NewAddressRequest

The `NewAddressRequest` type specifies parameters for address generation.

```go
// NewAddressRequest contains parameters for generating a new Taproot Asset
// address.
type NewAddressRequest struct {
    // AssetID is the 32-byte asset identifier to receive.
    // Required for V0/V1 addresses. For V2 addresses, either AssetID or
    // GroupKey must be set, but not both.
    AssetID *AssetID

    // Amount is the number of asset units to receive.
    // Required for V0/V1 addresses. Optional for V2 addresses (zero means
    // sender chooses the amount).
    Amount uint64

    // GroupKey is the group key to receive any asset from a group.
    // Only valid for V2 addresses. If set, AssetID must be nil.
    GroupKey *PubKey

    // ScriptKey is an optional custom script key for the receiving asset.
    // If nil, tapd derives a BIP-86 key from its wallet.
    // NOTE: If set, InternalKey must also be set.
    ScriptKey *ScriptKey

    // InternalKey is an optional custom internal key for the anchor output.
    // If nil, tapd derives a key from its wallet.
    // NOTE: If set, ScriptKey must also be set.
    InternalKey *KeyDescriptor

    // TapscriptSibling is an optional tapscript sibling preimage for
    // additional script paths in the Taproot tree.
    TapscriptSibling []byte

    // ProofCourierAddr is the optional proof courier address.
    // If empty, tapd uses its configured default.
    // For V2 addresses, must be a valid auth mailbox URL.
    ProofCourierAddr string

    // AssetVersion is the asset version for transfers to this address.
    // Defaults to the latest version if not specified.
    AssetVersion *AssetVersion

    // AddressVersion is the address format version to use.
    // Defaults to the latest version if not specified.
    AddressVersion *AddressVersion

    // SkipProofCourierConnCheck skips the connectivity check to the proof
    // courier when creating the address. Useful for offline address creation.
    SkipProofCourierConnCheck bool
}
```

### AddressQuery

The `AddressQuery` type specifies filters for querying addresses.

```go
// AddressQuery contains filters for querying stored addresses.
type AddressQuery struct {
    // CreatedAfter filters addresses created after this Unix timestamp.
    // Zero means no lower bound.
    CreatedAfter int64

    // CreatedBefore filters addresses created before this Unix timestamp.
    // Zero means no upper bound.
    CreatedBefore int64

    // Limit is the maximum number of addresses to return.
    // Zero means use server default.
    Limit int32

    // Offset is the number of addresses to skip for pagination.
    Offset int32
}
```

### AddressEvent

The `AddressEvent` type represents an incoming transfer event for an address.

```go
// AddressEvent represents an incoming asset transfer event for an address.
type AddressEvent struct {
    // CreationTime is the Unix timestamp when the event was created.
    CreationTime uint64

    // Address is the address that received the transfer.
    Address *Address

    // Status is the current status of the incoming transfer.
    Status AddressEventStatus

    // Outpoint is the Bitcoin outpoint containing the inbound transfer.
    // Format: "txid:index"
    Outpoint string

    // UTXOAmountSat is the amount in satoshis transferred on-chain.
    // This is independent of the asset amount.
    UTXOAmountSat uint64

    // TaprootSibling is the taproot sibling hash used for the output.
    TaprootSibling []byte

    // ConfirmationHeight is the block height where the output was confirmed.
    // Zero means the output is unconfirmed.
    ConfirmationHeight uint32

    // HasProof indicates whether a proof file exists for this transfer.
    HasProof bool
}
```

### AddressEventStatus

```go
// AddressEventStatus represents the status of an incoming address transfer.
type AddressEventStatus uint8

const (
    // AddressEventStatusUnknown indicates an unknown status.
    AddressEventStatusUnknown AddressEventStatus = 0

    // AddressEventStatusTransactionDetected means the on-chain transaction
    // was detected in the mempool or a block.
    AddressEventStatusTransactionDetected AddressEventStatus = 1

    // AddressEventStatusTransactionConfirmed means the transaction was
    // confirmed in a block.
    AddressEventStatusTransactionConfirmed AddressEventStatus = 2

    // AddressEventStatusProofReceived means the asset proof was received
    // from the sender via the proof courier.
    AddressEventStatusProofReceived AddressEventStatus = 3

    // AddressEventStatusCompleted means the transfer is complete and the
    // asset is available in the wallet.
    AddressEventStatusCompleted AddressEventStatus = 4
)
```

### AddressReceivesQuery

```go
// SortDirection specifies the sort order for queries.
// Values match taprpc.SortDirection enum.
type SortDirection uint8

const (
    // SortDescending sorts results in descending order.
    SortDescending SortDirection = 0

    // SortAscending sorts results in ascending order.
    SortAscending SortDirection = 1
)

// AddressReceivesQuery contains filters for querying address receive events.
type AddressReceivesQuery struct {
    // FilterAddr filters events for a specific address string.
    // Empty means return events for all addresses.
    FilterAddr string

    // FilterStatus filters events by status.
    // Zero (Unknown) means return all statuses.
    FilterStatus AddressEventStatus

    // StartTimestamp filters events created at or after this Unix timestamp.
    // Zero means no lower bound.
    StartTimestamp uint64

    // EndTimestamp filters events created at or before this Unix timestamp.
    // Zero means no upper bound.
    EndTimestamp uint64

    // Offset is the number of events to skip for pagination.
    Offset int32

    // Limit is the maximum number of events to return.
    // Zero means use server default.
    Limit int32

    // Direction specifies sort order by creation time.
    Direction SortDirection
}
```

## Interface Changes

### WalletClient Additions

The following methods will be added to the `WalletClient` interface in
`clients.go`:

```go
// WalletClient exposes the TaprootAssets service gRPC client.
type WalletClient interface {
    // ... existing methods ...

    // NewAddr creates a new Taproot Asset address for receiving assets.
    // The address is stored in tapd and can be queried later.
    //
    // For V0/V1 addresses, AssetID and Amount are required.
    // For V2 addresses, either AssetID or GroupKey must be set.
    //
    // If ScriptKey and InternalKey are not provided, tapd derives them
    // from its internal wallet.
    NewAddr(ctx context.Context,
        req *NewAddressRequest) (*Address, error)

    // DecodeAddr decodes a bech32m Taproot Asset address string into its
    // components. This does not store the address or require it to be
    // previously known.
    DecodeAddr(ctx context.Context, addr string) (*Address, error)

    // QueryAddrs returns addresses that were previously created by this
    // tapd instance, filtered by the query parameters.
    QueryAddrs(ctx context.Context,
        query *AddressQuery) ([]*Address, error)

    // AddrReceives returns incoming transfer events for addresses created
    // by this tapd instance. Use this to track the status of expected
    // incoming transfers.
    AddrReceives(ctx context.Context,
        query *AddressReceivesQuery) ([]*AddressEvent, error)
}
```

## Implementation Details

### File Structure

| File | Purpose |
|------|---------|
| `entities/address.go` | New file with Address, AddressVersion, AddressEventStatus, and related types |
| `grpc/wallet_client.go` | Add NewAddr, DecodeAddr, QueryAddrs, AddrReceives implementations |
| `clients.go` | Extend WalletClient interface with new methods |

### gRPC Mapping

| SDK Method | gRPC RPC | Service |
|------------|----------|---------|
| `NewAddr` | `NewAddr` | TaprootAssets |
| `DecodeAddr` | `DecodeAddr` | TaprootAssets |
| `QueryAddrs` | `QueryAddrs` | TaprootAssets |
| `AddrReceives` | `AddrReceives` | TaprootAssets |

### Unmarshaling

A new `unmarshalAddr` function will convert `*taprpc.Addr` to `*entities.Address`:

```go
func unmarshalAddr(rpcAddr *taprpc.Addr) (*entities.Address, error) {
    if rpcAddr == nil {
        return nil, fmt.Errorf("nil address")
    }

    addr := &entities.Address{
        Encoded:          rpcAddr.Encoded,
        AssetType:        entities.AssetType(rpcAddr.AssetType),
        Amount:           rpcAddr.Amount,
        TapscriptSibling: rpcAddr.TapscriptSibling,
        ProofCourierAddr: rpcAddr.ProofCourierAddr,
        AssetVersion:     entities.AssetVersion(rpcAddr.AssetVersion),
        AddressVersion:   entities.AddressVersion(rpcAddr.AddressVersion),
    }

    // Parse asset ID (may be empty for V2 group addresses)
    if len(rpcAddr.AssetId) == 32 {
        copy(addr.AssetID[:], rpcAddr.AssetId)
    }

    // Parse group key if present
    if len(rpcAddr.GroupKey) == 33 {
        var gk entities.PubKey
        copy(gk[:], rpcAddr.GroupKey)
        addr.GroupKey = &gk
    }

    // Parse script key (required)
    if len(rpcAddr.ScriptKey) != 33 {
        return nil, fmt.Errorf("invalid script key length: %d",
            len(rpcAddr.ScriptKey))
    }
    copy(addr.ScriptKey[:], rpcAddr.ScriptKey)

    // Parse internal key (required)
    if len(rpcAddr.InternalKey) != 33 {
        return nil, fmt.Errorf("invalid internal key length: %d",
            len(rpcAddr.InternalKey))
    }
    copy(addr.InternalKey[:], rpcAddr.InternalKey)

    // Parse taproot output key (required)
    if len(rpcAddr.TaprootOutputKey) != 32 {
        return nil, fmt.Errorf("invalid taproot output key length: %d",
            len(rpcAddr.TaprootOutputKey))
    }
    copy(addr.TaprootOutputKey[:], rpcAddr.TaprootOutputKey)

    return addr, nil
}
```

### Request Marshaling

The `NewAddrRequest` will be marshaled as follows:

```go
func marshalNewAddrRequest(req *NewAddressRequest) *taprpc.NewAddrRequest {
    rpcReq := &taprpc.NewAddrRequest{
        Amt:                       req.Amount,
        TapscriptSibling:          req.TapscriptSibling,
        ProofCourierAddr:          req.ProofCourierAddr,
        SkipProofCourierConnCheck: req.SkipProofCourierConnCheck,
    }

    if req.AssetID != nil {
        rpcReq.AssetId = req.AssetID[:]
    }

    if req.GroupKey != nil {
        rpcReq.GroupKey = req.GroupKey[:]
    }

    if req.ScriptKey != nil {
        rpcReq.ScriptKey = &taprpc.ScriptKey{
            PubKey:   req.ScriptKey.PubKey[:],
            TapTweak: req.ScriptKey.TapTweak,
        }
        if req.ScriptKey.KeyDesc.RawKeyBytes != (entities.PubKey{}) {
            rpcReq.ScriptKey.KeyDesc = &taprpc.KeyDescriptor{
                RawKeyBytes: req.ScriptKey.KeyDesc.RawKeyBytes[:],
                KeyLoc: &taprpc.KeyLocator{
                    KeyFamily: int32(req.ScriptKey.KeyDesc.KeyLocator.Family),
                    KeyIndex:  int32(req.ScriptKey.KeyDesc.KeyLocator.Index),
                },
            }
        }
    }

    if req.InternalKey != nil {
        rpcReq.InternalKey = &taprpc.KeyDescriptor{
            RawKeyBytes: req.InternalKey.RawKeyBytes[:],
            KeyLoc: &taprpc.KeyLocator{
                KeyFamily: int32(req.InternalKey.KeyLocator.Family),
                KeyIndex:  int32(req.InternalKey.KeyLocator.Index),
            },
        }
    }

    if req.AssetVersion != nil {
        rpcReq.AssetVersion = taprpc.AssetVersion(*req.AssetVersion)
    }

    if req.AddressVersion != nil {
        rpcReq.AddressVersion = taprpc.AddrVersion(*req.AddressVersion)
    }

    return rpcReq
}
```

## Wallet Convenience Methods

The high-level `Wallet` type will provide a simplified method for the common
receive pattern:

```go
// NewReceiveAddress creates a V2 address for receiving any asset from a
// group. This is the recommended way to receive assets, as it allows the
// sender to choose which specific asset and amount to send from the group.
//
// For more control (specific asset ID, custom keys, V0/V1 addresses), use
// the lower-level NewAddr method on the client directly.
func (w *Wallet) NewReceiveAddress(ctx context.Context,
    groupKey PubKey) (*Address, error) {

    v2 := AddressVersionV2
    return w.client.NewAddr(ctx, &NewAddressRequest{
        GroupKey:       &groupKey,
        AddressVersion: &v2,
    })
}
```

Note: The invoice-like pattern of specifying a specific asset ID and amount
(common for collectibles/NFTs where amount=1) is intentionally not exposed
as a convenience method. Users requiring this pattern should use the
lower-level `NewAddr` method directly with a `NewAddressRequest`.

## Error Handling

Address operations can fail for several reasons. The SDK will expose these
through the existing `Error` type with appropriate classification:

| Error Condition | gRPC Code | SDK Detection |
|-----------------|-----------|---------------|
| Invalid asset ID | InvalidArgument | `err.IsInvalidArgument()` |
| Asset not found | NotFound | `err.IsNotFound()` |
| tapd unavailable | Unavailable | `err.IsUnavailable()` |
| Invalid address format | InvalidArgument | `err.IsInvalidArgument()` |
| Proof courier unreachable | Unavailable | `err.IsUnavailable()` |

Request validation (missing asset ID, conflicting identifiers, partial key
config, etc.) is handled server-side by tapd. The SDK intentionally does not
duplicate this validation to maintain a single source of truth and avoid
divergence as tapd's rules evolve. Invalid requests will surface as gRPC
`InvalidArgument` errors detectable via `err.IsInvalidArgument()`.

## Example Usage

### Basic Address Generation (V2 Group Address)

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
    // Connect to tapd
    client, err := grpc.NewClient(&grpc.Config{
        Host:    "localhost:10029",
        Network: entities.Regtest,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Create wallet
    wallet := tapsdk.NewWallet(client, entities.Regtest)

    ctx := context.Background()

    // Parse group key (from asset issuer or discovery)
    groupKey, err := entities.ParsePubKeyHex(
        "02abc123...def456",
    )
    if err != nil {
        log.Fatal(err)
    }

    // Generate V2 address to receive any asset from the group
    addr, err := wallet.NewReceiveAddress(ctx, groupKey)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Send assets to: %s\n", addr.Encoded)
}
```

### Specific Asset Address (Invoice-like)

For receiving a specific asset with a known amount (e.g., collectibles),
use the lower-level `NewAddr` method:

```go
func createAssetInvoice(ctx context.Context, client tapsdk.Client,
    assetID entities.AssetID, amount uint64) (*entities.Address, error) {

    return client.NewAddr(ctx, &entities.NewAddressRequest{
        AssetID: &assetID,
        Amount:  amount,
    })
}
```

### Tracking Incoming Transfers

```go
func waitForTransfer(ctx context.Context, client tapsdk.Client,
    addr string) error {

    for {
        events, err := client.AddrReceives(ctx, &entities.AddressReceivesQuery{
            FilterAddr: addr,
        })
        if err != nil {
            return err
        }

        for _, event := range events {
            switch event.Status {
            case entities.AddressEventStatusCompleted:
                fmt.Printf("Transfer complete at %s\n", event.Outpoint)
                return nil

            case entities.AddressEventStatusProofReceived:
                fmt.Println("Proof received, waiting for completion...")

            case entities.AddressEventStatusTransactionConfirmed:
                fmt.Printf("Transaction confirmed at height %d\n",
                    event.ConfirmationHeight)

            case entities.AddressEventStatusTransactionDetected:
                fmt.Println("Transaction detected in mempool...")
            }
        }

        // Poll interval
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(5 * time.Second):
        }
    }
}
```

### Advanced Address with Custom Keys

```go
func createCustomAddress(ctx context.Context, client tapsdk.Client,
    groupKey entities.PubKey,
    scriptKey *entities.ScriptKey,
    internalKey *entities.KeyDescriptor) (*entities.Address, error) {

    v2 := entities.AddressVersionV2
    return client.NewAddr(ctx, &entities.NewAddressRequest{
        GroupKey:       &groupKey,
        AddressVersion: &v2,
        ScriptKey:      scriptKey,
        InternalKey:    internalKey,
    })
}
```

### Decoding External Address

```go
func inspectAddress(ctx context.Context, client tapsdk.Client,
    encoded string) {

    addr, err := client.DecodeAddr(ctx, encoded)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Version: V%d\n", addr.AddressVersion)

    if addr.GroupKey != nil {
        fmt.Printf("Group Key: %x\n", addr.GroupKey[:])
    }

    if addr.AssetID != (entities.AssetID{}) {
        fmt.Printf("Asset ID: %s\n", addr.AssetID)
    }

    if addr.Amount > 0 {
        fmt.Printf("Amount: %d\n", addr.Amount)
    }
}
```

## Testing Strategy

### Unit Tests

1. **Request marshaling tests**: Verify SDK types correctly convert to taprpc,
   including ScriptKey/InternalKey with KeyDescriptor fields.
2. **Response unmarshaling tests**: Verify taprpc responses correctly convert
   to SDK types, including edge cases (empty fields, V2 addresses, etc.).
3. **Constant alignment tests**: Verify SDK enum constants match taprpc values
   for AddressVersion, AddressEventStatus, and SortDirection.

### Integration Tests

The address operations should be testable in taproot-assets integration tests
using only SDK types:

```go
func TestSDKAddressFlow(t *testing.T) {
    // Create SDK client
    client := setupSDKClient(t)
    wallet := tapsdk.NewWallet(client, entities.Regtest)

    // Mint a grouped asset (using existing SDK or test helper)
    groupKey := mintTestGroupedAsset(t)

    // Generate V2 receive address for the group
    addr, err := wallet.NewReceiveAddress(ctx, groupKey)
    require.NoError(t, err)
    require.NotEmpty(t, addr.Encoded)
    require.Equal(t, entities.AddressVersionV2, addr.AddressVersion)

    // Decode the address
    decoded, err := client.DecodeAddr(ctx, addr.Encoded)
    require.NoError(t, err)
    require.Equal(t, groupKey, *decoded.GroupKey)

    // Query stored addresses
    addrs, err := client.QueryAddrs(ctx, nil)
    require.NoError(t, err)
    require.NotEmpty(t, addrs)

    // Send to address and verify receive events
    sendToAddress(t, addr.Encoded, 100)

    // Poll for completion
    require.Eventually(t, func() bool {
        events, err := client.AddrReceives(ctx, &entities.AddressReceivesQuery{
            FilterAddr: addr.Encoded,
        })
        require.NoError(t, err)
        for _, e := range events {
            if e.Status == entities.AddressEventStatusCompleted {
                return true
            }
        }
        return false
    }, 30*time.Second, time.Second)
}
```

## Migration Notes

This is an additive change with no breaking modifications to existing APIs.
Existing SDK users can adopt address operations incrementally.

## References

- [taprpc.proto](https://github.com/lightninglabs/taproot-assets/blob/main/taprpc/taprootassets.proto) - gRPC service definitions
- [tap-sdk clients.go](../clients.go) - Existing client interfaces
- [tap-sdk entities](../entities/) - Existing SDK types
