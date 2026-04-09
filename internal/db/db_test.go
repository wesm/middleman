package db

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenAndSchema(t *testing.T) {
	d := openTestDB(t)
	tables := []string{"middleman_repos", "middleman_merge_requests", "middleman_mr_events", "middleman_kanban_state"}
	for _, tbl := range tables {
		var name string
		err := d.ReadDB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		require.NoErrorf(t, err, "table %s should exist", tbl)
	}
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.db")
	d, err := Open(path)
	require.NoError(t, err)
	d.Close()
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d1, err := Open(path)
	require.NoError(t, err)
	d1.Close()
	d2, err := Open(path)
	require.NoError(t, err)
	d2.Close()
}

func TestOpenCreatesSchemaMigrationsTable(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)

	version := latestMigrationVersionForTest(t)
	var actualVersion int
	var dirty bool
	err := d.ReadDB().QueryRow(
		`SELECT version, dirty FROM schema_migrations LIMIT 1`,
	).Scan(&actualVersion, &dirty)
	require.NoError(err)
	require.Equal(version, actualVersion)
	require.False(dirty)
}

func TestOpenMigratesLegacyDatabase(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(initialMigrationSQLForTest(t))
	require.NoError(err)
	_, err = raw.Exec(
		`CREATE TABLE middleman_schema_version (version INTEGER NOT NULL)`,
	)
	require.NoError(err)
	_, err = raw.Exec(
		`INSERT INTO middleman_schema_version (version) VALUES (3)`,
	)
	require.NoError(err)
	require.NoError(raw.Close())

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(d.Close()) })

	version := latestMigrationVersionForTest(t)
	var actualVersion int
	var dirty bool
	err = d.ReadDB().QueryRow(
		`SELECT version, dirty FROM schema_migrations LIMIT 1`,
	).Scan(&actualVersion, &dirty)
	require.NoError(err)
	require.Equal(version, actualVersion)
	require.False(dirty)
	require.False(tableExistsForTest(t, d.ReadDB(), "middleman_schema_version"))
}

func TestOpenReturnsRecreateGuidanceForDirtyMigrations(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(
		`CREATE TABLE schema_migrations (version uint64, dirty bool)`,
	)
	require.NoError(err)
	_, err = raw.Exec(
		`INSERT INTO schema_migrations (version, dirty) VALUES (1, TRUE)`,
	)
	require.NoError(err)
	require.NoError(raw.Close())

	_, err = Open(path)
	require.Error(err)
	require.Contains(err.Error(), recreateDatabaseInstruction)
}

func TestOpenRejectsIncompleteLegacyDatabase(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "broken-legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(`CREATE TABLE middleman_repos (id INTEGER PRIMARY KEY)`)
	require.NoError(err)
	require.NoError(raw.Close())

	_, err = Open(path)
	require.Error(err)
	require.Contains(err.Error(), recreateDatabaseInstruction)
}

func initialMigrationSQLForTest(t *testing.T) string {
	t.Helper()
	contents, err := fs.ReadFile(
		migrationFiles,
		"migrations/000001_initial_schema.up.sql",
	)
	require.NoError(t, err)
	return string(contents)
}

func latestMigrationVersionForTest(t *testing.T) int {
	t.Helper()
	version, err := latestMigrationVersion()
	require.NoError(t, err)
	return version
}

func tableExistsForTest(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&count)
	require.NoError(t, err)
	return count > 0
}
