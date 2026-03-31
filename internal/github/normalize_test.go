package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
)

func ghTimestamp(t time.Time) *gh.Timestamp {
	return &gh.Timestamp{Time: t}
}

func TestNormalizePR_OpenPR(t *testing.T) {
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

	if pr.RepoID != 7 {
		t.Errorf("RepoID: got %d, want 7", pr.RepoID)
	}
	if pr.GitHubID != 1001 {
		t.Errorf("GitHubID: got %d, want 1001", pr.GitHubID)
	}
	if pr.Number != 42 {
		t.Errorf("Number: got %d, want 42", pr.Number)
	}
	if pr.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL: got %q", pr.URL)
	}
	if pr.Title != "My PR" {
		t.Errorf("Title: got %q", pr.Title)
	}
	if pr.Author != "alice" {
		t.Errorf("Author: got %q, want alice", pr.Author)
	}
	if pr.State != "open" {
		t.Errorf("State: got %q, want open", pr.State)
	}
	if pr.IsDraft {
		t.Error("IsDraft: got true, want false")
	}
	if pr.Body != "description" {
		t.Errorf("Body: got %q", pr.Body)
	}
	if pr.Additions != 10 {
		t.Errorf("Additions: got %d, want 10", pr.Additions)
	}
	if pr.Deletions != 5 {
		t.Errorf("Deletions: got %d, want 5", pr.Deletions)
	}
	if pr.HeadBranch != "feature" {
		t.Errorf("HeadBranch: got %q, want feature", pr.HeadBranch)
	}
	if pr.BaseBranch != "main" {
		t.Errorf("BaseBranch: got %q, want main", pr.BaseBranch)
	}
	if !pr.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", pr.CreatedAt, now)
	}
	if !pr.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt: got %v, want %v", pr.UpdatedAt, now)
	}
	if !pr.LastActivityAt.Equal(now) {
		t.Errorf("LastActivityAt: got %v, want %v", pr.LastActivityAt, now)
	}
	if pr.MergedAt != nil {
		t.Errorf("MergedAt: expected nil, got %v", pr.MergedAt)
	}
}

func TestNormalizePR_MergedPR(t *testing.T) {
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

	if pr.State != "merged" {
		t.Errorf("State: got %q, want merged", pr.State)
	}
	if pr.MergedAt == nil {
		t.Fatal("MergedAt: expected non-nil")
	}
	if !pr.MergedAt.Equal(mergedAt) {
		t.Errorf("MergedAt: got %v, want %v", *pr.MergedAt, mergedAt)
	}
}

func TestNormalizeCommentEvent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	c := &gh.IssueComment{
		ID:        new(int64(555)),
		User:      &gh.User{Login: new("carol")},
		Body:      new("looks good"),
		CreatedAt: ghTimestamp(now),
	}

	event := NormalizeCommentEvent(10, c)

	if event.PRID != 10 {
		t.Errorf("PRID: got %d, want 10", event.PRID)
	}
	if event.EventType != "issue_comment" {
		t.Errorf("EventType: got %q, want issue_comment", event.EventType)
	}
	if event.DedupeKey != "comment-555" {
		t.Errorf("DedupeKey: got %q, want comment-555", event.DedupeKey)
	}
	if event.Author != "carol" {
		t.Errorf("Author: got %q, want carol", event.Author)
	}
	if event.Body != "looks good" {
		t.Errorf("Body: got %q", event.Body)
	}
	if event.GitHubID == nil || *event.GitHubID != 555 {
		t.Errorf("GitHubID: got %v, want 555", event.GitHubID)
	}
	if !event.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", event.CreatedAt, now)
	}
}

func TestNormalizeIssueCommentEvent(t *testing.T) {
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

	if event.IssueID != 12 {
		t.Errorf("IssueID: got %d, want 12", event.IssueID)
	}
	if event.EventType != "issue_comment" {
		t.Errorf("EventType: got %q, want issue_comment", event.EventType)
	}
	if event.DedupeKey != "issue-comment-777" {
		t.Errorf("DedupeKey: got %q, want issue-comment-777", event.DedupeKey)
	}
	if event.Author != "dana" {
		t.Errorf("Author: got %q, want dana", event.Author)
	}
	if event.Body != "needs follow-up" {
		t.Errorf("Body: got %q", event.Body)
	}
	if event.GitHubID == nil || *event.GitHubID != 777 {
		t.Errorf("GitHubID: got %v, want 777", event.GitHubID)
	}
	if !event.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", event.CreatedAt, now)
	}
}

func TestDeriveReviewDecision_Empty(t *testing.T) {
	result := DeriveReviewDecision(nil)
	if result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestDeriveReviewDecision_ApprovedOnly(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("alice")}, State: new("APPROVED")},
		{User: &gh.User{Login: new("bob")}, State: new("COMMENTED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "approved" {
		t.Errorf("got %q, want approved", result)
	}
}

func TestDeriveReviewDecision_ChangesRequestedWins(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("alice")}, State: new("APPROVED")},
		{User: &gh.User{Login: new("bob")}, State: new("CHANGES_REQUESTED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "changes_requested" {
		t.Errorf("got %q, want changes_requested", result)
	}
}

func TestDeriveReviewDecision_CommentedOnlyIgnored(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("alice")}, State: new("COMMENTED")},
		{User: &gh.User{Login: new("bob")}, State: new("DISMISSED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestDeriveReviewDecision_LatestStatePerUser(t *testing.T) {
	// bob first requested changes, then approved — latest should be APPROVED
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: new("bob")}, State: new("CHANGES_REQUESTED")},
		{User: &gh.User{Login: new("bob")}, State: new("APPROVED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "approved" {
		t.Errorf("got %q, want approved", result)
	}
}
