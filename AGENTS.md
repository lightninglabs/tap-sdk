# AGENTS.md — tap-sdk

## Project Overview

`tap-sdk` is the official Go SDK for building applications on the Taproot
Assets protocol. It wraps the `tapd` (Taproot Assets daemon) gRPC interface,
enabling developers to create, manage, and transfer Taproot Assets without
dealing with low-level protobuf types.

**Repository:** `github.com/lightninglabs/tap-sdk`
**Language:** Go (source of truth)
**Status:** Pre-v1.0 (breaking API changes possible)

## Architecture

```
tap-sdk/
├── Root package (tapsdk)
│   ├── wallet.go              — High-level Wallet entrypoint
│   ├── tx_builder.go          — Address-based transfer builder
│   ├── interactive_tx_builder.go — Interactive (keyless) transfer builder
│   ├── clients.go             — Client interface definitions
│   └── errors.go              — SDK error types and sentinels
├── entities/                  — Domain types (pure Go, no proto deps)
├── grpc/                      — gRPC client implementations (internal)
├── vpsbt/                     — Virtual PSBT encoding
├── codec/                     — Cryptographic utilities (alt-leaves, STXO)
├── macaroon/                  — Authentication helpers
└── docs/                      — Design documents
    └── design/                — Architecture decision records
```

### Key Design Principles

1. **SDK types are the only public surface.** Users never import `taprpc`
   or other `taproot-assets` packages.
2. **gRPC is an implementation detail.** All proto conversions happen at the
   `grpc/` package boundary.
3. **Wallet is the entrypoint.** High-level operations go through `Wallet`,
   low-level through the `Client` interface.
4. **Entities are thin wrappers.** Fixed-size byte arrays with helper methods,
   not heavy objects.
5. **The SDK is opinionated.** The public surface should not blindly mirror
   raw `taprpc` semantics when a cleaner Taproot Assets model exists.

### Asset Identity Model

The SDK must distinguish between the protocol's low-level identifiers and the
user-facing identity model:

- **Fungible assets are identified by group key** in the normal SDK surface.
  The `asset_id` is tranche-level and should not be the primary identifier
  that application developers work with for fungible assets.
- **Collectibles / non-fungible assets are identified by asset ID.** In that
  case the tranche is the asset, so exposing the asset ID is correct.
- Low-level client wrappers may still need to translate raw RPC fields, but
  high-level APIs and docs should consistently present the semantic model:
  fungible => group key, non-fungible => asset ID.

### Address Defaults

The SDK should expose an opinionated default address flow:

- **V2 addresses are the default** for the normal, high-level receive/send
  surface unless there is a concrete reason to do otherwise.
- Older address versions are considered advanced or compatibility paths and
  should remain available through lower-level client methods instead of being
  promoted in the primary UX.

## Development

### Build Commands

| Command | Purpose |
|---------|---------|
| `make build` | Build the SDK |
| `make lint` | Run golangci-lint (via Docker) |
| `make fmt` | Format code (gosimports + gofmt) |
| `make unit` | Run all unit tests |
| `make unit pkg=<pkg>` | Run tests for specific package |
| `make unit pkg=<pkg> case=<test>` | Run a specific test case |

### Code Style

Follow Lightning Labs conventions (see `.gemini/styleguide.md`):

- **80 character line limit** (tabs count as 8 spaces)
- **Every exported function** must have a comment starting with function name
- **Comments explain "why"**, not "what"
- **Table-driven tests** with `require` assertions
- **Logical stanzas** separated by blank lines
- **No AI watermarks** — no `Co-authored-by: Claude` or similar

### Commit Style

Format: `subsystem: short description`

Examples:
- `entities: add FetchAsset request and response types`
- `grpc: wrap ListBalances RPC`
- `wallet: add high-level balance query method`
- `multi: fix lint issues across packages`

### PR Workflow

1. Create or update a design doc under `docs/design/` only for non-trivial
   design work, architecture decisions, or API redesigns
2. Skip dedicated design docs for straightforward low-level RPC wrapper work
   that does not introduce new user-facing design
3. Implement with granular, logical commits (not incremental)
4. Force-push to keep history clean
5. All CI must pass (lint, format, compile, unit tests)
6. Wait for review before merge

### Adding New RPC Wrappers

When wrapping a new `tapd` RPC:

1. Define SDK types in `entities/` (request, response, domain objects)
2. Implement the gRPC call in the appropriate `grpc/` client
3. Add marshal/unmarshal functions at the `grpc/` boundary
4. Add the method to the appropriate interface in `clients.go`
5. Optionally add a convenience method on `Wallet`
6. Write unit tests with mocks + table-driven patterns
7. Update CHANGELOG.md

## Dependencies

- **tapd** — Taproot Assets daemon (gRPC over TLS + macaroons)
- **taproot-assets/taprpc** — gRPC service definitions (proto)
- **btcsuite/btcd** — Bitcoin primitives (PSBT, keys, crypto)

## Success Metrics

The SDK is successful when:

1. Any project (lightning-terminal, loop, taproot-assets itests) can use
   `tap-sdk` instead of raw `taprpc` calls.
2. No consumer needs to import `taproot-assets` packages directly.
3. The SDK surface is opinionated with good defaults, not a 1:1 proto mirror.
4. Comprehensive test coverage exists for all wrapped RPCs.
