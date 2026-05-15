# Existing Label Editing Design

## Context

Issue #315 asks for a way to add labels to a PR or issue from Middleman. Labels are already synced, normalized, stored in `middleman_labels`, associated to PRs/issues through join tables, and displayed in list/detail UI through `GitHubLabels`. Missing piece is provider mutation plus a complete repository label catalog for picking labels that are not currently attached to any synced item.

Middleman now supports multiple providers through provider-aware routes and neutral capability interfaces. Label editing must keep that shape: no GitHub-only routes, no owner/name-only identity, and no provider-specific URL construction in frontend code.

## Goals

- Let users edit existing repository labels on PRs and issues from desktop detail views.
- Use a GitHub-style picker: searchable list, checked assigned labels, click toggles immediately.
- Add a command palette action that opens the same picker on PR/issue detail routes.
- Support GitHub, GitLab, Forgejo, and Gitea when their APIs support the required operations.
- Keep label catalog refresh in the standard background sync path.
- Use conditional requests such as ETag/`304 Not Modified` where provider APIs expose them, especially GitHub, to reduce API budget and token consumption.
- Keep mobile label UI read-only for this iteration.

## Non-goals

- Creating, editing, or deleting repository labels.
- Label editing from mobile/focused phone routes.
- List/sidebar quick editing.
- Command palette target selection across arbitrary PRs/issues.
- New GitHub-only compatibility routes.

## Recommended approach

Add provider-neutral label catalog and label mutation capabilities, backed by provider-specific implementations. Background repo sync refreshes the full repo label catalog and stores freshness metadata. Detail UI reads cached catalog, opens immediately, and sends full desired label names on each toggle.

This keeps UI responsive, aligns provider behavior around one replace-label contract, and gives background sync control over rate limits and conditional fetches.

## Provider capabilities

Add neutral label types and interfaces in `internal/platform`:

```go
type LabelCatalog struct {
    Labels      []Label
    NotModified bool
}

type LabelReader interface {
    ListLabels(ctx context.Context, ref RepoRef) (LabelCatalog, error)
}

type LabelMutator interface {
    SetMergeRequestLabels(ctx context.Context, ref RepoRef, number int, names []string) ([]Label, error)
    SetIssueLabels(ctx context.Context, ref RepoRef, number int, names []string) ([]Label, error)
}
```

`LabelCatalog.NotModified` is the provider-neutral conditional-fetch signal. Provider implementations can arrive there through existing in-memory ETag transports, explicit validator headers, or SDK-specific `304` errors. The sync layer does not need provider SDK details; it handles `NotModified` the same for every provider.

Add two capabilities to `platform.Capabilities` and `providerCapabilitiesResponse`:

- `ReadLabels`: provider can list repository labels for catalog sync and picker data.
- `LabelMutation`: provider can replace labels on issues and merge requests.

Capability flags and implemented interfaces must agree. `GET repo labels` requires `ReadLabels`; mutation routes require both cached catalog data and `LabelMutation`.

Provider implementation notes:

- GitHub: list repo labels through REST. Replace labels through Issues API for both issues and PRs, because GitHub PR labels are issue labels. Extend existing ETag handling to include repo label list endpoints.
- GitLab: list project labels. Replace labels on issues and merge requests using GitLab's label fields/add/remove semantics to produce the desired final label set.
- Forgejo/Gitea: list repo labels and replace issue labels. PR labels use issue-label endpoints where the API models PRs as issues.

If a provider host lacks catalog read support, expose `read_labels=false` and make `GET repo labels` return a typed unsupported capability problem. If it can read labels but cannot mutate, expose `label_mutation=false`; UI shows the disabled label button and mutation routes return unsupported capability.

## Storage and sync

Reuse `middleman_labels` as the repository label table, but distinguish selectable catalog membership from historical/assigned label rows.

Add label-level catalog fields:

- `catalog_present BOOLEAN NOT NULL DEFAULT 0`: label was present in the most recent successful catalog payload for the repo.
- `catalog_seen_at TEXT`: UTC time this label was last seen in a successful catalog payload.

Existing PR/issue join tables remain the assigned-label source for each item. Catalog refresh must never delete label rows that still have assigned-label joins. A successful full catalog refresh for a repo uses this algorithm in one transaction:

1. Mark all labels for the repo `catalog_present=false`.
2. Upsert returned provider labels by provider id/external id/name.
3. Set returned labels `catalog_present=true`, update metadata, and set `catalog_seen_at`.
4. Leave assigned-label joins untouched.

Picker catalog endpoints return only `catalog_present=true` labels. Historical labels may still display on existing PR/issue cards until item sync removes their joins, but they are not selectable and mutation validation rejects them.

Add repo-level catalog freshness fields on repository rows:

- `label_catalog_synced_at`: last successful catalog payload update or confirmed no-change check.
- `label_catalog_checked_at`: last attempted conditional/background check.
- `label_catalog_sync_error`: last catalog-specific sync error.

Catalog staleness is explicit: catalog is stale when `label_catalog_checked_at` is null or older than 10 minutes. A never-synced repo returns cached labels (usually empty), `stale=true`, and enqueues refresh.

Background repo sync adds a label-catalog step after repo identity is known. The step:

1. Looks up a `LabelReader` for the repo provider/host.
2. Performs a conditional list request when provider transport supports it.
3. On `NotModified`, leaves labels unchanged, clears catalog error, and updates `label_catalog_checked_at` and `label_catalog_synced_at`.
4. On changed payload, applies the catalog transaction above, clears catalog error, and updates freshness/check metadata.
5. On provider/catalog failure, records `label_catalog_sync_error` and `label_catalog_checked_at`, but does not fail PR/issue sync.

Foreground `GET repo labels` does not fetch provider labels directly. If cache is stale, it enqueues a label refresh through an in-process single-flight/dedup key scoped by repo ID. `syncing=true` means a label refresh is currently queued or running for that repo.

## API

Use existing provider-aware route shape and host-prefixed variants. Operation IDs should be stable for generated clients:

Default-host routes:

- `GET /api/v1/repo/{provider}/{owner}/{name}/labels` (`list-repo-labels`)
- `PUT /api/v1/pulls/{provider}/{owner}/{name}/{number}/labels` (`set-pr-labels`)
- `PUT /api/v1/issues/{provider}/{owner}/{name}/{number}/labels` (`set-issue-labels`)

Non-default host routes:

- `GET /api/v1/host/{platform_host}/repo/{provider}/{owner}/{name}/labels` (`list-repo-labels-on-host`)
- `PUT /api/v1/host/{platform_host}/pulls/{provider}/{owner}/{name}/{number}/labels` (`set-pr-labels-on-host`)
- `PUT /api/v1/host/{platform_host}/issues/{provider}/{owner}/{name}/{number}/labels` (`set-issue-labels-on-host`)

`GET` response returns cached catalog plus freshness state:

```json
{
  "labels": [
    { "name": "bug", "description": "Something is broken", "color": "d73a4a", "is_default": true }
  ],
  "stale": false,
  "syncing": false,
  "synced_at": "2026-05-15T12:00:00Z",
  "checked_at": "2026-05-15T12:00:00Z",
  "sync_error": ""
}
```

If catalog is stale, endpoint enqueues a repo label refresh through the existing sync path and still returns cached labels immediately. UI shows a small spinner when `syncing=true`. If `sync_error` is non-empty, the picker can still show cached labels and display the error only when useful, for example when no catalog labels are available.

`PUT` body:

```json
{ "labels": ["bug", "triage"] }
```

Server behavior:

1. Resolve repo through provider-aware lookup.
2. Check `LabelMutation` capability.
3. Normalize requested names by trimming whitespace, reject empty names, and reject duplicate names after exact string comparison.
4. Validate requested names against `catalog_present=true` labels in the cached repo catalog. Label name matching is exact and provider-case-sensitive in v1.
5. Call provider `LabelMutator` with the full desired label name set.
6. Persist returned labels through existing replace-label DB functions for the item.
7. Return updated PR or issue response/detail shape with current labels.

The UI does not block on full detail reload after mutation. It updates labels from the mutation response.

## Frontend UX

Desktop PR and issue detail headers add a small `Labels` `ActionButton` near existing detail actions/metadata. Existing label display remains visible.

Button states:

- Enabled when `label_mutation=true` and detail item is not stale relative to route props.
- Disabled when mutation is unavailable, with accessible explanatory text.
- Hidden or omitted from mobile-specific routes for this iteration.

Picker behavior:

- Shared `LabelPicker` component used by PR detail, issue detail, and command palette action.
- Popover/dropdown style matching GitHub label picker.
- Search input filters by label name and description.
- Each row shows color chip, name, description, and checkmark when assigned.
- Clicking a row computes the desired full label name set and immediately calls the replace-label API.
- Row/control is pending while request is in flight.
- On success, assigned labels update from API response.
- On error, selection rolls back and inline error displays provider/server message.
- If stale catalog refresh is enqueued, show a small spinner only; no explanatory text.

Command palette:

- Add action `Edit labels`.
- Only available on current PR or issue detail routes.
- Opens the same picker for the visible item.
- Does not choose arbitrary target PRs/issues in v1.

Frontend requests must use `providerRepoPath`, `providerItemPath`, and `providerRouteParams`. Do not hand-build API URLs or assume GitHub defaults.

## Forgejo-first e2e strategy

Implementation should explore the full flow against real Forgejo first, using existing docker-compose fixtures under `scripts/e2e/forgejo/` and shared gitealike bootstrap code.

Initial integration path:

1. Extend `scripts/e2e/gitealike/bootstrap.py` to seed repository labels and attach labels to the seeded PR and issue.
2. Add Forgejo e2e coverage for repo label catalog sync.
3. Add Forgejo e2e coverage for replacing issue labels and PR labels against the real provider.
4. Wire UI e2e once backend catalog and mutation flow is proven.
5. Port same provider coverage to Gitea.
6. Port same provider coverage to GitLab container fixture.
7. Keep GitHub covered by unit/httptest first, with optional live validation only if API behavior cannot be proven by fakes.

This keeps the first implementation loop grounded in a real provider API while avoiding GitHub token/API budget during early UI iteration.

## Error handling

- Missing provider/client: existing provider route lookup errors.
- Unsupported label catalog read: typed unsupported capability problem from `GET repo labels`.
- Unsupported label mutation: typed unsupported capability problem; frontend disabled button should prevent normal clicks.
- Requested unknown, non-catalog, duplicate, or empty label: `400 Bad Request` with label name or validation reason in message.
- Provider rejects label set: `502 Bad Gateway` with provider API error message shape consistent with existing mutation routes.
- Catalog sync failure: recorded on repo label freshness metadata and returned as `sync_error` by `GET repo labels`; it does not fail regular PR/issue sync.
- Mutation succeeds but DB persist fails: return `500`; UI rolls back optimistic state.
- Route/detail drift: mutation handlers must short-circuit like existing PR/issue state handlers when visible detail does not match current route props.

## Testing plan

Backend/unit:

- DB tests for catalog upsert, `catalog_present` handling, freshness metadata, and assigned-label joins surviving catalog refresh.
- DB tests proving deleted provider labels stop appearing in catalog results without deleting historical assigned-label joins.
- Provider tests for label list/set request shape and response normalization.
- GitHub tests for ETag/conditional label list behavior and `NotModified` handling.
- Registry/capability tests ensuring `ReadLabels` and `LabelMutation` match implemented interfaces.

Server e2e with real SQLite:

- `GET repo labels` returns cached catalog, freshness fields, and catalog sync errors.
- Stale catalog request enqueues a deduped refresh without blocking response and reports `syncing=true` while queued/running.
- Unsupported read-label capability returns expected problem response.
- `PUT PR labels` validates names, calls mutator, persists returned labels, and returns updated labels.
- `PUT issue labels` same.
- Unsupported provider/capability returns expected problem response.

Provider/container e2e:

- Forgejo first via docker-compose fixture.
- Gitea next via docker-compose fixture.
- GitLab after UI/backend flow stabilizes.

Frontend:

- `LabelPicker` search, toggle, pending state, and rollback behavior.
- PR detail header opens picker and updates labels from response.
- Issue detail header opens picker and updates labels from response.
- Disabled capability state is accessible.
- Command palette exposes `Edit labels` only on PR/issue detail routes.

Regeneration/verification:

- Run `make api-generate` after Huma route/API type changes.
- Run Go tests with `-shuffle=on`.
- Run focused frontend component tests.
- Run affected Playwright e2e after final UI/test edits.
