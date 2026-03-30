package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// --- /api/v1/pulls ---

// pullResponse extends db.PullRequest with resolved repo owner/name fields.
type pullResponse struct {
	db.PullRequest
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

func (s *Server) handleListPulls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	opts := db.ListPullsOpts{
		State:       q.Get("state"),
		KanbanState: q.Get("kanban"),
		Starred:     q.Get("starred") == "true",
		Search:      q.Get("q"),
	}

	if repo := q.Get("repo"); repo != "" {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			opts.RepoOwner = parts[0]
			opts.RepoName = parts[1]
		}
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			opts.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			opts.Offset = n
		}
	}

	prs, err := s.db.ListPullRequests(ctx, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list pulls: "+err.Error())
		return
	}

	// Build repo ID → Repo lookup to annotate each PR with owner/name.
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list repos: "+err.Error())
		return
	}
	repoByID := make(map[int64]db.Repo, len(repos))
	for _, rp := range repos {
		repoByID[rp.ID] = rp
	}

	out := make([]pullResponse, 0, len(prs))
	for _, pr := range prs {
		resp := pullResponse{PullRequest: pr}
		if rp, ok := repoByID[pr.RepoID]; ok {
			resp.RepoOwner = rp.Owner
			resp.RepoName = rp.Name
		}
		out = append(out, resp)
	}

	writeJSON(w, http.StatusOK, out)
}

// --- /api/v1/repos/{owner}/{name}/pulls/{number} ---

type pullDetailResponse struct {
	PullRequest *db.PullRequest `json:"pull_request"`
	Events      []db.PREvent    `json:"events"`
	RepoOwner   string          `json:"repo_owner"`
	RepoName    string          `json:"repo_name"`
}

func (s *Server) handleGetPull(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	numberStr := r.PathValue("number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	pr, err := s.db.GetPullRequest(ctx, owner, name, number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get pull request: "+err.Error())
		return
	}
	if pr == nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	events, err := s.db.ListPREvents(ctx, pr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list pr events: "+err.Error())
		return
	}
	if events == nil {
		events = []db.PREvent{}
	}

	writeJSON(w, http.StatusOK, pullDetailResponse{
		PullRequest: pr,
		Events:      events,
		RepoOwner:   owner,
		RepoName:    name,
	})
}

// --- PUT /api/v1/repos/{owner}/{name}/pulls/{number}/state ---

var validKanbanStates = map[string]bool{
	"new":           true,
	"reviewing":     true,
	"waiting":       true,
	"awaiting_merge": true,
}

func (s *Server) handleSetKanbanState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	numberStr := r.PathValue("number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if !validKanbanStates[body.Status] {
		writeError(w, http.StatusBadRequest,
			"status must be one of: new, reviewing, waiting, awaiting_merge")
		return
	}

	prID, err := s.db.GetPRIDByRepoAndNumber(ctx, owner, name, number)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := s.db.SetKanbanState(ctx, prID, body.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "set kanban state: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- POST /api/v1/repos/{owner}/{name}/pulls/{number}/comments ---

func (s *Server) handlePostComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	numberStr := r.PathValue("number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeError(w, http.StatusBadRequest, "comment body must not be empty")
		return
	}

	comment, err := s.gh.CreateIssueComment(ctx, owner, name, number, body.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "create comment on GitHub: "+err.Error())
		return
	}

	prID, err := s.db.GetPRIDByRepoAndNumber(ctx, owner, name, number)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	event := ghclient.NormalizeCommentEvent(prID, comment)
	if err := s.db.UpsertPREvents(ctx, []db.PREvent{event}); err != nil {
		// Log but don't fail — comment was already posted to GitHub.
		_ = err
	}

	writeJSON(w, http.StatusCreated, event)
}

// --- /api/v1/issues ---

type issueResponse struct {
	db.Issue
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	opts := db.ListIssuesOpts{
		State:  q.Get("state"),
		Search: q.Get("q"),
	}

	if repo := q.Get("repo"); repo != "" {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			opts.RepoOwner = parts[0]
			opts.RepoName = parts[1]
		}
	}

	if q.Get("starred") == "true" {
		opts.Starred = true
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			opts.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			opts.Offset = n
		}
	}

	issues, err := s.db.ListIssues(ctx, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"list issues: "+err.Error())
		return
	}

	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"list repos: "+err.Error())
		return
	}
	repoByID := make(map[int64]db.Repo, len(repos))
	for _, rp := range repos {
		repoByID[rp.ID] = rp
	}

	out := make([]issueResponse, 0, len(issues))
	for _, issue := range issues {
		resp := issueResponse{Issue: issue}
		if rp, ok := repoByID[issue.RepoID]; ok {
			resp.RepoOwner = rp.Owner
			resp.RepoName = rp.Name
		}
		out = append(out, resp)
	}

	writeJSON(w, http.StatusOK, out)
}

// --- /api/v1/repos/{owner}/{name}/issues/{number} ---

type issueDetailResponse struct {
	Issue     *db.Issue        `json:"issue"`
	Events    []db.IssueEvent  `json:"events"`
	RepoOwner string           `json:"repo_owner"`
	RepoName  string           `json:"repo_name"`
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	numberStr := r.PathValue("number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	issue, err := s.db.GetIssue(ctx, owner, name, number)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"get issue: "+err.Error())
		return
	}
	if issue == nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	events, err := s.db.ListIssueEvents(ctx, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"list issue events: "+err.Error())
		return
	}
	if events == nil {
		events = []db.IssueEvent{}
	}

	writeJSON(w, http.StatusOK, issueDetailResponse{
		Issue:     issue,
		Events:    events,
		RepoOwner: owner,
		RepoName:  name,
	})
}

// --- POST /api/v1/repos/{owner}/{name}/issues/{number}/comments ---

func (s *Server) handlePostIssueComment(
	w http.ResponseWriter, r *http.Request,
) {
	ctx := r.Context()
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	numberStr := r.PathValue("number")

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeError(w, http.StatusBadRequest,
			"comment body must not be empty")
		return
	}

	comment, err := s.gh.CreateIssueComment(
		ctx, owner, name, number, body.Body,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway,
			"create comment on GitHub: "+err.Error())
		return
	}

	issueID, err := s.db.GetIssueIDByRepoAndNumber(
		ctx, owner, name, number,
	)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	event := ghclient.NormalizeIssueCommentEvent(issueID, comment)
	if err := s.db.UpsertIssueEvents(
		ctx, []db.IssueEvent{event},
	); err != nil {
		// Comment was already posted to GitHub — log but don't fail.
		_ = err
	}

	writeJSON(w, http.StatusCreated, event)
}

// --- PUT /api/v1/starred ---

func (s *Server) handleSetStarred(
	w http.ResponseWriter, r *http.Request,
) {
	ctx := r.Context()

	var body struct {
		ItemType string `json:"item_type"`
		Owner    string `json:"owner"`
		Name     string `json:"name"`
		Number   int    `json:"number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.ItemType != "pr" && body.ItemType != "issue" {
		writeError(w, http.StatusBadRequest,
			"item_type must be 'pr' or 'issue'")
		return
	}

	repo, err := s.db.GetRepoByOwnerName(ctx, body.Owner, body.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"get repo: "+err.Error())
		return
	}
	if repo == nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	if err := s.db.SetStarred(
		ctx, body.ItemType, repo.ID, body.Number,
	); err != nil {
		writeError(w, http.StatusInternalServerError,
			"set starred: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- DELETE /api/v1/starred ---

func (s *Server) handleUnsetStarred(
	w http.ResponseWriter, r *http.Request,
) {
	ctx := r.Context()

	var body struct {
		ItemType string `json:"item_type"`
		Owner    string `json:"owner"`
		Name     string `json:"name"`
		Number   int    `json:"number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.ItemType != "pr" && body.ItemType != "issue" {
		writeError(w, http.StatusBadRequest,
			"item_type must be 'pr' or 'issue'")
		return
	}

	repo, err := s.db.GetRepoByOwnerName(ctx, body.Owner, body.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"get repo: "+err.Error())
		return
	}
	if repo == nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	if err := s.db.UnsetStarred(
		ctx, body.ItemType, repo.ID, body.Number,
	); err != nil {
		writeError(w, http.StatusInternalServerError,
			"unset starred: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- GET /api/v1/repos ---

func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.db.ListRepos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list repos: "+err.Error())
		return
	}
	if repos == nil {
		repos = []db.Repo{}
	}
	writeJSON(w, http.StatusOK, repos)
}

// --- POST /api/v1/sync ---

func (s *Server) handleTriggerSync(w http.ResponseWriter, r *http.Request) {
	go s.syncer.RunOnce(r.Context())
	w.WriteHeader(http.StatusAccepted)
}

// --- GET /api/v1/sync/status ---

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	status := s.syncer.Status()
	writeJSON(w, http.StatusOK, status)
}
