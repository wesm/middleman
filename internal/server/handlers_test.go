package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/config"
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

// --- Tests ---

func TestHandleListPulls(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pulls", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var results []pullResponse
	if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RepoOwner != "acme" || results[0].RepoName != "widget" {
		t.Errorf("unexpected repo: %s/%s", results[0].RepoOwner, results[0].RepoName)
	}
}

func TestHandleGetPull(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/widget/pulls/1", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp pullDetailResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PullRequest == nil {
		t.Fatal("expected pull_request in response")
	}
	if resp.PullRequest.Number != 1 {
		t.Errorf("expected PR number 1, got %d", resp.PullRequest.Number)
	}
	if resp.RepoOwner != "acme" || resp.RepoName != "widget" {
		t.Errorf("unexpected repo: %s/%s", resp.RepoOwner, resp.RepoName)
	}
}

func TestHandleGetPull404(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/widget/pulls/999", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleSetKanbanState(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	body := bytes.NewBufferString(`{"status":"reviewing"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/repos/acme/widget/pulls/1/state", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify DB was updated.
	pr, err := database.GetPullRequest(context.Background(), "acme", "widget", 1)
	if err != nil {
		t.Fatalf("get pull request: %v", err)
	}
	if pr == nil {
		t.Fatal("PR not found")
		return
	}
	if pr.KanbanStatus != "reviewing" {
		t.Errorf("expected kanban status 'reviewing', got %q", pr.KanbanStatus)
	}
}

func TestHandleSetKanbanStateInvalid(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	body := bytes.NewBufferString(`{"status":"nonsense"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/repos/acme/widget/pulls/1/state", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleListRepos(t *testing.T) {
	srv, database := setupTestServer(t)

	ctx := context.Background()
	if _, err := database.UpsertRepo(ctx, "acme", "widget"); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var repos []db.Repo
	if err := json.NewDecoder(rr.Body).Decode(&repos); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Owner != "acme" || repos[0].Name != "widget" {
		t.Errorf("unexpected repo: %s/%s", repos[0].Owner, repos[0].Name)
	}
}

func TestHandleSyncStatus(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var status ghclient.SyncStatus
	if err := json.NewDecoder(rr.Body).Decode(&status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !status.LastRunAt.IsZero() {
		t.Fatalf("expected empty sync status to omit last_run_at, got %v", status.LastRunAt)
	}
}

func TestHandleTriggerSyncIgnoresRequestCancellation(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, database, []ghclient.RepoRef{{
		Owner: "acme",
		Name:  "widget",
	}}, time.Minute)
	srv := New(database, mock, syncer, nil, "/")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil).WithContext(ctx)
	cancel()

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		repos, err := database.ListRepos(context.Background())
		if err != nil {
			t.Fatalf("list repos: %v", err)
		}
		if len(repos) == 1 && repos[0].Owner == "acme" && repos[0].Name == "widget" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected sync to complete despite request context cancellation")
}

// setupTestServerWithConfig opens a temp DB, saves the given config,
// and returns a Server wired with NewWithConfig.
func setupTestServerWithConfig(
	t *testing.T, cfg *config.Config,
) (*Server, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	configPath := filepath.Join(dir, "config.toml")
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(
		mock, database, nil, time.Minute,
	)
	srv := NewWithConfig(
		database, mock, syncer, nil, cfg, configPath,
	)
	return srv, database
}

func TestHandleGetSettings(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
			{Owner: "acme", Name: "gadget"},
		},
		Activity: config.Activity{
			ViewMode:  "threaded",
			TimeRange: "30d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(
		http.MethodGet, "/api/v1/settings", nil,
	)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s",
			rr.Code, rr.Body.String())
	}

	var resp settingsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(resp.Repos))
	}
	if resp.Activity.ViewMode != "threaded" {
		t.Errorf("expected view_mode threaded, got %q",
			resp.Activity.ViewMode)
	}
	if resp.Activity.TimeRange != "30d" {
		t.Errorf("expected time_range 30d, got %q",
			resp.Activity.TimeRange)
	}
}

func TestHandleUpdateSettings(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	body := bytes.NewBufferString(
		`{"activity":{"view_mode":"threaded","time_range":"30d"}}`,
	)
	req := httptest.NewRequest(
		http.MethodPut, "/api/v1/settings", body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s",
			rr.Code, rr.Body.String())
	}

	var resp settingsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Activity.ViewMode != "threaded" {
		t.Errorf("expected view_mode threaded, got %q",
			resp.Activity.ViewMode)
	}
	if resp.Activity.TimeRange != "30d" {
		t.Errorf("expected time_range 30d, got %q",
			resp.Activity.TimeRange)
	}
}

func TestHandleUpdateSettingsInvalid(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos:        []config.Repo{{Owner: "acme", Name: "widget"}},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	body := bytes.NewBufferString(
		`{"activity":{"view_mode":"kanban","time_range":"7d"}}`,
	)
	req := httptest.NewRequest(
		http.MethodPut, "/api/v1/settings", body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s",
			rr.Code, rr.Body.String())
	}

	// Verify in-memory config was not corrupted.
	getReq := httptest.NewRequest(
		http.MethodGet, "/api/v1/settings", nil,
	)
	getRR := httptest.NewRecorder()
	srv.ServeHTTP(getRR, getReq)

	var resp settingsResponse
	if err := json.NewDecoder(getRR.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Activity.ViewMode != "flat" {
		t.Fatalf(
			"expected view_mode to remain flat after invalid update, got %q",
			resp.Activity.ViewMode,
		)
	}
}

func TestHandleAddRepo(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	body := bytes.NewBufferString(
		`{"owner":"acme","name":"gadget"}`,
	)
	req := httptest.NewRequest(
		http.MethodPost, "/api/v1/repos", body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s",
			rr.Code, rr.Body.String())
	}

	var repo config.Repo
	if err := json.NewDecoder(rr.Body).Decode(&repo); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if repo.Owner != "acme" || repo.Name != "gadget" {
		t.Errorf("unexpected repo: %s/%s",
			repo.Owner, repo.Name)
	}
}

func TestHandleAddRepoDuplicate(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	body := bytes.NewBufferString(
		`{"owner":"acme","name":"widget"}`,
	)
	req := httptest.NewRequest(
		http.MethodPost, "/api/v1/repos", body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s",
			rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteRepo(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
			{Owner: "acme", Name: "gadget"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(
		http.MethodDelete, "/api/v1/repos/acme/widget", nil,
	)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s",
			rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteLastRepo(t *testing.T) {
	cfg := &config.Config{
		SyncInterval: "5m",
		Host:         "127.0.0.1",
		Port:         8090,
		BasePath:     "/",
		Repos: []config.Repo{
			{Owner: "acme", Name: "widget"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}
	srv, _ := setupTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(
		http.MethodDelete, "/api/v1/repos/acme/widget", nil,
	)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s",
			rr.Code, rr.Body.String())
	}
}

func TestHandleReadyForReview(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
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

	repoID, err := database.UpsertRepo(context.Background(), "acme", "widget")
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
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
	if err != nil {
		t.Fatalf("upsert pull request: %v", err)
	}
	if err := database.EnsureKanbanState(context.Background(), prID); err != nil {
		t.Fatalf("ensure kanban state: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/acme/widget/pulls/1/ready-for-review", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	pr, err := database.GetPullRequest(context.Background(), "acme", "widget", 1)
	if err != nil {
		t.Fatalf("get pull request: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PR to exist")
		return
	}
	if pr.IsDraft {
		t.Fatal("expected PR to no longer be draft")
	}
}

func TestCSRFRejectsCrossSite(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := bytes.NewBufferString(`{"status":"reviewing"}`)
	req := httptest.NewRequest(
		http.MethodPut, "/api/v1/repos/a/b/pulls/1/state", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-site, got %d", rr.Code)
	}
}

func TestCSRFRejectsWrongContentType(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := bytes.NewBufferString(`{"status":"reviewing"}`)
	req := httptest.NewRequest(
		http.MethodPut, "/api/v1/repos/a/b/pulls/1/state", body,
	)
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for text/plain, got %d", rr.Code)
	}
}

func TestCSRFAllowsSameOrigin(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	body := bytes.NewBufferString(`{"status":"reviewing"}`)
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/repos/acme/widget/pulls/1/state", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin, got %d: %s",
			rr.Code, rr.Body.String())
	}
}

func TestCSRFAllowsNoHeader(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	body := bytes.NewBufferString(`{"status":"reviewing"}`)
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/repos/acme/widget/pulls/1/state", body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf(
			"expected 200 without Sec-Fetch-Site, got %d: %s",
			rr.Code, rr.Body.String(),
		)
	}
}
