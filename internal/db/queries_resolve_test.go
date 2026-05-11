package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveItemNumber(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	database := openTestDB(t)
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, GitHubRepoIdentity("github.com", "acme", "widget"))
	require.NoError(err)

	// Seed a PR at number 10
	insertTestMRWithOptions(t, database, testMR(repoID, 10, withMRTitle("PR ten")))

	// Seed an issue at number 20
	insertTestIssueWithOptions(t, database, testIssue(repoID, 20, withIssueTitle("Issue twenty")))

	// Resolve PR
	itemType, found, err := database.ResolveItemNumber(ctx, repoID, 10)
	require.NoError(err)
	assert.True(found)
	assert.Equal("pr", itemType)

	// Resolve issue
	itemType, found, err = database.ResolveItemNumber(ctx, repoID, 20)
	require.NoError(err)
	assert.True(found)
	assert.Equal("issue", itemType)

	// Unknown number
	_, found, err = database.ResolveItemNumber(ctx, repoID, 999)
	require.NoError(err)
	assert.False(found)
}
