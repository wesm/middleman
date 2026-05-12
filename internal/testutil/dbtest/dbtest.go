package dbtest

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

var templateCache = &templateState{}

type templateState struct {
	once       sync.Once
	path       string
	initErr    error
	buildCount atomic.Int32
}

// Open returns an isolated, migrated SQLite test database.
//
// The first call builds a migrated template database. Later calls copy that
// template into t.TempDir(), preserving test isolation without rerunning every
// migration for every test fixture.
func Open(t testing.TB) *db.DB {
	t.Helper()

	templatePath := templatePath(t)
	path := filepath.Join(t.TempDir(), "test.db")
	copyFile(t, templatePath, path)

	database, err := db.OpenPreparedForTest(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}

func templatePath(t testing.TB) string {
	t.Helper()

	templateCache.once.Do(func() {
		templateCache.buildCount.Add(1)
		dir, err := os.MkdirTemp("", "middleman-test-db-template-*")
		if err != nil {
			templateCache.initErr = err
			return
		}
		path := filepath.Join(dir, "template.db")
		database, err := db.Open(path)
		if err != nil {
			templateCache.initErr = err
			return
		}
		if _, err := database.WriteDB().Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			_ = database.Close()
			templateCache.initErr = err
			return
		}
		if err := database.Close(); err != nil {
			templateCache.initErr = err
			return
		}
		templateCache.path = path
	})
	require.NoError(t, templateCache.initErr)
	return templateCache.path
}

func copyFile(t testing.TB, src, dst string) {
	t.Helper()

	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o600))
}
