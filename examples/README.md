# tap-sdk Examples

Runnable Go programs demonstrating common Taproot Assets workflows
using `tap-sdk`.

## Prerequisites

1. A running `tapd` instance (regtest recommended for development).
2. TLS certificate and admin macaroon for authentication.
3. Go 1.23+ installed.

## Configuration

All examples read connection parameters from environment variables:

| Variable | Description | Default |
|---|---|---|
| `TAPD_HOST` | gRPC host:port of tapd | `localhost:10029` |
| `TAPD_TLS_PATH` | Path to tapd's `tls.cert` | `~/.tapd/tls.cert` |
| `TAPD_MACAROON_PATH` | Path to `admin.macaroon` | (auto-detected from `~/.tapd/data/<network>/`) |
| `TAPD_NETWORK` | Bitcoin network | `regtest` |

## Examples

| Directory | Description |
|---|---|
| [`connect/`](connect/) | Connect to tapd, print node info |
| [`mint/`](mint/) | Full minting lifecycle: stage, finalize, confirm |
| [`send/`](send/) | Mint an asset, create a receive address, send it |
| [`proofs/`](proofs/) | Export, decode, and verify proofs |
| [`universe/`](universe/) | Query universe roots, stats, and leaves |

## Running

```bash
# Set environment variables (adjust paths for your setup).
export TAPD_HOST=localhost:10029
export TAPD_TLS_PATH=$HOME/.tapd/tls.cert
export TAPD_MACAROON_PATH=$HOME/.tapd/data/regtest/admin.macaroon
export TAPD_NETWORK=regtest

# Run an example.
cd examples/connect
go run .
```

Each example is a standalone `main.go` that compiles and runs
independently.
