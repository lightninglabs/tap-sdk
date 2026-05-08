# Compatibility

`tap-sdk` is tied to the `tapd` API surface exposed by Taproot Assets. The SDK
does not attempt to support older daemon versions when the daemon cannot return
the data needed for correct business-level `AssetRef` mapping.

## Version Matrix

| tap-sdk line | tapd / Taproot Assets | lnd | Go | Status |
|--------------|------------------------|-----|----|--------|
| `main` | tapd `main` or latest v0.8 release candidate | v0.20.x | 1.25.7+ | Development |
| first public release line | v0.8.0 or newer | v0.20.x | 1.25.7+ | Target |
| unsupported | v0.7.x and older | any | any | Unsupported |

## Why v0.8 Is Required

The SDK maps tapd rows into semantic business types:

- grouped fungible assets
- standalone NFTs
- NFT collections
- concrete NFT collection items
- issuances/tranches
- transfer and burn histories

That mapping depends on v0.8 fields such as per-row asset type data and
group-key-aware burn and transfer records. Without those fields, the SDK cannot
reliably tell whether a grouped row represents a fungible asset or an NFT
collection item. Returning a best guess would make the public API unsafe.

## Release Candidate Policy

During v0.8 release-candidate development, local integration tests should run
against tapd `main`:

```bash
make itest-main
```

The pinned integration-test image may lag until a new public RC image is
available. When in doubt, treat tapd `main` as the compatibility target for
SDK `main`.

## Feature Scope

The current SDK focuses on non-Lightning Taproot Assets workflows:

- wallet operations
- minting and issuance
- NFT collections
- proofs
- universe discovery and sync
- burns
- ownership proofs
- gRPC and REST transport parity

Lightning-specific Taproot Assets workflows are not part of this release line:

- RFQ
- price oracles
- Taproot Assets channels
- Portfolio Pilot
