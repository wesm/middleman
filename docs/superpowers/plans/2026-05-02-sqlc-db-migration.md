# sqlc DB Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move all static `internal/db` SQLite statements that sqlc can represent into checked-in sqlc query files and generated Go code.

**Architecture:** Keep `internal/db.DB` as the stable facade. Generate an internal `internal/db/sqlc` package from a migration-derived schema snapshot and domain-split query files, then update DB methods to delegate to generated calls while preserving timestamp normalization, domain structs, and dynamic query composition.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, `database/sql`, sqlc v2 config, Makefile generation targets, existing DB/server/github test suites.

---

### Task 1: Add sqlc Tooling And Schema Snapshot

**Files:**
- Modify: `go.mod`
- Modify: `Makefile`
- Create: `sqlc.yaml`
- Create: `internal/db/sqlc/schema.sql`
- Create: `internal/db/sqlc/queries/repos.sql`

- [ ] Add `tool github.com/sqlc-dev/sqlc/cmd/sqlc` to `go.mod`.
- [ ] Add `db-schema-generate` and `sqlc-generate` Makefile targets.
- [ ] Create `sqlc.yaml` with SQLite engine, schema `internal/db/sqlc/schema.sql`, queries `internal/db/sqlc/queries`, package `sqlc`, and output `internal/db/sqlc`.
- [ ] Generate `internal/db/sqlc/schema.sql` by concatenating ordered `internal/db/migrations/*.up.sql` files.
- [ ] Add initial repo queries in `repos.sql`: upsert repo, list repos, get repo by owner/name, get repo by ID, get repo by host/owner/name, update sync timestamps, update settings, update backfill cursor, purge other hosts.
- [ ] Run `go tool sqlc generate` and commit generated files.
- [ ] Run `go test ./internal/db -run TestUpsertAndListRepos -shuffle=on` to verify the generated package compiles with DB tests.

### Task 2: Wire Generated Repo Queries Behind DB Facade

**Files:**
- Modify: `internal/db/db.go`
- Modify: `internal/db/queries.go`
- Test: `internal/db/queries_test.go`

- [ ] Write or update a DB test that exercises repo create/list/get/update behavior through existing `*DB` methods.
- [ ] Verify the test fails if repo methods are temporarily pointed at an unimplemented generated wrapper.
- [ ] Add read/write sqlc query handles to `DB`.
- [ ] Refactor repo-related methods in `queries.go` to use generated repo queries.
- [ ] Preserve canonical repo identifier handling and UTC timestamp handling.
- [ ] Run `go test ./internal/db -run 'Test(UpsertAndListRepos|GetRepoByOwnerName|UpdateRepoSync|PurgeOtherHosts|GetRepoByHostOwnerName)' -shuffle=on`.

### Task 3: Migrate Labels, PRs, PR Events, Kanban, And Starred Queries

**Files:**
- Modify: `internal/db/sqlc/queries/pulls.sql`
- Modify: `internal/db/sqlc/queries/labels.sql`
- Modify: `internal/db/queries.go`
- Test: `internal/db/queries_test.go`

- [ ] Add sqlc queries for label lookups/upserts/association moves, PR upsert/get/list support queries, PR event upserts/deletes/lists, kanban CRUD, PR state/detail/CI/SHA updates, and starred item CRUD.
- [ ] Generate sqlc code.
- [ ] Refactor the corresponding `DB` methods to use generated static queries while keeping dynamic list filters in Go where needed.
- [ ] Preserve label attachment and wrong-repo validation behavior.
- [ ] Run `go test ./internal/db -run 'Test(UpsertAndGetPullRequest|ListPullRequests|PullRequestRepoScopedQueriesCanonicalizeOwnerName|ReplaceMergeRequestLabels|PREvents|KanbanState|UpdatePRState|UpdateMRTitleBody|RateLimitCRUD)' -shuffle=on`.

### Task 4: Migrate Issues And Issue Events

**Files:**
- Modify: `internal/db/sqlc/queries/issues.sql`
- Modify: `internal/db/queries.go`
- Test: `internal/db/queries_test.go`

- [ ] Add sqlc queries for issue upsert/get/list support, issue state/detail updates, issue events, issue comment deletion/existence checks, and issue number resolution.
- [ ] Generate sqlc code.
- [ ] Refactor issue-related `DB` methods to use generated static queries.
- [ ] Preserve issue label attachment and repo-scoped label behavior.
- [ ] Run `go test ./internal/db -run 'Test(Issue|ListIssues|ResolveItemNumber|ReplaceIssueLabels)' -shuffle=on`.

### Task 5: Migrate Worktree And Workspace Queries

**Files:**
- Modify: `internal/db/sqlc/queries/workspaces.sql`
- Modify: `internal/db/queries.go`
- Test: `internal/db/queries_test.go`

- [ ] Add sqlc queries for worktree links, workspace CRUD, workspace status/branch/retry updates, associated PR update, setup events, tmux sessions, and workspace summaries.
- [ ] Generate sqlc code.
- [ ] Refactor workspace-related `DB` methods to use generated static queries.
- [ ] Preserve concurrency-sensitive `StartWorkspaceRetry` affected-row behavior.
- [ ] Run `go test ./internal/db -run 'Test(Worktree|Workspace|SetWorkspaceAssociatedPRNumberIfNull)' -shuffle=on`.
- [ ] Run `go test ./internal/server -run 'Test.*Workspace|Test.*Tmux' -shuffle=on`.

### Task 6: Migrate Stacks, Activity, Repo Summaries, Rate Limits, And Autocomplete

**Files:**
- Modify: `internal/db/sqlc/queries/stacks.sql`
- Modify: `internal/db/sqlc/queries/activity.sql`
- Modify: `internal/db/sqlc/queries/repo_summaries.sql`
- Modify: `internal/db/queries_stacks.go`
- Modify: `internal/db/queries_activity.go`
- Modify: `internal/db/queries_repo_summaries.go`
- Modify: `internal/db/queries.go`
- Test: `internal/db/queries_stacks_test.go`
- Test: `internal/db/queries_activity_test.go`
- Test: `internal/db/queries_repo_summaries_test.go`

- [ ] Add sqlc queries for stack detection/member listing, activity feed fixed branches, repo summary stats/overview child rows, rate limit CRUD, and autocomplete result queries.
- [ ] Generate sqlc code.
- [ ] Refactor the corresponding methods while keeping dynamic pagination/filter composition in Go where sqlc cannot express optional branches cleanly.
- [ ] Preserve cursor parsing and UTC conversion in activity rows.
- [ ] Run `go test ./internal/db -run 'Test(ListActivity|RepoSummaries|Stack|RateLimit|Autocomplete)' -shuffle=on`.

### Task 7: Add Staleness Guard And Final Verification

**Files:**
- Modify: `Makefile`
- Create or modify: `internal/db/sqlc_generated_test.go`
- Modify generated files as produced by sqlc.

- [ ] Add a test or guardrail that runs sqlc generation into a temp copy or checks `go tool sqlc generate` leaves no diff.
- [ ] Run `go tool sqlc generate`.
- [ ] Run `go test ./internal/db -shuffle=on`.
- [ ] Run `go test ./internal/server -shuffle=on`.
- [ ] Run `go test ./internal/github -shuffle=on`.
- [ ] Run `go test ./... -short -shuffle=on`.
- [ ] Commit with a conventional message explaining that DB query code now comes from sqlc.

## Self-Review

The plan covers the full design scope by domain and keeps dynamic query construction explicitly limited to cases sqlc cannot represent without excessive duplication. It preserves the DB facade, runtime migrations, UTC handling, and existing integration-style DB tests. There are no placeholder tasks; each task names the files, generation step, and focused verification command.
