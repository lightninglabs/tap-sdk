# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, rebased onto `origin/main`.
- Live PR state before the latest push: PR #37 is open, mergeable, has no
  comments/reviews/review threads, and only the regtest integration workflow is
  failing.
- Current state: the regtest suite now uses explicit test fixtures instead of a
  shared parent `testing.T`, each subtest gets its own funded harness,
  grouped-asset mints resolve the canonical semantic `AssetRef` from
  `ListGroups`, grouped receive bootstrap is centralized in one helper, and the
  balance wait helper now logs balance/unconfirmed-transfer progress directly.
- Local validation: `go test ./...` and `go test -tags=itest -run '^$' ./itest/...`
  pass after the latest refactor.
- Regtest execution: not possible locally in this environment because Docker is
  unavailable, so CI is the source of truth for the full suite and will rerun
  after the branch push.
- No changelog entry yet. This repo is still pre-release.
