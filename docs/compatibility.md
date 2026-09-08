# Compatibility

`tap-sdk` is tied to the `tapd` API surface exposed by Taproot Assets. The SDK
does not attempt to support older daemon versions when the daemon cannot return
the data needed for correct business-level `AssetRef` mapping.

## Version Matrix

| tap-sdk line | tapd / Taproot Assets | lnd | Go | Status |
|--------------|------------------------|-----|----|--------|
| `main` / planned `v0.2.x` | v0.8.3 | v0.21.3-beta | 1.26.0+ | Release candidate |
| `v0.1.x` | v0.8.0 or newer | v0.21.0-beta or newer | 1.25.10+ | Released |

The full gRPC and REST integration suite is validated against tapd v0.8.3
and lnd v0.21.3-beta, including custom-anchor commitments, MuSig2 spends,
timeout spends, and proof paths. Advanced custom-anchor operations require
transition proof v1 support; the v0.8.0 daemon does not expose that field.

Daemon compatibility and Go module compatibility are separate. Current SDK
source uses btcd v2 packages and the corresponding Taproot Assets and lnd
revisions. The v0.8.3 Taproot Assets Go module and taprpc v1.3.3 still depend
on the older btcd packages, so those tags cannot replace the SDK's pinned
source revisions without a coordinated module migration. Applications can
use the SDK's own types without importing taprpc or Taproot Assets directly.

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
