package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ghTimestamp(t time.Time) *gh.Timestamp {
	return &gh.Timestamp{Time: t}
}

func TestNormalizePR_OpenPR(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)
	ghPR := &gh.PullRequest{
		ID:        new(int64(1001)),
		Number:    new(42),
		HTMLURL:   new("https://github.com/owner/repo/pull/42"),
		Title:     new("My PR"),
		User:      &gh.User{Login: new("alice")},
		State:     new("open"),
		Draft:     new(false),
		Body:      new("description"),
		Additions: new(10),
		Deletions: new(5),
		CreatedAt: ghTimestamp(now),
		UpdatedAt: ghTimestamp(now),
		Head:      &gh.PullRequestBranch{Ref: new("feature")},
		Base:      &gh.PullRequestBranch{Ref: new("main")},
	}

	pr := NormalizePR(7, ghPR)

	assert.Equal(int64(7), pr.RepoID)
	assert.Equal(int64(1001), pr.GitHubID)
	assert.Equal(42, pr.Number)
	assert.Equal("https://github.com/owner/repo/pull/42", pr.URL)
	assert.Equal("My PR", pr.Title)
	assert.Equal("alice", pr.Author)
	assert.Equal("open", pr.State)
	assert.False(pr.IsDraft)
	assert.Equal("description", pr.Body)
	assert.Equal(10, pr.Additions)
	assert.Equal(5, pr.Deletions)
	assert.Equal("feature", pr.HeadBranch)
	assert.Equal("main", pr.BaseBranch)
	assert.True(pr.CreatedAt.Equal(now))
	assert.True(pr.UpdatedAt.Equal(now))
	assert.True(pr.LastActivityAt.Equal(now))
	assert.Nil(pr.MergedAt)
}

func TestNormalizePR_MergedPR(t *testing.T) {
	assert := Assert.New(t)
	mergedAt := time.Now().UTC().Truncate(time.Second)
	ghPR := &gh.PullRequest{
		ID:       new(int64(2002)),
		Number:   new(99),
		State:    new("closed"),
		Merged:   new(true),
		MergedAt: ghTimestamp(mergedAt),
		User:     &gh.User{Login: new("bob")},
	}

	pr := NormalizePR(3, ghPR)

	assert.Equal("merged", pr.State)
	require.NotNil(t, pr.MergedAt)
	assert.True(pr.MergedAt.Equal(mergedAt))
}

func TestNormalizeCommentEvent(t *testing.T) {
	assert := Assert.New(t)
	now := time.Now().UTC().Truncate(time.Second)
	c := &gh.IssueComment{
		ID:        new(int64(555)),
		User:      &gh.User{Login: new("carol")},
		Body:      new("looks good"),
		CreatedAt: ghTimestamp(now),
	}

	event := NormalizeCommentEvent(10, c)

	assert.Equal(int64(10), event.PRID)
	assert.Equal("issue_comment", event.EventType)
	assert.Equal("comment-555", event.DedupeKey)
	assert.Equal("carol", event.Author)
	assert.Equal("looks good", event.Body)
	require.NotNil(t, event.GitHubID)
	assert.Equal(int64(555), *event.GitHubID)
	assert.True(event.CreatedAt.Equal(now))
}

func TestNormalizeIssueCommentEvent(t *testing.T) {
	assert := Assert.New(t)
	now := time.Date(2024, 6, 7, 8, 9, 10, 0, time.UTC)
	id := int64(777)
	body := "needs follow-up"
	login := "dana"
	c := &gh.IssueComment{
		ID:        &id,
		Body:      &body,
		User:      &gh.User{Login: &login},
		CreatedAt: &gh.Timestamp{Time: now},
	}

	event := NormalizeIssueCommentEvent(12, c)

	assert.Equal(int64(12), event.IssueID)
	assert.Equal("issue_comment", event.EventType)
	assert.Equal("issue-comment-777", event.DedupeKey)
	assert.Equal("dana", event.Author)
	assert.Equal("needs follow-up", event.Body)
	require.NotNil(t, event.GitHubID)
	assert.Equal(int64(777), *event.GitHubID)
	assert.True(event.CreatedAt.Equal(now))
}

func TestDeriveOverallCIStatus_NoChecksOrStatuses(t *testing.T) {
	result := DeriveOverallCIStatus(nil, nil)
	Assert.Empty(t, result)
}

func TestDeriveOverallCIStatus_EmptyCombined(t *testing.T) {
	combined := &gh.CombinedStatus{State: new("pending")}
	result := DeriveOverallCIStatus(nil, combined)
	Assert.Empty(t, result, "no actual statuses means empty, even if state says pending")
}

func TestDeriveOverallCIStatus_CheckRunsOnly(t *testing.T) {
	tests := []struct {
		name string
		runs []*gh.CheckRun
		want string
	}{
		{
			name: "all success",
			runs: []*gh.CheckRun{
				{Status: new("completed"), Conclusion: new("success")},
				{Status: new("completed"), Conclusion: new("success")},
			},
			want: "success",
		},
		{
			name: "one pending",
			runs: []*gh.CheckRun{
				{Status: new("completed"), Conclusion: new("success")},
				{Status: new("in_progress")},
			},
			want: "pending",
		},
		{
			name: "one queued",
			runs: []*gh.CheckRun{
				{Status: new("queued")},
			},
			want: "pending",
		},
		{
			name: "one failure",
			runs: []*gh.CheckRun{
				{Status: new("completed"), Conclusion: new("success")},
				{Status: new("completed"), Conclusion: new("failure")},
			},
			want: "failure",
		},
		{
			name: "failure beats pending",
			runs: []*gh.CheckRun{
				{Status: new("in_progress")},
				{Status: new("completed"), Conclusion: new("failure")},
			},
			want: "failure",
		},
		{
			name: "timed out is failure",
			runs: []*gh.CheckRun{
				{Status: new("completed"), Conclusion: new("timed_out")},
			},
			want: "failure",
		},
		{
			name: "skipped counts as success",
			runs: []*gh.CheckRun{
				{Status: new("completed"), Conclusion: new("skipped")},
			},
			want: "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveOverallCIStatus(tt.runs, nil)
			Assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeriveOverallCIStatus_CombinedStatusOnly(t *testing.T) {
	combined := &gh.CombinedStatus{
		Statuses: []*gh.RepoStatus{
			{State: new("success"), Context: new("ci/build")},
		},
	}
	Assert.Equal(t, "success", DeriveOverallCIStatus(nil, combined))
}

func TestDeriveOverallCIStatus_MixedSources(t *testing.T) {
	runs := []*gh.CheckRun{
		{Status: new("completed"), Conclusion: new("success")},
	}
	combined := &gh.CombinedStatus{
		Statuses: []*gh.RepoStatus{
			{State: new("pending"), Context: new("ci/deploy")},
		},
	}
	Assert.Equal(t, "pending", DeriveOverallCIStatus(runs, combined))
}

func TestDeriveReviewDecision_Empty(t *testing.T) {
	result := DeriveReviewDecision(nil)
	Assert.Empty(t, result)
}

func TestDeriveReviewDecision_ApprovedOnly(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("alice")}, State: new("APPROVED")},
		{User: &gh.User{Login: new("bob")}, State: new("COMMENTED")},
	}
	result := DeriveReviewDecision(reviews)
	Assert.Equal(t, "approved", result)
}

func TestDeriveReviewDecision_ChangesRequestedWins(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("alice")}, State: new("APPROVED")},
		{User: &gh.User{Login: new("bob")}, State: new("CHANGES_REQUESTED")},
	}
	result := DeriveReviewDecision(reviews)
	Assert.Equal(t, "changes_requested", result)
}

func TestDeriveReviewDecision_CommentedOnlyIgnored(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("alice")}, State: new("COMMENTED")},
		{User: &gh.User{Login: new("bob")}, State: new("DISMISSED")},
	}
	result := DeriveReviewDecision(reviews)
	Assert.Empty(t, result)
}

func TestDeriveReviewDecision_LatestStatePerUser(t *testing.T) {
	// bob first requested changes, then approved — latest should be APPROVED
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("bob")}, State: new("CHANGES_REQUESTED")},
		{User: &gh.User{Login: new("bob")}, State: new("APPROVED")},
	}
	result := DeriveReviewDecision(reviews)
	Assert.Equal(t, "approved", result)
}
