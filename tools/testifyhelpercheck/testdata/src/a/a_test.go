package a

import (
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedsHelper(t *testing.T) {
	require.NoError(t, nil)
	require.NotNil(t, &struct{}{})
	Assert.Equal(t, 1, 1)
	Assert.True(t, true) // want "test has 4 direct testify package calls; create a local assert helper with assert := Assert.New\\(t\\) and use it for repeated checks"
}

func TestHasHelper(t *testing.T) {
	assert := Assert.New(t)
	require.NoError(t, nil)
	require.NotNil(t, &struct{}{})
	assert.Equal(1, 1)
	assert.True(true)
}

func TestSubtestNeedsHelper(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		require.NoError(t, nil)
		require.NotNil(t, &struct{}{})
		Assert.Equal(t, 1, 1)
		Assert.True(t, true) // want "test has 4 direct testify package calls; create a local assert helper with assert := Assert.New\\(t\\) and use it for repeated checks"
	})
}

func TestNeedsRequireHelper(t *testing.T) {
	require.NoError(t, nil)
	require.NotNil(t, &struct{}{})
	require.NoError(t, nil)
	require.NotNil(t, &struct{}{}) // want "test has 4 direct testify package calls; create a local require helper with require := require.New\\(t\\) and use it for repeated checks"
}

func TestHasRequireHelper(t *testing.T) {
	require := require.New(t)
	require.NoError(nil)
	require.NotNil(&struct{}{})
	require.NoError(nil)
	require.NotNil(&struct{}{})
}
