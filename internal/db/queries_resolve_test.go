package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveItemNumber(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, "acme", "widget")
	require.NoError(err)

	now := time.Now().UTC().Truncate(time.Second)

	// Seed a PR at number 10
	_, err = database.UpsertPullRequest(ctx, &PullRequest{
		RepoID:         repoID,
		GitHubID:       10000,
		Number:         10,
		URL:            "https://github.com/acme/widget/pull/10",
		Title:          "PR ten",
		Author:         "user",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

	// Seed an issue at number 20
	_, err = database.UpsertIssue(ctx, &Issue{
		RepoID:         repoID,
		GitHubID:       20000,
		Number:         20,
		URL:            "https://github.com/acme/widget/issues/20",
		Title:          "Issue twenty",
		Author:         "user",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

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
