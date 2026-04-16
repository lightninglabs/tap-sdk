# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, now at local changes on top of
  `21d1dbe` (`multi: fall back to grouped balance lookups`).
- Live PR state at 2026-04-16 20:14 UTC before this maintenance slice:
  - PR #37 is open on `feat/integration-tests`
  - no comments, reviews, review threads, or other actionable feedback
  - `Format check`, `Lint check`, `Compilation check`, and `Unit tests`
    are green
  - `Regtest integration tests` is the only failing check
- Failure mode diagnosed in this run: the remaining regtest failure was not in
  the balance transports anymore. The itest helper that recovers the semantic
  grouped `AssetRef` from `ListGroups` hex keys was parsing 33-byte compressed
  group keys through `ParseTaprootPubKey`, which normalizes them via x-only
  Schnorr parsing. That flips odd-Y compressed pubkeys, so the tests could end
  up polling balances and creating receive addresses with a different group key
  than tapd actually minted.
- Current fix: `itest/parseGroupRefKey` now preserves exact 33-byte compressed
  group keys with `ParsePubKey`, while still accepting 32-byte x-only inputs
  through the taproot fallback path. Added focused itest unit coverage to lock
  down both behaviors.
- Local validation after the fix:
  - `go test ./...` ✅
  - `go test -tags=itest -run '^$' ./itest/...` ✅
  - `go test -tags=itest -run
    '^(TestParseGroupRefKeyPreservesCompressedKey|TestParseGroupRefKeyXOnlyFallback)$'
    ./itest/...` ✅
- Full regtest execution is still not possible locally in this environment
  because `docker` is unavailable, so CI remains the source of truth for the
  real integration run after the next push.
- No changelog entry yet. This repo is still pre-release.
