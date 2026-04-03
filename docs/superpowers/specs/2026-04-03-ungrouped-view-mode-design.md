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
- Each item-row gets a repo pill badge (same style) between the type badge and ref number.
- Sorted by latest event time across all repos.

### Activity Flat

- Unchanged — already ungrouped with a repo column.

## Repo Pill Badge

- 9px font, font-weight 600, border-radius 8px, padding 1px 5px.
- Background: `color-mix(in srgb, <repo-color> 15%, transparent)`.
- Color derived from repo name (hash to one of the existing accent colors).
- Only rendered when in ungrouped ("All") mode.

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
| `stores/pulls.svelte.ts` | Expose flat sorted list alongside `pullsByRepo()` |
| `stores/issues.svelte.ts` | Same as pulls |

No backend changes — APIs already return flat data with repo owner/name fields.
