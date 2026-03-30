# Threaded Activity View Design

A grouped/threaded view option for the activity feed that shows events organized by Project > Issue/PR > Event, sorted reverse-chronologically at every level. Coexists with the existing flat view as a toggle. Replaces limit-based pagination with time-window fetching for both views.

## View Toggle

A Flat/Threaded segmented control in the activity feed controls bar. Flat is the current table view. Threaded groups the same data into a nested tree. Both views render the same `displayItems` array (already filtered by type, bots, closed/merged). Toggling is instant with no re-fetch.

The toggle is local UI state, not persisted in the URL.

## Time-Windowed Fetching

Replaces the current limit/cursor pagination model.

### API change

Add a `since` parameter to `GET /api/v1/activity` — an ISO 8601 timestamp. The server returns all events with `created_at >= since`. A server-side safety cap (default 5000 rows) prevents unbounded responses.

Remove the `before` HTTP query parameter from the handler. The `after` cursor is retained for polling (prepending new items). The internal `BeforeTime`/`BeforeSource`/`BeforeSourceID` fields on `ListActivityOpts` remain in the code but are no longer used by the handler.

### Time range control

Preset time windows displayed as a segmented control at the top of the table: **24h / 7d / 30d / 90d**. Default is 7d. Selecting a range re-fetches with the new `since` value. No "Load more" button.

### Polling

Unchanged. Uses the `after` cursor of the newest displayed item to prepend new events on a 15-second interval.

## Threaded View Structure

Three visual nesting levels rendered from the `displayItems` array. No collapse/expand — all levels always visible.

### Level 1 — Project header

`repo_owner/repo_name` as a sticky section header. Shows count of active items and total events in the window. Sorted by most recent event in the group, descending.

### Level 2 — Issue/PR row (indented)

`#number title` with PR/Issue badge, state badge (Merged/Closed/Open), author, and time of most recent event. One row per unique item within the project. Sorted by most recent event, descending. Clickable — opens the detail drawer.

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
- `internal/server/handlers_activity.go` — add `since` param (ISO timestamp), apply 5000-row safety cap, remove `before` cursor support
- `internal/db/queries_activity.go` — add `Since *time.Time` to `ListActivityOpts`, apply as `WHERE created_at >= ?`
- `internal/db/types.go` — add `Since` field to `ListActivityOpts`
- `frontend/src/lib/api/activity.ts` — add `since` to `ActivityParams`, remove `before`
- `frontend/src/lib/stores/activity.svelte.ts` — replace limit-based loading with time-window, add `timeRange` state, remove `loadMoreActivity`
- `frontend/src/lib/components/ActivityFeed.svelte` — add Flat/Threaded toggle, time range control, remove "Load more" button, conditionally render flat table or `ActivityThreaded`

**New:**
- `frontend/src/lib/components/ActivityThreaded.svelte` — threaded view component: takes `displayItems` as prop, performs grouping, renders three-level nested tree
