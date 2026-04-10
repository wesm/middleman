package github

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// topLevelPageSize is the number of PRs fetched per GraphQL
// query page. Kept conservative to stay under GitHub's 500k
// node limit even with nested connections.
const topLevelPageSize = 25

// retryPageSize is used when the initial query fails (e.g.,
// complexity/node limit error). Half the default, minimum 5.
const retryPageSize = 12

// --- GraphQL query types (private) ---

type gqlPRQuery struct {
	Repository struct {
		PullRequests struct {
			Nodes    []gqlPR
			PageInfo pageInfo
		} `graphql:"pullRequests(first: $pageSize, states: OPEN, after: $cursor)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

type gqlPR struct {
	DatabaseId     int64 `graphql:"databaseId"`
	Number         int
	Title          string
	State          string
	IsDraft        bool
	Body           string
	URL            string
	Author         struct{ Login string }
	CreatedAt      time.Time
	UpdatedAt      time.Time
	MergedAt       *time.Time
	ClosedAt       *time.Time
	Additions      int
	Deletions      int
	Mergeable      string
	ReviewDecision string
	HeadRefName    string
	BaseRefName    string
	HeadRefOid     string `graphql:"headRefOid"`
	BaseRefOid     string `graphql:"baseRefOid"`
	HeadRepository *struct {
		URL string
	}
	Comments struct {
		Nodes    []gqlComment
		PageInfo pageInfo
	} `graphql:"comments(first: 100)"`
	Reviews struct {
		Nodes    []gqlReview
		PageInfo pageInfo
	} `graphql:"reviews(first: 100)"`
	AllCommits struct {
		Nodes    []gqlCommitNode
		PageInfo pageInfo
	} `graphql:"allCommits: commits(first: 100)"`
	LastCommit struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					Contexts struct {
						Nodes    []gqlCheckContext
						PageInfo pageInfo
					} `graphql:"contexts(first: 100)"`
				}
			}
		}
	} `graphql:"lastCommit: commits(last: 1)"`
}

type gqlComment struct {
	DatabaseId int64
	Author     struct{ Login string }
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type gqlReview struct {
	DatabaseId  int64
	Author      struct{ Login string }
	Body        string
	State       string
	SubmittedAt time.Time
}

type gqlCommitNode struct {
	Commit gqlCommit
}

type gqlCommit struct {
	OID     string `graphql:"oid"`
	Message string
	Author  struct {
		Name string
		Date time.Time
		User *struct{ Login string }
	}
}

type gqlCheckContext struct {
	Typename      string                 `graphql:"__typename"`
	CheckRun      gqlCheckRunFields      `graphql:"... on CheckRun"`
	StatusContext gqlStatusContextFields `graphql:"... on StatusContext"`
}

type gqlCheckRunFields struct {
	Name       string
	Status     string
	Conclusion string
	DetailsURL string `graphql:"detailsUrl"`
	CheckSuite struct {
		App struct {
			Name string
		}
	}
}

type gqlStatusContextFields struct {
	Context   string
	State     string
	TargetURL string `graphql:"targetUrl"`
}

// --- Adapter functions ---

func adaptPR(gql *gqlPR) *gh.PullRequest {
	state := stateToREST(gql.State)
	pr := &gh.PullRequest{
		ID:        new(gql.DatabaseId),
		Number:    new(gql.Number),
		Title:     new(gql.Title),
		State:     new(state),
		Draft:     new(gql.IsDraft),
		Body:      new(gql.Body),
		HTMLURL:   new(gql.URL),
		Additions: new(gql.Additions),
		Deletions: new(gql.Deletions),
		User:      &gh.User{Login: new(gql.Author.Login)},
		Head: &gh.PullRequestBranch{
			Ref: new(gql.HeadRefName),
			SHA: new(gql.HeadRefOid),
		},
		Base: &gh.PullRequestBranch{
			Ref: new(gql.BaseRefName),
			SHA: new(gql.BaseRefOid),
		},
		MergeableState: new(mergeableToREST(gql.Mergeable)),
	}

	created := gh.Timestamp{Time: gql.CreatedAt}
	updated := gh.Timestamp{Time: gql.UpdatedAt}
	pr.CreatedAt = &created
	pr.UpdatedAt = &updated

	if gql.MergedAt != nil {
		t := gh.Timestamp{Time: *gql.MergedAt}
		pr.MergedAt = &t
		pr.Merged = new(true)
	}
	if gql.ClosedAt != nil {
		t := gh.Timestamp{Time: *gql.ClosedAt}
		pr.ClosedAt = &t
	}
	if gql.HeadRepository != nil {
		pr.Head.Repo = &gh.Repository{
			CloneURL: new(gql.HeadRepository.URL),
		}
	}

	return pr
}

func stateToREST(graphqlState string) string {
	switch graphqlState {
	case "MERGED":
		return "closed"
	case "CLOSED":
		return "closed"
	default:
		return "open"
	}
}

func mergeableToREST(mergeable string) string {
	switch mergeable {
	case "MERGEABLE":
		return "clean"
	case "CONFLICTING":
		return "dirty"
	default:
		return "unknown"
	}
}

func adaptComment(gql *gqlComment) *gh.IssueComment {
	created := gh.Timestamp{Time: gql.CreatedAt}
	updated := gh.Timestamp{Time: gql.UpdatedAt}
	return &gh.IssueComment{
		ID:        new(gql.DatabaseId),
		Body:      new(gql.Body),
		User:      &gh.User{Login: new(gql.Author.Login)},
		CreatedAt: &created,
		UpdatedAt: &updated,
	}
}

func adaptReview(gql *gqlReview) *gh.PullRequestReview {
	submitted := gh.Timestamp{Time: gql.SubmittedAt}
	return &gh.PullRequestReview{
		ID:          new(gql.DatabaseId),
		Body:        new(gql.Body),
		State:       new(gql.State),
		User:        &gh.User{Login: new(gql.Author.Login)},
		SubmittedAt: &submitted,
	}
}

func adaptCommit(gql *gqlCommitNode) *gh.RepositoryCommit {
	c := &gh.RepositoryCommit{
		SHA: new(gql.Commit.OID),
		Commit: &gh.Commit{
			Message: new(gql.Commit.Message),
			Author: &gh.CommitAuthor{
				Name: new(gql.Commit.Author.Name),
				Date: &gh.Timestamp{Time: gql.Commit.Author.Date},
			},
		},
	}
	if gql.Commit.Author.User != nil {
		c.Author = &gh.User{Login: new(gql.Commit.Author.User.Login)}
	}
	return c
}

func splitCheckContexts(contexts []gqlCheckContext) ([]*gh.CheckRun, []*gh.RepoStatus) {
	var checks []*gh.CheckRun
	var statuses []*gh.RepoStatus
	for i := range contexts {
		c := &contexts[i]
		switch c.Typename {
		case "CheckRun":
			checks = append(checks, adaptCheckRun(&c.CheckRun))
		case "StatusContext":
			statuses = append(statuses, adaptStatusContext(&c.StatusContext))
		}
	}
	return checks, statuses
}

func adaptCheckRun(gql *gqlCheckRunFields) *gh.CheckRun {
	return &gh.CheckRun{
		Name:       new(gql.Name),
		Status:     new(toLower(gql.Status)),
		Conclusion: new(toLower(gql.Conclusion)),
		DetailsURL: new(gql.DetailsURL),
		App:        &gh.App{Name: new(gql.CheckSuite.App.Name)},
	}
}

func adaptStatusContext(gql *gqlStatusContextFields) *gh.RepoStatus {
	return &gh.RepoStatus{
		Context:   new(gql.Context),
		State:     new(toLower(gql.State)),
		TargetURL: new(gql.TargetURL),
	}
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// --- Bulk result types ---

// RepoBulkResult holds all open PRs fetched via GraphQL for a repo.
type RepoBulkResult struct {
	PullRequests []BulkPR
}

// BulkPR holds a PR and its nested data from a single GraphQL query.
// The *Complete flags indicate whether each nested connection was
// fully paginated. When false, the data is partial and the detail
// drain should fill in via REST.
type BulkPR struct {
	PR               *gh.PullRequest
	Comments         []*gh.IssueComment
	Reviews          []*gh.PullRequestReview
	Commits          []*gh.RepositoryCommit
	CheckRuns        []*gh.CheckRun
	Statuses         []*gh.RepoStatus
	CommentsComplete bool
	ReviewsComplete  bool
	CommitsComplete  bool
	CIComplete       bool
}

// --- GraphQL rate transport ---

type graphqlRateTransport struct {
	base        http.RoundTripper
	rateTracker *RateTracker
}

func (t *graphqlRateTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if t.rateTracker != nil {
		t.rateTracker.RecordRequest()
		if rate := parseRateLimitHeaders(resp); rate.Limit > 0 {
			t.rateTracker.UpdateFromRate(rate)
		}
	}
	return resp, err
}

func parseRateLimitHeaders(resp *http.Response) gh.Rate {
	var rate gh.Rate
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		rate.Remaining, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("X-RateLimit-Limit"); v != "" {
		rate.Limit, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		epoch, _ := strconv.ParseInt(v, 10, 64)
		rate.Reset = gh.Timestamp{Time: time.Unix(epoch, 0)}
	}
	return rate
}

// --- GraphQLFetcher ---

// GraphQLFetcher fetches PR data via GitHub's GraphQL API (v4).
type GraphQLFetcher struct {
	client      *githubv4.Client
	rateTracker *RateTracker
}

// NewGraphQLFetcher creates a fetcher for the given host. budget may be nil.
func NewGraphQLFetcher(
	token string,
	platformHost string,
	rateTracker *RateTracker,
	budget *SyncBudget,
) *GraphQLFetcher {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(context.Background(), ts)

	base := tc.Transport
	if rateTracker != nil {
		base = &graphqlRateTransport{
			base:        base,
			rateTracker: rateTracker,
		}
	}
	if budget != nil {
		tc.Transport = &budgetTransport{
			base:   base,
			budget: budget,
		}
	} else {
		tc.Transport = base
	}

	var gqlClient *githubv4.Client
	if platformHost == "" || platformHost == "github.com" {
		gqlClient = githubv4.NewClient(tc)
	} else {
		endpoint := graphQLEndpointForHost(platformHost)
		gqlClient = githubv4.NewEnterpriseClient(endpoint, tc)
	}

	return &GraphQLFetcher{
		client:      gqlClient,
		rateTracker: rateTracker,
	}
}

func (g *GraphQLFetcher) ShouldBackoff() (bool, time.Duration) {
	if g.rateTracker == nil {
		return false, 0
	}
	return g.rateTracker.ShouldBackoff()
}

func (g *GraphQLFetcher) FetchRepoPRs(
	ctx context.Context, owner, name string,
) (*RepoBulkResult, error) {
	result, err := g.fetchRepoPRsWithPageSize(
		ctx, owner, name, topLevelPageSize,
	)
	if err != nil {
		slog.Warn("GraphQL query failed, retrying with smaller page",
			"owner", owner, "name", name,
			"err", err, "retryPageSize", retryPageSize,
		)
		result, err = g.fetchRepoPRsWithPageSize(
			ctx, owner, name, retryPageSize,
		)
	}
	return result, err
}

func (g *GraphQLFetcher) fetchRepoPRsWithPageSize(
	ctx context.Context, owner, name string, pageSize int,
) (*RepoBulkResult, error) {
	gqlPRs, err := fetchAllPages(ctx, func(
		ctx context.Context, cursor *string,
	) ([]gqlPR, pageInfo, error) {
		var q gqlPRQuery
		vars := map[string]any{
			"owner":    githubv4.String(owner),
			"name":     githubv4.String(name),
			"pageSize": githubv4.Int(pageSize),
			"cursor":   cursorVar(cursor),
		}
		if err := g.client.Query(ctx, &q, vars); err != nil {
			return nil, pageInfo{}, err
		}
		return q.Repository.PullRequests.Nodes,
			q.Repository.PullRequests.PageInfo, nil
	})
	if err != nil {
		return nil, err
	}

	result := &RepoBulkResult{
		PullRequests: make([]BulkPR, 0, len(gqlPRs)),
	}
	for i := range gqlPRs {
		bulk := convertGQLPR(&gqlPRs[i])
		result.PullRequests = append(result.PullRequests, bulk)
	}
	return result, nil
}

func cursorVar(cursor *string) *githubv4.String {
	if cursor == nil {
		return nil
	}
	s := githubv4.String(*cursor)
	return &s
}

func convertGQLPR(gql *gqlPR) BulkPR {
	bulk := BulkPR{
		PR:               adaptPR(gql),
		CommentsComplete: !gql.Comments.PageInfo.HasNextPage,
		ReviewsComplete:  !gql.Reviews.PageInfo.HasNextPage,
		CommitsComplete:  !gql.AllCommits.PageInfo.HasNextPage,
	}

	for i := range gql.Comments.Nodes {
		bulk.Comments = append(bulk.Comments, adaptComment(&gql.Comments.Nodes[i]))
	}
	for i := range gql.Reviews.Nodes {
		bulk.Reviews = append(bulk.Reviews, adaptReview(&gql.Reviews.Nodes[i]))
	}
	for i := range gql.AllCommits.Nodes {
		bulk.Commits = append(bulk.Commits, adaptCommit(&gql.AllCommits.Nodes[i]))
	}

	bulk.CIComplete = true
	if len(gql.LastCommit.Nodes) > 0 {
		rollup := gql.LastCommit.Nodes[0].Commit.StatusCheckRollup
		if rollup != nil {
			bulk.CIComplete = !rollup.Contexts.PageInfo.HasNextPage
			bulk.CheckRuns, bulk.Statuses = splitCheckContexts(
				rollup.Contexts.Nodes,
			)
		}
	}

	return bulk
}
