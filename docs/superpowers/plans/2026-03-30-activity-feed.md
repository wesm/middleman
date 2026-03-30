# Activity Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a unified activity feed view showing all PR/issue events across repos in reverse chronological order, with cursor-based pagination, URL routing, and a detail drawer.

**Architecture:** UNION ALL query across 4 subqueries (new PRs, new issues, PR events, issue events) with keyset pagination. One new API endpoint. Frontend gets URL-based routing (pushState/popstate), a new ActivityFeed component, and a slide-out DetailDrawer that reuses existing detail components.

**Tech Stack:** Go (SQLite queries, HTTP handler), Svelte 5 (runes, SPA), TypeScript, CSS custom properties

---

## File Structure

**New files:**
- `internal/db/queries_activity.go` — ListActivity query, cursor encode/decode
- `internal/db/queries_activity_test.go` — Tests for activity query
- `internal/server/handlers_activity.go` — Activity API handler
- `frontend/src/lib/api/activity.ts` — Activity API client + types
- `frontend/src/lib/stores/activity.svelte.ts` — Activity feed store
- `frontend/src/lib/components/ActivityFeed.svelte` — Activity feed table view
- `frontend/src/lib/components/DetailDrawer.svelte` — Slide-out detail drawer

**Modified files:**
- `internal/db/types.go` — Add ActivityItem, ListActivityOpts types
- `internal/server/server.go:47` — Register activity route
- `frontend/src/lib/stores/router.svelte.ts` — Rewrite to URL-based routing
- `frontend/src/lib/stores/detail.svelte.ts` — Conditional list refresh
- `frontend/src/lib/components/layout/AppHeader.svelte` — Three tabs, conditional view switcher
- `frontend/src/App.svelte` — Route-based view switching
- `frontend/src/app.css` — Add `--accent-teal` token

---

### Task 1: Go types for activity feed

**Files:**
- Modify: `internal/db/types.go:136`

- [ ] **Step 1: Add ActivityItem and ListActivityOpts types**

Append to `internal/db/types.go` after line 136:

```go
// ActivityItem represents one row in the unified activity feed.
type ActivityItem struct {
	ActivityType string    // new_pr, new_issue, comment, review, commit
	Source       string    // pr, issue, pre, ise
	SourceID     int64     // PK from the source table
	RepoOwner    string
	RepoName     string
	ItemType     string    // pr or issue
	ItemNumber   int
	ItemTitle    string
	ItemURL      string
	Author       string
	CreatedAt    time.Time
	BodyPreview  string
}

// ListActivityOpts holds filters and pagination for the activity feed.
type ListActivityOpts struct {
	Repo   string   // "owner/name" filter
	Types  []string // activity type filter
	Search string   // title/body search
	Limit  int      // page size (default 50, max 200)
	// Cursor fields — decoded from opaque token by the handler.
	BeforeTime     *time.Time
	BeforeSource   string
	BeforeSourceID int64
	AfterTime      *time.Time
	AfterSource    string
	AfterSourceID  int64
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/db/types.go
git commit -m "Add ActivityItem and ListActivityOpts types"
```

---

### Task 2: Activity query with cursor pagination

**Files:**
- Create: `internal/db/queries_activity.go`
- Create: `internal/db/queries_activity_test.go`

- [ ] **Step 1: Write the test**

Create `internal/db/queries_activity_test.go`:

```go
package db

import (
	"context"
	"testing"
	"time"
)

func insertTestIssue(t *testing.T, d *DB, repoID int64, number int, title string, activity time.Time) int64 {
	t.Helper()
	issue := &Issue{
		RepoID:         repoID,
		GitHubID:       repoID*10000 + int64(number),
		Number:         number,
		URL:            "https://github.com/example/repo/issues/" + title,
		Title:          title,
		Author:         "author",
		State:          "open",
		CreatedAt:      activity,
		UpdatedAt:      activity,
		LastActivityAt: activity,
	}
	id, err := d.UpsertIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("UpsertIssue %d: %v", number, err)
	}
	return id
}

func TestListActivity(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()

	repoA := insertTestRepo(t, d, "alice", "alpha")
	repoB := insertTestRepo(t, d, "bob", "beta")

	// Insert PRs and issues at known times.
	prID1 := insertTestPR(t, d, repoA, 1, "Fix bug", base)
	prID2 := insertTestPR(t, d, repoB, 2, "Add feature", base.Add(1*time.Minute))
	issueID1 := insertTestIssue(t, d, repoA, 10, "Crash on startup", base.Add(2*time.Minute))

	// Insert PR events.
	err := d.UpsertPREvents(ctx, []PREvent{
		{PRID: prID1, EventType: "issue_comment", Author: "carol",
			Body: "Looks good to me", CreatedAt: base.Add(3 * time.Minute), DedupeKey: "comment-1"},
		{PRID: prID2, EventType: "review", Author: "dave",
			Summary: "APPROVED", CreatedAt: base.Add(4 * time.Minute), DedupeKey: "review-1"},
		{PRID: prID1, EventType: "commit", Author: "alice",
			Summary: "abc123", Body: "fix: handle nil", CreatedAt: base.Add(5 * time.Minute), DedupeKey: "commit-abc123"},
		{PRID: prID1, EventType: "review_comment", Author: "eve",
			Body: "nit: rename var", CreatedAt: base.Add(6 * time.Minute), DedupeKey: "review_comment-1"},
	})
	if err != nil {
		t.Fatalf("UpsertPREvents: %v", err)
	}

	// Insert issue events.
	err = d.UpsertIssueEvents(ctx, []IssueEvent{
		{IssueID: issueID1, EventType: "issue_comment", Author: "frank",
			Body: "Can reproduce on macOS", CreatedAt: base.Add(7 * time.Minute), DedupeKey: "icomment-1"},
	})
	if err != nil {
		t.Fatalf("UpsertIssueEvents: %v", err)
	}

	t.Run("unfiltered returns all five types in desc order", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{Limit: 50})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		// Expected order (newest first):
		// 1. issue comment (base+7m) - review_comment at base+6m is excluded
		// 2. commit (base+5m)
		// 3. review (base+4m)
		// 4. PR comment (base+3m)
		// 5. new issue (base+2m)
		// 6. new PR bob/beta#2 (base+1m)
		// 7. new PR alice/alpha#1 (base)
		if len(items) != 7 {
			t.Fatalf("expected 7 items, got %d", len(items))
		}
		if items[0].ActivityType != "comment" || items[0].ItemType != "issue" {
			t.Errorf("items[0]: got type=%s item=%s, want comment/issue", items[0].ActivityType, items[0].ItemType)
		}
		if items[1].ActivityType != "commit" {
			t.Errorf("items[1]: got type=%s, want commit", items[1].ActivityType)
		}
		if items[2].ActivityType != "review" {
			t.Errorf("items[2]: got type=%s, want review", items[2].ActivityType)
		}
		if items[3].ActivityType != "comment" || items[3].ItemType != "pr" {
			t.Errorf("items[3]: got type=%s item=%s, want comment/pr", items[3].ActivityType, items[3].ItemType)
		}
		if items[4].ActivityType != "new_issue" {
			t.Errorf("items[4]: got type=%s, want new_issue", items[4].ActivityType)
		}
		if items[5].ActivityType != "new_pr" || items[5].RepoOwner != "bob" {
			t.Errorf("items[5]: got type=%s owner=%s, want new_pr/bob", items[5].ActivityType, items[5].RepoOwner)
		}
		if items[6].ActivityType != "new_pr" || items[6].RepoOwner != "alice" {
			t.Errorf("items[6]: got type=%s owner=%s, want new_pr/alice", items[6].ActivityType, items[6].RepoOwner)
		}
	})

	t.Run("repo filter", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{Repo: "alice/alpha", Limit: 50})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		for _, it := range items {
			if it.RepoOwner != "alice" || it.RepoName != "alpha" {
				t.Errorf("expected alice/alpha, got %s/%s", it.RepoOwner, it.RepoName)
			}
		}
	})

	t.Run("type filter", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Types: []string{"new_pr", "new_issue"},
			Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items (2 PRs + 1 issue), got %d", len(items))
		}
		for _, it := range items {
			if it.ActivityType != "new_pr" && it.ActivityType != "new_issue" {
				t.Errorf("unexpected type: %s", it.ActivityType)
			}
		}
	})

	t.Run("search filter", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{Search: "bug", Limit: 50})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("expected at least one result for 'bug' search")
		}
		for _, it := range items {
			if it.ItemTitle != "Fix bug" {
				t.Errorf("unexpected item: %s", it.ItemTitle)
			}
		}
	})

	t.Run("limit and before cursor", func(t *testing.T) {
		// Get first 3 items.
		page1, err := d.ListActivity(ctx, ListActivityOpts{Limit: 3})
		if err != nil {
			t.Fatalf("ListActivity page1: %v", err)
		}
		if len(page1) != 3 {
			t.Fatalf("expected 3, got %d", len(page1))
		}

		// Get next page using cursor from last item.
		last := page1[2]
		page2, err := d.ListActivity(ctx, ListActivityOpts{
			Limit:          3,
			BeforeTime:     &last.CreatedAt,
			BeforeSource:   last.Source,
			BeforeSourceID: last.SourceID,
		})
		if err != nil {
			t.Fatalf("ListActivity page2: %v", err)
		}
		if len(page2) != 3 {
			t.Fatalf("expected 3, got %d", len(page2))
		}

		// Pages should not overlap.
		seen := make(map[string]bool)
		for _, it := range page1 {
			seen[it.Source+":"+string(rune(it.SourceID))] = true
		}
		for _, it := range page2 {
			key := it.Source + ":" + string(rune(it.SourceID))
			if seen[key] {
				t.Errorf("duplicate across pages: %s", key)
			}
		}
	})

	t.Run("after cursor for polling", func(t *testing.T) {
		// Get all items.
		all, err := d.ListActivity(ctx, ListActivityOpts{Limit: 50})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		newest := all[0]

		// Insert a new event after the newest.
		err = d.UpsertPREvents(ctx, []PREvent{
			{PRID: prID1, EventType: "issue_comment", Author: "grace",
				Body: "New comment", CreatedAt: base.Add(10 * time.Minute), DedupeKey: "comment-new"},
		})
		if err != nil {
			t.Fatalf("UpsertPREvents: %v", err)
		}

		// Poll for items newer than the previous newest.
		newItems, err := d.ListActivity(ctx, ListActivityOpts{
			Limit:         50,
			AfterTime:     &newest.CreatedAt,
			AfterSource:   newest.Source,
			AfterSourceID: newest.SourceID,
		})
		if err != nil {
			t.Fatalf("ListActivity after: %v", err)
		}
		if len(newItems) != 1 {
			t.Fatalf("expected 1 new item, got %d", len(newItems))
		}
		if newItems[0].Author != "grace" {
			t.Errorf("expected author grace, got %s", newItems[0].Author)
		}
	})

	_ = prID2 // used in setup
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/wesm/code/middleman && go test ./internal/db/ -run TestListActivity -v`
Expected: compile error — `d.ListActivity` not defined

- [ ] **Step 3: Implement ListActivity**

Create `internal/db/queries_activity.go`:

```go
package db

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ListActivity returns a unified, reverse-chronological feed of activity across
// all repos. It merges new PRs, new issues, PR events, and issue events into a
// single stream with cursor-based keyset pagination.
func (d *DB) ListActivity(ctx context.Context, opts ListActivityOpts) ([]ActivityItem, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var whereClauses []string
	var args []any

	// Repo filter.
	if opts.Repo != "" {
		whereClauses = append(whereClauses, "repo_owner || '/' || repo_name = ?")
		args = append(args, opts.Repo)
	}

	// Type filter.
	if len(opts.Types) > 0 {
		placeholders := make([]string, len(opts.Types))
		for i, t := range opts.Types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		whereClauses = append(whereClauses, "activity_type IN ("+strings.Join(placeholders, ",")+")")
	}

	// Search filter.
	if opts.Search != "" {
		pattern := "%" + opts.Search + "%"
		whereClauses = append(whereClauses, "(item_title LIKE ? OR body_preview LIKE ?)")
		args = append(args, pattern, pattern)
	}

	// Before cursor (for "load more").
	if opts.BeforeTime != nil {
		whereClauses = append(whereClauses,
			"(created_at < ? OR (created_at = ? AND (source < ? OR (source = ? AND source_id < ?))))")
		args = append(args, *opts.BeforeTime, *opts.BeforeTime, opts.BeforeSource, opts.BeforeSource, opts.BeforeSourceID)
	}

	// After cursor (for polling).
	if opts.AfterTime != nil {
		whereClauses = append(whereClauses,
			"(created_at > ? OR (created_at = ? AND (source > ? OR (source = ? AND source_id > ?))))")
		args = append(args, *opts.AfterTime, *opts.AfterTime, opts.AfterSource, opts.AfterSource, opts.AfterSourceID)
	}

	where := ""
	if len(whereClauses) > 0 {
		where = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT activity_type, source, source_id, repo_owner, repo_name,
		       item_type, item_number, item_title, item_url, author,
		       created_at, body_preview
		FROM (
			SELECT 'new_pr' AS activity_type, 'pr' AS source, p.id AS source_id,
			       r.owner AS repo_owner, r.name AS repo_name,
			       'pr' AS item_type, p.number AS item_number, p.title AS item_title,
			       p.url AS item_url, p.author, p.created_at,
			       '' AS body_preview
			FROM pull_requests p JOIN repos r ON p.repo_id = r.id
			UNION ALL
			SELECT 'new_issue', 'issue', i.id,
			       r.owner, r.name,
			       'issue', i.number, i.title,
			       i.url, i.author, i.created_at,
			       ''
			FROM issues i JOIN repos r ON i.repo_id = r.id
			UNION ALL
			SELECT CASE e.event_type WHEN 'issue_comment' THEN 'comment' ELSE e.event_type END,
			       'pre', e.id,
			       r.owner, r.name,
			       'pr', p.number, p.title,
			       p.url, e.author, e.created_at,
			       substr(COALESCE(e.body, ''), 1, 200)
			FROM pr_events e
			JOIN pull_requests p ON e.pr_id = p.id
			JOIN repos r ON p.repo_id = r.id
			WHERE e.event_type IN ('issue_comment', 'review', 'commit')
			UNION ALL
			SELECT 'comment', 'ise', e.id,
			       r.owner, r.name,
			       'issue', i.number, i.title,
			       i.url, e.author, e.created_at,
			       substr(COALESCE(e.body, ''), 1, 200)
			FROM issue_events e
			JOIN issues i ON e.issue_id = i.id
			JOIN repos r ON i.repo_id = r.id
			WHERE e.event_type = 'issue_comment'
		) unified
		%s
		ORDER BY created_at DESC, source DESC, source_id DESC
		LIMIT ?`, where)

	args = append(args, limit)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var items []ActivityItem
	for rows.Next() {
		var it ActivityItem
		if err := rows.Scan(
			&it.ActivityType, &it.Source, &it.SourceID,
			&it.RepoOwner, &it.RepoName,
			&it.ItemType, &it.ItemNumber, &it.ItemTitle,
			&it.ItemURL, &it.Author, &it.CreatedAt, &it.BodyPreview,
		); err != nil {
			return nil, fmt.Errorf("scan activity item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// EncodeCursor encodes a sort position into an opaque cursor string.
func EncodeCursor(createdAt time.Time, source string, sourceID int64) string {
	raw := fmt.Sprintf("%d:%s:%d", createdAt.UnixMilli(), source, sourceID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses an opaque cursor string into its components.
func DecodeCursor(cursor string) (time.Time, string, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", 0, fmt.Errorf("decode cursor base64: %w", err)
	}
	parts := strings.SplitN(string(raw), ":", 3)
	if len(parts) != 3 {
		return time.Time{}, "", 0, fmt.Errorf("invalid cursor: expected 3 parts, got %d", len(parts))
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", 0, fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	sourceID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, "", 0, fmt.Errorf("invalid cursor source_id: %w", err)
	}
	return time.UnixMilli(ms).UTC(), parts[1], sourceID, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/wesm/code/middleman && go test ./internal/db/ -run TestListActivity -v`
Expected: all subtests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/db/queries_activity.go internal/db/queries_activity_test.go
git commit -m "Add ListActivity query with cursor-based pagination"
```

---

### Task 3: Activity API handler

**Files:**
- Create: `internal/server/handlers_activity.go`
- Modify: `internal/server/server.go:47`

- [ ] **Step 1: Write the handler**

Create `internal/server/handlers_activity.go`:

```go
package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/wesm/middleman/internal/db"
)

type activityResponse struct {
	Items   []activityItemResponse `json:"items"`
	HasMore bool                   `json:"has_more"`
}

type activityItemResponse struct {
	ID           string `json:"id"`
	Cursor       string `json:"cursor"`
	ActivityType string `json:"activity_type"`
	RepoOwner    string `json:"repo_owner"`
	RepoName     string `json:"repo_name"`
	ItemType     string `json:"item_type"`
	ItemNumber   int    `json:"item_number"`
	ItemTitle    string `json:"item_title"`
	ItemURL      string `json:"item_url"`
	Author       string `json:"author"`
	CreatedAt    string `json:"created_at"`
	BodyPreview  string `json:"body_preview"`
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	opts := db.ListActivityOpts{
		Repo:   q.Get("repo"),
		Search: q.Get("search"),
	}

	if types := q.Get("types"); types != "" {
		opts.Types = strings.Split(types, ",")
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	// Fetch one extra to determine has_more.
	opts.Limit = limit + 1

	if cursor := q.Get("before"); cursor != "" {
		t, source, sourceID, err := db.DecodeCursor(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before cursor: "+err.Error())
			return
		}
		opts.BeforeTime = &t
		opts.BeforeSource = source
		opts.BeforeSourceID = sourceID
	}

	if cursor := q.Get("after"); cursor != "" {
		t, source, sourceID, err := db.DecodeCursor(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid after cursor: "+err.Error())
			return
		}
		opts.AfterTime = &t
		opts.AfterSource = source
		opts.AfterSourceID = sourceID
	}

	items, err := s.db.ListActivity(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list activity: "+err.Error())
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	out := make([]activityItemResponse, len(items))
	for i, it := range items {
		out[i] = activityItemResponse{
			ID:           it.Source + ":" + strconv.FormatInt(it.SourceID, 10),
			Cursor:       db.EncodeCursor(it.CreatedAt, it.Source, it.SourceID),
			ActivityType: it.ActivityType,
			RepoOwner:    it.RepoOwner,
			RepoName:     it.RepoName,
			ItemType:     it.ItemType,
			ItemNumber:   it.ItemNumber,
			ItemTitle:    it.ItemTitle,
			ItemURL:      it.ItemURL,
			Author:       it.Author,
			CreatedAt:    it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			BodyPreview:  it.BodyPreview,
		}
	}

	writeJSON(w, http.StatusOK, activityResponse{Items: out, HasMore: hasMore})
}
```

- [ ] **Step 2: Register the route**

In `internal/server/server.go`, add after line 47 (after the sync status route):

```go
	s.mux.HandleFunc("GET /api/v1/activity", s.handleListActivity)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/wesm/code/middleman && go build ./...`
Expected: no errors

- [ ] **Step 4: Run all tests to check for regressions**

Run: `cd /Users/wesm/code/middleman && go test ./... -short`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers_activity.go internal/server/server.go
git commit -m "Add GET /api/v1/activity endpoint"
```

---

### Task 4: Frontend types and API client

**Files:**
- Create: `frontend/src/lib/api/activity.ts`

- [ ] **Step 1: Create activity API types and client**

Create `frontend/src/lib/api/activity.ts`:

```ts
const BASE = "/api/v1";

export interface ActivityItem {
  id: string;
  cursor: string;
  activity_type: "new_pr" | "new_issue" | "comment" | "review" | "commit";
  repo_owner: string;
  repo_name: string;
  item_type: "pr" | "issue";
  item_number: number;
  item_title: string;
  item_url: string;
  author: string;
  created_at: string;
  body_preview: string;
}

export interface ActivityResponse {
  items: ActivityItem[];
  has_more: boolean;
}

export interface ActivityParams {
  repo?: string;
  types?: string[];
  search?: string;
  limit?: number;
  before?: string;
  after?: string;
}

export async function listActivity(params?: ActivityParams): Promise<ActivityResponse> {
  const sp = new URLSearchParams();
  if (params?.repo) sp.set("repo", params.repo);
  if (params?.types && params.types.length > 0) sp.set("types", params.types.join(","));
  if (params?.search) sp.set("search", params.search);
  if (params?.limit) sp.set("limit", String(params.limit));
  if (params?.before) sp.set("before", params.before);
  if (params?.after) sp.set("after", params.after);
  const qs = sp.toString();
  const res = await fetch(`${BASE}/activity${qs ? `?${qs}` : ""}`);
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`GET /activity → ${res.status}: ${text}`);
  }
  return res.json() as Promise<ActivityResponse>;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/api/activity.ts
git commit -m "Add activity feed API client and types"
```

---

### Task 5: Add CSS teal accent token

**Files:**
- Modify: `frontend/src/app.css:19` (light theme), `frontend/src/app.css:53` (dark theme)

- [ ] **Step 1: Add --accent-teal to both themes**

In `frontend/src/app.css`, in the light theme section (`:root`), after `--accent-red: #dc2626;` (line 19), add:

```css
  --accent-teal: #0d9488;
```

In the dark theme section (`:root.dark`), after `--accent-red: #f87171;` (line 53), add:

```css
  --accent-teal: #2dd4bf;
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/app.css
git commit -m "Add --accent-teal CSS token for commit badge"
```

---

### Task 6: URL-based router

**Files:**
- Modify: `frontend/src/lib/stores/router.svelte.ts` (full rewrite)

- [ ] **Step 1: Rewrite router.svelte.ts**

Replace the entire contents of `frontend/src/lib/stores/router.svelte.ts`:

```ts
export type Route =
  | { page: "activity" }
  | { page: "pulls"; view: "list" | "board"; selected?: { owner: string; name: string; number: number } }
  | { page: "issues"; selected?: { owner: string; name: string; number: number } };

function parseRoute(path: string): Route {
  if (path.startsWith("/pulls")) {
    const rest = path.slice("/pulls".length);
    if (rest === "/board") return { page: "pulls", view: "board" };
    const match = rest.match(/^\/([^/]+)\/([^/]+)\/(\d+)$/);
    if (match) {
      return {
        page: "pulls",
        view: "list",
        selected: { owner: match[1], name: match[2], number: parseInt(match[3], 10) },
      };
    }
    return { page: "pulls", view: "list" };
  }
  if (path.startsWith("/issues")) {
    const match = path.slice("/issues".length).match(/^\/([^/]+)\/([^/]+)\/(\d+)$/);
    if (match) {
      return {
        page: "issues",
        selected: { owner: match[1], name: match[2], number: parseInt(match[3], 10) },
      };
    }
    return { page: "issues" };
  }
  return { page: "activity" };
}

let route = $state<Route>(parseRoute(window.location.pathname));

export function getRoute(): Route {
  return route;
}

export function getPage(): "activity" | "pulls" | "issues" {
  return route.page;
}

export function navigate(path: string): void {
  history.pushState(null, "", path);
  route = parseRoute(path);
}

export function replaceUrl(path: string): void {
  history.replaceState(null, "", path);
  route = parseRoute(path);
}

// Listen for browser back/forward.
if (typeof window !== "undefined") {
  window.addEventListener("popstate", () => {
    route = parseRoute(window.location.pathname);
  });
}

// --- backward-compat helpers for existing components ---

export type View = "list" | "board";
export type Tab = "pulls" | "issues";

export function getView(): View {
  return route.page === "pulls" && route.view === "board" ? "board" : "list";
}

export function setView(v: View): void {
  if (route.page === "pulls") {
    navigate(v === "board" ? "/pulls/board" : "/pulls");
  }
}

export function getTab(): Tab {
  if (route.page === "pulls") return "pulls";
  if (route.page === "issues") return "issues";
  return "pulls";
}

export function setTab(t: Tab): void {
  navigate(t === "pulls" ? "/pulls" : "/issues");
}
```

- [ ] **Step 2: Verify frontend compiles**

Run: `cd /Users/wesm/code/middleman/frontend && npx tsc --noEmit`
Expected: no errors (or only pre-existing ones)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/stores/router.svelte.ts
git commit -m "Rewrite router to URL-based pushState/popstate"
```

---

### Task 7: Activity store

**Files:**
- Create: `frontend/src/lib/stores/activity.svelte.ts`

- [ ] **Step 1: Create the activity store**

Create `frontend/src/lib/stores/activity.svelte.ts`:

```ts
import { listActivity } from "../api/activity.js";
import type { ActivityItem, ActivityParams } from "../api/activity.js";

// --- state ---

let items = $state<ActivityItem[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let hasMore = $state(false);
let filterRepo = $state<string | undefined>(undefined);
let filterTypes = $state<string[]>([]);
let searchQuery = $state<string | undefined>(undefined);
let pollHandle: ReturnType<typeof setInterval> | null = null;

// --- reads ---

export function getActivityItems(): ActivityItem[] {
  return items;
}

export function isActivityLoading(): boolean {
  return loading;
}

export function getActivityError(): string | null {
  return error;
}

export function hasMoreActivity(): boolean {
  return hasMore;
}

export function getActivityFilterRepo(): string | undefined {
  return filterRepo;
}

export function getActivityFilterTypes(): string[] {
  return filterTypes;
}

export function getActivitySearch(): string | undefined {
  return searchQuery;
}

// --- writes ---

export function setActivityFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}

export function setActivityFilterTypes(types: string[]): void {
  filterTypes = types;
}

export function setActivitySearch(q: string | undefined): void {
  searchQuery = q;
}

function buildParams(): ActivityParams {
  const p: ActivityParams = { limit: 50 };
  if (filterRepo) p.repo = filterRepo;
  if (filterTypes.length > 0) p.types = filterTypes;
  if (searchQuery) p.search = searchQuery;
  return p;
}

/** Load the feed from the top (initial load or after filter change). */
export async function loadActivity(): Promise<void> {
  loading = true;
  error = null;
  try {
    const resp = await listActivity(buildParams());
    items = resp.items;
    hasMore = resp.has_more;
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}

/** Load more items (append to existing list). */
export async function loadMoreActivity(): Promise<void> {
  if (items.length === 0) return;
  const lastItem = items[items.length - 1];
  loading = true;
  error = null;
  try {
    const params = buildParams();
    params.before = lastItem.cursor;
    const resp = await listActivity(params);
    items = [...items, ...resp.items];
    hasMore = resp.has_more;
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}

const OVERFLOW_LIMIT = 500;

/** Poll for new items since the newest displayed item. */
async function pollNewItems(): Promise<void> {
  if (items.length === 0) {
    await loadActivity();
    return;
  }
  try {
    const params = buildParams();
    params.after = items[0].cursor;
    const resp = await listActivity(params);
    if (resp.items.length >= OVERFLOW_LIMIT) {
      // Too many new items — full reload.
      await loadActivity();
      return;
    }
    if (resp.items.length > 0) {
      // Dedupe by id and prepend.
      const existingIds = new Set(items.map((it) => it.id));
      const newItems = resp.items.filter((it) => !existingIds.has(it.id));
      if (newItems.length > 0) {
        items = [...newItems, ...items];
      }
    }
  } catch {
    // Silent poll failure — don't overwrite error state.
  }
}

export function startActivityPolling(): void {
  stopActivityPolling();
  pollHandle = setInterval(() => {
    void pollNewItems();
  }, 15_000);
}

export function stopActivityPolling(): void {
  if (pollHandle !== null) {
    clearInterval(pollHandle);
    pollHandle = null;
  }
}

/** Sync URL query params → store state. Called when navigating to /. */
export function syncFromURL(): void {
  const sp = new URLSearchParams(window.location.search);
  filterRepo = sp.get("repo") ?? undefined;
  const typesParam = sp.get("types");
  filterTypes = typesParam ? typesParam.split(",") : [];
  searchQuery = sp.get("search") ?? undefined;
}

/** Sync store state → URL query params (replaceState). */
export function syncToURL(): void {
  const sp = new URLSearchParams();
  if (filterRepo) sp.set("repo", filterRepo);
  if (filterTypes.length > 0) sp.set("types", filterTypes.join(","));
  if (searchQuery) sp.set("search", searchQuery);
  const qs = sp.toString();
  const url = "/" + (qs ? `?${qs}` : "");
  history.replaceState(null, "", url);
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/stores/activity.svelte.ts
git commit -m "Add activity feed store with cursor pagination and polling"
```

---

### Task 8: ActivityFeed component

**Files:**
- Create: `frontend/src/lib/components/ActivityFeed.svelte`

- [ ] **Step 1: Create the ActivityFeed component**

Create `frontend/src/lib/components/ActivityFeed.svelte`:

```svelte
<script lang="ts">
  import { onDestroy } from "svelte";
  import type { ActivityItem } from "../api/activity.js";
  import {
    getActivityItems,
    isActivityLoading,
    getActivityError,
    hasMoreActivity,
    getActivityFilterRepo,
    getActivityFilterTypes,
    getActivitySearch,
    setActivityFilterRepo,
    setActivityFilterTypes,
    setActivitySearch,
    loadActivity,
    loadMoreActivity,
    startActivityPolling,
    stopActivityPolling,
    syncFromURL,
    syncToURL,
  } from "../stores/activity.svelte.js";
  import { listRepos } from "../api/client.js";
  import type { Repo } from "../api/types.js";

  interface Props {
    onSelectItem?: (item: ActivityItem) => void;
  }

  let { onSelectItem }: Props = $props();

  let repos = $state<Repo[]>([]);
  let searchInput = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  const ALL_TYPES = ["new_pr", "new_issue", "comment", "review", "commit"] as const;

  const TYPE_LABELS: Record<string, string> = {
    new_pr: "New PR",
    new_issue: "New Issue",
    comment: "Comment",
    review: "Review",
    commit: "Commit",
  };

  $effect(() => {
    syncFromURL();
    searchInput = getActivitySearch() ?? "";
    void loadActivity();
    startActivityPolling();
    void listRepos().then((r) => { repos = r; });
  });

  onDestroy(() => {
    stopActivityPolling();
  });

  function handleRepoChange(e: Event): void {
    const val = (e.target as HTMLSelectElement).value;
    setActivityFilterRepo(val || undefined);
    syncToURL();
    void loadActivity();
  }

  function toggleType(type: string): void {
    const current = getActivityFilterTypes();
    if (current.length === 0) {
      // All active (empty = no filter) → switch to only this type.
      setActivityFilterTypes([type]);
    } else if (current.includes(type)) {
      // Remove this type. If result is empty, means show all.
      const next = current.filter((t) => t !== type);
      setActivityFilterTypes(next);
    } else {
      // Add this type to the inclusion list.
      setActivityFilterTypes([...current, type]);
    }
    syncToURL();
    void loadActivity();
  }

  function isTypeActive(type: string): boolean {
    const f = getActivityFilterTypes();
    // Empty = all types shown.
    if (f.length === 0) return true;
    return f.includes(type);
  }

  function handleSearchInput(e: Event): void {
    const val = (e.target as HTMLInputElement).value;
    searchInput = val;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      setActivitySearch(val || undefined);
      syncToURL();
      void loadActivity();
    }, 300);
  }

  function badgeClass(type: string): string {
    switch (type) {
      case "new_pr": return "badge-pr";
      case "new_issue": return "badge-issue";
      case "comment": return "badge-comment";
      case "review": return "badge-review";
      case "commit": return "badge-commit";
      default: return "";
    }
  }

  function relativeTime(iso: string): string {
    const diff = Date.now() - new Date(iso).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return "just now";
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    return new Date(iso).toLocaleDateString();
  }

  function handleRowClick(item: ActivityItem): void {
    onSelectItem?.(item);
  }

  function handleLinkClick(e: Event, url: string): void {
    e.stopPropagation();
    window.open(url, "_blank", "noopener");
  }
</script>

<div class="activity-feed">
  <div class="controls-bar">
    <select class="repo-select" value={getActivityFilterRepo() ?? ""} onchange={handleRepoChange}>
      <option value="">All repositories</option>
      {#each repos as repo}
        <option value="{repo.Owner}/{repo.Name}">{repo.Owner}/{repo.Name}</option>
      {/each}
    </select>

    <div class="type-pills">
      {#each ALL_TYPES as type}
        <button
          class="type-pill"
          class:active={isTypeActive(type)}
          onclick={() => toggleType(type)}
        >
          <span class="pill-dot {badgeClass(type)}"></span>
          {TYPE_LABELS[type]}
        </button>
      {/each}
    </div>

    <input
      class="search-input"
      type="text"
      placeholder="Search titles and content..."
      value={searchInput}
      oninput={handleSearchInput}
    />
  </div>

  {#if getActivityError()}
    <div class="error-banner">{getActivityError()}</div>
  {/if}

  <div class="table-container">
    <table class="activity-table">
      <thead>
        <tr>
          <th class="col-type">Type</th>
          <th class="col-repo">Repository</th>
          <th class="col-item">Item</th>
          <th class="col-author">Author</th>
          <th class="col-when">When</th>
          <th class="col-link"></th>
        </tr>
      </thead>
      <tbody>
        {#each getActivityItems() as item (item.id)}
          <tr class="activity-row" onclick={() => handleRowClick(item)}>
            <td class="col-type">
              <span class="badge {badgeClass(item.activity_type)}">{TYPE_LABELS[item.activity_type]}</span>
            </td>
            <td class="col-repo">{item.repo_owner}/{item.repo_name}</td>
            <td class="col-item">
              <span class="item-number">#{item.item_number}</span>
              <span class="item-title">{item.item_title}</span>
            </td>
            <td class="col-author">{item.author}</td>
            <td class="col-when">{relativeTime(item.created_at)}</td>
            <td class="col-link">
              <button
                class="link-btn"
                title="Open on GitHub"
                onclick={(e) => handleLinkClick(e, item.item_url)}
              >&#x2197;</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if getActivityItems().length === 0 && !isActivityLoading()}
      <div class="empty-state">No activity found</div>
    {/if}
  </div>

  {#if hasMoreActivity()}
    <div class="load-more">
      <button class="load-more-btn" onclick={() => void loadMoreActivity()} disabled={isActivityLoading()}>
        {isActivityLoading() ? "Loading..." : "Load more"}
      </button>
    </div>
  {/if}
</div>

<style>
  .activity-feed {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .controls-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .repo-select {
    font: inherit;
    font-size: 12px;
    padding: 4px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  .type-pills {
    display: flex;
    gap: 4px;
  }

  .type-pill {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    font-size: 11px;
    color: var(--text-muted);
    border: 1px solid var(--border-muted);
    background: transparent;
    transition: opacity 0.15s;
  }

  .type-pill.active {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-surface);
  }

  .type-pill:not(.active) {
    opacity: 0.5;
  }

  .pill-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .pill-dot.badge-pr { background: var(--accent-blue); }
  .pill-dot.badge-issue { background: var(--accent-purple); }
  .pill-dot.badge-comment { background: var(--accent-amber); }
  .pill-dot.badge-review { background: var(--accent-green); }
  .pill-dot.badge-commit { background: var(--accent-teal); }

  .search-input {
    margin-left: auto;
    width: 220px;
    font-size: 12px;
    padding: 4px 8px;
  }

  .table-container {
    flex: 1;
    overflow-y: auto;
  }

  .activity-table {
    width: 100%;
    border-collapse: collapse;
    table-layout: fixed;
  }

  .activity-table thead {
    position: sticky;
    top: 0;
    background: var(--bg-surface);
    z-index: 1;
  }

  .activity-table th {
    text-align: left;
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border-default);
  }

  .activity-table td {
    padding: 5px 12px;
    border-bottom: 1px solid var(--border-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .col-type { width: 76px; }
  .col-repo { width: 160px; }
  .col-item { width: auto; }
  .col-author { width: 130px; }
  .col-when { width: 80px; text-align: right; }
  th.col-when { text-align: right; }
  .col-link { width: 36px; text-align: center; }

  .activity-row {
    cursor: pointer;
    transition: background 0.1s;
  }

  .activity-row:hover {
    background: var(--bg-surface-hover);
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }

  .badge-pr { background: color-mix(in srgb, var(--accent-blue) 18%, transparent); color: var(--accent-blue); }
  .badge-issue { background: color-mix(in srgb, var(--accent-purple) 18%, transparent); color: var(--accent-purple); }
  .badge-comment { background: color-mix(in srgb, var(--accent-amber) 18%, transparent); color: var(--accent-amber); }
  .badge-review { background: color-mix(in srgb, var(--accent-green) 18%, transparent); color: var(--accent-green); }
  .badge-commit { background: color-mix(in srgb, var(--accent-teal) 18%, transparent); color: var(--accent-teal); }

  .col-repo {
    color: var(--text-muted);
    font-size: 12px;
  }

  .item-number {
    color: var(--text-muted);
    margin-right: 4px;
  }

  .item-title {
    color: var(--text-primary);
  }

  .col-author {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .col-when {
    color: var(--text-muted);
    font-size: 12px;
  }

  .link-btn {
    color: var(--text-muted);
    font-size: 13px;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }

  .link-btn:hover {
    color: var(--accent-blue);
    background: var(--bg-surface-hover);
  }

  .empty-state {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }

  .error-banner {
    padding: 8px 16px;
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 12px;
    border-bottom: 1px solid var(--border-default);
  }

  .load-more {
    padding: 8px 16px;
    text-align: center;
    border-top: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .load-more-btn {
    padding: 5px 16px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    font-size: 12px;
    color: var(--text-secondary);
    background: var(--bg-surface);
  }

  .load-more-btn:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .load-more-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/components/ActivityFeed.svelte
git commit -m "Add ActivityFeed component with controls and table"
```

---

### Task 9: Detail drawer component

**Files:**
- Create: `frontend/src/lib/components/DetailDrawer.svelte`

- [ ] **Step 1: Create the DetailDrawer component**

Create `frontend/src/lib/components/DetailDrawer.svelte`:

```svelte
<script lang="ts">
  import PullDetail from "./detail/PullDetail.svelte";
  import IssueDetail from "./detail/IssueDetail.svelte";

  interface Props {
    itemType: "pr" | "issue";
    owner: string;
    name: string;
    number: number;
    onClose: () => void;
  }

  let { itemType, owner, name, number, onClose }: Props = $props();

  function handleBackdropClick(e: MouseEvent): void {
    if (e.target === e.currentTarget) {
      onClose();
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }

  $effect(() => {
    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="drawer-backdrop" onclick={handleBackdropClick}>
  <aside class="drawer-panel">
    <div class="drawer-header">
      <button class="close-btn" onclick={onClose} title="Close (Esc)">&#x2715;</button>
      <span class="drawer-title">
        {owner}/{name}#{number}
      </span>
    </div>
    <div class="drawer-body">
      {#if itemType === "pr"}
        <PullDetail {owner} {name} {number} />
      {:else}
        <IssueDetail {owner} {name} {number} />
      {/if}
    </div>
  </aside>
</div>

<style>
  .drawer-backdrop {
    position: fixed;
    top: var(--header-height);
    left: 0;
    right: 0;
    bottom: var(--status-bar-height);
    z-index: 100;
  }

  .drawer-panel {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 50%;
    min-width: 500px;
    background: var(--bg-surface);
    border-left: 1px solid var(--border-default);
    box-shadow: var(--shadow-lg);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .drawer-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-default);
    flex-shrink: 0;
  }

  .close-btn {
    padding: 4px 8px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 14px;
  }

  .close-btn:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .drawer-title {
    font-size: 12px;
    color: var(--text-muted);
  }

  .drawer-body {
    flex: 1;
    overflow-y: auto;
  }

  @media (max-width: 1023px) {
    .drawer-panel {
      width: 100%;
      min-width: 0;
    }
  }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/components/DetailDrawer.svelte
git commit -m "Add DetailDrawer slide-out component"
```

---

### Task 10: Update AppHeader with Activity tab

**Files:**
- Modify: `frontend/src/lib/components/layout/AppHeader.svelte`

- [ ] **Step 1: Rewrite AppHeader**

Replace the entire contents of `frontend/src/lib/components/layout/AppHeader.svelte`:

```svelte
<script lang="ts">
  import { getPage, getView, navigate } from "../../stores/router.svelte.ts";
  import { getSyncState, triggerSync } from "../../stores/sync.svelte.js";

  let dark = $state(false);

  $effect(() => {
    dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    applyTheme(dark);
  });

  function applyTheme(isDark: boolean): void {
    document.documentElement.classList.toggle("dark", isDark);
  }

  function toggleTheme(): void {
    dark = !dark;
    applyTheme(dark);
  }

  async function handleSync(): Promise<void> {
    if (getSyncState()?.running) return;
    await triggerSync();
  }

  const syncing = $derived(getSyncState()?.running ?? false);
</script>

<header class="app-header">
  <div class="header-left">
    <span class="logo">middleman</span>
  </div>

  <nav class="header-center">
    <div class="tab-group">
      <button class="view-tab" class:active={getPage() === "activity"} onclick={() => navigate("/")}>
        Activity
      </button>
      <button class="view-tab" class:active={getPage() === "pulls"} onclick={() => navigate("/pulls")}>
        PRs
      </button>
      <button class="view-tab" class:active={getPage() === "issues"} onclick={() => navigate("/issues")}>
        Issues
      </button>
    </div>
    {#if getPage() === "pulls"}
      <div class="tab-group">
        <button class="view-tab" class:active={getView() === "list"} onclick={() => navigate("/pulls")}>
          List
        </button>
        <button class="view-tab" class:active={getView() === "board"} onclick={() => navigate("/pulls/board")}>
          Board
        </button>
      </div>
    {/if}
  </nav>

  <div class="header-right">
    <button class="action-btn" onclick={handleSync} disabled={syncing}>
      {syncing ? "Syncing..." : "Sync"}
    </button>
    <button class="action-btn icon-btn" onclick={toggleTheme} title="Toggle theme">
      {dark ? "☀" : "☾"}
    </button>
  </div>
</header>

<style>
  .app-header {
    height: var(--header-height);
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-default);
    display: flex;
    align-items: center;
    padding: 0 16px;
    gap: 16px;
    flex-shrink: 0;
    box-shadow: var(--shadow-sm);
  }

  .header-left {
    flex: 1;
    display: flex;
    align-items: center;
  }

  .logo {
    font-weight: 600;
    font-size: 15px;
    color: var(--text-primary);
    letter-spacing: -0.01em;
  }

  .header-center {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .tab-group {
    display: flex;
    align-items: center;
    gap: 2px;
    background: var(--bg-inset);
    border-radius: var(--radius-md);
    padding: 2px;
  }

  .view-tab {
    padding: 4px 14px;
    border-radius: calc(var(--radius-md) - 2px);
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
  }

  .view-tab:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .view-tab.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: var(--shadow-sm);
  }

  .header-right {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }

  .action-btn {
    padding: 5px 12px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
    background: var(--bg-surface);
    transition: background 0.15s, color 0.15s, border-color 0.15s;
  }

  .action-btn:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
    border-color: var(--border-muted);
  }

  .action-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .icon-btn {
    padding: 5px 10px;
  }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/components/layout/AppHeader.svelte
git commit -m "Add Activity tab to AppHeader, conditional view switcher"
```

---

### Task 11: Integrate routing in App.svelte

**Files:**
- Modify: `frontend/src/App.svelte` (full rewrite)

- [ ] **Step 1: Rewrite App.svelte**

Replace the entire contents of `frontend/src/App.svelte`:

```svelte
<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import StatusBar from "./lib/components/layout/StatusBar.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import PullDetail from "./lib/components/detail/PullDetail.svelte";
  import IssueList from "./lib/components/sidebar/IssueList.svelte";
  import IssueDetail from "./lib/components/detail/IssueDetail.svelte";
  import KanbanBoard from "./lib/components/kanban/KanbanBoard.svelte";
  import ActivityFeed from "./lib/components/ActivityFeed.svelte";
  import DetailDrawer from "./lib/components/DetailDrawer.svelte";
  import { getRoute, getPage, navigate } from "./lib/stores/router.svelte.ts";
  import { startPolling } from "./lib/stores/sync.svelte.js";
  import {
    getSelectedPR,
    selectNextPR,
    selectPrevPR,
    clearSelection,
    selectPR,
  } from "./lib/stores/pulls.svelte.js";
  import {
    getSelectedIssue,
    selectNextIssue,
    selectPrevIssue,
    clearIssueSelection,
    selectIssue,
  } from "./lib/stores/issues.svelte.js";
  import type { ActivityItem } from "./lib/api/activity.js";

  let drawerItem = $state<{
    itemType: "pr" | "issue";
    owner: string;
    name: string;
    number: number;
  } | null>(null);

  $effect(() => {
    startPolling();
  });

  // Restore drawer from URL on mount (/?selected=pr:owner/name/42).
  $effect(() => {
    const sp = new URLSearchParams(window.location.search);
    const sel = sp.get("selected");
    if (sel && getPage() === "activity") {
      const match = sel.match(/^(pr|issue):([^/]+)\/([^/]+)\/(\d+)$/);
      if (match) {
        drawerItem = {
          itemType: match[1] as "pr" | "issue",
          owner: match[2],
          name: match[3],
          number: parseInt(match[4], 10),
        };
      }
    }
  });

  // When navigating to a detail route, select the item.
  $effect(() => {
    const route = getRoute();
    if (route.page === "pulls" && route.selected) {
      selectPR(route.selected.owner, route.selected.name, route.selected.number);
    }
    if (route.page === "issues" && route.selected) {
      selectIssue(route.selected.owner, route.selected.name, route.selected.number);
    }
  });

  function updateDrawerURL(item: typeof drawerItem): void {
    const sp = new URLSearchParams(window.location.search);
    if (item) {
      sp.set("selected", `${item.itemType}:${item.owner}/${item.name}/${item.number}`);
    } else {
      sp.delete("selected");
    }
    const qs = sp.toString();
    history.replaceState(null, "", "/" + (qs ? `?${qs}` : ""));
  }

  function handleActivitySelect(item: ActivityItem): void {
    drawerItem = {
      itemType: item.item_type,
      owner: item.repo_owner,
      name: item.repo_name,
      number: item.item_number,
    };
    updateDrawerURL(drawerItem);
  }

  function closeDrawer(): void {
    drawerItem = null;
    updateDrawerURL(null);
  }

  function handleKeydown(e: KeyboardEvent): void {
    const tag = (e.target as HTMLElement).tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

    const page = getPage();

    if (page === "activity") {
      if (e.key === "Escape" && drawerItem) {
        e.preventDefault();
        closeDrawer();
      }
      return;
    }

    const isIssues = page === "issues";

    switch (e.key) {
      case "j":
        e.preventDefault();
        if (isIssues) selectNextIssue();
        else selectNextPR();
        break;
      case "k":
        e.preventDefault();
        if (isIssues) selectPrevIssue();
        else selectPrevPR();
        break;
      case "Escape":
        e.preventDefault();
        if (isIssues) clearIssueSelection();
        else clearSelection();
        break;
      case "1":
        e.preventDefault();
        navigate("/pulls");
        break;
      case "2":
        e.preventDefault();
        navigate("/pulls/board");
        break;
    }
  }

  $effect(() => {
    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

<AppHeader />

<main class="app-main">
  {#if getPage() === "activity"}
    <ActivityFeed onSelectItem={handleActivitySelect} />
    {#if drawerItem}
      <DetailDrawer
        itemType={drawerItem.itemType}
        owner={drawerItem.owner}
        name={drawerItem.name}
        number={drawerItem.number}
        onClose={closeDrawer}
      />
    {/if}
  {:else if getPage() === "pulls"}
    {@const route = getRoute()}
    {#if route.page === "pulls" && route.view === "board"}
      <div class="board-layout">
        <KanbanBoard />
      </div>
    {:else}
      <div class="list-layout">
        <aside class="sidebar">
          <PullList />
        </aside>
        <section class="detail-area" class:detail-area--empty={getSelectedPR() === null}>
          {#if getSelectedPR() !== null}
            {@const sel = getSelectedPR()!}
            <PullDetail owner={sel.owner} name={sel.name} number={sel.number} />
          {:else}
            <div class="placeholder-content">
              <p class="placeholder-text">Select a PR</p>
              <p class="placeholder-hint">j/k to navigate · 1/2 to switch views</p>
            </div>
          {/if}
        </section>
      </div>
    {/if}
  {:else}
    <div class="list-layout">
      <aside class="sidebar">
        <IssueList />
      </aside>
      <section class="detail-area" class:detail-area--empty={getSelectedIssue() === null}>
        {#if getSelectedIssue() !== null}
          {@const sel = getSelectedIssue()!}
          <IssueDetail owner={sel.owner} name={sel.name} number={sel.number} />
        {:else}
          <div class="placeholder-content">
            <p class="placeholder-text">Select an issue</p>
            <p class="placeholder-hint">j/k to navigate</p>
          </div>
        {/if}
      </section>
    </div>
  {/if}
</main>

<StatusBar />

<style>
  .app-main {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    position: relative;
  }

  .list-layout {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .sidebar {
    width: 340px;
    flex-shrink: 0;
    background: var(--bg-surface);
    border-right: 1px solid var(--border-default);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .detail-area {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .detail-area--empty {
    align-items: center;
    justify-content: center;
  }

  .board-layout {
    flex: 1;
    overflow: hidden;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .placeholder-content {
    text-align: center;
  }

  .placeholder-text {
    color: var(--text-muted);
    font-size: 13px;
  }

  .placeholder-hint {
    color: var(--text-muted);
    font-size: 11px;
    margin-top: 8px;
    opacity: 0.7;
  }
</style>
```

- [ ] **Step 2: Verify frontend compiles**

Run: `cd /Users/wesm/code/middleman/frontend && npx tsc --noEmit`
Expected: no errors (or only pre-existing ones)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.svelte
git commit -m "Integrate activity feed and URL routing in App.svelte"
```

---

### Task 12: Decouple detail store from list stores

**Files:**
- Modify: `frontend/src/lib/stores/detail.svelte.ts`

- [ ] **Step 1: Make list refresh conditional on current route**

Replace the entire contents of `frontend/src/lib/stores/detail.svelte.ts`:

```ts
import { getPull, postComment, setKanbanState, setStarred, unsetStarred } from "../api/client.js";
import type { KanbanStatus, PullDetail } from "../api/types.js";
import { getPage } from "./router.svelte.js";
import { loadPulls } from "./pulls.svelte.js";

// --- state ---

let detail = $state<PullDetail | null>(null);
let loading = $state(false);
let error = $state<string | null>(null);

// --- reads ---

export function getDetail(): PullDetail | null {
  return detail;
}

export function isDetailLoading(): boolean {
  return loading;
}

export function getDetailError(): string | null {
  return error;
}

// --- writes ---

export function clearDetail(): void {
  detail = null;
  error = null;
}

export async function loadDetail(owner: string, name: string, number: number): Promise<void> {
  loading = true;
  error = null;
  try {
    detail = await getPull(owner, name, number);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}

/** Refreshes the pulls list only when the pulls list view is active. */
async function refreshPullsIfActive(): Promise<void> {
  if (getPage() === "pulls") {
    await loadPulls();
  }
}

/** Optimistically updates the kanban state, then refreshes the pulls list. */
export async function updateKanbanState(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): Promise<void> {
  // Optimistic update on the cached detail.
  if (detail !== null) {
    detail = {
      ...detail,
      pull_request: { ...detail.pull_request, KanbanStatus: status },
    };
  }
  try {
    await setKanbanState(owner, name, number, status);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    // Reload to restore accurate server state on failure.
    await loadDetail(owner, name, number);
    return;
  }
  await refreshPullsIfActive();
}

// --- polling ---

let detailPollHandle: ReturnType<typeof setInterval> | null = null;

async function refreshDetail(owner: string, name: string, number: number): Promise<void> {
  try {
    detail = await getPull(owner, name, number);
  } catch {
    // Silent refresh - don't overwrite error state
  }
}

export function startDetailPolling(owner: string, name: string, number: number): void {
  stopDetailPolling();
  detailPollHandle = setInterval(() => {
    void refreshDetail(owner, name, number);
  }, 60_000);
}

export function stopDetailPolling(): void {
  if (detailPollHandle !== null) {
    clearInterval(detailPollHandle);
    detailPollHandle = null;
  }
}

export async function toggleDetailPRStar(
  owner: string,
  name: string,
  number: number,
  currentlyStarred: boolean,
): Promise<void> {
  // Optimistic update
  if (detail !== null) {
    detail = { ...detail, pull_request: { ...detail.pull_request, Starred: !currentlyStarred } };
  }
  try {
    if (currentlyStarred) await unsetStarred("pr", owner, name, number);
    else await setStarred("pr", owner, name, number);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    if (detail !== null) {
      detail = { ...detail, pull_request: { ...detail.pull_request, Starred: currentlyStarred } };
    }
    return;
  }
  await refreshPullsIfActive();
}

/** Posts a comment to GitHub, then reloads the detail to show the new event. */
export async function submitComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<void> {
  error = null;
  try {
    await postComment(owner, name, number, body);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    return;
  }
  await loadDetail(owner, name, number);
}
```

- [ ] **Step 2: Add selectIssue export to issues store**

The `App.svelte` imports `selectIssue` from the issues store. Check if it exists — if the issues store only has `selectIssue` as a non-exported function or missing, add it. Open `frontend/src/lib/stores/issues.svelte.ts` and ensure `selectIssue` is exported. If the store uses a different name (like the pattern in pulls.svelte.ts), add the matching export:

In `frontend/src/lib/stores/issues.svelte.ts`, find the function that sets the selected issue (similar to `selectPR` in pulls store). If it doesn't exist, add it after the existing selection-related functions:

```ts
export function selectIssue(owner: string, name: string, number: number): void {
  selectedIssue = { owner, name, number };
}
```

- [ ] **Step 3: Verify frontend compiles**

Run: `cd /Users/wesm/code/middleman/frontend && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Run Go tests**

Run: `cd /Users/wesm/code/middleman && go test ./... -short`
Expected: all PASS

- [ ] **Step 5: Build the full project**

Run: `cd /Users/wesm/code/middleman && make build`
Expected: binary builds successfully

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/stores/detail.svelte.ts frontend/src/lib/stores/issues.svelte.ts
git commit -m "Decouple detail store from list stores for drawer use"
```

---

### Task 13: SPA fallback for URL routes

**Files:**
- Verify: `internal/server/server.go:49-66`

- [ ] **Step 1: Verify SPA fallback handles new routes**

The existing server code at `internal/server/server.go:49-66` already serves `index.html` for any path that doesn't match a static file. This means routes like `/pulls`, `/issues`, `/pulls/apache/arrow/42` will correctly serve the SPA, which then parses the URL client-side. No changes needed — just verify by reading the code.

- [ ] **Step 2: Build and do a manual smoke test**

Run: `cd /Users/wesm/code/middleman && make build`

Start the server and verify:
- `/` shows the activity feed
- `/pulls` shows the PR list
- `/issues` shows the issue list
- Browser back/forward works
- Clicking an activity row opens the drawer

- [ ] **Step 3: Final commit if any adjustments were needed**

```bash
git add -A
git commit -m "Final adjustments for activity feed integration"
```
