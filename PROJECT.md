# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, pushed at `21d1dbe`
  (`multi: fall back to grouped balance lookups`).
- Live PR state after the 2026-04-16 08:02 UTC maintenance run:
  - PR #37 is open on `feat/integration-tests`
  - no comments, reviews, review threads, or other actionable feedback
  - CI was re-queued by the push and is now pending for `Format check`,
    `Lint check`, `Compilation check`, `Unit tests`, and
    `Regtest integration tests`
- Failure mode addressed in this run: the previous client-side regrouping fix
  still left `TestBalanceQueries/grouped_fungible_asset` timing out. When a
  grouped `AssetRef` lookup still came back empty after the asset-id grouped
  path, the SDK had no second path to recover the semantic group balance.
- Current fix: both the gRPC and REST clients now fall back to an explicit
  group-key balance query for grouped `AssetRef` requests whose semantic
  regrouping result is empty, then stitch that balance back onto a semantic
  `AssetRef` response using a representative grouped asset for genesis
  metadata. Unit tests cover the fallback decision, grouped balance lookup,
  and semantic response construction in both transports.
- Local validation after the fix:
  - `go test ./...` ✅
  - `go test -tags=itest -run '^$' ./itest/...` ✅
- Full regtest execution is still not possible locally in this environment
  because `docker` is unavailable, so CI remains the source of truth for the
  real integration run after the push.
- No changelog entry yet. This repo is still pre-release.
