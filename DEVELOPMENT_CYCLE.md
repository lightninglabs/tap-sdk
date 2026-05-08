# Development Cycle

`tap-sdk` follows [semantic versioning][Semantic Versioning], but it is
currently pre-v1. Until
`v1.0.0`, public APIs may change while the SDK settles around Taproot Assets
v0.8 and the planned multi-language model.

The first release line targets:

- tapd / Taproot Assets v0.8.0 or newer
- lnd v0.20.x
- Go 1.25.7+

Release branches are cut from `main`. After a release branch is cut, new
features should continue on `main` while the release branch focuses on fixes,
compatibility, and documentation.

Compatibility details live in [docs/compatibility.md](docs/compatibility.md).

[Semantic Versioning]: https://semver.org/
