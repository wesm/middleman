# Stacked PRs Feature Design

## Overview

Add stack-aware PR visualization and merge workflow management to middleman. Stacks are detected automatically from branch chains (platform-agnostic) and surfaced in two places: a dedicated Stacks view for overview/triage and a right-rail sidebar in PR detail for in-context navigation.

## Stack Detection

A stack is a chain of PRs where each PR's `base_branch` matches another open PR's `head_branch` in the same repo. The base of a stack targets the repo's default branch (e.g. `main`).

Detection runs after each sync cycle. No manual tagging required. Works with any branching tool (git-town, ghstack, Graphite, Sapling, manual).

### Stack Naming

Derived from the longest common prefix of branch names in the chain, matched on token boundaries (`/`, `-`, `_`) to avoid partial-word matches. Conventional prefixes (`feature/`, `fix/`, etc.) and trailing separators are stripped. Falls back to the base PR's title if no common prefix exists.

Example: `feature/auth-fix`, `feature/auth-retry`, `feature/auth-ui` → "auth"

### Stack Health

Each stack gets an aggregate health status derived from its members. Evaluated in priority order (first match wins):

| Priority | Status | Condition |
|----------|--------|-----------|
| 1 | Blocked | Any non-merged PR in the chain has `changes_requested` or failing CI, and at least one descendant exists below it |
| 2 | N/M merged | At least one PR merged, stack not yet complete |
| 3 | All green | Every open PR has CI passing and review approved |
| 4 | Base ready | Lowest open PR ready to merge (CI + review), some descendants not ready |
| 5 | In progress | None of the above — mixed states, no hard blockers |

Note: "Base ready" refers to the lowest *open* PR in the stack, which may not be position 1 if earlier PRs have already merged.

### Cascade Health Propagation

When any open PR in the chain has `changes_requested` or failing CI, all of its descendant PRs display dimmed with a "blocked by #N" label. This is visual only — it does not prevent navigation or viewing. Cascade propagates transitively: if #142 blocks #143, and #143 blocks #144, then #144 shows "blocked by #142" (the root blocker).

### Merged PR Treatment

Merged PRs display struck-through and dimmed (opacity 0.4). A stack is removed from the Stacks view when all its PRs are merged or closed.

## Data Model

New tables are added via a numbered migration under `internal/db/migrations/` (e.g. `000006_add_stacks.up.sql` / `.down.sql`). `internal/db/migrations/` is the source of truth for schema evolution per project conventions — no changes to `schema.sql` or `SchemaVersion` (those are legacy and replaced by golang-migrate).

### New Table: `middleman_stacks`

```sql
CREATE TABLE IF NOT EXISTS middleman_stacks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id INTEGER NOT NULL REFERENCES middleman_repos(id),
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

### New Table: `middleman_stack_members`

```sql
CREATE TABLE IF NOT EXISTS middleman_stack_members (
    stack_id INTEGER NOT NULL REFERENCES middleman_stacks(id) ON DELETE CASCADE,
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,  -- 1 = base (closest to main), ascending
    PRIMARY KEY (stack_id, merge_request_id)
);

CREATE INDEX IF NOT EXISTS idx_stack_members_mr
    ON middleman_stack_members(merge_request_id);
CREATE INDEX IF NOT EXISTS idx_stacks_repo
    ON middleman_stacks(repo_id);
```

Cross-repo stacking is prevented at the query level: the detection algorithm always scopes to a single `repo_id`. The schema intentionally does not add a `repo_id` column to `middleman_stack_members` since the FK through `middleman_stacks.repo_id` already constrains this — but detection queries must always filter by repo.

### Go Types

Add to `internal/db/types.go`:

```go
type Stack struct {
    ID        int64
    RepoID    int64
    Name      string
    CreatedAt time.Time
    UpdatedAt time.Time
}

type StackMember struct {
    StackID        int64
    MergeRequestID int64
    Position       int
}
```

### Query Functions

Add to `internal/db/queries.go`:

- `ListPRsForStackDetection(ctx, repoID) ([]MergeRequest, error)` — returns non-closed PRs for a repo (state IN open, merged), selecting only fields needed for detection (id, number, head_branch, base_branch, state, ci_status, review_decision).
- `UpsertStack(ctx, repoID, name) (int64, error)` — insert or update a stack, returns stack ID.
- `ReplaceStackMembers(ctx, stackID, members []StackMember) error` — delete existing members and insert new ones in a transaction.
- `DeleteStaleStacks(ctx, repoID, activeStackIDs []int64) error` — remove stacks for this repo that are not in the active set.
- `ListStacksWithMembers(ctx, repoFilter string) ([]StackWithRepo, map[int64][]StackMemberWithPR, error)` — returns all stacks (joined with `middleman_repos` for `owner`/`name`) and their member PRs. `StackWithRepo` embeds `Stack` and adds `RepoOwner string`, `RepoName string`. Used by the `/api/v1/stacks` endpoint.
- `GetStackForPR(ctx, owner, name string, number int) (*Stack, []StackMemberWithPR, error)` — returns stack context for a specific PR, or nil if not in a stack. Used by the `/repos/{owner}/{name}/pulls/{number}/stack` endpoint.

`StackMemberWithPR` is a query result type combining `StackMember` fields with the PR fields needed for `stackMemberResponse` (number, title, state, ci_status, review_decision, is_draft). The `blocked_by` field is computed server-side during response building by walking the member chain from base to tip: the first ancestor with `changes_requested` or failing CI becomes the root blocker for all descendants.

### Detection Algorithm

Detection runs after each sync cycle via the existing `SetOnSyncCompleted` callback in `internal/github/sync.go`. The callback receives repo keys as `owner/name` strings, so detection must resolve these to `repo_id` values before querying.

For each synced repo:

1. Load all non-closed PRs for the repo (filter: `repo_id = ? AND state IN ('open', 'merged')`). Merged PRs are included so stacks with partial merges are detected correctly; closed (unmerged) PRs are excluded.
2. Build a map: `head_branch → PR` (scoped to this repo — no cross-repo matching). For duplicate head branches, keep the PR with the lowest number.
3. Build a reverse map: `base_branch → []PR` (all PRs targeting each base). For each PR whose `base_branch` is NOT in the head_branch map (i.e. it targets main or a non-PR branch), this PR is a potential stack base. Walk the chain upward using a visited set to guard against cycles. At each step, find PRs whose `base_branch` matches the current PR's `head_branch`. If multiple children exist, continue the chain with the lowest PR number only — other children at the fork point are excluded from all stacks (maintains one-stack-per-PR invariant). If a branch is encountered twice, stop (cycle — skip that chain).
4. Each chain of length >= 2 forms a stack.
5. Assign positions: base = 1, incrementing toward the tip.
6. Diff against existing `middleman_stacks`/`middleman_stack_members` for this repo: create new stacks, update changed ones (members added/removed/reordered), delete stacks with no remaining open members.
7. Derive stack name from common branch prefix.

### Edge Cases

- **Branching from non-default branches**: A PR targeting a long-lived branch (e.g. `release-1.x`) that isn't another PR's head branch is treated as a stack base, same as one targeting `main`.
- **Duplicate head branches**: If two open PRs in the same repo have the same `head_branch` (unusual but possible with force-push races), the one with the lowest PR number is used in chain building. The other is ignored.
- **Single-PR "stacks"**: A chain of length 1 is not a stack. PRs must have at least one dependency to appear in the Stacks view.
- **Branching graphs**: If multiple PRs share the same `base_branch` (e.g. two PRs both based on `feature/auth`), the chain continues with the lowest-numbered child only. Other children at the fork point are not included in any stack. This maintains a one-stack-per-PR invariant — each PR has at most one `stack_id`, and `GetStackForPR` always returns a single result. The position model stays linear.
- **Cycles**: If branch references form a loop (A → B → A), the visited set in the chain walk detects this and skips the chain. No stack is created from cyclic references.

### Stack Lifecycle

- Created when detection finds a chain of >= 2 PRs (open or merged) with at least one open member.
- Updated each sync cycle (members may change as PRs merge/close/open). Merged PRs remain as members while the stack has open descendants.
- Deleted when all member PRs are merged or closed (no manual cleanup).

## API

All endpoints registered via Huma in `internal/server/huma_routes.go`, following existing patterns.

### New Types

```go
// internal/server/api_types.go

type stackMemberResponse struct {
    Number         int    `json:"number"`
    Title          string `json:"title"`
    State          string `json:"state"`
    CIStatus       string `json:"ci_status"`
    ReviewDecision string `json:"review_decision"`
    Position       int    `json:"position"`
    IsDraft        bool   `json:"is_draft"`
    BlockedBy      *int   `json:"blocked_by"` // PR number of root blocker, null if not blocked
}

type stackResponse struct {
    ID        int64                 `json:"id"`
    Name      string                `json:"name"`
    RepoOwner string                `json:"repo_owner"`
    RepoName  string                `json:"repo_name"`
    Health    string                `json:"health"` // "all_green", "base_ready", "blocked", "in_progress", "partial_merge"
    Members   []stackMemberResponse `json:"members"` // ordered by position, base first
}

type stackContextResponse struct {
    StackID       int64                 `json:"stack_id"`
    StackName     string                `json:"stack_name"`
    Position      int                   `json:"position"`
    Size          int                   `json:"size"`
    Health        string                `json:"health"`
    Members       []stackMemberResponse `json:"members"`
}
```

Note: `stackResponse.ID` and `stackContextResponse.StackID` use `int64` to match the DB PK convention (`Repo.ID`, `MergeRequest.ID`, etc. are all `int64`).

### New Endpoints

```
GET /api/v1/stacks
```

Returns `[]stackResponse`. Each stack carries `repo_owner`/`repo_name` — grouping by repo is a frontend concern, not the API's.

Query params:
- `repo` — filter by `owner/name` (optional)

```
GET /api/v1/repos/{owner}/{name}/pulls/{number}/stack
```

Returns `*stackContextResponse` — full stack context for a specific PR. Returns 404 if PR is not part of a stack (not an error — frontend checks presence). This endpoint feeds the PR detail sidebar.

### Modified Endpoints

```
GET /api/v1/pulls
```

Add nullable stack fields directly to `mergeRequestResponse` (not to `db.MergeRequest` — stack info is a presentation concern, not a model concern):

```go
// Added to mergeRequestResponse (alongside existing RepoOwner, RepoName, WorktreeLinks)
StackID       *int64  `json:"stack_id"`
StackName     *string `json:"stack_name"`
StackPosition *int    `json:"stack_position"`
StackSize     *int    `json:"stack_size"`
```

These fields are `null` for PRs not in any stack. Frontend uses presence of `stack_id` to decide whether to show stack indicators.

**Implementation note:** `ListMergeRequests` in `internal/db/queries.go` builds its SELECT dynamically and currently returns `[]db.MergeRequest`. To carry stack fields without polluting the model, `ListMergeRequests` returns a new `MergeRequestRow` struct that embeds `MergeRequest` and adds nullable stack fields (`StackID *int64`, `StackName *string`, `StackPosition *int`, `StackSize *int`). The query adds a LEFT JOIN on `middleman_stack_members sm ON sm.merge_request_id = p.id` + `middleman_stacks st ON st.id = sm.stack_id`, selects `st.id, st.name, sm.position`, and uses a subquery or window function for stack size. The handler maps `MergeRequestRow` to `mergeRequestResponse`.

## Frontend

### Routing and Store Integration

Add `"stacks"` to the `Route` union in `frontend/src/lib/stores/router.svelte.ts`:

```typescript
| { page: "stacks" }
```

Add `"stacks"` to the `NavigateEvent.route.page` union in `packages/ui/src/types.ts`.

Add a new `StacksStore` in `packages/ui/src/stores/stacks.svelte.ts` following the existing store pattern (factory function, reactive state via Svelte 5 runes). The store:

- Accepts `getGlobalRepo?: () => string | undefined` option (same pattern as pulls/issues/activity stores). When set, passes the value as the `repo` query param to `GET /api/v1/stacks`.
- Exposes grouped-by-repo stack data.
- Exposes a `loadStacks()` method for manual refresh.

The Stacks view component subscribes to `sync.subscribeSyncComplete()` to reload stacks after each sync cycle completes — same pattern as `ActivityFeed` and `PullList`.

Export `StacksStore` from `packages/ui/src/types.ts` and add it to `StoreInstances`.

### Stacks View

New top-level view in the navigation bar (alongside PRs, Kanban, Activity).

**Layout:**
- Header showing total stack count across repos
- Collapsible repo section headers (repo name + stack count badge)
- Stack cards within each repo section

**Stack card (collapsed):**
- Muted chevron (`›`) — rotates 90° when expanded
- Stack name
- PR count
- Inline colored health dots (one per PR, colored by status)
- Aggregate health badge (right-aligned pill)

**Stack card (expanded):**
- Chevron rotated
- Stack name + PR count + health badge (same header row)
- Mini vertical chain below: small colored dots connected by lines, each row showing clickable PR number + short title

**Dot colors:**
- Green (#238636): CI passing + review approved
- Red (#f85149): CI failing
- Yellow/orange (#d29922): CI pending or changes requested
- Gray (#8b949e): merged
- Gray outline (#21262d border): no CI/review data yet
- Dimmed (opacity 0.5): blocked by ancestor

**Health badge colors and text:**
- Green: "All green" or "Base ready"
- Yellow: "N/M merged" (N and M derived from member states) or "In progress"
- Red: "Blocked"

**Empty states** (Stacks view checks the existing repos store to distinguish these — same pattern as PullList/IssueList which use repo availability from their parent context):
- No repos configured: "No repositories configured. Add repos in config to detect stacks."
- Repos exist but no stacks: "No stacks detected."
- Loading: standard loading indicator while fetching

**Interactions:**
- Click repo header → collapse/expand repo section
- Click stack chevron/name → collapse/expand chain
- Click PR number → navigate to PR detail view

### PR Detail Sidebar

When a PR belongs to a stack, a 200px right-rail sidebar appears in the PR detail view.

**Contents:**
- Header: "STACK · {name}" in purple (#a371f7), uppercase
- Full vertical chain with larger dots and connecting lines
- Each PR shows: clickable number + short title, CI status, review status
- Current PR highlighted with purple left border + "You are here" label
- Base PR shows "Ready to merge → main" when applicable
- Blocked descendants show "blocked by #N" in red italic
- Footer: "View full stack →" link navigating to Stacks view

**Chain dot sizing:**
- Current PR: 10px filled purple dot
- Other open PRs: 8px dots colored by health
- Merged PRs: 8px gray dots

**Data source:**
- Frontend calls `GET /api/v1/repos/{owner}/{name}/pulls/{number}/stack` when loading PR detail
- If 404, no sidebar rendered
- Response provides all members with health data — no additional calls needed

**Behavior:**
- Sidebar only appears for PRs that are stack members
- Clicking a sibling PR navigates to that PR's detail (sidebar updates to reflect new "you are here")
- "View full stack →" navigates to Stacks view with the relevant repo expanded

### OpenAPI and Generated Client

After adding new endpoints and modifying response types, run `make api-generate` to regenerate:
- `frontend/openapi/openapi.json` (OpenAPI 3.1 spec)
- `internal/apiclient/spec/openapi.json` (OpenAPI 3.0 for Go client)
- `frontend/src/lib/api/generated/schema.ts` (TypeScript types)
- `internal/apiclient/generated/client.gen.go` (Go API client)

The frontend StacksStore and any integration tests depend on these generated artifacts.

## Platform Agnosticism

All stack detection logic operates on `base_branch`/`head_branch` fields that are already populated by the sync engine. No GitHub-specific API calls are needed for detection.

The stack data model uses `repo_id` and `merge_request_id` foreign keys — no platform-specific identifiers. This design works for any future platform that provides equivalent branch metadata on PRs/MRs.

**Forking workflows:** Branch-chain detection does not support fork-based stacking (fork PRs all target `main`, producing no chain). Commit ancestry detection was explored thoroughly (including spelunking GitHub CLI internals and the GitHub API) and ruled out — GitHub exposes no PR dependency metadata or merge base SHA, so detection would require expensive compare API calls that break on rebase. Fork stacking is rare in practice; all major stacking tools (Graphite, ghstack, git-town, Sapling) push to origin.

## Mockups

Visual mockups for both surfaces are available in `.superpowers/brainstorm/` (the session that produced this spec). Key files:
- `final-complete.html` — composite showing both Stacks view and PR detail sidebar
- `stacks-clickable-v2.html` — chevron affordance exploration
- `pr-detail-stack-sidebar.html` — sidebar placement options explored
