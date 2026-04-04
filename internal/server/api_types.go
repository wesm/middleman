package server

import (
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
)

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

type resolveItemResponse struct {
	ItemType    string `json:"item_type" doc:"'pr' or 'issue'"`
	Number      int    `json:"number"`
	RepoTracked bool   `json:"repo_tracked"`
}

type diffResponse struct {
	Stale               bool                `json:"stale"`
	WhitespaceOnlyCount int                 `json:"whitespace_only_count"`
	Files               []gitclone.DiffFile `json:"files"`
}

const activitySafetyCap = 5000

type activityResponse struct {
	Items  []activityItemResponse `json:"items"`
	Capped bool                   `json:"capped"`
}

type activityItemResponse struct {
	ID           string `json:"id"`
	Cursor       string `json:"cursor"`
	ActivityType string `json:"activity_type"`
	RepoOwner    string `json:"repo_owner"`
	RepoName     string `json:"repo_name"`
	ItemType     string `json:"item_type"`
	ItemNumber   int    `json:"item_number"`
	ItemTitle    string `json:"item_title"`
	ItemURL      string `json:"item_url"`
	ItemState    string `json:"item_state"`
	Author       string `json:"author"`
	CreatedAt    string `json:"created_at"`
	BodyPreview  string `json:"body_preview"`
}
