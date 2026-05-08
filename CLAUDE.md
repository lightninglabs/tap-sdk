# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working
with code in this repository.

## Project Overview

**tap-sdk** is the Go source-of-truth implementation of the Taproot Assets
application SDK. It exposes typed wallet, issuer, universe, proof, burn, and
transfer surfaces over `tapd` without requiring application developers to deal
with low-level protobuf types.

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
├── Root (wallet.go, tx_builder.go, asset.go, clients.go, errors.go)
│   High-level SDK facade, transaction builders, and public domain types
├── grpc/          gRPC client implementation
├── rest/          REST client implementation
├── internal/vpsbt/ Virtual PSBT encoding for interactive transfers
├── internal/codec/ Cryptographic utilities (alt-leaves, STXO derivation)
├── macaroon/      Authentication helpers
└── docs/
    └── design/    Durable design decisions
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

SDK business types live in the root package. Identifier types are thin
wrappers over byte arrays with helper methods (String(), parsing). Transport
packages convert protocol data to/from these SDK types at their package
boundaries.

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
(e.g., `wallet: add new key type`). Keep messages concise — subject
under ~70 characters; if a body is needed, 2–4 short sentences, not
paragraphs. **Commits are not PR descriptions** — design context,
test plans, rationale walk-throughs go in the PR body, not in every
commit. Explain the *why* only when it isn't obvious from the diff;
otherwise skip the body entirely.

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

1. Define SDK types in the root package (request, response, domain objects)
2. Implement the gRPC call in the appropriate `grpc/` sub-client
3. Add marshal/unmarshal functions at the transport boundary
4. Add the method to the appropriate interface in `clients.go`
5. Optionally add a convenience method on `Wallet`
6. Write unit tests with mocks + table-driven patterns
7. Write integration tests if applicable
8. Update README or docs pages when the public surface changes
9. Update CHANGELOG.md only when preparing a public release

## External Dependencies

- **tapd daemon** — Taproot Assets daemon (gRPC over TLS with macaroon
  auth)
- **taproot-assets/taprpc** — gRPC service definitions (lightweight proto
  module, not the full taproot-assets repo)
- **btcsuite/btcd** — Bitcoin primitives (PSBT, keys, crypto)

### Dependency Boundary

Wire-format imports are strictly confined to transport packages. No exported
type, function, or method outside transport packages may reference `taprpc`
types. The transport sub-client structs are unexported, and their internal
helpers must also remain unexported so wire types do not leak through
embedding.

Test files outside transport packages must only use root SDK types. If a mock
needs to implement a client interface, it should use SDK types, not proto
types.

## Design Documents

Create a design document under `docs/design/` only for durable API,
architecture, or package-boundary decisions. Do not use `docs/design/` for
short-lived implementation plans. Straightforward low-level RPC wrappers do
not need a dedicated design doc if no new user-facing design is introduced.

## Testing

- Unit tests use mocks for gRPC clients (see `tx_builder_test.go`)
- Table-driven tests are preferred
- Integration tests run against a real `tapd` in regtest
- All CI checks must pass before merge
