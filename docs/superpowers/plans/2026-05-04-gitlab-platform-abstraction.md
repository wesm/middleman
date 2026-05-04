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
- GitLab's Go API client docs are at <https://pkg.go.dev/gitlab.com/gitlab-org/api/client-go>. Use `gitlab.com/gitlab-org/api/client-go` as the module/import path.
- Context7 reports the old `github.com/xanzy/go-gitlab` client as moved to `gitlab.com/gitlab-org/api/client-go`; do not add the deprecated import path or copy examples that still import `github.com/xanzy/go-gitlab`.

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
- Create or modify `internal/server/repo_ref.go`: parse and validate provider-aware repository references from query/body payloads.
- Modify `internal/server/repo_import_handlers.go`: accept provider/host in preview and bulk add requests.
- Modify `internal/server/settings_handlers.go`: expose configured repo statuses with platform and host.
- Modify `internal/server/api_types.go` and `internal/server/huma_routes.go`: add provider/host fields, platform-aware repo-reference routes, stable error codes, and capability-gated actions.
- Regenerate `frontend/openapi/openapi.yaml`, `internal/apiclient/generated/client.gen.go`, `packages/ui/src/api/generated/*`, and `frontend/src/lib/api/generated/*` with `make api-generate`.
- Modify `packages/ui/src/api/types.ts` and affected stores/components only where generated types require provider/host threading.
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

Keep the base provider interface to metadata and capabilities only. Feature support is discovered through optional interfaces so future providers can implement a useful subset without adding no-op methods or fake unsupported behavior.

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

type Provider interface {
	Platform() Kind
	Host() string
	Capabilities() Capabilities
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
```

The registry stores `Provider` values and exposes helpers such as `RepositoryReader(platform, host)`, `MergeRequestReader(platform, host)`, and `IssueReader(platform, host)` that return typed missing-capability errors when the provider does not implement the requested optional interface. GitHub can implement extra mutation interfaces. GitLab first implements read interfaces and advertises mutation capabilities as false.

### API Repository Identity

GitLab nested namespaces make the existing `/repos/{owner}/{name}` route family unsafe because `owner` can contain `/`. New platform-aware endpoints should follow GitLab's own convention: carry the full repository path as one URL-escaped parameter, where `group/subgroup/project` becomes `group%2Fsubgroup%2Fproject`. Handlers decode that value into `owner = "group/subgroup"` and `name = "project"` before looking up the repo.

Rules:

- Keep existing GitHub-compatible routes such as `/repos/{owner}/{name}/pulls/{number}` working for GitHub and GitHub Enterprise.
- Before committing to an escaped path-param route, prove Huma and the server middleware preserve `group%2Fsubgroup%2Fproject` as one handler parameter. If that test fails, keep the same `provider`, `platform_host`, and `repo_path` JSON shape but use the fallback route shape below.
- Preferred route shape, if the router-level proof passes, uses `repo_path` as one escaped path parameter:

```text
GET  /api/v1/providers/gitlab/hosts/gitlab.com/repos/group%2Fsubgroup%2Fproject/pull-requests/12
POST /api/v1/providers/gitlab/hosts/gitlab.com/repos/group%2Fsubgroup%2Fproject/pull-requests/12/refresh
POST /api/v1/providers/gitlab/hosts/gitlab.com/repos/group%2Fsubgroup%2Fproject/issues/7/refresh
POST /api/v1/providers/gitlab/hosts/gitlab.com/repos/group%2Fsubgroup%2Fproject/pull-requests/12/actions
```

- Fallback route shape, if normal path params cannot preserve encoded slashes, uses query/body repo refs and does not rely on `%2F` surviving route matching:

```text
GET  /api/v1/items/pull-request?provider=gitlab&platform_host=gitlab.com&repo_path=group%2Fsubgroup%2Fproject&number=12
POST /api/v1/items/pull-request/refresh
POST /api/v1/items/issue/refresh
```

Fallback route matrix:

| Workflow | Method | Preferred escaped-path route | Fallback route | Fallback identity fields |
| --- | --- | --- | --- | --- |
| PR detail | `GET` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/pull-requests/{number}` | `/api/v1/items/pull-request` | query: `provider`, `platform_host`, `repo_path`, `number` |
| Issue detail | `GET` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/issues/{number}` | `/api/v1/items/issue` | query: `provider`, `platform_host`, `repo_path`, `number` |
| PR refresh | `POST` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/pull-requests/{number}/refresh` | `/api/v1/items/pull-request/refresh` | JSON body: `repo.provider`, `repo.platform_host`, `repo.repo_path`, `number` |
| Issue refresh | `POST` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/issues/{number}/refresh` | `/api/v1/items/issue/refresh` | JSON body: `repo.provider`, `repo.platform_host`, `repo.repo_path`, `number` |
| PR comments/events refresh | `POST` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/pull-requests/{number}/events/refresh` | `/api/v1/items/pull-request/events/refresh` | JSON body: `repo.provider`, `repo.platform_host`, `repo.repo_path`, `number` |
| Issue comments/events refresh | `POST` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/issues/{number}/events/refresh` | `/api/v1/items/issue/events/refresh` | JSON body: `repo.provider`, `repo.platform_host`, `repo.repo_path`, `number` |
| PR action/mutation | `POST` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/pull-requests/{number}/actions` | `/api/v1/items/pull-request/actions` | JSON body: `repo.provider`, `repo.platform_host`, `repo.repo_path`, `number`, `action`, action-specific fields |
| Issue action/mutation | `POST` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/issues/{number}/actions` | `/api/v1/items/issue/actions` | JSON body: `repo.provider`, `repo.platform_host`, `repo.repo_path`, `number`, `action`, action-specific fields |
| Repo detail/settings | `GET` | `/api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}` | `/api/v1/repo` | query: `provider`, `platform_host`, `repo_path` |

Only one route family should be implemented for new provider-aware clients after the Task 5 router proof: the escaped-path family if it works, otherwise the fallback family above. Existing GitHub compatibility routes remain wrappers either way.

- Request-body routes may use the same escaped repo path or the structured repo ref shape when the action already has a JSON body:

```json
{
  "repo": {
    "provider": "gitlab",
    "platform_host": "gitlab.com",
    "repo_path": "group/subgroup/project"
  },
  "number": 12
}
```

- JSON request and response bodies use `provider` as the client-facing discriminator. Server internals can map `provider` to the existing `platform` storage field. New route path segments should also use `providers/{provider}` so generated clients expose `Provider` consistently.
- Legacy GitHub routes should return the new `provider`, `platform_host`, and `repo_path` fields in response bodies as soon as the response types change. Compatibility is route-level, not response-shape-level, because frontend stores need one item identity shape.
- Generated clients and frontend route refs must carry `provider`, `platform_host`, `repo_path`, and `number`; helper methods can split `repo_path` into `owner/name` only at DB and provider boundaries. For GitHub UI URLs, `provider` and default `github.com` host may remain omitted in the visible route only when the route helper can reconstruct the full repo ref.
- Add e2e coverage for `group/subgroup/project` detail load and refresh through the Task 5 route shape before GitLab sync is considered complete.

### Error Taxonomy

Platform errors need stable machine-readable codes so the UI can distinguish unsupported actions from missing auth or permission problems.

```go
type PlatformErrorCode string

const (
	ErrCodeUnsupportedCapability PlatformErrorCode = "unsupported_capability"
	ErrCodeProviderNotConfigured PlatformErrorCode = "provider_not_configured"
	ErrCodeMissingToken          PlatformErrorCode = "missing_token"
	ErrCodeInvalidRepoRef        PlatformErrorCode = "invalid_repo_ref"
	ErrCodePermissionDenied      PlatformErrorCode = "permission_denied"
	ErrCodeNotFound              PlatformErrorCode = "not_found"
	ErrCodeRateLimited           PlatformErrorCode = "rate_limited"
)
```

Server handlers should return these codes in JSON error details for platform-aware routes. GitHub legacy routes may keep their current text messages until they are migrated, but new GitLab paths must use the coded errors from the start.

| Source error | Platform code | HTTP status | Response detail |
| --- | --- | --- | --- |
| Provider registry miss | `provider_not_configured` | `502 Bad Gateway` | `{ "code": "provider_not_configured", "provider": "...", "platform_host": "..." }` |
| Missing token during startup/client creation | `missing_token` | startup error or `502 Bad Gateway` in settings flows | `{ "code": "missing_token", "provider": "...", "token_env": "..." }` |
| Config or request repo ref validation failure | `invalid_repo_ref` | `400 Bad Request` | `{ "code": "invalid_repo_ref", "field": "repo_path" }` |
| Provider optional interface/capability absent | `unsupported_capability` | `501 Not Implemented` for route families, `409 Conflict` for item-specific actions | `{ "code": "unsupported_capability", "capability": "merge_mutation" }` |
| GitHub/GitLab 401 or 403 | `permission_denied` | `502 Bad Gateway` for background/preview flows, provider status stored on sync rows | `{ "code": "permission_denied" }` |
| GitHub/GitLab 404 | `not_found` | `404 Not Found` for direct user refresh/detail, `502 Bad Gateway` for startup/import validation | `{ "code": "not_found" }` |
| GitHub/GitLab rate-limit response | `rate_limited` | `429 Too Many Requests` for foreground actions, sync backoff for background | `{ "code": "rate_limited", "reset_at": "..." }` |

Provider clients should wrap raw GitHub/GitLab errors into typed platform errors at the transport boundary when status codes are known. HTTP handlers should only map typed platform errors to response codes and should avoid parsing raw provider error strings.

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

### GitLab CI Mapping

Middleman stores one aggregate `ci_status` plus a JSON list of checks. GitLab starts with a single synthesized check for the MR head pipeline.

| GitLab pipeline status | Middleman check status | Middleman conclusion | Aggregate `ci_status` |
| --- | --- | --- | --- |
| `created`, `waiting_for_resource`, `preparing`, `pending`, `running` | `in_progress` | empty | `pending` |
| `success` | `completed` | `success` | `success` |
| `failed` | `completed` | `failure` | `failure` |
| `canceled`, `cancelled` | `completed` | `cancelled` | `failure` |
| `skipped` | `completed` | `skipped` | `success` |
| `manual`, `scheduled` | `queued` | empty | `pending` |
| unknown non-empty status | `completed` | `neutral` | `neutral` |
| missing pipeline | empty list | empty | empty |

If the MR list response omits `head_pipeline`, detail sync should request the MR detail endpoint once before concluding CI is missing. If both omit pipeline data, leave `ci_status` and `ci_checks_json` empty rather than carrying stale data across a head SHA change.

### Existing Row Backfill

Migration `000017` must be safe for existing databases:

- Existing rows keep `platform = 'github'` and their current `platform_host`.
- `platform_repo_id`, `web_url`, `clone_url`, and `default_branch` default to empty strings in SQL so the migration is instant and reversible.
- The first successful provider sync fills these fields from provider repository metadata.
- For GitHub rows, a failed repository metadata fetch must not block PR/issue sync; it only leaves the new fields empty for that cycle.
- For GitLab rows, exact project resolution must fill `platform_repo_id` before sync proceeds when the project is reachable.

### Self-Hosted GitLab URLs

GitLab host configuration is host-only in the first milestone:

- `platform_host = "gitlab.example.com"` maps to API base URL `https://gitlab.example.com/api/v4`.
- `platform_host = "gitlab.example.com:8443"` is valid and maps to `https://gitlab.example.com:8443/api/v4`.
- A host value with `https://` or a trailing slash is normalized to the hostname and rejected if it includes a non-root path.
- GitLab installations under a subpath, such as `https://example.com/gitlab`, are explicitly unsupported in this milestone and should fail validation with `invalid_repo_ref`.
- Custom CA and insecure TLS settings are out of scope. Users must rely on the host OS trust store.

### Repo Path Canonicalization

- GitHub repo paths are case-folded to match current behavior.
- GitLab repo paths are stored using the canonical `path_with_namespace` returned by GitLab. User input can be normalized after a successful project lookup, but duplicate detection should compare the provider-canonical path rather than lowercasing arbitrary input.
- UI search can remain case-insensitive, but provider lookups and persisted identity should preserve GitLab's canonical path spelling.

## Implementation Tasks

Standing rule for API work: every task that changes Huma routes, request schemas, response schemas, or OpenAPI-visible error details must run `make api-generate` in that same task and commit the regenerated artifacts with the contract change.

### Task 1: Add Neutral Platform Types

**Files:**
- Create: `internal/platform/types.go`
- Create: `internal/platform/client.go`
- Create: `internal/platform/registry.go`
- Create: `internal/platform/errors.go`
- Test: `internal/platform/registry_test.go`

- [ ] Write tests for registry lookup by `(platform, host)`, missing client errors, optional reader lookup, and capability lookup.
- [ ] Add `Kind`, `RepoRef`, `Repository`, `MergeRequest`, `Issue`, `Label`, `MergeRequestEvent`, `IssueEvent`, `Release`, `Tag`, `CICheck`, and `Capabilities`.
- [ ] Add base `Provider` plus narrow reader and optional mutation interfaces; do not embed reader interfaces in `Provider`.
- [ ] Add `PlatformErrorCode` constants and typed errors for unsupported capability, missing provider, missing token, invalid repo ref, permission denied, not found, and rate limited.
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
- [ ] Add `UpdateRepoProviderMetadata(ctx, repoID, metadata)` for `platform_repo_id`, `web_url`, `clone_url`, and `default_branch`.
- [ ] Keep a small compatibility helper for GitHub-heavy tests:

```go
func GitHubRepoIdentity(host, owner, name string) RepoIdentity {
	return RepoIdentity{Platform: "github", PlatformHost: host, Owner: owner, Name: name}
}
```

- [ ] Update queries to select and scan new repo fields.
- [ ] Add migration tests proving existing rows keep `platform = 'github'`, new metadata fields default to empty strings, GitHub upserts still use `platform = 'github'`, GitLab upserts use `platform = 'gitlab'`, and the same host/owner/name on different platforms creates different rows.
- [ ] Add query tests proving provider metadata can be filled after the initial upsert without changing the repo identity.
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
- [ ] Normalize `https://gitlab.example.com/` to `gitlab.example.com`; preserve valid host ports such as `gitlab.example.com:8443`; reject `https://example.com/gitlab` subpath installs with `invalid_repo_ref`.
- [ ] Add tests for old GitHub config, GitHub Enterprise config, GitLab env token config, nested GitLab namespace config, self-hosted GitLab host normalization, host port preservation, subpath rejection, duplicate detection across platform/host, and token env conflicts scoped to `(platform, host)`.
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

### Task 5: Add Platform-Aware Repo Routes

**Files:**
- Create: `internal/server/repo_ref.go`
- Modify: `internal/server/huma_routes.go`
- Modify: `internal/server/api_types.go`
- Modify: `packages/ui/src/routes.ts`
- Modify: `packages/ui/src/api/types.ts`
- Test: `internal/server/api_test.go`
- Test: `packages/ui/src/routes.test.ts`

- [ ] Write the first server integration test before adding the final route contract. It must issue a real HTTP request with `group%2Fsubgroup%2Fproject` and assert the handler receives one `repo_path` value, not split path segments.
- [ ] If Huma/server routing cannot preserve `%2F` as one parameter, switch this task to the query/body fallback route shape from the API Repository Identity section before proceeding.
- [ ] Add `RepoRefInput`/`RepoRefResponse` API shapes with `provider`, `platform_host`, and `repo_path`.
- [ ] Add parser tests for `repo_path = "owner/repo"` and `repo_path = "group/subgroup/project"`.
- [ ] Add the selected platform-aware route family. Use escaped `repo_path` as one route parameter if the router proof passes; otherwise use the fallback query/body routes from the API Repository Identity section:

```text
GET /api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/pull-requests/{number}
GET /api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/issues/{number}
POST /api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/pull-requests/{number}/refresh
POST /api/v1/providers/{provider}/hosts/{platform_host}/repos/{repo_path}/issues/{number}/refresh
```

- [ ] Keep existing GitHub routes as compatibility wrappers that build a full repo ref and call the same handler internals.
- [ ] Ensure legacy GitHub route responses include `provider`, `platform_host`, and `repo_path` once the shared response types change.
- [ ] Return coded platform errors from new routes for invalid repo refs, missing provider clients, and unsupported capabilities.
- [ ] Run `make api-generate` immediately after the route contract changes.
- [ ] Run `go test ./internal/server -run 'Test.*RepoRef|Test.*Pull.*Route|Test.*Issue.*Route' -shuffle=on`.
- [ ] Run frontend route/type tests that cover the selected nested GitLab repo-path route family.
- [ ] Commit: `feat: add platform-aware repository routes`.

### Task 6: Wire Syncer Registry Lookup

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/github/repo_config_resolver.go`
- Test: `internal/github/sync_test.go`
- Test: `internal/github/repo_config_resolver_test.go`

- [ ] Change `Syncer.clients` from `map[string]Client` to the platform registry while keeping all configured clients GitHub adapters.
- [ ] Change syncer client lookup to request optional `RepositoryReader`, `MergeRequestReader`, and `IssueReader` interfaces as needed.
- [ ] Keep public method behavior unchanged for existing GitHub callers.
- [ ] Add tests for missing provider, missing optional reader, and duplicate owner/name on different platforms.
- [ ] Run `go test ./internal/github -run 'Test.*Client|Test.*Resolve|Test.*Tracked' -shuffle=on`.
- [ ] Commit: `refactor: resolve sync clients through platform registry`.

### Task 7: Propagate Full RepoRef Through Sync Paths

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/github/queue.go`
- Modify: `internal/github/repo_config_resolver.go`
- Test: `internal/github/sync_test.go`
- Test: `internal/github/repo_config_resolver_test.go`

- [ ] Change `RepoRef` to carry `Platform`, `Host`, `Owner`, `Name`, `RepoPath`, and `PlatformRepoID`.
- [ ] Update `syncRepo`, `indexSyncRepo`, `SyncMR`, `SyncIssue`, detail queue, backfill, and watched MR paths to pass a full `RepoRef` instead of reconstructing host/owner/name at call sites.
- [ ] Add tests proving the same `owner/name/number` on GitHub and GitLab resolves to the intended platform row and does not cross-sync.
- [ ] Run `go test ./internal/github -run 'Test.*Sync|Test.*Issue|Test.*MR' -shuffle=on`.
- [ ] Commit: `refactor: thread full repository refs through sync`.

### Task 8: Preserve GitHub Optimizations And Diff Behavior

**Files:**
- Modify: `internal/github/sync.go`
- Modify: `internal/platform/client.go`
- Test: `internal/github/sync_test.go`
- Test: `internal/github/merged_diff_test.go`

- [ ] Preserve GitHub GraphQL bulk fetch as a GitHub-only optimization behind an optional `BulkMergeRequestFetcher` interface.
- [ ] Preserve ETag invalidation as an optional conditional-list capability. GitLab can no-op initially.
- [ ] Use provider-supplied clone URLs for diff sync instead of formatting `https://host/owner/name.git`.
- [ ] Add tests proving GitHub GraphQL bulk fetch still runs when available, ETag invalidation still recovers failed GitHub list syncs, and diff clone URLs support GitLab nested namespace paths.
- [ ] Run `go test ./internal/github -run 'Test.*GraphQL|Test.*ETag|Test.*Diff|Test.*Merged' -shuffle=on`.
- [ ] Commit: `refactor: keep GitHub sync optimizations optional`.

### Task 9: Add GitLab Client And Normalization

**Files:**
- Create: `internal/platform/gitlab/client.go`
- Create: `internal/platform/gitlab/normalize.go`
- Create: `internal/platform/gitlab/client_test.go`
- Create: `internal/platform/gitlab/normalize_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] Add `gitlab.com/gitlab-org/api/client-go` with `go get gitlab.com/gitlab-org/api/client-go`.
- [ ] Import the GitLab client as `gitlab "gitlab.com/gitlab-org/api/client-go"` and use the package docs at <https://pkg.go.dev/gitlab.com/gitlab-org/api/client-go> as the source of truth for client APIs.
- [ ] Build clients with `gitlab.NewClient(token, gitlab.WithBaseURL("https://<host>/api/v4"))`.
- [ ] Implement exact project lookup by URL-escaped `path_with_namespace`.
- [ ] Implement namespace listing for repo preview. For groups, use group project listing with subgroups when available; for users, fall back to project search/listing filtered by namespace path.
- [ ] Implement MR list/detail pagination with project `id` when known and encoded `path_with_namespace` when not.
- [ ] Implement issues, comments, commits, releases, tags, and basic pipeline status reads.
- [ ] Normalize GitLab `iid` as middleman `number`; never use the global MR id as `number`.
- [ ] Implement the GitLab CI mapping table exactly: pending/running statuses become `pending`, success and skipped become `success`, failed/canceled become `failure`, manual/scheduled become `pending`, unknown statuses become `neutral`, and missing pipelines leave CI empty.
- [ ] Add httptest fixtures for paginated GitLab responses, pipeline status mapping, missing pipeline fallback, and self-hosted base URL construction.
- [ ] Run `go test ./internal/platform/gitlab -shuffle=on`.
- [ ] Commit: `feat: add GitLab read client`.

### Task 10: Wire Runtime Client Construction

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

### Task 11: Provider-Aware Repository Import

**Files:**
- Modify: `internal/server/repo_import_handlers.go`
- Modify: `internal/server/settings_handlers.go`
- Modify: `internal/server/api_types.go`
- Test: `internal/server/settings_e2e_test.go`
- Test: `internal/server/api_test.go`

- [ ] Extend preview request:

```json
{
  "provider": "gitlab",
  "platform_host": "gitlab.com",
  "owner": "my-group/subgroup",
  "pattern": "service-*"
}
```

- [ ] Default omitted `provider` to `github` and omitted `platform_host` to the provider default.
- [ ] Return provider and host on preview rows.
- [ ] Update bulk add request rows to include provider, host, and `repo_path`.
- [ ] Validate exact rows through the matching provider registry client.
- [ ] Store the provider-canonical `repo_path` returned by lookup. GitHub paths continue to case-fold; GitLab paths preserve `path_with_namespace`.
- [ ] Keep old GitHub-only requests working.
- [ ] Add full-stack e2e coverage using a fake GitLab API server and real SQLite: preview GitLab repos, add one exact repo, trigger syncer tracked repo update.
- [ ] Run `make api-generate` immediately after changing preview or bulk request/response contracts.
- [ ] Run `go test ./internal/server -run 'Test.*Repo.*' -shuffle=on`.
- [ ] Commit: `feat: import repositories from configured platforms`.

### Task 12: Gate Provider-Specific Actions

**Files:**
- Modify: `internal/server/huma_routes.go`
- Modify: `internal/server/api_types.go`
- Modify: `packages/ui/src/api/types.ts`
- Modify: PR/MR action components that call GitHub-only routes
- Test: `internal/server/api_test.go`
- Test: affected frontend component tests

- [ ] Add capabilities to repository or item detail responses.
- [ ] Return `409 Conflict` or `501 Not Implemented` for mutation routes when the provider lacks the capability, with `unsupported_capability`.
- [ ] Return `provider_not_configured`, `missing_token`, `invalid_repo_ref`, `permission_denied`, `not_found`, and `rate_limited` from the new platform-aware route family where applicable.
- [ ] Hide or disable unsupported action buttons in the frontend for GitLab rows.
- [ ] Keep GitHub action behavior unchanged.
- [ ] Run `make api-generate`.
- [ ] Run `go test ./internal/server -shuffle=on`.
- [ ] Run `bun test` in affected frontend/package workspaces according to existing package scripts.
- [ ] Commit: `feat: expose platform action capabilities`.

### Task 13: GitLab Sync E2E

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
- [ ] Assert nested namespace detail and refresh calls work through the Task 5 route shape selected by the router proof: escaped `repo_path` path parameter when supported, otherwise the query/body fallback.
- [ ] Assert GitHub rows remain absent.
- [ ] Run `go test ./internal/server -run TestGitLabSync -shuffle=on`.
- [ ] Commit: `test: cover GitLab repository sync end to end`.

### Task 14: Documentation And Invariants

**Files:**
- Create: `context/platform-sync-invariants.md`
- Modify: `context/github-sync-invariants.md`
- Modify: `README.md`
- Modify: `config.example.toml`

- [ ] Document identity as `(platform, platform_host, owner, name)`.
- [ ] Document GitLab nested namespace handling and escaped `repo_path` route conventions.
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

- GitLab nested namespaces are the main identity wrinkle. The plan stores the full namespace in `owner`; Task 5 must prove the server can preserve a single escaped `repo_path` route parameter before using that route shape. If the proof fails, the implementation must use the query/body fallback while still threading `provider`, `platform_host`, and `repo_path` everywhere it addresses a repo-scoped item.
- GitHub GraphQL bulk sync should stay GitHub-specific. Trying to model GitLab through the GitHub GraphQL bulk fetcher will make the abstraction brittle.
- Mutation parity is intentionally not part of the first GitLab milestone. Capability flags make this visible and prevent GitLab rows from showing buttons that cannot work.
- Rate limits are not identical across providers. The DB can keep `api_type`, but provider clients should name buckets as `rest`, `graphql`, or `gitlab-rest` as needed.
- Keep `db.MergeRequest` as the local row type for now. Renaming database and generated API vocabulary to "change request" is a separate migration-sized project.

## Self-Review

- Spec coverage: covers GitLab repo fetching, GitLab MR/issue sync, GitHub migration into an abstraction, future provider extensibility, config, DB, server, frontend, docs, and tests.
- Placeholder scan: no placeholder markers, deferred code stubs, or unnamed files remain.
- Type consistency: the plan consistently uses `platform.Kind`, `platform.RepoRef`, `Capabilities`, and provider reader interfaces.
- Scope check: this is one large but coherent platform-enablement project. If execution feels too broad, split after Task 5: first land the abstraction with GitHub unchanged, then land GitLab as a second branch.
