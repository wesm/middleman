package github

import (
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
)

func TestGitHubFixtureBuilders(t *testing.T) {
	assert := Assert.New(t)
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	repo := ghRepo("acme", "widget", true)
	assert.Equal("widget", repo.GetName())
	assert.Equal("acme/widget", repo.GetFullName())
	assert.True(repo.GetArchived())

	label := ghLabel(42, "bug", "Bug", "d73a4a", true)
	assert.Equal(int64(42), label.GetID())
	assert.Equal("bug", label.GetName())
	assert.True(label.GetDefault())

	pr := ghPR(7, now, withPRHead("feature/x", "abc123"), withPRHeadRepo("fork/widget", "https://example.invalid/repo.git"), withPRMerged("merge-sha", now))
	assert.Equal(7, pr.GetNumber())
	assert.Equal("feature/x", pr.GetHead().GetRef())
	assert.Equal("abc123", pr.GetHead().GetSHA())
	assert.Equal("fork/widget", pr.GetHead().GetRepo().GetFullName())
	assert.True(pr.GetMerged())
	assert.Equal("merge-sha", pr.GetMergeCommitSHA())
	assert.Equal(now, pr.GetMergedAt().Time)
}
