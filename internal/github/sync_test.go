package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

// openTestDB opens a temporary SQLite database for the duration of the test.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

// testBudget builds a per-host budget map for use in NewSyncer calls.
func testBudget(limit int) map[string]*SyncBudget {
	return map[string]*SyncBudget{
		"github.com": NewSyncBudget(limit),
	}
}

// mockClient implements Client with configurable canned responses.
type mockClient struct {
	budget                 *SyncBudget // optional: simulates transport counting
	openPRs                []*gh.PullRequest
	openIssues             []*gh.Issue
	listOpenPRsErr         error
	listOpenIssuesErr      error
	singlePR               *gh.PullRequest
	getPullRequestFn       func(context.Context, string, string, int) (*gh.PullRequest, error)
	getIssueFn             func(context.Context, string, string, int) (*gh.Issue, error)
	getUserFn              func(context.Context, string) (*gh.User, error)
	listOpenPRsFn          func(context.Context, string, string) ([]*gh.PullRequest, error)
	listPullRequestsPageFn func(context.Context, string, string, string, int) ([]*gh.PullRequest, bool, error)
	listIssuesPageFn       func(context.Context, string, string, string, int) ([]*gh.Issue, bool, error)
	comments               []*gh.IssueComment
	reviews                []*gh.PullRequestReview
	commits                []*gh.RepositoryCommit
	forcePushEvents        []ForcePushEvent
	forcePushEventsErr     error
	ciStatus               *gh.CombinedStatus
	checkRuns              []*gh.CheckRun
	workflowRuns           []*gh.WorkflowRun
	approveWorkflowRunFn   func(context.Context, string, string, int64) error
	listOpenPRsCalled          bool
	getUserCalls               atomic.Int32
	getCombinedCalls           atomic.Int32
	invalidateCalls            atomic.Int32
	listIssueCommentsCalled    atomic.Int32
}

func (m *mockClient) trackCall() {
	if m.budget != nil {
		m.budget.Spend(1)
	}
}

func (m *mockClient) InvalidateListETagsForRepo(_, _ string, _ ...string) {
	m.invalidateCalls.Add(1)
}

func (m *mockClient) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*gh.PullRequest, error) {
	m.trackCall()
	m.listOpenPRsCalled = true
	if m.listOpenPRsFn != nil {
		return m.listOpenPRsFn(ctx, owner, repo)
	}
	if m.listOpenPRsErr != nil {
		return nil, m.listOpenPRsErr
	}
	return m.openPRs, nil
}

func (m *mockClient) ListOpenIssues(_ context.Context, _, _ string) ([]*gh.Issue, error) {
	m.trackCall()
	if m.listOpenIssuesErr != nil {
		return nil, m.listOpenIssuesErr
	}
	return m.openIssues, nil
}

func (m *mockClient) GetIssue(
	ctx context.Context, owner, repo string, number int,
) (*gh.Issue, error) {
	m.trackCall()
	if m.getIssueFn != nil {
		return m.getIssueFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *mockClient) GetUser(ctx context.Context, login string) (*gh.User, error) {
	m.trackCall()
	m.getUserCalls.Add(1)
	if m.getUserFn != nil {
		return m.getUserFn(ctx, login)
	}
	name := "Display " + login
	return &gh.User{Login: &login, Name: &name}, nil
}

func (m *mockClient) GetPullRequest(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	m.trackCall()
	if m.getPullRequestFn != nil {
		return m.getPullRequestFn(ctx, owner, repo, number)
	}
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
	m.trackCall()
	m.listIssueCommentsCalled.Add(1)
	return m.comments, nil
}

func (m *mockClient) ListReviews(_ context.Context, _, _ string, _ int) ([]*gh.PullRequestReview, error) {
	m.trackCall()
	return m.reviews, nil
}

func (m *mockClient) ListCommits(_ context.Context, _, _ string, _ int) ([]*gh.RepositoryCommit, error) {
	m.trackCall()
	return m.commits, nil
}

func (m *mockClient) ListForcePushEvents(_ context.Context, _, _ string, _ int) ([]ForcePushEvent, error) {
	m.trackCall()
	return m.forcePushEvents, m.forcePushEventsErr
}

func (m *mockClient) GetCombinedStatus(_ context.Context, _, _, _ string) (*gh.CombinedStatus, error) {
	m.trackCall()
	m.getCombinedCalls.Add(1)
	return m.ciStatus, nil
}

func (m *mockClient) ListCheckRunsForRef(_ context.Context, _, _, _ string) ([]*gh.CheckRun, error) {
	m.trackCall()
	return m.checkRuns, nil
}

func (m *mockClient) ListWorkflowRunsForHeadSHA(
	_ context.Context, _, _, _ string,
) ([]*gh.WorkflowRun, error) {
	m.trackCall()
	return m.workflowRuns, nil
}

func (m *mockClient) ApproveWorkflowRun(
	ctx context.Context, owner, repo string, runID int64,
) error {
	m.trackCall()
	if m.approveWorkflowRunFn != nil {
		return m.approveWorkflowRunFn(ctx, owner, repo, runID)
	}
	return nil
}

func (m *mockClient) CreateIssueComment(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.IssueComment, error) {
	m.trackCall()
	return nil, nil
}

func (m *mockClient) GetRepository(
	_ context.Context, _, _ string,
) (*gh.Repository, error) {
	m.trackCall()
	return &gh.Repository{}, nil
}

func (m *mockClient) CreateReview(
	_ context.Context, _, _ string, _ int, _ string, _ string,
) (*gh.PullRequestReview, error) {
	m.trackCall()
	id := int64(1)
	state := "APPROVED"
	return &gh.PullRequestReview{ID: &id, State: &state}, nil
}

func (m *mockClient) MarkPullRequestReadyForReview(
	_ context.Context, _, _ string, number int,
) (*gh.PullRequest, error) {
	m.trackCall()
	draft := false
	return &gh.PullRequest{Number: &number, Draft: &draft}, nil
}

func (m *mockClient) MergePullRequest(
	_ context.Context, _, _ string, _ int, _, _, _ string,
) (*gh.PullRequestMergeResult, error) {
	m.trackCall()
	merged := true
	sha := "abc123"
	msg := "merged"
	return &gh.PullRequestMergeResult{
		Merged: &merged, SHA: &sha, Message: &msg,
	}, nil
}

func (m *mockClient) EditPullRequest(
	_ context.Context, _, _ string, _ int, state string,
) (*gh.PullRequest, error) {
	m.trackCall()
	return &gh.PullRequest{State: &state}, nil
}

func (m *mockClient) EditIssue(
	_ context.Context, _, _ string, _ int, state string,
) (*gh.Issue, error) {
	m.trackCall()
	return &gh.Issue{State: &state}, nil
}

func (m *mockClient) ListPullRequestsPage(
	ctx context.Context, owner, repo, state string, page int,
) ([]*gh.PullRequest, bool, error) {
	m.trackCall()
	if m.listPullRequestsPageFn != nil {
		return m.listPullRequestsPageFn(ctx, owner, repo, state, page)
	}
	return nil, false, nil
}

func (m *mockClient) ListIssuesPage(
	ctx context.Context, owner, repo, state string, page int,
) ([]*gh.Issue, bool, error) {
	m.trackCall()
	if m.listIssuesPageFn != nil {
		return m.listIssuesPageFn(ctx, owner, repo, state, page)
	}
	return nil, false, nil
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

func buildGitHubLabel(id int64, name, description, color string, isDefault bool) *gh.Label {
	return &gh.Label{
		ID:          &id,
		Name:        &name,
		Description: &description,
		Color:       &color,
		Default:     &isDefault,
	}
}

func TestSyncerStopIsIdempotent(t *testing.T) {
	syncer := NewSyncer(map[string]Client{"github.com": &mockClient{}}, nil, nil, nil, time.Minute, nil, nil)
	syncer.Stop()
	syncer.Stop() // must not panic
}

func TestDiffSyncErrorUserMessageSanitized(t *testing.T) {
	assert := Assert.New(t)
	// A representative leak: clone path, ref, SHA, and command stderr.
	leaky := fmt.Errorf(
		"rev-parse refs/pull/42/head for merged PR #42: " +
			"exec /home/user/.middleman/clones/github.com/owner/repo.git: " +
			"fatal: ambiguous argument 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef'")

	cases := []struct {
		name string
		code DiffSyncErrorCode
	}{
		{"clone unavailable", DiffSyncCodeCloneUnavailable},
		{"commit unreachable", DiffSyncCodeCommitUnreachable},
		{"merge base failed", DiffSyncCodeMergeBaseFailed},
		{"internal", DiffSyncCodeInternal},
		{"unknown code", DiffSyncErrorCode("not_a_real_code")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &DiffSyncError{Code: tc.code, Err: leaky}
			msg := e.UserMessage()
			assert.NotEmpty(msg, "user message should never be empty")
			assert.NotContains(msg, "/home/user", "user message must not leak filesystem paths")
			assert.NotContains(msg, "refs/pull/", "user message must not leak git refs")
			assert.NotContains(msg, "deadbeef", "user message must not leak SHAs")
			assert.NotContains(msg, "rev-parse", "user message must not leak git command names")
			assert.NotContains(msg, "fatal:", "user message must not leak git stderr")
		})
	}

	// Error() (used for server-side logs) is allowed to include the
	// underlying detail; only UserMessage() is the public surface.
	e := &DiffSyncError{Code: DiffSyncCodeCommitUnreachable, Err: leaky}
	assert.Contains(e.Error(), "commit_unreachable",
		"server-side Error() should include the categorization")
	assert.Contains(e.Error(), "deadbeef",
		"server-side Error() may include underlying detail for debugging")
}

func TestSyncCreatesAndUpdatesPRs(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
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
						Name: new("dev"),
						Date: commitDate,
					},
				},
			},
		},
		reviews:  []*gh.PullRequestReview{},
		comments: []*gh.IssueComment{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))
	syncer.RunOnce(ctx)

	// PR should be in the DB.
	pr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal(1, pr.Number)

	// Kanban state should have been created.
	ks, err := d.GetKanbanState(ctx, pr.ID)
	require.NoError(err)
	require.NotNil(ks)
	assert.Equal("new", ks.Status)

	// Commit event should have been stored (via detail drain).
	events, err := d.ListMREvents(ctx, pr.ID)
	require.NoError(err)
	require.NotEmpty(events)
	found := false
	for _, e := range events {
		if e.EventType == "commit" {
			found = true
			break
		}
	}
	assert.True(found)
}

func TestSyncStoresForcePushEvent(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	commitSHA := "abc123def456"
	commitMsg := "fix: tighten validation"
	ciState := "success"

	mc := &mockClient{
		openPRs: []*gh.PullRequest{buildOpenPR(1, now)},
		commits: []*gh.RepositoryCommit{{
			SHA: &commitSHA,
			Commit: &gh.Commit{
				Message: &commitMsg,
				Author:  &gh.CommitAuthor{Name: new("dev"), Date: makeTimestamp(now.Add(-1 * time.Hour))},
			},
		}},
		forcePushEvents: []ForcePushEvent{{
			Actor:     "alice",
			BeforeSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AfterSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Ref:       "feature",
			CreatedAt: now.Add(-30 * time.Minute),
		}},
		reviews:  []*gh.PullRequestReview{},
		comments: []*gh.IssueComment{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))
	syncer.RunOnce(ctx)

	pr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(pr)

	events, err := d.ListMREvents(ctx, pr.ID)
	require.NoError(err)
	require.NotEmpty(events)

	var forcePush *db.MREvent
	for i := range events {
		if events[i].EventType == "force_push" {
			forcePush = &events[i]
			break
		}
	}
	require.NotNil(forcePush)
	assert.Equal("alice", forcePush.Author)
	assert.Equal("aaaaaaa -> bbbbbbb", forcePush.Summary)
	assert.Contains(forcePush.MetadataJSON, `"ref":"feature"`)
}

func TestSyncStoresPRLabels(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 2, 12, 0, 0, 0, time.UTC)
	pr := buildOpenPR(1, now)
	pr.Labels = []*gh.Label{
		buildGitHubLabel(501, "needs-review", "Needs another reviewer", "fbca04", true),
	}

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{pr},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))
	syncer.RunOnce(ctx)

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored)
	require.Len(stored.Labels, 1)
	require.Equal("needs-review", stored.Labels[0].Name)
	require.Equal("Needs another reviewer", stored.Labels[0].Description)
	require.Equal("fbca04", stored.Labels[0].Color)
	require.True(stored.Labels[0].IsDefault)
	require.Equal(int64(501), stored.Labels[0].PlatformID)
	require.True(stored.Labels[0].UpdatedAt.Equal(now))
}

func TestSyncMRReplacesLabelsOnResync(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 3, 12, 0, 0, 0, time.UTC)
	pr := buildOpenPR(1, now)
	pr.Labels = []*gh.Label{
		buildGitHubLabel(701, "bug", "Bug fix", "d73a4a", true),
	}

	mc := &mockClient{singlePR: pr, comments: []*gh.IssueComment{}, reviews: []*gh.PullRequestReview{}, commits: []*gh.RepositoryCommit{}}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))

	require.NoError(syncer.SyncMR(ctx, "owner", "repo", 1))

	pr.Labels = []*gh.Label{
		buildGitHubLabel(702, "feature", "New feature", "0e8a16", false),
	}
	pr.UpdatedAt = makeTimestamp(now.Add(time.Minute))

	require.NoError(syncer.SyncMR(ctx, "owner", "repo", 1))

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored)
	require.Len(stored.Labels, 1)
	require.Equal("feature", stored.Labels[0].Name)
	require.Equal("New feature", stored.Labels[0].Description)
	require.Equal("0e8a16", stored.Labels[0].Color)
	require.False(stored.Labels[0].IsDefault)
	require.Equal(int64(702), stored.Labels[0].PlatformID)
}

func TestSyncIssueReplacesLabelsOnResync(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 4, 12, 0, 0, 0, time.UTC)
	issueNumber := 42
	issueTitle := "broken thing"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/42"
	issueBody := ""
	issueID := int64(900042)
	issue := &gh.Issue{
		ID:        &issueID,
		Number:    &issueNumber,
		Title:     &issueTitle,
		State:     &issueState,
		HTMLURL:   &issueURL,
		Body:      &issueBody,
		CreatedAt: makeTimestamp(now),
		UpdatedAt: makeTimestamp(now),
		Labels: []*gh.Label{
			buildGitHubLabel(801, "bug", "Something is broken", "d73a4a", true),
		},
	}

	mc := &mockClient{getIssueFn: func(context.Context, string, string, int) (*gh.Issue, error) {
		return issue, nil
	}, comments: []*gh.IssueComment{}}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, nil)

	require.NoError(syncer.SyncIssue(ctx, "owner", "repo", issueNumber))

	issue.Labels = []*gh.Label{
		buildGitHubLabel(802, "docs", "Documentation", "0075ca", false),
	}
	issue.UpdatedAt = makeTimestamp(now.Add(time.Minute))

	require.NoError(syncer.SyncIssue(ctx, "owner", "repo", issueNumber))

	stored, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(stored)
	require.Len(stored.Labels, 1)
	require.Equal("docs", stored.Labels[0].Name)
	require.Equal("Documentation", stored.Labels[0].Description)
	require.Equal("0075ca", stored.Labels[0].Color)
	require.False(stored.Labels[0].IsDefault)
	require.Equal(int64(802), stored.Labels[0].PlatformID)
}

func TestSyncIgnoresForcePushFetchFailures(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	commitSHA := "abc123def456"
	commitMsg := "fix: tighten validation"
	ciState := "success"
	commentBody := "Looks good to me"
	commentID := int64(41)
	commentURL := "https://github.com/owner/repo/pull/1#issuecomment-41"
	forcePushErr := errors.New("graphql unavailable")

	mc := &mockClient{
		openPRs: []*gh.PullRequest{buildOpenPR(1, now)},
		comments: []*gh.IssueComment{{
			ID:        &commentID,
			Body:      &commentBody,
			HTMLURL:   &commentURL,
			CreatedAt: makeTimestamp(now.Add(-45 * time.Minute)),
			User:      &gh.User{Login: new("alice")},
		}},
		commits: []*gh.RepositoryCommit{{
			SHA: &commitSHA,
			Commit: &gh.Commit{
				Message: &commitMsg,
				Author:  &gh.CommitAuthor{Name: new("dev"), Date: makeTimestamp(now.Add(-1 * time.Hour))},
			},
		}},
		forcePushEventsErr: forcePushErr,
		reviews:            []*gh.PullRequestReview{},
		ciStatus:           &gh.CombinedStatus{State: &ciState},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))
	syncer.RunOnce(ctx)

	pr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(pr)

	events, err := d.ListMREvents(ctx, pr.ID)
	require.NoError(err)
	require.Len(events, 2)

	var sawCommit bool
	var sawComment bool
	for _, event := range events {
		if event.EventType == "commit" {
			sawCommit = true
		}
		if event.EventType == "issue_comment" {
			sawComment = true
		}
		assert.NotEqual("force_push", event.EventType)
	}
	assert.True(sawCommit)
	assert.True(sawComment)
	assert.Equal(1, pr.CommentCount)
	assert.Equal(now, pr.LastActivityAt)
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

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))

	// Simulate a concurrent run already in progress.
	syncer.running.Store(true)
	syncer.RunOnce(ctx) // should be a no-op
	syncer.running.Store(false)

	// Verify no DB side-effects: repo row should not exist because the RunOnce was skipped.
	repo, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(t, err)
	Assert.Nil(t, repo)

	_ = callCount
}

func TestSyncPreservesMergeableState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	pr := buildOpenPR(1, now)
	additions := 10
	deletions := 5
	mergeableState := "dirty"
	pr.Additions = &additions
	pr.Deletions = &deletions
	pr.MergeableState = &mergeableState

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{pr},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))

	// First sync: full fetch occurs, MergeableState is stored.
	syncer.RunOnce(ctx)

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("dirty", stored.MergeableState)

	// Second sync: same UpdatedAt means no full fetch. The list endpoint
	// does not return MergeableState, so the preservation branch runs.
	// Reset the mock so the list PR has no MergeableState (as the real
	// list endpoint would return).
	listPR := buildOpenPR(1, now) // same UpdatedAt, no MergeableState set
	listPR.Additions = nil
	listPR.Deletions = nil
	mc.openPRs = []*gh.PullRequest{listPR}
	// Ensure full fetch would return empty MergeableState if it ran.
	mc.getPullRequestFn = func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
		p := buildOpenPR(1, now)
		return p, nil
	}

	syncer.RunOnce(ctx)

	stored2, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored2)
	assert.Equal("dirty", stored2.MergeableState, "MergeableState should be preserved when full fetch is skipped")
}

func TestSyncTriggersFullFetchForUnknownMergeableState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// Build a list PR with diff stats set so the zero-stats condition
	// doesn't trigger the full fetch independently.
	listPR := buildOpenPR(1, now)
	additions := 10
	deletions := 5
	listPR.Additions = &additions
	listPR.Deletions = &deletions

	// First full-fetch returns "unknown".
	fetchCount := 0
	mc := &mockClient{
		openPRs:  []*gh.PullRequest{listPR},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}
	mc.getPullRequestFn = func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
		fetchCount++
		p := buildOpenPR(1, now)
		a, d2 := 10, 5
		p.Additions = &a
		p.Deletions = &d2
		state := "unknown"
		if fetchCount >= 2 {
			state = "clean"
		}
		p.MergeableState = &state
		return p, nil
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))

	// First sync: index scan upserts list data, detail drain fetches
	// full PR (returns "unknown" MergeableState).
	syncer.RunOnce(ctx)

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("unknown", stored.MergeableState)
	assert.Equal(1, fetchCount, "first sync should trigger one full fetch via detail drain")
}

func TestSyncPreservesFieldsOnFullFetchFailure(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// First sync: full fetch succeeds, sets fields.
	pr := buildOpenPR(1, now)
	additions := 10
	deletions := 5
	mergeableState := "dirty"
	pr.Additions = &additions
	pr.Deletions = &deletions
	pr.MergeableState = &mergeableState

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{pr},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))
	syncer.RunOnce(ctx)

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.Equal("dirty", stored.MergeableState)
	require.Equal(10, stored.Additions)

	// Second sync: bump UpdatedAt so needsTimeline triggers, but full
	// fetch fails. Fields from the existing row should be preserved.
	later := now.Add(time.Hour)
	listPR := buildOpenPR(1, later)
	mc.openPRs = []*gh.PullRequest{listPR}
	mc.getPullRequestFn = func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
		return nil, fmt.Errorf("transient network error")
	}

	syncer.RunOnce(ctx)

	stored2, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	assert.Equal("dirty", stored2.MergeableState, "MergeableState should survive a failed full fetch")
	assert.Equal(10, stored2.Additions, "Additions should survive a failed full fetch")
	assert.Equal(5, stored2.Deletions, "Deletions should survive a failed full fetch")
}

func TestSyncStatusUpdated(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(500))

	before := time.Now()
	syncer.RunOnce(ctx)
	after := time.Now()

	status := syncer.Status()
	assert.False(status.Running)
	assert.False(status.LastRunAt.IsZero())
	assert.Condition(func() bool {
		return !status.LastRunAt.Before(before) && !status.LastRunAt.After(after)
	}, "status.LastRunAt %v should be between %v and %v", status.LastRunAt, before, after)
	assert.Empty(status.LastError)
}

// blockingMockClient embeds mockClient but blocks in
// ListOpenPullRequests until the provided channel is closed.
type blockingMockClient struct {
	mockClient
	entered chan struct{}
	blocked chan struct{}
}

func (b *blockingMockClient) ListOpenPullRequests(
	_ context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	if b.entered != nil {
		select {
		case b.entered <- struct{}{}:
		default:
		}
	}
	<-b.blocked
	return nil, nil
}

func TestSyncerStopWaitsForRunOnce(t *testing.T) {
	entered := make(chan struct{})
	blocked := make(chan struct{})
	mock := &blockingMockClient{
		entered: entered,
		blocked: blocked,
	}

	database := openTestDB(t)
	syncer := NewSyncer(
		map[string]Client{"github.com": mock}, database, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Hour, nil, nil,
	)

	syncer.Start(t.Context())

	// Wait for the goroutine to enter the blocked ListOpenPullRequests.
	<-entered

	// Call Stop while RunOnce is still in flight.
	stopped := make(chan struct{})
	go func() {
		syncer.Stop()
		close(stopped)
	}()

	// Stop should NOT return yet — RunOnce is still blocked.
	select {
	case <-stopped:
		require.Fail(t, "Stop returned while RunOnce was still in flight")
	case <-time.After(100 * time.Millisecond):
	}

	// Unblock RunOnce and verify Stop completes.
	close(blocked)

	select {
	case <-stopped:
		// Stop waited for RunOnce to finish.
	case <-time.After(5 * time.Second):
		require.Fail(t, "Stop did not return within timeout")
	}
}

// parallelMockClient tracks the maximum number of concurrent
// ListOpenPullRequests calls. Each call blocks until block is
// closed, so the in-flight count saturates at the worker pool size.
type parallelMockClient struct {
	mockClient
	inflight    atomic.Int32
	maxInflight atomic.Int32
	block       chan struct{}
}

func (p *parallelMockClient) ListOpenPullRequests(
	_ context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	n := p.inflight.Add(1)
	defer p.inflight.Add(-1)
	for {
		current := p.maxInflight.Load()
		if n <= current || p.maxInflight.CompareAndSwap(current, n) {
			break
		}
	}
	<-p.block
	return nil, nil
}

func TestRunOnceSyncesReposInParallel(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	const parallelism = 3
	const repoCount = 5

	mc := &parallelMockClient{block: make(chan struct{})}
	repos := make([]RepoRef, repoCount)
	for i := range repos {
		repos[i] = RepoRef{
			Owner:        "o",
			Name:         fmt.Sprintf("r%d", i),
			PlatformHost: "github.com",
		}
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil, repos,
		time.Minute, nil, nil,
	)
	syncer.SetParallelism(parallelism)

	done := make(chan struct{})
	go func() {
		syncer.RunOnce(ctx)
		close(done)
	}()

	require.Eventually(func() bool {
		return mc.inflight.Load() == int32(parallelism)
	}, 2*time.Second, 5*time.Millisecond,
		"expected %d concurrent syncs, got %d",
		parallelism, mc.inflight.Load())

	// Hold the saturation point briefly to ensure no extra workers
	// sneak past the bound.
	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(mc.maxInflight.Load(), int32(parallelism),
		"max concurrency exceeded bound")

	close(mc.block)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail("RunOnce did not complete after unblocking workers")
	}

	assert.Equal(int32(parallelism), mc.maxInflight.Load(),
		"should have reached the parallelism bound exactly")
}

func TestRunOnceCancelDuringBackoffDoesNotReportSuccess(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	// Put the rate tracker into a long backoff window so every
	// worker blocks on the select inside RunOnce rather than
	// calling the client.
	rt := NewRateTracker(d, "github.com", "rest")
	resetAt := time.Now().Add(time.Hour)
	rt.UpdateFromRate(gh.Rate{
		Remaining: 0,
		Reset:     gh.Timestamp{Time: resetAt},
	})

	mc := &mockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Minute,
		map[string]*RateTracker{"github.com": rt}, nil,
	)

	var completedCalled atomic.Bool
	syncer.SetOnSyncCompleted(func([]RepoSyncResult) {
		completedCalled.Store(true)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		syncer.RunOnce(ctx)
		close(done)
	}()

	// Give RunOnce time to reach the backoff select.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail("RunOnce did not return after ctx cancel")
	}

	assert.False(completedCalled.Load(),
		"onSyncCompleted must not fire when RunOnce is canceled")
	status := syncer.Status()
	assert.False(status.Running)
	assert.NotEmpty(status.LastError,
		"LastError should reflect the cancellation")
}

func TestRunOnceCancelAfterCompleteReportsSuccess(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	// Empty repo list + pre-canceled context. All work is trivially
	// "done" (completed == total == 0), so the cancel-cleanup path
	// must NOT treat the run as a failure. This exercises the gate
	// that distinguishes "canceled mid-flight" from "canceled after
	// every worker already finished".
	mc := &mockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{}, time.Minute, nil, nil,
	)

	var completedCalled atomic.Bool
	syncer.SetOnSyncCompleted(func([]RepoSyncResult) {
		completedCalled.Store(true)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		syncer.RunOnce(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail("RunOnce did not return")
	}

	assert.True(completedCalled.Load(),
		"onSyncCompleted should fire when no work was outstanding "+
			"at cancel time")
	status := syncer.Status()
	assert.False(status.Running)
	assert.Empty(status.LastError,
		"LastError should be empty when all work completed before cancel")
}

// cancelDuringSyncMockClient blocks ListOpenPullRequests until ctx
// is canceled, then returns ctx.Err(). This simulates a real
// network call that respects ctx and aborts mid-sync.
type cancelDuringSyncMockClient struct {
	mockClient
	entered chan struct{}
}

func (c *cancelDuringSyncMockClient) ListOpenPullRequests(
	ctx context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunOnceCancelDuringSyncRepoDoesNotReportSuccess(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	// Worker is mid-syncRepo when ctx cancels. syncRepo's API call
	// returns ctx.Err(). The worker must NOT count this repo as
	// completed, or the cancel-cleanup gate (completed < total) will
	// incorrectly fall through to the success path and fire
	// onSyncCompleted on a partially-aborted run.
	mc := &cancelDuringSyncMockClient{
		entered: make(chan struct{}, 1),
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	var completedCalled atomic.Bool
	syncer.SetOnSyncCompleted(func([]RepoSyncResult) {
		completedCalled.Store(true)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		syncer.RunOnce(ctx)
		close(done)
	}()

	select {
	case <-mc.entered:
	case <-time.After(2 * time.Second):
		require.Fail("worker did not enter ListOpenPullRequests")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail("RunOnce did not return")
	}

	assert.False(completedCalled.Load(),
		"onSyncCompleted must not fire when syncRepo was canceled "+
			"mid-flight")
	status := syncer.Status()
	assert.False(status.Running)
	assert.NotEmpty(status.LastError,
		"LastError should reflect the cancellation")
}

// deadlineExceededMockClient returns a wrapped context.DeadlineExceeded
// from ListOpenPullRequests. This simulates a per-request timeout
// (e.g. http.Client.Timeout firing) where the call's own deadline
// expired even though the run context is still alive.
type deadlineExceededMockClient struct {
	mockClient
}

func (c *deadlineExceededMockClient) ListOpenPullRequests(
	_ context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	return nil, fmt.Errorf("list timed out: %w", context.DeadlineExceeded)
}

func TestRunOncePerRequestDeadlineRecordsError(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	// syncRepo returns a wrapped context.DeadlineExceeded but the
	// run context is still alive. The worker must NOT mistake this
	// for a run-cancellation and bail silently — that would drop
	// the error from lastErr and report a clean status despite a
	// failed sync. Per-request timeouts must reach the normal
	// error-handling path so they show up in LastError.
	mc := &deadlineExceededMockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	var completedCalled atomic.Bool
	syncer.SetOnSyncCompleted(func([]RepoSyncResult) {
		completedCalled.Store(true)
	})

	syncer.RunOnce(context.Background())

	status := syncer.Status()
	assert.False(status.Running)
	assert.NotEmpty(status.LastError,
		"per-request DeadlineExceeded should be recorded in LastError")
	assert.Contains(status.LastError, "list timed out",
		"LastError should preserve the wrapped error message")
	require.True(completedCalled.Load(),
		"onSyncCompleted should fire on a finished run with errors")
}

// syncedWriter wraps an io.Writer with a mutex for safe concurrent
// writes from multiple goroutines, used to capture slog output in
// tests where workers run in parallel.
type syncedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (sw *syncedWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

func TestRunOnceDispatchHonorsCanceledCtx(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	// When ctx is already canceled before dispatch starts, the
	// dispatch loop must NOT enqueue any repos. Go's select picks
	// pseudo-randomly when both branches are ready, so a naive
	// `select { case work <- r: case <-ctx.Done(): }` can still
	// hand a repo to a ready worker after cancellation. Each
	// dispatched repo causes the worker to log "syncing repo"
	// before bailing on the canceled DB call, so counting those
	// log lines is the cleanest way to observe whether dispatch
	// is honoring cancel.
	var buf bytes.Buffer
	sw := &syncedWriter{w: &buf}
	h := slog.NewTextHandler(sw, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	orig := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(orig) })

	repos := make([]RepoRef, 100)
	for i := range repos {
		repos[i] = RepoRef{
			Owner:        "o",
			Name:         fmt.Sprintf("r%d", i),
			PlatformHost: "github.com",
		}
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": &mockClient{}}, d, nil,
		repos, time.Minute, nil, nil,
	)
	syncer.SetParallelism(4)

	// Run several iterations to maximize the chance the dispatch
	// race manifests if the ctx pre-check is missing. Each iter
	// uses a fresh pre-canceled ctx.
	for range 20 {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		syncer.RunOnce(ctx)
	}

	sw.mu.Lock()
	output := buf.String()
	sw.mu.Unlock()

	count := strings.Count(output, `msg="syncing repo"`)
	assert.Zero(count,
		"dispatch must not enqueue repos when ctx is pre-canceled "+
			"(observed %d 'syncing repo' log lines)", count)
}

// TestSyncMRReturnsErrorOnNilPullRequest verifies SyncMR returns
// a clear error when a Client returns (nil, nil) from
// GetPullRequest, instead of dereferencing nil in NormalizePR.
func TestSyncMRReturnsErrorOnNilPullRequest(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)

	mc := &mockClient{
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return nil, nil
		},
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	err := syncer.SyncMR(context.Background(), "owner", "repo", 1)
	require.Error(err)
	require.Contains(err.Error(), "nil pull request")
}

// TestRunOnceSyncOpenMRSurvivesNilFullPR verifies the periodic
// sync path does not panic when GetPullRequest returns (nil, nil)
// during syncOpenMR's full-PR fetch. It must fall back to the
// list-derived data and complete the sync.
func TestRunOnceSyncOpenMRSurvivesNilFullPR(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mc := &mockClient{
		openPRs: []*gh.PullRequest{buildOpenPR(7, now)},
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			// Contract violation: return (nil, nil). Periodic
			// sync must not panic on this.
			return nil, nil
		},
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// Must complete without panic.
	syncer.RunOnce(ctx)

	// The list-derived PR should still be persisted because the
	// nil full-PR fetch is non-fatal.
	pr, err := d.GetMergeRequest(ctx, "owner", "repo", 7)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal(7, pr.Number)
}

// trackingClient records every ListOpenPullRequests invocation
// so a test can assert that runWorker did not start any work.
type trackingClient struct {
	mockClient
	listCalls atomic.Int32
}

func (c *trackingClient) ListOpenPullRequests(
	_ context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	c.listCalls.Add(1)
	return nil, nil
}

// TestRunWorkerBailsOnCanceledCtx verifies the worker-side ctx
// check fires before any work is done. The dispatch race fix
// pre-checks ctx before the select, but a cancel can still land
// in the micro-window between pre-check and send and Go may pick
// the send branch. The worker must then discard the repo before
// logging "syncing repo" or calling syncRepo.
//
// This test exercises that path directly: it pre-loads a buffered
// work channel, cancels ctx, and calls runWorker with the
// canceled ctx. With the worker-side check the function returns
// without invoking the client; without the check it would log
// "syncing repo" and increment the completed counter.
func TestRunWorkerBailsOnCanceledCtx(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	var buf bytes.Buffer
	sw := &syncedWriter{w: &buf}
	h := slog.NewTextHandler(sw, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	orig := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(orig) })

	tc := &trackingClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": tc}, d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// Pre-load three repos so the worker would naturally drain
	// all three if the ctx check were missing.
	work := make(chan repoWork, 3)
	for i := range 3 {
		work <- repoWork{
			index: i,
			repo: RepoRef{
				Owner:        "o",
				Name:         fmt.Sprintf("r%d", i),
				PlatformHost: "github.com",
			},
		}
	}
	close(work)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var (
		completed atomic.Int32
		maxShown  atomic.Int32
		errMu     sync.Mutex
		lastErr   string
		canceled  atomic.Bool
	)
	state := &runState{
		completed: &completed,
		maxShown:  &maxShown,
		errMu:     &errMu,
		lastErr:   &lastErr,
		canceled:  &canceled,
		total:     3,
		results:   make([]RepoSyncResult, 3),
	}
	syncer.runWorker(ctx, work, state)

	sw.mu.Lock()
	output := buf.String()
	sw.mu.Unlock()

	assert.Zero(strings.Count(output, `msg="syncing repo"`),
		"runWorker must not log 'syncing repo' when ctx is canceled")
	assert.Zero(int(completed.Load()),
		"runWorker must not increment completed when ctx is canceled")
	assert.Zero(int(tc.listCalls.Load()),
		"runWorker must not call the GitHub client when ctx is canceled")
	assert.Empty(lastErr, "runWorker must not record an error when bailing on ctx")
}

// dedupGetUserClient blocks on GetUser calls to force concurrent
// display-name lookups into a race. It also tracks how many
// GetUser calls actually hit it.
type dedupGetUserClient struct {
	mockClient
	getUserCount atomic.Int32
	block        chan struct{}
}

func (c *dedupGetUserClient) GetUser(
	_ context.Context, login string,
) (*gh.User, error) {
	c.getUserCount.Add(1)
	<-c.block
	name := "Display " + login
	return &gh.User{Login: &login, Name: &name}, nil
}

func TestResolveDisplayNameDedupsConcurrentLookups(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	author := "alice"
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	// Build two PRs in two repos, both authored by "alice".
	buildAuthoredPR := func(num int) *gh.PullRequest {
		pr := buildOpenPR(num, now)
		pr.User = &gh.User{Login: &author}
		return pr
	}

	mc := &dedupGetUserClient{block: make(chan struct{})}
	mc.openPRs = []*gh.PullRequest{
		buildAuthoredPR(1),
		buildAuthoredPR(2),
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{
			{Owner: "o", Name: "r1", PlatformHost: "github.com"},
			{Owner: "o", Name: "r2", PlatformHost: "github.com"},
		},
		time.Minute, nil, nil,
	)
	syncer.SetParallelism(2)

	done := make(chan struct{})
	go func() {
		syncer.RunOnce(ctx)
		close(done)
	}()

	// Wait until at least one worker has entered GetUser. Sleeping
	// does not prove the second worker has arrived yet, but the
	// blocked fn holds the singleflight slot open until we release
	// it, so any arriving worker will be coalesced.
	require.Eventually(func() bool {
		return mc.getUserCount.Load() >= 1
	}, 2*time.Second, 5*time.Millisecond,
		"no worker reached GetUser")

	// Give the second worker plenty of time to enter singleflight.
	time.Sleep(100 * time.Millisecond)

	close(mc.block)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail("RunOnce did not complete")
	}

	assert.Equal(int32(1), mc.getUserCount.Load(),
		"concurrent display-name lookups for same author "+
			"should coalesce into one GetUser call")
}

func TestIsTrackedRepo(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	mc := &mockClient{}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, database, nil, []RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
		{Owner: "corp", Name: "lib", PlatformHost: "github.com"},
	}, time.Minute, nil, nil)

	assert.True(syncer.IsTrackedRepo("acme", "widget"))
	assert.True(syncer.IsTrackedRepo("corp", "lib"))
	assert.False(syncer.IsTrackedRepo("acme", "other"))
	assert.False(syncer.IsTrackedRepo("nobody", "widget"))
}

func TestSyncItemByNumber_Issue(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	number := 42
	title := "Bug report"
	state := "closed"
	author := "testuser"
	now := time.Now()
	ghTime := &gh.Timestamp{Time: now}

	mc := &mockClient{
		getIssueFn: func(_ context.Context, _, _ string, n int) (*gh.Issue, error) {
			if n != number {
				return nil, fmt.Errorf("unexpected number %d", n)
			}
			return &gh.Issue{
				ID:        new(int64(9999)),
				Number:    &number,
				Title:     &title,
				State:     &state,
				User:      &gh.User{Login: &author},
				HTMLURL:   new("https://github.com/acme/widget/issues/42"),
				CreatedAt: ghTime,
				UpdatedAt: ghTime,
			}, nil
		},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, database, nil, []RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
	}, time.Minute, nil, nil)

	itemType, err := syncer.SyncItemByNumber(ctx, "acme", "widget", number)
	require.NoError(err)
	assert.Equal("issue", itemType)

	issue, err := database.GetIssue(ctx, "acme", "widget", number)
	require.NoError(err)
	assert.NotNil(issue)
	assert.Equal(title, issue.Title)
}

func TestSyncItemByNumber_PR(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	number := 10
	title := "Add feature"
	state := "open"
	author := "testuser"
	now := time.Now()
	ghTime := &gh.Timestamp{Time: now}
	prURL := "https://github.com/acme/widget/pull/10"

	mc := &mockClient{
		getIssueFn: func(_ context.Context, _, _ string, n int) (*gh.Issue, error) {
			return &gh.Issue{
				ID:      new(int64(8888)),
				Number:  &number,
				Title:   &title,
				State:   &state,
				User:    &gh.User{Login: &author},
				HTMLURL: new(prURL),
				PullRequestLinks: &gh.PullRequestLinks{
					URL: &prURL,
				},
				CreatedAt: ghTime,
				UpdatedAt: ghTime,
			}, nil
		},
		singlePR: &gh.PullRequest{
			ID:      new(int64(8888)),
			Number:  &number,
			Title:   &title,
			State:   &state,
			User:    &gh.User{Login: &author},
			HTMLURL: &prURL,
			Head: &gh.PullRequestBranch{
				Ref: new("feature"),
				SHA: new("abc123"),
			},
			Base:      &gh.PullRequestBranch{Ref: new("main")},
			CreatedAt: ghTime,
			UpdatedAt: ghTime,
		},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, database, nil, []RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
	}, time.Minute, nil, nil)

	itemType, err := syncer.SyncItemByNumber(ctx, "acme", "widget", number)
	require.NoError(err)
	assert.Equal("pr", itemType)

	pr, err := database.GetMergeRequest(ctx, "acme", "widget", number)
	require.NoError(err)
	assert.NotNil(pr)
	assert.Equal(title, pr.Title)
}

func TestSyncItemByNumber_UntrackedRepo(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	database := openTestDB(t)
	ctx := context.Background()

	mc := &mockClient{}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, database, nil, []RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
	}, time.Minute, nil, nil)

	_, err := syncer.SyncItemByNumber(ctx, "other", "repo", 1)
	require.Error(err)
	assert.Contains(err.Error(), "not tracked")
}

func TestSyncerMultiHostClientDispatch(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	ghMock := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}
	gheMock := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	clients := map[string]Client{
		"github.com":   ghMock,
		"ghe.corp.com": gheMock,
	}
	repos := []RepoRef{
		{Owner: "pub", Name: "repo", PlatformHost: "github.com"},
		{Owner: "corp", Name: "internal", PlatformHost: "ghe.corp.com"},
	}

	syncer := NewSyncer(clients, d, nil, repos, time.Minute, nil, nil)
	syncer.RunOnce(ctx)

	assert.True(ghMock.listOpenPRsCalled,
		"github.com mock should have been called")
	assert.True(gheMock.listOpenPRsCalled,
		"ghe.corp.com mock should have been called")
}

func TestOnMRSyncedCalledDuringSync(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mc := &mockClient{
		openPRs:  []*gh.PullRequest{buildOpenPR(1, now)},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, testBudget(500),
	)

	type hookCall struct {
		owner        string
		name         string
		number       int
		ciChecksJSON string
		updatedAt    time.Time
	}
	var called []hookCall
	syncer.SetOnMRSynced(func(owner, name string, mr *db.MergeRequest) {
		called = append(called, hookCall{
			owner:        owner,
			name:         name,
			number:       mr.Number,
			ciChecksJSON: mr.CIChecksJSON,
			updatedAt:    mr.UpdatedAt,
		})
	})

	syncer.RunOnce(ctx)

	require.Len(called, 1)
	assert.Equal("owner", called[0].owner)
	assert.Equal("repo", called[0].name)
	assert.Equal(1, called[0].number)
	assert.True(called[0].updatedAt.Equal(now),
		"UpdatedAt should match the PR's UpdatedAt")
}

func TestOnMRSyncedIncludesCIChecksJSON(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ciState := "success"
	checkName := "build"
	checkStatus := "completed"
	checkConclusion := "success"
	mc := &mockClient{
		openPRs:  []*gh.PullRequest{buildOpenPR(1, now)},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
	}
	mc.checkRuns = []*gh.CheckRun{
		{
			Name:       &checkName,
			Status:     &checkStatus,
			Conclusion: &checkConclusion,
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "owner", Name: "repo",
			PlatformHost: "github.com",
		}},
		time.Minute, nil, testBudget(500),
	)

	var gotJSON string
	syncer.SetOnMRSynced(
		func(_ string, _ string, mr *db.MergeRequest) {
			gotJSON = mr.CIChecksJSON
		},
	)

	syncer.RunOnce(ctx)

	assert.Contains(gotJSON, "build",
		"CIChecksJSON should contain check run name")
}

func TestOnSyncCompletedCalledAfterSync(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{
			{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
			{Owner: "acme", Name: "lib", PlatformHost: "github.com"},
		},
		time.Minute, nil, nil,
	)

	var gotResults []RepoSyncResult
	syncer.SetOnSyncCompleted(func(results []RepoSyncResult) {
		gotResults = results
	})

	syncer.RunOnce(ctx)

	require.Len(gotResults, 2)
	assert.Equal("acme", gotResults[0].Owner)
	assert.Equal("widget", gotResults[0].Name)
	assert.Equal("github.com", gotResults[0].PlatformHost)
	assert.Empty(gotResults[0].Error)
	assert.Equal("acme", gotResults[1].Owner)
	assert.Equal("lib", gotResults[1].Name)
	assert.Equal("github.com", gotResults[1].PlatformHost)
	assert.Empty(gotResults[1].Error)
}

func TestNilHooksNoOp(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// No hooks set -- should not panic.
	syncer.RunOnce(ctx)
}

func TestWatchedMRsSyncedOnFastInterval(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := t.Context()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	pr := buildOpenPR(7, now)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		singlePR: pr,
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "acme", Name: "app",
			PlatformHost: "github.com",
		}},
		time.Hour, nil, nil, // bulk sync at 1h -- won't fire during test
	)
	syncer.SetWatchInterval(50 * time.Millisecond)

	var mu sync.Mutex
	var hookCalls []int
	syncer.SetOnMRSynced(
		func(_ string, _ string, mr *db.MergeRequest) {
			mu.Lock()
			hookCalls = append(hookCalls, mr.Number)
			mu.Unlock()
		},
	)

	syncer.SetWatchedMRs([]WatchedMR{
		{Owner: "acme", Name: "app", Number: 7},
	})

	syncer.Start(ctx)
	defer syncer.Stop()

	// Wait for at least one fast-sync tick.
	assert.Eventually(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(hookCalls) >= 1
	}, 2*time.Second, 20*time.Millisecond)

	// Verify the MR was persisted.
	mr, err := d.GetMergeRequest(ctx, "acme", "app", 7)
	require.NoError(err)
	require.NotNil(mr)
	assert.Equal(7, mr.Number)
}

func TestEmptyWatchListNoOp(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	mc := &mockClient{
		openPRs: []*gh.PullRequest{},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "acme", Name: "app",
			PlatformHost: "github.com",
		}},
		time.Hour, nil, nil,
	)
	syncer.SetWatchInterval(50 * time.Millisecond)

	callCount := 0
	syncer.SetOnMRSynced(
		func(_ string, _ string, _ *db.MergeRequest) {
			callCount++
		},
	)

	// Leave watch list empty.
	syncer.Start(ctx)

	// Let several ticks pass.
	time.Sleep(200 * time.Millisecond)
	syncer.Stop()

	Assert.Equal(t, 0, callCount,
		"empty watch list should not trigger any MR syncs")
}

func TestSetWatchedMRsReplacesList(t *testing.T) {
	assert := Assert.New(t)
	ctx := t.Context()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}
	// Return different PRs based on the requested number.
	mc.getPullRequestFn = func(
		_ context.Context, _, _ string, number int,
	) (*gh.PullRequest, error) {
		return buildOpenPR(number, now), nil
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "acme", Name: "app",
			PlatformHost: "github.com",
		}},
		time.Hour, nil, nil,
	)
	syncer.SetWatchInterval(50 * time.Millisecond)

	var mu sync.Mutex
	syncedNumbers := map[int]int{} // number -> count
	syncer.SetOnMRSynced(
		func(_ string, _ string, mr *db.MergeRequest) {
			mu.Lock()
			syncedNumbers[mr.Number]++
			mu.Unlock()
		},
	)

	// Start with PR #1 on the watch list.
	syncer.SetWatchedMRs([]WatchedMR{
		{Owner: "acme", Name: "app", Number: 1},
	})
	syncer.Start(ctx)
	defer syncer.Stop()

	// Wait for PR #1 to be synced.
	assert.Eventually(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return syncedNumbers[1] >= 1
	}, 2*time.Second, 20*time.Millisecond)

	// Replace with PR #2 only.
	mu.Lock()
	countPR1Before := syncedNumbers[1]
	mu.Unlock()

	syncer.SetWatchedMRs([]WatchedMR{
		{Owner: "acme", Name: "app", Number: 2},
	})

	// Wait for PR #2 to be synced.
	assert.Eventually(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return syncedNumbers[2] >= 1
	}, 2*time.Second, 20*time.Millisecond)

	// PR #1 should not accumulate many more syncs after replacement.
	// Allow at most 1 extra (for an in-flight tick at replacement time).
	mu.Lock()
	countPR1After := syncedNumbers[1]
	mu.Unlock()
	assert.LessOrEqual(countPR1After, countPR1Before+1,
		"PR #1 should stop being synced after watch list replacement")
}

func TestWatchedMRsSkipRateLimitedHost(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := t.Context()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		singlePR: buildOpenPR(5, now),
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	rt := NewRateTracker(d, "github.com", "rest")
	// Exhaust the rate limit with a future reset.
	futureReset := time.Now().Add(30 * time.Minute)
	rt.UpdateFromRate(gh.Rate{
		Remaining: 0,
		Reset:     gh.Timestamp{Time: futureReset},
	})

	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "acme", Name: "app",
			PlatformHost: "github.com",
		}},
		time.Hour,
		map[string]*RateTracker{"github.com": rt}, nil,
	)
	syncer.SetWatchInterval(50 * time.Millisecond)

	callCount := 0
	syncer.SetOnMRSynced(
		func(_ string, _ string, _ *db.MergeRequest) {
			callCount++
		},
	)

	syncer.SetWatchedMRs([]WatchedMR{
		{
			Owner: "acme", Name: "app",
			Number: 5, PlatformHost: "github.com",
		},
	})

	// Call syncWatchedMRs directly to avoid the bulk RunOnce goroutine.
	syncer.syncWatchedMRs(ctx)

	assert.Equal(0, callCount,
		"watched MRs should be skipped when host is rate-limited")
}

func TestWatchedMROnGHEHost(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := t.Context()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	gheMC := &mockClient{
		openPRs:  []*gh.PullRequest{},
		singlePR: buildOpenPR(3, now),
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	syncer := NewSyncer(
		map[string]Client{"ghes.corp.com": gheMC}, d, nil,
		[]RepoRef{{
			Owner: "corp", Name: "internal",
			PlatformHost: "ghes.corp.com",
		}},
		time.Hour, nil, nil,
	)

	var hookedOwner, hookedName string
	syncer.SetOnMRSynced(
		func(owner, name string, _ *db.MergeRequest) {
			hookedOwner = owner
			hookedName = name
		},
	)

	syncer.SetWatchedMRs([]WatchedMR{
		{
			Owner: "corp", Name: "internal",
			Number: 3, PlatformHost: "ghes.corp.com",
		},
	})

	syncer.syncWatchedMRs(ctx)

	// The MR should have been synced via the GHE client.
	mr, err := d.GetMergeRequest(ctx, "corp", "internal", 3)
	require.NoError(err)
	require.NotNil(mr)
	assert.Equal(3, mr.Number)
	assert.Equal("corp", hookedOwner)
	assert.Equal("internal", hookedName)

	// Verify the MR is associated with the GHE repo row, not github.com.
	repo, err := d.GetRepoByOwnerName(ctx, "corp", "internal")
	require.NoError(err)
	require.NotNil(repo)
	assert.Equal("ghes.corp.com", repo.PlatformHost)
	assert.Equal(repo.ID, mr.RepoID)
}

func TestWatchedMRRejectsUnmatchedHost(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mc := &mockClient{
		openPRs:  []*gh.PullRequest{},
		singlePR: buildOpenPR(1, now),
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	// Track acme/app only on github.com.
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "acme", Name: "app",
			PlatformHost: "github.com",
		}},
		time.Hour, nil, nil,
	)

	callCount := 0
	syncer.SetOnMRSynced(
		func(_ string, _ string, _ *db.MergeRequest) {
			callCount++
		},
	)

	// Watch the same owner/name but on a different host.
	syncer.SetWatchedMRs([]WatchedMR{
		{
			Owner: "acme", Name: "app",
			Number: 1, PlatformHost: "ghes.other.com",
		},
	})

	syncer.syncWatchedMRs(ctx)

	Assert.Equal(t, 0, callCount,
		"watched MR on untracked host should not be synced")
}

func TestRunOnceSkipsThrottledHosts(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	ghMock := &mockClient{
		openPRs:  []*gh.PullRequest{},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}
	gheMock := &mockClient{
		openPRs:  []*gh.PullRequest{buildOpenPR(1, now)},
		comments: []*gh.IssueComment{},
		reviews:  []*gh.PullRequestReview{},
		commits:  []*gh.RepositoryCommit{},
	}

	// Set up GHE tracker with remaining below reserve buffer.
	gheTracker := NewRateTracker(d, "ghe.corp.com", "rest")
	gheTracker.UpdateFromRate(gh.Rate{
		Limit:     5000,
		Remaining: 100, // below RateReserveBuffer (200)
		Reset:     gh.Timestamp{Time: time.Now().Add(30 * time.Minute)},
	})

	clients := map[string]Client{
		"github.com":   ghMock,
		"ghe.corp.com": gheMock,
	}
	trackers := map[string]*RateTracker{
		"ghe.corp.com": gheTracker,
	}
	repos := []RepoRef{
		{Owner: "pub", Name: "repo", PlatformHost: "github.com"},
		{Owner: "corp", Name: "internal", PlatformHost: "ghe.corp.com"},
	}

	syncer := NewSyncer(clients, d, nil, repos, time.Minute, trackers, nil)

	var gotResults []RepoSyncResult
	syncer.SetOnSyncCompleted(func(results []RepoSyncResult) {
		gotResults = results
	})

	syncer.RunOnce(ctx)

	require.Len(gotResults, 2)

	// github.com repo should have synced (no error).
	assert.Equal("pub", gotResults[0].Owner)
	assert.Equal("repo", gotResults[0].Name)
	assert.Equal("github.com", gotResults[0].PlatformHost)
	assert.Empty(gotResults[0].Error,
		"github.com repo should sync normally")

	// ghe.corp.com repo should be skipped due to throttle.
	assert.Equal("corp", gotResults[1].Owner)
	assert.Equal("internal", gotResults[1].Name)
	assert.Equal("ghe.corp.com", gotResults[1].PlatformHost)
	assert.Equal("skipped: rate limit throttled", gotResults[1].Error,
		"ghe.corp.com repo should be skipped when paused")

	// github.com mock should have been called, GHE should not.
	assert.True(ghMock.listOpenPRsCalled,
		"github.com client should have been called")
	assert.False(gheMock.listOpenPRsCalled,
		"ghe.corp.com client should NOT have been called")
}

// ignoresCancelClient embeds mockClient and triggers an outer
// cancel() on the first ListOpenIssues call while still returning
// (nil, nil) successfully. This simulates a Client implementation
// that ignores ctx cancellation mid-call -- the defensive case
// the RunOnce cancel latch must handle.
type ignoresCancelClient struct {
	mockClient
	cancel context.CancelFunc
	once   sync.Once
}

func (c *ignoresCancelClient) ListOpenIssues(
	_ context.Context, _, _ string,
) ([]*gh.Issue, error) {
	c.once.Do(c.cancel)
	return nil, nil
}

// TestRunOnceLatchesCancelWhenSyncRepoIgnoresCtx covers the
// defense-in-depth gap flagged on commit 45a5421: if a Client
// ignores ctx cancellation mid-sync and every call still returns
// success, syncRepo will return nil after ctx has been canceled.
// Under the old completed-count heuristic (`completed < total`)
// the run was misreported as a clean completion -- onSyncCompleted
// fired even though the user had asked to cancel. The latched
// cancel flag must catch this case and route through the cancel
// status path instead.
func TestRunOnceLatchesCancelWhenSyncRepoIgnoresCtx(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	c := &ignoresCancelClient{cancel: cancel}

	syncer := NewSyncer(
		map[string]Client{"github.com": c}, d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	var syncCompletedCalls atomic.Int32
	syncer.SetOnSyncCompleted(func(_ []RepoSyncResult) {
		syncCompletedCalls.Add(1)
	})

	syncer.RunOnce(ctx)

	assert.Zero(int(syncCompletedCalls.Load()),
		"onSyncCompleted must not fire when ctx was canceled "+
			"during the run, even if syncRepo returned success")
	status := syncer.Status()
	assert.False(status.Running, "sync must stop")
	assert.NotEmpty(status.LastError,
		"status must record the cancel as an error")
}

// --- Index/Detail Split Tests ---

// detailTrackingClient tracks which API methods are called so tests
// can verify that the index phase does NOT call GetPullRequest while
// the detail drain does.
type detailTrackingClient struct {
	mockClient
	getPRCalls atomic.Int32
}

func (c *detailTrackingClient) GetPullRequest(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	c.trackCall()
	c.getPRCalls.Add(1)
	return c.mockClient.GetPullRequest(ctx, owner, repo, number)
}

func TestRunOnceIndexOnly(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mc := &detailTrackingClient{}
	mc.openPRs = []*gh.PullRequest{
		buildOpenPR(1, now),
		buildOpenPR(2, now),
	}

	// Budget=0 disables detail drain entirely.
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "owner", Name: "repo",
			PlatformHost: "github.com",
		}},
		time.Minute, nil, nil,
	)

	syncer.RunOnce(ctx)

	// ListOpenPullRequests should have been called.
	assert.True(mc.listOpenPRsCalled,
		"index scan should call ListOpenPullRequests")

	// GetPullRequest should NOT have been called (no detail fetch).
	assert.Zero(int(mc.getPRCalls.Load()),
		"index-only sync should not call GetPullRequest")

	// PRs should be in DB with nil detail_fetched_at.
	pr1, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(pr1)
	assert.Equal(1, pr1.Number)
	assert.Nil(pr1.DetailFetchedAt,
		"detail_fetched_at should be nil after index-only sync")

	pr2, err := d.GetMergeRequest(ctx, "owner", "repo", 2)
	require.NoError(err)
	require.NotNil(pr2)
	assert.Equal(2, pr2.Number)
	assert.Nil(pr2.DetailFetchedAt,
		"detail_fetched_at should be nil after index-only sync")

	// No timeline events should exist (no detail fetch).
	events, err := d.ListMREvents(ctx, pr1.ID)
	require.NoError(err)
	assert.Empty(events,
		"no events should exist after index-only sync")
}

func TestRunOnceDetailDrain(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ciState := "success"

	mc := &detailTrackingClient{}
	mc.openPRs = []*gh.PullRequest{
		buildOpenPR(1, now),
		buildOpenPR(2, now),
	}
	mc.comments = []*gh.IssueComment{}
	mc.reviews = []*gh.PullRequestReview{}
	mc.commits = []*gh.RepositoryCommit{}
	mc.ciStatus = &gh.CombinedStatus{State: &ciState}

	// Budget=500 allows detail drain to run.
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "owner", Name: "repo",
			PlatformHost: "github.com",
		}},
		time.Minute, nil, testBudget(500),
	)

	syncer.RunOnce(ctx)

	// GetPullRequest should have been called for each PR
	// during detail drain.
	assert.GreaterOrEqual(int(mc.getPRCalls.Load()), 2,
		"detail drain should call GetPullRequest for open PRs")

	// Both PRs should have detail_fetched_at set.
	pr1, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(pr1)
	assert.NotNil(pr1.DetailFetchedAt,
		"detail_fetched_at should be set after detail drain")

	pr2, err := d.GetMergeRequest(ctx, "owner", "repo", 2)
	require.NoError(err)
	require.NotNil(pr2)
	assert.NotNil(pr2.DetailFetchedAt,
		"detail_fetched_at should be set after detail drain")
}

func TestDetailDrainRespectsBudget(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ciState := "success"

	// Create 5 PRs.
	var prs []*gh.PullRequest
	for i := 1; i <= 5; i++ {
		prs = append(prs, buildOpenPR(i, now))
	}

	// Index overhead: GetRepo(1) + ListPRs(1) + ListIssues(1) +
	// GetUser(1, deduplicated by singleflight) = 4 calls. One PR
	// detail = 8 calls. Budget of 15 covers index + 1 detail (12)
	// with 3 remaining, which is below the 8 needed for a 2nd.
	budget := testBudget(15)
	mc := &detailTrackingClient{}
	mc.budget = budget["github.com"]
	mc.openPRs = prs
	mc.comments = []*gh.IssueComment{}
	mc.reviews = []*gh.PullRequestReview{}
	mc.commits = []*gh.RepositoryCommit{}
	mc.ciStatus = &gh.CombinedStatus{State: &ciState}

	// Budget covers index overhead + 1 PR detail fetch, not 2.
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, d, nil,
		[]RepoRef{{
			Owner: "owner", Name: "repo",
			PlatformHost: "github.com",
		}},
		time.Minute, nil, budget,
	)

	syncer.RunOnce(ctx)

	// All 5 PRs should be in DB (index scan).
	for i := 1; i <= 5; i++ {
		pr, err := d.GetMergeRequest(
			ctx, "owner", "repo", i,
		)
		require.NoError(err)
		require.NotNil(pr, "PR #%d should exist", i)
	}

	// Only 1 PR should have detail_fetched_at set (budget
	// allows at most 1 full detail fetch).
	detailCount := 0
	for i := 1; i <= 5; i++ {
		pr, _ := d.GetMergeRequest(
			ctx, "owner", "repo", i,
		)
		if pr != nil && pr.DetailFetchedAt != nil {
			detailCount++
		}
	}
	assert.Equal(1, detailCount,
		"budget should limit detail fetches to 1 PR")

	// Budget should be spent.
	hostBudget := syncer.Budgets()["github.com"]
	require.NotNil(hostBudget)
	assert.Positive(hostBudget.Spent(),
		"budget should have been spent")
}

func TestBudgetResetOnRateWindowReset(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	rt := NewRateTracker(d, "github.com", "rest")
	budget := NewSyncBudget(100)
	rt.SetOnWindowReset(budget.Reset)

	// Simulate some spending.
	budget.Spend(50)
	assert.Equal(50, budget.Spent())

	// First rate update sets remaining to 4999.
	rt.UpdateFromRate(gh.Rate{
		Remaining: 4999,
		Limit:     5000,
		Reset:     gh.Timestamp{Time: time.Now().Add(time.Hour)},
	})

	// No window reset yet (first contact).
	assert.Equal(50, budget.Spent(),
		"budget should not reset on first contact")

	// Simulate rate decrease (normal usage).
	rt.UpdateFromRate(gh.Rate{
		Remaining: 4990,
		Limit:     5000,
		Reset:     gh.Timestamp{Time: time.Now().Add(time.Hour)},
	})
	assert.Equal(50, budget.Spent(),
		"budget should not reset on normal decrease")

	// Simulate window expiry: move resetAt to the past.
	rt.mu.Lock()
	pastReset := time.Now().Add(-1 * time.Second)
	rt.resetAt = &pastReset
	rt.mu.Unlock()

	// Simulate window reset (remaining jumps up + old resetAt passed).
	rt.UpdateFromRate(gh.Rate{
		Remaining: 5000,
		Limit:     5000,
		Reset:     gh.Timestamp{Time: time.Now().Add(2 * time.Hour)},
	})
	assert.Equal(0, budget.Spent(),
		"budget should reset when rate window resets")
}

func TestSyncMRSkipsGetUserWhenDisplayNameCached(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	sha := "abc123"
	number := 1
	author := "testuser"
	title := "Test PR"
	state := "open"
	url := "https://github.com/acme/widget/pull/1"
	now := &gh.Timestamp{Time: time.Now()}

	mock := &mockClient{
		singlePR: &gh.PullRequest{
			Number:    &number,
			Title:     &title,
			State:     &state,
			HTMLURL:   &url,
			User:      &gh.User{Login: &author},
			UpdatedAt: now,
			CreatedAt: now,
			Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
			Base:      &gh.PullRequestBranch{Ref: new("main")},
		},
		checkRuns: []*gh.CheckRun{{
			Name:       new("ci"),
			Status:     new("completed"),
			Conclusion: new("success"),
		}},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "acme", Name: "widget", PlatformHost: "github.com"}},
		time.Minute,
		nil,
		nil,
	)

	// First sync: GetUser should be called to resolve display name
	err := syncer.SyncMR(context.Background(), "acme", "widget", 1)
	require.NoError(t, err)
	assert.Equal(int32(1), mock.getUserCalls.Load())

	// Second sync: display name is in DB, GetUser should be skipped
	err = syncer.SyncMR(context.Background(), "acme", "widget", 1)
	require.NoError(t, err)
	assert.Equal(int32(1), mock.getUserCalls.Load(),
		"GetUser should not be called again when display name is cached")
}

func TestRefreshCIStatusAlwaysFetchesCombined(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	mock := &mockClient{
		checkRuns: []*gh.CheckRun{{
			Name:       new("ci"),
			Status:     new("completed"),
			Conclusion: new("success"),
		}},
		ciStatus: &gh.CombinedStatus{
			State:      new("success"),
			TotalCount: new(1),
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "acme", Name: "widget", PlatformHost: "github.com"}},
		time.Minute,
		nil,
		nil,
	)

	repoID, _ := d.UpsertRepo(context.Background(), "github.com", "acme", "widget")
	err := syncer.refreshCIStatus(
		context.Background(),
		RepoRef{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
		repoID,
		1,
		"abc123",
	)
	require.NoError(t, err)

	// GetCombinedStatus should always be called for correctness
	// (legacy commit statuses exist alongside check runs).
	assert.Equal(int32(1), mock.getCombinedCalls.Load(),
		"GetCombinedStatus should always be called")
}

func TestRefreshCIStatusFallsBackToCombinedWhenNoCheckRuns(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	mock := &mockClient{
		checkRuns: nil,
		ciStatus: &gh.CombinedStatus{
			State:      new("success"),
			TotalCount: new(1),
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "acme", Name: "widget", PlatformHost: "github.com"}},
		time.Minute,
		nil,
		nil,
	)

	repoID, _ := d.UpsertRepo(context.Background(), "github.com", "acme", "widget")
	err := syncer.refreshCIStatus(
		context.Background(),
		RepoRef{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
		repoID,
		1,
		"abc123",
	)
	require.NoError(t, err)

	// No check runs: GetCombinedStatus should be called as fallback
	assert.Equal(int32(1), mock.getCombinedCalls.Load(),
		"GetCombinedStatus should be called when no check runs exist")
}

// TestSyncer_OnStatusChangeCallback verifies the onStatusChange
// callback fires for each status transition during RunOnce. The
// SSE server uses this to broadcast live sync state.
func TestSyncer_OnStatusChangeCallback(t *testing.T) {
	assert := Assert.New(t)
	mock := &mockClient{openPRs: []*gh.PullRequest{}}
	d := openTestDB(t)
	_, err := d.UpsertRepo(context.Background(), "github.com", "o", "n")
	require.NoError(t, err)
	repos := []RepoRef{{Owner: "o", Name: "n", PlatformHost: "github.com"}}
	s := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil, repos, time.Hour, nil, nil,
	)

	var mu sync.Mutex
	var statuses []*SyncStatus
	s.SetOnStatusChange(func(status *SyncStatus) {
		mu.Lock()
		statuses = append(statuses, status)
		mu.Unlock()
	})

	s.RunOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(statuses), 2,
		"should fire at least started + completed")
	assert.True(statuses[0].Running,
		"first callback should be running=true")
	assert.False(statuses[len(statuses)-1].Running,
		"last callback should be running=false")
}

// TestSyncer_TriggerRunRunsRunOnce verifies TriggerRun kicks off
// a RunOnce and participates in the Syncer's wait group so Stop
// blocks until the triggered run completes.
func TestSyncer_TriggerRunRunsRunOnce(t *testing.T) {
	assert := Assert.New(t)
	mock := &mockClient{openPRs: []*gh.PullRequest{}}
	d := openTestDB(t)
	_, err := d.UpsertRepo(context.Background(), "github.com", "o", "n")
	require.NoError(t, err)
	repos := []RepoRef{{Owner: "o", Name: "n", PlatformHost: "github.com"}}
	s := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil, repos, time.Hour, nil, nil,
	)

	done := make(chan struct{}, 1)
	s.SetOnStatusChange(func(status *SyncStatus) {
		if !status.Running {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	s.TriggerRun(context.Background())

	select {
	case <-done:
	case <-time.After(time.Second):
		require.FailNow(t,
			"TriggerRun did not complete RunOnce within 1s")
	}
	s.Stop()
	assert.True(mock.listOpenPRsCalled,
		"TriggerRun should invoke ListOpenPullRequests")
}

// notModifiedErr returns the error shape go-github surfaces when the
// HTTP transport receives a 304 Not Modified response. The etag
// transport intercepts list-endpoint requests and adds If-None-Match
// headers; on a cache hit GitHub responds 304, which go-github wraps
// as *gh.ErrorResponse. The sync code calls IsNotModified to detect
// this and treat it as a no-op.
func notModifiedErr() error {
	return &gh.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusNotModified},
	}
}

// TestSyncerHandles304OnPRList verifies that a 304 response from
// the open-PR list is treated as "list unchanged, nothing to do"
// rather than a fatal sync error. Before the fix, IsNotModified
// was unused at the call site and the wrapped 304 was returned
// as "list open PRs: ...", failing the repo sync entirely.
func TestSyncerHandles304OnPRList(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	mc := &mockClient{
		listOpenPRsErr: notModifiedErr(),
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d,
		nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute,
		nil,
		nil,
	)

	var (
		results   []RepoSyncResult
		gotResult sync.WaitGroup
	)
	gotResult.Add(1)
	syncer.SetOnSyncCompleted(func(r []RepoSyncResult) {
		results = r
		gotResult.Done()
	})

	syncer.RunOnce(ctx)
	gotResult.Wait()

	require.Len(results, 1)
	assert.Empty(results[0].Error,
		"304 on open-PR list must not surface as a sync error")
}

// TestSyncerHandles304OnIssueList verifies the same short-circuit
// for the open-issue list endpoint. syncIssues is called from
// doSyncRepo with its error treated as non-fatal (logged only),
// so even before the fix the repo would not be marked failed —
// but the per-issue upserts and closure detection would still be
// skipped erroneously due to the early return path. After the
// fix, the function explicitly returns nil on 304 and the
// happy-path PR sync still completes cleanly.
func TestSyncerHandles304OnIssueList(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	mc := &mockClient{
		openPRs:           []*gh.PullRequest{},
		listOpenIssuesErr: notModifiedErr(),
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d,
		nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute,
		nil,
		nil,
	)

	var (
		results   []RepoSyncResult
		gotResult sync.WaitGroup
	)
	gotResult.Add(1)
	syncer.SetOnSyncCompleted(func(r []RepoSyncResult) {
		results = r
		gotResult.Done()
	})

	syncer.RunOnce(ctx)
	gotResult.Wait()

	require.Len(results, 1)
	assert.Empty(results[0].Error,
		"304 on open-issue list must not surface as a sync error")
}

// TestSyncerPRList304MakesNoAPICalls verifies that a 304 on the open-PR
// list endpoint triggers zero additional API calls for that repo's PRs.
// CI freshness for unchanged PRs is handled by the detail drain's
// priority scoring (ci_had_pending items get expedited refetches).
func TestSyncerPRList304MakesNoAPICalls(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	// Seed one open PR with pending CI via a full sync.
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	inProgress := "in_progress"
	seedClient := &mockClient{
		openPRs:   []*gh.PullRequest{buildOpenPR(1, now)},
		checkRuns: []*gh.CheckRun{{Status: &inProgress}},
	}
	repos := []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}
	seedSyncer := NewSyncer(
		map[string]Client{"github.com": seedClient},
		d, nil, repos, time.Minute, nil, testBudget(10000),
	)
	seedSyncer.RunOnce(ctx)

	pr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.Equal("pending", pr.CIStatus)

	// Second sync: PR list returns 304. The mock has CI data that
	// would change the status if called, but the 304 path must not
	// call any CI endpoints.
	completed := "completed"
	success := "success"
	spy := &callCountingClient{
		mockClient: mockClient{
			listOpenPRsErr: notModifiedErr(),
			checkRuns: []*gh.CheckRun{
				{Status: &completed, Conclusion: &success},
			},
		},
	}
	// budgetPerHour=0 disables detail drain so only index phase runs.
	refreshSyncer := NewSyncer(
		map[string]Client{"github.com": spy},
		d, nil, repos, time.Minute, nil, nil,
	)
	refreshSyncer.RunOnce(ctx)

	require.Equal(0, spy.ciCalls,
		"304 on PR list must not trigger any CI API calls")

	// CI state should be unchanged — still pending from seed.
	pr, err = d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.Equal("pending", pr.CIStatus,
		"CI should remain stale until detail drain refreshes it")
}

// callCountingClient wraps mockClient and counts CI-related API calls.
type callCountingClient struct {
	mockClient
	ciCalls int
}

func (c *callCountingClient) ListCheckRunsForRef(
	ctx context.Context, owner, repo, ref string,
) ([]*gh.CheckRun, error) {
	c.ciCalls++
	return c.mockClient.ListCheckRunsForRef(ctx, owner, repo, ref)
}

func (c *callCountingClient) GetCombinedStatus(
	ctx context.Context, owner, repo, ref string,
) (*gh.CombinedStatus, error) {
	c.ciCalls++
	return c.mockClient.GetCombinedStatus(ctx, owner, repo, ref)
}

// TestSyncerSyncsIssuesOnPRList304 verifies that a 304 on the open-PR
// list does not short-circuit issue sync. Issues have an independent
// ETag and their own open-list endpoint, so a PR-list 304 must not
// prevent new issues from being picked up.
func TestSyncerSyncsIssuesOnPRList304(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	issueNumber := 42
	issueTitle := "broken thing"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/42"
	issueBody := ""
	issueID := int64(900042)
	mc := &mockClient{
		listOpenPRsErr: notModifiedErr(),
		openIssues: []*gh.Issue{
			{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				Body:      &issueBody,
				CreatedAt: makeTimestamp(now),
				UpdatedAt: makeTimestamp(now),
			},
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)
	syncer.RunOnce(ctx)

	issue, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(issue, "issue sync must run even when PR list returns 304")
	assert.Equal(issueNumber, issue.Number)
	assert.Equal(issueTitle, issue.Title)
}

func TestSyncStoresIssueLabels(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 4, 12, 0, 0, 0, time.UTC)
	issueNumber := 42
	issueTitle := "broken thing"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/42"
	issueBody := ""
	issueID := int64(900042)
	mc := &mockClient{
		openIssues: []*gh.Issue{{
			ID:        &issueID,
			Number:    &issueNumber,
			Title:     &issueTitle,
			State:     &issueState,
			HTMLURL:   &issueURL,
			Body:      &issueBody,
			CreatedAt: makeTimestamp(now),
			UpdatedAt: makeTimestamp(now),
			Labels: []*gh.Label{
				buildGitHubLabel(801, "bug", "Something is broken", "d73a4a", true),
			},
		}},
		comments: []*gh.IssueComment{},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)
	syncer.RunOnce(ctx)

	issue, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(issue)
	require.Len(issue.Labels, 1)
	require.Equal("bug", issue.Labels[0].Name)
	require.Equal("Something is broken", issue.Labels[0].Description)
	require.Equal("d73a4a", issue.Labels[0].Color)
	require.True(issue.Labels[0].IsDefault)
	require.Equal(int64(801), issue.Labels[0].PlatformID)
	require.True(issue.Labels[0].UpdatedAt.Equal(now))
}

func TestFetchAndUpdateClosedRefreshesPRLabels(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	now := time.Date(2024, 6, 5, 12, 0, 0, 0, time.UTC)
	pr := buildOpenPR(7, now)
	pr.State = new("closed")
	closedAt := makeTimestamp(now)
	pr.ClosedAt = closedAt
	pr.Labels = []*gh.Label{buildGitHubLabel(901, "bug", "Old bug", "d73a4a", true)}
	_, err = d.UpsertMergeRequest(ctx, NormalizePR(repoID, pr))
	require.NoError(err)
	storedBefore, err := d.GetMergeRequest(ctx, "owner", "repo", 7)
	require.NoError(err)
	require.NoError(d.ReplaceMergeRequestLabels(ctx, repoID, storedBefore.ID, []db.Label{{
		PlatformID:  901,
		Name:        "bug",
		Description: "Old bug",
		Color:       "d73a4a",
		IsDefault:   true,
		UpdatedAt:   now,
	}}))

	pr.Labels = []*gh.Label{buildGitHubLabel(902, "release", "Ready to release", "5319e7", false)}
	pr.UpdatedAt = makeTimestamp(now.Add(time.Minute))
	mc := &mockClient{singlePR: pr}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, nil)

	require.NoError(syncer.fetchAndUpdateClosed(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoID, 7, false))

	storedAfter, err := d.GetMergeRequest(ctx, "owner", "repo", 7)
	require.NoError(err)
	require.Len(storedAfter.Labels, 1)
	require.Equal("release", storedAfter.Labels[0].Name)
	require.Equal(int64(902), storedAfter.Labels[0].PlatformID)
}

func TestFetchAndUpdateClosedRefreshesPRLabelsWithSameRepoOnAnotherHost(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	otherRepoID, err := d.UpsertRepo(ctx, "ghe.corp.com", "owner", "repo")
	require.NoError(err)
	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	now := time.Date(2024, 6, 5, 12, 0, 0, 0, time.UTC)

	otherPR := buildOpenPR(7, now)
	otherPR.State = new("closed")
	otherPR.ClosedAt = makeTimestamp(now)
	otherPR.Labels = []*gh.Label{buildGitHubLabel(990, "other-host", "Other host label", "333333", false)}
	otherMRID, err := d.UpsertMergeRequest(ctx, NormalizePR(otherRepoID, otherPR))
	require.NoError(err)
	require.NoError(d.ReplaceMergeRequestLabels(ctx, otherRepoID, otherMRID, []db.Label{{
		PlatformID:  990,
		Name:        "other-host",
		Description: "Other host label",
		Color:       "333333",
		UpdatedAt:   now,
	}}))

	pr := buildOpenPR(7, now)
	pr.State = new("closed")
	pr.ClosedAt = makeTimestamp(now)
	pr.Labels = []*gh.Label{buildGitHubLabel(901, "bug", "Old bug", "d73a4a", true)}
	targetMRID, err := d.UpsertMergeRequest(ctx, NormalizePR(repoID, pr))
	require.NoError(err)
	require.NoError(d.ReplaceMergeRequestLabels(ctx, repoID, targetMRID, []db.Label{{
		PlatformID:  901,
		Name:        "bug",
		Description: "Old bug",
		Color:       "d73a4a",
		IsDefault:   true,
		UpdatedAt:   now,
	}}))

	pr.Labels = []*gh.Label{buildGitHubLabel(902, "release", "Ready to release", "5319e7", false)}
	pr.UpdatedAt = makeTimestamp(now.Add(time.Minute))
	mc := &mockClient{singlePR: pr}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, nil)

	require.NoError(syncer.fetchAndUpdateClosed(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoID, 7, false))

	var labelName string
	err = d.ReadDB().QueryRowContext(ctx, `
		SELECT l.name
		FROM middleman_merge_request_labels ml
		JOIN middleman_labels l ON l.id = ml.label_id
		WHERE ml.merge_request_id = ?`, targetMRID,
	).Scan(&labelName)
	require.NoError(err)
	require.Equal("release", labelName)

	err = d.ReadDB().QueryRowContext(ctx, `
		SELECT l.name
		FROM middleman_merge_request_labels ml
		JOIN middleman_labels l ON l.id = ml.label_id
		WHERE ml.merge_request_id = ?`, otherMRID,
	).Scan(&labelName)
	require.NoError(err)
	require.Equal("other-host", labelName)
}

func TestFetchAndUpdateClosedRefreshesIssueLabels(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	now := time.Date(2024, 6, 6, 12, 0, 0, 0, time.UTC)
	issueNumber := 9
	issueTitle := "closed issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/9"
	issueBody := ""
	issueID := int64(900009)
	issue := &gh.Issue{ID: &issueID, Number: &issueNumber, Title: &issueTitle, State: &issueState, HTMLURL: &issueURL, Body: &issueBody, CreatedAt: makeTimestamp(now), UpdatedAt: makeTimestamp(now), Labels: []*gh.Label{buildGitHubLabel(1001, "bug", "Old bug", "d73a4a", true)}}
	issueRowID, err := d.UpsertIssue(ctx, NormalizeIssue(repoID, issue))
	require.NoError(err)
	require.NoError(d.ReplaceIssueLabels(ctx, repoID, issueRowID, []db.Label{{PlatformID: 1001, Name: "bug", Description: "Old bug", Color: "d73a4a", IsDefault: true, UpdatedAt: now}}))

	closedState := "closed"
	issue.State = &closedState
	issue.UpdatedAt = makeTimestamp(now.Add(time.Minute))
	issue.Labels = []*gh.Label{buildGitHubLabel(1002, "docs", "Documentation", "0075ca", false)}
	closedAt := makeTimestamp(now.Add(2 * time.Minute))
	issue.ClosedAt = closedAt
	mc := &mockClient{getIssueFn: func(context.Context, string, string, int) (*gh.Issue, error) { return issue, nil }}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, nil)

	require.NoError(syncer.fetchAndUpdateClosedIssue(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoID, issueNumber))

	stored, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.Len(stored.Labels, 1)
	require.Equal("docs", stored.Labels[0].Name)
	require.Equal(int64(1002), stored.Labels[0].PlatformID)
}

func TestFetchAndUpdateClosedRefreshesIssueLabelsWithSameRepoOnAnotherHost(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	otherRepoID, err := d.UpsertRepo(ctx, "ghe.corp.com", "owner", "repo")
	require.NoError(err)
	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	now := time.Date(2024, 6, 6, 12, 0, 0, 0, time.UTC)
	issueNumber := 9

	otherState := "open"
	otherTitle := "other closed issue"
	otherURL := "https://ghe.corp.com/owner/repo/issues/9"
	otherBody := ""
	otherID := int64(800009)
	otherIssue := &gh.Issue{ID: &otherID, Number: &issueNumber, Title: &otherTitle, State: &otherState, HTMLURL: &otherURL, Body: &otherBody, CreatedAt: makeTimestamp(now), UpdatedAt: makeTimestamp(now), Labels: []*gh.Label{buildGitHubLabel(1901, "other-host", "Other host label", "333333", false)}}
	otherIssueRowID, err := d.UpsertIssue(ctx, NormalizeIssue(otherRepoID, otherIssue))
	require.NoError(err)
	require.NoError(d.ReplaceIssueLabels(ctx, otherRepoID, otherIssueRowID, []db.Label{{PlatformID: 1901, Name: "other-host", Description: "Other host label", Color: "333333", UpdatedAt: now}}))

	issueTitle := "closed issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/9"
	issueBody := ""
	issueID := int64(900009)
	issue := &gh.Issue{ID: &issueID, Number: &issueNumber, Title: &issueTitle, State: &issueState, HTMLURL: &issueURL, Body: &issueBody, CreatedAt: makeTimestamp(now), UpdatedAt: makeTimestamp(now), Labels: []*gh.Label{buildGitHubLabel(1001, "bug", "Old bug", "d73a4a", true)}}
	issueRowID, err := d.UpsertIssue(ctx, NormalizeIssue(repoID, issue))
	require.NoError(err)
	require.NoError(d.ReplaceIssueLabels(ctx, repoID, issueRowID, []db.Label{{PlatformID: 1001, Name: "bug", Description: "Old bug", Color: "d73a4a", IsDefault: true, UpdatedAt: now}}))

	closedState := "closed"
	issue.State = &closedState
	issue.UpdatedAt = makeTimestamp(now.Add(time.Minute))
	issue.Labels = []*gh.Label{buildGitHubLabel(1002, "docs", "Documentation", "0075ca", false)}
	closedAt := makeTimestamp(now.Add(2 * time.Minute))
	issue.ClosedAt = closedAt
	mc := &mockClient{getIssueFn: func(context.Context, string, string, int) (*gh.Issue, error) { return issue, nil }}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, nil)

	require.NoError(syncer.fetchAndUpdateClosedIssue(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoID, issueNumber))

	var labelName string
	err = d.ReadDB().QueryRowContext(ctx, `
		SELECT l.name
		FROM middleman_issue_labels il
		JOIN middleman_labels l ON l.id = il.label_id
		WHERE il.issue_id = ?`, issueRowID,
	).Scan(&labelName)
	require.NoError(err)
	require.Equal("docs", labelName)

	err = d.ReadDB().QueryRowContext(ctx, `
		SELECT l.name
		FROM middleman_issue_labels il
		JOIN middleman_labels l ON l.id = il.label_id
		WHERE il.issue_id = ?`, otherIssueRowID,
	).Scan(&labelName)
	require.NoError(err)
	require.Equal("other-host", labelName)
}

func TestBackfillRepoPersistsPRLabels(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	repoRow, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(err)
	require.NotNil(repoRow)
	now := time.Date(2024, 6, 7, 12, 0, 0, 0, time.UTC)
	pr := buildOpenPR(21, now)
	pr.State = new("closed")
	pr.Labels = []*gh.Label{buildGitHubLabel(1101, "backfill-pr", "Backfilled PR label", "5319e7", false)}

	mc := &mockClient{listPullRequestsPageFn: func(context.Context, string, string, string, int) ([]*gh.PullRequest, bool, error) {
		return []*gh.PullRequest{pr}, false, nil
	}}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(10))

	syncer.backfillRepo(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoRow, NewSyncBudget(10))

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 21)
	require.NoError(err)
	require.NotNil(stored)
	require.Equal(repoID, stored.RepoID)
	require.Len(stored.Labels, 1)
	require.Equal("backfill-pr", stored.Labels[0].Name)
}

func TestBackfillRepoPersistsIssueLabels(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	_, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	repoRow, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(err)
	require.NotNil(repoRow)
	now := time.Date(2024, 6, 8, 12, 0, 0, 0, time.UTC)
	issueNumber := 22
	issueTitle := "backfilled issue"
	issueState := "closed"
	issueURL := "https://github.com/owner/repo/issues/22"
	issueBody := ""
	issueID := int64(900022)
	issue := &gh.Issue{ID: &issueID, Number: &issueNumber, Title: &issueTitle, State: &issueState, HTMLURL: &issueURL, Body: &issueBody, CreatedAt: makeTimestamp(now), UpdatedAt: makeTimestamp(now), Labels: []*gh.Label{buildGitHubLabel(1201, "backfill-issue", "Backfilled issue label", "0052cc", false)}}

	mc := &mockClient{listIssuesPageFn: func(context.Context, string, string, string, int) ([]*gh.Issue, bool, error) {
		return []*gh.Issue{issue}, false, nil
	}}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(10))

	syncer.backfillRepo(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoRow, NewSyncBudget(10))

	stored, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(stored)
	require.Len(stored.Labels, 1)
	require.Equal("backfill-issue", stored.Labels[0].Name)
}

func TestBackfillRepoDoesNotAdvancePRCursorWhenLabelPersistenceFails(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	repoRow, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(err)
	require.NotNil(repoRow)
	now := time.Date(2024, 6, 9, 12, 0, 0, 0, time.UTC)

	require.NoError(d.UpsertLabels(ctx, repoID, []db.Label{{
		PlatformID:  100,
		Name:        "bug",
		Description: "name row",
		Color:       "111111",
		UpdatedAt:   now,
	}}))
	require.NoError(d.UpsertLabels(ctx, repoID, []db.Label{{
		PlatformID:  200,
		Name:        "renamed",
		Description: "platform row",
		Color:       "222222",
		UpdatedAt:   now,
	}}))

	pr := buildOpenPR(31, now)
	pr.State = new("closed")
	pr.Labels = []*gh.Label{buildGitHubLabel(200, "bug", "ambiguous", "333333", false)}

	mc := &mockClient{listPullRequestsPageFn: func(context.Context, string, string, string, int) ([]*gh.PullRequest, bool, error) {
		return []*gh.PullRequest{pr}, false, nil
	}}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(10))

	syncer.backfillRepo(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoRow, NewSyncBudget(10))

	repoAfter, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(err)
	require.NotNil(repoAfter)
	require.Equal(0, repoAfter.BackfillPRPage)
	require.False(repoAfter.BackfillPRComplete)
	require.Nil(repoAfter.BackfillPRCompletedAt)

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 31)
	require.NoError(err)
	require.NotNil(stored)
	require.Empty(stored.Labels)
}

func TestBackfillRepoDoesNotAdvanceIssueCursorWhenLabelPersistenceFails(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(err)
	repoRow, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(err)
	require.NotNil(repoRow)
	now := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)

	require.NoError(d.UpsertLabels(ctx, repoID, []db.Label{{
		PlatformID:  100,
		Name:        "bug",
		Description: "name row",
		Color:       "111111",
		UpdatedAt:   now,
	}}))
	require.NoError(d.UpsertLabels(ctx, repoID, []db.Label{{
		PlatformID:  200,
		Name:        "renamed",
		Description: "platform row",
		Color:       "222222",
		UpdatedAt:   now,
	}}))

	issueNumber := 32
	issueTitle := "ambiguous backfill issue"
	issueState := "closed"
	issueURL := "https://github.com/owner/repo/issues/32"
	issueBody := ""
	issueID := int64(900032)
	issue := &gh.Issue{ID: &issueID, Number: &issueNumber, Title: &issueTitle, State: &issueState, HTMLURL: &issueURL, Body: &issueBody, CreatedAt: makeTimestamp(now), UpdatedAt: makeTimestamp(now), Labels: []*gh.Label{buildGitHubLabel(200, "bug", "ambiguous", "333333", false)}}

	mc := &mockClient{listIssuesPageFn: func(context.Context, string, string, string, int) ([]*gh.Issue, bool, error) {
		return []*gh.Issue{issue}, false, nil
	}}
	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil, testBudget(10))

	syncer.backfillRepo(ctx, RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"}, repoRow, NewSyncBudget(10))

	repoAfter, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(err)
	require.NotNil(repoAfter)
	require.Equal(0, repoAfter.BackfillIssuePage)
	require.False(repoAfter.BackfillIssueComplete)
	require.Nil(repoAfter.BackfillIssueCompletedAt)

	stored, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(stored)
	require.Empty(stored.Labels)
}

// partialFailureMock embeds mockClient and simulates ETag-like
// behavior for issues: after a successful list fetch, subsequent
// calls return 304 (not-modified) unless InvalidateListETagsForRepo
// was called. This proves invalidation is load-bearing — without it
// the retry never fires and stale state persists.
type partialFailureMock struct {
	mockClient
	issuesCached         bool
	prsCached            bool
	listOpenPRsErr       error // injected error for ListOpenPullRequests
	listIssueCommentsErr error // injected error for ListIssueComments
	listReviewsErr       error // injected error for ListReviews (MR timeline)
	getIssueErr          error // injected error for GetIssue (closure path)
}

func (m *partialFailureMock) ListOpenPullRequests(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
	if m.listOpenPRsErr != nil {
		return nil, m.listOpenPRsErr
	}
	if m.prsCached {
		return nil, notModifiedErr()
	}
	m.prsCached = true
	return m.openPRs, nil
}

func (m *partialFailureMock) ListOpenIssues(_ context.Context, _, _ string) ([]*gh.Issue, error) {
	if m.listOpenIssuesErr != nil {
		return nil, m.listOpenIssuesErr
	}
	if m.issuesCached {
		return nil, notModifiedErr()
	}
	m.issuesCached = true
	return m.openIssues, nil
}

func (m *partialFailureMock) ListIssueComments(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
	if m.listIssueCommentsErr != nil {
		return nil, m.listIssueCommentsErr
	}
	return m.comments, nil
}

func (m *partialFailureMock) ListReviews(_ context.Context, _, _ string, _ int) ([]*gh.PullRequestReview, error) {
	if m.listReviewsErr != nil {
		return nil, m.listReviewsErr
	}
	return m.reviews, nil
}

func (m *partialFailureMock) GetIssue(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
	if m.getIssueErr != nil {
		return nil, m.getIssueErr
	}
	if m.getIssueFn != nil {
		return m.getIssueFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *partialFailureMock) InvalidateListETagsForRepo(_, _ string, endpoints ...string) {
	m.invalidateCalls.Add(1)
	if len(endpoints) == 0 {
		m.prsCached = false
		m.issuesCached = false
		return
	}
	for _, ep := range endpoints {
		switch ep {
		case "pulls":
			m.prsCached = false
		case "issues":
			m.issuesCached = false
		}
	}
}

// TestSyncerSyncOpenIssueFailureMarksRepoFailed verifies that when
// the open-issue list succeeds but syncOpenIssue fails for an
// individual item (here via a ListIssueComments error during timeline
// refresh), syncIssues returns an error, doSyncRepo calls
// markFailure, and the next cycle forces a timeline refresh via
// forceRefresh even though UpdatedAt hasn't changed.
func TestSyncerSyncOpenIssueFailureMarksRepoFailed(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	repos := []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}

	issueNumber := 7
	issueTitle := "per-item failure issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/7"
	issueBody := ""
	issueID := int64(777)
	openIssue := &gh.Issue{
		ID:        &issueID,
		Number:    &issueNumber,
		Title:     &issueTitle,
		State:     &issueState,
		HTMLURL:   &issueURL,
		Body:      &issueBody,
		CreatedAt: makeTimestamp(now),
		UpdatedAt: makeTimestamp(now),
	}

	commentID := int64(999)
	commentBody := "recovery comment"
	commentUser := "commenter"
	recoveryComment := &gh.IssueComment{
		ID:        &commentID,
		Body:      &commentBody,
		CreatedAt: makeTimestamp(now),
		UpdatedAt: makeTimestamp(now),
		User:      &gh.User{Login: &commentUser},
	}

	mc := &partialFailureMock{}
	mc.openPRs = []*gh.PullRequest{buildOpenPR(1, now)}
	mc.openIssues = []*gh.Issue{openIssue}
	mc.comments = []*gh.IssueComment{}
	mc.reviews = []*gh.PullRequestReview{}
	mc.commits = []*gh.RepositoryCommit{}
	// Issue list succeeds, but timeline refresh fails for the item.
	mc.listIssueCommentsErr = fmt.Errorf("transient comments failure")

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d, nil, repos, time.Minute, nil, nil,
	)

	// Cycle 1: issue list succeeds, issue is upserted to DB, but
	// refreshIssueTimeline fails → syncOpenIssue returns error →
	// hadItemFailure → syncIssues returns error → markFailure.
	syncer.RunOnce(ctx)

	// Issue row lands in DB (upsert happened before timeline).
	issue, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(issue, "issue should be upserted even though timeline failed")

	// No events should exist because timeline refresh failed.
	events, err := d.ListIssueEvents(ctx, issue.ID)
	require.NoError(err)
	assert.Empty(events, "no events should exist after failed timeline refresh")

	_, flagged := syncer.failedRepos.Load(repoFailKey(repos[0]))
	assert.True(flagged, "failedRepos must be set after per-item syncOpenIssue failure")

	// Clear the error, provide a comment, simulate warm cache.
	mc.listIssueCommentsErr = nil
	mc.comments = []*gh.IssueComment{recoveryComment}
	mc.issuesCached = true

	invalidateBefore := mc.invalidateCalls.Load()

	// Cycle 2: forceRefresh overrides needsTimeline even though
	// UpdatedAt hasn't changed → timeline refresh retried → comment lands.
	syncer.RunOnce(ctx)

	assert.Greater(mc.invalidateCalls.Load(), invalidateBefore,
		"next cycle should call InvalidateListETagsForRepo")

	// Verify timeline was actually refreshed: the comment should be in DB.
	issue, err = d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	events, err = d.ListIssueEvents(ctx, issue.ID)
	require.NoError(err)
	assert.Len(events, 1, "comment should be persisted after forced timeline retry")

	_, flagged = syncer.failedRepos.Load(repoFailKey(repos[0]))
	assert.False(flagged, "failedRepos must be cleared after successful retry")
}

// TestSyncerClosedIssueFailureMarksRepoFailed verifies that when
// the open-issue list succeeds but fetchAndUpdateClosedIssue fails
// for a previously-open issue (here via a GetIssue API error),
// syncIssues returns an error, doSyncRepo marks the repo failed,
// and the next cycle retries after ETag invalidation.
func TestSyncerClosedIssueFailureMarksRepoFailed(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	repos := []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}

	issueNumber := 7
	issueTitle := "will-close issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/7"
	issueBody := ""
	issueID := int64(777)
	openIssue := &gh.Issue{
		ID:        &issueID,
		Number:    &issueNumber,
		Title:     &issueTitle,
		State:     &issueState,
		HTMLURL:   &issueURL,
		Body:      &issueBody,
		CreatedAt: makeTimestamp(now),
		UpdatedAt: makeTimestamp(now),
	}

	// Seed issue #7 as open in DB via an initial sync with the
	// issue present in the open list.
	seedMC := &mockClient{
		openPRs:    []*gh.PullRequest{buildOpenPR(1, now)},
		openIssues: []*gh.Issue{openIssue},
		comments:   []*gh.IssueComment{},
		reviews:    []*gh.PullRequestReview{},
		commits:    []*gh.RepositoryCommit{},
	}

	seedSyncer := NewSyncer(
		map[string]Client{"github.com": seedMC},
		d, nil, repos, time.Minute, nil, nil,
	)
	seedSyncer.RunOnce(ctx)

	seeded, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(seeded, "seed cycle should persist issue #7")

	// Now build the real mock: open list returns EMPTY (issue #7
	// no longer open) → closure detection finds #7. GetIssue for
	// the closure path fails.
	mc := &partialFailureMock{}
	mc.openPRs = []*gh.PullRequest{buildOpenPR(1, now)}
	mc.openIssues = []*gh.Issue{} // issue #7 not in open list
	mc.comments = []*gh.IssueComment{}
	mc.reviews = []*gh.PullRequestReview{}
	mc.commits = []*gh.RepositoryCommit{}
	mc.getIssueErr = fmt.Errorf("transient API failure fetching closed issue")

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d, nil, repos, time.Minute, nil, nil,
	)

	// Cycle 1: list succeeds (empty), closure detection finds #7,
	// fetchAndUpdateClosedIssue fails → hadItemFailure → markFailure.
	syncer.RunOnce(ctx)

	_, flagged := syncer.failedRepos.Load(repoFailKey(repos[0]))
	assert.True(flagged, "failedRepos must be set after fetchAndUpdateClosedIssue failure")

	// Verify issue is still open in DB (closure update failed).
	stillOpen, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(stillOpen)
	assert.Equal("open", stillOpen.State, "issue should still be open because closure update failed")

	// Clear error, simulate warm cache, provide closed issue data.
	mc.getIssueErr = nil
	closedState := "closed"
	closedIssue := &gh.Issue{
		ID:        &issueID,
		Number:    &issueNumber,
		Title:     &issueTitle,
		State:     &closedState,
		HTMLURL:   &issueURL,
		Body:      &issueBody,
		CreatedAt: makeTimestamp(now),
		UpdatedAt: makeTimestamp(now.Add(time.Hour)),
	}
	mc.getIssueFn = func(_ context.Context, _, _ string, n int) (*gh.Issue, error) {
		if n == issueNumber {
			return closedIssue, nil
		}
		return nil, nil
	}
	mc.issuesCached = true

	invalidateBefore := mc.invalidateCalls.Load()

	// Cycle 2: invalidation → fresh list (empty) → closure
	// detection re-finds #7 → fetchAndUpdateClosedIssue succeeds.
	syncer.RunOnce(ctx)

	assert.Greater(mc.invalidateCalls.Load(), invalidateBefore,
		"next cycle should call InvalidateListETagsForRepo")

	updated, err := d.GetIssue(ctx, "owner", "repo", issueNumber)
	require.NoError(err)
	require.NotNil(updated)
	assert.Equal("closed", updated.State, "issue should be closed after successful retry")

	_, flagged = syncer.failedRepos.Load(repoFailKey(repos[0]))
	assert.False(flagged, "failedRepos must be cleared after successful retry")
}

// TestSyncerMRListFailureMarksRepoFailed verifies that when the
// PR list fails, the MR path is marked failed, and the next cycle
// invalidates the ETag and retries. Also verifies issue path is NOT
// force-refreshed when only MR path failed (scoped failure tracking).
func TestSyncerMRListFailureMarksRepoFailed(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	repos := []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}

	mc := &partialFailureMock{}
	mc.openPRs = []*gh.PullRequest{buildOpenPR(1, now)}
	mc.openIssues = []*gh.Issue{}
	mc.comments = []*gh.IssueComment{}
	mc.reviews = []*gh.PullRequestReview{}
	mc.commits = []*gh.RepositoryCommit{}
	// PR list fails on first call.
	mc.listOpenPRsErr = fmt.Errorf("transient PR list failure")

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d, nil, repos, time.Minute, nil, nil,
	)

	// Cycle 1: PR list fails → failMR set, issues unaffected.
	syncer.RunOnce(ctx)

	mr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	assert.Nil(mr, "MR should not be upserted when PR list failed")

	v, flagged := syncer.failedRepos.Load(repoFailKey(repos[0]))
	assert.True(flagged, "failedRepos must be set after PR list failure")
	assert.Equal(failMR, v.(failScope), "only failMR scope should be set")

	// Clear error, simulate warm caches.
	mc.listOpenPRsErr = nil
	mc.prsCached = false // allow next list to succeed
	mc.issuesCached = true

	invalidateBefore := mc.invalidateCalls.Load()

	// Cycle 2: ETag invalidated for pulls only → fresh PR list → MR upserted.
	// Issue cache should remain warm (only pulls invalidated).
	syncer.RunOnce(ctx)

	assert.Greater(mc.invalidateCalls.Load(), invalidateBefore,
		"next cycle should call InvalidateListETagsForRepo")

	// Issue cache must still be warm — MR-only failure should not
	// invalidate issue ETags.
	assert.True(mc.issuesCached,
		"issue cache should stay warm when only MR path failed")

	mr, err = d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(mr, "MR should be upserted after successful retry")

	_, flagged = syncer.failedRepos.Load(repoFailKey(repos[0]))
	assert.False(flagged, "failedRepos must be cleared after successful retry")
}

// TestSyncerMRDetailFailureRetries verifies that when fetchMRDetail
// fails during timeline refresh (via ListReviews error), the MR's
// detail_fetched_at stays nil so the detail queue picks it up again
// on the next cycle.
func TestSyncerMRDetailFailureRetries(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	repos := []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}
	ciState := "success"

	mc := &partialFailureMock{}
	mc.openPRs = []*gh.PullRequest{buildOpenPR(1, now)}
	mc.openIssues = []*gh.Issue{}
	mc.comments = []*gh.IssueComment{}
	mc.reviews = []*gh.PullRequestReview{}
	mc.commits = []*gh.RepositoryCommit{}
	mc.ciStatus = &gh.CombinedStatus{State: &ciState}
	// Timeline refresh fails at ListReviews during detail fetch.
	mc.listReviewsErr = fmt.Errorf("transient reviews failure")

	syncer := NewSyncer(
		map[string]Client{"github.com": mc},
		d, nil, repos, time.Minute, nil, testBudget(10000),
	)

	// Cycle 1: index upserts MR, detail drain calls fetchMRDetail →
	// refreshTimeline fails at ListReviews → detail_fetched_at stays nil.
	syncer.RunOnce(ctx)

	mr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(mr, "MR should be upserted by index phase")
	assert.Nil(mr.DetailFetchedAt,
		"detail_fetched_at should be nil after failed detail fetch")

	events, err := d.ListMREvents(ctx, mr.ID)
	require.NoError(err)
	assert.Empty(events, "no events should exist after failed timeline refresh")

	// Clear error, add a review for cycle 2.
	mc.listReviewsErr = nil
	reviewID := int64(500)
	reviewState := "APPROVED"
	reviewUser := "reviewer"
	reviewBody := "lgtm"
	mc.reviews = []*gh.PullRequestReview{{
		ID:          &reviewID,
		State:       &reviewState,
		Body:        &reviewBody,
		SubmittedAt: makeTimestamp(now),
		User:        &gh.User{Login: &reviewUser},
	}}

	// Cycle 2: detail drain picks up MR again (detail_fetched_at nil)
	// → fetchMRDetail succeeds → timeline events land.
	syncer.RunOnce(ctx)

	mr, err = d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(mr)
	assert.NotNil(mr.DetailFetchedAt,
		"detail_fetched_at should be set after successful detail fetch")

	events, err = d.ListMREvents(ctx, mr.ID)
	require.NoError(err)
	assert.NotEmpty(events, "review event should be persisted after detail retry")
}

func TestSyncRepoGraphQLIssues(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	mock := &mockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	issueID := int64(10000)
	issueNumber := 10
	issueTitle := "Bug report"
	issueState := "open"
	issueBody := "Something broke"
	issueURL := "https://github.com/owner/repo/issues/10"
	issueAuthor := "alice"
	commentID := int64(501)
	commentBody := "I see this too"
	commentLogin := "bob"
	commentTime := gh.Timestamp{Time: now}
	result := &RepoBulkResult{
		Issues: []BulkIssue{
			{
				Issue: &gh.Issue{
					ID:        &issueID,
					Number:    &issueNumber,
					Title:     &issueTitle,
					State:     &issueState,
					Body:      &issueBody,
					HTMLURL:   &issueURL,
					User:      &gh.User{Login: &issueAuthor},
					CreatedAt: &commentTime,
					UpdatedAt: &commentTime,
				},
				Comments: []*gh.IssueComment{
					{
						ID:        &commentID,
						Body:      &commentBody,
						User:      &gh.User{Login: &commentLogin},
						CreatedAt: &commentTime,
						UpdatedAt: &commentTime,
					},
				},
				CommentsComplete: true,
			},
		},
	}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// Verify issue in DB.
	issue, err := d.GetIssue(ctx, "owner", "repo", 10)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal("Bug report", issue.Title)
	assert.Equal("alice", issue.Author)
	assert.Equal("open", issue.State)
	assert.Equal(1, issue.CommentCount)

	// Verify comment event.
	events, err := d.ListIssueEvents(ctx, issue.ID)
	require.NoError(t, err)
	assert.Len(events, 1)
	assert.Equal("I see this too", events[0].Body)

	// Comments were complete — ListIssueComments should NOT be called.
	assert.Equal(int32(0), mock.listIssueCommentsCalled.Load())
}

// blockingCtxMockClient blocks in ListOpenPullRequests until either
// the provided channel is closed or the ctx is canceled. Unlike
// blockingMockClient (which ignores ctx), this variant is used by
// TestSyncerStopCancelsTriggerRun to verify Stop cancels an
// in-flight TriggerRun via the syncer's lifetime context.
type blockingCtxMockClient struct {
	mockClient
	entered chan struct{}
	release chan struct{}
}

func (b *blockingCtxMockClient) ListOpenPullRequests(
	ctx context.Context, _, _ string,
) ([]*gh.PullRequest, error) {
	if b.entered != nil {
		select {
		case b.entered <- struct{}{}:
		default:
		}
	}
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestSyncerStopCancelsTriggerRun verifies that Stop cancels the
// syncer's lifetime context, which in turn unblocks a TriggerRun
// whose GitHub call is waiting on ctx.Done(). Before the fix, Stop
// only closed stopCh and waited on wg; a TriggerRun mid-flight in a
// slow call held wg forever because its caller-supplied ctx
// (wrapped in context.WithoutCancel by the handlers) was immune to
// Stop.
func TestSyncerStopCancelsTriggerRun(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	mock := &blockingCtxMockClient{
		entered: entered,
		release: release,
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "o", Name: "r", PlatformHost: "github.com"}},
		time.Hour, nil, nil,
	)

	// The handler wraps the request ctx in context.WithoutCancel,
	// so the TriggerRun's caller context is effectively immune to
	// per-request cancellation. Only the syncer's lifetime ctx can
	// unblock the work.
	syncer.TriggerRun(context.WithoutCancel(context.Background()))

	// Wait for the blocked ListOpenPullRequests to enter. Using a
	// channel rather than time.Sleep so the test is deterministic
	// and fast.
	select {
	case <-entered:
	case <-time.After(time.Second):
		require.FailNow("TriggerRun did not start ListOpenPullRequests")
	}

	// Call Stop in a goroutine and assert it returns quickly.
	stopped := make(chan struct{})
	go func() {
		syncer.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		close(release) // unblock the mock so the test can tear down
		require.FailNow("Stop did not return after ctx cancellation")
	}
}

func TestResolveDisplayName(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		login         string
		getUserFn     func(context.Context, string) (*gh.User, error)
		wantName      string
		wantOK        bool
		wantAPICalled bool
	}{
		{
			name:  "regular user with display name",
			login: "alice",
			getUserFn: func(_ context.Context, login string) (*gh.User, error) {
				name := "Alice Smith"
				return &gh.User{Login: &login, Name: &name}, nil
			},
			wantName:      "Alice Smith",
			wantOK:        true,
			wantAPICalled: true,
		},
		{
			name:  "regular user without display name",
			login: "bob",
			getUserFn: func(_ context.Context, login string) (*gh.User, error) {
				return &gh.User{Login: &login}, nil
			},
			wantName:      "",
			wantOK:        true,
			wantAPICalled: true,
		},
		{
			name:  "bot login skips API call",
			login: "renovate[bot]",
			getUserFn: func(_ context.Context, _ string) (*gh.User, error) {
				return nil, nil
			},
			wantName:      "renovate[bot]",
			wantOK:        true,
			wantAPICalled: false,
		},
		{
			name:  "API-returned bot uses login as display name",
			login: "ci-helper",
			getUserFn: func(_ context.Context, login string) (*gh.User, error) {
				botType := "Bot"
				return &gh.User{Login: &login, Type: &botType}, nil
			},
			wantName:      "ci-helper",
			wantOK:        true,
			wantAPICalled: true,
		},
		{
			name:  "user not found returns false",
			login: "ghost",
			getUserFn: func(_ context.Context, _ string) (*gh.User, error) {
				return nil, fmt.Errorf("GET https://api.github.com/users/ghost: 404 Not Found")
			},
			wantName:      "",
			wantOK:        false,
			wantAPICalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := Assert.New(t)
			apiCalled := false
			mc := &mockClient{getUserFn: func(ctx context.Context, login string) (*gh.User, error) {
				apiCalled = true
				return tt.getUserFn(ctx, login)
			}}
			syncer := NewSyncer(
				map[string]Client{"github.com": mc}, nil, nil, nil,
				time.Minute, nil, nil,
			)
			syncer.displayNames = make(map[string]displayNameResult)

			name, ok := syncer.resolveDisplayName(ctx, mc, "github.com", tt.login)
			assert.Equal(tt.wantName, name)
			assert.Equal(tt.wantOK, ok)
			assert.Equal(tt.wantAPICalled, apiCalled, "GetUser call expectation")
		})
	}
}

func TestResolveDisplayName_CachesNegativeResult(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()

	callCount := 0
	mc := &mockClient{
		getUserFn: func(_ context.Context, _ string) (*gh.User, error) {
			callCount++
			return nil, fmt.Errorf("404 Not Found")
		},
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, nil, nil, nil,
		time.Minute, nil, nil,
	)
	syncer.displayNames = make(map[string]displayNameResult)

	// First call: hits API, returns failure.
	name1, ok1 := syncer.resolveDisplayName(ctx, mc, "github.com", "renovate")
	assert.Empty(name1)
	assert.False(ok1)
	assert.Equal(1, callCount)

	// Second call: should use cache, no additional API call.
	name2, ok2 := syncer.resolveDisplayName(ctx, mc, "github.com", "renovate")
	assert.Empty(name2)
	assert.False(ok2)
	assert.Equal(1, callCount, "GetUser should not be called again for cached failure")
}

func TestResolveDisplayName_CachesSuccessfulEmptyName(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()

	callCount := 0
	mc := &mockClient{
		getUserFn: func(_ context.Context, login string) (*gh.User, error) {
			callCount++
			return &gh.User{Login: &login}, nil // no display name
		},
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mc}, nil, nil, nil,
		time.Minute, nil, nil,
	)
	syncer.displayNames = make(map[string]displayNameResult)

	// First call: hits API, succeeds with empty name.
	name1, ok1 := syncer.resolveDisplayName(ctx, mc, "github.com", "no-profile")
	assert.Empty(name1)
	assert.True(ok1, "successful lookup of empty name should return ok=true")
	assert.Equal(1, callCount)

	// Second call: cache hit must still return ok=true, not flip to false.
	name2, ok2 := syncer.resolveDisplayName(ctx, mc, "github.com", "no-profile")
	assert.Empty(name2)
	assert.True(ok2, "cached empty name must remain ok=true")
	assert.Equal(1, callCount, "GetUser should not be called again for cached success")
}

func TestSyncRepoGraphQLIssuesCommentsIncomplete(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	commentTime := gh.Timestamp{Time: now}

	commentID := int64(777)
	commentBody := "REST comment"
	commentLogin := "carol"

	mock := &mockClient{
		comments: []*gh.IssueComment{
			{
				ID:        &commentID,
				Body:      &commentBody,
				User:      &gh.User{Login: &commentLogin},
				CreatedAt: &commentTime,
				UpdatedAt: &commentTime,
			},
		},
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	issueID := int64(20000)
	issueNumber := 20
	issueTitle := "Lots of comments"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/20"
	issueLogin := "dave"
	result := &RepoBulkResult{
		Issues: []BulkIssue{
			{
				Issue: &gh.Issue{
					ID:        &issueID,
					Number:    &issueNumber,
					Title:     &issueTitle,
					State:     &issueState,
					HTMLURL:   &issueURL,
					User:      &gh.User{Login: &issueLogin},
					CreatedAt: &commentTime,
					UpdatedAt: &commentTime,
				},
				CommentsComplete: false,
			},
		},
	}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// REST fallback should have been called
	assert.Equal(int32(1), mock.listIssueCommentsCalled.Load())

	// Verify the REST comment landed
	issue, err := d.GetIssue(ctx, "owner", "repo", 20)
	require.NoError(t, err)
	require.NotNil(t, issue)

	events, err := d.ListIssueEvents(ctx, issue.ID)
	require.NoError(t, err)
	assert.Len(events, 1)
	assert.Equal("REST comment", events[0].Body)
}

func TestSyncRepoGraphQLIssuesClosureDetection(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)

	// Pre-seed an open issue that will not appear in GraphQL results
	_, err = d.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     30000,
		Number:         30,
		URL:            "https://github.com/owner/repo/issues/30",
		Title:          "Will be closed",
		Author:         "eve",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(t, err)

	closedAt := gh.Timestamp{Time: now}
	closedState := "closed"
	closedIssueID := int64(30000)
	closedNumber := 30
	closedTitle := "Will be closed"

	mock := &mockClient{
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			if number == 30 {
				return &gh.Issue{
					ID:       &closedIssueID,
					Number:   &closedNumber,
					Title:    &closedTitle,
					State:    &closedState,
					ClosedAt: &closedAt,
				}, nil
			}
			return nil, fmt.Errorf("unexpected issue %d", number)
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// GraphQL returns no issues (issue #30 was closed)
	result := &RepoBulkResult{Issues: []BulkIssue{}}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// Issue should now be closed
	issue, err := d.GetIssue(ctx, "owner", "repo", 30)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal("closed", issue.State)
	assert.NotNil(issue.ClosedAt)
}

func TestSyncRepoGraphQLIssuesPreservesExistingFields(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	fetchedAt := now.Add(-time.Hour)

	// Pre-seed issue with existing derived fields
	_, err = d.UpsertIssue(ctx, &db.Issue{
		RepoID:          repoID,
		PlatformID:      40000,
		Number:          40,
		URL:             "https://github.com/owner/repo/issues/40",
		Title:           "Existing issue",
		Author:          "frank",
		State:           "open",
		CommentCount:    5,
		DetailFetchedAt: &fetchedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
		LastActivityAt:  now,
	})
	require.NoError(t, err)

	commentTime := gh.Timestamp{Time: now}
	mock := &mockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// GraphQL returns the same issue with no comments (incomplete)
	issueID := int64(40000)
	issueNumber := 40
	issueTitle := "Existing issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/40"
	issueLogin := "frank"
	result := &RepoBulkResult{
		Issues: []BulkIssue{
			{
				Issue: &gh.Issue{
					ID:        &issueID,
					Number:    &issueNumber,
					Title:     &issueTitle,
					State:     &issueState,
					HTMLURL:   &issueURL,
					User:      &gh.User{Login: &issueLogin},
					CreatedAt: &commentTime,
					UpdatedAt: &commentTime,
				},
				CommentsComplete: false,
			},
		},
	}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// DetailFetchedAt should be preserved through the upsert path.
	// CommentCount is updated by the REST fallback (0 comments returned
	// by the mock), which is the expected behavior: REST gives ground truth.
	issue, err := d.GetIssue(ctx, "owner", "repo", 40)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.NotNil(issue.DetailFetchedAt)
	// REST fallback was called with empty mock — count reflects REST result.
	assert.Equal(0, issue.CommentCount)
}

func TestSyncRepoGraphQLIssuesFallbackToREST(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	issueTime := makeTimestamp(now)
	issueID := int64(50000)
	issueNumber := 50
	issueTitle := "REST issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/50"
	issueLogin := "grace"

	ghIssue := &gh.Issue{
		ID:        &issueID,
		Number:    &issueNumber,
		Title:     &issueTitle,
		State:     &issueState,
		HTMLURL:   &issueURL,
		User:      &gh.User{Login: &issueLogin},
		CreatedAt: issueTime,
		UpdatedAt: issueTime,
	}

	mock := &mockClient{
		listOpenPRsErr: notModifiedErr(),
		openIssues:     []*gh.Issue{ghIssue},
		getIssueFn: func(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
			return ghIssue, nil
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, testBudget(1000),
	)

	// No fetchers set — fetcherFor returns nil, REST path runs.
	syncer.RunOnce(ctx)

	issue, err := d.GetIssue(ctx, "owner", "repo", 50)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal("REST issue", issue.Title)
	assert.Equal("grace", issue.Author)
}
