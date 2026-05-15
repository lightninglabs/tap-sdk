#!/usr/bin/env bash
set -euo pipefail

DEMO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALICE_CONTAINER="${TAPD_ALICE_CONTAINER:-tap-sdk-tapd-alice}"
BOB_CONTAINER="${TAPD_BOB_CONTAINER:-tap-sdk-tapd-bob}"
NETWORK="${TAPD_NETWORK:-regtest}"

"${DEMO_DIR}/scripts/ensure-env.sh"

copy_creds() {
  local container="$1"
  local name="$2"
  local target="${DEMO_DIR}/.tapd/${name}"

  mkdir -p "${target}"
  docker cp "${container}:/root/.tapd/tls.cert" \
    "${target}/tls.cert"
  docker cp "${container}:/root/.tapd/data/${NETWORK}/admin.macaroon" \
    "${target}/admin.macaroon"
  chmod 600 "${target}/admin.macaroon"
}

copy_creds "${ALICE_CONTAINER}" "alice"
copy_creds "${BOB_CONTAINER}" "bob"

echo "Exported tapd credentials to ${DEMO_DIR}/.tapd"
