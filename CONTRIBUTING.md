# Contributing

Thanks for helping improve `tap-sdk`. The project is pre-v1, so API changes
are still possible, but they should be intentional and backed by tests.

## Setup

Requirements:

- Go 1.25.10+
- Docker and Docker Compose
- A local `tapd` only when running manual tests outside the regtest stack

Useful commands:

```bash
make build
make fmt
make unit
make lint workers=4
```

Integration tests:

```bash
make itest       # pinned tapd image
make itest-main  # tapd built from taproot-assets main
```

Prefer `make itest-main` only when SDK `main` intentionally depends on
unreleased tapd behavior.

## Development Principles

- Keep the high-level API `AssetRef`-first.
- Preserve the distinction between `Asset`, `Collection`, and `Issuance`.
- Put SDK business types in the root package.
- Keep tapd wire details inside `grpc` and `rest`.
- Do not expose `taprpc` types in public signatures outside transport
  packages.
- Add integration tests for user-facing workflows, not just marshal helpers.

Read:

- [Architecture](docs/architecture.md)
- [Asset Model](docs/asset-model.md)
- [Design Decisions](docs/design/README.md)
- [Integration Tests](itest/README.md)

## Style

Follow Lightning Labs Go conventions:

- 80 character line limit where practical
- exported declarations have comments starting with the declaration name
- comments explain useful context, not obvious code mechanics
- table-driven tests with `require`
- `make fmt` for imports and formatting

## Commits

Format:

```text
subsystem: short description
```

Examples:

- `wallet: add ownership proof helper`
- `grpc: map burn asset type`
- `docs: refresh public README`

Commit subjects should be concise. Body lines should be wrapped to roughly 72
characters when a body is useful.

## Pull Requests

Before opening or updating a PR:

- run `make fmt`
- run `make unit`
- run `make lint workers=4`
- run focused itests when the change touches SDK workflows
- update docs when the public API changes

Use a design document under `docs/design/` for durable API or architecture
decisions. Do not add implementation plans to `docs/design/`.
