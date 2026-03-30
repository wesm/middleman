# Activity Feed Design

A compact, full-width tabular view showing all activity across monitored repositories in reverse chronological order. The feed surfaces activity on items the app has synced — it complements GitHub notifications rather than fully replacing them. Items opened and closed between sync intervals, or activity on items after they leave the sync window, will not appear.

## Event Types

Five event types unified from existing data — no new sync work required.

| Type | Source | Derived from |
|------|--------|--------------|
| `new_pr` | `pull_requests` | PR row `created_at` |
| `new_issue` | `issues` | Issue row `created_at` |
| `comment` | `pr_events` / `issue_events` | `event_type = 'issue_comment'` |
| `review` | `pr_events` | `event_type = 'review'` |
| `commit` | `pr_events` | `event_type = 'commit'` |

The `review_comment` event type (inline code comments on PRs) is excluded — reviews already capture the approval/changes-requested signal, and inline comments are noisy in a feed context. Can be added later if needed.

No new tables. No changes to the sync engine.

### Sync coverage limitations

The sync engine only fetches open PRs/issues and their timelines. This means:
- Activity on items that were opened and closed/merged between two sync intervals may never be captured.
- Activity that occurs after an item is closed/merged will not be synced.
- The feed is a view over what the app already knows, not a complete audit log.

These are acceptable limitations for v1. Expanding sync coverage (e.g., fetching recent closed items or using the GitHub Events API) is a separate future effort.

## Database Query

A single UNION ALL query across four subqueries (new PRs, new issues, PR events, issue events) returning a unified row shape:

```
activity_type, repo_owner, repo_name, item_type (pr/issue),
item_number, item_title, item_url, author, created_at,
body_preview (first 200 chars)
```

Filters applied in WHERE clause:
- Repo: `repo_owner || '/' || repo_name = ?`
- Activity type: `activity_type IN (?)`
- Search: `item_title LIKE ? OR body_preview LIKE ?`

Each row in the UNION carries a stable identity composed of its source and source ID:

| Subquery | `source` | `source_id` |
|----------|----------|-------------|
| New PR | `pr` | `pull_requests.id` |
| New issue | `issue` | `issues.id` |
| PR event | `pre` | `pr_events.id` |
| Issue event | `ise` | `issue_events.id` |

Ordered by `(created_at DESC, source DESC, source_id DESC)` — all columns descending so that a single tuple comparison works correctly for keyset pagination. Paginated with cursor-based keyset pagination (see below).

## API Endpoint

```
GET /api/v1/activity?repo=owner/name&types=comment,review&search=text&limit=50&before=<cursor>
```

Parameters:
- `repo` — filter to one repo (format: `owner/name`)
- `types` — comma-separated activity types
- `search` — searches item titles and body previews
- `limit` — default 50, max 200
- `before` — opaque cursor token (for "load more"); return items older than this position
- `after` — opaque cursor token (for polling); return items newer than this position

### Row identity and cursor tokens

Each activity row has a stable composite identity: `(created_at, source, source_id)`. This tuple is the sort key and the basis for pagination cursors.

The `id` field in each response item is a string of the form `{source}:{source_id}` (e.g., `pre:318`, `pr:42`). This serves as:
- A stable key for frontend keyed rendering (`{#each items as item (item.id)}`)
- A dedupe key when merging polled items into the existing list

The `cursor` field in each response item is an opaque string encoding the full sort position `(created_at, source, source_id)`. Format: base64 of `{unix_ms}:{source}:{source_id}`. The client treats it as an opaque token — it only passes it back as `before` or `after`.

### Pagination model

Cursor-based (keyset) pagination using composite sort keys:

- **Initial load:** No cursor. Returns the newest `limit` items.
- **Load more:** Pass `before` set to the `cursor` of the last displayed item. Query uses `WHERE (created_at, source, source_id) < (?, ?, ?)` (decoded from the cursor) with `ORDER BY created_at DESC, source DESC, source_id DESC LIMIT ?`. Because all three sort columns are descending, the `<` tuple comparison is correct — no skips or duplicates across timestamp ties.
- **Polling for new items:** Pass `after` set to the `cursor` of the newest displayed item. The `after` query returns all items newer than the cursor with no limit. In practice, new items are bounded by the sync interval (default 5m) and poll frequency (15s), so the result set is small. Items are prepended to the displayed list, deduped by `id`.
- **Polling overflow:** If the `after` response contains more than 500 items (a safeguard), the client discards the result and does a full reload from the top. This handles edge cases like a first sync after adding a large repo.

Response:

```json
{
  "items": [
    {
      "id": "pre:318",
      "cursor": "MTcxMTgwNzMyMDAwMDpwcmU6MzE4",
      "activity_type": "comment",
      "repo_owner": "apache",
      "repo_name": "arrow",
      "item_type": "pr",
      "item_number": 42318,
      "item_title": "Fix nested struct null bitmap handling",
      "item_url": "https://github.com/apache/arrow/pull/42318",
      "author": "pitrou",
      "created_at": "2026-03-30T14:22:00Z",
      "body_preview": "I think we should benchmark this against..."
    }
  ],
  "has_more": true
}
```

`has_more` is computed by fetching `limit + 1` rows and checking for the extra row.

## URL Routing

The app currently has no URL-based navigation. This feature adds `pushState`/`popstate` routing with no external library.

| URL | View |
|-----|------|
| `/` | Activity feed (default landing page) |
| `/?repo=owner/name&types=comment,review&search=text` | Activity feed with filters |
| `/pulls` | PR list view |
| `/pulls/board` | PR kanban board |
| `/pulls/{owner}/{name}/{number}` | PR detail |
| `/issues` | Issue list view |
| `/issues/{owner}/{name}/{number}` | Issue detail |

The existing `router.svelte.ts` is replaced with URL-derived state. The current in-memory `currentView`/`currentTab`/`selectedPR` state is computed from the URL path instead of being set directly.

### Activity feed filter state in the URL

The activity feed's repo filter, type filters, and search query are encoded as query parameters on `/`. Changing a filter updates the URL via `replaceState` (not `pushState` — filter changes don't create history entries). This means:
- Navigating away and pressing back restores the feed with its filters intact.
- The URL is bookmarkable/shareable with filters applied.
- The pagination cursor is not encoded in the URL — it's ephemeral client-side state. Pressing back reloads the feed from the top.

### Detail routes are standalone

When a URL like `/pulls/apache/arrow/42318` is loaded (whether by clicking a feed row or by direct navigation), the detail view fetches that specific item directly via the existing `GET /repos/{owner}/{name}/pulls/{number}` endpoint. It does not depend on the sidebar list containing that item.

The sidebar still loads its own list with default filters. If the selected item happens to appear in the sidebar list, it is highlighted. If not (e.g., the item is closed/merged and the sidebar defaults to open), the detail view still renders — the sidebar simply has no highlighted row.

## Frontend: Activity Feed View

### Layout

Full-width table (no sidebar). AppHeader shows "Activity" as the selected tab.

### Controls Bar

Above the table, left to right:
1. Repo filter dropdown (reuses existing `RepoSelector` pattern)
2. Type filter pills — toggleable buttons for each activity type, all active by default
3. Search input — debounced, filters on item title and body preview

Filter changes update the URL query parameters via `replaceState` and re-fetch with no cursor (back to the top).

### Table Columns

| Column | Width | Content |
|--------|-------|---------|
| Type | ~70px | Colored badge |
| Repository | ~160px | `owner/name`, muted text |
| Item | flex | `#number title`, clickable |
| Author | ~130px | GitHub username |
| When | ~80px | Relative time |
| Link | ~30px | External link icon |

### Type Badge Colors

| Type | Color | Token |
|------|-------|-------|
| New PR | Blue | `--accent-blue` |
| New Issue | Purple | `--accent-purple` |
| Comment | Amber | `--accent-amber` |
| Review | Green | `--accent-green` |
| Commit | Teal | New token `--accent-teal` |

### Interactions

- Row hover: `--bg-surface-hover` background
- Row click: opens item detail in a slide-out drawer panel (see Detail Drawer below)
- External link icon: opens `item_url` on GitHub in new tab
- "Load more" button at bottom when `has_more` is true, passes `cursor` of the last displayed item as `before`
- Auto-refresh on 15-second polling interval, passes `cursor` of the newest displayed item as `after`
- Polling prepends new items (deduped by `id`) without disrupting scroll position or resetting the loaded page
- If a poll returns 500+ items, discard and reload from the top

### Detail Drawer

When a row is clicked in the activity feed, a slide-out drawer panel opens on the right side of the screen, overlaying the table. The drawer shows the same PR/issue detail content (metadata, event timeline, comment box) as the existing `PullDetail`/`IssueDetail` components.

Behavior:
- Drawer opens from the right edge, width ~50% of the viewport (min 500px) on screens >= 1024px wide
- The feed table remains visible and scrollable beside the drawer on wide screens
- On screens < 1024px, the drawer becomes a full-width overlay sheet (100% width) with a back/close button at the top. The feed is hidden behind it. This is the same content, just full-screen instead of side-by-side
- Clicking a different row swaps the drawer content without closing/reopening
- Escape key or clicking outside the drawer closes it
- The URL updates to include the selected item (e.g., `/?selected=pr:apache/arrow/42318`) so the drawer state survives a page reload, but uses `replaceState` to avoid polluting history
- Closing the drawer removes the `selected` param from the URL via `replaceState`
- The drawer reuses the existing detail UI, but the current `PullDetail`/`IssueDetail` components are coupled to singleton stores (`detail.svelte.ts`) that own loading, polling, and side effects (e.g., PR actions refresh the pulls list store). To reuse them in the drawer without breaking the list view, the detail store needs to be decoupled from the list stores. The approach: the detail store's `loadDetail` function fetches and renders the item independently (it already does this via its own API call). Side effects that refresh list stores (e.g., after kanban state change or merge) should be conditional — only fire when the list view is active, not when the drawer is open. The simplest mechanism: check the current route before dispatching list refreshes.

### New Files

- `frontend/src/lib/components/ActivityFeed.svelte` — full-width table view with controls bar
- `frontend/src/lib/stores/activity.svelte.ts` — store for fetching, filtering, paginating activity data

## Navigation Changes

### AppHeader

Three tabs: **Activity** | **PRs** | **Issues**

Activity is the default (landing) tab. View switcher (list/board) only shows when PRs tab is selected.

Tab selection is derived from the current URL route.

### Detail View Integration

From the activity feed, clicking a row opens the detail drawer — the user stays on the feed. From the PR/issue list views, clicking an item still opens the inline detail panel as it does today.

The detail route URLs (`/pulls/{owner}/{name}/{number}`, `/issues/{owner}/{name}/{number}`) remain for direct navigation and for linking from the PR/issue list views. The detail view loads the item directly by owner/name/number — it does not require the item to be present in the sidebar list.
