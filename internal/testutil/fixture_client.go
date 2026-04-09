package testutil

import (
	"context"
	"errors"
	"fmt"

	gh "github.com/google/go-github/v84/github"
	ghclient "github.com/wesm/middleman/internal/github"
)

var errFixtureReadOnly = errors.New("fixture client: mutation not supported")

// FixtureClient is a ghclient.Client implementation for E2E tests. It serves
// seeded PRs and issues from the list methods and stubs out everything else.
type FixtureClient struct {
	OpenPRs    map[string][]*gh.PullRequest
	OpenIssues map[string][]*gh.Issue
}

// NewFixtureClient returns a FixtureClient with empty fixture maps.
func NewFixtureClient() ghclient.Client {
	return &FixtureClient{
		OpenPRs:    make(map[string][]*gh.PullRequest),
		OpenIssues: make(map[string][]*gh.Issue),
	}
}

func repoKey(owner, repo string) string {
	return fmt.Sprintf("%s/%s", owner, repo)
}

// ListOpenPullRequests returns the seeded open PRs for the given repo.
func (c *FixtureClient) ListOpenPullRequests(
	_ context.Context, owner, repo string,
) ([]*gh.PullRequest, error) {
	return c.OpenPRs[repoKey(owner, repo)], nil
}

// ListOpenIssues returns the seeded open issues for the given repo.
func (c *FixtureClient) ListOpenIssues(
	_ context.Context, owner, repo string,
) ([]*gh.Issue, error) {
	return c.OpenIssues[repoKey(owner, repo)], nil
}

// GetUser returns a stub user with the given login.
func (c *FixtureClient) GetUser(_ context.Context, login string) (*gh.User, error) {
	return &gh.User{Login: &login}, nil
}

// GetRepository returns a repository with all merge methods enabled.
func (c *FixtureClient) GetRepository(_ context.Context, _, _ string) (*gh.Repository, error) {
	t := true
	return &gh.Repository{
		AllowSquashMerge: &t,
		AllowMergeCommit: &t,
		AllowRebaseMerge: &t,
	}, nil
}

// GetPullRequest looks up the PR by owner/repo and number from
// the seeded open PR set. Returns nil, nil if not found.
func (c *FixtureClient) GetPullRequest(
	_ context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	for _, pr := range c.OpenPRs[repoKey(owner, repo)] {
		if pr.GetNumber() == number {
			return pr, nil
		}
	}
	return nil, nil
}

// GetIssue looks up the issue by owner/repo and number from
// the seeded open issue set. Returns nil, nil if not found.
func (c *FixtureClient) GetIssue(
	_ context.Context, owner, repo string, number int,
) (*gh.Issue, error) {
	for _, iss := range c.OpenIssues[repoKey(owner, repo)] {
		if iss.GetNumber() == number {
			return iss, nil
		}
	}
	return nil, nil
}

// ListIssueComments returns nil (read-only stub).
func (c *FixtureClient) ListIssueComments(
	_ context.Context, _, _ string, _ int,
) ([]*gh.IssueComment, error) {
	return nil, nil
}

// ListReviews returns nil (read-only stub).
func (c *FixtureClient) ListReviews(
	_ context.Context, _, _ string, _ int,
) ([]*gh.PullRequestReview, error) {
	return nil, nil
}

// ListCommits returns nil (read-only stub).
func (c *FixtureClient) ListCommits(
	_ context.Context, _, _ string, _ int,
) ([]*gh.RepositoryCommit, error) {
	return nil, nil
}

// ListForcePushEvents returns nil (read-only stub).
func (c *FixtureClient) ListForcePushEvents(
	_ context.Context, _, _ string, _ int,
) ([]ghclient.ForcePushEvent, error) {
	return nil, nil
}

// GetCombinedStatus returns nil (read-only stub).
func (c *FixtureClient) GetCombinedStatus(
	_ context.Context, _, _, _ string,
) (*gh.CombinedStatus, error) {
	return nil, nil
}

// ListCheckRunsForRef returns nil (read-only stub).
func (c *FixtureClient) ListCheckRunsForRef(
	_ context.Context, _, _, _ string,
) ([]*gh.CheckRun, error) {
	return nil, nil
}

// ListWorkflowRunsForHeadSHA returns nil (read-only stub).
func (c *FixtureClient) ListWorkflowRunsForHeadSHA(
	_ context.Context, _, _, _ string,
) ([]*gh.WorkflowRun, error) {
	return nil, nil
}

// ApproveWorkflowRun returns an error (mutations not supported).
func (c *FixtureClient) ApproveWorkflowRun(
	_ context.Context, _, _ string, _ int64,
) error {
	return errFixtureReadOnly
}

// CreateIssueComment returns an error (mutations not supported).
func (c *FixtureClient) CreateIssueComment(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.IssueComment, error) {
	return nil, errFixtureReadOnly
}

// CreateReview returns an error (mutations not supported).
func (c *FixtureClient) CreateReview(
	_ context.Context, _, _ string, _ int, _, _ string,
) (*gh.PullRequestReview, error) {
	return nil, errFixtureReadOnly
}

// MarkPullRequestReadyForReview returns an error (mutations not supported).
func (c *FixtureClient) MarkPullRequestReadyForReview(
	_ context.Context, _, _ string, _ int,
) (*gh.PullRequest, error) {
	return nil, errFixtureReadOnly
}

// MergePullRequest returns an error (mutations not supported).
func (c *FixtureClient) MergePullRequest(
	_ context.Context, _, _ string, _ int, _, _, _ string,
) (*gh.PullRequestMergeResult, error) {
	return nil, errFixtureReadOnly
}

// EditPullRequest returns an error (mutations not supported).
func (c *FixtureClient) EditPullRequest(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.PullRequest, error) {
	return nil, errFixtureReadOnly
}

// EditIssue returns an error (mutations not supported).
func (c *FixtureClient) EditIssue(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.Issue, error) {
	return nil, errFixtureReadOnly
}
