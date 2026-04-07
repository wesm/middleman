package gitclone

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned when a git ref or object cannot be resolved.
var ErrNotFound = errors.New("git object not found")

// Manager manages bare git clones for diff computation.
type Manager struct {
	baseDir string            // directory to store clones
	tokens  map[string]string // host -> token (e.g., "github.com" -> "ghp_...")
}

// New creates a Manager that stores bare clones under baseDir.
// tokens maps each host (e.g., "github.com") to its auth token.
// A nil or empty map means all operations proceed without auth.
func New(baseDir string, tokens map[string]string) *Manager {
	return &Manager{baseDir: baseDir, tokens: tokens}
}

// ClonePath returns the filesystem path for a repo's bare clone.
// Path is partitioned by host: {baseDir}/{host}/{owner}/{name}.git
func (m *Manager) ClonePath(host, owner, name string) string {
	return filepath.Join(m.baseDir, host, owner, name+".git")
}

// EnsureClone creates or fetches a bare clone for the given repo.
// remoteURL is the HTTPS clone URL (e.g., https://github.com/owner/name.git).
// On first call, clones the repo. On subsequent calls, fetches updates.
func (m *Manager) EnsureClone(
	ctx context.Context, host, owner, name, remoteURL string,
) error {
	clonePath := m.ClonePath(host, owner, name)

	if _, err := os.Stat(filepath.Join(clonePath, "HEAD")); os.IsNotExist(err) {
		return m.cloneBare(ctx, host, clonePath, remoteURL)
	}
	m.ensureRefspecs(ctx, host, clonePath)
	return m.fetch(ctx, host, clonePath)
}

// Fetch refspecs configured on every bare clone.
//
//   - branchRefspec is required so `git fetch` updates local refs/heads/*.
//     git clone --bare does NOT install a default fetch refspec, so without
//     this, branch refs stay frozen at initial-clone time and the merge
//     commits of merged PRs never reach the clone.
//   - pullRefspec makes `refs/pull/<N>/head` available, which is how we
//     resolve PR heads that live on forks.
const (
	branchRefspec = "+refs/heads/*:refs/heads/*"
	pullRefspec   = "+refs/pull/*/head:refs/pull/*/head"
)

// defaultRefspecs returns the full list of fetch refspecs every clone should
// have. Used by both cloneBare (fresh clones) and ensureRefspecs (migration).
func defaultRefspecs() []string {
	return []string{branchRefspec, pullRefspec}
}

// ensureRefspecs idempotently adds any missing fetch refspecs to an
// existing clone. This upgrades clones created before branch/pull ref
// support was in place.
func (m *Manager) ensureRefspecs(
	ctx context.Context, host, clonePath string,
) {
	out, err := m.git(ctx, host, clonePath,
		"config", "--get-all", "remote.origin.fetch")
	if err != nil {
		return // can't read config, skip silently
	}
	existing := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			existing[line] = true
		}
	}
	for _, refspec := range defaultRefspecs() {
		if existing[refspec] {
			continue
		}
		if _, err := m.git(ctx, host, clonePath,
			"config", "--add", "remote.origin.fetch", refspec); err != nil {
			slog.Warn("failed to add refspec to existing clone",
				"path", clonePath, "refspec", refspec, "err", err)
		}
	}
}

func (m *Manager) cloneBare(
	ctx context.Context, host, clonePath, remoteURL string,
) error {
	if err := os.MkdirAll(filepath.Dir(clonePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for clone: %w", err)
	}
	slog.Info("cloning bare repo", "path", clonePath)
	_, err := m.git(ctx, host, "", "clone", "--bare", remoteURL, clonePath)
	if err != nil {
		return fmt.Errorf("git clone --bare: %w", err)
	}

	// Install fetch refspecs so future fetches pull both branch heads and
	// pull refs. git clone --bare does not install a default refspec.
	// On failure, remove the partial clone so the next call retries.
	for _, refspec := range defaultRefspecs() {
		if _, err := m.git(ctx, host, clonePath,
			"config", "--add", "remote.origin.fetch", refspec); err != nil {
			os.RemoveAll(clonePath)
			return fmt.Errorf("add fetch refspec %q: %w", refspec, err)
		}
	}

	// Fetch immediately after clone so pull refs are available before
	// merge-base computation runs in the same sync cycle.
	return m.fetch(ctx, host, clonePath)
}

func (m *Manager) fetch(
	ctx context.Context, host, clonePath string,
) error {
	_, err := m.git(ctx, host, clonePath, "fetch", "--prune", "origin")
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}

// RevParse resolves a git ref to its SHA. Returns an empty string if the ref
// does not exist.
func (m *Manager) RevParse(
	ctx context.Context, host, owner, name, ref string,
) (string, error) {
	clonePath := m.ClonePath(host, owner, name)
	out, err := m.git(ctx, host, clonePath, "rev-parse", "--verify", ref)
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MergeBase computes the merge base between two commits.
func (m *Manager) MergeBase(
	ctx context.Context, host, owner, name, sha1, sha2 string,
) (string, error) {
	clonePath := m.ClonePath(host, owner, name)
	out, err := m.git(ctx, host, clonePath, "merge-base", sha1, sha2)
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s: %w", sha1, sha2, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// git runs a git command with auth env vars set for the given host.
func (m *Manager) git(
	ctx context.Context, host, dir string, args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
	)
	if token := m.tokens[host]; token != "" {
		// GitHub's smart HTTP endpoint requires Basic auth, not Bearer.
		// Use "x-access-token" as the username with the token as password.
		cred := base64.StdEncoding.EncodeToString(
			[]byte("x-access-token:" + token))
		cmd.Env = append(cmd.Env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+cred,
		)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := stderr.String()
		if isNotFoundError(msg) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, msg)
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return out, nil
}

// isNotFoundError checks if git stderr indicates a missing object or ref.
func isNotFoundError(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "unknown revision") ||
		strings.Contains(s, "bad object") ||
		strings.Contains(s, "not a valid object name") ||
		strings.Contains(s, "not a valid commit name") ||
		strings.Contains(s, "does not exist")
}
