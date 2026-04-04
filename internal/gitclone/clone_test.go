package gitclone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRepo creates a bare "remote" repo and returns its path.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	run(t, dir, "git", "init", "--bare", "--initial-branch=main", remote)

	// Create a working clone, add a commit, push.
	work := filepath.Join(dir, "work")
	run(t, dir, "git", "clone", remote, work)
	run(t, work, "git", "config", "user.email", "test@test.com")
	run(t, work, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(work, "hello.go"), []byte("package main\n"), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "initial")
	run(t, work, "git", "push", "origin", "main")
	return remote
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, out)
}

func TestEnsureClone(t *testing.T) {
	remote := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, "")

	ctx := context.Background()
	err := mgr.EnsureClone(ctx, "testowner", "testrepo", remote)
	require.NoError(t, err)

	clonePath := filepath.Join(clonesDir, "testowner", "testrepo.git")
	assert.DirExists(t, clonePath)

	// Second call should be a no-op fetch, not re-clone.
	err = mgr.EnsureClone(ctx, "testowner", "testrepo", remote)
	require.NoError(t, err)
}

func TestMergeBase(t *testing.T) {
	remote := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, "")

	ctx := context.Background()
	require.NoError(t, mgr.EnsureClone(ctx, "testowner", "testrepo", remote))

	// Get the HEAD SHA.
	clonePath := mgr.ClonePath("testowner", "testrepo")
	out, err := exec.Command("git", "-C", clonePath, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	headSHA := strings.TrimSpace(string(out))

	// Merge base of HEAD with itself is HEAD.
	mb, err := mgr.MergeBase(ctx, "testowner", "testrepo", headSHA, headSHA)
	require.NoError(t, err)
	assert.Equal(t, headSHA, mb)
}
