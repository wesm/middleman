package server

import (
	"context"
	"net/http"
	"strings"
	"time"

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
		Starred:     parseBoolFlag(q, "starred"),
		Search:      q.Get("q"),
	}

	if owner, name := parseRepoFilter(q.Get("repo")); owner != "" {
		opts.RepoOwner = owner
		opts.RepoName = name
	}

	opts.Limit = parseOptionalInt(q, "limit")
	opts.Offset = parseOptionalInt(q, "offset")

	prs, err := s.db.ListPullRequests(ctx, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list pulls: "+err.Error())
		return
	}

	// Build repo ID → Repo lookup to annotate each PR with owner/name.
	repoByID, err := s.lookupRepoMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	ref, ok := parseRepoNumberPath(w, r, "PR")
	if !ok {
		return
	}

	pr, err := s.db.GetPullRequest(ctx, ref.owner, ref.name, ref.number)
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
		RepoOwner:   ref.owner,
		RepoName:    ref.name,
	})
}

// --- PUT /api/v1/repos/{owner}/{name}/pulls/{number}/state ---

var validKanbanStates = map[string]bool{
	"new":            true,
	"reviewing":      true,
	"waiting":        true,
	"awaiting_merge": true,
}

func (s *Server) handleSetKanbanState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ref, ok := parseRepoNumberPath(w, r, "PR")
	if !ok {
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSONBody(w, r, &body, "invalid JSON body") {
		return
	}

	if !validKanbanStates[body.Status] {
		writeError(w, http.StatusBadRequest,
			"status must be one of: new, reviewing, waiting, awaiting_merge")
		return
	}

	prID, err := s.lookupPRID(ctx, ref)
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
	ref, ok := parseRepoNumberPath(w, r, "PR")
	if !ok {
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if !decodeJSONBody(w, r, &body, "invalid JSON body") {
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeError(w, http.StatusBadRequest, "comment body must not be empty")
		return
	}

	comment, err := s.gh.CreateIssueComment(
		ctx, ref.owner, ref.name, ref.number, body.Body,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "create comment on GitHub: "+err.Error())
		return
	}

	prID, err := s.lookupPRID(ctx, ref)
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

	if owner, name := parseRepoFilter(q.Get("repo")); owner != "" {
		opts.RepoOwner = owner
		opts.RepoName = name
	}

	opts.Starred = parseBoolFlag(q, "starred")
	opts.Limit = parseOptionalInt(q, "limit")
	opts.Offset = parseOptionalInt(q, "offset")

	issues, err := s.db.ListIssues(ctx, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"list issues: "+err.Error())
		return
	}

	repoByID, err := s.lookupRepoMap(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	Issue     *db.Issue       `json:"issue"`
	Events    []db.IssueEvent `json:"events"`
	RepoOwner string          `json:"repo_owner"`
	RepoName  string          `json:"repo_name"`
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ref, ok := parseRepoNumberPath(w, r, "issue")
	if !ok {
		return
	}

	issue, err := s.db.GetIssue(ctx, ref.owner, ref.name, ref.number)
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
		RepoOwner: ref.owner,
		RepoName:  ref.name,
	})
}

// --- POST /api/v1/repos/{owner}/{name}/issues/{number}/comments ---

func (s *Server) handlePostIssueComment(
	w http.ResponseWriter, r *http.Request,
) {
	ctx := r.Context()
	ref, ok := parseRepoNumberPath(w, r, "issue")
	if !ok {
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if !decodeJSONBody(w, r, &body, "invalid JSON body") {
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeError(w, http.StatusBadRequest,
			"comment body must not be empty")
		return
	}

	comment, err := s.gh.CreateIssueComment(
		ctx, ref.owner, ref.name, ref.number, body.Body,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway,
			"create comment on GitHub: "+err.Error())
		return
	}

	issueID, err := s.lookupIssueID(ctx, ref)
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
	body, repoID, ok := s.parseStarredRequest(w, r)
	if !ok {
		return
	}

	if err := s.db.SetStarred(
		ctx, body.ItemType, repoID, body.Number,
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
	body, repoID, ok := s.parseStarredRequest(w, r)
	if !ok {
		return
	}

	if err := s.db.UnsetStarred(
		ctx, body.ItemType, repoID, body.Number,
	); err != nil {
		writeError(w, http.StatusInternalServerError,
			"unset starred: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- GET /api/v1/repos/{owner}/{name} ---

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")
	repo, err := s.db.GetRepoByOwnerName(r.Context(), owner, name)
	if err != nil || repo == nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

// --- POST /api/v1/repos/{owner}/{name}/pulls/{number}/approve ---

func (s *Server) handleApprovePR(w http.ResponseWriter, r *http.Request) {
	ref, ok := parseRepoNumberPath(w, r, "PR")
	if !ok {
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if !decodeJSONBody(w, r, &body, "invalid JSON body") {
		return
	}

	review, err := s.gh.CreateReview(
		r.Context(), ref.owner, ref.name, ref.number, "APPROVE", body.Body,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway,
			"GitHub API error: "+err.Error())
		return
	}

	prID, lookupErr := s.lookupPRID(r.Context(), ref)
	if lookupErr == nil {
		event := ghclient.NormalizeReviewEvent(prID, review)
		_ = s.db.UpsertPREvents(r.Context(), []db.PREvent{event})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// --- POST /api/v1/repos/{owner}/{name}/pulls/{number}/ready-for-review ---

func (s *Server) handleReadyForReview(w http.ResponseWriter, r *http.Request) {
	ref, ok := parseRepoNumberPath(w, r, "PR")
	if !ok {
		return
	}

	pr, err := s.gh.MarkPullRequestReadyForReview(
		r.Context(), ref.owner, ref.name, ref.number,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "GitHub API error: "+err.Error())
		return
	}

	repoObj, err := s.db.GetRepoByOwnerName(r.Context(), ref.owner, ref.name)
	if err == nil && repoObj != nil {
		normalized := ghclient.NormalizePR(repoObj.ID, pr)
		if prID, upsertErr := s.db.UpsertPullRequest(r.Context(), normalized); upsertErr == nil {
			_ = s.db.EnsureKanbanState(r.Context(), prID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready_for_review"})
}

// --- POST /api/v1/repos/{owner}/{name}/pulls/{number}/merge ---

func (s *Server) handleMergePR(w http.ResponseWriter, r *http.Request) {
	ref, ok := parseRepoNumberPath(w, r, "PR")
	if !ok {
		return
	}

	var body struct {
		CommitTitle   string `json:"commit_title"`
		CommitMessage string `json:"commit_message"`
		Method        string `json:"method"`
	}
	if !decodeJSONBody(w, r, &body, "invalid request body") {
		return
	}

	validMethods := map[string]bool{
		"merge": true, "squash": true, "rebase": true,
	}
	if !validMethods[body.Method] {
		writeError(w, http.StatusBadRequest,
			"invalid merge method: must be merge, squash, or rebase")
		return
	}

	result, err := s.gh.MergePullRequest(
		r.Context(), ref.owner, ref.name, ref.number,
		body.CommitTitle, body.CommitMessage, body.Method,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway,
			"GitHub merge error: "+err.Error())
		return
	}

	repoObj, _ := s.db.GetRepoByOwnerName(r.Context(), ref.owner, ref.name)
	if repoObj != nil {
		now := time.Now()
		_ = s.db.UpdatePRState(
			r.Context(), repoObj.ID, ref.number, "merged", &now, &now,
		)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"merged":  result.GetMerged(),
		"sha":     result.GetSHA(),
		"message": result.GetMessage(),
	})
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
	go s.syncer.RunOnce(context.WithoutCancel(r.Context()))
	w.WriteHeader(http.StatusAccepted)
}

// --- GET /api/v1/sync/status ---

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	status := s.syncer.Status()
	writeJSON(w, http.StatusOK, status)
}
