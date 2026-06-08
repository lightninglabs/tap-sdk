# Compatibility

`tap-sdk` is tied to the `tapd` API surface exposed by Taproot Assets. The SDK
does not attempt to support older daemon versions when the daemon cannot return
the data needed for correct business-level `AssetRef` mapping.

## Version Matrix

| tap-sdk line | tapd / Taproot Assets | lnd | Go | Status |
|--------------|------------------------|-----|----|--------|
| `main` | tapd `main` after v0.8.0 | v0.21.0-beta or newer | 1.25.10+ | Development |
| `v0.1.x` | v0.8.0 or newer | v0.21.0-beta or newer | 1.25.10+ | First public release line |
| unsupported | v0.7.x and older | any | any | Unsupported |

The lnd column tracks the SDK's validated integration-test target and the
released Taproot Assets v0.8.0 module graph. Taproot Assets v0.8.0 documents
runtime support for lnd v0.20.0-beta or newer, but this SDK release line is
validated against v0.21.0-beta.

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

## Development Policy

Release branches and release validation should run against the pinned tapd
image:

```bash
make itest
```

When SDK `main` intentionally depends on unreleased tapd behavior, local
integration tests can run against tapd `main`:

```bash
make itest-main
```

The pinned integration-test image remains the compatibility target for release
branches.

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
