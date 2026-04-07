package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

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

// TestMigrateCascadeFKRetrofitsExistingDB creates a database with old-style
// FKs (no CASCADE), re-opens it to trigger the migration, and verifies that
// deleting a repo now cascades through dependent child tables.
func TestMigrateCascadeFKRetrofitsExistingDB(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	// Create a database with the old schema (no ON DELETE CASCADE on child tables).
	rawDB, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	require.NoError(err)
	_, err = rawDB.Exec("PRAGMA journal_mode=WAL")
	require.NoError(err)

	// Only create the tables we need, using the OLD FK definitions.
	oldSchema := `
	CREATE TABLE IF NOT EXISTS middleman_repos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL DEFAULT 'github',
		platform_host TEXT NOT NULL DEFAULT 'github.com',
		owner TEXT NOT NULL,
		name TEXT NOT NULL,
		last_sync_started_at DATETIME,
		last_sync_completed_at DATETIME,
		last_sync_error TEXT DEFAULT '',
		allow_squash_merge INTEGER NOT NULL DEFAULT 1,
		allow_merge_commit INTEGER NOT NULL DEFAULT 1,
		allow_rebase_merge INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		UNIQUE(platform, platform_host, owner, name)
	);
	CREATE TABLE IF NOT EXISTS middleman_merge_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		repo_id INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE,
		platform_id INTEGER NOT NULL,
		number INTEGER NOT NULL,
		url TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		author_display_name TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'open',
		is_draft INTEGER NOT NULL DEFAULT 0,
		body TEXT NOT NULL DEFAULT '',
		head_branch TEXT NOT NULL DEFAULT '',
		base_branch TEXT NOT NULL DEFAULT '',
		additions INTEGER NOT NULL DEFAULT 0,
		deletions INTEGER NOT NULL DEFAULT 0,
		comment_count INTEGER NOT NULL DEFAULT 0,
		review_decision TEXT NOT NULL DEFAULT '',
		ci_status TEXT NOT NULL DEFAULT '',
		ci_checks_json TEXT NOT NULL DEFAULT '',
		platform_head_sha TEXT NOT NULL DEFAULT '',
		platform_base_sha TEXT NOT NULL DEFAULT '',
		diff_head_sha TEXT NOT NULL DEFAULT '',
		diff_base_sha TEXT NOT NULL DEFAULT '',
		merge_base_sha TEXT NOT NULL DEFAULT '',
		head_repo_clone_url TEXT NOT NULL DEFAULT '',
		mergeable_state TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_activity_at DATETIME NOT NULL,
		merged_at DATETIME,
		closed_at DATETIME,
		UNIQUE(repo_id, number),
		UNIQUE(repo_id, platform_id)
	);
	-- OLD: no ON DELETE CASCADE
	CREATE TABLE IF NOT EXISTS middleman_mr_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id),
		platform_id INTEGER,
		event_type TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		metadata_json TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		dedupe_key TEXT NOT NULL,
		UNIQUE(dedupe_key)
	);
	-- OLD: no ON DELETE CASCADE
	CREATE TABLE IF NOT EXISTS middleman_kanban_state (
		merge_request_id INTEGER PRIMARY KEY REFERENCES middleman_merge_requests(id),
		status TEXT NOT NULL DEFAULT 'new',
		updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS middleman_issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		repo_id INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE,
		platform_id INTEGER NOT NULL,
		number INTEGER NOT NULL,
		url TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'open',
		body TEXT NOT NULL DEFAULT '',
		comment_count INTEGER NOT NULL DEFAULT 0,
		labels_json TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_activity_at DATETIME NOT NULL,
		closed_at DATETIME,
		UNIQUE(repo_id, number),
		UNIQUE(repo_id, platform_id)
	);
	-- OLD: no ON DELETE CASCADE
	CREATE TABLE IF NOT EXISTS middleman_issue_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		issue_id INTEGER NOT NULL REFERENCES middleman_issues(id),
		platform_id INTEGER,
		event_type TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		metadata_json TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		dedupe_key TEXT NOT NULL,
		UNIQUE(dedupe_key)
	);
	`
	_, err = rawDB.Exec(oldSchema)
	require.NoError(err)

	// Insert test data: repo -> MR -> events + kanban; repo -> issue -> events.
	now := "2024-01-15 12:00:00"
	_, err = rawDB.Exec(
		`INSERT INTO middleman_repos (platform, platform_host, owner, name) VALUES ('github','github.com','acme','app')`,
	)
	require.NoError(err)
	_, err = rawDB.Exec(
		`INSERT INTO middleman_merge_requests (repo_id, platform_id, number, created_at, updated_at, last_activity_at) VALUES (1, 100, 1, ?, ?, ?)`,
		now, now, now,
	)
	require.NoError(err)
	_, err = rawDB.Exec(
		`INSERT INTO middleman_mr_events (merge_request_id, event_type, created_at, dedupe_key) VALUES (1, 'comment', ?, 'evt-1')`, now,
	)
	require.NoError(err)
	_, err = rawDB.Exec(
		`INSERT INTO middleman_kanban_state (merge_request_id, status) VALUES (1, 'reviewing')`,
	)
	require.NoError(err)
	_, err = rawDB.Exec(
		`INSERT INTO middleman_issues (repo_id, platform_id, number, created_at, updated_at, last_activity_at) VALUES (1, 200, 10, ?, ?, ?)`,
		now, now, now,
	)
	require.NoError(err)
	_, err = rawDB.Exec(
		`INSERT INTO middleman_issue_events (issue_id, event_type, created_at, dedupe_key) VALUES (1, 'comment', ?, 'ievt-1')`, now,
	)
	require.NoError(err)
	rawDB.Close()

	// Re-open via db.Open — this runs init() + migrate(), which should
	// retrofit the CASCADE constraints.
	d, err := Open(path)
	require.NoError(err)
	defer d.Close()

	// Deleting the repo should now cascade through all child tables.
	_, err = d.WriteDB().Exec(`DELETE FROM middleman_repos WHERE id = 1`)
	require.NoError(err, "repo delete should cascade after migration")

	var count int
	require.NoError(d.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM middleman_merge_requests`,
	).Scan(&count))
	require.Equal(0, count, "MRs should be cascaded")

	require.NoError(d.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM middleman_mr_events`,
	).Scan(&count))
	require.Equal(0, count, "MR events should be cascaded")

	require.NoError(d.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM middleman_kanban_state`,
	).Scan(&count))
	require.Equal(0, count, "kanban state should be cascaded")

	require.NoError(d.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM middleman_issues`,
	).Scan(&count))
	require.Equal(0, count, "issues should be cascaded")

	require.NoError(d.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM middleman_issue_events`,
	).Scan(&count))
	require.Equal(0, count, "issue events should be cascaded")
}

func TestMigrateMergeableState(t *testing.T) {
	d := openTestDB(t)
	var val string
	err := d.ReadDB().QueryRow(
		"SELECT mergeable_state FROM middleman_merge_requests LIMIT 0",
	).Scan(&val)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
