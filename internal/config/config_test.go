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

func TestGitHubTokenFallsBackToGHCli(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\necho gh-secret\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("TEST_GH_TOKEN", "")

	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	if got := cfg.GitHubToken(); got != "gh-secret" {
		t.Fatalf("expected gh-secret, got %q", got)
	}
}

func TestGitHubTokenPrefersEnvVarOverGHCli(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\necho gh-secret\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("TEST_GH_TOKEN", "secret123")

	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	if got := cfg.GitHubToken(); got != "secret123" {
		t.Fatalf("expected secret123, got %q", got)
	}
}

func TestBasePathValidation(t *testing.T) {
	base := `
[[repos]]
owner = "a"
name = "b"
`
	tests := []struct {
		name    string
		value   string
		wantErr bool
		wantBP  string
	}{
		{"default", "", false, "/"},
		{"root", "/", false, "/"},
		{"simple", "middleman", false, "/middleman/"},
		{"with slashes", "/middleman/", false, "/middleman/"},
		{"nested", "/apps/middleman", false, "/apps/middleman/"},
		{"dot segment", "/../evil", true, ""},
		{"single dot", "/./path", true, ""},
		{"special chars", "/mid<script>", true, ""},
		{"quotes", `/mid"man`, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra := ""
			if tt.value != "" {
				extra = `base_path = "` + tt.value + `"`
			}
			path := writeConfig(t, extra+"\n"+base)
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.BasePath != tt.wantBP {
				t.Fatalf("expected BasePath %q, got %q", tt.wantBP, cfg.BasePath)
			}
		})
	}
}

func TestGitHubTokenReturnsEmptyWhenGHCliUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TEST_GH_TOKEN", "")

	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	if got := cfg.GitHubToken(); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestDBPath(t *testing.T) {
	cfg := &Config{DataDir: "/tmp/middleman-test"}
	expected := "/tmp/middleman-test/middleman.db"
	if got := cfg.DBPath(); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestLoadActivityDefaults(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Activity.ViewMode != "flat" {
		t.Fatalf("expected view_mode flat, got %q", cfg.Activity.ViewMode)
	}
	if cfg.Activity.TimeRange != "7d" {
		t.Fatalf("expected time_range 7d, got %q", cfg.Activity.TimeRange)
	}
	if cfg.Activity.HideClosed {
		t.Fatal("expected hide_closed false")
	}
	if cfg.Activity.HideBots {
		t.Fatal("expected hide_bots false")
	}
}

func TestLoadActivityExplicit(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"

[activity]
view_mode = "threaded"
time_range = "30d"
hide_closed = true
hide_bots = true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Activity.ViewMode != "threaded" {
		t.Fatalf("expected view_mode threaded, got %q", cfg.Activity.ViewMode)
	}
	if cfg.Activity.TimeRange != "30d" {
		t.Fatalf("expected time_range 30d, got %q", cfg.Activity.TimeRange)
	}
	if !cfg.Activity.HideClosed {
		t.Fatal("expected hide_closed true")
	}
	if !cfg.Activity.HideBots {
		t.Fatal("expected hide_bots true")
	}
}

func TestLoadActivityInvalidViewMode(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"

[activity]
view_mode = "kanban"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid view_mode")
	}
}

func TestLoadActivityInvalidTimeRange(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"

[activity]
time_range = "1y"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid time_range")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := writeConfig(t, `
sync_interval = "10m"
github_token_env = "MY_TOKEN"
host = "127.0.0.1"
port = 9000
base_path = "/app/"

[[repos]]
owner = "apache"
name = "arrow"

[activity]
view_mode = "threaded"
time_range = "30d"
hide_closed = true
hide_bots = true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	if err := cfg.Save(savePath); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load(savePath)
	if err != nil {
		t.Fatalf("reloading saved config: %v", err)
	}

	if cfg2.SyncInterval != cfg.SyncInterval {
		t.Fatalf("sync_interval: got %q, want %q",
			cfg2.SyncInterval, cfg.SyncInterval)
	}
	if cfg2.Host != cfg.Host {
		t.Fatalf("host: got %q, want %q", cfg2.Host, cfg.Host)
	}
	if cfg2.Port != cfg.Port {
		t.Fatalf("port: got %d, want %d", cfg2.Port, cfg.Port)
	}
	if cfg2.BasePath != cfg.BasePath {
		t.Fatalf("base_path: got %q, want %q",
			cfg2.BasePath, cfg.BasePath)
	}
	if len(cfg2.Repos) != len(cfg.Repos) {
		t.Fatalf("repos count: got %d, want %d",
			len(cfg2.Repos), len(cfg.Repos))
	}
	if cfg2.Repos[0].FullName() != cfg.Repos[0].FullName() {
		t.Fatalf("repos[0]: got %q, want %q",
			cfg2.Repos[0].FullName(), cfg.Repos[0].FullName())
	}
	if cfg2.Activity.ViewMode != cfg.Activity.ViewMode {
		t.Fatalf("activity.view_mode: got %q, want %q",
			cfg2.Activity.ViewMode, cfg.Activity.ViewMode)
	}
	if cfg2.Activity.TimeRange != cfg.Activity.TimeRange {
		t.Fatalf("activity.time_range: got %q, want %q",
			cfg2.Activity.TimeRange, cfg.Activity.TimeRange)
	}
	if cfg2.Activity.HideClosed != cfg.Activity.HideClosed {
		t.Fatalf("activity.hide_closed: got %v, want %v",
			cfg2.Activity.HideClosed, cfg.Activity.HideClosed)
	}
	if cfg2.Activity.HideBots != cfg.Activity.HideBots {
		t.Fatalf("activity.hide_bots: got %v, want %v",
			cfg2.Activity.HideBots, cfg.Activity.HideBots)
	}
}

func TestSavePreservesDefaults(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	if err := cfg.Save(savePath); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load(savePath)
	if err != nil {
		t.Fatalf("reloading saved config: %v", err)
	}

	if cfg2.SyncInterval != "5m" {
		t.Fatalf("expected default sync_interval 5m, got %q",
			cfg2.SyncInterval)
	}
	if cfg2.Host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %q", cfg2.Host)
	}
	if cfg2.Port != 8090 {
		t.Fatalf("expected default port 8090, got %d", cfg2.Port)
	}
	if cfg2.Activity.ViewMode != "flat" {
		t.Fatalf("expected default view_mode flat, got %q",
			cfg2.Activity.ViewMode)
	}
	if cfg2.Activity.TimeRange != "7d" {
		t.Fatalf("expected default time_range 7d, got %q",
			cfg2.Activity.TimeRange)
	}
}
