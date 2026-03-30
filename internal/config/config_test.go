package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeConfig(t, `
sync_interval = "10m"
github_token_env = "MY_TOKEN"
host = "127.0.0.1"
port = 9000

[[repos]]
owner = "apache"
name = "arrow"

[[repos]]
owner = "ibis-project"
name = "ibis"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].FullName() != "apache/arrow" {
		t.Fatalf("expected apache/arrow, got %s", cfg.Repos[0].FullName())
	}
	if cfg.SyncInterval != "10m" {
		t.Fatalf("expected 10m, got %s", cfg.SyncInterval)
	}
	if cfg.Port != 9000 {
		t.Fatalf("expected port 9000, got %d", cfg.Port)
	}
}

func TestLoadDefaults(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "test"
name = "repo"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncInterval != "5m" {
		t.Fatalf("expected default 5m, got %s", cfg.SyncInterval)
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("expected default 127.0.0.1, got %s", cfg.Host)
	}
	if cfg.Port != 8090 {
		t.Fatalf("expected default 8090, got %d", cfg.Port)
	}
}

func TestLoadNoRepos(t *testing.T) {
	path := writeConfig(t, `host = "127.0.0.1"`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for no repos")
	}
}

func TestLoadInvalidSyncInterval(t *testing.T) {
	path := writeConfig(t, `
sync_interval = "not-a-duration"
[[repos]]
owner = "a"
name = "b"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for bad sync_interval")
	}
}

func TestLoadRejectsNonLoopback(t *testing.T) {
	path := writeConfig(t, `
host = "0.0.0.0"
[[repos]]
owner = "a"
name = "b"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for non-loopback host")
	}
}

func TestLoadRepoMissingFields(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for repo missing name")
	}
}

func TestGitHubToken(t *testing.T) {
	t.Setenv("TEST_GH_TOKEN", "secret123")
	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	if got := cfg.GitHubToken(); got != "secret123" {
		t.Fatalf("expected secret123, got %s", got)
	}
}

func TestDBPath(t *testing.T) {
	cfg := &Config{DataDir: "/tmp/middleman-test"}
	expected := "/tmp/middleman-test/middleman.db"
	if got := cfg.DBPath(); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
