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

// setupTestRepo creates a bare "remote" repo with one commit and returns
// both the remote path and the working clone path (for follow-up pushes).
func setupTestRepo(t *testing.T) (remote, work string) {
	t.Helper()
	dir := t.TempDir()
	remote = filepath.Join(dir, "remote.git")
	run(t, dir, "git", "init", "--bare", "--initial-branch=main", remote)

	work = filepath.Join(dir, "work")
	run(t, dir, "git", "clone", remote, work)
	run(t, work, "git", "config", "user.email", "test@test.com")
	run(t, work, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(work, "hello.go"), []byte("package main\n"), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "initial")
	run(t, work, "git", "push", "origin", "main")
	return remote, work
}

// commitAndPush creates a new commit on main in the given working clone
// and pushes it to origin. Returns the new commit SHA.
func commitAndPush(t *testing.T, work, file, content, msg string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(work, file), []byte(content), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", msg)
	run(t, work, "git", "push", "origin", "main")
	out, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
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
	remote, _ := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, nil)

	ctx := context.Background()
	err := mgr.EnsureClone(ctx, "github.com", "testowner", "testrepo", remote)
	require.NoError(t, err)

	clonePath := filepath.Join(
		clonesDir, "github.com", "testowner", "testrepo.git")
	assert.DirExists(t, clonePath)

	// Second call should be a no-op fetch, not re-clone.
	err = mgr.EnsureClone(ctx, "github.com", "testowner", "testrepo", remote)
	require.NoError(t, err)
}

// TestEnsureCloneInstallsBothRefspecs verifies that a fresh clone gets both
// the branch and pull refspecs configured. Without the branch refspec,
// git fetch never updates refs/heads/* and merge commits of merged PRs
// never reach the clone.
func TestEnsureCloneInstallsBothRefspecs(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	remote, _ := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, nil)

	ctx := context.Background()
	require.NoError(mgr.EnsureClone(
		ctx, "github.com", "testowner", "testrepo", remote))

	clonePath := mgr.ClonePath("github.com", "testowner", "testrepo")
	refspecs := getFetchRefspecs(t, clonePath)
	assert.Contains(refspecs, "+refs/heads/*:refs/heads/*")
	assert.Contains(refspecs, "+refs/pull/*/head:refs/pull/*/head")
}

// TestEnsureCloneFetchesNewBranchCommits is the regression test for the bug
// where a merged PR's merge commit was never fetched into the bare clone
// because git clone --bare sets no default fetch refspec.
func TestEnsureCloneFetchesNewBranchCommits(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	remote, work := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, nil)

	ctx := context.Background()
	require.NoError(mgr.EnsureClone(
		ctx, "github.com", "testowner", "testrepo", remote))

	// Push a new commit to the remote after the initial clone.
	newSHA := commitAndPush(t, work, "second.go", "package main\n", "second")

	// Re-run EnsureClone and verify the new commit is now reachable.
	require.NoError(mgr.EnsureClone(
		ctx, "github.com", "testowner", "testrepo", remote))

	got, err := mgr.RevParse(ctx, "github.com", "testowner", "testrepo", newSHA)
	require.NoError(err)
	assert.Equal(newSHA, got)
}

// TestEnsureCloneMigratesBrokenClone simulates a clone created by the
// previous version of cloneBare (only pull refspec, no branch refspec) and
// verifies ensureRefspecs migrates it so branch fetches work again.
func TestEnsureCloneMigratesBrokenClone(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	remote, work := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, nil)

	ctx := context.Background()
	require.NoError(mgr.EnsureClone(
		ctx, "github.com", "testowner", "testrepo", remote))

	// Simulate a pre-fix clone: unset all fetch refspecs, then add only
	// the pull refspec back. This matches the state created by the old
	// cloneBare which never installed a branch refspec.
	clonePath := mgr.ClonePath("github.com", "testowner", "testrepo")
	run(t, clonePath, "git", "config", "--unset-all", "remote.origin.fetch")
	run(t, clonePath, "git", "config", "--add",
		"remote.origin.fetch", "+refs/pull/*/head:refs/pull/*/head")
	refspecs := getFetchRefspecs(t, clonePath)
	require.NotContains(refspecs, "+refs/heads/*:refs/heads/*")
	require.Contains(refspecs, "+refs/pull/*/head:refs/pull/*/head")

	// Push a new commit that would be invisible without the branch refspec.
	newSHA := commitAndPush(t, work, "third.go", "package main\n", "third")

	// Next EnsureClone should re-add the branch refspec and fetch the commit.
	require.NoError(mgr.EnsureClone(
		ctx, "github.com", "testowner", "testrepo", remote))

	refspecs = getFetchRefspecs(t, clonePath)
	assert.Contains(refspecs, "+refs/heads/*:refs/heads/*")
	assert.Contains(refspecs, "+refs/pull/*/head:refs/pull/*/head")

	got, err := mgr.RevParse(ctx, "github.com", "testowner", "testrepo", newSHA)
	require.NoError(err)
	assert.Equal(newSHA, got)
}

// getFetchRefspecs returns the current fetch refspecs configured for the
// "origin" remote in a bare clone.
func getFetchRefspecs(t *testing.T, clonePath string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", clonePath,
		"config", "--get-all", "remote.origin.fetch")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	require.NoError(t, err)
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}

func TestMergeBase(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	remote, _ := setupTestRepo(t)
	clonesDir := t.TempDir()
	mgr := New(clonesDir, nil)

	ctx := context.Background()
	require.NoError(mgr.EnsureClone(
		ctx, "github.com", "testowner", "testrepo", remote))

	// Get the HEAD SHA.
	clonePath := mgr.ClonePath("github.com", "testowner", "testrepo")
	out, err := exec.Command(
		"git", "-C", clonePath, "rev-parse", "HEAD").Output()
	require.NoError(err)
	headSHA := strings.TrimSpace(string(out))

	// Merge base of HEAD with itself is HEAD.
	mb, err := mgr.MergeBase(
		ctx, "github.com", "testowner", "testrepo", headSHA, headSHA)
	require.NoError(err)
	assert.Equal(headSHA, mb)
}

func TestClonePathIncludesHost(t *testing.T) {
	mgr := New("/tmp/clones", nil)

	path := mgr.ClonePath("github.com", "owner", "repo")
	assert.Equal(t,
		filepath.Join("/tmp/clones", "github.com", "owner", "repo.git"),
		path)

	// Different host produces a different path.
	ghePath := mgr.ClonePath("github.example.com", "owner", "repo")
	assert.Equal(t,
		filepath.Join("/tmp/clones", "github.example.com", "owner", "repo.git"),
		ghePath)
	assert.NotEqual(t, path, ghePath)
}
