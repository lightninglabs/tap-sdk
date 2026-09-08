# Compatibility

| tap-sdk line | tapd | lnd | Go |
|--------------|------|-----|----|
| `main` / `v0.2.x` | v0.8.3 | v0.21.3-beta | 1.26.0+ |
| `v0.1.x` | v0.8.0 | v0.21.0-beta | 1.25.10+ |

Both gRPC and REST transports are tested against these daemon versions,
including custom-anchor commitments, MuSig2 spends, timeout spends, and proof
paths in v0.2.x.

## Release validation

Run the full integration suite against the pinned daemon images:

```bash
make itest
```

For development against tapd main:

```bash
make itest-main
```
