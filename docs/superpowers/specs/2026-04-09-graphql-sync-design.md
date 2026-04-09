# GraphQL Sync for GitHub API Rate Limit Reduction

## Problem

The periodic sync engine makes multiple REST API calls per open PR (get PR, list comments, list reviews, list commits, get combined status, list check runs). At moderate scale, this easily exceeds GitHub's 5,000 requests/hour rate limit.

## Solution

Replace per-PR REST read calls with bulk GraphQL queries using `shurcooL/githubv4`. A single GraphQL query per repo fetches all open PRs with nested comments, reviews, commits, and CI status. Mutations (merge, comment, review, state changes) remain REST via existing `go-github` client.

Combined with ETags (implemented in a separate PR), the flow becomes: ETag check per repo, skip if unchanged, GraphQL bulk fetch only for repos with changes.

Estimated reduction: orders of magnitude fewer API calls per sync cycle.

## Architecture

### GraphQL Client Layer

New file: `internal/github/graphql.go`

`GraphQLFetcher` struct wraps `shurcooL/githubv4` client:

```go
type GraphQLFetcher struct {
    client      *githubv4.Client
    rateTracker *RateTracker
}
```

Constructor takes token + platformHost. For GitHub Enterprise, uses `githubv4.NewEnterpriseClient("https://{host}/api/graphql", httpClient)`.

Core method:

```go
func (g *GraphQLFetcher) FetchRepoPRs(ctx context.Context, owner, repo string) (*RepoBulkResult, error)
```

Sends a single GraphQL query fetching all open PRs with nested data:

```graphql
repository(owner: $owner, name: $name) {
  pullRequests(first: 25, states: OPEN) {
    pageInfo { hasNextPage endCursor }
    nodes {
      number title state isDraft body url
      author { login }
      createdAt updatedAt mergedAt closedAt
      additions deletions mergeable
      headRefName baseRefName headRefOid baseRefOid
      headRepository { url }
      comments(first: 50) { nodes { ... } pageInfo { ... } }
      reviews(first: 50) { nodes { ... } pageInfo { ... } }
      allCommits: commits(first: 50) { nodes { commit { ... } } pageInfo { ... } }
      lastCommit: commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
              contexts(first: 50) {
                pageInfo { hasNextPage endCursor }
                nodes {
                  ... on CheckRun { name status conclusion detailsUrl app { name } }
                  ... on StatusContext { context state targetUrl }
                }
              }
            }
          }
        }
      }
    }
  }
}
```

### Bulk Result Types

```go
type RepoBulkResult struct {
    PullRequests []BulkPR
}

type BulkPR struct {
    PR             *gh.PullRequest
    Comments       []*gh.IssueComment
    Reviews        []*gh.PullRequestReview
    Commits        []*gh.RepositoryCommit
    CheckRuns      []*gh.CheckRun
    CombinedStatus *gh.CombinedStatus  // adapter wraps GraphQL StatusContext nodes
}
```

GraphQL response types (private structs with `graphql` tags) get mapped to go-github types via adapter functions. The normalize layer stays unchanged.

A similar method handles issues: `FetchRepoIssues(ctx, owner, repo)`.

### Sync Engine Changes

`Syncer` struct gains `fetchers map[string]*GraphQLFetcher` alongside existing `clients map[string]Client`.

`doSyncRepo` new flow:

1. Call `fetcher.FetchRepoPRs()` — single GraphQL query (+ pagination if PRs exceed page size)
2. Loop over `BulkPR` results:
   - `NormalizePR()` using mapped `*gh.PullRequest` (unchanged)
   - Compare with existing DB record (same `UpdatedAt` check as today)
   - `UpsertMergeRequest()` (unchanged)
   - Normalize events from pre-fetched slices (unchanged)
   - `UpsertMREvents()` (unchanged)
   - `DeriveOverallCIStatus()` + `NormalizeCIChecks()` from pre-fetched check runs and status contexts (unchanged)
   - `UpdateMRCIStatus()`, `UpdateMRDerivedFields()` (unchanged)
3. Detect closed PRs via `GetPreviouslyOpenMRNumbers` diff (unchanged)
4. For closed PRs: single REST `GetPullRequest()` call (infrequent, not worth a GraphQL query)
5. Same pattern for issues via `FetchRepoIssues()`

ETag integration: before calling `FetchRepoPRs`, check ETag for repo. If 304, skip entire GraphQL fetch.

Conditional sync still applies: even with bulk data, skip timeline upsert if `UpdatedAt` unchanged. Bulk fetch is cheap (1 query), but DB writes are avoided for unchanged PRs.

Manual sync methods (`SyncMR`, `SyncIssue`) stay REST-based. They're single-item, infrequent, triggered by user action.

### Rate Tracking Adaptation

Schema change in `rate_limits` table:

- Add `api_type TEXT NOT NULL DEFAULT 'rest'` column
- Unique constraint changes from `(platform_host)` to `(platform_host, api_type)`
- Migration via `ALTER TABLE ADD COLUMN` + recreate index

`RateTracker` changes:

- `ShouldBackoff(apiType string)` — checks correct budget
- `RecordRequest(apiType string)` — records against correct budget
- `UpdateFromRate()` unchanged — both REST and GraphQL return same `X-RateLimit-*` headers
- Internal state becomes `map[string]rateBucket` keyed by api type

Callers:

- Sync loop: `ShouldBackoff("graphql")` before `FetchRepoPRs`
- Mutation handlers: `ShouldBackoff("rest")` (add explicit parameter to existing calls)
- `GraphQLFetcher`: calls `RecordRequest("graphql")` after each query
- Existing `liveClient`: calls `RecordRequest("rest")` (add parameter)

`SyncStatus` exposes both budgets for frontend display.

### Pagination Helper

Generic helper for `shurcooL/githubv4` cursor loops:

```go
func fetchAllPages[T any](
    ctx context.Context,
    client *githubv4.Client,
    query any,
    variables map[string]any,
    cursorVar string,
    extract func(query any) (nodes []T, pageInfo PageInfo),
) ([]T, error)
```

Three levels of pagination:

1. **Top-level:** PRs in a repo exceeding page size. Cursor loop on `FetchRepoPRs`, re-query with `after: endCursor`.
2. **Nested:** Comments/reviews/commits within a single PR exceeding page size. Detected via `pageInfo.hasNextPage` on nested connection. Separate follow-up query scoped to that PR+connection.
3. **CI contexts:** `statusCheckRollup.contexts` exceeding page size. Same nested pagination pattern — follow-up query scoped to that PR's head commit's `statusCheckRollup`.

Most PRs fit within a single page for all nested connections. Follow-up queries handle the rest.

### Query Complexity and Chunking

GitHub GraphQL has a 500,000 node limit per query. A bulk query fetching many PRs with all nested connections could approach this limit on busy repos.

Mitigation strategy:

1. **Smaller top-level pages:** Use a conservative top-level page size (e.g., 25 PRs per query) to keep node count well under limits.
2. **Nested page sizes:** Use smaller page sizes for nested connections (comments, reviews, commits, CI contexts). PRs exceeding the page size get follow-up queries.
3. **Retry on complexity errors:** If GitHub returns a complexity/node-limit error, halve the top-level page size and retry. Log a warning so operators can tune if needed.
4. **Per-repo error isolation:** A failed GraphQL query for one repo does not block other repos. Log the error, skip that repo for this cycle, retry next cycle. Same behavior as current REST path.

### Partial Failure and Data Integrity

GraphQL can return partial data — some PRs succeed while others have null fields or errors. The sync engine must not overwrite good DB data with incomplete GraphQL results.

Rules:

1. **Per-PR atomicity:** Only upsert a PR's data (MR row, events, CI status) if the GraphQL response for that PR has no execution errors — no entries in the `errors` array referencing that PR's path. Nullable fields (`body`, `headRepository`, `mergedAt`, `closedAt`) returning `null` are normal and not treated as failures. A PR is incomplete only when the `errors` array contains an error whose `path` points into that PR's node.
2. **Per-connection atomicity:** If a nested connection (comments, reviews, commits, CI contexts) fails during follow-up pagination, do not partially replace events. Either all pages succeeded and the full set is upserted, or none are and existing events are preserved.
3. **CI status:** Only call `UpdateMRCIStatus` if `statusCheckRollup` resolved fully (including all paginated context pages). A missing CI page could hide failures — better to keep stale-but-complete data than write truncated results.
4. **Logging:** Log warnings for every skipped PR or connection with the GraphQL error details, so operators can identify persistent issues.

## Testing

- **Adapter tests** (`graphql_test.go`): Test mapping from GraphQL response structs to `*gh.PullRequest`, etc. Pure data mapping, no HTTP.
- **Pagination tests**: Mock query function returning multi-page results, verify accumulation. Include nested pagination follow-up for comments and CI contexts exceeding page size.
- **Sync integration tests**: `httptest.Server` returning canned GraphQL JSON. Verify same DB state as REST path for identical input.
- **Error handling tests**: GraphQL partial responses (some PRs succeed, some fields null), complexity limit errors triggering page-size reduction, and nested pagination follow-up failures. Verify sync does not silently drop CI or event data — partial failures should log warnings and preserve previously-synced data.
- **Rate tracker tests**: Extend existing tests with `apiType` parameter. Verify independent REST/GraphQL budgets.

No new test infrastructure needed.

## Files Changed

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Add `shurcooL/githubv4` + `shurcooL/graphql` |
| `internal/github/graphql.go` | New: GraphQLFetcher, bulk fetch, pagination helper, type adapters |
| `internal/github/graphql_test.go` | New: adapter + pagination tests |
| `internal/github/sync.go` | Modify `doSyncRepo` to use GraphQLFetcher, add fetchers map |
| `internal/github/rate.go` | Add `apiType` param to `ShouldBackoff`/`RecordRequest` |
| `internal/github/rate_test.go` | Extend with `apiType` tests |
| `internal/github/client.go` | Construct `GraphQLFetcher` alongside REST client |
| `internal/db/db.go` | Migration: add `api_type` column to `rate_limits` |
| `internal/server/huma_routes.go` | Pass `"rest"` to `ShouldBackoff` calls in mutation handlers |

## Dependencies

- `github.com/shurcooL/githubv4` — zero external deps, stdlib only, no CGO
- `github.com/shurcooL/graphql` — transitive dep of above, also zero external deps

No changes to Makefile, frontend, config format, or OpenAPI spec.
