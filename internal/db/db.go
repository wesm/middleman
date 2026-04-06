package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

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

// migrate drops legacy table names (no backward compat needed pre-release).
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
