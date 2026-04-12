package testutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
)

// DiffRepoResult holds the SHAs from the test repo for use in assertions.
type DiffRepoResult struct {
	BaseSHA      string // merge-base / base branch tip
	HeadSHA      string // PR head commit
	Manager      *gitclone.Manager
	FileCount    int // number of changed files (excluding whitespace-only when hidden)
	AddedFiles   []string
	DeletedFiles []string
}

// SetupDiffRepo creates a git repository with known commits and wires
// it into the database so the diff endpoint works without mocking.
//
// The repo is set up for acme/widgets PR #1. It creates:
//   - Modified file with 2 hunks (internal/handler.go)
//   - Added file (internal/cache.go)
//   - Deleted file (config.yaml)
//   - Whitespace-only change (README.md)
//
// Returns a clone manager and metadata about the created diff.
func SetupDiffRepo(
	ctx context.Context,
	tmpDir string,
	d *db.DB,
) (*DiffRepoResult, error) {
	workDir := filepath.Join(tmpDir, "workrepo")
	cloneBase := filepath.Join(tmpDir, "clones")
	barePath := filepath.Join(
		cloneBase, "github.com", "acme", "widgets.git")

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir work: %w", err)
	}

	// Initialize a working repo and create the base commit.
	if err := git(workDir, "init", "-b", "main"); err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}
	if err := git(workDir,
		"config", "user.email", "test@example.com"); err != nil {
		return nil, err
	}
	if err := git(workDir,
		"config", "user.name", "Test"); err != nil {
		return nil, err
	}

	// --- Base commit files ---

	if err := writeFile(workDir, "main.go", mainGoContent); err != nil {
		return nil, err
	}
	if err := writeFile(workDir,
		"internal/handler.go", handlerGoBase); err != nil {
		return nil, err
	}
	if err := writeFile(workDir,
		"config.yaml", configYAMLContent); err != nil {
		return nil, err
	}
	if err := writeFile(workDir,
		"README.md", readmeBase); err != nil {
		return nil, err
	}

	if err := git(workDir, "add", "-A"); err != nil {
		return nil, err
	}
	if err := git(workDir,
		"commit", "-m", "Initial commit"); err != nil {
		return nil, err
	}

	baseSHA, err := revParse(workDir, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("rev-parse base: %w", err)
	}

	// --- PR branch with changes ---

	if err := git(workDir,
		"checkout", "-b", "feature/caching"); err != nil {
		return nil, err
	}

	// Modify handler.go in two separate locations (produces 2 hunks).
	if err := writeFile(workDir,
		"internal/handler.go", handlerGoHead); err != nil {
		return nil, err
	}
	// Add a new file.
	if err := writeFile(workDir,
		"internal/cache.go", cacheGoContent); err != nil {
		return nil, err
	}
	// Delete config.yaml.
	if err := os.Remove(
		filepath.Join(workDir, "config.yaml")); err != nil {
		return nil, err
	}
	// Whitespace-only change to README.md.
	if err := writeFile(workDir,
		"README.md", readmeHead); err != nil {
		return nil, err
	}

	if err := git(workDir, "add", "-A"); err != nil {
		return nil, err
	}
	if err := git(workDir,
		"commit", "-m", "feat: add caching layer"); err != nil {
		return nil, err
	}

	headSHA, err := revParse(workDir, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("rev-parse head: %w", err)
	}

	// Clone as bare to the path the clone manager expects.
	if err := os.MkdirAll(
		filepath.Dir(barePath), 0o755); err != nil {
		return nil, err
	}
	if err := git("",
		"clone", "--bare", workDir, barePath); err != nil {
		return nil, fmt.Errorf("bare clone: %w", err)
	}

	// Seed database with the real SHAs for acme/widgets PR #1.
	repoID, err := d.UpsertRepo(
		ctx, "github.com", "acme", "widgets")
	if err != nil {
		return nil, fmt.Errorf("upsert repo: %w", err)
	}
	if err := d.UpdateDiffSHAs(
		ctx, repoID, 1, headSHA, baseSHA, baseSHA); err != nil {
		return nil, fmt.Errorf("update diff SHAs: %w", err)
	}
	// Set platform SHAs to match so the diff is not stale.
	if err := d.UpdatePlatformSHAs(
		ctx, repoID, 1, headSHA, baseSHA); err != nil {
		return nil, fmt.Errorf("update platform SHAs: %w", err)
	}

	mgr := gitclone.New(cloneBase, nil)

	return &DiffRepoResult{
		BaseSHA:      baseSHA,
		HeadSHA:      headSHA,
		Manager:      mgr,
		FileCount:    4,
		AddedFiles:   []string{"internal/cache.go"},
		DeletedFiles: []string{"config.yaml"},
	}, nil
}

func git(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Filter out inherited GIT_* repo-context env vars. If this
	// test is run from within a git hook (e.g. the go-test-short
	// pre-commit hook in prek.toml), git sets GIT_DIR/GIT_WORK_TREE
	// etc. to the parent repo. Leaving those in place makes child
	// git commands target the parent repo instead of the temp one,
	// which silently runs the parent's pre-commit hook from the
	// temp cwd and then fails.
	cmd.Env = append(stripGitRepoEnv(os.Environ()),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_DATE=2026-03-28T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-03-28T12:00:00Z",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"git %s: %w: %s", args[0], err, stderr.String())
	}
	return nil
}

// stripGitRepoEnv removes inherited GIT_* repo-context env vars that
// would make a child git command target the parent repo instead of
// its own cwd. Only vars that encode repo location are filtered;
// author/committer/identity vars are preserved.
func stripGitRepoEnv(env []string) []string {
	bad := map[string]struct{}{
		"GIT_DIR":              {},
		"GIT_WORK_TREE":        {},
		"GIT_INDEX_FILE":       {},
		"GIT_OBJECT_DIRECTORY": {},
		"GIT_COMMON_DIR":       {},
		"GIT_NAMESPACE":        {},
		"GIT_PREFIX":           {},
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if _, skip := bad[key]; skip {
			continue
		}
		out = append(out, e)
	}
	return out
}

func revParse(dir, ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func writeFile(baseDir, relPath, content string) error {
	full := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// --- File contents for the test repo ---
// These are kept minimal but realistic enough to produce a
// meaningful diff with hunks, additions, deletions, and context.

const mainGoContent = `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("starting server")
	return nil
}
`

// handlerGoBase has two functions separated by enough lines
// to produce two separate hunks when both are modified.
const handlerGoBase = `package internal

import (
	"fmt"
	"log"
	"net/http"
)

// HandleRequest processes incoming HTTP requests.
func HandleRequest(w http.ResponseWriter, r *http.Request) {
	log.Println("handling request")
	path := r.URL.Path
	if path == "" {
		http.Error(w, "empty path", 400)
		return
	}
	w.WriteHeader(200)
	fmt.Fprintf(w, "OK: %s", path)
}

// unused spacing to separate functions in diff
// line 1
// line 2
// line 3
// line 4
// line 5
// line 6
// line 7
// line 8
// line 9
// line 10

// ProcessEvent handles a single event.
func ProcessEvent(name string) error {
	log.Printf("processing event: %s", name)
	if name == "" {
		return fmt.Errorf("empty event name")
	}
	return nil
}
`

// handlerGoHead modifies both functions (two hunks).
const handlerGoHead = `package internal

import (
	"fmt"
	"log/slog"
	"net/http"
)

// HandleRequest processes incoming HTTP requests.
func HandleRequest(w http.ResponseWriter, r *http.Request) {
	slog.Info("handling request", "method", r.Method, "path", r.URL.Path)
	path := r.URL.Path
	if path == "" {
		http.Error(w, "empty path", 400)
		return
	}
	w.WriteHeader(200)
	fmt.Fprintf(w, "OK: %s", path)
}

// unused spacing to separate functions in diff
// line 1
// line 2
// line 3
// line 4
// line 5
// line 6
// line 7
// line 8
// line 9
// line 10

// ProcessEvent handles a single event.
func ProcessEvent(name string) error {
	slog.Info("processing event", "name", name)
	if name == "" {
		return fmt.Errorf("empty event name")
	}
	fmt.Println("event processed successfully")
	return nil
}
`

const configYAMLContent = `# Application configuration
server:
  port: 8080
  host: localhost

database:
  path: ./data.db
  wal_mode: true
`

const cacheGoContent = `package internal

import (
	"sync"
	"time"
)

// Cache is a simple in-memory key-value cache with TTL.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]entry
	ttl     time.Duration
}

type entry struct {
	value     string
	expiresAt time.Time
}

// NewCache creates a cache with the given TTL.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]entry),
		ttl:     ttl,
	}
}

// Get retrieves a value by key. Returns empty string if
// the key is missing or expired.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.value, true
}

// Set stores a value with the cache's TTL.
func (c *Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}
`

// readmeBase and readmeHead differ only by intra-line whitespace
// (trailing spaces added to some lines, indentation changed).
// git diff -w treats these as identical, making this file whitespace-only.
const readmeBase = `# Widget Service

A simple HTTP service for widget management.

## Getting Started

Run the server with:

    go run .
`

//nolint:dupword // trailing whitespace is intentional for test
const readmeHead = `# Widget Service

A simple HTTP service for widget management.

## Getting Started

Run the server with:

      go run .
`
