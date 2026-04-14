# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, rebased onto `origin/main`.
- Current state: mint helpers now wait for confirmed balances, grouped receive
  flows explicitly bootstrap the issuance universe before creating V2
  addresses, and the bitcoind miner wallet is created only when missing.
- Local validation: `go test ./...` and `go test -tags=itest -run '^$' ./itest/...`
  pass after the latest refactor.
- Regtest execution: not possible locally in this environment because Docker is
  unavailable, so CI is the source of truth for the full suite and will rerun
  after the branch push.
- No changelog entry yet. This repo is still pre-release.
