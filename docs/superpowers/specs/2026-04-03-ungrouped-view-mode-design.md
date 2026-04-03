# Ungrouped View Mode (Issue #53)

## Overview

Add a shared toggle to switch all three list views (PRs, Issues, Activity Threaded) between repo-grouped and flat/ungrouped modes. When ungrouped, items appear in a single chronological list across all repos, with a colored repo pill badge on each item.

## Toggle Control

- Segmented control ("By Repo" / "All") in each pane's toolbar — filter bar for PR/Issue lists, controls bar for Activity.
- Shared state — toggling in one pane changes all three views.
- Persisted in `localStorage`, defaults to "By Repo".

## Ungrouped Mode Behavior

### PR & Issue Lists

- Flat list sorted by `last_activity_at DESC` (API already returns this order).
- Repo group headers removed.
- Each item gets a colored repo pill badge in the meta row: `[middleman] #52 · jdoe`.
- Keyboard navigation (j/k) walks the flat list without repo boundaries.

### Activity Threaded

- Remove repo-level grouping; threads still grouped by PR/issue.
- Grouping key must be `(repo_owner, repo_name, item_type, item_number)` — not just `item_type:item_number` — to prevent cross-repo collisions (e.g., PR #53 in two different repos must remain separate threads).
- Each item-row gets a repo pill badge (same style) between the type badge and ref number.
- Sorted by latest event time across all repos.

### Activity Flat

- Unchanged — already ungrouped with a repo column.
- The "By Repo / All" toggle is hidden when the activity view is in flat mode, since flat mode is inherently ungrouped. The toggle only appears when threaded mode is active.

## Repo Pill Badge

- 9px font, font-weight 600, border-radius 8px, padding 1px 5px.
- Background: `color-mix(in srgb, <repo-color> 15%, transparent)`.
- Color derived from repo name (hash to one of the existing accent colors).
- Only rendered when in ungrouped ("All") mode.
- Max-width 80px with `overflow: hidden; text-overflow: ellipsis; white-space: nowrap` to prevent long repo names from squeezing the author and number text.
- The meta-left container (`#number · author`) also gets `overflow: hidden; text-overflow: ellipsis` so the row degrades gracefully at narrow widths — the right-side cluster (star, status badge, time) is `flex-shrink: 0` and always visible.

## State Management

- New shared store value (`groupByRepo: boolean`) accessible from all views.
- Reads from `localStorage` on init, writes on change.
- Existing `pullsByRepo()` / `issuesByRepo()` functions stay unchanged.
- Components conditionally use grouped maps or flat arrays based on toggle state.

## Files Changed

| File | Change |
|------|--------|
| `frontend/src/lib/stores/` | New shared grouping store |
| `sidebar/PullList.svelte` | Toggle control, conditional grouped/flat rendering |
| `sidebar/IssueList.svelte` | Same as PullList |
| `sidebar/PullItem.svelte` | Conditional repo pill badge |
| `sidebar/IssueItem.svelte` | Conditional repo pill badge |
| `ActivityFeed.svelte` | Toggle control in controls bar |
| `ActivityThreaded.svelte` | Conditional repo-level grouping, repo badge on item rows |
| `stores/pulls.svelte.ts` | Expose flat sorted list alongside `pullsByRepo()`; update `selectNextPR`/`selectPrevPR` navigation helpers to walk the flat list when ungrouped |
| `stores/issues.svelte.ts` | Same as pulls — flat list + updated navigation helpers |

No backend changes — APIs already return flat data with repo owner/name fields.

## Testing

- **E2E: grouped vs ungrouped rendering** — verify PR list renders repo headers in "By Repo" mode and a flat list with repo badges in "All" mode. Same for issue list.
- **E2E: activity threaded ungrouped** — verify threads from different repos with the same PR number remain separate in ungrouped mode.
- **E2E: toggle persistence** — set toggle to "All", reload page, verify it restores to "All".
- **E2E: toggle syncs across views** — toggle in PR list, switch to issues, verify toggle state matches.
- **E2E: j/k navigation** — verify j/k walks the flat chronological list in ungrouped mode and the repo-grouped order in "By Repo" mode.
