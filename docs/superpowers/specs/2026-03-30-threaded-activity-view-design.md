# Threaded Activity View Design

A grouped/threaded view option for the activity feed that shows events organized by Project > Issue/PR > Event, sorted reverse-chronologically at every level. Coexists with the existing flat view as a toggle.

This spec also changes how both views fetch data: time-window fetching replaces the current limit/cursor pagination model. This is intentional — the flat view's "Load more" button and cursor-based pagination are removed in favor of a time-range selector. Both views benefit from seeing a complete time window rather than an arbitrary page of events.

## View Toggle

A Flat/Threaded segmented control in the activity feed controls bar. Flat is the current table view. Threaded groups the same data into a nested tree. Both views render the same `displayItems` array (already filtered by type, bots, closed/merged). Toggling is instant with no re-fetch.

The view mode and time range are persisted in the URL query string (`view=threaded&range=7d`) alongside the existing `repo`, `types`, and `search` params. This makes the current view bookmarkable and survives browser back/forward.

### URL and history model

All activity feed state changes (view mode, time range, repo filter, type filter, search) use `replaceState` — they update the URL without creating history entries. This is the same model used today for filters. Navigating away from the activity feed (e.g., clicking a PR tab) uses `pushState` as it does now, so browser back returns to the activity feed with the URL-encoded state intact. On `popstate`, the store re-reads all query params and re-fetches if the route is `/`.

## Time-Windowed Fetching

### API change

Replace the `limit`/`before` pagination model with time-window fetching.

Add a `since` parameter to `GET /api/v1/activity` — an ISO 8601 timestamp. The server returns all events with `created_at >= since`. Remove the `before` and `limit` HTTP query parameters from the handler. The `after` cursor is retained for polling.

The server applies a safety cap of 5000 rows. If the result set exceeds this, the response includes `"capped": true` so the frontend can inform the user (e.g., "Showing most recent 5000 events in this window").

The `has_more` field is removed from the response. The new response shape:

```json
{
  "items": [...],
  "capped": false
}
```

The handler sets `opts.Limit = 5001` (one more than the cap) to implement the safety cap using the existing `ListActivity` limit logic. If the query returns 5001 rows, the handler truncates to 5000 and sets `capped: true`. The `ListActivity` function's existing `if limit <= 0 { limit = 50 }` default is only a fallback — the handler always provides a value.

The `BeforeTime`/`BeforeSource`/`BeforeSourceID` fields on `ListActivityOpts` remain in the code but are no longer set by the handler. The `AfterTime`/`AfterSource`/`AfterSourceID` fields remain for polling.

### Client-side filter interaction with safety cap

The `hide bots` and `hide closed/merged` filters are client-side. In a busy window the 5000-row cap could cut off rows before client filtering, meaning the user sees fewer visible items than actually exist. This is acceptable because:
- 5000 rows in a 7-day window is a very high bar (714 events/day across all repos).
- When `capped` is true, the frontend shows a notice so the user knows the window is incomplete.
- The user can narrow the time range (24h) or use the server-side repo/type filters to reduce the result set.

### Time range control

Preset time windows displayed as a segmented control: **24h / 7d / 30d / 90d**. Default is 7d. Selecting a range computes `since` as `now - range` and re-fetches. No "Load more" button.

### Polling

Polling prepends new items using the `after` cursor. The poll request also passes `since` so the server applies the same time window. On each poll tick, the store also prunes items whose `created_at` is older than the current window's `since` boundary (recomputed from `now - range`). This keeps the view accurate as time passes.

If a poll response has `capped: true`, the client discards the poll result and does a full reload from scratch. This replaces the old `has_more`-based overflow detection — the semantics are the same (too many new items to merge incrementally, so start over).

## Threaded View Structure

Three visual nesting levels rendered from the `displayItems` array. No collapse/expand — all levels always visible.

### Level 1 — Project header

`repo_owner/repo_name` as a sticky section header. Shows count of active items and total events in the window. Sorted by most recent event in the group, descending.

### Level 2 — Issue/PR row (indented)

`#number title` with PR/Issue badge, state badge (Merged/Closed/Open), and time of most recent event. No author on the group header — when the item's `new_pr`/`new_issue` event is outside the time window, there is no reliable item-level author in the data. The event rows below already show per-event authors, which is more useful in context. One row per unique item within the project. Sorted by most recent event, descending. Clickable — opens the detail drawer.

### Level 3 — Event row (further indented)

Compact one-liner: event type (colored label), author, relative time. Sorted by `created_at` descending within the item group. Clickable — opens the detail drawer for the parent issue/PR.

### Visual nesting

Left-margin indentation at 0px / 24px / 48px. Subtle left border connecting events to their parent item for visual grouping.

## Grouping Logic

Client-side only. No backend changes to the query shape.

1. Take `displayItems` (already filtered by type/bots/closed).
2. Group by `repo_owner + '/' + repo_name` into project groups.
3. Within each project group, sub-group by `item_type + ':' + item_number`.
4. Sort project groups by the `created_at` of their most recent event, descending.
5. Sort item sub-groups by the `created_at` of their most recent event, descending.
6. Sort events within each sub-group by `created_at` descending.

## Controls and Filters

All existing filters carry forward unchanged. They operate on the flat item array before grouping.

Controls bar, left to right:
1. **Repo typeahead** — unchanged
2. **All / PRs / Issues** segmented control — unchanged
3. **Flat / Threaded** segmented control — new
4. **Time range** segmented control (24h / 7d / 30d / 90d) — new, replaces "Load more"
5. **Filter dropdown** — unchanged (event types, hide closed, hide bots)
6. **Search** — unchanged

## Interactions

- Clicking any row in the threaded view (Level 2 item header or Level 3 event) opens the detail drawer for the parent issue/PR, same as the flat view.
- Drawer behavior is identical to the flat view: slide-out panel, Escape to close, URL state preserved.

## File Changes

**Modified:**
- `internal/server/handlers_activity.go` — add `since` param, remove `before`/`limit`, apply 5000-row safety cap, return `capped` instead of `has_more`
- `internal/db/queries_activity.go` — add `Since *time.Time` to `ListActivityOpts`, apply as `WHERE created_at >= ?`
- `internal/db/types.go` — add `Since` field to `ListActivityOpts`
- `frontend/src/lib/api/activity.ts` — add `since` to `ActivityParams`, remove `before`, change response shape (`capped` replaces `has_more`)
- `frontend/src/lib/stores/activity.svelte.ts` — replace limit-based loading with time-window, add `timeRange` and `viewMode` state, remove `loadMoreActivity`, add window pruning to poll tick, persist view/range to URL
- `frontend/src/lib/components/ActivityFeed.svelte` — add Flat/Threaded toggle, time range control, remove "Load more" button, show capped notice, conditionally render flat table or `ActivityThreaded`

**New:**
- `frontend/src/lib/components/ActivityThreaded.svelte` — threaded view component: takes `displayItems` and `onSelectItem` as props, performs grouping, renders three-level nested tree
