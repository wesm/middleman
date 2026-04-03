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
	// Create a test repo with two commits on different branches.
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")

	run(t, dir, "git", "init", "--bare", "--initial-branch=main", remote)
	run(t, dir, "git", "clone", remote, work)
	run(t, work, "git", "config", "user.email", "test@test.com")
	run(t, work, "git", "config", "user.name", "Test")

	// Initial commit on main.
	require.NoError(t, os.WriteFile(filepath.Join(work, "hello.go"),
		[]byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "initial")
	run(t, work, "git", "push", "origin", "main")

	// Create a feature branch with changes.
	run(t, work, "git", "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(work, "hello.go"),
		[]byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(work, "new.go"),
		[]byte("package main\n"), 0o644))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "feature changes")
	run(t, work, "git", "push", "origin", "feature")

	// Get SHAs.
	mainSHA := getSHA(t, work, "origin/main")
	featureSHA := getSHA(t, work, "origin/feature")

	// Clone into manager.
	clonesDir := t.TempDir()
	mgr := New(clonesDir, "")
	require.NoError(t, mgr.EnsureClone(context.Background(), "test", "repo", remote))

	// Compute merge base.
	mb, err := mgr.MergeBase(context.Background(), "test", "repo", mainSHA, featureSHA)
	require.NoError(t, err)
	assert.Equal(t, mainSHA, mb) // merge base is the initial commit

	// Run diff.
	result, err := mgr.Diff(context.Background(), "test", "repo", mb, featureSHA, false)
	require.NoError(t, err)
	require.Len(t, result.Files, 2)

	// hello.go should be modified.
	assert.Equal(t, "hello.go", result.Files[0].Path)
	assert.Equal(t, "modified", result.Files[0].Status)
	assert.Equal(t, 1, result.Files[0].Additions)
	assert.Equal(t, 1, result.Files[0].Deletions)

	// new.go should be added.
	assert.Equal(t, "new.go", result.Files[1].Path)
	assert.Equal(t, "added", result.Files[1].Status)
}

func getSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
