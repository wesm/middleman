package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty/v2"
	gh "github.com/google/go-github/v84/github"
	"github.com/shurcooL/githubv4"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	"github.com/wesm/middleman/internal/gitenv"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/stacks"
	"github.com/wesm/middleman/internal/workspace"
	"github.com/wesm/middleman/internal/workspace/localruntime"
)

func cleanupContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func gracefulShutdown(t *testing.T, srv interface{ Shutdown(context.Context) error }) {
	t.Helper()
	ctx, cancel := cleanupContext(t)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// mockGH implements ghclient.Client for testing.
type mockGH struct {
	getRepositoryFn           func(context.Context, string, string) (*gh.Repository, error)
	getPullRequestFn          func(context.Context, string, string, int) (*gh.PullRequest, error)
	getIssueFn                func(context.Context, string, string, int) (*gh.Issue, error)
	getUserFn                 func(context.Context, string) (*gh.User, error)
	markReadyForReviewFn      func(context.Context, string, string, int) (*gh.PullRequest, error)
	editPullRequestFn         func(context.Context, string, string, int, ghclient.EditPullRequestOpts) (*gh.PullRequest, error)
	editIssueFn               func(context.Context, string, string, int, string) (*gh.Issue, error)
	mergePullRequestFn        func(context.Context, string, string, int, string, string, string) (*gh.PullRequestMergeResult, error)
	listWorkflowRunsForHeadFn func(context.Context, string, string, string) ([]*gh.WorkflowRun, error)
	approveWorkflowRunFn      func(context.Context, string, string, int64) error
	listReposByOwnerFn        func(context.Context, string) ([]*gh.Repository, error)
	listOpenPullRequestsFn    func(context.Context, string, string) ([]*gh.PullRequest, error)
	listCheckRunsForRefFn     func(context.Context, string, string, string) ([]*gh.CheckRun, error)
	getCombinedStatusFn       func(context.Context, string, string, string) (*gh.CombinedStatus, error)
	listOpenPRsErr            error
	listOpenIssuesFn          func(context.Context, string, string) ([]*gh.Issue, error)
	listIssueCommentsFn       func(context.Context, string, string, int) ([]*gh.IssueComment, error)
	listIssueCommentsErr      error
}

func (m *mockGH) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*gh.PullRequest, error) {
	if m.listOpenPullRequestsFn != nil {
		return m.listOpenPullRequestsFn(ctx, owner, repo)
	}
	if m.listOpenPRsErr != nil {
		return nil, m.listOpenPRsErr
	}
	return nil, nil
}

func (m *mockGH) ListOpenIssues(ctx context.Context, owner, repo string) ([]*gh.Issue, error) {
	if m.listOpenIssuesFn != nil {
		return m.listOpenIssuesFn(ctx, owner, repo)
	}
	return nil, nil
}

func (m *mockGH) GetIssue(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
	if m.getIssueFn != nil {
		return m.getIssueFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *mockGH) GetUser(ctx context.Context, login string) (*gh.User, error) {
	if m.getUserFn != nil {
		return m.getUserFn(ctx, login)
	}
	return &gh.User{Login: &login}, nil
}

func (m *mockGH) ListRepositoriesByOwner(
	ctx context.Context, owner string,
) ([]*gh.Repository, error) {
	if m.listReposByOwnerFn != nil {
		return m.listReposByOwnerFn(ctx, owner)
	}
	return nil, nil
}

func (m *mockGH) GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
	if m.getPullRequestFn != nil {
		return m.getPullRequestFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *mockGH) ListIssueComments(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.IssueComment, error) {
	if m.listIssueCommentsFn != nil {
		return m.listIssueCommentsFn(ctx, owner, repo, number)
	}
	if m.listIssueCommentsErr != nil {
		return nil, m.listIssueCommentsErr
	}
	return nil, nil
}

func (m *mockGH) ListIssueCommentsIfChanged(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.IssueComment, error) {
	if m.listIssueCommentsFn == nil && m.listIssueCommentsErr == nil {
		return nil, &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		}
	}
	return m.ListIssueComments(ctx, owner, repo, number)
}

func (m *mockGH) ListReviews(
	_ context.Context, _, _ string, _ int,
) ([]*gh.PullRequestReview, error) {
	return nil, nil
}

func (m *mockGH) ListCommits(
	_ context.Context, _, _ string, _ int,
) ([]*gh.RepositoryCommit, error) {
	return nil, nil
}

func (m *mockGH) ListForcePushEvents(
	_ context.Context, _, _ string, _ int,
) ([]ghclient.ForcePushEvent, error) {
	return nil, nil
}

func (m *mockGH) GetCombinedStatus(
	ctx context.Context, owner, repo, ref string,
) (*gh.CombinedStatus, error) {
	if m.getCombinedStatusFn != nil {
		return m.getCombinedStatusFn(ctx, owner, repo, ref)
	}
	return nil, nil
}

func (m *mockGH) ListCheckRunsForRef(
	ctx context.Context, owner, repo, ref string,
) ([]*gh.CheckRun, error) {
	if m.listCheckRunsForRefFn != nil {
		return m.listCheckRunsForRefFn(ctx, owner, repo, ref)
	}
	return nil, nil
}

func (m *mockGH) ListWorkflowRunsForHeadSHA(
	ctx context.Context, owner, repo, headSHA string,
) ([]*gh.WorkflowRun, error) {
	if m.listWorkflowRunsForHeadFn != nil {
		return m.listWorkflowRunsForHeadFn(ctx, owner, repo, headSHA)
	}
	return nil, nil
}

func (m *mockGH) ApproveWorkflowRun(
	ctx context.Context, owner, repo string, runID int64,
) error {
	if m.approveWorkflowRunFn != nil {
		return m.approveWorkflowRunFn(ctx, owner, repo, runID)
	}
	return nil
}

func (m *mockGH) CreateIssueComment(
	_ context.Context, _, _ string, _ int, body string,
) (*gh.IssueComment, error) {
	id := int64(42)
	return &gh.IssueComment{
		ID:   &id,
		Body: &body,
	}, nil
}

func (m *mockGH) GetRepository(
	ctx context.Context, owner, repo string,
) (*gh.Repository, error) {
	if m.getRepositoryFn != nil {
		return m.getRepositoryFn(ctx, owner, repo)
	}
	return &gh.Repository{
		Name:     &repo,
		Owner:    &gh.User{Login: &owner},
		Archived: new(false),
	}, nil
}

func (m *mockGH) CreateReview(
	_ context.Context, _, _ string, _ int, _ string, _ string,
) (*gh.PullRequestReview, error) {
	id := int64(99)
	state := "APPROVED"
	return &gh.PullRequestReview{ID: &id, State: &state}, nil
}

func (m *mockGH) MarkPullRequestReadyForReview(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	if m.markReadyForReviewFn != nil {
		return m.markReadyForReviewFn(ctx, owner, repo, number)
	}
	draft := false
	return &gh.PullRequest{Number: &number, Draft: &draft}, nil
}

func (m *mockGH) MergePullRequest(
	ctx context.Context, owner, repo string, number int,
	commitTitle, commitMessage, method string,
) (*gh.PullRequestMergeResult, error) {
	if m.mergePullRequestFn != nil {
		return m.mergePullRequestFn(ctx, owner, repo, number, commitTitle, commitMessage, method)
	}
	merged := true
	sha := "abc123"
	msg := "merged"
	return &gh.PullRequestMergeResult{
		Merged: &merged, SHA: &sha, Message: &msg,
	}, nil
}

func (m *mockGH) EditPullRequest(
	ctx context.Context, owner, repo string, number int, opts ghclient.EditPullRequestOpts,
) (*gh.PullRequest, error) {
	if m.editPullRequestFn != nil {
		return m.editPullRequestFn(ctx, owner, repo, number, opts)
	}
	pr := &gh.PullRequest{}
	if opts.State != nil {
		pr.State = opts.State
	}
	if opts.Title != nil {
		pr.Title = opts.Title
	}
	if opts.Body != nil {
		pr.Body = opts.Body
	}
	now := time.Now().UTC()
	ghTime := gh.Timestamp{Time: now}
	pr.UpdatedAt = &ghTime
	return pr, nil
}

func (m *mockGH) EditIssue(
	ctx context.Context, owner, repo string, number int, state string,
) (*gh.Issue, error) {
	if m.editIssueFn != nil {
		return m.editIssueFn(ctx, owner, repo, number, state)
	}
	return &gh.Issue{State: &state}, nil
}

func (m *mockGH) ListPullRequestsPage(
	_ context.Context, _, _, _ string, _ int,
) ([]*gh.PullRequest, bool, error) {
	return nil, false, nil
}

func (m *mockGH) ListIssuesPage(
	_ context.Context, _, _, _ string, _ int,
) ([]*gh.Issue, bool, error) {
	return nil, false, nil
}

// InvalidateListETagsForRepo is a no-op for the server test mock,
// which has no underlying HTTP cache.
func (m *mockGH) InvalidateListETagsForRepo(_, _ string, _ ...string) {}

// setupTestServer opens a temp DB, builds a Server, and returns both.
func setupTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	return setupTestServerWithMock(t, &mockGH{})
}

func setupTestServerWithDatabase(
	t *testing.T, database *db.DB, repos []ghclient.RepoRef,
) *Server {
	t.Helper()

	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": &mockGH{}}, database, nil, repos, time.Minute, nil, nil)
	t.Cleanup(syncer.Stop)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	return srv
}

func setupTestServerWithMock(t *testing.T, mock *mockGH) (*Server, *db.DB) {
	t.Helper()
	return setupTestServerWithRepos(t, mock, defaultTestRepos)
}

var defaultTestRepos = []ghclient.RepoRef{
	{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
}

func setupTestServerWithRepos(
	t *testing.T, mock *mockGH, repos []ghclient.RepoRef,
) (*Server, *db.DB) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, repos, time.Minute, nil, nil)
	// Drain any TriggerRun goroutines (fired by handlers like
	// POST /sync) before tests tear down. Registered after the DB
	// cleanup so LIFO ordering runs Stop first: without this, a
	// leaked goroutine from one test's handler can outlive its DB.
	t.Cleanup(syncer.Stop)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
	// Registered after the DB cleanup so LIFO ordering runs Shutdown
	// first and lets background goroutines finish before DB close.
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	return srv, database
}

func setupTestClient(t *testing.T, srv *Server) *apiclient.Client {
	t.Helper()
	return setupTestClientWithBaseURL(t, srv, "http://middleman.test")
}

func setupTestClientWithBaseURL(
	t *testing.T,
	srv *Server,
	baseURL string,
) *apiclient.Client {
	t.Helper()

	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body io.Reader = http.NoBody
			if req.Body != nil {
				payload, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				_ = req.Body.Close()
				body = strings.NewReader(string(payload))
			}

			serverReq := httptest.NewRequest(req.Method, req.URL.String(), body)
			serverReq.Header = req.Header.Clone()
			// Ensure mutation requests have Content-Type for CSRF.
			if req.Method != http.MethodGet && serverReq.Header.Get("Content-Type") == "" {
				serverReq.Header.Set("Content-Type", "application/json")
			}
			serverReq = serverReq.WithContext(req.Context())

			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, serverReq)
			return rr.Result(), nil
		}),
	}

	client, err := apiclient.NewWithHTTPClient(baseURL, httpClient)
	require.NoError(t, err)

	return client
}

func assertRFC3339UTC(t *testing.T, got string, want time.Time) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, got)
	require.NoError(t, err)
	Assert.Equal(t, want.UTC(), parsed.UTC())
	Assert.True(t, strings.HasSuffix(got, "Z"), "expected UTC RFC3339 with trailing Z: %s", got)
}

func setTestServerNow(t *testing.T, srv *Server, now time.Time) {
	t.Helper()
	srv.now = func() time.Time { return now }
}

func testEDTTime(hour, minute int) time.Time {
	//nolint:forbidigo // Test fixture intentionally uses a non-UTC timestamp to verify UTC normalization.
	return time.Date(2026, 4, 11, hour, minute, 0, 0, time.FixedZone("EDT", -4*60*60))
}

func assertTimePtrUTC(t *testing.T, got *time.Time) {
	t.Helper()
	require.NotNil(t, got)
	Assert.Equal(t, time.UTC, got.Location())
}

func assertTimePtrEqualsUTC(t *testing.T, got *time.Time, want time.Time) {
	t.Helper()
	assertTimePtrUTC(t, got)
	Assert.Equal(t, want.UTC(), got.UTC())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type staleReadyForReviewError struct{ err error }

func (e *staleReadyForReviewError) Error() string      { return e.err.Error() }
func (e *staleReadyForReviewError) Unwrap() error      { return e.err }
func (e *staleReadyForReviewError) StatusCode() int    { return http.StatusNotFound }
func (e *staleReadyForReviewError) IsStaleState() bool { return true }

type seedPROpt func(*db.MergeRequest)

func withSeedPRLabels(labels []db.Label) seedPROpt {
	return func(pr *db.MergeRequest) { pr.Labels = labels }
}

func withSeedPRHeadSHA(headSHA string) seedPROpt {
	return func(pr *db.MergeRequest) { pr.PlatformHeadSHA = headSHA }
}

// seedPR inserts a repo and a PR into the DB, returning the PR's internal ID.
func seedPR(t *testing.T, database *db.DB, owner, name string, number int, opts ...seedPROpt) int64 {
	t.Helper()
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, "github.com", owner, name)
	require.NoError(t, err)

	numberText := strconv.Itoa(number)
	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     int64(number) * 1000,
		Number:         number,
		URL:            "https://github.com/" + owner + "/" + name + "/pull/" + numberText,
		Title:          "Test PR #" + numberText,
		Author:         "testuser",
		State:          "open",
		IsDraft:        false,
		Body:           "test body",
		HeadBranch:     "feature",
		BaseBranch:     "main",
		Additions:      5,
		Deletions:      2,
		CommentCount:   0,
		ReviewDecision: "",
		CIStatus:       "",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	for _, opt := range opts {
		opt(pr)
	}

	prID, err := database.UpsertMergeRequest(ctx, pr)
	require.NoError(t, err)
	if len(pr.Labels) > 0 {
		require.NoError(t, database.ReplaceMergeRequestLabels(ctx, repoID, prID, pr.Labels))
	}
	require.NoError(t, database.EnsureKanbanState(ctx, prID))

	return prID
}

func seedPRWithLabels(t *testing.T, database *db.DB, owner, name string, number int, labels []db.Label) int64 {
	t.Helper()
	return seedPR(t, database, owner, name, number, withSeedPRLabels(labels))
}

func seedPRWithHeadSHA(t *testing.T, database *db.DB, owner, name string, number int, headSHA string) int64 {
	t.Helper()
	return seedPR(t, database, owner, name, number, withSeedPRHeadSHA(headSHA))
}

func TestAPIMergePR405ReturnsGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Pull Request is not mergeable",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusConflict, resp.StatusCode())
	require.Contains(string(resp.Body), "Pull Request is not mergeable")
}

func TestAPIMergePR409ReturnsGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 409},
				Message:  "Head branch was modified",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusConflict, resp.StatusCode())
	require.Contains(string(resp.Body), "Head branch was modified")
}

func TestAPIMergePRNetworkErrorReturns502(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
	require.Contains(string(resp.Body), "connection refused")
}

func TestAPIMergePR422ForwardsGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusUnprocessableEntity},
				Message:  "Required status check is failing",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusUnprocessableEntity, resp.StatusCode())
	require.Contains(string(resp.Body), "Required status check is failing")
}

func TestAPIMergePR403ForwardsGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusForbidden},
				Message:  "Resource not accessible by integration",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusForbidden, resp.StatusCode())
	require.Contains(string(resp.Body), "Resource not accessible by integration")
}

func TestAPIMergePR5xxReturns502WithGitHubMessage(t *testing.T) {
	require := require.New(t)

	mock := &mockGH{
		mergePullRequestFn: func(_ context.Context, _, _ string, _ int, _, _, _ string) (*gh.PullRequestMergeResult, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusServiceUnavailable},
				Message:  "Service unavailable",
			}
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
	require.Contains(string(resp.Body), "Service unavailable")
}

func TestAPIMergePRStoresUTCTimestamps(t *testing.T) {
	require := require.New(t)

	srv, database := setupTestServer(t)
	handlerNow := testEDTTime(8, 30)
	setTestServerNow(t, srv, handlerNow)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.Equal("merged", pr.State)
	assertTimePtrEqualsUTC(t, pr.MergedAt, handlerNow)
	assertTimePtrEqualsUTC(t, pr.ClosedAt, handlerNow)
}

func TestAPIClientConstruction(t *testing.T) {
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)
	require.NotNil(t, client)
	require.NotNil(t, client.HTTP)
}

func TestAPIListPulls(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.ListPullsWithResponse(t.Context(), nil)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	assert := Assert.New(t)
	assert.Equal("acme", (*resp.JSON200)[0].RepoOwner)
	assert.Equal("widget", (*resp.JSON200)[0].RepoName)
	assert.Equal("github.com", (*resp.JSON200)[0].PlatformHost)
}

func TestAPIListPullsIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	description := "Needs a fix"
	seedPRWithLabels(t, database, "acme", "widget", 1, []db.Label{{
		Name:        "bug",
		Description: description,
		Color:       "d73a4a",
		IsDefault:   true,
	}})
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.ListPullsWithResponse(t.Context(), nil)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.NotNil((*resp.JSON200)[0].Labels)
	require.Equal([]generated.Label{{
		Name:        "bug",
		Description: &description,
		Color:       "d73a4a",
		IsDefault:   true,
	}}, *(*resp.JSON200)[0].Labels)
}

func TestAPIGetPull(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.MergeRequest)
	require.EqualValues(1, resp.JSON200.MergeRequest.Number)
	require.Equal("acme", resp.JSON200.RepoOwner)
	require.Equal("widget", resp.JSON200.RepoName)
}

func TestAPIGetPullAcceptsMixedCaseRepoPath(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "Acme", "Widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Equal("acme", resp.JSON200.RepoOwner)
	require.Equal("widget", resp.JSON200.RepoName)
}

func TestAPIListPullsAcceptsMixedCaseRepoFilter(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	repo := "Acme/Widget"
	resp, err := client.HTTP.ListPullsWithResponse(
		t.Context(), &generated.ListPullsParams{Repo: &repo},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.Equal("acme", (*resp.JSON200)[0].RepoOwner)
	require.Equal("widget", (*resp.JSON200)[0].RepoName)
}

func TestAPIGetPullIncludesBranches(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	mr := resp.JSON200.MergeRequest
	require.NotNil(mr)
	require.Equal("feature", mr.HeadBranch)
	require.Equal("main", mr.BaseBranch)
}

func TestAPIGetPullIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPRWithLabels(t, database, "acme", "widget", 1, []db.Label{{
		Name:      "enhancement",
		Color:     "a2eeef",
		IsDefault: false,
	}})
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.MergeRequest.Labels)
	require.Equal([]generated.Label{{
		Name:      "enhancement",
		Color:     "a2eeef",
		IsDefault: false,
	}}, *resp.JSON200.MergeRequest.Labels)
}

func TestAPIGetPullIsDBOnly(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			require.Fail("GET pull detail should not call GitHub API")
			return nil, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, _ string) ([]*gh.WorkflowRun, error) {
			require.Fail("GET pull detail should not call ListWorkflowRunsForHeadSHA")
			return nil, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPRWithHeadSHA(t, database, "acme", "widget", 1, "deadbeef")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.MergeRequest)
	// Seeded PR has no DetailFetchedAt, so detail_loaded should be false.
	assert.False(resp.JSON200.DetailLoaded)
	assert.Nil(resp.JSON200.DetailFetchedAt)
	// GET path uses DB state (useLivePR=false) and must not make
	// any live GitHub calls, including ListWorkflowRunsForHeadSHA.
	// WorkflowApproval is empty (zero value) since the DB-only path
	// returns early without checking workflows.
	require.NotNil(resp.JSON200.WorkflowApproval)
	assert.False(resp.JSON200.WorkflowApproval.Checked)
}

func TestAPISyncPRIncludesWorkflowApproval(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
			id := int64(1001)
			sha := "abc123"
			state := "open"
			title := "Synced PR"
			url := "https://github.com/acme/widget/pull/1"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("abc123", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:           new(int64(77)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	// Sync response uses workflowCheckRuns mode: reads PR state
	// from DB (just synced) and fetches workflow runs live.
	require.NotNil(resp.JSON200.WorkflowApproval)
	assert.True(resp.JSON200.WorkflowApproval.Checked)
	assert.True(resp.JSON200.WorkflowApproval.Required)
	assert.Equal(int64(1), resp.JSON200.WorkflowApproval.Count)
}

func TestAPIApproveWorkflows(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	approvedRunIDs := []int64{}
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
			id := int64(1001)
			sha := "abc123"
			state := "open"
			title := "Workflow PR"
			url := "https://github.com/acme/widget/pull/1"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("abc123", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:           new(int64(81)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
				{
					ID:           new(int64(82)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
				{
					ID:           new(int64(99)),
					HeadSHA:      new("zzz999"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
			}, nil
		},
		approveWorkflowRunFn: func(_ context.Context, owner, repo string, runID int64) error {
			require.Equal("acme", owner)
			require.Equal("widget", repo)
			approvedRunIDs = append(approvedRunIDs, runID)
			return nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberApproveWorkflowsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.ApprovedCount)
	assert.Equal("approved_workflows", resp.JSON200.Status)
	assert.EqualValues(2, *resp.JSON200.ApprovedCount)
	assert.Equal([]int64{81, 82}, approvedRunIDs)

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("abc123", pr.PlatformHeadSHA)
}

func TestAPIApproveWorkflowsZeroMatchesStillSyncsPR(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(1002)
			sha := "abc123"
			state := "open"
			title := "Workflow PR"
			url := "https://github.com/acme/widget/pull/1"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("abc123", headSHA)
			return nil, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberApproveWorkflowsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.Equal("approved_workflows", resp.JSON200.Status)

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("abc123", pr.PlatformHeadSHA)
}

func TestAPIApproveWorkflowsReturnsUnderlyingApprovalErrorAfterPartialFailure(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	approvedRunIDs := []int64{}
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(1003)
			sha := "abc123"
			state := "open"
			title := "Workflow PR"
			url := "https://github.com/acme/widget/pull/1"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("abc123", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:           new(int64(91)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
				{
					ID:           new(int64(92)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
			}, nil
		},
		approveWorkflowRunFn: func(_ context.Context, _, _ string, runID int64) error {
			approvedRunIDs = append(approvedRunIDs, runID)
			if runID == 92 {
				return fmt.Errorf("permission denied")
			}
			return nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberApproveWorkflowsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
	require.NotNil(resp.ApplicationproblemJSONDefault)
	require.NotNil(resp.ApplicationproblemJSONDefault.Detail)
	assert.Contains(*resp.ApplicationproblemJSONDefault.Detail, "permission denied")
	assert.Equal([]int64{91, 92}, approvedRunIDs)

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("abc123", pr.PlatformHeadSHA)
}

// TestAPISyncPRIncludesWorkflowApprovalForForkPR covers the regression where
// runs from fork-based PRs have an empty pull_requests array in GitHub's API.
// The sync path must still flag workflow approval as required, otherwise the
// UI never shows the approve button for the exact case it was built for.
func TestAPISyncPRIncludesWorkflowApprovalForForkPR(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(2001)
			sha := "forkhead"
			state := "open"
			title := "Fork PR"
			url := "https://github.com/acme/widget/pull/1"
			cloneURL := "https://github.com/fork/widget.git"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head: &gh.PullRequestBranch{
					SHA:  &sha,
					Ref:  new("feature"),
					Repo: &gh.Repository{CloneURL: &cloneURL, FullName: new("fork/widget")},
				},
				Base: &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("forkhead", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:             new(int64(55)),
					HeadSHA:        new("forkhead"),
					Event:          new("pull_request"),
					HeadBranch:     new("feature"),
					HeadRepository: &gh.Repository{FullName: new("fork/widget")},
					PullRequests:   []*gh.PullRequest{},
				},
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.WorkflowApproval)
	assert.True(resp.JSON200.WorkflowApproval.Checked)
	assert.True(resp.JSON200.WorkflowApproval.Required)
	assert.Equal(int64(1), resp.JSON200.WorkflowApproval.Count)
}

// TestAPIApproveWorkflowsForForkPR verifies the approve endpoint reaches
// ApproveWorkflowRun for a fork-triggered run when the run's head repo and
// branch match the PR.
func TestAPIApproveWorkflowsForForkPR(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	approvedRunIDs := []int64{}
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(2002)
			sha := "forkhead"
			state := "open"
			title := "Fork PR"
			url := "https://github.com/acme/widget/pull/1"
			cloneURL := "https://github.com/fork/widget.git"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head: &gh.PullRequestBranch{
					SHA:  &sha,
					Ref:  new("feature"),
					Repo: &gh.Repository{CloneURL: &cloneURL, FullName: new("fork/widget")},
				},
				Base: &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("forkhead", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:             new(int64(71)),
					HeadSHA:        new("forkhead"),
					Event:          new("pull_request"),
					HeadBranch:     new("feature"),
					HeadRepository: &gh.Repository{FullName: new("fork/widget")},
				},
			}, nil
		},
		approveWorkflowRunFn: func(_ context.Context, _, _ string, runID int64) error {
			approvedRunIDs = append(approvedRunIDs, runID)
			return nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberApproveWorkflowsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.ApprovedCount)
	assert.Equal("approved_workflows", resp.JSON200.Status)
	assert.EqualValues(1, *resp.JSON200.ApprovedCount)
	assert.Equal([]int64{71}, approvedRunIDs)
}

// TestAPISyncPRIgnoresWorkflowRunsForOtherPRAtSameSHA covers the regression
// where two PRs share a head SHA and a populated pull_requests association
// points at the other PR. The sync path must not flag workflow approval as
// required for the wrong PR.
func TestAPISyncPRIgnoresWorkflowRunsForOtherPRAtSameSHA(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(3001)
			sha := "sharedsha"
			state := "open"
			title := "Shared SHA PR"
			url := "https://github.com/acme/widget/pull/1"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("sharedsha", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:           new(int64(88)),
					HeadSHA:      new("sharedsha"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(99)}},
				},
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.WorkflowApproval)
	assert.True(resp.JSON200.WorkflowApproval.Checked)
	assert.False(resp.JSON200.WorkflowApproval.Required)
	assert.Equal(int64(0), resp.JSON200.WorkflowApproval.Count)
}

// TestAPIApproveWorkflowsIgnoresRunsForOtherPRAtSameSHA verifies the approve
// endpoint does not call ApproveWorkflowRun for runs whose pull_requests
// association points at a different PR sharing the same head SHA.
func TestAPIApproveWorkflowsIgnoresRunsForOtherPRAtSameSHA(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	approvedRunIDs := []int64{}
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(3002)
			sha := "sharedsha"
			state := "open"
			title := "Shared SHA PR"
			url := "https://github.com/acme/widget/pull/1"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head:      &gh.PullRequestBranch{SHA: &sha, Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("sharedsha", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:           new(int64(88)),
					HeadSHA:      new("sharedsha"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(99)}},
				},
				{
					ID:           new(int64(89)),
					HeadSHA:      new("sharedsha"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(1)}},
				},
			}, nil
		},
		approveWorkflowRunFn: func(_ context.Context, _, _ string, runID int64) error {
			approvedRunIDs = append(approvedRunIDs, runID)
			return nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberApproveWorkflowsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.ApprovedCount)
	assert.EqualValues(1, *resp.JSON200.ApprovedCount)
	assert.Equal([]int64{89}, approvedRunIDs)
}

// TestAPIApproveWorkflowsRejectsRunFromDifferentForkAtSameSHA exercises the
// safety guarantee that two distinct forks sharing a head SHA do not
// cross-approve. The PR's head repo is alice/widget; the run's head repo is
// bob/widget. ApproveWorkflowRun must not be called.
func TestAPIApproveWorkflowsRejectsRunFromDifferentForkAtSameSHA(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	approvedRunIDs := []int64{}
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(4001)
			sha := "sharedsha"
			state := "open"
			title := "Alice Fork PR"
			url := "https://github.com/acme/widget/pull/1"
			cloneURL := "https://github.com/alice/widget.git"
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				UpdatedAt: &updatedAt,
				CreatedAt: &createdAt,
				Head: &gh.PullRequestBranch{
					SHA:  &sha,
					Ref:  new("feature"),
					Repo: &gh.Repository{CloneURL: &cloneURL, FullName: new("alice/widget")},
				},
				Base: &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
		listWorkflowRunsForHeadFn: func(_ context.Context, _, _, headSHA string) ([]*gh.WorkflowRun, error) {
			require.Equal("sharedsha", headSHA)
			return []*gh.WorkflowRun{
				{
					ID:             new(int64(123)),
					HeadSHA:        new("sharedsha"),
					Event:          new("pull_request"),
					HeadBranch:     new("feature"),
					HeadRepository: &gh.Repository{FullName: new("bob/widget")},
				},
			}, nil
		},
		approveWorkflowRunFn: func(_ context.Context, _, _ string, runID int64) error {
			approvedRunIDs = append(approvedRunIDs, runID)
			return nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberApproveWorkflowsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.Empty(approvedRunIDs)
}

func TestAPIGetPullNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 999,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode())
	require.NotNil(t, resp.ApplicationproblemJSONDefault)
}

// TestAPIGetPullEmitsDiffWarningWhenSHAsMissing covers the case where a
// previous diff sync failed and left the PR row without diff SHAs. The
// resolveItem path treats DiffSyncError as success and the resolve
// response has no warnings field, so the only place a client can learn
// the diff is unavailable is the next getPull call. This regression
// test pins that behavior so the warning can't silently disappear.
func TestAPIGetPullEmitsDiffWarningWhenSHAsMissing(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	// HasDiffSync gates the inferred warning, so the syncer must be
	// constructed with a non-nil clone manager. The manager itself is
	// never invoked by getPull.
	clonesDir := t.TempDir()
	clones := gitclone.New(clonesDir, nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 1)

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings, "warnings field should be set when diff is missing")
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	warning := warnings[0]
	assert.Contains(warning, "Diff data is unavailable")

	// Sanitization invariants: the warning must not leak any internal
	// detail even when emitted from the read path.
	assert.NotContains(warning, clonesDir)
	assert.NotContains(warning, "refs/")
	assert.NotContains(warning, "rev-parse")
}

// TestAPIGetPullNoDiffWarningWhenSHAsPresent verifies the warning does
// not fire when the row already carries valid diff SHAs that match the
// latest platform head.
func TestAPIGetPullNoDiffWarningWhenSHAsPresent(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 2)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	headSHA := "deadbeef00000000000000000000000000000001"
	baseSHA := "deadbeef00000000000000000000000000000010"
	require.NoError(database.UpdatePlatformSHAs(
		ctx, repoID, 2, headSHA, baseSHA,
	))
	require.NoError(database.UpdateDiffSHAs(
		ctx, repoID, 2,
		headSHA,
		baseSHA,
		"deadbeef00000000000000000000000000000003",
	))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 2,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	if resp.JSON200.Warnings != nil {
		assert.Empty(*resp.JSON200.Warnings)
	}
}

// TestAPIGetPullEmitsStaleDiffWarning covers the case where a diff sync
// populated the row but a later push advanced the platform head while
// the next diff sync failed. The recorded DiffHeadSHA is valid but no
// longer matches PlatformHeadSHA, so the UI would show a diff from the
// previous revision without any indication of drift. The warning must
// fire in that case.
func TestAPIGetPullEmitsStaleDiffWarning(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 3)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	// Platform reports the latest head; the recorded diff SHAs are from
	// an earlier push that no longer matches.
	require.NoError(database.UpdatePlatformSHAs(
		ctx, repoID, 3,
		"deadbeef00000000000000000000000000000099",
		"deadbeef00000000000000000000000000000010",
	))
	require.NoError(database.UpdateDiffSHAs(
		ctx, repoID, 3,
		"deadbeef00000000000000000000000000000001",
		"deadbeef00000000000000000000000000000002",
		"deadbeef00000000000000000000000000000003",
	))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 3,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings, "warnings field should be set when diff is stale")
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	assert.Contains(warnings[0], "out of date")
}

// TestAPIGetPullEmitsStaleDiffWarningOnBaseDrift covers the symmetric
// case to the head-drift test: the PR head is unchanged but the base
// branch advanced and the next diff sync failed. diffWarnings must
// mirror getDiff staleness logic, which treats base drift as stale
// for open PRs.
func TestAPIGetPullEmitsStaleDiffWarningOnBaseDrift(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 4)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	// Head matches, but the platform base advanced past the recorded
	// diff base — for example a merge landed on main after the diff
	// sync ran.
	headSHA := "deadbeef00000000000000000000000000000001"
	require.NoError(database.UpdatePlatformSHAs(
		ctx, repoID, 4,
		headSHA,
		"deadbeef00000000000000000000000000000099",
	))
	require.NoError(database.UpdateDiffSHAs(
		ctx, repoID, 4,
		headSHA,
		"deadbeef00000000000000000000000000000010",
		"deadbeef00000000000000000000000000000020",
	))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 4,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings, "warnings field should be set when base drifted")
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	assert.Contains(warnings[0], "out of date")
}

// TestAPIGetPullEmitsStaleDiffWarningOnMergedPR pins the staleness
// branch for merged PRs. getDiff treats merged PRs as stale when the
// recorded DiffHeadSHA no longer matches PlatformHeadSHA, so the
// warning must fire in the same case. Without this coverage a merged
// PR with a stale recorded diff would render outdated content with no
// indication.
func TestAPIGetPullEmitsStaleDiffWarningOnMergedPR(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 5)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	now := time.Now().UTC().Truncate(time.Second)
	mergedAt := now
	require.NoError(database.UpdateClosedMRState(
		ctx, repoID, 5, "merged", now, &mergedAt, &mergedAt,
		"deadbeef00000000000000000000000000000099",
		"deadbeef00000000000000000000000000000010",
	))
	// Recorded diff was computed against an earlier head; the merge
	// commit advanced the platform head past it.
	require.NoError(database.UpdateDiffSHAs(
		ctx, repoID, 5,
		"deadbeef00000000000000000000000000000001",
		"deadbeef00000000000000000000000000000010",
		"deadbeef00000000000000000000000000000003",
	))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 5,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings, "warnings field should be set when merged diff is stale")
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	assert.Contains(warnings[0], "out of date")
}

// TestAPIGetPullEmitsDiffWarningWhenSHAsMissingClosed covers a closed
// (not merged) PR whose fetchAndUpdateClosed path failed to populate
// diff SHAs - for example because the clone fetch errored out. The
// previous diffWarnings implementation suppressed warnings for any
// non-open/non-merged state and the user would silently see no diff.
func TestAPIGetPullEmitsDiffWarningWhenSHAsMissingClosed(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 6)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	now := time.Now().UTC().Truncate(time.Second)
	closedAt := now
	require.NoError(database.UpdateClosedMRState(
		ctx, repoID, 6, "closed", now, nil, &closedAt,
		"deadbeef00000000000000000000000000000001",
		"deadbeef00000000000000000000000000000010",
	))
	// Diff SHAs intentionally left empty to simulate a closed PR whose
	// diff sync errored out.

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 6,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings, "warnings field should be set when closed PR diff is missing")
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	assert.Contains(warnings[0], "unavailable")
}

// TestAPIGetPullEmitsStaleDiffWarningOnClosedPR covers a closed (not
// merged) PR whose head or base advanced after the diff sync recorded
// SHAs. getDiff treats this as stale; diffWarnings must agree so the
// detail page shows a warning instead of silently rendering an old
// diff.
func TestAPIGetPullEmitsStaleDiffWarningOnClosedPR(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 7)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	now := time.Now().UTC().Truncate(time.Second)
	closedAt := now
	require.NoError(database.UpdateClosedMRState(
		ctx, repoID, 7, "closed", now, nil, &closedAt,
		"deadbeef00000000000000000000000000000099",
		"deadbeef00000000000000000000000000000010",
	))
	require.NoError(database.UpdateDiffSHAs(
		ctx, repoID, 7,
		"deadbeef00000000000000000000000000000001",
		"deadbeef00000000000000000000000000000010",
		"deadbeef00000000000000000000000000000003",
	))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 7,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings, "warnings field should be set when closed PR diff is stale")
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	assert.Contains(warnings[0], "out of date")
}

// TestAPIGetPullNoDiffWarningOnMergedPRWithBaseDrift pins the
// asymmetry between merged and open/closed staleness: merged PRs only
// care about head SHA drift because the base never advances after
// merge. A merged PR whose head matches but base differs must NOT
// emit a warning.
func TestAPIGetPullNoDiffWarningOnMergedPRWithBaseDrift(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	clones := gitclone.New(t.TempDir(), nil)
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	seedPR(t, database, "acme", "widget", 8)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	now := time.Now().UTC().Truncate(time.Second)
	mergedAt := now
	headSHA := "deadbeef00000000000000000000000000000001"
	require.NoError(database.UpdateClosedMRState(
		ctx, repoID, 8, "merged", now, &mergedAt, &mergedAt,
		headSHA,
		"deadbeef00000000000000000000000000000099",
	))
	require.NoError(database.UpdateDiffSHAs(
		ctx, repoID, 8,
		headSHA,
		"deadbeef00000000000000000000000000000010",
		"deadbeef00000000000000000000000000000003",
	))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 8,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	if resp.JSON200.Warnings != nil {
		assert.Empty(*resp.JSON200.Warnings)
	}
}

// TestAPISyncPRSanitizesDiffFailureWarning drives the syncPR handler
// through a real diff-sync failure and asserts the HTTP response body
// contains only the sanitized UserMessage. Previous roborev reviews
// flagged that nothing pins the boundary between the raw Error() chain
// (which may carry clone paths, refs, SHAs, and git stderr) and the
// sanitized client-facing string; a future refactor could reintroduce
// the leak without breaking any lower-level test. This test closes
// that gap by wiring a real Syncer to a clone Manager whose base dir
// is unreadable, so EnsureClone fails and the handler must surface
// only the sanitized warning.
func TestAPISyncPRSanitizesDiffFailureWarning(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	// Create a clone base dir that cannot be used: 0o000 blocks every
	// git command rooted under it, so syncMRDiff fails at the clone
	// stage. The exact error message will contain the locked path,
	// which is precisely the detail that must NOT reach the client.
	lockedBase := filepath.Join(t.TempDir(), "locked-clones")
	require.NoError(os.MkdirAll(lockedBase, 0o755))
	require.NoError(os.Chmod(lockedBase, 0o000))
	t.Cleanup(func() { _ = os.Chmod(lockedBase, 0o755) })
	clones := gitclone.New(lockedBase, nil)

	// Mock returns a live open PR with head and base SHAs populated,
	// so syncMRDiff enters the merge-base path rather than the early
	// return for missing SHAs.
	now := gh.Timestamp{Time: time.Now().UTC().Truncate(time.Second)}
	prState := "open"
	prID := int64(9001)
	prNumber := 9
	title := "sync-warning repro"
	body := "body"
	url := "https://github.com/acme/widget/pull/9"
	headSHA := "deadbeef00000000000000000000000000000099"
	baseSHA := "deadbeef00000000000000000000000000000088"
	login := "author"
	headRef := "feature"
	baseRef := "main"
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return &gh.PullRequest{
				ID:        &prID,
				Number:    &prNumber,
				State:     &prState,
				Title:     &title,
				Body:      &body,
				HTMLURL:   &url,
				User:      &gh.User{Login: &login},
				Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
				Base:      &gh.PullRequestBranch{Ref: &baseRef, SHA: &baseSHA},
				CreatedAt: &now,
				UpdatedAt: &now,
			}, nil
		},
	}

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database, clones, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
		t.Context(), "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	// Diff-sync failures are non-fatal: the handler must return 200
	// with the PR row and a warning, not a 502.
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Warnings)
	warnings := *resp.JSON200.Warnings
	require.Len(warnings, 1)
	warning := warnings[0]
	assert.Contains(warning, "Diff data is unavailable")

	// Sanitization invariants: the warning must not leak any internal
	// detail from the underlying error chain. This is the regression
	// test the reviewer asked for.
	assert.NotContains(warning, lockedBase, "warning must not leak clone path")
	assert.NotContains(warning, "chdir", "warning must not leak chdir stderr")
	assert.NotContains(warning, "fetch", "warning must not leak git command name")
	assert.NotContains(warning, "ensure bare clone", "warning must not leak fmt.Errorf chain")
	assert.NotContains(warning, "github.com/acme", "warning must not leak remote URL path")
}

func TestAPISetKanbanState(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetKanbanStateWithResponse(
		t.Context(),
		"acme",
		"widget",
		1,
		generated.SetKanbanStateJSONRequestBody{Status: "reviewing"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	require.Equal("reviewing", pr.KanbanStatus)
}

func TestAPISetKanbanStateRejectsInvalidStatus(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetKanbanStateWithResponse(
		t.Context(),
		"acme",
		"widget",
		1,
		generated.SetKanbanStateJSONRequestBody{Status: "nonsense"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
	require.NotNil(t, resp.ApplicationproblemJSONDefault)
}

func TestAPIListRepos(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	_, err := database.UpsertRepo(t.Context(), "github.com", "acme", "widget")
	require.NoError(err)

	resp, err := client.HTTP.ListReposWithResponse(t.Context())
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.Equal("acme", (*resp.JSON200)[0].Owner)
	require.Equal("widget", (*resp.JSON200)[0].Name)
}

func TestAPIPostPrCommentAllowsMixedCaseTrackedRepo(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServerWithRepos(
		t,
		&mockGH{},
		[]ghclient.RepoRef{{
			Owner:        "Acme",
			Name:         "widget",
			PlatformHost: "github.com",
		}},
	)
	client := setupTestClient(t, srv)

	seedPR(t, database, "acme", "widget", 7)

	resp, err := client.HTTP.PostPrCommentWithResponse(
		t.Context(),
		"acme",
		"widget",
		7,
		generated.PostPrCommentJSONRequestBody{Body: "looks good"},
	)
	require.NoError(err)
	require.Equal(http.StatusCreated, resp.StatusCode())
	require.NotNil(resp.JSON201)
}

func TestAPICommentAutocomplete(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	prID, err := database.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     12000,
		Number:         12,
		URL:            "https://github.com/acme/widget/pull/12",
		Title:          "Polish mentions",
		Author:         "alice",
		State:          "open",
		HeadBranch:     "feature-12",
		BaseBranch:     "main",
		CreatedAt:      time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second),
		UpdatedAt:      time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second),
		LastActivityAt: time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second),
	})
	require.NoError(err)
	require.NoError(database.EnsureKanbanState(ctx, prID))
	_, err = database.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     17000,
		Number:         17,
		URL:            "https://github.com/acme/widget/issues/17",
		Title:          "Mention bug",
		Author:         "alex",
		State:          "open",
		CreatedAt:      time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second),
		UpdatedAt:      time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second),
		LastActivityAt: time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second),
	})
	require.NoError(err)
	require.NoError(database.UpsertMREvents(ctx, []db.MREvent{{
		MergeRequestID: prID,
		EventType:      "comment",
		Author:         "albert",
		CreatedAt:      time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		DedupeKey:      "autocomplete-mr-comment",
	}}))

	userReq := httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/widget/comment-autocomplete?trigger=@&q=al&limit=10", nil)
	userRR := httptest.NewRecorder()
	srv.ServeHTTP(userRR, userReq)
	require.Equal(http.StatusOK, userRR.Code, userRR.Body.String())

	var userBody commentAutocompleteResponse
	require.NoError(json.NewDecoder(userRR.Body).Decode(&userBody))
	assert.Equal([]string{"albert", "alex", "alice"}, userBody.Users)
	assert.Empty(userBody.References)

	refReq := httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/widget/comment-autocomplete?trigger=%23&q=1&limit=10", nil)
	refRR := httptest.NewRecorder()
	srv.ServeHTTP(refRR, refReq)
	require.Equal(http.StatusOK, refRR.Code, refRR.Body.String())

	var refBody commentAutocompleteResponse
	require.NoError(json.NewDecoder(refRR.Body).Decode(&refBody))
	assert.Equal([]db.CommentAutocompleteReference{
		{Kind: "issue", Number: 17, Title: "Mention bug", State: "open"},
		{Kind: "pull", Number: 12, Title: "Polish mentions", State: "open"},
	}, refBody.References)
	assert.Empty(refBody.Users)
}

func TestAPICommentAutocompleteUsesRepoPlatformHost(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, "ghe.example.com", "acme", "widget")
	require.NoError(err)
	_, err = database.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     12000,
		Number:         12,
		URL:            "https://ghe.example.com/acme/widget/pull/12",
		Title:          "Polish mentions",
		Author:         "alice",
		State:          "open",
		HeadBranch:     "feature-12",
		BaseBranch:     "main",
		CreatedAt:      time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second),
		UpdatedAt:      time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second),
		LastActivityAt: time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second),
	})
	require.NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/widget/comment-autocomplete?trigger=%23&q=1&limit=10", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	var body commentAutocompleteResponse
	require.NoError(json.NewDecoder(rr.Body).Decode(&body))
	assert.Equal([]db.CommentAutocompleteReference{{Kind: "pull", Number: 12, Title: "Polish mentions", State: "open"}}, body.References)
}

func TestAPISyncStatus(t *testing.T) {
	require := require.New(t)

	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)
	srv.syncer.RunOnce(t.Context())

	resp, err := client.HTTP.GetSyncStatusWithResponse(t.Context())
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.False(resp.JSON200.Running)
	require.NotNil(resp.JSON200.LastRunAt)
	Assert.Equal(t, time.UTC, resp.JSON200.LastRunAt.Location())
}

func TestAPITriggerSyncIgnoresRequestCancellation(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, []ghclient.RepoRef{{
		Owner:        "acme",
		Name:         "widget",
		PlatformHost: "github.com",
	}}, time.Minute, nil, nil)
	t.Cleanup(func() { syncer.Stop() })
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
	t.Cleanup(syncer.Stop)

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	cancel()

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusAccepted, rr.Code, rr.Body.String())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		repos, err := database.ListRepos(t.Context())
		require.NoError(err)
		if len(repos) == 1 && repos[0].Owner == "acme" && repos[0].Name == "widget" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	Assert.Fail(t, "expected sync to complete despite request context cancellation")
}

func TestAPITriggerSyncBypassesNextSyncAfter(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	var listCalls atomic.Int32
	mock := &mockGH{
		listOpenPullRequestsFn: func(
			_ context.Context, _, _ string,
		) ([]*gh.PullRequest, error) {
			listCalls.Add(1)
			return nil, nil
		},
	}
	trackers := map[string]*ghclient.RateTracker{
		"github.com": ghclient.NewRateTracker(
			database, "github.com", "rest",
		),
	}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		[]ghclient.RepoRef{{
			Owner:        "acme",
			Name:         "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		trackers,
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	// Seed the host cooldown window exactly like a recent background sync.
	syncer.RunOnce(t.Context())
	require.Equal(int32(1), listCalls.Load())

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.TriggerSyncWithResponse(
		t.Context(),
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, resp.StatusCode())

	require.Eventually(func() bool {
		return listCalls.Load() == 2
	}, 2*time.Second, 10*time.Millisecond)
}

func TestAPIReadyForReview(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{
		markReadyForReviewFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(1001)
			title := "Ready PR"
			state := "open"
			url := "https://github.com/acme/widget/pull/1"
			author := "octocat"
			draft := false
			now := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				Draft:     &draft,
				CreatedAt: &now,
				UpdatedAt: &now,
				User:      &gh.User{Login: &author},
				Head:      &gh.PullRequestBranch{Ref: new("feature")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
			}, nil
		},
	}
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, defaultTestRepos, time.Minute, nil, nil)
	t.Cleanup(syncer.Stop)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
	client := setupTestClient(t, srv)

	repoID, err := database.UpsertRepo(t.Context(), "github.com", "acme", "widget")
	require.NoError(err)

	now := time.Now().UTC().Truncate(time.Second)
	prID, err := database.UpsertMergeRequest(t.Context(), &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     1001,
		Number:         1,
		URL:            "https://github.com/acme/widget/pull/1",
		Title:          "Ready PR",
		Author:         "octocat",
		State:          "open",
		IsDraft:        true,
		Body:           "",
		HeadBranch:     "feature",
		BaseBranch:     "main",
		Additions:      0,
		Deletions:      0,
		CommentCount:   0,
		ReviewDecision: "",
		CIStatus:       "",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)
	require.NoError(database.EnsureKanbanState(t.Context(), prID))

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	require.False(pr.IsDraft)
}

func TestAPISetStarred(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetStarredWithResponse(t.Context(), generated.SetStarredJSONRequestBody{
		ItemType: "pr",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	starred, err := database.IsStarred(t.Context(), "pr", 1, 1)
	require.NoError(err)
	require.True(starred)
}

func TestAPIUnsetStarred(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	require.NoError(database.SetStarred(t.Context(), "pr", 1, 1))
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.UnsetStarredWithResponse(t.Context(), generated.UnsetStarredJSONRequestBody{
		ItemType: "pr",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	starred, err := database.IsStarred(t.Context(), "pr", 1, 1)
	require.NoError(err)
	require.False(starred)
}

func TestAPISetStarredRejectsInvalidItemType(t *testing.T) {
	require := require.New(t)
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetStarredWithResponse(t.Context(), generated.SetStarredJSONRequestBody{
		ItemType: "repo",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(err)
	require.Equal(http.StatusBadRequest, resp.StatusCode())
	require.NotNil(resp.ApplicationproblemJSONDefault)
	require.NotNil(resp.ApplicationproblemJSONDefault.Detail)
	require.Contains(*resp.ApplicationproblemJSONDefault.Detail, "item_type must be 'pr' or 'issue'")
}

func TestOpenAPIEndpointReflectsHumaContract(t *testing.T) {
	require := require.New(t)
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	body := rr.Body.String()
	require.Contains(body, `"/activity"`)
	require.Contains(body, `"name":"since"`)
	require.Contains(body, `"capped"`)
	require.NotContains(body, `"name":"before"`)
	require.NotContains(body, `"has_more"`)
}

// seedIssue inserts a repo and an issue into the DB.
func seedIssue(t *testing.T, database *db.DB, owner, name string, number int, state string) int64 {
	t.Helper()
	ctx := t.Context()
	repoID, err := database.UpsertRepo(ctx, "github.com", owner, name)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	issue := &db.Issue{
		RepoID: repoID, PlatformID: int64(number) * 1000, Number: number,
		URL:   "https://github.com/" + owner + "/" + name + "/issues/1",
		Title: "Test Issue", Author: "testuser", State: state,
		CreatedAt: now, UpdatedAt: now, LastActivityAt: now,
	}
	if state == "closed" {
		issue.ClosedAt = &now
	}
	issueID, err := database.UpsertIssue(ctx, issue)
	require.NoError(t, err)
	return issueID
}

func seedIssueWithLabels(t *testing.T, database *db.DB, owner, name string, number int, state string, labels []db.Label) int64 {
	t.Helper()
	ctx := t.Context()
	issueID := seedIssue(t, database, owner, name, number, state)
	repo, err := database.GetRepoByOwnerName(ctx, owner, name)
	require.NoError(t, err)
	require.NoError(t, database.ReplaceIssueLabels(ctx, repo.ID, issueID, labels))
	return issueID
}

func seedIssueOnHost(
	t *testing.T, database *db.DB,
	host, owner, name string, number int,
	state, title string,
) int64 {
	t.Helper()
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, host, owner, name)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	issue := &db.Issue{
		RepoID:         repoID,
		PlatformID:     int64(number) * 1000,
		Number:         number,
		URL:            fmt.Sprintf("https://%s/%s/%s/issues/%d", host, owner, name, number),
		Title:          title,
		Author:         "testuser",
		State:          state,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	if state == "closed" {
		issue.ClosedAt = &now
	}

	issueID, err := database.UpsertIssue(ctx, issue)
	require.NoError(t, err)
	return issueID
}

func TestAPIClosePR(t *testing.T) {
	require := require.New(t)

	srv, database := setupTestServer(t)
	handlerNow := testEDTTime(9, 15)
	setTestServerNow(t, srv, handlerNow)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.Equal("closed", pr.State)
	assertTimePtrEqualsUTC(t, pr.ClosedAt, handlerNow)
}

func TestAPIReopenPR(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	ctx := t.Context()

	// Close it first.
	repo, err := database.GetRepoByOwnerName(ctx, "acme", "widget")
	require.NoError(err)
	now := time.Now()
	require.NoError(database.UpdateMRState(ctx, repo.ID, 1, "closed", nil, &now))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		ctx, "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "open"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(ctx, "acme", "widget", 1)
	require.NoError(err)
	require.Equal("open", pr.State)
	require.Nil(pr.ClosedAt, "closed_at should be cleared on reopen")
}

func TestAPIClosePRRejectsMerged(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	ctx := t.Context()

	repo, err := database.GetRepoByOwnerName(ctx, "acme", "widget")
	require.NoError(err)
	now := time.Now()
	require.NoError(database.UpdateMRState(ctx, repo.ID, 1, "merged", &now, &now))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		ctx, "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "open"},
	)
	require.NoError(err)
	require.Equal(http.StatusConflict, resp.StatusCode())
}

func TestAPIClosePRInvalidState(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "nonsense"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPICloseIssue(t *testing.T) {
	require := require.New(t)

	srv, database := setupTestServer(t)
	handlerNow := testEDTTime(10, 45)
	setTestServerNow(t, srv, handlerNow)
	seedIssue(t, database, "acme", "widget", 5, "open")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		t.Context(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	issue, err := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.NoError(err)
	require.Equal("closed", issue.State)
	assertTimePtrEqualsUTC(t, issue.ClosedAt, handlerNow)
}

func TestAPIReopenIssue(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedIssue(t, database, "acme", "widget", 5, "closed")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		t.Context(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "open"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	issue, err := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.NoError(err)
	require.Equal("open", issue.State)
	require.Nil(issue.ClosedAt, "closed_at should be cleared on reopen")
}

func TestAPISyncPRDoesNotOverwriteNewerStateChange(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	staleUpdatedAt := time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC)
	syncStarted := make(chan struct{}, 1)
	releaseSync := make(chan struct{})
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
			syncStarted <- struct{}{}
			<-releaseSync

			id := int64(101)
			state := "open"
			title := "stale sync"
			url := "https://github.com/acme/widget/pull/1"
			author := "alice"
			headSHA := "abc123"
			baseSHA := "def456"
			featureRef := "feature"
			mainRef := "main"
			createdAt := gh.Timestamp{Time: staleUpdatedAt.Add(-time.Hour)}
			updatedAt := gh.Timestamp{Time: staleUpdatedAt}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				User:      &gh.User{Login: &author},
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				Head:      &gh.PullRequestBranch{SHA: &headSHA, Ref: &featureRef},
				Base:      &gh.PullRequestBranch{SHA: &baseSHA, Ref: &mainRef},
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	syncDone := make(chan *generated.PostReposByOwnerByNamePullsByNumberSyncResponse, 1)
	syncErr := make(chan error, 1)
	go func() {
		resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
			t.Context(), "acme", "widget", 1,
		)
		if err != nil {
			syncErr <- err
			return
		}
		syncDone <- resp
	}()

	<-syncStarted

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	closedPR, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.Equal("closed", closedPR.State)
	require.NotNil(closedPR.ClosedAt)

	close(releaseSync)

	completed := false
	select {
	case err := <-syncErr:
		require.NoError(err)
		completed = true
	case resp := <-syncDone:
		require.Equal(http.StatusOK, resp.StatusCode())
		completed = true
	case <-time.After(5 * time.Second):
	}
	require.True(completed, "timed out waiting for stale PR sync")

	finalPR, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	assert.Equal("closed", finalPR.State)
	assert.NotNil(finalPR.ClosedAt)
	assert.Equal("Test PR #1", finalPR.Title)
	assert.True(finalPR.UpdatedAt.After(staleUpdatedAt))
}

func TestAPIReadyForReviewDoesNotGetRevertedByStaleSync(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	staleUpdatedAt := time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC)
	readyUpdatedAt := staleUpdatedAt.Add(30 * time.Minute)
	syncStarted := make(chan struct{}, 1)
	releaseSync := make(chan struct{})
	mock := &mockGH{
		getPullRequestFn: func(_ context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
			syncStarted <- struct{}{}
			<-releaseSync

			id := int64(101)
			state := "open"
			title := "stale sync"
			url := "https://github.com/acme/widget/pull/1"
			author := "alice"
			draft := true
			headSHA := "abc123"
			baseSHA := "def456"
			featureRef := "feature"
			mainRef := "main"
			createdAt := gh.Timestamp{Time: staleUpdatedAt.Add(-time.Hour)}
			updatedAt := gh.Timestamp{Time: staleUpdatedAt}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				Draft:     &draft,
				User:      &gh.User{Login: &author},
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				Head:      &gh.PullRequestBranch{SHA: &headSHA, Ref: &featureRef},
				Base:      &gh.PullRequestBranch{SHA: &baseSHA, Ref: &mainRef},
			}, nil
		},
		markReadyForReviewFn: func(_ context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
			id := int64(101)
			state := "open"
			title := "ready for review"
			url := "https://github.com/acme/widget/pull/1"
			author := "alice"
			draft := false
			headSHA := "abc123"
			baseSHA := "def456"
			featureRef := "feature"
			mainRef := "main"
			createdAt := gh.Timestamp{Time: staleUpdatedAt.Add(-time.Hour)}
			updatedAt := gh.Timestamp{Time: readyUpdatedAt}
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				Draft:     &draft,
				User:      &gh.User{Login: &author},
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				Head:      &gh.PullRequestBranch{SHA: &headSHA, Ref: &featureRef},
				Base:      &gh.PullRequestBranch{SHA: &baseSHA, Ref: &mainRef},
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	client := setupTestClient(t, srv)

	repoID, err := database.UpsertRepo(t.Context(), "github.com", "acme", "widget")
	require.NoError(err)

	prID, err := database.UpsertMergeRequest(t.Context(), &db.MergeRequest{
		RepoID:          repoID,
		PlatformID:      101,
		Number:          1,
		URL:             "https://github.com/acme/widget/pull/1",
		Title:           "draft PR",
		Author:          "alice",
		State:           "open",
		IsDraft:         true,
		Body:            "",
		HeadBranch:      "feature",
		BaseBranch:      "main",
		PlatformHeadSHA: "abc123",
		PlatformBaseSHA: "def456",
		Additions:       0,
		Deletions:       0,
		CommentCount:    0,
		ReviewDecision:  "",
		CIStatus:        "",
		CreatedAt:       staleUpdatedAt.Add(-time.Hour),
		UpdatedAt:       staleUpdatedAt.Add(-time.Minute),
		LastActivityAt:  staleUpdatedAt.Add(-time.Minute),
	})
	require.NoError(err)
	require.NoError(database.EnsureKanbanState(t.Context(), prID))

	syncDone := make(chan *generated.PostReposByOwnerByNamePullsByNumberSyncResponse, 1)
	syncErr := make(chan error, 1)
	go func() {
		resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
			t.Context(), "acme", "widget", 1,
		)
		if err != nil {
			syncErr <- err
			return
		}
		syncDone <- resp
	}()

	<-syncStarted

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	readyPR, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.False(readyPR.IsDraft)
	assert.True(readyPR.UpdatedAt.Equal(readyUpdatedAt))

	close(releaseSync)

	completed := false
	select {
	case err := <-syncErr:
		require.NoError(err)
		completed = true
	case resp := <-syncDone:
		require.Equal(http.StatusOK, resp.StatusCode())
		completed = true
	case <-time.After(5 * time.Second):
	}
	require.True(completed, "timed out waiting for stale draft sync")

	finalPR, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	assert.False(finalPR.IsDraft)
	assert.Equal("ready for review", finalPR.Title)
	assert.True(finalPR.UpdatedAt.Equal(readyUpdatedAt))
}

func TestAPISyncIssueDoesNotOverwriteNewerStateChange(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	staleUpdatedAt := time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC)
	syncStarted := make(chan struct{}, 1)
	releaseSync := make(chan struct{})
	mock := &mockGH{
		getIssueFn: func(_ context.Context, owner, repo string, number int) (*gh.Issue, error) {
			syncStarted <- struct{}{}
			<-releaseSync

			id := int64(202)
			state := "open"
			title := "stale issue sync"
			url := "https://github.com/acme/widget/issues/5"
			author := "alice"
			createdAt := gh.Timestamp{Time: staleUpdatedAt.Add(-time.Hour)}
			updatedAt := gh.Timestamp{Time: staleUpdatedAt}
			return &gh.Issue{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				User:      &gh.User{Login: &author},
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedIssue(t, database, "acme", "widget", 5, "open")
	client := setupTestClient(t, srv)

	syncDone := make(chan *generated.PostReposByOwnerByNameIssuesByNumberSyncResponse, 1)
	syncErr := make(chan error, 1)
	go func() {
		resp, err := client.HTTP.PostReposByOwnerByNameIssuesByNumberSyncWithResponse(
			t.Context(), "acme", "widget", 5,
			nil,
		)
		if err != nil {
			syncErr <- err
			return
		}
		syncDone <- resp
	}()

	<-syncStarted

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		t.Context(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	closedIssue, err := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.NoError(err)
	require.Equal("closed", closedIssue.State)
	require.NotNil(closedIssue.ClosedAt)

	close(releaseSync)

	completed := false
	select {
	case err := <-syncErr:
		require.NoError(err)
		completed = true
	case resp := <-syncDone:
		require.Equal(http.StatusOK, resp.StatusCode())
		completed = true
	case <-time.After(5 * time.Second):
	}
	require.True(completed, "timed out waiting for stale issue sync")

	finalIssue, err := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.NoError(err)
	assert.Equal("closed", finalIssue.State)
	assert.NotNil(finalIssue.ClosedAt)
	assert.Equal("Test Issue", finalIssue.Title)
	assert.True(finalIssue.UpdatedAt.After(staleUpdatedAt))
}

// TestAPISyncIssueNilUpdatedAtFallsBackToCreatedAt drives the full
// HTTP handler -> syncer -> SQLite path with a GitHub response that
// has updated_at: null, and verifies last_activity_at falls back to
// created_at via the nil guard in refreshIssueTimeline. The sync_test
// unit tests cover the same logic at the syncer layer; this test
// covers the request path users actually hit in production.
func TestAPISyncIssueNilUpdatedAtFallsBackToCreatedAt(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	createdAt := time.Date(2025, 3, 14, 9, 0, 0, 0, time.UTC)
	mock := &mockGH{
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			id := int64(9999)
			state := "open"
			title := "nil updated_at"
			url := "https://github.com/acme/widget/issues/9"
			author := "alice"
			createdTs := gh.Timestamp{Time: createdAt}
			return &gh.Issue{
				ID:        &id,
				Number:    &number,
				State:     &state,
				Title:     &title,
				HTMLURL:   &url,
				User:      &gh.User{Login: &author},
				CreatedAt: &createdTs,
				UpdatedAt: nil,
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	seedIssue(t, database, "acme", "widget", 9, "open")
	client := setupTestClient(t, srv)

	// Before the nil guard, refreshIssueTimeline panicked on
	// ghIssue.UpdatedAt.Time and the handler returned 502.
	syncResp, err := client.HTTP.PostReposByOwnerByNameIssuesByNumberSyncWithResponse(
		ctx, "acme", "widget", 9,
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, syncResp.StatusCode())
	require.NotNil(syncResp.JSON200)
	// LastActivityAt must equal CreatedAt, not Go's zero time.
	// Without the fallback, activity-ordered views would sort
	// this issue at 0001-01-01 instead of its creation date.
	assert.False(syncResp.JSON200.Issue.LastActivityAt.IsZero())
	assert.Equal(createdAt, syncResp.JSON200.Issue.LastActivityAt.UTC())

	// Verify the persisted value round-trips through the read
	// endpoint so the storage -> serializer path is covered.
	getResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", 9,
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	assert.Equal(createdAt, getResp.JSON200.Issue.LastActivityAt.UTC())
}

func TestAPIListPullsStateFilter(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := t.Context()

	seedPR(t, database, "acme", "widget", 1) // open
	seedPR(t, database, "acme", "widget", 2) // will close
	seedPR(t, database, "acme", "widget", 3) // will merge

	repo, _ := database.GetRepoByOwnerName(ctx, "acme", "widget")
	now := time.Now()
	_ = database.UpdateMRState(ctx, repo.ID, 2, "closed", nil, &now)
	_ = database.UpdateMRState(ctx, repo.ID, 3, "merged", &now, &now)

	client := setupTestClient(t, srv)

	// Default (open)
	resp, err := client.HTTP.ListPullsWithResponse(ctx, nil)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.Len(*resp.JSON200, 1)

	// Closed (includes merged)
	state := "closed"
	resp, err = client.HTTP.ListPullsWithResponse(ctx, &generated.ListPullsParams{State: &state})
	require.NoError(err)
	require.Len(*resp.JSON200, 2)

	// All
	state = "all"
	resp, err = client.HTTP.ListPullsWithResponse(ctx, &generated.ListPullsParams{State: &state})
	require.NoError(err)
	require.Len(*resp.JSON200, 3)

	// Invalid
	state = "bogus"
	resp, err = client.HTTP.ListPullsWithResponse(ctx, &generated.ListPullsParams{State: &state})
	require.NoError(err)
	require.Equal(http.StatusBadRequest, resp.StatusCode())
}

func TestAPIListPullsCasefoldsRepoNames(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServerWithRepos(t, &mockGH{}, []ghclient.RepoRef{
		{Owner: "org", Name: "foo", PlatformHost: "github.com"},
	})

	seedPR(t, database, "Org", "Foo", 1)
	seedPR(t, database, "org", "foo", 1)

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.ListPullsWithResponse(t.Context(), nil)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	assert.Equal("org", (*resp.JSON200)[0].RepoOwner)
	assert.Equal("foo", (*resp.JSON200)[0].RepoName)
}

func TestAPIListIssuesStateFilter(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := t.Context()

	seedIssue(t, database, "acme", "widget", 1, "open")
	seedIssue(t, database, "acme", "widget", 2, "closed")

	client := setupTestClient(t, srv)

	// Default (open)
	resp, err := client.HTTP.ListIssuesWithResponse(ctx, nil)
	require.NoError(err)
	require.Len(*resp.JSON200, 1)

	// Closed
	state := "closed"
	resp, err = client.HTTP.ListIssuesWithResponse(ctx, &generated.ListIssuesParams{State: &state})
	require.NoError(err)
	require.Len(*resp.JSON200, 1)

	// All
	state = "all"
	resp, err = client.HTTP.ListIssuesWithResponse(ctx, &generated.ListIssuesParams{State: &state})
	require.NoError(err)
	require.Len(*resp.JSON200, 2)
}

func TestAPIListIssuesIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedIssueWithLabels(t, database, "acme", "widget", 5, "open", []db.Label{{
		Name:      "triage",
		Color:     "fbca04",
		IsDefault: false,
	}})
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.ListIssuesWithResponse(t.Context(), nil)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.NotNil((*resp.JSON200)[0].Labels)
	require.Equal([]generated.Label{{
		Name:      "triage",
		Color:     "fbca04",
		IsDefault: false,
	}}, *(*resp.JSON200)[0].Labels)
}

func TestAPIGetIssueIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	description := "Customer reported"
	seedIssueWithLabels(t, database, "acme", "widget", 5, "open", []db.Label{{
		Name:        "bug",
		Description: description,
		Color:       "d73a4a",
		IsDefault:   true,
	}})
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		t.Context(), "acme", "widget", 5,
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Issue.Labels)
	require.Equal([]generated.Label{{
		Name:        "bug",
		Description: &description,
		Color:       "d73a4a",
		IsDefault:   true,
	}}, *resp.JSON200.Issue.Labels)
}

func TestAPIGetIssueAcceptsMixedCaseRepoPath(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedIssue(t, database, "acme", "widget", 5, "open")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		t.Context(), "Acme", "Widget", 5,
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Equal("acme", resp.JSON200.RepoOwner)
	require.Equal("widget", resp.JSON200.RepoName)
}

func TestAPIListIssuesAcceptsMixedCaseRepoFilter(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedIssue(t, database, "acme", "widget", 5, "open")
	client := setupTestClient(t, srv)

	repo := "Acme/Widget"
	resp, err := client.HTTP.ListIssuesWithResponse(
		t.Context(), &generated.ListIssuesParams{Repo: &repo},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.Equal("acme", (*resp.JSON200)[0].RepoOwner)
	require.Equal("widget", (*resp.JSON200)[0].RepoName)
}

func TestAPIGetIssueUsesPlatformHostQuery(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	database, err := db.Open(
		filepath.Join(t.TempDir(), "test.db"),
	)
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	seedIssueOnHost(
		t, database,
		"github.com", "acme", "widget", 7,
		"open", "GitHub issue",
	)
	seedIssueOnHost(
		t, database,
		"ghe.example.com", "acme", "widget", 7,
		"open", "GHES issue",
	)

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{
			"github.com":      &mockGH{},
			"ghe.example.com": &mockGH{},
		},
		database,
		nil,
		[]ghclient.RepoRef{
			{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
			{Owner: "acme", Name: "widget", PlatformHost: "ghe.example.com"},
		},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(
		database, syncer, nil, "/", nil, ServerOptions{},
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/repos/acme/widget/issues/7?platform_host=ghe.example.com",
		nil,
	)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)
	var body rawIssueDetailResponse
	require.NoError(json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal("ghe.example.com", body.PlatformHost)
	if assert.NotNil(body.Issue) {
		assert.Equal("GHES issue", body.Issue.Title)
	}
}

func TestAPISyncIssueUsesPlatformHostQuery(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()

	database, err := db.Open(
		filepath.Join(t.TempDir(), "test.db"),
	)
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	seedIssueOnHost(
		t, database,
		"github.com", "acme", "widget", 7,
		"open", "GitHub stale issue",
	)
	seedIssueOnHost(
		t, database,
		"ghe.example.com", "acme", "widget", 7,
		"open", "GHES stale issue",
	)

	githubClient := &mockGH{
		getIssueFn: func(_ context.Context, owner, repo string, number int) (*gh.Issue, error) {
			title := "GitHub synced issue"
			state := "open"
			url := fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number)
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.Issue{
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				User:      &gh.User{Login: new("github-user")},
			}, nil
		},
	}
	ghesClient := &mockGH{
		getIssueFn: func(_ context.Context, owner, repo string, number int) (*gh.Issue, error) {
			title := "GHES synced issue"
			state := "open"
			url := fmt.Sprintf("https://ghe.example.com/%s/%s/issues/%d", owner, repo, number)
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.Issue{
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				User:      &gh.User{Login: new("ghes-user")},
			}, nil
		},
	}

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{
			"github.com":      githubClient,
			"ghe.example.com": ghesClient,
		},
		database,
		nil,
		[]ghclient.RepoRef{
			{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
			{Owner: "acme", Name: "widget", PlatformHost: "ghe.example.com"},
		},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(
		database, syncer, nil, "/", nil, ServerOptions{},
	)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/sync?platform_host=ghe.example.com",
		http.NoBody,
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)
	var body rawIssueDetailResponse
	require.NoError(json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal("ghe.example.com", body.PlatformHost)
	if assert.NotNil(body.Issue) {
		assert.Equal("GHES synced issue", body.Issue.Title)
	}

	githubRepo, err := database.GetRepoByHostOwnerName(
		ctx, "github.com", "acme", "widget",
	)
	require.NoError(err)
	require.NotNil(githubRepo)
	githubIssue, err := database.GetIssueByRepoIDAndNumber(
		ctx, githubRepo.ID, 7,
	)
	require.NoError(err)
	require.NotNil(githubIssue)
	assert.Equal("GitHub stale issue", githubIssue.Title)

	ghesRepo, err := database.GetRepoByHostOwnerName(
		ctx, "ghe.example.com", "acme", "widget",
	)
	require.NoError(err)
	require.NotNil(ghesRepo)
	ghesIssue, err := database.GetIssueByRepoIDAndNumber(
		ctx, ghesRepo.ID, 7,
	)
	require.NoError(err)
	require.NotNil(ghesIssue)
	assert.Equal("GHES synced issue", ghesIssue.Title)
}

func TestAPISetIssueStateUsesPlatformHostBody(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()

	database, err := db.Open(
		filepath.Join(t.TempDir(), "test.db"),
	)
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	seedIssueOnHost(
		t, database,
		"github.com", "acme", "widget", 7,
		"open", "GitHub issue",
	)
	seedIssueOnHost(
		t, database,
		"ghe.example.com", "acme", "widget", 7,
		"open", "GHES issue",
	)

	githubClient := &mockGH{
		editIssueFn: func(_ context.Context, _, _ string, number int, state string) (*gh.Issue, error) {
			url := fmt.Sprintf("https://github.com/acme/widget/issues/%d", number)
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			title := "GitHub issue"
			return &gh.Issue{
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				User:      &gh.User{Login: new("github-user")},
			}, nil
		},
	}
	ghesClient := &mockGH{
		editIssueFn: func(_ context.Context, _, _ string, number int, state string) (*gh.Issue, error) {
			url := fmt.Sprintf("https://ghe.example.com/acme/widget/issues/%d", number)
			createdAt := gh.Timestamp{Time: time.Now().UTC()}
			updatedAt := gh.Timestamp{Time: time.Now().UTC()}
			title := "GHES issue"
			return &gh.Issue{
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
				User:      &gh.User{Login: new("ghes-user")},
			}, nil
		},
	}

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{
			"github.com":      githubClient,
			"ghe.example.com": ghesClient,
		},
		database,
		nil,
		[]ghclient.RepoRef{
			{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
			{Owner: "acme", Name: "widget", PlatformHost: "ghe.example.com"},
		},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(
		database, syncer, nil, "/", nil, ServerOptions{},
	)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/github-state",
		strings.NewReader(`{"state":"closed","platform_host":"ghe.example.com"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)

	githubRepo, err := database.GetRepoByHostOwnerName(
		ctx, "github.com", "acme", "widget",
	)
	require.NoError(err)
	require.NotNil(githubRepo)
	githubIssue, err := database.GetIssueByRepoIDAndNumber(
		ctx, githubRepo.ID, 7,
	)
	require.NoError(err)
	require.NotNil(githubIssue)
	assert.Equal("open", githubIssue.State)

	ghesRepo, err := database.GetRepoByHostOwnerName(
		ctx, "ghe.example.com", "acme", "widget",
	)
	require.NoError(err)
	require.NotNil(ghesRepo)
	ghesIssue, err := database.GetIssueByRepoIDAndNumber(
		ctx, ghesRepo.ID, 7,
	)
	require.NoError(err)
	require.NotNil(ghesIssue)
	assert.Equal("closed", ghesIssue.State)
}

// TestAPIIssueDataFromGraphQLSync verifies the API correctly serves
// issue data that was persisted by the GraphQL sync path. The sync
// path itself (GraphQL fetch → normalize → DB upsert) is tested in
// internal/github/sync_test.go; this test covers the DB → API layer.
func TestAPIIssueDataFromGraphQLSync(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	mock := &mockGH{}
	srv, database := setupTestServerWithMock(t, mock)
	client := setupTestClient(t, srv)

	// Seed DB directly — same shape as GraphQL sync output.
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)

	now := time.Now().UTC().Truncate(time.Second)
	issueID, err := database.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     60000,
		Number:         60,
		URL:            "https://github.com/acme/widget/issues/60",
		Title:          "GraphQL synced issue",
		Author:         "testuser",
		State:          "open",
		Body:           "Synced via GraphQL",
		CommentCount:   1,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

	// Add a label
	require.NoError(database.ReplaceIssueLabels(ctx, repoID, issueID, []db.Label{
		{PlatformID: 1, Name: "bug", Color: "d73a4a", UpdatedAt: now},
	}))

	// Add a comment event
	require.NoError(database.UpsertIssueEvents(ctx, []db.IssueEvent{
		{
			IssueID:   issueID,
			EventType: "issue_comment",
			Author:    "commenter",
			Body:      "I can reproduce",
			CreatedAt: now,
			DedupeKey: "issue-comment-601",
		},
	}))

	// Verify via ListIssues API
	resp, err := client.HTTP.ListIssuesWithResponse(ctx, nil)
	require.NoError(err)
	require.Equal(200, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)

	apiIssue := (*resp.JSON200)[0]
	assert.Equal(int64(60), apiIssue.Number)
	assert.Equal("GraphQL synced issue", apiIssue.Title)
	assert.Equal("testuser", apiIssue.Author)
	assert.Equal("open", apiIssue.State)
	require.NotNil(apiIssue.Labels)
	require.Len(*apiIssue.Labels, 1)
	assert.Equal("bug", (*apiIssue.Labels)[0].Name)

	// Verify via GetIssue API
	detailResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", 60,
		nil,
	)
	require.NoError(err)
	require.Equal(200, detailResp.StatusCode())
	require.NotNil(detailResp.JSON200)
	assert.Equal("Synced via GraphQL", detailResp.JSON200.Issue.Body)
	assert.Equal(int64(1), detailResp.JSON200.Issue.CommentCount)
}

// TestE2EGraphQLIssueSyncThroughAPI is a full-stack test that runs the
// real GraphQL issue sync path against a mocked GraphQL HTTP backend
// with real SQLite, then verifies the resulting issue data through
// the HTTP API. Exercises: GraphQL HTTP → adapter → NormalizeIssue →
// UpsertIssue → HTTP API handler → JSON response.
func TestE2EGraphQLIssueSyncThroughAPI(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

	// Mock GraphQL backend returning a single issue with a label
	// and a comment.
	gqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		if bytes.Contains(body, []byte("pullRequests")) {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`))
			return
		}
		resp := `{"data":{"repository":{"issues":{"nodes":[{
			"databaseId":80000,
			"number":80,
			"title":"Full stack GraphQL issue",
			"state":"OPEN",
			"body":"Synced through the HTTP API",
			"url":"https://github.com/acme/widget/issues/80",
			"author":{"login":"ivy"},
			"createdAt":"` + now + `",
			"updatedAt":"` + now + `",
			"closedAt":null,
			"labels":{"nodes":[{"name":"bug","color":"d73a4a","description":"","isDefault":false}]},
			"comments":{"totalCount":1,"nodes":[{"databaseId":801,"author":{"login":"judy"},"body":"full stack comment","createdAt":"` + now + `","updatedAt":"` + now + `"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}
		}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer gqlSrv.Close()

	// REST mock: PR list returns 304 (skip PR sync), issue list
	// returns minimal data to pass the ETag gate so GraphQL runs.
	issueID := int64(80000)
	issueNumber := 80
	issueTitle := "Full stack GraphQL issue"
	issueState := "open"
	issueURL := "https://github.com/acme/widget/issues/80"
	issueLogin := "ivy"
	issueTime := gh.Timestamp{Time: time.Now().UTC().Truncate(time.Second)}
	mock := &mockGH{
		listOpenPRsErr: &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		},
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return []*gh.Issue{{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				User:      &gh.User{Login: &issueLogin},
				CreatedAt: &issueTime,
				UpdatedAt: &issueTime,
			}}, nil
		},
	}
	srv, _ := setupTestServerWithMock(t, mock)

	// Wire a real GraphQLFetcher pointing at the mock GraphQL server
	// into the syncer.
	gqlClient := githubv4.NewEnterpriseClient(gqlSrv.URL, gqlSrv.Client())
	srv.syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": ghclient.NewGraphQLFetcherWithClient(gqlClient, nil),
	})

	// Trigger the real sync pipeline.
	srv.syncer.RunOnce(ctx)

	// Verify through the HTTP API that issue data flowed end-to-end.
	client := setupTestClient(t, srv)

	listResp, err := client.HTTP.ListIssuesWithResponse(ctx, nil)
	require.NoError(err)
	require.Equal(200, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.Len(*listResp.JSON200, 1)

	apiIssue := (*listResp.JSON200)[0]
	assert.Equal(int64(80), apiIssue.Number)
	assert.Equal("Full stack GraphQL issue", apiIssue.Title)
	assert.Equal("ivy", apiIssue.Author)
	assert.Equal("open", apiIssue.State)
	require.NotNil(apiIssue.Labels)
	require.Len(*apiIssue.Labels, 1)
	assert.Equal("bug", (*apiIssue.Labels)[0].Name)

	detailResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", 80,
		nil,
	)
	require.NoError(err)
	require.Equal(200, detailResp.StatusCode())
	require.NotNil(detailResp.JSON200)
	assert.Equal("Synced through the HTTP API", detailResp.JSON200.Issue.Body)
	assert.Equal(int64(1), detailResp.JSON200.Issue.CommentCount)
}

// TestE2EGraphQLIssueSyncTrustsTotalCount pre-seeds an issue with a
// stale CommentCount, runs a real GraphQL sync with truncated
// comments (totalCount > nodes, HasNextPage=true), and forces the
// REST fallback to fail. The only remaining count in the DB is
// whatever UpsertIssue wrote from NormalizeIssue — which must be
// GraphQL's TotalCount, not the stale existing.CommentCount.
// Regression test for the "preserve existing.CommentCount" overwrite.
func TestE2EGraphQLIssueSyncTrustsTotalCount(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	nowRFC3339 := now.Format(time.RFC3339)

	// GraphQL: totalCount=42, HasNextPage=true → CommentsComplete=false.
	// REST ListIssueComments will error. Stale DB count is 5.
	// Post-sync count must be 42 (fresh GraphQL TotalCount), not 5.
	gqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		if bytes.Contains(body, []byte("pullRequests")) {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`))
			return
		}
		resp := `{"data":{"repository":{"issues":{"nodes":[{
			"databaseId":90000,
			"number":90,
			"title":"Stale count issue",
			"state":"OPEN",
			"body":"GraphQL count must win",
			"url":"https://github.com/acme/widget/issues/90",
			"author":{"login":"kate"},
			"createdAt":"` + nowRFC3339 + `",
			"updatedAt":"` + nowRFC3339 + `",
			"closedAt":null,
			"labels":{"nodes":[]},
			"comments":{"totalCount":42,"nodes":[{"databaseId":901,"author":{"login":"leo"},"body":"one","createdAt":"` + nowRFC3339 + `","updatedAt":"` + nowRFC3339 + `"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor1"}}
		}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer gqlSrv.Close()

	issueID := int64(90000)
	issueNumber := 90
	issueTitle := "Stale count issue"
	issueState := "open"
	issueURL := "https://github.com/acme/widget/issues/90"
	issueLogin := "kate"
	issueTime := gh.Timestamp{Time: now}
	mock := &mockGH{
		listOpenPRsErr: &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		},
		listIssueCommentsErr: fmt.Errorf("transient comments failure"),
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return []*gh.Issue{{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				User:      &gh.User{Login: &issueLogin},
				CreatedAt: &issueTime,
				UpdatedAt: &issueTime,
			}}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)

	// Pre-seed DB with a stale CommentCount (5). REST fallback fails,
	// so UpsertIssue's value is what survives. With the bug, it's 5.
	// Without the bug, it's TotalCount=42.
	//
	// The pre-seed UpdatedAt must be strictly older than the
	// GraphQL mock's updatedAt (`now` above). UpsertIssue's
	// stale-snapshot guard skips the update when
	// excluded.updated_at < middleman_issues.updated_at, so if
	// `stale` rolls forward past `now` (common under the race
	// detector's slower execution) the fresh GraphQL data would be
	// blocked and the assertion below would read back the stale 5
	// — a test-only flake, not a production bug.
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	stale := now.Add(-time.Second)
	_, err = database.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     90000,
		Number:         90,
		URL:            issueURL,
		Title:          issueTitle,
		Author:         issueLogin,
		State:          "open",
		CommentCount:   5, // stale
		CreatedAt:      stale,
		UpdatedAt:      stale,
		LastActivityAt: stale,
	})
	require.NoError(err)

	gqlClient := githubv4.NewEnterpriseClient(gqlSrv.URL, gqlSrv.Client())
	srv.syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": ghclient.NewGraphQLFetcherWithClient(gqlClient, nil),
	})

	srv.syncer.RunOnce(ctx)

	// API must expose GraphQL TotalCount (42), not stale DB (5).
	// With the preservation bug, count would remain 5.
	client := setupTestClient(t, srv)
	detailResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", 90,
		nil,
	)
	require.NoError(err)
	require.Equal(200, detailResp.StatusCode())
	require.NotNil(detailResp.JSON200)
	assert.Equal(int64(42), detailResp.JSON200.Issue.CommentCount)
}

func TestE2EPRDetailRefreshesEditedCommentBody(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	prNumber := 160
	prID := int64(160000)
	prTitle := "Edited comment refresh"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/160"
	headRef := "feature/edited-comment"
	headSHA := "deadbeef"
	baseRef := "main"
	commentID := int64(9001)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	commentBody := "original body"

	mock := &mockGH{
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return nil, nil
		},
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			require.Equal(prNumber, number)
			return &gh.PullRequest{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				HTMLURL:   &prURL,
				State:     &prState,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
				Head: &gh.PullRequestBranch{
					Ref: &headRef,
					SHA: &headSHA,
				},
				Base: &gh.PullRequestBranch{
					Ref: &baseRef,
				},
			}, nil
		},
	}
	prListCalls := 0
	mock.listOpenPullRequestsFn = func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
		prListCalls++
		if prListCalls == 1 {
			return []*gh.PullRequest{{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				HTMLURL:   &prURL,
				State:     &prState,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
				Head: &gh.PullRequestBranch{
					Ref: &headRef,
					SHA: &headSHA,
				},
				Base: &gh.PullRequestBranch{
					Ref: &baseRef,
				},
			}}, nil
		}
		return nil, &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		}
	}
	mockComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &commentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}
	mock.listIssueCommentsFn = func(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
		return mockComments, nil
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("original body", (*firstResp.JSON200.Events)[0].Body)

	editedBody := "edited body"
	mockComments = []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &editedBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: now.Add(4 * time.Minute)},
	}}

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.NotNil(secondResp.JSON200.Events)
	require.Len(*secondResp.JSON200.Events, 1)
	assert.Equal("edited body", (*secondResp.JSON200.Events)[0].Body)
}

func TestE2EPRDetailRemovesDeletedCommentWhenPRListIsUnchanged(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	prNumber := 160
	prID := int64(160000)
	prTitle := "Deleted comment refresh"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/160"
	headRef := "feature/deleted-comment"
	headSHA := "deadbeef"
	baseRef := "main"
	commentID := int64(9001)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	commentBody := "body to remove"

	mock := &mockGH{
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return nil, nil
		},
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			require.Equal(prNumber, number)
			return &gh.PullRequest{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				HTMLURL:   &prURL,
				State:     &prState,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
				Head: &gh.PullRequestBranch{
					Ref: &headRef,
					SHA: &headSHA,
				},
				Base: &gh.PullRequestBranch{
					Ref: &baseRef,
				},
			}, nil
		},
	}
	prListCalls := 0
	mock.listOpenPullRequestsFn = func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
		prListCalls++
		if prListCalls == 1 {
			return []*gh.PullRequest{{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				HTMLURL:   &prURL,
				State:     &prState,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
				Head: &gh.PullRequestBranch{
					Ref: &headRef,
					SHA: &headSHA,
				},
				Base: &gh.PullRequestBranch{
					Ref: &baseRef,
				},
			}}, nil
		}
		return nil, &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		}
	}
	mockComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &commentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}
	mock.listIssueCommentsFn = func(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
		return mockComments, nil
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.MergeRequest.CommentCount)
	require.Equal(commentCreatedAt.UTC(), firstResp.JSON200.MergeRequest.LastActivityAt.UTC())
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("body to remove", (*firstResp.JSON200.Events)[0].Body)

	mockComments = []*gh.IssueComment{}

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.MergeRequest.CommentCount)
	require.Equal(now.UTC(), secondResp.JSON200.MergeRequest.LastActivityAt.UTC())
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EPRDetailRemovesDeletedCommentWhenAnotherPRChanges(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
	targetNumber := 160
	targetID := int64(160000)
	targetTitle := "Target PR keeps stale comment"
	targetURL := "https://github.com/acme/widget/pull/160"
	otherNumber := 161
	otherID := int64(161000)
	otherTitle := "Other PR changes"
	otherURL := "https://github.com/acme/widget/pull/161"
	prState := "open"
	headRef := "feature/comments"
	headSHA := "deadbeef"
	baseRef := "main"
	commentID := int64(9050)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	targetCommentBody := "target comment"
	targetComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &targetCommentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}
	otherUpdatedAt := now

	prListCalls := 0
	mock := &mockGH{
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return nil, nil
		},
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			prListCalls++
			if prListCalls > 1 {
				otherUpdatedAt = now.Add(5 * time.Minute)
			}
			return []*gh.PullRequest{
				{
					ID:        &targetID,
					Number:    &targetNumber,
					Title:     &targetTitle,
					HTMLURL:   &targetURL,
					State:     &prState,
					UpdatedAt: &gh.Timestamp{Time: now},
					CreatedAt: &gh.Timestamp{Time: now},
					Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
					Base:      &gh.PullRequestBranch{Ref: &baseRef},
				},
				{
					ID:        &otherID,
					Number:    &otherNumber,
					Title:     &otherTitle,
					HTMLURL:   &otherURL,
					State:     &prState,
					UpdatedAt: &gh.Timestamp{Time: otherUpdatedAt},
					CreatedAt: &gh.Timestamp{Time: now},
					Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
					Base:      &gh.PullRequestBranch{Ref: &baseRef},
				},
			}, nil
		},
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			switch number {
			case targetNumber:
				return &gh.PullRequest{
					ID:        &targetID,
					Number:    &targetNumber,
					Title:     &targetTitle,
					HTMLURL:   &targetURL,
					State:     &prState,
					UpdatedAt: &gh.Timestamp{Time: now},
					CreatedAt: &gh.Timestamp{Time: now},
					Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
					Base:      &gh.PullRequestBranch{Ref: &baseRef},
				}, nil
			case otherNumber:
				return &gh.PullRequest{
					ID:        &otherID,
					Number:    &otherNumber,
					Title:     &otherTitle,
					HTMLURL:   &otherURL,
					State:     &prState,
					UpdatedAt: &gh.Timestamp{Time: otherUpdatedAt},
					CreatedAt: &gh.Timestamp{Time: now},
					Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
					Base:      &gh.PullRequestBranch{Ref: &baseRef},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected pull request %d", number)
			}
		},
		listIssueCommentsFn: func(_ context.Context, _, _ string, number int) ([]*gh.IssueComment, error) {
			if number == targetNumber {
				return targetComments, nil
			}
			return []*gh.IssueComment{}, nil
		},
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(targetNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.MergeRequest.CommentCount)
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("target comment", (*firstResp.JSON200.Events)[0].Body)

	targetComments = []*gh.IssueComment{}

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(targetNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.MergeRequest.CommentCount)
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EIssueDetailRefreshesEditedCommentBody(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	issueNumber := 161
	issueID := int64(161000)
	issueTitle := "Edited issue comment refresh"
	issueState := "open"
	issueURL := "https://github.com/acme/widget/issues/161"
	commentID := int64(9011)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	commentBody := "original issue body"

	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			require.Equal(issueNumber, number)
			return &gh.Issue{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
			}, nil
		},
	}
	issueListCalls := 0
	mock.listOpenIssuesFn = func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
		issueListCalls++
		if issueListCalls == 1 {
			return []*gh.Issue{{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
			}}, nil
		}
		return nil, &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		}
	}
	mockComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &commentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}
	mock.listIssueCommentsFn = func(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
		return mockComments, nil
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("original issue body", (*firstResp.JSON200.Events)[0].Body)

	editedBody := "edited issue body"
	mockComments = []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &editedBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: now.Add(4 * time.Minute)},
	}}

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.NotNil(secondResp.JSON200.Events)
	require.Len(*secondResp.JSON200.Events, 1)
	assert.Equal("edited issue body", (*secondResp.JSON200.Events)[0].Body)
}

func TestE2EIssueDetailRemovesDeletedCommentWhenIssueListIsUnchanged(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	issueNumber := 161
	issueID := int64(161000)
	issueTitle := "Deleted issue comment refresh"
	issueState := "open"
	issueURL := "https://github.com/acme/widget/issues/161"
	commentID := int64(9011)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	commentBody := "issue body to remove"

	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			require.Equal(issueNumber, number)
			return &gh.Issue{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
			}, nil
		},
	}
	issueListCalls := 0
	mock.listOpenIssuesFn = func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
		issueListCalls++
		if issueListCalls == 1 {
			return []*gh.Issue{{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				UpdatedAt: &gh.Timestamp{Time: now},
				CreatedAt: &gh.Timestamp{Time: now},
			}}, nil
		}
		return nil, &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		}
	}
	mockComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &commentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}
	mock.listIssueCommentsFn = func(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
		return mockComments, nil
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.Issue.CommentCount)
	require.Equal(commentCreatedAt.UTC(), firstResp.JSON200.Issue.LastActivityAt.UTC())
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("issue body to remove", (*firstResp.JSON200.Events)[0].Body)

	mockComments = []*gh.IssueComment{}

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.Issue.CommentCount)
	require.Equal(now.UTC(), secondResp.JSON200.Issue.LastActivityAt.UTC())
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EIssueDetailRemovesDeletedCommentWhenAnotherIssueChanges(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)
	targetNumber := 161
	targetID := int64(161000)
	targetTitle := "Target issue keeps stale comment"
	targetURL := "https://github.com/acme/widget/issues/161"
	otherNumber := 162
	otherID := int64(162000)
	otherTitle := "Other issue changes"
	otherURL := "https://github.com/acme/widget/issues/162"
	issueState := "open"
	commentID := int64(9060)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	targetCommentBody := "target issue comment"
	targetComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &targetCommentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}
	otherUpdatedAt := now

	issueListCalls := 0
	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			issueListCalls++
			if issueListCalls > 1 {
				otherUpdatedAt = now.Add(5 * time.Minute)
			}
			return []*gh.Issue{
				{
					ID:        &targetID,
					Number:    &targetNumber,
					Title:     &targetTitle,
					State:     &issueState,
					HTMLURL:   &targetURL,
					UpdatedAt: &gh.Timestamp{Time: now},
					CreatedAt: &gh.Timestamp{Time: now},
				},
				{
					ID:        &otherID,
					Number:    &otherNumber,
					Title:     &otherTitle,
					State:     &issueState,
					HTMLURL:   &otherURL,
					UpdatedAt: &gh.Timestamp{Time: otherUpdatedAt},
					CreatedAt: &gh.Timestamp{Time: now},
				},
			}, nil
		},
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			switch number {
			case targetNumber:
				return &gh.Issue{
					ID:        &targetID,
					Number:    &targetNumber,
					Title:     &targetTitle,
					State:     &issueState,
					HTMLURL:   &targetURL,
					UpdatedAt: &gh.Timestamp{Time: now},
					CreatedAt: &gh.Timestamp{Time: now},
				}, nil
			case otherNumber:
				return &gh.Issue{
					ID:        &otherID,
					Number:    &otherNumber,
					Title:     &otherTitle,
					State:     &issueState,
					HTMLURL:   &otherURL,
					UpdatedAt: &gh.Timestamp{Time: otherUpdatedAt},
					CreatedAt: &gh.Timestamp{Time: now},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected issue %d", number)
			}
		},
		listIssueCommentsFn: func(_ context.Context, _, _ string, number int) ([]*gh.IssueComment, error) {
			if number == targetNumber {
				return targetComments, nil
			}
			return []*gh.IssueComment{}, nil
		},
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(targetNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.Issue.CommentCount)
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("target issue comment", (*firstResp.JSON200.Events)[0].Body)

	targetComments = []*gh.IssueComment{}

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(targetNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.Issue.CommentCount)
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EPRDetailRemovesDeletedCommentOnFullRefresh(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	prNumber := 170
	prID := int64(170000)
	prTitle := "Full refresh deleted comment"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/170"
	headRef := "feature/full-refresh-delete"
	headSHA := "feedface"
	baseRef := "main"
	commentID := int64(9101)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	commentBody := "comment removed on full refresh"
	currentUpdatedAt := now
	currentComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &commentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}

	mock := &mockGH{
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return nil, nil
		},
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			require.Equal(prNumber, number)
			return &gh.PullRequest{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				HTMLURL:   &prURL,
				State:     &prState,
				UpdatedAt: &gh.Timestamp{Time: currentUpdatedAt},
				CreatedAt: &gh.Timestamp{Time: now},
				Head: &gh.PullRequestBranch{
					Ref: &headRef,
					SHA: &headSHA,
				},
				Base: &gh.PullRequestBranch{
					Ref: &baseRef,
				},
			}, nil
		},
		listIssueCommentsFn: func(_ context.Context, _, _ string, number int) ([]*gh.IssueComment, error) {
			require.Equal(prNumber, number)
			return currentComments, nil
		},
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	require.NoError(srv.syncer.SyncMR(ctx, "acme", "widget", prNumber))

	firstResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.MergeRequest.CommentCount)
	require.Equal(commentCreatedAt.UTC(), firstResp.JSON200.MergeRequest.LastActivityAt.UTC())
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("comment removed on full refresh", (*firstResp.JSON200.Events)[0].Body)

	currentUpdatedAt = now.Add(time.Minute)
	currentComments = []*gh.IssueComment{}

	require.NoError(srv.syncer.SyncMR(ctx, "acme", "widget", prNumber))

	secondResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.MergeRequest.CommentCount)
	require.Equal(currentUpdatedAt.UTC(), secondResp.JSON200.MergeRequest.LastActivityAt.UTC())
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EIssueDetailRemovesDeletedCommentOnFullRefresh(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	issueNumber := 171
	issueID := int64(171000)
	issueTitle := "Full refresh deleted issue comment"
	issueState := "open"
	issueURL := "https://github.com/acme/widget/issues/171"
	commentID := int64(9111)
	commentAuthor := "reviewer"
	commentCreatedAt := now.Add(2 * time.Minute)
	commentBody := "issue comment removed on full refresh"
	currentUpdatedAt := now
	currentComments := []*gh.IssueComment{{
		ID:        &commentID,
		Body:      &commentBody,
		User:      &gh.User{Login: &commentAuthor},
		CreatedAt: &gh.Timestamp{Time: commentCreatedAt},
		UpdatedAt: &gh.Timestamp{Time: commentCreatedAt},
	}}

	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			require.Equal(issueNumber, number)
			return &gh.Issue{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				UpdatedAt: &gh.Timestamp{Time: currentUpdatedAt},
				CreatedAt: &gh.Timestamp{Time: now},
			}, nil
		},
		listIssueCommentsFn: func(_ context.Context, _, _ string, number int) ([]*gh.IssueComment, error) {
			require.Equal(issueNumber, number)
			return currentComments, nil
		},
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	require.NoError(srv.syncer.SyncIssue(ctx, "acme", "widget", issueNumber))

	firstResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.Issue.CommentCount)
	require.Equal(commentCreatedAt.UTC(), firstResp.JSON200.Issue.LastActivityAt.UTC())
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("issue comment removed on full refresh", (*firstResp.JSON200.Events)[0].Body)

	currentUpdatedAt = now.Add(time.Minute)
	currentComments = []*gh.IssueComment{}

	require.NoError(srv.syncer.SyncIssue(ctx, "acme", "widget", issueNumber))

	secondResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.Issue.CommentCount)
	require.Equal(currentUpdatedAt.UTC(), secondResp.JSON200.Issue.LastActivityAt.UTC())
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EIssueDetailRemovesDeletedCommentOnGraphQLBulkSync(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 13, 11, 0, 0, 0, time.UTC)
	firstUpdatedAt := now.Format(time.RFC3339)
	secondUpdatedAt := now.Add(time.Minute).Format(time.RFC3339)
	currentUpdatedAt := firstUpdatedAt
	currentCommentJSON := `{"totalCount":1,"nodes":[{"databaseId":9122,"author":{"login":"commenter"},"body":"bulk comment removed","createdAt":"` + firstUpdatedAt + `","updatedAt":"` + firstUpdatedAt + `"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}`

	gqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if bytes.Contains(body, []byte("pullRequests")) {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`))
			return
		}
		resp := `{"data":{"repository":{"issues":{"nodes":[{
			"databaseId":171100,
			"number":172,
			"title":"Bulk deleted comment issue",
			"state":"OPEN",
			"body":"GraphQL bulk issue",
			"url":"https://github.com/acme/widget/issues/172",
			"author":{"login":"heidi"},
			"createdAt":"` + firstUpdatedAt + `",
			"updatedAt":"` + currentUpdatedAt + `",
			"closedAt":null,
			"labels":{"nodes":[]},
			"comments":` + currentCommentJSON + `
		}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer gqlSrv.Close()

	issueID := int64(171100)
	issueNumber := 172
	issueTitle := "Bulk deleted comment issue"
	issueState := "open"
	issueURL := "https://github.com/acme/widget/issues/172"
	issueAuthor := "heidi"
	issueTime := gh.Timestamp{Time: now}
	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return []*gh.Issue{{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				User:      &gh.User{Login: &issueAuthor},
				CreatedAt: &issueTime,
				UpdatedAt: &issueTime,
			}}, nil
		},
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database,
		nil,
		defaultTestRepos,
		time.Minute,
		nil,
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(10000)},
	)
	t.Cleanup(syncer.Stop)

	gqlClient := githubv4.NewEnterpriseClient(gqlSrv.URL, gqlSrv.Client())
	syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": ghclient.NewGraphQLFetcherWithClient(gqlClient, nil),
	})

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.Issue.CommentCount)
	require.Equal(now.UTC(), firstResp.JSON200.Issue.LastActivityAt.UTC())
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("bulk comment removed", (*firstResp.JSON200.Events)[0].Body)

	currentUpdatedAt = secondUpdatedAt
	currentCommentJSON = `{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}`

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", int64(issueNumber),
		nil,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.Issue.CommentCount)
	require.Equal(now.Add(time.Minute).UTC(), secondResp.JSON200.Issue.LastActivityAt.UTC())
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EPRDetailRemovesDeletedCommentOnGraphQLBulkSync(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()

	now := time.Date(2026, 4, 13, 11, 30, 0, 0, time.UTC)
	firstUpdatedAt := now.Format(time.RFC3339)
	secondUpdatedAt := now.Add(time.Minute).Format(time.RFC3339)
	commentCreatedAt := now.Add(2 * time.Minute).Format(time.RFC3339)
	currentUpdatedAt := firstUpdatedAt
	currentCommentsJSON := `{"nodes":[{"databaseId":9222,"author":{"login":"commenter"},"body":"bulk PR comment removed","createdAt":"` + commentCreatedAt + `","updatedAt":"` + commentCreatedAt + `"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}`

	gqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if bytes.Contains(body, []byte("pullRequests")) {
			resp := `{"data":{"repository":{"pullRequests":{"nodes":[{
				"databaseId":172100,
				"number":173,
				"title":"Bulk deleted comment PR",
				"state":"OPEN",
				"isDraft":false,
				"body":"GraphQL bulk PR",
				"url":"https://github.com/acme/widget/pull/173",
				"author":{"login":"heidi"},
				"createdAt":"` + firstUpdatedAt + `",
				"updatedAt":"` + currentUpdatedAt + `",
				"mergedAt":null,
				"closedAt":null,
				"additions":1,
				"deletions":0,
				"mergeable":"MERGEABLE",
				"reviewDecision":"",
				"headRefName":"feature/bulk-pr",
				"baseRefName":"main",
				"headRefOid":"deadbeef",
				"baseRefOid":"feedface",
				"headRepository":{"url":"https://github.com/acme/widget"},
				"labels":{"nodes":[]},
				"comments":` + currentCommentsJSON + `,
				"reviews":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"review-cursor"}},
				"allCommits":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"commit-cursor"}},
				"lastCommit":{"nodes":[]}
			}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
			_, _ = w.Write([]byte(resp))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`))
	}))
	defer gqlSrv.Close()

	prID := int64(172100)
	prNumber := 173
	prTitle := "Bulk deleted comment PR"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/173"
	headRef := "feature/bulk-pr"
	headSHA := "deadbeef"
	baseRef := "main"
	prTime := gh.Timestamp{Time: now}
	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			updatedAt, parseErr := time.Parse(time.RFC3339, currentUpdatedAt)
			require.NoError(parseErr)
			updatedStamp := gh.Timestamp{Time: updatedAt}
			return []*gh.PullRequest{{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				State:     &prState,
				HTMLURL:   &prURL,
				User:      &gh.User{Login: new("heidi")},
				CreatedAt: &prTime,
				UpdatedAt: &updatedStamp,
				Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
				Base:      &gh.PullRequestBranch{Ref: &baseRef},
			}}, nil
		},
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
	}

	srv, _ := setupTestServerWithMock(t, mock)
	gqlClient := githubv4.NewEnterpriseClient(gqlSrv.URL, gqlSrv.Client())
	srv.syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": ghclient.NewGraphQLFetcherWithClient(gqlClient, nil),
	})
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	firstResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	require.Equal(int64(1), firstResp.JSON200.MergeRequest.CommentCount)
	require.Equal(now.Add(2*time.Minute).UTC(), firstResp.JSON200.MergeRequest.LastActivityAt.UTC())
	require.NotNil(firstResp.JSON200.Events)
	require.Len(*firstResp.JSON200.Events, 1)
	assert.Equal("bulk PR comment removed", (*firstResp.JSON200.Events)[0].Body)

	currentUpdatedAt = secondUpdatedAt
	currentCommentsJSON = `{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}`

	srv.syncer.RunOnce(ctx)

	secondResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	require.Equal(int64(0), secondResp.JSON200.MergeRequest.CommentCount)
	require.Equal(now.Add(time.Minute).UTC(), secondResp.JSON200.MergeRequest.LastActivityAt.UTC())
	require.NotNil(secondResp.JSON200.Events)
	require.Empty(*secondResp.JSON200.Events)
}

func TestE2EGraphQLBulkSyncKeepsNewestCICheckBySuiteCreatedAt(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()

	older := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	newer := older.Add(10 * time.Minute)
	prUpdatedAt := newer.Add(time.Minute)
	prID := int64(173100)
	prNumber := 174
	prTitle := "GraphQL check dedupe"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/174"
	prAuthor := "alice"
	headRef := "feature/graphql-check-dedupe"
	baseRef := "main"
	headSHA := "abc123"
	baseSHA := "def456"
	checkName := "build"
	oldCheckURL := "https://ci.example.com/runs/old"
	newCheckURL := "https://ci.example.com/runs/new"

	gqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if bytes.Contains(body, []byte("pullRequests")) {
			resp := `{"data":{"repository":{"pullRequests":{"nodes":[{
				"databaseId":173100,
				"number":174,
				"title":"GraphQL check dedupe",
				"state":"OPEN",
				"isDraft":false,
				"body":"",
				"url":"https://github.com/acme/widget/pull/174",
				"author":{"login":"alice"},
				"createdAt":"` + older.Format(time.RFC3339) + `",
				"updatedAt":"` + prUpdatedAt.Format(time.RFC3339) + `",
				"mergedAt":null,
				"closedAt":null,
				"additions":1,
				"deletions":0,
				"mergeable":"MERGEABLE",
				"reviewDecision":"",
				"headRefName":"feature/graphql-check-dedupe",
				"baseRefName":"main",
				"headRefOid":"abc123",
				"baseRefOid":"def456",
				"headRepository":{"url":"https://github.com/acme/widget"},
				"labels":{"nodes":[]},
				"comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}},
				"reviews":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}},
				"allCommits":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}},
				"lastCommit":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
					{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://ci.example.com/runs/old","startedAt":null,"completedAt":null,"checkSuite":{"createdAt":"` + older.Format(time.RFC3339) + `","app":{"name":"GitHub Actions"}}},
					{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://ci.example.com/runs/new","startedAt":null,"completedAt":null,"checkSuite":{"createdAt":"` + newer.Format(time.RFC3339) + `","app":{"name":"GitHub Actions"}}}
				],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}]}
			}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
			_, _ = w.Write([]byte(resp))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`))
	}))
	defer gqlSrv.Close()

	prTime := gh.Timestamp{Time: prUpdatedAt}
	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return []*gh.PullRequest{{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				State:     &prState,
				HTMLURL:   &prURL,
				User:      &gh.User{Login: &prAuthor},
				CreatedAt: &gh.Timestamp{Time: older},
				UpdatedAt: &prTime,
				Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &headSHA},
				Base:      &gh.PullRequestBranch{Ref: &baseRef, SHA: &baseSHA},
			}}, nil
		},
		listOpenIssuesFn: func(_ context.Context, _, _ string) ([]*gh.Issue, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusNotModified},
			}
		},
	}

	srv, _ := setupTestServerWithMock(t, mock)
	gqlClient := githubv4.NewEnterpriseClient(gqlSrv.URL, gqlSrv.Client())
	srv.syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": ghclient.NewGraphQLFetcherWithClient(gqlClient, nil),
	})
	client := setupTestClient(t, srv)

	srv.syncer.RunOnce(ctx)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.MergeRequest)
	require.Equal("success", resp.JSON200.MergeRequest.CIStatus)

	var checks []db.CICheck
	require.NoError(json.Unmarshal(
		[]byte(resp.JSON200.MergeRequest.CIChecksJSON),
		&checks,
	))
	require.Len(checks, 1)
	assert.Equal(checkName, checks[0].Name)
	assert.Equal("completed", checks[0].Status)
	assert.Equal("success", checks[0].Conclusion)
	assert.Equal(newCheckURL, checks[0].URL)
	assert.Equal("GitHub Actions", checks[0].App)
	assert.NotEqual(oldCheckURL, checks[0].URL)
}

func make422Error() error {
	return &gh.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusUnprocessableEntity},
		Message:  "Validation Failed",
	}
}

func TestAPISetIssueGitHubStateReturns404WhenNoClientConfigured(t *testing.T) {
	require := require.New(t)
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "widget", PlatformHost: "ghe.corp.com"}}
	srv, database := setupTestServerWithRepos(t, &mockGH{}, repos)
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, "ghe.corp.com", "acme", "widget")
	require.NoError(err)
	_, err = database.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     5000,
		Number:         5,
		URL:            "https://ghe.corp.com/acme/widget/issues/5",
		Title:          "Issue",
		Author:         "u",
		State:          "open",
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
		UpdatedAt:      time.Now().UTC().Truncate(time.Second),
		LastActivityAt: time.Now().UTC().Truncate(time.Second),
	})
	require.NoError(err)

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		ctx, "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusNotFound, resp.StatusCode())
}

func TestAPIClosePR422NilFallbackPayloadDoesNotCorruptDB(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		editPullRequestFn: func(_ context.Context, _, _ string, _ int, _ ghclient.EditPullRequestOpts) (*gh.PullRequest, error) {
			return nil, make422Error()
		},
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return nil, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	before, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(before)

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())

	after, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(after)
	assert.Equal(before.State, after.State)
	assert.Equal(before.UpdatedAt, after.UpdatedAt)
	assert.Nil(after.ClosedAt)
}

func TestAPICloseIssue422NilFallbackPayloadDoesNotCorruptDB(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	mock := &mockGH{
		editIssueFn: func(_ context.Context, _, _ string, _ int, _ string) (*gh.Issue, error) {
			return nil, make422Error()
		},
		getIssueFn: func(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
			return nil, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedIssue(t, database, "acme", "widget", 5, "open")
	before, err := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.NoError(err)
	require.NotNil(before)

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		t.Context(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())

	after, err := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.NoError(err)
	require.NotNil(after)
	assert.Equal(before.State, after.State)
	assert.Equal(before.UpdatedAt, after.UpdatedAt)
	assert.Nil(after.ClosedAt)
}

func TestAPIClosePR422AlreadyClosed(t *testing.T) {
	require := require.New(t)
	// EditPullRequest returns 422, but re-fetch shows PR is already closed.
	// Should succeed since the requested state matches.
	state := "closed"
	mock := &mockGH{
		editPullRequestFn: func(_ context.Context, _, _ string, _ int, _ ghclient.EditPullRequestOpts) (*gh.PullRequest, error) {
			return nil, make422Error()
		},
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			id := int64(1000)
			now := gh.Timestamp{Time: time.Now().UTC()}
			closedAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.PullRequest{
				ID: &id, Number: new(1), State: &state,
				Title: new("PR"), HTMLURL: new("https://example.com"),
				User:      &gh.User{Login: new("u")},
				Head:      &gh.PullRequestBranch{Ref: new("f")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
				CreatedAt: &now, UpdatedAt: &now, ClosedAt: &closedAt,
			}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, _ := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.Equal("closed", pr.State)
}

// When MarkPullRequestReadyForReview returns (nil, nil) the handler
// must return 502 rather than claiming success with no PR payload.
func TestAPIReadyForReview502OnNilPR(t *testing.T) {
	require := require.New(t)
	mock := &mockGH{
		markReadyForReviewFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return nil, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
}

func TestAPIReadyForReviewReturnsUnderlyingErrorDetail(t *testing.T) {
	require := require.New(t)
	mock := &mockGH{
		markReadyForReviewFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return nil, errors.New("marking acme/widget#1 ready for review: draft review threads still pending")
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
	require.NotNil(resp.ApplicationproblemJSONDefault)
	require.NotNil(resp.ApplicationproblemJSONDefault.Detail)
	require.Equal(
		"marking acme/widget#1 ready for review: draft review threads still pending",
		*resp.ApplicationproblemJSONDefault.Detail,
	)
}

func TestAPIReadyForReviewStaleStateRefreshesAndReturnsSuccess(t *testing.T) {
	require := require.New(t)

	staleErr := &staleReadyForReviewError{
		err: errors.New(
			"marking acme/widget#1 ready for review: graphql errors: Could not resolve to a PullRequest with the global id of 'PR_kwDOAAABc84'.",
		),
	}
	mock := &mockGH{
		markReadyForReviewFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return nil, staleErr
		},
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(1001)
			title := "Already ready"
			state := "open"
			url := "https://github.com/acme/widget/pull/1"
			author := "octocat"
			draft := false
			now := gh.Timestamp{Time: time.Now().UTC()}
			headSHA := "abc123"
			baseSHA := "def456"
			featureRef := "feature"
			mainRef := "main"
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				Draft:     &draft,
				CreatedAt: &now,
				UpdatedAt: &now,
				User:      &gh.User{Login: &author},
				Head:      &gh.PullRequestBranch{SHA: &headSHA, Ref: &featureRef},
				Base:      &gh.PullRequestBranch{SHA: &baseSHA, Ref: &mainRef},
			}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	pr.IsDraft = true
	_, err = database.UpsertMergeRequest(t.Context(), pr)
	require.NoError(err)

	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err = database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	require.False(pr.IsDraft)
}

func TestAPIReadyForReview404RefreshesStaleDraftState(t *testing.T) {
	require := require.New(t)
	notFound := &gh.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found"},
		Message:  "Not Found",
	}
	mock := &mockGH{
		markReadyForReviewFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			return nil, fmt.Errorf("marking acme/widget#1 ready for review: %w", notFound)
		},
		getPullRequestFn: func(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
			id := int64(1001)
			title := "Already ready"
			state := "open"
			url := "https://github.com/acme/widget/pull/1"
			author := "octocat"
			draft := false
			now := gh.Timestamp{Time: time.Now().UTC()}
			headSHA := "abc123"
			baseSHA := "def456"
			featureRef := "feature"
			mainRef := "main"
			return &gh.PullRequest{
				ID:        &id,
				Number:    &number,
				Title:     &title,
				State:     &state,
				HTMLURL:   &url,
				Draft:     &draft,
				CreatedAt: &now,
				UpdatedAt: &now,
				User:      &gh.User{Login: &author},
				Head:      &gh.PullRequestBranch{SHA: &headSHA, Ref: &featureRef},
				Base:      &gh.PullRequestBranch{SHA: &baseSHA, Ref: &mainRef},
			}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(t.Context(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	require.False(pr.IsDraft)
}

func TestAPIClosePR422Merged(t *testing.T) {
	// EditPullRequest returns 422, re-fetch shows PR is merged.
	// Should return 409.
	merged := "closed"
	mock := &mockGH{
		editPullRequestFn: func(_ context.Context, _, _ string, _ int, _ ghclient.EditPullRequestOpts) (*gh.PullRequest, error) {
			return nil, make422Error()
		},
		getPullRequestFn: func(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
			id := int64(1000)
			now := gh.Timestamp{Time: time.Now().UTC()}
			mergedBool := true
			return &gh.PullRequest{
				ID: &id, Number: new(1), State: &merged, Merged: &mergedBool,
				Title: new("PR"), HTMLURL: new("https://example.com"),
				User:      &gh.User{Login: new("u")},
				Head:      &gh.PullRequestBranch{Ref: new("f")},
				Base:      &gh.PullRequestBranch{Ref: new("main")},
				CreatedAt: &now, UpdatedAt: &now,
			}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		t.Context(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, resp.StatusCode())
}

func TestResolveItem_PR(t *testing.T) {
	require := require.New(t)
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "widget"}}
	srv, database := setupTestServerWithRepos(t, &mockGH{}, repos)
	seedPR(t, database, "acme", "widget", 42)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNameItemsByNumberResolveWithResponse(
		t.Context(), "acme", "widget", 42,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Equal("pr", resp.JSON200.ItemType)
	require.EqualValues(42, resp.JSON200.Number)
	require.True(resp.JSON200.RepoTracked)
}

func TestResolveItem_Issue(t *testing.T) {
	require := require.New(t)
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "widget"}}
	srv, database := setupTestServerWithRepos(t, &mockGH{}, repos)
	seedIssue(t, database, "acme", "widget", 7, "open")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNameItemsByNumberResolveWithResponse(
		t.Context(), "acme", "widget", 7,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Equal("issue", resp.JSON200.ItemType)
	require.EqualValues(7, resp.JSON200.Number)
	require.True(resp.JSON200.RepoTracked)
}

func TestResolveItem_UntrackedRepo(t *testing.T) {
	require := require.New(t)
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNameItemsByNumberResolveWithResponse(
		t.Context(), "unknown", "repo", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.False(resp.JSON200.RepoTracked)
	require.EqualValues(1, resp.JSON200.Number)
	require.Empty(resp.JSON200.ItemType)
}

func TestResolveItem_NotFoundOnGitHub(t *testing.T) {
	require := require.New(t)
	mock := &mockGH{
		getIssueFn: func(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 404},
				Message:  "Not Found",
			}
		},
	}
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "widget"}}
	srv, _ := setupTestServerWithRepos(t, mock, repos)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNameItemsByNumberResolveWithResponse(
		t.Context(), "acme", "widget", 999,
	)
	require.NoError(err)
	require.Equal(http.StatusNotFound, resp.StatusCode())
}

func TestResolveItem_GitHubServerError(t *testing.T) {
	require := require.New(t)
	mock := &mockGH{
		getIssueFn: func(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 500},
				Message:  "Internal Server Error",
			}
		},
	}
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "widget"}}
	srv, _ := setupTestServerWithRepos(t, mock, repos)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNameItemsByNumberResolveWithResponse(
		t.Context(), "acme", "widget", 999,
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
}

func TestAPICloseIssue422AlreadyClosed(t *testing.T) {
	require := require.New(t)
	state := "closed"
	mock := &mockGH{
		editIssueFn: func(_ context.Context, _, _ string, _ int, _ string) (*gh.Issue, error) {
			return nil, make422Error()
		},
		getIssueFn: func(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
			id := int64(5000)
			now := gh.Timestamp{Time: time.Now().UTC()}
			closedAt := gh.Timestamp{Time: time.Now().UTC()}
			return &gh.Issue{
				ID: &id, Number: new(5), State: &state,
				Title: new("Issue"), HTMLURL: new("https://example.com"),
				User:      &gh.User{Login: new("u")},
				CreatedAt: &now, UpdatedAt: &now, ClosedAt: &closedAt,
			}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	seedIssue(t, database, "acme", "widget", 5, "open")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		t.Context(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	issue, _ := database.GetIssue(t.Context(), "acme", "widget", 5)
	require.Equal("closed", issue.State)
}

func TestAPIGetMRImportMetadata(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)

	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.MergeRequest{
		RepoID:           repoID,
		PlatformID:       42000,
		Number:           42,
		URL:              "https://github.com/acme/widget/pull/42",
		Title:            "Add feature X",
		Author:           "octocat",
		State:            "open",
		IsDraft:          true,
		Body:             "body",
		HeadBranch:       "feature-x",
		BaseBranch:       "main",
		PlatformHeadSHA:  "abc123def456",
		HeadRepoCloneURL: "https://github.com/fork/widget.git",
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActivityAt:   now,
	}
	prID, err := database.UpsertMergeRequest(ctx, pr)
	require.NoError(err)
	require.NoError(database.EnsureKanbanState(ctx, prID))

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/repos/acme/widget/pulls/42/import-metadata", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(body, `"number":42`)
	require.Contains(body, `"head_branch":"feature-x"`)
	require.Contains(body, `"platform_head_sha":"abc123def456"`)
	require.Contains(body, `"head_repo_clone_url":"https://github.com/fork/widget.git"`)
	require.Contains(body, `"state":"open"`)
	require.Contains(body, `"is_draft":true`)
	require.Contains(body, `"title":"Add feature X"`)
}

func TestAPIGetMRImportMetadataNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/repos/acme/widget/pulls/999/import-metadata", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestOpenAPIDocumentsCustomStatusCodes(t *testing.T) {
	require := require.New(t)
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code, rr.Body.String())

	spec := rr.Body.String()
	require.Contains(spec, `"/sync":{"post":{"operationId":"trigger-sync"`)
	require.Contains(spec, `"/starred":{"delete":{"operationId":"unset-starred"`)
	require.Contains(spec, `"/repos/{owner}/{name}/pulls/{number}/comments":{"post":{"operationId":"post-pr-comment"`)
	require.Contains(spec, `"trigger-sync","responses":{"202":{"description":"Accepted"}`)
	require.Contains(spec, `"set-starred","requestBody"`)
	require.Contains(spec, `"responses":{"200":{"description":"OK"}`)
	require.True(
		strings.Contains(spec, `"operationId":"post-pr-comment","parameters"`) ||
			strings.Contains(spec, `"operationId":"post-pr-comment","requestBody"`),
		"expected post-pr-comment operation to be present",
	)
	require.Contains(spec, `"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/MREvent"}}},"description":"Created"}`)
	require.Contains(spec, `"operationId":"post-issue-comment"`)
	require.Contains(spec, `"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/IssueEvent"}}},"description":"Created"}`)
}

func TestMRListIncludesWorktreeLinks(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	prID := seedPR(t, database, "acme", "widget", 1)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(database.SetWorktreeLinks(
		t.Context(),
		[]db.WorktreeLink{
			{
				MergeRequestID: prID,
				WorktreeKey:    "wt-abc",
				WorktreePath:   "/tmp/wt",
				WorktreeBranch: "feature",
				LinkedAt:       now,
			},
		}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pulls", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(body, `"worktree_links"`)
	require.Contains(body, `"worktree_key":"wt-abc"`)
	require.Contains(body, `"worktree_path":"/tmp/wt"`)
	require.Contains(body, `"worktree_branch":"feature"`)
}

func TestMRDetailIncludesWorktreeLinks(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	prID := seedPR(t, database, "acme", "widget", 1)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(database.SetWorktreeLinks(
		t.Context(),
		[]db.WorktreeLink{
			{
				MergeRequestID: prID,
				WorktreeKey:    "wt-detail",
				WorktreePath:   "/tmp/detail",
				LinkedAt:       now,
			},
		}))

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/repos/acme/widget/pulls/1", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(body, `"worktree_links"`)
	require.Contains(body, `"worktree_key":"wt-detail"`)
}

func TestMRListEmptyLinksWhenNone(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pulls", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusOK, rr.Code)
	body := rr.Body.String()
	// Should contain an empty array, not null.
	require.Contains(body, `"worktree_links":[]`)
}

func TestAPIGetFiles503WhenCloneManagerNil(t *testing.T) {
	require := require.New(t)

	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberFilesWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusServiceUnavailable, resp.StatusCode())
}

func TestSetActiveWorktreeKey(t *testing.T) {
	assert := Assert.New(t)
	srv, _ := setupTestServer(t)

	key, set := srv.ActiveWorktreeKey()
	assert.Empty(key)
	assert.False(set)

	srv.SetActiveWorktreeKey("wt-abc")
	key, set = srv.ActiveWorktreeKey()
	assert.Equal("wt-abc", key)
	assert.True(set)

	srv.SetActiveWorktreeKey("")
	key, set = srv.ActiveWorktreeKey()
	assert.Empty(key)
	assert.True(set, "should still be 'set' even when cleared")
}

func TestAPIRateLimits(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	rt := ghclient.NewRateTracker(database, "github.com", "rest")

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": rt},
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	gh, ok := body.Hosts["github.com"]
	assert.True(ok)
	assert.Equal(0, gh.RequestsHour)
	assert.Equal(-1, gh.RateRemaining)
	assert.False(gh.Known)
	assert.Equal(1, gh.SyncThrottleFactor)
	assert.False(gh.SyncPaused)
	assert.Equal(200, gh.ReserveBuffer)
	// Budget fields default to zero when budgetPerHour=0.
	assert.Equal(0, gh.BudgetLimit)
	assert.Equal(0, gh.BudgetSpent)
	assert.Equal(0, gh.BudgetRemaining)
}

func TestAPISyncPRIncrementsRequestCount(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	rt := ghclient.NewRateTracker(database, "github.com", "rest")

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": rt},
		nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Before any requests: requests_hour should be 0.
	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var before rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&before)
	require.NoError(err)

	gh0, ok := before.Hosts["github.com"]
	assert.True(ok)
	assert.Equal(0, gh0.RequestsHour)

	// Simulate 5 API calls via RecordRequest.
	for range 5 {
		rt.RecordRequest()
	}

	// After recording: requests_hour should be 5.
	resp2, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(err)
	defer resp2.Body.Close()
	assert.Equal(200, resp2.StatusCode)

	var after rateLimitsResponse
	err = json.NewDecoder(resp2.Body).Decode(&after)
	require.NoError(err)

	gh5, ok := after.Hosts["github.com"]
	assert.True(ok)
	assert.Equal(5, gh5.RequestsHour)
}

func TestAPIRateLimitsWithBudget(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	rt := ghclient.NewRateTracker(database, "github.com", "rest")

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": rt},
		map[string]*ghclient.SyncBudget{"github.com": ghclient.NewSyncBudget(500)},
	)
	t.Cleanup(syncer.Stop)

	// Simulate some budget spend.
	budgets := syncer.Budgets()
	budgets["github.com"].Spend(42)

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	gh, ok := body.Hosts["github.com"]
	assert.True(ok)
	assert.Equal(500, gh.BudgetLimit)
	assert.Equal(42, gh.BudgetSpent)
	assert.Equal(458, gh.BudgetRemaining)
}

func TestAPIRateLimitsWithGQL(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	restRT := ghclient.NewRateTracker(database, "github.com", "rest")
	gqlRT := ghclient.NewRateTracker(database, "github.com", "graphql")

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": restRT},
		nil,
	)

	fetcher := ghclient.NewGraphQLFetcher("token", "github.com", gqlRT, nil)
	syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": fetcher,
	})

	// Simulate GraphQL rate data.
	gqlRT.UpdateFromRate(gh.Rate{
		Limit:     5000,
		Remaining: 4800,
		Reset:     gh.Timestamp{Time: time.Now().Add(30 * time.Minute)},
	})

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	host, ok := body.Hosts["github.com"]
	assert.True(ok)

	// GQL fields should be populated.
	assert.Equal(4800, host.GQLRemaining)
	assert.Equal(5000, host.GQLLimit)
	assert.True(host.GQLKnown)
	assert.NotEmpty(host.GQLResetAt)
}

func TestAPIRateLimitsGQLDefaultsUnknown(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	rt := ghclient.NewRateTracker(database, "github.com", "rest")
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": rt},
		nil,
	)
	// No SetFetchers call — GQL data should be unknown.

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	host := body.Hosts["github.com"]
	assert.Equal(-1, host.GQLRemaining)
	assert.Equal(-1, host.GQLLimit)
	assert.False(host.GQLKnown)
	assert.Empty(host.GQLResetAt)
}

func TestAPIRateLimitsMultiHostMixed(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Two hosts: github.com has GQL data, ghe.example.com does not.
	ghRT := ghclient.NewRateTracker(database, "github.com", "rest")
	gheRT := ghclient.NewRateTracker(database, "ghe.example.com", "rest")
	gqlRT := ghclient.NewRateTracker(database, "github.com", "graphql")
	gqlRT.UpdateFromRate(gh.Rate{
		Limit:     5000,
		Remaining: 4500,
		Reset:     gh.Timestamp{Time: time.Now().Add(30 * time.Minute)},
	})

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{
			"github.com":      &mockGH{},
			"ghe.example.com": &mockGH{},
		},
		database, nil,
		[]ghclient.RepoRef{
			{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
			{Owner: "corp", Name: "internal", PlatformHost: "ghe.example.com"},
		},
		time.Minute,
		map[string]*ghclient.RateTracker{
			"github.com":      ghRT,
			"ghe.example.com": gheRT,
		},
		nil,
	)

	fetcher := ghclient.NewGraphQLFetcher("token", "github.com", gqlRT, nil)
	syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": fetcher,
	})

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	// Both hosts present.
	assert.Len(body.Hosts, 2)

	// github.com has GQL data.
	ghHost := body.Hosts["github.com"]
	assert.True(ghHost.GQLKnown)
	assert.Equal(4500, ghHost.GQLRemaining)
	assert.Equal(5000, ghHost.GQLLimit)

	// ghe.example.com has no GQL fetcher — defaults to unknown.
	gheHost := body.Hosts["ghe.example.com"]
	assert.Equal(-1, gheHost.GQLRemaining)
	assert.Equal(-1, gheHost.GQLLimit)
	assert.False(gheHost.GQLKnown)
}

func TestAPIGetPullDetailLoaded(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	// Before detail fetch: detail_loaded=false.
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.False(resp.JSON200.DetailLoaded)
	assert.Nil(resp.JSON200.DetailFetchedAt)

	// Insert a second PR with DetailFetchedAt set.
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	_, err = database.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:          repoID,
		PlatformID:      2000,
		Number:          2,
		URL:             "https://github.com/acme/widget/pull/2",
		Title:           "PR with detail",
		Author:          "testuser",
		State:           "open",
		HeadBranch:      "feature",
		BaseBranch:      "main",
		CreatedAt:       now,
		UpdatedAt:       now,
		LastActivityAt:  now,
		DetailFetchedAt: &now,
	})
	require.NoError(err)

	resp2, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		t.Context(), "acme", "widget", 2,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp2.StatusCode())
	require.NotNil(resp2.JSON200)
	assert.True(resp2.JSON200.DetailLoaded)
	require.NotNil(resp2.JSON200.DetailFetchedAt)
	assertRFC3339UTC(t, *resp2.JSON200.DetailFetchedAt, now)
}

func TestAPIActivityReturnsUTCCreatedAt(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)
	prID := seedPR(t, database, "acme", "widget", 1)
	ctx := t.Context()
	createdAtUTC := time.Now().UTC().Add(-2 * time.Hour).Round(time.Second)
	//nolint:forbidigo // Test fixture intentionally uses a non-UTC timestamp to verify UTC normalization.
	createdAt := createdAtUTC.In(time.FixedZone("EDT", -4*60*60))

	require.NoError(database.UpsertMREvents(ctx, []db.MREvent{{
		MergeRequestID: prID,
		EventType:      "issue_comment",
		Author:         "reviewer",
		Body:           "Looks good",
		CreatedAt:      createdAt,
		DedupeKey:      "comment-utc-created-at",
	}}))

	since := createdAtUTC.Add(-time.Hour).Format(time.RFC3339)
	resp, err := client.HTTP.GetActivityWithResponse(
		ctx, &generated.GetActivityParams{Since: &since},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Items)
	require.NotEmpty(*resp.JSON200.Items)

	var commentItem *generated.ActivityItemResponse
	for i := range *resp.JSON200.Items {
		item := &(*resp.JSON200.Items)[i]
		if item.Author == "reviewer" && item.ActivityType == "comment" {
			commentItem = item
			break
		}
	}
	require.NotNil(commentItem)
	assertRFC3339UTC(t, commentItem.CreatedAt, createdAt)
	assert.Equal("reviewer", commentItem.Author)
	assert.Equal("comment", commentItem.ActivityType)
}

func TestAPIActivityStartupRepairsLegacyTimestampStorage(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	database, err := db.Open(path)
	require.NoError(err)

	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	prID, err := database.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            repoID,
		PlatformID:        101,
		Number:            1,
		URL:               "https://github.com/acme/widget/pull/1",
		Title:             "Legacy PR",
		Author:            "octocat",
		AuthorDisplayName: "octocat",
		State:             "open",
		HeadBranch:        "feature",
		BaseBranch:        "main",
		CreatedAt:         time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		LastActivityAt:    time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(err)
	issueID, err := database.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     201,
		Number:         2,
		URL:            "https://github.com/acme/widget/issues/2",
		Title:          "Legacy issue",
		Author:         "octocat",
		State:          "open",
		CreatedAt:      time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(err)
	require.NoError(database.UpsertMREvents(ctx, []db.MREvent{{
		MergeRequestID: prID,
		EventType:      "issue_comment",
		Author:         "pr-reviewer",
		Body:           "PR comment",
		CreatedAt:      time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		DedupeKey:      "comment-pr-legacy",
	}}))
	require.NoError(database.UpsertIssueEvents(ctx, []db.IssueEvent{{
		IssueID:   issueID,
		EventType: "issue_comment",
		Author:    "issue-reporter",
		Body:      "Issue comment",
		CreatedAt: time.Date(2026, 4, 11, 13, 0, 0, 0, time.UTC),
		DedupeKey: "comment-issue-legacy",
	}}))
	require.NoError(database.Close())

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.ExecContext(ctx,
		`UPDATE middleman_mr_events SET created_at = ? WHERE dedupe_key = ?`,
		"2026-04-11 08:00:00 -0400 EDT",
		"comment-pr-legacy",
	)
	require.NoError(err)
	_, err = raw.ExecContext(ctx,
		`UPDATE middleman_issue_events SET created_at = ? WHERE dedupe_key = ?`,
		"2026-04-11 09:00:00 -0400 EDT",
		"comment-issue-legacy",
	)
	require.NoError(err)
	_, err = raw.ExecContext(ctx, `
		DROP TRIGGER IF EXISTS middleman_workspaces_casefold_update;
		DROP TRIGGER IF EXISTS middleman_workspaces_casefold_insert;

		DROP INDEX IF EXISTS middleman_workspace_setup_events_workspace_id_idx;
		DROP TABLE IF EXISTS middleman_workspace_setup_events;

		ALTER TABLE middleman_workspaces
			RENAME TO middleman_workspaces_v11;

		CREATE TABLE middleman_workspaces (
		    id            TEXT PRIMARY KEY,
		    platform_host TEXT NOT NULL,
		    repo_owner    TEXT NOT NULL,
		    repo_name     TEXT NOT NULL,
		    mr_number     INTEGER NOT NULL,
		    mr_head_ref   TEXT NOT NULL,
		    mr_head_repo  TEXT,
		    worktree_path TEXT NOT NULL,
		    tmux_session  TEXT NOT NULL,
		    status        TEXT NOT NULL DEFAULT 'creating',
		    error_message TEXT,
		    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
		    UNIQUE(platform_host, repo_owner, repo_name, mr_number)
		);

		INSERT INTO middleman_workspaces (
		    id, platform_host, repo_owner, repo_name,
		    mr_number, mr_head_ref, mr_head_repo,
		    worktree_path, tmux_session, status,
		    error_message, created_at
		)
		SELECT
		    id, platform_host, repo_owner, repo_name,
		    item_number, git_head_ref, mr_head_repo,
		    worktree_path, tmux_session, status,
		    error_message, created_at
		FROM middleman_workspaces_v11;

		DROP TABLE middleman_workspaces_v11;

		CREATE TRIGGER middleman_workspaces_casefold_insert
		BEFORE INSERT ON middleman_workspaces
		WHEN NEW.platform_host <> lower(NEW.platform_host)
		  OR NEW.repo_owner <> lower(NEW.repo_owner)
		  OR NEW.repo_name <> lower(NEW.repo_name)
		BEGIN
		    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
		END;

		CREATE TRIGGER middleman_workspaces_casefold_update
		BEFORE UPDATE OF platform_host, repo_owner, repo_name ON middleman_workspaces
		WHEN NEW.platform_host <> lower(NEW.platform_host)
		  OR NEW.repo_owner <> lower(NEW.repo_owner)
		  OR NEW.repo_name <> lower(NEW.repo_name)
		BEGIN
		    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
		END;
	`)
	require.NoError(err)
	_, err = raw.ExecContext(ctx,
		`UPDATE schema_migrations SET version = ?, dirty = FALSE`,
		9,
	)
	require.NoError(err)
	require.NoError(raw.Close())

	reopened, err := db.Open(path)
	require.NoError(err)
	t.Cleanup(func() { require.NoError(reopened.Close()) })

	srv := setupTestServerWithDatabase(t, reopened, defaultTestRepos)
	client := setupTestClient(t, srv)

	since := "2026-04-11T11:30:00Z"
	resp, err := client.HTTP.GetActivityWithResponse(
		ctx, &generated.GetActivityParams{Since: &since},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Items)
	commentItems := make([]generated.ActivityItemResponse, 0, 2)
	for _, item := range *resp.JSON200.Items {
		if item.ActivityType == "comment" {
			commentItems = append(commentItems, item)
		}
	}
	require.Len(commentItems, 2)
	assert.Equal("issue-reporter", commentItems[0].Author)
	assert.Equal("pr-reviewer", commentItems[1].Author)
	assertRFC3339UTC(t, commentItems[0].CreatedAt, time.Date(2026, 4, 11, 13, 0, 0, 0, time.UTC))
	assertRFC3339UTC(t, commentItems[1].CreatedAt, time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC))

	since = "2026-04-11T12:30:00Z"
	resp, err = client.HTTP.GetActivityWithResponse(
		ctx, &generated.GetActivityParams{Since: &since},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Items)
	require.Len(*resp.JSON200.Items, 1)
	assert.Equal("issue-reporter", (*resp.JSON200.Items)[0].Author)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(gitenv.StripAll(os.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

func testGitSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	cmd.Env = append(gitenv.StripAll(os.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func setupTestServerWithClones(t *testing.T) (
	client *apiclient.Client,
	database *db.DB,
	mergeBase string,
	headSHA string,
	commitSHAs []string,
) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bareDir := filepath.Join(dir, "clones")
	require.NoError(t, os.MkdirAll(bareDir, 0o755))
	bare := filepath.Join(bareDir, "github.com", "acme", "widget.git")

	tmpWork := filepath.Join(dir, "work")
	runGit(t, dir, "init", "--bare", "--initial-branch=main", bare)
	runGit(t, dir, "clone", bare, tmpWork)
	runGit(t, tmpWork, "config", "user.email", "test@test.com")
	runGit(t, tmpWork, "config", "user.name", "Test")

	require.NoError(t, os.WriteFile(filepath.Join(tmpWork, "base.txt"), []byte("base\n"), 0o644))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "base commit")
	runGit(t, tmpWork, "push", "origin", "main")
	mergeBase = testGitSHA(t, tmpWork, "HEAD")

	runGit(t, tmpWork, "checkout", "-b", "pr")
	for i := 1; i <= 5; i++ {
		fname := fmt.Sprintf("file%d.txt", i)
		require.NoError(t, os.WriteFile(filepath.Join(tmpWork, fname), fmt.Appendf(nil, "content %d\n", i), 0o644))
		runGit(t, tmpWork, "add", ".")
		runGit(t, tmpWork, "commit", "-m", fmt.Sprintf("commit %d", i))
	}
	runGit(t, tmpWork, "push", "origin", "pr")
	headSHA = testGitSHA(t, tmpWork, "HEAD")

	// Collect SHAs newest-first.
	commitSHAs = make([]string, 5)
	sha := headSHA
	for i := range 5 {
		commitSHAs[i] = sha
		sha = testGitSHA(t, tmpWork, sha+"^1")
	}

	clones := gitclone.New(bareDir, nil)
	mock := &mockGH{}
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "widget", PlatformHost: "github.com"}}
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, repos, time.Minute, nil, nil)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{Clones: clones})

	seedPR(t, database, "acme", "widget", 1)
	ctx := t.Context()
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(t, err)
	require.NoError(t, database.UpdateDiffSHAs(ctx, repoID, 1, headSHA, mergeBase, mergeBase))

	client = setupTestClient(t, srv)
	return client, database, mergeBase, headSHA, commitSHAs
}

func TestAPIGetCommits(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberCommitsWithResponse(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.Len(*resp.JSON200.Commits, 5)
	assert.Equal(commitSHAs[0], (*resp.JSON200.Commits)[0].Sha)
	assert.Equal("commit 5", (*resp.JSON200.Commits)[0].Message)
	assert.Equal(time.UTC, (*resp.JSON200.Commits)[0].AuthoredAt.Location())
}

func TestAPIGetCommits_NotFound(t *testing.T) {
	client, _, _, _, _ := setupTestServerWithClones(t)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberCommitsWithResponse(
		t.Context(), "acme", "widget", 999,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode())
}

func TestAPIGetDiff_SingleCommit(t *testing.T) {
	require := require.New(t)

	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{Commit: &commitSHAs[2]},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200.Files, 1)
}

func TestAPIGetDiff_Range(t *testing.T) {
	require := require.New(t)

	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	from := commitSHAs[4] // commit 1 (oldest)
	to := commitSHAs[2]   // commit 3
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{From: &from, To: &to},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200.Files, 3)
}

func TestAPIGetDiff_InvalidScope(t *testing.T) {
	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	from := commitSHAs[0]
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{Commit: &commitSHAs[0], From: &from},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPIGetDiff_UnknownSHA(t *testing.T) {
	client, _, _, _, _ := setupTestServerWithClones(t)
	bogus := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{Commit: &bogus},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPIGetDiff_ReversedRange(t *testing.T) {
	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	from := commitSHAs[0] // newest
	to := commitSHAs[4]   // oldest
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{From: &from, To: &to},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPIGetDiff_FromWithoutTo(t *testing.T) {
	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	from := commitSHAs[0]
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{From: &from},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPIGetDiff_RootCommit(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	bareDir := filepath.Join(dir, "clones")
	require.NoError(os.MkdirAll(bareDir, 0o755))
	bare := filepath.Join(bareDir, "github.com", "acme", "rootrepo.git")
	tmpWork := filepath.Join(dir, "work")
	runGit(t, dir, "init", "--bare", "--initial-branch=main", bare)
	runGit(t, dir, "clone", bare, tmpWork)
	runGit(t, tmpWork, "config", "user.email", "test@test.com")
	runGit(t, tmpWork, "config", "user.name", "Test")

	require.NoError(os.WriteFile(filepath.Join(tmpWork, "root.txt"), []byte("root\n"), 0o644))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "root commit")
	rootSHA := testGitSHA(t, tmpWork, "HEAD")

	require.NoError(os.WriteFile(filepath.Join(tmpWork, "second.txt"), []byte("second\n"), 0o644))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "second commit")
	runGit(t, tmpWork, "push", "origin", "main")
	headSHA := testGitSHA(t, tmpWork, "HEAD")

	clones := gitclone.New(bareDir, nil)
	mock := &mockGH{}
	repos := []ghclient.RepoRef{{Owner: "acme", Name: "rootrepo", PlatformHost: "github.com"}}
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, repos, time.Minute, nil, nil)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{Clones: clones})

	seedPR(t, database, "acme", "rootrepo", 1)
	ctx := t.Context()
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "rootrepo")
	require.NoError(err)
	require.NoError(database.UpdateDiffSHAs(ctx, repoID, 1, headSHA, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "4b825dc642cb6eb9a060e54bf8d69288fbee4904"))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		t.Context(), "acme", "rootrepo", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{Commit: &rootSHA},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
}

func TestAPIListActivity(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	prID := seedPR(t, database, "acme", "widget", 1)
	ctx := t.Context()

	require.NoError(database.UpsertMREvents(ctx, []db.MREvent{
		{
			MergeRequestID: prID,
			EventType:      "issue_comment",
			Author:         "reviewer",
			Body:           "Looks good",
			CreatedAt:      time.Now().UTC(),
			DedupeKey:      "comment-1",
		},
	}))

	since := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	resp, err := client.HTTP.GetActivityWithResponse(
		ctx, &generated.GetActivityParams{Since: &since},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Items)
	assert.NotEmpty(*resp.JSON200.Items,
		"activity feed should contain PR and comment items")
	assert.Equal("github.com", (*resp.JSON200.Items)[0].PlatformHost)
}

// --- Stacks E2E ---

func seedStackedPR(
	t *testing.T, database *db.DB,
	owner, name string, number int,
	head, base, state, ci, review string,
) int64 {
	return seedStackedPRDraft(t, database, owner, name, number, head, base, state, ci, review, false)
}

func seedStackedPRDraft(
	t *testing.T, database *db.DB,
	owner, name string, number int,
	head, base, state, ci, review string,
	isDraft bool,
) int64 {
	t.Helper()
	ctx := t.Context()
	repoID, err := database.UpsertRepo(ctx, "github.com", owner, name)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     int64(number) * 1000,
		Number:         number,
		Title:          fmt.Sprintf("PR #%d: %s", number, head),
		Author:         "testuser",
		State:          state,
		IsDraft:        isDraft,
		HeadBranch:     head,
		BaseBranch:     base,
		CIStatus:       ci,
		ReviewDecision: review,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	prID, err := database.UpsertMergeRequest(ctx, pr)
	require.NoError(t, err)
	require.NoError(t, database.EnsureKanbanState(ctx, prID))
	return prID
}

func runStackDetection(t *testing.T, database *db.DB, owner, name string) {
	t.Helper()
	ctx := t.Context()
	repo, err := database.GetRepoByOwnerName(ctx, owner, name)
	require.NoError(t, err)
	require.NotNil(t, repo)
	require.NoError(t, stacks.RunDetection(ctx, database, repo.ID))
}

func TestAPIListStacks(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	seedStackedPR(t, database, "acme", "widget", 10, "feat/auth", "main", "open", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/auth-retry", "feat/auth", "open", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 12, "feat/auth-ui", "feat/auth-retry", "open", "pending", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.ListStacksWithResponse(t.Context(), &generated.ListStacksParams{})
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	var stks []generated.StackResponse
	require.NoError(json.Unmarshal(resp.Body, &stks))
	assert.Len(stks, 1)
	assert.Equal("auth", stks[0].Name)
	require.NotNil(stks[0].Members)
	assert.Len(*stks[0].Members, 3)
	assert.Equal(int64(10), (*stks[0].Members)[0].Number)
}

func TestAPIListStacks_RepoFilter(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	repos := []ghclient.RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
		{Owner: "acme", Name: "tools", PlatformHost: "github.com"},
	}
	srv, database := setupTestServerWithRepos(t, &mockGH{}, repos)
	client := setupTestClient(t, srv)
	ctx := t.Context()

	seedStackedPR(t, database, "acme", "widget", 10, "feat/a", "main", "open", "", "")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/b", "feat/a", "open", "", "")
	runStackDetection(t, database, "acme", "widget")

	seedStackedPR(t, database, "acme", "tools", 20, "feat/c", "main", "open", "", "")
	seedStackedPR(t, database, "acme", "tools", 21, "feat/d", "feat/c", "open", "", "")
	runStackDetection(t, database, "acme", "tools")

	respAll, err := client.HTTP.ListStacksWithResponse(ctx, &generated.ListStacksParams{})
	require.NoError(err)
	var allStks []generated.StackResponse
	require.NoError(json.Unmarshal(respAll.Body, &allStks))
	assert.Len(allStks, 2)

	repo := "acme/widget"
	resp, err := client.HTTP.ListStacksWithResponse(ctx, &generated.ListStacksParams{Repo: &repo})
	require.NoError(err)
	assert.Equal(http.StatusOK, resp.StatusCode())
	var filtered []generated.StackResponse
	require.NoError(json.Unmarshal(resp.Body, &filtered))
	assert.Len(filtered, 1)
	assert.Equal("widget", filtered[0].RepoName)

	bad := "noslash"
	resp2, err := client.HTTP.ListStacksWithResponse(ctx, &generated.ListStacksParams{Repo: &bad})
	require.NoError(err)
	assert.Equal(http.StatusBadRequest, resp2.StatusCode())
	assert.Contains(string(resp2.Body), "invalid repo filter")
}

func TestAPIGetStackForPR(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)
	ctx := t.Context()

	// Failing base with an open descendant is blocked.
	seedStackedPR(t, database, "acme", "widget", 10, "feat/api-base", "main", "open", "failure", "")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/api-retry", "feat/api-base", "open", "success", "APPROVED")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(ctx, "acme", "widget", 10)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.Equal("api", resp.JSON200.StackName)
	assert.Equal(int64(2), resp.JSON200.Size)
	assert.Equal("blocked", resp.JSON200.Health)

	seedPR(t, database, "acme", "widget", 99)
	resp2, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(ctx, "acme", "widget", 99)
	require.NoError(err)
	assert.Equal(http.StatusNotFound, resp2.StatusCode())
}

func TestAPIGetStackForPR_DraftNotBaseReady(t *testing.T) {
	assert := Assert.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	// Draft base with green CI + approval; non-draft tip pending.
	seedStackedPRDraft(t, database, "acme", "widget", 10, "feat/x", "main", "open", "success", "APPROVED", true)
	seedStackedPR(t, database, "acme", "widget", 11, "feat/y", "feat/x", "open", "pending", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(t.Context(), "acme", "widget", 10)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	assert.NotEqual("base_ready", resp.JSON200.Health, "draft base must not be base_ready")
	assert.NotEqual("all_green", resp.JSON200.Health, "draft stack must not be all_green")
}

func TestAPIListStacks_DraftNotAllGreen(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	// Both draft, green CI + approved — must not be all_green.
	seedStackedPRDraft(t, database, "acme", "widget", 10, "feat/a", "main", "open", "success", "APPROVED", true)
	seedStackedPRDraft(t, database, "acme", "widget", 11, "feat/b", "feat/a", "open", "success", "APPROVED", true)
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.ListStacksWithResponse(t.Context(), &generated.ListStacksParams{})
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	var stks []generated.StackResponse
	require.NoError(json.Unmarshal(resp.Body, &stks))
	require.Len(stks, 1)
	assert.NotEqual("all_green", stks[0].Health, "all-draft stack must not be all_green")
	assert.NotEqual("base_ready", stks[0].Health, "draft base must not be base_ready")
}

// TestAPIStacks_DetectionViaSyncHook exercises the production wiring:
// SetOnSyncCompleted(stacks.SyncCompletedHook) fires after RunOnce and
// populates stacks without calling RunDetection directly. Verifies that
// GET /stacks and GET /repos/{owner}/{name}/pulls/{number}/stack return
// data produced entirely by the sync-completion callback path.
func TestAPIStacks_DetectionViaSyncHook(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := t.Context()

	// Build GitHub PRs the mock will return; the sync will persist these
	// into DB as open PRs forming a linear chain.
	now := time.Now().UTC().Truncate(time.Second)
	stringPtr := func(s string) *string { return &s }
	makeGHPR := func(id int64, number int, head, base string) *gh.PullRequest {
		sha := fmt.Sprintf("sha%d", number)
		title := fmt.Sprintf("PR #%d: %s", number, head)
		return &gh.PullRequest{
			ID:        &id,
			Number:    &number,
			State:     stringPtr("open"),
			Title:     &title,
			Body:      stringPtr(""),
			User:      &gh.User{Login: stringPtr("testuser")},
			CreatedAt: &gh.Timestamp{Time: now},
			UpdatedAt: &gh.Timestamp{Time: now},
			Head:      &gh.PullRequestBranch{Ref: &head, SHA: &sha},
			Base:      &gh.PullRequestBranch{Ref: &base, SHA: stringPtr("basesha")},
		}
	}
	mock := &mockGH{
		listOpenPullRequestsFn: func(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
			return []*gh.PullRequest{
				makeGHPR(1001, 10, "feat/hook-base", "main"),
				makeGHPR(1011, 11, "feat/hook-tip", "feat/hook-base"),
			}, nil
		},
	}
	srv, database := setupTestServerWithMock(t, mock)
	client := setupTestClient(t, srv)

	// Wire the production hook and run one sync pass. RunOnce will fetch
	// from the mock, persist PRs into DB, then invoke OnSyncCompleted,
	// which runs stack detection.
	srv.syncer.SetOnSyncCompleted(stacks.SyncCompletedHook(ctx, database, nil))
	srv.syncer.RunOnce(ctx)

	// Stacks should be populated purely by the hook path.
	listResp, err := client.HTTP.ListStacksWithResponse(ctx, &generated.ListStacksParams{})
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	var stks []generated.StackResponse
	require.NoError(json.Unmarshal(listResp.Body, &stks))
	require.Len(stks, 1, "sync-hook detection should produce one stack")
	assert.Equal("hook", stks[0].Name)

	ctxResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(ctx, "acme", "widget", 10)
	require.NoError(err)
	require.Equal(http.StatusOK, ctxResp.StatusCode())
	require.NotNil(ctxResp.JSON200)
	assert.Equal("hook", ctxResp.JSON200.StackName)
	assert.Equal(int64(2), ctxResp.JSON200.Size)
}

func TestAPIGetStackForPR_SingleFailingIsInProgress(t *testing.T) {
	assert := Assert.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	// 2-PR chain where tip is failing but has no descendants.
	// Per blocked semantics, this is partial_merge when base is merged.
	seedStackedPR(t, database, "acme", "widget", 10, "feat/base", "main", "merged", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/tip", "feat/base", "open", "failure", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(t.Context(), "acme", "widget", 11)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	assert.Equal("partial_merge", resp.JSON200.Health,
		"failing tip with merged base and no open descendant is partial_merge, not blocked")
}

func TestAPIGetStackForPR_BaseBranchNotMain(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	// Base PR targets "master" not "main" — API must return real base_branch.
	seedStackedPR(t, database, "acme", "widget", 10, "feat/base", "master", "open", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/tip", "feat/base", "open", "pending", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(t.Context(), "acme", "widget", 10)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Members)
	assert.Len(*resp.JSON200.Members, 2)
	assert.Equal("master", (*resp.JSON200.Members)[0].BaseBranch)
	assert.Equal("feat/base", (*resp.JSON200.Members)[1].BaseBranch)
}

func TestAPIListStacks_Empty(t *testing.T) {
	assert := Assert.New(t)
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.ListStacksWithResponse(t.Context(), &generated.ListStacksParams{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())

	var stks []generated.StackResponse
	require.NoError(t, json.Unmarshal(resp.Body, &stks))
	assert.Empty(stks)
}

// TestDisplayNameCacheE2E verifies the display-name cache
// through the full stack: sync → SQLite → HTTP API. Two
// RunOnce passes populate and then cache-hit the display name;
// the test asserts /api/v1/pulls returns the expected
// AuthorDisplayName after each pass, and that GetUser is only
// called during the first sync.
func TestDisplayNameCacheE2E(t *testing.T) {
	require := require.New(t)

	now := time.Now().UTC().Truncate(time.Second)
	prID := int64(1000)
	prNumber := 1
	prTitle := "test pr"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/1"
	prBody := ""
	prAuthor := "alice"
	displayName := "Alice Smith"
	getUserCalls := 0

	mock := &mockGH{
		listOpenPullRequestsFn: func(
			_ context.Context, _, _ string,
		) ([]*gh.PullRequest, error) {
			return []*gh.PullRequest{{
				ID:        &prID,
				Number:    &prNumber,
				Title:     &prTitle,
				State:     &prState,
				HTMLURL:   &prURL,
				Body:      &prBody,
				User:      &gh.User{Login: &prAuthor},
				CreatedAt: &gh.Timestamp{Time: now},
				UpdatedAt: &gh.Timestamp{Time: now},
			}}, nil
		},
		getUserFn: func(_ context.Context, login string) (*gh.User, error) {
			getUserCalls++
			return &gh.User{Login: &login, Name: &displayName}, nil
		},
	}

	srv, _ := setupTestServerWithMock(t, mock)

	// First sync: populates display name via GetUser.
	srv.syncer.RunOnce(t.Context())
	require.Positive(getUserCalls, "first sync should call GetUser")
	firstCalls := getUserCalls

	// GET /api/v1/pulls — display name must appear.
	rr := doJSON(t, srv, http.MethodGet, "/api/v1/pulls", nil)
	require.Equal(http.StatusOK, rr.Code)
	require.Contains(rr.Body.String(), `"AuthorDisplayName":"Alice Smith"`)

	// Second sync: cache hit, no new GetUser calls.
	srv.syncer.RunOnce(t.Context())
	require.Equal(firstCalls, getUserCalls,
		"second sync must not re-fetch cached display names")

	// GET /api/v1/pulls — display name still present.
	rr2 := doJSON(t, srv, http.MethodGet, "/api/v1/pulls", nil)
	require.Equal(http.StatusOK, rr2.Code)
	require.Contains(rr2.Body.String(), `"AuthorDisplayName":"Alice Smith"`)
}

func TestCICheckDedupLatestRunWinsE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	older := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	newer := older.Add(10 * time.Minute)
	prID := int64(1001)
	prNumber := 1
	prTitle := "check dedupe"
	prState := "open"
	prURL := "https://github.com/acme/widget/pull/1"
	prBody := ""
	prAuthor := "alice"
	headRef := "feature/check-dedupe"
	baseRef := "main"
	headSHA := "abc123"
	baseSHA := "def456"
	headCloneURL := "https://github.com/acme/widget.git"
	checkName := "build"
	checkStatus := "completed"
	oldConclusion := "failure"
	newConclusion := "success"
	oldCheckURL := "https://github.com/acme/widget/actions/runs/1"
	newCheckURL := "https://github.com/acme/widget/actions/runs/2"
	combinedTotal := 1
	combinedState := "success"

	pr := &gh.PullRequest{
		ID:        &prID,
		Number:    &prNumber,
		Title:     &prTitle,
		State:     &prState,
		HTMLURL:   &prURL,
		Body:      &prBody,
		User:      &gh.User{Login: &prAuthor},
		CreatedAt: &gh.Timestamp{Time: older},
		UpdatedAt: &gh.Timestamp{Time: newer},
		Head: &gh.PullRequestBranch{
			Ref: &headRef,
			SHA: &headSHA,
			Repo: &gh.Repository{
				CloneURL: &headCloneURL,
			},
		},
		Base: &gh.PullRequestBranch{
			Ref: &baseRef,
			SHA: &baseSHA,
		},
	}

	mock := &mockGH{
		getPullRequestFn: func(
			_ context.Context, _, _ string, _ int,
		) (*gh.PullRequest, error) {
			return pr, nil
		},
		listCheckRunsForRefFn: func(
			_ context.Context, owner, repo, ref string,
		) ([]*gh.CheckRun, error) {
			require.Equal("acme", owner)
			require.Equal("widget", repo)
			require.Equal(headSHA, ref)
			return []*gh.CheckRun{
				{
					ID:          new(int64(10)),
					Name:        &checkName,
					Status:      &checkStatus,
					Conclusion:  &oldConclusion,
					CompletedAt: &gh.Timestamp{Time: older},
					HTMLURL:     &oldCheckURL,
				},
				{
					ID:          new(int64(11)),
					Name:        &checkName,
					Status:      &checkStatus,
					Conclusion:  &newConclusion,
					CompletedAt: &gh.Timestamp{Time: newer},
					HTMLURL:     &newCheckURL,
				},
			}, nil
		},
		getCombinedStatusFn: func(
			_ context.Context, owner, repo, ref string,
		) (*gh.CombinedStatus, error) {
			require.Equal("acme", owner)
			require.Equal("widget", repo)
			require.Equal(headSHA, ref)
			return &gh.CombinedStatus{
				TotalCount: &combinedTotal,
				State:      &combinedState,
			}, nil
		},
	}

	srv, database := setupTestServerWithMock(t, mock)
	client := setupTestClient(t, srv)
	seedPR(t, database, "acme", "widget", prNumber)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
		context.Background(), "acme", "widget", int64(prNumber),
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.MergeRequest)
	require.Equal("success", resp.JSON200.MergeRequest.CIStatus)

	var checks []db.CICheck
	require.NoError(json.Unmarshal(
		[]byte(resp.JSON200.MergeRequest.CIChecksJSON),
		&checks,
	))
	require.Len(checks, 1)
	assert.Equal("build", checks[0].Name)
	assert.Equal("completed", checks[0].Status)
	assert.Equal("success", checks[0].Conclusion)
	assert.Equal(newCheckURL, checks[0].URL)
}

// setupTestServerWithWorkspaces creates a test server wired with
// both a gitclone.Manager and a workspace.Manager backed by a
// bare repo that has a "pr" branch. It seeds a PR in the DB
// and returns the API client and database.
func setupTestServerWithWorkspaces(
	t *testing.T,
) (*apiclient.Client, *db.DB, string, string) {
	t.Helper()
	fixture := setupWorkspaceServerFixture(t, nil)
	return fixture.client, fixture.database, fixture.bare, fixture.remote
}

func setupTestServerWithWorkspacesServer(
	t *testing.T,
	cfg *config.Config,
) (*apiclient.Client, *db.DB, string, string, *Server) {
	t.Helper()
	fixture := setupWorkspaceServerFixture(t, cfg)
	return fixture.client, fixture.database, fixture.bare, fixture.remote, fixture.server
}

type workspaceServerFixture struct {
	server   *Server
	client   *apiclient.Client
	database *db.DB
	bare     string
	remote   string
}

func setupWorkspaceServerFixture(
	t *testing.T,
	cfg *config.Config,
) workspaceServerFixture {
	t.Helper()

	if testing.Short() {
		t.Skip("workspace e2e tests skipped in short mode")
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	remoteDir := filepath.Join(dir, "remote")
	require.NoError(t, os.MkdirAll(remoteDir, 0o755))
	remote := filepath.Join(remoteDir, "widget.git")
	runGit(
		t, dir, "init", "--bare", "--initial-branch=main", remote,
	)

	tmpWork := filepath.Join(dir, "work")
	runGit(t, dir, "clone", remote, tmpWork)
	runGit(t, tmpWork, "config", "user.email", "test@test.com")
	runGit(t, tmpWork, "config", "user.name", "Test")

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpWork, "base.txt"),
		[]byte("base\n"), 0o644,
	))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "base commit")
	runGit(t, tmpWork, "push", "origin", "main")

	runGit(t, tmpWork, "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpWork, "new.txt"),
		[]byte("new\n"), 0o644,
	))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "feature commit")
	runGit(t, tmpWork, "push", "origin", "feature")

	bareDir := filepath.Join(dir, "clones")
	require.NoError(t, os.MkdirAll(bareDir, 0o755))
	bare := filepath.Join(
		bareDir, "github.com", "acme", "widget.git",
	)
	runGit(t, dir, "clone", "--bare", remote, bare)

	clones := gitclone.New(bareDir, nil)
	worktreeDir := filepath.Join(dir, "worktrees")
	mock := &mockGH{}
	repos := []ghclient.RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
	}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database, nil, repos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)
	basePath := "/"
	if cfg != nil && cfg.BasePath != "" {
		basePath = cfg.BasePath
	}
	srv := New(database, syncer, nil, basePath, cfg, ServerOptions{
		Clones:      clones,
		WorktreeDir: worktreeDir,
	})
	// Cleanup callbacks run LIFO. Drain the server first so async
	// workspace setup cannot create a tmux session after fixture
	// artifact cleanup has listed workspaces. The DB cleanup was
	// registered earlier, so it remains open for artifact cleanup.
	t.Cleanup(func() { cleanupWorkspaceServerFixtureArtifacts(t, srv, database) })
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	seedPR(t, database, "acme", "widget", 1)

	clientBaseURL := "http://middleman.test"
	if basePath != "/" {
		clientBaseURL += strings.TrimSuffix(basePath, "/")
	}
	client := setupTestClientWithBaseURL(t, srv, clientBaseURL)
	return workspaceServerFixture{
		server:   srv,
		client:   client,
		database: database,
		bare:     bare,
		remote:   remote,
	}
}

func cleanupWorkspaceServerFixtureArtifacts(
	t *testing.T,
	srv *Server,
	database *db.DB,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(
		t,
		cleanupWorkspaceServerFixtureArtifactsWithContext(ctx, srv, database),
	)
}

func cleanupWorkspaceServerFixtureArtifactsWithContext(
	ctx context.Context,
	srv *Server,
	database *db.DB,
) error {
	if srv.workspaces == nil {
		return nil
	}

	workspaces, err := database.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	var errs []error
	for _, ws := range workspaces {
		_, err := func() ([]string, error) {
			beforeDestructive := func(stopCtx context.Context) {
				if srv.runtime != nil {
					srv.runtime.StopWorkspace(stopCtx, ws.ID)
				}
			}
			if srv.runtime != nil {
				srv.runtime.BeginStopping(ws.ID)
				defer srv.runtime.EndStopping(ws.ID)
			}
			return srv.workspaces.Delete(ctx, ws.ID, true, beforeDestructive)
		}()
		if err != nil {
			errs = append(
				errs,
				fmt.Errorf("delete workspace %s: %w", ws.ID, err),
			)
		}
	}
	return errors.Join(errs...)
}

func waitForWorkspaceReady(
	t *testing.T,
	ctx context.Context,
	client *apiclient.Client,
	wsID string,
) *generated.WorkspaceResponse {
	t.Helper()

	var ready *generated.WorkspaceResponse
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		getResp, err := client.HTTP.GetWorkspacesByIdWithResponse(
			ctx, wsID,
		)
		require.NoError(t, err)
		if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
			continue
		}
		if getResp.JSON200.Status == "ready" {
			ready = getResp.JSON200
			break
		}
	}

	require.NotNil(t, ready, "workspace never became ready: %s", wsID)
	return ready
}

func TestWorkspaceServerFixtureCleansUpTmuxSessions(t *testing.T) {
	require := require.New(t)
	if testing.Short() {
		t.Skip("workspace e2e tests skipped in short mode")
	}

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	t.Run("fixture", func(t *testing.T) {
		t.Setenv("TMUX_RECORD", record)
		cfg := &config.Config{
			Tmux: config.Tmux{Command: []string{script}},
		}
		client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)

		createReadyWorkspace(t, context.Background(), client)
	})

	var killed bool
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 3 &&
			argv[0] == "kill-session" &&
			argv[1] == "-t" &&
			strings.HasPrefix(argv[2], "middleman-") {
			killed = true
			break
		}
	}
	require.True(killed, "fixture cleanup did not kill workspace tmux session")
}

func TestCleanupWorkspaceServerFixtureArtifactsKeepsDeletingAfterError(
	t *testing.T,
) {
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`if [ "$1" = "kill-session" ] && [ "$3" = "middleman-fails" ]; then` + "\n" +
		`  echo "permission denied" >&2` + "\n" +
		`  exit 1` + "\n" +
		`fi` + "\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	manager := workspace.NewManager(database, filepath.Join(dir, "worktrees"))
	manager.SetTmuxCommand([]string{script})
	srv := &Server{workspaces: manager}
	ctx := context.Background()
	require.NoError(database.InsertWorkspace(ctx, &workspace.Workspace{
		ID:              "ws-succeeds",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		ItemType:        db.WorkspaceItemTypePullRequest,
		ItemNumber:      1,
		GitHeadRef:      "feature/succeeds",
		WorkspaceBranch: "middleman/pr-1",
		WorktreePath:    filepath.Join(dir, "succeeds"),
		TmuxSession:     "middleman-succeeds",
		Status:          "ready",
	}))
	time.Sleep(time.Millisecond)
	require.NoError(database.InsertWorkspace(ctx, &workspace.Workspace{
		ID:              "ws-fails",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		ItemType:        db.WorkspaceItemTypePullRequest,
		ItemNumber:      2,
		GitHeadRef:      "feature/fails",
		WorkspaceBranch: "middleman/pr-2",
		WorktreePath:    filepath.Join(dir, "fails"),
		TmuxSession:     "middleman-fails",
		Status:          "ready",
	}))

	err = cleanupWorkspaceServerFixtureArtifactsWithContext(ctx, srv, database)
	require.Error(err)
	require.Contains(err.Error(), "ws-fails")
	require.Contains(err.Error(), "permission denied")

	killedSessions := map[string]bool{}
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 3 &&
			argv[0] == "kill-session" {
			killedSessions[argv[2]] = true
		}
	}
	require.True(
		killedSessions["middleman-succeeds"],
		"cleanup stopped before later workspace tmux session",
	)
}

func TestWorkspaceRuntimeTargetsE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, nil)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	resp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.Id)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.LaunchTargets)
	require.NotNil(resp.JSON200.Sessions)
	assert.NotEmpty(*resp.JSON200.LaunchTargets)
	assert.Empty(*resp.JSON200.Sessions)
	assert.Nil(resp.JSON200.ShellSession)
	assertWorkspaceRuntimeTarget(
		t, *resp.JSON200.LaunchTargets, "plain_shell",
	)
	assertWorkspaceRuntimeTarget(t, *resp.JSON200.LaunchTargets, "tmux")
}

func TestWorkspaceRuntimeTargetsUseConfiguredTmuxCommandE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	tmuxPath := filepath.Join(dir, "tmux-wrapper")
	require.NoError(os.WriteFile(
		tmuxPath,
		[]byte("#!/bin/sh\nexit 0\n"),
		0o755,
	))
	cfg := &config.Config{Tmux: config.Tmux{
		Command: []string{tmuxPath, "--scope", "tmux"},
	}}
	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	resp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.Id)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.LaunchTargets)

	var tmux generated.LaunchTarget
	for _, target := range *resp.JSON200.LaunchTargets {
		if target.Key == "tmux" {
			tmux = target
			break
		}
	}
	assert.Equal([]string{tmuxPath, "--scope", "tmux"}, *tmux.Command)
	assert.True(tmux.Available)
}

func TestWorkspaceRuntimeLaunchUnavailableTargetE2E(t *testing.T) {
	disabled := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "disabled",
		Label:   "Disabled",
		Enabled: &disabled,
	}}}
	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	resp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "disabled",
		},
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
	require.Contains(t, string(resp.Body), "not available")
}

func TestWorkspaceRuntimeLaunchPlainShellUsesShellSessionE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, nil)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	resp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "plain_shell",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	shell := resp.JSON200
	assert.Equal("plain_shell", shell.TargetKey)
	assert.Equal(string(localruntime.LaunchTargetPlainShell), shell.Kind)
	assert.Equal(string(localruntime.SessionStatusRunning), shell.Status)

	getResp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.Id)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	require.NotNil(getResp.JSON200.ShellSession)
	require.NotNil(getResp.JSON200.Sessions)
	assert.Equal(shell.Key, getResp.JSON200.ShellSession.Key)
	assert.Empty(*getResp.JSON200.Sessions)
}

func TestWorkspaceRuntimeLaunchSingletonAndStopE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	assert := Assert.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("sleep"),
	}}, Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions}}
	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	firstResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, firstResp.StatusCode())
	require.NotNil(firstResp.JSON200)
	first := firstResp.JSON200

	secondResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, secondResp.StatusCode())
	require.NotNil(secondResp.JSON200)
	second := secondResp.JSON200
	assert.Equal(first.Key, second.Key)
	assert.Equal(string(localruntime.SessionStatusRunning), first.Status)

	listResp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(
		ctx, ws.Id,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Sessions)
	require.Len(*listResp.JSON200.Sessions, 1)
	assert.Equal(first.Key, (*listResp.JSON200.Sessions)[0].Key)

	stopResp, err := client.HTTP.StopWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id, first.Key,
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, stopResp.StatusCode())

	afterStopResp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(
		ctx, ws.Id,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, afterStopResp.StatusCode())
	require.NotNil(afterStopResp.JSON200)
	require.NotNil(afterStopResp.JSON200.Sessions)
	assert.Empty(*afterStopResp.JSON200.Sessions)
}

func TestWorkspaceRuntimeIncludesStoredTmuxSessionsAfterReloadE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	tmuxPath := filepath.Join(dir, "fake-tmux")
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
case "$1" in
  list-sessions)
    printf '%s\n' middleman-0000000000000001
    printf '%s\n' "$RESTORED_TMUX_SESSION"
    exit 0
    ;;
  attach-session)
    sleep 30
    exit 0
    ;;
  kill-session)
    exit 0
    ;;
esac
exit 0
`), 0o755))

	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })
	seedPR(t, database, "acme", "widget", 1)
	worktreeDir := filepath.Join(dir, "worktrees")
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("sleep"),
	}}, Tmux: config.Tmux{Command: []string{tmuxPath}}}
	ctx := context.Background()
	ws := &workspace.Workspace{
		ID:              "0000000000000001",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		ItemType:        db.WorkspaceItemTypePullRequest,
		ItemNumber:      1,
		GitHeadRef:      "feature",
		WorkspaceBranch: "feature",
		WorktreePath:    filepath.Join(worktreeDir, "acme-widget-1"),
		TmuxSession:     "middleman-0000000000000001",
		Status:          "ready",
	}
	require.NoError(database.InsertWorkspace(ctx, ws))
	tmuxSession := runtimeTmuxSessionNameForTest(ws.ID, "helper")
	t.Setenv("RESTORED_TMUX_SESSION", tmuxSession)
	require.NoError(database.UpsertWorkspaceTmuxSession(
		ctx,
		&db.WorkspaceTmuxSession{
			WorkspaceID: ws.ID,
			SessionName: tmuxSession,
			TargetKey:   "helper",
		},
	))
	srv := New(database, nil, nil, "/", cfg, ServerOptions{
		WorktreeDir: worktreeDir,
	})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)

	require.Len(srv.runtime.ListSessions(ws.ID), 1)

	resp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.ID)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.Sessions)
	require.Len(*resp.JSON200.Sessions, 1)

	session := (*resp.JSON200.Sessions)[0]
	assert.Equal(ws.ID+":helper", session.Key)
	assert.Equal(ws.ID, session.WorkspaceId)
	assert.Equal("helper", session.TargetKey)
	assert.Equal("Helper", session.Label)
	assert.Equal(string(localruntime.LaunchTargetAgent), session.Kind)
	assert.Equal(string(localruntime.SessionStatusRunning), session.Status)
	assert.False(session.CreatedAt.IsZero())
	assert.Equal(time.UTC, session.CreatedAt.Location())
}

func TestWorkspaceRuntimeLaunchAgentCreatesProbeableTmuxSessionE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tmuxPath := filepath.Join(dir, "fake-tmux")
	agentPath := filepath.Join(dir, "helper-agent")
	require.NoError(os.WriteFile(agentPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
session_file="${TMUX_RECORD}.sessions"
target=""
mode=""
new_session=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-t" ]; then target="$a"; fi
  if [ "$prev" = "-s" ]; then new_session="$a"; fi
  if [ "$a" = "display-message" ]; then mode="display-message"; fi
  if [ "$a" = "capture-pane" ]; then mode="capture-pane"; fi
  if [ "$a" = "list-sessions" ]; then
    [ -f "$session_file" ] && cat "$session_file"
    exit 0
  fi
  prev="$a"
done
if [ "$mode" = "display-message" ]; then
  case "$target" in
    middleman-????????????????-*) printf '⠴ t3code-b5014b03\n' ;;
    *) printf 'idle\n' ;;
  esac
  exit 0
fi
if [ "$mode" = "capture-pane" ]; then
  printf 'stable\n'
  exit 0
fi
if [ "$1" = "has-session" ]; then
  exit 1
fi
if [ -n "$new_session" ]; then
  printf '%s\n' "$new_session" >> "$session_file"
fi
exit 0
`), 0o755))
	t.Setenv("TMUX_RECORD", record)
	cfg := &config.Config{
		Agents: []config.Agent{{
			Key:     "helper",
			Label:   "Helper",
			Command: []string{agentPath, "--flag"},
		}},
		Tmux: config.Tmux{Command: []string{tmuxPath}},
	}
	client, database, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	resp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)

	var newSession []string
	require.Eventually(func() bool {
		for _, argv := range readTmuxRecord(t, record) {
			if len(argv) > 0 &&
				argv[0] == "new-session" &&
				strings.Contains(strings.Join(argv, "\n"), agentPath) {
				newSession = argv
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond)

	session, ok := argAfter(newSession, "-s")
	require.True(ok, "new-session should name a tmux session")
	assert.Equal(runtimeTmuxSessionNameForTest(ws.Id, "helper"), session)
	assert.Contains(newSession, "-d")
	assert.Contains(newSession, "-c")
	assert.Contains(strings.Join(newSession, "\n"), agentPath)
	assert.Contains(strings.Join(newSession, "\n"), "--flag")
	assert.Contains(newSession, "@middleman_owner")
	assert.Contains(newSession, srv.workspaces.TmuxOwnerMarker())

	listResp, err := client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Workspaces)

	var listed *generated.WorkspaceResponse
	for i := range *listResp.JSON200.Workspaces {
		if (*listResp.JSON200.Workspaces)[i].Id == ws.Id {
			listed = &(*listResp.JSON200.Workspaces)[i]
			break
		}
	}
	require.NotNil(listed)
	assert.True(listed.TmuxWorking)
	assert.Equal(tmuxActivitySourceTitle, listed.TmuxActivitySource)
	require.NotNil(listed.TmuxPaneTitle)
	assert.Equal("⠴ t3code-b5014b03", *listed.TmuxPaneTitle)
	assert.Contains(readTmuxRecord(t, record), []string{
		"display-message", "-p", "-t", session, "#{pane_title}",
	})
	stored, err := database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 1)
	assert.Equal(session, stored[0].SessionName)
	assert.Equal("helper", stored[0].TargetKey)
}

func TestServerStartupReapsUnrecordedRuntimeTmuxSessionE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tmuxPath := filepath.Join(dir, "fake-tmux")
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
target=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-t" ]; then target="$a"; fi
  prev="$a"
done
case "$1" in
  list-sessions)
    printf 'middleman-0000000000000001\nmiddleman-0000000000000001-0123456789abcdef\n'
    exit 0
    ;;
  show-options)
    printf '%s\n' "$MIDDLEMAN_TMUX_OWNER"
    exit 0
    ;;
  kill-session)
    exit 0
    ;;
esac
exit 0
`), 0o755))
	t.Setenv("TMUX_RECORD", record)

	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })
	seedPR(t, database, "acme", "widget", 1)

	worktreeDir := filepath.Join(dir, "worktrees")
	ownerMarker := workspace.NewManager(database, worktreeDir).TmuxOwnerMarker()
	t.Setenv("MIDDLEMAN_TMUX_OWNER", ownerMarker)
	ws := &workspace.Workspace{
		ID:              "0000000000000001",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		ItemType:        db.WorkspaceItemTypePullRequest,
		ItemNumber:      1,
		GitHeadRef:      "feature",
		WorkspaceBranch: "feature",
		WorktreePath:    filepath.Join(worktreeDir, "acme-widget-1"),
		TmuxSession:     "middleman-0000000000000001",
		Status:          "ready",
	}
	require.NoError(database.InsertWorkspace(t.Context(), ws))

	cfg := &config.Config{Tmux: config.Tmux{Command: []string{tmuxPath}}}
	srv := New(database, nil, nil, "/", cfg, ServerOptions{
		WorktreeDir: worktreeDir,
	})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetWorkspacesByIdWithResponse(t.Context(), ws.ID)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	argvs := readTmuxRecord(t, record)
	assert.Contains(argvs, []string{
		"kill-session", "-t", "middleman-0000000000000001-0123456789abcdef",
	})
	assert.NotContains(argvs, []string{
		"kill-session", "-t", "middleman-0000000000000001",
	})
}

func TestWorkspaceResponseProbesStoredRuntimeTmuxSessionWithoutBaseE2E(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tmuxPath := filepath.Join(dir, "fake-tmux")
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
target=""
mode=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-t" ]; then target="$a"; fi
  if [ "$a" = "display-message" ]; then mode="display-message"; fi
  if [ "$a" = "capture-pane" ]; then mode="capture-pane"; fi
  prev="$a"
done
case "$1" in
  list-sessions)
    printf '%s\n' 'middleman-0000000000000001-e81d3b0e9d82feaa'
    exit 0
    ;;
esac
if [ "$mode" = "display-message" ]; then
  case "$target" in
    middleman-????????????????-*) printf '⠴ t3code-b5014b03\n' ;;
    *) printf 'idle\n' ;;
  esac
  exit 0
fi
if [ "$mode" = "capture-pane" ]; then
  printf 'stable\n'
  exit 0
fi
exit 0
`), 0o755))
	t.Setenv("TMUX_RECORD", record)

	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })
	seedPR(t, database, "acme", "widget", 1)

	worktreeDir := filepath.Join(dir, "worktrees")
	ws := &workspace.Workspace{
		ID:              "0000000000000001",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		ItemType:        db.WorkspaceItemTypePullRequest,
		ItemNumber:      1,
		GitHeadRef:      "feature",
		WorkspaceBranch: "feature",
		WorktreePath:    filepath.Join(worktreeDir, "acme-widget-1"),
		Status:          "ready",
	}
	require.NoError(database.InsertWorkspace(t.Context(), ws))
	require.NoError(database.UpsertWorkspaceTmuxSession(
		t.Context(),
		&db.WorkspaceTmuxSession{
			WorkspaceID: ws.ID,
			SessionName: runtimeTmuxSessionNameForTest(
				"0000000000000001", "helper",
			),
			TargetKey: "helper",
		},
	))
	sessionName := runtimeTmuxSessionNameForTest("0000000000000001", "helper")

	cfg := &config.Config{Tmux: config.Tmux{Command: []string{tmuxPath}}}
	srv := New(database, nil, nil, "/", cfg, ServerOptions{
		WorktreeDir: worktreeDir,
	})
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetWorkspacesByIdWithResponse(t.Context(), ws.ID)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.True(resp.JSON200.TmuxWorking)
	assert.Equal(tmuxActivitySourceTitle, resp.JSON200.TmuxActivitySource)
	require.NotNil(resp.JSON200.TmuxPaneTitle)
	assert.Equal("⠴ t3code-b5014b03", *resp.JSON200.TmuxPaneTitle)
	assert.Contains(readTmuxRecord(t, record), []string{
		"display-message", "-p", "-t",
		sessionName, "#{pane_title}",
	})
}

func TestWorkspaceRuntimeLaunchTmuxOwnerMarkerFailureCleansSessionE2E(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tmuxPath := filepath.Join(dir, "fake-tmux")
	agentPath := filepath.Join(dir, "helper-agent")
	require.NoError(os.WriteFile(agentPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
target=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-t" ]; then target="$a"; fi
  prev="$a"
done
case "$1" in
  has-session)
    exit 1
    ;;
  new-session)
    for a in "$@"; do
      if [ "$a" = "@middleman_owner" ]; then
        echo "owner marker denied" >&2
        exit 42
      fi
    done
    exit 0
    ;;
  set-option)
    case "$target" in
      middleman-????????????????-*)
        echo "owner marker denied" >&2
        exit 42
        ;;
    esac
    exit 0
    ;;
  kill-session)
    exit 0
    ;;
esac
exit 0
`), 0o755))
	t.Setenv("TMUX_RECORD", record)
	cfg := &config.Config{
		Agents: []config.Agent{{
			Key:     "helper",
			Label:   "Helper",
			Command: []string{agentPath},
		}},
		Tmux: config.Tmux{Command: []string{tmuxPath}},
	}
	client, database, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	sessionName := runtimeTmuxSessionNameForTest(ws.Id, "helper")

	require.Eventually(func() bool {
		return tmuxRecordContains(readTmuxRecord(t, record), []string{
			"kill-session", "-t", sessionName,
		})
	}, 2*time.Second, 20*time.Millisecond)
	var runtimeNewSession []string
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) > 0 &&
			argv[0] == "new-session" &&
			slices.Contains(argv, sessionName) {
			runtimeNewSession = argv
			break
		}
	}
	require.NotNil(runtimeNewSession)
	assert.Contains(runtimeNewSession, "@middleman_owner")
	assert.Contains(runtimeNewSession, srv.workspaces.TmuxOwnerMarker())

	var runtimeResp *generated.GetWorkspaceRuntimeResponse
	require.Eventually(func() bool {
		runtimeResp, err = client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.Id)
		if err != nil ||
			runtimeResp.StatusCode() != http.StatusOK ||
			runtimeResp.JSON200 == nil ||
			runtimeResp.JSON200.Sessions == nil ||
			len(*runtimeResp.JSON200.Sessions) != 1 {
			return false
		}
		return (*runtimeResp.JSON200.Sessions)[0].Status == "exited"
	}, 2*time.Second, 20*time.Millisecond)

	stored, err := database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 1)
	assert.Equal(sessionName, stored[0].SessionName)

	stopResp, err := client.HTTP.StopWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id, launchResp.JSON200.Key,
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, stopResp.StatusCode())
	stored, err = database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	assert.Empty(stored)
}

func tmuxRecordContains(argvs [][]string, want []string) bool {
	return slices.ContainsFunc(argvs, func(argv []string) bool {
		return slices.Equal(argv, want)
	})
}

func runtimeTmuxSessionNameForTest(workspaceID string, targetKey string) string {
	sum := sha256.Sum256([]byte(targetKey))
	return "middleman-" + workspaceID + "-" + hex.EncodeToString(sum[:8])
}

func TestWorkspaceRuntimeTmuxSessionsHashUnsafeTargetKeysE2E(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tmuxPath := filepath.Join(dir, "fake-tmux")
	agentPath := filepath.Join(dir, "helper-agent")
	require.NoError(os.WriteFile(agentPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
case "$1" in
  has-session)
    exit 1
    ;;
  new-session|set-option|attach-session|kill-session)
    exit 0
    ;;
esac
exit 0
`), 0o755))
	t.Setenv("TMUX_RECORD", record)
	cfg := &config.Config{
		Agents: []config.Agent{
			{Key: "foo/bar", Label: "Foo Slash", Command: []string{agentPath}},
			{Key: "foo:bar", Label: "Foo Colon", Command: []string{agentPath}},
		},
		Tmux: config.Tmux{Command: []string{tmuxPath}},
	}
	client, database, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	var launched []generated.SessionInfo
	for _, targetKey := range []string{"foo/bar", "foo:bar"} {
		resp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
			ctx, ws.Id,
			generated.LaunchWorkspaceRuntimeSessionInputBody{
				TargetKey: targetKey,
			},
		)
		require.NoError(err)
		require.Equal(http.StatusOK, resp.StatusCode())
		require.NotNil(resp.JSON200)
		launched = append(launched, *resp.JSON200)
	}

	stored, err := database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 2)
	sessionsByTarget := map[string]string{}
	for _, session := range stored {
		sessionsByTarget[session.TargetKey] = session.SessionName
	}
	slashSession := runtimeTmuxSessionNameForTest(ws.Id, "foo/bar")
	colonSession := runtimeTmuxSessionNameForTest(ws.Id, "foo:bar")
	assert.Equal(slashSession, sessionsByTarget["foo/bar"])
	assert.Equal(colonSession, sessionsByTarget["foo:bar"])
	assert.NotEqual(slashSession, colonSession)
	for _, sessionName := range []string{slashSession, colonSession} {
		assert.NotContains(sessionName, "foo")
		assert.NotContains(sessionName, "/")
		assert.NotContains(sessionName, ":")
	}

	for _, session := range launched {
		stopResp, err := client.HTTP.StopWorkspaceRuntimeSessionWithResponse(
			ctx, ws.Id, session.Key,
		)
		require.NoError(err)
		require.Equal(http.StatusNoContent, stopResp.StatusCode())
	}
	stored, err = database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	assert.Empty(stored)
	assert.Contains(readTmuxRecord(t, record), []string{
		"kill-session", "-t", slashSession,
	})
	assert.Contains(readTmuxRecord(t, record), []string{
		"kill-session", "-t", colonSession,
	})
}

func TestWorkspaceRuntimeStopClearsStoredShellKeyTmuxSessionAfterRuntimeForgetE2E(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	tmuxPath := filepath.Join(dir, "fake-tmux")
	agentPath := filepath.Join(dir, "helper-agent")
	require.NoError(os.WriteFile(agentPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
case "$1" in
  has-session)
    exit 1
    ;;
  new-session|set-option|attach-session|kill-session)
    exit 0
    ;;
esac
exit 0
`), 0o755))
	t.Setenv("TMUX_RECORD", record)
	cfg := &config.Config{
		Agents: []config.Agent{{
			Key:     "shell",
			Label:   "Shell Agent",
			Command: []string{agentPath},
		}},
		Tmux: config.Tmux{Command: []string{tmuxPath}},
	}
	client, database, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "shell",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	sessionName := runtimeTmuxSessionNameForTest(ws.Id, "shell")

	stored, err := database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 1)
	assert.Equal(sessionName, stored[0].SessionName)

	require.NoError(srv.runtime.Stop(ctx, ws.Id, launchResp.JSON200.Key))
	stored, err = database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 1)

	stopResp, err := client.HTTP.StopWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id, launchResp.JSON200.Key,
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, stopResp.StatusCode())
	stored, err = database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	assert.Empty(stored)
	assert.Contains(readTmuxRecord(t, record), []string{
		"kill-session", "-t", sessionName,
	})
}

func TestWorkspaceRuntimeStopTmuxCleanupFailureKeepsSessionE2E(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	tmuxPath := filepath.Join(dir, "fake-tmux")
	agentPath := filepath.Join(dir, "helper-agent")
	require.NoError(os.WriteFile(agentPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
target=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-t" ]; then target="$a"; fi
  prev="$a"
done
if [ "$1" = "kill-session" ]; then
  case "$target" in
    middleman-????????????????-*)
      echo "permission denied" >&2
      exit 42
      ;;
  esac
fi
exit 0
`), 0o755))
	cfg := &config.Config{
		Agents: []config.Agent{{
			Key:     "helper",
			Label:   "Helper",
			Command: []string{agentPath},
		}},
		Tmux: config.Tmux{Command: []string{tmuxPath}},
	}
	client, database, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)
	t.Cleanup(func() {
		_ = database.DeleteWorkspaceTmuxSessions(context.Background(), ws.Id)
	})

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)

	stopResp, err := client.HTTP.StopWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id, launchResp.JSON200.Key,
	)
	require.NoError(err)
	require.Equal(http.StatusInternalServerError, stopResp.StatusCode())

	getResp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.Id)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	require.NotNil(getResp.JSON200.Sessions)
	require.Len(*getResp.JSON200.Sessions, 1)
	assert.Equal(launchResp.JSON200.Key, (*getResp.JSON200.Sessions)[0].Key)

	stored, err := database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 1)
	assert.Equal(
		runtimeTmuxSessionNameForTest(ws.Id, "helper"),
		stored[0].SessionName,
	)
}

func TestWorkspaceResponseUsesStoredRuntimeTmuxSessionsAfterRestartE2E(
	t *testing.T,
) {
	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	tmuxPath := filepath.Join(dir, "fake-tmux")
	require.NoError(os.WriteFile(tmuxPath, []byte(`#!/bin/sh
target=""
mode=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-t" ]; then target="$a"; fi
  if [ "$a" = "display-message" ]; then mode="display-message"; fi
  if [ "$a" = "capture-pane" ]; then mode="capture-pane"; fi
  if [ "$a" = "list-sessions" ]; then
    printf '%s\n' "$TMUX_LIVE_SESSIONS"
    exit 0
  fi
  prev="$a"
done
if [ "$mode" = "display-message" ]; then
  case "$target" in
    *-claude) printf '⠴ claude-activity\n' ;;
    *) printf 'idle\n' ;;
  esac
  exit 0
fi
if [ "$mode" = "capture-pane" ]; then
  printf 'stable\n'
  exit 0
fi
exit 0
`), 0o755))
	cfg := &config.Config{Tmux: config.Tmux{Command: []string{tmuxPath}}}
	client, database, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)
	require.NotEmpty(ws.TmuxSession)
	t.Setenv(
		"TMUX_LIVE_SESSIONS",
		strings.Join([]string{
			ws.TmuxSession,
			ws.TmuxSession + "-codex",
			ws.TmuxSession + "-claude",
		}, "\n"),
	)
	require.NoError(database.UpsertWorkspaceTmuxSession(
		ctx,
		&db.WorkspaceTmuxSession{
			WorkspaceID: ws.Id,
			SessionName: ws.TmuxSession + "-codex",
			TargetKey:   "codex",
		},
	))
	require.NoError(database.UpsertWorkspaceTmuxSession(
		ctx,
		&db.WorkspaceTmuxSession{
			WorkspaceID: ws.Id,
			SessionName: ws.TmuxSession + "-claude",
			TargetKey:   "claude",
		},
	))

	listResp, err := client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Workspaces)

	var listed *generated.WorkspaceResponse
	for i := range *listResp.JSON200.Workspaces {
		if (*listResp.JSON200.Workspaces)[i].Id == ws.Id {
			listed = &(*listResp.JSON200.Workspaces)[i]
			break
		}
	}
	require.NotNil(listed)
	assert.True(listed.TmuxWorking)
	assert.Equal(tmuxActivitySourceTitle, listed.TmuxActivitySource)
	require.NotNil(listed.TmuxPaneTitle)
	assert.Equal("⠴ claude-activity", *listed.TmuxPaneTitle)
}

func TestWorkspaceDeleteStopsRuntimeSessionsE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	assert := Assert.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("sleep"),
	}}, Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions}}
	client, _, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)

	shellResp, err := client.HTTP.EnsureWorkspaceRuntimeShellWithResponse(
		ctx, ws.Id,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, shellResp.StatusCode())

	require.Len(srv.runtime.ListSessions(ws.Id), 1)
	require.NotNil(srv.runtime.ShellSession(ws.Id))

	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, ws.Id,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, delResp.StatusCode())

	assert.Empty(srv.runtime.ListSessions(ws.Id))
	assert.Nil(srv.runtime.ShellSession(ws.Id))
}

// TestWorkspaceDeleteDirtyKeepsRuntimeSessionsE2E covers the case where the
// workspace is dirty and delete is rejected with 409. Runtime sessions must
// survive — killing them on a delete that didn't actually happen would leave
// the user with a workspace whose agent and shell were silently terminated.
func TestWorkspaceDeleteDirtyKeepsRuntimeSessionsE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	assert := Assert.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("sleep"),
	}}, Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions}}
	client, _, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	shellResp, err := client.HTTP.EnsureWorkspaceRuntimeShellWithResponse(
		ctx, ws.Id,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, shellResp.StatusCode())
	require.Len(srv.runtime.ListSessions(ws.Id), 1)
	require.NotNil(srv.runtime.ShellSession(ws.Id))

	// Make the worktree dirty so a non-forced delete will be rejected.
	require.NoError(os.WriteFile(
		filepath.Join(ws.WorktreePath, "dirty.txt"),
		[]byte("uncommitted\n"), 0o644,
	))

	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, ws.Id, &generated.DeleteWorkspaceParams{},
	)
	require.NoError(err)
	require.Equal(http.StatusConflict, delResp.StatusCode())

	// The 409 must not have killed the runtime sessions.
	assert.Len(srv.runtime.ListSessions(ws.Id), 1)
	assert.NotNil(srv.runtime.ShellSession(ws.Id))
}

// TestWorkspaceListReportsCommitsAheadBehindE2E verifies that the
// /api/v1/workspaces list response includes commits_ahead /
// commits_behind for ready workspaces, computed against the worktree's
// `@{upstream}` tracking branch. The sidebar's push-state pills depend
// on these fields, so a regression here would silently turn the pills
// off without any test failure at the unit-test layer.
func TestWorkspaceListReportsCommitsAheadBehindE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, nil)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	// runGit strips global/system git config, so the workspace's
	// worktree has no committer identity. Set one locally so the
	// commits below succeed in CI as well as on developer machines.
	runGit(t, ws.WorktreePath, "config", "user.email", "test@test.com")
	runGit(t, ws.WorktreePath, "config", "user.name", "Test")

	// Add two local commits in the worktree so HEAD is ahead of
	// origin/feature by 2.
	require.NoError(os.WriteFile(
		filepath.Join(ws.WorktreePath, "ahead-1.txt"),
		[]byte("a1\n"), 0o644,
	))
	runGit(t, ws.WorktreePath, "add", ".")
	runGit(t, ws.WorktreePath, "commit", "-m", "ahead 1")
	require.NoError(os.WriteFile(
		filepath.Join(ws.WorktreePath, "ahead-2.txt"),
		[]byte("a2\n"), 0o644,
	))
	runGit(t, ws.WorktreePath, "add", ".")
	runGit(t, ws.WorktreePath, "commit", "-m", "ahead 2")

	listResp, err := client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Workspaces)

	var found *generated.WorkspaceResponse
	for i := range *listResp.JSON200.Workspaces {
		entry := &(*listResp.JSON200.Workspaces)[i]
		if entry.Id == ws.Id {
			found = entry
			break
		}
	}
	require.NotNil(found, "workspace %s missing from list", ws.Id)
	require.NotNil(
		found.CommitsAhead,
		"commits_ahead must be populated for a ready workspace",
	)
	require.NotNil(
		found.CommitsBehind,
		"commits_behind must be populated for a ready workspace",
	)
	assert.Equal(int64(2), *found.CommitsAhead)
	assert.Equal(int64(0), *found.CommitsBehind)
}

func TestWorkspaceListPrunesMissingTmuxSessionsE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	if testing.Short() {
		t.Skip("workspace e2e tests skipped in short mode")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "list-sessions" ]; then` + "\n" +
		`    printf 'middleman-0000000000000001\nmiddleman-0000000000000002-e81d3b0e9d82feaa\n'` + "\n" +
		`    exit 0` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	cfg := &config.Config{
		Tmux: config.Tmux{Command: []string{script}},
	}
	client, database, _, _, _ := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()

	require.NoError(database.InsertWorkspace(ctx, &db.Workspace{
		ID:           "0000000000000002",
		PlatformHost: "github.com",
		RepoOwner:    "acme",
		RepoName:     "widget",
		ItemType:     db.WorkspaceItemTypePullRequest,
		ItemNumber:   1,
		GitHeadRef:   "feature/stale",
		WorktreePath: filepath.Join(dir, "stale"),
		TmuxSession:  "middleman-0000000000000002",
		Status:       "ready",
	}))
	runtimeSession := runtimeTmuxSessionNameForTest(
		"0000000000000002", "helper",
	)
	require.NoError(database.UpsertWorkspaceTmuxSession(
		ctx,
		&db.WorkspaceTmuxSession{
			WorkspaceID: "0000000000000002",
			SessionName: runtimeSession,
			TargetKey:   "helper",
		},
	))

	listResp, err := client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Workspaces)
	require.Len(*listResp.JSON200.Workspaces, 1)
	got := (*listResp.JSON200.Workspaces)[0]
	assert.Equal("0000000000000002", got.Id)
	assert.Equal("error", got.Status)
	require.NotNil(got.ErrorMessage)
	assert.Contains(*got.ErrorMessage, "tmux session is no longer running")

	stored, err := database.GetWorkspace(ctx, "0000000000000002")
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("error", stored.Status)
	runtimeRows, err := database.ListWorkspaceTmuxSessions(
		ctx, "0000000000000002",
	)
	require.NoError(err)
	require.Len(runtimeRows, 1)
	assert.Equal(runtimeSession, runtimeRows[0].SessionName)
}

func TestWorkspaceRuntimeEnsureShellE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _, _ := setupTestServerWithWorkspacesServer(t, nil)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	shellResp, err := client.HTTP.EnsureWorkspaceRuntimeShellWithResponse(
		ctx, ws.Id,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, shellResp.StatusCode())
	require.NotNil(shellResp.JSON200)
	shell := shellResp.JSON200
	assert.Equal("plain_shell", shell.TargetKey)
	assert.Equal(string(localruntime.LaunchTargetPlainShell), shell.Kind)
	assert.Equal(string(localruntime.SessionStatusRunning), shell.Status)

	getResp, err := client.HTTP.GetWorkspaceRuntimeWithResponse(ctx, ws.Id)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	require.NotNil(getResp.JSON200.ShellSession)
	require.NotNil(getResp.JSON200.Sessions)
	assert.Equal(shell.Key, getResp.JSON200.ShellSession.Key)
	assert.Empty(*getResp.JSON200.Sessions)
}

func TestWorkspaceRuntimeSessionTerminalWebSocketE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("echo"),
	}}, Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions}}
	client, _, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	session := launchResp.JSON200

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/ws/v1/workspaces/" + ws.Id +
		"/runtime/sessions/" + session.Key + "/terminal"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(err)
	defer conn.Close(websocket.StatusNormalClosure, "done")

	require.NoError(conn.Write(
		ctx, websocket.MessageBinary, []byte("ping\n"),
	))
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var got strings.Builder
	for {
		typ, data, readErr := conn.Read(readCtx)
		if readErr != nil {
			break
		}
		if typ != websocket.MessageBinary {
			continue
		}
		got.WriteString(string(data))
		if strings.Contains(got.String(), "echo:ping") {
			return
		}
	}
	require.Contains(got.String(), "echo:ping")
}

func TestWorkspaceRuntimeSessionTerminalWebSocketBasePathE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{
		BasePath: "/middleman/",
		Agents: []config.Agent{{
			Key:     "helper",
			Label:   "Helper",
			Command: serverRuntimeHelperCommand("echo"),
		}},
		Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions},
	}
	client, _, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	session := launchResp.JSON200

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/middleman/ws/v1/workspaces/" + ws.Id +
		"/runtime/sessions/" + session.Key + "/terminal"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(err)
	defer conn.Close(websocket.StatusNormalClosure, "done")

	require.NoError(conn.Write(
		ctx, websocket.MessageBinary, []byte("ping\n"),
	))
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var got strings.Builder
	for {
		typ, data, readErr := conn.Read(readCtx)
		if readErr != nil {
			break
		}
		if typ != websocket.MessageBinary {
			continue
		}
		got.WriteString(string(data))
		if strings.Contains(got.String(), "echo:ping") {
			return
		}
	}
	require.Contains(got.String(), "echo:ping")
}

func TestWorkspaceRuntimeSessionTerminalSkipsAltScreenReplayE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	assert := Assert.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("altscreen"),
	}}, Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions}}
	client, _, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	session := launchResp.JSON200
	time.Sleep(100 * time.Millisecond)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/ws/v1/workspaces/" + ws.Id +
		"/runtime/sessions/" + session.Key + "/terminal"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(err)
	defer conn.Close(websocket.StatusNormalClosure, "done")

	type terminalRead struct {
		typ  websocket.MessageType
		data []byte
		err  error
	}
	reads := make(chan terminalRead, 1)
	readOnce := func() {
		go func() {
			typ, data, readErr := conn.Read(context.Background())
			reads <- terminalRead{typ: typ, data: data, err: readErr}
		}()
	}
	readOnce()
	select {
	case read := <-reads:
		require.NoError(read.err)
		require.Empty(
			string(read.data),
			"late attach must not replay stale alternate-screen output",
		)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(conn.Write(
		ctx, websocket.MessageBinary, []byte("paint\n"),
	))
	var got strings.Builder
	deadline := time.After(2 * time.Second)
	for {
		select {
		case read := <-reads:
			require.NoError(read.err)
			if read.typ == websocket.MessageBinary {
				got.WriteString(string(read.data))
			}
			if strings.Contains(got.String(), "live:paint") {
				break
			}
			readOnce()
			continue
		case <-deadline:
			require.Contains(got.String(), "live:paint")
		}
		break
	}
	assert.NotContains(got.String(), "codex screen")
	require.Contains(got.String(), "live:paint")
}

func TestWorkspaceRuntimeSessionTerminalAppliesInitialSizeE2E(t *testing.T) {
	t.Setenv("MIDDLEMAN_SERVER_RUNTIME_HELPER", "1")

	require := require.New(t)
	disableTmuxAgentSessions := false
	cfg := &config.Config{Agents: []config.Agent{{
		Key:     "helper",
		Label:   "Helper",
		Command: serverRuntimeHelperCommand("size"),
	}}, Tmux: config.Tmux{AgentSessions: &disableTmuxAgentSessions}}
	client, _, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	session := launchResp.JSON200

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/ws/v1/workspaces/" + ws.Id +
		"/runtime/sessions/" + session.Key +
		"/terminal?cols=177&rows=41"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(err)
	defer conn.Close(websocket.StatusNormalClosure, "done")

	require.NoError(conn.Write(
		ctx, websocket.MessageBinary, []byte("size\n"),
	))
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var got strings.Builder
	for {
		typ, data, readErr := conn.Read(readCtx)
		if readErr != nil {
			break
		}
		if typ != websocket.MessageBinary {
			continue
		}
		got.WriteString(string(data))
		if strings.Contains(got.String(), "size:41:177") {
			return
		}
	}
	require.Contains(got.String(), "size:41:177")
}

func TestWorkspaceRuntimeSessionTerminalTmuxBackedWebSocketE2E(
	t *testing.T,
) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not available")
	}

	require := require.New(t)
	assert := Assert.New(t)
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "size-agent")
	require.NoError(os.WriteFile(agentPath, []byte(`#!/bin/sh
IFS= read -r line
set -- $(stty size 2>/dev/null || printf '0 0')
printf 'size:%s:%s:%s\n' "$1" "$2" "$line"
`), 0o755))
	cfg := &config.Config{
		Agents: []config.Agent{{
			Key:     "helper",
			Label:   "Helper",
			Command: []string{agentPath},
		}},
		Tmux: config.Tmux{Command: []string{tmuxPath}},
	}
	client, database, _, _, srv := setupTestServerWithWorkspacesServer(t, cfg)
	ctx := context.Background()
	ws := createReadyWorkspace(t, ctx, client)

	launchResp, err := client.HTTP.LaunchWorkspaceRuntimeSessionWithResponse(
		ctx, ws.Id,
		generated.LaunchWorkspaceRuntimeSessionInputBody{
			TargetKey: "helper",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, launchResp.StatusCode())
	require.NotNil(launchResp.JSON200)
	session := launchResp.JSON200
	stored, err := database.ListWorkspaceTmuxSessions(ctx, ws.Id)
	require.NoError(err)
	require.Len(stored, 1)
	assert.Equal(
		runtimeTmuxSessionNameForTest(ws.Id, "helper"),
		stored[0].SessionName,
	)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/ws/v1/workspaces/" + ws.Id +
		"/runtime/sessions/" + session.Key +
		"/terminal?cols=177&rows=41"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(err)
	defer conn.Close(websocket.StatusNormalClosure, "done")

	require.NoError(conn.Write(
		ctx, websocket.MessageBinary, []byte("size\n"),
	))
	readCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var got strings.Builder
	for {
		typ, data, readErr := conn.Read(readCtx)
		if readErr != nil {
			break
		}
		if typ != websocket.MessageBinary {
			continue
		}
		got.WriteString(string(data))
		// tmux keeps one row for its status line by default, so the
		// pane sees one fewer row than the attached terminal while
		// preserving the requested column count.
		if strings.Contains(got.String(), "size:40:177:size") {
			return
		}
	}
	require.Contains(got.String(), "size:40:177:size")
}

func createReadyWorkspace(
	t *testing.T,
	ctx context.Context,
	client *apiclient.Client,
) *generated.WorkspaceResponse {
	t.Helper()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, createResp.StatusCode())
	require.NotNil(t, createResp.JSON202)
	return waitForWorkspaceReady(t, ctx, client, createResp.JSON202.Id)
}

func assertWorkspaceRuntimeTarget(
	t *testing.T,
	targets []generated.LaunchTarget,
	key string,
) {
	t.Helper()

	for _, target := range targets {
		if target.Key == key {
			return
		}
	}
	require.Failf(t, "runtime target not found", "key %q", key)
}

func serverRuntimeHelperCommand(mode string) []string {
	return []string{
		os.Args[0],
		"-test.run=TestServerRuntimeHelperProcess",
		"--",
		mode,
	}
}

func TestServerRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("MIDDLEMAN_SERVER_RUNTIME_HELPER") != "1" {
		return
	}
	args := os.Args
	mode := args[len(args)-1]
	switch mode {
	case "sleep":
		select {}
	case "echo":
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err == nil {
			fmt.Print("echo:" + line)
		}
		select {}
	case "altscreen":
		fmt.Print("\x1b[?1049h\x1b[Hcodex screen")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err == nil {
			fmt.Print("\x1b[Hlive:" + line)
		}
		select {}
	case "size":
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err == nil {
			rows, cols, sizeErr := pty.Getsize(os.Stdin)
			if sizeErr == nil {
				fmt.Printf("size:%d:%d:%s", rows, cols, line)
			}
		}
		return
	default:
		os.Exit(2)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(
		gitenv.StripAll(os.Environ()),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
	return strings.TrimSpace(string(out))
}

type rawWorkspaceStatusResponse struct {
	ID           string  `json:"id"`
	PlatformHost string  `json:"platform_host"`
	RepoOwner    string  `json:"repo_owner"`
	RepoName     string  `json:"repo_name"`
	ItemType     string  `json:"item_type"`
	ItemNumber   int     `json:"item_number"`
	GitHeadRef   string  `json:"git_head_ref"`
	WorktreePath string  `json:"worktree_path"`
	TmuxSession  string  `json:"tmux_session"`
	Status       string  `json:"status"`
	ErrorMessage *string `json:"error_message"`
}

type rawIssueWorkspaceRef struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type rawIssueSummary struct {
	Title string `json:"title"`
	State string `json:"state"`
}

type rawIssueDetailResponse struct {
	Issue        *rawIssueSummary      `json:"issue"`
	PlatformHost string                `json:"platform_host"`
	RepoOwner    string                `json:"repo_owner"`
	RepoName     string                `json:"repo_name"`
	Workspace    *rawIssueWorkspaceRef `json:"workspace"`
}

type rawProblemDetail struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Errors []struct {
		Message  string `json:"message"`
		Location string `json:"location"`
		Value    any    `json:"value"`
	} `json:"errors"`
}

func TestWorkspaceCRUDE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	// 1. List workspaces -- initially empty.
	listResp, err := client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Workspaces)
	assert.Empty(*listResp.JSON200.Workspaces)

	// 2. Create workspace.
	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id
	assert.NotEmpty(wsID)
	assert.Equal("github.com", createResp.JSON202.PlatformHost)
	assert.Equal("acme", createResp.JSON202.RepoOwner)
	assert.Equal("widget", createResp.JSON202.RepoName)
	assert.Equal(db.WorkspaceItemTypePullRequest, createResp.JSON202.ItemType)
	assert.Equal(int64(1), createResp.JSON202.ItemNumber)

	// 3. Get workspace by ID.
	getResp, err := client.HTTP.GetWorkspacesByIdWithResponse(
		ctx, wsID,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	assert.Equal(wsID, getResp.JSON200.Id)

	// 4. List workspaces -- now has one.
	listResp2, err := client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp2.StatusCode())
	require.NotNil(listResp2.JSON200)
	require.NotNil(listResp2.JSON200.Workspaces)
	assert.Len(*listResp2.JSON200.Workspaces, 1)

	// 5. Delete workspace (force).
	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID, &generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, delResp.StatusCode())

	// 6. Verify deleted -- GET returns 404.
	getResp2, err := client.HTTP.GetWorkspacesByIdWithResponse(
		ctx, wsID,
	)
	require.NoError(err)
	require.Equal(http.StatusNotFound, getResp2.StatusCode())
}

func TestWorkspaceRetryErroredWorkspaceE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, database, _, _ := setupTestServerWithWorkspaces(t)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id
	waitForWorkspaceReady(t, ctx, client, wsID)

	msg := "ensure clone: git fetch: fork/exec /opt/homebrew/bin/git: resource temporarily unavailable"
	err = database.UpdateWorkspaceStatus(ctx, wsID, "error", &msg)
	require.NoError(err)

	retryResp, err := client.HTTP.RetryWorkspaceWithResponse(ctx, wsID)
	require.NoError(err)
	require.Equal(http.StatusAccepted, retryResp.StatusCode())
	require.NotNil(retryResp.JSON202)
	retryBody := retryResp.JSON202
	assert.Equal(wsID, retryBody.Id)
	assert.Equal("creating", retryBody.Status)
	assert.Nil(retryBody.ErrorMessage)

	ready := waitForWorkspaceReady(t, ctx, client, wsID)
	assert.Equal(wsID, ready.Id)
	assert.Nil(ready.ErrorMessage)
}

func TestWorkspaceRetryReadyWorkspaceConflictE2E(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, database, _, _ := setupTestServerWithWorkspaces(t)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	waitForWorkspaceReady(t, ctx, client, wsID)
	before, err := database.GetWorkspace(ctx, wsID)
	require.NoError(err)
	require.NotNil(before)
	require.Equal("ready", before.Status)
	require.Nil(before.ErrorMessage)
	require.NotEmpty(before.WorktreePath)
	beforeEvents, err := database.ListWorkspaceSetupEvents(ctx, wsID)
	require.NoError(err)

	retryResp, err := client.HTTP.RetryWorkspaceWithResponse(ctx, wsID)
	require.NoError(err)
	require.Equal(http.StatusConflict, retryResp.StatusCode())

	after, err := database.GetWorkspace(ctx, wsID)
	require.NoError(err)
	require.NotNil(after)
	assert.Equal("ready", after.Status)
	assert.Nil(after.ErrorMessage)
	assert.Equal(before.WorktreePath, after.WorktreePath)
	assert.Equal(before.WorkspaceBranch, after.WorkspaceBranch)

	afterEvents, err := database.ListWorkspaceSetupEvents(ctx, wsID)
	require.NoError(err)
	assert.Len(afterEvents, len(beforeEvents))
}

func TestWorkspaceCreateNotFound(t *testing.T) {
	require := require.New(t)

	client, _, _, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	// Non-existent repo.
	resp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "nope",
			Name:         "missing",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusNotFound, resp.StatusCode())

	// Existing repo, non-existent MR.
	resp2, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     999,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusNotFound, resp2.StatusCode())
}

func TestWorkspaceMRDetailHasWorkspace(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, _, _, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	// Create a workspace for PR #1.
	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	// MR detail should include the workspace reference.
	mrResp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, mrResp.StatusCode())
	require.NotNil(mrResp.JSON200)
	require.NotNil(mrResp.JSON200.Workspace)
	assert.Equal(wsID, mrResp.JSON200.Workspace.Id)
	assert.NotEmpty(mrResp.JSON200.Workspace.Status)

	// Clean up: delete the workspace.
	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, delResp.StatusCode())
}

func TestWorkspaceCreateDuplicate(t *testing.T) {
	require := require.New(t)

	client, _, _, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	body := generated.CreateWorkspaceInputBody{
		PlatformHost: "github.com",
		Owner:        "acme",
		Name:         "widget",
		MrNumber:     1,
	}

	// First create succeeds.
	resp1, err := client.HTTP.CreateWorkspaceWithResponse(ctx, body)
	require.NoError(err)
	require.Equal(http.StatusAccepted, resp1.StatusCode())

	// Duplicate create returns 409.
	resp2, err := client.HTTP.CreateWorkspaceWithResponse(ctx, body)
	require.NoError(err)
	require.Equal(http.StatusConflict, resp2.StatusCode())
}

func TestWorkspaceCreateIssueE2E(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	fixture := setupWorkspaceServerFixture(t, nil)
	ctx := context.Background()

	seedIssue(t, fixture.database, "acme", "widget", 7, "open")

	createRR := doJSON(
		t,
		fixture.server,
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/workspace",
		map[string]string{"platform_host": "github.com"},
	)
	require.Equal(http.StatusAccepted, createRR.Code, createRR.Body.String())

	var created rawWorkspaceStatusResponse
	require.NoError(json.NewDecoder(createRR.Body).Decode(&created))
	require.NotEmpty(created.ID)
	assert.Equal("issue", created.ItemType)
	assert.Equal(7, created.ItemNumber)
	assert.Equal("middleman/issue-7", created.GitHeadRef)

	ready := waitForWorkspaceReady(t, ctx, fixture.client, created.ID)
	assert.Equal(
		"middleman/issue-7",
		gitOutput(t, ready.WorktreePath, "branch", "--show-current"),
	)
	assert.Equal(
		testGitSHA(t, fixture.remote, "refs/heads/main"),
		testGitSHA(t, ready.WorktreePath, "HEAD"),
	)

	getIssueRR := doJSON(
		t,
		fixture.server,
		http.MethodGet,
		"/api/v1/repos/acme/widget/issues/7",
		nil,
	)
	require.Equal(http.StatusOK, getIssueRR.Code, getIssueRR.Body.String())

	var issueDetail rawIssueDetailResponse
	require.NoError(json.NewDecoder(getIssueRR.Body).Decode(&issueDetail))
	require.NotNil(issueDetail.Workspace)
	assert.Equal(created.ID, issueDetail.Workspace.ID)
	assert.NotEmpty(issueDetail.Workspace.Status)
}

func TestWorkspaceCreateIssueIsIdempotent(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	fixture := setupWorkspaceServerFixture(t, nil)
	seedIssue(t, fixture.database, "acme", "widget", 7, "open")

	path := "/api/v1/repos/acme/widget/issues/7/workspace"
	body := map[string]string{"platform_host": "github.com"}

	firstRR := doJSON(
		t, fixture.server, http.MethodPost, path, body,
	)
	require.Equal(http.StatusAccepted, firstRR.Code, firstRR.Body.String())

	var first rawWorkspaceStatusResponse
	require.NoError(json.NewDecoder(firstRR.Body).Decode(&first))
	require.NotEmpty(first.ID)

	secondRR := doJSON(
		t, fixture.server, http.MethodPost, path, body,
	)
	require.Equal(http.StatusAccepted, secondRR.Code, secondRR.Body.String())

	var second rawWorkspaceStatusResponse
	require.NoError(json.NewDecoder(secondRR.Body).Decode(&second))
	assert.Equal(first.ID, second.ID)
	assert.Equal("issue", second.ItemType)
	assert.Equal(7, second.ItemNumber)
}

func TestWorkspaceCreateIssueAfterDeleteRecreatesBranch(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	fixture := setupWorkspaceServerFixture(t, nil)
	ctx := context.Background()

	seedIssue(t, fixture.database, "acme", "widget", 7, "open")

	createRR := doJSON(
		t,
		fixture.server,
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/workspace",
		map[string]string{"platform_host": "github.com"},
	)
	require.Equal(http.StatusAccepted, createRR.Code, createRR.Body.String())

	var created rawWorkspaceStatusResponse
	require.NoError(json.NewDecoder(createRR.Body).Decode(&created))
	ready := waitForWorkspaceReady(t, ctx, fixture.client, created.ID)
	assert.Equal(
		"middleman/issue-7",
		gitOutput(t, ready.WorktreePath, "branch", "--show-current"),
	)

	force := true
	deleteResp, err := fixture.client.HTTP.DeleteWorkspaceWithResponse(
		ctx,
		created.ID,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, deleteResp.StatusCode())

	recreateRR := doJSON(
		t,
		fixture.server,
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/workspace",
		map[string]string{"platform_host": "github.com"},
	)
	require.Equal(http.StatusAccepted, recreateRR.Code, recreateRR.Body.String())

	var recreated rawWorkspaceStatusResponse
	require.NoError(json.NewDecoder(recreateRR.Body).Decode(&recreated))
	recreatedReady := waitForWorkspaceReady(t, ctx, fixture.client, recreated.ID)
	assert.Equal(
		"middleman/issue-7",
		gitOutput(t, recreatedReady.WorktreePath, "branch", "--show-current"),
	)
}

func TestWorkspaceCreatePRAndIssueCanCoexistForSameRepoNumber(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	fixture := setupWorkspaceServerFixture(t, nil)
	ctx := context.Background()

	seedIssue(t, fixture.database, "acme", "widget", 1, "open")

	prResp, err := fixture.client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, prResp.StatusCode())
	require.NotNil(prResp.JSON202)
	assert.Equal("pull_request", prResp.JSON202.ItemType)
	assert.Equal(int64(1), prResp.JSON202.ItemNumber)

	issueResp, err := fixture.client.HTTP.CreateIssueWorkspaceWithResponse(
		ctx,
		"acme",
		"widget",
		1,
		generated.CreateIssueWorkspaceInputBody{
			PlatformHost: "github.com",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, issueResp.StatusCode())
	require.NotNil(issueResp.JSON202)
	assert.Equal("issue", issueResp.JSON202.ItemType)
	assert.Equal(int64(1), issueResp.JSON202.ItemNumber)
	assert.NotEqual(prResp.JSON202.Id, issueResp.JSON202.Id)

	listResp, err := fixture.client.HTTP.GetWorkspacesWithResponse(ctx)
	require.NoError(err)
	require.Equal(http.StatusOK, listResp.StatusCode())
	require.NotNil(listResp.JSON200)
	require.NotNil(listResp.JSON200.Workspaces)
	require.Len(*listResp.JSON200.Workspaces, 2)
}

func TestWorkspaceCreateIssueBranchConflictReturnsTyped409(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	fixture := setupWorkspaceServerFixture(t, nil)
	ctx := context.Background()

	seedIssue(t, fixture.database, "acme", "widget", 7, "open")

	mainSHA := testGitSHA(t, fixture.remote, "refs/heads/main")
	runGit(
		t,
		fixture.bare,
		"update-ref",
		"refs/heads/middleman/issue-7",
		mainSHA,
	)

	conflictRR := doJSON(
		t,
		fixture.server,
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/workspace",
		map[string]string{"platform_host": "github.com"},
	)
	require.Equal(http.StatusConflict, conflictRR.Code, conflictRR.Body.String())

	var problem rawProblemDetail
	require.NoError(json.NewDecoder(conflictRR.Body).Decode(&problem))
	assert.Equal(
		"urn:middleman:error:issue-workspace-branch-conflict",
		problem.Type,
	)
	assert.Equal(http.StatusConflict, problem.Status)
	assert.NotEmpty(problem.Detail)

	locations := map[string]any{}
	for _, errDetail := range problem.Errors {
		locations[errDetail.Location] = errDetail.Value
	}
	assert.Equal("middleman/issue-7", locations["body.git_head_ref"])
	assert.Equal(
		"middleman/issue-7-2",
		locations["body.suggested_git_head_ref"],
	)

	reuseRR := doJSON(
		t,
		fixture.server,
		http.MethodPost,
		"/api/v1/repos/acme/widget/issues/7/workspace",
		map[string]any{
			"platform_host":         "github.com",
			"git_head_ref":          "middleman/issue-7",
			"reuse_existing_branch": true,
		},
	)
	require.Equal(http.StatusAccepted, reuseRR.Code, reuseRR.Body.String())

	var reused rawWorkspaceStatusResponse
	require.NoError(json.NewDecoder(reuseRR.Body).Decode(&reused))
	reusedReady := waitForWorkspaceReady(t, ctx, fixture.client, reused.ID)
	assert.Equal(
		"middleman/issue-7",
		gitOutput(t, reusedReady.WorktreePath, "branch", "--show-current"),
	)

	stored, err := fixture.database.GetWorkspace(ctx, reused.ID)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("middleman/issue-7", stored.WorkspaceBranch)
}

func TestWorkspaceCreateUsesPRBranchAndFallbackBranch(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client, database, clonePath, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	seedPR(t, database, "acme", "widget", 2)

	createResp1, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp1.StatusCode())
	require.NotNil(createResp1.JSON202)

	ws1 := waitForWorkspaceReady(t, ctx, client, createResp1.JSON202.Id)
	assert.Equal(
		"feature",
		gitOutput(t, ws1.WorktreePath, "branch", "--show-current"),
	)
	assert.Equal(
		"origin",
		gitOutput(
			t, ws1.WorktreePath,
			"config", "--get", "branch.feature.remote",
		),
	)
	assert.Equal(
		"refs/heads/feature",
		gitOutput(
			t, ws1.WorktreePath,
			"config", "--get", "branch.feature.merge",
		),
	)
	runGit(t, clonePath, "fetch", "--prune", "origin")

	createResp2, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     2,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp2.StatusCode())
	require.NotNil(createResp2.JSON202)

	ws2 := waitForWorkspaceReady(t, ctx, client, createResp2.JSON202.Id)
	assert.Equal(
		"middleman/pr-2",
		gitOutput(t, ws2.WorktreePath, "branch", "--show-current"),
	)
	assert.Equal(
		testGitSHA(t, ws1.WorktreePath, "HEAD"),
		testGitSHA(t, ws2.WorktreePath, "HEAD"),
	)
}

func TestWorkspaceDeleteRecreatesForkBranchName(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client, database, _, remotePath := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	repo, err := database.GetRepoByHostOwnerName(
		ctx, "github.com", "acme", "widget",
	)
	require.NoError(err)
	require.NotNil(repo)

	headSHA := testGitSHA(t, remotePath, "feature")
	runGit(t, remotePath, "update-ref", "refs/pull/2/head", headSHA)

	now := time.Now().UTC().Truncate(time.Second)
	forkPR := &db.MergeRequest{
		RepoID:           repo.ID,
		PlatformID:       2000,
		Number:           2,
		URL:              "https://github.com/acme/widget/pull/2",
		Title:            "Fork PR #2",
		Author:           "fork-user",
		State:            "open",
		Body:             "fork test body",
		HeadBranch:       "fork-feature",
		BaseBranch:       "main",
		HeadRepoCloneURL: "https://github.com/fork/widget.git",
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActivityAt:   now,
	}
	prID, err := database.UpsertMergeRequest(ctx, forkPR)
	require.NoError(err)
	require.NoError(database.EnsureKanbanState(ctx, prID))

	create1, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     2,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, create1.StatusCode())
	require.NotNil(create1.JSON202)

	ws1 := waitForWorkspaceReady(t, ctx, client, create1.JSON202.Id)
	assert.Equal(
		"fork-feature",
		gitOutput(t, ws1.WorktreePath, "branch", "--show-current"),
	)

	force := true
	delete1, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, create1.JSON202.Id,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, delete1.StatusCode())

	create2, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     2,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, create2.StatusCode())
	require.NotNil(create2.JSON202)

	ws2 := waitForWorkspaceReady(t, ctx, client, create2.JSON202.Id)
	assert.Equal(
		"fork-feature",
		gitOutput(t, ws2.WorktreePath, "branch", "--show-current"),
	)
	assert.Equal(
		headSHA,
		testGitSHA(t, ws2.WorktreePath, "HEAD"),
	)
}

func TestWorkspaceDeletePreservesUserCreatedBranch(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client, _, clonePath, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)

	ws := waitForWorkspaceReady(t, ctx, client, createResp.JSON202.Id)
	runGit(t, ws.WorktreePath, "checkout", "-b", "user-scratch")
	scratchSHA := testGitSHA(t, ws.WorktreePath, "HEAD")

	force := true
	deleteResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, createResp.JSON202.Id,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, deleteResp.StatusCode())

	assert.Equal(
		scratchSHA,
		testGitSHA(t, clonePath, "refs/heads/user-scratch"),
	)
}

func TestWorkspaceCreatePreservesExistingLocalPreferredBranch(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client, _, clonePath, remotePath := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	privateClone := filepath.Join(t.TempDir(), "private-clone")
	runGit(t, filepath.Dir(privateClone), "clone", clonePath, privateClone)
	runGit(t, privateClone, "config", "user.email", "test@test.com")
	runGit(t, privateClone, "config", "user.name", "Test")
	runGit(t, privateClone, "checkout", "feature")

	require.NoError(os.WriteFile(
		filepath.Join(privateClone, "private.txt"),
		[]byte("private\n"), 0o644,
	))
	runGit(t, privateClone, "add", "private.txt")
	runGit(t, privateClone, "commit", "-m", "private commit")
	privateSHA := testGitSHA(t, privateClone, "HEAD")
	runGit(t, privateClone, "push", clonePath, "HEAD:feature")

	originSHA := testGitSHA(t, remotePath, "refs/heads/feature")
	assert.NotEqual(originSHA, privateSHA)
	assert.Equal(privateSHA, testGitSHA(t, clonePath, "refs/heads/feature"))

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)

	ws := waitForWorkspaceReady(t, ctx, client, createResp.JSON202.Id)
	assert.Equal(
		"middleman/pr-1",
		gitOutput(t, ws.WorktreePath, "branch", "--show-current"),
	)
	assert.Equal(originSHA, testGitSHA(t, ws.WorktreePath, "HEAD"))
	assert.Equal(privateSHA, testGitSHA(t, clonePath, "refs/heads/feature"))

	force := true
	deleteResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, createResp.JSON202.Id,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, deleteResp.StatusCode())

	assert.Equal(privateSHA, testGitSHA(t, clonePath, "refs/heads/feature"))
}

func TestWorkspaceDeleteLegacySyntheticBranchAllowsRecreate(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client, database, clonePath, remotePath := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	privateClone := filepath.Join(t.TempDir(), "legacy-private-clone")
	runGit(t, filepath.Dir(privateClone), "clone", clonePath, privateClone)
	runGit(t, privateClone, "config", "user.email", "test@test.com")
	runGit(t, privateClone, "config", "user.name", "Test")
	runGit(t, privateClone, "checkout", "feature")
	require.NoError(os.WriteFile(
		filepath.Join(privateClone, "legacy-private.txt"),
		[]byte("legacy private\n"), 0o644,
	))
	runGit(t, privateClone, "add", "legacy-private.txt")
	runGit(t, privateClone, "commit", "-m", "legacy private commit")
	privateSHA := testGitSHA(t, privateClone, "HEAD")
	runGit(t, privateClone, "push", clonePath, "HEAD:feature")
	originSHA := testGitSHA(t, remotePath, "refs/heads/feature")
	assert.NotEqual(originSHA, privateSHA)

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)

	ws := waitForWorkspaceReady(t, ctx, client, createResp.JSON202.Id)
	assert.Equal(
		"middleman/pr-1",
		gitOutput(t, ws.WorktreePath, "branch", "--show-current"),
	)

	_, err = database.WriteDB().ExecContext(ctx, `
		UPDATE middleman_workspaces
		SET workspace_branch = '__middleman_unknown__'
		WHERE id = ?`,
		createResp.JSON202.Id,
	)
	require.NoError(err)

	force := true
	deleteResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, createResp.JSON202.Id,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, deleteResp.StatusCode())

	runGit(t, clonePath, "fetch", "--prune", "origin")

	recreateResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, recreateResp.StatusCode())
	require.NotNil(recreateResp.JSON202)

	recreated := waitForWorkspaceReady(t, ctx, client, recreateResp.JSON202.Id)
	assert.Equal(
		"middleman/pr-1",
		gitOutput(t, recreated.WorktreePath, "branch", "--show-current"),
	)
	assert.Equal(originSHA, testGitSHA(t, recreated.WorktreePath, "HEAD"))
}

func TestWorkspacePRDetailPlatformHost(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	database, err := db.Open(
		filepath.Join(t.TempDir(), "test.db"),
	)
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	// Seed same owner/name on different hosts to test ambiguity.
	seedPROnHost(
		t, database,
		"github.com", "acme", "widget", 10,
	)
	seedPROnHost(
		t, database,
		"ghe.example.com", "acme", "widget", 20,
	)

	mock := &mockGH{}
	repos := []ghclient.RepoRef{
		{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		},
		{
			Owner: "acme", Name: "widget",
			PlatformHost: "ghe.example.com",
		},
	}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{
			"github.com":      mock,
			"ghe.example.com": mock,
		},
		database, nil, repos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)

	srv := New(
		database, syncer, nil, "/", nil, ServerOptions{},
	)
	t.Cleanup(func() { gracefulShutdown(t, srv) })
	client := setupTestClient(t, srv)
	ctx := t.Context()

	// PR on github.com
	r1, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", 10,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, r1.StatusCode())
	require.NotNil(r1.JSON200)
	assert.Equal("github.com", r1.JSON200.PlatformHost)

	// PR on ghe.example.com (same owner/name, different number)
	r2, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		ctx, "acme", "widget", 20,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, r2.StatusCode())
	require.NotNil(r2.JSON200)
	assert.Equal("ghe.example.com", r2.JSON200.PlatformHost)
}

// seedPROnHost seeds a repo on a specific platform host and
// inserts a PR for it.
func seedPROnHost(
	t *testing.T, database *db.DB,
	host, owner, name string, number int,
) int64 {
	t.Helper()
	ctx := t.Context()

	repoID, err := database.UpsertRepo(ctx, host, owner, name)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     int64(number) * 1000,
		Number:         number,
		URL:            fmt.Sprintf("https://%s/%s/%s/pull/%d", host, owner, name, number),
		Title:          fmt.Sprintf("Test PR #%d", number),
		Author:         "testuser",
		State:          "open",
		IsDraft:        false,
		Body:           "test body",
		HeadBranch:     "feature",
		BaseBranch:     "main",
		Additions:      5,
		Deletions:      2,
		CommentCount:   0,
		ReviewDecision: "",
		CIStatus:       "",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}

	prID, err := database.UpsertMergeRequest(ctx, pr)
	require.NoError(t, err)
	require.NoError(t, database.EnsureKanbanState(ctx, prID))

	return prID
}

func TestWorkspaceDeleteDirty(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	client, database, _, _ := setupTestServerWithWorkspaces(t)
	ctx := t.Context()

	// Create workspace.
	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	// Poll until workspace is ready.
	var wsPath string
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		getResp, gErr := client.HTTP.GetWorkspacesByIdWithResponse(
			ctx, wsID,
		)
		require.NoError(gErr)
		if getResp.StatusCode() != http.StatusOK {
			continue
		}
		if getResp.JSON200 != nil &&
			getResp.JSON200.Status == "ready" {
			wsPath = getResp.JSON200.WorktreePath
			break
		}
	}
	require.NotEmpty(wsPath, "workspace never became ready")

	// Write a dirty file into the worktree.
	require.NoError(os.WriteFile(
		filepath.Join(wsPath, "dirty.txt"),
		[]byte("uncommitted\n"), 0o644,
	))

	// DELETE without force -> 409.
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID, &generated.DeleteWorkspaceParams{},
	)
	require.NoError(err)
	assert.Equal(http.StatusConflict, delResp.StatusCode())

	// DELETE with force -> 204.
	force := true
	delResp2, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	assert.Equal(http.StatusNoContent, delResp2.StatusCode())

	// Verify deleted.
	getResp, err := client.HTTP.GetWorkspacesByIdWithResponse(
		ctx, wsID,
	)
	require.NoError(err)
	assert.Equal(http.StatusNotFound, getResp.StatusCode())

	// --- Second scenario: corrupt/missing worktree ---
	// Seed a second PR and create a workspace for it.
	seedPR(t, database, "acme", "widget", 2)
	create2, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     2,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, create2.StatusCode())
	ws2ID := create2.JSON202.Id

	// Poll until ready.
	var ws2Path string
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		g, gErr := client.HTTP.GetWorkspacesByIdWithResponse(ctx, ws2ID)
		require.NoError(gErr)
		if g.JSON200 != nil && g.JSON200.Status == "ready" {
			ws2Path = g.JSON200.WorktreePath
			break
		}
	}
	require.NotEmpty(ws2Path, "workspace 2 never became ready")

	// Nuke the worktree directory to simulate corruption.
	require.NoError(os.RemoveAll(ws2Path))

	// DELETE without force → 409 (dirty check fails on missing dir).
	del3, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, ws2ID, &generated.DeleteWorkspaceParams{},
	)
	require.NoError(err)
	assert.Equal(http.StatusConflict, del3.StatusCode())

	// DELETE with force → 204.
	del4, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, ws2ID,
		&generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	assert.Equal(http.StatusNoContent, del4.StatusCode())

	// Verify deleted.
	get2, err := client.HTTP.GetWorkspacesByIdWithResponse(ctx, ws2ID)
	require.NoError(err)
	assert.Equal(http.StatusNotFound, get2.StatusCode())
}

// --- edit-pr-content (PATCH) tests ---

func TestAPIEditPRTitleAndBody(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]string{"title": "updated title", "body": "updated body"})
	require.Equal(http.StatusOK, rr.Code)

	mr, err := database.GetMergeRequest(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal("updated title", mr.Title)
	require.Equal("updated body", mr.Body)
}

func TestAPIEditPRTitleOnly(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]string{"title": "new title"})
	require.Equal(http.StatusOK, rr.Code)

	mr, err := database.GetMergeRequest(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal("new title", mr.Title)
	require.Equal("test body", mr.Body)
}

func TestAPIEditPRBodyOnly(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]string{"body": "new body"})
	require.Equal(http.StatusOK, rr.Code)

	mr, err := database.GetMergeRequest(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal("Test PR #1", mr.Title)
	require.Equal("new body", mr.Body)
}

func TestAPIEditPRClearBody(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]string{"body": ""})
	require.Equal(http.StatusOK, rr.Code)

	mr, err := database.GetMergeRequest(
		t.Context(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal("Test PR #1", mr.Title)
	require.Empty(mr.Body)
}

func TestAPIEditPRNoFields400(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]any{})
	require.Equal(http.StatusBadRequest, rr.Code)
}

func TestAPIEditPRBlankTitle400(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]string{"title": "   "})
	require.Equal(http.StatusBadRequest, rr.Code)
}

func TestAPIEditPRPreservesDerivedFields(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	ctx := t.Context()

	// Seed non-default derived fields so we can detect clobbering.
	repo, err := database.GetRepoByOwnerName(ctx, "acme", "widget")
	require.NoError(err)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(database.UpdateMRDerivedFields(ctx, repo.ID, 1, db.MRDerivedFields{
		ReviewDecision: "APPROVED",
		CommentCount:   7,
		LastActivityAt: now,
	}))
	require.NoError(database.UpdateMRCIStatus(ctx, repo.ID, 1, "success", "[]"))

	rr := doJSON(t, srv, http.MethodPatch,
		"/api/v1/repos/acme/widget/pulls/1",
		map[string]string{"title": "changed title"})
	require.Equal(http.StatusOK, rr.Code)

	after, err := database.GetMergeRequest(ctx, "acme", "widget", 1)
	require.NoError(err)
	require.Equal("changed title", after.Title)
	require.Equal(7, after.CommentCount)
	require.Equal("success", after.CIStatus)
	require.Equal("APPROVED", after.ReviewDecision)
	require.Equal("open", after.State)
}
