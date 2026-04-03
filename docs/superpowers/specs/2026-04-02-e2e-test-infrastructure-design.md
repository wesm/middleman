# E2E Test Infrastructure with Synthetic Database

**Date:** 2026-04-02
**Status:** Draft

## Goal

Build a reusable synthetic test database and full-stack E2E test suite that catches integration bugs between the frontend UI and backend API -- like the activity feed filter bug where clicking "PRs" still showed issues.

## Approach: Full-Stack E2E with Seeded SQLite

Playwright tests run against the real Go server serving a seeded SQLite database. No API route mocking for activity tests -- the data flows through the real query layer.

### Why full-stack over route mocking

The existing `mockApi.ts` approach intercepts at the HTTP layer, so it can't catch bugs where:
- The frontend sends filter params the backend interprets differently (the exact bug we just hit)
- The SQL query returns unexpected shapes or orderings
- Server-side config filtering (e.g. repo allowlist) interacts with client filters

## Synthetic Test Database

### Fixture Seeder

A Go helper in `internal/testutil/fixtures.go` that populates a DB with realistic data. Called from `cmd/e2e-server/main.go` at startup.

### Data Set

**Repos (3):**

| Owner | Name | Purpose |
|-------|------|---------|
| acme | widgets | Active repo: mix of open/merged/closed PRs and issues |
| acme | tools | Secondary repo: fewer items, validates cross-repo filtering |
| acme | archived | Dormant repo: no recent activity, validates empty states |

**Pull Requests (8-10 across repos):**
- Open PRs: with reviews, comments, commits (widget repo)
- Merged PRs: recent and older
- Closed PR: unmerged
- Draft PR: no reviews
- PR with merge conflict status
- PR by a bot author (e.g. `dependabot[bot]`)

**Issues (5-6 across repos):**
- Open issues: with comments
- Closed issues: recent
- Issue by a bot author

**PR Events:**
- `issue_comment` events on PRs (maps to `activity_type = "comment"`)
- `review` events (APPROVED, CHANGES_REQUESTED)
- `commit` events (multiple consecutive by same author for collapse testing)

**Issue Events:**
- `issue_comment` events on issues (also maps to `activity_type = "comment"`)

**Time Distribution:**
- All timestamps computed relative to `time.Now()` at seed time, not a fixed constant. This prevents time-window tests from rotting as the fixture ages.
- Activity spread across last 90 days, with density in the last 7 days
- Enough items in each time bucket (24h, 7d, 30d, 90d) to validate range filters
- Some items near bucket boundaries

**Key Properties the Data Must Exercise:**
1. Both PRs and issues have comments -- so `activity_type = "comment"` exists with both `item_type = "pr"` and `item_type = "issue"`
2. Bot authors exist on both PRs and issues
3. Merged/closed items exist alongside open ones
4. Multiple consecutive commits by same author on same PR (for collapse logic)
5. Cross-repo activity (items in both acme/widgets and acme/tools)

### Fixture Implementation

```go
// internal/testutil/fixtures.go
package testutil

// SeedTestDB populates a database with synthetic data for E2E tests.
// Timestamps are relative to time.Now() so time-range filters work
// regardless of when the tests run.
func SeedTestDB(d *db.DB) error {
    now := time.Now().UTC()
    // Insert repos, PRs, issues, events using offsets from now
}
```

The seeder calls `db.UpsertRepo`, `db.UpsertPullRequest`, `db.UpsertIssue`, `db.UpsertPREvents`, and `db.UpsertIssueEvents` to populate data. This exercises the storage and query layers but bypasses the sync engine's GitHub API normalization logic -- the goal is to test the API and UI, not the sync path.

### Test Server Binary

A small Go program in `cmd/e2e-server/main.go` that:
1. Creates a temp SQLite DB and calls `SeedTestDB()`
2. Constructs a `config.Config` with the three test repos listed, so `/settings` returns valid data and the server's repo allowlist filtering works
3. Creates a `ghclient.Syncer` with a `NoopClient` (defined in `internal/testutil/noop_client.go` since the existing mock in `api_test.go` is in a `_test.go` file and not importable). This satisfies `/sync/status` without panicking and makes `POST /sync` a safe no-op.
4. Passes the embedded frontend FS (`internal/web`) to `server.New()` so the SPA is served via `go:embed`, matching production
5. Starts the HTTP server on a fixed port (default 4174, configurable via flag)
6. Exits on SIGTERM

The Makefile handles all build-time concerns: `make frontend` populates `internal/web/dist/` before `go build` compiles the binary with `go:embed`. The binary itself has no build responsibilities at runtime.

Settings mutation flows (PUT /settings, add/remove repos) are out of scope for the initial E2E suite. If added later, the server constructor should switch from `server.New()` to `server.NewWithConfig()` with a writable temp config path.

## Playwright Configuration

### New Config: `playwright-e2e.config.ts`

Separate from the existing `playwright.config.ts` (which uses Vite + route mocking). The new config:
- Points `baseURL` at the Go server (port 4174)
- `webServer` starts the e2e-server binary (single lifecycle owner -- Playwright manages start and teardown)
- Test directory: `tests/e2e-full/` (distinct from `tests/e2e/`)

The existing Vite-based E2E tests in `tests/e2e/` remain untouched.

### Makefile Target

```makefile
test-e2e: frontend
	go build -o ./cmd/e2e-server/e2e-server ./cmd/e2e-server
	cd frontend && bun run playwright test --config=playwright-e2e.config.ts
```

The `frontend` prerequisite ensures `internal/web/dist/` is fresh before `go build` compiles the binary with `go:embed`. The binary is written next to its source to avoid creating a separate `bin/` directory.

## Test Cases

### Phase 1: Activity Feed Filters (the pain point)

**File:** `tests/e2e-full/activity-filters.spec.ts`

1. **PR filter shows only PR items** -- click "PRs", verify every visible row has a "PR" badge, no "Issue" badges
2. **Issues filter shows only issue items** -- click "Issues", verify every visible row has an "Issue" badge
3. **All filter shows both** -- click "All", verify both PR and Issue badges present
4. **Event type toggles** -- disable "Comments" in filter dropdown, verify no comment rows; re-enable, verify they return
5. **Hide closed/merged** -- toggle on, verify no "Merged"/"Closed" state badges visible
6. **Hide bots** -- toggle on, verify no bot authors visible
7. **Time range filter** -- switch to "24h", verify fewer items than "7d"
8. **Search filter** -- type a known title substring, verify only matching items shown
9. **Combined filters** -- "PRs" + hide closed/merged + "24h": verify intersection

### Phase 2: PR and Issue Lists

**File:** `tests/e2e-full/pull-list.spec.ts`

1. PR list renders expected count and titles
2. Repo filter narrows results
3. State filter works (open vs closed, where closed includes merged)
4. Search filters by title

**File:** `tests/e2e-full/issue-list.spec.ts`

1. Issue list renders expected count and titles
2. State filter works (open vs closed)
3. Search filters by title

### Phase 3: Navigation and Detail Views

**File:** `tests/e2e-full/navigation.spec.ts`

1. Clicking a PR row navigates to detail view
2. Clicking an issue row navigates to detail view
3. Sidebar navigation between views (PRs, Issues, Activity)
4. Back navigation works

## File Structure

```
cmd/e2e-server/main.go              -- test server binary
internal/testutil/fixtures.go        -- fixture seeder
internal/testutil/fixtures_test.go   -- verify fixtures produce expected data
internal/testutil/noop_client.go     -- no-op ghclient.Client for e2e server
frontend/playwright-e2e.config.ts    -- full-stack Playwright config
frontend/tests/e2e-full/
  activity-filters.spec.ts           -- Phase 1
  pull-list.spec.ts                  -- Phase 2
  issue-list.spec.ts                 -- Phase 2
  navigation.spec.ts                 -- Phase 3
```

## What This Does NOT Cover

- Frontend component unit tests (separate effort)
- Frontend store unit tests (separate effort)
- Performance/load testing
- Visual regression testing
- Demo mode CLI command (future project)
