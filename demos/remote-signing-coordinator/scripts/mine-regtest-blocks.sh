#!/usr/bin/env bash
set -euo pipefail

BITCOIND_CONTAINER="${BITCOIND_CONTAINER:-tap-sdk-bitcoind}"
BITCOIND_USER="${BITCOIND_USER:-devuser}"
BITCOIND_PASS="${BITCOIND_PASS:-devpass}"
REGTEST_MINER_WALLET="${REGTEST_MINER_WALLET:-miner}"
REGTEST_MINE_BLOCKS="${REGTEST_MINE_BLOCKS:-6}"

bitcoin_cli() {
  docker exec "${BITCOIND_CONTAINER}" bitcoin-cli \
    -regtest \
    -rpcuser="${BITCOIND_USER}" \
    -rpcpassword="${BITCOIND_PASS}" \
    "$@"
}

miner_cli() {
  bitcoin_cli -rpcwallet="${REGTEST_MINER_WALLET}" "$@"
}

bitcoin_cli createwallet "${REGTEST_MINER_WALLET}" >/dev/null 2>&1 ||
  bitcoin_cli loadwallet "${REGTEST_MINER_WALLET}" >/dev/null 2>&1 ||
  true

address="$(miner_cli getnewaddress "" bech32)"
miner_cli generatetoaddress "${REGTEST_MINE_BLOCKS}" "${address}" >/dev/null

echo "Mined ${REGTEST_MINE_BLOCKS} regtest blocks"
