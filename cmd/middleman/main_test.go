package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/testutil"
)

func TestResolveStartupReposExpandsConfiguredGlobs(t *testing.T) {
	assert := Assert.New(t)
	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "roborev-dev", Name: "*"}},
	}
	client := &testutil.FixtureClient{
		ReposByOwner: map[string][]*gh.Repository{
			"roborev-dev": {
				{
					Name:     new("middleman"),
					Archived: new(false),
				},
				{
					Name:     new("archived"),
					Archived: new(true),
				},
			},
		},
	}

	repos := resolveStartupRepos(
		t.Context(),
		cfg,
		map[string]ghclient.Client{"github.com": client},
		nil,
	)

	assert.Equal([]ghclient.RepoRef{{
		Owner:        "roborev-dev",
		Name:         "middleman",
		PlatformHost: "github.com",
	}}, repos)
}

func TestResolveStartupReposKeepsExactReposWhenResolutionFails(t *testing.T) {
	assert := Assert.New(t)
	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "roborev-dev", Name: "middleman"}},
	}

	repos := resolveStartupRepos(
		t.Context(),
		cfg,
		map[string]ghclient.Client{},
		nil,
	)

	assert.Equal([]ghclient.RepoRef{{
		Owner:        "roborev-dev",
		Name:         "middleman",
		PlatformHost: "github.com",
	}}, repos)
}

func TestResolveStartupReposFallsBackToDBForOfflineGlobs(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	ctx := t.Context()
	_, err = database.UpsertRepo(ctx, "github.com", "acme", "widgets")
	require.NoError(err)
	_, err = database.UpsertRepo(ctx, "github.com", "acme", "tools")
	require.NoError(err)

	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "acme", Name: "*"}},
	}

	repos := resolveStartupRepos(
		ctx, cfg, map[string]ghclient.Client{}, database,
	)

	assert.Len(repos, 2)
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	assert.ElementsMatch([]string{"widgets", "tools"}, names)
}

func TestStartupFallbackKeepsPersistedGlobMatchesInAPIs(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	_, err = database.UpsertRepo(
		t.Context(), "github.com", "roborev-dev", "middleman",
	)
	require.NoError(err)
	_, err = database.UpsertRepo(
		t.Context(), "github.com", "roborev-dev", "worker",
	)
	require.NoError(err)

	cfgPath := filepath.Join(dir, "config.toml")
	cfg := &config.Config{
		GitHubTokenEnv: "MIDDLEMAN_GITHUB_TOKEN",
		Host:           "127.0.0.1",
		Port:           8091,
		BasePath:       "/",
		DataDir:        dir,
		Repos: []config.Repo{
			{Owner: "roborev-dev", Name: "*"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	require.NoError(cfg.Save(cfgPath))

	client := &testutil.FixtureClient{
		ListRepositoriesByOwnerFn: func(
			context.Context, string,
		) ([]*gh.Repository, error) {
			return nil, errors.New("offline")
		},
	}
	repos := resolveStartupRepos(
		t.Context(),
		cfg,
		map[string]ghclient.Client{"github.com": client},
		database,
	)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": client},
		database, nil, repos, 0, nil, nil,
	)
	t.Cleanup(syncer.Stop)

	srv := server.NewWithConfig(
		database, syncer, nil, nil, cfg, cfgPath,
		server.ServerOptions{},
	)

	reposReq := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	reposRR := httptest.NewRecorder()
	srv.ServeHTTP(reposRR, reposReq)
	require.Equal(http.StatusOK, reposRR.Code, reposRR.Body.String())

	var listed []struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	require.NoError(json.NewDecoder(reposRR.Body).Decode(&listed))
	require.Len(listed, 2)
	assert.ElementsMatch([]string{"middleman", "worker"}, []string{
		listed[0].Name,
		listed[1].Name,
	})

	settingsReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	settingsRR := httptest.NewRecorder()
	srv.ServeHTTP(settingsRR, settingsReq)
	require.Equal(http.StatusOK, settingsRR.Code, settingsRR.Body.String())

	var settings struct {
		Repos []struct {
			Owner            string `json:"owner"`
			Name             string `json:"name"`
			MatchedRepoCount int    `json:"matched_repo_count"`
		} `json:"repos"`
	}
	require.NoError(json.NewDecoder(settingsRR.Body).Decode(&settings))
	require.Len(settings.Repos, 1)
	assert.Equal("roborev-dev", settings.Repos[0].Owner)
	assert.Equal("*", settings.Repos[0].Name)
	assert.Equal(2, settings.Repos[0].MatchedRepoCount)
}

func TestRunCLIConfigReadPort(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(os.WriteFile(cfgPath, []byte("port = 9123\n"), 0o644))

	var stdout bytes.Buffer
	err := runCLI([]string{"config", "read", "-config", cfgPath, "port"}, &stdout)
	require.NoError(err)
	assert.Equal("9123\n", stdout.String())
}

func TestRunCLIConfigReadPortCreatesDefaultConfig(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	var stdout bytes.Buffer
	err := runCLI([]string{"config", "read", "-config", cfgPath, "port"}, &stdout)
	require.NoError(err)
	assert.Equal("8091\n", stdout.String())

	content, err := os.ReadFile(cfgPath)
	require.NoError(err)
	assert.Contains(string(content), "port = 8091")
}
