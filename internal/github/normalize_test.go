package github

import (
	"encoding/json"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

func ghTimestamp(t time.Time) *gh.Timestamp {
	return &gh.Timestamp{Time: t}
}

func githubLabel(id int64, name, description, color string, isDefault bool) *gh.Label {
	return &gh.Label{
		ID:          &id,
		Name:        &name,
		Description: &description,
		Color:       &color,
		Default:     &isDefault,
	}
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
		Head: &gh.PullRequestBranch{
			Ref: new("feature"),
			Repo: &gh.Repository{
				CloneURL: new("https://github.com/fork/repo.git"),
			},
		},
		Base: &gh.PullRequestBranch{Ref: new("main")},
	}

	pr := NormalizePR(7, ghPR)

	assert.Equal(int64(7), pr.RepoID)
	assert.Equal(int64(1001), pr.PlatformID)
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
	assert.Equal("https://github.com/fork/repo.git", pr.HeadRepoCloneURL)
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

func TestNormalizePR_Labels(t *testing.T) {
	require := require.New(t)
	updatedAt := time.Date(2024, 6, 7, 10, 11, 12, 0, time.UTC)
	ghPR := &gh.PullRequest{
		ID:        new(int64(3003)),
		Number:    new(11),
		UpdatedAt: ghTimestamp(updatedAt),
		Labels: []*gh.Label{
			githubLabel(5001, "needs-review", "Needs another reviewer", "fbca04", true),
		},
	}

	pr := NormalizePR(9, ghPR)

	require.Equal([]db.Label{{
		PlatformID:  5001,
		Name:        "needs-review",
		Description: "Needs another reviewer",
		Color:       "fbca04",
		IsDefault:   true,
		UpdatedAt:   updatedAt,
	}}, pr.Labels)
}

func TestNormalizeIssue_Labels(t *testing.T) {
	require := require.New(t)
	updatedAt := time.Date(2024, 6, 8, 9, 10, 11, 0, time.UTC)
	ghIssue := &gh.Issue{
		ID:        new(int64(4004)),
		Number:    new(12),
		UpdatedAt: ghTimestamp(updatedAt),
		Labels: []*gh.Label{
			githubLabel(6001, "bug", "Something is broken", "d73a4a", false),
		},
	}

	issue := NormalizeIssue(10, ghIssue)

	require.Equal([]db.Label{{
		PlatformID:  6001,
		Name:        "bug",
		Description: "Something is broken",
		Color:       "d73a4a",
		IsDefault:   false,
		UpdatedAt:   updatedAt,
	}}, issue.Labels)
}

func TestNormalizePR_LabelsSkipsMalformedLabels(t *testing.T) {
	require := require.New(t)
	updatedAt := time.Date(2024, 6, 9, 10, 11, 12, 0, time.UTC)
	blankName := "   "
	ghPR := &gh.PullRequest{
		ID:        new(int64(3004)),
		Number:    new(13),
		UpdatedAt: ghTimestamp(updatedAt),
		Labels: []*gh.Label{
			nil,
			{Name: &blankName},
			githubLabel(5002, "ready", "Ready to merge", "0e8a16", false),
		},
	}

	pr := NormalizePR(9, ghPR)

	require.Equal([]db.Label{{
		PlatformID:  5002,
		Name:        "ready",
		Description: "Ready to merge",
		Color:       "0e8a16",
		IsDefault:   false,
		UpdatedAt:   updatedAt,
	}}, pr.Labels)
}

func TestNormalizeIssue_LabelsFallbackToCreatedAt(t *testing.T) {
	require := require.New(t)
	createdAt := time.Date(2024, 6, 10, 9, 10, 11, 0, time.UTC)
	ghIssue := &gh.Issue{
		ID:        new(int64(4005)),
		Number:    new(14),
		CreatedAt: ghTimestamp(createdAt),
		Labels: []*gh.Label{
			githubLabel(6002, "triage", "Needs triage", "ededed", false),
		},
	}

	issue := NormalizeIssue(10, ghIssue)

	require.Equal([]db.Label{{
		PlatformID:  6002,
		Name:        "triage",
		Description: "Needs triage",
		Color:       "ededed",
		IsDefault:   false,
		UpdatedAt:   createdAt,
	}}, issue.Labels)
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

	assert.Equal(int64(10), event.MergeRequestID)
	assert.Equal("issue_comment", event.EventType)
	assert.Equal("comment-555", event.DedupeKey)
	assert.Equal("carol", event.Author)
	assert.Equal("looks good", event.Body)
	require.NotNil(t, event.PlatformID)
	assert.Equal(int64(555), *event.PlatformID)
	assert.True(event.CreatedAt.Equal(now))
}

func TestNormalizeForcePushEvent(t *testing.T) {
	require := require.New(t)
	createdAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	event := NormalizeForcePushEvent(17, ForcePushEvent{
		Actor:     "alice",
		BeforeSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AfterSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Ref:       "feature",
		CreatedAt: createdAt,
	})

	require.Equal(int64(17), event.MergeRequestID)
	require.Equal("force_push", event.EventType)
	require.Equal("alice", event.Author)
	require.Equal("aaaaaaa -> bbbbbbb", event.Summary)
	require.Equal("force-push-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", event.DedupeKey)
	require.Equal(createdAt, event.CreatedAt)
	require.Contains(event.MetadataJSON, `"before_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)
	require.Contains(event.MetadataJSON, `"after_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`)
	require.Contains(event.MetadataJSON, `"ref":"feature"`)
}

func TestNormalizeCommitEvent_CreatedAtIsUTC(t *testing.T) {
	assert := Assert.New(t)
	//nolint:forbidigo // Test fixture intentionally uses a non-UTC zone to verify normalization.
	edt := time.FixedZone("EDT", -4*3600)
	authorDate := time.Date(2024, 6, 1, 10, 30, 0, 0, edt)
	sha := "abcdef1234567890abcdef1234567890abcdef12"

	commit := &gh.RepositoryCommit{
		SHA: &sha,
		Commit: &gh.Commit{
			Message: new("fix things"),
			Author: &gh.CommitAuthor{
				Date: &gh.Timestamp{Time: authorDate},
				Name: new("Alice"),
			},
		},
		Author: &gh.User{Login: new("alice")},
	}

	event := NormalizeCommitEvent(5, commit)

	assert.Equal(time.UTC, event.CreatedAt.Location())
	assert.True(event.CreatedAt.Equal(authorDate))
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
	require.NotNil(t, event.PlatformID)
	assert.Equal(int64(777), *event.PlatformID)
	assert.True(event.CreatedAt.Equal(now))
}

func TestNormalizePR_MergeableState(t *testing.T) {
	tests := []struct {
		name  string
		state *string
		want  string
	}{
		{"dirty", new("dirty"), "dirty"},
		{"clean", new("clean"), "clean"},
		{"unknown", new("unknown"), "unknown"},
		{"blocked", new("blocked"), "blocked"},
		{"behind", new("behind"), "behind"},
		{"unstable", new("unstable"), "unstable"},
		{"has_hooks", new("has_hooks"), "has_hooks"},
		{"draft", new("draft"), "draft"},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ghPR := &gh.PullRequest{
				ID:             new(int64(1)),
				Number:         new(1),
				State:          new("open"),
				MergeableState: tt.state,
			}
			pr := NormalizePR(1, ghPR)
			Assert.Equal(t, tt.want, pr.MergeableState)
		})
	}
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

func TestDeriveOverallCIStatus_NonSuccessConclusions(t *testing.T) {
	// Any completed conclusion not in {success, neutral, skipped} is failure.
	for _, conclusion := range []string{
		"action_required", "failure", "timed_out",
		"cancelled", "stale", "startup_failure",
	} {
		t.Run(conclusion, func(t *testing.T) {
			runs := []*gh.CheckRun{
				{Status: new("completed"), Conclusion: new(conclusion)},
			}
			Assert.Equal(t, "failure", DeriveOverallCIStatus(runs, nil))
		})
	}
}

func TestDeriveOverallCIStatus_CombinedStatusOnly(t *testing.T) {
	combined := &gh.CombinedStatus{
		TotalCount: new(1),
		State:      new("success"),
		Statuses: []*gh.RepoStatus{
			{State: new("success"), Context: new("ci/build")},
		},
	}
	Assert.Equal(t, "success", DeriveOverallCIStatus(nil, combined))
}

func TestDeriveOverallCIStatus_CombinedUsesAggregatedState(t *testing.T) {
	// Statuses slice may be truncated by pagination; the pre-aggregated
	// State field reflects all pages, so we rely on it instead.
	combined := &gh.CombinedStatus{
		TotalCount: new(50),
		State:      new("failure"),
		Statuses: []*gh.RepoStatus{
			{State: new("success"), Context: new("ci/build")},
		},
	}
	Assert.Equal(t, "failure", DeriveOverallCIStatus(nil, combined))
}

func TestDeriveOverallCIStatus_MixedSources(t *testing.T) {
	runs := []*gh.CheckRun{
		{Status: new("completed"), Conclusion: new("success")},
	}
	combined := &gh.CombinedStatus{
		TotalCount: new(1),
		State:      new("pending"),
		Statuses: []*gh.RepoStatus{
			{State: new("pending"), Context: new("ci/deploy")},
		},
	}
	Assert.Equal(t, "pending", DeriveOverallCIStatus(runs, combined))
}

func TestNormalizeCIChecks_ExpectedAndPendingStatus(t *testing.T) {
	assert := Assert.New(t)

	combined := &gh.CombinedStatus{
		TotalCount: new(2),
		State:      new("pending"),
		Statuses: []*gh.RepoStatus{
			{State: new("pending"), Context: new("ci/build")},
			{State: new("expected"), Context: new("ci/required")},
		},
	}

	raw := NormalizeCIChecks(nil, combined)
	require.NotEmpty(t, raw)

	var checks []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &checks))
	require.Len(t, checks, 2)

	assert.Equal("ci/build", checks[0].Name)
	assert.Equal("in_progress", checks[0].Status)
	assert.Empty(checks[0].Conclusion)

	assert.Equal("ci/required", checks[1].Name)
	assert.Equal("in_progress", checks[1].Status)
	assert.Empty(checks[1].Conclusion)
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
