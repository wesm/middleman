package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderMetadataForBuiltIns(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	github, ok := MetadataFor(KindGitHub)
	require.True(ok)
	assert.Equal(DefaultGitHubHost, github.DefaultHost)
	assert.False(github.AllowNestedOwner)
	assert.True(github.LowercaseRepoNames)

	gitlab, ok := MetadataFor(KindGitLab)
	require.True(ok)
	assert.Equal(DefaultGitLabHost, gitlab.DefaultHost)
	assert.True(gitlab.AllowNestedOwner)
}

func TestNormalizeKindAllowsFutureProviderKinds(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	kind, err := NormalizeKind("Codeberg")
	require.NoError(err)
	assert.Equal(Kind("codeberg"), kind)
	assert.True(AllowsNestedOwner(kind))

	_, ok := DefaultHost(kind)
	assert.False(ok)
}
