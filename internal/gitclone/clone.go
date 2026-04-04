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
	baseDir string // directory to store clones (e.g., {dataDir}/clones)
	token   string // GitHub token for auth (may be empty for public repos)
}

// New creates a Manager that stores bare clones under baseDir.
func New(baseDir, token string) *Manager {
	return &Manager{baseDir: baseDir, token: token}
}

// SetToken updates the GitHub token used for authentication.
func (m *Manager) SetToken(token string) {
	m.token = token
}

// ClonePath returns the filesystem path for a repo's bare clone.
func (m *Manager) ClonePath(owner, name string) string {
	return filepath.Join(m.baseDir, owner, name+".git")
}

// EnsureClone creates or fetches a bare clone for the given repo.
// remoteURL is the HTTPS clone URL (e.g., https://github.com/owner/name.git).
// On first call, clones the repo. On subsequent calls, fetches updates.
func (m *Manager) EnsureClone(ctx context.Context, owner, name, remoteURL string) error {
	clonePath := m.ClonePath(owner, name)

	if _, err := os.Stat(filepath.Join(clonePath, "HEAD")); os.IsNotExist(err) {
		return m.cloneBare(ctx, clonePath, remoteURL)
	}
	m.ensurePullRefspec(ctx, clonePath)
	return m.fetch(ctx, clonePath)
}

const pullRefspec = "+refs/pull/*/head:refs/pull/*/head"

// ensurePullRefspec idempotently adds the pull refs fetch refspec to an
// existing clone. This upgrades clones created before pull ref support.
func (m *Manager) ensurePullRefspec(ctx context.Context, clonePath string) {
	out, err := m.git(ctx, clonePath, "config", "--get-all", "remote.origin.fetch")
	if err != nil {
		return // can't read config, skip silently
	}
	if strings.Contains(string(out), "refs/pull/*/head") {
		return // already configured
	}
	if _, err := m.git(ctx, clonePath, "config", "--add", "remote.origin.fetch", pullRefspec); err != nil {
		slog.Warn("failed to add pull refspec to existing clone", "path", clonePath, "err", err)
	}
}

func (m *Manager) cloneBare(ctx context.Context, clonePath, remoteURL string) error {
	if err := os.MkdirAll(filepath.Dir(clonePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for clone: %w", err)
	}
	slog.Info("cloning bare repo", "path", clonePath)
	_, err := m.git(ctx, "", "clone", "--bare", remoteURL, clonePath)
	if err != nil {
		return fmt.Errorf("git clone --bare: %w", err)
	}

	// Add pull refs fetch refspec so we get refs/pull/*/head.
	// On failure, remove the partial clone so the next call retries from scratch.
	_, err = m.git(ctx, clonePath, "config", "--add", "remote.origin.fetch", pullRefspec)
	if err != nil {
		os.RemoveAll(clonePath)
		return fmt.Errorf("add pull refs refspec: %w", err)
	}

	// Fetch immediately after clone so pull refs are available before
	// merge-base computation runs in the same sync cycle.
	return m.fetch(ctx, clonePath)
}

func (m *Manager) fetch(ctx context.Context, clonePath string) error {
	_, err := m.git(ctx, clonePath, "fetch", "--prune", "origin")
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}

// RevParse resolves a git ref to its SHA. Returns an empty string if the ref
// does not exist.
func (m *Manager) RevParse(ctx context.Context, owner, name, ref string) (string, error) {
	clonePath := m.ClonePath(owner, name)
	out, err := m.git(ctx, clonePath, "rev-parse", "--verify", ref)
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MergeBase computes the merge base between two commits.
func (m *Manager) MergeBase(ctx context.Context, owner, name, sha1, sha2 string) (string, error) {
	clonePath := m.ClonePath(owner, name)
	out, err := m.git(ctx, clonePath, "merge-base", sha1, sha2)
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s: %w", sha1, sha2, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// git runs a git command with auth env vars set.
func (m *Manager) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
	)
	if m.token != "" {
		// GitHub's smart HTTP endpoint requires Basic auth, not Bearer.
		// Use "x-access-token" as the username with the token as password.
		cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + m.token))
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
