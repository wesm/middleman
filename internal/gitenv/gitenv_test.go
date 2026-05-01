package gitenv

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripInheritedBlockedVars(t *testing.T) {
	blocked := []string{
		// Repo context.
		"GIT_DIR=/tmp/x",
		"GIT_WORK_TREE=/tmp/x",
		"GIT_INDEX_FILE=/tmp/x",
		"GIT_OBJECT_DIRECTORY=/tmp/x",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/tmp/x",
		"GIT_COMMON_DIR=/tmp/x",
		"GIT_NAMESPACE=ns",
		"GIT_PREFIX=sub/",
		// Config family.
		"GIT_CONFIG=/tmp/x",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic abc",
		"GIT_CONFIG_PARAMETERS='http.extraHeader=X: y'",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		// Identity.
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2026-03-28T12:00:00Z",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_COMMITTER_DATE=2026-03-28T12:00:00Z",
		// Credential/interactive.
		"GIT_ASKPASS=/bin/false",
		"GIT_SSH_COMMAND=/bin/false",
		"SSH_ASKPASS=/bin/false",
	}

	out := StripInherited(blocked)
	assert.Empty(t, out, "StripInherited left inherited vars through: %v", out)
}

func TestStripInheritedPreservesNonInherited(t *testing.T) {
	preserved := []string{
		"HOME=/Users/test",
		"PATH=/usr/bin",
		"HTTPS_PROXY=http://proxy:8080",
		"GIT_SSL_CAINFO=/etc/ssl/cert.pem",
		"GIT_SSL_NO_VERIFY=1",
		"GIT_PROXY_COMMAND=/usr/bin/proxy",
		"GIT_HTTP_LOW_SPEED_LIMIT=1000",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TRACE=1",
		"GIT_TRACE_PACKET=1",
	}

	out := StripInherited(preserved)
	assert.Equal(t, preserved, out)
}

func TestStripInheritedMixed(t *testing.T) {
	r := require.New(t)
	env := []string{
		"PATH=/usr/bin",
		"GIT_DIR=/tmp/parent/.git",
		"HOME=/Users/test",
		"GIT_CONFIG_GLOBAL=/tmp/global",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Leaked",
	}

	out := StripInherited(env)
	r.Len(out, 3)
	r.Contains(out, "PATH=/usr/bin")
	r.Contains(out, "HOME=/Users/test")
	r.Contains(out, "GIT_TERMINAL_PROMPT=0")
}

func TestStripAllRemovesEveryGitVar(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/Users/test",
		"GIT_DIR=/tmp/x",
		"GIT_TRACE=1",
		"GIT_SSL_CAINFO=/etc/ssl/cert.pem",
		"GIT_DEFAULT_HASH=sha256",
		"GIT_TERMINAL_PROMPT=0",
		"SSH_ASKPASS=/bin/false",
		"HTTPS_PROXY=http://proxy:8080",
	}

	out := StripAll(env)
	assert := assert.New(t)
	assert.Len(out, 3)
	assert.Contains(out, "PATH=/usr/bin")
	assert.Contains(out, "HOME=/Users/test")
	assert.Contains(out, "HTTPS_PROXY=http://proxy:8080")
}

func TestStripNilReturnsExplicitEmptyEnv(t *testing.T) {
	assert := assert.New(t)

	inherited := StripInherited(nil)
	assert.NotNil(inherited)
	assert.Empty(inherited)

	all := StripAll(nil)
	assert.NotNil(all)
	assert.Empty(all)
}
