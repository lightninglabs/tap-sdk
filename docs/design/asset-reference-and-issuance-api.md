# Asset Reference and Issuance API Redesign

## Status

Accepted and implemented for the Wallet and Issuer core surfaces.

This document defines the public direction for tap-sdk's asset identity and
issuance naming model.

## Problem

Taproot Assets exposes two protocol-level identifiers for what users think of
as "the asset":

- `asset_id`, the unique genesis identifier of a specific issuance/tranche
- `group_key`, the identifier that ties multiple issuances into one fungible
  asset family

That distinction is correct at the protocol layer, but it is the wrong default
abstraction for SDK consumers.

A user interacting with the SDK should not need to know:

- whether the asset they hold is addressed by `asset_id` or `group_key`
- whether the asset is backed by a single issuance or many issuances
- that Taproot Assets internally distinguishes a user-facing asset from a
  protocol issuance/tranche

The early API leaked that distinction in too many places:

- requests accept separate `assetId` vs `groupKey`
- responses surface one or both and force callers to branch
- balances inherit tapd's raw grouping modes instead of semantic asset balance
- universe IDs mirror protocol identifier variants directly
- tapd minting uses one low-level request shape for two different actions:
  - creating a brand-new asset
  - issuing more units of an existing asset

## Goals

1. **Single user-facing asset identifier**
   - every public request/response that refers to an asset should use one field
   - callers pass one string, store one string, compare one string

2. **Hide `asset_id` vs `group_key` from normal SDK use**
   - the SDK decides which protocol identifier to use underneath
   - callers should not branch on fungible vs collectible identity mechanics

3. **Rename protocol-level identifiers honestly**
   - when a field refers to a specific issuance/tranche, name it
     `IssuanceID` or `TrancheID`, not `AssetID`
   - reserve `AssetRef` / asset language for semantic assets as users see them

4. **Semantic asset balance and query surface**
   - `ListBalances`, address creation, proof registration, universe lookups,
     metadata lookup, and burn/filter APIs should all operate on `AssetRef`

5. **Clean minting nomenclature**
   - separate "create a new asset" from "create a new issuance"
- stop exposing `MintAsset` as the overloaded high-level issuer concept

## Non-Goals

- hiding all protocol details from every deep inspection object forever
- changing tapd or the underlying RPC definitions
- eliminating protocol terms like group keys internally
- optimizing every path for minimum RPC count in the first pass

This redesign prioritizes the correct public model first. Some paths will do
extra SDK-side resolution rather than mirroring tapd's raw shape.

## Core Decision: `AssetRef`

The public SDK asset identifier is `tapsdk.AssetRef`.

### Properties

- opaque string
- stable for storage and round-tripping
- canonical lowercase form
- internally encodes exactly one protocol identifier:
  - `asset_id` for unique or ungrouped assets
  - `group_key` for multi-issuance fungible assets

### Encoding

`AssetRef` uses a bech32m string with HRP `assetref`.

Payload format:

- 1 byte format version
- 1 byte discriminator
- raw identifier bytes

Discriminators:

- `0x00` = protocol `asset_id`
- `0x01` = protocol `group_key`

### Why opaque instead of `asset:<hex>` or `group:<hex>`?

Because the whole point is to remove that distinction from the caller's mental
model.

A prefixed plain-text format still teaches the user that they need to care
about the difference. An opaque string lets the SDK keep the distinction where
it belongs: at the bridge to tapd.

### Why bech32m?

- human-friendly
- checksummed
- easy to copy/paste and compare
- lowercase canonical form
- future-proof for versioned payloads
- already familiar in the Bitcoin ecosystem

## Public Naming Rules

### Asset-level fields

Use these when the caller is referring to the asset as a user-visible thing:

- `AssetRef`
- `Asset`
- `ListAssets`
- `FetchAssetMeta`
- `ListBalances`
- `CreateFungible`
- `CreateNFT`

### Issuance/tranche-level fields

Use these when the field refers to one concrete protocol issuance:

- `IssuanceID`
- `TrancheID` when a shorter-lived or batch-local identifier is more precise
- `IssueFungible`
- `MintCollectionItem`

### Terms to avoid in the public SDK surface

Avoid forcing protocol terms into the main API when a user-facing name exists:

- `Group` for semantic assets
- `GroupedAsset` as a primary concept
- `AssetID` when the field is actually an issuance identifier

## Public API Shape

### Addresses

Address creation should accept:

```go
NewAddressRequest {
    AssetRef AssetRef
    ...
}
```

Address responses should return:

```go
Address {
    AssetRef AssetRef
    ...
}
```

The caller should not pass separate `AssetID` or `GroupKey`.

### Balances

`ListBalances` should return semantic balances keyed by `AssetRef`, not raw
balance buckets keyed by tapd grouping modes.

Implementation choice:

- use asset listings and aggregate by `AssetRef`
- keep tapd's raw balance RPC only as an implementation detail when needed

Why:

- tapd's `asset_id` grouping is too issuance-centric
- tapd's `group_key` grouping loses the identity of ungrouped assets
- the SDK can do better by lifting both into one semantic balance map

### Metadata lookup

Metadata lookup should accept `AssetRef` and internally resolve to the correct
issuance ID when tapd still requires one.

### Burns

`Wallet.Burn` accepts `AssetRef`, supplies tapd's confirmation text internally,
and returns a compact burn result. The low-level `Client.BurnAsset` remains
available for callers that need the raw daemon request shape.

Burn history filters also accept `AssetRef`.

### Universe

The high-level universe facade uses `AssetRef` directly:

```go
universe := wallet.NewUniverse()

known, err := universe.HasAsset(ctx, ref)
roots, err := universe.GetRoots(ctx, ref)
proofs, err := universe.ListProofs(ctx, ref)
_, err = universe.SyncAsset(ctx, ref, "tapd.example:10029")
```

Universe sync hosts should be trusted configuration, not direct user input.
Current tapd versions dial remote universe servers without certificate
verification during sync.

The low-level universe client still uses:

```go
UniverseID {
    AssetRef AssetRef
    ProofType ProofType
}
```

This keeps normal application code on `AssetRef` while retaining direct
protocol access for advanced callers.

### Proofs

Proof decode and transfer registration responses should include `AssetRef`.
When the proof is about a specific issuance, the concrete identifier should be
named `IssuanceID`.

Ownership proof requests are also `AssetRef`-first at the Wallet layer.
`Wallet.ProveOwnership` resolves wallet-owned outputs to the concrete
issuance ID, script key, and outpoint needed by tapd's low-level ownership RPC.
Fungible proofs require an explicit amount and may return multiple concrete
proofs to satisfy it. Collection refs prove one owned NFT item by default, or
every owned item when requested explicitly.

## Minting Redesign

This is the most important naming cleanup after `AssetRef`.

### Current problem

The protocol mint request is overloaded.

It currently means both:

1. create a brand-new asset namespace, like `USDT`
2. issue more units of an existing asset namespace

Those are not the same action.

### Public actions

The public SDK exposes explicit operations for each business action:

#### `CreateFungible`

Creates a new grouped fungible asset and returns an `Asset` keyed by its group
`AssetRef`.

#### `IssueFungible`

Creates a new issuance/tranche for an existing fungible asset identified by
`AssetRef` and returns an `Issuance`.

#### `CreateNFT`

Creates a standalone NFT and returns an `Asset` keyed by the NFT's asset-ID
`AssetRef`.

#### `CreateCollection`

Creates an NFT collection by minting the first collection item. It returns a
`CollectionMintResult` containing the `Collection` keyed by group `AssetRef`
and the first NFT `Asset` keyed by item asset-ID `AssetRef`.

#### `MintCollectionItem`

Mints another NFT item into an existing collection identified by collection
`AssetRef`.

### Why this matters

The user thinks in terms of:

- "new asset"
- "more units of this asset"

They do **not** think in terms of:

- group anchors
- grouped vs new grouped assets
- whether a mint request really means a new asset or a new tranche

### Internal mapping to tapd

The SDK keeps tapd's `MintAsset` vocabulary on the low-level `MintClient`.
The overload belongs there as an advanced daemon-shaped escape hatch, not in
the high-level issuer API.

## Compatibility and Migration

This is a pre-v1 SDK. The redesign intentionally favors a correct long-term
public model over preserving a leaky early API.

Migration rules:

- replace request `AssetID` / `GroupKey` pairs with `AssetRef`
- rename response `AssetID` fields to `IssuanceID` when they identify one
  concrete issuance
- prefer `Balances[assetRef]` over raw grouped balance maps
- replace `UniverseID{AssetID|GroupKey}` with `UniverseID{AssetRef}`
- replace overloaded mint entrypoints with explicit fungible, NFT, collection,
  and issuance methods

## Implementation Strategy

### Phase 1

- introduce opaque `AssetRef`
- thread it through the major public request/response types
- add helpers for protocol bridging
- aggregate balances by `AssetRef`
- rename obvious issuance-level fields to `IssuanceID`

### Phase 2

- split the high-level issuer surface into `CreateFungible`, `IssueFungible`,
  `CreateNFT`, `CreateCollection`, and `MintCollectionItem`
- keep `MintAsset` and `MintIssuance` as the low-level batch mint API
- update examples and top-level README snippets

## Consequences

### Good

- much cleaner public API
- fewer caller branches
- semantic asset identity is consistent everywhere
- room for future protocol changes without breaking caller code
- issuance terminology becomes more honest and less confusing

### Trade-offs

- some SDK calls do extra resolution work instead of mirroring tapd directly
- some protocol inspection objects still expose deep details where tapd does
- grouped asset burns may still need concrete issuance resolution rules
- this is a breaking change for early adopters

## Takeaways

The SDK should model what users mean, not what tapd happens to require.

`AssetRef` becomes the single public asset identifier.
`IssuanceID` names concrete protocol-level tranches honestly.
The issuer API splits into explicit actions for creating fungible assets,
issuing more fungible supply, creating standalone NFTs, creating NFT
collections, and minting more collection items.

That is the right abstraction boundary for tap-sdk.
