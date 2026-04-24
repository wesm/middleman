package testutil

import (
	"os"
	"os/exec"
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
