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
	firstLegacySchemaVersion    = 1
	latestLegacySchemaVersion   = 3
	migrationTableName          = "schema_migrations"
	recreateDatabaseInstruction = "delete the database file and let middleman recreate it"
)

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

	if version == migratedb.NilVersion {
		legacyVersion, hasLegacyVersion, err := readLegacySchemaVersion(rw)
		if err != nil {
			return wrapMigrationError(fmt.Errorf("read legacy schema version: %w", err))
		}

		switch {
		case hasLegacyVersion:
			if !hasMiddlemanTables(rw) {
				return wrapMigrationError(
					fmt.Errorf("legacy database schema version metadata exists without middleman tables"),
				)
			}
			if legacyVersion > latestLegacySchemaVersion {
				return fmt.Errorf(
					"middleman schema version %d is newer than this binary "+
						"(expects %d); upgrade middleman",
					legacyVersion, latestLegacySchemaVersion,
				)
			}
			if legacyVersion < firstLegacySchemaVersion {
				return wrapMigrationError(
					fmt.Errorf("legacy database schema version %d is invalid", legacyVersion),
				)
			}
			if err := databaseDriver.SetVersion(legacyVersion, false); err != nil {
				return wrapMigrationError(fmt.Errorf("seed legacy migration version: %w", err))
			}

		case hasMiddlemanTables(rw):
			return wrapMigrationError(
				fmt.Errorf("legacy database is missing schema version metadata"),
			)
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

func hasTable(db *sql.DB, name string) bool {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&count)
	return err == nil && count > 0
}

func readLegacySchemaVersion(db *sql.DB) (int, bool, error) {
	var version int
	err := db.QueryRow(
		`SELECT version FROM middleman_schema_version LIMIT 1`,
	).Scan(&version)
	if err == nil {
		return version, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if hasTable(db, "middleman_schema_version") {
		return 0, false, err
	}
	return 0, false, nil
}

func wrapMigrationError(err error) error {
	return fmt.Errorf("database migration failed; %s: %w", recreateDatabaseInstruction, err)
}
