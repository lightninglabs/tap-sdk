# Integration Tests

The integration suite runs SDK workflows against real `tapd` nodes in regtest.
SDK-surface tests run against both gRPC and REST so the two transports stay in
lock-step.

## Prerequisites

- Docker and Docker Compose
- Go 1.25.7+
- `curl` for bitcoind RPC fallback when `bitcoin-cli` is unavailable

## Run the Suite

```bash
# Full suite against the pinned tapd image, same shape as CI.
make itest

# Run against tapd built from taproot-assets main.
make itest-main
```

For iteration:

```bash
make itest-up
make itest-run case=TestGetInfo
make itest-down
```

For tapd main:

```bash
make itest-up-main
make itest-run-main case=TestGetInfo
make itest-down-main
```

## Version Targets

The pinned compose stack uses the tapd image in
[`docker-compose.yml`](docker-compose.yml). The local override
[`docker-compose.local.yml`](docker-compose.local.yml) rebuilds tapd from the
upstream `taproot-assets` `main` branch.

Use `make itest-main` when SDK `main` is ahead of the latest released tapd
image and you need to validate behavior against unreleased daemon changes.

## Useful Knobs

| Variable | Purpose |
|----------|---------|
| `case` | Regex passed to `go test -run`, e.g. `make itest-run case=TestBurn` |
| `ITEST_TIMEOUT` | `go test -timeout` value. Default: `20m` |
| `ITEST_ARGS` | Extra flags passed to `go test`, e.g. `ITEST_ARGS='-count=2'` |
| `TAP_SDK_TRANSPORTS` | Comma-separated transports. Default: `grpc,rest` |

Example:

```bash
TAP_SDK_TRANSPORTS=grpc make itest-run case=TestIssuer
```

## Stack

```
bitcoind (regtest)
├── lnd-alice  ← tapd-alice  (gRPC 10029, REST 8089)
└── lnd-bob    ← tapd-bob    (gRPC 10030, REST 8090)
```

Checked-in config lives under:

- `itest/bitcoin/`
- `itest/lnd/`
- `itest/tapd/`

## Test Helpers

- `NewTestHarness` creates Alice and Bob SDK clients and wallets.
- `newFundedHarness` funds Alice's lnd wallet before running asset workflows.
- `CreateFungibleAndConfirm`, `CreateNFTAndConfirm`,
  `CreateCollectionAndConfirm`, `IssueFungibleAndConfirm`, and
  `MintCollectionItemAndConfirm` use the high-level `Wallet.NewIssuer()`
  surface.
- `MintAssetAndConfirm` is reserved for tests that intentionally exercise the
  low-level mint batch lifecycle.
- `MintResult.Ref` carries the semantic `AssetRef` that high-level wallet
  helpers should use.
- `CreateGroupedReceiveAddress` centralizes the V2 grouped receive bootstrap
  flow for Bob.

## Coverage Themes

The suite is intentionally written as user workflows:

- connect over gRPC and REST
- issue fungibles, NFTs, and collections
- send by V2 address
- aggregate balances by `AssetRef`
- export, import, unpack, and decode proofs
- query universe roots and proofs
- burn fungibles and NFT collection items
- receive mint, send, and receive events
- prove and verify ownership
- reject bad TLS and macaroon material
- verify read-only macaroon permissions

When a test needs a workaround, treat it as a signal that the SDK or tapd API
surface may need improvement.

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `TAPD_ALICE_HOST` | `localhost:10029` | Alice gRPC address |
| `TAPD_ALICE_REST` | `https://localhost:8089` | Alice REST address |
| `TAPD_ALICE_UNIVERSE_HOST` | `tapd-alice:10029` | Alice as seen from Bob |
| `TAPD_BOB_HOST` | `localhost:10030` | Bob gRPC address |
| `TAPD_BOB_REST` | `https://localhost:8090` | Bob REST address |
| `BITCOIND_HOST` | `localhost:18443` | bitcoind RPC address |
| `TAPD_ALICE_TLS` | extracted from container | Alice TLS cert path |
| `TAPD_BOB_TLS` | extracted from container | Bob TLS cert path |
| `TAPD_ALICE_MAC` | extracted from container | Alice macaroon path |
| `TAPD_BOB_MAC` | extracted from container | Bob macaroon path |
| `TAPD_BOB_PROOF_COURIER_ADDR` | `authmailbox+universerpc://tapd-bob:10029` | Bob proof courier for V2 receive addresses |
