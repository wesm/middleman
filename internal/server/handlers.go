package server

import "github.com/wesm/middleman/internal/db"

// --- /api/v1/pulls ---

// pullResponse extends db.PullRequest with resolved repo owner/name fields.
type pullResponse struct {
	db.PullRequest
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

type pullDetailResponse struct {
	PullRequest *db.PullRequest `json:"pull_request"`
	Events      []db.PREvent    `json:"events"`
	RepoOwner   string          `json:"repo_owner"`
	RepoName    string          `json:"repo_name"`
}

// --- PUT /api/v1/repos/{owner}/{name}/pulls/{number}/state ---

var validKanbanStates = map[string]bool{
	"new":            true,
	"reviewing":      true,
	"waiting":        true,
	"awaiting_merge": true,
}

type issueResponse struct {
	db.Issue
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

type issueDetailResponse struct {
	Issue     *db.Issue       `json:"issue"`
	Events    []db.IssueEvent `json:"events"`
	RepoOwner string          `json:"repo_owner"`
	RepoName  string          `json:"repo_name"`
}
