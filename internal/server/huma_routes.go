package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

type listPullsInput struct {
	Repo    string `query:"repo"`
	State   string `query:"state"`
	Kanban  string `query:"kanban"`
	Starred bool   `query:"starred"`
	Q       string `query:"q"`
	Limit   int    `query:"limit"`
	Offset  int    `query:"offset"`
}

type listPullsOutput struct {
	Body []pullResponse
}

type repoNumberInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
}

type getPullOutput struct {
	Body pullDetailResponse
}

type setKanbanStateInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
	Body   struct {
		Status string `json:"status"`
	}
}

type statusOnlyOutput struct {
	Status int `status:"200"`
}

type postCommentInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
	Body   struct {
		Body string `json:"body"`
	}
}

type postCommentOutput struct {
	Status int `status:"201"`
	Body   db.PREvent
}

type listIssuesInput struct {
	Repo    string `query:"repo"`
	State   string `query:"state"`
	Starred bool   `query:"starred"`
	Q       string `query:"q"`
	Limit   int    `query:"limit"`
	Offset  int    `query:"offset"`
}

type listIssuesOutput struct {
	Body []issueResponse
}

type getIssueOutput struct {
	Body issueDetailResponse
}

type postIssueCommentInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
	Body   struct {
		Body string `json:"body"`
	}
}

type postIssueCommentOutput struct {
	Status int `status:"201"`
	Body   db.IssueEvent
}

type starredInput struct {
	Body starredRequest
}

type getRepoInput struct {
	Owner string `path:"owner"`
	Name  string `path:"name"`
}

type getRepoOutput struct {
	Body db.Repo
}

type approvePRInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
	Body   struct {
		Body string `json:"body"`
	}
}

type actionStatusBody struct {
	Status string `json:"status"`
}

type actionStatusOutput struct {
	Body actionStatusBody
}

type mergePRInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
	Body   struct {
		CommitTitle   string `json:"commit_title"`
		CommitMessage string `json:"commit_message"`
		Method        string `json:"method"`
	}
}

type mergePRBody struct {
	Merged  bool   `json:"merged"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

type mergePROutput struct {
	Body mergePRBody
}

type githubStateInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
	Body   struct {
		State string `json:"state"`
	}
}

type githubStateOutput struct {
	Body struct {
		State string `json:"state"`
	}
}

type listReposOutput struct {
	Body []db.Repo
}

type acceptedOutput struct {
	Status int `status:"202"`
}

type syncStatusOutput struct {
	Body *ghclient.SyncStatus
}

type listActivityInput struct {
	Repo   string   `query:"repo"`
	Types  []string `query:"types"`
	Search string   `query:"search"`
	After  string   `query:"after"`
	Since  string   `query:"since"`
}

type listActivityOutput struct {
	Body activityResponse
}

func apiConfig(basePath string) huma.Config {
	config := huma.DefaultConfig("middleman API", "0.1.0")
	config.OpenAPIPath = "/openapi"
	config.DocsPath = "/docs"
	config.SchemasPath = "/schemas"
	config.Servers = []*huma.Server{{
		URL: strings.TrimSuffix(basePath, "/") + "/api/v1",
	}}
	return config
}

func (s *Server) registerAPI(api huma.API) {
	huma.Get(api, "/activity", s.listActivity)
	huma.Get(api, "/pulls", s.listPulls)
	huma.Get(api, "/repos/{owner}/{name}/pulls/{number}", s.getPull)
	huma.Register(api, huma.Operation{
		OperationID:   "set-kanban-state",
		Method:        http.MethodPut,
		Path:          "/repos/{owner}/{name}/pulls/{number}/state",
		DefaultStatus: http.StatusOK,
	}, s.setKanbanState)
	huma.Register(api, huma.Operation{
		OperationID:   "post-pr-comment",
		Method:        http.MethodPost,
		Path:          "/repos/{owner}/{name}/pulls/{number}/comments",
		DefaultStatus: http.StatusCreated,
	}, s.postComment)

	huma.Get(api, "/issues", s.listIssues)
	huma.Get(api, "/repos/{owner}/{name}/issues/{number}", s.getIssue)
	huma.Register(api, huma.Operation{
		OperationID:   "post-issue-comment",
		Method:        http.MethodPost,
		Path:          "/repos/{owner}/{name}/issues/{number}/comments",
		DefaultStatus: http.StatusCreated,
	}, s.postIssueComment)

	huma.Register(api, huma.Operation{
		OperationID:   "set-starred",
		Method:        http.MethodPut,
		Path:          "/starred",
		DefaultStatus: http.StatusOK,
	}, s.setStarred)
	huma.Register(api, huma.Operation{
		OperationID:   "unset-starred",
		Method:        http.MethodDelete,
		Path:          "/starred",
		DefaultStatus: http.StatusOK,
	}, s.unsetStarred)

	huma.Get(api, "/repos", s.listRepos)
	huma.Get(api, "/repos/{owner}/{name}", s.getRepo)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/approve", s.approvePR)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/ready-for-review", s.readyForReview)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/merge", s.mergePR)
	huma.Register(api, huma.Operation{
		OperationID:   "set-pr-github-state",
		Method:        http.MethodPost,
		Path:          "/repos/{owner}/{name}/pulls/{number}/github-state",
		DefaultStatus: http.StatusOK,
	}, s.setPRGitHubState)
	huma.Register(api, huma.Operation{
		OperationID:   "set-issue-github-state",
		Method:        http.MethodPost,
		Path:          "/repos/{owner}/{name}/issues/{number}/github-state",
		DefaultStatus: http.StatusOK,
	}, s.setIssueGitHubState)
	huma.Register(api, huma.Operation{
		OperationID:   "trigger-sync",
		Method:        http.MethodPost,
		Path:          "/sync",
		DefaultStatus: http.StatusAccepted,
	}, s.triggerSync)
	huma.Get(api, "/sync/status", s.syncStatus)
}

func NewOpenAPI() *huma.OpenAPI {
	mux := http.NewServeMux()
	s := &Server{}
	api := humago.NewWithPrefix(mux, "/api/v1", apiConfig("/"))
	s.registerAPI(api)
	return api.OpenAPI()
}

func (s *Server) listPulls(ctx context.Context, input *listPullsInput) (*listPullsOutput, error) {
	if input.State != "" {
		valid := map[string]bool{
			"open": true, "closed": true, "all": true,
		}
		if !valid[input.State] {
			return nil, huma.Error400BadRequest(
				"state must be one of: open, closed, all",
			)
		}
	}

	opts := db.ListPullsOpts{
		State:       input.State,
		KanbanState: input.Kanban,
		Starred:     input.Starred,
		Search:      input.Q,
		Limit:       input.Limit,
		Offset:      input.Offset,
	}
	if owner, name := parseRepoFilter(input.Repo); owner != "" {
		opts.RepoOwner = owner
		opts.RepoName = name
	}

	prs, err := s.db.ListPullRequests(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("list pulls failed")
	}

	repoByID, err := s.lookupRepoMap(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("repo lookup failed")
	}

	out := make([]pullResponse, 0, len(prs))
	for _, pr := range prs {
		rp, ok := repoByID[pr.RepoID]
		if !ok {
			continue
		}
		out = append(out, pullResponse{
			PullRequest: pr,
			RepoOwner:   rp.Owner,
			RepoName:    rp.Name,
		})
	}

	return &listPullsOutput{Body: out}, nil
}

func (s *Server) getPull(ctx context.Context, input *repoNumberInput) (*getPullOutput, error) {
	pr, err := s.db.GetPullRequest(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get pull request failed")
	}
	if pr == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}

	events, err := s.db.ListPREvents(ctx, pr.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list pr events failed")
	}
	if events == nil {
		events = []db.PREvent{}
	}

	return &getPullOutput{
		Body: pullDetailResponse{
			PullRequest: pr,
			Events:      events,
			RepoOwner:   input.Owner,
			RepoName:    input.Name,
		},
	}, nil
}

func (s *Server) setKanbanState(ctx context.Context, input *setKanbanStateInput) (*statusOnlyOutput, error) {
	if !validKanbanStates[input.Body.Status] {
		return nil, huma.Error400BadRequest("status must be one of: new, reviewing, waiting, awaiting_merge")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	prID, err := s.lookupPRID(ctx, ref)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err := s.db.SetKanbanState(ctx, prID, input.Body.Status); err != nil {
		return nil, huma.Error500InternalServerError("set kanban state failed")
	}

	return &statusOnlyOutput{Status: http.StatusOK}, nil
}

func (s *Server) postComment(ctx context.Context, input *postCommentInput) (*postCommentOutput, error) {
	if strings.TrimSpace(input.Body.Body) == "" {
		return nil, huma.Error400BadRequest("comment body must not be empty")
	}

	comment, err := s.gh.CreateIssueComment(ctx, input.Owner, input.Name, input.Number, input.Body.Body)
	if err != nil {
		return nil, huma.Error502BadGateway("create comment on GitHub failed")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	prID, err := s.lookupPRID(ctx, ref)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	event := ghclient.NormalizeCommentEvent(prID, comment)
	if err := s.db.UpsertPREvents(ctx, []db.PREvent{event}); err != nil {
		_ = err
	}

	return &postCommentOutput{Status: http.StatusCreated, Body: event}, nil
}

func (s *Server) listIssues(ctx context.Context, input *listIssuesInput) (*listIssuesOutput, error) {
	if input.State != "" {
		valid := map[string]bool{
			"open": true, "closed": true, "all": true,
		}
		if !valid[input.State] {
			return nil, huma.Error400BadRequest(
				"state must be one of: open, closed, all",
			)
		}
	}

	opts := db.ListIssuesOpts{
		State:   input.State,
		Search:  input.Q,
		Starred: input.Starred,
		Limit:   input.Limit,
		Offset:  input.Offset,
	}
	if owner, name := parseRepoFilter(input.Repo); owner != "" {
		opts.RepoOwner = owner
		opts.RepoName = name
	}

	issues, err := s.db.ListIssues(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("list issues failed")
	}

	repoByID, err := s.lookupRepoMap(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("repo lookup failed")
	}

	out := make([]issueResponse, 0, len(issues))
	for _, issue := range issues {
		rp, ok := repoByID[issue.RepoID]
		if !ok {
			continue
		}
		out = append(out, issueResponse{
			Issue:     issue,
			RepoOwner: rp.Owner,
			RepoName:  rp.Name,
		})
	}

	return &listIssuesOutput{Body: out}, nil
}

func (s *Server) getIssue(ctx context.Context, input *repoNumberInput) (*getIssueOutput, error) {
	issue, err := s.db.GetIssue(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get issue failed")
	}
	if issue == nil {
		return nil, huma.Error404NotFound("issue not found")
	}

	events, err := s.db.ListIssueEvents(ctx, issue.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list issue events failed")
	}
	if events == nil {
		events = []db.IssueEvent{}
	}

	return &getIssueOutput{
		Body: issueDetailResponse{
			Issue:     issue,
			Events:    events,
			RepoOwner: input.Owner,
			RepoName:  input.Name,
		},
	}, nil
}

func (s *Server) postIssueComment(ctx context.Context, input *postIssueCommentInput) (*postIssueCommentOutput, error) {
	if strings.TrimSpace(input.Body.Body) == "" {
		return nil, huma.Error400BadRequest("comment body must not be empty")
	}

	comment, err := s.gh.CreateIssueComment(ctx, input.Owner, input.Name, input.Number, input.Body.Body)
	if err != nil {
		return nil, huma.Error502BadGateway("create comment on GitHub failed")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	issueID, err := s.lookupIssueID(ctx, ref)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	event := ghclient.NormalizeIssueCommentEvent(issueID, comment)
	if err := s.db.UpsertIssueEvents(ctx, []db.IssueEvent{event}); err != nil {
		_ = err
	}

	return &postIssueCommentOutput{Status: http.StatusCreated, Body: event}, nil
}

func (s *Server) setStarred(ctx context.Context, input *starredInput) (*statusOnlyOutput, error) {
	repoID, err := s.lookupStarredRepoID(ctx, input.Body)
	if err != nil {
		return nil, err
	}
	if err := s.db.SetStarred(ctx, input.Body.ItemType, repoID, input.Body.Number); err != nil {
		return nil, huma.Error500InternalServerError("set starred failed")
	}
	return &statusOnlyOutput{Status: http.StatusOK}, nil
}

func (s *Server) unsetStarred(ctx context.Context, input *starredInput) (*statusOnlyOutput, error) {
	repoID, err := s.lookupStarredRepoID(ctx, input.Body)
	if err != nil {
		return nil, err
	}
	if err := s.db.UnsetStarred(ctx, input.Body.ItemType, repoID, input.Body.Number); err != nil {
		return nil, huma.Error500InternalServerError("unset starred failed")
	}
	return &statusOnlyOutput{Status: http.StatusOK}, nil
}

func (s *Server) getRepo(ctx context.Context, input *getRepoInput) (*getRepoOutput, error) {
	repo, err := s.db.GetRepoByOwnerName(ctx, input.Owner, input.Name)
	if err != nil || repo == nil {
		return nil, huma.Error404NotFound("repo not found")
	}
	return &getRepoOutput{Body: *repo}, nil
}

func (s *Server) approvePR(ctx context.Context, input *approvePRInput) (*actionStatusOutput, error) {
	review, err := s.gh.CreateReview(ctx, input.Owner, input.Name, input.Number, "APPROVE", input.Body.Body)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	prID, lookupErr := s.lookupPRID(ctx, ref)
	if lookupErr == nil {
		event := ghclient.NormalizeReviewEvent(prID, review)
		_ = s.db.UpsertPREvents(ctx, []db.PREvent{event})
	}

	return &actionStatusOutput{Body: actionStatusBody{Status: "approved"}}, nil
}

func (s *Server) readyForReview(ctx context.Context, input *repoNumberInput) (*actionStatusOutput, error) {
	pr, err := s.gh.MarkPullRequestReadyForReview(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error")
	}

	repoObj, err := s.db.GetRepoByOwnerName(ctx, input.Owner, input.Name)
	if err == nil && repoObj != nil {
		normalized := ghclient.NormalizePR(repoObj.ID, pr)
		if prID, upsertErr := s.db.UpsertPullRequest(ctx, normalized); upsertErr == nil {
			_ = s.db.EnsureKanbanState(ctx, prID)
		}
	}

	return &actionStatusOutput{Body: actionStatusBody{Status: "ready_for_review"}}, nil
}

func (s *Server) mergePR(ctx context.Context, input *mergePRInput) (*mergePROutput, error) {
	validMethods := map[string]bool{"merge": true, "squash": true, "rebase": true}
	if !validMethods[input.Body.Method] {
		return nil, huma.Error400BadRequest("invalid merge method: must be merge, squash, or rebase")
	}

	result, err := s.gh.MergePullRequest(
		ctx,
		input.Owner,
		input.Name,
		input.Number,
		input.Body.CommitTitle,
		input.Body.CommitMessage,
		input.Body.Method,
	)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub merge error")
	}

	repoObj, _ := s.db.GetRepoByOwnerName(ctx, input.Owner, input.Name)
	if repoObj != nil {
		now := time.Now()
		_ = s.db.UpdatePRState(ctx, repoObj.ID, input.Number, "merged", &now, &now)
	}

	return &mergePROutput{
		Body: mergePRBody{
			Merged:  result.GetMerged(),
			SHA:     result.GetSHA(),
			Message: result.GetMessage(),
		},
	}, nil
}

func (s *Server) setPRGitHubState(
	ctx context.Context, input *githubStateInput,
) (*githubStateOutput, error) {
	if input.Body.State != "open" && input.Body.State != "closed" {
		return nil, huma.Error400BadRequest(
			"state must be 'open' or 'closed'",
		)
	}

	pr, err := s.db.GetPullRequest(
		ctx, input.Owner, input.Name, input.Number,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get pull request: " + err.Error(),
		)
	}
	if pr == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}
	if pr.State == "merged" {
		return nil, huma.Error409Conflict(
			"cannot change state of a merged pull request",
		)
	}

	if _, err := s.gh.EditPullRequest(
		ctx, input.Owner, input.Name,
		input.Number, input.Body.State,
	); err != nil {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) &&
			ghErr.Response.StatusCode == http.StatusUnprocessableEntity {
			// Re-fetch to sync local state and determine the real cause.
			repoID, repoErr := s.lookupRepoID(
				ctx, input.Owner, input.Name,
			)
			if repoErr == nil {
				ghPR, fetchErr := s.gh.GetPullRequest(
					ctx, input.Owner, input.Name, input.Number,
				)
				if fetchErr == nil {
					normalized := ghclient.NormalizePR(repoID, ghPR)
					_, _ = s.db.UpsertPullRequest(ctx, normalized)
					if ghPR.GetMerged() {
						return nil, huma.Error409Conflict(
							"cannot change state of a merged pull request",
						)
					}
				}
			}
		}
		return nil, huma.Error502BadGateway(
			"GitHub API error: " + err.Error(),
		)
	}

	repoID, err := s.lookupRepoID(ctx, input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get repo: " + err.Error(),
		)
	}

	var closedAt *time.Time
	if input.Body.State == "closed" {
		now := time.Now()
		closedAt = &now
	}
	if err := s.db.UpdatePRState(
		ctx, repoID, input.Number,
		input.Body.State, nil, closedAt,
	); err != nil {
		return nil, huma.Error500InternalServerError(
			"update pr state: " + err.Error(),
		)
	}

	out := &githubStateOutput{}
	out.Body.State = input.Body.State
	return out, nil
}

func (s *Server) setIssueGitHubState(
	ctx context.Context, input *githubStateInput,
) (*githubStateOutput, error) {
	if input.Body.State != "open" && input.Body.State != "closed" {
		return nil, huma.Error400BadRequest(
			"state must be 'open' or 'closed'",
		)
	}

	issue, err := s.db.GetIssue(
		ctx, input.Owner, input.Name, input.Number,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get issue: " + err.Error(),
		)
	}
	if issue == nil {
		return nil, huma.Error404NotFound("issue not found")
	}

	if _, err := s.gh.EditIssue(
		ctx, input.Owner, input.Name,
		input.Number, input.Body.State,
	); err != nil {
		return nil, huma.Error502BadGateway(
			"GitHub API error: " + err.Error(),
		)
	}

	repoID, err := s.lookupRepoID(ctx, input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get repo: " + err.Error(),
		)
	}

	var closedAt *time.Time
	if input.Body.State == "closed" {
		now := time.Now()
		closedAt = &now
	}
	if err := s.db.UpdateIssueState(
		ctx, repoID, input.Number,
		input.Body.State, closedAt,
	); err != nil {
		return nil, huma.Error500InternalServerError(
			"update issue state: " + err.Error(),
		)
	}

	out := &githubStateOutput{}
	out.Body.State = input.Body.State
	return out, nil
}

func (s *Server) listRepos(ctx context.Context, _ *struct{}) (*listReposOutput, error) {
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list repos failed")
	}
	if repos == nil {
		repos = []db.Repo{}
	}
	if s.cfg != nil {
		repos = s.filterConfiguredRepos(repos)
	}

	return &listReposOutput{Body: repos}, nil
}

func (s *Server) triggerSync(ctx context.Context, _ *struct{}) (*acceptedOutput, error) {
	go s.syncer.RunOnce(context.WithoutCancel(ctx))
	return &acceptedOutput{Status: http.StatusAccepted}, nil
}

func (s *Server) syncStatus(_ context.Context, _ *struct{}) (*syncStatusOutput, error) {
	return &syncStatusOutput{Body: s.syncer.Status()}, nil
}

func (s *Server) listActivity(ctx context.Context, input *listActivityInput) (*listActivityOutput, error) {
	opts := db.ListActivityOpts{
		Repo:   input.Repo,
		Types:  input.Types,
		Search: input.Search,
	}

	opts.Limit = activitySafetyCap + 1

	if input.Since != "" {
		t, err := time.Parse(time.RFC3339, input.Since)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid since: " + err.Error())
		}
		opts.Since = &t
	} else {
		defaultSince := time.Now().UTC().AddDate(0, 0, -7)
		opts.Since = &defaultSince
	}

	if input.After != "" {
		t, source, sourceID, err := db.DecodeCursor(input.After)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid after cursor: " + err.Error())
		}
		opts.AfterTime = &t
		opts.AfterSource = source
		opts.AfterSourceID = sourceID
	}

	items, err := s.db.ListActivity(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("list activity failed")
	}

	if s.cfg != nil {
		s.cfgMu.Lock()
		configured := make(map[string]bool, len(s.cfg.Repos))
		for _, cr := range s.cfg.Repos {
			configured[cr.Owner+"/"+cr.Name] = true
		}
		s.cfgMu.Unlock()

		filtered := items[:0]
		for _, it := range items {
			if configured[it.RepoOwner+"/"+it.RepoName] {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	capped := len(items) > activitySafetyCap
	if capped {
		items = items[:activitySafetyCap]
	}

	out := make([]activityItemResponse, len(items))
	for i, it := range items {
		out[i] = activityItemResponse{
			ID:           it.Source + ":" + strconv.FormatInt(it.SourceID, 10),
			Cursor:       db.EncodeCursor(it.CreatedAt, it.Source, it.SourceID),
			ActivityType: it.ActivityType,
			RepoOwner:    it.RepoOwner,
			RepoName:     it.RepoName,
			ItemType:     it.ItemType,
			ItemNumber:   it.ItemNumber,
			ItemTitle:    it.ItemTitle,
			ItemURL:      it.ItemURL,
			ItemState:    it.ItemState,
			Author:       it.Author,
			CreatedAt:    it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			BodyPreview:  it.BodyPreview,
		}
	}

	return &listActivityOutput{
		Body: activityResponse{Items: out, Capped: capped},
	}, nil
}

func (s *Server) lookupStarredRepoID(ctx context.Context, body starredRequest) (int64, error) {
	if !validateStarredRequest(body) {
		return 0, huma.Error400BadRequest("item_type must be 'pr' or 'issue'")
	}

	repoID, err := s.lookupRepoID(ctx, body.Owner, body.Name)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			return 0, huma.Error404NotFound(err.Error())
		}
		return 0, huma.Error500InternalServerError("repo lookup failed")
	}

	return repoID, nil
}
