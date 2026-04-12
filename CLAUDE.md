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

- **Server**: Huma-based HTTP server on loopback (default 127.0.0.1:8090)
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
| `internal/db/migrations/` | Numbered SQL migrations for schema changes |
| `internal/db/db.go` | Database open, WAL, migration init |
| `internal/db/queries.go` | All CRUD operations |
| `internal/db/types.go` | DB model types |
| `internal/github/client.go` | GitHub API interface and live implementation |
| `internal/github/normalize.go` | Convert GitHub types to DB types |
| `internal/github/sync.go` | Periodic sync engine |
| `internal/server/server.go` | HTTP router, SPA serving |
| `internal/server/huma_routes.go` | Huma API registrations and handlers |
| `internal/server/api_types.go` | Shared API response types used by Huma |
| `internal/apiclient/generated/client.gen.go` | Generated Go API client from the checked-in OpenAPI spec |
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

### End-to-End Tests

**E2E tests are non-negotiable.** Every major feature, bug fix, and refactor must include e2e tests that exercise the full stack (HTTP API with real SQLite). Even small changes merit e2e coverage when they touch API behavior, data flow between layers, or anything a user would notice if it broke. When in doubt, write the e2e test — the cost of a missing one is always higher than the cost of writing it.

### Test Guidelines

- Always pass `-shuffle=on` when invoking `go test` directly (e.g. `go test ./internal/db -run TestFoo -shuffle=on`). The `make test` and `make test-short` targets already set it. Shuffled ordering catches hidden test-to-test coupling
- Do not pass `-count=1` to `go test`. `-count=1` is the default and specifying it wastes tokens and disables the build cache unnecessarily. Omit the flag. If a genuine need to bypass cache arises, confirm with the user first
- Only pass `-count=N` when `N > 1` (e.g. `-count=10` for flake hunting)
- Table-driven tests for Go code
- Use `testify` consistently in Go tests; prefer `require` for setup/preconditions and `assert` for non-blocking checks
- When a test function has more than 3 assertions, create a local helper with `assert := Assert.New(t)` and use the helper methods for the rest of the checks
- Do not use `t.Fatal`, `t.Fatalf`, `t.Error`, `t.Errorf`, `t.Fail`, or `t.FailNow` in tests; use testify assertions instead
- Prefer the generated Go API client in `internal/apiclient` for integration-style API tests
- Use `openTestDB(t)` helper for database tests
- All tests use `t.TempDir()` for temp directories
- Tests should be fast and isolated
- Do not run tests with `-v` (especially `go test`) — default output has enough signal to debug failures, and verbose output wastes tokens. Only use `-v` if the user asks for it or a failure genuinely needs the extra detail

## Build Requirements

- **No CGO required** — uses modernc.org/sqlite (pure Go)
- **Frontend**: Bun for Svelte build/test tooling, embedded via `internal/web/dist/`

## Conventions

- Prefer stdlib over external dependencies
- Use `huma` for the web framework and OpenAPI generation
- Regenerate API artifacts with `make api-generate`; the Go client also supports `go generate ./internal/apiclient/generated`
- **Never use npm** — use `bun install`, `bun run build`, `bun run dev`, etc. for all frontend operations. Never run `npm install` or `npm run` — this creates `package-lock.json` which conflicts with the bun lockfile
- Tests should be fast and isolated
- No emojis in code or output
- Database schema changes must be added as numbered SQL migrations in `internal/db/migrations/`
- `internal/db/migrations/` is the source of truth for schema evolution
- Add both `.up.sql` and `.down.sql` files for schema changes
- Validate schema changes through `db.Open()` and application-level tests rather than testing `golang-migrate` internals
- Datetimes are UTC across storage and API boundaries. Store timestamps in UTC, emit API timestamps as UTC RFC3339, and only convert to local time in the Svelte UI presentation layer.

## Git Workflow

- **Commit every turn** — always commit your work at the end of each turn, no exceptions
- **Never amend commits** — always create new commits for fixes, never use `--amend`
- **Never change branches** — don't create, switch, or delete branches without explicit permission
- Use conventional commit messages
- Run tests before committing when applicable
- Never push or pull unless explicitly asked

## Pull Requests

- PR descriptions should be concise: summarize what changed, not how or why in detail
- No test plans, implementation details, or checklists in PR descriptions
- No marketing language (critical, robust, comprehensive, etc.)
- A bulleted summary of user-visible changes is sufficient
