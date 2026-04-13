package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
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
	Body []mergeRequestResponse
}

type repoNumberInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
}

type getPullOutput struct {
	Body mergeRequestDetailResponse
}

type getMRImportMetadataOutput struct {
	Body mrImportMetadataResponse
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
	Body   db.MREvent
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

type commentAutocompleteInput struct {
	Owner   string `path:"owner"`
	Name    string `path:"name"`
	Trigger string `query:"trigger"`
	Q       string `query:"q"`
	Limit   int    `query:"limit"`
}

type commentAutocompleteOutput struct {
	Body commentAutocompleteResponse
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
	Status        string `json:"status"`
	ApprovedCount int    `json:"approved_count,omitempty"`
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

type syncPROutput struct {
	Body mergeRequestDetailResponse
}

type syncIssueOutput struct {
	Body issueDetailResponse
}

type resolveItemOutput struct {
	Body resolveItemResponse
}

type syncStatusOutput struct {
	Body *ghclient.SyncStatus
}

type rateLimitsOutput struct {
	Body rateLimitsResponse
}

type listStacksInput struct {
	Repo string `query:"repo"`
}

type listStacksOutput struct {
	Body []stackResponse
}

type getStackForPROutput struct {
	Body stackContextResponse
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
	huma.Get(api, "/repos/{owner}/{name}/pulls/{number}/import-metadata", s.getMRImportMetadata)
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

	huma.Post(api, "/repos/{owner}/{name}/items/{number}/resolve", s.resolveItem)

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
	huma.Get(api, "/repos/{owner}/{name}/comment-autocomplete", s.getCommentAutocomplete)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/approve", s.approvePR)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/approve-workflows", s.approveWorkflows)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/ready-for-review", s.readyForReview)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/merge", s.mergePR)
	huma.Post(api, "/repos/{owner}/{name}/pulls/{number}/sync", s.syncPR)
	huma.Post(api, "/repos/{owner}/{name}/issues/{number}/sync", s.syncIssue)
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
	huma.Get(api, "/rate-limits", s.getRateLimits)
	huma.Get(api, "/repos/{owner}/{name}/pulls/{number}/commits", s.getCommits)
	huma.Get(api, "/repos/{owner}/{name}/pulls/{number}/diff", s.getDiff)
	huma.Get(api, "/repos/{owner}/{name}/pulls/{number}/files", s.getFiles)
	huma.Get(api, "/stacks", s.listStacks)
	huma.Get(api, "/repos/{owner}/{name}/pulls/{number}/stack", s.getStackForPR)
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

	opts := db.ListMergeRequestsOpts{
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

	mrs, err := s.db.ListMergeRequests(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("list pulls failed")
	}

	repoByID, err := s.lookupRepoMap(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("repo lookup failed")
	}

	mrIDs := make([]int64, len(mrs))
	for i, mr := range mrs {
		mrIDs[i] = mr.ID
	}
	links, err := s.db.GetWorktreeLinksForMRs(mrIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("load worktree links failed")
	}
	linksByMR := indexWorktreeLinksByMR(links)

	out := make([]mergeRequestResponse, 0, len(mrs))
	for _, mr := range mrs {
		rp, ok := repoByID[mr.RepoID]
		if !ok {
			continue
		}
		wl := linksByMR[mr.ID]
		if wl == nil {
			wl = []worktreeLinkResponse{}
		}
		resp := mergeRequestResponse{
			MergeRequest:  mr,
			RepoOwner:     rp.Owner,
			RepoName:      rp.Name,
			PlatformHost:  rp.PlatformHost,
			WorktreeLinks: wl,
			DetailLoaded:  mr.DetailFetchedAt != nil,
		}
		if mr.DetailFetchedAt != nil {
			resp.DetailFetchedAt = formatUTCRFC3339(*mr.DetailFetchedAt)
		}
		out = append(out, resp)
	}

	return &listPullsOutput{Body: out}, nil
}

func (s *Server) getPull(ctx context.Context, input *repoNumberInput) (*getPullOutput, error) {
	mr, err := s.db.GetMergeRequest(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get pull request failed")
	}
	if mr == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}

	body, err := s.buildPullDetailResponse(ctx, input.Owner, input.Name, mr, workflowDBOnly)
	if err != nil {
		return nil, err
	}

	return &getPullOutput{Body: body}, nil
}

func (s *Server) buildPullDetailResponse(
	ctx context.Context,
	owner, name string,
	mr *db.MergeRequest,
	wfMode workflowMode,
) (mergeRequestDetailResponse, error) {
	events, err := s.db.ListMREvents(ctx, mr.ID)
	if err != nil {
		return mergeRequestDetailResponse{}, huma.Error500InternalServerError("list mr events failed")
	}
	if events == nil {
		events = []db.MREvent{}
	}

	dbLinks, err := s.db.GetWorktreeLinksForMR(mr.ID)
	if err != nil {
		return mergeRequestDetailResponse{}, huma.Error500InternalServerError(
			"load worktree links failed",
		)
	}

	resp := mergeRequestDetailResponse{
		MergeRequest:     mr,
		Events:           events,
		RepoOwner:        owner,
		RepoName:         name,
		WorktreeLinks:    toWorktreeLinkResponses(dbLinks),
		WorkflowApproval: s.workflowApprovalState(ctx, owner, name, mr, wfMode),
		Warnings:         s.diffWarnings(mr),
		DetailLoaded:     mr.DetailFetchedAt != nil,
	}
	if mr.DetailFetchedAt != nil {
		resp.DetailFetchedAt = formatUTCRFC3339(*mr.DetailFetchedAt)
	}
	return resp, nil
}

// diffWarnings returns warnings inferred from the persisted PR row. The
// resolveItem and syncPR paths log diff sync failures via slog and (in
// syncPR's case) surface them in the immediate response, but neither
// persists the failure. Without inferring from the row state, a client
// that lands on the PR detail page after resolveItem (which has no
// warnings field) or after a refresh would see no indication that the
// diff is unavailable. We therefore emit a sanitized warning whenever a
// PR that should have diff data is missing it.
func (s *Server) diffWarnings(mr *db.MergeRequest) []string {
	if mr == nil {
		return nil
	}
	if !s.syncer.HasDiffSync() {
		return nil
	}
	// Closed (including merged) PRs also get diff SHAs populated via
	// fetchAndUpdateClosed, so the warning logic must cover every state
	// that getDiff would render, not just open and merged.
	if mr.DiffHeadSHA == "" {
		return []string{"Diff data is unavailable for this pull request."}
	}
	shas := db.DiffSHAs{
		PlatformHeadSHA: mr.PlatformHeadSHA,
		PlatformBaseSHA: mr.PlatformBaseSHA,
		DiffHeadSHA:     mr.DiffHeadSHA,
		DiffBaseSHA:     mr.DiffBaseSHA,
		State:           mr.State,
	}
	if shas.Stale() {
		return []string{"Diff data is out of date for this pull request."}
	}
	return nil
}

// workflowMode controls which live GitHub calls workflowApprovalState makes.
type workflowMode int

const (
	// workflowDBOnly makes no live calls. Used by GET detail.
	workflowDBOnly workflowMode = iota
	// workflowCheckRuns reads PR state from DB but fetches
	// workflow runs live. Used by POST sync (PR just synced,
	// no need to re-fetch it).
	workflowCheckRuns
	// workflowFull fetches the PR live and then workflow runs.
	// Used by the approve-workflows action.
	workflowFull
)

func (s *Server) workflowApprovalState(
	ctx context.Context,
	owner, name string,
	mr *db.MergeRequest,
	mode workflowMode,
) workflowApprovalResponse {
	if mode == workflowDBOnly {
		return workflowApprovalResponse{}
	}

	client, err := s.syncer.ClientForRepo(owner, name)
	if err != nil {
		return workflowApprovalResponse{}
	}

	var currentState, headSHA string
	if mode == workflowFull {
		pr, prErr := client.GetPullRequest(ctx, owner, name, mr.Number)
		if prErr != nil || pr == nil {
			return workflowApprovalResponse{}
		}
		currentState = pr.GetState()
		headSHA = pr.GetHead().GetSHA()
	} else {
		currentState = mr.State
		headSHA = mr.PlatformHeadSHA
	}

	if currentState != "open" || headSHA == "" {
		return workflowApprovalResponse{Checked: true}
	}

	runs, err := client.ListWorkflowRunsForHeadSHA(ctx, owner, name, headSHA)
	if err != nil {
		return workflowApprovalResponse{}
	}

	state := ghclient.WorkflowApprovalStateFromRuns(
		ghclient.FilterWorkflowRunsAwaitingApproval(runs, mr.Number, headSHA),
	)
	return workflowApprovalResponse{
		Checked:  state.Checked,
		Required: state.Required,
		Count:    state.Count,
	}
}

func (s *Server) getMRImportMetadata(
	ctx context.Context, input *repoNumberInput,
) (*getMRImportMetadataOutput, error) {
	mr, err := s.db.GetMergeRequest(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"failed to query merge request",
		)
	}
	if mr == nil {
		return nil, huma.Error404NotFound("merge request not found")
	}
	return &getMRImportMetadataOutput{
		Body: mrImportMetadataResponse{
			Number:           mr.Number,
			HeadBranch:       mr.HeadBranch,
			PlatformHeadSHA:  mr.PlatformHeadSHA,
			HeadRepoCloneURL: mr.HeadRepoCloneURL,
			State:            mr.State,
			IsDraft:          mr.IsDraft,
			Title:            mr.Title,
		},
	}, nil
}

func (s *Server) setKanbanState(ctx context.Context, input *setKanbanStateInput) (*statusOnlyOutput, error) {
	if !validKanbanStates[input.Body.Status] {
		return nil, huma.Error400BadRequest("status must be one of: new, reviewing, waiting, awaiting_merge")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	mrID, err := s.lookupMRID(ctx, ref)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err := s.db.SetKanbanState(ctx, mrID, input.Body.Status); err != nil {
		return nil, huma.Error500InternalServerError("set kanban state failed")
	}

	return &statusOnlyOutput{Status: http.StatusOK}, nil
}

func (s *Server) postComment(ctx context.Context, input *postCommentInput) (*postCommentOutput, error) {
	if strings.TrimSpace(input.Body.Body) == "" {
		return nil, huma.Error400BadRequest("comment body must not be empty")
	}

	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	comment, err := client.CreateIssueComment(ctx, input.Owner, input.Name, input.Number, input.Body.Body)
	if err != nil {
		return nil, huma.Error502BadGateway("create comment on GitHub failed")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	mrID, err := s.lookupMRID(ctx, ref)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	event := ghclient.NormalizeCommentEvent(mrID, comment)
	if err := s.db.UpsertMREvents(ctx, []db.MREvent{event}); err != nil {
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
		resp := issueResponse{
			Issue:        issue,
			RepoOwner:    rp.Owner,
			RepoName:     rp.Name,
			DetailLoaded: issue.DetailFetchedAt != nil,
		}
		if issue.DetailFetchedAt != nil {
			resp.DetailFetchedAt = formatUTCRFC3339(*issue.DetailFetchedAt)
		}
		out = append(out, resp)
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

	issueResp := issueDetailResponse{
		Issue:        issue,
		Events:       events,
		RepoOwner:    input.Owner,
		RepoName:     input.Name,
		DetailLoaded: issue.DetailFetchedAt != nil,
	}
	if issue.DetailFetchedAt != nil {
		issueResp.DetailFetchedAt = formatUTCRFC3339(*issue.DetailFetchedAt)
	}
	return &getIssueOutput{Body: issueResp}, nil
}

func (s *Server) postIssueComment(ctx context.Context, input *postIssueCommentInput) (*postIssueCommentOutput, error) {
	if strings.TrimSpace(input.Body.Body) == "" {
		return nil, huma.Error400BadRequest("comment body must not be empty")
	}

	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	comment, err := client.CreateIssueComment(ctx, input.Owner, input.Name, input.Number, input.Body.Body)
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

func (s *Server) getCommentAutocomplete(
	ctx context.Context,
	input *commentAutocompleteInput,
) (*commentAutocompleteOutput, error) {
	repo, err := s.db.GetRepoByOwnerName(ctx, input.Owner, input.Name)
	if err != nil || repo == nil {
		return nil, huma.Error404NotFound("repo not found")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}

	switch input.Trigger {
	case "@":
		users, err := s.db.ListCommentAutocompleteUsers(
			ctx,
			repo.PlatformHost,
			input.Owner,
			input.Name,
			input.Q,
			limit,
		)
		if err != nil {
			return nil, huma.Error500InternalServerError("list comment autocomplete users failed")
		}
		return &commentAutocompleteOutput{Body: commentAutocompleteResponse{Users: users}}, nil
	case "#":
		references, err := s.db.ListCommentAutocompleteReferences(
			ctx,
			repo.PlatformHost,
			input.Owner,
			input.Name,
			input.Q,
			limit,
		)
		if err != nil {
			return nil, huma.Error500InternalServerError("list comment autocomplete references failed")
		}
		return &commentAutocompleteOutput{Body: commentAutocompleteResponse{References: references}}, nil
	default:
		return nil, huma.Error400BadRequest("trigger must be @ or #")
	}
}

func (s *Server) approvePR(ctx context.Context, input *approvePRInput) (*actionStatusOutput, error) {
	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	review, err := client.CreateReview(ctx, input.Owner, input.Name, input.Number, "APPROVE", input.Body.Body)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error")
	}

	ref := repoNumberPathRef{owner: input.Owner, name: input.Name, number: input.Number}
	mrID, lookupErr := s.lookupMRID(ctx, ref)
	if lookupErr == nil {
		event := ghclient.NormalizeReviewEvent(mrID, review)
		_ = s.db.UpsertMREvents(ctx, []db.MREvent{event})
	}

	return &actionStatusOutput{Body: actionStatusBody{Status: "approved"}}, nil
}

func (s *Server) approveWorkflows(ctx context.Context, input *repoNumberInput) (*actionStatusOutput, error) {
	mr, err := s.db.GetMergeRequest(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get pull request failed")
	}
	if mr == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}

	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	pr, err := client.GetPullRequest(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error")
	}
	if pr == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}

	headSHA := pr.GetHead().GetSHA()
	if pr.GetState() != "open" || headSHA == "" {
		return &actionStatusOutput{Body: actionStatusBody{Status: "approved_workflows"}}, nil
	}

	runs, err := client.ListWorkflowRunsForHeadSHA(ctx, input.Owner, input.Name, headSHA)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error")
	}
	pending := ghclient.FilterWorkflowRunsAwaitingApproval(runs, input.Number, headSHA)

	approvedCount := 0
	for _, run := range pending {
		if err := client.ApproveWorkflowRun(ctx, input.Owner, input.Name, run.GetID()); err != nil {
			if approvedCount > 0 {
				if syncErr := s.syncer.SyncMR(context.WithoutCancel(ctx), input.Owner, input.Name, input.Number); syncErr != nil {
					slog.Warn("sync after workflow approval failure", "err", syncErr)
				}
			}
			return nil, huma.Error502BadGateway(err.Error())
		}
		approvedCount++
	}

	if syncErr := s.syncer.SyncMR(context.WithoutCancel(ctx), input.Owner, input.Name, input.Number); syncErr != nil {
		slog.Warn("sync after workflow approval", "err", syncErr)
	}

	return &actionStatusOutput{Body: actionStatusBody{
		Status:        "approved_workflows",
		ApprovedCount: approvedCount,
	}}, nil
}

func (s *Server) readyForReview(ctx context.Context, input *repoNumberInput) (*actionStatusOutput, error) {
	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	pr, err := client.MarkPullRequestReadyForReview(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error")
	}
	if pr == nil {
		// No PR payload means we cannot verify GitHub accepted the
		// transition, so don't claim success or poison the cache.
		return nil, huma.Error502BadGateway("GitHub API returned no pull request")
	}

	repoObj, err := s.db.GetRepoByOwnerName(ctx, input.Owner, input.Name)
	if err == nil && repoObj != nil {
		normalized := ghclient.NormalizePR(repoObj.ID, pr)
		if mrID, upsertErr := s.db.UpsertMergeRequest(ctx, normalized); upsertErr == nil {
			_ = s.db.EnsureKanbanState(ctx, mrID)
		}
	}

	return &actionStatusOutput{Body: actionStatusBody{Status: "ready_for_review"}}, nil
}

func (s *Server) mergePR(ctx context.Context, input *mergePRInput) (*mergePROutput, error) {
	validMethods := map[string]bool{"merge": true, "squash": true, "rebase": true}
	if !validMethods[input.Body.Method] {
		return nil, huma.Error400BadRequest("invalid merge method: must be merge, squash, or rebase")
	}

	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	result, err := client.MergePullRequest(
		ctx,
		input.Owner,
		input.Name,
		input.Number,
		input.Body.CommitTitle,
		input.Body.CommitMessage,
		input.Body.Method,
	)
	if err != nil {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) {
			slog.Warn("github merge failed",
				"owner", input.Owner, "repo", input.Name,
				"number", input.Number, "method", input.Body.Method,
				"status", ghErr.Response.StatusCode,
				"message", ghErr.Message)

			if ghErr.Response.StatusCode == http.StatusMethodNotAllowed ||
				ghErr.Response.StatusCode == http.StatusConflict {
				s.runBackground(func(bgCtx context.Context) {
					if syncErr := s.syncer.SyncMR(
						bgCtx, input.Owner, input.Name, input.Number,
					); syncErr != nil {
						slog.Warn("background sync after merge failure", "err", syncErr)
					}
				})
				return nil, huma.Error409Conflict(ghErr.Message)
			}

			// Forward 4xx GitHub errors as-is so the user sees the real cause
			// (e.g. 422 validation, 403 forbidden). 5xx becomes 502.
			if ghErr.Response.StatusCode >= 400 && ghErr.Response.StatusCode < 500 {
				return nil, huma.NewError(ghErr.Response.StatusCode, ghErr.Message)
			}
			return nil, huma.Error502BadGateway("GitHub: " + ghErr.Message)
		}
		slog.Warn("github merge transport error",
			"owner", input.Owner, "repo", input.Name,
			"number", input.Number, "method", input.Body.Method,
			"err", err)
		return nil, huma.Error502BadGateway("GitHub merge error: " + err.Error())
	}

	repoObj, _ := s.db.GetRepoByOwnerName(ctx, input.Owner, input.Name)
	if repoObj != nil {
		now := time.Now().UTC()
		_ = s.db.UpdateMRState(ctx, repoObj.ID, input.Number, "merged", &now, &now)
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

	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	mr, err := s.db.GetMergeRequest(
		ctx, input.Owner, input.Name, input.Number,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get pull request: " + err.Error(),
		)
	}
	if mr == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}
	if mr.State == "merged" {
		return nil, huma.Error409Conflict(
			"cannot change state of a merged pull request",
		)
	}

	if _, err := client.EditPullRequest(
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
				ghPR, fetchErr := client.GetPullRequest(
					ctx, input.Owner, input.Name, input.Number,
				)
				if fetchErr == nil {
					normalized := ghclient.NormalizePR(repoID, ghPR)
					_, _ = s.db.UpsertMergeRequest(ctx, normalized)
					if ghPR.GetMerged() {
						return nil, huma.Error409Conflict(
							"cannot change state of a merged pull request",
						)
					}
					// Already in requested state (concurrent edit).
					if ghPR.GetState() == input.Body.State {
						out := &githubStateOutput{}
						out.Body.State = input.Body.State
						return out, nil
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
		now := time.Now().UTC()
		closedAt = &now
	}
	if err := s.db.UpdateMRState(
		ctx, repoID, input.Number,
		input.Body.State, nil, closedAt,
	); err != nil {
		return nil, huma.Error500InternalServerError(
			"update mr state: " + err.Error(),
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

	client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
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

	if _, err := client.EditIssue(
		ctx, input.Owner, input.Name,
		input.Number, input.Body.State,
	); err != nil {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) &&
			ghErr.Response.StatusCode == http.StatusUnprocessableEntity {
			// Re-fetch to sync local state. If already in the
			// requested state (concurrent edit), treat as success.
			repoID, repoErr := s.lookupRepoID(
				ctx, input.Owner, input.Name,
			)
			if repoErr == nil {
				ghIssue, fetchErr := client.GetIssue(
					ctx, input.Owner, input.Name, input.Number,
				)
				if fetchErr == nil {
					normalized := ghclient.NormalizeIssue(repoID, ghIssue)
					_, _ = s.db.UpsertIssue(ctx, normalized)
					if ghIssue.GetState() == input.Body.State {
						out := &githubStateOutput{}
						out.Body.State = input.Body.State
						return out, nil
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
		now := time.Now().UTC()
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
	s.syncer.TriggerRun(context.WithoutCancel(ctx))
	return &acceptedOutput{Status: http.StatusAccepted}, nil
}

func (s *Server) syncStatus(_ context.Context, _ *struct{}) (*syncStatusOutput, error) {
	return &syncStatusOutput{Body: s.syncer.Status()}, nil
}

func (s *Server) getRateLimits(
	_ context.Context, _ *struct{},
) (*rateLimitsOutput, error) {
	trackers := s.syncer.RateTrackers()
	gqlTrackers := s.syncer.GQLRateTrackers()
	budgets := s.syncer.Budgets()
	hosts := make(map[string]rateLimitHostStatus, len(trackers))
	for host, rt := range trackers {
		resetStr := ""
		if resetAt := rt.ResetAt(); resetAt != nil {
			resetStr = formatUTCRFC3339(*resetAt)
		}
		status := rateLimitHostStatus{
			RequestsHour:       rt.RequestsThisHour(),
			RateRemaining:      rt.Remaining(),
			RateLimit:          rt.RateLimit(),
			RateResetAt:        resetStr,
			HourStart:          formatUTCRFC3339(rt.HourStart()),
			SyncThrottleFactor: rt.ThrottleFactor(),
			SyncPaused:         rt.IsPaused(),
			ReserveBuffer:      ghclient.RateReserveBuffer,
			Known:              rt.Known(),
			GQLRemaining:       -1,
			GQLLimit:           -1,
		}
		if gqlRT := gqlTrackers[host]; gqlRT != nil {
			status.GQLRemaining = gqlRT.Remaining()
			status.GQLLimit = gqlRT.RateLimit()
			status.GQLKnown = gqlRT.Known()
			if resetAt := gqlRT.ResetAt(); resetAt != nil {
				status.GQLResetAt = resetAt.UTC().Format(time.RFC3339)
			}
		}
		if b := budgets[host]; b != nil {
			status.BudgetLimit = b.Limit()
			status.BudgetSpent = b.Spent()
			status.BudgetRemaining = b.Remaining()
		}
		hosts[host] = status
	}
	return &rateLimitsOutput{
		Body: rateLimitsResponse{Hosts: hosts},
	}, nil
}

func (s *Server) syncPR(ctx context.Context, input *repoNumberInput) (*syncPROutput, error) {
	// SyncMR distinguishes a non-fatal diff failure from a hard sync failure
	// via DiffSyncError. The PR row, timeline, and CI status are all current
	// in either case, so degrade gracefully: keep the response, but report
	// the diff problem as a warning so the UI can explain why the diff view
	// is stale or empty.
	var diffErr *ghclient.DiffSyncError
	syncErr := s.syncer.SyncMR(ctx, input.Owner, input.Name, input.Number)
	if syncErr != nil && !errors.As(syncErr, &diffErr) {
		if strings.Contains(syncErr.Error(), "is not tracked") {
			return nil, huma.Error403Forbidden(syncErr.Error())
		}
		return nil, huma.Error502BadGateway("sync PR: " + syncErr.Error())
	}

	mr, err := s.db.GetMergeRequest(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get pull request: " + err.Error())
	}
	if mr == nil {
		return nil, huma.Error404NotFound("pull request not found after sync")
	}

	body, err := s.buildPullDetailResponse(ctx, input.Owner, input.Name, mr, workflowCheckRuns)
	if err != nil {
		return nil, err
	}

	if diffErr != nil {
		slog.Warn("diff sync failed during sync PR",
			"owner", input.Owner,
			"name", input.Name,
			"number", input.Number,
			"code", diffErr.Code,
			"err", diffErr.Err,
		)
		// Replace inferred warnings with the explicit error, which is
		// more specific than the row-state-based diffWarnings.
		body.Warnings = []string{diffErr.UserMessage()}
	}

	return &syncPROutput{Body: body}, nil
}

func (s *Server) syncIssue(ctx context.Context, input *repoNumberInput) (*syncIssueOutput, error) {
	if err := s.syncer.SyncIssue(ctx, input.Owner, input.Name, input.Number); err != nil {
		if strings.Contains(err.Error(), "is not tracked") {
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error502BadGateway("sync issue: " + err.Error())
	}

	issue, err := s.db.GetIssue(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get issue: " + err.Error())
	}
	if issue == nil {
		return nil, huma.Error404NotFound("issue not found after sync")
	}

	events, err := s.db.ListIssueEvents(ctx, issue.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list issue events: " + err.Error())
	}
	if events == nil {
		events = []db.IssueEvent{}
	}

	syncIssueResp := issueDetailResponse{
		Issue:        issue,
		Events:       events,
		RepoOwner:    input.Owner,
		RepoName:     input.Name,
		DetailLoaded: issue.DetailFetchedAt != nil,
	}
	if issue.DetailFetchedAt != nil {
		syncIssueResp.DetailFetchedAt = formatUTCRFC3339(*issue.DetailFetchedAt)
	}
	return &syncIssueOutput{Body: syncIssueResp}, nil
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
		slog.Error("list activity failed", "err", err)
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
			CreatedAt:    formatUTCRFC3339(it.CreatedAt),
			BodyPreview:  it.BodyPreview,
		}
	}

	return &listActivityOutput{
		Body: activityResponse{Items: out, Capped: capped},
	}, nil
}

func (s *Server) resolveItem(
	ctx context.Context, input *repoNumberInput,
) (*resolveItemOutput, error) {
	owner, name, number := input.Owner, input.Name, input.Number

	if !s.syncer.IsTrackedRepo(owner, name) {
		return &resolveItemOutput{
			Body: resolveItemResponse{
				Number:      number,
				RepoTracked: false,
			},
		}, nil
	}

	repo, err := s.db.GetRepoByOwnerName(ctx, owner, name)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"get repo: " + err.Error(),
		)
	}
	if repo != nil {
		itemType, found, err := s.db.ResolveItemNumber(
			ctx, repo.ID, number,
		)
		if err != nil {
			return nil, huma.Error500InternalServerError(
				"resolve item: " + err.Error(),
			)
		}
		if found {
			return &resolveItemOutput{
				Body: resolveItemResponse{
					ItemType:    itemType,
					Number:      number,
					RepoTracked: true,
				},
			}, nil
		}
	}

	itemType, err := s.syncer.SyncItemByNumber(
		ctx, owner, name, number,
	)
	// A DiffSyncError means the PR row was upserted but the diff
	// computation failed. Resolution doesn't need diff data, so treat
	// the result as success here. The resolve response has no warnings
	// field, so the staleness reaches the client when they navigate to
	// the PR detail page: getPull infers the warning from the persisted
	// row state via diffWarnings.
	var diffErr *ghclient.DiffSyncError
	if err != nil && !errors.As(err, &diffErr) {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) {
			if ghErr.Response != nil &&
				ghErr.Response.StatusCode == 404 {
				return nil, huma.Error404NotFound(
					"item not found: " + err.Error(),
				)
			}
			return nil, huma.Error502BadGateway(
				"GitHub API error: " + err.Error(),
			)
		}
		return nil, huma.Error500InternalServerError(
			"resolve item: " + err.Error(),
		)
	}
	if diffErr != nil {
		slog.Warn("resolve item: diff sync failed but PR row was synced",
			"owner", owner,
			"name", name,
			"number", number,
			"err", err,
		)
	}

	return &resolveItemOutput{
		Body: resolveItemResponse{
			ItemType:    itemType,
			Number:      number,
			RepoTracked: true,
		},
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

// --- Commits ---

type getCommitsOutput struct {
	Body commitsResponse
}

func (s *Server) getCommits(ctx context.Context, input *repoNumberInput) (*getCommitsOutput, error) {
	if s.clones == nil {
		return nil, huma.Error503ServiceUnavailable("commits not available: clone manager not configured")
	}

	shas, err := s.db.GetDiffSHAs(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to look up PR")
	}
	if shas == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}
	if shas.DiffHeadSHA == "" || shas.MergeBaseSHA == "" {
		return nil, huma.Error404NotFound("commits not available for this pull request")
	}

	host := s.syncer.HostForRepo(input.Owner, input.Name)
	commits, err := s.clones.ListCommits(ctx, host, input.Owner, input.Name, shas.MergeBaseSHA, shas.DiffHeadSHA)
	if err != nil {
		if errors.Is(err, gitclone.ErrNotFound) {
			return nil, huma.Error404NotFound("commits not available: referenced commit not found")
		}
		return nil, huma.Error502BadGateway("failed to list commits: " + err.Error())
	}

	resp := commitsResponse{Commits: make([]commitResponse, len(commits))}
	for i, c := range commits {
		resp.Commits[i] = commitResponse{
			SHA:        c.SHA,
			Message:    c.Message,
			AuthorName: c.AuthorName,
			AuthoredAt: c.AuthoredAt.UTC(),
		}
	}
	return &getCommitsOutput{Body: resp}, nil
}

// --- Diff ---

type getDiffInput struct {
	Owner      string `path:"owner"`
	Name       string `path:"name"`
	Number     int    `path:"number"`
	Whitespace string `query:"whitespace"`
	Commit     string `query:"commit" doc:"Scope to a single commit SHA"`
	From       string `query:"from"   doc:"Start SHA for range diff (inclusive)"`
	To         string `query:"to"     doc:"End SHA for range diff (inclusive)"`
}

type getDiffOutput struct {
	Body diffResponse
}

func (s *Server) getDiff(ctx context.Context, input *getDiffInput) (*getDiffOutput, error) {
	if s.clones == nil {
		return nil, huma.Error503ServiceUnavailable("diff view not available: clone manager not configured")
	}

	shas, err := s.db.GetDiffSHAs(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to look up PR")
	}
	if shas == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}
	if shas.DiffHeadSHA == "" || shas.MergeBaseSHA == "" {
		return nil, huma.Error404NotFound("diff not available for this pull request")
	}

	host := s.syncer.HostForRepo(input.Owner, input.Name)
	hideWhitespace := input.Whitespace == "hide"

	// Determine diff range based on scope query params.
	diffFrom := shas.MergeBaseSHA
	diffTo := shas.DiffHeadSHA

	hasCommit := input.Commit != ""
	hasFrom := input.From != ""
	hasTo := input.To != ""

	switch {
	case !hasCommit && !hasFrom && !hasTo:
		// Default: full PR diff. diffFrom/diffTo already set.

	case hasCommit && !hasFrom && !hasTo:
		if _, err := s.validateSHAs(ctx, host, input, shas, input.Commit); err != nil {
			return nil, err
		}
		parent, err := s.clones.ParentOf(ctx, host, input.Owner, input.Name, input.Commit)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to resolve parent: " + err.Error())
		}
		diffFrom = parent
		diffTo = input.Commit

	case !hasCommit && hasFrom && hasTo:
		indexMap, err := s.validateSHAs(ctx, host, input, shas, input.From, input.To)
		if err != nil {
			return nil, err
		}
		// In newest-first order, "from" (older) must have a higher index than "to" (newer).
		if indexMap[input.From] <= indexMap[input.To] {
			return nil, huma.Error400BadRequest("invalid range: 'from' must be older than 'to'")
		}
		parent, err := s.clones.ParentOf(ctx, host, input.Owner, input.Name, input.From)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to resolve parent: " + err.Error())
		}
		diffFrom = parent
		diffTo = input.To

	default:
		return nil, huma.Error400BadRequest("invalid scope: use 'commit' alone or 'from'+'to' together")
	}

	result, err := s.clones.Diff(ctx, host, input.Owner, input.Name, diffFrom, diffTo, hideWhitespace)
	if err != nil {
		if errors.Is(err, gitclone.ErrNotFound) {
			return nil, huma.Error404NotFound("diff not available: referenced commit not found")
		}
		slog.Error("failed to compute diff", "owner", input.Owner, "name", input.Name, "number", input.Number, "err", err)
		return nil, huma.Error502BadGateway("failed to compute diff")
	}

	result.Stale = shas.Stale()

	return &getDiffOutput{Body: diffResponse{
		Stale:               result.Stale,
		WhitespaceOnlyCount: result.WhitespaceOnlyCount,
		Files:               result.Files,
	}}, nil
}

// --- Files (lightweight) ---

type getFilesInput struct {
	Owner  string `path:"owner"`
	Name   string `path:"name"`
	Number int    `path:"number"`
}

type getFilesOutput struct {
	Body filesResponse
}

func (s *Server) getFiles(ctx context.Context, input *getFilesInput) (*getFilesOutput, error) {
	if s.clones == nil {
		return nil, huma.Error503ServiceUnavailable("files view not available: clone manager not configured")
	}

	shas, err := s.db.GetDiffSHAs(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to look up PR")
	}
	if shas == nil {
		return nil, huma.Error404NotFound("pull request not found")
	}
	if shas.DiffHeadSHA == "" || shas.MergeBaseSHA == "" {
		return nil, huma.Error404NotFound("file list not available for this pull request")
	}

	host := s.syncer.HostForRepo(input.Owner, input.Name)
	files, err := s.clones.DiffFiles(ctx, host, input.Owner, input.Name, shas.MergeBaseSHA, shas.DiffHeadSHA)
	if err != nil {
		if errors.Is(err, gitclone.ErrNotFound) {
			return nil, huma.Error404NotFound("file list not available: referenced commit not found")
		}
		slog.Error("failed to list files", "owner", input.Owner, "name", input.Name, "number", input.Number, "err", err)
		return nil, huma.Error502BadGateway("failed to list files")
	}

	return &getFilesOutput{Body: filesResponse{
		Stale: shas.Stale(),
		Files: files,
	}}, nil
}

// validateSHAs checks that all provided SHAs are in the PR's first-parent commit list.
// Returns a SHA -> index map (newest-first order) so callers can check range ordering.
func (s *Server) validateSHAs(
	ctx context.Context,
	host string,
	input *getDiffInput,
	shas *db.DiffSHAs,
	userSHAs ...string,
) (map[string]int, error) {
	commits, err := s.clones.ListCommits(ctx, host, input.Owner, input.Name, shas.MergeBaseSHA, shas.DiffHeadSHA)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list commits for validation: " + err.Error())
	}
	indexMap := make(map[string]int, len(commits))
	for i, c := range commits {
		indexMap[c.SHA] = i
	}
	for _, sha := range userSHAs {
		if _, ok := indexMap[sha]; !ok {
			return nil, huma.Error400BadRequest("sha not in pull request: " + sha)
		}
	}
	return indexMap, nil
}

// --- Stacks ---

func (s *Server) listStacks(ctx context.Context, input *listStacksInput) (*listStacksOutput, error) {
	if input.Repo != "" {
		if strings.Count(input.Repo, "/") != 1 {
			return nil, huma.Error400BadRequest("invalid repo filter: expected owner/name")
		}
		owner, name, _ := strings.Cut(input.Repo, "/")
		if owner == "" || name == "" {
			return nil, huma.Error400BadRequest("invalid repo filter: expected owner/name")
		}
	}
	stackList, memberMap, err := s.db.ListStacksWithMembers(ctx, input.Repo)
	if err != nil {
		return nil, huma.Error500InternalServerError("list stacks failed")
	}

	out := make([]stackResponse, 0, len(stackList))
	for _, st := range stackList {
		members := memberMap[st.ID]
		out = append(out, stackResponse{
			ID:        st.ID,
			Name:      st.Name,
			RepoOwner: st.RepoOwner,
			RepoName:  st.RepoName,
			Health:    computeStackHealth(members),
			Members:   toStackMemberResponses(members),
		})
	}

	return &listStacksOutput{Body: out}, nil
}

func (s *Server) getStackForPR(ctx context.Context, input *repoNumberInput) (*getStackForPROutput, error) {
	stack, members, err := s.db.GetStackForPR(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get stack for pr failed")
	}
	if stack == nil {
		return nil, huma.Error404NotFound("PR is not part of a stack")
	}

	var position int
	for _, m := range members {
		if m.Number == input.Number {
			position = m.Position
			break
		}
	}

	return &getStackForPROutput{
		Body: stackContextResponse{
			StackID:   stack.ID,
			StackName: stack.Name,
			Position:  position,
			Size:      len(members),
			Health:    computeStackHealth(members),
			Members:   toStackMemberResponses(members),
		},
	}, nil
}
