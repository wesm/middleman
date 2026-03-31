package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v84/github"
	"golang.org/x/oauth2"
)

// Client is the interface for interacting with the GitHub API.
type Client interface {
	ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*gh.PullRequest, error)
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error)
	GetUser(ctx context.Context, login string) (*gh.User, error)
	ListOpenIssues(ctx context.Context, owner, repo string) ([]*gh.Issue, error)
	GetIssue(ctx context.Context, owner, repo string, number int) (*gh.Issue, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]*gh.IssueComment, error)
	ListReviews(ctx context.Context, owner, repo string, number int) ([]*gh.PullRequestReview, error)
	ListCommits(ctx context.Context, owner, repo string, number int) ([]*gh.RepositoryCommit, error)
	GetCombinedStatus(ctx context.Context, owner, repo, ref string) (*gh.CombinedStatus, error)
	ListCheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*gh.CheckRun, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (*gh.IssueComment, error)
	GetRepository(ctx context.Context, owner, repo string) (*gh.Repository, error)
	CreateReview(ctx context.Context, owner, repo string, number int, event string, body string) (*gh.PullRequestReview, error)
	MarkPullRequestReadyForReview(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error)
	MergePullRequest(ctx context.Context, owner, repo string, number int, commitTitle, commitMessage, method string) (*gh.PullRequestMergeResult, error)
	EditPullRequest(ctx context.Context, owner, repo string, number int, state string) (*gh.PullRequest, error)
	EditIssue(ctx context.Context, owner, repo string, number int, state string) (*gh.Issue, error)
}

// NewClient creates a GitHub Client authenticated with the given token.
func NewClient(token string) Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &liveClient{gh: gh.NewClient(tc)}
}

type liveClient struct {
	gh *gh.Client
}

func (c *liveClient) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*gh.PullRequest, error) {
	opts := &gh.PullRequestListOptions{
		State:       "open",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	all, err := collectPages(ctx, func(pageOpts *gh.ListOptions) ([]*gh.PullRequest, *gh.Response, error) {
		opts.ListOptions = *pageOpts
		page, resp, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("listing open pull requests for %s/%s: %w", owner, repo, err)
		}
		return page, resp, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) ListOpenIssues(
	ctx context.Context, owner, repo string,
) ([]*gh.Issue, error) {
	opts := &gh.IssueListByRepoOptions{
		State:       "open",
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	issues, err := collectPages(ctx, func(pageOpts *gh.ListOptions) ([]*gh.Issue, *gh.Response, error) {
		opts.ListOptions = *pageOpts
		issues, resp, err := c.gh.Issues.ListByRepo(
			ctx, owner, repo, opts,
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"listing issues for %s/%s: %w", owner, repo, err,
			)
		}
		return issues, resp, nil
	})
	if err != nil {
		return nil, err
	}

	var all []*gh.Issue
	// GitHub's Issues API returns PRs too — filter them out.
	for _, issue := range issues {
		if issue.PullRequestLinks == nil {
			all = append(all, issue)
		}
	}
	return all, nil
}

func (c *liveClient) GetIssue(
	ctx context.Context, owner, repo string, number int,
) (*gh.Issue, error) {
	issue, _, err := c.gh.Issues.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf(
			"getting issue %s/%s#%d: %w", owner, repo, number, err,
		)
	}
	return issue, nil
}

func (c *liveClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("getting pull request %s/%s#%d: %w", owner, repo, number, err)
	}
	return pr, nil
}

func (c *liveClient) GetUser(ctx context.Context, login string) (*gh.User, error) {
	user, _, err := c.gh.Users.Get(ctx, login)
	if err != nil {
		return nil, fmt.Errorf("getting user %s: %w", login, err)
	}
	return user, nil
}

func (c *liveClient) ListIssueComments(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.IssueComment, error) {
	opts := &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	all, err := collectPages(ctx, func(pageOpts *gh.ListOptions) ([]*gh.IssueComment, *gh.Response, error) {
		opts.ListOptions = *pageOpts
		page, resp, err := c.gh.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("listing comments for %s/%s#%d: %w", owner, repo, number, err)
		}
		return page, resp, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) ListReviews(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.PullRequestReview, error) {
	all, err := collectPages(ctx, func(opts *gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error) {
		page, resp, err := c.gh.PullRequests.ListReviews(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("listing reviews for %s/%s#%d: %w", owner, repo, number, err)
		}
		return page, resp, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) ListCommits(
	ctx context.Context, owner, repo string, number int,
) ([]*gh.RepositoryCommit, error) {
	all, err := collectPages(ctx, func(opts *gh.ListOptions) ([]*gh.RepositoryCommit, *gh.Response, error) {
		page, resp, err := c.gh.PullRequests.ListCommits(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("listing commits for %s/%s#%d: %w", owner, repo, number, err)
		}
		return page, resp, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) GetCombinedStatus(
	ctx context.Context, owner, repo, ref string,
) (*gh.CombinedStatus, error) {
	status, _, err := c.gh.Repositories.GetCombinedStatus(ctx, owner, repo, ref, nil)
	if err != nil {
		return nil, fmt.Errorf("getting combined status for %s/%s@%s: %w", owner, repo, ref, err)
	}
	return status, nil
}

func (c *liveClient) ListCheckRunsForRef(
	ctx context.Context, owner, repo, ref string,
) ([]*gh.CheckRun, error) {
	opts := &gh.ListCheckRunsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	all, err := collectPages(ctx, func(pageOpts *gh.ListOptions) ([]*gh.CheckRun, *gh.Response, error) {
		opts.ListOptions = *pageOpts
		result, resp, err := c.gh.Checks.ListCheckRunsForRef(
			ctx, owner, repo, ref, opts,
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"listing check runs for %s/%s@%s: %w",
				owner, repo, ref, err,
			)
		}
		return result.CheckRuns, resp, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) CreateIssueComment(
	ctx context.Context, owner, repo string, number int, body string,
) (*gh.IssueComment, error) {
	comment, _, err := c.gh.Issues.CreateComment(ctx, owner, repo, number, &gh.IssueComment{
		Body: new(body),
	})
	if err != nil {
		return nil, fmt.Errorf("creating comment on %s/%s#%d: %w", owner, repo, number, err)
	}
	return comment, nil
}

func (c *liveClient) GetRepository(
	ctx context.Context, owner, repo string,
) (*gh.Repository, error) {
	r, _, err := c.gh.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("getting repository %s/%s: %w", owner, repo, err)
	}
	return r, nil
}

func (c *liveClient) CreateReview(
	ctx context.Context, owner, repo string, number int,
	event string, body string,
) (*gh.PullRequestReview, error) {
	review, _, err := c.gh.PullRequests.CreateReview(
		ctx, owner, repo, number, &gh.PullRequestReviewRequest{
			Event: new(event),
			Body:  new(body),
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"creating review on %s/%s#%d: %w", owner, repo, number, err,
		)
	}
	return review, nil
}

func (c *liveClient) MarkPullRequestReadyForReview(
	ctx context.Context, owner, repo string, number int,
) (*gh.PullRequest, error) {
	req, err := c.gh.NewRequest(
		"POST",
		fmt.Sprintf("repos/%s/%s/pulls/%d/ready_for_review", owner, repo, number),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"building ready-for-review request for %s/%s#%d: %w",
			owner, repo, number, err,
		)
	}

	pr := new(gh.PullRequest)
	if _, err := c.gh.Do(ctx, req, pr); err != nil {
		return nil, fmt.Errorf(
			"marking %s/%s#%d ready for review: %w",
			owner, repo, number, err,
		)
	}

	return pr, nil
}

func (c *liveClient) MergePullRequest(
	ctx context.Context, owner, repo string, number int,
	commitTitle, commitMessage, method string,
) (*gh.PullRequestMergeResult, error) {
	opts := &gh.PullRequestOptions{
		CommitTitle: commitTitle,
		MergeMethod: method,
	}
	result, _, err := c.gh.PullRequests.Merge(
		ctx, owner, repo, number, commitMessage, opts,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"merging %s/%s#%d: %w", owner, repo, number, err,
		)
	}
	return result, nil
}

func (c *liveClient) EditPullRequest(
	ctx context.Context, owner, repo string, number int, state string,
) (*gh.PullRequest, error) {
	pr, _, err := c.gh.PullRequests.Edit(
		ctx, owner, repo, number, &gh.PullRequest{State: &state},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"editing pull request %s/%s#%d: %w",
			owner, repo, number, err,
		)
	}
	return pr, nil
}

func (c *liveClient) EditIssue(
	ctx context.Context, owner, repo string, number int, state string,
) (*gh.Issue, error) {
	issue, _, err := c.gh.Issues.Edit(
		ctx, owner, repo, number, &gh.IssueRequest{State: &state},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"editing issue %s/%s#%d: %w",
			owner, repo, number, err,
		)
	}
	return issue, nil
}
