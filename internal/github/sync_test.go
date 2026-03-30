package github

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
)

// openTestDB opens a temporary SQLite database for the duration of the test.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// mockClient implements Client with configurable canned responses.
type mockClient struct {
	openPRs  []*gh.PullRequest
	singlePR *gh.PullRequest
	comments []*gh.IssueComment
	reviews  []*gh.PullRequestReview
	commits  []*gh.RepositoryCommit
	ciStatus *gh.CombinedStatus
}

func (m *mockClient) ListOpenPullRequests(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
	return m.openPRs, nil
}

func (m *mockClient) ListOpenIssues(_ context.Context, _, _ string) ([]*gh.Issue, error) {
	return nil, nil
}

func (m *mockClient) GetIssue(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
	return nil, nil
}

func (m *mockClient) GetPullRequest(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
	if m.singlePR != nil {
		return m.singlePR, nil
	}
	// Fall back to matching from the open PRs list
	for _, pr := range m.openPRs {
		if pr.GetNumber() == number {
			return pr, nil
		}
	}
	return nil, nil
}

func (m *mockClient) ListIssueComments(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
	return m.comments, nil
}

func (m *mockClient) ListReviews(_ context.Context, _, _ string, _ int) ([]*gh.PullRequestReview, error) {
	return m.reviews, nil
}

func (m *mockClient) ListCommits(_ context.Context, _, _ string, _ int) ([]*gh.RepositoryCommit, error) {
	return m.commits, nil
}

func (m *mockClient) GetCombinedStatus(_ context.Context, _, _, _ string) (*gh.CombinedStatus, error) {
	return m.ciStatus, nil
}

func (m *mockClient) ListCheckRunsForRef(_ context.Context, _, _, _ string) ([]*gh.CheckRun, error) {
	return nil, nil
}

func (m *mockClient) CreateIssueComment(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.IssueComment, error) {
	return nil, nil
}

func (m *mockClient) GetRepository(
	_ context.Context, _, _ string,
) (*gh.Repository, error) {
	return &gh.Repository{}, nil
}

func (m *mockClient) CreateReview(
	_ context.Context, _, _ string, _ int, _ string, _ string,
) (*gh.PullRequestReview, error) {
	id := int64(1)
	state := "APPROVED"
	return &gh.PullRequestReview{ID: &id, State: &state}, nil
}

func (m *mockClient) MarkPullRequestReadyForReview(
	_ context.Context, _, _ string, number int,
) (*gh.PullRequest, error) {
	draft := false
	return &gh.PullRequest{Number: &number, Draft: &draft}, nil
}

func (m *mockClient) MergePullRequest(
	_ context.Context, _, _ string, _ int, _, _, _ string,
) (*gh.PullRequestMergeResult, error) {
	merged := true
	sha := "abc123"
	msg := "merged"
	return &gh.PullRequestMergeResult{
		Merged: &merged, SHA: &sha, Message: &msg,
	}, nil
}

// makeTimestamp is a helper for constructing go-github Timestamp values.
func makeTimestamp(t time.Time) *gh.Timestamp {
	return &gh.Timestamp{Time: t}
}

// buildOpenPR constructs a minimal open *gh.PullRequest for tests.
func buildOpenPR(number int, updatedAt time.Time) *gh.PullRequest {
	sha := "abc123def456"
	state := "open"
	title := "test PR"
	url := "https://github.com/owner/repo/pull/1"
	id := int64(number) * 1000
	headRef := "feature-branch"
	baseRef := "main"
	return &gh.PullRequest{
		ID:        &id,
		Number:    &number,
		Title:     &title,
		HTMLURL:   &url,
		State:     &state,
		UpdatedAt: makeTimestamp(updatedAt),
		CreatedAt: makeTimestamp(updatedAt),
		Head: &gh.PullRequestBranch{
			Ref: &headRef,
			SHA: &sha,
		},
		Base: &gh.PullRequestBranch{
			Ref: &baseRef,
		},
	}
}

func TestSyncCreatesAndUpdatesPRs(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	commitMsg := "initial commit"
	commitSHA := "abc123def456"
	commitDate := makeTimestamp(now.Add(-1 * time.Hour))
	ciState := "success"

	mc := &mockClient{
		openPRs: []*gh.PullRequest{buildOpenPR(1, now)},
		commits: []*gh.RepositoryCommit{
			{
				SHA: &commitSHA,
				Commit: &gh.Commit{
					Message: &commitMsg,
					Author: &gh.CommitAuthor{
						Name: gh.Ptr("dev"),
						Date: commitDate,
					},
				},
			},
		},
		reviews:  []*gh.PullRequestReview{},
		comments: []*gh.IssueComment{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
	}

	syncer := NewSyncer(mc, d, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute)
	syncer.RunOnce(ctx)

	// PR should be in the DB.
	pr, err := d.GetPullRequest(ctx, "owner", "repo", 1)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PR in DB, got nil")
	}
	if pr.Number != 1 {
		t.Errorf("pr.Number = %d, want 1", pr.Number)
	}

	// Kanban state should have been created.
	ks, err := d.GetKanbanState(ctx, pr.ID)
	if err != nil {
		t.Fatalf("GetKanbanState: %v", err)
	}
	if ks == nil {
		t.Fatal("expected kanban state, got nil")
	}
	if ks.Status != "new" {
		t.Errorf("kanban status = %q, want %q", ks.Status, "new")
	}

	// Commit event should have been stored.
	events, err := d.ListPREvents(ctx, pr.ID)
	if err != nil {
		t.Fatalf("ListPREvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one PR event")
	}
	found := false
	for _, e := range events {
		if e.EventType == "commit" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a commit event, none found")
	}
}

func TestSyncSingleFlight(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	callCount := 0
	mc := &mockClient{
		openPRs: []*gh.PullRequest{},
	}
	// Wrap in a counter client to detect calls.
	_ = mc

	syncer := NewSyncer(mc, d, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute)

	// Simulate a concurrent run already in progress.
	syncer.running.Store(true)
	syncer.RunOnce(ctx) // should be a no-op
	syncer.running.Store(false)

	// Verify no DB side-effects: repo row should not exist because the RunOnce was skipped.
	repo, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("GetRepoByOwnerName: %v", err)
	}
	if repo != nil {
		t.Errorf("expected nil repo (sync was skipped), got %+v", repo)
	}

	_ = callCount
}

func TestSyncStatusUpdated(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(mc, d, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute)

	before := time.Now()
	syncer.RunOnce(ctx)
	after := time.Now()

	status := syncer.Status()
	if status.Running {
		t.Error("status.Running should be false after sync completes")
	}
	if status.LastRunAt.IsZero() {
		t.Error("status.LastRunAt should be set after sync")
	}
	if status.LastRunAt.Before(before) || status.LastRunAt.After(after) {
		t.Errorf("status.LastRunAt %v not between %v and %v", status.LastRunAt, before, after)
	}
	if status.LastError != "" {
		t.Errorf("expected no error, got %q", status.LastError)
	}
}
