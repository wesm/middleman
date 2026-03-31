package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// mockGH implements ghclient.Client for testing.
type mockGH struct {
	getPullRequestFn     func(context.Context, string, string, int) (*gh.PullRequest, error)
	markReadyForReviewFn func(context.Context, string, string, int) (*gh.PullRequest, error)
}

func (m *mockGH) ListOpenPullRequests(_ context.Context, _, _ string) ([]*gh.PullRequest, error) {
	return nil, nil
}

func (m *mockGH) ListOpenIssues(_ context.Context, _, _ string) ([]*gh.Issue, error) {
	return nil, nil
}

func (m *mockGH) GetIssue(_ context.Context, _, _ string, _ int) (*gh.Issue, error) {
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
	_ context.Context, _, _ string, _ int, _, _, _ string,
) (*gh.PullRequestMergeResult, error) {
	merged := true
	sha := "abc123"
	msg := "merged"
	return &gh.PullRequestMergeResult{
		Merged: &merged, SHA: &sha, Message: &msg,
	}, nil
}

// setupTestServer opens a temp DB, builds a Server, and returns both.
func setupTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, database, nil, time.Minute)
	srv := New(database, mock, syncer, nil, "/")
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
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	pr := &db.PullRequest{
		RepoID:         repoID,
		GitHubID:       int64(number) * 1000,
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

	prID, err := database.UpsertPullRequest(ctx, pr)
	if err != nil {
		t.Fatalf("upsert pr: %v", err)
	}

	if err := database.EnsureKanbanState(ctx, prID); err != nil {
		t.Fatalf("ensure kanban state: %v", err)
	}

	return prID
}

func TestAPIClientConstruction(t *testing.T) {
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)
	require.NotNil(t, client)
	require.NotNil(t, client.HTTP)
}

func TestAPIListPulls(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.ListPullsWithResponse(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.Len(t, *resp.JSON200, 1)
	require.Equal(t, "acme", (*resp.JSON200)[0].RepoOwner)
	require.Equal(t, "widget", (*resp.JSON200)[0].RepoName)
}

func TestAPIGetPull(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetReposByOwnerByNamePullsByNumberWithResponse(
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.NotNil(t, resp.JSON200.PullRequest)
	require.EqualValues(t, 1, resp.JSON200.PullRequest.Number)
	require.Equal(t, "acme", resp.JSON200.RepoOwner)
	require.Equal(t, "widget", resp.JSON200.RepoName)
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
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())

	pr, err := database.GetPullRequest(context.Background(), "acme", "widget", 1)
	require.NoError(t, err)
	require.NotNil(t, pr)
	require.Equal(t, "reviewing", pr.KanbanStatus)
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
	srv, database := setupTestServer(t)
	client := setupTestClient(t, srv)

	_, err := database.UpsertRepo(context.Background(), "acme", "widget")
	require.NoError(t, err)

	resp, err := client.HTTP.ListReposWithResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.Len(t, *resp.JSON200, 1)
	require.Equal(t, "acme", (*resp.JSON200)[0].Owner)
	require.Equal(t, "widget", (*resp.JSON200)[0].Name)
}

func TestAPISyncStatus(t *testing.T) {
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.GetSyncStatusWithResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.False(t, resp.JSON200.Running)
}

func TestAPITriggerSyncIgnoresRequestCancellation(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, database, []ghclient.RepoRef{{
		Owner: "acme",
		Name:  "widget",
	}}, time.Minute)
	srv := New(database, mock, syncer, nil, "/")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	cancel()

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		repos, err := database.ListRepos(context.Background())
		require.NoError(t, err)
		if len(repos) == 1 && repos[0].Owner == "acme" && repos[0].Name == "widget" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected sync to complete despite request context cancellation")
}

func TestAPIReadyForReview(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
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
	syncer := ghclient.NewSyncer(mock, database, nil, time.Minute)
	srv := New(database, mock, syncer, nil, "/")
	client := setupTestClient(t, srv)

	repoID, err := database.UpsertRepo(context.Background(), "acme", "widget")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	prID, err := database.UpsertPullRequest(context.Background(), &db.PullRequest{
		RepoID:         repoID,
		GitHubID:       1001,
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
	require.NoError(t, err)
	require.NoError(t, database.EnsureKanbanState(context.Background(), prID))

	resp, err := client.HTTP.PostReposByOwnerByNamePullsByNumberReadyForReviewWithResponse(
		context.Background(), "acme", "widget", 1,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)

	pr, err := database.GetPullRequest(context.Background(), "acme", "widget", 1)
	require.NoError(t, err)
	require.NotNil(t, pr)
	require.False(t, pr.IsDraft)
}

func TestAPISetStarred(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetStarredWithResponse(context.Background(), generated.SetStarredJSONRequestBody{
		ItemType: "pr",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())

	starred, err := database.IsStarred(context.Background(), "pr", 1, 1)
	require.NoError(t, err)
	require.True(t, starred)
}

func TestAPIUnsetStarred(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)
	require.NoError(t, database.SetStarred(context.Background(), "pr", 1, 1))
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.UnsetStarredWithResponse(context.Background(), generated.UnsetStarredJSONRequestBody{
		ItemType: "pr",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())

	starred, err := database.IsStarred(context.Background(), "pr", 1, 1)
	require.NoError(t, err)
	require.False(t, starred)
}

func TestAPISetStarredRejectsInvalidItemType(t *testing.T) {
	srv, _ := setupTestServer(t)
	client := setupTestClient(t, srv)

	resp, err := client.HTTP.SetStarredWithResponse(context.Background(), generated.SetStarredJSONRequestBody{
		ItemType: "repo",
		Owner:    "acme",
		Name:     "widget",
		Number:   1,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode())
	require.NotNil(t, resp.ApplicationproblemJSONDefault)
	require.NotNil(t, resp.ApplicationproblemJSONDefault.Detail)
	require.Contains(t, *resp.ApplicationproblemJSONDefault.Detail, "item_type must be 'pr' or 'issue'")
}

func TestOpenAPIEndpointReflectsHumaContract(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	body := rr.Body.String()
	require.Contains(t, body, `"/activity"`)
	require.Contains(t, body, `"name":"since"`)
	require.Contains(t, body, `"capped"`)
	require.NotContains(t, body, `"name":"before"`)
	require.NotContains(t, body, `"has_more"`)
}

func TestOpenAPIDocumentsCustomStatusCodes(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	spec := rr.Body.String()
	require.Contains(t, spec, `"/sync":{"post":{"operationId":"trigger-sync"`)
	require.Contains(t, spec, `"/starred":{"delete":{"operationId":"unset-starred"`)
	require.Contains(t, spec, `"/repos/{owner}/{name}/pulls/{number}/comments":{"post":{"operationId":"post-pr-comment"`)
	require.Contains(t, spec, `"trigger-sync","responses":{"202":{"description":"Accepted"}`)
	require.Contains(t, spec, `"set-starred","requestBody"`)
	require.Contains(t, spec, `"responses":{"200":{"description":"OK"}`)
	require.True(t,
		strings.Contains(spec, `"operationId":"post-pr-comment","parameters"`) ||
			strings.Contains(spec, `"operationId":"post-pr-comment","requestBody"`),
		"expected post-pr-comment operation to be present",
	)
	require.Contains(t, spec, `"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PREvent"}}},"description":"Created"}`)
	require.Contains(t, spec, `"operationId":"post-issue-comment"`)
	require.Contains(t, spec, `"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/IssueEvent"}}},"description":"Created"}`)
}
