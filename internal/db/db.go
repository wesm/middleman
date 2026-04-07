package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

// SchemaVersion is the current schema version. Bump this whenever
// schema.sql changes. The database stores its version in SQLite's
// PRAGMA user_version. On open:
//   - Fresh DB (version 0): schema is applied, version is set.
//   - Matching version: proceed normally.
//   - Stale DB (version < SchemaVersion): refuse to open.
//     The user must delete the database and let middleman recreate it.
//
// When real migrations are implemented, the stale case will run
// forward migrations instead of refusing.
const SchemaVersion = 1

//go:embed schema.sql
var schemaSQL string

// DB holds separate read-write and read-only connections to the SQLite database.
type DB struct {
	rw *sql.DB
	ro *sql.DB
}

// Open opens (or creates) a SQLite database at path, applies the schema, and
// enables WAL mode. Returns an error if the database was created by a newer
// or older version of middleman.
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

	version := d.readSchemaVersion()

	switch {
	case version == 0:
		// Fresh database — apply schema and stamp version.
		if _, err := d.rw.Exec(schemaSQL); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		d.writeSchemaVersion(SchemaVersion)

	case version == SchemaVersion:
		// Schema matches — nothing to do.

	case version > SchemaVersion:
		return fmt.Errorf(
			"middleman schema version %d is newer than this "+
				"binary (expects %d); upgrade middleman",
			version, SchemaVersion,
		)

	default:
		return fmt.Errorf(
			"middleman schema version %d is older than this "+
				"binary (expects %d); delete the database file "+
				"and let middleman recreate it",
			version, SchemaVersion,
		)
	}

	return nil
}

// readSchemaVersion returns the middleman schema version stored in the
// database, or 0 if the version table does not exist yet.
func (d *DB) readSchemaVersion() int {
	var version int
	err := d.rw.QueryRow(
		`SELECT version FROM middleman_schema_version LIMIT 1`,
	).Scan(&version)
	if err != nil {
		return 0
	}
	return version
}

// writeSchemaVersion upserts the schema version row.
func (d *DB) writeSchemaVersion(version int) {
	_, _ = d.rw.Exec(
		`CREATE TABLE IF NOT EXISTS middleman_schema_version (
			version INTEGER NOT NULL
		)`,
	)
	_, _ = d.rw.Exec(`DELETE FROM middleman_schema_version`)
	_, _ = d.rw.Exec(
		`INSERT INTO middleman_schema_version (version) VALUES (?)`,
		version,
	)
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
