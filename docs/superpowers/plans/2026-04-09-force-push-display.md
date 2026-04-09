# Force-Push Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show GitHub-native pull-request force-push events, including the actor, in the PR timeline and activity feeds without replacing the existing commit event model.

**Architecture:** Extend the GitHub client with a GraphQL-only `HeadRefForcePushedEvent` fetcher, normalize those events into existing `db.MREvent` rows, and let the current server/detail/activity APIs surface them unchanged. On the frontend, treat `force_push` as another activity event type, add one shared commit-collapse helper so feed views stop grouping across rewrite boundaries, and render the PR timeline marker with actor plus SHA transition.

**Tech Stack:** Go, go-github v84, GitHub GraphQL over authenticated HTTP, SQLite, Svelte 5, Vitest, Testing Library, Bun

---

## File Structure

**Modify:** `internal/github/client.go`
Purpose: add a `ForcePushEvent` transport type, a `ListForcePushEvents` client method, and the GraphQL request helper that fetches `HeadRefForcePushedEvent` timeline items.

**Modify:** `internal/github/client_test.go`
Purpose: test GraphQL endpoint selection and paginated GraphQL decoding for force-push events.

**Modify:** `internal/github/normalize.go`
Purpose: normalize a `ForcePushEvent` into a `db.MREvent` with actor, SHA-pair summary, metadata JSON, and SHA-pair dedupe key.

**Modify:** `internal/github/normalize_test.go`
Purpose: verify `NormalizeForcePushEvent` output.

**Modify:** `internal/github/sync.go`
Purpose: fetch force-push events during PR timeline refresh and append them to the existing MR event upsert batch.

**Modify:** `internal/github/sync_test.go`
Purpose: expand the sync mock client with `ListForcePushEvents` and verify a synced PR stores a `force_push` MR event.

**Modify:** `internal/server/api_test.go`
Purpose: keep the server mock implementation compiling after the GitHub client interface grows.

**Modify:** `internal/testutil/fixture_client.go`
Purpose: keep the E2E fixture client compiling with the new client interface method.

**Modify:** `internal/db/queries_activity.go`
Purpose: include `force_push` MR events in the unified activity feed query.

**Modify:** `internal/db/queries_activity_test.go`
Purpose: verify `force_push` events appear in activity results and preserve ordering.

**Create:** `packages/ui/src/components/activityRows.ts`
Purpose: hold the shared commit-collapse logic used by both flat and threaded activity views so `force_push` naturally breaks commit runs.

**Create:** `packages/ui/src/components/activityRows.test.ts`
Purpose: verify collapsing still works for plain commit runs and stops at `force_push` boundaries.

**Modify:** `packages/ui/src/stores/activity.svelte.ts`
Purpose: add `force_push` to default enabled activity filters and URL-derived filter reconstruction.

**Modify:** `packages/ui/src/components/ActivityFeed.svelte`
Purpose: add `force_push` labels/colors/filtering and switch to the shared collapse helper.

**Modify:** `packages/ui/src/components/ActivityThreaded.svelte`
Purpose: add `force_push` labels/colors and switch to the shared collapse helper.

**Modify:** `packages/ui/src/components/detail/EventTimeline.svelte`
Purpose: render `force_push` cards with the actor and compact SHA transition summary.

**Create:** `packages/ui/src/components/detail/EventTimeline.test.ts`
Purpose: verify the PR detail timeline renders the force-push label, actor, and SHA transition.

**Modify:** `docs/superpowers/specs/2026-04-09-force-push-display-design.md`
Purpose: no code changes expected here during implementation.

**No API generation expected:** `event_type` is already an open string in the existing response shapes, so `make api-generate` should not be necessary for this feature.

### Task 1: GitHub Force-Push Client And Normalizer

**Files:**
- Modify: `internal/github/client.go`
- Modify: `internal/github/client_test.go`
- Modify: `internal/github/normalize.go`
- Modify: `internal/github/normalize_test.go`

- [ ] **Step 1: Write the failing tests**

Add a paginated GraphQL client test in `internal/github/client_test.go` and a normalizer test in `internal/github/normalize_test.go`.

```go
package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGraphQLEndpointForHost(t *testing.T) {
	require.Equal(t, "https://api.github.com/graphql", graphQLEndpointForHost(""))
	require.Equal(t, "https://api.github.com/graphql", graphQLEndpointForHost("github.com"))
	require.Equal(t, "https://github.example.com/api/graphql", graphQLEndpointForHost("github.example.com"))
}

func TestListForcePushEvents(t *testing.T) {
	require := require.New(t)
	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(http.MethodPost, r.Method)
		require.Equal("application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[{"actor":{"login":"alice"},"beforeCommit":{"oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"afterCommit":{"oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"createdAt":"2024-06-01T12:00:00Z","ref":{"name":"feature"}}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[{"actor":{"login":"alice"},"beforeCommit":{"oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"afterCommit":{"oid":"cccccccccccccccccccccccccccccccccccccccc"},"createdAt":"2024-06-01T12:05:00Z","ref":{"name":"feature"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &liveClient{
		httpClient:      srv.Client(),
		graphQLEndpoint: srv.URL + "/graphql",
	}

	events, err := c.ListForcePushEvents(context.Background(), "owner", "repo", 42)
	require.NoError(err)
	require.Len(events, 2)
	require.Equal("alice", events[0].Actor)
	require.Equal("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", events[0].BeforeSHA)
	require.Equal("cccccccccccccccccccccccccccccccccccccccc", events[1].AfterSHA)
	require.Equal("feature", events[0].Ref)
	require.Equal(2, calls)
}

func TestNormalizeForcePushEvent(t *testing.T) {
	require := require.New(t)
	createdAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	event := NormalizeForcePushEvent(17, ForcePushEvent{
		Actor:     "alice",
		BeforeSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AfterSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Ref:       "feature",
		CreatedAt: createdAt,
	})

	require.Equal(int64(17), event.MergeRequestID)
	require.Equal("force_push", event.EventType)
	require.Equal("alice", event.Author)
	require.Equal("aaaaaaa -> bbbbbbb", event.Summary)
	require.Equal("force-push-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", event.DedupeKey)
	require.Equal(createdAt, event.CreatedAt)
	require.Contains(event.MetadataJSON, `"before_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)
	require.Contains(event.MetadataJSON, `"after_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`)
	require.Contains(event.MetadataJSON, `"ref":"feature"`)
}
```

- [ ] **Step 2: Run the targeted backend tests to verify they fail**

Run: `go test ./internal/github -run 'TestGraphQLEndpointForHost|TestListForcePushEvents|TestNormalizeForcePushEvent'`
Expected: FAIL with undefined `graphQLEndpointForHost`, `ListForcePushEvents`, `ForcePushEvent`, or `NormalizeForcePushEvent` symbols.

- [ ] **Step 3: Implement the GraphQL client and normalizer**

Update `internal/github/client.go` and `internal/github/normalize.go` with a small GraphQL transport type, a client method, and a force-push normalizer.

```go
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
)

type ForcePushEvent struct {
	Actor     string
	BeforeSHA string
	AfterSHA  string
	Ref       string
	CreatedAt time.Time
}

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
}

type liveClient struct {
	gh               *gh.Client
	httpClient       *http.Client
	rateTracker      *RateTracker
	graphQLEndpoint  string
}

func graphQLEndpointForHost(platformHost string) string {
	if platformHost == "" || platformHost == "github.com" {
		return "https://api.github.com/graphql"
	}
	return "https://" + platformHost + "/api/graphql"
}

func NewClient(token string, platformHost string, rateTracker *RateTracker) (Client, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	// existing gh client creation stays the same
	return &liveClient{
		gh:              ghClient,
		httpClient:      tc,
		rateTracker:     rateTracker,
		graphQLEndpoint: graphQLEndpointForHost(platformHost),
	}, nil
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

func (c *liveClient) ListForcePushEvents(ctx context.Context, owner, repo string, number int) ([]ForcePushEvent, error) {
	type request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	type response struct {
		Data struct {
			Repository struct {
				PullRequest struct {
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
							Ref *struct {
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
		payload, err := json.Marshal(request{
			Query: forcePushTimelineQuery,
			Variables: map[string]any{
				"owner": owner,
				"repo": repo,
				"number": number,
				"cursor": cursor,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("marshal force-push query: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphQLEndpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create force-push request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list force-push events for %s/%s#%d: %w", owner, repo, number, err)
		}
		c.trackRateHeaders(resp)
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("list force-push events for %s/%s#%d: graphql status %s", owner, repo, number, resp.Status)
		}

		var decoded response
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("decode force-push events for %s/%s#%d: %w", owner, repo, number, err)
		}
		_ = resp.Body.Close()

		for _, node := range decoded.Data.Repository.PullRequest.TimelineItems.Nodes {
			actor := ""
			if node.Actor != nil {
				actor = node.Actor.Login
			}
			before := ""
			if node.BeforeCommit != nil {
				before = node.BeforeCommit.OID
			}
			after := ""
			if node.AfterCommit != nil {
				after = node.AfterCommit.OID
			}
			ref := ""
			if node.Ref != nil {
				ref = node.Ref.Name
			}
			events = append(events, ForcePushEvent{
				Actor:     actor,
				BeforeSHA: before,
				AfterSHA:  after,
				Ref:       ref,
				CreatedAt: node.CreatedAt,
			})
		}

		pageInfo := decoded.Data.Repository.PullRequest.TimelineItems.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		cursor = pageInfo.EndCursor
	}
	return events, nil
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

type forcePushMetadata struct {
	BeforeSHA string `json:"before_sha"`
	AfterSHA  string `json:"after_sha"`
	Ref       string `json:"ref"`
}

func NormalizeForcePushEvent(mrID int64, fp ForcePushEvent) db.MREvent {
	meta, _ := json.Marshal(forcePushMetadata{
		BeforeSHA: fp.BeforeSHA,
		AfterSHA:  fp.AfterSHA,
		Ref:       fp.Ref,
	})
	return db.MREvent{
		MergeRequestID: mrID,
		EventType:      "force_push",
		Author:         fp.Actor,
		Summary:        shortSHA(fp.BeforeSHA) + " -> " + shortSHA(fp.AfterSHA),
		MetadataJSON:   string(meta),
		CreatedAt:      fp.CreatedAt,
		DedupeKey:      fmt.Sprintf("force-push-%s-%s", fp.BeforeSHA, fp.AfterSHA),
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
```

- [ ] **Step 4: Run the backend tests again to verify they pass**

Run: `go test ./internal/github -run 'TestGraphQLEndpointForHost|TestListForcePushEvents|TestNormalizeForcePushEvent'`
Expected: PASS.

- [ ] **Step 5: Commit the backend client groundwork**

```bash
git add internal/github/client.go internal/github/client_test.go internal/github/normalize.go internal/github/normalize_test.go
git commit -m "feat(github): fetch PR force-push timeline events"
```

### Task 2: Persist Force-Push Events And Include Them In Activity Queries

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/github/sync_test.go`
- Modify: `internal/server/api_test.go`
- Modify: `internal/testutil/fixture_client.go`
- Modify: `internal/db/queries_activity.go`
- Modify: `internal/db/queries_activity_test.go`

- [ ] **Step 1: Write the failing sync and activity query tests**

Add one sync test and one activity query test.

```go
func TestSyncStoresForcePushEvent(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	commitSHA := "abc123def456"
	commitMsg := "fix: tighten validation"
	ciState := "success"

	mc := &mockClient{
		openPRs: []*gh.PullRequest{buildOpenPR(1, now)},
		commits: []*gh.RepositoryCommit{{
			SHA: &commitSHA,
			Commit: &gh.Commit{
				Message: &commitMsg,
				Author: &gh.CommitAuthor{Name: new("dev"), Date: makeTimestamp(now.Add(-1 * time.Hour))},
			},
		}},
		forcePushEvents: []ForcePushEvent{{
			Actor:     "alice",
			BeforeSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AfterSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Ref:       "feature",
			CreatedAt: now.Add(-30 * time.Minute),
		}},
		reviews:  []*gh.PullRequestReview{},
		comments: []*gh.IssueComment{},
		ciStatus: &gh.CombinedStatus{State: &ciState},
	}

	syncer := NewSyncer(map[string]Client{"github.com": mc}, d, nil, []RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}}, time.Minute, nil)
	syncer.RunOnce(ctx)

	pr, err := d.GetMergeRequest(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(pr)

	events, err := d.ListMREvents(ctx, pr.ID)
	require.NoError(err)
	require.NotEmpty(events)

	var forcePush *db.MREvent
	for i := range events {
		if events[i].EventType == "force_push" {
			forcePush = &events[i]
			break
		}
	}
	require.NotNil(forcePush)
	assert.Equal("alice", forcePush.Author)
	assert.Equal("aaaaaaa -> bbbbbbb", forcePush.Summary)
	assert.Contains(forcePush.MetadataJSON, `"ref":"feature"`)
}
```

```go
t.Run("force push events appear in the activity feed", func(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()
	repoID := insertTestRepo(t, d, "alice", "alpha")
	prID := insertTestMR(t, d, repoID, 1, "Rewrite branch", base)

	err := d.UpsertMREvents(ctx, []MREvent{
		{
			MergeRequestID: prID,
			EventType:      "force_push",
			Author:         "alice",
			Summary:        "abc1234 -> def5678",
			CreatedAt:      base.Add(5 * time.Minute),
			DedupeKey:      "force-push-abc1234-def5678",
		},
	})
	require.NoError(t, err)

	items, err := d.ListActivity(ctx, ListActivityOpts{Limit: 50})
	require.NoError(t, err)
	require.NotEmpty(t, items)
	assert.Equal("force_push", items[0].ActivityType)
	assert.Equal("alice", items[0].Author)
	assert.Equal("Rewrite branch", items[0].ItemTitle)
})
```

- [ ] **Step 2: Run the focused Go tests to verify they fail**

Run: `go test ./internal/github ./internal/db -run 'TestSyncStoresForcePushEvent|TestListActivity'`
Expected: FAIL because the syncer does not fetch force-push events yet and the activity query does not include `force_push` rows.

- [ ] **Step 3: Wire force-push events through sync, query, and test doubles**

Update the sync pipeline, the activity query, and all client test doubles that need the new interface method.

```go
package github

type mockClient struct {
	openPRs         []*gh.PullRequest
	singlePR        *gh.PullRequest
	comments        []*gh.IssueComment
	reviews         []*gh.PullRequestReview
	commits         []*gh.RepositoryCommit
	forcePushEvents []ForcePushEvent
	ciStatus        *gh.CombinedStatus
	checkRuns       []*gh.CheckRun
	workflowRuns    []*gh.WorkflowRun
}

func (m *mockClient) ListForcePushEvents(_ context.Context, _, _ string, _ int) ([]ForcePushEvent, error) {
	return m.forcePushEvents, nil
}
```

```go
func (m *mockGH) ListForcePushEvents(_ context.Context, _, _ string, _ int) ([]ghclient.ForcePushEvent, error) {
	return nil, nil
}

func (c *FixtureClient) ListForcePushEvents(_ context.Context, _, _ string, _ int) ([]ghclient.ForcePushEvent, error) {
	return nil, nil
}
```

```go
func (s *Syncer) refreshTimeline(ctx context.Context, repo RepoRef, repoID int64, mrID int64, ghPR *gh.PullRequest) error {
	number := ghPR.GetNumber()
	client := s.clientFor(repo)

	comments, err := client.ListIssueComments(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list comments for MR #%d: %w", number, err)
	}

	reviews, err := client.ListReviews(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list reviews for MR #%d: %w", number, err)
	}

	commits, err := client.ListCommits(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list commits for MR #%d: %w", number, err)
	}

	forcePushes, err := client.ListForcePushEvents(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list force-push events for MR #%d: %w", number, err)
	}

	var events []db.MREvent
	for _, c := range comments {
		events = append(events, NormalizeCommentEvent(mrID, c))
	}
	for _, r := range reviews {
		events = append(events, NormalizeReviewEvent(mrID, r))
	}
	for _, c := range commits {
		events = append(events, NormalizeCommitEvent(mrID, c))
	}
	for _, fp := range forcePushes {
		events = append(events, NormalizeForcePushEvent(mrID, fp))
	}

	if err := s.db.UpsertMREvents(ctx, events); err != nil {
		return fmt.Errorf("upsert events for MR #%d: %w", number, err)
	}

	reviewDecision := DeriveReviewDecision(reviews)
	lastActivityAt := computeLastActivity(ghPR, comments, reviews, commits)
	return s.db.UpdateMRDerivedFields(ctx, repoID, number, db.MRDerivedFields{
		ReviewDecision: reviewDecision,
		CommentCount:   len(comments),
		LastActivityAt: lastActivityAt,
	})
}
```

```go
query := fmt.Sprintf(`
	SELECT activity_type, source, source_id,
	       repo_owner, repo_name,
	       item_type, item_number, item_title,
	       item_url, item_state, author,
	       created_at, body_preview
	FROM (
		SELECT CASE e.event_type
		           WHEN 'issue_comment' THEN 'comment'
		           ELSE e.event_type
		       END,
		       'pre', e.id,
		       r.owner, r.name,
		       'pr', p.number, p.title,
		       p.url, p.state,
		       e.author, e.created_at,
		       substr(COALESCE(e.body, ''), 1, 200)
		FROM middleman_mr_events e
		JOIN middleman_merge_requests p ON e.merge_request_id = p.id
		JOIN middleman_repos r ON p.repo_id = r.id
		WHERE e.event_type IN (
			'issue_comment', 'review', 'commit', 'force_push')
	) unified
	%s
	ORDER BY created_at DESC, source DESC, source_id DESC
	LIMIT ?`, where)
```

- [ ] **Step 4: Run the Go tests again to verify they pass**

Run: `go test ./internal/github ./internal/db -run 'TestSyncStoresForcePushEvent|TestListActivity'`
Expected: PASS.

- [ ] **Step 5: Commit the sync and activity feed backend changes**

```bash
git add internal/github/sync.go internal/github/sync_test.go internal/server/api_test.go internal/testutil/fixture_client.go internal/db/queries_activity.go internal/db/queries_activity_test.go
git commit -m "feat(activity): persist force-push events"
```

### Task 3: Feed Filters And Commit-Collapse Boundaries

**Files:**
- Create: `packages/ui/src/components/activityRows.ts`
- Create: `packages/ui/src/components/activityRows.test.ts`
- Modify: `packages/ui/src/stores/activity.svelte.ts`
- Modify: `packages/ui/src/components/ActivityFeed.svelte`
- Modify: `packages/ui/src/components/ActivityThreaded.svelte`

- [ ] **Step 1: Write the failing UI helper tests**

Create `packages/ui/src/components/activityRows.test.ts`.

```ts
import { describe, expect, it } from "vitest";
import type { ActivityItem } from "../api/types.js";
import {
  collapseActivityCommitRuns,
  isCollapsedActivityRow,
} from "./activityRows.js";

function item(id: string, activity_type: string, author: string): ActivityItem {
  return {
    id,
    cursor: id,
    activity_type,
    repo_owner: "acme",
    repo_name: "widgets",
    item_type: "pr",
    item_number: 7,
    item_title: "Rewrite branch",
    item_url: "https://github.com/acme/widgets/pull/7",
    item_state: "open",
    author,
    created_at: new Date(Number(id) * 1000).toISOString(),
    body_preview: "",
  };
}

describe("collapseActivityCommitRuns", () => {
  it("collapses three consecutive commits from the same author", () => {
    const rows = collapseActivityCommitRuns([
      item("7", "commit", "alice"),
      item("6", "commit", "alice"),
      item("5", "commit", "alice"),
    ]);

    expect(rows).toHaveLength(1);
    expect(isCollapsedActivityRow(rows[0]!)).toBe(true);
  });

  it("does not collapse across a force-push boundary", () => {
    const rows = collapseActivityCommitRuns([
      item("9", "commit", "alice"),
      item("8", "commit", "alice"),
      item("7", "commit", "alice"),
      item("6", "force_push", "alice"),
      item("5", "commit", "alice"),
      item("4", "commit", "alice"),
      item("3", "commit", "alice"),
    ]);

    expect(rows).toHaveLength(3);
    expect(isCollapsedActivityRow(rows[0]!)).toBe(true);
    expect(!isCollapsedActivityRow(rows[1]!) && rows[1]!.activity_type).toBe("force_push");
    expect(isCollapsedActivityRow(rows[2]!)).toBe(true);
  });
});
```

- [ ] **Step 2: Run the focused frontend test to verify it fails**

Run: `bun --cwd frontend run test -- ../packages/ui/src/components/activityRows.test.ts`
Expected: FAIL because `activityRows.ts` does not exist yet.

- [ ] **Step 3: Implement the helper and feed/filter updates**

Create the helper and switch both activity views plus the activity store over to `force_push`-aware defaults.

```ts
import type { ActivityItem } from "../api/types.js";

export interface CollapsedActivityCommits {
  kind: "collapsed";
  id: string;
  author: string;
  count: number;
  earliest: string;
  latest: string;
  representative: ActivityItem;
}

export type ActivityRow = ActivityItem | CollapsedActivityCommits;

export function isCollapsedActivityRow(row: ActivityRow): row is CollapsedActivityCommits {
  return "kind" in row && row.kind === "collapsed";
}

export function collapseActivityCommitRuns(items: ActivityItem[]): ActivityRow[] {
  const result: ActivityRow[] = [];
  let i = 0;
  while (i < items.length) {
    const item = items[i]!;
    if (item.activity_type !== "commit") {
      result.push(item);
      i++;
      continue;
    }
    let j = i + 1;
    while (j < items.length) {
      const next = items[j]!;
      if (
        next.activity_type !== "commit"
        || next.author !== item.author
        || next.repo_owner !== item.repo_owner
        || next.repo_name !== item.repo_name
        || next.item_number !== item.item_number
      ) {
        break;
      }
      j++;
    }
    const count = j - i;
    if (count < 3) {
      for (let k = i; k < j; k++) result.push(items[k]!);
    } else {
      const latest = items[i]!;
      const earliest = items[j - 1]!;
      result.push({
        kind: "collapsed",
        id: `collapsed-${latest.id}-${count}`,
        author: item.author,
        count,
        earliest: earliest.created_at,
        latest: latest.created_at,
        representative: latest,
      });
    }
    i = j;
  }
  return result;
}
```

```ts
const DEFAULT_EVENT_TYPES = ["comment", "review", "commit", "force_push"] as const;

let enabledEvents = $state<Set<string>>(new Set(DEFAULT_EVENT_TYPES));

function deriveFiltersFromTypes(): void {
  if (filterTypes.length === 0) {
    itemFilter = "all";
    enabledEvents = new Set(DEFAULT_EVENT_TYPES);
    return;
  }
  const hasPR = filterTypes.includes("new_pr");
  const hasIssue = filterTypes.includes("new_issue");
  if (hasPR && !hasIssue) itemFilter = "prs";
  else if (hasIssue && !hasPR) itemFilter = "issues";
  else itemFilter = "all";
  enabledEvents = new Set(DEFAULT_EVENT_TYPES.filter((t) => filterTypes.includes(t)));
}
```

```ts
const EVENT_TYPES = ["comment", "review", "commit", "force_push"] as const;

const EVENT_LABELS: Record<string, string> = {
  comment: "Comments",
  review: "Reviews",
  commit: "Commits",
  force_push: "Force pushes",
};

const EVENT_COLORS: Record<string, string> = {
  comment: "var(--accent-amber)",
  review: "var(--accent-green)",
  commit: "var(--accent-teal)",
  force_push: "var(--accent-red)",
};

function eventLabel(item: ActivityItem): string {
  switch (item.activity_type) {
    case "new_pr": return "Opened";
    case "new_issue": return "Opened";
    case "comment": return "Comment";
    case "review": return "Review";
    case "commit": return "Commit";
    case "force_push": return "Force-pushed";
    default: return item.activity_type;
  }
}

function eventClass(type: string): string {
  switch (type) {
    case "comment": return "evt-comment";
    case "review": return "evt-review";
    case "commit": return "evt-commit";
    case "force_push": return "evt-force-push";
    default: return "";
  }
}
```

```ts
import {
  collapseActivityCommitRuns,
  isCollapsedActivityRow,
} from "./activityRows.js";

const flatRows = $derived(collapseActivityCommitRuns(displayItems));
```

```ts
import {
  collapseActivityCommitRuns,
  isCollapsedActivityRow,
} from "./activityRows.js";

displayEvents: collapseActivityCommitRuns(events),
```

- [ ] **Step 4: Run the focused frontend test again to verify it passes**

Run: `bun --cwd frontend run test -- ../packages/ui/src/components/activityRows.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit the feed/filter UI changes**

```bash
git add packages/ui/src/components/activityRows.ts packages/ui/src/components/activityRows.test.ts packages/ui/src/stores/activity.svelte.ts packages/ui/src/components/ActivityFeed.svelte packages/ui/src/components/ActivityThreaded.svelte
git commit -m "feat(ui): show force-push events in activity feeds"
```

### Task 4: Pull Request Timeline Rendering

**Files:**
- Modify: `packages/ui/src/components/detail/EventTimeline.svelte`
- Create: `packages/ui/src/components/detail/EventTimeline.test.ts`

- [ ] **Step 1: Write the failing timeline component test**

Create `packages/ui/src/components/detail/EventTimeline.test.ts`.

```ts
import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../../utils/clipboard.js", () => ({
  copyToClipboard: () => Promise.resolve(true),
}));

import EventTimeline from "./EventTimeline.svelte";

describe("EventTimeline", () => {
  afterEach(() => cleanup());

  it("renders force-push events with actor and sha transition", () => {
    render(EventTimeline, {
      props: {
        repoOwner: "acme",
        repoName: "widgets",
        events: [{
          ID: 42,
          EventType: "force_push",
          Author: "alice",
          Summary: "aaaaaaa -> bbbbbbb",
          Body: "",
          CreatedAt: "2024-06-01T12:00:00Z",
        }],
      },
    });

    expect(screen.getByText("Force-pushed")).toBeTruthy();
    expect(screen.getByText("alice")).toBeTruthy();
    expect(screen.getByText("aaaaaaa -> bbbbbbb")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run the focused component test to verify it fails**

Run: `bun --cwd frontend run test -- ../packages/ui/src/components/detail/EventTimeline.test.ts`
Expected: FAIL because `force_push` is not labeled or rendered yet.

- [ ] **Step 3: Implement the timeline marker rendering**

Update `packages/ui/src/components/detail/EventTimeline.svelte`.

```svelte
<script lang="ts">
  const typeLabels: Record<string, string> = {
    issue_comment: "Comment",
    review: "Review",
    commit: "Commit",
    force_push: "Force-pushed",
    review_comment: "Review Comment",
  };

  const typeColors: Record<string, string> = {
    issue_comment: "var(--accent-blue)",
    review: "var(--accent-purple)",
    review_comment: "var(--accent-purple)",
    commit: "var(--accent-green)",
    force_push: "var(--accent-red)",
  };
</script>

{#if event.Summary && (event.EventType === "commit" || event.EventType === "force_push")}
  <p class="event-summary">{event.Summary}</p>
{/if}
```

- [ ] **Step 4: Run the focused component test again to verify it passes**

Run: `bun --cwd frontend run test -- ../packages/ui/src/components/detail/EventTimeline.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit the PR timeline UI changes**

```bash
git add packages/ui/src/components/detail/EventTimeline.svelte packages/ui/src/components/detail/EventTimeline.test.ts
git commit -m "feat(ui): show force-push markers in PR timelines"
```

### Task 5: Full Verification

**Files:**
- Modify: no source changes planned

- [ ] **Step 1: Run the targeted Go packages**

Run: `go test ./internal/github ./internal/db`
Expected: PASS.

- [ ] **Step 2: Run the targeted frontend tests**

Run: `bun --cwd frontend run test -- ../packages/ui/src/components/activityRows.test.ts ../packages/ui/src/components/detail/EventTimeline.test.ts`
Expected: PASS.

- [ ] **Step 3: Run frontend type checking**

Run: `bun --cwd frontend run typecheck`
Expected: PASS.

- [ ] **Step 4: Run the repository short test suite**

Run: `make test-short`
Expected: PASS.

- [ ] **Step 5: Confirm the worktree is clean except for intentional changes**

Run: `git status --short`
Expected: no unexpected modified files.

## Self-Review Checklist

- Spec coverage:
  - GitHub-native force-push source event with actor: covered by Task 1.
  - Persistence in existing MR event pipeline: covered by Task 2.
  - Activity feed inclusion: covered by Task 2 and Task 3.
  - Minimal PR timeline rendering: covered by Task 4.
  - Commit-collapse boundary behavior: covered by Task 3.
- Placeholder scan:
  - No placeholder markers or “similar to Task N” references remain.
- Type consistency:
  - The same `force_push` event type is used in backend persistence, activity queries, filters, feed rendering, and detail rendering.
  - The same `ForcePushEvent` transport type is used from client fetch through normalization.

## Notes

- Keep the change additive; do not delete historical `commit` events.
- Reuse existing `db.MREvent` rows instead of introducing a new table or a grouped push model.
- Leave API generation alone unless a concrete schema diff appears during implementation.
