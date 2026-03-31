# Settings Page and Repo Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a settings page for managing repos and activity defaults through the UI, persisted to config.toml, and fix activity feed state loss on navigation.

**Architecture:** Config file remains source of truth. New API endpoints let the frontend add/remove repos (with GitHub validation) and read/write activity preferences. The Syncer gains a thread-safe SetRepos method. The activity store moves component-local filter state to module scope and adds a hydration phase gated by appReady in App.svelte.

**Tech Stack:** Go (stdlib HTTP, BurntSushi/toml), Svelte 5 (runes), TypeScript

**Spec:** `docs/superpowers/specs/2026-03-30-settings-and-repo-management-design.md`

---

### Task 1: Config — Activity struct and Save method

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for Activity defaults, validation, and Save**

Add to `internal/config/config_test.go`:

```go
func TestLoadActivityDefaults(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "test"
name = "repo"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Activity.ViewMode != "flat" {
		t.Fatalf("expected flat, got %s", cfg.Activity.ViewMode)
	}
	if cfg.Activity.TimeRange != "7d" {
		t.Fatalf("expected 7d, got %s", cfg.Activity.TimeRange)
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
owner = "test"
name = "repo"

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
		t.Fatalf("expected threaded, got %s", cfg.Activity.ViewMode)
	}
	if cfg.Activity.TimeRange != "30d" {
		t.Fatalf("expected 30d, got %s", cfg.Activity.TimeRange)
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
view_mode = "unknown"
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
host = "127.0.0.1"
port = 9000

[[repos]]
owner = "apache"
name = "arrow"

[activity]
view_mode = "threaded"
time_range = "30d"
hide_closed = true
hide_bots = false
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
		t.Fatalf("reload saved config: %v", err)
	}
	if cfg2.SyncInterval != "10m" {
		t.Fatalf("expected 10m, got %s", cfg2.SyncInterval)
	}
	if cfg2.Port != 9000 {
		t.Fatalf("expected 9000, got %d", cfg2.Port)
	}
	if len(cfg2.Repos) != 1 || cfg2.Repos[0].FullName() != "apache/arrow" {
		t.Fatalf("unexpected repos: %v", cfg2.Repos)
	}
	if cfg2.Activity.ViewMode != "threaded" {
		t.Fatalf("expected threaded, got %s", cfg2.Activity.ViewMode)
	}
	if cfg2.Activity.TimeRange != "30d" {
		t.Fatalf("expected 30d, got %s", cfg2.Activity.TimeRange)
	}
	if !cfg2.Activity.HideClosed {
		t.Fatal("expected hide_closed true")
	}
	if cfg2.Activity.HideBots {
		t.Fatal("expected hide_bots false")
	}
}

func TestSavePreservesDefaults(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
owner = "test"
name = "repo"
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
		t.Fatalf("reload saved config: %v", err)
	}
	if cfg2.SyncInterval != "5m" {
		t.Fatalf("expected default 5m, got %s", cfg2.SyncInterval)
	}
	if cfg2.Host != "127.0.0.1" {
		t.Fatalf("expected default host, got %s", cfg2.Host)
	}
	if cfg2.Port != 8090 {
		t.Fatalf("expected default port, got %d", cfg2.Port)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./internal/config/ -run "TestLoadActivity|TestSave" -v`

Expected: compilation errors — `Activity` struct and `Save` method don't exist yet.

- [ ] **Step 3: Implement Activity struct, apply defaults in Load, validate, and Save**

In `internal/config/config.go`, add the `Activity` struct and update `Config`:

```go
type Activity struct {
	ViewMode  string `toml:"view_mode"`
	TimeRange string `toml:"time_range"`
	HideClosed bool  `toml:"hide_closed"`
	HideBots   bool  `toml:"hide_bots"`
}

type Config struct {
	SyncInterval   string   `toml:"sync_interval"`
	GitHubTokenEnv string   `toml:"github_token_env"`
	Host           string   `toml:"host"`
	Port           int      `toml:"port"`
	BasePath       string   `toml:"base_path"`
	DataDir        string   `toml:"data_dir"`
	Repos          []Repo   `toml:"repos"`
	Activity       Activity `toml:"activity"`
}
```

In `Load`, after unmarshaling and before `Validate`, apply activity defaults:

```go
if cfg.Activity.ViewMode == "" {
	cfg.Activity.ViewMode = "flat"
}
if cfg.Activity.TimeRange == "" {
	cfg.Activity.TimeRange = "7d"
}
```

In `Validate`, add activity checks after the existing base_path validation:

```go
validViewModes := map[string]bool{"flat": true, "threaded": true}
if !validViewModes[c.Activity.ViewMode] {
	return fmt.Errorf("config: invalid activity view_mode %q", c.Activity.ViewMode)
}
validTimeRanges := map[string]bool{
	"24h": true, "7d": true, "30d": true, "90d": true,
}
if !validTimeRanges[c.Activity.TimeRange] {
	return fmt.Errorf("config: invalid activity time_range %q", c.Activity.TimeRange)
}
```

Add the `Save` method. This writes a TOML-serializable struct that omits internal fields like `DataDir` and `BasePath` (which are derived/normalized):

```go
// configFile is the subset of Config that gets written to disk.
// It excludes derived fields (DataDir, BasePath are computed on Load).
type configFile struct {
	SyncInterval   string   `toml:"sync_interval"`
	GitHubTokenEnv string   `toml:"github_token_env,omitempty"`
	Host           string   `toml:"host"`
	Port           int      `toml:"port"`
	BasePath       string   `toml:"base_path,omitempty"`
	DataDir        string   `toml:"data_dir,omitempty"`
	Repos          []Repo   `toml:"repos"`
	Activity       Activity `toml:"activity"`
}

func (c *Config) Save(path string) error {
	f := configFile{
		SyncInterval:   c.SyncInterval,
		GitHubTokenEnv: c.GitHubTokenEnv,
		Host:           c.Host,
		Port:           c.Port,
		DataDir:        c.DataDir,
		Repos:          c.Repos,
		Activity:       c.Activity,
	}
	// Only write base_path if it's non-default.
	if c.BasePath != "/" {
		f.BasePath = c.BasePath
	}
	// Only write data_dir if non-default.
	if c.DataDir != DefaultDataDir() {
		f.DataDir = c.DataDir
	}
	// Only write github_token_env if non-default.
	if c.GitHubTokenEnv == "MIDDLEMAN_GITHUB_TOKEN" {
		f.GitHubTokenEnv = ""
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
```

Add `"bytes"` to the imports in config.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./internal/config/ -v`

Expected: all tests pass including new and existing ones.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add Activity config struct, validation, and Save method"
```

---

### Task 2: Syncer — SetRepos with snapshot-under-lock

**Files:**
- Modify: `internal/github/sync.go`

- [ ] **Step 1: Add mutex and SetRepos method, update RunOnce to snapshot**

In `internal/github/sync.go`, add a `sync.Mutex` to the `Syncer` struct and a `SetRepos` method. Update `RunOnce` to copy the repo list under the lock.

Change the `Syncer` struct (around line 28):

```go
type Syncer struct {
	client   Client
	db       *db.DB
	repos    []RepoRef
	reposMu  sync.Mutex
	interval time.Duration
	running  atomic.Bool
	status   atomic.Value // stores *SyncStatus
	stopCh   chan struct{}
}
```

Add `"sync"` to the imports.

Add the `SetRepos` method after `NewSyncer`:

```go
// SetRepos updates the repo list that will be used by the next sync pass.
func (s *Syncer) SetRepos(repos []RepoRef) {
	s.reposMu.Lock()
	s.repos = repos
	s.reposMu.Unlock()
}
```

In `RunOnce` (around line 82), replace the direct read of `s.repos` with a snapshot:

```go
func (s *Syncer) RunOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	s.reposMu.Lock()
	repos := make([]RepoRef, len(s.repos))
	copy(repos, s.repos)
	s.reposMu.Unlock()

	s.status.Store(&SyncStatus{Running: true})
	slog.Info("sync started", "repos", len(repos))

	var lastErr string
	for i, repo := range repos {
		slog.Info("syncing repo",
			"repo", repo.Owner+"/"+repo.Name,
			"progress", fmt.Sprintf("%d/%d", i+1, len(repos)),
		)
		if err := s.syncRepo(ctx, repo); err != nil {
			slog.Error("sync repo failed", "repo", repo.Owner+"/"+repo.Name, "err", err)
			lastErr = err.Error()
		}
	}

	slog.Info("sync complete", "repos", len(repos))
	s.status.Store(&SyncStatus{
		Running:   false,
		LastRunAt: time.Now(),
		LastError: lastErr,
	})
}
```

- [ ] **Step 2: Run existing tests to verify nothing breaks**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./internal/github/ -v -short`

Expected: all existing tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/github/sync.go
git commit -m "Add Syncer.SetRepos with snapshot-under-lock in RunOnce"
```

---

### Task 3: Server — Thread config and add settings/repo API endpoints

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/handlers.go`
- Modify: `cmd/middleman/main.go`
- Test: `internal/server/handlers_test.go`

- [ ] **Step 1: Write failing tests for the new endpoints**

Add to `internal/server/handlers_test.go`:

```go
func TestHandleGetSettings(t *testing.T) {
	srv, _ := setupTestServerWithConfig(t, &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
			{Owner: "org", Name: "lib"},
		},
		Activity: config.Activity{
			ViewMode:   "threaded",
			TimeRange:  "30d",
			HideClosed: true,
			HideBots:   false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp settingsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(resp.Repos))
	}
	if resp.Activity.ViewMode != "threaded" {
		t.Fatalf("expected threaded, got %s", resp.Activity.ViewMode)
	}
}

func TestHandleUpdateSettings(t *testing.T) {
	srv, _ := setupTestServerWithConfig(t, &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos:        []config.Repo{{Owner: "acme", Name: "widget"}},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	})

	body := bytes.NewBufferString(`{"activity":{"view_mode":"threaded","time_range":"30d","hide_closed":true,"hide_bots":false}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp settingsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Activity.ViewMode != "threaded" {
		t.Fatalf("expected threaded, got %s", resp.Activity.ViewMode)
	}
	if resp.Activity.TimeRange != "30d" {
		t.Fatalf("expected 30d, got %s", resp.Activity.TimeRange)
	}
}

func TestHandleAddRepo(t *testing.T) {
	srv, _ := setupTestServerWithConfig(t, &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos:        []config.Repo{{Owner: "acme", Name: "widget"}},
		Activity:     config.Activity{ViewMode: "flat", TimeRange: "7d"},
	})

	body := bytes.NewBufferString(`{"owner":"org","name":"lib"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAddRepoDuplicate(t *testing.T) {
	srv, _ := setupTestServerWithConfig(t, &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos:        []config.Repo{{Owner: "acme", Name: "widget"}},
		Activity:     config.Activity{ViewMode: "flat", TimeRange: "7d"},
	})

	body := bytes.NewBufferString(`{"owner":"acme","name":"widget"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteRepo(t *testing.T) {
	srv, _ := setupTestServerWithConfig(t, &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
			{Owner: "org", Name: "lib"},
		},
		Activity: config.Activity{ViewMode: "flat", TimeRange: "7d"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/repos/org/lib", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteLastRepo(t *testing.T) {
	srv, _ := setupTestServerWithConfig(t, &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos:        []config.Repo{{Owner: "acme", Name: "widget"}},
		Activity:     config.Activity{ViewMode: "flat", TimeRange: "7d"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/repos/acme/widget", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for last repo, got %d: %s", rr.Code, rr.Body.String())
	}
}
```

Also add the test helper `setupTestServerWithConfig`:

```go
func setupTestServerWithConfig(t *testing.T, cfg *config.Config) (*Server, *db.DB) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	configPath := filepath.Join(dir, "config.toml")
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, database, nil, time.Minute)
	srv := NewWithConfig(database, mock, syncer, nil, cfg, configPath)
	return srv, database
}
```

Add the `config` import to the test file:

```go
"github.com/wesm/middleman/internal/config"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./internal/server/ -run "TestHandleGetSettings|TestHandleUpdateSettings|TestHandleAddRepo|TestHandleDeleteRepo|TestHandleDeleteLastRepo|TestHandleAddRepoDuplicate" -v`

Expected: compilation errors — `NewWithConfig`, `settingsResponse`, and the new handlers don't exist yet.

- [ ] **Step 3: Update Server struct and constructor**

In `internal/server/server.go`, update the `Server` struct and add the new constructor:

```go
import (
	// ...existing imports...
	"sync"

	"github.com/wesm/middleman/internal/config"
	// ...
)

type Server struct {
	db         *db.DB
	gh         ghclient.Client
	syncer     *ghclient.Syncer
	cfg        *config.Config
	cfgPath    string
	cfgMu      sync.Mutex
	basePath   string
	handler    http.Handler
}
```

Refactor into a private `newServer` function and two public constructors:

```go
// New creates a Server without config persistence (used by tests).
func New(
	database *db.DB, gh ghclient.Client,
	syncer *ghclient.Syncer, frontend fs.FS,
	basePath string,
) *Server {
	return newServer(database, gh, syncer, frontend, basePath, nil, "")
}

// NewWithConfig creates a Server with config persistence for settings/repo endpoints.
func NewWithConfig(
	database *db.DB, gh ghclient.Client,
	syncer *ghclient.Syncer, frontend fs.FS,
	cfg *config.Config, cfgPath string,
) *Server {
	return newServer(database, gh, syncer, frontend, cfg.BasePath, cfg, cfgPath)
}

func newServer(
	database *db.DB, gh ghclient.Client,
	syncer *ghclient.Syncer, frontend fs.FS,
	basePath string, cfg *config.Config, cfgPath string,
) *Server {
```

Move the body of the old `New` into `newServer`, using the `basePath` parameter directly and adding `cfg`/`cfgPath`:

```go
	s := &Server{
		db:       database,
		basePath: basePath,
		gh:       gh,
		syncer:   syncer,
		cfg:      cfg,
		cfgPath:  cfgPath,
	}
```

This preserves backward compatibility: existing tests use `New(db, gh, syncer, nil, "/")`, while `main.go` uses `NewWithConfig(db, gh, syncer, assets, cfg, configPath)`.

Register the new routes in the mux (add these alongside the existing route registrations):

```go
	mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/v1/repos", s.handleAddRepo)
	mux.HandleFunc("DELETE /api/v1/repos/{owner}/{name}", s.handleDeleteRepo)
```

- [ ] **Step 4: Implement the handler functions**

In `internal/server/handlers.go`, add the new handlers and response types:

```go
// --- GET /api/v1/settings ---

type settingsResponse struct {
	Repos    []config.Repo   `json:"repos"`
	Activity config.Activity `json:"activity"`
}

func (s *Server) handleGetSettings(
	w http.ResponseWriter, r *http.Request,
) {
	s.cfgMu.Lock()
	resp := settingsResponse{
		Repos:    s.cfg.Repos,
		Activity: s.cfg.Activity,
	}
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// --- PUT /api/v1/settings ---

type updateSettingsRequest struct {
	Activity config.Activity `json:"activity"`
}

func (s *Server) handleUpdateSettings(
	w http.ResponseWriter, r *http.Request,
) {
	var body updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	s.cfg.Activity = body.Activity

	// Apply defaults for empty fields before validation.
	if s.cfg.Activity.ViewMode == "" {
		s.cfg.Activity.ViewMode = "flat"
	}
	if s.cfg.Activity.TimeRange == "" {
		s.cfg.Activity.TimeRange = "7d"
	}

	if err := s.cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.cfg.Save(s.cfgPath); err != nil {
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, settingsResponse{
		Repos:    s.cfg.Repos,
		Activity: s.cfg.Activity,
	})
}

// --- POST /api/v1/repos ---

func (s *Server) handleAddRepo(
	w http.ResponseWriter, r *http.Request,
) {
	var body struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Owner == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest,
			"owner and name are required")
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	// Check for duplicates.
	for _, r := range s.cfg.Repos {
		if r.Owner == body.Owner && r.Name == body.Name {
			writeError(w, http.StatusBadRequest,
				body.Owner+"/"+body.Name+" is already configured")
			return
		}
	}

	// Validate the repo exists on GitHub.
	if _, err := s.gh.GetRepository(
		r.Context(), body.Owner, body.Name,
	); err != nil {
		writeError(w, http.StatusBadGateway,
			"GitHub API error: "+err.Error())
		return
	}

	s.cfg.Repos = append(s.cfg.Repos, config.Repo{
		Owner: body.Owner, Name: body.Name,
	})

	if err := s.cfg.Save(s.cfgPath); err != nil {
		// Roll back the in-memory change.
		s.cfg.Repos = s.cfg.Repos[:len(s.cfg.Repos)-1]
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}

	// Update syncer and trigger sync.
	refs := make([]ghclient.RepoRef, len(s.cfg.Repos))
	for i, rp := range s.cfg.Repos {
		refs[i] = ghclient.RepoRef{
			Owner: rp.Owner, Name: rp.Name,
		}
	}
	s.syncer.SetRepos(refs)
	go s.syncer.RunOnce(context.WithoutCancel(r.Context()))

	writeJSON(w, http.StatusCreated, config.Repo{
		Owner: body.Owner, Name: body.Name,
	})
}

// --- DELETE /api/v1/repos/{owner}/{name} ---

func (s *Server) handleDeleteRepo(
	w http.ResponseWriter, r *http.Request,
) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	if len(s.cfg.Repos) <= 1 {
		writeError(w, http.StatusBadRequest,
			"cannot remove the last configured repository")
		return
	}

	idx := -1
	for i, rp := range s.cfg.Repos {
		if rp.Owner == owner && rp.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		writeError(w, http.StatusNotFound,
			owner+"/"+name+" is not configured")
		return
	}

	removed := s.cfg.Repos[idx]
	s.cfg.Repos = append(
		s.cfg.Repos[:idx], s.cfg.Repos[idx+1:]...,
	)

	if err := s.cfg.Save(s.cfgPath); err != nil {
		// Roll back.
		s.cfg.Repos = append(
			s.cfg.Repos[:idx],
			append([]config.Repo{removed}, s.cfg.Repos[idx:]...)...,
		)
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}

	refs := make([]ghclient.RepoRef, len(s.cfg.Repos))
	for i, rp := range s.cfg.Repos {
		refs[i] = ghclient.RepoRef{
			Owner: rp.Owner, Name: rp.Name,
		}
	}
	s.syncer.SetRepos(refs)

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Update main.go to pass config to server**

In `cmd/middleman/main.go`, change the `server.New` call to `server.NewWithConfig`:

```go
srv := server.NewWithConfig(database, ghClient, syncer, assets, cfg, configPath)
```

Where `configPath` is the `*configPath` variable already available in `run()`. Thread `configPath` through:

Change the `run` function signature — it already receives `configPath string`. Just use it:

```go
srv := server.NewWithConfig(database, ghClient, syncer, assets, cfg, configPath)
```

Add the import if needed: `"github.com/wesm/middleman/internal/server"` is already there.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./internal/server/ -v`

Expected: all tests pass including the new ones.

- [ ] **Step 7: Run full Go test suite**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./... -short`

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/server/server.go internal/server/handlers.go internal/server/handlers_test.go cmd/middleman/main.go
git commit -m "Add settings and repo management API endpoints"
```

---

### Task 4: Frontend — API client and types

**Files:**
- Modify: `frontend/src/lib/api/types.ts`
- Modify: `frontend/src/lib/api/client.ts`

- [ ] **Step 1: Add Settings types**

In `frontend/src/lib/api/types.ts`, add at the end:

```typescript
export interface ActivitySettings {
  view_mode: "flat" | "threaded";
  time_range: "24h" | "7d" | "30d" | "90d";
  hide_closed: boolean;
  hide_bots: boolean;
}

export interface ConfigRepo {
  owner: string;
  name: string;
}

export interface Settings {
  repos: ConfigRepo[];
  activity: ActivitySettings;
}
```

- [ ] **Step 2: Add API functions**

In `frontend/src/lib/api/client.ts`, add the import for the new types at the top (update the existing import line):

```typescript
import type { Issue, IssueDetail, KanbanStatus, PullDetail, PullRequest, Repo, Settings, SyncStatus } from "./types.js";
```

Add the new functions at the end of the file:

```typescript
export async function getSettings(): Promise<Settings> {
  return request<Settings>("/settings");
}

export async function updateSettings(
  settings: { activity: Settings["activity"] },
): Promise<Settings> {
  return request<Settings>("/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
}

export async function addRepo(
  owner: string,
  name: string,
): Promise<{ owner: string; name: string }> {
  return request("/repos", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ owner, name }),
  });
}

export async function removeRepo(
  owner: string,
  name: string,
): Promise<void> {
  await request(`/repos/${owner}/${name}`, { method: "DELETE" });
}
```

- [ ] **Step 3: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/lib/api/types.ts frontend/src/lib/api/client.ts
git commit -m "Add settings and repo management API client functions"
```

---

### Task 5: Frontend — Activity store refactor (state persistence + hydration)

**Files:**
- Modify: `frontend/src/lib/stores/activity.svelte.ts`

- [ ] **Step 1: Move component-local state to store and add hydration**

Replace the full contents of `frontend/src/lib/stores/activity.svelte.ts`. The key changes from the current file:
- Add `hideClosedMerged`, `hideBots`, `enabledEvents`, `itemFilter` as module-level state
- Add `initialized` flag
- Add `hydrateActivityDefaults()` function
- Change `syncFromURL()` to only override fields present in the URL

```typescript
import { listActivity } from "../api/activity.js";
import type { ActivityItem, ActivityParams } from "../api/activity.js";
import type { ActivitySettings } from "../api/types.js";

// --- constants ---

export type TimeRange = "24h" | "7d" | "30d" | "90d";
export type ViewMode = "flat" | "threaded";
export type ItemFilter = "all" | "prs" | "issues";

const RANGE_MS: Record<TimeRange, number> = {
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
  "90d": 90 * 24 * 60 * 60 * 1000,
};

const EVENT_TYPES = ["comment", "review", "commit"] as const;

// --- state ---

let items = $state<ActivityItem[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let capped = $state(false);
let filterRepo = $state<string | undefined>(undefined);
let filterTypes = $state<string[]>([]);
let searchQuery = $state<string | undefined>(undefined);
let timeRange = $state<TimeRange>("7d");
let viewMode = $state<ViewMode>("flat");
let hideClosedMerged = $state(false);
let hideBots = $state(false);
let enabledEvents = $state<Set<string>>(new Set(EVENT_TYPES));
let itemFilter = $state<ItemFilter>("all");
let initialized = false;
let pollHandle: ReturnType<typeof setInterval> | null = null;
let requestVersion = 0;

// --- reads ---

export function getActivityItems(): ActivityItem[] {
  return items;
}

export function isActivityLoading(): boolean {
  return loading;
}

export function getActivityError(): string | null {
  return error;
}

export function isActivityCapped(): boolean {
  return capped;
}

export function getActivityFilterRepo(): string | undefined {
  return filterRepo;
}

export function getActivityFilterTypes(): string[] {
  return filterTypes;
}

export function getActivitySearch(): string | undefined {
  return searchQuery;
}

export function getTimeRange(): TimeRange {
  return timeRange;
}

export function getViewMode(): ViewMode {
  return viewMode;
}

export function getHideClosedMerged(): boolean {
  return hideClosedMerged;
}

export function getHideBots(): boolean {
  return hideBots;
}

export function getEnabledEvents(): Set<string> {
  return enabledEvents;
}

export function getItemFilter(): ItemFilter {
  return itemFilter;
}

export function isInitialized(): boolean {
  return initialized;
}

// --- writes ---

export function setActivityFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}

export function setActivityFilterTypes(types: string[]): void {
  filterTypes = types;
}

export function setActivitySearch(q: string | undefined): void {
  searchQuery = q;
}

export function setTimeRange(range_: TimeRange): void {
  timeRange = range_;
}

export function setViewMode(mode: ViewMode): void {
  viewMode = mode;
}

export function setHideClosedMerged(v: boolean): void {
  hideClosedMerged = v;
}

export function setHideBots(v: boolean): void {
  hideBots = v;
}

export function setEnabledEvents(events: Set<string>): void {
  enabledEvents = events;
}

export function setItemFilter(f: ItemFilter): void {
  itemFilter = f;
}

// --- hydration ---

/**
 * Apply config defaults to the store. Called once on app startup,
 * before any view mounts. Only sets values; does not trigger loads.
 */
export function hydrateActivityDefaults(
  activity: ActivitySettings,
): void {
  viewMode = activity.view_mode;
  timeRange = activity.time_range;
  hideClosedMerged = activity.hide_closed;
  hideBots = activity.hide_bots;
}

/**
 * Called by ActivityFeed on mount. First mount syncs from URL,
 * subsequent mounts restore URL from store state.
 */
export function initializeFromMount(): void {
  if (!initialized) {
    syncFromURL();
    initialized = true;
  } else {
    syncToURL();
  }
}

// --- internal ---

function computeSince(): string {
  return new Date(Date.now() - RANGE_MS[timeRange]).toISOString();
}

function buildParams(): ActivityParams {
  const p: ActivityParams = { since: computeSince() };
  if (filterRepo) p.repo = filterRepo;
  if (filterTypes.length > 0) p.types = filterTypes;
  if (searchQuery) p.search = searchQuery;
  return p;
}

/** Load the full time window from scratch. */
export async function loadActivity(): Promise<void> {
  const version = ++requestVersion;
  loading = true;
  error = null;
  try {
    const resp = await listActivity(buildParams());
    if (version !== requestVersion) return;
    items = resp.items;
    capped = resp.capped;
  } catch (err_) {
    if (version !== requestVersion) return;
    error = err_ instanceof Error ? err_.message : String(err_);
  } finally {
    if (version === requestVersion) loading = false;
  }
}

/** Poll for new items since the newest displayed item. */
async function pollNewItems(): Promise<void> {
  if (items.length === 0) {
    await loadActivity();
    return;
  }
  try {
    const params = buildParams();
    params.after = items[0]!.cursor;
    const resp = await listActivity(params);
    if (resp.capped) {
      await loadActivity();
      return;
    }
    if (resp.items.length > 0) {
      const existingIds = new Set(items.map((it) => it.id));
      const newItems = resp.items.filter(
        (it) => !existingIds.has(it.id),
      );
      if (newItems.length > 0) {
        items = [...newItems, ...items];
      }
    }
  } catch {
    // Silent poll failure
  }
  const cutoff = new Date(Date.now() - RANGE_MS[timeRange]);
  items = items.filter((it) => new Date(it.created_at) >= cutoff);
}

export function startActivityPolling(): void {
  stopActivityPolling();
  pollHandle = setInterval(() => {
    void pollNewItems();
  }, 15_000);
}

export function stopActivityPolling(): void {
  if (pollHandle !== null) {
    clearInterval(pollHandle);
    pollHandle = null;
  }
}

/**
 * Sync URL query params -> store state. Only overrides fields
 * whose params are present in the URL; absent params leave the
 * current store value untouched.
 */
export function syncFromURL(): void {
  const sp = new URLSearchParams(window.location.search);

  if (sp.has("repo")) {
    filterRepo = sp.get("repo") ?? undefined;
  }
  if (sp.has("types")) {
    const typesParam = sp.get("types");
    filterTypes = typesParam ? typesParam.split(",") : [];
  }
  if (sp.has("search")) {
    searchQuery = sp.get("search") ?? undefined;
  }
  if (sp.has("range")) {
    const rangeParam = sp.get("range");
    if (rangeParam && rangeParam in RANGE_MS) {
      timeRange = rangeParam as TimeRange;
    }
  }
  if (sp.has("view")) {
    const viewParam = sp.get("view");
    if (viewParam === "flat" || viewParam === "threaded") {
      viewMode = viewParam;
    }
  }
}

/** Sync store state -> URL query params (replaceState). */
export function syncToURL(): void {
  const sp = new URLSearchParams(window.location.search);
  if (filterRepo) sp.set("repo", filterRepo);
  else sp.delete("repo");
  if (filterTypes.length > 0) sp.set("types", filterTypes.join(","));
  else sp.delete("types");
  if (searchQuery) sp.set("search", searchQuery);
  else sp.delete("search");
  if (timeRange !== "7d") sp.set("range", timeRange);
  else sp.delete("range");
  if (viewMode !== "flat") sp.set("view", viewMode);
  else sp.delete("view");
  const qs = sp.toString();
  const base =
    (window.__BASE_PATH__ ?? "/").replace(/\/$/, "") || "";
  const url = (base || "/") + (qs ? `?${qs}` : "");
  history.replaceState(null, "", url);
}
```

- [ ] **Step 2: Verify frontend builds**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project/frontend && bun run build`

Expected: build succeeds (or type errors from ActivityFeed that we fix in the next task).

- [ ] **Step 3: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/lib/stores/activity.svelte.ts
git commit -m "Move activity filter state to store, add hydration and partial syncFromURL"
```

---

### Task 6: Frontend — Update ActivityFeed to use store state

**Files:**
- Modify: `frontend/src/lib/components/ActivityFeed.svelte`

- [ ] **Step 1: Update ActivityFeed to read filter state from store**

In `ActivityFeed.svelte`, update the `<script>` section. The key changes:
- Remove local `hideClosedMerged`, `hideBots`, `itemFilter`, `enabledEvents` state declarations
- Import the store getters/setters and `initializeFromMount` instead
- Replace `onMount`'s `syncFromURL()` call with `initializeFromMount()`
- Replace all local state reads/writes with store function calls

Update the imports to add the new store functions:

```typescript
import { onMount, onDestroy } from "svelte";
import type { ActivityItem } from "../api/activity.js";
import {
  getActivityItems,
  isActivityLoading,
  getActivityError,
  isActivityCapped,
  getActivityFilterRepo,
  getActivityFilterTypes,
  getActivitySearch,
  getTimeRange,
  getViewMode,
  getHideClosedMerged,
  getHideBots,
  getEnabledEvents,
  getItemFilter,
  setActivityFilterRepo,
  setActivityFilterTypes,
  setActivitySearch,
  setTimeRange,
  setViewMode,
  setHideClosedMerged,
  setHideBots,
  setEnabledEvents,
  setItemFilter,
  loadActivity,
  startActivityPolling,
  stopActivityPolling,
  initializeFromMount,
  syncToURL,
} from "../stores/activity.svelte.js";
import type { TimeRange, ViewMode } from "../stores/activity.svelte.js";
import RepoTypeahead from "./RepoTypeahead.svelte";
import ActivityThreaded from "./ActivityThreaded.svelte";
```

Remove these local state declarations (lines 36-39 in the original):

```
let hideClosedMerged = $state(false);
let hideBots = $state(false);
```

And (line 47):
```
type ItemFilter = "all" | "prs" | "issues";
let itemFilter = $state<ItemFilter>("all");
```

And (line 67):
```
let enabledEvents = $state<Set<string>>(new Set(EVENT_TYPES));
```

Replace `onMount` body:

```typescript
onMount(() => {
  initializeFromMount();
  searchInput = getActivitySearch() ?? "";
  void loadActivity();
  startActivityPolling();
});
```

Remove `restoreFiltersFromStore()` function entirely — initialization is now handled by `initializeFromMount`.

Update `hiddenFilterCount` derived to use store getters:

```typescript
const hiddenFilterCount = $derived(
  (EVENT_TYPES.length - getEnabledEvents().size)
  + (getHideClosedMerged() ? 1 : 0)
  + (getHideBots() ? 1 : 0),
);
```

Update `applyFilters` to use store:

```typescript
function applyFilters(): void {
  const types: string[] = [];
  const currentItemFilter = getItemFilter();
  if (currentItemFilter === "prs") {
    types.push("new_pr");
  } else if (currentItemFilter === "issues") {
    types.push("new_issue");
  } else {
    types.push("new_pr", "new_issue");
  }
  for (const evt of getEnabledEvents()) {
    types.push(evt);
  }
  const allSelected = currentItemFilter === "all"
    && getEnabledEvents().size === EVENT_TYPES.length;
  setActivityFilterTypes(allSelected ? [] : types);
  syncToURL();
  void loadActivity();
}
```

Update `setItemFilter_` (rename to avoid collision with store import):

```typescript
function handleItemFilterChange(f: "all" | "prs" | "issues"): void {
  setItemFilter(f);
  applyFilters();
}
```

Update `toggleEvent`:

```typescript
function toggleEvent(evt: string): void {
  const current = getEnabledEvents();
  const next = new Set(current);
  if (next.has(evt)) {
    if (next.size > 1) next.delete(evt);
  } else {
    next.add(evt);
  }
  setEnabledEvents(next);
  applyFilters();
}
```

Update `displayItems` derived:

```typescript
const displayItems = $derived.by(() => {
  let result = getActivityItems();
  if (getHideClosedMerged()) {
    result = result.filter((it) =>
      it.item_state !== "merged" && it.item_state !== "closed");
  }
  if (getHideBots()) {
    result = result.filter((it) => !isBot(it.author));
  }
  return result;
});
```

Update `resetFilters`:

```typescript
function resetFilters(): void {
  setEnabledEvents(new Set(EVENT_TYPES));
  setHideClosedMerged(false);
  setHideBots(false);
  applyFilters();
}
```

In the template, replace `itemFilter` with `getItemFilter()`, `hideClosedMerged` with `getHideClosedMerged()`, `hideBots` with `getHideBots()`, `enabledEvents` with `getEnabledEvents()`, and `setItemFilter("all")` with `handleItemFilterChange("all")` etc.

- [ ] **Step 2: Verify frontend builds**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project/frontend && bun run build`

Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/lib/components/ActivityFeed.svelte
git commit -m "Use store state for activity filters, fix state loss on navigation"
```

---

### Task 7: Frontend — Router settings route

**Files:**
- Modify: `frontend/src/lib/stores/router.svelte.ts`

- [ ] **Step 1: Add settings to Route type and parseRoute**

In `frontend/src/lib/stores/router.svelte.ts`, update the `Route` type:

```typescript
export type Route =
  | { page: "activity" }
  | { page: "pulls"; view: "list" | "board"; selected?: { owner: string; name: string; number: number } }
  | { page: "issues"; selected?: { owner: string; name: string; number: number } }
  | { page: "settings" };
```

In `parseRoute`, add the settings check before the fallback `return { page: "activity" }`:

```typescript
if (path === "/settings") {
  return { page: "settings" };
}
return { page: "activity" };
```

Update `getPage` return type:

```typescript
export function getPage(): "activity" | "pulls" | "issues" | "settings" {
  return route.page;
}
```

- [ ] **Step 2: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/lib/stores/router.svelte.ts
git commit -m "Add settings route to frontend router"
```

---

### Task 8: Frontend — Settings page components

**Files:**
- Create: `frontend/src/lib/components/settings/SettingsSection.svelte`
- Create: `frontend/src/lib/components/settings/RepoSettings.svelte`
- Create: `frontend/src/lib/components/settings/ActivitySettings.svelte`
- Create: `frontend/src/lib/components/settings/SettingsPage.svelte`

- [ ] **Step 1: Create SettingsSection wrapper**

Create `frontend/src/lib/components/settings/SettingsSection.svelte`:

```svelte
<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title: string;
    children: Snippet;
  }

  let { title, children }: Props = $props();
</script>

<section class="settings-section">
  <h2 class="section-title">{title}</h2>
  <div class="section-body">
    {@render children()}
  </div>
</section>

<style>
  .settings-section {
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
  }

  .section-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-muted);
  }

  .section-body {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
</style>
```

- [ ] **Step 2: Create RepoSettings component**

Create `frontend/src/lib/components/settings/RepoSettings.svelte`:

```svelte
<script lang="ts">
  import type { ConfigRepo } from "../../api/types.js";
  import { addRepo, removeRepo, getSettings } from "../../api/client.js";

  interface Props {
    repos: ConfigRepo[];
    onUpdate: (repos: ConfigRepo[]) => void;
  }

  let { repos, onUpdate }: Props = $props();

  let inputValue = $state("");
  let adding = $state(false);
  let addError = $state<string | null>(null);
  let confirmingRemove = $state<string | null>(null);
  let removeError = $state<string | null>(null);

  async function handleAdd(): Promise<void> {
    const trimmed = inputValue.trim();
    if (!trimmed) return;

    const parts = trimmed.split("/");
    if (parts.length !== 2 || !parts[0] || !parts[1]) {
      addError = "Format: owner/name";
      return;
    }

    adding = true;
    addError = null;
    try {
      await addRepo(parts[0], parts[1]);
      inputValue = "";
      const settings = await getSettings();
      onUpdate(settings.repos);
    } catch (err) {
      addError = err instanceof Error ? err.message : String(err);
    } finally {
      adding = false;
    }
  }

  async function handleRemove(
    owner: string, name: string,
  ): Promise<void> {
    removeError = null;
    try {
      await removeRepo(owner, name);
      confirmingRemove = null;
      const settings = await getSettings();
      onUpdate(settings.repos);
    } catch (err) {
      removeError = err instanceof Error ? err.message : String(err);
    }
  }

  function handleInputKeydown(e: KeyboardEvent): void {
    if (e.key === "Enter") {
      e.preventDefault();
      void handleAdd();
    }
  }
</script>

<div class="repo-list">
  {#each repos as repo (`${repo.owner}/${repo.name}`)}
    {@const key = `${repo.owner}/${repo.name}`}
    <div class="repo-row">
      <span class="repo-name">{repo.owner}/{repo.name}</span>
      {#if confirmingRemove === key}
        <span class="confirm-prompt">
          Remove?
          <button
            class="confirm-btn confirm-yes"
            onclick={() => void handleRemove(repo.owner, repo.name)}
          >Yes</button>
          <button
            class="confirm-btn confirm-no"
            onclick={() => { confirmingRemove = null; removeError = null; }}
          >No</button>
        </span>
      {:else}
        <button
          class="remove-btn"
          disabled={repos.length <= 1}
          title={repos.length <= 1 ? "Cannot remove the last repository" : `Remove ${key}`}
          onclick={() => { confirmingRemove = key; removeError = null; }}
        >&times;</button>
      {/if}
    </div>
  {/each}
</div>

{#if removeError}
  <div class="error-msg">{removeError}</div>
{/if}

<div class="add-form">
  <input
    class="add-input"
    type="text"
    placeholder="owner/name"
    bind:value={inputValue}
    onkeydown={handleInputKeydown}
    disabled={adding}
  />
  <button
    class="add-btn"
    onclick={() => void handleAdd()}
    disabled={adding || !inputValue.trim()}
  >
    {adding ? "Adding..." : "Add"}
  </button>
</div>

{#if addError}
  <div class="error-msg">{addError}</div>
{/if}

<style>
  .repo-list {
    display: flex;
    flex-direction: column;
  }

  .repo-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 0;
    border-bottom: 1px solid var(--border-muted);
  }

  .repo-row:last-child {
    border-bottom: none;
  }

  .repo-name {
    font-size: 13px;
    color: var(--text-primary);
    font-weight: 500;
  }

  .remove-btn {
    font-size: 16px;
    color: var(--text-muted);
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    line-height: 1;
    transition: color 0.1s, background 0.1s;
  }

  .remove-btn:hover:not(:disabled) {
    color: var(--accent-red);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
  }

  .remove-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .confirm-prompt {
    font-size: 12px;
    color: var(--text-secondary);
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .confirm-btn {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: var(--radius-sm);
  }

  .confirm-yes {
    color: var(--accent-red);
    border: 1px solid var(--accent-red);
  }

  .confirm-yes:hover {
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
  }

  .confirm-no {
    color: var(--text-muted);
    border: 1px solid var(--border-muted);
  }

  .confirm-no:hover {
    background: var(--bg-surface-hover);
  }

  .add-form {
    display: flex;
    gap: 8px;
  }

  .add-input {
    flex: 1;
    font-size: 13px;
    padding: 6px 10px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
  }

  .add-input:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .add-btn {
    padding: 6px 14px;
    font-size: 13px;
    font-weight: 500;
    color: white;
    background: var(--accent-blue);
    border-radius: var(--radius-sm);
    transition: opacity 0.12s;
  }

  .add-btn:hover:not(:disabled) {
    opacity: 0.9;
  }

  .add-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .error-msg {
    font-size: 12px;
    color: var(--accent-red);
    padding: 4px 0;
  }
</style>
```

- [ ] **Step 3: Create ActivitySettings component**

Create `frontend/src/lib/components/settings/ActivitySettings.svelte`:

```svelte
<script lang="ts">
  import type { ActivitySettings as ActivitySettingsType } from "../../api/types.js";
  import { updateSettings } from "../../api/client.js";

  interface Props {
    activity: ActivitySettingsType;
    onUpdate: (activity: ActivitySettingsType) => void;
  }

  let { activity, onUpdate }: Props = $props();

  const TIME_RANGES: { value: ActivitySettingsType["time_range"]; label: string }[] = [
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
    { value: "90d", label: "90d" },
  ];

  async function save(
    updated: ActivitySettingsType,
  ): Promise<void> {
    try {
      const settings = await updateSettings({ activity: updated });
      onUpdate(settings.activity);
    } catch (err) {
      console.warn("Failed to save activity settings:", err);
    }
  }

  function setViewMode(
    mode: ActivitySettingsType["view_mode"],
  ): void {
    const updated = { ...activity, view_mode: mode };
    onUpdate(updated);
    void save(updated);
  }

  function setTimeRange(
    range_: ActivitySettingsType["time_range"],
  ): void {
    const updated = { ...activity, time_range: range_ };
    onUpdate(updated);
    void save(updated);
  }

  function toggleHideClosed(): void {
    const updated = {
      ...activity, hide_closed: !activity.hide_closed,
    };
    onUpdate(updated);
    void save(updated);
  }

  function toggleHideBots(): void {
    const updated = {
      ...activity, hide_bots: !activity.hide_bots,
    };
    onUpdate(updated);
    void save(updated);
  }
</script>

<div class="setting-row">
  <label class="setting-label">Default view mode</label>
  <div class="segmented-control">
    <button
      class="seg-btn"
      class:active={activity.view_mode === "flat"}
      onclick={() => setViewMode("flat")}
    >Flat</button>
    <button
      class="seg-btn"
      class:active={activity.view_mode === "threaded"}
      onclick={() => setViewMode("threaded")}
    >Threaded</button>
  </div>
</div>

<div class="setting-row">
  <label class="setting-label">Default time range</label>
  <div class="segmented-control">
    {#each TIME_RANGES as r}
      <button
        class="seg-btn"
        class:active={activity.time_range === r.value}
        onclick={() => setTimeRange(r.value)}
      >{r.label}</button>
    {/each}
  </div>
</div>

<div class="setting-row">
  <label class="setting-label">Hide closed/merged</label>
  <button
    class="toggle-btn"
    class:toggle-on={activity.hide_closed}
    onclick={toggleHideClosed}
  >
    <span class="toggle-track">
      <span class="toggle-thumb"></span>
    </span>
  </button>
</div>

<div class="setting-row">
  <label class="setting-label">Hide bots</label>
  <button
    class="toggle-btn"
    class:toggle-on={activity.hide_bots}
    onclick={toggleHideBots}
  >
    <span class="toggle-track">
      <span class="toggle-thumb"></span>
    </span>
  </button>
</div>

<style>
  .setting-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 32px;
  }

  .setting-label {
    font-size: 13px;
    color: var(--text-secondary);
  }

  .segmented-control {
    display: flex;
    align-items: center;
    gap: 1px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    padding: 2px;
  }

  .seg-btn {
    padding: 4px 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    border-radius: calc(var(--radius-sm) - 1px);
    transition: background 0.12s, color 0.12s;
  }

  .seg-btn.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: var(--shadow-sm);
  }

  .seg-btn:hover:not(.active) {
    color: var(--text-secondary);
  }

  .toggle-btn {
    cursor: pointer;
    padding: 0;
    background: none;
  }

  .toggle-track {
    display: block;
    width: 36px;
    height: 20px;
    border-radius: 10px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    position: relative;
    transition: background 0.15s, border-color 0.15s;
  }

  .toggle-on .toggle-track {
    background: var(--accent-blue);
    border-color: var(--accent-blue);
  }

  .toggle-thumb {
    display: block;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: white;
    position: absolute;
    top: 2px;
    left: 2px;
    transition: transform 0.15s;
    box-shadow: var(--shadow-sm);
  }

  .toggle-on .toggle-thumb {
    transform: translateX(16px);
  }
</style>
```

- [ ] **Step 4: Create SettingsPage component**

Create `frontend/src/lib/components/settings/SettingsPage.svelte`:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import type { Settings } from "../../api/types.js";
  import { getSettings } from "../../api/client.js";
  import SettingsSection from "./SettingsSection.svelte";
  import RepoSettings from "./RepoSettings.svelte";
  import ActivitySettings from "./ActivitySettings.svelte";

  let settings = $state<Settings | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  onMount(() => {
    void loadSettings();
  });

  async function loadSettings(): Promise<void> {
    loading = true;
    error = null;
    try {
      settings = await getSettings();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }
</script>

<div class="settings-page">
  {#if loading}
    <p class="state-msg">Loading settings...</p>
  {:else if error}
    <p class="state-msg state-error">Error: {error}</p>
  {:else if settings}
    <h1 class="page-title">Settings</h1>

    <SettingsSection title="Repositories">
      <RepoSettings
        repos={settings.repos}
        onUpdate={(repos) => { settings = { ...settings!, repos }; }}
      />
    </SettingsSection>

    <SettingsSection title="Activity feed defaults">
      <ActivitySettings
        activity={settings.activity}
        onUpdate={(activity) => { settings = { ...settings!, activity }; }}
      />
    </SettingsSection>
  {/if}
</div>

<style>
  .settings-page {
    max-width: 640px;
    margin: 0 auto;
    padding: 24px 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    overflow-y: auto;
    height: 100%;
  }

  .page-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }

  .state-msg {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }

  .state-error {
    color: var(--accent-red);
  }
</style>
```

- [ ] **Step 5: Verify frontend builds**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project/frontend && bun run build`

Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/lib/components/settings/
git commit -m "Add settings page with repo management and activity defaults"
```

---

### Task 9: Frontend — App.svelte integration (settings hydration, route, appReady)

**Files:**
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Add settings hydration and settings route**

In `frontend/src/App.svelte`, add the imports:

```typescript
import SettingsPage from "./lib/components/settings/SettingsPage.svelte";
import { getSettings } from "./lib/api/client.js";
import { hydrateActivityDefaults } from "./lib/stores/activity.svelte.js";
```

Add `appReady` state:

```typescript
let appReady = $state(false);
```

Replace the existing `$effect` that calls `startPolling`, `loadPulls`, `loadIssues` with one that also hydrates settings:

```typescript
$effect(() => {
  void (async () => {
    try {
      const settings = await getSettings();
      hydrateActivityDefaults(settings.activity);
    } catch (err) {
      console.warn("Failed to load settings, using defaults:", err);
    }
    appReady = true;
    startPolling();
    void loadPulls();
    void loadIssues();
  })();
});
```

Wrap the main content with an appReady gate. Change the template from:

```svelte
<main class="app-main">
  {#if getPage() === "activity"}
```

To:

```svelte
<main class="app-main">
  {#if !appReady}
    <div class="loading-state">Loading...</div>
  {:else if getPage() === "settings"}
    <SettingsPage />
  {:else if getPage() === "activity"}
```

Add `.loading-state` style:

```css
.loading-state {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
  color: var(--text-muted);
  font-size: 13px;
}
```

- [ ] **Step 2: Verify frontend builds**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project/frontend && bun run build`

Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/App.svelte
git commit -m "Add settings hydration gate and settings route to App.svelte"
```

---

### Task 10: Frontend — Header gear icon and sidebar add-repo links

**Files:**
- Modify: `frontend/src/lib/components/layout/AppHeader.svelte`
- Modify: `frontend/src/lib/components/sidebar/PullList.svelte`
- Modify: `frontend/src/lib/components/sidebar/IssueList.svelte`

- [ ] **Step 1: Add gear icon to header**

In `frontend/src/lib/components/layout/AppHeader.svelte`, in the `.header-right` div, add a settings button after the theme toggle:

```svelte
<button
  class="action-btn icon-btn"
  class:active={getPage() === "settings"}
  onclick={() => navigate("/settings")}
  title="Settings"
>
  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
    <path d="M8 4.754a3.246 3.246 0 100 6.492 3.246 3.246 0 000-6.492zM5.754 8a2.246 2.246 0 114.492 0 2.246 2.246 0 01-4.492 0z"/>
    <path d="M9.796 1.343c-.527-1.79-3.065-1.79-3.592 0l-.094.319a.873.873 0 01-1.255.52l-.292-.16c-1.64-.892-3.433.902-2.54 2.541l.159.292a.873.873 0 01-.52 1.255l-.319.094c-1.79.527-1.79 3.065 0 3.592l.319.094a.873.873 0 01.52 1.255l-.16.292c-.892 1.64.901 3.434 2.541 2.54l.292-.159a.873.873 0 011.255.52l.094.319c.527 1.79 3.065 1.79 3.592 0l.094-.319a.873.873 0 011.255-.52l.292.16c1.64.893 3.434-.902 2.54-2.541l-.159-.292a.873.873 0 01.52-1.255l.319-.094c1.79-.527 1.79-3.065 0-3.592l-.319-.094a.873.873 0 01-.52-1.255l.16-.292c.893-1.64-.902-3.433-2.541-2.54l-.292.159a.873.873 0 01-1.255-.52l-.094-.319zm-2.633.283c.246-.835 1.428-.835 1.674 0l.094.319a1.873 1.873 0 002.693 1.115l.291-.16c.764-.415 1.6.42 1.184 1.185l-.159.292a1.873 1.873 0 001.116 2.692l.318.094c.835.246.835 1.428 0 1.674l-.319.094a1.873 1.873 0 00-1.115 2.693l.16.291c.415.764-.421 1.6-1.185 1.184l-.291-.159a1.873 1.873 0 00-2.693 1.116l-.094.318c-.246.835-1.428.835-1.674 0l-.094-.319a1.873 1.873 0 00-2.692-1.115l-.292.16c-.764.415-1.6-.421-1.184-1.185l.159-.291A1.873 1.873 0 001.945 8.93l-.319-.094c-.835-.246-.835-1.428 0-1.674l.319-.094A1.873 1.873 0 003.06 4.377l-.16-.292c-.415-.764.42-1.6 1.185-1.184l.292.159a1.873 1.873 0 002.692-1.115l.094-.319z"/>
  </svg>
</button>
```

- [ ] **Step 2: Add "Add repo" link to PullList sidebar**

In `frontend/src/lib/components/sidebar/PullList.svelte`, add the import:

```typescript
import { navigate } from "../../stores/router.svelte.ts";
```

After the closing `</div>` of `.list-body` (before the closing `</div>` of `.pull-list`), add:

```svelte
<div class="sidebar-footer">
  <button class="add-repo-link" onclick={() => navigate("/settings")}>
    + Add repository
  </button>
</div>
```

Add the styles:

```css
.sidebar-footer {
  padding: 8px 12px;
  border-top: 1px solid var(--border-muted);
  flex-shrink: 0;
}

.add-repo-link {
  font-size: 12px;
  color: var(--text-muted);
  cursor: pointer;
  transition: color 0.1s;
  padding: 0;
}

.add-repo-link:hover {
  color: var(--accent-blue);
}
```

- [ ] **Step 3: Add "Add repo" link to IssueList sidebar**

Same change in `frontend/src/lib/components/sidebar/IssueList.svelte`. Add the import:

```typescript
import { navigate } from "../../stores/router.svelte.ts";
```

After `.list-body`, before the closing `.issue-list`:

```svelte
<div class="sidebar-footer">
  <button class="add-repo-link" onclick={() => navigate("/settings")}>
    + Add repository
  </button>
</div>
```

Add same styles as PullList.

- [ ] **Step 4: Verify frontend builds**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project/frontend && bun run build`

Expected: build succeeds.

- [ ] **Step 5: Commit**

```bash
cd /Users/wesm/.superset/worktrees/middleman/feat/add-project
git add frontend/src/lib/components/layout/AppHeader.svelte frontend/src/lib/components/sidebar/PullList.svelte frontend/src/lib/components/sidebar/IssueList.svelte
git commit -m "Add settings gear icon to header and add-repo links to sidebars"
```

---

### Task 11: Full integration test

- [ ] **Step 1: Run all Go tests**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && go test ./... -short`

Expected: all tests pass.

- [ ] **Step 2: Run Go linter**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && golangci-lint run`

Expected: no errors.

- [ ] **Step 3: Build full binary with embedded frontend**

Run: `cd /Users/wesm/.superset/worktrees/middleman/feat/add-project && make build`

Expected: build succeeds.

- [ ] **Step 4: Fix any issues found, commit**

If any issues found, fix them and commit with a descriptive message.
