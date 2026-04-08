package gitclone

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBlockedEnvVar(t *testing.T) {
	blocked := []string{
		// Worktree/index
		"GIT_DIR=/tmp/x",
		"GIT_WORK_TREE=/tmp/x",
		"GIT_INDEX_FILE=/tmp/x",
		"GIT_OBJECT_DIRECTORY=/tmp/x",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/tmp/x",
		// Config family
		"GIT_CONFIG=/tmp/x",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic abc",
		"GIT_CONFIG_PARAMETERS='http.extraHeader=X: y'",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		// Credential/interactive
		"GIT_ASKPASS=/bin/false",
		"GIT_SSH_COMMAND=/bin/false",
		"SSH_ASKPASS=/bin/false",
	}

	allowed := []string{
		"HOME=/Users/test",
		"PATH=/usr/bin",
		"GIT_SSL_CAINFO=/etc/ssl/cert.pem",
		"GIT_SSL_NO_VERIFY=1",
		"GIT_PROXY_COMMAND=/usr/bin/proxy",
		"GIT_HTTP_LOW_SPEED_LIMIT=1000",
		"GIT_TERMINAL_PROMPT=0",
		"HTTPS_PROXY=http://proxy:8080",
	}

	for _, e := range blocked {
		assert.True(t, isBlockedEnvVar(e), "should block %s", e)
	}
	for _, e := range allowed {
		assert.False(t, isBlockedEnvVar(e), "should allow %s", e)
	}
}
