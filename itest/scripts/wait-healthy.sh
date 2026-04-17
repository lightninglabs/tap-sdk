#!/usr/bin/env bash
# wait-healthy.sh — Block until the itest Docker Compose stack is ready.
#
# Used by the Makefile (itest-up / itest-up-main) and the CI workflow
# so the sequencing lives in one place.
#
# Env vars (all optional):
#   BITCOIND_USER      (default: devuser)
#   BITCOIND_PASS      (default: devpass)
#   WAIT_BITCOIND_SECS (default: 60)
#   WAIT_LND_SECS      (default: 120)
#   WAIT_TAPD_SECS     (default: 180)

set -euo pipefail

BITCOIND_USER="${BITCOIND_USER:-devuser}"
BITCOIND_PASS="${BITCOIND_PASS:-devpass}"

wait_healthy() {
  local container="$1"
  local max_wait="$2"
  local elapsed=0
  while [ "$elapsed" -lt "$max_wait" ]; do
    status=$(docker inspect --format='{{.State.Health.Status}}' \
      "$container" 2>/dev/null || echo "missing")
    if [ "$status" = "healthy" ]; then
      echo "$container is healthy"
      return 0
    fi
    echo "  $container status: $status (${elapsed}s / ${max_wait}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: $container never became healthy after ${max_wait}s" >&2
  docker logs "$container" 2>&1 | tail -50 >&2
  return 1
}

wait_healthy tap-sdk-bitcoind  "${WAIT_BITCOIND_SECS:-60}"
wait_healthy tap-sdk-lnd-alice "${WAIT_LND_SECS:-120}"
wait_healthy tap-sdk-lnd-bob   "${WAIT_LND_SECS:-120}"

# Mine a single block so LND can activate and tapd can start syncing.
# The Go harness handles coinbase-maturity mining + LND funding from
# there — this step only unblocks the tapd healthchecks.
echo "Mining 1 activation block..."
docker exec tap-sdk-bitcoind bitcoin-cli \
  -regtest -rpcuser="$BITCOIND_USER" -rpcpassword="$BITCOIND_PASS" \
  createwallet "miner" >/dev/null 2>&1 || true
ADDR=$(docker exec tap-sdk-bitcoind bitcoin-cli \
  -regtest -rpcuser="$BITCOIND_USER" -rpcpassword="$BITCOIND_PASS" \
  -rpcwallet=miner getnewaddress)
docker exec tap-sdk-bitcoind bitcoin-cli \
  -regtest -rpcuser="$BITCOIND_USER" -rpcpassword="$BITCOIND_PASS" \
  -rpcwallet=miner generatetoaddress 1 "$ADDR" >/dev/null

wait_healthy tap-sdk-tapd-alice "${WAIT_TAPD_SECS:-180}"
wait_healthy tap-sdk-tapd-bob   "${WAIT_TAPD_SECS:-180}"

# Final verification: tapd macaroons must exist.
for c in tap-sdk-tapd-alice tap-sdk-tapd-bob; do
  docker exec "$c" ls -la \
    /root/.tapd/data/regtest/admin.macaroon >/dev/null
done
echo "All services ready"
