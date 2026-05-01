package server

import (
	"context"
	"time"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/workspace"
)

type mergeRequestDetailProjection struct {
	MergeRequest     *db.MergeRequest
	Events           []db.MREvent
	Repo             db.Repo
	WorktreeLinks    []db.WorktreeLink
	WorkflowApproval workflowApprovalResponse
	Warnings         []string
	Workspace        *workspace.Workspace
}

type issueDetailProjection struct {
	Issue     *db.Issue
	Events    []db.IssueEvent
	Repo      db.Repo
	Workspace *workspace.Workspace
}

func projectMergeRequestListResponse(
	mr db.MergeRequest,
	repo db.Repo,
	links []worktreeLinkResponse,
) mergeRequestResponse {
	if links == nil {
		links = []worktreeLinkResponse{}
	}
	detailLoaded, detailFetchedAt := projectDetailFetchedAt(mr.DetailFetchedAt)
	resp := mergeRequestResponse{
		MergeRequest:    mr,
		RepoOwner:       repo.Owner,
		RepoName:        repo.Name,
		PlatformHost:    repo.PlatformHost,
		WorktreeLinks:   links,
		DetailLoaded:    detailLoaded,
		DetailFetchedAt: detailFetchedAt,
	}
	return resp
}

func projectMergeRequestDetailResponse(
	input mergeRequestDetailProjection,
) mergeRequestDetailResponse {
	events := input.Events
	if events == nil {
		events = []db.MREvent{}
	}
	resp := mergeRequestDetailResponse{
		MergeRequest:     input.MergeRequest,
		Events:           events,
		RepoOwner:        input.Repo.Owner,
		RepoName:         input.Repo.Name,
		PlatformHost:     input.Repo.PlatformHost,
		WorktreeLinks:    toWorktreeLinkResponses(input.WorktreeLinks),
		WorkflowApproval: input.WorkflowApproval,
		Warnings:         input.Warnings,
		Workspace:        projectWorkspaceRef(input.Workspace),
	}
	if input.MergeRequest != nil {
		detailLoaded, detailFetchedAt := projectDetailFetchedAt(
			input.MergeRequest.DetailFetchedAt,
		)
		resp.PlatformHeadSHA = input.MergeRequest.PlatformHeadSHA
		resp.PlatformBaseSHA = input.MergeRequest.PlatformBaseSHA
		resp.DiffHeadSHA = input.MergeRequest.DiffHeadSHA
		resp.MergeBaseSHA = input.MergeRequest.MergeBaseSHA
		resp.DetailLoaded = detailLoaded
		resp.DetailFetchedAt = detailFetchedAt
	}
	return resp
}

func projectIssueListResponse(issue db.Issue, repo db.Repo) issueResponse {
	detailLoaded, detailFetchedAt := projectDetailFetchedAt(issue.DetailFetchedAt)
	resp := issueResponse{
		Issue:           issue,
		PlatformHost:    repo.PlatformHost,
		RepoOwner:       repo.Owner,
		RepoName:        repo.Name,
		DetailLoaded:    detailLoaded,
		DetailFetchedAt: detailFetchedAt,
	}
	return resp
}

func projectIssueDetailResponse(input issueDetailProjection) issueDetailResponse {
	events := input.Events
	if events == nil {
		events = []db.IssueEvent{}
	}
	resp := issueDetailResponse{
		Issue:        input.Issue,
		Events:       events,
		PlatformHost: input.Repo.PlatformHost,
		RepoOwner:    input.Repo.Owner,
		RepoName:     input.Repo.Name,
		Workspace:    projectWorkspaceRef(input.Workspace),
	}
	if input.Issue != nil {
		detailLoaded, detailFetchedAt := projectDetailFetchedAt(input.Issue.DetailFetchedAt)
		resp.DetailLoaded = detailLoaded
		resp.DetailFetchedAt = detailFetchedAt
	}
	return resp
}

func projectDetailFetchedAt(t *time.Time) (bool, string) {
	if t == nil {
		return false, ""
	}
	return true, formatUTCRFC3339(*t)
}

func projectWorkspaceRef(ws *workspace.Workspace) *workspaceRef {
	if ws == nil {
		return nil
	}
	return &workspaceRef{
		ID:     ws.ID,
		Status: ws.Status,
	}
}

func (s *Server) workspaceForMergeRequest(
	ctx context.Context,
	repo db.Repo,
	number int,
) *workspace.Workspace {
	if s.workspaces == nil {
		return nil
	}
	ws, err := s.workspaces.GetByMR(
		ctx, repo.PlatformHost, repo.Owner, repo.Name, number,
	)
	if err != nil {
		return nil
	}
	return ws
}

func (s *Server) workspaceForIssue(
	ctx context.Context,
	repo db.Repo,
	number int,
) *workspace.Workspace {
	if s.workspaces == nil {
		return nil
	}
	ws, err := s.workspaces.GetByIssue(
		ctx, repo.PlatformHost, repo.Owner, repo.Name, number,
	)
	if err != nil {
		return nil
	}
	return ws
}

func (s *Server) buildIssueDetailResponse(
	ctx context.Context,
	repo *db.Repo,
	issue *db.Issue,
) (issueDetailResponse, error) {
	events, err := s.db.ListIssueEvents(ctx, issue.ID)
	if err != nil {
		return issueDetailResponse{}, err
	}
	return projectIssueDetailResponse(issueDetailProjection{
		Issue:     issue,
		Events:    events,
		Repo:      *repo,
		Workspace: s.workspaceForIssue(ctx, *repo, issue.Number),
	}), nil
}

// toWorktreeLinkResponses converts DB links to API responses.
// Returns an empty non-nil slice when input is nil.
func toWorktreeLinkResponses(
	links []db.WorktreeLink,
) []worktreeLinkResponse {
	out := make([]worktreeLinkResponse, len(links))
	for i, l := range links {
		out[i] = worktreeLinkResponse{
			WorktreeKey:    l.WorktreeKey,
			WorktreePath:   l.WorktreePath,
			WorktreeBranch: l.WorktreeBranch,
		}
	}
	return out
}

// indexWorktreeLinksByMR groups worktree link responses by merge request ID.
func indexWorktreeLinksByMR(
	links []db.WorktreeLink,
) map[int64][]worktreeLinkResponse {
	m := make(map[int64][]worktreeLinkResponse)
	for _, l := range links {
		m[l.MergeRequestID] = append(
			m[l.MergeRequestID],
			worktreeLinkResponse{
				WorktreeKey:    l.WorktreeKey,
				WorktreePath:   l.WorktreePath,
				WorktreeBranch: l.WorktreeBranch,
			},
		)
	}
	return m
}
