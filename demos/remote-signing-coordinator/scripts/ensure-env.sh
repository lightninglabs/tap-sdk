#!/usr/bin/env bash
set -euo pipefail

DEMO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "${DEMO_DIR}/.env" ]]; then
  cp "${DEMO_DIR}/.env.example" "${DEMO_DIR}/.env"
  echo "Created ${DEMO_DIR}/.env from .env.example"
fi
