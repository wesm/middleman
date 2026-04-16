package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowsNewMigration(t *testing.T) {
	isolateGitEnvironment(t)
	repo := initRepoWithMainMigration(t)
	t.Chdir(repo)
	t.Setenv("MIDDLEMAN_MIGRATION_BASE_REF", "main")

	writeFile(t, repo, "internal/db/migrations/000002_next.up.sql", "new\n")
	gitCommand(t, "add", "internal/db/migrations/000002_next.up.sql")

	var stderr bytes.Buffer
	assert.Zero(t, run(&stderr))
	assert.Empty(t, stderr.String())
}

func TestBlocksMainBranchMigrationEdit(t *testing.T) {
	isolateGitEnvironment(t)
	repo := initRepoWithMainMigration(t)
	t.Chdir(repo)
	t.Setenv("MIDDLEMAN_MIGRATION_BASE_REF", "main")

	writeFile(t, repo, "internal/db/migrations/000001_init.up.sql", "changed\n")
	gitCommand(t, "add", "internal/db/migrations/000001_init.up.sql")

	var stderr bytes.Buffer
	assert.Equal(t, 1, run(&stderr))
	assert.Contains(t, stderr.String(), "Refusing to commit edits to migrations")
	assert.Contains(t, stderr.String(), "internal/db/migrations/000001_init.up.sql")
}

func TestBlocksMainBranchMigrationRename(t *testing.T) {
	isolateGitEnvironment(t)
	repo := initRepoWithMainMigration(t)
	t.Chdir(repo)
	t.Setenv("MIDDLEMAN_MIGRATION_BASE_REF", "main")

	gitCommand(t, "mv", "internal/db/migrations/000001_init.up.sql", "internal/db/migrations/000001_renamed.up.sql")

	var stderr bytes.Buffer
	assert.Equal(t, 1, run(&stderr))
	assert.Contains(t, stderr.String(), "internal/db/migrations/000001_init.up.sql")
}

func initRepoWithMainMigration(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	migrationPath := filepath.Join(repo, "internal/db/migrations/000001_init.up.sql")
	require.NoError(t, os.MkdirAll(filepath.Dir(migrationPath), 0o755))
	require.NoError(t, os.WriteFile(migrationPath, []byte("old\n"), 0o644))

	gitCommandIn(t, repo, "init", "-q", "-b", "main")
	gitCommandIn(t, repo, "config", "user.email", "test@example.com")
	gitCommandIn(t, repo, "config", "user.name", "Test")
	gitCommandIn(t, repo, "add", ".")
	gitCommandIn(t, repo, "commit", "-qm", "init")
	gitCommandIn(t, repo, "checkout", "-qb", "feature")

	return repo
}

func writeFile(t *testing.T, repo, path, content string) {
	t.Helper()

	fullPath := filepath.Join(repo, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}

func gitCommand(t *testing.T, args ...string) {
	t.Helper()
	gitCommandIn(t, "", args...)
}

func gitCommandIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitArgs := append([]string{"-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

func isolateGitEnvironment(t *testing.T) {
	t.Helper()

	originalGitEnv := gitEnv
	gitEnv = cleanGitEnv(os.Environ())
	t.Cleanup(func() {
		gitEnv = originalGitEnv
	})
}

func cleanGitEnv(env []string) []string {
	cleaned := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == "GIT_DIR" || key == "GIT_WORK_TREE" || strings.HasPrefix(key, "GIT_") {
			continue
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned
}
