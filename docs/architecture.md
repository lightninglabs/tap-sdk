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
│  Uses: Wallet, Issuer, Universe, TxBuilder, etc.    │
├─────────────────────────────────────────────────────┤
│                    tap-sdk                           │
│                                                     │
│  ┌────────┐ ┌────────┐ ┌──────────┐ ┌───────────┐ │
│  │ Wallet │ │ Issuer │ │ Universe │ │ TxBuilder │ │
│  └───┬────┘ └───┬────┘ └────┬─────┘ └─────┬─────┘ │
│      │          │           │             │       │
│  ┌───┴──────────┴───────────┴─────────────┴────┐  │
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
  focused high-level methods for common operations (receive addresses, key
  derivation, proof import, asset listing, send, burn, and transfer history).
  Raw RPC-shaped operations remain available through `Wallet.Client()`.

- **`Issuer`** — High-level minting surface returned by `Wallet.NewIssuer()`.
  Creates fungible assets, issues more fungible supply, creates standalone
  NFTs, creates NFT collections, and mints collection items without exposing
  tapd mint batch control to application code.

- **`Universe`** — High-level universe surface returned by
  `Wallet.NewUniverse()`. Provides `AssetRef`-first known-asset checks, root
  lookup, proof listing, proof lookup, and targeted sync helpers. Raw
  protocol-shaped universe RPCs remain on `UniverseClient`.

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
  fungible assets, NFT items, and collections are passed around by
  `AssetRef`; raw group keys and concrete issuance asset IDs are protocol
  details for low-level records and diagnostics.

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
| `mintClient` | `Mint` | mint |
| `eventClient` | `TaprootAssets`, `Mint` | admin, mint |

### Event Subscriptions

`EventClient` exposes the three server-streaming RPCs (`SubscribeReceive`,
`SubscribeSend`, `SubscribeMint`) as typed channels that deliver SDK
entities until the caller's context is cancelled.

- The **gRPC** client consumes the native server-streaming RPCs directly.
- The **REST** client dials the grpc-gateway WebSocket bridge
  (`wss://.../v1/taproot-assets/events/*?method=POST`), writes the JSON
  request as the first frame, and decodes each incoming
  `{"result": ...}` envelope into an SDK event. Errors arrive inside
  `{"error": ...}` envelopes and surface as `*APIError` on the error
  channel so callers can use the same `status`/`codes` helpers they use
  for unary calls.

The high-level `tapsdk.EventListener` sits on top of `EventClient` and
adds reconnect/backoff, so the two transports share reconnection
behavior.

`EventClient.SubscribeSendEvents` / `SubscribeReceiveEvents` deliver raw
records (`SendEventRecord`, `ReceiveEventRecord`) for advanced consumers
that need PSBTs, virtual packets, or other protocol-shaped fields. The
high-level `EventListener` projects these records into the AssetRef-keyed
`SendEvent` and `ReceiveEvent` shapes before invoking handlers, so
application code stays on the same semantic asset handles used by the
rest of the Wallet API.

Transfer records include tapd's asset type for each input and output. The
SDK uses it when projecting raw transfer rows so grouped fungibles stay keyed
by their group `AssetRef`, while NFT collection items stay keyed by their
concrete item `AssetRef`. Receive addresses for NFT collections are still
collection `AssetRef`s because the concrete item is chosen at send time;
completed send events and transfer history expose the item `AssetRef` once
tapd has recorded the transfer.

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

- Load macaroons from files, directories, or hex strings via a
  typed `Source` (`macaroon.FromPath` / `FromDir` / `FromHex`)
- Attach macaroon metadata to gRPC contexts
- Support per-service macaroon granularity

## TLS and Authentication

The SDK communicates with `tapd` over gRPC with TLS encryption and
macaroon-based authentication. Both inputs are supplied as typed
sources on `Config` — exactly one choice per field, enforced at
compile time.

### TLS Configuration

`grpc.Config.TLS` (and `rest.Config.TLS`) takes a `TLSSource`:

| Constructor | Behavior |
|-------------|----------|
| (nil) | Load cert from tapd default path (`~/.tapd/tls.cert`) |
| `TLSFromPath(path)` | Load cert from a custom file path |
| `TLSFromData(pem)` | Use PEM-encoded certificate data directly |
| `TLSSystemCert()` | Trust the system certificate pool |
| `TLSInsecure()` | Skip TLS verification (testing only) |

Since `TLS` is a single field, conflicting combinations cannot be
expressed — previously-runtime errors like `ErrTLSConflict` are
replaced by a type-checked choice.

### TLS Hardening

**Minimum version floor.** The SDK enforces TLS 1.2 as the minimum
protocol version. This is configurable via `Config.TLSMinVersion`
(use `crypto/tls` constants). Go's TLS implementation already
defaults to 1.2, but the SDK is explicit to prevent regressions.

**Certificate pinning.** Set `Config.TLSPinnedCertFingerprint` to
a hex-encoded SHA-256 digest of the expected server leaf certificate.
When set, the SDK rejects connections to any server presenting a
different certificate. The fingerprint can use colons as separators
(e.g. `aa:bb:cc:...`).

To obtain a fingerprint from an existing `tapd` TLS certificate:

```bash
openssl x509 -in ~/.tapd/tls.cert -outform DER | \
  sha256sum | awk '{print $1}'
```

### Macaroon Authentication

Each gRPC sub-client uses a service-specific macaroon:

| Sub-client | Macaroon |
|------------|----------|
| walletClient | `admin.macaroon` |
| walletKitClient | `walletkit.macaroon` |
| proofClient | `proof.macaroon` |
| universeClient | `universe.macaroon` |
| mintClient | `mint.macaroon` |

Macaroons can be loaded from a directory, a specific file path, or
hex-encoded data.

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
  ├─ ImportProofFile() ──►│                           │
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

## Dependency Boundary

The SDK enforces a strict dependency boundary between consumers and the
`taproot-assets` ecosystem:

- **No `taprpc` types in public signatures.** Every exported type, function,
  and method outside the `grpc/` package is defined entirely in terms of
  `entities/` types and Go/btcsuite primitives. Consumers never need to
  import `taproot-assets/taprpc` or any of its sub-packages.

- **`grpc/` is the sole proto consumer.** All proto imports, marshal/unmarshal
  functions, and raw RPC client types are confined to the `grpc/` package.
  The sub-client structs (`walletClient`, `proofClient`, etc.) are unexported,
  and their internal helper methods (macaroon auth, raw client access) are
  also unexported to prevent taprpc types from leaking through embedding.

- **`taproot-assets/taprpc` is a lightweight module.** The `go.mod`
  dependency is on `taproot-assets/taprpc`, which is a standalone Go module
  containing only protobuf definitions. It does NOT pull the full
  `taproot-assets` repository with its heavy dependencies (bbolt, neutrino,
  btcwallet, etc.).

- **Consumer `go.mod` impact.** When a consumer runs
  `go get github.com/lightninglabs/tap-sdk`, the `taproot-assets/taprpc`
  module appears as an indirect dependency. Consumers who only import the
  root package or `entities/` never interact with it directly.

## Future Directions

1. **Complete RPC coverage** — Wrap remaining `tapd` RPCs where low-level
   access is still useful, without turning the public SDK surface into a thin
   `taprpc` mirror
2. **Asset minting** — High-level mint workflows via the Mint service,
   redesigned around fungible/non-fungible concepts instead of raw RPC shape
3. **Multi-language bindings** — WASM, Python, mobile via FFI
4. **Integration test suite** — Regtest-based tests against a real `tapd`
