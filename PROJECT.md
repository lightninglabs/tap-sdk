# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, rebased onto `origin/main`.
- Live PR state before the next push on 2026-04-15 UTC: PR #37 is open,
  mergeable, has no comments/reviews/review threads, and only the regtest
  integration workflow is failing. The live failure is `TestAddressSend`,
  which times out on a redundant pre-send grouped balance wait while the
  dedicated grouped-balance test is already green in the same CI run.
- Current state: the regtest suite now uses explicit fixtures for connected vs
  funded harness setup, grouped-asset mints resolve the canonical semantic
  `AssetRef` from `ListGroups`, grouped receive bootstrap retries the real end
  condition, Bob successfully creating a V2 receive address, and
  `TestAddressSend` no longer blocks on that redundant pre-send balance poll.
  Group-key parsing in the itest helpers also now accepts the x-only or
  compressed encodings returned by the underlying stack.
- Local validation: `go test ./...` and
  `go test -tags=itest -run '^$' ./itest/...` pass after the latest change.
- Regtest execution: still not possible locally in this environment because
  `docker` is unavailable, so CI remains the source of truth for the full
  suite after the branch push.
- No changelog entry yet. This repo is still pre-release.
