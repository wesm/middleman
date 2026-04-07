package github

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
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

// mockClient implements Client with configurable canned responses.
type mockClient struct {
	openPRs           []*gh.PullRequest
	singlePR          *gh.PullRequest
	getPullRequestFn  func(context.Context, string, string, int) (*gh.PullRequest, error)
	getIssueFn        func(context.Context, string, string, int) (*gh.Issue, error)
	comments          []*gh.IssueComment
	reviews           []*gh.PullRequestReview
	commits           []*gh.RepositoryCommit
	ciStatus          *gh.CombinedStatus
	checkRuns         []*gh.CheckRun
	listOpenPRsCalled bool
}

func (m *mockClient) ListOpenPullRequests(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
	m.listOpenPRsCalled = true
	return m.openPRs, nil
}

func (m *mockClient) ListOpenIssues(_ context.Context, _, _ string) ([]*gh.Issue, error) {
	return nil, nil
}

func (m *mockClient) GetIssue(
	ctx context.Context, owner, repo string, number int,
) (*gh.Issue, error) {
	if m.getIssueFn != nil {
		return m.getIssueFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *mockClient) GetUser(_ context.Context, login string) (*gh.User, error) {
	return &gh.User{Login: &login}, nil
}

func (m *mockClient) GetPullRequest(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
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
	return m.checkRuns, nil
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

func (m *mockClient) EditPullRequest(
	_ context.Context, _, _ string, _ int, state string,
) (*gh.PullRequest, error) {
	return &gh.PullRequest{State: &state}, nil
}

func (m *mockClient) EditIssue(
	_ context.Context, _, _ string, _ int, state string,
) (*gh.Issue, error) {
	return &gh.Issue{State: &state}, nil
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

func TestSyncerStopIsIdempotent(t *testing.T) {
	syncer := NewSyncer(map[string]Client{"github.com": &mockClient{}}, nil, nil, nil, time.Minute, nil)
	syncer.Stop()
	syncer.Stop() // must not panic
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

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)
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

	// Commit event should have been stored.
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

func TestSyncSingleFlight(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	callCount := 0
	mc := &mockClient{
		openPRs: []*gh.PullRequest{},
	}
	// Wrap in a counter client to detect calls.
	_ = mc

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)

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

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)

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

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)

	// First sync: PR is new, full fetch triggers, returns "unknown".
	syncer.RunOnce(ctx)

	stored, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("unknown", stored.MergeableState)
	assert.Equal(1, fetchCount, "first sync should trigger one full fetch")

	// Second sync: same UpdatedAt, but MergeableState == "unknown" should
	// trigger another full fetch. The callback now returns "clean".
	syncer.RunOnce(ctx)

	stored2, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(stored2)
	assert.Equal("clean", stored2.MergeableState, "second sync should resolve unknown to clean")
	assert.Equal(2, fetchCount, "second sync should trigger another full fetch for unknown state")
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

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)
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

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)

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
		time.Hour, nil,
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

func TestIsTrackedRepo(t *testing.T) {
	assert := Assert.New(t)
	database := openTestDB(t)
	mc := &mockClient{}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, database, nil, []RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
		{Owner: "corp", Name: "lib", PlatformHost: "github.com"},
	}, time.Minute, nil)

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
	}, time.Minute, nil)

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
	}, time.Minute, nil)

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
	}, time.Minute, nil)

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

	syncer := NewSyncer(clients, d, nil, repos, time.Minute, nil)
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
		time.Minute, nil,
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
		time.Minute, nil,
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
		time.Minute, nil,
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
		time.Minute, nil,
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
		time.Hour, nil, // bulk sync at 1h -- won't fire during test
	)
	syncer.SetWatchInterval(50 * time.Millisecond)

	var hookCalls []int
	syncer.SetOnMRSynced(
		func(_ string, _ string, mr *db.MergeRequest) {
			hookCalls = append(hookCalls, mr.Number)
		},
	)

	syncer.SetWatchedMRs([]WatchedMR{
		{Owner: "acme", Name: "app", Number: 7},
	})

	syncer.Start(ctx)
	defer syncer.Stop()

	// Wait for at least one fast-sync tick.
	assert.Eventually(func() bool {
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
		time.Hour, nil,
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
		time.Hour, nil,
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

	rt := NewRateTracker(d, "github.com")
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
		map[string]*RateTracker{"github.com": rt},
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
		time.Hour, nil,
	)

	// Insert the repo with the GHE host so SyncMR can find it.
	_, err := d.WriteDB().ExecContext(ctx,
		`INSERT INTO middleman_repos
		    (platform, platform_host, owner, name)
		 VALUES ('github', 'ghes.corp.com', 'corp', 'internal')
		 ON CONFLICT DO NOTHING`,
	)
	require.NoError(err)

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
}
