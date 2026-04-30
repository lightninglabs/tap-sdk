# Integration Tests (regtest)

End-to-end tests that run against real `tapd` instances in regtest mode.
Every SDK-level test runs against both the `grpc` and `rest` transports
so the two clients stay in lock-step.

## Prerequisites

- Docker and Docker Compose
- Go 1.25+
- `curl` (for bitcoind RPC fallback when `bitcoin-cli` is unavailable)

## Quick Start

The Makefile drives the whole flow — stack up, wait-healthy, Go tests,
and teardown — so both local dev and CI run the same commands.

```bash
# Full run against the pinned taproot-assets image (same thing CI does).
make itest

# Or break it apart for iteration:
make itest-up     # start + wait-healthy
make itest-run    # just the Go tests
make itest-down   # stop + clean volumes
```

### Running against tapd built from `main`

The default compose file pins `lightninglabs/taproot-assets:v0.7.2`.
Some tests (e.g. `TestBurnAssetByGroupKey`) require features that only
ship in tapd `main`, so they are skipped unless `TAP_SDK_TAPD_MAIN=1`.

To run the full suite against a locally-built tapd:

```bash
make itest-main            # up (rebuild) + tests (TAP_SDK_TAPD_MAIN=1) + down
# or piece-wise:
make itest-up-main
make itest-run-main
make itest-down-main
```

`docker-compose.local.yml` rebuilds the `tap-sdk/tapd:local` image from
the upstream `taproot-assets` `main` Dockerfile each time. CI does not
use this override.

### Common Make knobs

| Variable | Purpose |
|---|---|
| `case` | Restrict to tests matching a regex (`make itest-run case=TestMintAsset`). |
| `ITEST_TIMEOUT` | `go test -timeout=` value. Default `20m`. |
| `ITEST_ARGS` | Extra flags forwarded to `go test` (e.g. `ITEST_ARGS='-v -count=2'`). |

## Architecture

```
bitcoind (regtest)                  [bitcoin/bitcoin image]
├── lnd-alice  ← tapd-alice         (gRPC 10029, REST 8089)
└── lnd-bob    ← tapd-bob           (gRPC 10030, REST 8090)
```

Each daemon is configured through a checked-in config file under
`itest/{bitcoin,lnd,tapd}/` — the compose file only layers them into
the containers.

## Transport Matrix

Tests that exercise the SDK surface use `runForTransports(t, fn)` and
appear in verbose `go test -v` output as:

```
--- PASS: TestGetInfo
    --- PASS: TestGetInfo/grpc
    --- PASS: TestGetInfo/rest
```

To narrow a run to a single transport, set `TAP_SDK_TRANSPORTS`:

```bash
TAP_SDK_TRANSPORTS=grpc go test -tags=itest ./itest/...
```

The Makefile defaults to non-verbose `go test` output so CI failures stay
focused on the final assertion. When you need the full helper trace locally,
pass `ITEST_ARGS='-v'`.

Streaming event subscriptions run across both transports. The REST client uses
tapd's grpc-gateway WebSocket bridge for server-streaming RPCs.

## Helper Structure

- `NewTestHarness(t)` / `NewTestHarnessWithTransport(t, transport)` wires
  Alice and Bob SDK clients + wallets against the requested transport.
- `newHarnessContext(t)` / `newHarnessContextFor(t, transport)` produce a
  harness plus background context for connection-only tests.
- `newFundedHarness(t)` / `newFundedHarnessFor(t, transport)` bootstrap
  Alice's LND wallet with coins so asset tests do not repeat funding.
- `CreateFungibleAndConfirm`, `CreateNFTAndConfirm`, `CreateCollectionAndConfirm`,
  `IssueFungibleAndConfirm`, and `MintCollectionItemAndConfirm` use the
  high-level `Wallet.NewIssuer()` surface. They mine, wait for chain sync, and
  wait for tapd's mint batch to leave the active lifecycle before returning.
- `MintAssetAndConfirm` is reserved for tests that intentionally assert the
  low-level mint batch lifecycle.
- `MintResult.Ref` carries the semantic `AssetRef` that high-level wallet
  helpers should use, so grouped fungible tests do not depend on the raw
  group-key shape returned by `ListAssetRecords`.
- `CreateGroupedReceiveAddress(...)` centralises the V2 grouped receive
  bootstrap flow for Bob and retries until the receiver is ready.

## Test Scenarios

| Test | Description |
|------|-------------|
| `TestGetInfo` | Connect and verify node information |
| `TestMintAsset` | Low-level fungible mint + list groups/batches |
| `TestMintCollectible` | Low-level NFT mint |
| `TestIssuerFungibleSurface` | High-level issuer create/issue fungible flow |
| `TestIssuerNFTCollectionSurface` | High-level issuer standalone NFT and collection flow |
| `TestIssuerPendingBatchConflict` | High-level issuer rejects active low-level batches |
| `TestCancelBatch` | Stage a batch and cancel before finalization |
| `TestAddressSend` | Mint → address → send → verify balances, transfers and receive events |
| `TestProofOperations` | Export, unpack, decode, and verify proofs |
| `TestBalanceQueries` | AssetRef-based balance queries (fungible + collectible), `Wallet.GetBalance` parity |
| `TestWalletSurface` | Low-level ListAssetRecords/ListUtxos/ListAssetGroups/FetchAssetMeta round-trips |
| `TestMultiTrancheGroup` | Wallet ListAssets fungible aggregation and ListIssuances tranche access |
| `TestBurnAsset` | Burn + ListBurns lifecycle |
| `TestErrorHandling` | Invalid inputs return proper errors |
| `TestEventListenerMintAndSend` | `tapsdk.NewEventListener` delivers mint/send/receive events |
| `TestTLSAndMacaroonGuards` | Valid creds connect; bad TLS/macaroon rejected |

## Environment Variables

Override default connection settings with:

| Variable | Default | Description |
|----------|---------|-------------|
| `TAP_SDK_TRANSPORTS` | `grpc,rest` | Comma-separated list of transports to exercise |
| `TAPD_ALICE_HOST` | `localhost:10029` | Alice's tapd gRPC address |
| `TAPD_ALICE_REST` | `https://localhost:8089` | Alice's tapd REST address |
| `TAPD_ALICE_UNIVERSE_HOST` | `tapd-alice:10029` | Alice's tapd address as seen from Bob's container during universe sync |
| `TAPD_BOB_HOST` | `localhost:10030` | Bob's tapd gRPC address |
| `TAPD_BOB_REST` | `https://localhost:8090` | Bob's tapd REST address |
| `BITCOIND_HOST` | `localhost:18443` | bitcoind RPC address |
| `TAPD_ALICE_TLS` | Auto-extracted from container | Alice's TLS cert path |
| `TAPD_BOB_TLS` | Auto-extracted from container | Bob's TLS cert path |
| `TAPD_ALICE_MAC` | Auto-extracted from container | Alice's macaroon path |
| `TAPD_BOB_MAC` | Auto-extracted from container | Bob's macaroon path |
| `TAPD_BOB_PROOF_COURIER_ADDR` | `authmailbox+universerpc://tapd-bob:10029` | Bob's default proof courier for V2 receive addresses |
