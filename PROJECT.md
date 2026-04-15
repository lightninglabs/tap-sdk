# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, regtest integration suite cleanup and refactor.
- Branch: `feat/integration-tests`, rebased onto `origin/main`.
- Live PR state before the next push on 2026-04-15 20:08 UTC: PR #37 is open,
  mergeable, has no comments/reviews/review threads, and only the regtest
  integration workflow is failing.
- Latest failure mode: `TestBalanceQueries/grouped_fungible_asset` times out
  while waiting for Alice's grouped balance after mint. The root cause is the
  SDK's optimized `ListBalances` path: it always groups by `asset_id` to keep
  genesis metadata, but it was still forwarding `group_key_filter` to tapd,
  even though tapd only accepts that filter when the daemon itself groups by
  group key.
- Current fix in progress: grouped `AssetRef` balance requests now fetch the
  per-asset-id balances, re-aggregate them into semantic `AssetRef` balances,
  and then apply the grouped filter client-side. Both the gRPC and REST
  clients follow that rule now, and unit tests cover the request marshaling and
  post-aggregation filtering behavior.
- Local validation after the fix: `go test ./...` and
  `go test -tags=itest -run '^$' ./itest/...` pass.
- Regtest execution: still not possible locally in this environment because
  `docker` is unavailable, so CI remains the source of truth for the full
  suite after the branch push.
- No changelog entry yet. This repo is still pre-release.
