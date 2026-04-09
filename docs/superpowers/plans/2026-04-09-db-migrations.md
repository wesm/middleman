# DB Migrations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace custom schema version bootstrapping with embedded `golang-migrate` SQLite migrations and update agent instructions to treat numbered migrations as the database schema source of truth.

**Architecture:** `internal/db/db.go` will continue to open SQLite connections and enable WAL, but it will hand schema setup to embedded `golang-migrate` migrations loaded from `internal/db/migrations/` via `source/iofs`. Legacy local databases with `middleman_*` tables and no migration metadata will be seeded to baseline version `1`; on migration failure, startup returns a direct delete-and-recreate instruction.

**Tech Stack:** Go, `github.com/golang-migrate/migrate/v4`, `modernc.org/sqlite`, embedded `io/fs`, `testify`

---

### Task 1: Convert Schema Source Of Truth To Migration Files

**Files:**
- Create: `internal/db/migrations/000001_initial_schema.up.sql`
- Create: `internal/db/migrations/000001_initial_schema.down.sql`
- Delete: `internal/db/schema.sql`

- [ ] **Step 1: Copy the current schema into the first up migration**

```sql
-- internal/db/migrations/000001_initial_schema.up.sql
CREATE TABLE IF NOT EXISTS middleman_repos (...);
CREATE TABLE IF NOT EXISTS middleman_merge_requests (...);
CREATE TABLE IF NOT EXISTS middleman_mr_events (...);
CREATE TABLE IF NOT EXISTS middleman_kanban_state (...);
CREATE TABLE IF NOT EXISTS middleman_issues (...);
CREATE TABLE IF NOT EXISTS middleman_issue_events (...);
CREATE TABLE IF NOT EXISTS middleman_starred_items (...);
CREATE TABLE IF NOT EXISTS middleman_mr_worktree_links (...);
CREATE TABLE IF NOT EXISTS middleman_rate_limits (...);
CREATE INDEX IF NOT EXISTS idx_mr_repo_state_activity ...;
...
```

- [ ] **Step 2: Add the matching down migration**

```sql
-- internal/db/migrations/000001_initial_schema.down.sql
DROP TABLE IF EXISTS middleman_rate_limits;
DROP TABLE IF EXISTS middleman_mr_worktree_links;
DROP TABLE IF EXISTS middleman_starred_items;
DROP TABLE IF EXISTS middleman_issue_events;
DROP TABLE IF EXISTS middleman_issues;
DROP TABLE IF EXISTS middleman_kanban_state;
DROP TABLE IF EXISTS middleman_mr_events;
DROP TABLE IF EXISTS middleman_merge_requests;
DROP TABLE IF EXISTS middleman_repos;
```

- [ ] **Step 3: Delete the standalone schema snapshot**

Remove `internal/db/schema.sql` so future schema changes only happen through numbered migrations.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/000001_initial_schema.up.sql internal/db/migrations/000001_initial_schema.down.sql internal/db/schema.sql
git commit -m "refactor: move sqlite schema into migrations"
```

### Task 2: Add A Failing Legacy Migration Bootstrap Test

**Files:**
- Modify: `internal/db/db_test.go`
- Test: `internal/db/db_test.go`

- [ ] **Step 1: Replace schema-version tests with migration integration tests**

```go
func TestOpenMigratesLegacyDatabase(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(`CREATE TABLE middleman_repos (id INTEGER PRIMARY KEY AUTOINCREMENT)`)
	require.NoError(err)
	require.NoError(raw.Close())

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(d.Close()) })

	var version int
	var dirty bool
	err = d.ReadDB().QueryRow(`SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	require.NoError(err)
	require.Equal(1, version)
	require.False(dirty)
}
```

- [ ] **Step 2: Run the new focused test and verify it fails**

Run: `go test ./internal/db -run TestOpenMigratesLegacyDatabase -count=1`
Expected: FAIL because `Open` still rejects legacy databases or still uses custom schema version logic.

- [ ] **Step 3: Commit the failing-test checkpoint only if your workflow requires it**

```bash
git add internal/db/db_test.go
git commit -m "test: cover legacy database migration bootstrap"
```

### Task 3: Replace Custom Schema Version Logic With Embedded Migrations

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/migrations.go`

- [ ] **Step 1: Add embedded migration setup helpers**

```go
// internal/db/migrations.go
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func runMigrations(rw *sql.DB) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	...
}
```

- [ ] **Step 2: Seed legacy databases to baseline version 1 when needed**

```go
func seedLegacyBaseline(rw *sql.DB) error {
	_, err := rw.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version uint64, dirty bool)`)
	if err != nil {
		return err
	}
	_, err = rw.Exec(`DELETE FROM schema_migrations`)
	if err != nil {
		return err
	}
	_, err = rw.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (1, FALSE)`)
	return err
}
```

- [ ] **Step 3: Update `Open` to call the migration runner**

```go
func (d *DB) init() error {
	if _, err := d.rw.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}
	if err := runMigrations(d.rw); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Remove `SchemaVersion`, `readSchemaVersion`, `writeSchemaVersion`, and `middleman_schema_version` logic**

Delete the old version-table helpers and replace the top-level comments with migration-based behavior.

- [ ] **Step 5: Run the focused test and verify it passes**

Run: `go test ./internal/db -run TestOpenMigratesLegacyDatabase -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/db/db.go internal/db/migrations.go internal/db/db_test.go go.mod go.sum
git commit -m "feat: run embedded sqlite migrations on startup"
```

### Task 4: Cover Fresh Open And Reopen Through The Migration Path

**Files:**
- Modify: `internal/db/db_test.go`
- Test: `internal/db/db_test.go`

- [ ] **Step 1: Add a fresh database smoke test through `schema_migrations`**

```go
func TestOpenCreatesSchemaMigrationsTable(t *testing.T) {
	d := openTestDB(t)
	var version int
	var dirty bool
	err := d.ReadDB().QueryRow(`SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	require.NoError(t, err)
	require.Equal(t, 1, version)
	require.False(t, dirty)
}
```

- [ ] **Step 2: Keep the reopen test and route it through the migrated DB path**

```go
func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d1, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, d1.Close())
	d2, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, d2.Close())
}
```

- [ ] **Step 3: Run the targeted db test package**

Run: `go test ./internal/db -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/db/db_test.go
git commit -m "test: cover database open through migrations"
```

### Task 5: Update Agent Instructions For Migration Workflow

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Replace the old schema-version guidance**

```md
- Database schema changes must be added as numbered SQL migrations in `internal/db/migrations/`
- `internal/db/migrations/` is the source of truth for schema evolution
- Add both `.up.sql` and `.down.sql` files for schema changes
- Validate schema changes through `db.Open()` and application-level tests rather than testing migration-library internals
```

- [ ] **Step 2: Update key file references if needed**

Change any references that still present `internal/db/schema.sql` as the schema source of truth.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md CLAUDE.md
git commit -m "docs: document migration-based schema workflow"
```

### Task 6: Verify End-To-End And Clean Up

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Tidy module dependencies**

Run: `go mod tidy`
Expected: `golang-migrate` and any new transitive dependencies are recorded in `go.mod` and `go.sum`

- [ ] **Step 2: Run focused verification**

Run: `go test ./internal/db -count=1`
Expected: PASS

- [ ] **Step 3: Run broader verification if the db package changes compile surface area**

Run: `make test-short`
Expected: PASS

- [ ] **Step 4: Commit final cleanup**

```bash
git add go.mod go.sum
git commit -m "chore: tidy migration dependencies"
```
