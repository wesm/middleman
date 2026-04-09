package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v84/github"
	"golang.org/x/oauth2"
)

type ForcePushEvent struct {
	Actor     string
	BeforeSHA string
	AfterSHA  string
	Ref       string
	CreatedAt time.Time
}

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
	ListForcePushEvents(ctx context.Context, owner, repo string, number int) ([]ForcePushEvent, error)
	GetCombinedStatus(ctx context.Context, owner, repo, ref string) (*gh.CombinedStatus, error)
	ListCheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*gh.CheckRun, error)
	ListWorkflowRunsForHeadSHA(ctx context.Context, owner, repo, headSHA string) ([]*gh.WorkflowRun, error)
	ApproveWorkflowRun(ctx context.Context, owner, repo string, runID int64) error
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (*gh.IssueComment, error)
	GetRepository(ctx context.Context, owner, repo string) (*gh.Repository, error)
	CreateReview(ctx context.Context, owner, repo string, number int, event string, body string) (*gh.PullRequestReview, error)
	MarkPullRequestReadyForReview(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error)
	MergePullRequest(ctx context.Context, owner, repo string, number int, commitTitle, commitMessage, method string) (*gh.PullRequestMergeResult, error)
	EditPullRequest(ctx context.Context, owner, repo string, number int, state string) (*gh.PullRequest, error)
	EditIssue(ctx context.Context, owner, repo string, number int, state string) (*gh.Issue, error)
	ListPullRequestsPage(ctx context.Context, owner, repo, state string, page int) ([]*gh.PullRequest, bool, error)
	ListIssuesPage(ctx context.Context, owner, repo, state string, page int) ([]*gh.Issue, bool, error)
}

func graphQLEndpointForHost(platformHost string) string {
	if platformHost == "" || platformHost == "github.com" {
		return "https://api.github.com/graphql"
	}
	return "https://" + platformHost + "/api/graphql"
}

// NewClient creates a GitHub Client authenticated with the given
// token. platformHost selects the API endpoint: "" or "github.com"
// uses the public API; any other value creates an Enterprise
// client. rateTracker may be nil if rate tracking is not needed.
func NewClient(
	token string,
	platformHost string,
	rateTracker *RateTracker,
) (Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(context.Background(), ts)

	var ghClient *gh.Client
	if platformHost == "" || platformHost == "github.com" {
		ghClient = gh.NewClient(tc)
	} else {
		baseURL := "https://" + platformHost + "/api/v3/"
		uploadURL := "https://" + platformHost +
			"/api/uploads/"
		var err error
		ghClient, err = gh.NewClient(tc).
			WithEnterpriseURLs(baseURL, uploadURL)
		if err != nil {
			return nil, fmt.Errorf(
				"create enterprise client: %w", err,
			)
		}
	}
	return &liveClient{
		gh:              ghClient,
		httpClient:      tc,
		rateTracker:     rateTracker,
		graphQLEndpoint: graphQLEndpointForHost(platformHost),
	}, nil
}

type liveClient struct {
	gh              *gh.Client
	httpClient      *http.Client
	rateTracker     *RateTracker
	graphQLEndpoint string
}

const forcePushTimelineQuery = `
query($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      timelineItems(itemTypes: [HEAD_REF_FORCE_PUSHED_EVENT], first: 100, after: $cursor) {
        nodes {
          ... on HeadRefForcePushedEvent {
            actor { login }
            beforeCommit { oid }
            afterCommit { oid }
            createdAt
            ref { name }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}`

// trackRate records the request and updates rate limit state
// from the response. Safe to call with nil response or nil
// tracker.
func (c *liveClient) trackRate(resp *gh.Response) {
	if resp == nil || c.rateTracker == nil {
		return
	}
	c.rateTracker.RecordRequest()
	c.rateTracker.UpdateFromRate(resp.Rate)
}

func (c *liveClient) trackRateHeaders(resp *http.Response) {
	if resp == nil || c.rateTracker == nil {
		return
	}
	c.rateTracker.RecordRequest()
	remaining, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	resetUnix, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return
	}
	c.rateTracker.UpdateFromRate(gh.Rate{
		Remaining: remaining,
		Reset:     gh.Timestamp{Time: time.Unix(resetUnix, 0).UTC()},
	})
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
	}, c.trackRate)
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
	}, c.trackRate)
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
	issue, resp, err := c.gh.Issues.Get(ctx, owner, repo, number)
	c.trackRate(resp)
	if err != nil {
		return nil, fmt.Errorf(
			"getting issue %s/%s#%d: %w", owner, repo, number, err,
		)
	}
	return issue, nil
}

func (c *liveClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, error) {
	pr, resp, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	c.trackRate(resp)
	if err != nil {
		return nil, fmt.Errorf("getting pull request %s/%s#%d: %w", owner, repo, number, err)
	}
	return pr, nil
}

func (c *liveClient) GetUser(ctx context.Context, login string) (*gh.User, error) {
	user, resp, err := c.gh.Users.Get(ctx, login)
	c.trackRate(resp)
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
	}, c.trackRate)
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
	}, c.trackRate)
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
	}, c.trackRate)
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) ListForcePushEvents(
	ctx context.Context, owner, repo string, number int,
) ([]ForcePushEvent, error) {
	type graphQLRequest struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	type graphQLResponse struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Repository *struct {
				PullRequest *struct {
					TimelineItems struct {
						Nodes []struct {
							Actor *struct {
								Login string `json:"login"`
							} `json:"actor"`
							BeforeCommit *struct {
								OID string `json:"oid"`
							} `json:"beforeCommit"`
							AfterCommit *struct {
								OID string `json:"oid"`
							} `json:"afterCommit"`
							CreatedAt time.Time `json:"createdAt"`
							Ref       *struct {
								Name string `json:"name"`
							} `json:"ref"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool    `json:"hasNextPage"`
							EndCursor   *string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"timelineItems"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	var events []ForcePushEvent
	var cursor *string
	for {
		payload, err := json.Marshal(graphQLRequest{
			Query: forcePushTimelineQuery,
			Variables: map[string]any{
				"owner":  owner,
				"repo":   repo,
				"number": number,
				"cursor": cursor,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("marshal force-push query: %w", err)
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.graphQLEndpoint,
			bytes.NewReader(payload),
		)
		if err != nil {
			return nil, fmt.Errorf("create force-push request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf(
				"list force-push events for %s/%s#%d: %w",
				owner, repo, number, err,
			)
		}
		c.trackRateHeaders(resp)
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf(
				"list force-push events for %s/%s#%d: graphql status %s",
				owner, repo, number, resp.Status,
			)
		}

		var decoded graphQLResponse
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf(
				"decode force-push events for %s/%s#%d: %w",
				owner, repo, number, err,
			)
		}
		_ = resp.Body.Close()

		if len(decoded.Errors) > 0 {
			messages := make([]string, 0, len(decoded.Errors))
			for _, graphQLError := range decoded.Errors {
				if graphQLError.Message != "" {
					messages = append(messages, graphQLError.Message)
				}
			}
			if len(messages) == 0 {
				messages = append(messages, "unknown GraphQL error")
			}
			return nil, fmt.Errorf(
				"list force-push events for %s/%s#%d: graphql errors: %s",
				owner, repo, number, strings.Join(messages, "; "),
			)
		}

		if decoded.Data.Repository == nil {
			return nil, fmt.Errorf(
				"list force-push events for %s/%s#%d: missing repository in graphql response",
				owner, repo, number,
			)
		}
		if decoded.Data.Repository.PullRequest == nil {
			return nil, fmt.Errorf(
				"list force-push events for %s/%s#%d: missing pull request in graphql response",
				owner, repo, number,
			)
		}

		for _, node := range decoded.Data.Repository.PullRequest.TimelineItems.Nodes {
			event := ForcePushEvent{CreatedAt: node.CreatedAt}
			if node.Actor != nil {
				event.Actor = node.Actor.Login
			}
			if node.BeforeCommit != nil {
				event.BeforeSHA = node.BeforeCommit.OID
			}
			if node.AfterCommit != nil {
				event.AfterSHA = node.AfterCommit.OID
			}
			if node.Ref != nil {
				event.Ref = node.Ref.Name
			}
			events = append(events, event)
		}

		pageInfo := decoded.Data.Repository.PullRequest.TimelineItems.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		cursor = pageInfo.EndCursor
	}

	return events, nil
}

func (c *liveClient) GetCombinedStatus(
	ctx context.Context, owner, repo, ref string,
) (*gh.CombinedStatus, error) {
	status, resp, err := c.gh.Repositories.GetCombinedStatus(ctx, owner, repo, ref, nil)
	c.trackRate(resp)
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
	}, c.trackRate)
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) ListWorkflowRunsForHeadSHA(
	ctx context.Context, owner, repo, headSHA string,
) ([]*gh.WorkflowRun, error) {
	opts := &gh.ListWorkflowRunsOptions{
		HeadSHA:     headSHA,
		Status:      "action_required",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	all, err := collectPages(ctx, func(pageOpts *gh.ListOptions) ([]*gh.WorkflowRun, *gh.Response, error) {
		opts.ListOptions = *pageOpts
		result, resp, err := c.gh.Actions.ListRepositoryWorkflowRuns(
			ctx, owner, repo, opts,
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"listing workflow runs for %s/%s@%s: %w",
				owner, repo, headSHA, err,
			)
		}
		return result.WorkflowRuns, resp, nil
	}, c.trackRate)
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (c *liveClient) ApproveWorkflowRun(
	ctx context.Context, owner, repo string, runID int64,
) error {
	req, err := c.gh.NewRequest(
		"POST",
		fmt.Sprintf("repos/%s/%s/actions/runs/%d/approve", owner, repo, runID),
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"building workflow approval request for %s/%s run %d: %w",
			owner, repo, runID, err,
		)
	}

	resp, err := c.gh.Do(ctx, req, nil)
	c.trackRate(resp)
	if err != nil {
		return fmt.Errorf(
			"approving workflow run %s/%s#%d: %w",
			owner, repo, runID, err,
		)
	}
	return nil
}

func (c *liveClient) CreateIssueComment(
	ctx context.Context, owner, repo string, number int, body string,
) (*gh.IssueComment, error) {
	comment, resp, err := c.gh.Issues.CreateComment(ctx, owner, repo, number, &gh.IssueComment{
		Body: new(body),
	})
	c.trackRate(resp)
	if err != nil {
		return nil, fmt.Errorf("creating comment on %s/%s#%d: %w", owner, repo, number, err)
	}
	return comment, nil
}

func (c *liveClient) GetRepository(
	ctx context.Context, owner, repo string,
) (*gh.Repository, error) {
	r, resp, err := c.gh.Repositories.Get(ctx, owner, repo)
	c.trackRate(resp)
	if err != nil {
		return nil, fmt.Errorf("getting repository %s/%s: %w", owner, repo, err)
	}
	return r, nil
}

func (c *liveClient) CreateReview(
	ctx context.Context, owner, repo string, number int,
	event string, body string,
) (*gh.PullRequestReview, error) {
	review, resp, err := c.gh.PullRequests.CreateReview(
		ctx, owner, repo, number, &gh.PullRequestReviewRequest{
			Event: new(event),
			Body:  new(body),
		},
	)
	c.trackRate(resp)
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
	resp, err := c.gh.Do(ctx, req, pr)
	c.trackRate(resp)
	if err != nil {
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
	result, resp, err := c.gh.PullRequests.Merge(
		ctx, owner, repo, number, commitMessage, opts,
	)
	c.trackRate(resp)
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
	pr, resp, err := c.gh.PullRequests.Edit(
		ctx, owner, repo, number, &gh.PullRequest{State: &state},
	)
	c.trackRate(resp)
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
	issue, resp, err := c.gh.Issues.Edit(
		ctx, owner, repo, number, &gh.IssueRequest{State: &state},
	)
	c.trackRate(resp)
	if err != nil {
		return nil, fmt.Errorf(
			"editing issue %s/%s#%d: %w",
			owner, repo, number, err,
		)
	}
	return issue, nil
}

func (c *liveClient) ListPullRequestsPage(
	ctx context.Context, owner, repo, state string, page int,
) ([]*gh.PullRequest, bool, error) {
	opts := &gh.PullRequestListOptions{
		State:     state,
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gh.ListOptions{
			Page:    page,
			PerPage: 100,
		},
	}
	prs, resp, err := c.gh.PullRequests.List(
		ctx, owner, repo, opts,
	)
	c.trackRate(resp)
	if err != nil {
		return nil, false, fmt.Errorf(
			"list %s PRs page %d for %s/%s: %w",
			state, page, owner, repo, err,
		)
	}
	hasMore := resp != nil && resp.NextPage > 0
	return prs, hasMore, nil
}

func (c *liveClient) ListIssuesPage(
	ctx context.Context, owner, repo, state string, page int,
) ([]*gh.Issue, bool, error) {
	opts := &gh.IssueListByRepoOptions{
		State:     state,
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gh.ListOptions{
			Page:    page,
			PerPage: 100,
		},
	}
	issues, resp, err := c.gh.Issues.ListByRepo(
		ctx, owner, repo, opts,
	)
	c.trackRate(resp)
	if err != nil {
		return nil, false, fmt.Errorf(
			"list %s issues page %d for %s/%s: %w",
			state, page, owner, repo, err,
		)
	}
	// Filter out PRs (GitHub Issues API returns them).
	var filtered []*gh.Issue
	for _, issue := range issues {
		if issue.PullRequestLinks == nil {
			filtered = append(filtered, issue)
		}
	}
	hasMore := resp != nil && resp.NextPage > 0
	return filtered, hasMore, nil
}
