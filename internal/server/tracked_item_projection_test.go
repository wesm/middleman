package server

import (
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/workspace"
)

func TestProjectMergeRequestListResponseNormalizesDetailTimestampAndLinks(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	detailFetchedAt := testEDTTime(9, 30)
	resp := projectMergeRequestListResponse(
		db.MergeRequest{
			ID:              11,
			RepoID:          22,
			Number:          3,
			Title:           "Projection PR",
			DetailFetchedAt: &detailFetchedAt,
		},
		db.Repo{
			PlatformHost: "ghe.example.com",
			Owner:        "acme",
			Name:         "widget",
		},
		nil,
	)

	assert.Equal("ghe.example.com", resp.PlatformHost)
	assert.Equal("acme", resp.RepoOwner)
	assert.Equal("widget", resp.RepoName)
	assert.True(resp.DetailLoaded)
	require.NotEmpty(resp.DetailFetchedAt)
	assertRFC3339UTC(t, resp.DetailFetchedAt, detailFetchedAt)
	assert.NotNil(resp.WorktreeLinks)
	assert.Empty(resp.WorktreeLinks)
}

func TestProjectIssueDetailResponseNormalizesEmptyEventsWorkspaceAndDetailTimestamp(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	detailFetchedAt := testEDTTime(15, 45)
	resp := projectIssueDetailResponse(issueDetailProjection{
		Issue: &db.Issue{
			ID:              33,
			RepoID:          44,
			Number:          7,
			Title:           "Projection issue",
			DetailFetchedAt: &detailFetchedAt,
		},
		Events: nil,
		Repo: db.Repo{
			PlatformHost: "github.example.com",
			Owner:        "acme",
			Name:         "widget",
		},
		Workspace: &workspace.Workspace{
			ID:     "ws-123",
			Status: "ready",
		},
	})

	assert.Equal("github.example.com", resp.PlatformHost)
	assert.Equal("acme", resp.RepoOwner)
	assert.Equal("widget", resp.RepoName)
	assert.True(resp.DetailLoaded)
	require.NotEmpty(resp.DetailFetchedAt)
	assertRFC3339UTC(t, resp.DetailFetchedAt, detailFetchedAt)
	assert.NotNil(resp.Events)
	assert.Empty(resp.Events)
	require.NotNil(resp.Workspace)
	assert.Equal("ws-123", resp.Workspace.ID)
	assert.Equal("ready", resp.Workspace.Status)
}

func TestProjectIssueListResponseNormalizesRepoAndDetailTimestamp(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	detailFetchedAt := testEDTTime(10, 5)
	resp := projectIssueListResponse(
		db.Issue{
			ID:              34,
			RepoID:          45,
			Number:          8,
			Title:           "Projection issue list",
			DetailFetchedAt: &detailFetchedAt,
		},
		db.Repo{
			PlatformHost: "ghe.example.com",
			Owner:        "acme",
			Name:         "widget",
		},
	)

	assert.Equal("ghe.example.com", resp.PlatformHost)
	assert.Equal("acme", resp.RepoOwner)
	assert.Equal("widget", resp.RepoName)
	assert.True(resp.DetailLoaded)
	require.NotEmpty(resp.DetailFetchedAt)
	assertRFC3339UTC(t, resp.DetailFetchedAt, detailFetchedAt)
}

func TestProjectMergeRequestDetailResponseKeepsEmptySlicesNonNil(t *testing.T) {
	assert := Assert.New(t)

	resp := projectMergeRequestDetailResponse(mergeRequestDetailProjection{
		MergeRequest: &db.MergeRequest{
			ID:     55,
			RepoID: 66,
			Number: 9,
			Title:  "Projection detail",
		},
		Events:        nil,
		WorktreeLinks: nil,
		Repo: db.Repo{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
		},
	})

	assert.False(resp.DetailLoaded)
	assert.Empty(resp.DetailFetchedAt)
	assert.NotNil(resp.Events)
	assert.Empty(resp.Events)
	assert.NotNil(resp.WorktreeLinks)
	assert.Empty(resp.WorktreeLinks)
}
