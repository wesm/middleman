package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/gitenv"
)

func GitEnv() []string {
	return append(gitenv.StripAll(os.Environ()),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
}

func RunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = GitEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

func RunGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = GitEnv()
	out, err := cmd.Output()
	require.NoError(t, err, "git %v failed", args)
	return strings.TrimSpace(string(out))
}

func GitSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	return RunGitOutput(t, dir, "rev-parse", ref)
}

type GitRepoFixture struct {
	Dir       string
	Remote    string
	Work      string
	ClonesDir string
	BarePath  string
}

type GitRepoOptions struct {
	InitialBranch string
	UserName      string
	UserEmail     string
}

func NewGitRepoFixture(t *testing.T, opts GitRepoOptions) *GitRepoFixture {
	t.Helper()
	if opts.InitialBranch == "" {
		opts.InitialBranch = "main"
	}
	if opts.UserName == "" {
		opts.UserName = "Test"
	}
	if opts.UserEmail == "" {
		opts.UserEmail = "test@test.com"
	}
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	RunGit(t, dir, "init", "--bare", "--initial-branch="+opts.InitialBranch, remote)
	work := filepath.Join(dir, "work")
	RunGit(t, dir, "clone", remote, work)
	RunGit(t, work, "config", "user.email", opts.UserEmail)
	RunGit(t, work, "config", "user.name", opts.UserName)
	return &GitRepoFixture{Dir: dir, Remote: remote, Work: work, ClonesDir: filepath.Join(dir, "clones"), BarePath: remote}
}

func (r *GitRepoFixture) CommitFile(t *testing.T, relPath, content, message string) string {
	t.Helper()
	path := filepath.Join(r.Work, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	RunGit(t, r.Work, "add", ".")
	RunGit(t, r.Work, "commit", "-m", message)
	return GitSHA(t, r.Work, "HEAD")
}

func (r *GitRepoFixture) CheckoutNewBranch(t *testing.T, branch string) {
	t.Helper()
	RunGit(t, r.Work, "checkout", "-b", branch)
}

func (r *GitRepoFixture) Push(t *testing.T, ref string) {
	t.Helper()
	RunGit(t, r.Work, "push", "origin", ref)
}
