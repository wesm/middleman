# middleman

Local-first GitHub dashboard for project maintainers. Syncs PRs and issues from a configurable set of repos into SQLite, serves a fast Svelte frontend, and keeps you out of GitHub's notification inbox.

## What it does

- Shows all open PRs and issues across your repos in one view
- Local kanban state tracking (New / Reviewing / Waiting / Awaiting Merge)
- Star/favorite items to keep them visible
- Post comments, approve PRs, and merge directly from the dashboard
- Expandable CI check details with links to failing runs
- 60-second polling on the active PR for near-real-time comment updates
- Keyboard navigation (j/k, Escape, 1/2 to switch views)
- Dark mode

## Requirements

- Go 1.22+ (no CGO required — uses pure Go SQLite)
- Bun 1.3+
- A GitHub personal access token with `repo` scope

## Setup

1. Clone and build:

```
git clone https://github.com/wesm/middleman.git
cd middleman
make build
```

2. Create a config file at `~/.config/middleman/config.toml`:

```toml
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
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

4. Open http://localhost:8090

The first sync runs immediately on startup. PRs and issues will populate within a few seconds.

## Development

Run the Go backend and Vite dev server in parallel:

```
make dev            # Go server on :8090
make frontend-dev   # Vite on :5173 (proxies /api to Go)
```

Other targets:

```
make build          # Production binary with embedded frontend
make test           # Run all Go tests
make lint           # golangci-lint
make install        # Install to ~/.local/bin
make install-hooks  # Install pre-commit hooks via prek
```

Pre-commit hooks are managed with [prek](https://github.com/j178/prek).
Run `brew install prek && make install-hooks` after cloning. The hook
runs `make lint` on every commit, auto-fixing formatting issues. If the
hook rewrites files, re-stage and re-commit.

## License

MIT
