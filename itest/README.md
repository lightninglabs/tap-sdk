# Integration Tests (regtest)

End-to-end tests that run against real `tapd` instances in regtest mode.

## Prerequisites

- Docker and Docker Compose
- Go 1.25+
- `curl` (for bitcoind RPC fallback when `bitcoin-cli` is unavailable)

## Quick Start

```bash
# Start the test infrastructure.
docker compose -f itest/docker-compose.yml up -d

# Wait for all services to become healthy (check with docker ps).
docker ps --format "table {{.Names}}\t{{.Status}}"

# Run the integration tests.
go test -v -tags=itest -timeout=20m ./itest/...

# Tear down.
docker compose -f itest/docker-compose.yml down -v
```

## Architecture

```
bitcoind (regtest)
├── lnd-alice ← tapd-alice (port 10029)
└── lnd-bob  ← tapd-bob  (port 10030)
```

Two independent tapd nodes allow testing send/receive flows between
separate wallets.

## Helper Structure

- `newHarnessContext(t)` builds a fresh connected harness for tests that only
  need the two tapd clients.
- `newFundedHarness(t)` builds a fresh funded harness per test case so asset
  flow tests do not repeat the wallet-funding setup.
- `MintResult.Ref` carries the semantic `AssetRef` that high-level wallet
  helpers should use, so grouped fungible tests do not depend on the raw group
  key shape returned by `ListAssets`.
- `CreateGroupedReceiveAddress(...)` centralizes the V2 grouped receive
  bootstrap flow for Bob and retries until the real end condition, successful
  grouped address creation, is ready.

## Test Scenarios

| Test | Description |
|------|-------------|
| `TestGetInfo` | Connect and verify node information |
| `TestMintAsset` | Full minting lifecycle (fungible) |
| `TestMintCollectible` | Mint a collectible (NFT) asset |
| `TestAddressSend` | Mint → create address → send → verify receipt |
| `TestProofOperations` | Export, unpack, and decode proofs |
| `TestBalanceQueries` | AssetRef-based balance queries for fungible and collectible assets |
| `TestErrorHandling` | Invalid inputs return proper errors |

## Environment Variables

Override default connection settings with:

| Variable | Default | Description |
|----------|---------|-------------|
| `TAPD_ALICE_HOST` | `localhost:10029` | Alice's tapd gRPC address |
| `TAPD_ALICE_UNIVERSE_HOST` | `tapd-alice:10029` | Alice's tapd address as seen from Bob's container during universe sync |
| `TAPD_BOB_HOST` | `localhost:10030` | Bob's tapd gRPC address |
| `BITCOIND_HOST` | `localhost:18443` | bitcoind RPC address |
| `TAPD_ALICE_TLS` | Auto-extracted from container | Alice's TLS cert path |
| `TAPD_BOB_TLS` | Auto-extracted from container | Bob's TLS cert path |
| `TAPD_ALICE_MAC` | Auto-extracted from container | Alice's macaroon path |
| `TAPD_BOB_MAC` | Auto-extracted from container | Bob's macaroon path |
| `TAPD_BOB_PROOF_COURIER_ADDR` | `authmailbox+universerpc://tapd-bob:10029` | Bob's default proof courier for V2 receive addresses |

## CI

The `regtest.yml` workflow runs on:
- Manual dispatch (`workflow_dispatch`)
- PRs that modify `itest/` or the workflow itself

Container logs are uploaded as artifacts on failure.
