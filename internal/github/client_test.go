package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Compile-time assertion that liveClient satisfies Client.
var _ Client = (*liveClient)(nil)

func TestNewClientReturnsNonNil(t *testing.T) {
	c := NewClient("fake-token", nil)
	require.NotNil(t, c)
}
