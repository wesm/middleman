package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	tables := []string{
		"middleman_repos",
		"middleman_merge_requests",
		"middleman_mr_events",
		"middleman_kanban_state",
		"middleman_labels",
		"middleman_merge_request_labels",
		"middleman_issue_labels",
	}
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
	for _, tc := range []struct {
		name    string
		version int
	}{
		{name: "schema_version_1", version: 1},
		{name: "schema_version_2", version: 2},
		{name: "schema_version_3", version: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
			dir := t.TempDir()
			path := filepath.Join(dir, "legacy.db")

			raw, err := sql.Open("sqlite", path)
			require.NoError(err)
			_, err = raw.Exec(legacySchemaSQLForTest(t, tc.version))
			require.NoError(err)
			_, err = raw.Exec(
				`CREATE TABLE middleman_schema_version (version INTEGER NOT NULL)`,
			)
			require.NoError(err)
			_, err = raw.Exec(
				`INSERT INTO middleman_schema_version (version) VALUES (?)`,
				tc.version,
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
		})
	}
}

func TestOpenBackfillsLegacyIssueLabelsIntoNormalizedTables(t *testing.T) {
	require := require.New(t)
	path, raw := openSchemaVersion4DBForTest(t)
	defer func() { require.NoError(raw.Close()) }()
	seedLegacyIssueForTest(t, raw, 1, 1, 101, 7, `[{"name":"bug","color":"d73a4a"}]`)

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(d.Close()) })

	var issueLabelCount int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_issue_labels WHERE issue_id = ?`, 1).Scan(&issueLabelCount)
	require.NoError(err)
	require.Equal(1, issueLabelCount)

	var platformID sql.NullInt64
	var name string
	var description string
	var color string
	var isDefault bool
	var updatedAt string
	err = d.ReadDB().QueryRow(
		`SELECT l.platform_id, l.name, l.description, l.color, l.is_default, l.updated_at
		 FROM middleman_labels l
		 JOIN middleman_issue_labels il ON il.label_id = l.id
		 WHERE il.issue_id = ?`,
		1,
	).Scan(&platformID, &name, &description, &color, &isDefault, &updatedAt)
	require.NoError(err)
	require.False(platformID.Valid)
	require.Equal("bug", name)
	require.Empty(description)
	require.Equal("d73a4a", color)
	require.False(isDefault)
	require.NotEmpty(updatedAt)
}

func TestOpenIgnoresMalformedLegacyIssueLabelsJSON(t *testing.T) {
	require := require.New(t)
	path, raw := openSchemaVersion4DBForTest(t)
	defer func() { require.NoError(raw.Close()) }()

	seedLegacyIssueForTest(t, raw, 1, 1, 101, 7, `[{"name":"bug","color":"d73a4a"}`)

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(d.Close()) })

	var labelCount int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_labels`).Scan(&labelCount)
	require.NoError(err)
	require.Equal(0, labelCount)

	var issueLabelCount int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_issue_labels`).Scan(&issueLabelCount)
	require.NoError(err)
	require.Equal(0, issueLabelCount)
}

func TestOpenBackfillsDuplicateLegacyIssueLabelsDeterministically(t *testing.T) {
	require := require.New(t)
	path, raw := openSchemaVersion4DBForTest(t)
	defer func() { require.NoError(raw.Close()) }()

	seedLegacyIssueForTest(t, raw, 1, 1, 101, 7, `[{"name":"bug","color":"ff0000"}]`)
	seedLegacyIssueForTest(t, raw, 2, 1, 102, 8, `[{"name":"bug","color":"00ff00"}]`)

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(d.Close()) })

	var labelCount int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_labels WHERE repo_id = ? AND name = ?`, 1, "bug").Scan(&labelCount)
	require.NoError(err)
	require.Equal(1, labelCount)

	var color string
	err = d.ReadDB().QueryRow(
		`SELECT color FROM middleman_labels WHERE repo_id = ? AND name = ?`,
		1,
		"bug",
	).Scan(&color)
	require.NoError(err)
	require.Equal("00ff00", color)
}

func TestOpenCasefoldsDuplicateRepositoryRows(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(legacySchemaSQLForTest(t, 7))
	require.NoError(err)
	_, err = raw.Exec(`CREATE TABLE schema_migrations (version uint64, dirty bool)`)
	require.NoError(err)
	_, err = raw.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (7, FALSE)`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_repos (
			id, platform, platform_host, owner, name,
			created_at, backfill_pr_page, backfill_pr_complete,
			backfill_issue_page, backfill_issue_complete
		) VALUES
			(1, 'github', 'github.com', 'Org', 'Foo', datetime('now'), 0, 0, 0, 0),
			(2, 'github', 'github.com', 'org', 'foo', datetime('now'), 0, 0, 0, 0)`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_merge_requests (
			id, repo_id, platform_id, number, url, title, author, state,
			created_at, updated_at, last_activity_at
		) VALUES
			(1, 1, 100, 1, 'https://github.com/Org/Foo/pull/1', 'PR', 'octo', 'open',
			 datetime('now'), datetime('now'), datetime('now')),
			(2, 2, 100, 1, 'https://github.com/org/foo/pull/1', 'PR', 'octo', 'open',
			 datetime('now'), datetime('now'), datetime('now')),
			(3, 2, 200, 2, 'https://github.com/org/foo/pull/2', 'Unique PR', 'octo', 'open',
			 datetime('now'), datetime('now'), datetime('now'))`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_mr_events (
			merge_request_id, event_type, author, created_at, dedupe_key
		) VALUES
			(1, 'comment', 'octo', datetime('now'), 'duplicate-pr-comment'),
			(2, 'comment', 'octo', datetime('now'), 'duplicate-pr-comment'),
			(3, 'comment', 'octo', datetime('now'), 'unique-comment')`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_kanban_state (merge_request_id, status, updated_at)
		VALUES
			(1, 'new', '2024-01-01T00:00:00Z'),
			(2, 'reviewing', '2024-01-02T00:00:00Z'),
			(3, 'reviewing', '2024-01-03T00:00:00Z')`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_issues (
			id, repo_id, platform_id, number, url, title, author, state,
			created_at, updated_at, last_activity_at
		) VALUES
			(1, 1, 800, 8, 'https://github.com/Org/Foo/issues/8', 'Issue', 'octo', 'open',
			 datetime('now'), datetime('now'), datetime('now')),
			(2, 2, 800, 8, 'https://github.com/org/foo/issues/8', 'Issue', 'octo', 'open',
			 datetime('now'), datetime('now'), datetime('now')),
			(3, 2, 900, 9, 'https://github.com/org/foo/issues/9', 'Unique issue', 'octo', 'open',
			 datetime('now'), datetime('now'), datetime('now'))`)
	require.NoError(err)
	_, err = raw.Exec(`
		DROP INDEX idx_issue_events_created;
		ALTER TABLE middleman_issue_events RENAME TO middleman_issue_events_strict;
		CREATE TABLE middleman_issue_events (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER NOT NULL REFERENCES middleman_issues(id) ON DELETE CASCADE,
			event_type   TEXT NOT NULL,
			author       TEXT NOT NULL,
			body         TEXT,
			created_at   TEXT NOT NULL,
			dedupe_key   TEXT NOT NULL,
			UNIQUE(issue_id, dedupe_key)
		);
		CREATE INDEX idx_issue_events_created
			ON middleman_issue_events(issue_id, created_at DESC);
		DROP TABLE middleman_issue_events_strict;`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_issue_events (
			issue_id, event_type, author, created_at, dedupe_key
		) VALUES
			(1, 'comment', 'octo', datetime('now'), 'duplicate-issue-comment'),
			(2, 'comment', 'octo', datetime('now'), 'duplicate-issue-comment')`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_labels (
			id, repo_id, platform_id, name, updated_at
		) VALUES
			(1, 1, 700, 'enhancement-renamed', datetime('now')),
			(2, 2, 700, 'enhancement', datetime('now')),
			(3, 2, 701, 'triage', datetime('now')),
			(4, 1, NULL, 'stale-label', datetime('now')),
			(5, 2, 702, 'stale-label', datetime('now'))`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_issue_labels (issue_id, label_id)
		VALUES
			(2, 3),
			(3, 2)`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_merge_request_labels (merge_request_id, label_id)
		VALUES (3, 2)`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_starred_items (item_type, repo_id, number)
		VALUES ('issue', 2, 9)`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_stacks (id, repo_id, base_number, name)
		VALUES (1, 2, 2, 'Unique stack')`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_workspaces (
			id, platform_host, repo_owner, repo_name, mr_number, mr_head_ref,
			worktree_path, tmux_session
		) VALUES
			('one', 'github.com', 'Org', 'Foo', 1, 'feature', '/tmp/one', 'one'),
			('two', 'github.com', 'org', 'foo', 1, 'feature', '/tmp/two', 'two'),
			('three', 'github.com', 'org', 'foo', 2, 'feature-2', '/tmp/three', 'three')`)
	require.NoError(err)
	require.NoError(raw.Close())

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(d.Close()) })

	repos, err := d.ListRepos(context.Background())
	require.NoError(err)
	require.Len(repos, 1)
	require.Equal("org", repos[0].Owner)
	require.Equal("foo", repos[0].Name)

	var prCount int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_merge_requests`).Scan(&prCount)
	require.NoError(err)
	require.Equal(2, prCount)

	var uniquePRRepoID int
	err = d.ReadDB().QueryRow(
		`SELECT repo_id FROM middleman_merge_requests WHERE number = 2`,
	).Scan(&uniquePRRepoID)
	require.NoError(err)
	require.Equal(1, uniquePRRepoID)

	var uniquePREventCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_mr_events e
		JOIN middleman_merge_requests mr ON mr.id = e.merge_request_id
		WHERE mr.number = 2`,
	).Scan(&uniquePREventCount)
	require.NoError(err)
	require.Equal(1, uniquePREventCount)

	var duplicatePREventCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_mr_events e
		JOIN middleman_merge_requests mr ON mr.id = e.merge_request_id
		WHERE mr.number = 1 AND e.dedupe_key = 'duplicate-pr-comment'`,
	).Scan(&duplicatePREventCount)
	require.NoError(err)
	require.Equal(1, duplicatePREventCount)

	var kanbanStatus string
	err = d.ReadDB().QueryRow(`
		SELECT ks.status
		FROM middleman_kanban_state ks
		JOIN middleman_merge_requests mr ON mr.id = ks.merge_request_id
		WHERE mr.number = 2`,
	).Scan(&kanbanStatus)
	require.NoError(err)
	require.Equal("reviewing", kanbanStatus)

	var mergedKanbanStatus string
	err = d.ReadDB().QueryRow(`
		SELECT ks.status
		FROM middleman_kanban_state ks
		JOIN middleman_merge_requests mr ON mr.id = ks.merge_request_id
		WHERE mr.number = 1`,
	).Scan(&mergedKanbanStatus)
	require.NoError(err)
	require.Equal("reviewing", mergedKanbanStatus)

	var duplicateIssueEventCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_issue_events e
		JOIN middleman_issues i ON i.id = e.issue_id
		WHERE i.number = 8 AND e.dedupe_key = 'duplicate-issue-comment'`,
	).Scan(&duplicateIssueEventCount)
	require.NoError(err)
	require.Equal(1, duplicateIssueEventCount)

	var duplicateIssueLabelCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_issue_labels il
		JOIN middleman_issues i ON i.id = il.issue_id
		JOIN middleman_labels l ON l.id = il.label_id
		WHERE i.number = 8 AND l.name = 'triage'`,
	).Scan(&duplicateIssueLabelCount)
	require.NoError(err)
	require.Equal(1, duplicateIssueLabelCount)

	var issueRepoID int
	err = d.ReadDB().QueryRow(
		`SELECT repo_id FROM middleman_issues WHERE number = 9`,
	).Scan(&issueRepoID)
	require.NoError(err)
	require.Equal(1, issueRepoID)

	var labelRepoID int
	err = d.ReadDB().QueryRow(
		`SELECT repo_id FROM middleman_labels WHERE platform_id = 700`,
	).Scan(&labelRepoID)
	require.NoError(err)
	require.Equal(1, labelRepoID)

	var issuePlatformLabelCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_issue_labels il
		JOIN middleman_issues i ON i.id = il.issue_id
		JOIN middleman_labels l ON l.id = il.label_id
		WHERE i.number = 9 AND l.platform_id = 700`,
	).Scan(&issuePlatformLabelCount)
	require.NoError(err)
	require.Equal(1, issuePlatformLabelCount)

	var staleNamePlatformLabelCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_labels
		WHERE repo_id = 1 AND name = 'stale-label' AND platform_id = 702`,
	).Scan(&staleNamePlatformLabelCount)
	require.NoError(err)
	require.Equal(1, staleNamePlatformLabelCount)

	var mrPlatformLabelCount int
	err = d.ReadDB().QueryRow(`
		SELECT COUNT(*)
		FROM middleman_merge_request_labels mrl
		JOIN middleman_merge_requests mr ON mr.id = mrl.merge_request_id
		JOIN middleman_labels l ON l.id = mrl.label_id
		WHERE mr.number = 2 AND l.platform_id = 700`,
	).Scan(&mrPlatformLabelCount)
	require.NoError(err)
	require.Equal(1, mrPlatformLabelCount)

	var starredRepoID int
	err = d.ReadDB().QueryRow(
		`SELECT repo_id FROM middleman_starred_items WHERE item_type = 'issue' AND number = 9`,
	).Scan(&starredRepoID)
	require.NoError(err)
	require.Equal(1, starredRepoID)

	var stackRepoID int
	err = d.ReadDB().QueryRow(
		`SELECT repo_id FROM middleman_stacks WHERE base_number = 2`,
	).Scan(&stackRepoID)
	require.NoError(err)
	require.Equal(1, stackRepoID)

	var workspaceCount int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_workspaces`).Scan(&workspaceCount)
	require.NoError(err)
	require.Equal(2, workspaceCount)

	var integrityCheck string
	err = d.ReadDB().QueryRow(`PRAGMA integrity_check`).Scan(&integrityCheck)
	require.NoError(err)
	require.Equal("ok", integrityCheck)

	var foreignKeyViolations int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyViolations)
	require.NoError(err)
	require.Zero(foreignKeyViolations)
}

func TestOpenRejectsUnsupportedLegacySchemaVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
	}{
		{name: "version_0", version: 0},
		{name: "version_99", version: 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testRejectsUnsupportedLegacySchemaVersion(t, tc.version)
		})
	}
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

func testRejectsUnsupportedLegacySchemaVersion(t *testing.T, version int) {
	t.Helper()
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(legacySchemaSQLForTest(t, 3))
	require.NoError(err)
	_, err = raw.Exec(
		`CREATE TABLE middleman_schema_version (version INTEGER NOT NULL)`,
	)
	require.NoError(err)
	_, err = raw.Exec(
		`INSERT INTO middleman_schema_version (version) VALUES (?)`,
		version,
	)
	require.NoError(err)
	require.NoError(raw.Close())

	_, err = Open(path)
	require.Error(err)
	if version == 0 {
		require.Contains(err.Error(), recreateDatabaseInstruction)
		require.Contains(err.Error(), "is invalid")
		return
	}
	require.Contains(err.Error(), "newer than this binary")
}

func legacySchemaSQLForTest(t *testing.T, version int) string {
	t.Helper()
	parts := make([]string, 0, version)
	for i := 1; i <= version; i++ {
		contents, err := fs.ReadFile(
			migrationFiles,
			filepath.Join("migrations", legacyMigrationFilenameForTest(i)),
		)
		require.NoError(t, err)
		parts = append(parts, string(contents))
	}
	return strings.Join(parts, "\n")
}

func legacyMigrationFilenameForTest(version int) string {
	switch version {
	case 1:
		return "000001_initial_schema.up.sql"
	case 2:
		return "000002_update_mr_events_dedupe.up.sql"
	case 3:
		return "000003_add_backfill_and_detail_columns.up.sql"
	case 4:
		return "000004_drop_legacy_schema_version.up.sql"
	case 5:
		return "000005_graphql_sync_and_labels.up.sql"
	case 6:
		return "000006_add_stacks.up.sql"
	case 7:
		return "000007_add_workspaces.up.sql"
	default:
		return ""
	}
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

func openSchemaVersion4DBForTest(t *testing.T) (string, *sql.DB) {
	t.Helper()
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(legacySchemaSQLForTest(t, 4))
	require.NoError(err)
	_, err = raw.Exec(`CREATE TABLE schema_migrations (version uint64, dirty bool)`)
	require.NoError(err)
	_, err = raw.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (4, FALSE)`)
	require.NoError(err)
	_, err = raw.Exec(
		`INSERT INTO middleman_repos (
			id, platform, platform_host, owner, name,
			created_at, backfill_pr_page, backfill_pr_complete,
			backfill_issue_page, backfill_issue_complete
		) VALUES (?, 'github', 'github.com', 'octo', 'repo', datetime('now'), 0, 0, 0, 0)`,
		1,
	)
	require.NoError(err)

	return path, raw
}

func seedLegacyIssueForTest(
	t *testing.T,
	raw *sql.DB,
	id int,
	repoID int,
	platformID int,
	number int,
	labelsJSON string,
) {
	t.Helper()
	_, err := raw.Exec(
		`INSERT INTO middleman_issues (
			id, repo_id, platform_id, number, url, title, author, state,
			body, comment_count, labels_json, created_at, updated_at, last_activity_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'), datetime('now'))`,
		id,
		repoID,
		platformID,
		number,
		"https://github.com/octo/repo/issues/test",
		"Backfill labels",
		"octocat",
		"open",
		"",
		0,
		labelsJSON,
	)
	require.NoError(t, err)
}
