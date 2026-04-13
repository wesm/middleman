package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/shurcooL/githubv4"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	"github.com/wesm/middleman/internal/gitenv"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/stacks"
)

// mockGH implements ghclient.Client for testing.
type mockGH struct {
	getPullRequestFn          func(context.Context, string, string, int) (*gh.PullRequest, error)
	getIssueFn                func(context.Context, string, string, int) (*gh.Issue, error)
	markReadyForReviewFn      func(context.Context, string, string, int) (*gh.PullRequest, error)
	editPullRequestFn         func(context.Context, string, string, int, string) (*gh.PullRequest, error)
	editIssueFn               func(context.Context, string, string, int, string) (*gh.Issue, error)
	mergePullRequestFn        func(context.Context, string, string, int, string, string, string) (*gh.PullRequestMergeResult, error)
	listWorkflowRunsForHeadFn func(context.Context, string, string, string) ([]*gh.WorkflowRun, error)
	approveWorkflowRunFn      func(context.Context, string, string, int64) error
	listOpenPullRequestsFn    func(context.Context, string, string) ([]*gh.PullRequest, error)
	listOpenPRsErr            error
	listOpenIssuesFn          func(context.Context, string, string) ([]*gh.Issue, error)
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

func (m *mockGH) GetUser(_ context.Context, login string) (*gh.User, error) {
	return &gh.User{Login: &login}, nil
}

func (m *mockGH) GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
	if m.getPullRequestFn != nil {
		return m.getPullRequestFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (m *mockGH) ListIssueComments(
	_ context.Context, _, _ string, _ int,
) ([]*gh.IssueComment, error) {
	if m.listIssueCommentsErr != nil {
		return nil, m.listIssueCommentsErr
	}
	return nil, nil
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
	_ context.Context, _, _, _ string,
) (*gh.CombinedStatus, error) {
	return nil, nil
}

func (m *mockGH) ListCheckRunsForRef(
	_ context.Context, _, _, _ string,
) ([]*gh.CheckRun, error) {
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
	_ context.Context, _, _ string,
) (*gh.Repository, error) {
	return &gh.Repository{}, nil
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
	ctx context.Context, owner, repo string, number int, state string,
) (*gh.PullRequest, error) {
	if m.editPullRequestFn != nil {
		return m.editPullRequestFn(ctx, owner, repo, number, state)
	}
	return &gh.PullRequest{State: &state}, nil
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
	// leaked goroutine from one test's handler can call time.Now
	// concurrently with another test's setTestLocalEDT mutating
	// time.Local, which the race detector flags under -shuffle=on.
	t.Cleanup(syncer.Stop)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
	// Registered after the DB cleanup so LIFO ordering runs Shutdown
	// first and lets background goroutines finish before DB close.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv, database
}

func setupTestClient(t *testing.T, srv *Server) *apiclient.Client {
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

	client, err := apiclient.NewWithHTTPClient("http://middleman.test", httpClient)
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

func setTestLocalEDT(t *testing.T) {
	t.Helper()
	//nolint:forbidigo // Tests intentionally override the process local zone to verify UTC normalization.
	oldLocal := time.Local
	//nolint:forbidigo // Tests intentionally override the process local zone to verify UTC normalization.
	time.Local = time.FixedZone("EDT", -4*60*60)
	t.Cleanup(func() {
		//nolint:forbidigo // Tests intentionally restore the overridden process local zone.
		time.Local = oldLocal
	})
}

func assertTimePtrUTC(t *testing.T, got *time.Time) {
	t.Helper()
	require.NotNil(t, got)
	Assert.Equal(t, time.UTC, got.Location())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// seedPR inserts a repo and a PR into the DB, returning the PR's internal ID.
func seedPR(t *testing.T, database *db.DB, owner, name string, number int) int64 {
	t.Helper()
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, "github.com", owner, name)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     int64(number) * 1000,
		Number:         number,
		URL:            "https://github.com/" + owner + "/" + name + "/pull/" + string(rune('0'+number)),
		Title:          "Test PR #" + string(rune('0'+number)),
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

func seedPRWithLabels(t *testing.T, database *db.DB, owner, name string, number int, labels []db.Label) int64 {
	t.Helper()
	ctx := context.Background()
	prID := seedPR(t, database, owner, name, number)
	repo, err := database.GetRepoByOwnerName(ctx, owner, name)
	require.NoError(t, err)
	require.NoError(t, database.ReplaceMergeRequestLabels(ctx, repo.ID, prID, labels))
	return prID
}

func seedPRWithHeadSHA(t *testing.T, database *db.DB, owner, name string, number int, headSHA string) int64 {
	t.Helper()
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, "github.com", owner, name)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.MergeRequest{
		RepoID:          repoID,
		PlatformID:      int64(number) * 1000,
		Number:          number,
		URL:             "https://github.com/" + owner + "/" + name + "/pull/" + string(rune('0'+number)),
		Title:           "Test PR #" + string(rune('0'+number)),
		Author:          "testuser",
		State:           "open",
		IsDraft:         false,
		Body:            "test body",
		HeadBranch:      "feature",
		BaseBranch:      "main",
		PlatformHeadSHA: headSHA,
		Additions:       5,
		Deletions:       2,
		CommentCount:    0,
		ReviewDecision:  "",
		CIStatus:        "",
		CreatedAt:       now,
		UpdatedAt:       now,
		LastActivityAt:  now,
	}

	prID, err := database.UpsertMergeRequest(ctx, pr)
	require.NoError(t, err)

	require.NoError(t, database.EnsureKanbanState(ctx, prID))

	return prID
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
	setTestLocalEDT(t)

	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberMergeWithResponse(
		context.Background(), "acme", "widget", 1,
		generated.MergePRInputBody{
			CommitTitle:   "title",
			CommitMessage: "msg",
			Method:        "squash",
		},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
	require.NoError(err)
	require.Equal("merged", pr.State)
	assertTimePtrUTC(t, pr.MergedAt)
	assertTimePtrUTC(t, pr.ClosedAt)
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

	resp, err := client.HTTP.ListPullsWithResponse(context.Background(), nil)
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

	resp, err := client.HTTP.ListPullsWithResponse(context.Background(), nil)
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
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.MergeRequest)
	require.EqualValues(1, resp.JSON200.MergeRequest.Number)
	require.Equal("acme", resp.JSON200.RepoOwner)
	require.Equal("widget", resp.JSON200.RepoName)
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.NotNil(resp.JSON200.ApprovedCount)
	assert.Equal("approved_workflows", resp.JSON200.Status)
	assert.EqualValues(2, *resp.JSON200.ApprovedCount)
	assert.Equal([]int64{81, 82}, approvedRunIDs)

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.Equal("approved_workflows", resp.JSON200.Status)

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
	require.NotNil(resp.ApplicationproblemJSONDefault)
	require.NotNil(resp.ApplicationproblemJSONDefault.Detail)
	assert.Contains(*resp.ApplicationproblemJSONDefault.Detail, "permission denied")
	assert.Equal([]int64{91, 92}, approvedRunIDs)

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("abc123", pr.PlatformHeadSHA)
}

func TestAPIGetPullNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		context.Background(), "acme", "widget", 999,
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
		context.Background(), "acme", "widget", 1,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 2,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 3,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 4,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 5,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 6,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 7,
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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 8,
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
		context.Background(), "acme", "widget", int64(prNumber),
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
		context.Background(),
		"acme",
		"widget",
		1,
		generated.SetKanbanStateJSONRequestBody{Status: "reviewing"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	require.Equal("reviewing", pr.KanbanStatus)
}

func TestAPISetKanbanStateRejectsInvalidStatus(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetKanbanStateWithResponse(
		context.Background(),
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

	_, err := database.UpsertRepo(context.Background(), "github.com", "acme", "widget")
	require.NoError(err)

	resp, err := client.HTTP.ListReposWithResponse(context.Background())
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.Equal("acme", (*resp.JSON200)[0].Owner)
	require.Equal("widget", (*resp.JSON200)[0].Name)
}

func TestAPICommentAutocomplete(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := context.Background()

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
	ctx := context.Background()

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
	setTestLocalEDT(t)

	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)
	srv.syncer.RunOnce(context.Background())

	resp, err := client.HTTP.GetSyncStatusWithResponse(context.Background())
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

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	cancel()

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(http.StatusAccepted, rr.Code, rr.Body.String())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		repos, err := database.ListRepos(context.Background())
		require.NoError(err)
		if len(repos) == 1 && repos[0].Owner == "acme" && repos[0].Name == "widget" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	Assert.Fail(t, "expected sync to complete despite request context cancellation")
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

	repoID, err := database.UpsertRepo(context.Background(), "github.com", "acme", "widget")
	require.NoError(err)

	now := time.Now().UTC().Truncate(time.Second)
	prID, err := database.UpsertMergeRequest(context.Background(), &db.MergeRequest{
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
	require.NoError(database.EnsureKanbanState(context.Background(), prID))

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(pr)
	require.False(pr.IsDraft)
}

func TestAPISetStarred(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetStarredWithResponse(context.Background(), generated.SetStarredJSONRequestBody{
		ItemType: "pr",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	starred, err := database.IsStarred(context.Background(), "pr", 1, 1)
	require.NoError(err)
	require.True(starred)
}

func TestAPIUnsetStarred(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	require.NoError(database.SetStarred(context.Background(), "pr", 1, 1))
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.UnsetStarredWithResponse(context.Background(), generated.UnsetStarredJSONRequestBody{
		ItemType: "pr",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	starred, err := database.IsStarred(context.Background(), "pr", 1, 1)
	require.NoError(err)
	require.False(starred)
}

func TestAPISetStarredRejectsInvalidItemType(t *testing.T) {
	require := require.New(t)
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetStarredWithResponse(context.Background(), generated.SetStarredJSONRequestBody{
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
	ctx := context.Background()
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
	ctx := context.Background()
	issueID := seedIssue(t, database, owner, name, number, state)
	repo, err := database.GetRepoByOwnerName(ctx, owner, name)
	require.NoError(t, err)
	require.NoError(t, database.ReplaceIssueLabels(ctx, repo.ID, issueID, labels))
	return issueID
}

func TestAPIClosePR(t *testing.T) {
	require := require.New(t)
	setTestLocalEDT(t)

	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		context.Background(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
	require.NoError(err)
	require.Equal("closed", pr.State)
	assertTimePtrUTC(t, pr.ClosedAt)
}

func TestAPIReopenPR(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	ctx := context.Background()

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
	ctx := context.Background()

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
		context.Background(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "nonsense"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPICloseIssue(t *testing.T) {
	require := require.New(t)
	setTestLocalEDT(t)

	srv, database := setupTestServer(t)
	seedIssue(t, database, "acme", "widget", 5, "open")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		context.Background(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	issue, err := database.GetIssue(context.Background(), "acme", "widget", 5)
	require.NoError(err)
	require.Equal("closed", issue.State)
	assertTimePtrUTC(t, issue.ClosedAt)
}

func TestAPIReopenIssue(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedIssue(t, database, "acme", "widget", 5, "closed")
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		context.Background(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "open"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	issue, err := database.GetIssue(context.Background(), "acme", "widget", 5)
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
			context.Background(), "acme", "widget", 1,
		)
		if err != nil {
			syncErr <- err
			return
		}
		syncDone <- resp
	}()

	<-syncStarted

	resp, err := client.HTTP.SetPrGithubStateWithResponse(
		context.Background(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	closedPR, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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

	finalPR, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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

	repoID, err := database.UpsertRepo(context.Background(), "github.com", "acme", "widget")
	require.NoError(err)

	prID, err := database.UpsertMergeRequest(context.Background(), &db.MergeRequest{
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
	require.NoError(database.EnsureKanbanState(context.Background(), prID))

	syncDone := make(chan *generated.PostReposByOwnerByNamePullsByNumberSyncResponse, 1)
	syncErr := make(chan error, 1)
	go func() {
		resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberSyncWithResponse(
			context.Background(), "acme", "widget", 1,
		)
		if err != nil {
			syncErr <- err
			return
		}
		syncDone <- resp
	}()

	<-syncStarted

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	readyPR, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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

	finalPR, err := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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
			context.Background(), "acme", "widget", 5,
		)
		if err != nil {
			syncErr <- err
			return
		}
		syncDone <- resp
	}()

	<-syncStarted

	resp, err := client.HTTP.SetIssueGithubStateWithResponse(
		context.Background(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	closedIssue, err := database.GetIssue(context.Background(), "acme", "widget", 5)
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

	finalIssue, err := database.GetIssue(context.Background(), "acme", "widget", 5)
	require.NoError(err)
	assert.Equal("closed", finalIssue.State)
	assert.NotNil(finalIssue.ClosedAt)
	assert.Equal("Test Issue", finalIssue.Title)
	assert.True(finalIssue.UpdatedAt.After(staleUpdatedAt))
}

func TestAPIListPullsStateFilter(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := context.Background()

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

func TestAPIListIssuesStateFilter(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := context.Background()

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

	resp, err := client.HTTP.ListIssuesWithResponse(context.Background(), nil)
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
		context.Background(), "acme", "widget", 5,
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

// TestAPIIssueDataFromGraphQLSync verifies the API correctly serves
// issue data that was persisted by the GraphQL sync path. The sync
// path itself (GraphQL fetch → normalize → DB upsert) is tested in
// internal/github/sync_test.go; this test covers the DB → API layer.
func TestAPIIssueDataFromGraphQLSync(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

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
			"createdAt":"` + now + `",
			"updatedAt":"` + now + `",
			"closedAt":null,
			"labels":{"nodes":[]},
			"comments":{"totalCount":42,"nodes":[{"databaseId":901,"author":{"login":"leo"},"body":"one","createdAt":"` + now + `","updatedAt":"` + now + `"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor1"}}
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
	issueTime := gh.Timestamp{Time: time.Now().UTC().Truncate(time.Second)}
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
	stale := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
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
	)
	require.NoError(err)
	require.Equal(200, detailResp.StatusCode())
	require.NotNil(detailResp.JSON200)
	assert.Equal(int64(42), detailResp.JSON200.Issue.CommentCount)
}

func make422Error() error {
	return &gh.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusUnprocessableEntity},
		Message:  "Validation Failed",
	}
}

func TestAPIClosePR422AlreadyClosed(t *testing.T) {
	require := require.New(t)
	// EditPullRequest returns 422, but re-fetch shows PR is already closed.
	// Should succeed since the requested state matches.
	state := "closed"
	mock := &mockGH{
		editPullRequestFn: func(_ context.Context, _, _ string, _ int, _ string) (*gh.PullRequest, error) {
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
		context.Background(), "acme", "widget", 1,
		generated.SetPrGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	pr, _ := database.GetMergeRequest(context.Background(), "acme", "widget", 1)
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
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusBadGateway, resp.StatusCode())
}

func TestAPIClosePR422Merged(t *testing.T) {
	// EditPullRequest returns 422, re-fetch shows PR is merged.
	// Should return 409.
	merged := "closed"
	mock := &mockGH{
		editPullRequestFn: func(_ context.Context, _, _ string, _ int, _ string) (*gh.PullRequest, error) {
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 42,
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
		context.Background(), "acme", "widget", 7,
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
		context.Background(), "unknown", "repo", 1,
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
		context.Background(), "acme", "widget", 999,
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
		context.Background(), "acme", "widget", 999,
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
		context.Background(), "acme", "widget", 5,
		generated.SetIssueGithubStateJSONRequestBody{State: "closed"},
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())

	issue, _ := database.GetIssue(context.Background(), "acme", "widget", 5)
	require.Equal("closed", issue.State)
}

func TestAPIGetMRImportMetadata(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	ctx := context.Background()

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
	require.NoError(database.SetWorktreeLinks([]db.WorktreeLink{
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
	require.NoError(database.SetWorktreeLinks([]db.WorktreeLink{
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	assert.False(resp.JSON200.DetailLoaded)
	assert.Nil(resp.JSON200.DetailFetchedAt)

	// Insert a second PR with DetailFetchedAt set.
	ctx := context.Background()
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
		context.Background(), "acme", "widget", 2,
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
	setTestLocalEDT(t)

	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)
	prID := seedPR(t, database, "acme", "widget", 1)
	ctx := context.Background()
	//nolint:forbidigo // Test fixture intentionally uses a non-UTC timestamp to verify UTC normalization.
	createdAt := time.Date(2026, 4, 11, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

	require.NoError(database.UpsertMREvents(ctx, []db.MREvent{{
		MergeRequestID: prID,
		EventType:      "issue_comment",
		Author:         "reviewer",
		Body:           "Looks good",
		CreatedAt:      createdAt,
		DedupeKey:      "comment-utc-created-at",
	}}))

	since := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
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
	ctx := context.Background()
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 999,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode())
}

func TestAPIGetDiff_SingleCommit(t *testing.T) {
	require := require.New(t)

	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{Commit: &commitSHAs[0], From: &from},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPIGetDiff_UnknownSHA(t *testing.T) {
	client, _, _, _, _ := setupTestServerWithClones(t)
	bogus := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		context.Background(), "acme", "widget", 1,
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
		context.Background(), "acme", "widget", 1,
		&generated.GetReposByOwnerByNamePullsByNumberDiffParams{From: &from, To: &to},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
}

func TestAPIGetDiff_FromWithoutTo(t *testing.T) {
	client, _, _, _, commitSHAs := setupTestServerWithClones(t)
	from := commitSHAs[0]
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		context.Background(), "acme", "widget", 1,
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
	ctx := context.Background()
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "rootrepo")
	require.NoError(err)
	require.NoError(database.UpdateDiffSHAs(ctx, repoID, 1, headSHA, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "4b825dc642cb6eb9a060e54bf8d69288fbee4904"))

	client := setupTestClient(t, srv)
	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberDiffWithResponse(
		context.Background(), "acme", "rootrepo", 1,
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
	ctx := context.Background()

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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()

	seedStackedPR(t, database, "acme", "widget", 10, "feat/auth", "main", "open", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/auth-retry", "feat/auth", "open", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 12, "feat/auth-ui", "feat/auth-retry", "open", "pending", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.ListStacksWithResponse(ctx, &generated.ListStacksParams{})
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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

	// Draft base with green CI + approval; non-draft tip pending.
	seedStackedPRDraft(t, database, "acme", "widget", 10, "feat/x", "main", "open", "success", "APPROVED", true)
	seedStackedPR(t, database, "acme", "widget", 11, "feat/y", "feat/x", "open", "pending", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(ctx, "acme", "widget", 10)
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
	ctx := context.Background()

	// Both draft, green CI + approved — must not be all_green.
	seedStackedPRDraft(t, database, "acme", "widget", 10, "feat/a", "main", "open", "success", "APPROVED", true)
	seedStackedPRDraft(t, database, "acme", "widget", 11, "feat/b", "feat/a", "open", "success", "APPROVED", true)
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.ListStacksWithResponse(ctx, &generated.ListStacksParams{})
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
	ctx := context.Background()

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
	ctx := context.Background()

	// 2-PR chain where tip is failing but has no descendants.
	// Per blocked semantics, this is partial_merge when base is merged.
	seedStackedPR(t, database, "acme", "widget", 10, "feat/base", "main", "merged", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/tip", "feat/base", "open", "failure", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(ctx, "acme", "widget", 11)
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
	ctx := context.Background()

	// Base PR targets "master" not "main" — API must return real base_branch.
	seedStackedPR(t, database, "acme", "widget", 10, "feat/base", "master", "open", "success", "APPROVED")
	seedStackedPR(t, database, "acme", "widget", 11, "feat/tip", "feat/base", "open", "pending", "")
	runStackDetection(t, database, "acme", "widget")

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberStackWithResponse(ctx, "acme", "widget", 10)
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

	resp, err := client.HTTP.ListStacksWithResponse(context.Background(), &generated.ListStacksParams{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())

	var stks []generated.StackResponse
	require.NoError(t, json.Unmarshal(resp.Body, &stks))
	assert.Empty(stks)
}
