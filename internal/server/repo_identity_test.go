package server

import (
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ghclient "github.com/wesm/middleman/internal/github"
)

func TestRepositoryIdentityRequiresPlatformHostWhenOwnerNameIsAmbiguous(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	srv, database := setupTestServer(t)
	ctx := t.Context()
	_, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	gheID, err := database.UpsertRepo(ctx, "ghe.example.com", "acme", "widget")
	require.NoError(err)

	repo, err := srv.repoIdentity().LookupRepo(ctx, repoIdentityRef{
		owner: "acme",
		name:  "widget",
	}, repoLookupRequireUnambiguousOwnerName)
	require.ErrorIs(err, errRepoAmbiguous)
	assert.Nil(repo)

	repo, err = srv.repoIdentity().LookupRepo(ctx, repoIdentityRef{
		owner:        "acme",
		name:         "widget",
		platformHost: "ghe.example.com",
	}, repoLookupRequireUnambiguousOwnerName)
	require.NoError(err)
	require.NotNil(repo)
	assert.Equal(gheID, repo.ID)
	assert.Equal("ghe.example.com", repo.PlatformHost)
}

func TestRepositoryIdentityResolvesItemsByRepoIDAfterHostSelection(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	srv, database := setupTestServerWithRepos(t, &mockGH{}, []ghclient.RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
		{Owner: "acme", Name: "widget", PlatformHost: "ghe.example.com"},
	})
	githubIssueID := seedIssueOnHost(
		t, database, "github.com", "acme", "widget", 5, "open", "GitHub issue",
	)
	gheIssueID := seedIssueOnHost(
		t, database, "ghe.example.com", "acme", "widget", 5, "open", "GHE issue",
	)

	_, issue, err := srv.repoIdentity().LookupIssue(t.Context(), repoNumberPathRef{
		owner:        "acme",
		name:         "widget",
		number:       5,
		platformHost: "ghe.example.com",
	})
	require.NoError(err)
	require.NotNil(issue)
	assert.Equal(gheIssueID, issue.ID)

	_, issue, err = srv.repoIdentity().LookupIssue(t.Context(), repoNumberPathRef{
		owner:        "acme",
		name:         "widget",
		number:       5,
		platformHost: "github.com",
	})
	require.NoError(err)
	require.NotNil(issue)
	assert.Equal(githubIssueID, issue.ID)
}

func TestRepositoryIdentityTreatsMissingLocalRepoAsUnresolvedItem(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	srv, _ := setupTestServer(t)

	result, err := srv.repoIdentity().ResolveLocalItem(t.Context(), repoNumberPathRef{
		owner:  "acme",
		name:   "widget",
		number: 99,
	})
	require.NoError(err)
	assert.False(result.Found)
	assert.Nil(result.Repo)
}

func TestRepositoryIdentityReturnsNotFoundForMissingRepo(t *testing.T) {
	srv, _ := setupTestServer(t)

	repo, err := srv.repoIdentity().LookupRepo(t.Context(), repoIdentityRef{
		owner: "missing",
		name:  "repo",
	}, repoLookupOwnerNameAllowed)
	require.ErrorIs(t, err, errRepoNotFound)
	require.Nil(t, repo)
}
