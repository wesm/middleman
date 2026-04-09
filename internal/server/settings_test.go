package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

func setupTestServerWithConfig(
	t *testing.T,
) (*Server, *db.DB, string) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfgContent := `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "acme"
name = "widget"
`
	cfgPath := filepath.Join(dir, "config.toml")
	err = os.WriteFile(cfgPath, []byte(cfgContent), 0o644)
	require.NoError(t, err)

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock}, database, nil, nil, time.Minute, nil, 0,
	)
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
		Port:           8090,
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
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, nil, time.Minute, nil, 0)
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
