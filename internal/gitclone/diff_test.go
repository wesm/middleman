//go:build integration

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

func TestDiff(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	// Create a test repo with two commits on different branches.
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")

	run(t, dir, "git", "init", "--bare", "--initial-branch=main", remote)
	run(t, dir, "git", "clone", remote, work)
	run(t, work, "git", "config", "user.email", "test@test.com")
	run(t, work, "git", "config", "user.name", "Test")

	// Initial commit on main.
	require.NoError(os.WriteFile(filepath.Join(work, "hello.go"),
		[]byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "initial")
	run(t, work, "git", "push", "origin", "main")

	// Create a feature branch with changes.
	run(t, work, "git", "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(work, "hello.go"),
		[]byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n"), 0o644))
	require.NoError(os.WriteFile(filepath.Join(work, "new.go"),
		[]byte("package main\n"), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "feature changes")
	run(t, work, "git", "push", "origin", "feature")

	// Get SHAs.
	mainSHA := getSHA(t, work, "origin/main")
	featureSHA := getSHA(t, work, "origin/feature")

	// Clone into manager.
	clonesDir := t.TempDir()
	mgr := New(clonesDir, nil)
	require.NoError(mgr.EnsureClone(
		context.Background(), "github.com", "test", "repo", remote))

	// Compute merge base.
	mb, err := mgr.MergeBase(
		context.Background(), "github.com", "test", "repo",
		mainSHA, featureSHA)
	require.NoError(err)
	assert.Equal(mainSHA, mb) // merge base is the initial commit

	// Run diff.
	result, err := mgr.Diff(
		context.Background(), "github.com", "test", "repo",
		mb, featureSHA, false)
	require.NoError(err)
	require.Len(result.Files, 2)

	// hello.go should be modified.
	assert.Equal("hello.go", result.Files[0].Path)
	assert.Equal("modified", result.Files[0].Status)
	assert.Equal(1, result.Files[0].Additions)
	assert.Equal(1, result.Files[0].Deletions)

	// new.go should be added.
	assert.Equal("new.go", result.Files[1].Path)
	assert.Equal("added", result.Files[1].Status)
}

func getSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", ref)
	cmd.Env = filteredGitEnv()
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
