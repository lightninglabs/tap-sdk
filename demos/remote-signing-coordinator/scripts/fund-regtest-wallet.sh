#!/usr/bin/env bash
set -euo pipefail

BITCOIND_CONTAINER="${BITCOIND_CONTAINER:-tap-sdk-bitcoind}"
LND_ALICE_CONTAINER="${LND_ALICE_CONTAINER:-tap-sdk-lnd-alice}"
BITCOIND_USER="${BITCOIND_USER:-devuser}"
BITCOIND_PASS="${BITCOIND_PASS:-devpass}"
FUND_AMOUNT_BTC="${FUND_AMOUNT_BTC:-1.0}"
MIN_CONFIRMED_SATS="${MIN_CONFIRMED_SATS:-100000}"

bitcoin_cli() {
  docker exec "${BITCOIND_CONTAINER}" bitcoin-cli \
    -regtest \
    -rpcuser="${BITCOIND_USER}" \
    -rpcpassword="${BITCOIND_PASS}" \
    "$@"
}

miner_cli() {
  bitcoin_cli -rpcwallet=miner "$@"
}

alice_confirmed_balance() {
  docker exec "${LND_ALICE_CONTAINER}" lncli --network=regtest \
    walletbalance |
    sed -nE 's/.*"confirmed_balance": "([0-9]+)".*/\1/p' |
    head -n 1
}

mine_blocks() {
  local blocks="$1"
  local address

  address="$(miner_cli getnewaddress "" bech32)"
  miner_cli generatetoaddress "${blocks}" "${address}" >/dev/null
}

bitcoin_cli createwallet "miner" >/dev/null 2>&1 ||
  bitcoin_cli loadwallet "miner" >/dev/null 2>&1 ||
  true

confirmed_balance="$(alice_confirmed_balance)"
confirmed_balance="${confirmed_balance:-0}"

if [[ "${confirmed_balance}" -ge "${MIN_CONFIRMED_SATS}" ]]; then
  echo "Alice LND already funded with ${confirmed_balance} confirmed sats"
  exit 0
fi

echo "Mining coinbase maturity for the demo miner wallet..."
mine_blocks 110

alice_address="$(
  docker exec "${LND_ALICE_CONTAINER}" lncli --network=regtest \
    newaddress p2wkh |
    sed -nE 's/.*"address": "([^"]+)".*/\1/p' |
    head -n 1
)"

if [[ -z "${alice_address}" ]]; then
  echo "ERROR: failed to get an Alice LND address" >&2
  exit 1
fi

echo "Funding Alice LND with ${FUND_AMOUNT_BTC} BTC..."
miner_cli sendtoaddress "${alice_address}" "${FUND_AMOUNT_BTC}" >/dev/null

echo "Mining confirmations for Alice LND..."
mine_blocks 6

for _ in {1..30}; do
  confirmed_balance="$(alice_confirmed_balance)"
  confirmed_balance="${confirmed_balance:-0}"

  if [[ "${confirmed_balance}" -ge "${MIN_CONFIRMED_SATS}" ]]; then
    echo "Alice LND funded with ${confirmed_balance} confirmed sats"
    exit 0
  fi

  sleep 2
done

echo "ERROR: Alice LND wallet did not report confirmed funds" >&2
exit 1
