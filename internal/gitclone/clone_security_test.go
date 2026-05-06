package gitclone

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteURLHostRejectsMismatchedHTTPSHost(t *testing.T) {
	err := validateRemoteURLHost("github.com", "https://gitlab.com/acme/widget.git")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")
	assert.Contains(t, err.Error(), "github.com")
}

func TestValidateRemoteURLHostAcceptsMatchingHTTPSHost(t *testing.T) {
	err := validateRemoteURLHost("github.com", "https://github.com/acme/widget.git")

	require.NoError(t, err)
}

func TestValidateRemoteURLHostAcceptsLocalPath(t *testing.T) {
	err := validateRemoteURLHost("github.com", "/tmp/acme/widget.git")

	require.NoError(t, err)
}
