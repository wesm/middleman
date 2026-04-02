# Issue/PR Link Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `#123` and `owner/repo#123` references in markdown content clickable links that navigate within middleman, fetching from GitHub on demand for items not in the local DB.

**Architecture:** A marked.js inline extension converts `#N` and `owner/repo#N` patterns to `<a>` elements with GitHub fallback hrefs and data attributes. A global click handler intercepts normal clicks, calls a resolve API endpoint that checks the local DB then falls back to GitHub sync, and navigates to the PR or issue view. A flash banner component displays errors for untracked repos or missing items.

**Tech Stack:** Go (Huma API, go-github), Svelte 5, marked.js, DOMPurify, TypeScript

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/github/sync.go` | Export `IsTrackedRepo`, add `SyncItemByNumber` |
| `internal/db/queries.go` | Add `ResolveItemNumber` query |
| `internal/server/api_types.go` | Add `resolveItemResponse` struct |
| `internal/server/huma_routes.go` | Register resolve endpoint, implement handler |
| `internal/server/api_test.go` | Tests for resolve endpoint |
| `frontend/src/lib/utils/markdown.ts` | Marked extension, repo context, DOMPurify config |
| `frontend/src/lib/utils/itemRefHandler.ts` | New: global click handler for `.item-ref` links |
| `frontend/src/lib/stores/flash.svelte.ts` | New: flash message store |
| `frontend/src/lib/components/FlashBanner.svelte` | New: renders flash messages |
| `frontend/src/App.svelte` | Initialize click handler, render FlashBanner |
| `frontend/src/lib/components/detail/PullDetail.svelte` | Pass repo context to renderMarkdown |
| `frontend/src/lib/components/detail/IssueDetail.svelte` | Pass repo context to renderMarkdown |
| `frontend/src/lib/components/detail/EventTimeline.svelte` | Accept + pass repo context to renderMarkdown |

---

### Task 1: Export IsTrackedRepo on Syncer

**Files:**
- Modify: `internal/github/sync.go:597-608`

- [ ] **Step 1: Write the failing test**

Add to `internal/github/sync_test.go`:

```go
func TestIsTrackedRepo(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	mc := &mockClient{}

	syncer := NewSyncer(mc, database, []RepoRef{
		{Owner: "acme", Name: "widget"},
		{Owner: "corp", Name: "lib"},
	}, time.Minute)

	assert.True(syncer.IsTrackedRepo("acme", "widget"))
	assert.True(syncer.IsTrackedRepo("corp", "lib"))
	assert.False(syncer.IsTrackedRepo("acme", "other"))
	assert.False(syncer.IsTrackedRepo("nobody", "widget"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/ -run TestIsTrackedRepo -v`
Expected: FAIL — `syncer.IsTrackedRepo` is not exported.

- [ ] **Step 3: Export the method**

In `internal/github/sync.go`, rename `isTrackedRepo` to `IsTrackedRepo`:

```go
// IsTrackedRepo checks whether the given repo is in the configured list.
func (s *Syncer) IsTrackedRepo(owner, name string) bool {
	s.reposMu.Lock()
	repos := s.repos
	s.reposMu.Unlock()
	for _, r := range repos {
		if r.Owner == owner && r.Name == name {
			return true
		}
	}
	return false
}
```

Update the two call sites in `SyncPR` (line 614) and `SyncIssue` (line 663) to use `s.IsTrackedRepo`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/ -run TestIsTrackedRepo -v`
Expected: PASS

- [ ] **Step 5: Run all existing tests to confirm no regressions**

Run: `go test ./internal/github/ -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "Export IsTrackedRepo on Syncer"
```

---

### Task 2: Add SyncItemByNumber to Syncer

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/github/sync_test.go`

This method fetches an item by number from GitHub's Issues API (which returns both issues and PRs), determines the type, and delegates to existing `SyncPR` or `SyncIssue`.

- [ ] **Step 1: Write the failing test**

Add to `internal/github/sync_test.go`:

```go
func TestSyncItemByNumber_Issue(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	number := 42
	title := "Bug report"
	state := "closed"
	author := "testuser"
	now := time.Now()
	ghTime := &gh.Timestamp{Time: now}

	mc := &mockClient{
		getIssueFn: func(_ context.Context, _, _ string, n int) (*gh.Issue, error) {
			if n != number {
				return nil, fmt.Errorf("unexpected number %d", n)
			}
			return &gh.Issue{
				ID:        gh.Ptr(int64(9999)),
				Number:    &number,
				Title:     &title,
				State:     &state,
				User:      &gh.User{Login: &author},
				HTMLURL:   gh.Ptr("https://github.com/acme/widget/issues/42"),
				CreatedAt: ghTime,
				UpdatedAt: ghTime,
			}, nil
		},
	}

	syncer := NewSyncer(mc, database, []RepoRef{
		{Owner: "acme", Name: "widget"},
	}, time.Minute)

	itemType, err := syncer.SyncItemByNumber(ctx, "acme", "widget", number)
	assert.NoError(err)
	assert.Equal("issue", itemType)

	issue, err := database.GetIssue(ctx, "acme", "widget", number)
	assert.NoError(err)
	assert.NotNil(issue)
	assert.Equal(title, issue.Title)
}

func TestSyncItemByNumber_PR(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	number := 10
	title := "Add feature"
	state := "open"
	author := "testuser"
	now := time.Now()
	ghTime := &gh.Timestamp{Time: now}
	prURL := "https://github.com/acme/widget/pull/10"

	mc := &mockClient{
		getIssueFn: func(_ context.Context, _, _ string, n int) (*gh.Issue, error) {
			return &gh.Issue{
				ID:      gh.Ptr(int64(8888)),
				Number:  &number,
				Title:   &title,
				State:   &state,
				User:    &gh.User{Login: &author},
				HTMLURL: gh.Ptr(prURL),
				PullRequestLinks: &gh.PullRequestLinks{
					URL: &prURL,
				},
				CreatedAt: ghTime,
				UpdatedAt: ghTime,
			}, nil
		},
		singlePR: &gh.PullRequest{
			ID:      gh.Ptr(int64(8888)),
			Number:  &number,
			Title:   &title,
			State:   &state,
			User:    &gh.User{Login: &author},
			HTMLURL: &prURL,
			Head: &gh.PullRequestBranch{
				Ref: gh.Ptr("feature"),
				SHA: gh.Ptr("abc123"),
			},
			Base:      &gh.PullRequestBranch{Ref: gh.Ptr("main")},
			CreatedAt: ghTime,
			UpdatedAt: ghTime,
		},
	}

	syncer := NewSyncer(mc, database, []RepoRef{
		{Owner: "acme", Name: "widget"},
	}, time.Minute)

	itemType, err := syncer.SyncItemByNumber(ctx, "acme", "widget", number)
	assert.NoError(err)
	assert.Equal("pr", itemType)

	pr, err := database.GetPullRequest(ctx, "acme", "widget", number)
	assert.NoError(err)
	assert.NotNil(pr)
	assert.Equal(title, pr.Title)
}

func TestSyncItemByNumber_UntrackedRepo(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	mc := &mockClient{}
	syncer := NewSyncer(mc, database, []RepoRef{
		{Owner: "acme", Name: "widget"},
	}, time.Minute)

	_, err := syncer.SyncItemByNumber(ctx, "other", "repo", 1)
	assert.Error(err)
	assert.Contains(err.Error(), "not tracked")
}
```

Also add `getIssueFn` to the `mockClient` struct in `sync_test.go` and update its `GetIssue` method:

```go
// Add field to mockClient struct:
getIssueFn func(context.Context, string, string, int) (*gh.Issue, error)

// Replace the existing GetIssue method:
func (m *mockClient) GetIssue(
	ctx context.Context, owner, repo string, number int,
) (*gh.Issue, error) {
	if m.getIssueFn != nil {
		return m.getIssueFn(ctx, owner, repo, number)
	}
	return nil, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/github/ -run TestSyncItemByNumber -v`
Expected: FAIL — `SyncItemByNumber` does not exist.

- [ ] **Step 3: Implement SyncItemByNumber**

Add to the end of `internal/github/sync.go` (before the closing of the file):

```go
// SyncItemByNumber fetches an item by number from GitHub, determines
// whether it is a PR or issue, syncs it into the DB, and returns the
// item type ("pr" or "issue").
// Returns an error if the repo is not in the configured repo list.
func (s *Syncer) SyncItemByNumber(
	ctx context.Context, owner, name string, number int,
) (string, error) {
	if !s.IsTrackedRepo(owner, name) {
		return "", fmt.Errorf("repo %s/%s is not tracked", owner, name)
	}

	// GitHub's Issues API returns both issues and PRs. If the
	// response has PullRequestLinks, it's a PR.
	ghIssue, err := s.client.GetIssue(ctx, owner, name, number)
	if err != nil {
		return "", fmt.Errorf(
			"get item %s/%s#%d: %w", owner, name, number, err,
		)
	}

	if ghIssue.PullRequestLinks != nil {
		if err := s.SyncPR(ctx, owner, name, number); err != nil {
			return "", fmt.Errorf(
				"sync PR %s/%s#%d: %w", owner, name, number, err,
			)
		}
		return "pr", nil
	}

	if err := s.SyncIssue(ctx, owner, name, number); err != nil {
		return "", fmt.Errorf(
			"sync issue %s/%s#%d: %w", owner, name, number, err,
		)
	}
	return "issue", nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/github/ -run TestSyncItemByNumber -v`
Expected: All 3 tests PASS

- [ ] **Step 5: Run all github package tests**

Run: `go test ./internal/github/ -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "Add SyncItemByNumber to Syncer"
```

---

### Task 3: Add ResolveItemNumber DB query

**Files:**
- Modify: `internal/db/queries.go`
- Create: `internal/db/queries_test.go` (or add to existing test file)

- [ ] **Step 1: Write the failing test**

Create `internal/db/queries_resolve_test.go`:

```go
package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestResolveItemNumber(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, "acme", "widget")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)

	// Seed a PR at number 10
	_, err = database.UpsertPullRequest(ctx, &PullRequest{
		RepoID:    repoID,
		GitHubID:  10000,
		Number:    10,
		URL:       "https://github.com/acme/widget/pull/10",
		Title:     "PR ten",
		Author:    "user",
		State:     "open",
		CreatedAt: now, UpdatedAt: now, LastActivityAt: now,
	})
	require.NoError(t, err)

	// Seed an issue at number 20
	_, err = database.UpsertIssue(ctx, &Issue{
		RepoID:    repoID,
		GitHubID:  20000,
		Number:    20,
		URL:       "https://github.com/acme/widget/issues/20",
		Title:     "Issue twenty",
		Author:    "user",
		State:     "open",
		CreatedAt: now, UpdatedAt: now, LastActivityAt: now,
	})
	require.NoError(t, err)

	// Resolve PR
	itemType, found, err := database.ResolveItemNumber(ctx, repoID, 10)
	assert.NoError(err)
	assert.True(found)
	assert.Equal("pr", itemType)

	// Resolve issue
	itemType, found, err = database.ResolveItemNumber(ctx, repoID, 20)
	assert.NoError(err)
	assert.True(found)
	assert.Equal("issue", itemType)

	// Unknown number
	_, found, err = database.ResolveItemNumber(ctx, repoID, 999)
	assert.NoError(err)
	assert.False(found)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestResolveItemNumber -v`
Expected: FAIL — `ResolveItemNumber` does not exist.

- [ ] **Step 3: Implement ResolveItemNumber**

Add to `internal/db/queries.go`, after the `GetIssueIDByRepoAndNumber` function:

```go
// ResolveItemNumber checks whether the given number in a repo is a PR
// or issue. Returns the item type ("pr" or "issue") and whether it was
// found. PRs take precedence if both somehow exist.
func (d *DB) ResolveItemNumber(
	ctx context.Context, repoID int64, number int,
) (itemType string, found bool, err error) {
	var exists int
	err = d.ro.QueryRowContext(ctx,
		`SELECT 1 FROM pull_requests WHERE repo_id = ? AND number = ?`,
		repoID, number,
	).Scan(&exists)
	if err == nil {
		return "pr", true, nil
	}

	err = d.ro.QueryRowContext(ctx,
		`SELECT 1 FROM issues WHERE repo_id = ? AND number = ?`,
		repoID, number,
	).Scan(&exists)
	if err == nil {
		return "issue", true, nil
	}

	return "", false, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestResolveItemNumber -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/db/queries.go internal/db/queries_resolve_test.go
git commit -m "Add ResolveItemNumber DB query"
```

---

### Task 4: Add resolve API endpoint

**Files:**
- Modify: `internal/server/api_types.go`
- Modify: `internal/server/huma_routes.go`
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Add test helpers**

Add a `setupTestServerWithRepos` helper and a `seedIssue` helper to `internal/server/api_test.go`.

The default `setupTestServer` creates a syncer with nil repos, so `IsTrackedRepo` always returns false. The resolve endpoint needs tracked repos to reach the DB lookup path.

```go
func setupTestServerWithRepos(
	t *testing.T, mock *mockGH, repos []ghclient.RepoRef,
) (*Server, *db.DB) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(mock, database, repos, time.Minute)
	srv := New(
		database, mock, syncer, nil, "/",
		nil, ServerOptions{},
	)
	return srv, database
}
```

- [ ] **Step 2: Write the failing tests**

Add to `internal/server/api_test.go`:

```go
func TestResolveItem_PR(t *testing.T) {
	assert := Assert.New(t)

	srv, database := setupTestServerWithRepos(t, &mockGH{}, []ghclient.RepoRef{
		{Owner: "acme", Name: "widget"},
	})
	seedPR(t, database, "acme", "widget", 10)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNameItemsByNumberWithResponse(
		context.Background(), "acme", "widget", 10,
	)
	assert.NoError(err)
	assert.Equal(http.StatusOK, resp.StatusCode())
	assert.NotNil(resp.JSON200)
	assert.Equal("pr", resp.JSON200.ItemType)
	assert.Equal(10, resp.JSON200.Number)
	assert.True(resp.JSON200.RepoTracked)
}

func TestResolveItem_Issue(t *testing.T) {
	assert := Assert.New(t)

	srv, database := setupTestServerWithRepos(t, &mockGH{}, []ghclient.RepoRef{
		{Owner: "acme", Name: "widget"},
	})
	seedIssue(t, database, "acme", "widget", 20)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNameItemsByNumberWithResponse(
		context.Background(), "acme", "widget", 20,
	)
	assert.NoError(err)
	assert.Equal(http.StatusOK, resp.StatusCode())
	assert.NotNil(resp.JSON200)
	assert.Equal("issue", resp.JSON200.ItemType)
	assert.Equal(20, resp.JSON200.Number)
	assert.True(resp.JSON200.RepoTracked)
}

func TestResolveItem_UntrackedRepo(t *testing.T) {
	assert := Assert.New(t)

	// Syncer has no repos configured
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNameItemsByNumberWithResponse(
		context.Background(), "acme", "widget", 999,
	)
	assert.NoError(err)
	assert.Equal(http.StatusOK, resp.StatusCode())
	assert.NotNil(resp.JSON200)
	assert.False(resp.JSON200.RepoTracked)
}

func TestResolveItem_NotFoundOnGitHub(t *testing.T) {
	assert := Assert.New(t)

	mock := &mockGH{
		getIssueFn: func(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
			return nil, fmt.Errorf("getting issue: 404 not found")
		},
	}
	srv, database := setupTestServerWithRepos(t, mock, []ghclient.RepoRef{
		{Owner: "acme", Name: "widget"},
	})
	// Seed the repo row so DB lookup runs but finds nothing
	_, err := database.UpsertRepo(context.Background(), "acme", "widget")
	assert.NoError(err)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNameItemsByNumberWithResponse(
		context.Background(), "acme", "widget", 999,
	)
	assert.NoError(err)
	assert.Equal(http.StatusNotFound, resp.StatusCode())
}
```

Also add a `seedIssue` helper near the existing `seedPR` helper:

```go
func seedIssue(t *testing.T, database *db.DB, owner, name string, number int) int64 {
	t.Helper()
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, owner, name)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	issue := &db.Issue{
		RepoID:         repoID,
		GitHubID:       int64(number) * 1000,
		Number:         number,
		URL:            fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, name, number),
		Title:          fmt.Sprintf("Test Issue #%d", number),
		Author:         "testuser",
		State:          "open",
		Body:           "test body",
		CommentCount:   0,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}

	issueID, err := database.UpsertIssue(ctx, issue)
	require.NoError(t, err)

	return issueID
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestResolveItem -v`
Expected: FAIL — endpoint and types don't exist yet.

- [ ] **Step 4: Add the response type**

Add to `internal/server/api_types.go`:

```go
type resolveItemResponse struct {
	ItemType    string `json:"item_type" doc:"'pr' or 'issue'"`
	Number      int    `json:"number"`
	RepoTracked bool   `json:"repo_tracked"`
}
```

- [ ] **Step 5: Add the Huma input/output types and handler**

Add to the top of `internal/server/huma_routes.go` (with the other type definitions):

```go
type resolveItemOutput struct {
	Body resolveItemResponse
}
```

Add the handler method:

```go
func (s *Server) resolveItem(
	ctx context.Context, input *repoNumberInput,
) (*resolveItemOutput, error) {
	owner, name, number := input.Owner, input.Name, input.Number

	// Check if repo is tracked in syncer config.
	if !s.syncer.IsTrackedRepo(owner, name) {
		return &resolveItemOutput{
			Body: resolveItemResponse{
				Number:      number,
				RepoTracked: false,
			},
		}, nil
	}

	// Try local DB lookup first.
	repo, err := s.db.GetRepoByOwnerName(ctx, owner, name)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get repo: " + err.Error(),
		)
	}
	if repo != nil {
		itemType, found, err := s.db.ResolveItemNumber(
			ctx, repo.ID, number,
		)
		if err != nil {
			return nil, huma.Error500InternalServerError(
				"resolve item: " + err.Error(),
			)
		}
		if found {
			return &resolveItemOutput{
				Body: resolveItemResponse{
					ItemType:    itemType,
					Number:      number,
					RepoTracked: true,
				},
			}, nil
		}
	}

	// Not in DB — fetch from GitHub on demand.
	itemType, err := s.syncer.SyncItemByNumber(
		ctx, owner, name, number,
	)
	if err != nil {
		return nil, huma.Error404NotFound(
			"item not found: " + err.Error(),
		)
	}

	return &resolveItemOutput{
		Body: resolveItemResponse{
			ItemType:    itemType,
			Number:      number,
			RepoTracked: true,
		},
	}, nil
}
```

- [ ] **Step 6: Register the endpoint**

In `internal/server/huma_routes.go`, inside `registerAPI`, add after the issues block (after the `post-issue-comment` registration, around line 234):

```go
huma.Get(api, "/repos/{owner}/{name}/items/{number}", s.resolveItem)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestResolveItem -v`
Expected: All 4 tests PASS.

- [ ] **Step 8: Regenerate API clients**

Run: `make api-generate`

This updates the OpenAPI spec, the TypeScript schema, and the Go generated client so both frontend and backend tests can use the new endpoint.

- [ ] **Step 9: Run all server tests**

Run: `go test ./internal/server/ -v -count=1`
Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add internal/server/api_types.go internal/server/huma_routes.go \
       internal/server/api_test.go \
       frontend/openapi/ frontend/src/lib/api/generated/ \
       internal/apiclient/
git commit -m "Add resolve item API endpoint with on-demand sync"
```

---

### Task 5: Marked.js inline extension for #N references

**Files:**
- Modify: `frontend/src/lib/utils/markdown.ts`

- [ ] **Step 1: Implement the marked extension**

Replace the contents of `frontend/src/lib/utils/markdown.ts`:

```typescript
import { Marked } from "marked";
import type { TokenizerAndRendererExtension } from "marked";
import DOMPurify from "dompurify";

interface RepoContext {
  owner: string;
  name: string;
}

function itemRefExtension(repo?: RepoContext): TokenizerAndRendererExtension {
  return {
    name: "itemRef",
    level: "inline",
    start(src: string): number | undefined {
      // Cross-repo: look for word chars before #
      const crossIdx = src.search(/[\w.-]+\/[\w.-]+#\d/);
      // Bare: look for # preceded by start or non-word
      const bareIdx = src.search(/(^|[^\w])#\d/);
      const adjusted = bareIdx >= 0 && src[bareIdx] !== "#"
        ? bareIdx + 1
        : bareIdx;
      if (crossIdx >= 0 && (adjusted < 0 || crossIdx <= adjusted)) {
        return crossIdx;
      }
      return adjusted >= 0 ? adjusted : undefined;
    },
    tokenizer(src: string): { type: string; raw: string; owner: string; name: string; number: number; text: string } | undefined {
      // Cross-repo: owner/name#123
      const crossMatch = src.match(
        /^([\w.-]+)\/([\w.-]+)#(\d+)/,
      );
      if (crossMatch) {
        return {
          type: "itemRef",
          raw: crossMatch[0],
          owner: crossMatch[1]!,
          name: crossMatch[2]!,
          number: parseInt(crossMatch[3]!, 10),
          text: crossMatch[0],
        };
      }
      // Bare ref: #123
      const bareMatch = src.match(/^#(\d+)/);
      if (bareMatch && repo) {
        return {
          type: "itemRef",
          raw: bareMatch[0],
          owner: repo.owner,
          name: repo.name,
          number: parseInt(bareMatch[1]!, 10),
          text: bareMatch[0],
        };
      }
      return undefined;
    },
    renderer(token): string {
      const t = token as { owner: string; name: string; number: number; text: string };
      const href = `https://github.com/${t.owner}/${t.name}/issues/${t.number}`;
      return `<a class="item-ref" href="${href}" data-owner="${t.owner}" data-name="${t.name}" data-number="${t.number}">${t.text}</a>`;
    },
  };
}

const cache = new Map<string, string>();

export function renderMarkdown(
  raw: string,
  repo?: RepoContext,
): string {
  if (!raw) return "";
  const key = repo ? `${repo.owner}/${repo.name}\0${raw}` : raw;
  const cached = cache.get(key);
  if (cached !== undefined) return cached;

  const marked = new Marked({
    breaks: true,
    gfm: true,
  });
  marked.use({ extensions: [itemRefExtension(repo)] });

  const html = DOMPurify.sanitize(marked.parse(raw) as string, {
    ADD_ATTR: ["target", "data-owner", "data-name", "data-number"],
  });
  if (cache.size > 500) cache.clear();
  cache.set(key, html);
  return html;
}
```

- [ ] **Step 2: Verify the build compiles**

Run: `cd frontend && bun run typecheck`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/utils/markdown.ts
git commit -m "Add marked.js extension for #N item references"
```

---

### Task 6: Pass repo context to renderMarkdown in components

**Files:**
- Modify: `frontend/src/lib/components/detail/PullDetail.svelte:416`
- Modify: `frontend/src/lib/components/detail/IssueDetail.svelte:208`
- Modify: `frontend/src/lib/components/detail/EventTimeline.svelte:1-10,95-99`

- [ ] **Step 1: Update EventTimeline to accept repo context**

In `frontend/src/lib/components/detail/EventTimeline.svelte`, update the Props interface and destructuring:

```typescript
interface Props {
  events: Array<PREvent | IssueEvent>;
  repoOwner?: string;
  repoName?: string;
}

const { events, repoOwner, repoName }: Props = $props();
```

Update the renderMarkdown call (line 97) to pass context:

```svelte
{@html renderMarkdown(event.Body, repoOwner && repoName ? { owner: repoOwner, name: repoName } : undefined)}
```

- [ ] **Step 2: Update PullDetail to pass repo context**

In `frontend/src/lib/components/detail/PullDetail.svelte`:

Update the renderMarkdown call for the PR body (line 416):

```svelte
<div class="inset-box markdown-body">{@html renderMarkdown(pr.Body, { owner, name })}</div>
```

Update the EventTimeline usage (line 429):

```svelte
<EventTimeline events={detail.events ?? []} repoOwner={owner} repoName={name} />
```

- [ ] **Step 3: Update IssueDetail to pass repo context**

In `frontend/src/lib/components/detail/IssueDetail.svelte`:

Update the renderMarkdown call for the issue body (line 208):

```svelte
<div class="inset-box markdown-body">{@html renderMarkdown(issue.Body, { owner, name })}</div>
```

Update the EventTimeline usage (line 237):

```svelte
<EventTimeline events={detail.events ?? []} repoOwner={owner} repoName={name} />
```

- [ ] **Step 4: Verify the build compiles**

Run: `cd frontend && bun run typecheck`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/components/detail/PullDetail.svelte \
       frontend/src/lib/components/detail/IssueDetail.svelte \
       frontend/src/lib/components/detail/EventTimeline.svelte
git commit -m "Pass repo context to renderMarkdown in detail views"
```

---

### Task 7: Flash message store and banner component

**Files:**
- Create: `frontend/src/lib/stores/flash.svelte.ts`
- Create: `frontend/src/lib/components/FlashBanner.svelte`
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Create flash store**

Create `frontend/src/lib/stores/flash.svelte.ts`:

```typescript
let message = $state<string | null>(null);
let timer: ReturnType<typeof setTimeout> | null = null;

export function showFlash(msg: string, durationMs = 4000): void {
  if (timer !== null) clearTimeout(timer);
  message = msg;
  timer = setTimeout(() => {
    message = null;
    timer = null;
  }, durationMs);
}

export function getFlashMessage(): string | null {
  return message;
}

export function dismissFlash(): void {
  if (timer !== null) clearTimeout(timer);
  message = null;
  timer = null;
}
```

- [ ] **Step 2: Create FlashBanner component**

Create `frontend/src/lib/components/FlashBanner.svelte`:

```svelte
<script lang="ts">
  import { getFlashMessage, dismissFlash } from "../stores/flash.svelte.js";
</script>

{#if getFlashMessage()}
  <div class="flash-banner">
    <span class="flash-text">{getFlashMessage()}</span>
    <button class="flash-dismiss" onclick={dismissFlash} title="Dismiss">x</button>
  </div>
{/if}

<style>
  .flash-banner {
    position: fixed;
    top: 44px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 1000;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 16px;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
    font-size: 13px;
    color: var(--text-primary);
    max-width: 480px;
  }

  .flash-text {
    flex: 1;
  }

  .flash-dismiss {
    flex-shrink: 0;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 14px;
    padding: 0 2px;
    line-height: 1;
  }

  .flash-dismiss:hover {
    color: var(--text-primary);
  }
</style>
```

- [ ] **Step 3: Add FlashBanner to App.svelte**

In `frontend/src/App.svelte`, add the import:

```typescript
import FlashBanner from "./lib/components/FlashBanner.svelte";
```

Add `<FlashBanner />` right after `<AppHeader />` (line 202):

```svelte
<AppHeader />
<FlashBanner />
```

- [ ] **Step 4: Verify the build compiles**

Run: `cd frontend && bun run typecheck`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/stores/flash.svelte.ts \
       frontend/src/lib/components/FlashBanner.svelte \
       frontend/src/App.svelte
git commit -m "Add flash message store and banner component"
```

---

### Task 8: Global click handler for item references

**Files:**
- Create: `frontend/src/lib/utils/itemRefHandler.ts`
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Create the click handler**

Create `frontend/src/lib/utils/itemRefHandler.ts`:

```typescript
import { client } from "../api/runtime.js";
import { navigate } from "../stores/router.svelte.js";
import { showFlash } from "../stores/flash.svelte.js";

function findItemRef(target: EventTarget | null): HTMLAnchorElement | null {
  let el = target as HTMLElement | null;
  while (el) {
    if (el instanceof HTMLAnchorElement && el.classList.contains("item-ref")) {
      return el;
    }
    el = el.parentElement;
  }
  return null;
}

async function resolveAndNavigate(
  owner: string,
  name: string,
  number: number,
): Promise<void> {
  const { data, error } = await client.GET(
    "/repos/{owner}/{name}/items/{number}",
    { params: { path: { owner, name, number } } },
  );

  if (error) {
    showFlash(`Item ${owner}/${name}#${number} not found on GitHub.`);
    return;
  }

  if (!data.repo_tracked) {
    showFlash(
      `${owner}/${name} is not tracked. Add it in Settings to navigate here.`,
    );
    return;
  }

  const path = data.item_type === "pr"
    ? `/pulls/${owner}/${name}/${number}`
    : `/issues/${owner}/${name}/${number}`;
  navigate(path);
}

function handleClick(e: MouseEvent): void {
  // Let browser handle modified clicks (cmd, ctrl, shift, middle).
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;

  const anchor = findItemRef(e.target);
  if (!anchor) return;

  const owner = anchor.dataset.owner;
  const name = anchor.dataset.name;
  const numberStr = anchor.dataset.number;
  if (!owner || !name || !numberStr) return;

  e.preventDefault();
  void resolveAndNavigate(owner, name, parseInt(numberStr, 10));
}

export function initItemRefHandler(): () => void {
  document.addEventListener("click", handleClick);
  return () => document.removeEventListener("click", handleClick);
}
```

- [ ] **Step 2: Initialize in App.svelte**

In `frontend/src/App.svelte`, add the import:

```typescript
import { initItemRefHandler } from "./lib/utils/itemRefHandler.js";
```

In the `onMount` callback, after `startPolling()` (line 58), add:

```typescript
const cleanupItemRefs = initItemRefHandler();
```

Return a cleanup function from `onMount`. The current `onMount` does not return a cleanup. Add it at the end of the async IIFE — but since `onMount` in Svelte 5 returns the cleanup from the outer function, restructure slightly. Actually, in the existing code `onMount` runs an async IIFE with no return. Add the cleanup separately:

```typescript
onMount(() => {
  const cleanupItemRefs = initItemRefHandler();
  void (async () => {
    // ... existing async init ...
  })();
  return () => {
    cleanupItemRefs();
  };
});
```

Move the `initItemRefHandler()` call to the synchronous part of onMount so it returns the cleanup.

- [ ] **Step 3: Verify the build compiles**

Run: `cd frontend && bun run typecheck`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/utils/itemRefHandler.ts frontend/src/App.svelte
git commit -m "Add global click handler for item reference links"
```

---

### Task 9: Add item-ref CSS to app.css

**Files:**
- Modify: `frontend/src/app.css`

- [ ] **Step 1: Add styles**

Add to the end of `frontend/src/app.css` (or in the markdown-body section if one exists):

```css
/* Item reference links (#123, owner/repo#456) */
.item-ref {
  color: var(--text-link, var(--accent-blue));
  text-decoration: none;
  cursor: pointer;
}
.item-ref:hover {
  text-decoration: underline;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/app.css
git commit -m "Add styles for item reference links"
```

---

### Task 10: End-to-end verification

- [ ] **Step 1: Run all Go tests**

Run: `make test`
Expected: All PASS

- [ ] **Step 2: Run frontend type checks**

Run: `make frontend-check`
Expected: No errors

- [ ] **Step 3: Build the full binary**

Run: `make build`
Expected: Build succeeds

- [ ] **Step 4: Manual smoke test**

Run: `make dev` and `make frontend-dev` in parallel. Navigate to a PR that references `#N` in its body. Verify:
- `#N` renders as a clickable link
- Normal click resolves and navigates within middleman
- Cmd/ctrl-click opens the GitHub fallback URL
- A reference to an unknown item shows a flash message
- Cross-repo `owner/repo#N` references work when the repo is tracked

- [ ] **Step 5: Commit any fixes needed**

If any fixes were needed during smoke testing, commit them.
