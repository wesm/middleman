package middleman

import (
	"os"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeDevServicesUseEntrypointScripts(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	content, err := os.ReadFile("compose.yml")
	require.NoError(err)

	compose := string(content)
	assert.Contains(compose, "/app/docker/backend-dev-entrypoint.sh")
	assert.Contains(compose, "/app/docker/frontend-dev-entrypoint.sh")
	assert.NotContains(compose, "make dev")
	assert.NotContains(compose, "make frontend-dev BUN_INSTALL_FLAGS=--frozen-lockfile ARGS=\"--host 0.0.0.0 --port 15173\"")
}
