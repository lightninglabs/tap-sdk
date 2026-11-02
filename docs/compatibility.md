# Compatibility

| tap-sdk line | tapd | lnd | Go |
|--------------|------|-----|----|
| `v0.3.x`, `main` | v0.8.5 or newer | v0.21.4-beta | 1.26.0+ |
| `v0.2.x` | v0.8.3 | v0.21.3-beta | 1.26.0+ |
| `v0.1.1` and newer | v0.8.5 or newer | v0.21.4-beta | 1.26.0+ |
| `v0.1.0` | v0.8.0 | v0.21.0-beta | 1.25.10+ |

Both gRPC and REST transports are tested against these daemon versions,
including custom-anchor commitments, MuSig2 spends, timeout spends, and proof
paths in v0.1.1 and newer, v0.2.x, and v0.3.x.

## Go module lines

Go selects one version of each module for a whole build, so the SDK has two
release lines:

| tap-sdk line | Bitcoin modules | lnd module | tapd module |
|--------------|-----------------|------------|-------------|
| `v0.2.x`, `v0.3.x`, `main` | btcd v2 | lnd master | tapd main |
| `v0.1.1` and newer | btcd v1 | v0.21.3-beta or newer | v0.8.6 or newer |

The v0.1 line carries the v0.3.x API for modules that also require released
lnd v0.21 or tapd v0.8 packages, directly or through lndclient, Loop, or
lightning-terminal. Those packages import btcd v1 paths that the btcd v2 root
module no longer provides, so they cannot build with the v0.2.x or v0.3.x
lines.

Modules on the v0.1 line must also carry the lnd/tlv replacement from tapd
v0.8.6:

```bash
go mod edit -replace=github.com/lightningnetwork/lnd/tlv=github.com/lightninglabs/taproot-assets/third_party/lnd/tlv@v0.0.0-20261016193543-734ce74dd1e8
```

## Proof requirements

SDK v0.3.x and v0.1.1 and newer require tapd v0.8.5 or newer. New commitments
always use V1 transition proofs, including spender leaves, split-root STXO
proofs, and root-locator proofs. When upgrading from v0.2.x, remove uses of
the `TransitionProofVersion` type and constants and the
`CommitVirtualPsbtsRequest.TransitionProofVersion` selector. Previously
confirmed proof histories remain usable.

The integration suite runs the released daemon images on both module lines.
Regtest sets `proofactivationheight=1` on both nodes to enforce the v0.8.5
proof rules throughout the suite.

## Release validation

Run the full integration suite against the pinned daemon images:

```bash
make itest
```

For development against tapd main:

```bash
make itest-main
```
