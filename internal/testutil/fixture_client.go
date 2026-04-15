package testutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gh "github.com/google/go-github/v84/github"
	ghclient "github.com/wesm/middleman/internal/github"
)

var errFixtureReadOnly = errors.New("fixture client: mutation not supported")

type fixtureReadyForReviewStaleStateError struct {
	message string
}

func (e *fixtureReadyForReviewStaleStateError) Error() string      { return e.message }
func (e *fixtureReadyForReviewStaleStateError) StatusCode() int    { return http.StatusNotFound }
func (e *fixtureReadyForReviewStaleStateError) IsStaleState() bool { return true }

// FixtureClient is a ghclient.Client implementation for E2E tests. It serves
// seeded PRs and issues from the list methods and stubs out everything else.
type FixtureClient struct {
	OpenPRs                   map[string][]*gh.PullRequest
	PRs                       map[string][]*gh.PullRequest
	OpenIssues                map[string][]*gh.Issue
	Issues                    map[string][]*gh.Issue
	Comments                  map[string][]*gh.IssueComment
	ReposByOwner              map[string][]*gh.Repository
	CombinedStatuses          map[string]*gh.CombinedStatus
	CheckRuns                 map[string][]*gh.CheckRun
	ListRepositoriesByOwnerFn func(context.Context, string) ([]*gh.Repository, error)
	mu                        sync.Mutex
	nextID                    int64
}

// NewFixtureClient returns a FixtureClient with empty fixture maps.
func NewFixtureClient() ghclient.Client {
	return &FixtureClient{
		OpenPRs:          make(map[string][]*gh.PullRequest),
		PRs:              make(map[string][]*gh.PullRequest),
		OpenIssues:       make(map[string][]*gh.Issue),
		Issues:           make(map[string][]*gh.Issue),
		Comments:         make(map[string][]*gh.IssueComment),
		ReposByOwner:     make(map[string][]*gh.Repository),
		CombinedStatuses: make(map[string]*gh.CombinedStatus),
		CheckRuns:        make(map[string][]*gh.CheckRun),
		nextID:           10_000,
	}
}

func repoKey(owner, repo string) string {
	return fmt.Sprintf("%s/%s", owner, repo)
}

func issueKey(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s#%d", owner, repo, number)
}

func refKey(owner, repo, ref string) string {
	return fmt.Sprintf("%s/%s@%s", owner, repo, ref)
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

func (c *FixtureClient) ListRepositoriesByOwner(
	ctx context.Context, owner string,
) ([]*gh.Repository, error) {
	if c.ListRepositoriesByOwnerFn != nil {
		return c.ListRepositoriesByOwnerFn(ctx, owner)
	}
	repos := c.ReposByOwner[owner]
	if len(repos) == 0 {
		return nil, nil
	}
	out := make([]*gh.Repository, len(repos))
	copy(out, repos)
	return out, nil
}

// GetRepository returns a repository with all merge methods enabled.
func (c *FixtureClient) GetRepository(
	_ context.Context, owner, repo string,
) (*gh.Repository, error) {
	t := true
	archived := repo == "archived"
	return &gh.Repository{
		Name:             &repo,
		Owner:            &gh.User{Login: &owner},
		Archived:         &archived,
		AllowSquashMerge: &t,
		AllowMergeCommit: &t,
		AllowRebaseMerge: &t,
	}, nil
}

// GetPullRequest looks up the PR by owner/repo and number from
// the seeded fixture set. Returns nil, nil if not found.
func (c *FixtureClient) GetPullRequest(
	_ context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	for _, pr := range c.PRs[repoKey(owner, repo)] {
		if pr.GetNumber() == number {
			return pr, nil
		}
	}
	return nil, nil
}

func (c *FixtureClient) findPullRequest(owner, repo string, number int) *gh.PullRequest {
	for _, pr := range c.OpenPRs[repoKey(owner, repo)] {
		if pr.GetNumber() == number {
			return pr
		}
	}
	return nil
}

func (c *FixtureClient) updatePullRequestDraft(owner, repo string, number int, draft bool) *gh.PullRequest {
	var updated *gh.PullRequest
	now := gh.Timestamp{Time: time.Now().UTC()}
	for _, prs := range []map[string][]*gh.PullRequest{c.OpenPRs, c.PRs} {
		for _, pr := range prs[repoKey(owner, repo)] {
			if pr.GetNumber() != number {
				continue
			}
			pr.Draft = new(draft)
			pr.UpdatedAt = &now
			if updated == nil {
				updated = pr
			}
		}
	}
	return updated
}

// GetIssue looks up the issue by owner/repo and number from
// the seeded fixture set. Returns nil, nil if not found.
func (c *FixtureClient) GetIssue(
	_ context.Context, owner, repo string, number int,
) (*gh.Issue, error) {
	for _, iss := range c.Issues[repoKey(owner, repo)] {
		if iss.GetNumber() == number {
			return iss, nil
		}
	}
	return nil, nil
}

func (c *FixtureClient) findIssue(owner, repo string, number int) *gh.Issue {
	for _, issue := range c.OpenIssues[repoKey(owner, repo)] {
		if issue.GetNumber() == number {
			return issue
		}
	}
	return nil
}

// ListIssueComments returns nil (read-only stub).
func (c *FixtureClient) ListIssueComments(
	_ context.Context, owner, repo string, number int,
) ([]*gh.IssueComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	comments := c.Comments[issueKey(owner, repo, number)]
	if len(comments) == 0 {
		return nil, nil
	}
	out := make([]*gh.IssueComment, len(comments))
	copy(out, comments)
	return out, nil
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

// GetCombinedStatus returns a seeded combined status by repo/ref.
func (c *FixtureClient) GetCombinedStatus(
	_ context.Context, owner, repo, ref string,
) (*gh.CombinedStatus, error) {
	return c.CombinedStatuses[refKey(owner, repo, ref)], nil
}

// ListCheckRunsForRef returns seeded check runs by repo/ref.
func (c *FixtureClient) ListCheckRunsForRef(
	_ context.Context, owner, repo, ref string,
) ([]*gh.CheckRun, error) {
	runs := c.CheckRuns[refKey(owner, repo, ref)]
	if len(runs) == 0 {
		return nil, nil
	}
	out := make([]*gh.CheckRun, len(runs))
	copy(out, runs)
	return out, nil
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

func (c *FixtureClient) CreateIssueComment(
	_ context.Context, owner, repo string, number int, body string,
) (*gh.IssueComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	login := "fixture-bot"
	now := time.Now().UTC()
	id := c.nextID
	c.nextID++

	comment := &gh.IssueComment{
		ID:        &id,
		Body:      &body,
		CreatedAt: &gh.Timestamp{Time: now},
		User:      &gh.User{Login: &login},
	}
	key := issueKey(owner, repo, number)
	c.Comments[key] = append(c.Comments[key], comment)
	return comment, nil
}

// CreateReview returns an error (mutations not supported).
func (c *FixtureClient) CreateReview(
	_ context.Context, _, _ string, _ int, _, _ string,
) (*gh.PullRequestReview, error) {
	return nil, errFixtureReadOnly
}

func (c *FixtureClient) MarkPullRequestReadyForReview(
	_ context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pr := c.updatePullRequestDraft(owner, repo, number, false)
	if pr == nil {
		return nil, nil
	}
	if number == 6 {
		return nil, &fixtureReadyForReviewStaleStateError{
			message: fmt.Sprintf(
				"marking %s/%s#%d ready for review: graphql errors: Could not resolve to a PullRequest with the global id of 'PR_fixture_%d'.",
				owner, repo, number, number,
			),
		}
	}
	return pr, nil
}

// MergePullRequest returns an error (mutations not supported).
func (c *FixtureClient) MergePullRequest(
	_ context.Context, owner, repo string, number int, _, _, _ string,
) (*gh.PullRequestMergeResult, error) {
	pr := c.findPullRequest(owner, repo, number)
	if pr == nil {
		return nil, nil
	}
	now := gh.Timestamp{Time: time.Now().UTC()}
	state := "closed"
	merged := true
	pr.State = &state
	pr.Merged = &merged
	pr.ClosedAt = &now
	pr.MergedAt = &now
	sha := pr.GetHead().GetSHA()
	msg := "merged"
	return &gh.PullRequestMergeResult{SHA: &sha, Merged: &merged, Message: &msg}, nil
}

// EditPullRequest updates the seeded PR state for E2E mutations.
func (c *FixtureClient) EditPullRequest(
	_ context.Context, owner, repo string, number int, state string,
) (*gh.PullRequest, error) {
	pr := c.findPullRequest(owner, repo, number)
	if pr == nil {
		return nil, nil
	}
	now := gh.Timestamp{Time: time.Now().UTC()}
	pr.State = &state
	if state == "closed" {
		pr.ClosedAt = &now
	} else {
		pr.ClosedAt = nil
		pr.MergedAt = nil
		merged := false
		pr.Merged = &merged
	}
	return pr, nil
}

// EditIssue updates the seeded issue state for E2E mutations.
func (c *FixtureClient) EditIssue(
	_ context.Context, owner, repo string, number int, state string,
) (*gh.Issue, error) {
	issue := c.findIssue(owner, repo, number)
	if issue == nil {
		return nil, nil
	}
	now := gh.Timestamp{Time: time.Now().UTC()}
	issue.State = &state
	if state == "closed" {
		issue.ClosedAt = &now
	} else {
		issue.ClosedAt = nil
	}
	return issue, nil
}

// ListPullRequestsPage returns nil (read-only stub).
func (c *FixtureClient) ListPullRequestsPage(
	_ context.Context, _, _, _ string, _ int,
) ([]*gh.PullRequest, bool, error) {
	return nil, false, nil
}

// ListIssuesPage returns nil (read-only stub).
func (c *FixtureClient) ListIssuesPage(
	_ context.Context, _, _, _ string, _ int,
) ([]*gh.Issue, bool, error) {
	return nil, false, nil
}

// InvalidateListETagsForRepo is a no-op for the fixture client,
// which has no underlying HTTP cache.
func (c *FixtureClient) InvalidateListETagsForRepo(_, _ string, _ ...string) {}
