# GraphQL Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-PR REST read calls with bulk GraphQL queries to reduce GitHub API rate limit usage by orders of magnitude.

**Architecture:** New `GraphQLFetcher` in `internal/github/graphql.go` sends one query per repo page, returning all open PRs with nested comments, reviews, commits, and CI status. Adapter functions map GraphQL response types to existing go-github types so normalize/DB layers stay unchanged. `RateTracker` gains a per-instance `apiType` field (set at construction) so REST and GraphQL rate budgets are tracked independently without changing any public method signatures.

**Tech Stack:** `shurcooL/githubv4` (already in go.mod), existing `go-github/v84` types as adapter targets, SQLite table recreation for rate limit schema change.

**Spec:** `docs/superpowers/specs/2026-04-09-graphql-sync-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/github/graphql.go` | **New.** `GraphQLFetcher` struct, `FetchRepoPRs`, GraphQL query structs, adapter functions, `graphqlRateTransport` |
| `internal/github/graphql_pagination.go` | **New.** Generic `fetchAllPages[T]` helper for cursor-based GraphQL pagination |
| `internal/github/graphql_pagination_test.go` | **New.** Pagination helper unit tests |
| `internal/github/graphql_test.go` | **New.** Adapter mapping tests, fetcher integration tests |
| `internal/github/rate.go` | **Modify.** Add `apiType string` field to `RateTracker` struct; add param to `NewRateTracker`; update `hydrate`/`persist` to include `apiType` in DB calls. All public method signatures unchanged. |
| `internal/github/rate_test.go` | **Modify.** Update all `NewRateTracker` calls to pass `"rest"` as third arg |
| `internal/github/sync.go` | **Modify.** Add `fetchers` field to `Syncer`; `SetFetchers`, `fetcherFor`, `doSyncRepoGraphQL`, `syncOpenMRFromBulk` methods; wire GraphQL path into `indexSyncRepo` 200 branch |
| `internal/db/db.go` | **Modify.** `SchemaVersion` 3→4; add `case version == 3` migration handler |
| `internal/db/schema.sql` | **Modify.** `rate_limits` table: add `api_type` column, change UNIQUE constraint |
| `internal/db/queries.go` | **Modify.** `UpsertRateLimit` and `GetRateLimit` gain `apiType` parameter |
| `internal/db/types.go` | **Modify.** `RateLimit` struct gains `APIType string` field |
| `internal/db/queries_test.go` | **Modify.** Update `TestRateLimitCRUD` for `apiType` param |
| `cmd/middleman/main.go` | **Modify.** `NewRateTracker` calls pass `"rest"`; create GraphQL fetchers per host; wire to syncer |

---

### Task 1: DB Schema + Migration

Add `api_type` column to `rate_limits` table, change UNIQUE constraint from `(platform_host)` to `(platform_host, api_type)`, bump SchemaVersion to 4 with migration handler.

**Files:**
- Modify: `internal/db/schema.sql`
- Modify: `internal/db/db.go`
- Modify: `internal/db/types.go`
- Modify: `internal/db/queries.go`
- Modify: `internal/db/queries_test.go`

- [ ] **Step 1: Write the failing test**

Add a test for the new `apiType` parameter in `internal/db/queries_test.go`. Replace `TestRateLimitCRUD`:

```go
func TestRateLimitCRUD(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	host := "github.com"
	hourStart := baseTime()
	resetAt := hourStart.Add(30 * time.Minute)

	// Insert REST
	require.NoError(d.UpsertRateLimit(host, "rest", 5, hourStart, 4995, -1, &resetAt))

	got, err := d.GetRateLimit(host, "rest")
	require.NoError(err)
	require.NotNil(got)
	assert.Equal(host, got.PlatformHost)
	assert.Equal("rest", got.APIType)
	assert.Equal(5, got.RequestsHour)
	assert.True(got.HourStart.Equal(hourStart))
	assert.Equal(4995, got.RateRemaining)
	require.NotNil(got.RateResetAt)
	assert.True(got.RateResetAt.Equal(resetAt))

	// Insert GraphQL for same host — separate row
	require.NoError(d.UpsertRateLimit(host, "graphql", 2, hourStart, 4998, 5000, nil))

	gql, err := d.GetRateLimit(host, "graphql")
	require.NoError(err)
	require.NotNil(gql)
	assert.Equal("graphql", gql.APIType)
	assert.Equal(2, gql.RequestsHour)
	assert.Equal(4998, gql.RateRemaining)

	// REST row unchanged
	rest, err := d.GetRateLimit(host, "rest")
	require.NoError(err)
	require.NotNil(rest)
	assert.Equal(5, rest.RequestsHour)

	// Update via upsert
	laterStart := hourStart.Add(time.Hour)
	require.NoError(d.UpsertRateLimit(host, "rest", 10, laterStart, 4990, -1, nil))

	got2, err := d.GetRateLimit(host, "rest")
	require.NoError(err)
	require.NotNil(got2)
	assert.Equal(10, got2.RequestsHour)
	assert.True(got2.HourStart.Equal(laterStart))
	assert.Equal(4990, got2.RateRemaining)
	assert.Nil(got2.RateResetAt)

	// Not found
	missing, err := d.GetRateLimit("no.such.host", "rest")
	require.NoError(err)
	assert.Nil(missing)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/db/ -run TestRateLimitCRUD -v`
Expected: compile error — `UpsertRateLimit` and `GetRateLimit` don't accept `apiType` yet.

- [ ] **Step 3: Update schema.sql for fresh databases**

In `internal/db/schema.sql`, replace the `middleman_rate_limits` table definition (lines 149-158):

```sql
CREATE TABLE IF NOT EXISTS middleman_rate_limits (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    platform_host  TEXT NOT NULL,
    api_type       TEXT NOT NULL DEFAULT 'rest',
    requests_hour  INTEGER NOT NULL DEFAULT 0,
    hour_start     DATETIME NOT NULL,
    rate_remaining INTEGER NOT NULL DEFAULT -1,
    rate_limit     INTEGER NOT NULL DEFAULT -1,
    rate_reset_at  DATETIME,
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(platform_host, api_type)
);
```

- [ ] **Step 4: Add APIType field to RateLimit struct**

In `internal/db/types.go`, add `APIType` to the `RateLimit` struct after `PlatformHost`:

```go
type RateLimit struct {
	ID            int64
	PlatformHost  string
	APIType       string
	RequestsHour  int
	HourStart     time.Time
	RateRemaining int
	RateLimit     int
	RateResetAt   *time.Time
	UpdatedAt     time.Time
}
```

- [ ] **Step 5: Update UpsertRateLimit to accept apiType**

In `internal/db/queries.go`, replace `UpsertRateLimit` (line 1196):

```go
func (d *DB) UpsertRateLimit(
	platformHost string,
	apiType string,
	requestsHour int,
	hourStart time.Time,
	rateRemaining int,
	rateLimit int,
	rateResetAt *time.Time,
) error {
	_, err := d.rw.Exec(`
		INSERT INTO middleman_rate_limits
		    (platform_host, api_type, requests_hour, hour_start,
		     rate_remaining, rate_limit, rate_reset_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(platform_host, api_type) DO UPDATE SET
		    requests_hour  = excluded.requests_hour,
		    hour_start     = excluded.hour_start,
		    rate_remaining = excluded.rate_remaining,
		    rate_limit     = excluded.rate_limit,
		    rate_reset_at  = excluded.rate_reset_at,
		    updated_at     = datetime('now')`,
		platformHost, apiType, requestsHour, hourStart,
		rateRemaining, rateLimit, rateResetAt,
	)
	if err != nil {
		return fmt.Errorf("upsert rate limit: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Update GetRateLimit to accept apiType**

In `internal/db/queries.go`, replace `GetRateLimit` (line 1227):

```go
func (d *DB) GetRateLimit(
	platformHost string,
	apiType string,
) (*RateLimit, error) {
	var r RateLimit
	err := d.ro.QueryRow(`
		SELECT id, platform_host, api_type, requests_hour, hour_start,
		       rate_remaining, rate_limit, rate_reset_at, updated_at
		FROM middleman_rate_limits
		WHERE platform_host = ? AND api_type = ?`,
		platformHost, apiType,
	).Scan(
		&r.ID, &r.PlatformHost, &r.APIType, &r.RequestsHour, &r.HourStart,
		&r.RateRemaining, &r.RateLimit, &r.RateResetAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rate limit: %w", err)
	}
	return &r, nil
}
```

- [ ] **Step 7: Bump SchemaVersion and add migration**

In `internal/db/db.go`, change `SchemaVersion` to 4 and add migration handler.

Change line 22:
```go
const SchemaVersion = 4
```

In the `init()` method, add a `case version == 3:` handler before the `default:` case (after `case version == SchemaVersion:`, before `case version > SchemaVersion:`):

```go
	case version == 3:
		if err := d.migrateV3ToV4(); err != nil {
			return fmt.Errorf("migrate v3→v4: %w", err)
		}
		d.writeSchemaVersion(SchemaVersion)
```

Add the migration method after `writeSchemaVersion`:

```go
// migrateV3ToV4 adds the api_type column to middleman_rate_limits
// and changes the UNIQUE constraint from (platform_host) to
// (platform_host, api_type). SQLite cannot ALTER a UNIQUE
// constraint, so the table is recreated.
func (d *DB) migrateV3ToV4() error {
	migration := `
		CREATE TABLE middleman_rate_limits_new (
		    id             INTEGER PRIMARY KEY AUTOINCREMENT,
		    platform_host  TEXT NOT NULL,
		    api_type       TEXT NOT NULL DEFAULT 'rest',
		    requests_hour  INTEGER NOT NULL DEFAULT 0,
		    hour_start     DATETIME NOT NULL,
		    rate_remaining INTEGER NOT NULL DEFAULT -1,
		    rate_limit     INTEGER NOT NULL DEFAULT -1,
		    rate_reset_at  DATETIME,
		    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
		    UNIQUE(platform_host, api_type)
		);
		INSERT INTO middleman_rate_limits_new
		    (id, platform_host, api_type, requests_hour, hour_start,
		     rate_remaining, rate_limit, rate_reset_at, updated_at)
		SELECT id, platform_host, 'rest', requests_hour, hour_start,
		       rate_remaining, rate_limit, rate_reset_at, updated_at
		FROM middleman_rate_limits;
		DROP TABLE middleman_rate_limits;
		ALTER TABLE middleman_rate_limits_new
		    RENAME TO middleman_rate_limits;
	`
	_, err := d.rw.Exec(migration)
	return err
}
```

- [ ] **Step 8: Run tests**

Run: `nix shell nixpkgs#go --command go test ./internal/db/ -v`
Expected: all pass including `TestRateLimitCRUD` and `TestOpenAndSchema`.

- [ ] **Step 9: Fix compile errors in callers**

The `RateTracker.hydrate()` and `RateTracker.persist()` methods call the old signatures. They will fail to compile. Update them in the **next task** (Task 2). For now, verify DB tests pass by temporarily updating the calls in `internal/github/rate.go`:

In `hydrate()` (line 58), change:
```go
row, err := rt.db.GetRateLimit(rt.platformHost)
```
to:
```go
row, err := rt.db.GetRateLimit(rt.platformHost, "rest")
```

In `persist()` (line 265), change:
```go
err := rt.db.UpsertRateLimit(
	rt.platformHost,
	rt.count,
```
to:
```go
err := rt.db.UpsertRateLimit(
	rt.platformHost,
	"rest",
	rt.count,
```

These are temporary — Task 2 replaces `"rest"` with `rt.apiType`.

- [ ] **Step 10: Run full test suite**

Run: `nix shell nixpkgs#go --command go test ./internal/... -v`
Expected: all pass.

- [ ] **Step 11: Commit**

```bash
git add internal/db/schema.sql internal/db/db.go internal/db/types.go internal/db/queries.go internal/db/queries_test.go internal/github/rate.go
git commit -m "feat: add api_type to rate_limits table, schema v3→v4 migration"
```

---

### Task 2: RateTracker Per-Instance apiType

Add `apiType string` field to `RateTracker` struct and `NewRateTracker` constructor. Update `hydrate()` and `persist()` to use the instance's `apiType`. All public method signatures stay unchanged — zero caller changes in `sync.go` or `client.go`.

**Files:**
- Modify: `internal/github/rate.go`
- Modify: `internal/github/rate_test.go`
- Modify: `cmd/middleman/main.go`

- [ ] **Step 1: Write the failing test**

In `internal/github/rate_test.go`, add a test that verifies REST and GraphQL trackers are independent:

```go
func TestRateTrackerAPITypeIsolation(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	restRT := NewRateTracker(d, "github.com", "rest")
	gqlRT := NewRateTracker(d, "github.com", "graphql")

	// Record requests on each independently
	for range 5 {
		restRT.RecordRequest()
	}
	for range 3 {
		gqlRT.RecordRequest()
	}

	assert.Equal(5, restRT.RequestsThisHour())
	assert.Equal(3, gqlRT.RequestsThisHour())

	// Verify DB isolation
	restRow, err := d.GetRateLimit("github.com", "rest")
	require.NoError(err)
	require.NotNil(restRow)
	assert.Equal(5, restRow.RequestsHour)

	gqlRow, err := d.GetRateLimit("github.com", "graphql")
	require.NoError(err)
	require.NotNil(gqlRow)
	assert.Equal(3, gqlRow.RequestsHour)

	// Hydrate new trackers — they pick up correct state
	restRT2 := NewRateTracker(d, "github.com", "rest")
	gqlRT2 := NewRateTracker(d, "github.com", "graphql")
	assert.Equal(5, restRT2.RequestsThisHour())
	assert.Equal(3, gqlRT2.RequestsThisHour())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestRateTrackerAPITypeIsolation -v`
Expected: compile error — `NewRateTracker` doesn't accept `apiType` yet.

- [ ] **Step 3: Add apiType to RateTracker**

In `internal/github/rate.go`, add `apiType` field to the struct (after `platformHost`):

```go
type RateTracker struct {
	mu            sync.Mutex
	db            *db.DB
	platformHost  string
	apiType       string
	count         int
	hourStart     time.Time
	remaining     int
	limit         int
	resetAt       *time.Time
	lastRolledAt  time.Time
	onWindowReset func()
}
```

Update `NewRateTracker` (line 43) to accept `apiType`:

```go
func NewRateTracker(
	database *db.DB, platformHost string, apiType string,
) *RateTracker {
	rt := &RateTracker{
		db:           database,
		platformHost: platformHost,
		apiType:      apiType,
		remaining:    -1,
		limit:        -1,
		hourStart:    truncateHour(time.Now().UTC()),
	}
	rt.hydrate()
	return rt
}
```

Update `hydrate()` to use `rt.apiType`:

```go
func (rt *RateTracker) hydrate() {
	row, err := rt.db.GetRateLimit(rt.platformHost, rt.apiType)
```

Update `persist()` to use `rt.apiType`:

```go
func (rt *RateTracker) persist() {
	err := rt.db.UpsertRateLimit(
		rt.platformHost,
		rt.apiType,
		rt.count,
		rt.hourStart,
		rt.remaining,
		rt.limit,
		rt.resetAt,
	)
```

- [ ] **Step 4: Update all existing tests**

In `internal/github/rate_test.go`, update every `NewRateTracker(d, "github.com")` call to `NewRateTracker(d, "github.com", "rest")`. There are 11 tests — all use `NewRateTracker(d, "github.com")`:

- `TestRateTrackerCounting` (line 18)
- `TestRateTrackerBackoff` (line 41)
- `TestRateTrackerHourRollover` (line 89)
- `TestRateTrackerConcurrentAccess` (line 108)
- `TestRateTrackerThrottleFactor` (line 126)
- `TestRateTrackerStaleQuota` (line 181)
- `TestRateTrackerHydrateFromDB` (lines 199, 210)
- `TestRateTrackerWindowRolloverResetsQuota` (line 222)
- `TestRateTrackerWindowResetResetsCounter` (line 251)
- `TestRateTrackerResetAtJitterDoesNotResetCounter` (line 291)
- `TestRateTrackerProductionFlow` (line 324)

Use find-and-replace: change `NewRateTracker(d, "github.com")` → `NewRateTracker(d, "github.com", "rest")` across the file.

- [ ] **Step 5: Update main.go**

In `cmd/middleman/main.go`, update the `NewRateTracker` call (line 118):

```go
		rateTrackers[host] = ghclient.NewRateTracker(
			database, host, "rest",
		)
```

- [ ] **Step 6: Run all tests**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ ./internal/db/ -v`
Expected: all pass, including `TestRateTrackerAPITypeIsolation`.

- [ ] **Step 7: Build to verify no compile errors**

Run: `nix shell nixpkgs#go --command go build ./cmd/middleman/`
Expected: clean build.

- [ ] **Step 8: Commit**

```bash
git add internal/github/rate.go internal/github/rate_test.go cmd/middleman/main.go
git commit -m "feat: add apiType to RateTracker for independent REST/GraphQL tracking"
```

---

### Task 3: Pagination Helper

Generic cursor-based pagination helper for `shurcooL/githubv4` query loops.

**Files:**
- Create: `internal/github/graphql_pagination.go`
- Create: `internal/github/graphql_pagination_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/github/graphql_pagination_test.go`:

```go
package github

import (
	"context"
	"fmt"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchAllPagesSinglePage(t *testing.T) {
	assert := Assert.New(t)

	items, err := fetchAllPages(
		context.Background(),
		func(_ context.Context, cursor *string) ([]int, pageInfo, error) {
			assert.Nil(cursor)
			return []int{1, 2, 3}, pageInfo{HasNextPage: false}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal([]int{1, 2, 3}, items)
}

func TestFetchAllPagesMultiPage(t *testing.T) {
	assert := Assert.New(t)
	calls := 0

	items, err := fetchAllPages(
		context.Background(),
		func(_ context.Context, cursor *string) ([]string, pageInfo, error) {
			calls++
			switch calls {
			case 1:
				assert.Nil(cursor)
				return []string{"a", "b"}, pageInfo{
					HasNextPage: true,
					EndCursor:   "cursor1",
				}, nil
			case 2:
				require.NotNil(t, cursor)
				assert.Equal("cursor1", *cursor)
				return []string{"c"}, pageInfo{
					HasNextPage: false,
				}, nil
			default:
				t.Fatal("too many calls")
				return nil, pageInfo{}, nil
			}
		},
	)
	require.NoError(t, err)
	assert.Equal([]string{"a", "b", "c"}, items)
	assert.Equal(2, calls)
}

func TestFetchAllPagesError(t *testing.T) {
	assert := Assert.New(t)

	// Test error on first page
	_, err := fetchAllPages(
		context.Background(),
		func(_ context.Context, cursor *string) ([]int, pageInfo, error) {
			return nil, pageInfo{}, fmt.Errorf("graphql: rate limited")
		},
	)
	assert.Error(err)
	assert.Contains(err.Error(), "rate limited")
}

func TestFetchAllPagesContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchAllPages(
		ctx,
		func(ctx context.Context, cursor *string) ([]int, pageInfo, error) {
			return nil, pageInfo{}, ctx.Err()
		},
	)
	Assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestFetchAllPages -v`
Expected: compile error — `fetchAllPages` and `pageInfo` not defined.

- [ ] **Step 3: Implement pagination helper**

Create `internal/github/graphql_pagination.go`:

```go
package github

import "context"

// pageInfo holds GraphQL pagination state from a connection's
// pageInfo field.
type pageInfo struct {
	HasNextPage bool
	EndCursor   string
}

// fetchAllPages accumulates all nodes from a paginated GraphQL
// connection. queryFn is called with a nil cursor for the first
// page and the previous endCursor for subsequent pages. Returns
// all accumulated nodes, or partial results plus the first error.
func fetchAllPages[T any](
	ctx context.Context,
	queryFn func(ctx context.Context, cursor *string) ([]T, pageInfo, error),
) ([]T, error) {
	var all []T
	var cursor *string
	for {
		nodes, pi, err := queryFn(ctx, cursor)
		if err != nil {
			return all, err
		}
		all = append(all, nodes...)
		if !pi.HasNextPage {
			break
		}
		c := pi.EndCursor
		cursor = &c
	}
	return all, nil
}
```

- [ ] **Step 4: Run tests**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestFetchAllPages -v`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/github/graphql_pagination.go internal/github/graphql_pagination_test.go
git commit -m "feat: add generic GraphQL pagination helper"
```

---

### Task 4: GraphQL Types, Adapters, and Fetcher

GraphQL query struct types, adapter functions mapping GraphQL responses to go-github types, `GraphQLFetcher` struct with `FetchRepoPRs`, and `graphqlRateTransport` for capturing rate limit headers.

**Files:**
- Create: `internal/github/graphql.go`
- Create: `internal/github/graphql_test.go`

- [ ] **Step 1: Write adapter tests**

Create `internal/github/graphql_test.go` with tests for the adapter functions. These verify pure data mapping — no HTTP:

```go
package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptPR(t *testing.T) {
	assert := Assert.New(t)

	now := time.Now().UTC().Truncate(time.Second)
	merged := now.Add(-time.Hour)

	gql := gqlPR{
		DatabaseId:     12345,
		Number:         42,
		Title:          "Fix bug",
		State:          "OPEN",
		IsDraft:        true,
		Body:           "Fixes #1",
		URL:            "https://github.com/o/r/pull/42",
		Additions:      10,
		Deletions:      3,
		Mergeable:      "MERGEABLE",
		ReviewDecision: "APPROVED",
		HeadRefName:    "fix-branch",
		BaseRefName:    "main",
		HeadRefOid:     "abc123",
		BaseRefOid:     "def456",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	gql.Author.Login = "alice"
	gql.MergedAt = &merged
	gql.HeadRepository = &struct{ URL string }{URL: "https://github.com/o/r.git"}

	pr := adaptPR(&gql)

	assert.Equal(int64(12345), pr.GetID())
	assert.Equal(42, pr.GetNumber())
	assert.Equal("Fix bug", pr.GetTitle())
	assert.Equal("open", pr.GetState())
	assert.True(pr.GetDraft())
	assert.Equal("Fixes #1", pr.GetBody())
	assert.Equal("https://github.com/o/r/pull/42", pr.GetHTMLURL())
	assert.Equal(10, pr.GetAdditions())
	assert.Equal(3, pr.GetDeletions())
	assert.Equal("alice", pr.GetUser().GetLogin())
	assert.Equal("fix-branch", pr.GetHead().GetRef())
	assert.Equal("main", pr.GetBase().GetRef())
	assert.Equal("abc123", pr.GetHead().GetSHA())
	assert.Equal("def456", pr.GetBase().GetSHA())
	assert.Equal("https://github.com/o/r.git", pr.GetHead().GetRepo().GetCloneURL())
	assert.Equal("mergeable", pr.GetMergeableState())
	require.NotNil(t, pr.MergedAt)
	assert.True(pr.GetMerged())
}

func TestAdaptComment(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)

	gql := gqlComment{
		DatabaseId: 100,
		Body:       "LGTM",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	gql.Author.Login = "bob"

	c := adaptComment(&gql)

	assert.Equal(int64(100), c.GetID())
	assert.Equal("LGTM", c.GetBody())
	assert.Equal("bob", c.GetUser().GetLogin())
}

func TestAdaptReview(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)

	gql := gqlReview{
		DatabaseId:  200,
		Body:        "Looks good",
		State:       "APPROVED",
		SubmittedAt: now,
	}
	gql.Author.Login = "carol"

	r := adaptReview(&gql)

	assert.Equal(int64(200), r.GetID())
	assert.Equal("Looks good", r.GetBody())
	assert.Equal("APPROVED", r.GetState())
	assert.Equal("carol", r.GetUser().GetLogin())
}

func TestAdaptCommit(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)

	gql := gqlCommitNode{
		Commit: gqlCommit{
			OID:     "sha123",
			Message: "fix: something",
		},
	}
	gql.Commit.Author.Name = "Dave"
	gql.Commit.Author.Date = now
	gql.Commit.Author.User = &struct{ Login string }{Login: "dave"}

	c := adaptCommit(&gql)

	assert.Equal("sha123", c.GetSHA())
	assert.Equal("fix: something", c.GetCommit().GetMessage())
	assert.Equal("Dave", c.GetCommit().GetAuthor().GetName())
	assert.Equal("dave", c.GetAuthor().GetLogin())
}

func TestAdaptCheckContext(t *testing.T) {
	assert := Assert.New(t)

	contexts := []gqlCheckContext{
		{
			Typename: "CheckRun",
			CheckRun: gqlCheckRunFields{
				Name:       "ci/test",
				Status:     "COMPLETED",
				Conclusion: "SUCCESS",
				DetailsURL: "https://example.com/1",
			},
		},
		{
			Typename: "StatusContext",
			StatusContext: gqlStatusContextFields{
				Context:   "ci/lint",
				State:     "SUCCESS",
				TargetURL: "https://example.com/2",
			},
		},
	}
	contexts[0].CheckRun.CheckSuite.App.Name = "GitHub Actions"

	checks, statuses := splitCheckContexts(contexts)

	require.Equal(t, 1, len(checks))
	assert.Equal("ci/test", checks[0].GetName())
	assert.Equal("completed", checks[0].GetStatus())
	assert.Equal("success", checks[0].GetConclusion())
	assert.Equal("GitHub Actions", checks[0].GetApp().GetName())

	require.Equal(t, 1, len(statuses))
	assert.Equal("ci/lint", statuses[0].GetContext())
	assert.Equal("success", statuses[0].GetState())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestAdapt -v`
Expected: compile error — types and adapter functions not defined.

- [ ] **Step 3: Implement GraphQL types**

Create `internal/github/graphql.go` with the query types, adapter functions, and fetcher. This is the largest new file:

```go
package github

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// topLevelPageSize is the number of PRs fetched per GraphQL
// query page. Kept conservative to stay under GitHub's 500k
// node limit even with nested connections.
const topLevelPageSize = 25

// retryPageSize is used when the initial query fails (e.g.,
// complexity/node limit error). Half the default, minimum 5.
const retryPageSize = 12

// --- GraphQL query types (private) ---

// gqlPRQuery is the top-level query for fetching open PRs.
type gqlPRQuery struct {
	Repository struct {
		PullRequests struct {
			Nodes    []gqlPR
			PageInfo pageInfo
		} `graphql:"pullRequests(first: $pageSize, states: OPEN, after: $cursor)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

type gqlPR struct {
	DatabaseId     int64  `graphql:"databaseId"`
	Number         int
	Title          string
	State          string // OPEN, CLOSED, MERGED
	IsDraft        bool
	Body           string
	URL            string
	Author         struct{ Login string }
	CreatedAt      time.Time
	UpdatedAt      time.Time
	MergedAt       *time.Time
	ClosedAt       *time.Time
	Additions      int
	Deletions      int
	Mergeable      string // MERGEABLE, CONFLICTING, UNKNOWN
	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	HeadRefName    string
	BaseRefName    string
	HeadRefOid     string `graphql:"headRefOid"`
	BaseRefOid     string `graphql:"baseRefOid"`
	HeadRepository *struct {
		URL string
	}
	Comments struct {
		Nodes    []gqlComment
		PageInfo pageInfo
	} `graphql:"comments(first: 100)"`
	Reviews struct {
		Nodes    []gqlReview
		PageInfo pageInfo
	} `graphql:"reviews(first: 100)"`
	AllCommits struct {
		Nodes    []gqlCommitNode
		PageInfo pageInfo
	} `graphql:"allCommits: commits(first: 100)"`
	LastCommit struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					Contexts struct {
						Nodes    []gqlCheckContext
						PageInfo pageInfo
					} `graphql:"contexts(first: 100)"`
				}
			}
		}
	} `graphql:"lastCommit: commits(last: 1)"`
}

type gqlComment struct {
	DatabaseId int64
	Author     struct{ Login string }
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type gqlReview struct {
	DatabaseId  int64
	Author      struct{ Login string }
	Body        string
	State       string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, PENDING
	SubmittedAt time.Time
}

type gqlCommitNode struct {
	Commit gqlCommit
}

type gqlCommit struct {
	OID     string `graphql:"oid"`
	Message string
	Author  struct {
		Name string
		Date time.Time
		User *struct{ Login string }
	}
}

type gqlCheckContext struct {
	Typename      string                `graphql:"__typename"`
	CheckRun      gqlCheckRunFields     `graphql:"... on CheckRun"`
	StatusContext gqlStatusContextFields `graphql:"... on StatusContext"`
}

type gqlCheckRunFields struct {
	Name       string
	Status     string // QUEUED, IN_PROGRESS, COMPLETED, etc.
	Conclusion string // SUCCESS, FAILURE, NEUTRAL, etc.
	DetailsURL string `graphql:"detailsUrl"`
	CheckSuite struct {
		App struct {
			Name string
		}
	}
}

type gqlStatusContextFields struct {
	Context   string
	State     string // EXPECTED, ERROR, FAILURE, PENDING, SUCCESS
	TargetURL string `graphql:"targetUrl"`
}

// --- Adapter functions ---

// adaptPR converts a GraphQL PR to a go-github PullRequest.
func adaptPR(gql *gqlPR) *gh.PullRequest {
	state := stateToREST(gql.State)
	pr := &gh.PullRequest{
		ID:        gh.Ptr(gql.DatabaseId),
		Number:    gh.Ptr(gql.Number),
		Title:     gh.Ptr(gql.Title),
		State:     gh.Ptr(state),
		Draft:     gh.Ptr(gql.IsDraft),
		Body:      gh.Ptr(gql.Body),
		HTMLURL:   gh.Ptr(gql.URL),
		Additions: gh.Ptr(gql.Additions),
		Deletions: gh.Ptr(gql.Deletions),
		User:      &gh.User{Login: gh.Ptr(gql.Author.Login)},
		Head: &gh.PullRequestBranch{
			Ref: gh.Ptr(gql.HeadRefName),
			SHA: gh.Ptr(gql.HeadRefOid),
		},
		Base: &gh.PullRequestBranch{
			Ref: gh.Ptr(gql.BaseRefName),
			SHA: gh.Ptr(gql.BaseRefOid),
		},
		MergeableState: gh.Ptr(mergeableToREST(gql.Mergeable)),
	}

	created := gh.Timestamp{Time: gql.CreatedAt}
	updated := gh.Timestamp{Time: gql.UpdatedAt}
	pr.CreatedAt = &created
	pr.UpdatedAt = &updated

	if gql.MergedAt != nil {
		t := gh.Timestamp{Time: *gql.MergedAt}
		pr.MergedAt = &t
		pr.Merged = gh.Ptr(true)
	}
	if gql.ClosedAt != nil {
		t := gh.Timestamp{Time: *gql.ClosedAt}
		pr.ClosedAt = &t
	}
	if gql.HeadRepository != nil {
		pr.Head.Repo = &gh.Repository{
			CloneURL: gh.Ptr(gql.HeadRepository.URL),
		}
	}

	return pr
}

func stateToREST(graphqlState string) string {
	switch graphqlState {
	case "MERGED":
		return "closed" // NormalizePR handles merged detection via MergedAt
	case "CLOSED":
		return "closed"
	default:
		return "open"
	}
}

func mergeableToREST(mergeable string) string {
	switch mergeable {
	case "MERGEABLE":
		return "clean"
	case "CONFLICTING":
		return "dirty"
	default:
		return "unknown"
	}
}

func adaptComment(gql *gqlComment) *gh.IssueComment {
	created := gh.Timestamp{Time: gql.CreatedAt}
	updated := gh.Timestamp{Time: gql.UpdatedAt}
	return &gh.IssueComment{
		ID:        gh.Ptr(gql.DatabaseId),
		Body:      gh.Ptr(gql.Body),
		User:      &gh.User{Login: gh.Ptr(gql.Author.Login)},
		CreatedAt: &created,
		UpdatedAt: &updated,
	}
}

func adaptReview(gql *gqlReview) *gh.PullRequestReview {
	submitted := gh.Timestamp{Time: gql.SubmittedAt}
	return &gh.PullRequestReview{
		ID:          gh.Ptr(gql.DatabaseId),
		Body:        gh.Ptr(gql.Body),
		State:       gh.Ptr(gql.State),
		User:        &gh.User{Login: gh.Ptr(gql.Author.Login)},
		SubmittedAt: &submitted,
	}
}

func adaptCommit(gql *gqlCommitNode) *gh.RepositoryCommit {
	c := &gh.RepositoryCommit{
		SHA: gh.Ptr(gql.Commit.OID),
		Commit: &gh.Commit{
			Message: gh.Ptr(gql.Commit.Message),
			Author: &gh.CommitAuthor{
				Name: gh.Ptr(gql.Commit.Author.Name),
				Date: &gh.Timestamp{Time: gql.Commit.Author.Date},
			},
		},
	}
	if gql.Commit.Author.User != nil {
		c.Author = &gh.User{Login: gh.Ptr(gql.Commit.Author.User.Login)}
	}
	return c
}

// splitCheckContexts separates statusCheckRollup contexts into
// CheckRun and RepoStatus (StatusContext) slices.
func splitCheckContexts(contexts []gqlCheckContext) ([]*gh.CheckRun, []*gh.RepoStatus) {
	var checks []*gh.CheckRun
	var statuses []*gh.RepoStatus
	for i := range contexts {
		c := &contexts[i]
		switch c.Typename {
		case "CheckRun":
			checks = append(checks, adaptCheckRun(&c.CheckRun))
		case "StatusContext":
			statuses = append(statuses, adaptStatusContext(&c.StatusContext))
		}
	}
	return checks, statuses
}

func adaptCheckRun(gql *gqlCheckRunFields) *gh.CheckRun {
	return &gh.CheckRun{
		Name:       gh.Ptr(gql.Name),
		Status:     gh.Ptr(toLower(gql.Status)),
		Conclusion: gh.Ptr(toLower(gql.Conclusion)),
		DetailsURL: gh.Ptr(gql.DetailsURL),
		App:        &gh.App{Name: gh.Ptr(gql.CheckSuite.App.Name)},
	}
}

func adaptStatusContext(gql *gqlStatusContextFields) *gh.RepoStatus {
	return &gh.RepoStatus{
		Context:   gh.Ptr(gql.Context),
		State:     gh.Ptr(toLower(gql.State)),
		TargetURL: gh.Ptr(gql.TargetURL),
	}
}

func toLower(s string) string {
	// GraphQL returns UPPER_CASE; REST uses lower_case.
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// --- Bulk result types ---

// RepoBulkResult holds all open PRs fetched via GraphQL for a repo.
type RepoBulkResult struct {
	PullRequests []BulkPR
}

// BulkPR holds a PR and its nested data from GraphQL. The
// *Complete flags indicate whether each nested connection was
// fully paginated. When false, the data is partial and the
// detail drain should fill in via REST.
type BulkPR struct {
	PR               *gh.PullRequest
	Comments         []*gh.IssueComment
	Reviews          []*gh.PullRequestReview
	Commits          []*gh.RepositoryCommit
	CheckRuns        []*gh.CheckRun
	Statuses         []*gh.RepoStatus
	CommentsComplete bool
	ReviewsComplete  bool
	CommitsComplete  bool
	CIComplete       bool
}

// --- GraphQL rate transport ---

// graphqlRateTransport wraps an http.RoundTripper to record
// API requests and capture rate limit headers from GraphQL
// responses.
type graphqlRateTransport struct {
	base        http.RoundTripper
	rateTracker *RateTracker
}

func (t *graphqlRateTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if t.rateTracker != nil {
		t.rateTracker.RecordRequest()
		if rate := parseRateLimitHeaders(resp); rate.Limit > 0 {
			t.rateTracker.UpdateFromRate(rate)
		}
	}
	return resp, err
}

func parseRateLimitHeaders(resp *http.Response) gh.Rate {
	var rate gh.Rate
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		rate.Remaining, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("X-RateLimit-Limit"); v != "" {
		rate.Limit, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		epoch, _ := strconv.ParseInt(v, 10, 64)
		rate.Reset = gh.Timestamp{Time: time.Unix(epoch, 0)}
	}
	return rate
}

// --- GraphQLFetcher ---

// GraphQLFetcher wraps a shurcooL/githubv4 client for bulk PR
// data fetching.
type GraphQLFetcher struct {
	client      *githubv4.Client
	rateTracker *RateTracker
}

// NewGraphQLFetcher creates a fetcher for the given host. The
// rateTracker should be a "graphql"-type tracker. budget may
// be nil (disables budget tracking).
func NewGraphQLFetcher(
	token string,
	platformHost string,
	rateTracker *RateTracker,
	budget *SyncBudget,
) *GraphQLFetcher {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(context.Background(), ts)

	var base http.RoundTripper = tc.Transport
	if rateTracker != nil {
		base = &graphqlRateTransport{
			base:        base,
			rateTracker: rateTracker,
		}
	}
	if budget != nil {
		tc.Transport = &budgetTransport{
			base:   base,
			budget: budget,
		}
	} else {
		tc.Transport = base
	}

	var gqlClient *githubv4.Client
	if platformHost == "" || platformHost == "github.com" {
		gqlClient = githubv4.NewClient(tc)
	} else {
		endpoint := graphQLEndpointForHost(platformHost)
		gqlClient = githubv4.NewEnterpriseClient(endpoint, tc)
	}

	return &GraphQLFetcher{
		client:      gqlClient,
		rateTracker: rateTracker,
	}
}

// ShouldBackoff reports whether the GraphQL rate limit is
// exhausted.
func (g *GraphQLFetcher) ShouldBackoff() (bool, time.Duration) {
	if g.rateTracker == nil {
		return false, 0
	}
	return g.rateTracker.ShouldBackoff()
}

// FetchRepoPRs fetches all open PRs for a repo with nested
// comments, reviews, commits, and CI status. Returns adapted
// go-github types so existing normalize/DB layers work unchanged.
//
// If the initial query fails (complexity/node limit), retries
// with a smaller page size. Per-PR GraphQL errors cause the
// entire query to fail, falling back to REST via the caller.
func (g *GraphQLFetcher) FetchRepoPRs(
	ctx context.Context, owner, name string,
) (*RepoBulkResult, error) {
	result, err := g.fetchRepoPRsWithPageSize(
		ctx, owner, name, topLevelPageSize,
	)
	if err != nil {
		// Retry with smaller page for complexity/node-limit
		// errors. Also catches transient server errors.
		slog.Warn("GraphQL query failed, retrying with smaller page",
			"owner", owner, "name", name,
			"err", err, "retryPageSize", retryPageSize,
		)
		result, err = g.fetchRepoPRsWithPageSize(
			ctx, owner, name, retryPageSize,
		)
	}
	return result, err
}

func (g *GraphQLFetcher) fetchRepoPRsWithPageSize(
	ctx context.Context, owner, name string, pageSize int,
) (*RepoBulkResult, error) {
	gqlPRs, err := fetchAllPages(ctx, func(
		ctx context.Context, cursor *string,
	) ([]gqlPR, pageInfo, error) {
		var q gqlPRQuery
		vars := map[string]any{
			"owner":    githubv4.String(owner),
			"name":     githubv4.String(name),
			"pageSize": githubv4.Int(pageSize),
			"cursor":   cursorVar(cursor),
		}
		if err := g.client.Query(ctx, &q, vars); err != nil {
			return nil, pageInfo{}, err
		}
		return q.Repository.PullRequests.Nodes,
			q.Repository.PullRequests.PageInfo, nil
	})
	if err != nil {
		return nil, err
	}

	result := &RepoBulkResult{
		PullRequests: make([]BulkPR, 0, len(gqlPRs)),
	}
	for i := range gqlPRs {
		bulk := convertGQLPR(&gqlPRs[i])
		result.PullRequests = append(result.PullRequests, bulk)
	}
	return result, nil
}

// cursorVar converts a *string to the githubv4 cursor variable
// type. nil means first page.
func cursorVar(cursor *string) *githubv4.String {
	if cursor == nil {
		return nil
	}
	s := githubv4.String(*cursor)
	return &s
}

// convertGQLPR converts a single GraphQL PR to a BulkPR with
// adapted go-github types and completeness flags.
func convertGQLPR(gql *gqlPR) BulkPR {
	bulk := BulkPR{
		PR:               adaptPR(gql),
		CommentsComplete: !gql.Comments.PageInfo.HasNextPage,
		ReviewsComplete:  !gql.Reviews.PageInfo.HasNextPage,
		CommitsComplete:  !gql.AllCommits.PageInfo.HasNextPage,
	}

	for i := range gql.Comments.Nodes {
		bulk.Comments = append(bulk.Comments, adaptComment(&gql.Comments.Nodes[i]))
	}
	for i := range gql.Reviews.Nodes {
		bulk.Reviews = append(bulk.Reviews, adaptReview(&gql.Reviews.Nodes[i]))
	}
	for i := range gql.AllCommits.Nodes {
		bulk.Commits = append(bulk.Commits, adaptCommit(&gql.AllCommits.Nodes[i]))
	}

	// CI from last commit's statusCheckRollup
	bulk.CIComplete = true
	if len(gql.LastCommit.Nodes) > 0 {
		rollup := gql.LastCommit.Nodes[0].Commit.StatusCheckRollup
		if rollup != nil {
			bulk.CIComplete = !rollup.Contexts.PageInfo.HasNextPage
			bulk.CheckRuns, bulk.Statuses = splitCheckContexts(
				rollup.Contexts.Nodes,
			)
		}
	}

	return bulk
}
```

- [ ] **Step 4: Run adapter tests**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestAdapt -v`
Expected: all pass.

- [ ] **Step 5: Run full test suite**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -v`
Expected: all pass. If `gh.Ptr` is unavailable (older go-github), use `github.String()`/`github.Int()` helpers instead.

- [ ] **Step 6: Lint**

Run: `nix shell nixpkgs#golangci-lint --command golangci-lint run ./internal/github/`
Expected: zero warnings. Fix any issues.

- [ ] **Step 7: Commit**

```bash
git add internal/github/graphql.go internal/github/graphql_test.go
git commit -m "feat: add GraphQL types, adapters, and fetcher"
```

---

### Task 5: Sync Engine Integration

Wire `GraphQLFetcher` into the sync engine. Add `fetchers` field to `Syncer`, `SetFetchers`/`fetcherFor` methods, `doSyncRepoGraphQL`/`syncOpenMRFromBulk` for processing bulk results. Modify `indexSyncRepo` to use GraphQL path when fetcher is available and list returns 200.

**Files:**
- Modify: `internal/github/sync.go`

- [ ] **Step 1: Add fetchers field and accessor methods**

In `internal/github/sync.go`, add to the `Syncer` struct (after line 124, the `budgets` field):

```go
	fetchers  map[string]*GraphQLFetcher // host -> GraphQL fetcher
```

Add `SetFetchers` method (after `SetOnMRSynced` or similar setter methods):

```go
// SetFetchers registers GraphQL fetchers keyed by platform host.
func (s *Syncer) SetFetchers(fetchers map[string]*GraphQLFetcher) {
	s.fetchers = fetchers
}
```

Add `fetcherFor` method:

```go
// fetcherFor returns the GraphQL fetcher for a repo's host,
// or nil if none is configured.
func (s *Syncer) fetcherFor(repo RepoRef) *GraphQLFetcher {
	if s.fetchers == nil {
		return nil
	}
	host := repo.PlatformHost
	if host == "" {
		host = "github.com"
	}
	return s.fetchers[host]
}
```

- [ ] **Step 2: Implement doSyncRepoGraphQL**

Add the method that processes GraphQL bulk results. This replaces both the index upsert loop and the detail drain for completely-fetched PRs:

```go
// doSyncRepoGraphQL processes bulk GraphQL results for a repo.
// For each PR, it normalizes and upserts the MR row, inserts
// timeline events, updates CI status, and marks detail as
// fetched when all nested connections were complete.
func (s *Syncer) doSyncRepoGraphQL(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	result *RepoBulkResult,
	cloneFetchOK bool,
) error {
	var failedScope failScope
	stillOpen := make(map[int]bool, len(result.PullRequests))

	for i := range result.PullRequests {
		bulk := &result.PullRequests[i]
		number := bulk.PR.GetNumber()
		stillOpen[number] = true

		if err := s.syncOpenMRFromBulk(
			ctx, repo, repoID, bulk, cloneFetchOK,
		); err != nil {
			slog.Error("GraphQL sync MR failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
			failedScope |= failMR
		}
	}

	// Detect closed PRs — same as REST path.
	closedNumbers, err := s.db.GetPreviouslyOpenMRNumbers(
		ctx, repoID, stillOpen,
	)
	if err != nil {
		return fmt.Errorf("get previously open MRs: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosed(
			ctx, repo, repoID, number, cloneFetchOK,
		); err != nil {
			slog.Error("update closed MR failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
			failedScope |= failMR
		}
	}

	if failedScope != 0 {
		return fmt.Errorf("GraphQL sync had partial failures")
	}
	return nil
}
```

- [ ] **Step 3: Implement syncOpenMRFromBulk**

This method processes a single PR from GraphQL bulk results — normalize, upsert, events, CI, diff SHAs, mark detail fetched:

```go
// syncOpenMRFromBulk processes a single PR from GraphQL bulk
// results. It performs the same operations as fetchMRDetail but
// using pre-fetched data instead of per-PR REST calls.
func (s *Syncer) syncOpenMRFromBulk(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	bulk *BulkPR,
	cloneFetchOK bool,
) error {
	number := bulk.PR.GetNumber()
	normalized := NormalizePR(repoID, bulk.PR)

	// Resolve display name if missing.
	if normalized.Author != "" &&
		normalized.AuthorDisplayName == "" {
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		client := s.clientFor(repo)
		if name, ok := s.resolveDisplayName(
			ctx, client, host, normalized.Author,
		); ok {
			normalized.AuthorDisplayName = name
		}
	}

	mrID, err := s.db.UpsertMergeRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert MR #%d: %w", number, err)
	}

	if err := s.db.EnsureKanbanState(ctx, mrID); err != nil {
		return fmt.Errorf(
			"ensure kanban state for MR #%d: %w", number, err,
		)
	}

	// Diff SHAs.
	repoHost := repo.PlatformHost
	if repoHost == "" {
		repoHost = "github.com"
	}
	if s.clones != nil && cloneFetchOK {
		headSHA := normalized.PlatformHeadSHA
		baseSHA := normalized.PlatformBaseSHA
		if headSHA != "" && baseSHA != "" {
			mb, mbErr := s.clones.MergeBase(
				ctx, repoHost, repo.Owner,
				repo.Name, baseSHA, headSHA,
			)
			if mbErr != nil {
				slog.Warn("merge-base computation failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", number, "err", mbErr,
				)
			} else {
				if dbErr := s.db.UpdateDiffSHAs(
					ctx, repoID, number,
					headSHA, baseSHA, mb,
				); dbErr != nil {
					slog.Warn("update diff SHAs failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", number, "err", dbErr,
					)
				}
			}
		}
	}

	// Timeline events — comments, reviews, commits.
	// Events use ON CONFLICT DO NOTHING, so partial data is safe.
	var events []db.MREvent
	for _, c := range bulk.Comments {
		events = append(events, NormalizeCommentEvent(mrID, c))
	}
	for _, r := range bulk.Reviews {
		events = append(events, NormalizeReviewEvent(mrID, r))
	}
	for _, c := range bulk.Commits {
		events = append(events, NormalizeCommitEvent(mrID, c))
	}
	if len(events) > 0 {
		if err := s.db.UpsertMREvents(ctx, events); err != nil {
			return fmt.Errorf(
				"upsert events for MR #%d: %w", number, err,
			)
		}
	}

	// CI status — only write if complete (spec rule: don't write
	// truncated CI data that could hide failures).
	if bulk.CIComplete {
		ciChecks := normalizeBulkCI(bulk)
		ciJSON, _ := json.Marshal(ciChecks)
		ciStatus := DeriveOverallCIStatus(ciChecks)
		if err := s.db.UpdateMRCIStatus(
			ctx, repoID, number,
			ciStatus, string(ciJSON),
		); err != nil {
			slog.Warn("update CI status failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number, "err", err,
			)
		}
	}

	// Compute derived fields: ReviewDecision, CommentCount,
	// LastActivityAt — same as refreshTimeline does for REST.
	reviewDecision := DeriveReviewDecision(bulk.Reviews)
	lastActivity := computeLastActivity(
		bulk.PR, bulk.Comments, bulk.Reviews, bulk.Commits,
	)
	if err := s.db.UpdateMRDerivedFields(
		ctx, repoID, number, db.MRDerivedFields{
			ReviewDecision: reviewDecision,
			CommentCount:   len(bulk.Comments),
			LastActivityAt: lastActivity,
		},
	); err != nil {
		slog.Warn("update derived fields failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number, "err", err,
		)
	}

	// Mark detail as fetched only when ALL connections are
	// complete. Incomplete PRs leave DetailFetchedAt stale so
	// the detail drain picks them up for a full REST fetch.
	allComplete := bulk.CommentsComplete &&
		bulk.ReviewsComplete &&
		bulk.CommitsComplete &&
		bulk.CIComplete
	if allComplete {
		pending := false
		if bulk.CIComplete {
			ciChecks := normalizeBulkCI(bulk)
			ciJSON, _ := json.Marshal(ciChecks)
			pending = ciHasPending(string(ciJSON))
		}
		if err := s.db.UpdateMRDetailFetched(
			ctx, repoHost, repo.Owner, repo.Name,
			number, pending,
		); err != nil {
			slog.Warn("mark GraphQL detail fetched failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number, "err", err,
			)
		}
	}

	// Fire onMRSynced hook.
	if s.onMRSynced != nil {
		fresh, fErr := s.db.GetMergeRequest(
			ctx, repo.Owner, repo.Name, number,
		)
		if fErr != nil {
			slog.Warn("get MR for onMRSynced hook failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number, "err", fErr,
			)
		} else {
			s.onMRSynced(repo.Owner, repo.Name, fresh)
		}
	}

	return nil
}

// normalizeBulkCI converts GraphQL check runs and statuses to
// the db.CICheck slice format used by the rest of the codebase.
func normalizeBulkCI(bulk *BulkPR) []db.CICheck {
	var checks []db.CICheck
	for _, cr := range bulk.CheckRuns {
		checks = append(checks, db.CICheck{
			Name:       cr.GetName(),
			Status:     cr.GetStatus(),
			Conclusion: cr.GetConclusion(),
			URL:        cr.GetDetailsURL(),
			App:        cr.GetApp().GetName(),
		})
	}
	for _, s := range bulk.Statuses {
		// Map StatusContext to CICheck format. Status contexts
		// are always "completed" — they report final state only.
		// Map GitHub state names to CICheck conclusion values.
		conclusion := s.GetState()
		switch conclusion {
		case "failure", "error":
			conclusion = "failure"
		}
		checks = append(checks, db.CICheck{
			Name:       s.GetContext(),
			Status:     "completed",
			Conclusion: conclusion,
			URL:        s.GetTargetURL(),
		})
	}
	return checks
}
```

- [ ] **Step 4: Verify existing helper methods**

The following methods already exist and are reused by `syncOpenMRFromBulk` and `doSyncRepoGraphQL`:

```bash
nix shell nixpkgs#go --command grep -n 'func.*UpdateMRCIStatus\|func.*UpdateMRDerivedFields\|func DeriveOverallCIStatus\|func ciHasPending\|func.*GetPreviouslyOpenMRNumbers\|func.*fetchAndUpdateClosed\|func computeLastActivity\|func.*DeriveReviewDecision' internal/github/sync.go internal/github/normalize.go internal/db/queries.go
```

Expected matches:
- `UpdateMRCIStatus` in `queries.go:608`
- `UpdateMRDerivedFields` in `queries.go:588` (sets ReviewDecision + CommentCount + LastActivityAt)
- `DeriveOverallCIStatus` in `normalize.go:170`
- `DeriveReviewDecision` in `normalize.go:220`
- `ciHasPending` in `sync.go:1633`
- `computeLastActivity` in `sync.go:1650`
- `GetPreviouslyOpenMRNumbers` in `queries.go:553`
- `fetchAndUpdateClosed` in `sync.go:2567`

If any are missing, the plan has a bug. Stop and investigate.

- [ ] **Step 5: Wire GraphQL into indexSyncRepo**

In `internal/github/sync.go`, modify the `indexSyncRepo` method's 200 path (around line 1187). Replace the block:

```go
	} else {
		stillOpen := make(map[int]bool, len(ghPRs))
		for _, ghPR := range ghPRs {
			stillOpen[ghPR.GetNumber()] = true
		}

		for _, ghPR := range ghPRs {
			if err := s.indexUpsertMR(
				ctx, repo, repoID, ghPR,
			); err != nil {
```

With:

```go
	} else {
		// GraphQL path: if fetcher available and not rate-limited,
		// do a bulk fetch that replaces both index upsert and
		// detail drain for complete PRs.
		graphQLDone := false
		if fetcher := s.fetcherFor(repo); fetcher != nil {
			if backoff, _ := fetcher.ShouldBackoff(); !backoff {
				result, gqlErr := fetcher.FetchRepoPRs(
					ctx, repo.Owner, repo.Name,
				)
				if gqlErr != nil {
					slog.Warn("GraphQL fetch failed, falling back to REST index",
						"repo", repo.Owner+"/"+repo.Name,
						"err", gqlErr,
					)
				} else {
					if err := s.doSyncRepoGraphQL(
						ctx, repo, repoID, result, cloneFetchOK,
					); err != nil {
						failedScope |= failMR
					}
					graphQLDone = true
				}
			}
		}

		if !graphQLDone {
			// REST index fallback (original path).
			stillOpen := make(map[int]bool, len(ghPRs))
			for _, ghPR := range ghPRs {
				stillOpen[ghPR.GetNumber()] = true
			}

			for _, ghPR := range ghPRs {
				if err := s.indexUpsertMR(
					ctx, repo, repoID, ghPR,
				); err != nil {
```

The REST index loop, closed-PR detection, and closing braces continue as before — all nested inside the `if !graphQLDone` block. `doSyncRepoGraphQL` handles both index upsert and closed-PR detection internally, so the REST path is skipped entirely when GraphQL succeeds.

**Why not goto:** Go forbids `goto` jumping over variable declarations (`stillOpen := make(...)`). Boolean flag avoids this compile error.

- [ ] **Step 6: Add json import if needed**

Add `"encoding/json"` to the import block in `sync.go` if not already present (for `json.Marshal` in `normalizeBulkCI`). Check existing imports first.

- [ ] **Step 7: Build to verify compilation**

Run: `nix shell nixpkgs#go --command go build ./cmd/middleman/`
Expected: clean build. Fix any compile errors.

- [ ] **Step 8: Run tests**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ ./internal/db/ -v`
Expected: all existing tests pass. The new GraphQL path is not exercised by existing tests (no fetchers configured).

- [ ] **Step 9: Commit**

```bash
git add internal/github/sync.go internal/db/queries.go
git commit -m "feat: wire GraphQL sync into indexSyncRepo with REST fallback"
```

---

### Task 6: main.go Wiring and Verification

Create GraphQL fetchers in `main.go`, wire them to the syncer, run full test suite + build + lint.

**Files:**
- Modify: `cmd/middleman/main.go`

- [ ] **Step 1: Add GraphQL fetcher creation**

In `cmd/middleman/main.go`, add the GraphQL import and create fetchers after syncer creation.

Add to imports:

```go
// (already imported as ghclient "github.com/wesm/middleman/internal/github")
```

After the syncer creation (after line 153, `syncer := ghclient.NewSyncer(...)`), add:

```go
	fetchers := make(
		map[string]*ghclient.GraphQLFetcher, len(hostTokens),
	)
	for host, token := range hostTokens {
		gqlRT := ghclient.NewRateTracker(database, host, "graphql")
		fetchers[host] = ghclient.NewGraphQLFetcher(
			token, host, gqlRT, budgets[host],
		)
	}
	syncer.SetFetchers(fetchers)
```

- [ ] **Step 2: Build**

Run: `nix shell nixpkgs#go --command go build ./cmd/middleman/`
Expected: clean build.

- [ ] **Step 3: Run full test suite**

Run: `nix shell nixpkgs#go --command go test ./... -v`
Expected: all tests pass.

- [ ] **Step 4: Lint**

Run: `nix shell nixpkgs#golangci-lint --command golangci-lint run ./...`
Expected: zero warnings. Fix any issues.

- [ ] **Step 5: Commit**

```bash
git add cmd/middleman/main.go
git commit -m "feat: wire GraphQL fetchers into sync engine"
```

---

### Task 7: Test Suite

Add integration-style tests for the GraphQL sync path and rate transport. Verify adapter edge cases, partial failure behavior, and fallback to REST.

**Files:**
- Modify: `internal/github/graphql_test.go`
- Modify: `internal/github/graphql_pagination_test.go`

- [ ] **Step 1: Add rate transport test**

In `internal/github/graphql_test.go`, add a test for `graphqlRateTransport`:

```go
func TestGraphqlRateTransport(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com", "graphql")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(30*time.Minute).Unix()))
		w.WriteHeader(200)
		w.Write([]byte(`{"data":{}}`))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	transport := &graphqlRateTransport{
		base:        http.DefaultTransport,
		rateTracker: rt,
	}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest("POST", srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(1, rt.RequestsThisHour())
	assert.Equal(4999, rt.Remaining())
	assert.Equal(5000, rt.RateLimit())
}
```

Add imports for `"net/http"`, `"net/http/httptest"`, `"fmt"`, and `"time"` at the top of the test file.

- [ ] **Step 2: Add BulkPR completeness flag tests**

```go
func TestConvertGQLPRCompleteness(t *testing.T) {
	assert := Assert.New(t)

	// All complete
	gql := gqlPR{
		Number:    1,
		Title:     "test",
		State:     "OPEN",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	gql.Author.Login = "user"
	bulk := convertGQLPR(&gql)
	assert.True(bulk.CommentsComplete)
	assert.True(bulk.ReviewsComplete)
	assert.True(bulk.CommitsComplete)
	assert.True(bulk.CIComplete)

	// Comments incomplete
	gql.Comments.PageInfo.HasNextPage = true
	bulk = convertGQLPR(&gql)
	assert.False(bulk.CommentsComplete)
	assert.True(bulk.ReviewsComplete)
}

func TestNormalizeBulkCI(t *testing.T) {
	assert := Assert.New(t)

	bulk := &BulkPR{
		CheckRuns: []*gh.CheckRun{
			{
				Name:       gh.Ptr("test"),
				Status:     gh.Ptr("completed"),
				Conclusion: gh.Ptr("success"),
				DetailsURL: gh.Ptr("https://example.com"),
				App:        &gh.App{Name: gh.Ptr("Actions")},
			},
		},
		Statuses: []*gh.RepoStatus{
			{
				Context:   gh.Ptr("ci/lint"),
				State:     gh.Ptr("success"),
				TargetURL: gh.Ptr("https://example.com/2"),
			},
		},
	}

	checks := normalizeBulkCI(bulk)
	require.Equal(t, 2, len(checks))
	assert.Equal("test", checks[0].Name)
	assert.Equal("completed", checks[0].Status)
	assert.Equal("ci/lint", checks[1].Name)
	assert.Equal("completed", checks[1].Status)
}
```

- [ ] **Step 3: Add edge case adapter tests**

```go
func TestAdaptPRNilFields(t *testing.T) {
	assert := Assert.New(t)

	gql := gqlPR{
		Number:    1,
		Title:     "test",
		State:     "OPEN",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// HeadRepository is nil
	pr := adaptPR(&gql)
	assert.Nil(pr.GetHead().GetRepo())
	assert.Nil(pr.MergedAt)
	assert.False(pr.GetMerged())
}

func TestStateConversion(t *testing.T) {
	assert := Assert.New(t)
	assert.Equal("open", stateToREST("OPEN"))
	assert.Equal("closed", stateToREST("CLOSED"))
	assert.Equal("closed", stateToREST("MERGED"))
}

func TestMergeableConversion(t *testing.T) {
	assert := Assert.New(t)
	assert.Equal("clean", mergeableToREST("MERGEABLE"))
	assert.Equal("dirty", mergeableToREST("CONFLICTING"))
	assert.Equal("unknown", mergeableToREST("UNKNOWN"))
}
```

- [ ] **Step 4: Run all tests**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -v`
Expected: all pass.

- [ ] **Step 5: Final full suite**

Run: `nix shell nixpkgs#go --command go test ./... -v`
Expected: all pass.

- [ ] **Step 6: Final lint**

Run: `nix shell nixpkgs#golangci-lint --command golangci-lint run ./...`
Expected: zero warnings.

- [ ] **Step 7: Commit**

```bash
git add internal/github/graphql_test.go
git commit -m "test: add GraphQL adapter, transport, and completeness tests"
```

---

## Verification Checklist

1. `nix shell nixpkgs#go --command go test ./internal/github/ ./internal/db/ -v` — all pass
2. `nix shell nixpkgs#golangci-lint --command golangci-lint run ./...` — zero warnings
3. `nix shell nixpkgs#go --command go build ./cmd/middleman/` — clean build
4. Schema migration: opening a v3 database upgrades to v4 without error
5. REST trackers and GraphQL trackers operate independently (separate DB rows)
6. GraphQL path fires in `indexSyncRepo` when fetcher available + list returns 200
7. REST fallback fires when GraphQL is rate-limited or query fails
8. Detail drain skips GraphQL-fetched PRs (DetailFetchedAt is fresh)
9. Incomplete PRs (hasNextPage on any nested connection) are picked up by detail drain

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Per-instance RateTracker (apiType in constructor) | Zero public API changes. sync.go and client.go callers unchanged. GraphQL tracker is a separate object wired only to the fetcher. |
| No timeline_stale column | DetailFetchedAt mechanism handles this: complete PRs get DetailFetchedAt set (drain skips); incomplete PRs don't (drain picks up via REST). No schema bloat. |
| GraphQL rate tracking via transport wrapper | shurcooL/githubv4 doesn't expose HTTP response headers. Transport wrapper captures X-RateLimit-* headers automatically on every query. |
| Events always written (even partial) | ON CONFLICT DO NOTHING on dedupe_key means partial data is additive. REST detail drain adds missing events without duplicates. |
| CI only written when complete | Spec rule: truncated CI data could hide failures. Better stale-but-complete than fresh-but-partial. |
| Boolean flag for GraphQL/REST branching | `graphQLDone` flag controls whether REST fallback runs. Go forbids `goto` jumping over variable declarations, so a boolean flag is both correct and readable. |
| Detail drain for truncated nested connections | Spec mentions follow-up queries for nested connections exceeding page size. This plan uses the detail drain instead: if comments/reviews/commits/CI has HasNextPage=true, the Complete flag is false, DetailFetchedAt is not set, and the drain fetches via REST on the next cycle. Trade-off: one extra REST cycle for PRs with >100 events, but simpler implementation. Most PRs fit in a single page (100 items). Follow-up queries can be added later for efficiency. |
| Complexity retry with halved page size | Spec requires retry on 500k node-limit errors. Plan retries once with halved page size (25→12). If still fails, falls back to REST. |
| Full fallback on GraphQL errors | shurcooL/githubv4 returns partial data + error when the `errors` array is non-empty. The plan discards partial results on any error and falls back to REST, satisfying the spec's per-PR atomicity rule (no partial upserts from errored PRs). |
