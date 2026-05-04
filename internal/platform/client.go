package platform

import "context"

type Provider interface {
	Platform() Kind
	Host() string
	Capabilities() Capabilities
}

type RepositoryReader interface {
	GetRepository(ctx context.Context, ref RepoRef) (Repository, error)
	ListRepositories(
		ctx context.Context,
		owner string,
		opts RepositoryListOptions,
	) ([]Repository, error)
}

type MergeRequestReader interface {
	ListOpenMergeRequests(ctx context.Context, ref RepoRef) ([]MergeRequest, error)
	GetMergeRequest(ctx context.Context, ref RepoRef, number int) (MergeRequest, error)
	ListMergeRequestEvents(
		ctx context.Context,
		ref RepoRef,
		number int,
	) ([]MergeRequestEvent, error)
}

type IssueReader interface {
	ListOpenIssues(ctx context.Context, ref RepoRef) ([]Issue, error)
	GetIssue(ctx context.Context, ref RepoRef, number int) (Issue, error)
	ListIssueEvents(ctx context.Context, ref RepoRef, number int) ([]IssueEvent, error)
}

type ReleaseReader interface {
	ListReleases(ctx context.Context, ref RepoRef) ([]Release, error)
}

type TagReader interface {
	ListTags(ctx context.Context, ref RepoRef) ([]Tag, error)
}

type CIReader interface {
	ListCIChecks(ctx context.Context, ref RepoRef, sha string) ([]CICheck, error)
}

type CommentMutator interface {
	CreateMergeRequestComment(
		ctx context.Context,
		ref RepoRef,
		number int,
		body string,
	) error
	CreateIssueComment(ctx context.Context, ref RepoRef, number int, body string) error
}

type StateMutator interface {
	CloseMergeRequest(ctx context.Context, ref RepoRef, number int) error
	CloseIssue(ctx context.Context, ref RepoRef, number int) error
}

type MergeMutator interface {
	MergeMergeRequest(ctx context.Context, ref RepoRef, number int) error
}

type WorkflowApprovalMutator interface {
	ApproveWorkflow(ctx context.Context, ref RepoRef, runID string) error
}

type ReadyForReviewMutator interface {
	MarkReadyForReview(ctx context.Context, ref RepoRef, number int) error
}
