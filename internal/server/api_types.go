package server

import (
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
)

type worktreeLinkResponse struct {
	WorktreeKey    string `json:"worktree_key"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`
}

// mergeRequestResponse extends db.MergeRequest with resolved repo owner/name fields.
type mergeRequestResponse struct {
	db.MergeRequest
	RepoOwner     string                 `json:"repo_owner"`
	RepoName      string                 `json:"repo_name"`
	WorktreeLinks []worktreeLinkResponse `json:"worktree_links"`
}

type mergeRequestDetailResponse struct {
	MergeRequest  *db.MergeRequest       `json:"merge_request"`
	Events        []db.MREvent           `json:"events"`
	RepoOwner     string                 `json:"repo_owner"`
	RepoName      string                 `json:"repo_name"`
	WorktreeLinks []worktreeLinkResponse `json:"worktree_links"`
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

type mrImportMetadataResponse struct {
	Number           int    `json:"number"`
	HeadBranch       string `json:"head_branch"`
	PlatformHeadSHA  string `json:"platform_head_sha"`
	HeadRepoCloneURL string `json:"head_repo_clone_url"`
	State            string `json:"state"`
	IsDraft          bool   `json:"is_draft"`
	Title            string `json:"title"`
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
