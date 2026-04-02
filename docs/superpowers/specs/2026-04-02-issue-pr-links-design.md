# Intra-Middleman #NUMBER Link Navigation

**Issue:** #44
**Date:** 2026-04-02

## Goal

Make `#123` and `owner/repo#123` references in markdown content (PR/issue bodies, comments, reviews) clickable links that navigate within middleman. When a referenced item isn't in the local DB (e.g., a closed issue), fetch it from GitHub on demand.

## Architecture

```
User clicks #123 in rendered markdown
  -> Normal click: handler intercepts, extracts owner/name/number
  -> Cmd/ctrl/middle-click: follows href to GitHub (standard link behavior)
  -> Normal click calls GET /api/v1/repos/{owner}/{name}/items/{number}
  -> Backend checks DB (PR table, then issue table)
  -> If not found & repo is in syncer config: fetch from GitHub, sync into DB
  -> If repo not in syncer config: return repo_tracked=false
  -> Frontend navigates to /pulls/... or /issues/...
  -> If repo not tracked: show prompt to add repo
```

## Components

### 1. Marked.js Inline Extension

Add a custom inline extension to the existing `marked` instance in `markdown.ts`.

**Tokenizer** matches two patterns in markdown text nodes (not inside code spans/blocks):
- Bare refs: `#\d+` — same-repo reference
- Cross-repo refs: `[\w.-]+/[\w.-]+#\d+` — explicit repo

**Renderer** produces `<a>` elements with a real GitHub `href` as fallback (preserves cmd/ctrl-click, middle-click, copy-link, and keyboard accessibility):
```html
<!-- bare ref, repo filled from context -->
<a class="item-ref" href="https://github.com/wesm/middleman/issues/123"
   data-owner="wesm" data-name="middleman" data-number="123">#123</a>

<!-- cross-repo ref -->
<a class="item-ref" href="https://github.com/other/repo/issues/456"
   data-owner="other" data-name="repo" data-number="456">other/repo#456</a>
```

The GitHub `/issues/{number}` URL works for both PRs and issues (GitHub redirects).

**Signature change:**
```typescript
renderMarkdown(raw: string, repo?: { owner: string; name: string }): string
```

Cache key becomes `${owner}/${name}\0${raw}` when repo context is provided, plain `raw` otherwise.

**DOMPurify:** Add `data-owner`, `data-name`, `data-number` to `ADD_ATTR`.

### 2. Resolve API Endpoint

Registered in `registerAPI` like all other endpoints (Huma automatically prefixes `/api/v1`). Added to the OpenAPI spec; frontend client regenerated via `make api-generate`.

`GET /repos/{owner}/{name}/items/{number}`

**Response** (`ResolveItemResponse`):
```json
{
  "item_type": "pr" | "issue",
  "number": 123,
  "repo_tracked": true
}
```

**Resolution logic:**

"Tracked" means the repo is in the syncer's configured repo list (`Syncer.isTrackedRepo`), not merely present in the `repos` DB table. This matches how `SyncPR`/`SyncIssue` already gate on config.

1. Check `s.syncer.IsTrackedRepo(owner, name)`. If not tracked, return `repo_tracked: false` immediately.
2. Try `db.GetRepoByOwnerName(owner, name)` to get a `repoID`.
3. If `repoID` found, query `pull_requests` then `issues` tables for a matching number. If found, return.
4. If `repoID` not found (configured but never synced) OR item not in DB: call `SyncItemByNumber(ctx, owner, name, number)`. This delegates to `SyncPR`/`SyncIssue`, which internally call `UpsertRepo` to create the repo row if needed. Return the resolved type.

Steps 3 and 4 share the same fallback path — the only difference is whether the DB lookup is attempted. This avoids a separate branch for the "configured but never synced" case.

Exporting `isTrackedRepo` as `IsTrackedRepo` is required.

**Error cases:**
- Item doesn't exist on GitHub: return 404.
- GitHub API error: return 502 with message.

### 3. Global Click Handler

A single event listener (delegation on `document`) that intercepts normal left-clicks on `.item-ref` elements. Cmd/ctrl-click, middle-click, and right-click are left alone so the browser follows the GitHub fallback `href`.

**Flow:**
1. Check `e.metaKey`, `e.ctrlKey`, `e.shiftKey`, `e.button` — if any modifier or non-primary button, return (let browser handle).
2. `e.preventDefault()`
3. Read `data-owner`, `data-name`, `data-number` from the clicked element.
4. Call the resolve endpoint (via the generated API client).
5. On success: call `navigate()` to route to `/pulls/{o}/{n}/{num}` or `/issues/{o}/{n}/{num}`.
6. On `repo_tracked: false` or 404: show a flash banner (see below).

The handler lives in a new `itemRefHandler.ts` utility, initialized once in `App.svelte` on mount.

**Flash banner for errors:** The app has no toast system today. Add a minimal `flash.svelte.ts` store (a single `$state` string that auto-clears after 4 seconds) and a `FlashBanner.svelte` component rendered at the top of `App.svelte`. This follows the same inline-banner pattern used in `ActivityFeed.svelte` (`error-banner` class). Messages:
- Untracked repo: `"{owner}/{name} is not tracked. Add it in Settings to navigate here."`
- Not found: `"Item {owner}/{name}#{number} not found on GitHub."`

### 4. Component Changes

Components that call `renderMarkdown` need to pass repo context:

| Component | Context source |
|-----------|---------------|
| `PullDetail.svelte` | Props `owner`, `name` |
| `IssueDetail.svelte` | Props `owner`, `name` |
| `EventTimeline.svelte` | Needs new `owner`/`name` props passed from parent |

### 5. DB Query

New query in `queries.go`:

```go
func (db *DB) ResolveItemNumber(
    ctx context.Context, repoID int64, number int,
) (itemType string, found bool, err error)
```

Checks `pull_requests` then `issues` tables by `repo_id` + `number`.

### 6. GitHub On-Demand Sync

New method on `Syncer`:

```go
func (s *Syncer) SyncItemByNumber(
    ctx context.Context, owner, name string, number int,
) (itemType string, err error)
```

Fetches the item from GitHub's issues endpoint, determines PR vs issue, delegates to existing `SyncPR`/`SyncIssue` methods.

### 7. Styling

```css
.item-ref {
  color: var(--text-link);
  text-decoration: none;
  cursor: pointer;
  border-radius: 3px;
}
.item-ref:hover {
  text-decoration: underline;
}
```

## Files Changed

| File | Change |
|------|--------|
| `frontend/src/lib/utils/markdown.ts` | Marked extension, repo context param, DOMPurify config |
| `frontend/src/lib/utils/itemRefHandler.ts` | New: global click handler |
| `frontend/src/lib/stores/flash.svelte.ts` | New: flash message store (auto-clearing) |
| `frontend/src/lib/components/FlashBanner.svelte` | New: renders flash messages |
| `frontend/src/App.svelte` | Initialize click handler, render FlashBanner |
| `frontend/src/lib/components/detail/PullDetail.svelte` | Pass repo context to renderMarkdown |
| `frontend/src/lib/components/detail/IssueDetail.svelte` | Pass repo context to renderMarkdown |
| `frontend/src/lib/components/detail/EventTimeline.svelte` | Accept + pass repo context |
| `internal/server/api_types.go` | `ResolveItemResponse` struct |
| `internal/server/huma_routes.go` | Register resolve endpoint + handler |
| `internal/db/queries.go` | `ResolveItemNumber` query |
| `internal/github/sync.go` | Export `IsTrackedRepo`, add `SyncItemByNumber` |
| `internal/github/client.go` | Add `GetIssueOrPR` method if needed |
| OpenAPI spec + generated clients | `make api-generate` after adding the endpoint |

## Out of Scope

- Hover previews showing item title/state (future enhancement)
- Color-coding refs by state (open/closed/merged)
- Backlink tracking (showing which items reference a given item)
