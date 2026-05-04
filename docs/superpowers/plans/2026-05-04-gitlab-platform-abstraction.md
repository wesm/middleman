# GitLab Platform Abstraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add read-oriented GitLab repository and merge request sync while moving the current GitHub implementation behind a platform abstraction that can accept future providers.

**Architecture:** Introduce a provider-neutral `internal/platform` domain and split provider transport from sync orchestration. GitHub remains behaviorally unchanged through a GitHub adapter; GitLab starts with repository discovery, open merge request sync, issues, comments, labels, basic CI pipeline status, releases/tags, and diff clone support. Provider-specific actions such as workflow approval, ready-for-review, merge, and comment mutation are exposed through capability flags so unsupported platforms degrade explicitly instead of pretending to be GitHub.

**Tech Stack:** Go, SQLite migrations, Huma/OpenAPI, go-github/v84, GitLab API v4 via `gitlab.com/gitlab-org/api/client-go`, Svelte 5 generated API clients, Bun, full-stack Go API tests with real SQLite.

---

## Documentation Basis

- GitLab API v4 Projects API supports project metadata and uses `path_with_namespace`, `web_url`, visibility, archived state, and repository properties.
- GitLab API v4 Merge Requests API lists project merge requests with project-scoped `iid`, global `id`, branches, author, labels, draft/work-in-progress flags, merge status, timestamps, and `web_url`.
- GitLab API v4 accepts personal/project/group access tokens through token headers such as `PRIVATE-TOKEN`; the Go client also supports custom base URLs for self-hosted instances.
- Context7 reports the old `github.com/xanzy/go-gitlab` client as moved to `gitlab.com/gitlab-org/api/client-go`; use the moved GitLab-owned module rather than adding the deprecated import path.

## Scope

### First GitLab Milestone

- Exact configured GitLab repos.
- GitLab repo import/preview from a group/user namespace.
- GitLab open merge request sync into existing `middleman_merge_requests`.
- GitLab issues sync into existing `middleman_issues`.
- GitLab MR and issue comments, commit events, labels, timestamps, clone URLs, releases/tags, and basic pipeline-derived CI status when exposed by the MR/list/detail APIs.
- Existing GitHub behavior, API shape, and config files keep working.

### Deliberate Follow-Ups

- GitLab mutations: comments, close/reopen, approvals, merge, ready-for-review equivalents.
- GitLab review/discussion parity beyond comments and commits.
- GitLab project glob import across all accessible token associations.
- New UI vocabulary that renames "pull request" to a neutral "change request" everywhere.

This split keeps the first release useful and testable without dragging every platform-specific action into the abstraction at once.

## File Structure

- Create `internal/platform/types.go`: provider-neutral repository, merge request, issue, event, label, release, tag, CI, identity, capability, and state types.
- Create `internal/platform/client.go`: small interfaces for provider capabilities: repository discovery, change request read sync, issue read sync, comment read sync, release/tag sync, CI sync, and optional mutation actions.
- Create `internal/platform/registry.go`: keyed provider registry by `(platform, host)` with client lookup, capability lookup, and host display helpers.
- Create `internal/platform/github/adapter.go`: wraps existing GitHub client data into neutral `platform` types.
- Create `internal/platform/github/normalize.go`: move GitHub normalization out of `internal/github/normalize.go` behind the neutral output shape.
- Create `internal/platform/gitlab/client.go`: GitLab API client construction, auth, base URL handling, pagination, and rate tracking.
- Create `internal/platform/gitlab/normalize.go`: map GitLab projects, MRs, issues, comments, labels, releases, tags, commits, and pipeline status into neutral types.
- Create `internal/platform/gitlab/client_test.go`: httptest coverage for exact project lookup, namespace listing, MR pagination, issue pagination, comments, labels, and pipeline mapping.
- Create `internal/platform/gitlab/normalize_test.go`: unit coverage for state, draft, branch, author, label, timestamp, URL, and CI status normalization.
- Modify `internal/github/client.go`: keep the live GitHub transport initially, but prepare it to be consumed by `internal/platform/github`; do not add GitLab paths here.
- Modify `internal/github/sync.go` or move to `internal/syncer/sync.go`: replace direct `github.Client` calls with `platform.Client` capability interfaces.
- Modify `internal/github/repo_config_resolver.go` or move to `internal/platform/config_resolver.go`: resolve configured repos through provider repository discovery.
- Modify `internal/db/types.go`: add explicit `RepoIdentity` and preserve `db.MergeRequest` as the local change request row.
- Modify `internal/db/queries.go`: replace hard-coded GitHub repo upserts with platform-aware upserts.
- Create migration `internal/db/migrations/000017_platform_repo_identity.up.sql`: add stable provider repo fields needed for GitLab nested namespaces and future providers.
- Create migration `internal/db/migrations/000017_platform_repo_identity.down.sql`.
- Modify `internal/config/config.go`: add explicit `platform`, `platform_host`, and platform token config while preserving `github_token_env` and old repo entries.
- Modify `cmd/middleman/main.go`, `middleman.go`, and `cmd/e2e-server/main.go`: build a provider registry instead of `map[string]ghclient.Client`.
- Modify `internal/server/repo_import_handlers.go`: accept platform/host in preview and bulk add requests.
- Modify `internal/server/settings_handlers.go`: expose configured repo statuses with platform and host.
- Modify `internal/server/api_types.go` and `internal/server/huma_routes.go`: add platform/host fields to PR/MR detail responses and gate unsupported actions by capability.
- Regenerate `frontend/openapi/openapi.yaml`, `internal/apiclient/generated/client.gen.go`, `packages/ui/src/api/generated/*`, and `frontend/src/lib/api/generated/*` with `make api-generate`.
- Modify `packages/ui/src/api/types.ts` and affected stores/components only where generated types require platform/host threading.
- Modify `context/github-sync-invariants.md`: generalize identity rules to platform sync invariants and leave a GitHub-specific section for GraphQL-only behavior.
- Add `context/platform-sync-invariants.md`: provider identity, capability, normalization, and test rules.
- Add `docs/config/gitlab.md` or update `README.md`: GitLab config examples and supported/unsupported features.

## Core Design Decisions

### Identity

Use `(platform, platform_host, owner, name)` as the human-readable identity and add a stable provider repository id for platforms that expose one.

```go
package platform

type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

type RepoRef struct {
	Platform       Kind
	Host           string
	Owner          string
	Name           string
	PlatformRepoID string
	WebURL         string
	CloneURL       string
	DefaultBranch  string
}

func (r RepoRef) DisplayName() string {
	return r.Owner + "/" + r.Name
}
```

For GitHub, `Owner` remains the owner/org login and `Name` remains the repository name. For GitLab, `Owner` is the full namespace path without the project slug, so `group/subgroup/project` is stored as `Owner: "group/subgroup", Name: "project"`. The GitLab project `id` is stored as `PlatformRepoID` so sync can survive namespace path changes later.

### Config

Support new platform-aware config without breaking the old file.

```toml
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"

[[platforms]]
type = "gitlab"
host = "gitlab.com"
token_env = "MIDDLEMAN_GITLAB_TOKEN"

[[repos]]
platform = "gitlab"
platform_host = "gitlab.com"
owner = "my-group/subgroup"
name = "my-project"
```

Rules:

- Existing repos with no `platform` remain GitHub.
- Existing `platform_host` remains valid and means a GitHub host unless `platform = "gitlab"` is set.
- `github_token_env` remains the GitHub fallback.
- GitLab has no implicit `gh auth token` fallback; GitLab tokens must come from the selected env var.
- Duplicate config detection uses `(platform, platform_host, owner, name)`.

### Provider Interfaces

Keep interfaces narrow enough that future providers can implement a useful subset.

```go
package platform

type Capabilities struct {
	ReadRepositories bool
	ReadMergeRequests bool
	ReadIssues bool
	ReadComments bool
	ReadReleases bool
	ReadCI bool
	CommentMutation bool
	StateMutation bool
	MergeMutation bool
	WorkflowApproval bool
	ReadyForReview bool
}

type RepositoryReader interface {
	GetRepository(ctx context.Context, ref RepoRef) (Repository, error)
	ListRepositories(ctx context.Context, owner string, opts RepositoryListOptions) ([]Repository, error)
}

type MergeRequestReader interface {
	ListOpenMergeRequests(ctx context.Context, ref RepoRef) ([]MergeRequest, error)
	GetMergeRequest(ctx context.Context, ref RepoRef, number int) (MergeRequest, error)
	ListMergeRequestEvents(ctx context.Context, ref RepoRef, number int) ([]MergeRequestEvent, error)
}

type IssueReader interface {
	ListOpenIssues(ctx context.Context, ref RepoRef) ([]Issue, error)
	GetIssue(ctx context.Context, ref RepoRef, number int) (Issue, error)
	ListIssueEvents(ctx context.Context, ref RepoRef, number int) ([]IssueEvent, error)
}

type ProviderClient interface {
	Platform() Kind
	Host() string
	Capabilities() Capabilities
	RepositoryReader
	MergeRequestReader
	IssueReader
}
```

GitHub can implement extra mutation interfaces. GitLab first implements the read interfaces and advertises mutation capabilities as false.

### GitLab Mapping

Map GitLab fields into middleman rows as follows:

- `projects.id` -> `middleman_repos.platform_repo_id`.
- `projects.path_with_namespace` -> `owner/name`, with the last path segment as `name`.
- `projects.http_url_to_repo` -> repo clone URL when available.
- `merge_requests.id` -> `middleman_merge_requests.platform_id`.
- `merge_requests.iid` -> `middleman_merge_requests.number`.
- `merge_requests.web_url` -> `url`.
- `merge_requests.author.username` -> `author`; `author.name` -> `author_display_name`.
- `merge_requests.state` -> `open`, `closed`, or `merged`.
- `merge_requests.draft || work_in_progress` -> `is_draft`.
- `merge_requests.source_branch` -> `head_branch`; `target_branch` -> `base_branch`.
- `merge_requests.sha` -> `platform_head_sha` when present.
- `merge_requests.diff_refs.base_sha` or target branch commit -> `platform_base_sha` when present.
- `merge_requests.merge_status` / `detailed_merge_status` -> `mergeable_state`.
- `head_pipeline.status` -> `ci_status` and one synthesized `db.CICheck` named `GitLab Pipeline`.
- GitLab labels become `db.Label` rows. If GitLab returns only names for an endpoint, store color as empty and update richer label fields when the project labels endpoint is available.

## Implementation Tasks

### Task 1: Add Neutral Platform Types

**Files:**
- Create: `internal/platform/types.go`
- Create: `internal/platform/client.go`
- Create: `internal/platform/registry.go`
- Test: `internal/platform/registry_test.go`

- [ ] Write tests for registry lookup by `(platform, host)`, missing client errors, and capability lookup.
- [ ] Add `Kind`, `RepoRef`, `Repository`, `MergeRequest`, `Issue`, `Label`, `MergeRequestEvent`, `IssueEvent`, `Release`, `Tag`, `CICheck`, and `Capabilities`.
- [ ] Add narrow reader and optional mutation interfaces.
- [ ] Run `go test ./internal/platform -shuffle=on`.
- [ ] Commit: `feat: define provider-neutral platform contracts`.

### Task 2: Make Repository Storage Platform-Aware

**Files:**
- Modify: `internal/db/types.go`
- Modify: `internal/db/queries.go`
- Create: `internal/db/migrations/000017_platform_repo_identity.up.sql`
- Create: `internal/db/migrations/000017_platform_repo_identity.down.sql`
- Test: `internal/db/queries_test.go`

- [ ] Add migration:

```sql
ALTER TABLE middleman_repos ADD COLUMN platform_repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN web_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN clone_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN default_branch TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_repos_platform_repo_id
    ON middleman_repos(platform, platform_host, platform_repo_id)
    WHERE platform_repo_id <> '';
```

- [ ] Replace `UpsertRepo(ctx, host, owner, name)` with `UpsertRepo(ctx, identity db.RepoIdentity)`.
- [ ] Keep a small compatibility helper for GitHub-heavy tests:

```go
func GitHubRepoIdentity(host, owner, name string) RepoIdentity {
	return RepoIdentity{Platform: "github", PlatformHost: host, Owner: owner, Name: name}
}
```

- [ ] Update queries to select and scan new repo fields.
- [ ] Add tests that GitHub upserts still use `platform = 'github'`, GitLab upserts use `platform = 'gitlab'`, and the same host/owner/name on different platforms creates different rows.
- [ ] Run `go test ./internal/db -shuffle=on`.
- [ ] Commit: `feat: persist provider-aware repository identity`.

### Task 3: Generalize Config And Token Resolution

**Files:**
- Modify: `internal/config/config.go`
- Modify: `config.example.toml`
- Test: `internal/config/config_test.go`

- [ ] Add `Platform` to `config.Repo` and `PlatformConfig` to `config.Config`.
- [ ] Normalize repo platform to `github` when omitted.
- [ ] Keep `GitHubToken()` and add `TokenForPlatformHost(platform, host, repoTokenEnv string)`.
- [ ] Teach URL parsing to accept GitLab HTTP/SSH URLs, including nested namespace paths.
- [ ] Reject ambiguous GitLab URLs with fewer than two path segments.
- [ ] Add tests for old GitHub config, GitHub Enterprise config, GitLab env token config, nested GitLab namespace config, duplicate detection across platform/host, and token env conflicts scoped to `(platform, host)`.
- [ ] Run `go test ./internal/config -shuffle=on`.
- [ ] Commit: `feat: configure repositories by platform`.

### Task 4: Wrap GitHub In The Platform Adapter

**Files:**
- Create: `internal/platform/github/adapter.go`
- Create: `internal/platform/github/normalize.go`
- Test: `internal/platform/github/normalize_test.go`
- Modify: `internal/github/normalize.go`
- Modify: `internal/github/sync_test.go`

- [ ] Move GitHub normalization logic to return `platform.MergeRequest`, `platform.Issue`, and event types.
- [ ] Keep compatibility functions `NormalizePR`, `NormalizeIssue`, and event normalizers until the syncer is migrated; they should call the adapter and convert to DB rows.
- [ ] Add adapter tests proving GitHub PR number/id/state/draft/branches/labels/CI events match current persisted DB behavior.
- [ ] Run `go test ./internal/github ./internal/platform/github -shuffle=on`.
- [ ] Commit: `refactor: adapt GitHub data through platform models`.

### Task 5: Convert Syncer To Provider Clients

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/github/queue.go`
- Modify: `internal/github/repo_config_resolver.go`
- Test: `internal/github/sync_test.go`
- Test: `internal/github/repo_config_resolver_test.go`

- [ ] Change `Syncer.clients` from `map[string]Client` to a platform registry keyed by `(platform, host)`.
- [ ] Change `RepoRef` to carry `Platform`, `Host`, `Owner`, `Name`, and `PlatformRepoID`.
- [ ] Update `syncRepo`, `indexSyncRepo`, `SyncMR`, `SyncIssue`, detail queue, backfill, and watched MR paths to pass a full `RepoRef`.
- [ ] Preserve GitHub GraphQL bulk fetch as a GitHub-only optimization behind an optional `BulkMergeRequestFetcher` interface.
- [ ] Preserve ETag invalidation as an optional conditional-list capability. GitLab can no-op initially.
- [ ] Use provider-supplied clone URLs for diff sync instead of formatting `https://host/owner/name.git`.
- [ ] Add tests proving duplicate owner/name on GitHub and GitLab do not cross-sync, GitHub behavior is unchanged, and unsupported provider capabilities do not panic.
- [ ] Run `go test ./internal/github -shuffle=on`.
- [ ] Commit: `refactor: sync repositories through platform clients`.

### Task 6: Add GitLab Client And Normalization

**Files:**
- Create: `internal/platform/gitlab/client.go`
- Create: `internal/platform/gitlab/normalize.go`
- Create: `internal/platform/gitlab/client_test.go`
- Create: `internal/platform/gitlab/normalize_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] Add `gitlab.com/gitlab-org/api/client-go` with `go get gitlab.com/gitlab-org/api/client-go`.
- [ ] Build clients with `gitlab.NewClient(token, gitlab.WithBaseURL("https://<host>/api/v4"))`.
- [ ] Implement exact project lookup by URL-escaped `path_with_namespace`.
- [ ] Implement namespace listing for repo preview. For groups, use group project listing with subgroups when available; for users, fall back to project search/listing filtered by namespace path.
- [ ] Implement MR list/detail pagination with project `id` when known and encoded `path_with_namespace` when not.
- [ ] Implement issues, comments, commits, releases, tags, and basic pipeline status reads.
- [ ] Normalize GitLab `iid` as middleman `number`; never use the global MR id as `number`.
- [ ] Add httptest fixtures for paginated GitLab responses and self-hosted base URL construction.
- [ ] Run `go test ./internal/platform/gitlab -shuffle=on`.
- [ ] Commit: `feat: add GitLab read client`.

### Task 7: Wire Runtime Client Construction

**Files:**
- Modify: `cmd/middleman/main.go`
- Modify: `middleman.go`
- Modify: `cmd/e2e-server/main.go`
- Test: `cmd/middleman/main_test.go`
- Test: `middleman_test.go`

- [ ] Build per-platform host token maps from config.
- [ ] Construct GitHub adapters for GitHub platform entries.
- [ ] Construct GitLab clients for GitLab platform entries.
- [ ] Seed public GitHub as before so existing settings import works with old configs.
- [ ] Stop failing startup when no GitHub token exists but only GitLab repos are configured.
- [ ] Preserve the existing "GitHub token missing" error for configs that still require GitHub.
- [ ] Run `go test ./cmd/middleman . -shuffle=on`.
- [ ] Commit: `feat: wire platform registry at startup`.

### Task 8: Platform-Aware Repository Import

**Files:**
- Modify: `internal/server/repo_import_handlers.go`
- Modify: `internal/server/settings_handlers.go`
- Modify: `internal/server/api_types.go`
- Test: `internal/server/settings_e2e_test.go`
- Test: `internal/server/api_test.go`

- [ ] Extend preview request:

```json
{
  "platform": "gitlab",
  "platform_host": "gitlab.com",
  "owner": "my-group/subgroup",
  "pattern": "service-*"
}
```

- [ ] Default omitted `platform` to `github` and omitted `platform_host` to the platform default.
- [ ] Return platform and host on preview rows.
- [ ] Update bulk add request rows to include platform and host.
- [ ] Validate exact rows through the matching provider registry client.
- [ ] Keep old GitHub-only requests working.
- [ ] Add full-stack e2e coverage using a fake GitLab API server and real SQLite: preview GitLab repos, add one exact repo, trigger syncer tracked repo update.
- [ ] Run `go test ./internal/server -run 'Test.*Repo.*' -shuffle=on`.
- [ ] Commit: `feat: import repositories from configured platforms`.

### Task 9: Gate Provider-Specific Actions

**Files:**
- Modify: `internal/server/huma_routes.go`
- Modify: `internal/server/api_types.go`
- Modify: `packages/ui/src/api/types.ts`
- Modify: PR/MR action components that call GitHub-only routes
- Test: `internal/server/api_test.go`
- Test: affected frontend component tests

- [ ] Add capabilities to repository or item detail responses.
- [ ] Return `409 Conflict` or `501 Not Implemented` for mutation routes when the provider lacks the capability, with a stable machine-readable error code.
- [ ] Hide or disable unsupported action buttons in the frontend for GitLab rows.
- [ ] Keep GitHub action behavior unchanged.
- [ ] Run `make api-generate`.
- [ ] Run `go test ./internal/server -shuffle=on`.
- [ ] Run `bun test` in affected frontend/package workspaces according to existing package scripts.
- [ ] Commit: `feat: expose platform action capabilities`.

### Task 10: GitLab Sync E2E

**Files:**
- Create or modify: `internal/server/gitlab_sync_e2e_test.go`
- Modify: `internal/testutil/fixture_client.go` only if shared fixture support is useful
- Test: `internal/server/gitlab_sync_e2e_test.go`

- [ ] Start a fake GitLab API server with endpoints for project lookup, MRs, issues, comments, releases, tags, commits, and pipeline status.
- [ ] Configure middleman with one GitLab repo and token env var.
- [ ] Run the syncer once against real SQLite.
- [ ] Assert repo row has `platform = 'gitlab'`, GitLab host, namespace path, project id, web URL, clone URL, and default branch.
- [ ] Assert MR row has `number = iid`, `platform_id = id`, draft/state/branches/labels/CI status populated.
- [ ] Assert issue row and comments/events are populated.
- [ ] Assert GitHub rows remain absent.
- [ ] Run `go test ./internal/server -run TestGitLabSync -shuffle=on`.
- [ ] Commit: `test: cover GitLab repository sync end to end`.

### Task 11: Documentation And Invariants

**Files:**
- Create: `context/platform-sync-invariants.md`
- Modify: `context/github-sync-invariants.md`
- Modify: `README.md`
- Modify: `config.example.toml`

- [ ] Document identity as `(platform, platform_host, owner, name)`.
- [ ] Document GitLab nested namespace handling.
- [ ] Document supported GitLab features and unsupported mutation actions.
- [ ] Document token env configuration for GitLab and self-hosted GitLab.
- [ ] Run `go test ./... -shuffle=on`.
- [ ] Commit: `docs: describe platform sync invariants`.

## Verification

Run these before declaring the implementation complete:

```bash
go test ./... -shuffle=on
make api-generate
make frontend
make test-short
```

If frontend action gating or route behavior changes are visible, also run the relevant Playwright e2e tests rather than relying only on unit tests.

Do not run `npm` commands; use Bun for frontend work.

## Risk Notes

- GitLab nested namespaces are the main identity wrinkle. The plan stores the full namespace in `owner`, but API routes that use `/repos/{owner}/{name}` cannot safely represent owners containing `/`; routes and generated clients must thread platform/host and should move toward query/body repo refs for detail actions.
- GitHub GraphQL bulk sync should stay GitHub-specific. Trying to model GitLab through the GitHub GraphQL bulk fetcher will make the abstraction brittle.
- Mutation parity is intentionally not part of the first GitLab milestone. Capability flags make this visible and prevent GitLab rows from showing buttons that cannot work.
- Rate limits are not identical across providers. The DB can keep `api_type`, but provider clients should name buckets as `rest`, `graphql`, or `gitlab-rest` as needed.
- Keep `db.MergeRequest` as the local row type for now. Renaming database and generated API vocabulary to "change request" is a separate migration-sized project.

## Self-Review

- Spec coverage: covers GitLab repo fetching, GitLab MR/issue sync, GitHub migration into an abstraction, future provider extensibility, config, DB, server, frontend, docs, and tests.
- Placeholder scan: no placeholder markers, deferred code stubs, or unnamed files remain.
- Type consistency: the plan consistently uses `platform.Kind`, `platform.RepoRef`, `Capabilities`, and provider reader interfaces.
- Scope check: this is one large but coherent platform-enablement project. If execution feels too broad, split after Task 5: first land the abstraction with GitHub unchanged, then land GitLab as a second branch.
