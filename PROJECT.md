# tap-sdk Project State

- Repository: `github.com/lightninglabs/tap-sdk`
- Active focus: PR #37, integration test PR review follow-up.
- Branch: `feat/integration-tests`
- Checked on: 2026-04-17 20:03 UTC
- Current state: review fixes pushed, CI running again

## Open PRs

### PR #37

- Title: `itest: add integration test suite (regtest)`
- Branch: `feat/integration-tests`
- State before this slice: open, not draft, targets `main`
- Merge state before this slice: `CLEAN`
- Live feedback before coding:
  - one pending review from `darioAnongba`
  - two pending review threads
  - no issue comments or standalone review comments
  - all checks green from the previous push:
    - `Format check` ✅
    - `Lint check` ✅
    - `Compilation check` ✅
    - `Unit tests` ✅
    - `Regtest integration tests` ✅
- Actionable feedback handled in this slice:
  - run the regtest workflow on every pull request, not only when `itest/`
    or the workflow changes
  - stop using `lncm/bitcoind` in the integration test compose stack, use
    `bitcoin/bitcoin:30.2`
- Changes made:
  - updated `.github/workflows/regtest.yml` so `pull_request` runs without a
    path filter
  - updated `itest/README.md` to document that regtest runs on every PR
  - updated `itest/docker-compose.yml` to use `bitcoin/bitcoin:30.2`
- Local validation after the fix:
  - `go test ./...` ✅
  - `go test -tags=itest -run '^$' ./itest/...` ✅
- Local environment limit:
  - full regtest execution is still impossible here because `docker` is not
    available, so GitHub Actions remains the source of truth for the real
    end-to-end run after the next push

### PR #39

- Title: `examples: add tutorials and runnable examples`
- Branch: `docs/tutorials-and-examples`
- State: open, not draft, targets `main`
- Status: untouched in this slice

## Repo hygiene

Completed on 2026-04-17 20:03 UTC.

- `git fetch origin --prune`
- `git checkout main`
- `git pull --ff-only origin main`
- checked local branches already fully merged into `main`
- no branches were deleted, because none besides `main` were fully merged

## Next trigger

- wait for the re-run on PR #37 to finish
- resume only if PR #37 gets more review feedback or a failing check
- do not start a new PR while #37 and #39 remain open
