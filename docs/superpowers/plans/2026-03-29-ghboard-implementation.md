# ghboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local-first GitHub PR monitoring dashboard with Go+SQLite backend and Svelte frontend.

**Architecture:** Go HTTP server syncs PR data from GitHub into SQLite on a timer, serves a Svelte SPA via go:embed. The frontend reads from local cache for fast views, writes go through the server to GitHub API. Local Kanban state is stored only in SQLite.

**Tech Stack:** Go 1.25, SQLite (mattn/go-sqlite3), google/go-github/v84, BurntSushi/toml, Svelte 5.55, Vite 8, TypeScript 5.9

**Spec:** `docs/superpowers/specs/2026-03-29-ghboard-design.md`

---

## File Structure

```text
cmd/ghboard/
  main.go                         # CLI entry point, flag parsing, server startup
internal/
  config/
    config.go                     # TOML config loading and validation
    config_test.go
  db/
    db.go                         # SQLite connection, WAL setup, schema init
    db_test.go
    schema.sql                    # Table definitions and indexes
    queries.go                    # All query functions
    queries_test.go
    types.go                      # DB model types
  github/
    client.go                     # go-github wrapper, interface definition
    client_test.go
    normalize.go                  # Convert GitHub API types to DB types
    normalize_test.go
    sync.go                       # Sync engine (periodic + on-demand)
    sync_test.go
  server/
    server.go                     # HTTP server, routing, middleware
    handlers.go                   # API endpoint handlers
    handlers_test.go
  web/
    embed.go                      # go:embed compiled frontend
frontend/
  index.html
  package.json
  tsconfig.json
  vite.config.ts
  src/
    main.ts
    app.css                       # Design tokens, reset, theme
    App.svelte                    # Root component, view routing
    lib/
      api/
        client.ts                 # Fetch wrapper for Go API
        types.ts                  # TypeScript types matching backend
      stores/
        pulls.svelte.ts           # PR list, filtering, selection
        detail.svelte.ts          # Selected PR detail + events
        sync.svelte.ts            # Sync status polling
        router.svelte.ts          # View state (list vs board)
      components/
        layout/
          AppHeader.svelte        # Top bar: logo, view toggle, sync button, theme
        sidebar/
          PullList.svelte         # PR sidebar with repo grouping
          PullItem.svelte         # Single PR row in sidebar
        detail/
          PullDetail.svelte       # PR detail panel
          EventTimeline.svelte    # Activity timeline
          CommentBox.svelte       # Comment input + submit
        kanban/
          KanbanBoard.svelte      # Board layout with columns
          KanbanColumn.svelte     # Single status column
          KanbanCard.svelte       # PR card in board view
Makefile
go.mod
.gitignore
.golangci.yml
config.example.toml
```

---

## Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `.golangci.yml`
- Create: `config.example.toml`
- Create: `cmd/ghboard/main.go` (minimal)
- Create: `internal/web/embed.go`
- Create: `frontend/package.json`
- Create: `frontend/tsconfig.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/index.html`
- Create: `frontend/src/main.ts`
- Create: `frontend/src/App.svelte` (placeholder)

No tests in this task — it's pure scaffolding verified by `go build` and `npm install`.

- [ ] **Step 1: Initialize Go module and install dependencies**

```bash
cd /Users/wesm/code/ghboard
go mod init github.com/wesm/ghboard
go get github.com/google/go-github/v84/github
go get github.com/BurntSushi/toml@v1.6.0
go get github.com/mattn/go-sqlite3@v1.14.37
go get golang.org/x/oauth2
go mod tidy
```

- [ ] **Step 2: Create .gitignore**

```gitignore
# Build
ghboard
dist/
internal/web/dist/

# Frontend
frontend/node_modules/
frontend/dist/

# IDE
.idea/
.vscode/
*.swp

# macOS
.DS_Store

# Data
*.db

# Env
.env

# Brainstorm artifacts
.superpowers/
```

- [ ] **Step 3: Create .golangci.yml**

```yaml
version: "2"
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
  settings:
    govet:
      enable-all: true
      disable:
        - shadow
        - fieldalignment
```

- [ ] **Step 4: Create config.example.toml**

```toml
# ghboard configuration

# How often to sync from GitHub (Go duration string)
sync_interval = "5m"

# Environment variable containing your GitHub personal access token
# Required scopes: repo (for private repos) or public_repo (for public only)
github_token_env = "GITHUB_TOKEN"

# Server bind address
host = "127.0.0.1"
port = 8090

# Repositories to track
[[repos]]
owner = "apache"
name = "arrow"

[[repos]]
owner = "ibis-project"
name = "ibis"
```

- [ ] **Step 5: Create internal/web/embed.go**

```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the compiled frontend filesystem, rooted
// inside the embedded dist directory.
func Assets() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
```

- [ ] **Step 6: Create cmd/ghboard/main.go (minimal entrypoint)**

```go
package main

import (
	"fmt"
	"os"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("ghboard %s (%s) built %s\n", version, commit, buildDate)
		os.Exit(0)
	}
	fmt.Println("ghboard server — not yet implemented")
}
```

- [ ] **Step 7: Create Makefile**

```makefile
.DEFAULT_GOAL := help

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

LDFLAGS_RELEASE := $(LDFLAGS) -s -w

.PHONY: build build-release install frontend frontend-dev dev test test-short vet lint tidy clean ensure-embed-dir help

ensure-embed-dir:
	@mkdir -p internal/web/dist
	@test -n "$$(ls internal/web/dist/ 2>/dev/null)" \
		|| echo ok > internal/web/dist/stub.html

build: frontend
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o ghboard ./cmd/ghboard
	@chmod +x ghboard

build-release: frontend
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS_RELEASE)" -trimpath -o ghboard ./cmd/ghboard
	@chmod +x ghboard

install: build-release
	@if [ -d "$(HOME)/.local/bin" ]; then \
		echo "Installing to ~/.local/bin/ghboard"; \
		cp ghboard "$(HOME)/.local/bin/ghboard"; \
	else \
		INSTALL_DIR="$${GOBIN:-$$(go env GOBIN)}"; \
		if [ -z "$$INSTALL_DIR" ]; then \
			INSTALL_DIR="$$(go env GOPATH | cut -d: -f1)/bin"; \
		fi; \
		mkdir -p "$$INSTALL_DIR"; \
		echo "Installing to $$INSTALL_DIR/ghboard"; \
		cp ghboard "$$INSTALL_DIR/ghboard"; \
	fi

frontend:
	cd frontend && npm install && npm run build
	rm -rf internal/web/dist
	cp -r frontend/dist internal/web/dist

frontend-dev:
	cd frontend && npm run dev

dev: ensure-embed-dir
	go run -ldflags="$(LDFLAGS)" ./cmd/ghboard $(ARGS)

test: ensure-embed-dir
	CGO_ENABLED=1 go test ./... -v -count=1

test-short: ensure-embed-dir
	CGO_ENABLED=1 go test ./... -short -count=1

vet: ensure-embed-dir
	go vet ./...

lint: ensure-embed-dir
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found." >&2; \
		exit 1; \
	fi
	golangci-lint run --fix ./...

tidy:
	go mod tidy

clean:
	rm -f ghboard
	rm -rf internal/web/dist dist/

help:
	@echo "ghboard build targets:"
	@echo ""
	@echo "  build          Build with embedded frontend"
	@echo "  build-release  Release build (optimized)"
	@echo "  install        Build and install to PATH"
	@echo ""
	@echo "  dev            Run Go server (use with frontend-dev)"
	@echo "  frontend       Build frontend SPA"
	@echo "  frontend-dev   Run Vite dev server"
	@echo ""
	@echo "  test           Run all tests"
	@echo "  test-short     Run fast tests only"
	@echo "  vet            Run go vet"
	@echo "  lint           Run golangci-lint"
	@echo "  tidy           Tidy go.mod"
	@echo "  clean          Remove build artifacts"
```

- [ ] **Step 8: Initialize frontend**

```bash
cd /Users/wesm/code/ghboard
mkdir -p frontend/src
```

Create `frontend/package.json`:

```json
{
  "name": "ghboard-frontend",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "check": "svelte-check --tsconfig ./tsconfig.json"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "7.0.0",
    "@tsconfig/svelte": "5.0.8",
    "svelte": "5.55.0",
    "svelte-check": "4.4.5",
    "typescript": "5.9.3",
    "vite": "8.0.3"
  }
}
```

Create `frontend/tsconfig.json`:

```json
{
  "extends": "@tsconfig/svelte/tsconfig.json",
  "compilerOptions": {
    "target": "ESNext",
    "useDefineForClassFields": true,
    "module": "ESNext",
    "resolveJsonModule": true,
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noImplicitOverride": true,
    "verbatimModuleSyntax": true
  },
  "include": ["src/**/*.ts", "src/**/*.svelte"]
}
```

Create `frontend/vite.config.ts`:

```typescript
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  base: "/",
  plugins: [svelte()],
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8090",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
```

Create `frontend/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>ghboard</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

Create `frontend/src/main.ts`:

```typescript
import App from "./App.svelte";
import { mount } from "svelte";
import "./app.css";

mount(App, { target: document.getElementById("app")! });
```

Create `frontend/src/App.svelte`:

```svelte
<p>ghboard</p>
```

Create `frontend/src/app.css` (empty for now — populated in Task 9):

```css
/* Design tokens populated in Task 9 */
```

- [ ] **Step 9: Install frontend dependencies**

```bash
cd /Users/wesm/code/ghboard/frontend && npm install
```

- [ ] **Step 10: Verify Go builds**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go build ./cmd/ghboard
```

Expected: builds successfully, produces `ghboard` binary.

- [ ] **Step 11: Verify frontend builds**

```bash
cd /Users/wesm/code/ghboard/frontend && npm run build
```

Expected: produces `frontend/dist/` with index.html and JS bundle.

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "Scaffold project: Go backend, Svelte frontend, Makefile"
```

---

## Task 2: Config Package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Create types and loading in config.go**

```go
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Repo struct {
	Owner string `toml:"owner"`
	Name  string `toml:"name"`
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type Config struct {
	SyncInterval   string `toml:"sync_interval"`
	GitHubTokenEnv string `toml:"github_token_env"`
	Host           string `toml:"host"`
	Port           int    `toml:"port"`
	DataDir        string `toml:"data_dir"`
	Repos          []Repo `toml:"repos"`
}

func DefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "ghboard", "config.toml")
}

func DefaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "ghboard")
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		SyncInterval:   "5m",
		GitHubTokenEnv: "GITHUB_TOKEN",
		Host:           "127.0.0.1",
		Port:           8090,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}

	return cfg, cfg.Validate()
}

func (c *Config) Validate() error {
	if len(c.Repos) == 0 {
		return errors.New("config: at least one [[repos]] entry required")
	}

	for i, r := range c.Repos {
		if r.Owner == "" || r.Name == "" {
			return fmt.Errorf(
				"config: repos[%d] must have owner and name", i,
			)
		}
	}

	if _, err := time.ParseDuration(c.SyncInterval); err != nil {
		return fmt.Errorf(
			"config: invalid sync_interval %q: %w",
			c.SyncInterval, err,
		)
	}

	if ip := net.ParseIP(c.Host); ip == nil {
		return fmt.Errorf("config: invalid host %q", c.Host)
	} else if !ip.IsLoopback() {
		return fmt.Errorf(
			"config: host %q is not loopback; "+
				"ghboard v1 only supports loopback addresses",
			c.Host,
		)
	}

	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: invalid port %d", c.Port)
	}

	return nil
}

func (c *Config) SyncDuration() time.Duration {
	d, _ := time.ParseDuration(c.SyncInterval)
	return d
}

func (c *Config) GitHubToken() string {
	return os.Getenv(c.GitHubTokenEnv)
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "ghboard.db")
}
```

- [ ] **Step 2: Write tests in config_test.go**

```go
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
	cfg := &Config{DataDir: "/tmp/ghboard-test"}
	expected := "/tmp/ghboard-test/ghboard.db"
	if got := cfg.DBPath(); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go test ./internal/config/... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 4: Run lint**

```bash
cd /Users/wesm/code/ghboard && make vet
```

Expected: no issues.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "Add config package: TOML loading, validation, defaults"
```

---

## Task 3: Database Schema & Connection

**Files:**
- Create: `internal/db/types.go`
- Create: `internal/db/schema.sql`
- Create: `internal/db/db.go`
- Create: `internal/db/db_test.go`

- [ ] **Step 1: Create types.go with model types**

```go
package db

import "time"

type Repo struct {
	ID                  int64
	Owner               string
	Name                string
	LastSyncStartedAt   *time.Time
	LastSyncCompletedAt *time.Time
	LastSyncError       string
	CreatedAt           time.Time
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type PullRequest struct {
	ID             int64
	RepoID         int64
	GitHubID       int64
	Number         int
	URL            string
	Title          string
	Author         string
	State          string // open, closed, merged
	IsDraft        bool
	Body           string
	HeadBranch     string
	BaseBranch     string
	Additions      int
	Deletions      int
	CommentCount   int
	ReviewDecision string // approved, changes_requested, review_required, none
	CIStatus       string // pending, success, failure, or empty
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time
	MergedAt       *time.Time
	ClosedAt       *time.Time

	// Joined from kanban_state when queried
	KanbanStatus string
}

type PREvent struct {
	ID           int64
	PRID         int64
	GitHubID     *int64
	EventType    string // issue_comment, review, review_comment, commit, state_change
	Author       string
	Summary      string
	Body         string
	MetadataJSON string
	CreatedAt    time.Time
	DedupeKey    string
}

type KanbanState struct {
	PRID      int64
	Status    string // new, reviewing, waiting, awaiting_merge
	UpdatedAt time.Time
}

type ListPullsOpts struct {
	RepoOwner   string
	RepoName    string
	State       string // open, closed, merged — default open
	KanbanState string
	Search      string
	Limit       int
	Offset      int
}
```

- [ ] **Step 2: Create schema.sql**

```sql
CREATE TABLE IF NOT EXISTS repos (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    owner                 TEXT NOT NULL,
    name                  TEXT NOT NULL,
    last_sync_started_at  DATETIME,
    last_sync_completed_at DATETIME,
    last_sync_error       TEXT DEFAULT '',
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(owner, name)
);

CREATE TABLE IF NOT EXISTS pull_requests (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id          INTEGER NOT NULL REFERENCES repos(id),
    github_id        INTEGER NOT NULL,
    number           INTEGER NOT NULL,
    url              TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    author           TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT 'open',
    is_draft         INTEGER NOT NULL DEFAULT 0,
    body             TEXT NOT NULL DEFAULT '',
    head_branch      TEXT NOT NULL DEFAULT '',
    base_branch      TEXT NOT NULL DEFAULT '',
    additions        INTEGER NOT NULL DEFAULT 0,
    deletions        INTEGER NOT NULL DEFAULT 0,
    comment_count    INTEGER NOT NULL DEFAULT 0,
    review_decision  TEXT NOT NULL DEFAULT '',
    ci_status        TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    last_activity_at DATETIME NOT NULL,
    merged_at        DATETIME,
    closed_at        DATETIME,
    UNIQUE(repo_id, number),
    UNIQUE(github_id)
);

CREATE TABLE IF NOT EXISTS pr_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id         INTEGER NOT NULL REFERENCES pull_requests(id),
    github_id     INTEGER,
    event_type    TEXT NOT NULL,
    author        TEXT NOT NULL DEFAULT '',
    summary       TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    dedupe_key    TEXT NOT NULL,
    UNIQUE(dedupe_key)
);

CREATE TABLE IF NOT EXISTS kanban_state (
    pr_id      INTEGER PRIMARY KEY REFERENCES pull_requests(id),
    status     TEXT NOT NULL DEFAULT 'new',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Hot-path indexes
CREATE INDEX IF NOT EXISTS idx_pr_repo_state_activity
    ON pull_requests(repo_id, state, last_activity_at DESC);

CREATE INDEX IF NOT EXISTS idx_pr_state_activity
    ON pull_requests(state, last_activity_at DESC);

CREATE INDEX IF NOT EXISTS idx_kanban_status
    ON kanban_state(status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_events_pr_created
    ON pr_events(pr_id, created_at DESC);
```

- [ ] **Step 3: Create db.go with connection setup**

```go
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// DB wraps a SQLite database connection for ghboard.
type DB struct {
	rw   *sql.DB // read-write connection
	ro   *sql.DB // read-only connection pool
}

// Open creates or opens the SQLite database at the given path
// and applies the schema.
func Open(path string) (*DB, error) {
	rw, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	rw.SetMaxOpenConns(1)

	ro, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&mode=ro")
	if err != nil {
		rw.Close()
		return nil, fmt.Errorf("open db read-only: %w", err)
	}
	ro.SetMaxOpenConns(4)

	d := &DB{rw: rw, ro: ro}
	if err := d.init(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) init() error {
	// Enable WAL mode
	if _, err := d.rw.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}
	// Apply schema
	if _, err := d.rw.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Close closes both database connections.
func (d *DB) Close() error {
	d.ro.Close()
	return d.rw.Close()
}

// ReadDB returns the read-only connection pool for query use.
func (d *DB) ReadDB() *sql.DB {
	return d.ro
}

// WriteDB returns the single read-write connection for mutations.
func (d *DB) WriteDB() *sql.DB {
	return d.rw
}

// Tx runs fn inside a write transaction.
func (d *DB) Tx(
	ctx context.Context,
	fn func(tx *sql.Tx) error,
) error {
	tx, err := d.rw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Write db_test.go**

```go
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

	// Verify tables exist by querying them
	tables := []string{
		"repos", "pull_requests", "pr_events", "kanban_state",
	}
	for _, tbl := range tables {
		var name string
		err := d.ReadDB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			tbl,
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

	// Open again — schema should apply without error
	d2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
}
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go test ./internal/db/... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/db/
git commit -m "Add database schema and connection management"
```

---

## Task 4: Database Queries

**Files:**
- Create: `internal/db/queries.go`
- Create: `internal/db/queries_test.go`

- [ ] **Step 1: Create queries.go**

```go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// --- Repos ---

// UpsertRepo inserts or returns the existing repo ID.
func (d *DB) UpsertRepo(
	ctx context.Context, owner, name string,
) (int64, error) {
	res, err := d.rw.ExecContext(ctx,
		`INSERT INTO repos (owner, name)
		 VALUES (?, ?)
		 ON CONFLICT(owner, name) DO NOTHING`,
		owner, name,
	)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// Row already existed, look it up
		err = d.ro.QueryRowContext(ctx,
			`SELECT id FROM repos WHERE owner = ? AND name = ?`,
			owner, name,
		).Scan(&id)
	}
	return id, err
}

// ListRepos returns all tracked repos.
func (d *DB) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT id, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, created_at
		 FROM repos ORDER BY owner, name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(
			&r.ID, &r.Owner, &r.Name,
			&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
			&r.LastSyncError, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// UpdateRepoSyncStarted marks a repo sync as started.
func (d *DB) UpdateRepoSyncStarted(
	ctx context.Context, id int64, t time.Time,
) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE repos SET last_sync_started_at = ? WHERE id = ?`,
		t, id,
	)
	return err
}

// UpdateRepoSyncCompleted marks a repo sync as completed.
func (d *DB) UpdateRepoSyncCompleted(
	ctx context.Context, id int64, t time.Time, syncErr string,
) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE repos
		 SET last_sync_completed_at = ?,
		     last_sync_error = ?
		 WHERE id = ?`,
		t, syncErr, id,
	)
	return err
}

// GetRepoByOwnerName looks up a repo by owner/name.
func (d *DB) GetRepoByOwnerName(
	ctx context.Context, owner, name string,
) (*Repo, error) {
	var r Repo
	err := d.ro.QueryRowContext(ctx,
		`SELECT id, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, created_at
		 FROM repos WHERE owner = ? AND name = ?`,
		owner, name,
	).Scan(
		&r.ID, &r.Owner, &r.Name,
		&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
		&r.LastSyncError, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

// --- Pull Requests ---

// UpsertPullRequest inserts or updates a pull request.
func (d *DB) UpsertPullRequest(
	ctx context.Context, pr *PullRequest,
) (int64, error) {
	res, err := d.rw.ExecContext(ctx,
		`INSERT INTO pull_requests (
			repo_id, github_id, number, url, title, author,
			state, is_draft, body, head_branch, base_branch,
			additions, deletions, comment_count,
			review_decision, ci_status,
			created_at, updated_at, last_activity_at,
			merged_at, closed_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
			title = excluded.title,
			author = excluded.author,
			state = excluded.state,
			is_draft = excluded.is_draft,
			body = excluded.body,
			head_branch = excluded.head_branch,
			base_branch = excluded.base_branch,
			additions = excluded.additions,
			deletions = excluded.deletions,
			comment_count = excluded.comment_count,
			review_decision = excluded.review_decision,
			ci_status = excluded.ci_status,
			updated_at = excluded.updated_at,
			last_activity_at = excluded.last_activity_at,
			merged_at = excluded.merged_at,
			closed_at = excluded.closed_at`,
		pr.RepoID, pr.GitHubID, pr.Number, pr.URL,
		pr.Title, pr.Author, pr.State, pr.IsDraft,
		pr.Body, pr.HeadBranch, pr.BaseBranch,
		pr.Additions, pr.Deletions, pr.CommentCount,
		pr.ReviewDecision, pr.CIStatus,
		pr.CreatedAt, pr.UpdatedAt, pr.LastActivityAt,
		pr.MergedAt, pr.ClosedAt,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		err = d.ro.QueryRowContext(ctx,
			`SELECT id FROM pull_requests
			 WHERE repo_id = ? AND number = ?`,
			pr.RepoID, pr.Number,
		).Scan(&id)
	}
	return id, err
}

// GetPullRequest returns a single PR by repo owner/name and number.
func (d *DB) GetPullRequest(
	ctx context.Context, owner, name string, number int,
) (*PullRequest, error) {
	var pr PullRequest
	var kanban sql.NullString
	err := d.ro.QueryRowContext(ctx,
		`SELECT p.id, p.repo_id, p.github_id, p.number, p.url,
		        p.title, p.author, p.state, p.is_draft,
		        p.body, p.head_branch, p.base_branch,
		        p.additions, p.deletions, p.comment_count,
		        p.review_decision, p.ci_status,
		        p.created_at, p.updated_at, p.last_activity_at,
		        p.merged_at, p.closed_at,
		        k.status
		 FROM pull_requests p
		 JOIN repos r ON r.id = p.repo_id
		 LEFT JOIN kanban_state k ON k.pr_id = p.id
		 WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(
		&pr.ID, &pr.RepoID, &pr.GitHubID, &pr.Number, &pr.URL,
		&pr.Title, &pr.Author, &pr.State, &pr.IsDraft,
		&pr.Body, &pr.HeadBranch, &pr.BaseBranch,
		&pr.Additions, &pr.Deletions, &pr.CommentCount,
		&pr.ReviewDecision, &pr.CIStatus,
		&pr.CreatedAt, &pr.UpdatedAt, &pr.LastActivityAt,
		&pr.MergedAt, &pr.ClosedAt,
		&kanban,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if kanban.Valid {
		pr.KanbanStatus = kanban.String
	}
	return &pr, err
}

// ListPullRequests returns PRs matching the given filters.
func (d *DB) ListPullRequests(
	ctx context.Context, opts ListPullsOpts,
) ([]PullRequest, error) {
	var where []string
	var args []any

	if opts.RepoOwner != "" && opts.RepoName != "" {
		where = append(where,
			"r.owner = ? AND r.name = ?")
		args = append(args, opts.RepoOwner, opts.RepoName)
	}

	state := opts.State
	if state == "" {
		state = "open"
	}
	where = append(where, "p.state = ?")
	args = append(args, state)

	if opts.KanbanState != "" {
		where = append(where, "k.status = ?")
		args = append(args, opts.KanbanState)
	}

	if opts.Search != "" {
		where = append(where,
			"(p.title LIKE ? OR p.author LIKE ?)")
		q := "%" + opts.Search + "%"
		args = append(args, q, q)
	}

	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(
		`SELECT p.id, p.repo_id, p.github_id, p.number, p.url,
		        p.title, p.author, p.state, p.is_draft,
		        p.body, p.head_branch, p.base_branch,
		        p.additions, p.deletions, p.comment_count,
		        p.review_decision, p.ci_status,
		        p.created_at, p.updated_at, p.last_activity_at,
		        p.merged_at, p.closed_at,
		        COALESCE(k.status, '') as kanban_status,
		        r.owner, r.name
		 FROM pull_requests p
		 JOIN repos r ON r.id = p.repo_id
		 LEFT JOIN kanban_state k ON k.pr_id = p.id
		 WHERE %s
		 ORDER BY p.last_activity_at DESC
		 LIMIT ? OFFSET ?`,
		strings.Join(where, " AND "),
	)
	args = append(args, limit, offset)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prs []PullRequest
	for rows.Next() {
		var pr PullRequest
		var repoOwner, repoName string
		if err := rows.Scan(
			&pr.ID, &pr.RepoID, &pr.GitHubID, &pr.Number, &pr.URL,
			&pr.Title, &pr.Author, &pr.State, &pr.IsDraft,
			&pr.Body, &pr.HeadBranch, &pr.BaseBranch,
			&pr.Additions, &pr.Deletions, &pr.CommentCount,
			&pr.ReviewDecision, &pr.CIStatus,
			&pr.CreatedAt, &pr.UpdatedAt, &pr.LastActivityAt,
			&pr.MergedAt, &pr.ClosedAt,
			&pr.KanbanStatus,
			&repoOwner, &repoName,
		); err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

// --- PR Events ---

// UpsertPREvents bulk-inserts events, skipping duplicates.
func (d *DB) UpsertPREvents(
	ctx context.Context, events []PREvent,
) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO pr_events (
				pr_id, github_id, event_type, author,
				summary, body, metadata_json, created_at,
				dedupe_key
			) VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(dedupe_key) DO NOTHING`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, e := range events {
			if _, err := stmt.ExecContext(ctx,
				e.PRID, e.GitHubID, e.EventType, e.Author,
				e.Summary, e.Body, e.MetadataJSON, e.CreatedAt,
				e.DedupeKey,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListPREvents returns events for a PR, newest first.
func (d *DB) ListPREvents(
	ctx context.Context, prID int64,
) ([]PREvent, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT id, pr_id, github_id, event_type, author,
		        summary, body, metadata_json, created_at,
		        dedupe_key
		 FROM pr_events
		 WHERE pr_id = ?
		 ORDER BY created_at DESC`,
		prID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []PREvent
	for rows.Next() {
		var e PREvent
		if err := rows.Scan(
			&e.ID, &e.PRID, &e.GitHubID, &e.EventType,
			&e.Author, &e.Summary, &e.Body, &e.MetadataJSON,
			&e.CreatedAt, &e.DedupeKey,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Kanban State ---

// EnsureKanbanState creates a "new" kanban entry if none exists.
func (d *DB) EnsureKanbanState(
	ctx context.Context, prID int64,
) error {
	_, err := d.rw.ExecContext(ctx,
		`INSERT INTO kanban_state (pr_id, status)
		 VALUES (?, 'new')
		 ON CONFLICT(pr_id) DO NOTHING`,
		prID,
	)
	return err
}

// SetKanbanState updates the kanban status for a PR.
func (d *DB) SetKanbanState(
	ctx context.Context, prID int64, status string,
) error {
	_, err := d.rw.ExecContext(ctx,
		`INSERT INTO kanban_state (pr_id, status, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(pr_id) DO UPDATE SET
		   status = excluded.status,
		   updated_at = excluded.updated_at`,
		prID, status,
	)
	return err
}

// GetKanbanState returns the kanban state for a PR, or nil.
func (d *DB) GetKanbanState(
	ctx context.Context, prID int64,
) (*KanbanState, error) {
	var ks KanbanState
	err := d.ro.QueryRowContext(ctx,
		`SELECT pr_id, status, updated_at
		 FROM kanban_state WHERE pr_id = ?`,
		prID,
	).Scan(&ks.PRID, &ks.Status, &ks.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &ks, err
}

// GetPRIDByRepoAndNumber looks up the internal PR id.
func (d *DB) GetPRIDByRepoAndNumber(
	ctx context.Context, owner, name string, number int,
) (int64, error) {
	var id int64
	err := d.ro.QueryRowContext(ctx,
		`SELECT p.id FROM pull_requests p
		 JOIN repos r ON r.id = p.repo_id
		 WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(&id)
	return id, err
}

// GetPreviouslyOpenPRNumbers returns PR numbers that were open
// in the DB but are not in the given set of still-open numbers.
func (d *DB) GetPreviouslyOpenPRNumbers(
	ctx context.Context, repoID int64, stillOpen map[int]bool,
) ([]int, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT number FROM pull_requests
		 WHERE repo_id = ? AND state = 'open'`,
		repoID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var closed []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if !stillOpen[n] {
			closed = append(closed, n)
		}
	}
	return closed, rows.Err()
}

// UpdatePRState sets the final state of a PR (closed or merged).
func (d *DB) UpdatePRState(
	ctx context.Context, repoID int64, number int,
	state string, mergedAt, closedAt *time.Time,
) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE pull_requests
		 SET state = ?, merged_at = ?, closed_at = ?
		 WHERE repo_id = ? AND number = ?`,
		state, mergedAt, closedAt, repoID, number,
	)
	return err
}
```

- [ ] **Step 2: Write queries_test.go**

```go
package db

import (
	"context"
	"testing"
	"time"
)

func TestUpsertAndListRepos(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id1, err := d.UpsertRepo(ctx, "apache", "arrow")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := d.UpsertRepo(ctx, "ibis-project", "ibis")
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatal("expected different IDs")
	}

	// Upsert same repo returns same ID
	id1b, err := d.UpsertRepo(ctx, "apache", "arrow")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id1b {
		t.Fatalf("expected same ID, got %d vs %d", id1, id1b)
	}

	repos, err := d.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
}

func TestUpsertAndGetPullRequest(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID, _ := d.UpsertRepo(ctx, "apache", "arrow")
	now := time.Now().UTC().Truncate(time.Second)

	pr := &PullRequest{
		RepoID:         repoID,
		GitHubID:       12345,
		Number:         42,
		URL:            "https://github.com/apache/arrow/pull/42",
		Title:          "Fix memory leak",
		Author:         "contributor",
		State:          "open",
		HeadBranch:     "fix-leak",
		BaseBranch:     "main",
		Additions:      10,
		Deletions:      3,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}

	id, err := d.UpsertPullRequest(ctx, pr)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	// Fetch it back
	got, err := d.GetPullRequest(ctx, "apache", "arrow", 42)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected PR, got nil")
	}
	if got.Title != "Fix memory leak" {
		t.Fatalf("expected title 'Fix memory leak', got %q", got.Title)
	}
	if got.Author != "contributor" {
		t.Fatalf("expected author 'contributor', got %q", got.Author)
	}

	// Update via upsert
	pr.Title = "Fix critical memory leak"
	_, err = d.UpsertPullRequest(ctx, pr)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetPullRequest(ctx, "apache", "arrow", 42)
	if got.Title != "Fix critical memory leak" {
		t.Fatalf("expected updated title, got %q", got.Title)
	}
}

func TestListPullRequests(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID, _ := d.UpsertRepo(ctx, "apache", "arrow")
	now := time.Now().UTC().Truncate(time.Second)

	for i := range 3 {
		pr := &PullRequest{
			RepoID:         repoID,
			GitHubID:       int64(100 + i),
			Number:         i + 1,
			Title:          "PR " + string(rune('A'+i)),
			Author:         "dev",
			State:          "open",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastActivityAt: now.Add(time.Duration(i) * time.Hour),
		}
		if _, err := d.UpsertPullRequest(ctx, pr); err != nil {
			t.Fatal(err)
		}
	}

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 {
		t.Fatalf("expected 3, got %d", len(prs))
	}
	// Should be sorted by last_activity_at DESC
	if prs[0].Number != 3 {
		t.Fatalf("expected PR 3 first, got %d", prs[0].Number)
	}
}

func TestKanbanState(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID, _ := d.UpsertRepo(ctx, "test", "repo")
	now := time.Now().UTC().Truncate(time.Second)
	prID, _ := d.UpsertPullRequest(ctx, &PullRequest{
		RepoID: repoID, GitHubID: 1, Number: 1,
		State: "open", CreatedAt: now, UpdatedAt: now,
		LastActivityAt: now,
	})

	// EnsureKanbanState creates "new"
	if err := d.EnsureKanbanState(ctx, prID); err != nil {
		t.Fatal(err)
	}
	ks, err := d.GetKanbanState(ctx, prID)
	if err != nil {
		t.Fatal(err)
	}
	if ks.Status != "new" {
		t.Fatalf("expected new, got %s", ks.Status)
	}

	// SetKanbanState updates
	if err := d.SetKanbanState(ctx, prID, "reviewing"); err != nil {
		t.Fatal(err)
	}
	ks, _ = d.GetKanbanState(ctx, prID)
	if ks.Status != "reviewing" {
		t.Fatalf("expected reviewing, got %s", ks.Status)
	}

	// EnsureKanbanState does not overwrite
	if err := d.EnsureKanbanState(ctx, prID); err != nil {
		t.Fatal(err)
	}
	ks, _ = d.GetKanbanState(ctx, prID)
	if ks.Status != "reviewing" {
		t.Fatalf("expected reviewing preserved, got %s", ks.Status)
	}
}

func TestPREvents(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID, _ := d.UpsertRepo(ctx, "test", "repo")
	now := time.Now().UTC().Truncate(time.Second)
	prID, _ := d.UpsertPullRequest(ctx, &PullRequest{
		RepoID: repoID, GitHubID: 1, Number: 1,
		State: "open", CreatedAt: now, UpdatedAt: now,
		LastActivityAt: now,
	})

	events := []PREvent{
		{
			PRID: prID, EventType: "issue_comment",
			Author: "alice", Summary: "alice commented",
			Body: "Looks good", CreatedAt: now,
			DedupeKey: "comment-1",
		},
		{
			PRID: prID, EventType: "commit",
			Author: "bob", Summary: "bob pushed 1 commit",
			CreatedAt: now.Add(time.Hour),
			DedupeKey: "commit-abc",
		},
	}

	if err := d.UpsertPREvents(ctx, events); err != nil {
		t.Fatal(err)
	}

	got, err := d.ListPREvents(ctx, prID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	// Newest first
	if got[0].EventType != "commit" {
		t.Fatalf("expected commit first, got %s", got[0].EventType)
	}

	// Duplicate insert is no-op
	if err := d.UpsertPREvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	got, _ = d.ListPREvents(ctx, prID)
	if len(got) != 2 {
		t.Fatalf("expected still 2 events, got %d", len(got))
	}
}

func TestListPullRequestsFilterByKanban(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID, _ := d.UpsertRepo(ctx, "test", "repo")
	now := time.Now().UTC().Truncate(time.Second)

	for i := range 3 {
		pr := &PullRequest{
			RepoID: repoID, GitHubID: int64(100 + i),
			Number: i + 1, State: "open",
			CreatedAt: now, UpdatedAt: now,
			LastActivityAt: now,
		}
		prID, _ := d.UpsertPullRequest(ctx, pr)
		d.EnsureKanbanState(ctx, prID)
	}
	// Set PR 2 to reviewing
	prID2, _ := d.GetPRIDByRepoAndNumber(ctx, "test", "repo", 2)
	d.SetKanbanState(ctx, prID2, "reviewing")

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{
		KanbanState: "reviewing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1, got %d", len(prs))
	}
	if prs[0].Number != 2 {
		t.Fatalf("expected PR 2, got %d", prs[0].Number)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go test ./internal/db/... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/db/queries.go internal/db/queries_test.go
git commit -m "Add database query functions with tests"
```

---

## Task 5: GitHub Client Interface & Normalization

**Files:**
- Create: `internal/github/client.go`
- Create: `internal/github/client_test.go`
- Create: `internal/github/normalize.go`
- Create: `internal/github/normalize_test.go`

- [ ] **Step 1: Create client.go with interface and real implementation**

```go
package github

import (
	"context"

	gh "github.com/google/go-github/v84/github"
	"golang.org/x/oauth2"
)

// Client defines the GitHub API operations ghboard needs.
// Abstracted for testing.
type Client interface {
	ListOpenPullRequests(
		ctx context.Context, owner, repo string,
	) ([]*gh.PullRequest, error)

	GetPullRequest(
		ctx context.Context, owner, repo string, number int,
	) (*gh.PullRequest, error)

	ListIssueComments(
		ctx context.Context, owner, repo string, number int,
	) ([]*gh.IssueComment, error)

	ListReviews(
		ctx context.Context, owner, repo string, number int,
	) ([]*gh.PullRequestReview, error)

	ListCommits(
		ctx context.Context, owner, repo string, number int,
	) ([]*gh.RepositoryCommit, error)

	GetCombinedStatus(
		ctx context.Context, owner, repo, ref string,
	) (*gh.CombinedStatus, error)

	CreateIssueComment(
		ctx context.Context, owner, repo string,
		number int, body string,
	) (*gh.IssueComment, error)
}

// liveClient is the real GitHub API client.
type liveClient struct {
	gh *gh.Client
}

// NewClient creates a GitHub API client with the given token.
func NewClient(token string) Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(context.Background(), ts)
	return &liveClient{gh: gh.NewClient(tc)}
}

func (c *liveClient) ListOpenPullRequests(
	ctx context.Context, owner, repo string,
) ([]*gh.PullRequest, error) {
	var all []*gh.PullRequest
	opts := &gh.PullRequestListOptions{
		State:     "open",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	for {
		prs, resp, err := c.gh.PullRequests.List(
			ctx, owner, repo, opts,
		)
		if err != nil {
			return all, err
		}
		all = append(all, prs...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func (c *liveClient) GetPullRequest(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	return pr, err
}

func (c *liveClient) ListIssueComments(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.IssueComment, error) {
	var all []*gh.IssueComment
	opts := &gh.IssueListCommentsOptions{
		Sort:        gh.String("created"),
		Direction:   gh.String("asc"),
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := c.gh.Issues.ListComments(
			ctx, owner, repo, number, opts,
		)
		if err != nil {
			return all, err
		}
		all = append(all, comments...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func (c *liveClient) ListReviews(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.PullRequestReview, error) {
	var all []*gh.PullRequestReview
	opts := &gh.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := c.gh.PullRequests.ListReviews(
			ctx, owner, repo, number, opts,
		)
		if err != nil {
			return all, err
		}
		all = append(all, reviews...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func (c *liveClient) ListCommits(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.RepositoryCommit, error) {
	var all []*gh.RepositoryCommit
	opts := &gh.ListOptions{PerPage: 100}
	for {
		commits, resp, err := c.gh.PullRequests.ListCommits(
			ctx, owner, repo, number, opts,
		)
		if err != nil {
			return all, err
		}
		all = append(all, commits...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func (c *liveClient) GetCombinedStatus(
	ctx context.Context, owner, repo, ref string,
) (*gh.CombinedStatus, error) {
	status, _, err := c.gh.Repositories.GetCombinedStatus(
		ctx, owner, repo, ref, nil,
	)
	return status, err
}

func (c *liveClient) CreateIssueComment(
	ctx context.Context, owner, repo string,
	number int, body string,
) (*gh.IssueComment, error) {
	comment, _, err := c.gh.Issues.CreateComment(
		ctx, owner, repo, number,
		&gh.IssueComment{Body: &body},
	)
	return comment, err
}
```

- [ ] **Step 2: Create normalize.go**

```go
package github

import (
	"fmt"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/ghboard/internal/db"
)

// NormalizePR converts a GitHub PR to the local DB model.
func NormalizePR(
	repoID int64, ghPR *gh.PullRequest,
) *db.PullRequest {
	pr := &db.PullRequest{
		RepoID:     repoID,
		GitHubID:   ghPR.GetID(),
		Number:     ghPR.GetNumber(),
		URL:        ghPR.GetHTMLURL(),
		Title:      ghPR.GetTitle(),
		Author:     ghPR.GetUser().GetLogin(),
		State:      ghPR.GetState(),
		IsDraft:    ghPR.GetDraft(),
		Body:       ghPR.GetBody(),
		Additions:  ghPR.GetAdditions(),
		Deletions:  ghPR.GetDeletions(),
		CreatedAt:  ghPR.GetCreatedAt().UTC(),
		UpdatedAt:  ghPR.GetUpdatedAt().UTC(),
	}

	if ghPR.GetMerged() {
		pr.State = "merged"
	}

	if head := ghPR.GetHead(); head != nil {
		pr.HeadBranch = head.GetRef()
	}
	if base := ghPR.GetBase(); base != nil {
		pr.BaseBranch = base.GetRef()
	}

	if t := ghPR.GetMergedAt(); !t.IsZero() {
		u := t.UTC()
		pr.MergedAt = &u
	}
	if t := ghPR.GetClosedAt(); !t.IsZero() {
		u := t.UTC()
		pr.ClosedAt = &u
	}

	pr.LastActivityAt = pr.UpdatedAt
	return pr
}

// NormalizeCommentEvent converts an issue comment to a PREvent.
func NormalizeCommentEvent(
	prID int64, c *gh.IssueComment,
) db.PREvent {
	ghID := c.GetID()
	return db.PREvent{
		PRID:      prID,
		GitHubID:  &ghID,
		EventType: "issue_comment",
		Author:    c.GetUser().GetLogin(),
		Summary:   fmt.Sprintf("%s commented", c.GetUser().GetLogin()),
		Body:      c.GetBody(),
		CreatedAt: c.GetCreatedAt().UTC(),
		DedupeKey: fmt.Sprintf("comment-%d", ghID),
	}
}

// NormalizeReviewEvent converts a PR review to a PREvent.
func NormalizeReviewEvent(
	prID int64, r *gh.PullRequestReview,
) db.PREvent {
	ghID := r.GetID()
	state := r.GetState()
	author := r.GetUser().GetLogin()
	summary := fmt.Sprintf("%s reviewed: %s", author, state)
	return db.PREvent{
		PRID:      prID,
		GitHubID:  &ghID,
		EventType: "review",
		Author:    author,
		Summary:   summary,
		Body:      r.GetBody(),
		CreatedAt: r.GetSubmittedAt().UTC(),
		DedupeKey: fmt.Sprintf("review-%d", ghID),
	}
}

// NormalizeCommitEvent converts a commit to a PREvent.
func NormalizeCommitEvent(
	prID int64, c *gh.RepositoryCommit,
) db.PREvent {
	sha := c.GetSHA()
	author := ""
	if a := c.GetAuthor(); a != nil {
		author = a.GetLogin()
	}
	if author == "" {
		if cc := c.GetCommit(); cc != nil {
			if ca := cc.GetAuthor(); ca != nil {
				author = ca.GetName()
			}
		}
	}
	msg := ""
	if cc := c.GetCommit(); cc != nil {
		msg = cc.GetMessage()
	}
	// Truncate commit message to first line
	if idx := len(msg); idx > 0 {
		for i, ch := range msg {
			if ch == '\n' {
				msg = msg[:i]
				break
			}
		}
	}
	return db.PREvent{
		PRID:      prID,
		EventType: "commit",
		Author:    author,
		Summary:   msg,
		CreatedAt: commitTime(c),
		DedupeKey: fmt.Sprintf("commit-%s", sha[:min(len(sha), 12)]),
	}
}

func commitTime(c *gh.RepositoryCommit) time.Time {
	if cc := c.GetCommit(); cc != nil {
		if a := cc.GetAuthor(); a != nil {
			return a.GetDate().UTC()
		}
	}
	return time.Now().UTC()
}

// NormalizeCIStatus summarizes a combined status into a string.
func NormalizeCIStatus(cs *gh.CombinedStatus) string {
	if cs == nil {
		return ""
	}
	return cs.GetState() // "success", "pending", "failure"
}

// DeriveReviewDecision picks the most relevant review state.
func DeriveReviewDecision(
	reviews []*gh.PullRequestReview,
) string {
	if len(reviews) == 0 {
		return ""
	}
	// Walk reviews newest-first by submitted time.
	// Keep the last decisive state per reviewer.
	latest := make(map[string]string)
	for _, r := range reviews {
		user := r.GetUser().GetLogin()
		state := r.GetState()
		if state == "APPROVED" || state == "CHANGES_REQUESTED" {
			latest[user] = state
		}
	}
	if len(latest) == 0 {
		return ""
	}
	for _, s := range latest {
		if s == "CHANGES_REQUESTED" {
			return "changes_requested"
		}
	}
	return "approved"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Write normalize_test.go**

```go
package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
)

func ptr[T any](v T) *T { return &v }

func TestNormalizePR(t *testing.T) {
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ghPR := &gh.PullRequest{
		ID:        ptr(int64(999)),
		Number:    ptr(42),
		HTMLURL:   ptr("https://github.com/test/repo/pull/42"),
		Title:     ptr("Fix bug"),
		User:      &gh.User{Login: ptr("alice")},
		State:     ptr("open"),
		Draft:     ptr(false),
		Body:      ptr("Fixes a bug"),
		Additions: ptr(10),
		Deletions: ptr(3),
		Head:      &gh.PullRequestBranch{Ref: ptr("fix-bug")},
		Base:      &gh.PullRequestBranch{Ref: ptr("main")},
		CreatedAt: &gh.Timestamp{Time: ts},
		UpdatedAt: &gh.Timestamp{Time: ts.Add(time.Hour)},
	}

	pr := NormalizePR(1, ghPR)
	if pr.GitHubID != 999 {
		t.Fatalf("expected github_id 999, got %d", pr.GitHubID)
	}
	if pr.Number != 42 {
		t.Fatalf("expected number 42, got %d", pr.Number)
	}
	if pr.Author != "alice" {
		t.Fatalf("expected author alice, got %s", pr.Author)
	}
	if pr.State != "open" {
		t.Fatalf("expected open, got %s", pr.State)
	}
	if pr.HeadBranch != "fix-bug" {
		t.Fatalf("expected fix-bug, got %s", pr.HeadBranch)
	}
}

func TestNormalizePRMerged(t *testing.T) {
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ghPR := &gh.PullRequest{
		ID:        ptr(int64(1)),
		Number:    ptr(1),
		User:      &gh.User{Login: ptr("bob")},
		State:     ptr("closed"),
		Merged:    ptr(true),
		CreatedAt: &gh.Timestamp{Time: ts},
		UpdatedAt: &gh.Timestamp{Time: ts},
		MergedAt:  &gh.Timestamp{Time: ts},
	}
	pr := NormalizePR(1, ghPR)
	if pr.State != "merged" {
		t.Fatalf("expected merged, got %s", pr.State)
	}
	if pr.MergedAt == nil {
		t.Fatal("expected merged_at to be set")
	}
}

func TestNormalizeCommentEvent(t *testing.T) {
	c := &gh.IssueComment{
		ID:        ptr(int64(555)),
		User:      &gh.User{Login: ptr("alice")},
		Body:      ptr("Looks good"),
		CreatedAt: &gh.Timestamp{
			Time: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	e := NormalizeCommentEvent(1, c)
	if e.EventType != "issue_comment" {
		t.Fatalf("expected issue_comment, got %s", e.EventType)
	}
	if e.DedupeKey != "comment-555" {
		t.Fatalf("expected comment-555, got %s", e.DedupeKey)
	}
	if e.Author != "alice" {
		t.Fatalf("expected alice, got %s", e.Author)
	}
}

func TestDeriveReviewDecision(t *testing.T) {
	tests := []struct {
		name    string
		reviews []*gh.PullRequestReview
		want    string
	}{
		{"empty", nil, ""},
		{
			"approved",
			[]*gh.PullRequestReview{
				{User: &gh.User{Login: ptr("r1")}, State: ptr("APPROVED")},
			},
			"approved",
		},
		{
			"changes_requested_wins",
			[]*gh.PullRequestReview{
				{User: &gh.User{Login: ptr("r1")}, State: ptr("APPROVED")},
				{User: &gh.User{Login: ptr("r2")}, State: ptr("CHANGES_REQUESTED")},
			},
			"changes_requested",
		},
		{
			"commented_only_ignored",
			[]*gh.PullRequestReview{
				{User: &gh.User{Login: ptr("r1")}, State: ptr("COMMENTED")},
			},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveReviewDecision(tt.reviews)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
```

- [ ] **Step 4: Write a basic client_test.go to verify interface satisfaction**

```go
package github

import (
	"testing"
)

// Compile-time check that liveClient implements Client.
var _ Client = (*liveClient)(nil)

func TestNewClientReturnsNonNil(t *testing.T) {
	c := NewClient("fake-token")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go test ./internal/github/... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/github/
git commit -m "Add GitHub client interface and PR/event normalization"
```

---

## Task 6: Sync Engine

**Files:**
- Create: `internal/github/sync.go`
- Create: `internal/github/sync_test.go`

- [ ] **Step 1: Create sync.go**

```go
package github

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/ghboard/internal/db"
)

// SyncStatus tracks the state of the sync engine.
type SyncStatus struct {
	Running   bool      `json:"running"`
	LastRunAt time.Time `json:"last_run_at,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// Syncer periodically syncs GitHub data into the local DB.
type Syncer struct {
	client   Client
	db       *db.DB
	repos    []RepoRef
	interval time.Duration

	running atomic.Bool
	status  atomic.Value // *SyncStatus
	stopCh  chan struct{}
}

// RepoRef identifies a repo to sync.
type RepoRef struct {
	Owner string
	Name  string
}

// NewSyncer creates a sync engine.
func NewSyncer(
	client Client, database *db.DB,
	repos []RepoRef, interval time.Duration,
) *Syncer {
	s := &Syncer{
		client:   client,
		db:       database,
		repos:    repos,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	s.status.Store(&SyncStatus{})
	return s
}

// Start begins periodic syncing in the background.
func (s *Syncer) Start(ctx context.Context) {
	// Sync immediately on start
	go func() {
		s.RunOnce(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RunOnce(ctx)
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the sync loop to stop.
func (s *Syncer) Stop() {
	close(s.stopCh)
}

// Status returns the current sync status.
func (s *Syncer) Status() *SyncStatus {
	return s.status.Load().(*SyncStatus)
}

// RunOnce executes a single sync run across all repos.
func (s *Syncer) RunOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return // already running
	}
	defer s.running.Store(false)

	st := &SyncStatus{Running: true}
	s.status.Store(st)

	var lastErr string
	for _, repo := range s.repos {
		if err := s.syncRepo(ctx, repo); err != nil {
			log.Printf("sync %s/%s: %v", repo.Owner, repo.Name, err)
			lastErr = err.Error()
		}
	}

	s.status.Store(&SyncStatus{
		Running:   false,
		LastRunAt: time.Now().UTC(),
		LastError: lastErr,
	})
}

func (s *Syncer) syncRepo(ctx context.Context, ref RepoRef) error {
	repoID, err := s.db.UpsertRepo(ctx, ref.Owner, ref.Name)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if err := s.db.UpdateRepoSyncStarted(ctx, repoID, now); err != nil {
		return err
	}

	syncErr := s.doSyncRepo(ctx, repoID, ref)
	errStr := ""
	if syncErr != nil {
		errStr = syncErr.Error()
	}

	_ = s.db.UpdateRepoSyncCompleted(
		ctx, repoID, time.Now().UTC(), errStr,
	)
	return syncErr
}

func (s *Syncer) doSyncRepo(
	ctx context.Context, repoID int64, ref RepoRef,
) error {
	ghPRs, err := s.client.ListOpenPullRequests(
		ctx, ref.Owner, ref.Name,
	)
	if err != nil {
		return err
	}

	stillOpen := make(map[int]bool, len(ghPRs))

	for _, ghPR := range ghPRs {
		pr := NormalizePR(repoID, ghPR)
		stillOpen[pr.Number] = true

		// Check if PR updated since last sync
		existing, _ := s.db.GetPullRequest(
			ctx, ref.Owner, ref.Name, pr.Number,
		)
		needsTimelineRefresh := existing == nil ||
			!existing.UpdatedAt.Equal(pr.UpdatedAt)

		prID, err := s.db.UpsertPullRequest(ctx, pr)
		if err != nil {
			return err
		}

		if needsTimelineRefresh {
			if err := s.syncTimeline(
				ctx, repoID, prID, ref, ghPR,
			); err != nil {
				log.Printf(
					"sync timeline %s/%s#%d: %v",
					ref.Owner, ref.Name, pr.Number, err,
				)
			}
		}

		if err := s.db.EnsureKanbanState(ctx, prID); err != nil {
			return err
		}
	}

	// Handle PRs that are no longer open
	closed, err := s.db.GetPreviouslyOpenPRNumbers(
		ctx, repoID, stillOpen,
	)
	if err != nil {
		return err
	}
	for _, num := range closed {
		s.resolveClosed(ctx, repoID, ref, num)
	}

	return nil
}

func (s *Syncer) syncTimeline(
	ctx context.Context,
	repoID, prID int64,
	ref RepoRef,
	ghPR *gh.PullRequest,
) error {
	number := ghPR.GetNumber()
	var events []db.PREvent

	// Comments
	comments, err := s.client.ListIssueComments(
		ctx, ref.Owner, ref.Name, number,
	)
	if err != nil {
		return err
	}
	for _, c := range comments {
		events = append(events, NormalizeCommentEvent(prID, c))
	}

	// Reviews
	reviews, err := s.client.ListReviews(
		ctx, ref.Owner, ref.Name, number,
	)
	if err != nil {
		return err
	}
	for _, r := range reviews {
		events = append(events, NormalizeReviewEvent(prID, r))
	}

	// Commits
	commits, err := s.client.ListCommits(
		ctx, ref.Owner, ref.Name, number,
	)
	if err != nil {
		return err
	}
	for _, c := range commits {
		events = append(events, NormalizeCommitEvent(prID, c))
	}

	if err := s.db.UpsertPREvents(ctx, events); err != nil {
		return err
	}

	// Update derived fields
	pr := NormalizePR(repoID, ghPR)
	pr.ReviewDecision = DeriveReviewDecision(reviews)
	pr.CommentCount = len(comments)

	// Compute last_activity_at from events
	maxActivity := pr.UpdatedAt
	for _, e := range events {
		if e.CreatedAt.After(maxActivity) {
			maxActivity = e.CreatedAt
		}
	}
	pr.LastActivityAt = maxActivity

	// CI status
	if head := ghPR.GetHead(); head != nil && head.GetSHA() != "" {
		cs, err := s.client.GetCombinedStatus(
			ctx, ref.Owner, ref.Name, head.GetSHA(),
		)
		if err == nil {
			pr.CIStatus = NormalizeCIStatus(cs)
		}
	}

	_, err = s.db.UpsertPullRequest(ctx, pr)
	return err
}

func (s *Syncer) resolveClosed(
	ctx context.Context, repoID int64,
	ref RepoRef, number int,
) {
	ghPR, err := s.client.GetPullRequest(
		ctx, ref.Owner, ref.Name, number,
	)
	if err != nil {
		log.Printf(
			"resolve %s/%s#%d: %v",
			ref.Owner, ref.Name, number, err,
		)
		return
	}

	state := ghPR.GetState()
	if ghPR.GetMerged() {
		state = "merged"
	}

	var mergedAt, closedAt *time.Time
	if t := ghPR.GetMergedAt(); !t.IsZero() {
		u := t.UTC()
		mergedAt = &u
	}
	if t := ghPR.GetClosedAt(); !t.IsZero() {
		u := t.UTC()
		closedAt = &u
	}

	_ = s.db.UpdatePRState(
		ctx, repoID, number, state, mergedAt, closedAt,
	)
}
```

- [ ] **Step 2: Create sync_test.go with mock client**

```go
package github

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/ghboard/internal/db"
)

// mockClient implements Client for testing.
type mockClient struct {
	prs      []*gh.PullRequest
	comments []*gh.IssueComment
	reviews  []*gh.PullRequestReview
	commits  []*gh.RepositoryCommit
	status   *gh.CombinedStatus

	// Track calls
	getCallCount int
}

func (m *mockClient) ListOpenPullRequests(
	_ context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	return m.prs, nil
}

func (m *mockClient) GetPullRequest(
	_ context.Context, _, _ string, _ int,
) (*gh.PullRequest, error) {
	m.getCallCount++
	if len(m.prs) > 0 {
		return m.prs[0], nil
	}
	return nil, nil
}

func (m *mockClient) ListIssueComments(
	_ context.Context, _, _ string, _ int,
) ([]*gh.IssueComment, error) {
	return m.comments, nil
}

func (m *mockClient) ListReviews(
	_ context.Context, _, _ string, _ int,
) ([]*gh.PullRequestReview, error) {
	return m.reviews, nil
}

func (m *mockClient) ListCommits(
	_ context.Context, _, _ string, _ int,
) ([]*gh.RepositoryCommit, error) {
	return m.commits, nil
}

func (m *mockClient) GetCombinedStatus(
	_ context.Context, _, _, _ string,
) (*gh.CombinedStatus, error) {
	if m.status != nil {
		return m.status, nil
	}
	return &gh.CombinedStatus{State: ptr("pending")}, nil
}

func (m *mockClient) CreateIssueComment(
	_ context.Context, _, _ string, _ int, body string,
) (*gh.IssueComment, error) {
	return &gh.IssueComment{
		ID:   ptr(int64(999)),
		Body: &body,
	}, nil
}

var _ Client = (*mockClient)(nil)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSyncCreatesAndUpdatesPRs(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	mock := &mockClient{
		prs: []*gh.PullRequest{
			{
				ID:        ptr(int64(100)),
				Number:    ptr(1),
				Title:     ptr("First PR"),
				HTMLURL:   ptr("https://github.com/t/r/pull/1"),
				User:      &gh.User{Login: ptr("alice")},
				State:     ptr("open"),
				Head: &gh.PullRequestBranch{
					Ref: ptr("feat"), SHA: ptr("abc123"),
				},
				Base:      &gh.PullRequestBranch{Ref: ptr("main")},
				CreatedAt: &gh.Timestamp{Time: ts},
				UpdatedAt: &gh.Timestamp{Time: ts},
			},
		},
		commits: []*gh.RepositoryCommit{
			{
				SHA: ptr("abc123"),
				Commit: &gh.Commit{
					Message: ptr("initial"),
					Author: &gh.CommitAuthor{
						Name: ptr("alice"),
						Date: &gh.Timestamp{Time: ts},
					},
				},
			},
		},
	}

	syncer := NewSyncer(mock, d, []RepoRef{
		{Owner: "t", Name: "r"},
	}, time.Minute)

	syncer.RunOnce(ctx)

	// Verify PR was created
	pr, err := d.GetPullRequest(ctx, "t", "r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil {
		t.Fatal("expected PR to exist")
	}
	if pr.Title != "First PR" {
		t.Fatalf("expected 'First PR', got %q", pr.Title)
	}

	// Verify kanban state was created
	ks, err := d.GetKanbanState(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ks == nil || ks.Status != "new" {
		t.Fatal("expected kanban state 'new'")
	}

	// Verify events
	events, err := d.ListPREvents(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestSyncSingleFlight(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	mock := &mockClient{}
	syncer := NewSyncer(mock, d, nil, time.Minute)

	// Force running state
	syncer.running.Store(true)
	syncer.RunOnce(ctx) // should be a no-op

	st := syncer.Status()
	if st.Running {
		t.Fatal("expected not running after no-op")
	}
}

func TestSyncStatusUpdated(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	mock := &mockClient{}
	syncer := NewSyncer(
		mock, d,
		[]RepoRef{{Owner: "t", Name: "r"}},
		time.Minute,
	)

	syncer.RunOnce(ctx)
	st := syncer.Status()
	if st.Running {
		t.Fatal("expected not running after sync")
	}
	if st.LastRunAt.IsZero() {
		t.Fatal("expected LastRunAt to be set")
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go test ./internal/github/... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "Add sync engine with periodic GitHub data pull"
```

---

## Task 7: HTTP Server & Handlers

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/handlers.go`
- Create: `internal/server/handlers_test.go`

- [ ] **Step 1: Create server.go**

```go
package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/wesm/ghboard/internal/db"
	ghclient "github.com/wesm/ghboard/internal/github"
)

// Server is the ghboard HTTP server.
type Server struct {
	db     *db.DB
	gh     ghclient.Client
	syncer *ghclient.Syncer
	mux    *http.ServeMux
}

// New creates a server with all routes registered.
func New(
	database *db.DB,
	gh ghclient.Client,
	syncer *ghclient.Syncer,
	frontend fs.FS,
) *Server {
	s := &Server{
		db:     database,
		gh:     gh,
		syncer: syncer,
		mux:    http.NewServeMux(),
	}
	s.routes(frontend)
	return s
}

func (s *Server) routes(frontend fs.FS) {
	// API
	s.mux.HandleFunc("GET /api/v1/pulls", s.handleListPulls)
	s.mux.HandleFunc(
		"GET /api/v1/repos/{owner}/{name}/pulls/{number}",
		s.handleGetPull,
	)
	s.mux.HandleFunc(
		"PUT /api/v1/repos/{owner}/{name}/pulls/{number}/state",
		s.handleSetKanbanState,
	)
	s.mux.HandleFunc(
		"POST /api/v1/repos/{owner}/{name}/pulls/{number}/comments",
		s.handlePostComment,
	)
	s.mux.HandleFunc("GET /api/v1/repos", s.handleListRepos)
	s.mux.HandleFunc("POST /api/v1/sync", s.handleTriggerSync)
	s.mux.HandleFunc("GET /api/v1/sync/status", s.handleSyncStatus)

	// SPA: serve frontend, fall back to index.html
	if frontend != nil {
		fileServer := http.FileServer(http.FS(frontend))
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Try serving static file first
			path := r.URL.Path
			if path == "/" {
				path = "/index.html"
			}
			// Check if file exists
			f, err := frontend.Open(path[1:]) // strip leading /
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			// Fall back to index.html for SPA routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
		})
	}
}

// Handler returns the http.Handler for this server.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("ghboard listening on http://%s", addr)
	return srv.ListenAndServe()
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 2: Create handlers.go**

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/wesm/ghboard/internal/db"
	ghclient "github.com/wesm/ghboard/internal/github"
)

func (s *Server) handleListPulls(
	w http.ResponseWriter, r *http.Request,
) {
	q := r.URL.Query()
	opts := db.ListPullsOpts{
		State:       q.Get("state"),
		KanbanState: q.Get("kanban"),
		Search:      q.Get("q"),
	}

	if repo := q.Get("repo"); repo != "" {
		// Parse "owner/name"
		for i, ch := range repo {
			if ch == '/' {
				opts.RepoOwner = repo[:i]
				opts.RepoName = repo[i+1:]
				break
			}
		}
	}

	if v := q.Get("limit"); v != "" {
		opts.Limit, _ = strconv.Atoi(v)
	}
	if v := q.Get("offset"); v != "" {
		opts.Offset, _ = strconv.Atoi(v)
	}

	prs, err := s.db.ListPullRequests(r.Context(), opts)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	// Include repo owner/name in response
	type pullResponse struct {
		db.PullRequest
		RepoOwner string `json:"repo_owner"`
		RepoName  string `json:"repo_name"`
	}

	// Look up repo info for each PR
	repoCache := make(map[int64]*db.Repo)
	result := make([]pullResponse, 0, len(prs))
	for _, pr := range prs {
		repo, ok := repoCache[pr.RepoID]
		if !ok {
			repos, _ := s.db.ListRepos(r.Context())
			for i := range repos {
				repoCache[repos[i].ID] = &repos[i]
			}
			repo = repoCache[pr.RepoID]
		}
		resp := pullResponse{PullRequest: pr}
		if repo != nil {
			resp.RepoOwner = repo.Owner
			resp.RepoName = repo.Name
		}
		result = append(result, resp)
	}

	writeJSON(w, 200, result)
}

func (s *Server) handleGetPull(
	w http.ResponseWriter, r *http.Request,
) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeError(w, 400, "invalid PR number")
		return
	}

	pr, err := s.db.GetPullRequest(r.Context(), owner, name, number)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if pr == nil {
		writeError(w, 404, "pull request not found")
		return
	}

	events, err := s.db.ListPREvents(r.Context(), pr.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, map[string]any{
		"pull_request": pr,
		"events":       events,
		"repo_owner":   owner,
		"repo_name":    name,
	})
}

func (s *Server) handleSetKanbanState(
	w http.ResponseWriter, r *http.Request,
) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeError(w, 400, "invalid PR number")
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	valid := map[string]bool{
		"new": true, "reviewing": true,
		"waiting": true, "awaiting_merge": true,
	}
	if !valid[body.Status] {
		writeError(w, 400, "invalid status: must be new, reviewing, waiting, or awaiting_merge")
		return
	}

	prID, err := s.db.GetPRIDByRepoAndNumber(
		r.Context(), owner, name, number,
	)
	if err != nil {
		writeError(w, 404, "pull request not found")
		return
	}

	if err := s.db.SetKanbanState(
		r.Context(), prID, body.Status,
	); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, map[string]string{"status": body.Status})
}

func (s *Server) handlePostComment(
	w http.ResponseWriter, r *http.Request,
) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeError(w, 400, "invalid PR number")
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.Body == "" {
		writeError(w, 400, "body is required")
		return
	}

	// Post to GitHub
	comment, err := s.gh.CreateIssueComment(
		r.Context(), owner, name, number, body.Body,
	)
	if err != nil {
		writeError(w, 502, "GitHub API error: "+err.Error())
		return
	}

	// Store locally
	prID, lookupErr := s.db.GetPRIDByRepoAndNumber(
		r.Context(), owner, name, number,
	)
	if lookupErr == nil {
		event := ghclient.NormalizeCommentEvent(prID, comment)
		_ = s.db.UpsertPREvents(r.Context(), []db.PREvent{event})
	}

	writeJSON(w, 201, map[string]any{
		"id":   comment.GetID(),
		"body": comment.GetBody(),
	})
}

func (s *Server) handleListRepos(
	w http.ResponseWriter, r *http.Request,
) {
	repos, err := s.db.ListRepos(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, repos)
}

func (s *Server) handleTriggerSync(
	w http.ResponseWriter, r *http.Request,
) {
	go s.syncer.RunOnce(r.Context())
	w.WriteHeader(202)
}

func (s *Server) handleSyncStatus(
	w http.ResponseWriter, r *http.Request,
) {
	writeJSON(w, 200, s.syncer.Status())
}
```

- [ ] **Step 3: Create handlers_test.go**

```go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wesm/ghboard/internal/db"
	ghclient "github.com/wesm/ghboard/internal/github"
)

func setupTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, d, nil, time.Minute)
	srv := New(d, mock, syncer, nil)
	return srv, d
}

type mockGH struct{}

func (m *mockGH) ListOpenPullRequests(
	_ context.Context, _, _ string,
) ([]*ghLib.PullRequest, error) {
	return nil, nil
}
func (m *mockGH) GetPullRequest(
	_ context.Context, _, _ string, _ int,
) (*ghLib.PullRequest, error) {
	return nil, nil
}
func (m *mockGH) ListIssueComments(
	_ context.Context, _, _ string, _ int,
) ([]*ghLib.IssueComment, error) {
	return nil, nil
}
func (m *mockGH) ListReviews(
	_ context.Context, _, _ string, _ int,
) ([]*ghLib.PullRequestReview, error) {
	return nil, nil
}
func (m *mockGH) ListCommits(
	_ context.Context, _, _ string, _ int,
) ([]*ghLib.RepositoryCommit, error) {
	return nil, nil
}
func (m *mockGH) GetCombinedStatus(
	_ context.Context, _, _, _ string,
) (*ghLib.CombinedStatus, error) {
	return nil, nil
}
func (m *mockGH) CreateIssueComment(
	_ context.Context, _, _ string, _ int, body string,
) (*ghLib.IssueComment, error) {
	id := int64(1)
	return &ghLib.IssueComment{ID: &id, Body: &body}, nil
}

var _ ghclient.Client = (*mockGH)(nil)

func seedPR(t *testing.T, d *db.DB) {
	t.Helper()
	ctx := context.Background()
	repoID, _ := d.UpsertRepo(ctx, "test", "repo")
	now := time.Now().UTC().Truncate(time.Second)
	prID, _ := d.UpsertPullRequest(ctx, &db.PullRequest{
		RepoID: repoID, GitHubID: 1, Number: 42,
		URL:   "https://github.com/test/repo/pull/42",
		Title: "Test PR", Author: "alice", State: "open",
		CreatedAt: now, UpdatedAt: now, LastActivityAt: now,
	})
	d.EnsureKanbanState(ctx, prID)
}

func TestHandleListPulls(t *testing.T) {
	srv, d := setupTestServer(t)
	seedPR(t, d)

	req := httptest.NewRequest("GET", "/api/v1/pulls", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(result))
	}
}

func TestHandleGetPull(t *testing.T) {
	srv, d := setupTestServer(t)
	seedPR(t, d)

	req := httptest.NewRequest(
		"GET",
		"/api/v1/repos/test/repo/pulls/42",
		nil,
	)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetPull404(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(
		"GET",
		"/api/v1/repos/no/repo/pulls/999",
		nil,
	)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleSetKanbanState(t *testing.T) {
	srv, d := setupTestServer(t)
	seedPR(t, d)

	body := bytes.NewBufferString(`{"status":"reviewing"}`)
	req := httptest.NewRequest(
		"PUT",
		"/api/v1/repos/test/repo/pulls/42/state",
		body,
	)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it persisted
	prID, _ := d.GetPRIDByRepoAndNumber(
		context.Background(), "test", "repo", 42,
	)
	ks, _ := d.GetKanbanState(context.Background(), prID)
	if ks.Status != "reviewing" {
		t.Fatalf("expected reviewing, got %s", ks.Status)
	}
}

func TestHandleSetKanbanStateInvalid(t *testing.T) {
	srv, d := setupTestServer(t)
	seedPR(t, d)

	body := bytes.NewBufferString(`{"status":"invalid"}`)
	req := httptest.NewRequest(
		"PUT",
		"/api/v1/repos/test/repo/pulls/42/state",
		body,
	)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleListRepos(t *testing.T) {
	srv, d := setupTestServer(t)
	d.UpsertRepo(context.Background(), "test", "repo")

	req := httptest.NewRequest("GET", "/api/v1/repos", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var repos []map[string]any
	json.NewDecoder(w.Body).Decode(&repos)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
}

func TestHandleSyncStatus(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/sync/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
```

**Note:** The test file above has an import alias issue — `ghLib` should be the go-github package. The import should be:

```go
import (
	ghLib "github.com/google/go-github/v84/github"
)
```

Add this to the imports section of the test file.

- [ ] **Step 4: Run tests**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go test ./internal/server/... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "Add HTTP server with API handlers and tests"
```

---

## Task 8: Wire Up main.go

**Files:**
- Modify: `cmd/ghboard/main.go`

- [ ] **Step 1: Replace main.go with full server startup**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wesm/ghboard/internal/config"
	"github.com/wesm/ghboard/internal/db"
	ghclient "github.com/wesm/ghboard/internal/github"
	"github.com/wesm/ghboard/internal/server"
	"github.com/wesm/ghboard/internal/web"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	configPath := flag.String(
		"config", config.DefaultConfigPath(),
		"path to config.toml",
	)
	flag.Parse()

	if len(flag.Args()) > 0 && flag.Args()[0] == "version" {
		fmt.Printf(
			"ghboard %s (%s) built %s\n",
			version, commit, buildDate,
		)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	token := cfg.GitHubToken()
	if token == "" {
		log.Fatalf(
			"GitHub token not found in env var %s",
			cfg.GitHubTokenEnv,
		)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	gh := ghclient.NewClient(token)

	repos := make([]ghclient.RepoRef, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = ghclient.RepoRef{
			Owner: r.Owner, Name: r.Name,
		}
	}

	syncer := ghclient.NewSyncer(
		gh, database, repos, cfg.SyncDuration(),
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	syncer.Start(ctx)
	defer syncer.Stop()

	frontend, err := web.Assets()
	if err != nil {
		log.Printf("no embedded frontend: %v", err)
	}

	srv := server.New(database, gh, syncer, frontend)
	log.Fatal(srv.ListenAndServe(cfg.ListenAddr()))
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/wesm/code/ghboard && make ensure-embed-dir && CGO_ENABLED=1 go build -o ghboard ./cmd/ghboard
```

Expected: builds successfully.

- [ ] **Step 3: Commit**

```bash
git add cmd/ghboard/main.go
git commit -m "Wire up main entrypoint with config, DB, sync, and server"
```

---

## Task 9: Frontend Design System & Layout Shell

**Files:**
- Modify: `frontend/src/app.css`
- Modify: `frontend/src/App.svelte`
- Create: `frontend/src/lib/stores/router.svelte.ts`
- Create: `frontend/src/lib/components/layout/AppHeader.svelte`

- [ ] **Step 1: Write app.css design tokens**

```css
/* Reset */
*,
*::before,
*::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

/* Light theme */
:root {
  --bg-primary: #f5f6f8;
  --bg-surface: #ffffff;
  --bg-surface-hover: #f0f1f4;
  --bg-inset: #ecedf2;
  --border-default: #d8dae2;
  --border-muted: #e4e6ec;
  --text-primary: #181b24;
  --text-secondary: #555b6e;
  --text-muted: #878ea0;
  --accent-blue: #2563eb;
  --accent-amber: #d97706;
  --accent-purple: #7c3aed;
  --accent-green: #059669;
  --accent-red: #dc2626;
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);
  --shadow-md: 0 2px 8px rgba(0, 0, 0, 0.08);
  --radius-sm: 4px;
  --radius-md: 6px;
  --radius-lg: 8px;
  --font-sans: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI",
    Helvetica, Arial, sans-serif;
  --font-mono: "JetBrains Mono", "SF Mono", Menlo, Consolas, monospace;
  --header-height: 44px;

  /* Kanban state colors */
  --kanban-new: var(--accent-blue);
  --kanban-reviewing: var(--accent-amber);
  --kanban-waiting: var(--accent-purple);
  --kanban-awaiting-merge: var(--accent-green);

  color-scheme: light;
}

/* Dark theme */
:root.dark {
  --bg-primary: #0d0d12;
  --bg-surface: #16161e;
  --bg-surface-hover: #1f1f2a;
  --bg-inset: #111116;
  --border-default: #2b2b38;
  --border-muted: #232330;
  --text-primary: #e4e6eb;
  --text-secondary: #9ea5b4;
  --text-muted: #6c7385;
  --accent-blue: #60a5fa;
  --accent-amber: #fbbf24;
  --accent-purple: #a78bfa;
  --accent-green: #34d399;
  --accent-red: #f87171;
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.25);
  --shadow-md: 0 2px 8px rgba(0, 0, 0, 0.35);

  --kanban-new: var(--accent-blue);
  --kanban-reviewing: var(--accent-amber);
  --kanban-waiting: var(--accent-purple);
  --kanban-awaiting-merge: var(--accent-green);

  color-scheme: dark;
}

html,
body {
  height: 100%;
  overflow: hidden;
  font-family: var(--font-sans);
  font-size: 13px;
  line-height: 1.5;
  color: var(--text-primary);
  background: var(--bg-primary);
  -webkit-font-smoothing: antialiased;
}

#app {
  height: 100%;
  display: flex;
  flex-direction: column;
}

/* Scrollbar */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
::-webkit-scrollbar-thumb {
  background: var(--border-default);
  border-radius: 3px;
}
::-webkit-scrollbar-track {
  background: transparent;
}

/* Utility */
button {
  font: inherit;
  color: inherit;
  cursor: pointer;
  border: none;
  background: none;
}

input,
textarea {
  font: inherit;
  color: inherit;
}

a {
  color: var(--accent-blue);
  text-decoration: none;
}
a:hover {
  text-decoration: underline;
}
```

- [ ] **Step 2: Create router store**

```typescript
// frontend/src/lib/stores/router.svelte.ts

type View = "list" | "board";

let currentView = $state<View>("list");

export function getView(): View {
  return currentView;
}

export function setView(v: View): void {
  currentView = v;
}
```

- [ ] **Step 3: Create AppHeader component**

```svelte
<!-- frontend/src/lib/components/layout/AppHeader.svelte -->
<script lang="ts">
  import { getView, setView } from "../../stores/router.svelte";

  let dark = $state(
    window.matchMedia("(prefers-color-scheme: dark)").matches,
  );

  $effect(() => {
    document.documentElement.classList.toggle("dark", dark);
  });

  function toggleTheme() {
    dark = !dark;
  }

  let syncing = $state(false);

  async function triggerSync() {
    syncing = true;
    try {
      await fetch("/api/v1/sync", { method: "POST" });
    } finally {
      setTimeout(() => (syncing = false), 2000);
    }
  }
</script>

<header class="header">
  <div class="header-left">
    <span class="logo">ghboard</span>
  </div>

  <div class="header-center">
    <div class="view-toggle">
      <button
        class="toggle-btn"
        class:active={getView() === "list"}
        onclick={() => setView("list")}
      >
        List
      </button>
      <button
        class="toggle-btn"
        class:active={getView() === "board"}
        onclick={() => setView("board")}
      >
        Board
      </button>
    </div>
  </div>

  <div class="header-right">
    <button
      class="icon-btn"
      onclick={triggerSync}
      disabled={syncing}
      title="Sync now"
    >
      {syncing ? "Syncing..." : "Sync"}
    </button>
    <button class="icon-btn" onclick={toggleTheme} title="Toggle theme">
      {dark ? "Light" : "Dark"}
    </button>
  </div>
</header>

<style>
  .header {
    height: var(--header-height);
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 16px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
  }
  .header-left,
  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .header-center {
    display: flex;
    align-items: center;
  }
  .logo {
    font-weight: 700;
    font-size: 15px;
    letter-spacing: -0.02em;
  }
  .view-toggle {
    display: flex;
    background: var(--bg-inset);
    border-radius: var(--radius-md);
    padding: 2px;
  }
  .toggle-btn {
    padding: 4px 12px;
    border-radius: var(--radius-sm);
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    transition: all 0.1s;
  }
  .toggle-btn.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: var(--shadow-sm);
  }
  .icon-btn {
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    font-size: 12px;
    color: var(--text-secondary);
    transition: background 0.1s;
  }
  .icon-btn:hover {
    background: var(--bg-surface-hover);
  }
  .icon-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
```

- [ ] **Step 4: Update App.svelte with layout shell**

```svelte
<!-- frontend/src/App.svelte -->
<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import { getView } from "./lib/stores/router.svelte";
</script>

<AppHeader />

<main class="main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        <p style="padding: 16px; color: var(--text-muted)">
          PR list (coming soon)
        </p>
      </aside>
      <section class="detail">
        <p style="padding: 16px; color: var(--text-muted)">
          Select a PR to view details
        </p>
      </section>
    </div>
  {:else}
    <div class="board-layout">
      <p style="padding: 16px; color: var(--text-muted)">
        Board view (coming soon)
      </p>
    </div>
  {/if}
</main>

<style>
  .main {
    flex: 1;
    overflow: hidden;
  }
  .list-layout {
    display: flex;
    height: 100%;
  }
  .sidebar {
    width: 340px;
    border-right: 1px solid var(--border-muted);
    overflow-y: auto;
    background: var(--bg-surface);
    flex-shrink: 0;
  }
  .detail {
    flex: 1;
    overflow-y: auto;
  }
  .board-layout {
    height: 100%;
    overflow-x: auto;
    padding: 16px;
  }
</style>
```

- [ ] **Step 5: Verify frontend builds**

```bash
cd /Users/wesm/code/ghboard/frontend && npm run build
```

Expected: builds without errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/
git commit -m "Add frontend design system, header, and layout shell"
```

---

## Task 10: Frontend API Client & Stores

**Files:**
- Create: `frontend/src/lib/api/types.ts`
- Create: `frontend/src/lib/api/client.ts`
- Create: `frontend/src/lib/stores/pulls.svelte.ts`
- Create: `frontend/src/lib/stores/detail.svelte.ts`
- Create: `frontend/src/lib/stores/sync.svelte.ts`

- [ ] **Step 1: Create types.ts**

```typescript
export interface PullRequest {
  ID: number;
  RepoID: number;
  GitHubID: number;
  Number: number;
  URL: string;
  Title: string;
  Author: string;
  State: string;
  IsDraft: boolean;
  Body: string;
  HeadBranch: string;
  BaseBranch: string;
  Additions: number;
  Deletions: number;
  CommentCount: number;
  ReviewDecision: string;
  CIStatus: string;
  CreatedAt: string;
  UpdatedAt: string;
  LastActivityAt: string;
  MergedAt: string | null;
  ClosedAt: string | null;
  KanbanStatus: string;
  // Joined fields from list endpoint
  repo_owner?: string;
  repo_name?: string;
}

export interface PREvent {
  ID: number;
  PRID: number;
  GitHubID: number | null;
  EventType: string;
  Author: string;
  Summary: string;
  Body: string;
  MetadataJSON: string;
  CreatedAt: string;
  DedupeKey: string;
}

export interface Repo {
  ID: number;
  Owner: string;
  Name: string;
  LastSyncStartedAt: string | null;
  LastSyncCompletedAt: string | null;
  LastSyncError: string;
  CreatedAt: string;
}

export interface PullDetail {
  pull_request: PullRequest;
  events: PREvent[];
  repo_owner: string;
  repo_name: string;
}

export interface SyncStatus {
  running: boolean;
  last_run_at: string;
  last_error: string;
}

export type KanbanStatus =
  | "new"
  | "reviewing"
  | "waiting"
  | "awaiting_merge";
```

- [ ] **Step 2: Create client.ts**

```typescript
import type {
  PullRequest,
  PullDetail,
  Repo,
  SyncStatus,
  KanbanStatus,
} from "./types";

const BASE = "/api/v1";

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(BASE + path, init);
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`API ${res.status}: ${body}`);
  }
  if (res.status === 202) return undefined as T;
  return res.json();
}

export async function listPulls(params?: {
  repo?: string;
  state?: string;
  kanban?: string;
  q?: string;
  limit?: number;
  offset?: number;
}): Promise<PullRequest[]> {
  const sp = new URLSearchParams();
  if (params?.repo) sp.set("repo", params.repo);
  if (params?.state) sp.set("state", params.state);
  if (params?.kanban) sp.set("kanban", params.kanban);
  if (params?.q) sp.set("q", params.q);
  if (params?.limit) sp.set("limit", String(params.limit));
  if (params?.offset) sp.set("offset", String(params.offset));
  const qs = sp.toString();
  return request<PullRequest[]>(`/pulls${qs ? "?" + qs : ""}`);
}

export async function getPull(
  owner: string,
  name: string,
  number: number,
): Promise<PullDetail> {
  return request<PullDetail>(
    `/repos/${owner}/${name}/pulls/${number}`,
  );
}

export async function setKanbanState(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): Promise<void> {
  await request(`/repos/${owner}/${name}/pulls/${number}/state`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ status }),
  });
}

export async function postComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<{ id: number; body: string }> {
  return request(
    `/repos/${owner}/${name}/pulls/${number}/comments`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ body }),
    },
  );
}

export async function listRepos(): Promise<Repo[]> {
  return request<Repo[]>("/repos");
}

export async function triggerSync(): Promise<void> {
  await request("/sync", { method: "POST" });
}

export async function getSyncStatus(): Promise<SyncStatus> {
  return request<SyncStatus>("/sync/status");
}
```

- [ ] **Step 3: Create pulls store**

```typescript
// frontend/src/lib/stores/pulls.svelte.ts
import type { PullRequest, KanbanStatus } from "../api/types";
import { listPulls } from "../api/client";

let pulls = $state<PullRequest[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let filterRepo = $state<string | null>(null);
let filterKanban = $state<string | null>(null);
let searchQuery = $state("");
let selectedPR = $state<{
  owner: string;
  name: string;
  number: number;
} | null>(null);

export function getPulls(): PullRequest[] {
  return pulls;
}

export function isLoading(): boolean {
  return loading;
}

export function getError(): string | null {
  return error;
}

export function getFilterRepo(): string | null {
  return filterRepo;
}

export function setFilterRepo(repo: string | null): void {
  filterRepo = repo;
}

export function getFilterKanban(): string | null {
  return filterKanban;
}

export function setFilterKanban(k: string | null): void {
  filterKanban = k;
}

export function getSearchQuery(): string {
  return searchQuery;
}

export function setSearchQuery(q: string): void {
  searchQuery = q;
}

export function getSelectedPR() {
  return selectedPR;
}

export function selectPR(
  owner: string,
  name: string,
  number: number,
): void {
  selectedPR = { owner, name, number };
}

export function clearSelection(): void {
  selectedPR = null;
}

export async function loadPulls(): Promise<void> {
  loading = true;
  error = null;
  try {
    pulls = await listPulls({
      repo: filterRepo ?? undefined,
      kanban: filterKanban ?? undefined,
      q: searchQuery || undefined,
      limit: 100,
    });
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  } finally {
    loading = false;
  }
}

// Group pulls by repo for sidebar display
export function pullsByRepo(): Map<string, PullRequest[]> {
  const grouped = new Map<string, PullRequest[]>();
  for (const pr of pulls) {
    const key = `${pr.repo_owner}/${pr.repo_name}`;
    const list = grouped.get(key) ?? [];
    list.push(pr);
    grouped.set(key, list);
  }
  return grouped;
}
```

- [ ] **Step 4: Create detail store**

```typescript
// frontend/src/lib/stores/detail.svelte.ts
import type { PullDetail, KanbanStatus } from "../api/types";
import { getPull, setKanbanState, postComment } from "../api/client";
import { loadPulls } from "./pulls.svelte";

let detail = $state<PullDetail | null>(null);
let loading = $state(false);
let error = $state<string | null>(null);

export function getDetail(): PullDetail | null {
  return detail;
}

export function isDetailLoading(): boolean {
  return loading;
}

export function getDetailError(): string | null {
  return error;
}

export async function loadDetail(
  owner: string,
  name: string,
  number: number,
): Promise<void> {
  loading = true;
  error = null;
  try {
    detail = await getPull(owner, name, number);
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  } finally {
    loading = false;
  }
}

export function clearDetail(): void {
  detail = null;
}

export async function updateKanbanState(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): Promise<void> {
  await setKanbanState(owner, name, number, status);
  // Optimistically update local state
  if (detail?.pull_request.Number === number) {
    detail = {
      ...detail,
      pull_request: {
        ...detail.pull_request,
        KanbanStatus: status,
      },
    };
  }
  // Refresh the list
  await loadPulls();
}

export async function submitComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<void> {
  await postComment(owner, name, number, body);
  // Reload detail to show the new comment
  await loadDetail(owner, name, number);
}
```

- [ ] **Step 5: Create sync store**

```typescript
// frontend/src/lib/stores/sync.svelte.ts
import type { SyncStatus } from "../api/types";
import {
  getSyncStatus,
  triggerSync as apiTriggerSync,
} from "../api/client";

let status = $state<SyncStatus | null>(null);
let pollInterval: ReturnType<typeof setInterval> | null = null;

export function getSyncState(): SyncStatus | null {
  return status;
}

export async function refreshSyncStatus(): Promise<void> {
  try {
    status = await getSyncStatus();
  } catch {
    // Silently ignore — server may not be ready
  }
}

export async function triggerSync(): Promise<void> {
  await apiTriggerSync();
  // Poll more frequently until sync completes
  await refreshSyncStatus();
}

export function startPolling(intervalMs = 30_000): void {
  stopPolling();
  refreshSyncStatus();
  pollInterval = setInterval(refreshSyncStatus, intervalMs);
}

export function stopPolling(): void {
  if (pollInterval !== null) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
}
```

- [ ] **Step 6: Verify frontend builds**

```bash
cd /Users/wesm/code/ghboard/frontend && npm run build
```

Expected: builds without errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/
git commit -m "Add frontend API client and reactive stores"
```

---

## Task 11: Frontend List View

**Files:**
- Create: `frontend/src/lib/components/sidebar/PullList.svelte`
- Create: `frontend/src/lib/components/sidebar/PullItem.svelte`
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Create PullItem.svelte**

```svelte
<script lang="ts">
  import type { PullRequest } from "../../api/types";

  interface Props {
    pr: PullRequest;
    selected: boolean;
    onclick: () => void;
  }

  let { pr, selected, onclick }: Props = $props();

  function timeAgo(dateStr: string): string {
    const diff = Date.now() - new Date(dateStr).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  }

  const kanbanColors: Record<string, string> = {
    new: "var(--kanban-new)",
    reviewing: "var(--kanban-reviewing)",
    waiting: "var(--kanban-waiting)",
    awaiting_merge: "var(--kanban-awaiting-merge)",
  };

  const kanbanLabels: Record<string, string> = {
    new: "new",
    reviewing: "reviewing",
    waiting: "waiting",
    awaiting_merge: "merge",
  };
</script>

<button
  class="item"
  class:selected
  {onclick}
>
  <div class="item-top">
    <span class="title">{pr.Title}</span>
  </div>
  <div class="item-bottom">
    <span class="meta">
      #{pr.Number}
      {#if pr.Author}&middot; {pr.Author}{/if}
    </span>
    <span class="right">
      {#if pr.KanbanStatus}
        <span
          class="badge"
          style="background: {kanbanColors[pr.KanbanStatus] ?? 'var(--text-muted)'};"
        >
          {kanbanLabels[pr.KanbanStatus] ?? pr.KanbanStatus}
        </span>
      {/if}
      <span class="time">{timeAgo(pr.LastActivityAt)}</span>
    </span>
  </div>
</button>

<style>
  .item {
    display: block;
    width: 100%;
    text-align: left;
    padding: 8px 12px;
    border-left: 3px solid transparent;
    transition: background 0.08s;
  }
  .item:hover {
    background: var(--bg-surface-hover);
  }
  .item.selected {
    background: var(--bg-surface-hover);
    border-left-color: var(--accent-blue);
  }
  .item-top {
    display: flex;
    align-items: baseline;
    gap: 6px;
  }
  .title {
    font-weight: 500;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .item-bottom {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-top: 2px;
  }
  .meta {
    font-size: 11px;
    color: var(--text-muted);
  }
  .right {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .badge {
    font-size: 10px;
    font-weight: 600;
    color: white;
    padding: 1px 6px;
    border-radius: 3px;
  }
  .time {
    font-size: 11px;
    color: var(--text-muted);
  }
</style>
```

- [ ] **Step 2: Create PullList.svelte**

```svelte
<script lang="ts">
  import PullItem from "./PullItem.svelte";
  import {
    getPulls,
    pullsByRepo,
    isLoading,
    getError,
    loadPulls,
    getSelectedPR,
    selectPR,
    getSearchQuery,
    setSearchQuery,
  } from "../../stores/pulls.svelte";

  let searchInput = $state(getSearchQuery());
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  function onSearchInput(e: Event) {
    const val = (e.target as HTMLInputElement).value;
    searchInput = val;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      setSearchQuery(val);
      loadPulls();
    }, 300);
  }

  $effect(() => {
    loadPulls();
  });

  const selected = $derived(getSelectedPR());
  const grouped = $derived(pullsByRepo());
  const totalCount = $derived(getPulls().length);
</script>

<div class="pull-list">
  <div class="search-bar">
    <input
      type="text"
      placeholder="Filter PRs..."
      value={searchInput}
      oninput={onSearchInput}
    />
    <span class="count">{totalCount}</span>
  </div>

  {#if isLoading() && totalCount === 0}
    <p class="empty">Loading...</p>
  {:else if getError()}
    <p class="empty error">{getError()}</p>
  {:else if totalCount === 0}
    <p class="empty">No pull requests found</p>
  {:else}
    {#each [...grouped.entries()] as [repoName, prs]}
      <div class="repo-group">
        <div class="repo-header">{repoName}</div>
        {#each prs as pr}
          <PullItem
            {pr}
            selected={selected?.owner === pr.repo_owner &&
              selected?.name === pr.repo_name &&
              selected?.number === pr.Number}
            onclick={() =>
              selectPR(
                pr.repo_owner ?? "",
                pr.repo_name ?? "",
                pr.Number,
              )}
          />
        {/each}
      </div>
    {/each}
  {/if}
</div>

<style>
  .pull-list {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-muted);
  }
  .search-bar input {
    flex: 1;
    padding: 4px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    font-size: 12px;
  }
  .search-bar input:focus {
    outline: 2px solid var(--accent-blue);
    outline-offset: -1px;
  }
  .count {
    font-size: 11px;
    color: var(--text-muted);
  }
  .repo-group {
    border-bottom: 1px solid var(--border-muted);
  }
  .repo-header {
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .empty {
    padding: 24px 16px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }
  .error {
    color: var(--accent-red);
  }
</style>
```

- [ ] **Step 3: Update App.svelte to use PullList**

Replace the sidebar placeholder in `App.svelte`:

```svelte
<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import { getView } from "./lib/stores/router.svelte";
  import { getSelectedPR } from "./lib/stores/pulls.svelte";
  import { startPolling, stopPolling } from "./lib/stores/sync.svelte";
  import { onMount } from "svelte";

  onMount(() => {
    startPolling();
    return stopPolling;
  });
</script>

<AppHeader />

<main class="main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        <PullList />
      </aside>
      <section class="detail">
        {#if getSelectedPR()}
          <p style="padding: 16px; color: var(--text-muted)">
            Detail view (coming next task)
          </p>
        {:else}
          <div class="empty-state">
            <p>Select a PR to view details</p>
          </div>
        {/if}
      </section>
    </div>
  {:else}
    <div class="board-layout">
      <p style="padding: 16px; color: var(--text-muted)">
        Board view (coming in Task 13)
      </p>
    </div>
  {/if}
</main>

<style>
  .main {
    flex: 1;
    overflow: hidden;
  }
  .list-layout {
    display: flex;
    height: 100%;
  }
  .sidebar {
    width: 340px;
    border-right: 1px solid var(--border-muted);
    overflow-y: auto;
    background: var(--bg-surface);
    flex-shrink: 0;
  }
  .detail {
    flex: 1;
    overflow-y: auto;
  }
  .board-layout {
    height: 100%;
    overflow-x: auto;
    padding: 16px;
  }
  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-muted);
  }
</style>
```

- [ ] **Step 4: Verify frontend builds**

```bash
cd /Users/wesm/code/ghboard/frontend && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add frontend/
git commit -m "Add PR list sidebar with repo grouping and search"
```

---

## Task 12: Frontend Detail View & Comment Box

**Files:**
- Create: `frontend/src/lib/components/detail/PullDetail.svelte`
- Create: `frontend/src/lib/components/detail/EventTimeline.svelte`
- Create: `frontend/src/lib/components/detail/CommentBox.svelte`
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Create EventTimeline.svelte**

```svelte
<script lang="ts">
  import type { PREvent } from "../../api/types";

  interface Props {
    events: PREvent[];
  }

  let { events }: Props = $props();

  function timeAgo(dateStr: string): string {
    const diff = Date.now() - new Date(dateStr).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  }

  const typeIcons: Record<string, string> = {
    issue_comment: "comment",
    review: "review",
    commit: "commit",
    state_change: "state",
  };
</script>

<div class="timeline">
  {#each events as event}
    <div class="event">
      <div class="event-marker">
        <span class="dot" data-type={event.EventType}></span>
      </div>
      <div class="event-content">
        <div class="event-header">
          <span class="event-type">
            {typeIcons[event.EventType] ?? event.EventType}
          </span>
          <span class="author">{event.Author}</span>
          <span class="sep">&middot;</span>
          <span class="time">{timeAgo(event.CreatedAt)}</span>
        </div>
        {#if event.Summary}
          <div class="summary">{event.Summary}</div>
        {/if}
        {#if event.Body}
          <div class="body">{event.Body}</div>
        {/if}
      </div>
    </div>
  {/each}
  {#if events.length === 0}
    <p class="empty">No activity yet</p>
  {/if}
</div>

<style>
  .timeline {
    padding: 0 0 16px;
  }
  .event {
    display: flex;
    gap: 12px;
    padding: 10px 0;
  }
  .event + .event {
    border-top: 1px solid var(--border-muted);
  }
  .event-marker {
    padding-top: 2px;
  }
  .dot {
    display: block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-muted);
  }
  .dot[data-type="issue_comment"] {
    background: var(--accent-blue);
  }
  .dot[data-type="review"] {
    background: var(--accent-purple);
  }
  .dot[data-type="commit"] {
    background: var(--accent-green);
  }
  .event-content {
    flex: 1;
    min-width: 0;
  }
  .event-header {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
  }
  .event-type {
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    color: var(--text-muted);
  }
  .author {
    font-weight: 500;
  }
  .sep,
  .time {
    color: var(--text-muted);
  }
  .summary {
    font-size: 12px;
    margin-top: 2px;
    color: var(--text-secondary);
  }
  .body {
    font-size: 12px;
    margin-top: 6px;
    padding: 8px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    white-space: pre-wrap;
    line-height: 1.5;
  }
  .empty {
    text-align: center;
    color: var(--text-muted);
    padding: 24px;
  }
</style>
```

- [ ] **Step 2: Create CommentBox.svelte**

```svelte
<script lang="ts">
  import { submitComment } from "../../stores/detail.svelte";

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  let { owner, name, number }: Props = $props();

  let body = $state("");
  let submitting = $state(false);
  let error = $state<string | null>(null);

  async function handleSubmit() {
    if (!body.trim() || submitting) return;
    submitting = true;
    error = null;
    try {
      await submitComment(owner, name, number, body.trim());
      body = "";
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      submitting = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      handleSubmit();
    }
  }
</script>

<div class="comment-box">
  <textarea
    bind:value={body}
    placeholder="Write a comment... (Cmd+Enter to submit)"
    rows="3"
    onkeydown={onKeydown}
  ></textarea>
  {#if error}
    <p class="error">{error}</p>
  {/if}
  <div class="actions">
    <button
      class="submit-btn"
      onclick={handleSubmit}
      disabled={!body.trim() || submitting}
    >
      {submitting ? "Posting..." : "Comment"}
    </button>
  </div>
</div>

<style>
  .comment-box {
    border-top: 1px solid var(--border-muted);
    padding: 12px 0 0;
  }
  textarea {
    width: 100%;
    padding: 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    resize: vertical;
    font-size: 13px;
    line-height: 1.5;
  }
  textarea:focus {
    outline: 2px solid var(--accent-blue);
    outline-offset: -1px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 8px;
  }
  .submit-btn {
    padding: 6px 16px;
    background: var(--accent-blue);
    color: white;
    border-radius: var(--radius-sm);
    font-size: 13px;
    font-weight: 500;
    transition: opacity 0.1s;
  }
  .submit-btn:hover:not(:disabled) {
    opacity: 0.9;
  }
  .submit-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .error {
    font-size: 12px;
    color: var(--accent-red);
    margin-top: 4px;
  }
</style>
```

- [ ] **Step 3: Create PullDetail.svelte**

```svelte
<script lang="ts">
  import EventTimeline from "./EventTimeline.svelte";
  import CommentBox from "./CommentBox.svelte";
  import type { KanbanStatus } from "../../api/types";
  import {
    getDetail,
    isDetailLoading,
    getDetailError,
    loadDetail,
    updateKanbanState,
  } from "../../stores/detail.svelte";

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  let { owner, name, number }: Props = $props();

  $effect(() => {
    loadDetail(owner, name, number);
  });

  const detail = $derived(getDetail());
  const pr = $derived(detail?.pull_request);

  const kanbanOptions: { value: KanbanStatus; label: string }[] = [
    { value: "new", label: "New" },
    { value: "reviewing", label: "Reviewing" },
    { value: "waiting", label: "Waiting" },
    { value: "awaiting_merge", label: "Awaiting Merge" },
  ];

  function timeAgo(dateStr: string): string {
    const diff = Date.now() - new Date(dateStr).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  }

  async function onKanbanChange(e: Event) {
    const val = (e.target as HTMLSelectElement).value as KanbanStatus;
    await updateKanbanState(owner, name, number, val);
  }
</script>

<div class="detail-panel">
  {#if isDetailLoading() && !pr}
    <p class="loading">Loading...</p>
  {:else if getDetailError()}
    <p class="error">{getDetailError()}</p>
  {:else if pr}
    <div class="detail-header">
      <div class="title-row">
        <h2 class="title">{pr.Title}</h2>
        <a class="gh-link" href={pr.URL} target="_blank">&nearr;</a>
      </div>
      <div class="meta-row">
        <span class="repo">{owner}/{name}</span>
        <span class="sep">&middot;</span>
        <span>#{pr.Number}</span>
        <span class="sep">&middot;</span>
        <span>{pr.Author}</span>
        <span class="sep">&middot;</span>
        <span>{timeAgo(pr.CreatedAt)}</span>
      </div>
      <div class="chips">
        {#if pr.IsDraft}
          <span class="chip draft">Draft</span>
        {/if}
        {#if pr.CIStatus}
          <span
            class="chip"
            class:ci-pass={pr.CIStatus === "success"}
            class:ci-fail={pr.CIStatus === "failure"}
            class:ci-pending={pr.CIStatus === "pending"}
          >
            CI: {pr.CIStatus}
          </span>
        {/if}
        {#if pr.ReviewDecision}
          <span class="chip">{pr.ReviewDecision}</span>
        {/if}
        <span class="chip changes">
          +{pr.Additions} &minus;{pr.Deletions}
        </span>
      </div>
      <div class="kanban-row">
        <label class="kanban-label">Status:</label>
        <select
          class="kanban-select"
          value={pr.KanbanStatus || "new"}
          onchange={onKanbanChange}
        >
          {#each kanbanOptions as opt}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
      </div>
    </div>

    {#if pr.Body}
      <div class="body-section">
        <div class="body">{pr.Body}</div>
      </div>
    {/if}

    <div class="timeline-section">
      <h3 class="section-title">Activity</h3>
      <EventTimeline events={detail?.events ?? []} />
    </div>

    <div class="comment-section">
      <CommentBox {owner} {name} {number} />
    </div>
  {/if}
</div>

<style>
  .detail-panel {
    padding: 20px 24px;
    max-width: 800px;
  }
  .loading,
  .error {
    padding: 24px;
    text-align: center;
    color: var(--text-muted);
  }
  .error {
    color: var(--accent-red);
  }
  .detail-header {
    margin-bottom: 16px;
  }
  .title-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .title {
    font-size: 18px;
    font-weight: 600;
    line-height: 1.3;
  }
  .gh-link {
    font-size: 14px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .gh-link:hover {
    color: var(--accent-blue);
  }
  .meta-row {
    font-size: 12px;
    color: var(--text-muted);
    margin-top: 4px;
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .repo {
    font-weight: 500;
    color: var(--text-secondary);
  }
  .chips {
    display: flex;
    gap: 6px;
    margin-top: 8px;
    flex-wrap: wrap;
  }
  .chip {
    font-size: 11px;
    font-weight: 500;
    padding: 2px 8px;
    border-radius: 3px;
    background: var(--bg-inset);
    color: var(--text-secondary);
  }
  .draft {
    background: var(--accent-amber);
    color: white;
  }
  .ci-pass {
    background: var(--accent-green);
    color: white;
  }
  .ci-fail {
    background: var(--accent-red);
    color: white;
  }
  .ci-pending {
    background: var(--accent-amber);
    color: white;
  }
  .changes {
    font-family: var(--font-mono);
  }
  .kanban-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 10px;
  }
  .kanban-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .kanban-select {
    font-size: 12px;
    padding: 3px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
  }
  .body-section {
    margin-bottom: 20px;
  }
  .body {
    font-size: 13px;
    line-height: 1.6;
    padding: 12px;
    background: var(--bg-inset);
    border-radius: var(--radius-md);
    white-space: pre-wrap;
  }
  .timeline-section {
    margin-bottom: 16px;
  }
  .section-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-muted);
    margin-bottom: 8px;
  }
  .comment-section {
    margin-top: 8px;
  }
</style>
```

- [ ] **Step 4: Update App.svelte to use PullDetail**

Replace the detail placeholder:

```svelte
<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import PullDetail from "./lib/components/detail/PullDetail.svelte";
  import { getView } from "./lib/stores/router.svelte";
  import { getSelectedPR } from "./lib/stores/pulls.svelte";
  import { startPolling, stopPolling } from "./lib/stores/sync.svelte";
  import { onMount } from "svelte";

  onMount(() => {
    startPolling();
    return stopPolling;
  });

  const selected = $derived(getSelectedPR());
</script>

<AppHeader />

<main class="main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        <PullList />
      </aside>
      <section class="detail">
        {#if selected}
          <PullDetail
            owner={selected.owner}
            name={selected.name}
            number={selected.number}
          />
        {:else}
          <div class="empty-state">
            <p>Select a PR to view details</p>
          </div>
        {/if}
      </section>
    </div>
  {:else}
    <div class="board-layout">
      <p style="padding: 16px; color: var(--text-muted)">
        Board view (coming next task)
      </p>
    </div>
  {/if}
</main>

<style>
  .main { flex: 1; overflow: hidden; }
  .list-layout { display: flex; height: 100%; }
  .sidebar {
    width: 340px;
    border-right: 1px solid var(--border-muted);
    overflow-y: auto;
    background: var(--bg-surface);
    flex-shrink: 0;
  }
  .detail { flex: 1; overflow-y: auto; }
  .board-layout { height: 100%; overflow-x: auto; padding: 16px; }
  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-muted);
  }
</style>
```

- [ ] **Step 5: Verify frontend builds**

```bash
cd /Users/wesm/code/ghboard/frontend && npm run build
```

- [ ] **Step 6: Commit**

```bash
git add frontend/
git commit -m "Add PR detail view with event timeline and comment box"
```

---

## Task 13: Frontend Kanban Board

**Files:**
- Create: `frontend/src/lib/components/kanban/KanbanCard.svelte`
- Create: `frontend/src/lib/components/kanban/KanbanColumn.svelte`
- Create: `frontend/src/lib/components/kanban/KanbanBoard.svelte`
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Create KanbanCard.svelte**

```svelte
<script lang="ts">
  import type { PullRequest } from "../../api/types";

  interface Props {
    pr: PullRequest;
    onclick: () => void;
  }

  let { pr, onclick }: Props = $props();

  function timeAgo(dateStr: string): string {
    const diff = Date.now() - new Date(dateStr).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  }
</script>

<button class="card" {onclick}>
  <div class="card-title">{pr.Title}</div>
  <div class="card-meta">
    <span class="repo">{pr.repo_owner}/{pr.repo_name}</span>
    <span>#{pr.Number}</span>
  </div>
  <div class="card-footer">
    <span class="author">{pr.Author}</span>
    <span class="time">{timeAgo(pr.LastActivityAt)}</span>
  </div>
</button>

<style>
  .card {
    display: block;
    width: 100%;
    text-align: left;
    padding: 10px;
    background: var(--bg-surface);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-sm);
    transition: box-shadow 0.1s;
    cursor: pointer;
  }
  .card:hover {
    box-shadow: var(--shadow-md);
  }
  .card-title {
    font-size: 13px;
    font-weight: 500;
    line-height: 1.3;
    margin-bottom: 4px;
  }
  .card-meta {
    font-size: 11px;
    color: var(--text-muted);
    display: flex;
    gap: 4px;
  }
  .repo {
    font-weight: 500;
    color: var(--text-secondary);
  }
  .card-footer {
    display: flex;
    justify-content: space-between;
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 6px;
  }
</style>
```

- [ ] **Step 2: Create KanbanColumn.svelte**

```svelte
<script lang="ts">
  import type { PullRequest } from "../../api/types";
  import KanbanCard from "./KanbanCard.svelte";

  interface Props {
    title: string;
    color: string;
    pulls: PullRequest[];
    onSelect: (pr: PullRequest) => void;
  }

  let { title, color, pulls, onSelect }: Props = $props();
</script>

<div class="column">
  <div class="column-header">
    <span class="column-title" style="color: {color};">{title}</span>
    <span class="column-count">{pulls.length}</span>
  </div>
  <div class="column-body">
    {#each pulls as pr}
      <KanbanCard {pr} onclick={() => onSelect(pr)} />
    {/each}
    {#if pulls.length === 0}
      <p class="empty">No PRs</p>
    {/if}
  </div>
</div>

<style>
  .column {
    flex: 1;
    min-width: 260px;
    max-width: 360px;
    display: flex;
    flex-direction: column;
    background: var(--bg-inset);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .column-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border-muted);
  }
  .column-title {
    font-weight: 600;
    font-size: 13px;
  }
  .column-count {
    font-size: 12px;
    color: var(--text-muted);
    background: var(--bg-surface);
    padding: 1px 8px;
    border-radius: 10px;
  }
  .column-body {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .empty {
    text-align: center;
    color: var(--text-muted);
    font-size: 12px;
    padding: 20px;
  }
</style>
```

- [ ] **Step 3: Create KanbanBoard.svelte**

```svelte
<script lang="ts">
  import type { PullRequest, KanbanStatus } from "../../api/types";
  import KanbanColumn from "./KanbanColumn.svelte";
  import { getPulls, loadPulls, selectPR } from "../../stores/pulls.svelte";
  import { setView } from "../../stores/router.svelte";

  $effect(() => {
    loadPulls();
  });

  const columns: { key: KanbanStatus; title: string; color: string }[] = [
    { key: "new", title: "New", color: "var(--kanban-new)" },
    { key: "reviewing", title: "Reviewing", color: "var(--kanban-reviewing)" },
    { key: "waiting", title: "Waiting", color: "var(--kanban-waiting)" },
    {
      key: "awaiting_merge",
      title: "Awaiting Merge",
      color: "var(--kanban-awaiting-merge)",
    },
  ];

  function pullsForStatus(status: KanbanStatus): PullRequest[] {
    return getPulls().filter(
      (pr) => (pr.KanbanStatus || "new") === status,
    );
  }

  function handleSelect(pr: PullRequest) {
    selectPR(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number);
    setView("list");
  }
</script>

<div class="board">
  {#each columns as col}
    <KanbanColumn
      title={col.title}
      color={col.color}
      pulls={pullsForStatus(col.key)}
      onSelect={handleSelect}
    />
  {/each}
</div>

<style>
  .board {
    display: flex;
    gap: 12px;
    height: 100%;
    padding: 16px;
    overflow-x: auto;
  }
</style>
```

- [ ] **Step 4: Update App.svelte to use KanbanBoard**

Replace the board placeholder:

```svelte
<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import PullDetail from "./lib/components/detail/PullDetail.svelte";
  import KanbanBoard from "./lib/components/kanban/KanbanBoard.svelte";
  import { getView } from "./lib/stores/router.svelte";
  import { getSelectedPR } from "./lib/stores/pulls.svelte";
  import { startPolling, stopPolling } from "./lib/stores/sync.svelte";
  import { onMount } from "svelte";

  onMount(() => {
    startPolling();
    return stopPolling;
  });

  const selected = $derived(getSelectedPR());
</script>

<AppHeader />

<main class="main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        <PullList />
      </aside>
      <section class="detail">
        {#if selected}
          <PullDetail
            owner={selected.owner}
            name={selected.name}
            number={selected.number}
          />
        {:else}
          <div class="empty-state">
            <p>Select a PR to view details</p>
          </div>
        {/if}
      </section>
    </div>
  {:else}
    <KanbanBoard />
  {/if}
</main>

<style>
  .main { flex: 1; overflow: hidden; }
  .list-layout { display: flex; height: 100%; }
  .sidebar {
    width: 340px;
    border-right: 1px solid var(--border-muted);
    overflow-y: auto;
    background: var(--bg-surface);
    flex-shrink: 0;
  }
  .detail { flex: 1; overflow-y: auto; }
  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-muted);
  }
</style>
```

- [ ] **Step 5: Verify frontend builds**

```bash
cd /Users/wesm/code/ghboard/frontend && npm run build
```

- [ ] **Step 6: Commit**

```bash
git add frontend/
git commit -m "Add Kanban board view with four status columns"
```

---

## Task 14: Production Build & Smoke Test

**Files:**
- Modify: `Makefile` (already done in Task 1)
- Verify full build pipeline

- [ ] **Step 1: Build the full production binary**

```bash
cd /Users/wesm/code/ghboard && make build
```

Expected: produces `ghboard` binary with embedded frontend.

- [ ] **Step 2: Verify the binary starts (config validation)**

Create a test config:

```bash
cat > /tmp/ghboard-test-config.toml << 'EOF'
sync_interval = "5m"
github_token_env = "GITHUB_TOKEN"
host = "127.0.0.1"
port = 8090
data_dir = "/tmp/ghboard-test-data"

[[repos]]
owner = "test"
name = "repo"
EOF
```

Run with a fake token to verify startup:

```bash
GITHUB_TOKEN=fake ./ghboard -config /tmp/ghboard-test-config.toml &
sleep 1
curl -s http://127.0.0.1:8090/ | head -5
curl -s http://127.0.0.1:8090/api/v1/repos
curl -s http://127.0.0.1:8090/api/v1/sync/status
kill %1
```

Expected: index.html served at `/`, JSON from API endpoints.

- [ ] **Step 3: Run all Go tests**

```bash
cd /Users/wesm/code/ghboard && make test
```

Expected: all tests pass.

- [ ] **Step 4: Run go vet**

```bash
cd /Users/wesm/code/ghboard && make vet
```

Expected: no issues.

- [ ] **Step 5: Clean up test artifacts**

```bash
rm -f /tmp/ghboard-test-config.toml
rm -rf /tmp/ghboard-test-data
```

- [ ] **Step 6: Commit any final fixes**

```bash
git add -A
git commit -m "Verify production build and smoke test"
```
