# middleman

A local-first GitHub dashboard for project maintainers. Syncs PRs and issues from your repos into SQLite, serves a fast Svelte 5 frontend from a single binary, and keeps you out of GitHub's notification inbox.

Middleman runs entirely on your machine — no hosted service, no telemetry, no account to create. One binary, one config file, and you're up.

## Features

### Activity feed

A unified timeline of comments, reviews, and commits across all your repos. Switch between flat and threaded views. Threaded view groups events by PR/issue and collapses long commit runs for readability.

Filter by time range (24h / 7d / 30d / 90d), event type, repo, item type (PRs vs issues), or free-text search. Hide closed items and bot noise with a toggle.

### Pull request management

Browse, search, and filter PRs across repos. From the detail view you can:

- **Comment** directly on a PR
- **Approve** a PR
- **Merge** with your choice of merge commit, squash, or rebase
- **Mark draft PRs as ready** for review
- **Close and reopen** PRs
- **Star** items for quick filtering

Review decisions, diff stats (additions/deletions), CI status, and branch info are visible at a glance.

### Kanban board

Track PRs through **New / Reviewing / Waiting / Awaiting Merge** columns with drag-and-drop. Kanban state is local to middleman — it doesn't touch your GitHub labels or projects.

### Issue tracking

Same filtering, search, and detail view as PRs. Post comments, close/reopen, and star issues without context-switching to GitHub.

### CI checks

Expandable check run section on each PR shows pass/fail/pending status with color-coded indicators and direct links to the failing run on GitHub.

### Sync engine

- Runs immediately on startup, then on a configurable interval (default 5 minutes)
- Opening a PR or issue triggers an immediate sync for that item
- The active detail view polls every 60 seconds for new comments
- Progress is visible in the status bar; errors surface clearly

### Keyboard navigation

| Key | Action |
|-----|--------|
| `j` / `k` | Move through the list |
| `1` / `2` | Switch between list and kanban views |
| `Escape` | Close detail view / clear selection |

### Other

- **Dark mode** — auto-detects system preference, with a manual toggle
- **Copy to clipboard** — one-click copy of PR/issue bodies and comments
- **Settings UI** — add/remove repos and configure activity feed defaults from the browser
- **Reverse proxy support** — deploy behind a proxy with the `base_path` config
- **Version info** — `middleman version` prints the version, commit, and build date

## Quickstart

### Requirements

- Go 1.26+
- [Bun](https://bun.sh/) (or install via [mise](https://mise.jdx.dev/))
- A GitHub token (classic or fine-grained with repo read access)

### Build and run

```sh
git clone https://github.com/wesm/middleman.git
cd middleman
make build
```

On first run, middleman creates a default config at `~/.config/middleman/config.toml`. Edit it to add at least one repo:

```toml
[[repos]]
owner = "your-org"
name = "your-repo"

[[repos]]
owner = "your-org"
name = "another-repo"
```

Set your token and start middleman:

```sh
export MIDDLEMAN_GITHUB_TOKEN=ghp_your_token_here
./middleman
```

If you use the [GitHub CLI](https://cli.github.com/), middleman will use `gh auth token` automatically — no env var needed.

Open **http://localhost:8090**. The first sync runs immediately; PRs and issues appear within seconds.

### Install to PATH

```sh
make install   # installs to ~/.local/bin
```

## Configuration

All fields are optional. Repos can be added in the config file or through the Settings UI.

| Field | Default | Description |
|-------|---------|-------------|
| `sync_interval` | `"5m"` | How often to pull from GitHub |
| `github_token_env` | `"MIDDLEMAN_GITHUB_TOKEN"` | Env var holding your token |
| `host` | `"127.0.0.1"` | Listen address |
| `port` | `8090` | Listen port |
| `base_path` | `"/"` | URL prefix for reverse proxy deployments |
| `data_dir` | `"~/.config/middleman"` | Directory for the SQLite database |
| `activity.view_mode` | `"threaded"` | `"flat"` or `"threaded"` |
| `activity.time_range` | `"7d"` | `"24h"`, `"7d"`, `"30d"`, or `"90d"` |
| `activity.hide_closed` | `false` | Hide closed/merged items in the feed |
| `activity.hide_bots` | `false` | Hide bot activity |

## Architecture

Middleman is a single Go binary with the Svelte frontend embedded at build time. No external services — just SQLite on disk.

```
middleman binary
  ├── Config loader (TOML)
  ├── Sync engine → GitHub API (go-github)
  ├── SQLite database (WAL mode, pure Go driver)
  └── HTTP server (Huma) → REST API + embedded SPA
```

- **No CGO required** — uses [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), a pure Go SQLite implementation
- **Loopback only** — binds to 127.0.0.1 by default; this is a personal tool, not a shared service
- **Graceful shutdown** — handles SIGINT/SIGTERM cleanly

## Development

Run the Go backend and Vite dev server in parallel:

```sh
make air-install    # one-time: install air for live reload
make dev            # Go server on :8090 with live reload
make frontend-dev   # Vite on :5173, proxies /api to Go
```

Other targets:

```sh
make build          # Debug build with embedded frontend
make build-release  # Optimized, stripped release binary
make test           # All Go tests
make test-short     # Fast tests only
make lint           # golangci-lint
make frontend-check # Svelte and TypeScript checks
make api-generate   # Regenerate OpenAPI spec and clients
make clean          # Remove build artifacts
```

### Pre-commit hooks

Managed with [prek](https://github.com/j178/prek):

```sh
brew install prek
prek install
```

## License

MIT
