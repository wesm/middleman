# Threaded Activity View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a threaded/grouped view toggle for the activity feed (Project > Issue/PR > Event hierarchy) and replace limit-based pagination with time-window fetching for both views.

**Architecture:** Add `since` parameter to the existing activity API (time-window instead of limit/cursor). Frontend gets a time-range selector (24h/7d/30d/90d), a Flat/Threaded view toggle, and a new `ActivityThreaded` component that groups the same flat item array into a nested tree. All grouping is client-side — no backend query changes.

**Tech Stack:** Go (HTTP handler param changes), Svelte 5 (runes, components), TypeScript

---

## File Structure

**New files:**
- `frontend/src/lib/components/ActivityThreaded.svelte` — threaded view component

**Modified files:**
- `internal/db/types.go` — add `Since` field to `ListActivityOpts`
- `internal/db/queries_activity.go` — apply `Since` as `WHERE created_at >= ?`
- `internal/db/queries_activity_test.go` — add test for `Since` filter
- `internal/server/handlers_activity.go` — add `since` param, remove `before`/`limit`, use 5001 limit for cap detection, return `capped` instead of `has_more`
- `frontend/src/lib/api/activity.ts` — add `since`, remove `before`, change response shape
- `frontend/src/lib/stores/activity.svelte.ts` — time-window loading, `viewMode`/`timeRange` state, window pruning, remove `loadMoreActivity`
- `frontend/src/lib/components/ActivityFeed.svelte` — add Flat/Threaded toggle, time range control, remove "Load more", show capped notice, conditional rendering

---

### Task 1: Add `Since` to Go types and query

**Files:**
- Modify: `internal/db/types.go:155-168`
- Modify: `internal/db/queries_activity.go:15-70`
- Modify: `internal/db/queries_activity_test.go`

- [ ] **Step 1: Add `Since` field to `ListActivityOpts`**

In `internal/db/types.go`, add a `Since` field to `ListActivityOpts` after the `Limit` field:

```go
type ListActivityOpts struct {
	Repo   string   // "owner/name" filter
	Types  []string // activity type filter
	Search string   // title/body search
	Limit  int      // page size (default 50, max 200)
	Since  *time.Time // only return events created at or after this time
	// Cursor fields -- decoded from opaque token by the handler.
	BeforeTime     *time.Time
	BeforeSource   string
	BeforeSourceID int64
	AfterTime      *time.Time
	AfterSource    string
	AfterSourceID  int64
}
```

- [ ] **Step 2: Apply `Since` filter in `ListActivity`**

In `internal/db/queries_activity.go`, add a `Since` WHERE clause after the existing Search filter block (after the `if opts.Search != ""` block, around line 48):

```go
	// Time window filter.
	if opts.Since != nil {
		whereClauses = append(whereClauses, "created_at >= ?")
		args = append(args, *opts.Since)
	}
```

- [ ] **Step 3: Write test for `Since` filter**

In `internal/db/queries_activity_test.go`, add a new subtest inside `TestListActivity` (after the existing `after cursor for polling` subtest):

```go
	t.Run("since time window", func(t *testing.T) {
		since := base.Add(4 * time.Minute)
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Limit: 50,
			Since: &since,
		})
		if err != nil {
			t.Fatalf("ListActivity since: %v", err)
		}
		for _, it := range items {
			if it.CreatedAt.Before(since) {
				t.Errorf("item %s:%d has created_at %v before since %v",
					it.Source, it.SourceID, it.CreatedAt, since)
			}
		}
		// base+4m is review, base+5m is commit, base+6m is review_comment (excluded),
		// base+7m is issue comment, base+10m is the comment-new from after cursor test.
		// So we expect: comment-new(base+10m), issue comment(base+7m), commit(base+5m), review(base+4m) = 4 items
		if len(items) != 4 {
			t.Fatalf("expected 4 items since base+4m, got %d", len(items))
		}
	})
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/wesm/code/middleman && go test ./internal/db/ -run TestListActivity -v`
Expected: all subtests PASS including the new `since time window` subtest

- [ ] **Step 5: Commit**

```bash
git add internal/db/types.go internal/db/queries_activity.go internal/db/queries_activity_test.go
git commit -m "Add Since time-window filter to ListActivity query"
```

---

### Task 2: Update API handler for time-window fetching

**Files:**
- Modify: `internal/server/handlers_activity.go`

- [ ] **Step 1: Rewrite the handler**

Replace the entire contents of `internal/server/handlers_activity.go`:

```go
package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/db"
)

const activitySafetyCap = 5000

type activityResponse struct {
	Items  []activityItemResponse `json:"items"`
	Capped bool                   `json:"capped"`
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
	ItemState    string `json:"item_state"`
	Author       string `json:"author"`
	CreatedAt    string `json:"created_at"`
	BodyPreview  string `json:"body_preview"`
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	opts := db.ListActivityOpts{
		Repo:   q.Get("repo"),
		Search: q.Get("search"),
		Limit:  activitySafetyCap + 1,
	}

	if types := q.Get("types"); types != "" {
		opts.Types = strings.Split(types, ",")
	}

	if since := q.Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since: "+err.Error())
			return
		}
		opts.Since = &t
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

	capped := len(items) > activitySafetyCap
	if capped {
		items = items[:activitySafetyCap]
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
			ItemState:    it.ItemState,
			Author:       it.Author,
			CreatedAt:    it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			BodyPreview:  it.BodyPreview,
		}
	}

	writeJSON(w, http.StatusOK, activityResponse{Items: out, Capped: capped})
}
```

Key changes from old handler:
- `since` param parsed as RFC3339, sets `opts.Since`
- `before` and `limit` params removed
- `opts.Limit` always set to `activitySafetyCap + 1` (5001)
- Response has `capped` boolean instead of `has_more`

- [ ] **Step 2: Run tests**

Run: `cd /Users/wesm/code/middleman && go test ./... -short`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/handlers_activity.go
git commit -m "Switch activity handler to time-window fetching with safety cap"
```

---

### Task 3: Update frontend API types

**Files:**
- Modify: `frontend/src/lib/api/activity.ts`

- [ ] **Step 1: Rewrite activity.ts**

Replace the entire contents of `frontend/src/lib/api/activity.ts`:

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
  item_state: "open" | "merged" | "closed";
  author: string;
  created_at: string;
  body_preview: string;
}

export interface ActivityResponse {
  items: ActivityItem[];
  capped: boolean;
}

export interface ActivityParams {
  repo?: string;
  types?: string[];
  search?: string;
  since?: string;
  after?: string;
}

export async function listActivity(params?: ActivityParams): Promise<ActivityResponse> {
  const sp = new URLSearchParams();
  if (params?.repo) sp.set("repo", params.repo);
  if (params?.types && params.types.length > 0) sp.set("types", params.types.join(","));
  if (params?.search) sp.set("search", params.search);
  if (params?.since) sp.set("since", params.since);
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

Changes: removed `before`, `limit` from `ActivityParams`. Changed `has_more` to `capped` in `ActivityResponse`. Added `since`.

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/api/activity.ts
git commit -m "Update activity API types: since param, capped response"
```

---

### Task 4: Rewrite activity store for time-window fetching

**Files:**
- Modify: `frontend/src/lib/stores/activity.svelte.ts`

- [ ] **Step 1: Rewrite the store**

Replace the entire contents of `frontend/src/lib/stores/activity.svelte.ts`:

```ts
import { listActivity } from "../api/activity.js";
import type { ActivityItem, ActivityParams } from "../api/activity.js";

// --- constants ---

export type TimeRange = "24h" | "7d" | "30d" | "90d";
export type ViewMode = "flat" | "threaded";

const RANGE_MS: Record<TimeRange, number> = {
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
  "90d": 90 * 24 * 60 * 60 * 1000,
};

// --- state ---

let items = $state<ActivityItem[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let capped = $state(false);
let filterRepo = $state<string | undefined>(undefined);
let filterTypes = $state<string[]>([]);
let searchQuery = $state<string | undefined>(undefined);
let timeRange = $state<TimeRange>("7d");
let viewMode = $state<ViewMode>("flat");
let pollHandle: ReturnType<typeof setInterval> | null = null;
let requestVersion = 0;

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

export function isActivityCapped(): boolean {
  return capped;
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

export function getTimeRange(): TimeRange {
  return timeRange;
}

export function getViewMode(): ViewMode {
  return viewMode;
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

export function setTimeRange(range: TimeRange): void {
  timeRange = range;
}

export function setViewMode(mode: ViewMode): void {
  viewMode = mode;
}

function computeSince(): string {
  return new Date(Date.now() - RANGE_MS[timeRange]).toISOString();
}

function buildParams(): ActivityParams {
  const p: ActivityParams = { since: computeSince() };
  if (filterRepo) p.repo = filterRepo;
  if (filterTypes.length > 0) p.types = filterTypes;
  if (searchQuery) p.search = searchQuery;
  return p;
}

/** Load the full time window from scratch. */
export async function loadActivity(): Promise<void> {
  const version = ++requestVersion;
  loading = true;
  error = null;
  try {
    const resp = await listActivity(buildParams());
    if (version !== requestVersion) return;
    items = resp.items;
    capped = resp.capped;
  } catch (err) {
    if (version !== requestVersion) return;
    error = err instanceof Error ? err.message : String(err);
  } finally {
    if (version === requestVersion) loading = false;
  }
}

/** Poll for new items since the newest displayed item. */
async function pollNewItems(): Promise<void> {
  if (items.length === 0) {
    await loadActivity();
    return;
  }
  try {
    const params = buildParams();
    params.after = items[0]!.cursor;
    const resp = await listActivity(params);
    if (resp.capped) {
      // Too many new items — full reload.
      await loadActivity();
      return;
    }
    if (resp.items.length > 0) {
      const existingIds = new Set(items.map((it) => it.id));
      const newItems = resp.items.filter((it) => !existingIds.has(it.id));
      if (newItems.length > 0) {
        items = [...newItems, ...items];
      }
    }
  } catch {
    // Silent poll failure
  }
  // Prune items older than the current window.
  const cutoff = new Date(Date.now() - RANGE_MS[timeRange]);
  items = items.filter((it) => new Date(it.created_at) >= cutoff);
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

/** Sync URL query params → store state. Called on mount. */
export function syncFromURL(): void {
  const sp = new URLSearchParams(window.location.search);
  filterRepo = sp.get("repo") ?? undefined;
  const typesParam = sp.get("types");
  filterTypes = typesParam ? typesParam.split(",") : [];
  searchQuery = sp.get("search") ?? undefined;
  const rangeParam = sp.get("range");
  if (rangeParam && rangeParam in RANGE_MS) {
    timeRange = rangeParam as TimeRange;
  }
  const viewParam = sp.get("view");
  if (viewParam === "flat" || viewParam === "threaded") {
    viewMode = viewParam;
  }
}

/** Sync store state → URL query params (replaceState). */
export function syncToURL(): void {
  const sp = new URLSearchParams(window.location.search);
  if (filterRepo) sp.set("repo", filterRepo);
  else sp.delete("repo");
  if (filterTypes.length > 0) sp.set("types", filterTypes.join(","));
  else sp.delete("types");
  if (searchQuery) sp.set("search", searchQuery);
  else sp.delete("search");
  if (timeRange !== "7d") sp.set("range", timeRange);
  else sp.delete("range");
  if (viewMode !== "flat") sp.set("view", viewMode);
  else sp.delete("view");
  const qs = sp.toString();
  const url = "/" + (qs ? `?${qs}` : "");
  history.replaceState(null, "", url);
}
```

Key changes:
- `loadMoreActivity` removed entirely
- `hasMore`/`hasMoreActivity` replaced with `capped`/`isActivityCapped`
- `timeRange` and `viewMode` state added
- `buildParams` uses `computeSince()` instead of `limit: 50`
- Polling prunes items older than the time window
- Poll overflow uses `capped` instead of `has_more`
- `syncFromURL`/`syncToURL` handle `range` and `view` params

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/stores/activity.svelte.ts
git commit -m "Rewrite activity store for time-window fetching"
```

---

### Task 5: Create ActivityThreaded component

**Files:**
- Create: `frontend/src/lib/components/ActivityThreaded.svelte`

- [ ] **Step 1: Create the threaded view component**

Create `frontend/src/lib/components/ActivityThreaded.svelte`:

```svelte
<script lang="ts">
  import type { ActivityItem } from "../api/activity.js";

  interface Props {
    items: ActivityItem[];
    onSelectItem?: (item: ActivityItem) => void;
  }

  let { items, onSelectItem }: Props = $props();

  interface ItemGroup {
    itemType: string;
    itemNumber: number;
    itemTitle: string;
    itemUrl: string;
    itemState: string;
    repoOwner: string;
    repoName: string;
    latestTime: string;
    events: ActivityItem[];
  }

  interface RepoGroup {
    repo: string;
    itemCount: number;
    eventCount: number;
    latestTime: string;
    items: ItemGroup[];
  }

  const grouped = $derived.by(() => {
    const repoMap = new Map<string, Map<string, ActivityItem[]>>();

    for (const item of items) {
      const repoKey = `${item.repo_owner}/${item.repo_name}`;
      const itemKey = `${item.item_type}:${item.item_number}`;

      let itemMap = repoMap.get(repoKey);
      if (!itemMap) {
        itemMap = new Map();
        repoMap.set(repoKey, itemMap);
      }

      let events = itemMap.get(itemKey);
      if (!events) {
        events = [];
        itemMap.set(itemKey, events);
      }
      events.push(item);
    }

    const repoGroups: RepoGroup[] = [];

    for (const [repo, itemMap] of repoMap) {
      const itemGroups: ItemGroup[] = [];

      for (const [, events] of itemMap) {
        events.sort((a, b) =>
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

        const first = events[0]!;
        itemGroups.push({
          itemType: first.item_type,
          itemNumber: first.item_number,
          itemTitle: first.item_title,
          itemUrl: first.item_url,
          itemState: first.item_state,
          repoOwner: first.repo_owner,
          repoName: first.repo_name,
          latestTime: first.created_at,
          events,
        });
      }

      itemGroups.sort((a, b) =>
        new Date(b.latestTime).getTime() - new Date(a.latestTime).getTime());

      const allEvents = itemGroups.flatMap((g) => g.events);
      const latestTime = itemGroups[0]?.latestTime ?? "";

      repoGroups.push({
        repo,
        itemCount: itemGroups.length,
        eventCount: allEvents.length,
        latestTime,
        items: itemGroups,
      });
    }

    repoGroups.sort((a, b) =>
      new Date(b.latestTime).getTime() - new Date(a.latestTime).getTime());

    return repoGroups;
  });

  function eventLabel(type: string): string {
    switch (type) {
      case "new_pr": case "new_issue": return "Opened";
      case "comment": return "Comment";
      case "review": return "Review";
      case "commit": return "Commit";
      default: return type;
    }
  }

  function eventClass(type: string): string {
    switch (type) {
      case "comment": return "evt-comment";
      case "review": return "evt-review";
      case "commit": return "evt-commit";
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

  function handleItemClick(group: ItemGroup): void {
    if (group.events.length > 0) {
      onSelectItem?.(group.events[0]!);
    }
  }

  function handleEventClick(event: ActivityItem): void {
    onSelectItem?.(event);
  }
</script>

<div class="threaded-view">
  {#each grouped as repoGroup (repoGroup.repo)}
    <div class="repo-section">
      <div class="repo-header">
        <span class="repo-name">{repoGroup.repo}</span>
        <span class="repo-stats">{repoGroup.itemCount} items, {repoGroup.eventCount} events</span>
      </div>

      {#each repoGroup.items as itemGroup (`${itemGroup.itemType}:${itemGroup.itemNumber}`)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="item-row" onclick={() => handleItemClick(itemGroup)}>
          <span class="item-badge" class:badge-pr={itemGroup.itemType === "pr"} class:badge-issue={itemGroup.itemType === "issue"}>
            {itemGroup.itemType === "pr" ? "PR" : "Issue"}
          </span>
          {#if itemGroup.itemState === "merged"}
            <span class="state-tag state-merged">Merged</span>
          {:else if itemGroup.itemState === "closed"}
            <span class="state-tag state-closed">Closed</span>
          {/if}
          <span class="item-ref">#{itemGroup.itemNumber}</span>
          <span class="item-title">{itemGroup.itemTitle}</span>
          <span class="item-time">{relativeTime(itemGroup.latestTime)}</span>
        </div>

        {#each itemGroup.events as event (event.id)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="event-row" onclick={() => handleEventClick(event)}>
            <span class="event-type {eventClass(event.activity_type)}">{eventLabel(event.activity_type)}</span>
            <span class="event-author">{event.author}</span>
            <span class="event-time">{relativeTime(event.created_at)}</span>
          </div>
        {/each}
      {/each}
    </div>
  {/each}

  {#if grouped.length === 0}
    <div class="empty-state">No activity found</div>
  {/if}
</div>

<style>
  .threaded-view {
    flex: 1;
    overflow-y: auto;
    padding: 0 16px;
  }

  .repo-section {
    margin-bottom: 4px;
  }

  .repo-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0 4px;
    position: sticky;
    top: 0;
    background: var(--bg-primary);
    z-index: 2;
    border-bottom: 1px solid var(--border-default);
  }

  .repo-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .repo-stats {
    font-size: 10px;
    color: var(--text-muted);
  }

  .item-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 0 5px 24px;
    cursor: pointer;
    border-bottom: 1px solid var(--border-muted);
    transition: background 0.1s;
  }

  .item-row:hover {
    background: var(--bg-surface-hover);
  }

  .item-badge {
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    padding: 1px 4px;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .badge-pr {
    background: color-mix(in srgb, var(--accent-blue) 15%, transparent);
    color: var(--accent-blue);
  }
  .badge-issue {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }

  .state-tag {
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    padding: 1px 4px;
    border-radius: 3px;
    flex-shrink: 0;
  }
  .state-merged {
    background: color-mix(in srgb, var(--accent-purple) 20%, transparent);
    color: var(--accent-purple);
  }
  .state-closed {
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
    color: var(--accent-red);
  }

  .item-ref {
    font-size: 12px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .item-title {
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
  }

  .item-time {
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
    margin-left: auto;
  }

  .event-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 0 3px 48px;
    cursor: pointer;
    border-bottom: 1px solid var(--border-muted);
    border-left: 2px solid var(--border-muted);
    margin-left: 24px;
    transition: background 0.1s;
  }

  .event-row:hover {
    background: var(--bg-surface-hover);
  }

  .event-type {
    font-size: 11px;
    font-weight: 500;
    flex-shrink: 0;
    color: var(--text-secondary);
  }

  .event-type.evt-comment { color: var(--accent-amber); }
  .event-type.evt-review { color: var(--accent-green); }
  .event-type.evt-commit { color: var(--accent-teal); }

  .event-author {
    font-size: 11px;
    color: var(--text-secondary);
  }

  .event-time {
    font-size: 11px;
    color: var(--text-muted);
    margin-left: auto;
    flex-shrink: 0;
  }

  .empty-state {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/components/ActivityThreaded.svelte
git commit -m "Add ActivityThreaded component with grouped tree view"
```

---

### Task 6: Update ActivityFeed with view toggle, time range, and conditional rendering

**Files:**
- Modify: `frontend/src/lib/components/ActivityFeed.svelte`

This is the largest task. The changes:
1. Import new store functions (`getTimeRange`, `setTimeRange`, `getViewMode`, `setViewMode`, `isActivityCapped`) and `ActivityThreaded`
2. Remove `hasMoreActivity` and `loadMoreActivity` imports
3. Add Flat/Threaded toggle and time range selector to controls bar
4. Remove "Load more" button
5. Add capped notice
6. Conditionally render flat table or `ActivityThreaded`

- [ ] **Step 1: Update imports**

Replace the import block at the top of the `<script>` section:

```svelte
<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import type { ActivityItem } from "../api/activity.js";
  import {
    getActivityItems,
    isActivityLoading,
    getActivityError,
    isActivityCapped,
    getActivityFilterRepo,
    getActivityFilterTypes,
    getActivitySearch,
    getTimeRange,
    getViewMode,
    setActivityFilterRepo,
    setActivityFilterTypes,
    setActivitySearch,
    setTimeRange,
    setViewMode,
    loadActivity,
    startActivityPolling,
    stopActivityPolling,
    syncFromURL,
    syncToURL,
  } from "../stores/activity.svelte.js";
  import type { TimeRange, ViewMode } from "../stores/activity.svelte.js";
  import RepoTypeahead from "./RepoTypeahead.svelte";
  import ActivityThreaded from "./ActivityThreaded.svelte";
```

- [ ] **Step 2: Add time range and view mode handlers**

After the existing `handleRepoChange` function, add:

```ts
  function handleTimeRangeChange(range: TimeRange): void {
    setTimeRange(range);
    syncToURL();
    void loadActivity();
  }

  function handleViewModeChange(mode: ViewMode): void {
    setViewMode(mode);
    syncToURL();
  }

  const TIME_RANGES: { value: TimeRange; label: string }[] = [
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
    { value: "90d", label: "90d" },
  ];
```

- [ ] **Step 3: Update controls bar in the template**

In the controls bar, after the All/PRs/Issues segmented control and before the filter-wrap, add the Flat/Threaded toggle and time range selector:

```svelte
      <div class="segmented-control">
        <button class="seg-btn" class:active={getViewMode() === "flat"} onclick={() => handleViewModeChange("flat")}>Flat</button>
        <button class="seg-btn" class:active={getViewMode() === "threaded"} onclick={() => handleViewModeChange("threaded")}>Threaded</button>
      </div>

      <div class="segmented-control">
        {#each TIME_RANGES as r}
          <button class="seg-btn" class:active={getTimeRange() === r.value} onclick={() => handleTimeRangeChange(r.value)}>{r.label}</button>
        {/each}
      </div>
```

- [ ] **Step 4: Replace table + load-more with conditional rendering**

Replace the `<div class="table-container">` through the end of the load-more section with:

```svelte
  {#if getViewMode() === "threaded"}
    <ActivityThreaded items={displayItems} onSelectItem={onSelectItem} />
  {:else}
    <div class="table-container">
      <table class="activity-table">
        <!-- existing thead and tbody unchanged -->
      </table>

      {#if displayItems.length === 0 && !isActivityLoading()}
        <div class="empty-state">No activity found</div>
      {/if}
    </div>
  {/if}

  {#if isActivityCapped()}
    <div class="capped-notice">
      Showing most recent 5,000 events. Narrow the time range or use filters to see more.
    </div>
  {/if}
```

- [ ] **Step 5: Remove the load-more section and its CSS**

Delete the `{#if hasMoreActivity()}` block and the `.load-more` / `.load-more-btn` CSS rules.

- [ ] **Step 6: Add CSS for capped notice**

```css
  .capped-notice {
    padding: 6px 16px;
    font-size: 11px;
    color: var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 8%, transparent);
    border-top: 1px solid var(--border-default);
    text-align: center;
    flex-shrink: 0;
  }
```

- [ ] **Step 7: Build and verify**

Run: `cd /Users/wesm/code/middleman/frontend && npm run build`
Expected: builds with no errors

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/components/ActivityFeed.svelte
git commit -m "Add Flat/Threaded toggle, time range selector, capped notice"
```

---

### Task 7: Build and smoke test

- [ ] **Step 1: Run Go tests**

Run: `cd /Users/wesm/code/middleman && go test ./... -short`
Expected: all PASS

- [ ] **Step 2: Build the full project**

Run: `cd /Users/wesm/code/middleman/frontend && npm install && npm run build && cd .. && rm -rf internal/web/dist && cp -r frontend/dist internal/web/dist && go build -o middleman ./cmd/middleman`
Expected: binary builds successfully

- [ ] **Step 3: Final commit if any adjustments needed**

```bash
git add -A
git commit -m "Final adjustments for threaded activity view"
```
