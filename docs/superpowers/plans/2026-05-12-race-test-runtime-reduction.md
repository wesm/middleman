# Race Test Runtime Reduction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce `go test -race` wall-clock time by splitting monolithic test packages, replacing sleep-based waits with deterministic synchronization, and parallelizing only tests that are isolated enough to run safely under the race detector.

**Architecture:** Treat this as a test-architecture change, not a CI-only tweak. First make the slow packages schedulable by Go as smaller test binaries, then remove fixed sleeps from hot paths, then add controlled intra-package parallelism where isolation is proven.

**Tech Stack:** Go 1.26, `go test -race`, `testing/synctest` for pure in-process timing tests, SQLite test fixtures, Huma/API integration tests, tmux/git workspace tests.

---

## Baseline

The PR race run linked by the user spent most of its time in package execution, not Testcontainers compilation:

- `internal/server`: `8m17s`
- `internal/github`: `4m06s`
- `internal/db`: `3m58s`
- `internal/workspace`: `1m26s`

The current race command in `.github/workflows/ci.yml` is:

```bash
go tool gotestsum --format pkgname-and-test-fails --jsonfile=tmp/test-race-output.json -- -race ./... -shuffle=on
```

That means any package with hundreds of serial tests becomes the long pole. Package splitting and deterministic waits are the main levers.

## Synctest Guidance

Use `testing/synctest` only for tests whose concurrency and timers are fully in-process.

Good candidates:

- syncer cancellation tests using fake clients and timers
- cooldown/backoff tests that wait on `time.After`, `time.Sleep`, or context deadlines
- event hub tests that coordinate goroutines and timers without sockets/processes
- local manager state-machine tests with fake command runners, after a runner seam exists

Avoid `synctest` for:

- `httptest.Server`, WebSockets, real TCP listeners
- tmux, PTY, shell, git, or any external process
- filesystem polling driven by external processes
- tests that call `t.Parallel`, `t.Run`, or `t.Deadline` inside the `synctest.Run` bubble

`synctest.Wait` is meaningful for `-race`: the Go blog documents that `Wait` is understood by the race detector as synchronization, and omitting it can still produce a race report. So `synctest` can work well with race when the test is structurally eligible.

## File Structure

Create or modify these areas:

- Modify `.github/workflows/ci.yml`: optionally split the race job once package split work lands.
- Create `internal/server/apitest/`: black-box API tests currently concentrated in `internal/server/api_test.go`.
- Create `internal/server/workspacetest/`: workspace and tmux-heavy server e2e tests currently concentrated in `internal/server/api_test.go` and `internal/server/tmux_wrapper_test.go`.
- Create `internal/server/settingstest/`: settings/repo-config API tests currently in `internal/server/settings_test.go`.
- Create `internal/github/syncertest/`: black-box syncer behavior using exported APIs.
- Modify `internal/server/api_test.go`, `internal/server/tmux_wrapper_test.go`, `internal/server/settings_test.go`: leave only tests that need unexported server internals.
- Modify `internal/github/sync_test.go`: leave only tests that need unexported syncer internals.
- Modify `internal/server/*_test.go`, `internal/github/*_test.go`, `internal/workspace/*_test.go`: replace sleeps with event-driven helpers or `synctest` where eligible.
- Modify `internal/testutil/`: add shared helpers for copied SQLite templates, readiness channels, and black-box server fixtures if needed.

## Task 1: Add Repeatable Timing Baselines

**Files:**
- Create: `scripts/test-package-times.sh`
- Modify: `Makefile`

- [ ] **Step 1: Add a timing script**

Create `scripts/test-package-times.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

packages=("$@")
if [ "${#packages[@]}" -eq 0 ]; then
  packages=(
    ./internal/server
    ./internal/github
    ./internal/db
    ./internal/workspace
    ./internal/ratelimit
    .
  )
fi

mkdir -p tmp
go test -race -shuffle=on -json "${packages[@]}" \
  | tee tmp/race-package-times.json \
  | jq -r '
      select(.Action == "pass" and .Package and (.Test | not))
      | "\(.Elapsed)s\t\(.Package)"
    '
```

- [ ] **Step 2: Make it executable**

Run:

```bash
chmod +x scripts/test-package-times.sh
```

- [ ] **Step 3: Add a Makefile target**

Add:

```make
.PHONY: race-times
race-times:
	./scripts/test-package-times.sh
```

- [ ] **Step 4: Verify baseline**

Run:

```bash
make race-times
```

Expected: package timing rows for the slow packages and `tmp/race-package-times.json`.

- [ ] **Step 5: Commit**

```bash
git add Makefile scripts/test-package-times.sh
git commit -m "test: add race package timing helper"
```

## Task 2: Split Server API Tests Into Black-Box Packages

**Files:**
- Create: `internal/server/apitest/api_test.go`
- Create: `internal/server/apitest/fixtures_test.go`
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Identify export-safe tests**

Move tests that only exercise public HTTP/API behavior through the generated client and do not require unexported `server` package symbols.

Initial candidates:

```text
TestAPIListPulls
TestAPIGetPull
TestAPIListRepos
TestAPIListRepoSummaries
TestAPIListIssuesStateFilter
TestAPIGetIssueIncludesLabels
TestAPIRateLimits
TestAPIGetCommits
TestAPIGetDiff_*
TestAPIListActivity
```

Leave tests in `internal/server` when they directly use unexported helpers or inspect unexported fields.

- [ ] **Step 2: Create a black-box fixture**

Create `internal/server/apitest/fixtures_test.go` with package `apitest`. Start with a small fixture that imports `github.com/wesm/middleman/internal/server`, `internal/db`, `internal/apiclient`, and `internal/testutil`.

The fixture should create:

```go
type fixture struct {
	database *db.DB
	server   *server.Server
	client   *apiclient.Client
}
```

Use `httptest.NewServer` or the existing in-process client pattern, whichever avoids reaching into unexported server state.

- [ ] **Step 3: Move a first small batch**

Move only 10-20 API tests first. Do not move workspace/tmux tests in this task.

- [ ] **Step 4: Run both packages**

Run:

```bash
go test ./internal/server ./internal/server/apitest -shuffle=on
go test -race ./internal/server ./internal/server/apitest -shuffle=on
```

Expected: both commands pass, and the race run shows two package timings instead of one monolith.

- [ ] **Step 5: Commit**

```bash
git add internal/server/api_test.go internal/server/apitest
git commit -m "test: split server API tests into black-box package"
```

## Task 3: Split Workspace/Tmux Server E2E Tests

**Files:**
- Create: `internal/server/workspacetest/workspace_test.go`
- Create: `internal/server/workspacetest/fixtures_test.go`
- Modify: `internal/server/api_test.go`
- Modify: `internal/server/tmux_wrapper_test.go`

- [ ] **Step 1: Move only black-box workspace flows**

Move tests that exercise workspace endpoints through HTTP/client APIs and do not require unexported state. Initial candidates include tests named:

```text
TestWorkspaceRuntime*E2E
TestWorkspaceCreate*E2E
TestWorkspaceDelete*E2E
TestWorkspaceDiffEndpoint*E2E
TestWorkspaceList*E2E
TestTerminalRouteE2EPropagatesWorkspaceID
```

- [ ] **Step 2: Preserve tmux isolation**

Every moved test must use unique temp dirs and unique tmux session names derived from `t.Name()` or the existing workspace ID. Do not introduce package-level tmux state.

- [ ] **Step 3: Replace readiness polling in moved tests**

Replace the helper that sleeps before polling:

```go
for range 50 {
	time.Sleep(100 * time.Millisecond)
	// poll workspace
}
```

with a helper that checks immediately and uses a ticker only as a fallback:

```go
func waitForWorkspaceReady(t *testing.T, ctx context.Context, client *apiclient.Client, wsID string) *generated.WorkspaceResponse {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		getResp, err := client.HTTP.GetWorkspacesByIdWithResponse(ctx, wsID)
		require.NoError(t, err)
		if getResp.StatusCode() == http.StatusOK && getResp.JSON200 != nil && getResp.JSON200.Status == "ready" {
			return getResp.JSON200
		}
		select {
		case <-ctx.Done():
			require.NoError(t, ctx.Err(), "workspace never became ready: %s", wsID)
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 4: Run focused race tests**

Run:

```bash
go test -race ./internal/server ./internal/server/workspacetest -shuffle=on
```

Expected: pass. Compare package timing to the pre-split baseline.

- [ ] **Step 5: Commit**

```bash
git add internal/server/api_test.go internal/server/tmux_wrapper_test.go internal/server/workspacetest
git commit -m "test: split workspace server e2e tests"
```

## Task 4: Split Server Settings Tests

**Files:**
- Create: `internal/server/settingstest/settings_test.go`
- Create: `internal/server/settingstest/fixtures_test.go`
- Modify: `internal/server/settings_test.go`

- [ ] **Step 1: Move HTTP-only settings tests**

Move tests covering settings, repo config, glob preview, bulk add, and refresh behavior when they can use exported HTTP endpoints.

- [ ] **Step 2: Keep internal handler tests in place**

Keep tests named like `TestHandle*` in `internal/server` if they call handlers directly or rely on unexported mocks.

- [ ] **Step 3: Replace channel sleeps**

For tests waiting for async sync or config refresh, add explicit callback channels in test fixtures instead of `time.After(5 * time.Second)` waits.

Use this pattern:

```go
done := make(chan struct{})
syncer.SetOnSyncCompleted(func(_ []ghclient.RepoSyncResult) {
	close(done)
})

select {
case <-done:
case <-time.After(time.Second):
	require.Fail(t, "timed out waiting for sync completion")
}
```

Prefer closing a channel from the mock action that the test actually needs over waiting for broad sync completion.

- [ ] **Step 4: Verify**

Run:

```bash
go test -race ./internal/server ./internal/server/settingstest -shuffle=on
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/settings_test.go internal/server/settingstest
git commit -m "test: split server settings API tests"
```

## Task 5: Split GitHub Sync Tests by Public Contract

**Files:**
- Create: `internal/github/syncertest/syncer_test.go`
- Create: `internal/github/syncertest/fixtures_test.go`
- Modify: `internal/github/sync_test.go`

- [ ] **Step 1: Move exported API tests**

Move tests that use only exported constructors and methods:

```text
NewSyncer
RunOnce
Stop
TriggerRun
SyncMR
SyncIssue
SetOnStatusChange
SetOnSyncCompleted
SetWatchedMRs
```

Keep tests that call unexported methods like `doSyncRepoGraphQLIssues`, `backfillRepo`, or `drainPendingCommentSyncs` in `internal/github`.

- [ ] **Step 2: Create black-box mocks**

Use a local fake client in `syncertest` that implements `github.Client`. Keep it minimal and per-test.

- [ ] **Step 3: Run both packages**

Run:

```bash
go test -race ./internal/github ./internal/github/syncertest -shuffle=on
```

Expected: pass and lower max package duration.

- [ ] **Step 4: Commit**

```bash
git add internal/github/sync_test.go internal/github/syncertest
git commit -m "test: split syncer contract tests"
```

## Task 6: Replace Sleep-Based Syncer Tests With Synctest

**Files:**
- Modify: `internal/github/sync_test.go`
- Modify: `internal/github/syncertest/syncer_test.go`

- [ ] **Step 1: Pick one eligible test**

Start with a test that uses fake clients and sleeps only to allow goroutines or timers to advance, such as cancellation/backoff tests in `internal/github/sync_test.go`.

- [ ] **Step 2: Convert to `testing/synctest`**

Use this shape:

```go
func TestRunOnceCancelDuringBackoffDoesNotReportSuccess(t *testing.T) {
	synctest.Run(func() {
		// construct fake client, DB, and syncer inside the bubble
		// start goroutines inside the bubble
		// advance fake time with time.Sleep(...)
		synctest.Wait()
		// assert final state
	})
}
```

Do not call `t.Run`, `t.Parallel`, or `t.Deadline` inside the `synctest.Run` function.

- [ ] **Step 3: Verify with race**

Run:

```bash
go test -race ./internal/github -run 'TestRunOnceCancelDuringBackoffDoesNotReportSuccess' -shuffle=on
```

Expected: pass with no real-time waiting.

- [ ] **Step 4: Convert remaining eligible tests**

Convert only tests where every goroutine and timer is created inside the bubble. Leave HTTP, git, tmux, and DB migration tests alone unless proven safe.

- [ ] **Step 5: Commit**

```bash
git add internal/github
git commit -m "test: remove real sleeps from syncer timing tests"
```

## Task 7: Replace Server Sleep/Polling With Events

**Files:**
- Modify: `internal/server/server_test.go`
- Modify: `internal/server/event_hub_test.go`
- Modify: `internal/server/sync_cooldown_e2e_test.go`
- Modify: `internal/server/api_test.go`
- Modify: moved server test packages from Tasks 2-4

- [ ] **Step 1: Replace readiness sleeps**

Every test that sleeps before checking a condition should check once immediately, then wait on either:

- a callback channel from the fake syncer/client
- a server-sent event already emitted by the system under test
- a short ticker only when the underlying behavior is inherently observable only by polling

- [ ] **Step 2: Replace negative sleeps carefully**

Tests using `assert.Never` or `time.After(50 * time.Millisecond)` should prefer asserting explicit non-events after draining known work:

```go
select {
case got := <-unexpected:
	require.Failf(t, "unexpected event", "%v", got)
default:
}
```

If real time is the behavior under test, keep a small timeout and document why.

- [ ] **Step 3: Verify server packages**

Run:

```bash
go test -race ./internal/server/... -shuffle=on
```

- [ ] **Step 4: Commit**

```bash
git add internal/server
git commit -m "test: replace server polling sleeps with deterministic waits"
```

## Task 8: Add Safe `t.Parallel` in Isolated Tests

**Files:**
- Modify: split test packages from Tasks 2-5
- Modify: `internal/db/*_test.go`
- Modify: `internal/github/normalize_test.go`
- Modify: `internal/platform/**/*_test.go` if needed

- [ ] **Step 1: Define exclusion rules**

Do not add `t.Parallel` to tests that use:

- `t.Setenv`
- `os.Chdir`
- package-level mutable state
- fixed TCP ports
- shared tmux session names
- shared filesystem paths outside `t.TempDir`
- external processes that read process-global env
- SQLite databases shared across tests

- [ ] **Step 2: Add `t.Parallel` to pure tests first**

Good first targets:

```text
internal/github/normalize_test.go
internal/github/workflow_approval_test.go
internal/platform/*/convert_test.go
internal/db query tests that each use openTestDB(t)
```

- [ ] **Step 3: Run with shuffle and race**

Run:

```bash
go test -race ./internal/github ./internal/db ./internal/platform/... -shuffle=on
```

- [ ] **Step 4: Commit**

```bash
git add internal/github internal/db internal/platform
git commit -m "test: parallelize isolated unit tests"
```

## Task 9: Reduce SQLite Setup Cost for Non-Migration Tests

**Files:**
- Create: `internal/db/testtemplate_test.go`
- Modify: `internal/db/*_test.go`
- Modify: `internal/github/*_test.go`
- Modify: `internal/server/**/fixtures_test.go`
- Modify: `internal/workspace/*_test.go`

- [ ] **Step 1: Add a copied template DB helper**

Create a helper that migrates once per package, then copies the migrated database file into each test temp dir. Do not use it for migration tests.

Target API:

```go
func openTemplateTestDB(t *testing.T) *DB
```

- [ ] **Step 2: Keep migration tests on `Open`**

Do not change tests that intentionally cover legacy migrations, dirty migrations, timestamp repair, or schema history.

- [ ] **Step 3: Convert high-volume fixture callers**

Convert `openTestDB(t)` in packages with many DB-backed tests after verifying the copied template handles WAL sidecar files correctly.

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test -race ./internal/db ./internal/github ./internal/server/... ./internal/workspace -shuffle=on
```

- [ ] **Step 5: Commit**

```bash
git add internal/db internal/github internal/server internal/workspace
git commit -m "test: reuse migrated SQLite templates in isolated tests"
```

## Task 10: Split CI Race Job After Package Restructure

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add a race matrix**

Replace the single package sweep with a matrix after package splitting has landed:

```yaml
strategy:
  fail-fast: false
  matrix:
    group:
      - name: server-api
        packages: ./internal/server/...
      - name: github-db-workspace
        packages: ./internal/github/... ./internal/db ./internal/workspace ./internal/workspace/localruntime
      - name: other
        packages: ./...
```

Do not leave `other` as `./...` unless exclusions are added, or it will duplicate the slow groups. Use an explicit package list generated by:

```bash
go list ./... | grep -Ev '/internal/(server|github|db|workspace)(/|$)'
```

- [ ] **Step 2: Preserve reports**

Make report filenames include the matrix group:

```bash
go tool gotestsum --format pkgname-and-test-fails --jsonfile="tmp/test-race-${{ matrix.group.name }}.json" -- -race ${{ matrix.group.packages }} -shuffle=on
```

- [ ] **Step 3: Verify workflow syntax**

Run:

```bash
git diff --check .github/workflows/ci.yml
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: split race tests across package groups"
```

## Final Verification

Run:

```bash
make race-times
go test -race ./... -shuffle=on
```

Expected outcome:

- no single package remains above roughly 2-3 minutes under `-race`
- full local `go test -race ./... -shuffle=on` passes
- CI wall-clock time drops because the long poles are split into smaller package binaries and/or separate matrix jobs

## Self-Review

- Spec coverage: package splitting, sleep replacement, `synctest`, race compatibility, and CI follow-up are covered.
- Placeholder scan: no implementation task uses placeholder language.
- Type consistency: package names and helper names are intentionally concrete enough for implementation, but exact moved test lists should be adjusted during execution based on unexported-symbol dependencies.
