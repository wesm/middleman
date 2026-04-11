# GraphQL Issues Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fetch open issues via GitHub's GraphQL API, matching the existing PR GraphQL path, with REST fallback.

**Architecture:** Add `gqlIssueQuery`/`gqlIssue` types and `adaptIssue()` adapter in `graphql.go`, extend `RepoBulkResult` with `Issues []BulkIssue`, add `FetchRepoIssues()` to `GraphQLFetcher`, then wire the GraphQL path into `indexSyncRepo` with ETag gating and REST fallback. TDD throughout.

**Tech Stack:** Go, `shurcooL/githubv4`, `google/go-github/v84`, SQLite, testify

**Spec:** `docs/superpowers/specs/2026-04-11-graphql-issues-design.md`

**Dev tooling:** Go and other tools are not on PATH. Wrap all commands in `nix shell nixpkgs#go --command ...`. Use `CGO_ENABLED=0` for builds and tests (pure Go SQLite driver). Example: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestName -v`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/github/graphql.go` | Modify | Add `gqlIssueQuery`, `gqlIssue`, `adaptIssue()`, `convertGQLIssue()`, `BulkIssue`, extend `RepoBulkResult`, add `FetchRepoIssues()` |
| `internal/github/graphql_test.go` | Modify | Add `TestAdaptIssue`, `TestConvertGQLIssue`, `TestAdaptIssueNilFields` |
| `internal/github/sync.go` | Modify | Refactor `indexSyncIssues` caller to add ETag-gated GraphQL path, add `doSyncRepoGraphQLIssues()` |
| `internal/github/sync_test.go` | Modify | Add `mockClient` tracking, `buildOpenIssue` helper, GraphQL issue sync integration tests |
| `internal/server/api_test.go` | Modify | Add `TestAPISyncIssuesViaGraphQL` e2e test |

---

### Task 1: Types and Adapter — `adaptIssue`

**Files:**
- Modify: `internal/github/graphql_test.go`
- Modify: `internal/github/graphql.go`

- [ ] **Step 1: Write failing test for `adaptIssue`**

Add to `internal/github/graphql_test.go`:

```go
func TestAdaptIssue(t *testing.T) {
	assert := Assert.New(t)

	now := time.Now().UTC().Truncate(time.Second)
	closed := now.Add(-time.Hour)

	gql := gqlIssue{
		DatabaseId: 99999,
		Number:     10,
		Title:      "Bug report",
		State:      "OPEN",
		Body:       "Something broke",
		URL:        "https://github.com/o/r/issues/10",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	gql.Author.Login = "alice"
	gql.Labels.Nodes = []gqlLabel{
		{Name: "bug", Color: "d73a4a", Description: "Something broken", IsDefault: false},
	}
	gql.Comments.Nodes = []gqlComment{
		{DatabaseId: 501, Body: "I see this too", CreatedAt: now, UpdatedAt: now},
	}
	gql.Comments.Nodes[0].Author.Login = "bob"

	issue := adaptIssue(&gql)

	assert.Equal(int64(99999), issue.GetID())
	assert.Equal(10, issue.GetNumber())
	assert.Equal("Bug report", issue.GetTitle())
	assert.Equal("open", issue.GetState())
	assert.Equal("Something broke", issue.GetBody())
	assert.Equal("https://github.com/o/r/issues/10", issue.GetHTMLURL())
	assert.Equal("alice", issue.GetUser().GetLogin())
	require.Len(t, issue.Labels, 1)
	assert.Equal("bug", issue.Labels[0].GetName())
	assert.Equal("d73a4a", issue.Labels[0].GetColor())
	assert.Nil(issue.ClosedAt)

	// Test closed state
	gql.State = "CLOSED"
	gql.ClosedAt = &closed
	issue = adaptIssue(&gql)
	assert.Equal("closed", issue.GetState())
	require.NotNil(t, issue.ClosedAt)
	assert.Equal(closed, issue.ClosedAt.Time)
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestAdaptIssue -v`
Expected: compile error — `gqlIssue` and `adaptIssue` not defined.

- [ ] **Step 3: Add `gqlIssueQuery`, `gqlIssue`, and `adaptIssue` to `graphql.go`**

Add after the `gqlPR` type definition block (before the `// --- Adapter functions ---` comment):

```go
type gqlIssueQuery struct {
	Repository struct {
		Issues struct {
			Nodes    []gqlIssue
			PageInfo pageInfo
		} `graphql:"issues(first: $pageSize, states: OPEN, after: $cursor)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

type gqlIssue struct {
	DatabaseId int64 `graphql:"databaseId"`
	Number     int
	Title      string
	State      string
	Body       string
	URL        string `graphql:"url"`
	Author     struct{ Login string }
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ClosedAt   *time.Time
	Labels     struct {
		Nodes []gqlLabel
	} `graphql:"labels(first: 100)"`
	Comments struct {
		Nodes    []gqlComment
		PageInfo pageInfo
	} `graphql:"comments(first: 100)"`
}
```

Add the adapter function after `adaptPR`:

```go
func adaptIssue(gql *gqlIssue) *gh.Issue {
	state := stateToREST(gql.State)
	issue := &gh.Issue{
		ID:      new(gql.DatabaseId),
		Number:  new(gql.Number),
		Title:   new(gql.Title),
		State:   new(state),
		Body:    new(gql.Body),
		HTMLURL: new(gql.URL),
		User:    &gh.User{Login: new(gql.Author.Login)},
	}

	created := gh.Timestamp{Time: gql.CreatedAt}
	updated := gh.Timestamp{Time: gql.UpdatedAt}
	issue.CreatedAt = &created
	issue.UpdatedAt = &updated

	if gql.ClosedAt != nil {
		t := gh.Timestamp{Time: *gql.ClosedAt}
		issue.ClosedAt = &t
	}
	for _, l := range gql.Labels.Nodes {
		issue.Labels = append(issue.Labels, &gh.Label{
			Name:        new(l.Name),
			Color:       new(l.Color),
			Description: new(l.Description),
			Default:     new(l.IsDefault),
		})
	}

	return issue
}
```

- [ ] **Step 4: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestAdaptIssue -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/github/graphql.go internal/github/graphql_test.go
git commit -m "feat: add gqlIssue types and adaptIssue adapter"
```

---

### Task 2: Nil-field and edge-case adapter tests

**Files:**
- Modify: `internal/github/graphql_test.go`

- [ ] **Step 1: Write nil-field test**

Add to `internal/github/graphql_test.go`:

```go
func TestAdaptIssueNilFields(t *testing.T) {
	assert := Assert.New(t)

	gql := gqlIssue{
		Number:    1,
		Title:     "minimal",
		State:     "OPEN",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// Author empty, no labels, no ClosedAt
	issue := adaptIssue(&gql)
	assert.Empty(issue.GetUser().GetLogin())
	assert.Nil(issue.ClosedAt)
	assert.Empty(issue.Labels)
}
```

- [ ] **Step 2: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestAdaptIssueNilFields -v`
Expected: PASS (adapter already handles these cases)

- [ ] **Step 3: Commit**

```bash
git add internal/github/graphql_test.go
git commit -m "test: add nil-field edge case test for adaptIssue"
```

---

### Task 3: `BulkIssue` type and `convertGQLIssue`

**Files:**
- Modify: `internal/github/graphql_test.go`
- Modify: `internal/github/graphql.go`

- [ ] **Step 1: Write failing test for `convertGQLIssue` completeness flags**

Add to `internal/github/graphql_test.go`:

```go
func TestConvertGQLIssue(t *testing.T) {
	assert := Assert.New(t)

	now := time.Now()
	gql := gqlIssue{
		DatabaseId: 1,
		Number:     5,
		Title:      "test",
		State:      "OPEN",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	gql.Author.Login = "user"

	// All complete (no next page)
	bulk := convertGQLIssue(&gql)
	assert.True(bulk.CommentsComplete)
	assert.NotNil(bulk.Issue)
	assert.Equal(5, bulk.Issue.GetNumber())
	assert.Empty(bulk.Comments)

	// Add comments with next page
	gql.Comments.Nodes = []gqlComment{
		{DatabaseId: 100, Body: "hello", CreatedAt: now, UpdatedAt: now},
	}
	gql.Comments.Nodes[0].Author.Login = "commenter"
	gql.Comments.PageInfo.HasNextPage = true

	bulk = convertGQLIssue(&gql)
	assert.False(bulk.CommentsComplete)
	require.Len(t, bulk.Comments, 1)
	assert.Equal("hello", bulk.Comments[0].GetBody())
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestConvertGQLIssue -v`
Expected: compile error — `BulkIssue` and `convertGQLIssue` not defined.

- [ ] **Step 3: Add `BulkIssue` type and extend `RepoBulkResult`**

In `graphql.go`, add `BulkIssue` after `BulkPR` and add `Issues` field to `RepoBulkResult`:

```go
// BulkIssue holds an issue and its nested comments from a single
// GraphQL query. CommentsComplete indicates whether the comments
// connection was fully paginated.
type BulkIssue struct {
	Issue            *gh.Issue
	Comments         []*gh.IssueComment
	CommentsComplete bool
}
```

Update `RepoBulkResult`:

```go
// RepoBulkResult holds all open PRs and issues fetched via GraphQL for a repo.
type RepoBulkResult struct {
	PullRequests []BulkPR
	Issues       []BulkIssue
}
```

- [ ] **Step 4: Add `convertGQLIssue` function**

Add after `convertGQLPR`:

```go
func convertGQLIssue(gql *gqlIssue) BulkIssue {
	bulk := BulkIssue{
		Issue:            adaptIssue(gql),
		CommentsComplete: !gql.Comments.PageInfo.HasNextPage,
	}

	for i := range gql.Comments.Nodes {
		bulk.Comments = append(bulk.Comments, adaptComment(&gql.Comments.Nodes[i]))
	}

	return bulk
}
```

- [ ] **Step 5: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestConvertGQLIssue -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/graphql.go internal/github/graphql_test.go
git commit -m "feat: add BulkIssue type and convertGQLIssue"
```

---

### Task 4: `FetchRepoIssues` on `GraphQLFetcher`

**Files:**
- Modify: `internal/github/graphql.go`

- [ ] **Step 1: Add `FetchRepoIssues` and its page-size helper**

Add after `fetchRepoPRsWithPageSize`:

```go
func (g *GraphQLFetcher) FetchRepoIssues(
	ctx context.Context, owner, name string,
) (*RepoBulkResult, error) {
	result, err := g.fetchRepoIssuesWithPageSize(
		ctx, owner, name, topLevelPageSize,
	)
	if err != nil {
		slog.Warn("GraphQL issue query failed, retrying with smaller page",
			"owner", owner, "name", name,
			"err", err, "retryPageSize", retryPageSize,
		)
		result, err = g.fetchRepoIssuesWithPageSize(
			ctx, owner, name, retryPageSize,
		)
	}
	return result, err
}

func (g *GraphQLFetcher) fetchRepoIssuesWithPageSize(
	ctx context.Context, owner, name string, pageSize int,
) (*RepoBulkResult, error) {
	gqlIssues, err := fetchAllPages(ctx, func(
		ctx context.Context, cursor *string,
	) ([]gqlIssue, pageInfo, error) {
		var q gqlIssueQuery
		vars := map[string]any{
			"owner":    githubv4.String(owner),
			"name":     githubv4.String(name),
			"pageSize": githubv4.Int(pageSize),
			"cursor":   cursorVar(cursor),
		}
		if err := g.client.Query(ctx, &q, vars); err != nil {
			return nil, pageInfo{}, err
		}
		return q.Repository.Issues.Nodes,
			q.Repository.Issues.PageInfo, nil
	})
	if err != nil {
		return nil, err
	}

	result := &RepoBulkResult{
		Issues: make([]BulkIssue, 0, len(gqlIssues)),
	}
	for i := range gqlIssues {
		bulk := convertGQLIssue(&gqlIssues[i])
		result.Issues = append(result.Issues, bulk)
	}
	return result, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go vet ./internal/github/`
Expected: clean

- [ ] **Step 3: Commit**

```bash
git add internal/github/graphql.go
git commit -m "feat: add FetchRepoIssues to GraphQLFetcher"
```

---

### Task 5: `doSyncRepoGraphQLIssues` in sync.go

**Files:**
- Modify: `internal/github/sync_test.go`
- Modify: `internal/github/sync.go`

- [ ] **Step 1: Add `buildOpenIssue` test helper and `listIssueCommentsCalled` tracking to `mockClient`**

Add to `sync_test.go` after `buildGitHubLabel`:

```go
func buildOpenIssue(number int, updatedAt time.Time) *gh.Issue {
	state := "open"
	title := "test issue"
	url := "https://github.com/owner/repo/issues/" + fmt.Sprintf("%d", number)
	id := int64(number) * 1000
	return &gh.Issue{
		ID:        &id,
		Number:    &number,
		Title:     &title,
		HTMLURL:   &url,
		State:     &state,
		UpdatedAt: makeTimestamp(updatedAt),
		CreatedAt: makeTimestamp(updatedAt),
	}
}
```

Add `listIssueCommentsCalled` field to `mockClient`:

```go
listIssueCommentsCalled atomic.Int32
```

Update `ListIssueComments` on `mockClient` to track calls:

```go
func (m *mockClient) ListIssueComments(_ context.Context, _, _ string, _ int) ([]*gh.IssueComment, error) {
	m.trackCall()
	m.listIssueCommentsCalled.Add(1)
	return m.comments, nil
}
```

- [ ] **Step 2: Write failing test for `doSyncRepoGraphQLIssues`**

Add to `sync_test.go`:

```go
func TestSyncRepoGraphQLIssues(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	mock := &mockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	commentID := int64(501)
	commentBody := "I see this too"
	commentLogin := "bob"
	commentTime := gh.Timestamp{Time: now}
	result := &RepoBulkResult{
		Issues: []BulkIssue{
			{
				Issue: &gh.Issue{
					ID:        new(int64(10000)),
					Number:    new(10),
					Title:     new("Bug report"),
					State:     new("open"),
					Body:      new("Something broke"),
					HTMLURL:   new("https://github.com/owner/repo/issues/10"),
					User:      &gh.User{Login: new("alice")},
					CreatedAt: &commentTime,
					UpdatedAt: &commentTime,
				},
				Comments: []*gh.IssueComment{
					{
						ID:        &commentID,
						Body:      &commentBody,
						User:      &gh.User{Login: &commentLogin},
						CreatedAt: &commentTime,
						UpdatedAt: &commentTime,
					},
				},
				CommentsComplete: true,
			},
		},
	}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// Verify issue in DB
	issue, err := d.GetIssue(ctx, "owner", "repo", 10)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal("Bug report", issue.Title)
	assert.Equal("alice", issue.Author)
	assert.Equal("open", issue.State)
	assert.Equal(1, issue.CommentCount)

	// Verify comment event
	events, err := d.ListIssueEvents(ctx, issue.ID)
	require.NoError(t, err)
	assert.Len(events, 1)
	assert.Equal("I see this too", events[0].Body)

	// Comments were complete — ListIssueComments should NOT be called
	assert.Equal(int32(0), mock.listIssueCommentsCalled.Load())
}
```

- [ ] **Step 3: Run test, confirm it fails**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssues -v`
Expected: compile error — `doSyncRepoGraphQLIssues` not defined.

- [ ] **Step 4: Implement `doSyncRepoGraphQLIssues` in `sync.go`**

Add after `doSyncRepoGraphQL`:

```go
// doSyncRepoGraphQLIssues processes bulk GraphQL results for issues.
func (s *Syncer) doSyncRepoGraphQLIssues(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	result *RepoBulkResult,
) error {
	var failedScope failScope
	stillOpen := make(map[int]bool, len(result.Issues))

	for i := range result.Issues {
		bulk := &result.Issues[i]
		number := bulk.Issue.GetNumber()
		stillOpen[number] = true

		if err := s.syncOpenIssueFromBulk(
			ctx, repo, repoID, bulk,
		); err != nil {
			slog.Error("GraphQL sync issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
			failedScope |= failIssues
		}
	}

	// Detect closed issues — same as REST path.
	closedNumbers, err := s.db.GetPreviouslyOpenIssueNumbers(
		ctx, repoID, stillOpen,
	)
	if err != nil {
		return fmt.Errorf("get previously open issues: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosedIssue(
			ctx, repo, repoID, number,
		); err != nil {
			slog.Error("update closed issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
			failedScope |= failIssues
		}
	}

	if failedScope != 0 {
		return fmt.Errorf("GraphQL issue sync had partial failures")
	}
	return nil
}

// syncOpenIssueFromBulk processes a single issue from GraphQL bulk
// results. Uses pre-fetched data instead of per-issue REST calls.
func (s *Syncer) syncOpenIssueFromBulk(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	bulk *BulkIssue,
) error {
	number := bulk.Issue.GetNumber()
	normalized := NormalizeIssue(repoID, bulk.Issue)

	// Preserve derived fields that NormalizeIssue doesn't populate
	// from bulk data. Without this, upsert overwrites them with
	// zero values.
	existing, err := s.db.GetIssueByRepoIDAndNumber(
		ctx, repoID, number,
	)
	if err != nil {
		return fmt.Errorf(
			"get existing issue #%d: %w", number, err,
		)
	}
	if existing != nil {
		normalized.CommentCount = existing.CommentCount
		normalized.DetailFetchedAt = existing.DetailFetchedAt
	}

	issueID, err := s.db.UpsertIssue(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert issue #%d: %w", number, err)
	}

	if err := s.replaceIssueLabels(
		ctx, repoID, issueID, normalized.Labels,
	); err != nil {
		return fmt.Errorf(
			"persist labels for issue #%d: %w", number, err,
		)
	}

	if bulk.CommentsComplete {
		// Events from bulk data — skip REST ListIssueComments.
		var events []db.IssueEvent
		for _, c := range bulk.Comments {
			events = append(events, NormalizeIssueCommentEvent(issueID, c))
		}
		if len(events) > 0 {
			if err := s.db.UpsertIssueEvents(ctx, events); err != nil {
				return fmt.Errorf(
					"upsert issue events for #%d: %w", number, err,
				)
			}
		}
		// Update comment count and last activity from bulk data.
		lastActivity := normalized.UpdatedAt
		for _, c := range bulk.Comments {
			if c.UpdatedAt != nil && c.UpdatedAt.After(lastActivity) {
				lastActivity = c.UpdatedAt.Time
			}
		}
		_, err = s.db.WriteDB().ExecContext(ctx,
			`UPDATE middleman_issues SET comment_count = ?, last_activity_at = ?
			 WHERE id = ?`,
			len(bulk.Comments), lastActivity, issueID,
		)
		if err != nil {
			return fmt.Errorf(
				"update issue #%d derived fields: %w", number, err,
			)
		}
	} else {
		// Comments truncated — fall back to REST.
		if err := s.refreshIssueTimeline(
			ctx, repo, issueID, bulk.Issue,
		); err != nil {
			return fmt.Errorf(
				"refresh timeline for issue #%d: %w", number, err,
			)
		}
	}

	return nil
}
```

- [ ] **Step 5: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssues -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: add doSyncRepoGraphQLIssues for bulk issue processing"
```

---

### Task 6: Comments-incomplete REST fallback test

**Files:**
- Modify: `internal/github/sync_test.go`

- [ ] **Step 1: Write test for incomplete comments triggering REST fallback**

Add to `sync_test.go`:

```go
func TestSyncRepoGraphQLIssuesCommentsIncomplete(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	commentTime := gh.Timestamp{Time: now}

	commentID := int64(777)
	commentBody := "REST comment"
	commentLogin := "carol"

	mock := &mockClient{
		comments: []*gh.IssueComment{
			{
				ID:        &commentID,
				Body:      &commentBody,
				User:      &gh.User{Login: &commentLogin},
				CreatedAt: &commentTime,
				UpdatedAt: &commentTime,
			},
		},
	}
	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	result := &RepoBulkResult{
		Issues: []BulkIssue{
			{
				Issue: &gh.Issue{
					ID:        new(int64(20000)),
					Number:    new(20),
					Title:     new("Lots of comments"),
					State:     new("open"),
					HTMLURL:   new("https://github.com/owner/repo/issues/20"),
					User:      &gh.User{Login: new("dave")},
					CreatedAt: &commentTime,
					UpdatedAt: &commentTime,
				},
				CommentsComplete: false,
			},
		},
	}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// REST fallback should have been called
	assert.Equal(int32(1), mock.listIssueCommentsCalled.Load())

	// Verify the REST comment landed
	issue, err := d.GetIssue(ctx, "owner", "repo", 20)
	require.NoError(t, err)
	require.NotNil(t, issue)

	events, err := d.ListIssueEvents(ctx, issue.ID)
	require.NoError(t, err)
	assert.Len(events, 1)
	assert.Equal("REST comment", events[0].Body)
}
```

- [ ] **Step 2: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssuesCommentsIncomplete -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/github/sync_test.go
git commit -m "test: verify REST fallback when GraphQL issue comments incomplete"
```

---

### Task 7: Closure detection test

**Files:**
- Modify: `internal/github/sync_test.go`

- [ ] **Step 1: Write test for closure detection via DB diff**

Add to `sync_test.go`:

```go
func TestSyncRepoGraphQLIssuesClosureDetection(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)

	// Pre-seed an open issue that will not appear in GraphQL results
	_, err = d.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     30000,
		Number:         30,
		URL:            "https://github.com/owner/repo/issues/30",
		Title:          "Will be closed",
		Author:         "eve",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(t, err)

	closedAt := gh.Timestamp{Time: now}
	closedState := "closed"
	closedIssueID := int64(30000)
	closedNumber := 30
	closedTitle := "Will be closed"

	mock := &mockClient{
		getIssueFn: func(_ context.Context, _, _ string, number int) (*gh.Issue, error) {
			if number == 30 {
				return &gh.Issue{
					ID:       &closedIssueID,
					Number:   &closedNumber,
					Title:    &closedTitle,
					State:    &closedState,
					ClosedAt: &closedAt,
				}, nil
			}
			return nil, fmt.Errorf("unexpected issue %d", number)
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// GraphQL returns no issues (issue #30 was closed)
	result := &RepoBulkResult{Issues: []BulkIssue{}}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// Issue should now be closed
	issue, err := d.GetIssue(ctx, "owner", "repo", 30)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal("closed", issue.State)
	assert.NotNil(issue.ClosedAt)
}
```

- [ ] **Step 2: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssuesClosureDetection -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/github/sync_test.go
git commit -m "test: verify GraphQL issue closure detection via DB diff"
```

---

### Task 8: Preserved fields test

**Files:**
- Modify: `internal/github/sync_test.go`

- [ ] **Step 1: Write test verifying existing CommentCount is preserved**

Add to `sync_test.go`:

```go
func TestSyncRepoGraphQLIssuesPreservesExistingFields(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID, err := d.UpsertRepo(ctx, "github.com", "owner", "repo")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	fetchedAt := now.Add(-time.Hour)

	// Pre-seed issue with existing derived fields
	issueID, err := d.UpsertIssue(ctx, &db.Issue{
		RepoID:          repoID,
		PlatformID:      40000,
		Number:          40,
		URL:             "https://github.com/owner/repo/issues/40",
		Title:           "Existing issue",
		Author:          "frank",
		State:           "open",
		CommentCount:    5,
		DetailFetchedAt: &fetchedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
		LastActivityAt:  now,
	})
	require.NoError(t, err)
	_ = issueID

	commentTime := gh.Timestamp{Time: now}
	mock := &mockClient{}
	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, nil,
	)

	// GraphQL returns the same issue with no comments (incomplete)
	result := &RepoBulkResult{
		Issues: []BulkIssue{
			{
				Issue: &gh.Issue{
					ID:        new(int64(40000)),
					Number:    new(40),
					Title:     new("Existing issue"),
					State:     new("open"),
					HTMLURL:   new("https://github.com/owner/repo/issues/40"),
					User:      &gh.User{Login: new("frank")},
					CreatedAt: &commentTime,
					UpdatedAt: &commentTime,
				},
				CommentsComplete: false,
			},
		},
	}

	err = syncer.doSyncRepoGraphQLIssues(ctx,
		RepoRef{Owner: "owner", Name: "repo", PlatformHost: "github.com"},
		repoID, result,
	)
	require.NoError(t, err)

	// CommentCount should be preserved (not overwritten to 0)
	issue, err := d.GetIssue(ctx, "owner", "repo", 40)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal(5, issue.CommentCount)
	assert.NotNil(issue.DetailFetchedAt)
}
```

- [ ] **Step 2: Run test, confirm it passes**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssuesPreservesExistingFields -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/github/sync_test.go
git commit -m "test: verify GraphQL issue sync preserves existing derived fields"
```

---

### Task 9: Wire GraphQL path into `indexSyncRepo`

**Files:**
- Modify: `internal/github/sync_test.go`
- Modify: `internal/github/sync.go`

- [ ] **Step 1: Write failing integration test for REST-fallback path**

This test verifies that when `FetchRepoIssues` fails, the REST `indexSyncIssues` path runs.

Add to `sync_test.go`:

```go
func TestSyncRepoGraphQLIssuesFallbackToREST(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	issueTime := makeTimestamp(now)
	issueID := int64(50000)
	issueNumber := 50
	issueTitle := "REST issue"
	issueState := "open"
	issueURL := "https://github.com/owner/repo/issues/50"
	issueLogin := "grace"

	mock := &mockClient{
		openIssues: []*gh.Issue{
			{
				ID:        &issueID,
				Number:    &issueNumber,
				Title:     &issueTitle,
				State:     &issueState,
				HTMLURL:   &issueURL,
				User:      &gh.User{Login: &issueLogin},
				CreatedAt: issueTime,
				UpdatedAt: issueTime,
			},
		},
	}

	syncer := NewSyncer(
		map[string]Client{"github.com": mock},
		d, nil,
		[]RepoRef{{Owner: "owner", Name: "repo", PlatformHost: "github.com"}},
		time.Minute, nil, testBudget(1000),
	)

	// Set up a fetcher that always fails
	syncer.SetFetchers(map[string]*GraphQLFetcher{
		"github.com": {client: nil}, // nil client will panic/error
	})

	// Run full sync — should fall back to REST
	syncer.RunOnce(ctx)

	// Issue should be in DB via REST path
	issue, err := d.GetIssue(ctx, "owner", "repo", 50)
	require.NoError(t, err)
	require.NotNil(t, issue)
	assert.Equal("REST issue", issue.Title)
	assert.Equal("grace", issue.Author)
}
```

- [ ] **Step 2: Run test, confirm it fails or passes depending on current wiring**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssuesFallbackToREST -v`

If the GraphQL path isn't wired yet, the REST path should still work (test passes). If it panics on nil client, it needs the wiring changes.

- [ ] **Step 3: Refactor issue sync block in `indexSyncRepo`**

In `sync.go`, replace the current issue sync block (the `indexSyncIssues` call around line 1298-1308) with the ETag-gated GraphQL pattern:

```go
	// Index issues — ETag-gated, with GraphQL when available.
	// Same structure as PR sync: REST list first (ETag gate),
	// then GraphQL if available, REST fallback if not.
	ghIssues, issueListErr := client.ListOpenIssues(
		ctx, repo.Owner, repo.Name,
	)
	if issueListErr != nil {
		if IsNotModified(issueListErr) {
			// 304: open issue list unchanged, skip.
		} else {
			slog.Error("list open issues failed",
				"repo", repo.Owner+"/"+repo.Name,
				"err", issueListErr,
			)
			failedScope |= failIssues
		}
	} else {
		graphQLIssuesDone := false
		if fetcher := s.fetcherFor(repo); fetcher != nil {
			if backoff, _ := fetcher.ShouldBackoff(); !backoff {
				issueResult, gqlErr := fetcher.FetchRepoIssues(
					ctx, repo.Owner, repo.Name,
				)
				if gqlErr != nil {
					slog.Warn("GraphQL issue fetch failed, falling back to REST",
						"repo", repo.Owner+"/"+repo.Name,
						"err", gqlErr,
					)
				} else {
					if err := s.doSyncRepoGraphQLIssues(
						ctx, repo, repoID, issueResult,
					); err != nil {
						failedScope |= failIssues
					}
					graphQLIssuesDone = true
				}
			}
		}

		if !graphQLIssuesDone {
			// REST fallback using already-fetched ghIssues.
			if err := s.syncIssuesFromList(
				ctx, repo, repoID, ghIssues, forceIssues,
			); err != nil {
				slog.Error("REST issue sync failed",
					"repo", repo.Owner+"/"+repo.Name,
					"err", err,
				)
				failedScope |= failIssues
			}
		}
	}
```

- [ ] **Step 4: Extract `syncIssuesFromList` from `indexSyncIssues`**

The existing `indexSyncIssues` does two things: (1) calls `ListOpenIssues` and (2) processes the results. Extract the processing part so both the GraphQL fallback path and the standalone REST path can reuse it.

Rename the body of `indexSyncIssues` (from after the `ListOpenIssues` call) into a new function:

```go
// syncIssuesFromList processes a pre-fetched list of open issues
// via the REST path. Handles per-issue upsert and closure detection.
func (s *Syncer) syncIssuesFromList(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	ghIssues []*gh.Issue,
	forceRefresh bool,
) error {
	stillOpen := make(map[int]bool, len(ghIssues))
	for _, issue := range ghIssues {
		stillOpen[issue.GetNumber()] = true
	}

	var hadItemFailure bool
	for _, ghIssue := range ghIssues {
		if err := s.syncOpenIssue(ctx, repo, repoID, ghIssue, forceRefresh); err != nil {
			slog.Error("sync issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghIssue.GetNumber(),
				"err", err,
			)
			hadItemFailure = true
		}
	}

	closedNumbers, err := s.db.GetPreviouslyOpenIssueNumbers(
		ctx, repoID, stillOpen,
	)
	if err != nil {
		return fmt.Errorf("get previously open issues: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosedIssue(
			ctx, repo, repoID, number,
		); err != nil {
			slog.Error("update closed issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
			hadItemFailure = true
		}
	}

	if hadItemFailure {
		return fmt.Errorf("one or more issue sync items failed")
	}
	return nil
}
```

Update `indexSyncIssues` to call `syncIssuesFromList`:

```go
func (s *Syncer) indexSyncIssues(
	ctx context.Context, repo RepoRef, repoID int64, forceRefresh bool,
) error {
	client := s.clientFor(repo)
	ghIssues, err := client.ListOpenIssues(
		ctx, repo.Owner, repo.Name,
	)
	if err != nil {
		if IsNotModified(err) {
			return nil
		}
		return fmt.Errorf("list open issues: %w", err)
	}
	return s.syncIssuesFromList(ctx, repo, repoID, ghIssues, forceRefresh)
}
```

- [ ] **Step 5: Run all existing tests to verify refactor**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -v -count=1`
Expected: all existing tests PASS

- [ ] **Step 6: Run the fallback test**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncRepoGraphQLIssuesFallbackToREST -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: wire GraphQL issue sync into indexSyncRepo with ETag gate"
```

---

### Task 10: E2E test

**Files:**
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Write e2e test**

Add to `api_test.go`:

```go
func TestAPISyncIssuesViaGraphQL(t *testing.T) {
	assert := Assert.New(t)
	ctx := context.Background()

	mock := &mockGH{}
	srv, database := setupTestServerWithMock(t, mock)
	client := setupTestClient(t, srv)

	// Seed via DB directly — simulating what GraphQL sync produces.
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	issueID, err := database.UpsertIssue(ctx, &db.Issue{
		RepoID:         repoID,
		PlatformID:     60000,
		Number:         60,
		URL:            "https://github.com/acme/widget/issues/60",
		Title:          "GraphQL synced issue",
		Author:         "testuser",
		State:          "open",
		Body:           "Synced via GraphQL",
		CommentCount:   2,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(t, err)

	// Add a label
	require.NoError(t, database.ReplaceIssueLabels(ctx, repoID, issueID, []db.Label{
		{PlatformID: 1, Name: "bug", Color: "d73a4a", UpdatedAt: now},
	}))

	// Add a comment event
	require.NoError(t, database.UpsertIssueEvents(ctx, []db.IssueEvent{
		{
			IssueID:   issueID,
			EventType: "issue_comment",
			Author:    "commenter",
			Body:      "I can reproduce",
			CreatedAt: now,
			DedupeKey: "issue-comment-601",
		},
	}))

	// Verify via ListIssues API
	resp, err := client.HTTP.ListIssuesWithResponse(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.Len(t, resp.JSON200.Issues, 1)

	apiIssue := resp.JSON200.Issues[0]
	assert.Equal(60, apiIssue.Number)
	assert.Equal("GraphQL synced issue", apiIssue.Title)
	assert.Equal("testuser", apiIssue.Author)
	assert.Equal("open", apiIssue.State)
	require.Len(t, *apiIssue.Labels, 1)
	assert.Equal("bug", (*apiIssue.Labels)[0].Name)

	// Verify via GetIssue API
	detailResp, err := client.HTTP.GetReposByOwnerByNameIssuesByNumberWithResponse(
		ctx, "acme", "widget", 60,
	)
	require.NoError(t, err)
	require.Equal(t, 200, detailResp.StatusCode())
	require.NotNil(t, detailResp.JSON200)
	assert.Equal("Synced via GraphQL", *detailResp.JSON200.Body)
	assert.Equal(2, detailResp.JSON200.CommentCount)
}
```

- [ ] **Step 2: Run test**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./internal/server/ -run TestAPISyncIssuesViaGraphQL -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/api_test.go
git commit -m "test: add e2e test for GraphQL-synced issues via API"
```

---

### Task 11: Run full test suite and lint

**Files:** none (validation only)

- [ ] **Step 1: Run all Go tests**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go test ./... -count=1`
Expected: all PASS

- [ ] **Step 2: Run vet**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go --command go vet ./...`
Expected: clean

- [ ] **Step 3: Run linter**

Run: `CGO_ENABLED=0 nix shell nixpkgs#go nixpkgs#golangci-lint --command golangci-lint run ./...`
Expected: clean (fix any warnings)

- [ ] **Step 4: Commit any lint fixes**

```bash
git add -A
git commit -m "fix: address lint warnings"
```

Only commit if there were changes. Skip if clean.
