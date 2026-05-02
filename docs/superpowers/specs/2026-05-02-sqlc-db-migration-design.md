# sqlc DB Migration Design

## Goal

Move the DB layer's static SQLite statements and row models to sqlc-generated Go code while preserving the existing `internal/db.DB` public API used by the sync engine, HTTP server, workspace runtime, and tests.

## Scope

This migration covers the full `internal/db` query surface:

- Core repository, pull request, issue, label, event, kanban, starred item, rate limit, worktree link, workspace, setup event, tmux session, and workspace summary queries in `internal/db/queries.go`.
- Stack queries in `internal/db/queries_stacks.go`.
- Activity feed queries in `internal/db/queries_activity.go`.
- Repository summary and overview queries in `internal/db/queries_repo_summaries.go`.

Queries that are truly dynamic because they assemble optional filters, variable-length `IN` lists, or shared SELECT fragments may keep small hand-written wrappers, but the wrapper should delegate every static leaf query that sqlc can represent. Existing migrations remain the schema source of truth at runtime; sqlc gets a generated schema snapshot derived from the current migrations.

## Architecture

Add sqlc as a checked Go tool and generate an internal package under `internal/db/sqlc`. The generated package should not become the public application DB API. `internal/db.DB` keeps the existing method names and domain structs, owns read/write pool selection, transaction boundaries, timestamp canonicalization, and domain composition such as attaching labels to PRs and issues.

`DB` will hold generated query handles for both pools:

- `readQueries` bound to `ro`.
- `writeQueries` bound to `rw`.

Inside transactions, code will use `sqlc.New(tx)` or `writeQueries.WithTx(tx)` so multi-statement flows stay atomic. Existing domain structs in `internal/db/types.go` remain the types returned to callers unless a generated sqlc row type is already identical enough to alias safely. This avoids leaking sqlc naming and nullability choices through the rest of the application.

## Generated Inputs

Create:

- `sqlc.yaml` with SQLite engine, `database/sql` output, package `sqlc`, and output directory `internal/db/sqlc`.
- `internal/db/sqlc/schema.sql`, generated from all current `*.up.sql` migrations in order.
- Query files under `internal/db/sqlc/queries/`, split by domain so generated files stay readable.

Add Makefile targets:

- `db-schema-generate`: rebuild the sqlc schema snapshot from migrations.
- `sqlc-generate`: run schema generation and `go tool sqlc generate`.

The generated Go files are checked in, matching the existing OpenAPI client pattern.

## Data Flow

Application code continues to call methods such as `UpsertRepo`, `ListRepos`, `UpsertMergeRequest`, `ListActivity`, and `ListWorkspaceSummaries` on `*db.DB`.

Each method performs only application-specific orchestration:

- Canonicalize timestamps before writes.
- Canonicalize repository identifiers before lookups.
- Convert generated nullable fields to existing pointer fields.
- Attach labels or grouped child records after sqlc returns rows.
- Build dynamic WHERE clauses only where required.

All stable SQL text moves into `.sql` files with sqlc annotations such as `:one`, `:many`, `:exec`, `:execresult`, or `:execrows`.

## Error Handling

Existing error behavior must remain stable:

- `sql.ErrNoRows` should continue to map to `nil` returns where current methods do that.
- Context cancellation should still flow through the generated query calls.
- Multi-statement operations should keep current rollback-on-error behavior.
- Error wrapping should preserve the current high-level operation names where tests or callers depend on them.

## Testing

Use existing DB tests as the primary regression suite. Add one focused test or guardrail that fails when generated sqlc files are stale relative to `sqlc.yaml`, query files, or schema snapshot. For changed API/data-flow behavior, prefer existing `internal/db` integration-style tests with real SQLite.

Verification commands:

- `go tool sqlc generate`
- `go test ./internal/db -shuffle=on`
- `go test ./internal/server -shuffle=on` if workspace/API-facing DB methods changed
- `go test ./internal/github -shuffle=on` if sync-facing DB methods changed
- `go test ./... -short -shuffle=on` before the final commit

## Non-Goals

- Do not change the runtime migration system.
- Do not switch SQLite drivers.
- Do not redesign domain models outside the DB package.
- Do not rewrite tests to assert generated implementation details.
