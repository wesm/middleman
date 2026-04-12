# UTC Datetime Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce the new UTC datetime policy in code, tests, linting, and CI so storage and API layers stay UTC-only and the Svelte UI is the only place that localizes timestamps for display.

**Architecture:** Keep the storage/API invariant strict while staying backward-compatible with existing SQLite rows. Normalize timestamps to UTC at backend write and serialization boundaries, centralize frontend display formatting in shared UI helpers, and add lightweight lint rules plus CI coverage so regressions fail quickly.

**Tech Stack:** Go, SQLite, Huma, golangci-lint with forbidigo, Bun, Svelte 5, Vitest, Playwright, GitHub Actions

---

## Planned File Changes

- Modify: `.golangci.yml`
  Purpose: add targeted `forbidigo` guardrails for backend timezone APIs that should not appear in storage/API code.

- Modify: `.github/workflows/ci.yml`
  Purpose: run frontend lint/typecheck/unit tests and add `packages/ui` lint/typecheck coverage.

- Modify: `internal/github/sync.go`
  Purpose: normalize sync status and repo sync timestamps to UTC at creation time.

- Modify: `internal/github/sync_test.go`
  Purpose: verify sync status timestamps are UTC even when the process local timezone is not UTC.

- Modify: `internal/server/helpers.go`
  Purpose: add a small shared helper for UTC RFC3339 API serialization.

- Modify: `internal/server/huma_routes.go`
  Purpose: standardize API datetime formatting on the helper and normalize server-side merge/close/reopen write paths to UTC before persisting.

- Modify: `internal/server/api_test.go`
  Purpose: add API regression tests for UTC RFC3339 output with trailing `Z` and for UTC timestamps written by state-transition handlers.

- Modify: `packages/ui/src/utils/time.ts`
  Purpose: centralize API timestamp parsing and local presentation helpers.

- Modify: `packages/ui/src/components/ActivityFeed.svelte`
- Modify: `packages/ui/src/components/ActivityThreaded.svelte`
- Modify: `packages/ui/src/components/diff/CommitListItem.svelte`
  Purpose: replace inline date rendering logic with shared UI helpers.

- Create: `packages/ui/src/utils/time.test.ts`
  Purpose: verify UTC API timestamps are parsed as canonical instants and localized only through UI helpers.

- Create: `packages/ui/eslint.config.mjs`
  Purpose: lint `packages/ui`, including a narrow rule that bans locale-formatting calls outside the shared time helper.

- Modify: `packages/ui/package.json`
  Purpose: add or align `lint` and Svelte-aware `typecheck` scripts so the package can be checked directly in CI.

- Modify: `frontend/tests/e2e-full/activity-filters.spec.ts`
  Purpose: add end-to-end coverage for UTC API timestamps flowing through to localized UI rendering.

### Task 1: Add Backend UTC Regression Tests First

**Files:**
- Modify: `internal/db/queries_activity_test.go`
- Modify: `internal/github/sync_test.go`
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Add a DB regression test that asserts parsed timestamps end in the UTC location**

```go
func TestParseDBTimeNormalizesLocationToUTC(t *testing.T) {
	got, err := parseDBTime("2026-04-10 18:48:35 -0400 EDT")
	require.NoError(t, err)
	assert.Equal(t, time.UTC, got.Location())
	assert.Equal(t,
		time.Date(2026, 4, 10, 22, 48, 35, 0, time.UTC),
		got,
	)
}
```

- [ ] **Step 2: Add a syncer regression test that proves `LastRunAt` is UTC even when process local time is not UTC**

```go
func TestSyncStatusUpdatedUsesUTC(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("EDT", -4*60*60)
	t.Cleanup(func() { time.Local = oldLocal })

	ctx := context.Background()
	d := openTestDB(t)
	mc := &mockClient{openPRs: []*gh.PullRequest{}, comments: []*gh.IssueComment{}, reviews: []*gh.PullRequestReview{}, commits: []*gh.RepositoryCommit{}}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d,
		nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute,
		nil,
		testBudget(500),
	)

	syncer.RunOnce(ctx)
	status := syncer.Status()
	assert.Equal(t, time.UTC, status.LastRunAt.Location())
}
```

- [ ] **Step 3: Add API regression tests for UTC RFC3339 serialization with trailing `Z`**

```go
func assertRFC3339UTC(t *testing.T, got string, want time.Time) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, got)
	require.NoError(t, err)
	assert.Equal(t, want.UTC(), parsed.UTC())
	assert.True(t, strings.HasSuffix(got, "Z"), "expected UTC RFC3339 with trailing Z: %s", got)
}
```

Add coverage to existing API tests for:

- `detail_fetched_at` in `TestAPIPullDetailLoadedFlag`
- `last_run_at` in `TestAPISyncStatus`
- `created_at` in a new `TestAPIActivityReturnsUTCCreatedAt`
- `closed_at` and `merged_at` in new or expanded state-transition tests that exercise the `UpdateMRState` and `UpdateIssueState` handlers end-to-end through the API

- [ ] **Step 4: Run the backend regression tests and confirm at least the sync/API UTC checks fail before implementation**

Run: `go test ./internal/db ./internal/github ./internal/server -run 'TestParseDBTime|TestSyncStatusUpdatedUsesUTC|TestAPISyncStatus|TestAPIGetPullDetailLoaded|TestAPIActivityReturnsUTCCreatedAt|TestAPIMergePRStoresUTCTimestamps|TestAPIClosePR|TestAPICloseIssue' -count=1`

Expected: DB parser test passes, but at least one sync/API UTC test fails because the current code still creates or formats some timestamps outside the new canonical rule, including state-transition write paths in `internal/server/huma_routes.go`.

- [ ] **Step 5: Commit the test-first checkpoint**

```bash
git add internal/db/queries_activity_test.go internal/github/sync_test.go internal/server/api_test.go
git commit -m "test: add UTC datetime regression coverage"
```

### Task 2: Normalize Backend Write And Serialization Paths To UTC

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/server/helpers.go`
- Modify: `internal/server/huma_routes.go`

- [ ] **Step 1: Add a tiny shared helper for API datetime formatting**

```go
func formatUTCRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatOptionalUTCRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := formatUTCRFC3339(*t)
	return &v
}
```

- [ ] **Step 2: Replace ad hoc API datetime formatting with the helper**

Apply the helper to the existing explicit string fields in `internal/server/huma_routes.go`, including:

```go
if mr.DetailFetchedAt != nil {
	resp.DetailFetchedAt = formatUTCRFC3339(*mr.DetailFetchedAt)
}

resetStr := ""
if resetAt := rt.ResetAt(); resetAt != nil {
	resetStr = formatUTCRFC3339(*resetAt)
}

out[i] = activityItemResponse{
	CreatedAt: formatUTCRFC3339(it.CreatedAt),
}
```

The key cleanup here is removing the custom layout string at the activity endpoint and using `time.RFC3339` consistently.

- [ ] **Step 3: Normalize sync engine and server write timestamps at creation time**

Update `internal/github/sync.go` so status and sync bookkeeping use UTC immediately, and update `internal/server/huma_routes.go` so merge/close/reopen handlers persist `time.Now().UTC()` instead of local wall-clock values:

```go
s.publishStatus(&SyncStatus{
	Running:   false,
	LastRunAt: time.Now().UTC(),
	LastError: err.Error(),
})

if err := s.db.UpdateRepoSyncStarted(ctx, repoID, time.Now().UTC()); err != nil {
	return fmt.Errorf("mark sync started for %s/%s: %w", repo.Owner, repo.Name, err)
}

if err := s.db.UpdateRepoSyncCompleted(ctx, repoID, time.Now().UTC(), syncErrStr); err != nil {
	slog.Error("mark sync completed", "repo", repo.Owner+"/"+repo.Name, "err", err)
}

now := time.Now().UTC()
if err := s.db.UpdateMRState(ctx, repoID, input.Number, input.Body.State, mergedAt, closedAt); err != nil {
	return err
}
if err := s.db.UpdateIssueState(ctx, repoID, input.Number, input.Body.State, closedAt); err != nil {
	return err
}
```

- [ ] **Step 4: Re-run the targeted backend tests and then the wider backend suite**

Run: `go test ./internal/db ./internal/github ./internal/server -run 'TestParseDBTime|TestSyncStatusUpdatedUsesUTC|TestAPISyncStatus|TestAPIGetPullDetailLoaded|TestAPIActivityReturnsUTCCreatedAt|TestAPIMergePRStoresUTCTimestamps|TestAPIClosePR|TestAPICloseIssue' -count=1`

Expected: PASS

Run: `go test ./internal/github ./internal/server ./internal/db -count=1`

Expected: PASS

- [ ] **Step 5: Commit the backend UTC enforcement change**

```bash
git add internal/github/sync.go internal/github/sync_test.go internal/server/helpers.go internal/server/huma_routes.go internal/server/api_test.go internal/db/queries_activity_test.go
git commit -m "fix: normalize backend datetimes to UTC"
```

### Task 3: Centralize Frontend Presentation-Layer Time Formatting

**Files:**
- Modify: `packages/ui/src/utils/time.ts`
- Modify: `packages/ui/src/components/ActivityFeed.svelte`
- Modify: `packages/ui/src/components/ActivityThreaded.svelte`
- Modify: `packages/ui/src/components/diff/CommitListItem.svelte`
- Create: `packages/ui/src/utils/time.test.ts`

- [ ] **Step 1: Add shared helpers that separate API timestamp parsing from local display formatting**

Expand `packages/ui/src/utils/time.ts` to hold the presentation-layer-only helpers:

```ts
export function parseAPITimestamp(iso: string): Date {
  return new Date(iso);
}

export function timeAgo(dateStr: string): string {
  const diffMs = Date.now() - parseAPITimestamp(dateStr).getTime();
  const diffMin = Math.floor(diffMs / 60_000);
  if (diffMin < 1) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const days = Math.floor(diffHr / 24);
  if (days < 30) return `${days}d ago`;
  return `${Math.floor(days / 30)}mo ago`;
}

export function localDateLabel(dateStr: string): string {
  return parseAPITimestamp(dateStr).toLocaleDateString();
}
```

- [ ] **Step 2: Replace inline component logic with the shared helpers**

Use the helpers in the timestamp-heavy Svelte components instead of inline `new Date(iso).toLocaleDateString()` code:

```ts
import { localDateLabel, parseAPITimestamp, timeAgo } from "../utils/time.js";

function relativeTime(iso: string): string {
  const diff = Date.now() - parseAPITimestamp(iso).getTime();
  const days = Math.floor(diff / 86_400_000);
  if (days < 7) return timeAgo(iso);
  return localDateLabel(iso);
}
```

Keep local formatting in components, but route it through the shared helper so linting can enforce the boundary.

- [ ] **Step 3: Add Vitest coverage for the shared helper behavior**

Create `packages/ui/src/utils/time.test.ts` with coverage like:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";
import { localDateLabel, parseAPITimestamp, timeAgo } from "./time.js";

describe("time helpers", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-11T12:00:00Z"));
  });

  it("parses UTC API timestamps as absolute instants", () => {
    expect(parseAPITimestamp("2026-04-11T08:00:00-04:00").toISOString()).toBe("2026-04-11T12:00:00.000Z");
  });

  it("uses local formatting only in the presentation helper", () => {
    const spy = vi.spyOn(Date.prototype, "toLocaleDateString");
    localDateLabel("2026-04-11T12:00:00Z");
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("computes relative time from canonical instants", () => {
    expect(timeAgo("2026-04-11T11:30:00Z")).toBe("30m ago");
  });
});
```

- [ ] **Step 4: Run frontend unit tests that cover both `frontend` and `packages/ui`**

Run: `cd frontend && bun install && bun run test`

Expected: PASS, including tests under `../packages/ui/src/**/*.{test,spec}.*` via `frontend/vite.config.ts`

- [ ] **Step 5: Commit the frontend presentation-layer refactor**

```bash
git add packages/ui/src/utils/time.ts packages/ui/src/utils/time.test.ts packages/ui/src/components/ActivityFeed.svelte packages/ui/src/components/ActivityThreaded.svelte packages/ui/src/components/diff/CommitListItem.svelte
git commit -m "refactor: centralize UI datetime presentation"
```

### Task 4: Add Lint And CI Enforcement For The Policy

**Files:**
- Modify: `.golangci.yml`
- Modify: `.github/workflows/ci.yml`
- Create: `packages/ui/eslint.config.mjs`
- Modify: `packages/ui/package.json`

- [ ] **Step 1: Add targeted Go lint guardrails with `forbidigo`**

Extend `.golangci.yml` with narrow timezone rules that are safe to enforce in non-test backend code:

```yaml
    forbidigo:
      forbid:
        - pattern: '^time\.Local$'
          msg: Backend timezone conversion is not allowed. Keep storage and API datetimes in UTC.
        - pattern: '^time\.LoadLocation$'
          msg: Do not add backend timezone loading for storage or API paths. Local timezone conversion belongs in the Svelte UI.
        - pattern: '^time\.FixedZone$'
          msg: Do not create backend timezone conversions outside tests.
```

Apply them only to non-test Go files, either via `files`/`exclude` scoping or an equivalent golangci-lint exclusion, so regression tests can still use `time.Local` and `time.FixedZone` intentionally. Do not ban `time.Now()` globally, because deadlines and timers legitimately use it.

- [ ] **Step 2: Add `packages/ui` linting that bans locale-formatting calls outside the shared helper**

Create `packages/ui/eslint.config.mjs` by mirroring `frontend/eslint.config.mjs`, then add a restriction like:

```js
{
  files: ["src/**/*.{ts,svelte}"],
  rules: {
    "no-restricted-syntax": [
      "error",
      {
        selector: "CallExpression[callee.property.name='toLocaleDateString']",
        message: "Use localDateLabel() from src/utils/time.ts so timestamp localization stays in the presentation helper.",
      },
      {
        selector: "CallExpression[callee.property.name='toLocaleString']",
        message: "Use shared presentation-layer time helpers instead of inline locale formatting.",
      },
      {
        selector: "CallExpression[callee.property.name='toLocaleTimeString']",
        message: "Use shared presentation-layer time helpers instead of inline locale formatting.",
      },
    ],
  },
}
```

Add an override that disables those restrictions in `src/utils/time.ts`.

- [ ] **Step 3: Add package scripts so CI can check `packages/ui` directly**

Update `packages/ui/package.json` with:

```json
{
  "scripts": {
    "lint": "eslint .",
    "typecheck": "bunx svelte-check --tsconfig ./tsconfig.json --fail-on-warnings && bunx tsc --noEmit -p ./tsconfig.json"
  }
}
```

Keep `svelte-check` in the script even if `tsc` also runs, because this package exports `.svelte` components and plain TypeScript compilation is not enough to cover them. If Bun workspace resolution requires local devDependencies for ESLint or Svelte tooling, add the same lint-related packages already used by `frontend` rather than inventing a different toolchain.

- [ ] **Step 4: Wire the checks into CI**

Update `.github/workflows/ci.yml` so CI runs the currently-missing frontend and `packages/ui` checks:

```yaml
  lint:
    steps:
      - uses: oven-sh/setup-bun@0c5077e51419868618aeaa5fe8019c62421857d6
      - name: Install JS dependencies
        run: bun install --frozen-lockfile
      - name: Run frontend lint and typecheck
        run: cd frontend && bun run typecheck && bun run lint
      - name: Run packages/ui lint and typecheck
        run: cd packages/ui && bun run typecheck && bun run lint

  test:
    steps:
      - uses: oven-sh/setup-bun@0c5077e51419868618aeaa5fe8019c62421857d6
      - name: Install JS dependencies
        run: bun install --frozen-lockfile
      - name: Run frontend unit tests
        run: cd frontend && bun run test
```

This gives CI coverage for:

- frontend lint
- frontend typecheck
- frontend Vitest tests
- `packages/ui` lint
- `packages/ui` typecheck
- `packages/ui` tests via the existing `frontend/vite.config.ts` include path

- [ ] **Step 5: Run the lint and CI-equivalent checks locally**

Run: `make lint`

Expected: PASS

Run: `cd frontend && bun install && bun run typecheck && bun run lint && bun run test`

Expected: PASS

Run: `cd packages/ui && bun run typecheck && bun run lint`

Expected: PASS

- [ ] **Step 6: Commit the lint/CI enforcement layer**

```bash
git add .golangci.yml .github/workflows/ci.yml packages/ui/eslint.config.mjs packages/ui/package.json
git commit -m "build: enforce UTC datetime policy in lint and CI"
```

### Task 5: Add E2E Coverage For User-Visible UTC Behavior

**Files:**
- Modify: `frontend/tests/e2e-full/activity-filters.spec.ts`

- [ ] **Step 1: Extend an existing Playwright full-stack test to verify UTC timestamps stay canonical in the API and localize only in the UI**

Exercise a real end-to-end flow that loads activity or detail data from the Go server, then assert both of these conditions:

- the underlying API payload contains RFC3339 UTC strings with trailing `Z`
- the rendered UI shows a localized date label derived from that canonical timestamp rather than echoing the raw API string

Prefer extending an existing activity-focused E2E spec instead of creating a brand-new suite.

- [ ] **Step 2: Run the targeted E2E coverage locally**

Run: `cd frontend && bun run test:e2e --config playwright-e2e.config.ts --grep "activity"`

Expected: PASS

- [ ] **Step 3: Commit the E2E regression coverage**

```bash
git add frontend/tests/e2e-full/activity-filters.spec.ts
git commit -m "test: cover UTC datetime behavior end to end"
```

## Spec Coverage Check

- UTC storage/API rule: covered by Tasks 1 and 2
- frontend presentation-only localization: covered by Task 3
- ADR and CLAUDE changes: already completed before this plan
- tests for DB/API/UI boundaries: covered by Tasks 1, 3, and 5
- lint/CI enforcement: covered by Task 4

## Final Verification

After all tasks are done, run the full project verification before opening a PR:

```bash
make test
make lint
cd frontend && bun run typecheck && bun run lint && bun run test
cd packages/ui && bun run typecheck && bun run lint
cd frontend && bun run test:e2e --config playwright-e2e.config.ts --grep "activity"
```

Expected:

- all Go tests pass
- golangci-lint passes with the new `forbidigo` rules
- frontend typecheck/lint/tests pass
- `packages/ui` typecheck/lint pass
- the targeted Playwright E2E UTC flow passes
