package github

import (
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

func TestDetailRefreshPlannerBuildsItemsForTrackedRepos(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := t.Context()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	untrackedRepoID, err := d.UpsertRepo(ctx, "github.com", "owner", "untracked")
	require.NoError(err)

	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	detailFetchedAt := now.Add(-time.Hour)

	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:          repoID,
		PlatformID:      101,
		Number:          7,
		URL:             "https://github.com/owner/repo/pull/7",
		Title:           "Tracked PR",
		Author:          "alice",
		State:           "open",
		HeadBranch:      "feature",
		BaseBranch:      "main",
		PlatformHeadSHA: "abc123",
		PlatformBaseSHA: "def456",
		CreatedAt:       now.Add(-2 * time.Hour),
		UpdatedAt:       now,
		LastActivityAt:  now,
		CIHadPending:    true,
	})
	require.NoError(err)
	_, err = d.UpsertIssue(ctx, &db.Issue{
		RepoID:          repoID,
		PlatformID:      201,
		Number:          8,
		URL:             "https://github.com/owner/repo/issues/8",
		Title:           "Tracked issue",
		Author:          "bob",
		State:           "open",
		CreatedAt:       now.Add(-3 * time.Hour),
		UpdatedAt:       now.Add(-30 * time.Minute),
		LastActivityAt:  now.Add(-30 * time.Minute),
		DetailFetchedAt: &detailFetchedAt,
	})
	require.NoError(err)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:          untrackedRepoID,
		PlatformID:      301,
		Number:          9,
		URL:             "https://github.com/owner/untracked/pull/9",
		Title:           "Untracked PR",
		Author:          "carol",
		State:           "open",
		HeadBranch:      "feature",
		BaseBranch:      "main",
		PlatformHeadSHA: "abc123",
		PlatformBaseSHA: "def456",
		CreatedAt:       now,
		UpdatedAt:       now,
		LastActivityAt:  now,
	})
	require.NoError(err)

	planner := newDetailRefreshPlanner(d)
	plan := planner.Build(ctx, detailRefreshPlanInput{
		TrackedRepos: []RepoRef{{
			Owner: "owner",
			Name:  "repo",
			// Empty host must match the DB's canonical github.com host.
		}},
		WatchedMRs: []WatchedMR{{
			Owner:  "owner",
			Name:   "repo",
			Number: 7,
		}},
	})

	require.NoError(plan.PullRequestListErr)
	require.NoError(plan.IssueListErr)
	require.Len(plan.Items, 2)

	pr := plan.Items[0]
	assert.Equal(QueueItemPR, pr.Type)
	assert.Equal("github.com", pr.PlatformHost)
	assert.Equal("owner", pr.RepoOwner)
	assert.Equal("repo", pr.RepoName)
	assert.Equal(7, pr.Number)
	assert.True(pr.CIHadPending)
	assert.True(pr.Watched)
	assert.True(pr.IsOpen)

	issue := plan.Items[1]
	assert.Equal(QueueItemIssue, issue.Type)
	assert.Equal("github.com", issue.PlatformHost)
	assert.Equal("owner", issue.RepoOwner)
	assert.Equal("repo", issue.RepoName)
	assert.Equal(8, issue.Number)
	assert.Equal(&detailFetchedAt, issue.DetailFetchedAt)
	assert.False(issue.Watched)
	assert.True(issue.IsOpen)
}

func TestDetailRefreshPlannerRepoKeysDefaultHostAndSeparateHosts(t *testing.T) {
	assert := Assert.New(t)

	tracked := detailTrackedRepoSet([]RepoRef{
		{Owner: "owner", Name: "repo"},
		{PlatformHost: "ghe.corp.example", Owner: "owner", Name: "repo"},
	})

	assert.True(tracked[detailRepoKey("github.com", "owner", "repo")])
	assert.True(tracked[detailRepoKey("", "owner", "repo")])
	assert.True(tracked[detailRepoKey("ghe.corp.example", "owner", "repo")])
	assert.False(tracked[detailRepoKey("gitlab.example", "owner", "repo")])
}
