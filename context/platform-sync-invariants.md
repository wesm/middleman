# Platform Sync Invariants

Use this document for changes that touch provider-aware repository identity,
sync, import, server routes, settings, or API responses.

## Identity

Repository identity is `(platform, platform_host, owner, name)`.

- `platform` is the provider kind, such as `github` or `gitlab`.
- `platform_host` is the normalized host for that provider. Preserve ports.
- `owner` and `name` are provider-canonical display/config fields.
- `repo_path` carries the full provider path when `owner/name` is not enough.

GitLab nested namespaces make `repo_path` mandatory for reliable addressing:
`group/subgroup/project` has owner `group/subgroup` and name `project`.
GitHub repositories can continue to omit `repo_path` when the path is exactly
`owner/name`.

Do not identify repos, merge requests, issues, events, stars, workspaces, or
activity rows by owner/name/number alone. Thread the full provider ref through
requests, sync queues, persistence, and responses.

## Provider Hosts And Tokens

Each configured provider host may have its own token env var.

- Legacy GitHub config still defaults to `github` on `github.com`.
- GitLab public config defaults to `gitlab.com`.
- Self-hosted hosts are hostnames with optional ports, not URL paths.
- A missing token should fail only the provider host that needs it.

Provider clients must be registered by `(platform, platform_host)`. A third
provider should add a provider implementation and config metadata; it should not
need to masquerade as GitHub or GitLab.

## Sync Capabilities

Middleman reads repositories, merge requests, issues, releases, tags, CI, and
timeline/comment-like events through provider capability interfaces.

- Missing optional capabilities should degrade that feature with a typed
  platform error, not break unrelated sync work.
- Mutation routes must check provider capabilities before posting comments,
  changing state, merging, requesting review, or approving workflows.
- GitHub GraphQL bulk fetch, ETag recovery, and detailed diff behavior are
  GitHub-only optimizations. Keep them optional around the neutral persistence
  path.
- Provider-supplied web URLs, clone URLs, default branches, platform ids, and
  external ids should be persisted when available instead of reconstructed from
  host/owner/name.

## GitLab Shape

GitLab API calls address projects by numeric id or URL-escaped path with
slashes. Middleman should prefer the stored provider id after resolution and
preserve `path_with_namespace` as `repo_path`.

GitLab merge request and issue `iid` values are repo-scoped numbers. Persist
provider object ids separately from user-visible numbers, and scope events by
provider identity so equal GitHub/GitLab ids do not collide.

## Import And Routes

Repository import requests and route/query shapes should carry
`provider`, `platform_host`, and either `repo_path` or exact `owner/name`.

- Legacy GitHub owner/name requests may default provider and host only when the
  route is explicitly a GitHub compatibility path.
- New provider-aware routes should not require clients to split nested paths.
- Frontend route state must encode slashes, host ports, mixed case, and special
  characters exactly once.

## Testing

Provider work should be covered at the boundary where a regression would show:

- provider package tests for API normalization and capability errors;
- sync tests for full provider refs, optional capability behavior, and DB
  identity scoping;
- server e2e tests with real SQLite for API payloads and settings/import flows;
- optional live/container tests for provider API compatibility when fakes are
  too weak to catch endpoint or auth drift.

Run Go tests with `-shuffle=on`. Use the GitLab CE container fixture for
changes that need real GitLab REST behavior.
