package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// mockGH implements ghclient.Client for testing.
type mockGH struct {
	getPullRequestFn     func(context.Context, string, string, int) (*gh.PullRequest, error)
	getIssueFn           func(context.Context, string, string, int) (*gh.Issue, error)
	markReadyForReviewFn func(context.Context, string, string, int) (*gh.PullRequest, error)
	editPullRequestFn    func(context.Context, string, string, int, string) (*gh.PullRequest, error)
	editIssueFn          func(context.Context, string, string, int, string) (*gh.Issue, error)
	mergePullRequestFn   func(context.Context, string, string, int, string, string, string) (*gh.PullRequestMergeResult, error)
}

func (m *mockGH) ListOpenPullRequests(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
	return nil, nil
}

func (m *mockGH) ListOpenIssues(_ context.Context, _, _ string) ([]*gh.Issue, error) {
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

func (m *mockGH) GetPullRequest(_ context.Context, _, _ string, _ int) (*gh.PullRequest, error) {
	if m.getPullRequestFn != nil {
		return m.getPullRequestFn(context.Background(), "", "", 0)
	}
	return nil, nil
}

func (m *mockGH) ListIssueComments(
	_ context.Context, _, _ string, _ int,
) ([]*gh.IssueComment, error) {
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

	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, repos, time.Minute, nil)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// seedPR inserts a repo and a PR into the DB, returning the PR's internal ID.
func seedPR(t *testing.T, database *db.DB, owner, name string, number int) int64 {
	t.Helper()
	ctx := context.Background()

	repoID, err := database.UpsertRepo(ctx, owner, name)
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
	require.Equal("acme", (*resp.JSON200)[0].RepoOwner)
	require.Equal("widget", (*resp.JSON200)[0].RepoName)
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

	_, err := database.UpsertRepo(context.Background(), "acme", "widget")
	require.NoError(err)

	resp, err := client.HTTP.ListReposWithResponse(context.Background())
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.Len(*resp.JSON200, 1)
	require.Equal("acme", (*resp.JSON200)[0].Owner)
	require.Equal("widget", (*resp.JSON200)[0].Name)
}

func TestAPISyncStatus(t *testing.T) {
	require := require.New(t)
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetSyncStatusWithResponse(context.Background())
	require.NoError(err)
	require.Equal(http.StatusOK, resp.StatusCode())
	require.NotNil(resp.JSON200)
	require.False(resp.JSON200.Running)
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
	}}, time.Minute, nil)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)

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
	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": mock}, database, nil, defaultTestRepos, time.Minute, nil)
	srv := New(
		database, syncer, nil, "/",
		nil, ServerOptions{},
	)
	client := setupTestClient(t, srv)

	repoID, err := database.UpsertRepo(context.Background(), "acme", "widget")
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
func seedIssue(t *testing.T, database *db.DB, owner, name string, number int, state string) {
	t.Helper()
	ctx := context.Background()
	repoID, err := database.UpsertRepo(ctx, owner, name)
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
	_, err = database.UpsertIssue(ctx, issue)
	require.NoError(t, err)
}

func TestAPIClosePR(t *testing.T) {
	require := require.New(t)
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
	require.NotNil(pr.ClosedAt)
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
	require.NotNil(issue.ClosedAt)
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

	repoID, err := database.UpsertRepo(ctx, "acme", "widget")
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
