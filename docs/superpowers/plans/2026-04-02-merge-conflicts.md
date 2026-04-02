# Merge Conflict Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface PR merge conflict status throughout the UI and return actionable error messages on merge failure.

**Architecture:** Add `mergeable_state` column to the DB, capture it during sync from the GitHub API's individual PR endpoint, and expose it in API responses (automatic via embedded struct). Frontend shows conflict indicators in the PR list and a warning banner in PR detail. Merge errors forward GitHub's message directly rather than classifying through cached state.

**Tech Stack:** Go (SQLite, Huma, go-github v84), Svelte 5, TypeScript, openapi-typescript

**Spec:** `docs/superpowers/specs/2026-04-01-merge-conflicts-design.md`

---

### Task 1: DB migration and types

**Files:**
- Modify: `internal/db/types.go:22-49`
- Modify: `internal/db/db.go:57-68`
- Test: `internal/db/db_test.go`

- [ ] **Step 1: Write failing test for migration**

Add to `internal/db/db_test.go`:

```go
func TestMigrateMergeableState(t *testing.T) {
	d := openTestDB(t)

	// The column should exist after Open (which runs migrate).
	var val string
	err := d.ReadDB().QueryRow(
		"SELECT mergeable_state FROM pull_requests LIMIT 0",
	).Scan(&val)
	// LIMIT 0 returns no rows, but the column must parse without error.
	// sql.ErrNoRows is fine — it means the column exists.
	require.ErrorIs(t, err, sql.ErrNoRows)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestMigrateMergeableState -v`
Expected: FAIL — `no such column: mergeable_state`

- [ ] **Step 3: Add MergeableState field to PullRequest struct**

In `internal/db/types.go`, add `MergeableState` after `Starred` (line 48):

```go
type PullRequest struct {
	// ... existing fields ...
	Starred        bool
	MergeableState string
}
```

- [ ] **Step 4: Add ALTER TABLE migration**

In `internal/db/db.go`, append to the `migrations` slice in `migrate()`:

```go
"ALTER TABLE pull_requests ADD COLUMN mergeable_state TEXT NOT NULL DEFAULT ''",
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestMigrateMergeableState -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/db/types.go internal/db/db.go internal/db/db_test.go
git commit -m "feat: add mergeable_state column to pull_requests"
```

---

### Task 2: Include mergeable_state in DB queries

**Files:**
- Modify: `internal/db/queries.go:128-176` (UpsertPullRequest)
- Modify: `internal/db/queries.go:179-214` (GetPullRequest)
- Modify: `internal/db/queries.go:258-298` (ListPullRequests)
- Test: `internal/db/queries_test.go`

- [ ] **Step 1: Write failing test for upsert and retrieval of mergeable_state**

Add to `internal/db/queries_test.go`:

```go
func TestUpsertPullRequestMergeableState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID := insertTestRepo(t, d, "acme", "widget")
	now := baseTime()
	pr := &db.PullRequest{
		RepoID:         repoID,
		GitHubID:       9001,
		Number:         42,
		State:          "open",
		MergeableState: "dirty",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}

	_, err := d.UpsertPullRequest(ctx, pr)
	require.NoError(err)

	got, err := d.GetPullRequest(ctx, "acme", "widget", 42)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal("dirty", got.MergeableState)

	// Update to "clean" via upsert.
	pr.MergeableState = "clean"
	_, err = d.UpsertPullRequest(ctx, pr)
	require.NoError(err)

	got, err = d.GetPullRequest(ctx, "acme", "widget", 42)
	require.NoError(err)
	assert.Equal("clean", got.MergeableState)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestUpsertPullRequestMergeableState -v`
Expected: FAIL — column count mismatch or scan error

- [ ] **Step 3: Update UpsertPullRequest**

In `internal/db/queries.go`, modify `UpsertPullRequest` to include `mergeable_state` in both the INSERT column list and ON CONFLICT UPDATE:

INSERT columns (after `closed_at` on line 134):
```
     last_activity_at, merged_at, closed_at, mergeable_state)
```

VALUES placeholders: add one more `?` (25 total instead of 24).

ON CONFLICT block (after `closed_at` line 156):
```
    closed_at            = excluded.closed_at,
    mergeable_state      = excluded.mergeable_state`,
```

Bind args (after `pr.ClosedAt` on line 162):
```
		pr.LastActivityAt, pr.MergedAt, pr.ClosedAt, pr.MergeableState,
```

- [ ] **Step 4: Update GetPullRequest SELECT and Scan**

In `internal/db/queries.go`, modify `GetPullRequest`:

Add `p.mergeable_state` to the SELECT column list (after `p.closed_at` on line 188):
```
		       p.merged_at, p.closed_at, p.mergeable_state,
```

Add `&pr.MergeableState` to the Scan call (after `&pr.Starred` on line 205):
```
		&pr.MergedAt, &pr.ClosedAt, &pr.MergeableState,
		&pr.KanbanStatus, &pr.Starred,
```

Note: `mergeable_state` must be scanned BEFORE the kanban/starred columns since the SQL column order matters. Place it after `closed_at` in the SELECT and Scan to match.

- [ ] **Step 5: Update ListPullRequests SELECT and Scan**

In `internal/db/queries.go`, modify `ListPullRequests` the same way:

Add `p.mergeable_state` to the SELECT (after `p.closed_at` on line 265):
```
		       p.merged_at, p.closed_at, p.mergeable_state,
```

Add `&pr.MergeableState` to the Scan (after `&pr.Starred` on line 292):
```
		&pr.MergedAt, &pr.ClosedAt, &pr.MergeableState,
		&pr.KanbanStatus, &pr.Starred,
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestUpsertPullRequestMergeableState -v`
Expected: PASS

- [ ] **Step 7: Run all DB tests to verify no regressions**

Run: `go test ./internal/db/ -v`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/db/queries.go internal/db/queries_test.go
git commit -m "feat: include mergeable_state in PR upsert and queries"
```

---

### Task 3: Capture mergeable_state in normalization

**Files:**
- Modify: `internal/github/normalize.go:32-75`
- Test: `internal/github/normalize_test.go`

- [ ] **Step 1: Write failing test for mergeable state normalization**

Add to `internal/github/normalize_test.go`:

```go
func TestNormalizePR_MergeableState(t *testing.T) {
	tests := []struct {
		name  string
		state *string
		want  string
	}{
		{"dirty", new("dirty"), "dirty"},
		{"clean", new("clean"), "clean"},
		{"unknown", new("unknown"), "unknown"},
		{"blocked", new("blocked"), "blocked"},
		{"behind", new("behind"), "behind"},
		{"unstable", new("unstable"), "unstable"},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ghPR := &gh.PullRequest{
				ID:             new(int64(1)),
				Number:         new(1),
				State:          new("open"),
				MergeableState: tt.state,
			}
			pr := NormalizePR(1, ghPR)
			Assert.Equal(t, tt.want, pr.MergeableState)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/ -run TestNormalizePR_MergeableState -v`
Expected: FAIL — `MergeableState` is always `""`

- [ ] **Step 3: Add GetMergeableState() capture in NormalizePR**

In `internal/github/normalize.go`, add after the `BaseBranch` assignment (after line 72):

```go
	if ghPR.GetBase() != nil {
		pr.BaseBranch = ghPR.GetBase().GetRef()
	}

	pr.MergeableState = ghPR.GetMergeableState()

	return pr
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/ -run TestNormalizePR_MergeableState -v`
Expected: PASS

- [ ] **Step 5: Run all normalization tests**

Run: `go test ./internal/github/ -run TestNormalize -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/normalize.go internal/github/normalize_test.go
git commit -m "feat: capture MergeableState in NormalizePR"
```

---

### Task 4: Sync preservation and full-fetch trigger

**Files:**
- Modify: `internal/github/sync.go:224-257`
- Test: `internal/github/sync_test.go`

- [ ] **Step 1: Write failing test for mergeable_state preservation**

Add to `internal/github/sync_test.go`:

```go
func TestSyncPreservesMergeableState(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ciState := "success"
	pr := buildOpenPR(1, now)
	// Full PR sets mergeable state.
	mergeableState := "dirty"
	pr.MergeableState = &mergeableState
	pr.Additions = new(10)
	pr.Deletions = new(5)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{pr},
		commits:  []*gh.RepositoryCommit{},
		reviews:  []*gh.PullRequestReview{},
		comments: []*gh.IssueComment{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
	}

	syncer := NewSyncer(mc, d, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute)
	syncer.RunOnce(ctx)

	// Verify initial state is captured.
	got, err := d.GetPullRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(got)
	require.Equal("dirty", got.MergeableState)

	// Second sync with same UpdatedAt — full fetch should be skipped,
	// MergeableState should be preserved from the DB.
	// The list endpoint won't have mergeable_state, so the normalized
	// PR from the list will have MergeableState = "".
	listPR := buildOpenPR(1, now)
	// List endpoint doesn't return mergeable_state or diff stats.
	mc.openPRs = []*gh.PullRequest{listPR}
	syncer.RunOnce(ctx)

	got, err = d.GetPullRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(got)
	require.Equal("dirty", got.MergeableState, "mergeable_state should be preserved when full fetch is skipped")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/ -run TestSyncPreservesMergeableState -v`
Expected: FAIL — MergeableState is `""` after second sync

- [ ] **Step 3: Add preservation and full-fetch trigger**

In `internal/github/sync.go`, modify `syncOpenPR()`:

Update the `needsFullFetch` condition (around line 236):
```go
	needsFullFetch := needsTimeline ||
		(existing != nil && existing.Additions == 0 && existing.Deletions == 0) ||
		(existing != nil && existing.MergeableState == "") ||
		(existing != nil && existing.MergeableState == "unknown")
```

Update the `else if existing != nil` preservation block (around line 253):
```go
	} else if existing != nil {
		// Preserve fields the list endpoint doesn't return
		normalized.Additions = existing.Additions
		normalized.Deletions = existing.Deletions
		normalized.MergeableState = existing.MergeableState
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/ -run TestSyncPreservesMergeableState -v`
Expected: PASS

- [ ] **Step 5: Write test for full-fetch trigger on empty/unknown state**

Add to `internal/github/sync_test.go`:

```go
func TestSyncTriggersFullFetchForUnknownMergeableState(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ciState := "success"

	// First sync — full PR returns "unknown" mergeableState.
	pr := buildOpenPR(1, now)
	unknownState := "unknown"
	pr.MergeableState = &unknownState
	pr.Additions = new(10)
	pr.Deletions = new(5)

	fullFetchCount := 0
	mc := &mockClient{
		openPRs:  []*gh.PullRequest{pr},
		commits:  []*gh.RepositoryCommit{},
		reviews:  []*gh.PullRequestReview{},
		comments: []*gh.IssueComment{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			fullFetchCount++
			// Second call returns "clean".
			cleanState := "clean"
			fullPR := buildOpenPR(1, now)
			fullPR.MergeableState = &cleanState
			fullPR.Additions = new(10)
			fullPR.Deletions = new(5)
			return fullPR, nil
		},
	}

	syncer := NewSyncer(mc, d, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute)
	syncer.RunOnce(ctx)

	// After first sync, state is "unknown" (full fetch returned it).
	got, err := d.GetPullRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.Equal("unknown", got.MergeableState)

	// Second sync with same UpdatedAt — should still trigger full fetch
	// because MergeableState == "unknown".
	initialCount := fullFetchCount
	syncer.RunOnce(ctx)

	require.Greater(fullFetchCount, initialCount, "should trigger full fetch for unknown state")

	got, err = d.GetPullRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.Equal("clean", got.MergeableState)
}
```

Note: This test requires adding a `getPullRequestFn` field to `mockClient`. Add it to the struct definition and the `GetPullRequest` method:

In the `mockClient` struct (around line 26 of sync_test.go), add:
```go
type mockClient struct {
	openPRs          []*gh.PullRequest
	// ... existing fields ...
	getPullRequestFn func(context.Context, string, string, int) (*gh.PullRequest, error)
}
```

Update `GetPullRequest` (around line 47):
```go
func (m *mockClient) GetPullRequest(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	if m.getPullRequestFn != nil {
		return m.getPullRequestFn(ctx, owner, repo, number)
	}
	if len(m.openPRs) > 0 {
		return m.openPRs[0], nil
	}
	return nil, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/github/ -run TestSyncTriggersFullFetchForUnknownMergeableState -v`
Expected: PASS

- [ ] **Step 7: Run all sync tests**

Run: `go test ./internal/github/ -v`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: preserve mergeable_state in sync, trigger full fetch for empty/unknown"
```

---

### Task 5: Merge error handling

**Files:**
- Modify: `internal/server/huma_routes.go:567-599`
- Test: `internal/server/api_test.go`

- [ ] **Step 1: Write failing test for merge 405 error forwarding**

Add to `internal/server/api_test.go`:

```go
func TestAPIMergePR405ReturnsGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Pull Request is not mergeable",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		context.Background(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err) // HTTP call itself succeeds
	require.Equal(http.StatusConflict, resp.StatusCode())
}
```

Note: This test requires adding a `mergePullRequestFn` field to `mockGH`. Add it to the struct definition (around line 23 of api_test.go):

```go
type mockGH struct {
	// ... existing fields ...
	mergePullRequestFn func(context.Context, string, string, int, string, string, string) (*gh.PullRequestMergeResult, error)
}
```

Update the `MergePullRequest` method (around line 121):
```go
func (m *mockGH) MergePullRequest(
	ctx context.Context, owner, repo string, number int,
	commitTitle, commitMessage, method string,
) (*gh.PullRequestMergeResult, error) {
	if m.mergePullRequestFn != nil {
		return m.mergePullRequestFn(ctx, owner, repo, number, commitTitle, commitMessage, method)
	}
	merged := true
	sha := "abc123"
	msg := "merged"
	return &gh.PullRequestMergeResult{
		Merged: &merged, SHA: &sha, Message: &msg,
	}, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAPIMergePR405ReturnsGitHubMessage -v`
Expected: FAIL — returns 502 instead of 409

- [ ] **Step 3: Write test for merge 409 (SHA mismatch) forwarding**

Add to `internal/server/api_test.go`:

```go
func TestAPIMergePR409ReturnsGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 409},
				Message:  "Head branch was modified",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		context.Background(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusConflict, resp.StatusCode())
}
```

- [ ] **Step 4: Write test for non-405/409 returns 502**

Add to `internal/server/api_test.go`:

```go
func TestAPIMergePRNetworkErrorReturns502(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		context.Background(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
}
```

Add `"fmt"` to the imports if not already present.

- [ ] **Step 5: Implement merge error handling**

In `internal/server/huma_routes.go`, replace the error handling in `mergePR()` (lines 582-583):

```go
	if err != nil {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) &&
			(ghErr.Response.StatusCode == 405 || ghErr.Response.StatusCode == 409) {
			go s.syncer.SyncPR(context.WithoutCancel(ctx), input.Owner, input.Name, input.Number)
			return nil, huma.Error409Conflict(ghErr.Message)
		}
		return nil, huma.Error502BadGateway("GitHub merge error")
	}
```

Verify the `errors` import is already present (it is, line 5). The `gh` import alias for `go-github` is also present (line 13).

- [ ] **Step 6: Run all three merge error tests**

Run: `go test ./internal/server/ -run "TestAPIMergePR405|TestAPIMergePR409|TestAPIMergePRNetworkError" -v`
Expected: All PASS

- [ ] **Step 7: Run full server test suite**

Run: `go test ./internal/server/ -v`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/server/huma_routes.go internal/server/api_test.go
git commit -m "feat: forward GitHub merge error messages, background re-sync"
```

---

### Task 6: Regenerate API artifacts

**Files:**
- Modify: `frontend/openapi/openapi.json` (regenerated)
- Modify: `frontend/src/lib/api/generated/schema.ts` (regenerated)
- Modify: `internal/apiclient/spec/openapi.json` (regenerated)
- Modify: `internal/apiclient/generated/client.gen.go` (regenerated)

- [ ] **Step 1: Run all Go tests before regenerating**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 2: Regenerate API artifacts**

Run: `make api-generate`
Expected: Regenerates OpenAPI spec and both frontend/backend clients. `MergeableState` field should appear in the `PullRequest` schema.

- [ ] **Step 3: Verify MergeableState appears in generated schema**

Run: `grep -c MergeableState frontend/src/lib/api/generated/schema.ts`
Expected: At least 1 match

- [ ] **Step 4: Verify frontend types compile**

Run: `cd frontend && bun run check`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/openapi/openapi.json frontend/src/lib/api/generated/ \
  internal/apiclient/spec/openapi.json internal/apiclient/generated/
git commit -m "chore: regenerate API artifacts with MergeableState field"
```

---

### Task 7: Frontend conflict indicator in PR list

**Files:**
- Modify: `frontend/src/lib/components/sidebar/PullItem.svelte`

- [ ] **Step 1: Add conflict indicator to PullItem**

In `frontend/src/lib/components/sidebar/PullItem.svelte`, add a conflict icon in the `meta-right` span, before the star button (around line 48):

```svelte
    <span class="meta-right">
      {#if pr.MergeableState === "dirty"}
        <span class="conflict-dot" title="Has merge conflicts"></span>
      {/if}
      <span
        class="star-btn"
```

Add the CSS for `.conflict-dot` in the `<style>` block:

```css
  .conflict-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent-amber);
    flex-shrink: 0;
  }
```

- [ ] **Step 2: Verify frontend compiles**

Run: `cd frontend && bun run check`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/components/sidebar/PullItem.svelte
git commit -m "feat: show conflict indicator in PR list"
```

---

### Task 8: Frontend conflict banner in PR detail

**Files:**
- Modify: `frontend/src/lib/components/detail/PullDetail.svelte`

- [ ] **Step 1: Add mergeable state banner**

In `frontend/src/lib/components/detail/PullDetail.svelte`, add a banner between the kanban row and the actions row (after the `</div>` closing the kanban row, around line 311, before the `<!-- Approve / Merge / Close / Reopen actions -->` comment):

```svelte
      <!-- Mergeable state warnings -->
      {#if pr.State === "open" && pr.MergeableState === "dirty"}
        <div class="merge-warning merge-warning--conflict">
          <span>This branch has conflicts that must be resolved before merging.</span>
          <a href={pr.URL} target="_blank" rel="noopener noreferrer">View on GitHub</a>
        </div>
      {:else if pr.State === "open" && pr.MergeableState === "blocked"}
        <div class="merge-warning merge-warning--info">
          <span>Branch protection rules may prevent this merge.</span>
        </div>
      {:else if pr.State === "open" && pr.MergeableState === "behind"}
        <div class="merge-warning merge-warning--info">
          <span>This branch is behind the base branch and may need to be updated.</span>
        </div>
      {:else if pr.State === "open" && pr.MergeableState === "unstable"}
        <div class="merge-warning merge-warning--info">
          <span>Required status checks have not passed.</span>
        </div>
      {/if}
```

- [ ] **Step 2: Add CSS for merge warning banners**

Add to the `<style>` block in `PullDetail.svelte`:

```css
  .merge-warning {
    font-size: 12px;
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .merge-warning a {
    color: inherit;
    text-decoration: underline;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .merge-warning--conflict {
    background: color-mix(in srgb, var(--accent-amber) 12%, transparent);
    color: var(--accent-amber);
  }

  .merge-warning--info {
    background: color-mix(in srgb, var(--accent-blue) 10%, transparent);
    color: var(--text-secondary);
  }
```

- [ ] **Step 3: Verify frontend compiles**

Run: `cd frontend && bun run check`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/components/detail/PullDetail.svelte
git commit -m "feat: show merge conflict and advisory banners in PR detail"
```

---

### Task 9: Final verification

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: PASS

- [ ] **Step 3: Run go vet**

Run: `make vet`
Expected: PASS

- [ ] **Step 4: Run frontend checks**

Run: `cd frontend && bun run check`
Expected: PASS

- [ ] **Step 5: Build full binary**

Run: `make build`
Expected: PASS — binary builds with embedded frontend
