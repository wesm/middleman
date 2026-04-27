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
	firstLegacySchemaVersion              = 1
	latestLegacySchemaVersion             = 3
	migrationTableName                    = "schema_migrations"
	recreateDatabaseInstruction           = "delete the database file and let middleman recreate it"
	timestampRepairGateVersion            = 10
	workspaceSetupMigrationVersion        = 11
	workspaceAssociatedPRMigrationVersion = 13
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func runMigrations(rw *sql.DB) (int, error) {
	sourceDriver, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return migratedb.NilVersion, fmt.Errorf("load embedded migrations: %w", err)
	}

	databaseDriver, err := migratesqlite.WithInstance(rw, &migratesqlite.Config{
		MigrationsTable: migrationTableName,
	})
	if err != nil {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("open migration driver: %w", err))
	}

	version, dirty, err := databaseDriver.Version()
	if err != nil {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("read migration version: %w", err))
	}
	startVersion := version

	latest, err := latestMigrationVersion()
	if err != nil {
		return migratedb.NilVersion, fmt.Errorf("read embedded migration versions: %w", err)
	}

	if version > latest {
		return migratedb.NilVersion, fmt.Errorf(
			"middleman schema version %d is newer than this binary "+
				"(expects %d); upgrade middleman",
			version, latest,
		)
	}

	if dirty {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("database is in a dirty migration state"))
	}

	if version == migratedb.NilVersion {
		legacyVersion, hasLegacyVersion, err := readLegacySchemaVersion(rw)
		if err != nil {
			return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("read legacy schema version: %w", err))
		}

		switch {
		case hasLegacyVersion:
			if !hasMiddlemanTables(rw) {
				return migratedb.NilVersion, wrapMigrationError(
					fmt.Errorf("legacy database schema version metadata exists without middleman tables"),
				)
			}
			if legacyVersion > latestLegacySchemaVersion {
				return migratedb.NilVersion, fmt.Errorf(
					"middleman schema version %d is newer than this binary "+
						"(expects %d); upgrade middleman",
					legacyVersion, latestLegacySchemaVersion,
				)
			}
			if legacyVersion < firstLegacySchemaVersion {
				return migratedb.NilVersion, wrapMigrationError(
					fmt.Errorf("legacy database schema version %d is invalid", legacyVersion),
				)
			}
			if err := databaseDriver.SetVersion(legacyVersion, false); err != nil {
				return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("seed legacy migration version: %w", err))
			}
			startVersion = legacyVersion

		case hasMiddlemanTables(rw):
			return migratedb.NilVersion, wrapMigrationError(
				fmt.Errorf("legacy database is missing schema version metadata"),
			)
		}
	}

	if version < workspaceAssociatedPRMigrationVersion {
		repaired, err := reconcileWorkspaceAssociatedPRMigrationVersion12(
			rw, databaseDriver,
		)
		if err != nil {
			return migratedb.NilVersion, wrapMigrationError(
				fmt.Errorf("repair workspace associated PR migration state: %w", err),
			)
		}
		if repaired {
			version, dirty, err = databaseDriver.Version()
			if err != nil {
				return migratedb.NilVersion, wrapMigrationError(
					fmt.Errorf("read migration version after workspace associated PR repair: %w", err),
				)
			}
			if dirty {
				return migratedb.NilVersion, wrapMigrationError(
					fmt.Errorf("database is in a dirty migration state"),
				)
			}
		}
	}

	if version == timestampRepairGateVersion {
		_, err := reconcileWorkspaceSetupMigrationVersion10(
			rw, databaseDriver,
		)
		if err != nil {
			return migratedb.NilVersion, wrapMigrationError(
				fmt.Errorf("repair workspace migration state: %w", err),
			)
		}
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", databaseDriver)
	if err != nil {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("create migrator: %w", err))
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("apply migrations: %w", err))
	}

	version, dirty, err = databaseDriver.Version()
	if err != nil {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("read migration version after update: %w", err))
	}
	if dirty {
		return migratedb.NilVersion, wrapMigrationError(fmt.Errorf("database is in a dirty migration state"))
	}
	if version != latest {
		return migratedb.NilVersion, wrapMigrationError(
			fmt.Errorf("database ended at migration version %d, expected %d", version, latest),
		)
	}

	return startVersion, nil
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

func hasIndex(db *sql.DB, name string) bool {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`,
		name,
	).Scan(&count)
	return err == nil && count > 0
}

func hasColumn(
	db *sql.DB, tableName, columnName string,
) (bool, error) {
	rows, err := db.Query(
		fmt.Sprintf(`PRAGMA table_info(%s)`, tableName),
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(
			&cid, &name, &columnType, &notNull, &defaultVal, &pk,
		); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}

	return false, rows.Err()
}

func reconcileWorkspaceSetupMigrationVersion10(
	rw *sql.DB, driver migratedb.Driver,
) (bool, error) {
	hasEventsTable := hasTable(rw, "middleman_workspace_setup_events")
	hasEventsIndex := hasIndex(
		rw, "middleman_workspace_setup_events_workspace_id_idx",
	)
	hasWorkspaceBranch, err := hasColumn(
		rw, "middleman_workspaces", "workspace_branch",
	)
	if err != nil {
		return false, err
	}
	if !hasEventsTable && !hasEventsIndex && !hasWorkspaceBranch {
		return false, nil
	}
	if err := ensureWorkspaceSetupMigrationArtifacts(
		rw, hasEventsTable, hasEventsIndex, hasWorkspaceBranch,
	); err != nil {
		return false, err
	}

	if err := driver.SetVersion(
		workspaceSetupMigrationVersion, false,
	); err != nil {
		return false, err
	}

	return true, nil
}

func reconcileWorkspaceAssociatedPRMigrationVersion12(
	rw *sql.DB, driver migratedb.Driver,
) (bool, error) {
	hasAssociatedPR, err := hasColumn(
		rw, "middleman_workspaces", "associated_pr_number",
	)
	if err != nil {
		return false, err
	}
	if !hasAssociatedPR {
		return false, nil
	}

	hasEventsTable := hasTable(rw, "middleman_workspace_setup_events")
	hasEventsIndex := hasIndex(
		rw, "middleman_workspace_setup_events_workspace_id_idx",
	)
	hasWorkspaceBranch, err := hasColumn(
		rw, "middleman_workspaces", "workspace_branch",
	)
	if err != nil {
		return false, err
	}
	if err := ensureWorkspaceSetupMigrationArtifacts(
		rw, hasEventsTable, hasEventsIndex, hasWorkspaceBranch,
	); err != nil {
		return false, err
	}

	hasItemType, err := hasColumn(
		rw, "middleman_workspaces", "item_type",
	)
	if err != nil {
		return false, err
	}
	if !hasItemType {
		hasMRNumber, err := hasColumn(
			rw, "middleman_workspaces", "mr_number",
		)
		if err != nil {
			return false, err
		}
		hasMRHeadRef, err := hasColumn(
			rw, "middleman_workspaces", "mr_head_ref",
		)
		if err != nil {
			return false, err
		}
		if hasMRNumber {
			if _, err := rw.Exec(`
				ALTER TABLE middleman_workspaces
				    RENAME COLUMN mr_number TO item_number
			`); err != nil {
				return false, err
			}
		}
		if hasMRHeadRef {
			if _, err := rw.Exec(`
				ALTER TABLE middleman_workspaces
				    RENAME COLUMN mr_head_ref TO git_head_ref
			`); err != nil {
				return false, err
			}
		}
		if _, err := rw.Exec(`
			ALTER TABLE middleman_workspaces
			    ADD COLUMN item_type TEXT NOT NULL DEFAULT 'pull_request'
		`); err != nil {
			return false, err
		}
		if err := rebuildWorkspacesWithItemTypeUniqueness(rw); err != nil {
			return false, err
		}
	}

	if _, err := rw.Exec(`
		UPDATE middleman_workspaces
		SET associated_pr_number = item_number
		WHERE item_type = 'pull_request' AND associated_pr_number IS NULL
	`); err != nil {
		return false, err
	}

	if err := driver.SetVersion(
		workspaceAssociatedPRMigrationVersion, false,
	); err != nil {
		return false, err
	}

	return true, nil
}

func rebuildWorkspacesWithItemTypeUniqueness(rw *sql.DB) error {
	_, err := rw.Exec(`
		DROP TRIGGER IF EXISTS middleman_workspaces_casefold_update;
		DROP TRIGGER IF EXISTS middleman_workspaces_casefold_insert;

		DROP TABLE IF EXISTS temp.middleman_workspace_setup_events_backup;
		CREATE TEMP TABLE middleman_workspace_setup_events_backup AS
		SELECT id, workspace_id, stage, outcome, message, created_at
		FROM middleman_workspace_setup_events;

		DROP INDEX IF EXISTS middleman_workspace_setup_events_workspace_id_idx;
		DROP TABLE IF EXISTS middleman_workspace_setup_events;

		ALTER TABLE middleman_workspaces
		    RENAME TO middleman_workspaces_repair;

		CREATE TABLE middleman_workspaces (
		    id                   TEXT PRIMARY KEY,
		    platform_host        TEXT NOT NULL,
		    repo_owner           TEXT NOT NULL,
		    repo_name            TEXT NOT NULL,
		    item_type            TEXT NOT NULL DEFAULT 'pull_request',
		    item_number          INTEGER NOT NULL,
		    associated_pr_number INTEGER,
		    git_head_ref         TEXT NOT NULL,
		    mr_head_repo         TEXT,
		    worktree_path        TEXT NOT NULL,
		    tmux_session         TEXT NOT NULL,
		    status               TEXT NOT NULL DEFAULT 'creating',
		    error_message        TEXT,
		    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
		    workspace_branch     TEXT NOT NULL DEFAULT '__middleman_unknown__',
		    UNIQUE(platform_host, repo_owner, repo_name, item_type, item_number)
		);

		INSERT INTO middleman_workspaces (
		    id, platform_host, repo_owner, repo_name,
		    item_type, item_number, associated_pr_number,
		    git_head_ref, mr_head_repo,
		    worktree_path, tmux_session, status,
		    error_message, created_at, workspace_branch
		)
		SELECT
		    id, platform_host, repo_owner, repo_name,
		    item_type, item_number, associated_pr_number,
		    git_head_ref, mr_head_repo,
		    worktree_path, tmux_session, status,
		    error_message, created_at, workspace_branch
		FROM middleman_workspaces_repair;

		DROP TABLE middleman_workspaces_repair;

		CREATE TRIGGER middleman_workspaces_casefold_insert
		BEFORE INSERT ON middleman_workspaces
		WHEN NEW.platform_host <> lower(NEW.platform_host)
		  OR NEW.repo_owner <> lower(NEW.repo_owner)
		  OR NEW.repo_name <> lower(NEW.repo_name)
		BEGIN
		    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
		END;

		CREATE TRIGGER middleman_workspaces_casefold_update
		BEFORE UPDATE OF platform_host, repo_owner, repo_name ON middleman_workspaces
		WHEN NEW.platform_host <> lower(NEW.platform_host)
		  OR NEW.repo_owner <> lower(NEW.repo_owner)
		  OR NEW.repo_name <> lower(NEW.repo_name)
		BEGIN
		    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
		END;

		CREATE TABLE middleman_workspace_setup_events (
		    id           INTEGER PRIMARY KEY AUTOINCREMENT,
		    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
		    stage        TEXT NOT NULL,
		    outcome      TEXT NOT NULL,
		    message      TEXT NOT NULL,
		    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		INSERT INTO middleman_workspace_setup_events (
		    id, workspace_id, stage, outcome, message, created_at
		)
		SELECT
		    id, workspace_id, stage, outcome, message, created_at
		FROM middleman_workspace_setup_events_backup;

		DROP TABLE middleman_workspace_setup_events_backup;

		CREATE INDEX middleman_workspace_setup_events_workspace_id_idx
		    ON middleman_workspace_setup_events (workspace_id, id);
	`)
	return err
}

func ensureWorkspaceSetupMigrationArtifacts(
	rw *sql.DB,
	hasEventsTable, hasEventsIndex, hasWorkspaceBranch bool,
) error {
	if !hasEventsTable {
		if _, err := rw.Exec(`
			CREATE TABLE IF NOT EXISTS middleman_workspace_setup_events (
			    id          INTEGER PRIMARY KEY AUTOINCREMENT,
			    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
			    stage       TEXT NOT NULL,
			    outcome     TEXT NOT NULL,
			    message     TEXT NOT NULL,
			    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
			)
		`); err != nil {
			return err
		}
	}

	if !hasEventsIndex {
		if _, err := rw.Exec(`
			CREATE INDEX IF NOT EXISTS middleman_workspace_setup_events_workspace_id_idx
			    ON middleman_workspace_setup_events (workspace_id, id)
		`); err != nil {
			return err
		}
	}

	if !hasWorkspaceBranch {
		if _, err := rw.Exec(`
			ALTER TABLE middleman_workspaces
			    ADD COLUMN workspace_branch TEXT NOT NULL DEFAULT '__middleman_unknown__'
		`); err != nil {
			return err
		}
	}
	return nil
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
