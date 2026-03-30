package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
)

func ptr[T any](v T) *T { return &v }

func ghTimestamp(t time.Time) *gh.Timestamp {
	return &gh.Timestamp{Time: t}
}

func TestNormalizePR_OpenPR(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ghPR := &gh.PullRequest{
		ID:        ptr(int64(1001)),
		Number:    ptr(42),
		HTMLURL:   ptr("https://github.com/owner/repo/pull/42"),
		Title:     ptr("My PR"),
		User:      &gh.User{Login: ptr("alice")},
		State:     ptr("open"),
		Draft:     ptr(false),
		Body:      ptr("description"),
		Additions: ptr(10),
		Deletions: ptr(5),
		CreatedAt: ghTimestamp(now),
		UpdatedAt: ghTimestamp(now),
		Head:      &gh.PullRequestBranch{Ref: ptr("feature")},
		Base:      &gh.PullRequestBranch{Ref: ptr("main")},
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
		ID:       ptr(int64(2002)),
		Number:   ptr(99),
		State:    ptr("closed"),
		Merged:   ptr(true),
		MergedAt: ghTimestamp(mergedAt),
		User:     &gh.User{Login: ptr("bob")},
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
		ID:        ptr(int64(555)),
		User:      &gh.User{Login: ptr("carol")},
		Body:      ptr("looks good"),
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

func TestDeriveReviewDecision_Empty(t *testing.T) {
	result := DeriveReviewDecision(nil)
	if result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestDeriveReviewDecision_ApprovedOnly(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: ptr("alice")}, State: ptr("APPROVED")},
		{User: &gh.User{Login: ptr("bob")}, State: ptr("COMMENTED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "approved" {
		t.Errorf("got %q, want approved", result)
	}
}

func TestDeriveReviewDecision_ChangesRequestedWins(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: ptr("alice")}, State: ptr("APPROVED")},
		{User: &gh.User{Login: ptr("bob")}, State: ptr("CHANGES_REQUESTED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "changes_requested" {
		t.Errorf("got %q, want changes_requested", result)
	}
}

func TestDeriveReviewDecision_CommentedOnlyIgnored(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: ptr("alice")}, State: ptr("COMMENTED")},
		{User: &gh.User{Login: ptr("bob")}, State: ptr("DISMISSED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestDeriveReviewDecision_LatestStatePerUser(t *testing.T) {
	// bob first requested changes, then approved — latest should be APPROVED
	reviews := []*gh.PullRequestReview{
		{User: &gh.User{Login: ptr("bob")}, State: ptr("CHANGES_REQUESTED")},
		{User: &gh.User{Login: ptr("bob")}, State: ptr("APPROVED")},
	}
	result := DeriveReviewDecision(reviews)
	if result != "approved" {
		t.Errorf("got %q, want approved", result)
	}
}
