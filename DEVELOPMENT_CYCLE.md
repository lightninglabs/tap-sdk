# Development Cycle

`tap-sdk` follows [semantic versioning][Semantic Versioning], but it is
currently pre-v1. Until `v1.0.0`, public APIs may change while the SDK settles
around Taproot Assets v0.8 and the planned multi-language model.

The first release line targets:

- tapd / Taproot Assets v0.8.0 or newer
- lnd v0.21.0-beta or newer
- Go 1.25.10+

The first public tag is `v0.1.0`, not `v1.0.0`. The SDK is still missing some
planned workflows and has not yet had broad external developer testing, so
keeping the release pre-v1 correctly signals that public APIs may still change.

Release branches are cut from `main`. After a release branch is cut, new
features should continue on `main` while the release branch focuses on fixes,
compatibility, and documentation.

Compatibility details live in [docs/compatibility.md](docs/compatibility.md).

[Semantic Versioning]: https://semver.org/
