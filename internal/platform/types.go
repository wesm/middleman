package platform

import "time"

type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

type RepoRef struct {
	Platform           Kind
	Host               string
	Owner              string
	Name               string
	RepoPath           string
	PlatformID         int64
	PlatformExternalID string
	WebURL             string
	CloneURL           string
	DefaultBranch      string
}

func (r RepoRef) DisplayName() string {
	if r.RepoPath != "" {
		return r.RepoPath
	}
	if r.Owner == "" {
		return r.Name
	}
	if r.Name == "" {
		return r.Owner
	}
	return r.Owner + "/" + r.Name
}

type Repository struct {
	Ref                RepoRef
	PlatformID         int64
	PlatformExternalID string
	Description        string
	Private            bool
	Archived           bool
	DefaultBranch      string
	WebURL             string
	CloneURL           string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type MergeRequest struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	Number             int
	URL                string
	Title              string
	Author             string
	AuthorDisplayName  string
	State              string
	IsDraft            bool
	Body               string
	HeadBranch         string
	BaseBranch         string
	HeadSHA            string
	BaseSHA            string
	HeadRepoCloneURL   string
	Additions          int
	Deletions          int
	CommentCount       int
	ReviewDecision     string
	CIStatus           string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastActivityAt     time.Time
	MergedAt           *time.Time
	ClosedAt           *time.Time
	Labels             []Label
}

type Issue struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	Number             int
	URL                string
	Title              string
	Author             string
	State              string
	Body               string
	CommentCount       int
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastActivityAt     time.Time
	ClosedAt           *time.Time
	Labels             []Label
}

type Label struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	Name               string
	Description        string
	Color              string
	IsDefault          bool
}

type MergeRequestEvent struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	MergeRequestNumber int
	EventType          string
	Author             string
	Summary            string
	Body               string
	MetadataJSON       string
	CreatedAt          time.Time
	DedupeKey          string
}

type IssueEvent struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	IssueNumber        int
	EventType          string
	Author             string
	Summary            string
	Body               string
	MetadataJSON       string
	CreatedAt          time.Time
	DedupeKey          string
}

type Release struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	TagName            string
	Name               string
	URL                string
	TargetCommitish    string
	Prerelease         bool
	PublishedAt        *time.Time
	CreatedAt          time.Time
}

type Tag struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	Name               string
	SHA                string
	URL                string
}

type CICheck struct {
	Repo               RepoRef
	PlatformID         int64
	PlatformExternalID string
	Name               string
	Status             string
	Conclusion         string
	URL                string
	App                string
	StartedAt          *time.Time
	CompletedAt        *time.Time
}

type Capabilities struct {
	ReadRepositories  bool
	ReadMergeRequests bool
	ReadIssues        bool
	ReadComments      bool
	ReadReleases      bool
	ReadCI            bool
	CommentMutation   bool
	StateMutation     bool
	MergeMutation     bool
	WorkflowApproval  bool
	ReadyForReview    bool
}

type RepositoryListOptions struct {
	Limit  int
	Offset int
}
