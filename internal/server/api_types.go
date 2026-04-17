package server

import (
	"time"

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
	PlatformHost    string                 `json:"platform_host"`
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
	PlatformHost     string                   `json:"platform_host"`
	WorktreeLinks    []worktreeLinkResponse   `json:"worktree_links"`
	WorkflowApproval workflowApprovalResponse `json:"workflow_approval"`
	Warnings         []string                 `json:"warnings,omitempty"`
	DetailLoaded     bool                     `json:"detail_loaded"`
	DetailFetchedAt  string                   `json:"detail_fetched_at,omitempty"`
	Workspace        *workspaceMRRef          `json:"workspace,omitempty"`
}

var validKanbanStates = map[string]bool{
	"new":            true,
	"reviewing":      true,
	"waiting":        true,
	"awaiting_merge": true,
}

type issueResponse struct {
	db.Issue
	PlatformHost    string `json:"platform_host"`
	RepoOwner       string `json:"repo_owner"`
	RepoName        string `json:"repo_name"`
	DetailLoaded    bool   `json:"detail_loaded"`
	DetailFetchedAt string `json:"detail_fetched_at,omitempty"`
}

type issueDetailResponse struct {
	Issue           *db.Issue       `json:"issue"`
	Events          []db.IssueEvent `json:"events"`
	PlatformHost    string          `json:"platform_host"`
	RepoOwner       string          `json:"repo_owner"`
	RepoName        string          `json:"repo_name"`
	DetailLoaded    bool            `json:"detail_loaded"`
	DetailFetchedAt string          `json:"detail_fetched_at,omitempty"`
}

type commentAutocompleteResponse struct {
	Users      []string                          `json:"users,omitempty"`
	References []db.CommentAutocompleteReference `json:"references,omitempty"`
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

type filesResponse struct {
	Stale bool                `json:"stale"`
	Files []gitclone.DiffFile `json:"files"`
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
	GQLRemaining       int    `json:"gql_remaining"`
	GQLLimit           int    `json:"gql_limit"`
	GQLResetAt         string `json:"gql_reset_at"`
	GQLKnown           bool   `json:"gql_known"`
}

type rateLimitsResponse struct {
	Hosts map[string]rateLimitHostStatus `json:"hosts"`
}

type commitResponse struct {
	SHA        string    `json:"sha"         doc:"Full commit SHA"`
	Message    string    `json:"message"     doc:"First line of commit message"`
	AuthorName string    `json:"author_name" doc:"Commit author display name"`
	AuthoredAt time.Time `json:"authored_at" doc:"Commit author date (RFC3339)"`
}

type commitsResponse struct {
	Commits []commitResponse `json:"commits" doc:"Commits in newest-first order"`
}

type workspaceResponse struct {
	ID               string  `json:"id"`
	PlatformHost     string  `json:"platform_host"`
	RepoOwner        string  `json:"repo_owner"`
	RepoName         string  `json:"repo_name"`
	MRNumber         int     `json:"mr_number"`
	MRHeadRef        string  `json:"mr_head_ref"`
	WorktreePath     string  `json:"worktree_path"`
	TmuxSession      string  `json:"tmux_session"`
	Status           string  `json:"status"`
	ErrorMessage     *string `json:"error_message,omitempty"`
	CreatedAt        string  `json:"created_at"`
	MRTitle          *string `json:"mr_title,omitempty"`
	MRState          *string `json:"mr_state,omitempty"`
	MRIsDraft        *bool   `json:"mr_is_draft,omitempty"`
	MRCIStatus       *string `json:"mr_ci_status,omitempty"`
	MRReviewDecision *string `json:"mr_review_decision,omitempty"`
	MRAdditions      *int    `json:"mr_additions,omitempty"`
	MRDeletions      *int    `json:"mr_deletions,omitempty"`
}

type workspaceMRRef struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func toWorkspaceResponse(
	s *db.WorkspaceSummary,
) workspaceResponse {
	return workspaceResponse{
		ID:               s.ID,
		PlatformHost:     s.PlatformHost,
		RepoOwner:        s.RepoOwner,
		RepoName:         s.RepoName,
		MRNumber:         s.MRNumber,
		MRHeadRef:        s.MRHeadRef,
		WorktreePath:     s.WorktreePath,
		TmuxSession:      s.TmuxSession,
		Status:           s.Status,
		ErrorMessage:     s.ErrorMessage,
		CreatedAt:        s.CreatedAt.UTC().Format(time.RFC3339),
		MRTitle:          s.MRTitle,
		MRState:          s.MRState,
		MRIsDraft:        s.MRIsDraft,
		MRCIStatus:       s.MRCIStatus,
		MRReviewDecision: s.MRReviewDecision,
		MRAdditions:      s.MRAdditions,
		MRDeletions:      s.MRDeletions,
	}
}

const activitySafetyCap = 5000

type activityResponse struct {
	Items  []activityItemResponse `json:"items"`
	Capped bool                   `json:"capped"`
}

type stackMemberResponse struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	CIStatus       string `json:"ci_status"`
	ReviewDecision string `json:"review_decision"`
	Position       int    `json:"position"`
	IsDraft        bool   `json:"is_draft"`
	BaseBranch     string `json:"base_branch"`
	BlockedBy      *int   `json:"blocked_by"`
}

type stackResponse struct {
	ID        int64                 `json:"id"`
	Name      string                `json:"name"`
	RepoOwner string                `json:"repo_owner"`
	RepoName  string                `json:"repo_name"`
	Health    string                `json:"health"`
	Members   []stackMemberResponse `json:"members"`
}

type stackContextResponse struct {
	StackID   int64                 `json:"stack_id"`
	StackName string                `json:"stack_name"`
	Position  int                   `json:"position"`
	Size      int                   `json:"size"`
	Health    string                `json:"health"`
	Members   []stackMemberResponse `json:"members"`
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
