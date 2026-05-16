package db

import (
	"cmp"
	"strings"
	"time"
)

type Label struct {
	ID                 int64     `json:"-"`
	RepoID             int64     `json:"-"`
	PlatformID         int64     `json:"-"`
	PlatformExternalID string    `json:"-"`
	Name               string    `json:"name"`
	Description        string    `json:"description,omitempty"`
	Color              string    `json:"color"`
	IsDefault          bool      `json:"is_default"`
	UpdatedAt          time.Time `json:"-"`
}

type Repo struct {
	ID                       int64
	Platform                 string
	PlatformHost             string
	PlatformRepoID           string `json:"-"`
	Owner                    string
	Name                     string
	RepoPath                 string `json:"-"`
	OwnerKey                 string `json:"-"`
	NameKey                  string `json:"-"`
	RepoPathKey              string `json:"-"`
	WebURL                   string `json:"-"`
	CloneURL                 string `json:"-"`
	DefaultBranch            string `json:"-"`
	LastSyncStartedAt        *time.Time
	LastSyncCompletedAt      *time.Time
	LastSyncError            string
	AllowSquashMerge         bool
	AllowMergeCommit         bool
	AllowRebaseMerge         bool
	BackfillPRPage           int
	BackfillPRComplete       bool
	BackfillPRCompletedAt    *time.Time
	BackfillIssuePage        int
	BackfillIssueComplete    bool
	BackfillIssueCompletedAt *time.Time
	CreatedAt                time.Time
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type RepoIdentity struct {
	Platform       string
	PlatformHost   string
	PlatformRepoID string
	Owner          string
	Name           string
	RepoPath       string
	OwnerKey       string
	NameKey        string
	RepoPathKey    string
}

type RepoProviderMetadata struct {
	PlatformRepoID string
	WebURL         string
	CloneURL       string
	DefaultBranch  string
}

type RepoSummary struct {
	Repo                 Repo
	CachedPRCount        int
	OpenPRCount          int
	DraftPRCount         int
	CachedIssueCount     int
	OpenIssueCount       int
	MostRecentActivityAt *time.Time
	Overview             RepoOverview
	ActiveAuthors        []RepoActivityAuthor
	RecentIssues         []RepoIssueHeadline
}

type RepoOverview struct {
	LatestRelease       *RepoRelease
	Releases            []RepoRelease
	CommitsSinceRelease *int
	CommitTimeline      []RepoCommitTimelinePoint
	TimelineUpdatedAt   *time.Time
}

type RepoRelease struct {
	TagName         string
	Name            string
	URL             string
	TargetCommitish string
	Prerelease      bool
	PublishedAt     *time.Time
}

type RepoCommitTimelinePoint struct {
	SHA         string
	Message     string
	CommittedAt time.Time
}

type RepoActivityAuthor struct {
	Login     string
	ItemCount int
}

type RepoIssueHeadline struct {
	Number         int
	Title          string
	Author         string
	State          string
	URL            string
	LastActivityAt time.Time
}

type MergeRequest struct {
	ID                 int64
	RepoID             int64
	PlatformID         int64
	PlatformExternalID string
	Number             int
	URL                string
	Title              string
	Author             string
	AuthorDisplayName  string
	State              string
	IsDraft            bool
	IsLocked           bool
	Body               string
	HeadBranch         string
	BaseBranch         string
	PlatformHeadSHA    string `json:"-"`
	PlatformBaseSHA    string `json:"-"`
	DiffHeadSHA        string `json:"-"`
	DiffBaseSHA        string `json:"-"`
	MergeBaseSHA       string `json:"-"`
	HeadRepoCloneURL   string
	Additions          int
	Deletions          int
	CommentCount       int
	ReviewDecision     string
	CIStatus           string
	CIChecksJSON       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastActivityAt     time.Time
	MergedAt           *time.Time
	ClosedAt           *time.Time
	MergeableState     string
	DetailFetchedAt    *time.Time
	CIHadPending       bool
	KanbanStatus       string
	Starred            bool
	Labels             []Label `json:"labels,omitempty"`
}

func (mr MergeRequest) Compare(other MergeRequest) int {
	return cmp.Compare(mr.Number, other.Number)
}

// CICheck represents a single CI check run.
type CICheck struct {
	Name            string `json:"name"`
	Status          string `json:"status"`     // queued, in_progress, completed
	Conclusion      string `json:"conclusion"` // success, failure, neutral, cancelled, skipped, timed_out, action_required, or empty
	URL             string `json:"url"`        // link to the check run details page
	App             string `json:"app"`        // app name (e.g., "GitHub Actions")
	DurationSeconds *int64 `json:"duration_seconds,omitempty"`
}

func (c CICheck) Compare(other CICheck) int {
	leftFolded := strings.ToLower(c.Name)
	rightFolded := strings.ToLower(other.Name)
	if leftFolded != rightFolded {
		return cmp.Compare(leftFolded, rightFolded)
	}
	return cmp.Compare(c.Name, other.Name)
}

type MREvent struct {
	ID                 int64
	MergeRequestID     int64
	PlatformID         *int64
	PlatformExternalID string
	EventType          string
	Author             string
	Summary            string
	Body               string
	MetadataJSON       string
	CreatedAt          time.Time
	DedupeKey          string
}

type KanbanState struct {
	MergeRequestID int64
	Status         string
	UpdatedAt      time.Time
}

type ListMergeRequestsOpts struct {
	PlatformHost string
	RepoOwner    string
	RepoName     string
	State        string
	KanbanState  string
	Starred      bool
	Search       string
	Limit        int
	Offset       int
}

type Issue struct {
	ID                 int64
	RepoID             int64
	PlatformID         int64
	PlatformExternalID string
	Number             int
	URL                string
	Title              string
	Author             string
	State              string
	Body               string
	CommentCount       int
	LabelsJSON         string `json:"-"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastActivityAt     time.Time
	ClosedAt           *time.Time
	DetailFetchedAt    *time.Time
	Starred            bool
	Labels             []Label `json:"labels,omitempty"`
}

type IssueEvent struct {
	ID                 int64
	IssueID            int64
	PlatformID         *int64
	PlatformExternalID string
	EventType          string
	Author             string
	Summary            string
	Body               string
	MetadataJSON       string
	CreatedAt          time.Time
	DedupeKey          string
}

type CommentAutocompleteReference struct {
	Kind   string `json:"kind"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

type ListIssuesOpts struct {
	PlatformHost string
	RepoOwner    string
	RepoName     string
	State        string
	Starred      bool
	Search       string
	Limit        int
	Offset       int
}

type StarredItem struct {
	ItemType  string
	RepoID    int64
	Number    int
	StarredAt time.Time
}

type Notification struct {
	ID                       int64
	Platform                 string
	PlatformHost             string
	PlatformNotificationID   string
	RepoID                   *int64
	RepoOwner                string
	RepoName                 string
	SubjectType              string
	SubjectTitle             string
	SubjectURL               string
	SubjectLatestCommentURL  string
	WebURL                   string
	ItemNumber               *int
	ItemType                 string
	ItemAuthor               string
	Reason                   string
	Unread                   bool
	Participating            bool
	SourceUpdatedAt          time.Time
	SourceLastAcknowledgedAt *time.Time
	SyncedAt                 time.Time
	DoneAt                   *time.Time
	DoneReason               string
	SourceAckQueuedAt        *time.Time
	SourceAckSyncedAt        *time.Time
	SourceAckGenerationAt    *time.Time
	SourceAckError           string
	SourceAckAttempts        int
	SourceAckLastAttemptAt   *time.Time
	SourceAckNextAttemptAt   *time.Time
}

type ListNotificationsOpts struct {
	Platform     string
	PlatformHost string
	RepoOwner    string
	RepoName     string
	Repos        []NotificationRepoFilter
	State        string
	Reasons      []string
	ItemTypes    []string
	Search       string
	Sort         string
	Limit        int
	Offset       int
}

type NotificationRepoFilter struct {
	Platform     string
	PlatformHost string
	RepoOwner    string
	RepoName     string
}

type NotificationSummary struct {
	TotalActive int
	Unread      int
	Done        int
	ByReason    map[string]int
	ByRepo      map[string]int
}

type NotificationSyncWatermark struct {
	Platform             string
	LastSuccessfulSyncAt time.Time
	LastFullSyncAt       *time.Time
	SyncCursor           string
	TrackedReposKey      string
}

// WorktreeLink associates a merge request with an external worktree.
type WorktreeLink struct {
	ID             int64
	MergeRequestID int64
	WorktreeKey    string
	WorktreePath   string
	WorktreeBranch string
	LinkedAt       time.Time
}

// RateLimit tracks per-host API rate limit state.
type RateLimit struct {
	ID            int64
	Platform      string
	PlatformHost  string
	APIType       string
	RequestsHour  int
	HourStart     time.Time
	RateRemaining int
	RateLimit     int
	RateResetAt   *time.Time
	UpdatedAt     time.Time
}

// ActivityItem represents one row in the unified activity feed.
type ActivityItem struct {
	ActivityType string // new_pr, new_issue, comment, review, commit
	Source       string // pr, issue, pre, ise
	SourceID     int64  // PK from the source table
	Platform     string
	PlatformHost string
	RepoOwner    string
	RepoName     string
	ItemType     string // pr or issue
	ItemNumber   int
	ItemTitle    string
	ItemURL      string
	ItemState    string // open, merged, closed
	Author       string
	CreatedAt    time.Time
	BodyPreview  string
}

// Stack represents a detected chain of dependent PRs.
type Stack struct {
	ID         int64
	RepoID     int64
	BaseNumber int
	Name       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// StackMember links a merge request to a stack with a position.
type StackMember struct {
	StackID        int64
	MergeRequestID int64
	Position       int
}

// StackWithRepo extends Stack with resolved repo owner/name.
type StackWithRepo struct {
	Stack
	RepoOwner string
	RepoName  string
}

// StackMemberWithPR combines stack membership with PR fields needed for display.
type StackMemberWithPR struct {
	StackID        int64
	MergeRequestID int64
	Position       int
	Number         int
	Title          string
	State          string
	CIStatus       string
	ReviewDecision string
	IsDraft        bool
	BaseBranch     string
}

const (
	WorkspaceItemTypePullRequest = "pull_request"
	WorkspaceItemTypeIssue       = "issue"
)

// Workspace represents a middleman-managed git worktree linked to a
// pull request or issue.
type Workspace struct {
	ID                 string
	Platform           string
	PlatformHost       string
	RepoOwner          string
	RepoName           string
	ItemType           string
	ItemNumber         int
	AssociatedPRNumber *int
	GitHeadRef         string
	MRHeadRepo         *string // nil for same-repo PRs
	// WorkspaceBranch is the exact branch name checked out in the
	// worktree after setup. Before setup completes it may contain the
	// requested branch name or workspaceBranchUnknown.
	WorkspaceBranch string
	WorktreePath    string
	TmuxSession     string
	TerminalBackend string
	Status          string // "creating", "ready", "error"
	ErrorMessage    *string
	CreatedAt       time.Time
}

// WorkspaceSummary extends Workspace with joined MR metadata.
type WorkspaceSummary struct {
	Workspace
	MRTitle          *string
	MRState          *string
	MRIsDraft        *bool
	MRCIStatus       *string
	MRReviewDecision *string
	MRAdditions      *int
	MRDeletions      *int
}

type WorkspaceSetupEvent struct {
	ID          int64
	WorkspaceID string
	Stage       string
	Outcome     string
	Message     string
	CreatedAt   time.Time
}

type WorkspaceTmuxSession struct {
	WorkspaceID string
	SessionName string
	TargetKey   string
	CreatedAt   time.Time
}

// ListActivityOpts holds filters and pagination for the activity feed.
type ListActivityOpts struct {
	Repo   string     // "owner/name" filter
	Types  []string   // activity type filter
	Search string     // title/body search
	Limit  int        // page size (default 50, max 200)
	Since  *time.Time // only return events created at or after this time
	// Cursor fields -- decoded from opaque token by the handler.
	BeforeTime     *time.Time
	BeforeSource   string
	BeforeSourceID int64
	AfterTime      *time.Time
	AfterSource    string
	AfterSourceID  int64
}
