package db

import (
	"os"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenAndSchema(t *testing.T) {
	d := openTestDB(t)
	tables := []string{"repos", "pull_requests", "pr_events", "kanban_state"}
	for _, tbl := range tables {
		var name string
		err := d.ReadDB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %s not found: %v", tbl, err)
		}
	}
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file not created: %v", err)
	}
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()
	d2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
}
