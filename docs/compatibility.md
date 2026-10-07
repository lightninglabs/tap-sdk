# Compatibility

| tap-sdk line | tapd | lnd | Go |
|--------------|------|-----|----|
| `v0.3.x`, `main` | v0.8.5 or newer | v0.21.4-beta | 1.26.0+ |
| `v0.2.x` | v0.8.3 | v0.21.3-beta | 1.26.0+ |
| `v0.1.x` | v0.8.0 | v0.21.0-beta | 1.25.10+ |

Both gRPC and REST transports are tested against these daemon versions,
including custom-anchor commitments, MuSig2 spends, timeout spends, and proof
paths in v0.2.x and v0.3.x.

SDK v0.3.x requires tapd v0.8.5 or newer. New commitments always use V1
transition proofs, including spender leaves, split-root STXO proofs, and
root-locator proofs. When upgrading from v0.2.x, remove uses of the
`TransitionProofVersion` type and constants and the
`CommitVirtualPsbtsRequest.TransitionProofVersion` selector. Previously
confirmed proof histories remain usable.

The Go dependencies follow the btcd-v2 development line, while the integration
suite runs the released daemon images. Regtest sets `proofactivationheight=1`
on both nodes to enforce the v0.8.5 proof rules throughout the suite.

## Release validation

Run the full integration suite against the pinned daemon images:

```bash
make itest
```

For development against tapd main:

```bash
make itest-main
```
