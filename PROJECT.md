# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, rebased onto `origin/main`.
- Current state: integration helpers split into focused files, tests now use the
  opinionated `Wallet` receive/send and balance flow where available, and the
  refactor is pushed to the PR branch.
- Local validation: `go test ./...` and `go test -tags=itest -run '^$' ./itest/...`
  pass.
- Regtest execution: not possible locally in this environment because Docker is
  unavailable, so CI is the source of truth for the full suite and is currently
  rerunning after the force-push.
- No changelog entry yet. This repo is still pre-release.
