package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Compile-time assertion that liveClient satisfies Client.
var _ Client = (*liveClient)(nil)

func TestNewClientReturnsNonNil(t *testing.T) {
	c, err := NewClient("fake-token", "", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientEnterprise(t *testing.T) {
	c, err := NewClient("test-token", "github.mycompany.com", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientGitHubDotCom(t *testing.T) {
	c, err := NewClient("test-token", "github.com", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientEmptyHost(t *testing.T) {
	c, err := NewClient("test-token", "", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}
