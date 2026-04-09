# DB Migrations Design

## Goal

Replace the ad hoc `SchemaVersion`/`middleman_schema_version` startup checks with embedded SQL migrations powered by `github.com/golang-migrate/migrate/v4`, while keeping SQLite pure-Go and preserving the current `db.Open()` startup flow.

## Context

The current database bootstrap logic in `internal/db/db.go` embeds `schema.sql`, stamps a custom schema version table, and refuses to open databases whose version does not exactly match the binary. That blocks forward schema evolution and forces users to delete their database after schema changes.

This repository already uses `modernc.org/sqlite`, so any migration solution must stay CGO-free.

## Decision

Adopt `golang-migrate` as the single source of truth for database schema state.

- Use embedded SQL migrations from `internal/db/migrations/`.
- Use `source/iofs` so migrations ship inside the Go binary.
- Use the library's `database/sqlite` driver so the project remains on `modernc.org/sqlite`.
- Remove runtime dependence on `SchemaVersion`, `schema.sql`, and `middleman_schema_version`.
- Let `schema_migrations` be the authoritative version table.

## Migration Layout

Create numbered migration pairs under `internal/db/migrations/`.

Initial structure:

- `000001_initial_schema.up.sql`
- `000001_initial_schema.down.sql`

`000001_initial_schema.up.sql` will contain the full current schema from `internal/db/schema.sql`.

Subsequent schema changes will be expressed only as new numbered migrations. The migration directory becomes the schema source of truth.

## Runtime Flow

`db.Open(path)` will keep responsibility for opening the SQLite connections and enabling WAL. After that it will run migrations before returning the `*DB` handle.

Proposed startup sequence:

1. Open the read-write and read-only SQLite handles.
2. Enable `PRAGMA journal_mode=WAL` on the write handle.
3. Detect whether `schema_migrations` exists.
4. If `schema_migrations` exists, run `m.Up()` and allow `migrate.ErrNoChange`.
5. If `schema_migrations` does not exist but existing `middleman_*` tables do exist, treat the database as a legacy baseline and seed `schema_migrations` to version `1`.
6. Run forward migrations to the latest version.
7. If migration fails or the database is dirty, return an error instructing the user to delete the database file and let middleman recreate it.

## Legacy Database Handling

Legacy databases are databases that predate `schema_migrations` but already contain the expected `middleman_*` tables.

The chosen policy is intentionally narrow:

- Assume the pre-migration production schema corresponds to migration version `1`.
- Seed the migration metadata to version `1` only for that legacy shape.
- If that assumption is wrong, or if any migration step fails, stop startup and give the user a direct delete-and-recreate instruction.

This keeps upgrade logic simple and avoids inventing a broad legacy compatibility layer.

## Error Handling

Migration failures should produce actionable startup errors.

Expected error behavior:

- Dirty migration state: tell the user the database migration failed and they should delete the DB file and restart middleman.
- Failed baseline inference or failed migration execution: same instruction.
- Newer migration state than the binary understands: preserve the current style of explicit upgrade guidance.

The user-facing message should favor direct recovery over deep internal details.

## Code Changes

Expected implementation changes:

- Add `golang-migrate` dependencies to `go.mod`.
- Add embedded migration SQL files under `internal/db/migrations/`.
- Replace schema bootstrap logic in `internal/db/db.go` with a migration runner.
- Add a small helper for migration setup and legacy baseline seeding.
- Delete the old custom schema version helpers and comments that describe the no-migrations behavior.
- Update `AGENTS.md` and `CLAUDE.md` so future agents use numbered migrations instead of editing a standalone schema snapshot.

## Testing

Do not add tests whose purpose is to re-verify `golang-migrate` internals or schema-version bookkeeping owned by the library.

Keep test coverage focused on middleman's integration points:

- `db.Open()` still succeeds for a fresh database.
- `db.Open()` is still idempotent on reopen.
- A legacy unversioned middleman database can be opened through the new startup path when baseline seeding applies.
- Migration failures return a user-facing delete-and-recreate instruction.

Remove the existing custom schema version tests because that behavior is being deleted.

## Agent Guidance Updates

Update both `AGENTS.md` and `CLAUDE.md` to state:

- Database schema changes must be added as numbered SQL migrations in `internal/db/migrations/`.
- The migration directory is the source of truth for schema evolution.
- Agents must add both `.up.sql` and `.down.sql` files for schema changes.
- Agents should validate behavior through `db.Open()` and application-level tests rather than testing migration-library internals.

## Out Of Scope

- Adding a standalone migration CLI.
- Supporting arbitrary third-party local schema variants.
- Backfilling exhaustive historical version detection beyond the single legacy baseline assumption.
