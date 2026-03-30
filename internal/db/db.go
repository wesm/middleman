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

// migrate applies idempotent column additions for existing databases.
func (d *DB) migrate() {
	migrations := []string{
		"ALTER TABLE pull_requests ADD COLUMN ci_checks_json TEXT NOT NULL DEFAULT ''",
	}
	for _, m := range migrations {
		d.rw.Exec(m) // Ignore errors — column may already exist
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
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
