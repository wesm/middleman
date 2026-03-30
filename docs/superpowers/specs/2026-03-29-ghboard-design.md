# ghboard — GitHub PR Dashboard

Personal PR monitoring dashboard for a maintainer managing ~6 active GitHub projects. Replaces GitHub email notifications and github.com with a focused, local-first tool optimized for PR merge throughput.

## Problem

GitHub's notification system and web UI are noisy and slow for a maintainer whose core loop is: see what's active, check out the branch, fix it up, merge. ghboard provides a streamlined view of PR activity across multiple repos with local Kanban state tracking.

## Tech Stack

- **Backend:** Go + SQLite (CGO for FTS if needed later)
- **Frontend:** Svelte 5 + TypeScript + Vite
- **GitHub API:** google/go-github v69
- **Config:** TOML (project manifest + settings)
- **Build:** Single binary with embedded SPA (go:embed), Makefile

Follows the same architecture patterns as agentsview (~/code/agentsview).

## Architecture

Full local cache. All GitHub data syncs into SQLite on a configurable timer (default 5 minutes). The frontend reads only from the local DB via the Go HTTP API. GitHub API is hit during sync and for write operations (posting comments). Data can be up to one sync interval stale, which is acceptable.

## Data Model

### repos

Tracked repositories from config.toml.

| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | auto |
| owner | TEXT | GitHub org/user |
| name | TEXT | repo name |
| last_synced_at | DATETIME | last successful sync |
| created_at | DATETIME | |

UNIQUE(owner, name)

### pull_requests

PR metadata synced from GitHub.

| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | auto |
| repo_id | INTEGER FK | refs repos.id |
| number | INTEGER | PR number |
| title | TEXT | |
| author | TEXT | GitHub login |
| state | TEXT | open, closed, merged |
| body | TEXT | PR description |
| head_branch | TEXT | |
| base_branch | TEXT | |
| additions | INTEGER | |
| deletions | INTEGER | |
| ci_status | TEXT | pending, success, failure, null |
| created_at | DATETIME | from GitHub |
| updated_at | DATETIME | from GitHub |
| merged_at | DATETIME | nullable |
| closed_at | DATETIME | nullable |

UNIQUE(repo_id, number)

### pr_events

Timeline of PR activity synced from GitHub.

| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | auto |
| pr_id | INTEGER FK | refs pull_requests.id |
| event_type | TEXT | comment, review, push, state_change |
| author | TEXT | GitHub login |
| body | TEXT | nullable (pushes have no body) |
| github_id | INTEGER | for dedup |
| created_at | DATETIME | from GitHub |

UNIQUE(pr_id, event_type, github_id)

### kanban_state

Local-only state tracking. Not synced to GitHub.

| Column | Type | Notes |
|--------|------|-------|
| pr_id | INTEGER PK FK | refs pull_requests.id |
| status | TEXT | new, reviewing, waiting, awaiting_merge |
| updated_at | DATETIME | last state change |

New PRs auto-enter as "new". When a PR is merged or closed on GitHub, its kanban state becomes irrelevant and the PR drops out of the active dashboard (but remains in the DB for history).

## Sync Engine

- Runs on startup and every `sync_interval` (configurable, default "5m")
- Manual trigger via POST `/api/v1/sync`
- For each tracked repo:
  1. Fetch open PRs sorted by updated_at desc (go-github ListPullRequests)
  2. For each PR, fetch timeline events: issue comments, review comments, commits
  3. Upsert into pull_requests and pr_events tables
  4. For PRs previously open but now missing from the open list, mark as closed/merged
  5. Auto-create kanban_state rows for newly seen PRs (status = "new")
- Sync runs sequentially per repo to stay well within rate limits
- Track last_synced_at per repo for observability

## Config

File: `~/.config/ghboard/config.toml` (or path from `--config` flag)

```toml
# Sync interval (Go duration string)
sync_interval = "5m"

# GitHub token read from this env var
github_token_env = "GITHUB_TOKEN"

# Server
host = "127.0.0.1"
port = 8090

# Tracked repositories
[[repos]]
owner = "apache"
name = "arrow"

[[repos]]
owner = "ibis-project"
name = "ibis"
```

The GitHub token is never stored in the config file — only the env var name.

## API

All endpoints under `/api/v1/`. Frontend SPA served at `/`.

### Pull Requests

**GET /api/v1/pulls**
List PRs. Query params: `repo` (owner/name), `state` (open/closed/merged), `kanban` (new/reviewing/waiting/awaiting_merge), `limit`, `offset`. Default: open PRs sorted by most recent activity.

**GET /api/v1/pulls/:id**
PR detail with full event timeline.

**PUT /api/v1/pulls/:id/state**
Set local kanban state. Body: `{"status": "reviewing"}`.

**POST /api/v1/pulls/:id/comments**
Post a comment to GitHub. Body: `{"body": "..."}`. Proxied through go-github to the GitHub API, then the comment is also stored locally.

### Repos

**GET /api/v1/repos**
List tracked repos with last_synced_at.

### Sync

**POST /api/v1/sync**
Trigger immediate sync. Returns 202.

**GET /api/v1/sync/status**
Current sync state (idle, syncing, last error).

## Frontend

Svelte 5 with TypeScript. Embedded in the Go binary for production; Vite dev server with proxy for development.

### Views

**List View (default)**
- Left sidebar: PR list grouped by repo, sorted by most recent activity
- Each PR item shows: title, number, author, time since last activity, kanban state badge
- Click a PR to open detail in the main panel
- Filter by repo and kanban state
- Search PRs by title

**Detail Panel**
- PR title, author, repo, open date, CI status
- Kanban state dropdown (click to change)
- PR body/description
- Event timeline (comments, reviews, pushes) in chronological order
- Comment box at the bottom to post a top-level comment

**Kanban Board View**
- Toggle from header: List | Board
- Four columns: New, Reviewing, Waiting, Awaiting Merge
- Cards show: title, repo, number, last activity time
- Click card to open detail (slide-out or navigate to list view with PR selected)
- PRs within columns sorted by most recent activity

### Design System

CSS custom properties matching agentsview's pattern:
- Light/dark theme via `html.dark` class toggle
- Design tokens for colors, spacing, typography
- Font: Inter (sans), JetBrains Mono (mono)
- Accent colors per kanban state: blue (new), amber (reviewing), purple (waiting), green (awaiting merge)

## Project Structure

```
cmd/ghboard/main.go
internal/
  config/config.go
  db/
    db.go
    schema.sql
    queries.go
  github/
    sync.go
    client.go
  server/
    server.go
    handlers.go
  web/embed.go
frontend/
  src/
    App.svelte
    app.css
    main.ts
    lib/
      api/client.ts
      stores/
        pulls.svelte.ts
        detail.svelte.ts
        sync.svelte.ts
      components/
        layout/AppHeader.svelte
        sidebar/PullList.svelte
        sidebar/PullItem.svelte
        detail/PullDetail.svelte
        detail/EventTimeline.svelte
        detail/CommentBox.svelte
        kanban/KanbanBoard.svelte
        kanban/KanbanColumn.svelte
        kanban/KanbanCard.svelte
  vite.config.ts
  package.json
  tsconfig.json
Makefile
go.mod
config.toml (example)
.gitignore
```

## Build System (Makefile)

| Target | Purpose |
|--------|---------|
| `make dev` | Go server (no embedded frontend) |
| `make frontend-dev` | Vite dev server on :5173 with proxy to Go |
| `make build` | Production binary with embedded SPA |
| `make frontend` | Build frontend for embedding |
| `make test` | Go tests |
| `make lint` | golangci-lint |
| `make clean` | Remove build artifacts |

## Scope Boundaries

**In scope for v1:**
- Sync open PRs and their activity timeline from configured repos
- List view with repo grouping and activity sorting
- PR detail with event timeline
- Local kanban state management (new/reviewing/waiting/awaiting_merge)
- Kanban board view
- Post top-level comments on PRs
- Light/dark theme
- Configurable sync interval

**Explicitly out of scope (future):**
- Inline diff viewer
- Review comments (line-level)
- Issue tracking (only PRs)
- Webhook-based real-time updates
- Multi-user / auth
- Desktop app (Tauri)
- GitHub Actions / workflow status detail
- Drag-and-drop in Kanban view
