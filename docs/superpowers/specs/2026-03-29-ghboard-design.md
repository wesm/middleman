# ghboard - GitHub PR Dashboard

Single-user, local-first dashboard for a maintainer managing a small fixed set of GitHub repositories. The product is optimized for the core loop of triage, review, follow-up, and merge without living in GitHub's notification UI or inbox.

## Product Goals

- Show all active PRs across tracked repositories in one fast local view
- Make "what changed since I last looked?" easy to answer
- Keep lightweight local workflow state separate from GitHub
- Stay operationally small: one binary, one SQLite database, no external services
- Prefer fast reads from a local cache over live GitHub round-trips

## Non-Goals

- Full replacement for all GitHub features or notifications
- Inline diff viewing or line-level review comments
- Multi-user access, authentication, or shared state
- Real-time updates via webhooks or websockets
- Issue tracking outside pull requests
- Local git/worktree automation in v1

## User and Workflow

Primary user: one maintainer working across roughly six repositories.

Core loop:

1. Scan open PRs sorted by recent activity
2. Open a PR to understand what changed and what state it is in
3. Move it into a local status bucket
4. Post a top-level comment when needed
5. Leave ghboard to merge, close, or do local branch work

The spec deliberately treats ghboard as a focused maintainer dashboard, not a general GitHub client.

## Tech Stack

- Backend: Go
- Database: SQLite
- Frontend: Svelte 5 + TypeScript + Vite
- GitHub API client: `google/go-github/v69`
- Config: TOML
- Packaging: single binary with embedded SPA via `go:embed`
- Build/test orchestration: `Makefile`

The structure should stay close to patterns already used in `agentsview` where that reduces design churn, but ghboard should only copy patterns that fit a local dashboard product.

## Architecture

ghboard is a local HTTP server plus SPA with a SQLite cache.

- The frontend talks only to the local Go API
- The Go API reads from SQLite for all dashboard views
- A background sync worker pulls data from GitHub into SQLite on a timer
- Write operations that mutate GitHub, such as posting a top-level PR comment, go through the Go server and then update the local cache

This is a cache-first design. Data may be stale by up to one sync interval, which is acceptable for v1 as long as the UI makes staleness visible.

## Operational Constraints

- v1 is single-user and local-only
- Default bind address is loopback; non-loopback hosting is out of scope
- The database should live in a user data directory, not beside the binary
- If GitHub is unavailable, cached reads should continue to work
- Sync failures for one repository must not prevent cached data for other repositories from being used

## Data Model

### `repos`

Tracked repositories from config.

| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | auto |
| owner | TEXT | GitHub org/user |
| name | TEXT | repo name |
| last_sync_started_at | DATETIME | nullable |
| last_sync_completed_at | DATETIME | nullable |
| last_sync_error | TEXT | nullable, most recent sync failure |
| created_at | DATETIME | local row creation |

UNIQUE(owner, name)

### `pull_requests`

Normalized PR summary data needed for list, board, and detail views.

| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | internal DB id |
| repo_id | INTEGER FK | refs `repos.id` |
| github_id | INTEGER | stable GitHub id for reconciliation |
| number | INTEGER | PR number within repo |
| url | TEXT | browser URL |
| title | TEXT | |
| author | TEXT | GitHub login |
| state | TEXT | `open`, `closed`, `merged` |
| is_draft | BOOLEAN | whether PR is draft |
| body | TEXT | PR description |
| head_branch | TEXT | |
| base_branch | TEXT | |
| additions | INTEGER | |
| deletions | INTEGER | |
| comment_count | INTEGER | top-level issue comments count |
| review_decision | TEXT | `approved`, `changes_requested`, `review_required`, `none` |
| ci_status | TEXT | normalized checks summary |
| created_at | DATETIME | from GitHub |
| updated_at | DATETIME | from GitHub |
| last_activity_at | DATETIME | derived sort key used by UI |
| merged_at | DATETIME | nullable |
| closed_at | DATETIME | nullable |

UNIQUE(repo_id, number)
UNIQUE(github_id)

`last_activity_at` should be recomputed during sync as the max of PR update time and latest relevant event timestamp. This keeps the list and board views honest without forcing the frontend to derive it repeatedly.

### `pr_events`

Timeline entries for detail view. Keep the schema flexible rather than over-normalizing event subtypes on day one.

| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | auto |
| pr_id | INTEGER FK | refs `pull_requests.id` |
| github_id | INTEGER | nullable for synthesized local events |
| event_type | TEXT | `issue_comment`, `review`, `review_comment`, `commit`, `state_change` |
| author | TEXT | nullable when GitHub does not provide actor |
| summary | TEXT | one-line display text |
| body | TEXT | nullable |
| metadata_json | TEXT | nullable, compact event-specific details |
| created_at | DATETIME | event timestamp |
| dedupe_key | TEXT | deterministic unique key |

UNIQUE(dedupe_key)

Using `metadata_json` avoids a migration every time timeline rendering needs one more field.

### `kanban_state`

Local-only workflow state. This is not synced back to GitHub.

| Column | Type | Notes |
|--------|------|-------|
| pr_id | INTEGER PK FK | refs `pull_requests.id` |
| status | TEXT | `new`, `reviewing`, `waiting`, `awaiting_merge` |
| updated_at | DATETIME | last local state change |

Status semantics must stay explicit:

- `new`: unseen or not yet triaged
- `reviewing`: maintainer is actively inspecting or editing
- `waiting`: blocked on contributor, CI, or external feedback
- `awaiting_merge`: functionally ready to land, waiting only on maintainer action

Newly discovered open PRs should automatically enter `new`.

## Database Indexes

v1 should create indexes for the known hot paths:

- `pull_requests(repo_id, state, last_activity_at DESC)`
- `pull_requests(state, last_activity_at DESC)`
- `kanban_state(status, updated_at DESC)`
- `pr_events(pr_id, created_at DESC)`

The product lives on fast list/detail reads, so these indexes should be part of the initial schema instead of deferred work.

## Sync Engine

Sync runs on startup and then every `sync_interval` (default `5m`). A manual sync endpoint triggers the same workflow.

### Sync Rules

- Only one sync run may execute at a time
- Repositories sync independently inside a run; one repo failure does not abort the rest
- Cached data remains readable during sync failures
- Sync status must expose whether a run is idle or active, when it last succeeded, and per-repo error state

### Per-Repo Sync Algorithm

1. List open PRs for the repo, sorted by `updated_at` descending
2. Upsert PR summary fields into `pull_requests`
3. For each PR that is new locally or whose GitHub `updated_at` advanced since the previous sync, refresh its activity timeline:
   - issue comments
   - reviews
   - review comments
   - commits
4. Normalize those events into `pr_events`
5. Recompute `last_activity_at`, `review_decision`, and `ci_status`
6. Auto-create `kanban_state` rows for first-seen PRs
7. For PRs previously known as open but missing from the current open list, fetch final PR state once to distinguish `closed` from `merged`

This keeps the common path cheap without requiring a full timeline refetch for every open PR on every interval.

### Failure and Backoff Behavior

- Persist the last sync error on the affected repo row
- Show stale data rather than empty states when sync fails
- Respect GitHub rate limits and `Retry-After` if returned
- If a sync is already running, `POST /api/v1/sync` should return `202` with current status instead of starting a second run

## Config

Default path: `~/.config/ghboard/config.toml`, overridable via `--config`.

```toml
sync_interval = "5m"
github_token_env = "GITHUB_TOKEN"
database_path = "~/.local/share/ghboard/ghboard.db"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "apache"
name = "arrow"

[[repos]]
owner = "ibis-project"
name = "ibis"
```

Notes:

- The config stores the environment variable name, not the token value
- v1 should reject non-loopback `host` values rather than pretending to support remote access
- Document the required GitHub token scopes explicitly in the README when implementation starts

## API

All endpoints live under `/api/v1`. The SPA is served at `/`.

API paths should use stable repository identity plus PR number rather than internal database ids. Internal ids remain a storage detail.

### Pull Requests

**GET `/api/v1/pulls`**

List PRs for dashboard views.

Query params:

- `repo=owner/name`
- `state=open|closed|merged` (default `open`)
- `kanban=new|reviewing|waiting|awaiting_merge`
- `q=<search text>`
- `limit`
- `offset`

Default sort: `last_activity_at DESC`.

**GET `/api/v1/repos/:owner/:name/pulls/:number`**

Return PR detail including timeline events and local kanban state.

**PUT `/api/v1/repos/:owner/:name/pulls/:number/state`**

Set local kanban state.

Request body:

```json
{"status":"reviewing"}
```

**POST `/api/v1/repos/:owner/:name/pulls/:number/comments`**

Post a top-level issue comment to GitHub, then persist the resulting comment locally.

Request body:

```json
{"body":"..."}
```

### Repositories

**GET `/api/v1/repos`**

List tracked repositories with last sync timestamps and current error state.

### Sync

**POST `/api/v1/sync`**

Trigger an immediate sync. Returns `202`.

**GET `/api/v1/sync/status`**

Return current sync state plus per-repo status summary.

## Frontend

Svelte 5 + TypeScript. Production serves the compiled SPA from the Go binary. Development uses Vite with a proxy to the Go backend.

### Views

**List View (default)**

- Left sidebar with PRs grouped by repo
- Sorted by `last_activity_at`
- Each item shows title, PR number, author, relative last activity time, draft/review/CI indicators, and kanban badge
- Filters for repo and kanban state
- Search across title, author, repo, and PR number
- Global sync button and stale/sync-error indicators

**Detail Panel**

- Title, repo, author, created date
- Chips for draft state, review decision, and CI status
- Local kanban state selector
- PR body
- Timeline rendered newest-first
- Comment box for posting a top-level comment

**Kanban Board**

- Header toggle between List and Board
- Four columns matching the local statuses
- Cards show title, repo, PR number, and recent activity
- Cards open the same detail panel as list items
- Within each column, cards stay sorted by `last_activity_at`

## UX Notes

- The app should make data freshness obvious; "last synced 18m ago" matters
- Empty states should distinguish "no matching PRs" from "sync failed" from "still syncing"
- Local kanban actions should feel instant; update SQLite first, then refresh derived views
- Keep the UI keyboard-friendly, but dedicated keyboard shortcuts are optional for v1

## Project Structure

```text
cmd/ghboard/main.go
internal/
  config/
  db/
    schema.sql
    queries.go
    migrations.go
  github/
    client.go
    sync.go
    normalize.go
  server/
    server.go
    handlers.go
  web/
    embed.go
frontend/
  src/
    App.svelte
    app.css
    main.ts
    lib/
      api/
      stores/
      components/
Makefile
go.mod
config.toml
.gitignore
```

The important boundary is not the exact folder names; it is that GitHub sync, DB access, HTTP handlers, and frontend state remain separate units.

## Testing Strategy

Backend:

- unit tests for config parsing and validation
- DB integration tests against temporary SQLite databases
- sync normalization tests using canned GitHub API fixtures
- handler tests for filtering, state updates, comment posting, and sync endpoints

Frontend:

- store tests for filtering, selection, and optimistic local state updates
- component tests for list, detail, and board rendering

v1 does not need a heavy end-to-end suite on day one, but one smoke path from sync status to list to detail would be high-value once the basic flows exist.

## Build System

| Target | Purpose |
|--------|---------|
| `make dev` | Run Go server against local frontend dev flow |
| `make frontend-dev` | Run Vite dev server with proxy to Go |
| `make frontend` | Build frontend assets for embedding |
| `make build` | Build production binary with embedded SPA |
| `make test` | Run backend and frontend tests |
| `make lint` | Run linting |
| `make clean` | Remove build artifacts |

## Scope Boundaries

### In Scope for v1

- Open PR dashboard across configured repositories
- Local cache of PR metadata and activity timeline
- Local kanban state management
- List and board views
- PR detail view
- Posting top-level PR comments
- Manual and periodic sync
- Light/dark theme if cheap to support

### Explicitly Out of Scope

- Line-level review comments
- Inline diffs
- Review approval submission from ghboard
- Webhook delivery or real-time push updates
- Multi-user auth or remote hosting
- Drag-and-drop kanban interactions
- Issue tracking
- Desktop packaging
- Local git checkout/worktree automation
