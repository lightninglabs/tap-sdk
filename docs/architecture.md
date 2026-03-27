# Architecture

This document describes the architecture of `tap-sdk`, the official Go SDK
for the Taproot Assets protocol.

## Overview

`tap-sdk` is a client-side SDK that communicates with a running `tapd`
(Taproot Assets daemon) instance over gRPC. It provides typed Go interfaces
that encapsulate all protobuf types and gRPC details, enabling developers to
build Taproot Assets applications without direct dependency on the
`taproot-assets` repository.

```
┌─────────────────────────────────────────────────────┐
│                   Application                       │
│                                                     │
│  Uses: tapsdk.Wallet, entities.*, TxBuilder, etc.   │
├─────────────────────────────────────────────────────┤
│                    tap-sdk                           │
│                                                     │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐ │
│  │  Wallet   │  │ TxBuilder │  │ InteractiveTxBldr│ │
│  └────┬─────┘  └─────┬─────┘  └────────┬─────────┘ │
│       │               │                 │           │
│  ┌────┴───────────────┴─────────────────┴────────┐  │
│  │              Client Interfaces                │  │
│  │  WalletClient | WalletKitClient | ProofClient │  │
│  │                UniverseClient                 │  │
│  └───────────────────┬───────────────────────────┘  │
│                      │                              │
│  ┌───────────────────┴───────────────────────────┐  │
│  │          grpc/ (internal boundary)            │  │
│  │  marshal/unmarshal ←→ taprpc types            │  │
│  └───────────────────┬───────────────────────────┘  │
├──────────────────────┼──────────────────────────────┤
│                      │ gRPC + TLS + Macaroons       │
│                      ▼                              │
│              tapd (daemon)                          │
└─────────────────────────────────────────────────────┘
```

## Package Structure

### Root Package (`tapsdk`)

The root package contains the high-level API surface:

- **`Wallet`** — The primary entrypoint. Wraps a `Client` and provides
  convenience methods for common operations (receive addresses, key
  derivation, proof import).

- **`TxBuilder`** — Builder pattern for address-based asset transfers.
  Guides users through the Fund → Sign → Commit → Finish pipeline.

- **`InteractiveTxBuilder`** — Builder for interactive transfers where
  the receiver provides keys directly instead of an address. Handles
  vPSBT construction, funding, signing, and anchoring.

- **`Client`** — The composite interface embedding all sub-clients.

- **`Error`** — SDK error type that wraps gRPC errors with operation
  context and convenience methods (`IsNotFound`, `IsUnavailable`, etc.).

### `entities/`

Pure Go domain types with no external dependencies beyond `btcsuite/btcd`.
These are the types that SDK consumers interact with.

Design rules:
- Fixed-size byte arrays for identifiers (`AssetID [32]byte`,
  `PubKey [33]byte`, `XOnlyPubKey [32]byte`)
- Helper methods for parsing and string conversion
- Request/response structs for API operations
- No proto imports, no gRPC dependencies
- High-level APIs must preserve the SDK's semantic asset model:
  fungible assets are identified by group key, while collectibles are
  identified by asset ID

### `grpc/`

The gRPC boundary layer. This is the only package that imports `taprpc`.

Responsibilities:
- Connect to `tapd` with TLS and macaroon authentication
- Implement all `Client` interface methods via gRPC calls
- Marshal SDK types → proto requests
- Unmarshal proto responses → SDK types
- Apply timeouts and context propagation

Each sub-client wraps a specific gRPC service:

| Sub-client | gRPC Service | Macaroon |
|------------|-------------|----------|
| `walletClient` | `TaprootAssets` | admin |
| `walletKitClient` | `AssetWallet` | walletkit |
| `proofClient` | `TaprootAssets` | proof |
| `universeClient` | `Universe` | universe |

### `vpsbt/`

Virtual PSBT (vPSBT) encoding for interactive transfers. This package
constructs the binary vPSBT format that `tapd` expects for the
`FundVirtualPsbt` RPC.

Key type: `InteractiveVPacket` — Contains asset ID, amount, receiver keys,
lock times, and alt-leaves. Encodes to a BIP-174 compatible PSBT with
Taproot Assets-specific key-value pairs.

### `codec/`

Cryptographic utilities:

- **Alt-leaves** — TLV encoding for auxiliary Taproot leaves committed
  alongside asset commitments
- **STXO derivation** — Deriving provably unspendable burn keys by
  tweaking a NUMS point
- **VarInt** — BIP-174/TLV compact integer encoding

### `macaroon/`

Macaroon authentication helpers:

- Load macaroons from files, directories, or hex strings
- Attach macaroon metadata to gRPC contexts
- Support per-service macaroon granularity

## Transfer Flows

### Address-Based Send

The normal address-based receive flow should prefer V2 addresses. Older
address versions remain available for advanced compatibility use through the
lower-level client surface, but they are not the default UX the SDK should
promote.

```
Sender                             tapd
  │                                  │
  ├─ TxBuilder.AddRecipient(addr) ─►│
  ├─ TxBuilder.Fund() ──────────►│ FundVirtualPsbt
  ├─ TxBuilder.Sign() ──────────►│ SignVirtualPsbt
  ├─ TxBuilder.Commit() ────────►│ CommitVirtualPsbts
  ├─ TxBuilder.Finish() ────────►│ PublishAndLogTransfer
  │                                  │
  │  (proof courier delivers proof)  │
```

### Interactive Send

```
Receiver                Sender                      tapd
  │                       │                           │
  ├─ DeriveKeys() ──────►│                           │
  │  (share keys)         │                           │
  │                       ├─ SetAsset(id, amt) ──────►│
  │                       ├─ SetReceiverKeys(keys) ──►│
  │                       ├─ Execute() ──────────────►│
  │                       │   (build vPSBT internally)│
  │                       │   FundInteractivePsbt ───►│
  │                       │   SignVirtualPsbt ────────►│
  │                       │   AnchorVirtualPsbts ─────►│
  │                       │                           │
  │  (sender delivers     │                           │
  │   proof out-of-band)  │                           │
  │                       │                           │
  ├─ ImportProof() ──────►│                           │
  │   UnpackProofFile ────┼──────────────────────────►│
  │   InsertProof (×N) ──┼──────────────────────────►│
  │   RegisterTransfer ──┼──────────────────────────►│
```

## Error Strategy

The SDK uses a single `Error` type that wraps all failures:

```go
type Error struct {
    Op  string  // Operation that failed
    Err error   // Underlying error (often gRPC status)
}
```

Classification methods check the underlying gRPC status code:
- `IsNotFound()` — resource doesn't exist
- `IsUnavailable()` — `tapd` is unreachable
- `IsInvalidArgument()` — bad input

Sentinel errors exist for builder state violations:
- `ErrBuilderFinished` — builder already executed
- `ErrNotFunded` — attempted to sign before funding
- `ErrNotSigned` — attempted to commit before signing
- `ErrNoRecipients` — no recipients configured

## Future Directions

1. **Complete RPC coverage** — Wrap remaining `tapd` RPCs where low-level
   access is still useful, without turning the public SDK surface into a thin
   `taprpc` mirror
2. **Streaming support** — Subscribe to receive/send events
3. **Asset minting** — High-level mint workflows via the Mint service,
   redesigned around fungible/non-fungible concepts instead of raw RPC shape
4. **Multi-language bindings** — WASM, Python, mobile via FFI
5. **Integration test suite** — Regtest-based tests against a real `tapd`
