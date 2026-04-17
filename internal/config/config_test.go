package config

import (
	"fmt"
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

func TestLoadCasefoldsRepoOwnerAndName(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "Org"
name = "Foo"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Repos, 1)
	assert.Equal("org", cfg.Repos[0].Owner)
	assert.Equal("foo", cfg.Repos[0].Name)
}

func TestLoadRejectsDuplicateReposAfterCasefolding(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "Org"
name = "Foo"

[[repos]]
owner = "org"
name = "foo"
`)

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), `duplicate repo "org/foo"`)
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
	assert.Equal(8091, cfg.Port)
}

func TestLoadNoRepos(t *testing.T) {
	path := writeConfig(t, `host = "127.0.0.1"`)
	cfg, err := Load(path)
	require.NoError(t, err)
	Assert.Empty(t, cfg.Repos)
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

func TestLoadRepoNameDotGitOnly(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = ".git"
`)
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadRejectsGlobInOwner(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "acme-*"
name = "widgets"
`)

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "glob syntax in owner")
}

func TestLoadRejectsGlobInOwnerBeforeGitHubRefNormalization(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "acme-*"
name = "https://github.com/acme/widgets"
`)

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "glob syntax in owner")
}

func TestRepoHasNameGlob(t *testing.T) {
	assert := Assert.New(t)

	assert.False((&Repo{Owner: "acme", Name: "widgets"}).HasNameGlob())
	assert.True((&Repo{Owner: "acme", Name: "widgets-*"}).HasNameGlob())
	assert.True((&Repo{Owner: "acme", Name: "widgets-?"}).HasNameGlob())
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

func TestMiddlemanHomeOverridesDefaultPaths(t *testing.T) {
	assert := Assert.New(t)
	t.Setenv("MIDDLEMAN_HOME", "/tmp/middleman-test")

	assert.Equal(
		"/tmp/middleman-test/config.toml",
		DefaultConfigPath(),
	)
	assert.Equal("/tmp/middleman-test", DefaultDataDir())
}

func TestDefaultPathsWithoutMiddlemanHome(t *testing.T) {
	assert := Assert.New(t)
	t.Setenv("MIDDLEMAN_HOME", "")
	t.Setenv("HOME", "/fakehome")

	assert.Equal(
		"/fakehome/.config/middleman/config.toml",
		DefaultConfigPath(),
	)
	assert.Equal("/fakehome/.config/middleman", DefaultDataDir())
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
	assert.Equal("threaded", cfg.Activity.ViewMode)
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

func TestLoadNormalizesRepoNames(t *testing.T) {
	tests := []struct {
		name      string
		owner     string
		repoName  string
		wantOwner string
		wantName  string
	}{
		{
			name:      "strips .git suffix",
			owner:     "apache",
			repoName:  "arrow.git",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "HTTPS URL in name",
			owner:     "ignored",
			repoName:  "https://github.com/apache/arrow",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "HTTPS URL with .git in name",
			owner:     "ignored",
			repoName:  "https://github.com/apache/arrow.git",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "SSH URL in name",
			owner:     "ignored",
			repoName:  "git@github.com:apache/arrow.git",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "SSH URL without .git in name",
			owner:     "ignored",
			repoName:  "git@github.com:apache/arrow",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "SSH URI-style URL",
			owner:     "ignored",
			repoName:  "ssh://git@github.com/apache/arrow.git",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "SSH URI-style with port",
			owner:     "ignored",
			repoName:  "ssh://git@github.com:22/apache/arrow.git",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "SSH URI-style non-github host",
			owner:     "myorg",
			repoName:  "ssh://git@gitlab.com/apache/arrow.git",
			wantOwner: "myorg",
			wantName:  "ssh://git@gitlab.com/apache/arrow",
		},
		{
			name:      "bare github.com path in name",
			owner:     "ignored",
			repoName:  "github.com/apache/arrow",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "HTTPS URL in owner",
			owner:     "https://github.com/apache/arrow.git",
			repoName:  "ignored",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "plain owner and name unchanged",
			owner:     "apache",
			repoName:  "arrow",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "URL with query string",
			owner:     "ignored",
			repoName:  "https://github.com/apache/arrow?tab=readme",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "URL with fragment",
			owner:     "ignored",
			repoName:  "https://github.com/apache/arrow#readme",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "URL with trailing slash",
			owner:     "ignored",
			repoName:  "https://github.com/apache/arrow/",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "URL with .git and trailing slash",
			owner:     "ignored",
			repoName:  "https://github.com/apache/arrow.git/",
			wantOwner: "apache",
			wantName:  "arrow",
		},
		{
			name:      "repo literally named github.com",
			owner:     "acme",
			repoName:  "github.com",
			wantOwner: "acme",
			wantName:  "github.com",
		},
		{
			name:      "non-github HTTPS host not parsed",
			owner:     "ignored",
			repoName:  "https://notgithub.com/apache/arrow",
			wantOwner: "ignored",
			wantName:  "https://notgithub.com/apache/arrow",
		},
		{
			name:      "non-github SSH host not parsed",
			owner:     "ignored",
			repoName:  "git@notgithub.com:apache/arrow.git",
			wantOwner: "ignored",
			wantName:  "git@notgithub.com:apache/arrow",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := Assert.New(t)
			cfg := fmt.Sprintf(`
[[repos]]
owner = %q
name = %q
`, tt.owner, tt.repoName)
			path := writeConfig(t, cfg)
			got, err := Load(path)
			require.NoError(t, err)
			assert.Equal(tt.wantOwner, got.Repos[0].Owner)
			assert.Equal(tt.wantName, got.Repos[0].Name)
		})
	}
}

func TestLoadRejectsMalformedGitHubRef(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		repoName string
	}{
		{
			name:     "HTTPS URL missing repo",
			owner:    "ignored",
			repoName: "https://github.com/apache/",
		},
		{
			name:     "HTTPS URL owner only",
			owner:    "ignored",
			repoName: "https://github.com/apache",
		},
		{
			name:     "SSH URL missing repo",
			owner:    "ignored",
			repoName: "git@github.com:apache",
		},
		{
			name:     "bare HTTPS prefix",
			owner:    "ignored",
			repoName: "https://github.com/",
		},
		{
			name:     "bare github.com slash",
			owner:    "ignored",
			repoName: "github.com/",
		},
		{
			name:     "bare SSH prefix",
			owner:    "ignored",
			repoName: "git@github.com:",
		},
		{
			name:     "HTTPS host only no slash",
			owner:    "ignored",
			repoName: "https://github.com",
		},
		{
			name:     "SSH URI bare host",
			owner:    "ignored",
			repoName: "ssh://git@github.com",
		},
		{
			name:     "SSH URI bare host with port",
			owner:    "ignored",
			repoName: "ssh://git@github.com:22",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fmt.Sprintf(`
[[repos]]
owner = %q
name = %q
`, tt.owner, tt.repoName)
			path := writeConfig(t, cfg)
			_, err := Load(path)
			require.Error(t, err)
			Assert.Contains(t, err.Error(), "incomplete GitHub reference")
		})
	}
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
	assert.Equal(8091, cfg2.Port)
	assert.Equal("threaded", cfg2.Activity.ViewMode)
	assert.Equal("7d", cfg2.Activity.TimeRange)
}

func TestEnsureDefaultCreatesFile(t *testing.T) {
	assert := Assert.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")

	require.NoError(t, EnsureDefault(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(string(data), "sync_interval")
	assert.Contains(string(data), "github_token_env")
	assert.Contains(string(data), "[[repos]]")
}

func TestEnsureDefaultSkipsExisting(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"
`)
	require.NoError(t, EnsureDefault(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	Assert.Contains(t, string(data), `owner = "a"`)
}

func TestEnsureDefaultIdempotent(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	require.NoError(EnsureDefault(path))
	info1, err := os.Stat(path)
	require.NoError(err)

	require.NoError(EnsureDefault(path))
	info2, err := os.Stat(path)
	require.NoError(err)

	require.Equal(info1.ModTime(), info2.ModTime())
}

func TestLoadRepoPlatformHost(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "apache"
name = "arrow"
platform_host = "github.example.com"
token_env = "GHE_TOKEN"

[[repos]]
owner = "ibis-project"
name = "ibis"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Repos, 2)
	assert.Equal("github.example.com", cfg.Repos[0].PlatformHost)
	assert.Equal("GHE_TOKEN", cfg.Repos[0].TokenEnv)
	assert.Empty(cfg.Repos[1].PlatformHost)
	assert.Empty(cfg.Repos[1].TokenEnv)
}

func TestRepoPlatformHostOrDefault(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{"empty defaults to github.com", "", "github.com"},
		{"explicit host preserved", "github.example.com", "github.example.com"},
		{"github.com explicit", "github.com", "github.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Repo{
				Owner:        "a",
				Name:         "b",
				PlatformHost: tt.host,
			}
			Assert.Equal(t, tt.want, r.PlatformHostOrDefault())
		})
	}
}

func TestRepoResolveToken(t *testing.T) {
	t.Run("token_env set and populated", func(t *testing.T) {
		t.Setenv("MY_GHE_TOKEN", "ghe-secret")
		r := Repo{Owner: "a", Name: "b", TokenEnv: "MY_GHE_TOKEN"}
		Assert.Equal(t, "ghe-secret", r.ResolveToken("global-token"))
	})

	t.Run("token_env set but empty falls back to global", func(t *testing.T) {
		t.Setenv("MY_GHE_TOKEN", "")
		r := Repo{Owner: "a", Name: "b", TokenEnv: "MY_GHE_TOKEN"}
		Assert.Equal(t, "global-token", r.ResolveToken("global-token"))
	})

	t.Run("token_env not set falls back to global", func(t *testing.T) {
		r := Repo{Owner: "a", Name: "b"}
		Assert.Equal(t, "global-token", r.ResolveToken("global-token"))
	})
}

func TestValidateRejectsDuplicateOwnerName(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "apache"
name = "arrow"

[[repos]]
owner = "apache"
name = "arrow"
`)
	_, err := Load(path)
	require.Error(t, err)
	Assert.Contains(t, err.Error(), "duplicate repo")
}

func TestValidateRejectsConflictingTokenEnv(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "org1"
name = "repo1"
platform_host = "github.example.com"
token_env = "GHE_TOKEN_A"

[[repos]]
owner = "org2"
name = "repo2"
platform_host = "github.example.com"
token_env = "GHE_TOKEN_B"
`)
	_, err := Load(path)
	require.Error(t, err)
	Assert.Contains(t, err.Error(), "conflicting token_env")
}

func TestSaveRoundTripPlatformHost(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "apache"
name = "arrow"
platform_host = "github.example.com"
token_env = "GHE_TOKEN"

[[repos]]
owner = "ibis-project"
name = "ibis"
`)
	cfg, err := Load(path)
	require.NoError(err)

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	require.NoError(cfg.Save(savePath))

	cfg2, err := Load(savePath)
	require.NoError(err)
	require.Len(cfg2.Repos, 2)
	assert.Equal("github.example.com", cfg2.Repos[0].PlatformHost)
	assert.Equal("GHE_TOKEN", cfg2.Repos[0].TokenEnv)
	assert.Empty(cfg2.Repos[1].PlatformHost)
	assert.Empty(cfg2.Repos[1].TokenEnv)
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

func TestRoborevConfigRoundTrip(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"

[roborev]
endpoint = "http://custom:9999"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal("http://custom:9999", cfg.RoborevEndpoint())

	savePath := filepath.Join(t.TempDir(), "saved.toml")
	require.NoError(t, cfg.Save(savePath))

	cfg2, err := Load(savePath)
	require.NoError(t, err)
	assert.Equal("http://custom:9999", cfg2.RoborevEndpoint())
}

func TestSyncBudgetPerHour(t *testing.T) {
	t.Run("default is 500 when not set", func(t *testing.T) {
		path := writeConfig(t, `
[[repos]]
owner = "a"
name = "b"
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		Assert.Equal(t, 500, cfg.BudgetPerHour())
	})

	t.Run("rejects value below 50", func(t *testing.T) {
		path := writeConfig(t, `
sync_budget_per_hour = 49
[[repos]]
owner = "a"
name = "b"
`)
		_, err := Load(path)
		require.Error(t, err)
		Assert.Contains(t, err.Error(), "sync_budget_per_hour must be >= 50 or omitted")
	})

	t.Run("configured value preserved", func(t *testing.T) {
		path := writeConfig(t, `
sync_budget_per_hour = 1000
[[repos]]
owner = "a"
name = "b"
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		Assert.Equal(t, 1000, cfg.BudgetPerHour())
	})

	t.Run("round-trips through Save", func(t *testing.T) {
		require := require.New(t)
		path := writeConfig(t, `
sync_budget_per_hour = 750
[[repos]]
owner = "a"
name = "b"
`)
		cfg, err := Load(path)
		require.NoError(err)

		savePath := filepath.Join(t.TempDir(), "saved.toml")
		require.NoError(cfg.Save(savePath))

		cfg2, err := Load(savePath)
		require.NoError(err)
		Assert.Equal(t, 750, cfg2.BudgetPerHour())
	})
}

func TestRoborevEndpointDefault(t *testing.T) {
	cfg := &Config{}
	Assert.Equal(
		t, "http://127.0.0.1:7373", cfg.RoborevEndpoint(),
	)
}

func TestLoadTmuxCommand(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[tmux]
command = ["systemd-run", "--user", "--scope", "tmux"]
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(
		[]string{"systemd-run", "--user", "--scope", "tmux"},
		cfg.Tmux.Command,
	)
}

func TestLoadTmuxCommandOmitted(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, ``)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Empty(cfg.Tmux.Command)
	assert.Equal([]string{"tmux"}, cfg.TmuxCommand())
}

func TestLoadTmuxCommandEmptyArray(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[tmux]
command = []
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal([]string{"tmux"}, cfg.TmuxCommand())
}

func TestTmuxCommandDefensiveCopy(t *testing.T) {
	assert := Assert.New(t)
	cfg := &Config{Tmux: Tmux{
		Command: []string{"tmux"},
	}}
	first := cfg.TmuxCommand()
	first[0] = "hacked"
	second := cfg.TmuxCommand()
	assert.Equal([]string{"tmux"}, second)
}

func TestTmuxCommandNilReceiver(t *testing.T) {
	assert := Assert.New(t)
	var cfg *Config
	assert.Equal([]string{"tmux"}, cfg.TmuxCommand())
}

func TestLoadTmuxCommandRejectsEmptyFirstElement(t *testing.T) {
	path := writeConfig(t, `
[tmux]
command = ["", "extra"]
`)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(
		t, err.Error(),
		`config: invalid tmux.command`,
	)
}

// TestLoadTmuxCommandRejectsWhitespaceFirstElement covers the
// whitespace-only case: "   " would sneak past a plain == "" check
// and exec("   ") fails with a confusing shell-level error rather
// than the config-load validation message operators actually want.
func TestLoadTmuxCommandRejectsWhitespaceFirstElement(t *testing.T) {
	path := writeConfig(t, `
[tmux]
command = ["   ", "extra"]
`)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(
		t, err.Error(),
		`config: invalid tmux.command`,
	)
}

func TestSavePreservesTmuxCommand(t *testing.T) {
	assert := Assert.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		SyncInterval:   "5m",
		GitHubTokenEnv: "MIDDLEMAN_GITHUB_TOKEN",
		Host:           "127.0.0.1",
		Port:           8091,
		DataDir:        dir,
		Activity:       Activity{ViewMode: "threaded", TimeRange: "7d"},
		Tmux: Tmux{
			Command: []string{"systemd-run", "--user", "--scope", "tmux"},
		},
	}
	require.NoError(t, cfg.Save(path))

	reloaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(
		[]string{"systemd-run", "--user", "--scope", "tmux"},
		reloaded.Tmux.Command,
	)
}
