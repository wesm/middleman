# Claude Code Instructions

## Project Overview

middleman is a local-first GitHub PR monitoring dashboard for a maintainer managing a small fixed set of repositories. It syncs PR data from GitHub into SQLite on a timer, serves a Svelte 5 SPA via an embedded Go HTTP server, and provides a focused workflow for triage, review, and merge without living in GitHub's notification UI.

## Architecture

```
CLI (middleman) → Config (TOML) → DB (SQLite)
                    ↓                ↓
               Sync Engine → GitHub API (go-github/v84)
                    ↓                ↓
               HTTP Server → REST API + Embedded SPA
```

- **Server**: HTTP server on loopback (default 127.0.0.1:8090)
- **Storage**: SQLite with WAL mode (pure Go driver: modernc.org/sqlite)
- **Sync**: Periodic pull from GitHub API (configurable, default 5m)
- **Frontend**: Svelte 5 SPA embedded in the Go binary at build time
- **Config**: TOML at `~/.config/middleman/config.toml`, GitHub token from env var `MIDDLEMAN_GITHUB_TOKEN`

## Project Structure

- `cmd/middleman/` - Go server entrypoint
- `internal/config/` - TOML config loading and validation
- `internal/db/` - SQLite schema, connection, queries, types
- `internal/github/` - GitHub API client, normalization, sync engine
- `internal/server/` - HTTP handlers and routing
- `internal/web/` - Embedded frontend (dist/ copied at build time)
- `frontend/` - Svelte 5 SPA (Vite, TypeScript)

## Key Files

| Path | Purpose |
|------|---------|
| `cmd/middleman/main.go` | CLI entry point, server startup, signal handling |
| `internal/config/config.go` | TOML config, validation, defaults |
| `internal/db/schema.sql` | Table definitions and indexes |
| `internal/db/db.go` | Database open, WAL, schema init |
| `internal/db/queries.go` | All CRUD operations |
| `internal/db/types.go` | DB model types |
| `internal/github/client.go` | GitHub API interface and live implementation |
| `internal/github/normalize.go` | Convert GitHub types to DB types |
| `internal/github/sync.go` | Periodic sync engine |
| `internal/server/server.go` | HTTP router, SPA serving |
| `internal/server/handlers.go` | API endpoint handlers |
| `frontend/src/App.svelte` | Root component, view routing |
| `frontend/src/app.css` | Design tokens, theme, global styles |
| `frontend/src/lib/stores/` | Svelte 5 rune-based stores |
| `frontend/src/lib/components/` | UI components (sidebar, detail, kanban) |

## Development

```bash
make build          # Build binary with embedded frontend
make dev            # Run Go server in dev mode
make frontend       # Build frontend SPA only
make frontend-dev   # Run Vite dev server (use alongside make dev)
make install        # Build and install to ~/.local/bin or GOPATH
```

For development, run `make dev` and `make frontend-dev` in parallel. Vite proxies `/api` to the Go server on :8090.

## Testing

```bash
make test       # All Go tests
make test-short # Fast tests only
make lint       # golangci-lint
make vet        # go vet
```

### Test Guidelines

- Table-driven tests for Go code
- Use `openTestDB(t)` helper for database tests
- All tests use `t.TempDir()` for temp directories
- Tests should be fast and isolated

## Build Requirements

- **No CGO required** — uses modernc.org/sqlite (pure Go)
- **Frontend**: Bun for Svelte build/test tooling, embedded via `internal/web/dist/`

## Conventions

- Prefer stdlib over external dependencies
- Use Bun for frontend package management and script execution; do not introduce npm-based workflow changes unless explicitly requested
- Tests should be fast and isolated
- No emojis in code or output
- Schema changes should use `ALTER TABLE ADD COLUMN` migrations in `db.init()` for backward compatibility with existing databases

## Git Workflow

- **Commit every turn** — always commit your work at the end of each turn, no exceptions
- **Never amend commits** — always create new commits for fixes, never use `--amend`
- **Never change branches** — don't create, switch, or delete branches without explicit permission
- Use conventional commit messages
- Run tests before committing when applicable
- Never push or pull unless explicitly asked
