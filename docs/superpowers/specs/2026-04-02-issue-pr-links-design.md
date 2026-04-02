# Intra-Middleman #NUMBER Link Navigation

**Issue:** #44
**Date:** 2026-04-02

## Goal

Make `#123` and `owner/repo#123` references in markdown content (PR/issue bodies, comments, reviews) clickable links that navigate within middleman. When a referenced item isn't in the local DB (e.g., a closed issue), fetch it from GitHub on demand.

## Architecture

```
User clicks #123 in rendered markdown
  -> Global click handler extracts owner/name/number from data attributes
  -> Calls GET /api/repos/{owner}/{name}/items/{number}
  -> Backend checks DB (PR table, then issue table)
  -> If not found & repo tracked: fetch from GitHub, sync into DB
  -> If repo not tracked: return repo_tracked=false
  -> Frontend navigates to /pulls/... or /issues/...
  -> If repo not tracked: show prompt to add repo
```

## Components

### 1. Marked.js Inline Extension

Add a custom inline extension to the existing `marked` instance in `markdown.ts`.

**Tokenizer** matches two patterns in markdown text nodes (not inside code spans/blocks):
- Bare refs: `#\d+` — same-repo reference
- Cross-repo refs: `[\w.-]+/[\w.-]+#\d+` — explicit repo

**Renderer** produces:
```html
<!-- bare ref, repo filled from context -->
<a class="item-ref" data-owner="wesm" data-name="middleman" data-number="123">#123</a>

<!-- cross-repo ref -->
<a class="item-ref" data-owner="other" data-name="repo" data-number="456">other/repo#456</a>
```

**Signature change:**
```typescript
renderMarkdown(raw: string, repo?: { owner: string; name: string }): string
```

Cache key becomes `${owner}/${name}\0${raw}` when repo context is provided, plain `raw` otherwise.

**DOMPurify:** Add `data-owner`, `data-name`, `data-number` to `ADD_ATTR`.

### 2. Resolve API Endpoint

`GET /api/repos/{owner}/{name}/items/{number}`

**Response** (`ResolveItemResponse`):
```json
{
  "item_type": "pr" | "issue",
  "number": 123,
  "repo_tracked": true
}
```

**Resolution logic:**

1. Query `pull_requests` table for a row with matching repo + number. If found, return `item_type: "pr"`.
2. Query `issues` table for a row with matching repo + number. If found, return `item_type: "issue"`.
3. If repo is tracked (exists in `repos` table) but item not in DB:
   - Call GitHub Issues API `GET /repos/{owner}/{name}/issues/{number}` (returns both issues and PRs).
   - If the response has a `pull_request` field, it's a PR: call the existing `SyncPR` flow.
   - Otherwise, sync it as an issue via the existing `SyncIssue` flow.
   - Return the resolved type.
4. If repo is not tracked: return `repo_tracked: false` (no GitHub fetch attempted).

**Error cases:**
- Item doesn't exist on GitHub: return 404.
- GitHub API error: return 502 with message.

### 3. Global Click Handler

A single event listener (delegation on `document`) that intercepts clicks on `.item-ref` elements.

**Flow:**
1. `e.preventDefault()`
2. Read `data-owner`, `data-name`, `data-number` from the clicked element.
3. Call the resolve endpoint.
4. On success: call `navigate()` to route to `/pulls/{o}/{n}/{num}` or `/issues/{o}/{n}/{num}`.
5. On `repo_tracked: false`: show a toast/notification suggesting the user add the repo to their config.
6. On 404: show a brief "Item not found" message.

The handler lives in a new `itemRefHandler.ts` utility, initialized once in `App.svelte` on mount.

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
| `frontend/src/App.svelte` | Initialize click handler on mount |
| `frontend/src/lib/components/detail/PullDetail.svelte` | Pass repo context to renderMarkdown |
| `frontend/src/lib/components/detail/IssueDetail.svelte` | Pass repo context to renderMarkdown |
| `frontend/src/lib/components/detail/EventTimeline.svelte` | Accept + pass repo context |
| `internal/server/api_types.go` | `ResolveItemResponse` struct |
| `internal/server/huma_routes.go` | Register resolve endpoint + handler |
| `internal/db/queries.go` | `ResolveItemNumber` query |
| `internal/github/sync.go` | `SyncItemByNumber` method |
| `internal/github/client.go` | Add `GetIssueOrPR` method if needed |

## Out of Scope

- Hover previews showing item title/state (future enhancement)
- Color-coding refs by state (open/closed/merged)
- Backlink tracking (showing which items reference a given item)
