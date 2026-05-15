package testutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v84/github"
	ghclient "github.com/wesm/middleman/internal/github"
)

var errFixtureReadOnly = errors.New("fixture client: mutation not supported")
var errFixtureNotFound = errors.New("fixture client: not found")

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
	Reviews                   map[string][]*gh.PullRequestReview
	ReposByOwner              map[string][]*gh.Repository
	Releases                  map[string][]*gh.RepositoryRelease
	Tags                      map[string][]*gh.RepositoryTag
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
		Reviews:          make(map[string][]*gh.PullRequestReview),
		ReposByOwner:     make(map[string][]*gh.Repository),
		Releases:         make(map[string][]*gh.RepositoryRelease),
		Tags:             make(map[string][]*gh.RepositoryTag),
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
	return slices.Clone(repos), nil
}

func (c *FixtureClient) ListReleases(
	_ context.Context, owner, repo string, perPage int,
) ([]*gh.RepositoryRelease, error) {
	releases := c.Releases[repoKey(owner, repo)]
	if len(releases) == 0 {
		return nil, nil
	}
	if perPage > 0 && perPage < len(releases) {
		releases = releases[:perPage]
	}
	return slices.Clone(releases), nil
}

func (c *FixtureClient) ListTags(
	_ context.Context, owner, repo string, perPage int,
) ([]*gh.RepositoryTag, error) {
	tags := c.Tags[repoKey(owner, repo)]
	if len(tags) == 0 {
		return nil, nil
	}
	if perPage > 0 && perPage < len(tags) {
		tags = tags[:perPage]
	}
	return slices.Clone(tags), nil
}

// GetRepository returns a repository with all merge methods enabled.
func (c *FixtureClient) GetRepository(
	_ context.Context, owner, repo string,
) (*gh.Repository, error) {
	t := true
	archived := repo == "archived"
	nodeID := "repo-" + owner + "-" + repo
	return &gh.Repository{
		Name:             &repo,
		NodeID:           &nodeID,
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

func (c *FixtureClient) CreateIssue(
	_ context.Context, owner, repo, title, body string,
) (*gh.Issue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	issuesKey := repoKey(owner, repo)
	maxNumber := 0
	for _, issue := range c.Issues[issuesKey] {
		if n := issue.GetNumber(); n > maxNumber {
			maxNumber = n
		}
	}

	number := maxNumber + 1
	now := gh.Timestamp{Time: time.Now().UTC()}
	state := "open"
	id := c.nextID
	c.nextID++
	labelID := c.nextID
	c.nextID++
	htmlURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number)
	login := "fixture-bot"
	comments := 0
	labelName := "created-from-repos"
	labelDescription := "Issue created from the repositories page"
	labelColor := "0e8a16"
	labelDefault := false

	issue := &gh.Issue{
		ID:               &id,
		Number:           &number,
		Title:            &title,
		Body:             &body,
		State:            &state,
		HTMLURL:          &htmlURL,
		User:             &gh.User{Login: &login},
		Comments:         &comments,
		CreatedAt:        &now,
		UpdatedAt:        &now,
		ClosedAt:         nil,
		PullRequestLinks: nil,
		Labels: []*gh.Label{{
			ID:          &labelID,
			Name:        &labelName,
			Description: &labelDescription,
			Color:       &labelColor,
			Default:     &labelDefault,
		}},
	}
	c.Issues[issuesKey] = append([]*gh.Issue{issue}, c.Issues[issuesKey]...)
	c.OpenIssues[issuesKey] = append([]*gh.Issue{issue}, c.OpenIssues[issuesKey]...)
	return issue, nil
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
	return slices.Clone(comments), nil
}

func (c *FixtureClient) ListIssueCommentsIfChanged(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.IssueComment, error) {
	if len(c.Comments[issueKey(owner, repo, number)]) == 0 {
		return nil, &gh.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusNotModified},
		}
	}
	return c.ListIssueComments(ctx, owner, repo, number)
}

func (c *FixtureClient) ListReviews(
	_ context.Context, owner, repo string, number int,
) ([]*gh.PullRequestReview, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	reviews := c.Reviews[issueKey(owner, repo, number)]
	if len(reviews) == 0 {
		return nil, nil
	}
	return slices.Clone(reviews), nil
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

// ListPullRequestTimelineEvents returns nil (read-only stub).
func (c *FixtureClient) ListPullRequestTimelineEvents(
	_ context.Context, _, _ string, _ int,
) ([]ghclient.PullRequestTimelineEvent, error) {
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
	return slices.Clone(runs), nil
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

func (c *FixtureClient) EditIssueComment(
	_ context.Context, owner, repo string, commentID int64, body string,
) (*gh.IssueComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefix := repoKey(owner, repo) + "#"
	for key, comments := range c.Comments {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for _, comment := range comments {
			if comment.GetID() == commentID {
				comment.Body = &body
				updatedAt := time.Now().UTC()
				comment.UpdatedAt = &gh.Timestamp{Time: updatedAt}
				return comment, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: comment %d", errFixtureNotFound, commentID)
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

// EditPullRequest updates seeded PR fields for E2E mutations.
func (c *FixtureClient) EditPullRequest(
	_ context.Context, owner, repo string, number int, opts ghclient.EditPullRequestOpts,
) (*gh.PullRequest, error) {
	pr := c.findPullRequest(owner, repo, number)
	if pr == nil {
		return nil, nil
	}
	now := gh.Timestamp{Time: time.Now().UTC()}
	if opts.State != nil {
		pr.State = opts.State
		if *opts.State == "closed" {
			pr.ClosedAt = &now
		} else {
			pr.ClosedAt = nil
			pr.MergedAt = nil
			merged := false
			pr.Merged = &merged
		}
	}
	if opts.Title != nil {
		pr.Title = opts.Title
	}
	if opts.Body != nil {
		pr.Body = opts.Body
	}
	pr.UpdatedAt = &now
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
