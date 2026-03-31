# middleman

Local-first GitHub dashboard for project maintainers. Syncs PRs and issues from a configurable set of repos into SQLite, serves a fast Svelte frontend, and keeps you out of GitHub's notification inbox.

## What it does

**Activity feed** — See recent comments, reviews, and commits across all your repos in one timeline. Switch between flat and threaded views. Filter by time range (24h/7d/30d/90d), hide closed items, or hide bot activity.

**PR and issue management** — Browse open PRs and issues across repos. Post comments, approve PRs, merge (merge commit, squash, or rebase), close/reopen, and mark draft PRs as ready — all without leaving the dashboard.

**Kanban board** — Track PRs through New / Reviewing / Waiting / Awaiting Merge columns with drag-and-drop.

**Near-real-time updates** — Opening a PR or issue triggers an immediate sync from GitHub. The active item polls every 60 seconds for new comments.

**Settings** — Add and remove repos from the UI. Configure activity feed defaults (view mode, time range, filters) that persist to your config file.

**Other** — Star/favorite items, expandable CI checks with links to failing runs, keyboard navigation (`j`/`k` to move, `1`/`2` to switch views, `Escape` to close), dark mode, copy PR/issue bodies, reverse proxy support via `base_path`.

## Requirements

- Go 1.22+ (no CGO required — uses pure Go SQLite)
- Bun (managed via mise)
- [mise](https://mise.jdx.dev/)
- A GitHub token: set `MIDDLEMAN_GITHUB_TOKEN`, or authenticate with `gh auth login`

## Setup

1. Clone and build:

```
git clone https://github.com/wesm/middleman.git
cd middleman
mise install
make build
```

2. Create a config file at `~/.config/middleman/config.toml`:

```toml
sync_interval = "5m"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "your-org"
name = "your-repo"

[[repos]]
owner = "your-org"
name = "another-repo"
```

3. Set your GitHub token and run:

```
export MIDDLEMAN_GITHUB_TOKEN=ghp_your_token_here
./middleman
```

If you use the [GitHub CLI](https://cli.github.com/), middleman will fall back to `gh auth token` automatically — no env var needed.

4. Open http://localhost:8090

The first sync runs immediately on startup. PRs and issues populate within a few seconds.

### Configuration reference

All fields are optional except `[[repos]]`.

| Field | Default | Description |
|-------|---------|-------------|
| `sync_interval` | `"5m"` | How often to pull from GitHub |
| `github_token_env` | `"MIDDLEMAN_GITHUB_TOKEN"` | Env var holding your token |
| `host` | `"127.0.0.1"` | Listen address (loopback only) |
| `port` | `8090` | Listen port |
| `base_path` | `"/"` | URL prefix for reverse proxy deployments |
| `data_dir` | `"~/.config/middleman"` | Directory for the SQLite database |
| `activity.view_mode` | `"threaded"` | `"flat"` or `"threaded"` |
| `activity.time_range` | `"7d"` | `"24h"`, `"7d"`, `"30d"`, or `"90d"` |
| `activity.hide_closed` | `false` | Hide closed/merged items in the feed |
| `activity.hide_bots` | `false` | Hide bot activity in the feed |

## Development

Run the Go backend and Vite dev server in parallel:

```
make air-install    # one-time install for backend live reload
make dev            # Go server on :8090 with air live reload
make frontend-dev   # Vite on :5173 (proxies /api to Go)
```

`make dev` uses [air](https://github.com/air-verse/air) to rebuild and restart the Go server on file changes. Pass backend flags with `make dev ARGS='-config /path/to/config.toml'`.

Other targets:

```
make build          # Production binary with embedded frontend
make build-release  # Optimized, stripped release build
make test           # Run all Go tests
make test-short     # Fast tests only
make lint           # mise-managed golangci-lint
make frontend-check # Svelte / TypeScript checks
make api-generate   # Regenerate OpenAPI spec and clients
make install        # Install to ~/.local/bin
make install-hooks  # Install pre-commit hooks via prek
```

### Pre-commit hooks

Hooks are managed with [prek](https://github.com/j178/prek). After cloning:

```sh
brew install prek
prek install
```

Go commits run `gofmt`, `golangci-lint`, `go test -short`, and API client regeneration. Frontend commits run `make frontend-check`. If a hook auto-fixes files, re-stage and re-commit. Config lives in `prek.toml`.

## License

MIT
