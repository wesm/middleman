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
	RepoOwner       string                 `json:"repo_owner"`
	RepoName        string                 `json:"repo_name"`
	WorktreeLinks   []worktreeLinkResponse `json:"worktree_links"`
	DetailLoaded    bool                   `json:"detail_loaded"`
	DetailFetchedAt string                 `json:"detail_fetched_at,omitempty"`
}

type workflowApprovalResponse struct {
	Checked  bool `json:"checked"`
	Required bool `json:"required"`
	Count    int  `json:"count"`
}

type mergeRequestDetailResponse struct {
	MergeRequest     *db.MergeRequest         `json:"merge_request"`
	Events           []db.MREvent             `json:"events"`
	RepoOwner        string                   `json:"repo_owner"`
	RepoName         string                   `json:"repo_name"`
	WorktreeLinks    []worktreeLinkResponse   `json:"worktree_links"`
	WorkflowApproval workflowApprovalResponse `json:"workflow_approval"`
	Warnings         []string                 `json:"warnings,omitempty"`
	DetailLoaded     bool                     `json:"detail_loaded"`
	DetailFetchedAt  string                   `json:"detail_fetched_at,omitempty"`
}

var validKanbanStates = map[string]bool{
	"new":            true,
	"reviewing":      true,
	"waiting":        true,
	"awaiting_merge": true,
}

type issueResponse struct {
	db.Issue
	RepoOwner       string `json:"repo_owner"`
	RepoName        string `json:"repo_name"`
	DetailLoaded    bool   `json:"detail_loaded"`
	DetailFetchedAt string `json:"detail_fetched_at,omitempty"`
}

type issueDetailResponse struct {
	Issue           *db.Issue       `json:"issue"`
	Events          []db.IssueEvent `json:"events"`
	RepoOwner       string          `json:"repo_owner"`
	RepoName        string          `json:"repo_name"`
	DetailLoaded    bool            `json:"detail_loaded"`
	DetailFetchedAt string          `json:"detail_fetched_at,omitempty"`
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

type rateLimitHostStatus struct {
	RequestsHour       int    `json:"requests_hour"`
	RateRemaining      int    `json:"rate_remaining"`
	RateLimit          int    `json:"rate_limit"`
	RateResetAt        string `json:"rate_reset_at"`
	HourStart          string `json:"hour_start"`
	SyncThrottleFactor int    `json:"sync_throttle_factor"`
	SyncPaused         bool   `json:"sync_paused"`
	ReserveBuffer      int    `json:"reserve_buffer"`
	Known              bool   `json:"known"`
	BudgetLimit        int    `json:"budget_limit"`
	BudgetSpent        int    `json:"budget_spent"`
	BudgetRemaining    int    `json:"budget_remaining"`
}

type rateLimitsResponse struct {
	Hosts map[string]rateLimitHostStatus `json:"hosts"`
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
