# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**tap-sdk** is the official Go SDK for building applications on the Taproot Assets protocol. It wraps the `tapd` (Taproot Assets daemon) gRPC interface, enabling developers to create, sign, and publish asset transfers without dealing with low-level protobuf types.

**Status:** Pre-v1.0 (breaking API changes possible). See DEVELOPMENT_CYCLE.md.

## Build Commands

| Command | Purpose |
|---------|---------|
| `make build` | Build the SDK |
| `make lint` | Run golangci-lint (via Docker) |
| `make fmt` | Format code with gosimports and gofmt |
| `make unit` | Run all unit tests |
| `make unit pkg=<package>` | Run tests for specific package |
| `make unit pkg=<package> case=<test>` | Run specific test case |

## Architecture

### Package Structure

```
tap-sdk/
├── Root (wallet.go, tx_builder.go, interactive_tx_builder.go, clients.go, errors.go)
│   High-level SDK facade and transaction builders
├── entities/      Domain types (Asset, Keys, Transfer, Proof, Network)
├── grpc/          gRPC client implementations (hidden from SDK consumers)
├── vpsbt/         Virtual PSBT encoding for interactive transfers
├── codec/         Cryptographic utilities (alt-leaves, STXO derivation)
└── macaroon/      Authentication helpers
```

### Client Interface Hierarchy

The SDK exposes a single `Client` interface that embeds four specialized clients:

```go
Client
├── WalletClient      // GetInfo, ListAssets, ListTransfers
├── WalletKitClient   // DeriveScriptKey, DeriveInternalKey, Fund, Sign, Commit, Publish
├── ProofClient       // ExportProof, DecodeProof, RegisterTransfer, UnpackProofFile
└── UniverseClient    // InsertProof
```

### Entity Design

SDK entities are thin wrappers over byte arrays with helper methods (String(), parsing).
All gRPC types are converted to/from these entities at the `grpc/` package boundary:

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
- Every exported function MUST have a comment starting with the function name
- Comments explain "why", not "what"
- Use `require` library for test assertions; prefer table-driven tests
- Segment code into logical stanzas separated by blank lines

**Git commits:** Format as `subsystem: description` (e.g., `entities: add new key type`)

**Function wrapping:**
```go
// If exceeds 80 chars, put closing paren on own line
value, err := bar(
    a, b, c,
)

// Multi-line function definitions need blank line before body
func foo(a, b, c,
    d, e) error {

    var a int
}
```

## External Dependencies

- **tapd daemon** - Taproot Assets daemon (gRPC over TLS with macaroon auth)
- **taproot-assets/taprpc** - gRPC service definitions
- **btcsuite/btcd** - Bitcoin primitives (PSBT, keys, crypto)
