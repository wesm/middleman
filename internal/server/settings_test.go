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
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
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
	if err := os.WriteFile(
		cfgPath, []byte(cfgContent), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(
		mock, database, nil, time.Minute,
	)
	srv := NewWithConfig(
		database, mock, syncer, nil, cfg, cfgPath,
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
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
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
	srv, _, _ := setupTestServerWithConfig(t)

	rr := doJSON(t, srv, http.MethodGet, "/api/v1/settings", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp settingsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(resp.Repos))
	}
	if resp.Repos[0].Owner != "acme" {
		t.Fatalf("expected acme, got %s", resp.Repos[0].Owner)
	}
	if resp.Activity.ViewMode != "flat" {
		t.Fatalf(
			"expected flat view mode, got %s", resp.Activity.ViewMode,
		)
	}
}

func TestHandleUpdateSettings(t *testing.T) {
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
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify persisted to disk.
	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Activity.ViewMode != "threaded" {
		t.Fatalf(
			"expected threaded, got %s", cfg2.Activity.ViewMode,
		)
	}
	if cfg2.Activity.TimeRange != "30d" {
		t.Fatalf("expected 30d, got %s", cfg2.Activity.TimeRange)
	}
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
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify config was NOT modified (rollback).
	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Activity.ViewMode != "flat" {
		t.Fatalf(
			"expected flat after rollback, got %s",
			cfg2.Activity.ViewMode,
		)
	}
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
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(cfg2.Repos))
	}
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
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteRepo(t *testing.T) {
	srv, _, cfgPath := setupTestServerWithConfig(t)

	// Add a second repo first so we can delete one.
	addBody := map[string]string{
		"owner": "other-org",
		"name":  "other-repo",
	}
	addRR := doJSON(
		t, srv, http.MethodPost, "/api/v1/repos", addBody,
	)
	if addRR.Code != http.StatusCreated {
		t.Fatalf(
			"setup: expected 201, got %d: %s",
			addRR.Code, addRR.Body.String(),
		)
	}

	rr := doJSON(
		t, srv, http.MethodDelete,
		"/api/v1/repos/acme/widget", nil,
	)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg2.Repos))
	}
	if cfg2.Repos[0].Owner != "other-org" {
		t.Fatalf("expected other-org, got %s", cfg2.Repos[0].Owner)
	}
}

func TestHandleDeleteLastRepo(t *testing.T) {
	srv, _, _ := setupTestServerWithConfig(t)

	rr := doJSON(
		t, srv, http.MethodDelete,
		"/api/v1/repos/acme/widget", nil,
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
