package config

import (
	"os"
	"path/filepath"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestLoadValid(t *testing.T) {
	assert := Assert.New(t)
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
	require.NoError(t, err)
	require.Len(t, cfg.Repos, 2)
	assert.Equal("apache/arrow", cfg.Repos[0].FullName())
	assert.Equal("10m", cfg.SyncInterval)
	assert.Equal(9000, cfg.Port)
}

func TestLoadDefaults(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "test"
name = "repo"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal("5m", cfg.SyncInterval)
	assert.Equal("127.0.0.1", cfg.Host)
	assert.Equal(8090, cfg.Port)
}

func TestLoadNoRepos(t *testing.T) {
	path := writeConfig(t, `host = "127.0.0.1"`)
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadInvalidSyncInterval(t *testing.T) {
	path := writeConfig(t, `
sync_interval = "not-a-duration"
[[repos]]
owner = "a"
name = "b"
`)
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadRejectsNonLoopback(t *testing.T) {
	path := writeConfig(t, `
host = "0.0.0.0"
[[repos]]
owner = "a"
name = "b"
`)
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadRepoMissingFields(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
`)
	_, err := Load(path)
	require.Error(t, err)
}

func TestGitHubToken(t *testing.T) {
	t.Setenv("TEST_GH_TOKEN", "secret123")
	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	Assert.Equal(t, "secret123", cfg.GitHubToken())
}

func TestGitHubTokenFallsBackToGHCli(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	require.NoError(t, os.WriteFile(ghPath, []byte("#!/bin/sh\necho gh-secret\n"), 0o755))

	t.Setenv("PATH", dir)
	t.Setenv("TEST_GH_TOKEN", "")

	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	Assert.Equal(t, "gh-secret", cfg.GitHubToken())
}

func TestGitHubTokenPrefersEnvVarOverGHCli(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	require.NoError(t, os.WriteFile(ghPath, []byte("#!/bin/sh\necho gh-secret\n"), 0o755))

	t.Setenv("PATH", dir)
	t.Setenv("TEST_GH_TOKEN", "secret123")

	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	Assert.Equal(t, "secret123", cfg.GitHubToken())
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
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			Assert.Equal(t, tt.wantBP, cfg.BasePath)
		})
	}
}

func TestGitHubTokenReturnsEmptyWhenGHCliUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TEST_GH_TOKEN", "")

	cfg := &Config{GitHubTokenEnv: "TEST_GH_TOKEN"}
	Assert.Empty(t, cfg.GitHubToken())
}

func TestDBPath(t *testing.T) {
	cfg := &Config{DataDir: "/tmp/middleman-test"}
	expected := "/tmp/middleman-test/middleman.db"
	Assert.Equal(t, expected, cfg.DBPath())
}

func TestLoadActivityDefaults(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal("flat", cfg.Activity.ViewMode)
	assert.Equal("7d", cfg.Activity.TimeRange)
	assert.False(cfg.Activity.HideClosed)
	assert.False(cfg.Activity.HideBots)
}

func TestLoadActivityExplicit(t *testing.T) {
	assert := Assert.New(t)
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
	require.NoError(t, err)
	assert.Equal("threaded", cfg.Activity.ViewMode)
	assert.Equal("30d", cfg.Activity.TimeRange)
	assert.True(cfg.Activity.HideClosed)
	assert.True(cfg.Activity.HideBots)
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
	require.Error(t, err)
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
	require.Error(t, err)
}

func TestSaveRoundTrip(t *testing.T) {
	assert := Assert.New(t)
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
	require.NoError(t, err)

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	require.NoError(t, cfg.Save(savePath))

	cfg2, err := Load(savePath)
	require.NoError(t, err)
	assert.Equal("MY_TOKEN", cfg2.GitHubTokenEnv)
	assert.Equal(cfg.SyncInterval, cfg2.SyncInterval)
	assert.Equal(cfg.Host, cfg2.Host)
	assert.Equal(cfg.Port, cfg2.Port)
	assert.Equal(cfg.BasePath, cfg2.BasePath)
	assert.Len(cfg2.Repos, len(cfg.Repos))
	assert.Equal(cfg.Repos[0].FullName(), cfg2.Repos[0].FullName())
	assert.Equal(cfg.Activity.ViewMode, cfg2.Activity.ViewMode)
	assert.Equal(cfg.Activity.TimeRange, cfg2.Activity.TimeRange)
	assert.Equal(cfg.Activity.HideClosed, cfg2.Activity.HideClosed)
	assert.Equal(cfg.Activity.HideBots, cfg2.Activity.HideBots)
}

func TestSavePreservesDefaults(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"
`)
	cfg, err := Load(path)
	require.NoError(t, err)

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	require.NoError(t, cfg.Save(savePath))

	cfg2, err := Load(savePath)
	require.NoError(t, err)
	assert.Equal("5m", cfg2.SyncInterval)
	assert.Equal("127.0.0.1", cfg2.Host)
	assert.Equal(8090, cfg2.Port)
	assert.Equal("flat", cfg2.Activity.ViewMode)
	assert.Equal("7d", cfg2.Activity.TimeRange)
}

func TestSaveRoundTripEmptyGitHubTokenEnv(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
github_token_env = ""

[[repos]]
owner = "a"
name = "b"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Empty(cfg.GitHubTokenEnv)

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	require.NoError(t, cfg.Save(savePath))

	cfg2, err := Load(savePath)
	require.NoError(t, err)
	assert.Empty(cfg2.GitHubTokenEnv)
}
