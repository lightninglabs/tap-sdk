# Development Cycle

This project follows a regular releasing schedule where a new release is made at a regular cadence, with all the feature/bugfixes that made it to `main` in time. This ensures that we don't keep delaying releases waiting for "just one more little thing".

This project uses [Semantic Versioning], but is currently at MAJOR version zero (0.y.z) meaning it is still in initial development. Anything MAY change at any time. The public API SHOULD NOT be considered stable. Until we reach version `1.0.0` we will do our best to document any breaking API changes in the changelog info attached to each release tag.

A "feature freeze" happens when a new branch is created originating from the `main` tip at that time, and in that branch we will stop adding new features and only focus on ensuring the ones we've added are working properly.

[Semantic Versioning]: https://semver.org/
