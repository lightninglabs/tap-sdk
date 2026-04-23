# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working
with code in this repository.

## Project Overview

**tap-sdk** is the official Go SDK for building applications on the Taproot
Assets protocol. It wraps the `tapd` (Taproot Assets daemon) gRPC interface,
enabling developers to create, sign, and publish asset transfers without
dealing with low-level protobuf types.

**Status:** Pre-v1.0 (breaking API changes possible).
See DEVELOPMENT_CYCLE.md.

## Build Commands

| Command | Purpose |
|---------|---------|
| `make build` | Build the SDK |
| `make lint` | Run golangci-lint (via Docker) |
| `make fmt` | Format code with gosimports and gofmt |
| `make unit` | Run all unit tests |
| `make unit pkg=<package>` | Run tests for specific package |
| `make unit pkg=<package> case=<test>` | Run specific test case |
| `make itest-main` | Run integration tests against tapd built from `taproot-assets` main (**use this locally**) |
| `make itest` | Run integration tests against the pinned tapd image (what CI uses) |
| `make itest-run-main case=<test>` | Run a single itest against tapd-main without re-spinning the stack |

## Integration tests

When developing locally, **always use `itest-main`** (or `itest-run-main` if
the stack is already up). It builds tapd from the latest `taproot-assets`
main so new RPCs and proto changes are exercised before CI catches them.

CI pins to the latest released tapd image via plain `itest` because
bleeding-edge tapd does not always have a pre-built image yet. Use plain
`itest` only when you need to reproduce what CI sees.

## Architecture

### Package Structure

```
tap-sdk/
├── Root (wallet.go, tx_builder.go, interactive_tx_builder.go,
│        clients.go, errors.go)
│   High-level SDK facade and transaction builders
├── entities/      Domain types (Asset, Keys, Transfer, Proof, Network)
├── grpc/          gRPC client implementations (hidden from consumers)
├── vpsbt/         Virtual PSBT encoding for interactive transfers
├── codec/         Cryptographic utilities (alt-leaves, STXO derivation)
├── macaroon/      Authentication helpers
└── docs/
    └── design/    Architecture decision records
```

### Client Interface Hierarchy

The SDK exposes a single `Client` interface that embeds four specialized
clients:

```go
Client
├── WalletClient      // GetInfo, ListAssets, ListBalances, ListTransfers,
│                     // NewAddr, DecodeAddr, QueryAddrs, AddrReceives, SendAsset
├── WalletKitClient   // DeriveScriptKey, DeriveInternalKey, Fund,
│                     // Sign, Commit, Publish, Anchor
├── ProofClient       // ExportProof, DecodeProof, RegisterTransfer,
│                     // UnpackProofFile
└── UniverseClient    // InsertProof
```

### Entity Design

SDK entities are thin wrappers over byte arrays with helper methods
(String(), parsing). All gRPC types are converted to/from these entities
at the `grpc/` package boundary.

The public entity model is intentionally opinionated:
- for **fungible assets**, the normal SDK surface should identify the asset
  by **group key**, not tranche-level `asset_id`
- for **collectibles / non-fungible assets**, the correct public identifier
  is the **asset ID**
- low-level wrappers may still need to translate raw RPC fields, but the
  high-level API and docs should consistently preserve that distinction

Typical identifier types remain:

```go
type AssetID [32]byte      // Hex string representation
type PubKey [33]byte       // Compressed secp256k1
type XOnlyPubKey [32]byte  // Schnorr/Taproot x-only
type Outpoint struct { Txid [32]byte; Index uint32 }
```

### Error Handling

Custom `Error` type wraps RPC errors with operation context:
```go
type Error struct { Op string; Err error }
func (e *Error) IsNotFound() bool      // gRPC code checks
func (e *Error) IsUnavailable() bool
func (e *Error) IsInvalidArgument() bool
```

## Code Style

**Critical requirements:**
- Line length MUST NOT exceed 80 characters (tabs count as 8 spaces)
- Every exported function MUST have a comment starting with the function
  name
- Comments explain "why", not "what"
- Use `require` library for test assertions; prefer table-driven tests
- Segment code into logical stanzas separated by blank lines
- Follow Lightning Labs conventions (see `.gemini/styleguide.md`)

**Git commits:** Format as `subsystem: description`
(e.g., `entities: add new key type`). Keep messages concise — subject
under ~70 characters; a short body (one or two paragraphs at most)
when it helps, explaining *why*, not the diff.

**Function wrapping:**
```go
// If exceeds 80 chars, put closing paren on own line.
value, err := bar(
    a, b, c,
)

// Multi-line function definitions need blank line before body.
func foo(a, b, c,
    d, e) error {

    var a int
}
```

**Error/log messages:** Keep compact, don't break across fmt.Errorf calls
unnecessarily:
```go
return fmt.Errorf("failed to fund transfer with %d "+
    "recipients", len(recipients))
```

## Adding New RPC Wrappers

When wrapping a new `tapd` RPC, follow this checklist:

1. Define SDK types in `entities/` (request, response, domain objects)
2. Implement the gRPC call in the appropriate `grpc/` sub-client
3. Add marshal/unmarshal functions at the `grpc/` boundary
4. Add the method to the appropriate interface in `clients.go`
5. Optionally add a convenience method on `Wallet`
6. Write unit tests with mocks + table-driven patterns
7. Write integration tests if applicable
8. Update CHANGELOG.md

## External Dependencies

- **tapd daemon** — Taproot Assets daemon (gRPC over TLS with macaroon
  auth)
- **taproot-assets/taprpc** — gRPC service definitions (lightweight proto
  module, not the full taproot-assets repo)
- **btcsuite/btcd** — Bitcoin primitives (PSBT, keys, crypto)

### Dependency Boundary

`taprpc` imports are strictly confined to `grpc/`. No exported type,
function, or method outside `grpc/` may reference `taprpc` types.
The `grpc/` sub-client structs are unexported, and their internal helpers
(macaroon auth, raw client access) must also remain unexported so that
`taprpc` types do not leak through `grpc.Client` struct embedding.

Test files outside `grpc/` must only use `entities/` types. If a mock
needs to implement a client interface, it should use SDK types, not proto
types.

## Design Documents

Create a design document under `docs/design/` before implementation only for
non-trivial design work, architecture decisions, or public API redesigns.
Straightforward low-level RPC wrappers do not need a dedicated design doc if
no new user-facing design is being introduced.

## Testing

- Unit tests use mocks for gRPC clients (see `tx_builder_test.go`)
- Table-driven tests are preferred
- Integration tests run against a real `tapd` in regtest
- All CI checks must pass before merge
