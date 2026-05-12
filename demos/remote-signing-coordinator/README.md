# Remote Signing Coordinator

This demo is a local coordinator for Taproot Assets Issuances that need an
external signing device. It uses the Go SDK on the server side and a
Next.js/Tailwind dashboard for the browser workflow.

The dashboard does not talk to `tapd` directly. The Go server connects to
`tapd`, starts an SDK issuer call, pauses when the SDK asks for an external
Issuance signature, exposes the unsigned virtual PSBT for review, accepts the
signed virtual PSBT, and then lets the SDK finalize the Issuance.

## Requirements

- Docker with Compose
- Go, using the version required by this repository
- Node.js with Yarn 4 through Corepack

If Corepack is not enabled yet:

```bash
corepack enable
```

## Quick Start

From this directory:

```bash
cd dashboard
yarn install
yarn regtest
yarn dev
```

Open the dashboard at:

```text
http://127.0.0.1:3000
```

`yarn regtest` starts the repository integration-test regtest stack, funds
Alice's LND wallet, and exports Alice and Bob `tapd` credentials into `.tapd/`.
The default `.env` values point the coordinator at Alice on regtest.

Stop the regtest stack with:

```bash
yarn regtest:down
```

## Commands

Run from `dashboard/`:

| Command | Purpose |
| --- | --- |
| `yarn dev` | Run the Go coordinator and dashboard together |
| `yarn server` | Run only the Go coordinator |
| `yarn dashboard` | Run only the Next.js dashboard |
| `yarn regtest` | Start the pinned regtest stack, fund Alice, and export creds |
| `yarn regtest:down` | Stop the pinned regtest stack |
| `yarn test` | Typecheck, lint, build dashboard, build server |

## Configuration

Defaults live in `.env.example`. Running any demo command creates `.env` from
that file if it does not exist.

| Variable | Default |
| --- | --- |
| `COORDINATOR_LISTEN` | `127.0.0.1:8091` |
| `COORDINATOR_API_URL` | `http://127.0.0.1:8091` |
| `TAP_TRANSPORT` | `grpc` |
| `TAPD_HOST` | `localhost:10029` |
| `TAPD_REST_URL` | `https://localhost:8089` |
| `TAPD_NETWORK` | `regtest` |
| `TAPD_TLS_PATH` | `.tapd/alice/tls.cert` |
| `TAPD_MACAROON_PATH` | `.tapd/alice/admin.macaroon` |
| `TAPD_TLS_INSECURE` | `false` |

Relative paths are resolved from this demo directory, so the default credential
paths work after `corepack yarn regtest`.

## Flow

1. Enter the external Issuance key descriptor: account xpub, master
   fingerprint, and full child derivation path, such as
   `m/86'/1'/0'/0/0` on regtest.
   The included software device can generate this descriptor from a browser
   mnemonic using BDK-WASM.
2. Start either a new Asset or a new Issuance for an existing AssetRef. The
   dashboard accepts the anchor fee rate in sat/vB and converts it to the
   SDK's sat/kWU fee-rate option.
3. The server calls the SDK issuer with an external signer callback.
4. The SDK stages and funds the Issuance, then returns a signing request.
5. The dashboard presents the SDK review fields:
   - the virtual PSBT authorizes a new Issuance
   - the AssetRef being affected
   - the amount by which supply increases
   - the Issuance key descriptor
   - the script key that controls the minted Asset
   - the anchor outpoint that commits the Issuance
6. Sign the virtual PSBT externally.
7. Paste the signed virtual PSBT into the dashboard.
8. The SDK submits the signature, seals the batch, and finalizes the Issuance.

Coordinator runs are kept in memory. Restarting the Go coordinator clears them.
