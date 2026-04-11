# GraphQL Issues Sync

Extend the existing GraphQL sync path to fetch open issues via GitHub's GraphQL API (v4), matching the pattern established for PRs. Issues are structurally simpler than PRs (no reviews, CI, commits, diff SHAs, mergeable state), so the implementation is a subset of the PR path.

## Decisions

- **Separate queries**: `FetchRepoIssues()` is its own GraphQL query, not combined with `FetchRepoPRs()`. The `fetchAllPages` pagination helper operates on a single connection; combining two independently-paginating connections would require new pagination logic.
- **OPEN only**: GraphQL fetches `states: OPEN`. Closure detection uses the existing DB-diff pattern (`GetPreviouslyOpenIssueNumbers`) with individual REST fetches for closed issues via `fetchAndUpdateClosedIssue`.
- **Sequential**: Issue GraphQL fetch runs after PR GraphQL fetch in `indexSyncRepo`. No concurrent fetching — avoids doubling instantaneous rate-limit pressure and keeps shared backoff state coherent.
- **Extend `RepoBulkResult`**: Add `Issues []BulkIssue` field to existing struct rather than creating a separate result type.

## GraphQL Types & Adapters

New types in `graphql.go`:

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
    DatabaseId int64
    Number     int
    Title      string
    State      string
    Body       string
    URL        string
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

`gqlLabel`, `gqlComment`, and `pageInfo` are reused from the PR path. No duplication.

New adapter function:

```go
func adaptIssue(gql *gqlIssue) *gh.Issue
```

Converts `gqlIssue` to `*gh.Issue` (go-github type). Same pattern as `adaptPR`. `adaptComment()` is already shared and works for both.

New bulk result type:

```go
type BulkIssue struct {
    Issue            *gh.Issue
    Comments         []*gh.IssueComment
    CommentsComplete bool
}
```

`RepoBulkResult` gains an `Issues []BulkIssue` field.

## GraphQLFetcher

New method on `GraphQLFetcher`:

```go
func (g *GraphQLFetcher) FetchRepoIssues(
    ctx context.Context, owner, name string,
) (*RepoBulkResult, error)
```

Mirrors `FetchRepoPRs()`: uses `fetchAllPages` with `gqlIssueQuery`, same retry-with-smaller-page-size pattern. Returns `RepoBulkResult` with only `Issues` populated.

Helper `convertGQLIssue(*gqlIssue) BulkIssue` parallel to `convertGQLPR`.

## Sync Integration

In `indexSyncRepo`, after the PR GraphQL/REST block, the existing `indexSyncIssues` call gains a GraphQL path:

1. Check `fetcherFor(repo)` and `ShouldBackoff()`.
2. If available and not rate-limited: call `FetchRepoIssues()`.
3. On error: log warning, fall through to REST `indexSyncIssues`.
4. On success: call `doSyncRepoGraphQLIssues()`, set `graphQLIssuesDone = true`.
5. If `!graphQLIssuesDone`: run existing REST `indexSyncIssues`.

New `doSyncRepoGraphQLIssues()` method on `Syncer`, parallel to `doSyncRepoGraphQL()`:

For each `BulkIssue`:
1. `NormalizeIssue(repoID, bulk.Issue)` (existing function).
2. Preserve existing `CommentCount` and `DetailFetchedAt` from DB row if present (same pattern as `syncOpenMRFromBulk` preserving derived fields).
3. `UpsertIssue` then `replaceIssueLabels`.
4. If `CommentsComplete`: upsert events and update comment count from bulk data directly, skipping REST `ListIssueComments`.
5. If `!CommentsComplete`: fall back to `refreshIssueTimeline` via REST.

Closure detection: same DB-diff pattern as PRs. `GetPreviouslyOpenIssueNumbers` then `fetchAndUpdateClosedIssue` via REST for each.

## Budget Integration

No changes needed. `budgetTransport` already wraps all HTTP calls made through the `GraphQLFetcher`'s `http.Client`, counting GraphQL requests against the sync budget. `IssueDetailWorstCase` remains relevant for REST fallback on incomplete comments.

## Consolidation

Shared types requiring no duplication:
- `gqlComment`, `gqlLabel` — used by both PR and issue queries.
- `adaptComment()` — works for both PR and issue comments.
- `fetchAllPages` — generic, works with any node type.
- `normalizeLabels` — shared normalization.
- `pageInfo` — shared pagination type.

No new abstractions forced. PR and issue sync methods follow the same template but operate on different field sets and DB calls, so they remain separate concrete functions.

## Testing

### Unit tests (no DB, in `graphql_test.go`)

- `TestAdaptIssue`: verify `adaptIssue` produces correct go-github fields. Cover open state, closed state with `ClosedAt`, nil author, labels, empty body.
- `TestConvertGQLIssue`: verify `CommentsComplete` flag from `PageInfo`.
- `TestAdaptIssueNilFields`: nil `ClosedAt`, empty labels.

### Integration tests (with DB via `openTestDB`, in `sync_test.go`)

- `TestSyncRepoGraphQLIssues`: mock fetcher returns `BulkIssue` data, verify issues land in DB with correct fields, labels, and comment events.
- `TestSyncRepoGraphQLIssuesCommentsComplete`: `CommentsComplete=true` triggers event upsert from bulk data. Assert mock's `ListIssueComments` never called.
- `TestSyncRepoGraphQLIssuesCommentsIncomplete`: `CommentsComplete=false` triggers REST fallback via `ListIssueComments`.
- `TestSyncRepoGraphQLIssuesClosureDetection`: pre-seed open issue in DB, GraphQL omits it, verify `fetchAndUpdateClosedIssue` fires and state updates.
- `TestSyncRepoGraphQLIssuesFallbackToREST`: `FetchRepoIssues` errors, verify REST `indexSyncIssues` path runs.
- `TestSyncRepoGraphQLIssuesPreservesExistingFields`: existing `CommentCount` preserved from DB when bulk data doesn't include it.

### E2E test (full HTTP stack, real SQLite, in `api_test.go`)

- `TestAPISyncIssuesViaGraphQL`: set up server with syncer configured with GraphQL fetcher (or mock equivalent), trigger sync via API, verify issues appear correctly through `ListIssues` and `GetIssue` API endpoints with labels and comments. Exercises the full path: GraphQL fetch -> normalize -> DB upsert -> API response.

Uses existing infrastructure: `setupTestServer`, `setupTestClient`, generated API client, `seedIssue` helper.
