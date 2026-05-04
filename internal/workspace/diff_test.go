package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorktreeDiffFilesAgainstHead(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	work := setupDivergenceWorktree(t)

	require.NoError(os.WriteFile(
		filepath.Join(work, "f.txt"), []byte("dirty\n"), 0o644,
	))
	require.NoError(os.WriteFile(
		filepath.Join(work, "dirty-test.txt"), []byte("test\n"), 0o644,
	))

	files, ok, err := WorktreeDiffFiles(
		t.Context(), work, WorktreeDiffBaseHead, false,
	)
	require.NoError(err)
	require.True(ok)
	require.Len(files, 2)
	assert.Equal("f.txt", files[0].Path)
	assert.Equal("modified", files[0].Status)
	assert.Equal(1, files[0].Additions)
	assert.Equal(1, files[0].Deletions)
	assert.Equal("dirty-test.txt", files[1].Path)
	assert.Equal("added", files[1].Status)
}

func TestWorktreeDiffFilesHidesWhitespaceOnlyChanges(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	work := setupDivergenceWorktree(t)

	require.NoError(os.WriteFile(
		filepath.Join(work, "f.txt"), []byte("f1  \n"), 0o644,
	))
	require.NoError(os.WriteFile(
		filepath.Join(work, "dirty-test.txt"), []byte("test\n"), 0o644,
	))

	files, ok, err := WorktreeDiffFiles(
		t.Context(), work, WorktreeDiffBaseHead, true,
	)
	require.NoError(err)
	require.True(ok)
	require.Len(files, 1)
	assert.Equal("dirty-test.txt", files[0].Path)
}

func TestWorktreeDiffFilesHidesWhitespaceOnlyUntrackedFiles(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	work := setupDivergenceWorktree(t)

	require.NoError(os.WriteFile(
		filepath.Join(work, "dirty-test.txt"), []byte("test\n"), 0o644,
	))
	require.NoError(os.WriteFile(
		filepath.Join(work, "z-blank.txt"), []byte(" \t\n"), 0o644,
	))

	files, ok, err := WorktreeDiffFiles(
		t.Context(), work, WorktreeDiffBaseHead, true,
	)
	require.NoError(err)
	require.True(ok)
	require.Len(files, 1)
	assert.Equal("dirty-test.txt", files[0].Path)
}

func TestWorktreeDiffAgainstUpstreamIncludesLocalCommitsAndDirtyChanges(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	work := setupDivergenceWorktree(t)

	require.NoError(os.WriteFile(
		filepath.Join(work, "committed.go"), []byte("package committed\n"), 0o644,
	))
	runWorkspaceTestGit(t, work, "add", ".")
	runWorkspaceTestGit(t, work, "commit", "-m", "local commit")
	require.NoError(os.WriteFile(
		filepath.Join(work, "dirty.go"), []byte("package dirty\n"), 0o644,
	))

	diff, ok, err := WorktreeDiff(
		t.Context(), work, WorktreeDiffBaseUpstream, false,
	)
	require.NoError(err)
	require.True(ok)
	require.NotNil(diff)

	paths := make([]string, 0, len(diff.Files))
	for _, file := range diff.Files {
		paths = append(paths, file.Path)
	}
	assert.Contains(paths, "committed.go")
	assert.Contains(paths, "dirty.go")
	assert.Equal(0, diff.WhitespaceOnlyCount)
}

func TestWorktreeDiffAgainstUpstreamWithoutTrackingBranch(t *testing.T) {
	require := require.New(t)
	root := t.TempDir()
	work := filepath.Join(root, "work")
	runWorkspaceTestGit(t, root, "init", "--initial-branch=main", work)
	runWorkspaceTestGit(t, work, "config", "user.email", "t@test.com")
	runWorkspaceTestGit(t, work, "config", "user.name", "Test")
	require.NoError(os.WriteFile(
		filepath.Join(work, "x.txt"), []byte("x\n"), 0o644,
	))
	runWorkspaceTestGit(t, work, "add", ".")
	runWorkspaceTestGit(t, work, "commit", "-m", "init")

	diff, ok, err := WorktreeDiff(
		t.Context(), work, WorktreeDiffBaseUpstream, false,
	)
	require.NoError(err)
	require.False(ok)
	require.Nil(diff)
}

func TestWorktreeDiffRendersUntrackedSymlinkTarget(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	root := t.TempDir()
	work := filepath.Join(root, "work")
	secret := filepath.Join(root, "secret.txt")
	runWorkspaceTestGit(t, root, "init", "--initial-branch=main", work)
	runWorkspaceTestGit(t, work, "config", "user.email", "t@test.com")
	runWorkspaceTestGit(t, work, "config", "user.name", "Test")
	require.NoError(os.WriteFile(
		filepath.Join(work, "tracked.txt"), []byte("tracked\n"), 0o644,
	))
	runWorkspaceTestGit(t, work, "add", ".")
	runWorkspaceTestGit(t, work, "commit", "-m", "init")
	require.NoError(os.WriteFile(secret, []byte("do not expose\n"), 0o644))
	require.NoError(os.Symlink(secret, filepath.Join(work, "secret-link")))

	diff, ok, err := WorktreeDiff(
		t.Context(), work, WorktreeDiffBaseHead, false,
	)
	require.NoError(err)
	require.True(ok)
	require.NotNil(diff)
	require.Len(diff.Files, 1)
	require.Len(diff.Files[0].Hunks, 1)

	file := diff.Files[0]
	assert.Equal("secret-link", file.Path)
	assert.Equal("added", file.Status)
	assert.Equal(1, file.Additions)
	assert.False(file.IsBinary)
	require.Len(file.Hunks[0].Lines, 1)
	assert.Equal(secret, file.Hunks[0].Lines[0].Content)
	assert.True(file.Hunks[0].Lines[0].NoNewline)
	assert.NotContains(file.Hunks[0].Lines[0].Content, "do not expose")
}

func TestWorktreeDiffMarksLargeUntrackedFileBinary(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	work := setupDivergenceWorktree(t)
	require.NoError(os.WriteFile(
		filepath.Join(work, "large.txt"),
		bytes.Repeat([]byte("x"), maxUntrackedTextFileBytes+1),
		0o644,
	))

	diff, ok, err := WorktreeDiff(
		t.Context(), work, WorktreeDiffBaseHead, false,
	)
	require.NoError(err)
	require.True(ok)
	require.NotNil(diff)
	require.Len(diff.Files, 1)

	file := diff.Files[0]
	assert.Equal("large.txt", file.Path)
	assert.True(file.IsBinary)
	assert.Zero(file.Additions)
	assert.Empty(file.Hunks)
}
