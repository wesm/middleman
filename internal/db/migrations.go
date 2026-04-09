package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

const (
	legacyBaselineVersion       = 1
	migrationTableName          = "schema_migrations"
	recreateDatabaseInstruction = "delete the database file and let middleman recreate it"
)

var legacyBaselineTables = []string{
	"middleman_repos",
	"middleman_merge_requests",
	"middleman_mr_events",
	"middleman_kanban_state",
	"middleman_issues",
	"middleman_issue_events",
	"middleman_starred_items",
	"middleman_mr_worktree_links",
	"middleman_rate_limits",
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

func runMigrations(rw *sql.DB) error {
	sourceDriver, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	databaseDriver, err := migratesqlite.WithInstance(rw, &migratesqlite.Config{
		MigrationsTable: migrationTableName,
	})
	if err != nil {
		return wrapMigrationError(fmt.Errorf("open migration driver: %w", err))
	}

	version, dirty, err := databaseDriver.Version()
	if err != nil {
		return wrapMigrationError(fmt.Errorf("read migration version: %w", err))
	}

	latest, err := latestMigrationVersion()
	if err != nil {
		return fmt.Errorf("read embedded migration versions: %w", err)
	}

	if version > latest {
		return fmt.Errorf(
			"middleman schema version %d is newer than this binary "+
				"(expects %d); upgrade middleman",
			version, latest,
		)
	}

	if dirty {
		return wrapMigrationError(fmt.Errorf("database is in a dirty migration state"))
	}

	if version == migratedb.NilVersion && hasMiddlemanTables(rw) {
		if !hasLegacyBaselineSchema(rw) {
			return wrapMigrationError(
				fmt.Errorf("legacy database schema does not match the expected baseline"),
			)
		}
		if err := databaseDriver.SetVersion(legacyBaselineVersion, false); err != nil {
			return wrapMigrationError(fmt.Errorf("seed legacy migration version: %w", err))
		}
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", databaseDriver)
	if err != nil {
		return wrapMigrationError(fmt.Errorf("create migrator: %w", err))
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return wrapMigrationError(fmt.Errorf("apply migrations: %w", err))
	}

	version, dirty, err = databaseDriver.Version()
	if err != nil {
		return wrapMigrationError(fmt.Errorf("read migration version after update: %w", err))
	}
	if dirty {
		return wrapMigrationError(fmt.Errorf("database is in a dirty migration state"))
	}
	if version != latest {
		return wrapMigrationError(
			fmt.Errorf("database ended at migration version %d, expected %d", version, latest),
		)
	}

	return nil
}

func latestMigrationVersion() (int, error) {
	files, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		return 0, err
	}

	latest := migratedb.NilVersion
	for _, file := range files {
		name := path.Base(file)
		prefix := strings.TrimSuffix(name, ".up.sql")
		versionText, _, found := strings.Cut(prefix, "_")
		if !found {
			return 0, fmt.Errorf("parse migration version from %q", name)
		}

		version, err := strconv.Atoi(versionText)
		if err != nil {
			return 0, fmt.Errorf("parse migration version from %q: %w", name, err)
		}
		if version > latest {
			latest = version
		}
	}

	if latest == migratedb.NilVersion {
		return 0, fmt.Errorf("no embedded migrations found")
	}

	return latest, nil
}

func hasMiddlemanTables(db *sql.DB) bool {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master
		 WHERE type = 'table'
		   AND name GLOB 'middleman_*'`,
	).Scan(&count)
	return err == nil && count > 0
}

func hasLegacyBaselineSchema(db *sql.DB) bool {
	for _, table := range legacyBaselineTables {
		if !hasTable(db, table) {
			return false
		}
	}
	return true
}

func hasTable(db *sql.DB, name string) bool {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&count)
	return err == nil && count > 0
}

func wrapMigrationError(err error) error {
	return fmt.Errorf("database migration failed; %s: %w", recreateDatabaseInstruction, err)
}
