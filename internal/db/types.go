package db

import "time"

type Repo struct {
	ID                  int64
	Platform            string
	PlatformHost        string
	Owner               string
	Name                string
	LastSyncStartedAt   *time.Time
	LastSyncCompletedAt *time.Time
	LastSyncError       string
	AllowSquashMerge    bool
	AllowMergeCommit    bool
	AllowRebaseMerge    bool
	CreatedAt           time.Time
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type MergeRequest struct {
	ID                int64
	RepoID            int64
	PlatformID        int64
	Number            int
	URL               string
	Title             string
	Author            string
	AuthorDisplayName string
	State             string
	IsDraft           bool
	Body              string
	HeadBranch        string
	BaseBranch        string
	PlatformHeadSHA   string `json:"-"`
	PlatformBaseSHA   string `json:"-"`
	DiffHeadSHA       string `json:"-"`
	DiffBaseSHA       string `json:"-"`
	MergeBaseSHA      string `json:"-"`
	HeadRepoCloneURL  string
	Additions         int
	Deletions         int
	CommentCount      int
	ReviewDecision    string
	CIStatus          string
	CIChecksJSON      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LastActivityAt    time.Time
	MergedAt          *time.Time
	ClosedAt          *time.Time
	MergeableState    string
	KanbanStatus      string
	Starred           bool
}

// CICheck represents a single CI check run.
type CICheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued, in_progress, completed
	Conclusion string `json:"conclusion"` // success, failure, neutral, cancelled, skipped, timed_out, action_required, or empty
	URL        string `json:"url"`        // link to the check run details page
	App        string `json:"app"`        // app name (e.g., "GitHub Actions")
}

type MREvent struct {
	ID             int64
	MergeRequestID int64
	PlatformID     *int64
	EventType      string
	Author         string
	Summary        string
	Body           string
	MetadataJSON   string
	CreatedAt      time.Time
	DedupeKey      string
}

type KanbanState struct {
	MergeRequestID int64
	Status         string
	UpdatedAt      time.Time
}

type ListMergeRequestsOpts struct {
	RepoOwner   string
	RepoName    string
	State       string
	KanbanState string
	Starred     bool
	Search      string
	Limit       int
	Offset      int
}

type Issue struct {
	ID             int64
	RepoID         int64
	PlatformID     int64
	Number         int
	URL            string
	Title          string
	Author         string
	State          string
	Body           string
	CommentCount   int
	LabelsJSON     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time
	ClosedAt       *time.Time
	Starred        bool
}

type IssueEvent struct {
	ID           int64
	IssueID      int64
	PlatformID   *int64
	EventType    string
	Author       string
	Summary      string
	Body         string
	MetadataJSON string
	CreatedAt    time.Time
	DedupeKey    string
}

type ListIssuesOpts struct {
	RepoOwner string
	RepoName  string
	State     string
	Starred   bool
	Search    string
	Limit     int
	Offset    int
}

type StarredItem struct {
	ItemType  string
	RepoID    int64
	Number    int
	StarredAt time.Time
}

// RateLimit tracks per-host API rate limit state.
type RateLimit struct {
	ID            int64
	PlatformHost  string
	RequestsHour  int
	HourStart     time.Time
	RateRemaining int
	RateResetAt   *time.Time
	UpdatedAt     time.Time
}

// ActivityItem represents one row in the unified activity feed.
type ActivityItem struct {
	ActivityType string // new_pr, new_issue, comment, review, commit
	Source       string // pr, issue, pre, ise
	SourceID     int64  // PK from the source table
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
