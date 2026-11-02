# Development Cycle

`tap-sdk` follows [semantic versioning][Semantic Versioning], but it is
currently pre-v1. Until `v1.0.0`, public APIs may change while the SDK settles
around Taproot Assets v0.8 and the planned multi-language model.

The v0.1 release line, from v0.1.1, targets:

- tapd / Taproot Assets v0.8.5 or newer, built with the tapd v0.8.6 module
- lnd v0.21.4-beta, built with the lnd v0.21.3-beta module
- btcd v1 modules
- Go 1.26.0+

The first public tag was `v0.1.0`, not `v1.0.0`. The SDK is still missing some
planned workflows and has not yet had broad external developer testing, so
keeping the release pre-v1 correctly signals that public APIs may still change.

Release branches are cut from `main`. After a release branch is cut, new
features should continue on `main` while the release branch focuses on fixes,
compatibility, and documentation.

The `v0.1` branch carries the `main` API on the btcd v1 modules for
applications that require released lnd v0.21 or tapd v0.8 packages. Fixes land
on `main` first and are backported with their btcd imports rewritten. The
branch takes no new features and ends once lnd and tapd release on btcd v2. It
is based on `v0.1.0` rather than `main`, so its pseudo-versions stay below
`v0.2.0`.

Compatibility details live in [docs/compatibility.md](docs/compatibility.md).

[Semantic Versioning]: https://semver.org/
