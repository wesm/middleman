# Forgejo Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Forgejo as the third Middleman provider, alongside the existing GitHub and GitLab providers, with read sync, repo import, provider-aware routing, and optional mutation capabilities where the Forgejo API supports them.

**Architecture:** Reuse the existing `internal/platform` capability model. GitHub remains the compatibility baseline for user-visible PR behavior, GitLab remains the template for provider-neutral startup, registry, pagination, and persistence wiring, and Forgejo gets its own `internal/platform/forgejo` package backed by the Gitea Go SDK because Forgejo exposes a Gitea-compatible API surface. Keep Forgejo provider code acyclic: provider packages import `internal/platform`, not server/config packages.

**Tech Stack:** Go, SQLite, Huma/OpenAPI, `code.gitea.io/sdk/gitea` v0.25.0 or current pinned release, Svelte 5, Bun, Forgejo container e2e fixture, provider-aware Go API tests with real SQLite.

---

## Research Basis

- Forgejo documents Gitea API compatibility through its version scheme. Forgejo versions include `+gitea-<version>` metadata, and the `/api/v1/version` endpoint can be used to inspect the compatible Gitea version. Source: https://forgejo.org/docs/latest/user/versions/
- Forgejo API authentication accepts OAuth2 bearer tokens and token query parameters. Middleman should use an access token from an environment variable and send it through the SDK token option, not via query strings. Source: https://forgejo.org/docs/latest/user/api-usage/
- Forgejo scoped tokens require explicit scopes. Read-only sync needs `read:repository` for repositories, pull requests, releases, tags, files, and statuses, plus `read:issue` for issues, labels, milestones, and issue or PR comments under issue routes. Notification import would additionally need `read:notification`. Comment, issue, review, merge, or state mutations need the corresponding write scopes. Source: https://forgejo.org/docs/latest/user/token-scope/
- Forgejo provides official container images at `codeberg.org/forgejo/forgejo:<major>` and documents Docker Compose installation. Use those images for optional local e2e coverage. Source: https://forgejo.org/docs/latest/admin/installation/docker/
- The current Go SDK choice is `code.gitea.io/sdk/gitea`, published as `v0.25.0` on May 5, 2026. It exposes `NewClient(url, options...)`, `SetToken`, `SetHTTPClient`, `SetUserAgent`, repository APIs such as `GetRepo`, pull APIs such as `ListRepoPullRequests`, issue/comment APIs such as `ListRepoIssues` and `ListRepoIssueComments`, release/tag APIs, status APIs, and `Response.NextPage` pagination. Source: https://pkg.go.dev/code.gitea.io/sdk/gitea
- `go list -m -versions code.gitea.io/sdk/gitea` currently reports versions through `v0.25.0`; pin the exact release in `go.mod` when implementation starts.

## Core Decisions

- Provider kind: add `platform.KindForgejo` with string value `"forgejo"`.
- Default host: use `codeberg.org`, because Codeberg is the public Forgejo instance most users will expect. Self-hosted Forgejo remains first-class through `platform_host`.
- Metadata: `AllowNestedOwner=false`; Forgejo/Gitea repository identities are `owner/repo`, unlike GitLab nested namespaces.
- Case handling: do not lower-case configured Forgejo owner/name during config normalization. Preserve the API-returned canonical owner, repo name, and full name in persisted display fields. Existing DB lookup keys may remain case-folded where already provider-neutral, but provider normalization should not rewrite display values.
- Token env: introduce `MIDDLEMAN_FORGEJO_TOKEN` as the documented default for Forgejo platform config. Do not add a `gh auth token` fallback for Forgejo.
- SDK: use `code.gitea.io/sdk/gitea`, not a stale `gitea.com/gitea/go-sdk/gitea` import path and not a generated Forgejo-only client for the first milestone.
- API base URL: construct the SDK client with `https://<platform_host>` for normal use, plus `WithBaseURLForTesting` for container and `httptest` coverage. The SDK appends API routes internally.
- First milestone capabilities: read repositories, open pull requests as `platform.MergeRequest`, open issues, comments/timeline where available, releases, tags, and commit statuses. Enable comment mutation, issue mutation, state mutation, review mutation, and merge mutation only after tests prove the SDK methods work against Forgejo.
- CI mapping: start with `ListStatuses(owner, repo, sha, opts)` because Forgejo/Gitea commit status APIs are closer to GitHub statuses than GitLab pipelines. Treat Actions runs as a follow-up unless needed for the first UI status chip.
- Repo import: support exact owner/org repo listing through `ListUserRepos` or `ListOrgRepos` style SDK methods, with a user-first then org fallback similar to GitLab group/user fallback. Do not support nested group glob semantics for Forgejo.
- Local e2e: add an optional Forgejo fixture parallel to `scripts/e2e/gitlab`, using SQLite inside the container for speed and an API bootstrap script that seeds a user/org, repository, branch, pull request, issue, label, comments, tag, release, and commit status.

## File Structure

- Modify `internal/platform/types.go`: add `KindForgejo` and `DefaultForgejoHost`.
- Modify `internal/platform/metadata.go`: add Forgejo metadata, kind aliases, and tests.
- Create `internal/platform/forgejo/client.go`: SDK client construction, options, auth, rate-limit tracking, pagination helpers, provider capabilities, and provider interface methods.
- Create `internal/platform/forgejo/normalize.go`: map Gitea SDK repository, pull request, issue, comment, review, release, tag, and status types into `platform` types.
- Create `internal/platform/forgejo/client_test.go`: `httptest` coverage for auth header shape, base URL, pagination, repository lookup, repo import fallback, pull/issue listing, comments, releases/tags, statuses, and error mapping.
- Create `internal/platform/forgejo/normalize_test.go`: deterministic unit coverage for state, draft, branches, SHAs, author, timestamps, URLs, labels, releases, tags, and CI status normalization.
- Modify `cmd/middleman/provider_startup.go`: register the Forgejo factory and create rate trackers and clone tokens using the existing provider host key model.
- Modify `internal/config/config.go` and `internal/config/config_test.go`: add Forgejo defaults, URL parsing, token env behavior, duplicate detection, host validation, and config examples.
- Modify `internal/server/repo_import_handlers.go`, `internal/server/settings_test.go`, and any generated route tests only where provider lists or defaults are hard-coded.
- Modify `frontend/src/lib/components/settings/repoImportProviders.ts` and tests: expose Forgejo in the import modal with default host `codeberg.org`.
- Modify `README.md` and `config.example.toml`: document Forgejo config, token scopes, and supported capabilities.
- Create `scripts/e2e/forgejo/docker-compose.yml`: optional Forgejo service bound to loopback.
- Create `scripts/e2e/forgejo/wait.sh`: readiness probe for the Forgejo UI and `/api/v1/version`.
- Create `scripts/e2e/forgejo/bootstrap.sh`: idempotent fixture seeding and manifest output.
- Create `scripts/e2e/forgejo/README.md`: usage, image tag policy, cleanup, and resource notes.
- Create `internal/server/forgejo_container_e2e_test.go`: gated optional full-stack Forgejo test with real SQLite.
- Regenerate generated API artifacts with `make api-generate` only if Huma route schemas change. Adding a provider option alone should not require route shape changes.

## Task 1: Provider Metadata And Config Defaults

**Files:**
- Modify: `internal/platform/types.go`
- Modify: `internal/platform/metadata.go`
- Modify: `internal/platform/metadata_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing metadata tests**

Add assertions to `TestProviderMetadataForBuiltIns`:

```go
forgejo, ok := MetadataFor(KindForgejo)
require.True(t, ok)
assert.Equal(KindForgejo, forgejo.Kind)
assert.Equal("Forgejo", forgejo.Label)
assert.Equal(DefaultForgejoHost, forgejo.DefaultHost)
assert.False(forgejo.AllowNestedOwner)
assert.False(forgejo.LowercaseRepoNames)
```

Add assertions to `TestNormalizeKindAllowsFutureProviderKinds`:

```go
fj, err := NormalizeKind("fj")
require.NoError(t, err)
assert.Equal(KindForgejo, fj)

forgejo, err := NormalizeKind("Forgejo")
require.NoError(t, err)
assert.Equal(KindForgejo, forgejo)
```

- [ ] **Step 2: Run metadata tests and confirm failure**

Run:

```bash
go test ./internal/platform -run 'TestProviderMetadataForBuiltIns|TestNormalizeKindAllowsFutureProviderKinds' -shuffle=on
```

Expected: fails because `KindForgejo` and `DefaultForgejoHost` are undefined.

- [ ] **Step 3: Implement metadata**

In `internal/platform/types.go`, add:

```go
KindForgejo Kind = "forgejo"
```

and:

```go
DefaultForgejoHost = "codeberg.org"
```

In `internal/platform/metadata.go`, add:

```go
KindForgejo: {
	Kind:             KindForgejo,
	Label:            "Forgejo",
	DefaultHost:      DefaultForgejoHost,
	AllowNestedOwner: false,
},
```

and extend `NormalizeKind`:

```go
case "fj":
	return KindForgejo, nil
```

- [ ] **Step 4: Write failing config tests**

Add tests to `internal/config/config_test.go`:

```go
func TestLoadPlatformConfigForgejoToken(t *testing.T) {
	cfg := loadConfigFromString(t, `
[[platforms]]
type = "forgejo"
host = "codeberg.org"
token_env = "MIDDLEMAN_FORGEJO_TOKEN"

[[repos]]
platform = "forgejo"
platform_host = "codeberg.org"
owner = "forgejo"
name = "forgejo"
`)
	t.Setenv("MIDDLEMAN_FORGEJO_TOKEN", "forgejo-secret")

	assert.Equal(t, "forgejo", cfg.Platforms[0].Type)
	assert.Equal(t, "codeberg.org", cfg.Platforms[0].Host)
	assert.Equal(t, "forgejo", cfg.Repos[0].Platform)
	assert.Equal(t, "codeberg.org", cfg.Repos[0].PlatformHost)
	assert.Equal(t, "forgejo-secret", cfg.TokenForPlatformHost("forgejo", "codeberg.org", ""))
}
```

```go
func TestLoadParsesForgejoCodebergURL(t *testing.T) {
	cfg := loadConfigFromString(t, `
[[repos]]
platform = "forgejo"
name = "https://codeberg.org/forgejo/forgejo.git"
`)

	require.Len(t, cfg.Repos, 1)
	assert.Equal(t, "forgejo", cfg.Repos[0].Platform)
	assert.Equal(t, "codeberg.org", cfg.Repos[0].PlatformHost)
	assert.Equal(t, "forgejo", cfg.Repos[0].Owner)
	assert.Equal(t, "forgejo", cfg.Repos[0].Name)
	assert.Equal(t, "forgejo/forgejo", cfg.Repos[0].RepoPath)
}
```

- [ ] **Step 5: Run config tests and confirm failure**

Run:

```bash
go test ./internal/config -run 'TestLoadPlatformConfigForgejoToken|TestLoadParsesForgejoCodebergURL' -shuffle=on
```

Expected: fails until Forgejo metadata and URL parsing are wired.

- [ ] **Step 6: Implement config support**

Update `Repo.normalize` so parsed Forgejo URLs set `RepoPath = owner + "/" + name`, just like GitHub. Extend URL host inference so `codeberg.org` maps to Forgejo when `platform` is omitted or explicitly `forgejo`.

- [ ] **Step 7: Run tests**

Run:

```bash
go test ./internal/platform ./internal/config -shuffle=on
```

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/types.go internal/platform/metadata.go internal/platform/metadata_test.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: recognize Forgejo provider configuration" -m "Adds Forgejo metadata and config parsing so Codeberg and self-hosted Forgejo repos can be represented before provider sync is implemented."
```

## Task 2: Forgejo Client Skeleton And Read Capabilities

**Files:**
- Create: `internal/platform/forgejo/client.go`
- Create: `internal/platform/forgejo/client_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add SDK dependency**

Run:

```bash
go get code.gitea.io/sdk/gitea@v0.25.0
```

Expected: `go.mod` and `go.sum` include `code.gitea.io/sdk/gitea`.

- [ ] **Step 2: Write failing client construction and auth tests**

Create `internal/platform/forgejo/client_test.go` with a test that starts `httptest.Server`, constructs:

```go
client, err := NewClient(
	"codeberg.test",
	"forgejo-token",
	WithBaseURLForTesting(server.URL),
)
```

and calls `GetRepository` for `owner/repo`. The handler must assert the request path is `/api/v1/repos/owner/repo` and that the token is sent through an authorization header accepted by the SDK. Accept either `Authorization: token forgejo-token` or `Authorization: Bearer forgejo-token` depending on the SDK output; lock the observed header into the test after the first failure.

- [ ] **Step 3: Run the failing test**

Run:

```bash
go test ./internal/platform/forgejo -run TestClientLooksUpRepositoryAndSendsToken -shuffle=on
```

Expected: package does not exist.

- [ ] **Step 4: Implement `client.go` skeleton**

Implement:

```go
package forgejo

type ClientOption func(*clientOptions)

type clientOptions struct {
	baseURL           string
	foregroundTimeout time.Duration
	rateTracker       *ratelimit.RateTracker
}

type Client struct {
	host              string
	baseURL           string
	api               *gitea.Client
	foregroundTimeout time.Duration
}
```

`NewClient(host, token string, options ...ClientOption) (*Client, error)` should default `baseURL` to `https://` plus the normalized host, use `gitea.NewClient(baseURL, gitea.SetToken(token), gitea.SetUserAgent("middleman"))`, and attach `gitea.SetHTTPClient` when rate tracking is configured.

- [ ] **Step 5: Implement provider identity and capabilities**

Implement:

```go
func (c *Client) Platform() platform.Kind { return platform.KindForgejo }
func (c *Client) Host() string { return c.host }
func (c *Client) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		ReadRepositories:  true,
		ReadMergeRequests: true,
		ReadIssues:        true,
		ReadComments:      true,
		ReadReleases:      true,
		ReadCI:            true,
	}
}
```

Leave mutation capabilities false until Task 7.

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/platform/forgejo -shuffle=on
```

Expected: pass for construction and capability tests.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/platform/forgejo/client.go internal/platform/forgejo/client_test.go
git commit -m "feat: add Forgejo provider client skeleton" -m "Introduces the Gitea SDK-backed Forgejo provider with host-scoped auth, rate-tracking hooks, and read capability metadata."
```

## Task 3: Forgejo Normalization

**Files:**
- Create: `internal/platform/forgejo/normalize.go`
- Create: `internal/platform/forgejo/normalize_test.go`

- [ ] **Step 1: Write normalization tests**

Cover these SDK fields:

- `Repository.ID`, `Repository.Owner.UserName`, `Repository.Name`, `Repository.FullName`, `Repository.HTMLURL`, `Repository.CloneURL`, `Repository.DefaultBranch`, `Repository.Private`, `Repository.Archived`, `Repository.Description`, `Repository.Created`, `Repository.Updated`
- `PullRequest.ID`, `PullRequest.Index`, `PullRequest.HTMLURL`, `PullRequest.Title`, `PullRequest.User.UserName`, `PullRequest.State`, `PullRequest.IsLocked`, `PullRequest.Body`, `PullRequest.Head.Ref`, `PullRequest.Head.Sha`, `PullRequest.Base.Ref`, `PullRequest.Base.Sha`, `PullRequest.Labels`, `PullRequest.Created`, `PullRequest.Updated`, `PullRequest.Merged`, `PullRequest.MergedAt`, `PullRequest.Closed`
- `Issue.ID`, `Issue.Index`, `Issue.HTMLURL`, `Issue.Title`, `Issue.User.UserName`, `Issue.State`, `Issue.Body`, `Issue.Comments`, `Issue.Labels`, `Issue.Created`, `Issue.Updated`, `Issue.Closed`
- `Comment.ID`, `Comment.User.UserName`, `Comment.Body`, `Comment.Created`
- `Release.ID`, `Release.TagName`, `Release.Title`, `Release.HTMLURL`, `Release.Target`, `Release.Prerelease`, `Release.PublishedAt`, `Release.CreatedAt`
- `Tag.Name`, `Tag.Commit.SHA`
- `Status.ID`, `Status.Context`, `Status.State`, `Status.TargetURL`, `Status.Description`, `Status.Created`, `Status.Updated`

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./internal/platform/forgejo -run 'TestNormalize' -shuffle=on
```

Expected: fails because normalizers are missing.

- [ ] **Step 3: Implement normalizers**

Implement:

```go
func NormalizeRepository(host string, repo *gitea.Repository) (platform.Repository, error)
func NormalizePullRequest(repo platform.RepoRef, pr *gitea.PullRequest) platform.MergeRequest
func NormalizeIssue(repo platform.RepoRef, issue *gitea.Issue) platform.Issue
func NormalizeIssueComments(repo platform.RepoRef, number int, comments []*gitea.Comment) []platform.IssueEvent
func NormalizeMergeRequestComments(repo platform.RepoRef, number int, comments []*gitea.Comment) []platform.MergeRequestEvent
func NormalizeRelease(repo platform.RepoRef, release *gitea.Release) platform.Release
func NormalizeTag(repo platform.RepoRef, tag *gitea.Tag) platform.Tag
func NormalizeStatuses(repo platform.RepoRef, statuses []*gitea.Status) []platform.CICheck
```

Use `platform.KindForgejo`, `host`, `owner/name`, and `owner/name` as `RepoPath`. Use `strconv.FormatInt(id, 10)` for `PlatformExternalID` when the API only provides numeric IDs. Convert `open` and `closed` states directly; map merged pull requests to `"merged"` when the SDK exposes merged state or merged timestamps.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/platform/forgejo -shuffle=on
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/forgejo/normalize.go internal/platform/forgejo/normalize_test.go
git commit -m "feat: normalize Forgejo API records" -m "Maps Forgejo repositories, pulls, issues, comments, releases, tags, and commit statuses into the provider-neutral platform models."
```

## Task 4: Forgejo Read APIs, Pagination, And Error Mapping

**Files:**
- Modify: `internal/platform/forgejo/client.go`
- Modify: `internal/platform/forgejo/client_test.go`

- [ ] **Step 1: Write failing read API tests**

Add `httptest` cases for:

- `GetRepository` calls `GetRepo(owner, repo)`.
- `ListRepositories` tries user repositories first and organization repositories second, returning only repos matching the requested owner.
- `ListOpenMergeRequests` calls `ListRepoPullRequests` with open state and paginates until `Response.NextPage == 0`.
- `GetMergeRequest` calls `GetPullRequest`.
- `ListMergeRequestEvents` combines `ListRepoIssueComments`, `ListPullReviews`, and `ListPullRequestCommits` where available.
- `ListOpenIssues` calls `ListRepoIssues` with open state and excludes records that are pull requests if the SDK marks them as pull requests.
- `GetIssue` calls `GetIssue`.
- `ListIssueEvents` calls issue comments and maps them to issue events.
- `ListReleases`, `ListTags`, and `ListCIChecks` paginate.
- HTTP 404 maps to `platform.ErrRepoNotFound` or a typed provider error equivalent used elsewhere.

- [ ] **Step 2: Run focused failing tests**

Run:

```bash
go test ./internal/platform/forgejo -run 'TestClient' -shuffle=on
```

Expected: fails for unimplemented provider methods.

- [ ] **Step 3: Implement pagination helpers**

Add a local helper pattern matching GitLab:

```go
const defaultPageSize = 100

func nextPage(resp *gitea.Response) int {
	if resp == nil {
		return 0
	}
	return resp.NextPage
}
```

Every list call should start with `gitea.ListOptions{Page: 1, PageSize: defaultPageSize}` or the SDK's current equivalent. Update the exact option field names after `go test` compiles against `v0.25.0`.

- [ ] **Step 4: Implement read methods**

Implement all read interfaces from `internal/platform/client.go`. Keep unsupported optional data as empty slices, not errors, when the SDK or Forgejo returns 404 for a feature that does not exist on a repository.

- [ ] **Step 5: Run provider tests**

Run:

```bash
go test ./internal/platform/forgejo -shuffle=on
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/forgejo/client.go internal/platform/forgejo/client_test.go
git commit -m "feat: read Forgejo repositories and pull requests" -m "Completes the read-side Forgejo provider with pagination, issue/comment sync, releases, tags, and commit status normalization."
```

## Task 5: Startup, Registry, Settings, And UI Provider List

**Files:**
- Modify: `cmd/middleman/provider_startup.go`
- Modify: `cmd/middleman/main_test.go`
- Modify: `internal/server/settings_test.go`
- Modify: `frontend/src/lib/components/settings/repoImportProviders.ts`
- Modify: `frontend/src/lib/components/settings/repoImportSelection.test.ts`
- Modify: `frontend/src/lib/components/settings/RepoImportModal.test.ts`

- [ ] **Step 1: Write startup factory test**

Extend `TestBuildProviderStartupUsesRegisteredFactoryForFutureProvider` or add:

```go
func TestBuildProviderStartupRegistersForgejoFactory(t *testing.T) {
	// Configure a Forgejo platform with token env and assert the registry has
	// provider capabilities under (forgejo, codeberg.org).
}
```

Use a fake factory if the test should not instantiate a real SDK client.

- [ ] **Step 2: Run focused startup tests**

Run:

```bash
go test ./cmd/middleman -run 'TestBuildProviderStartup|TestResolveStartupRepos' -shuffle=on
```

Expected: fails until factory is registered.

- [ ] **Step 3: Wire default factory**

Import `github.com/wesm/middleman/internal/platform/forgejo` as `forgejoclient` and add a factory under `platform.KindForgejo` that calls:

```go
forgejoclient.NewClient(
	input.host,
	input.token,
	forgejoclient.WithRateTracker(input.rateTracker),
)
```

- [ ] **Step 4: Add UI import provider**

Add to `repoImportProviders`:

```ts
{
  id: "forgejo",
  label: "Forgejo",
  defaultHost: "codeberg.org",
  allowNestedOwner: false,
  ownerPatternPlaceholder: "owner/pattern",
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./cmd/middleman ./internal/server -run 'TestBuildProviderStartup|TestHandlePreviewRepos|TestHandleBulkAddRepos' -shuffle=on
bun test frontend/src/lib/components/settings/repoImportSelection.test.ts frontend/src/lib/components/settings/RepoImportModal.test.ts
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/middleman/provider_startup.go cmd/middleman/main_test.go internal/server/settings_test.go frontend/src/lib/components/settings/repoImportProviders.ts frontend/src/lib/components/settings/repoImportSelection.test.ts frontend/src/lib/components/settings/RepoImportModal.test.ts
git commit -m "feat: wire Forgejo provider startup" -m "Registers Forgejo with the provider registry and exposes it in repository import settings."
```

## Task 6: Optional Forgejo Container E2E Fixture

**Files:**
- Create: `scripts/e2e/forgejo/docker-compose.yml`
- Create: `scripts/e2e/forgejo/wait.sh`
- Create: `scripts/e2e/forgejo/bootstrap.sh`
- Create: `scripts/e2e/forgejo/README.md`
- Create: `internal/server/forgejo_container_e2e_test.go`

- [ ] **Step 1: Write gated e2e test shell**

Create `internal/server/forgejo_container_e2e_test.go` with:

```go
if os.Getenv("MIDDLEMAN_FORGEJO_CONTAINER_TESTS") != "1" {
	t.Skip("set MIDDLEMAN_FORGEJO_CONTAINER_TESTS=1 to run Forgejo container e2e")
}
```

The test should start the compose stack, run the bootstrap script, load the manifest, instantiate `platform/forgejo.NewClient` with the manifest API URL, sync one configured repo through Middleman, and assert one pull request, one issue, one release, one tag, and one CI check are persisted.

- [ ] **Step 2: Add compose fixture**

Use:

```yaml
services:
  forgejo:
    image: ${MIDDLEMAN_FORGEJO_IMAGE:-codeberg.org/forgejo/forgejo:13}
    ports:
      - "127.0.0.1:${FORGEJO_HTTP_PORT:-13000}:3000"
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
      FORGEJO__server__ROOT_URL: "http://localhost:3000/"
      FORGEJO__security__INSTALL_LOCK: "true"
      FORGEJO__service__DISABLE_REGISTRATION: "true"
      FORGEJO__database__DB_TYPE: "sqlite3"
    volumes:
      - forgejo-data:/data

volumes:
  forgejo-data:
```

Adjust env names if the image requires app.ini injection for the pinned version.

- [ ] **Step 3: Add bootstrap**

`bootstrap.sh` should create an admin token or use a pre-seeded admin account, then idempotently create:

- owner `middleman-fixture`
- repo `project-special`
- branch `feature/forgejo`
- file commit on feature branch
- pull request from feature branch to main
- issue with label
- comments on both PR and issue
- tag `v1.0.0`
- release for `v1.0.0`
- commit status for the feature SHA

Write a manifest JSON with `base_url`, `api_url`, `token`, `owner`, `repo`, `repo_path`, `pull_number`, `issue_number`, and `head_sha`.

- [ ] **Step 4: Run optional e2e locally**

Run:

```bash
MIDDLEMAN_FORGEJO_CONTAINER_TESTS=1 go test ./internal/server -run TestForgejoContainerSync -shuffle=on
```

Expected: pass on machines with Docker available. The default test suite must still skip this test.

- [ ] **Step 5: Commit**

```bash
git add scripts/e2e/forgejo internal/server/forgejo_container_e2e_test.go
git commit -m "test: cover Forgejo sync against a real instance" -m "Adds an optional Forgejo container fixture so provider API compatibility can be validated outside the default fast suite."
```

## Task 7: Mutations And Capability Gating

**Files:**
- Modify: `internal/platform/forgejo/client.go`
- Modify: `internal/platform/forgejo/client_test.go`
- Modify: `internal/server/api_test.go`
- Modify: `frontend/tests/e2e-full/provider-capabilities.spec.ts`

- [ ] **Step 1: Prove supported mutation endpoints**

Add `httptest` coverage for SDK calls equivalent to:

- `CreateIssueComment` for PR comments and issue comments.
- `EditIssueComment` for editable comments.
- `EditIssue` for close/reopen issue state.
- `EditPullRequest` for close/reopen pull request state and title/body edits.
- `CreatePullReview` and submit review for approval if Forgejo accepts approval reviews.
- `MergePullRequest` for merge support.

- [ ] **Step 2: Implement only proven mutators**

Set capability flags only for methods implemented and tested against both `httptest` and the optional container fixture. If approval or ready-for-review does not map cleanly, leave `ReviewMutation` or `ReadyForReview` false.

- [ ] **Step 3: Add server capability tests**

Extend provider capability tests to assert Forgejo-supported actions are visible and unsupported actions remain hidden or return typed capability errors.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/platform/forgejo ./internal/server -run 'Test.*Forgejo|Test.*Capability|Test.*Comment|Test.*Merge' -shuffle=on
bun test frontend/tests/e2e-full/provider-capabilities.spec.ts
```

Expected: pass. If the Playwright command needs the dev server, use the repo's existing e2e-full invocation instead of a standalone file run.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/forgejo/client.go internal/platform/forgejo/client_test.go internal/server/api_test.go frontend/tests/e2e-full/provider-capabilities.spec.ts
git commit -m "feat: enable supported Forgejo actions" -m "Adds Forgejo mutations only for API operations validated by provider tests and keeps unsupported actions behind typed capability errors."
```

## Task 8: Documentation And Final Verification

**Files:**
- Modify: `README.md`
- Modify: `config.example.toml`
- Modify: `context/platform-sync-invariants.md`
- Modify: `frontend/openapi/openapi.yaml` only if generated API schemas changed
- Modify: `internal/apiclient/generated/client.gen.go` only if generated API schemas changed
- Modify: `packages/ui/src/api/generated/*` only if generated API schemas changed

- [ ] **Step 1: Document config**

Add:

```toml
[[platforms]]
type = "forgejo"
host = "codeberg.org"
token_env = "MIDDLEMAN_FORGEJO_TOKEN"

[[repos]]
platform = "forgejo"
platform_host = "codeberg.org"
owner = "forgejo"
name = "forgejo"
repo_path = "forgejo/forgejo"
```

Document minimum read scopes:

```text
read:repository, read:issue
```

and mutation scopes:

```text
write:repository and/or write:issue, depending on the enabled action
```

- [ ] **Step 2: Regenerate API only if needed**

If any Huma schema or route type changed, run:

```bash
make api-generate
```

Expected: generated Go and TypeScript clients update consistently. If only provider metadata changed, skip this step and state why in the commit body.

- [ ] **Step 3: Run focused verification**

Run:

```bash
go test ./internal/platform ./internal/platform/forgejo ./internal/config ./cmd/middleman ./internal/server -shuffle=on
bun test frontend/src/lib/components/settings/repoImportSelection.test.ts frontend/src/lib/components/settings/RepoImportModal.test.ts
```

Expected: pass.

- [ ] **Step 4: Run broader verification**

Run:

```bash
make test-short
```

Expected: pass.

If visible UI changed beyond the provider dropdown, run the affected Playwright e2e-full suite before pushing, per repository instructions.

- [ ] **Step 5: Commit**

```bash
git add README.md config.example.toml context/platform-sync-invariants.md frontend/openapi/openapi.yaml internal/apiclient/generated/client.gen.go packages/ui/src/api/generated
git commit -m "docs: describe Forgejo provider setup" -m "Documents Forgejo token scopes, Codeberg defaults, self-hosted configuration, and the verified capability set."
```

## Open Questions To Resolve During Implementation

- Does Forgejo `v13` accept `SetToken` as `Authorization: token ...`, or should Middleman wrap the SDK transport to send `Authorization: Bearer ...`? Lock this down in Task 2's auth header test.
- Does the SDK expose draft status on pull requests in a stable field for Forgejo, or must `NormalizePullRequest` infer draft from title prefixes like `WIP:` and `Draft:`?
- Do Forgejo issue list responses include pull requests, and if so which SDK field reliably distinguishes them? Task 4 must prevent PRs from duplicating into the issue list.
- Does Forgejo return useful commit statuses from `ListStatuses` for Actions runs, or should the first release show only external statuses and treat Actions job detail as follow-up work?
- Which mutation capabilities are acceptable in the first implementation? The safe default is read-only plus comments; merge/review/state mutations should wait for container proof.

## Final Test Matrix

- `go test ./internal/platform ./internal/platform/forgejo -shuffle=on`
- `go test ./internal/config -shuffle=on`
- `go test ./cmd/middleman -shuffle=on`
- `go test ./internal/server -run 'Test.*Forgejo|Test.*Provider|Test.*Import|Test.*Capability' -shuffle=on`
- `bun test frontend/src/lib/components/settings/repoImportSelection.test.ts frontend/src/lib/components/settings/RepoImportModal.test.ts`
- `make test-short`
- Optional: `MIDDLEMAN_FORGEJO_CONTAINER_TESTS=1 go test ./internal/server -run TestForgejoContainerSync -shuffle=on`

## Handoff Notes

- The branch for this stack is `forgejo-provider-impl`.
- Git-spice tracks this branch on local `main`. Local `main` is currently behind `origin/main` in another worktree, so do not restack until the local trunk situation is intentionally resolved.
- Do not prefix follow-up branch names with `codex/`.
