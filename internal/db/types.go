package db

import "time"

type Repo struct {
	ID                  int64
	Owner               string
	Name                string
	LastSyncStartedAt   *time.Time
	LastSyncCompletedAt *time.Time
	LastSyncError       string
	CreatedAt           time.Time
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type PullRequest struct {
	ID             int64
	RepoID         int64
	GitHubID       int64
	Number         int
	URL            string
	Title          string
	Author         string
	State          string
	IsDraft        bool
	Body           string
	HeadBranch     string
	BaseBranch     string
	Additions      int
	Deletions      int
	CommentCount   int
	ReviewDecision string
	CIStatus       string
	CIChecksJSON   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time
	MergedAt       *time.Time
	ClosedAt       *time.Time
	KanbanStatus   string
}

// CICheck represents a single CI check run.
type CICheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued, in_progress, completed
	Conclusion string `json:"conclusion"` // success, failure, neutral, cancelled, skipped, timed_out, action_required, or empty
	URL        string `json:"url"`        // link to the check run details page
	App        string `json:"app"`        // app name (e.g., "GitHub Actions")
}

type PREvent struct {
	ID           int64
	PRID         int64
	GitHubID     *int64
	EventType    string
	Author       string
	Summary      string
	Body         string
	MetadataJSON string
	CreatedAt    time.Time
	DedupeKey    string
}

type KanbanState struct {
	PRID      int64
	Status    string
	UpdatedAt time.Time
}

type ListPullsOpts struct {
	RepoOwner   string
	RepoName    string
	State       string
	KanbanState string
	Search      string
	Limit       int
	Offset      int
}
