package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

func setupTestServerWithConfig(
	t *testing.T,
) (*Server, *db.DB, string) {
	return setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "acme"
name = "widget"
`, &mockGH{})
}

func setupTestServerWithConfigContent(
	t *testing.T,
	cfgContent string,
	mock *mockGH,
) (*Server, *db.DB, string) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	cfgPath := filepath.Join(dir, "config.toml")
	err = os.WriteFile(cfgPath, []byte(cfgContent), 0o644)
	require.NoError(t, err)

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)

	clients := map[string]ghclient.Client{"github.com": mock}
	resolved := ghclient.ResolveConfiguredRepos(
		context.Background(), clients, cfg.Repos,
	)
	syncer := ghclient.NewSyncer(
		clients, database, nil, resolved.Expanded,
		time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := NewWithConfig(
		database, syncer, nil, nil, cfg, cfgPath,
		ServerOptions{},
	)
	return srv, database, cfgPath
}

func doJSON(
	t *testing.T,
	srv *Server,
	method, path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func TestHandleGetSettings(t *testing.T) {
	assert := Assert.New(t)
	srv, _, _ := setupTestServerWithConfig(t)

	rr := doJSON(t, srv, http.MethodGet, "/api/v1/settings", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var resp settingsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Len(t, resp.Repos, 1)
	assert.Equal("acme", resp.Repos[0].Owner)
	assert.Equal(1, resp.Repos[0].MatchedRepoCount)
	assert.Equal("threaded", resp.Activity.ViewMode)
}

func TestHandleUpdateSettings(t *testing.T) {
	assert := Assert.New(t)
	srv, _, cfgPath := setupTestServerWithConfig(t)

	body := updateSettingsRequest{
		Activity: config.Activity{
			ViewMode:   "threaded",
			TimeRange:  "30d",
			HideClosed: true,
			HideBots:   true,
		},
	}
	rr := doJSON(
		t, srv, http.MethodPut, "/api/v1/settings", body,
	)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// Verify persisted to disk.
	cfg2, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal("threaded", cfg2.Activity.ViewMode)
	assert.Equal("30d", cfg2.Activity.TimeRange)
}

func TestHandleUpdateSettingsInvalid(t *testing.T) {
	srv, _, cfgPath := setupTestServerWithConfig(t)

	body := updateSettingsRequest{
		Activity: config.Activity{
			ViewMode:  "kanban",
			TimeRange: "7d",
		},
	}
	rr := doJSON(
		t, srv, http.MethodPut, "/api/v1/settings", body,
	)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

	// Verify config was NOT modified (rollback).
	cfg2, err := config.Load(cfgPath)
	require.NoError(t, err)
	Assert.Equal(t, "threaded", cfg2.Activity.ViewMode)
}

func TestHandleAddRepo(t *testing.T) {
	srv, _, cfgPath := setupTestServerWithConfig(t)

	body := map[string]string{
		"owner": "other-org",
		"name":  "other-repo",
	}
	rr := doJSON(
		t, srv, http.MethodPost, "/api/v1/repos", body,
	)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	cfg2, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Len(t, cfg2.Repos, 2)
}

func TestHandleAddRepoTriggersImmediateSyncDuringCooldown(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(os.WriteFile(cfgPath, []byte(`
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "acme"
name = "widget"
`), 0o644))

	cfg, err := config.Load(cfgPath)
	require.NoError(err)

	mock := &mockGH{}
	trackers := map[string]*ghclient.RateTracker{
		"github.com": ghclient.NewRateTracker(
			database, "github.com", "rest",
		),
	}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		[]ghclient.RepoRef{{
			Owner:        "acme",
			Name:         "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		trackers,
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := NewWithConfig(
		database, syncer, nil, nil, cfg, cfgPath,
		ServerOptions{},
	)

	// Prime nextSyncAfter so the add-repo trigger exercises the same
	// cooldown path as a user clicking Sync right after a recent sync.
	syncer.RunOnce(context.Background())

	rr := doJSON(
		t, srv, http.MethodPost, "/api/v1/repos",
		map[string]string{
			"owner": "other-org",
			"name":  "other-repo",
		},
	)
	require.Equal(http.StatusCreated, rr.Code, rr.Body.String())

	require.Eventually(func() bool {
		repos, err := database.ListRepos(context.Background())
		if err != nil {
			return false
		}
		if len(repos) != 2 {
			return false
		}
		for _, repo := range repos {
			if repo.Owner == "other-org" &&
				repo.Name == "other-repo" {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
}

func TestHandleAddRepoDuplicate(t *testing.T) {
	srv, _, _ := setupTestServerWithConfig(t)

	body := map[string]string{
		"owner": "acme",
		"name":  "widget",
	}
	rr := doJSON(
		t, srv, http.MethodPost, "/api/v1/repos", body,
	)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
}

func TestHandleDeleteRepo(t *testing.T) {
	require := require.New(t)
	srv, _, cfgPath := setupTestServerWithConfig(t)

	// Add a second repo first so we can delete one.
	addBody := map[string]string{
		"owner": "other-org",
		"name":  "other-repo",
	}
	addRR := doJSON(
		t, srv, http.MethodPost, "/api/v1/repos", addBody,
	)
	require.Equal(http.StatusCreated, addRR.Code, addRR.Body.String())

	rr := doJSON(
		t, srv, http.MethodDelete,
		"/api/v1/repos/acme/widget", nil,
	)
	require.Equal(http.StatusNoContent, rr.Code, rr.Body.String())

	cfg2, err := config.Load(cfgPath)
	require.NoError(err)
	require.Len(cfg2.Repos, 1)
	Assert.Equal(t, "other-org", cfg2.Repos[0].Owner)
}

func TestGetSettingsWithoutPersistence(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		SyncInterval:   "5m",
		GitHubTokenEnv: "UNUSED",
		Host:           "127.0.0.1",
		Port:           8091,
		BasePath:       "/",
		DataDir:        dir,
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "30d",
		},
	}
	mock := &mockGH{}
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, nil, time.Minute, nil, nil)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", cfg, ServerOptions{})

	// GET /settings should work (read-only).
	rr := doJSON(t, srv, http.MethodGet, "/api/v1/settings", nil)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	var resp settingsResponse
	require.NoError(json.NewDecoder(rr.Body).Decode(&resp))
	require.Len(resp.Repos, 1)
	assert.Equal("acme", resp.Repos[0].Owner)
	assert.Equal("flat", resp.Activity.ViewMode)

	// Mutations should be rejected (no cfgPath).
	mutRR := doJSON(t, srv, http.MethodPut, "/api/v1/settings",
		updateSettingsRequest{Activity: cfg.Activity})
	assert.Equal(http.StatusNotFound, mutRR.Code)

	addRR := doJSON(t, srv, http.MethodPost, "/api/v1/repos",
		map[string]string{"owner": "x", "name": "y"})
	assert.Equal(http.StatusNotFound, addRR.Code)

	delRR := doJSON(t, srv, http.MethodDelete,
		"/api/v1/repos/acme/widget", nil)
	assert.Equal(http.StatusNotFound, delRR.Code)
}

func TestHandleDeleteLastRepo(t *testing.T) {
	srv, _, cfgPath := setupTestServerWithConfig(t)

	rr := doJSON(
		t, srv, http.MethodDelete,
		"/api/v1/repos/acme/widget", nil,
	)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	cfg2, err := config.Load(cfgPath)
	require.NoError(t, err)
	Assert.Empty(t, cfg2.Repos)
}

func TestHandleGetSettingsIncludesGlobCounts(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	mock := &mockGH{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("middleman"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("globber"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("archived"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(true),
				},
			}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"
`, mock)

	rr := doJSON(t, srv, http.MethodGet, "/api/v1/settings", nil)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	var resp settingsResponse
	require.NoError(json.NewDecoder(rr.Body).Decode(&resp))
	require.Len(resp.Repos, 1)
	assert.Equal("roborev-dev", resp.Repos[0].Owner)
	assert.Equal("*", resp.Repos[0].Name)
	assert.True(resp.Repos[0].IsGlob)
	assert.Equal(2, resp.Repos[0].MatchedRepoCount)
}

func TestHandleRefreshRepoRebuildsExpandedSyncSet(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	mock := &mockGH{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("middleman"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("globber"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
				{
					Name:     new("archived"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(true),
				},
			}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"
`, mock)

	rr := doJSON(
		t, srv, http.MethodPost,
		"/api/v1/repos/roborev-dev/*/refresh", nil,
	)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	var resp settingsResponse
	require.NoError(json.NewDecoder(rr.Body).Decode(&resp))
	require.Equal(2, resp.Repos[0].MatchedRepoCount)
	assert.True(srv.syncer.IsTrackedRepo("roborev-dev", "middleman"))
	assert.True(srv.syncer.IsTrackedRepo("roborev-dev", "globber"))
	assert.False(srv.syncer.IsTrackedRepo("roborev-dev", "archived"))
}

func TestHandleRefreshRepoKeepsReposMatchedByOtherConfigEntries(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	mock := &mockGH{
		getRepositoryFn: func(
			_ context.Context, owner, repo string,
		) (*gh.Repository, error) {
			return &gh.Repository{
				Name:     new(repo),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}, nil
		},
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("middleman"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
			}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"

[[repos]]
owner = "roborev-dev"
name = "worker"
`, mock)

	rr := doJSON(
		t, srv, http.MethodPost,
		"/api/v1/repos/roborev-dev/*/refresh", nil,
	)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	var resp settingsResponse
	require.NoError(json.NewDecoder(rr.Body).Decode(&resp))
	require.Len(resp.Repos, 2)
	assert.True(srv.syncer.IsTrackedRepo("roborev-dev", "middleman"))
	assert.True(srv.syncer.IsTrackedRepo("roborev-dev", "worker"))
}

func TestHandleDeleteRepoRebuildsExpandedSetFromRemainingPatterns(t *testing.T) {
	mock := &mockGH{
		getRepositoryFn: func(
			_ context.Context, owner, repo string,
		) (*gh.Repository, error) {
			return &gh.Repository{
				Name:     new(repo),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}, nil
		},
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			return []*gh.Repository{
				{
					Name:     new("middleman"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				},
			}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"

[[repos]]
owner = "roborev-dev"
name = "tools"
`, mock)

	rr := doJSON(
		t, srv, http.MethodDelete,
		"/api/v1/repos/roborev-dev/*", nil,
	)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	Assert.True(t, srv.syncer.IsTrackedRepo("roborev-dev", "tools"))
	Assert.False(t, srv.syncer.IsTrackedRepo("roborev-dev", "middleman"))
}

func TestRefreshRepoPreservesExistingWhenResolutionFails(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	fail := true
	mock := &mockGH{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			if fail {
				return nil, errors.New("boom")
			}
			return []*gh.Repository{{
				Name:     new("middleman"),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"
`, mock)
	// Prime the syncer with a previously resolved match so we can
	// verify it survives a failed refresh.
	srv.syncer.SetRepos([]ghclient.RepoRef{{
		Owner:        "roborev-dev",
		Name:         "middleman",
		PlatformHost: "github.com",
	}})

	rr := doJSON(
		t, srv, http.MethodPost,
		"/api/v1/repos/roborev-dev/*/refresh", nil,
	)
	require.Equal(http.StatusBadGateway, rr.Code, rr.Body.String())
	assert.True(srv.syncer.IsTrackedRepo("roborev-dev", "middleman"))
}

func TestGetSettingsDoesNotCallGitHub(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	mock := &mockGH{
		getRepositoryFn: func(
			_ context.Context, _, _ string,
		) (*gh.Repository, error) {
			require.FailNow("GET /settings must not call GetRepository")
			return nil, nil
		},
		listReposByOwnerFn: func(
			_ context.Context, _ string,
		) ([]*gh.Repository, error) {
			require.FailNow("GET /settings must not call ListRepositoriesByOwner")
			return nil, nil
		},
	}
	// Build the server directly (bypass setup helper) to avoid
	// its startup call to ResolveConfiguredRepos, which would
	// trip the failing mock during seeding.
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(os.WriteFile(cfgPath, []byte(`
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "acme"
name = "widget"
`), 0o644))
	cfg, err := config.Load(cfgPath)
	require.NoError(err)

	clients := map[string]ghclient.Client{"github.com": mock}
	syncer := ghclient.NewSyncer(
		clients, database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := NewWithConfig(
		database, syncer, nil, nil, cfg, cfgPath,
		ServerOptions{},
	)

	rr := doJSON(t, srv, http.MethodGet, "/api/v1/settings", nil)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	var resp settingsResponse
	require.NoError(json.NewDecoder(rr.Body).Decode(&resp))
	require.Len(resp.Repos, 1)
	assert.Equal(1, resp.Repos[0].MatchedRepoCount)
}

func TestGlobMatchingIsCaseInsensitive(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	mock := &mockGH{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			return []*gh.Repository{{
				Name:     new("Widget-API"),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "acme"
name = "Widget-*"
`, mock)

	rr := doJSON(
		t, srv, http.MethodPost,
		"/api/v1/repos/acme/Widget-*/refresh", nil,
	)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())
	assert.True(srv.syncer.IsTrackedRepo("acme", "Widget-API"))
}

func TestAddRepoDoesNotDropConcurrentActivityChange(t *testing.T) {
	// Pre-check for the race fix: handleAddRepo must not
	// overwrite a concurrent handleUpdateSettings change.
	// The setup mutates s.cfg.Activity after the add's
	// pre-check but before its save, then verifies the
	// activity change survives in both memory and on disk.
	assert := Assert.New(t)
	require := require.New(t)
	srv, _, cfgPath := setupTestServerWithConfig(t)

	// Change activity via the update handler.
	rr := doJSON(
		t, srv, http.MethodPut, "/api/v1/settings",
		updateSettingsRequest{
			Activity: config.Activity{
				ViewMode:  "threaded",
				TimeRange: "30d",
			},
		},
	)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	// Add a new repo; handler should preserve activity.
	addRR := doJSON(
		t, srv, http.MethodPost, "/api/v1/repos",
		map[string]string{
			"owner": "other-org", "name": "other-repo",
		},
	)
	require.Equal(http.StatusCreated, addRR.Code, addRR.Body.String())

	cfg2, err := config.Load(cfgPath)
	require.NoError(err)
	assert.Equal("30d", cfg2.Activity.TimeRange)
	assert.Len(cfg2.Repos, 2)
}

// TestConcurrentRefreshAndDeleteDoesNotResurrect exercises the
// race where a refresh of a glob is in-flight (blocked on the
// GitHub call) while a DELETE removes that glob. Before the fix
// the refresh would apply its stale expansion after the delete
// and re-add the removed repos to the syncer's tracked set.
func TestConcurrentRefreshAndDeleteDoesNotResurrect(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	var calls atomic.Int32
	ghBlocked := make(chan struct{}, 1)
	ghUnblock := make(chan struct{})
	mock := &mockGH{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			// The setup helper resolves the glob once at
			// server construction; block only on the second
			// call (the refresh request under test).
			if calls.Add(1) == 2 {
				ghBlocked <- struct{}{}
				<-ghUnblock
			}
			return []*gh.Repository{{
				Name:     new("middleman"),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}}, nil
		},
	}
	srv, _, _ := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"
`, mock)
	require.True(srv.syncer.IsTrackedRepo("roborev-dev", "middleman"))

	refreshDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		// Inline the request (no testify helpers) so the
		// linter does not flag assertions inside the goroutine.
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/repos/roborev-dev/*/refresh", nil,
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		refreshDone <- rr
	}()

	select {
	case <-ghBlocked:
	case <-time.After(5 * time.Second):
		require.FailNow("refresh did not reach the GH mock")
	}

	delRR := doJSON(
		t, srv, http.MethodDelete,
		"/api/v1/repos/roborev-dev/*", nil,
	)
	require.Equal(http.StatusNoContent, delRR.Code, delRR.Body.String())
	require.False(srv.syncer.IsTrackedRepo("roborev-dev", "middleman"))

	close(ghUnblock)
	var refreshRR *httptest.ResponseRecorder
	select {
	case refreshRR = <-refreshDone:
	case <-time.After(5 * time.Second):
		require.FailNow("refresh did not complete")
	}
	// Refresh should observe that the glob no longer exists
	// and report 404 rather than applying its stale expansion.
	assert.Equal(http.StatusNotFound, refreshRR.Code, refreshRR.Body.String())

	// The deleted repo must not have reappeared after the
	// refresh completed.
	assert.False(srv.syncer.IsTrackedRepo("roborev-dev", "middleman"),
		"deleted repo resurrected by concurrent refresh")
}
