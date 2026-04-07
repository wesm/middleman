package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// DB holds separate read-write and read-only connections to the SQLite database.
type DB struct {
	rw *sql.DB
	ro *sql.DB
}

// Open opens (or creates) a SQLite database at path, applies the schema, and
// enables WAL mode.
func Open(path string) (*DB, error) {
	rw, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	rw.SetMaxOpenConns(1)

	ro, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		rw.Close()
		return nil, fmt.Errorf("open db read-only: %w", err)
	}
	ro.SetMaxOpenConns(4)

	d := &DB{rw: rw, ro: ro}
	if err := d.init(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) init() error {
	if _, err := d.rw.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := d.rw.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	d.migrate()
	return nil
}

// migrate drops legacy table names and retrofits schema changes
// that CREATE TABLE IF NOT EXISTS cannot apply to existing databases.
func (d *DB) migrate() {
	oldTables := []string{
		"repos", "pull_requests", "pr_events",
		"kanban_state", "issues", "issue_events", "starred_items",
	}
	for _, t := range oldTables {
		_, _ = d.rw.Exec("DROP TABLE IF EXISTS " + t)
	}
	oldIndexes := []string{
		"idx_pr_repo_state_activity", "idx_pr_state_activity",
		"idx_events_pr_created",
	}
	for _, idx := range oldIndexes {
		_, _ = d.rw.Exec("DROP INDEX IF EXISTS " + idx)
	}

	d.migrateCascadeFK()
}

// migrateCascadeFK recreates child tables that lacked ON DELETE CASCADE.
// SQLite does not support ALTER TABLE to change FK constraints, so we
// recreate each table and copy data over. Only runs when the existing
// table is missing CASCADE (detected via table_info pragma on the DDL).
func (d *DB) migrateCascadeFK() {
	type migration struct {
		table  string
		check  string // SQL fragment to detect if migration is needed
		create string // full CREATE TABLE with CASCADE
		cols   string // column list for INSERT...SELECT
	}
	migrations := []migration{
		{
			table: "middleman_mr_events",
			check: "middleman_merge_requests(id) ON DELETE CASCADE",
			create: `CREATE TABLE middleman_mr_events_new (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
				platform_id      INTEGER,
				event_type       TEXT NOT NULL,
				author           TEXT NOT NULL DEFAULT '',
				summary          TEXT NOT NULL DEFAULT '',
				body             TEXT NOT NULL DEFAULT '',
				metadata_json    TEXT NOT NULL DEFAULT '',
				created_at       DATETIME NOT NULL,
				dedupe_key       TEXT NOT NULL,
				UNIQUE(dedupe_key)
			)`,
			cols: "id, merge_request_id, platform_id, event_type, author, summary, body, metadata_json, created_at, dedupe_key",
		},
		{
			table: "middleman_kanban_state",
			check: "middleman_merge_requests(id) ON DELETE CASCADE",
			create: `CREATE TABLE middleman_kanban_state_new (
				merge_request_id INTEGER PRIMARY KEY REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
				status           TEXT NOT NULL DEFAULT 'new',
				updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
			cols: "merge_request_id, status, updated_at",
		},
		{
			table: "middleman_issue_events",
			check: "middleman_issues(id) ON DELETE CASCADE",
			create: `CREATE TABLE middleman_issue_events_new (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id      INTEGER NOT NULL REFERENCES middleman_issues(id) ON DELETE CASCADE,
				platform_id   INTEGER,
				event_type    TEXT NOT NULL,
				author        TEXT NOT NULL DEFAULT '',
				summary       TEXT NOT NULL DEFAULT '',
				body          TEXT NOT NULL DEFAULT '',
				metadata_json TEXT NOT NULL DEFAULT '',
				created_at    DATETIME NOT NULL,
				dedupe_key    TEXT NOT NULL,
				UNIQUE(dedupe_key)
			)`,
			cols: "id, issue_id, platform_id, event_type, author, summary, body, metadata_json, created_at, dedupe_key",
		},
	}

	for _, m := range migrations {
		if d.tableHasCascade(m.table, m.check) {
			continue
		}
		newTable := m.table + "_new"
		stmts := []string{
			m.create,
			fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
				newTable, m.cols, m.cols, m.table),
			"DROP TABLE " + m.table,
			fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
				newTable, m.table),
		}
		for _, stmt := range stmts {
			if _, err := d.rw.Exec(stmt); err != nil {
				// Log but don't fail — the old schema still works
				// with explicit FK-order deletes in PurgeOtherHosts.
				return
			}
		}
	}
}

// tableHasCascade checks whether the CREATE TABLE DDL for the given
// table contains the expected CASCADE fragment.
func (d *DB) tableHasCascade(table, fragment string) bool {
	var ddl string
	err := d.rw.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name=?",
		table,
	).Scan(&ddl)
	if err != nil {
		return true // table doesn't exist yet; schema.sql will create it
	}
	return strings.Contains(ddl, fragment)
}

// Close closes both database connections.
func (d *DB) Close() error {
	d.ro.Close()
	return d.rw.Close()
}

// ReadDB returns the read-only connection pool.
func (d *DB) ReadDB() *sql.DB { return d.ro }

// WriteDB returns the read-write connection pool.
func (d *DB) WriteDB() *sql.DB { return d.rw }

// Tx runs fn inside a transaction, rolling back on error.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.rw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
