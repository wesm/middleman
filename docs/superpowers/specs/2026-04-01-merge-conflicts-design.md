# Merge Conflict Visibility

GitHub issue: #42

## Problem

When a PR has merge conflicts, attempting to merge from middleman shows a generic "GitHub merge error" with no explanation. There is also no proactive indication that a PR has conflicts before the user tries to merge.

## Solution

Capture the `mergeable_state` field from the GitHub API during sync, store it in the database, surface it in the PR list and detail views, and return actionable error messages when a merge fails.

## Data Layer

Add column to `pull_requests` table:

```sql
ALTER TABLE pull_requests ADD COLUMN mergeable_state TEXT NOT NULL DEFAULT '';
```

GitHub's documented `MergeableState` values (REST) / `MergeStateStatus` enum (GraphQL): `"unknown"`, `"clean"`, `"dirty"` (conflicts), `"unstable"`, `"blocked"`, `"behind"`, `"has_hooks"`, `"draft"`. We store whatever GitHub returns. The DB default `""` means "not yet fetched."

Add `MergeableState string` to `db.PullRequest`. Include in upsert and select queries.

Since `pullResponse` embeds `db.PullRequest` (which has no JSON tags, so Huma serializes with PascalCase), the API field name is `MergeableState`. Frontend TypeScript references use `pr.MergeableState`.

## Sync and Normalization

`NormalizePR()` captures `ghPR.GetMergeableState()`.

In `syncOpenPR()`, treat both `""` (never fetched) and `"unknown"` (GitHub still computing) as triggers for the full PR fetch, alongside the existing zero-diff-stats trigger:

```go
needsFullFetch := needsTimeline ||
    (existing != nil && existing.Additions == 0 && existing.Deletions == 0) ||
    (existing != nil && existing.MergeableState == "") ||
    (existing != nil && existing.MergeableState == "unknown")
```

The list endpoint does not return mergeable fields; the individual `GetPullRequest` call (already made during full fetch) does. If the full fetch still returns `"unknown"`, we store it as-is and retry on the next sync cycle.

## Merge Error Handling

In `mergePR()`, parse the error from `go-github` to detect merge-specific failures. GitHub's merge endpoint returns:

- **405**: "merge cannot be performed" (conflicts, branch protection, required checks, etc.)
- **409**: SHA mismatch

For 405 errors, use the cached `mergeable_state` to craft a context-specific message:

| `mergeable_state` | Error message |
|---|---|
| `"dirty"` | "This pull request has merge conflicts that must be resolved" |
| `"blocked"` | "Branch protection rules prevent this merge" |
| `"behind"` | "This branch is behind the base branch and must be updated" |
| `"unstable"` | "Required status checks have not passed" |
| other / `""` | "GitHub rejected the merge request" (with the raw error detail) |

Return these as HTTP 409 (conflict). For non-405 GitHub errors (network, auth), return HTTP 502.

After any failed merge attempt, trigger `syncer.SyncPR()` to refresh the cached `mergeable_state`.

## Frontend: PR List

Show a conflict indicator on PRs where `MergeableState === "dirty"`:

- Small icon or colored indicator inline with existing PR metadata (CI status, review state).
- Tooltip: "Has merge conflicts".
- Subtle treatment, similar to CI failure indicators.

## Frontend: PR Detail

When `MergeableState === "dirty"`, show a warning banner near the merge button area:

- Yellow/orange background.
- Text: "This branch has conflicts that must be resolved before merging".
- Link: "View on GitHub" pointing to the PR URL where the user can see conflict details and use GitHub's resolution UI.
- Disable the merge button.

When `MergeableState` is `"blocked"`, `"behind"`, or `"unstable"`, show an appropriate message but do not disable the merge button (these are advisory; GitHub may still allow the merge depending on repo settings).

## Frontend: Merge Modal

No changes needed to the modal itself. If opened with stale state (conflicts resolved between sync cycles), the merge attempt succeeds normally. If conflicts exist, the API returns the 409 error which displays in the existing error area with the context-specific message.

## Testing

- **DB migration**: Verify ALTER TABLE adds column, existing rows get default `""`.
- **Normalization**: Verify `NormalizePR()` captures all documented `MergeableState` values.
- **Sync full-fetch trigger**: Verify `""` and `"unknown"` both trigger a full fetch; `"clean"`, `"dirty"`, etc. do not.
- **Merge error classification**: Verify 405 with each cached `mergeable_state` produces the correct HTTP status and message. Verify non-405 errors return 502.
- **API response**: Verify `MergeableState` appears in both list and detail endpoints.
- **Frontend banner**: Verify `"dirty"` shows conflict warning with disabled merge button; `"blocked"`/`"behind"`/`"unstable"` show advisory messages with enabled merge button; `"clean"`/`""` show no banner.
- **Frontend list indicator**: Verify conflict icon appears for `"dirty"` PRs only.

## Files Changed

| File | Change |
|------|--------|
| `internal/db/db.go` | ALTER TABLE migration |
| `internal/db/types.go` | Add `MergeableState` field |
| `internal/db/queries.go` | Include in upsert/select |
| `internal/github/normalize.go` | Capture `GetMergeableState()` |
| `internal/github/sync.go` | Full-fetch trigger for empty/unknown mergeable state |
| `internal/server/huma_routes.go` | Parse merge errors with context-specific messages, trigger re-sync |
| `frontend/openapi/openapi.json` | Regenerated (new field) |
| `frontend/src/lib/api/generated/` | Regenerated |
| `frontend/src/lib/components/detail/PullDetail.svelte` | Conflict banner, disable merge button |
| `frontend/src/lib/components/sidebar/PullItem.svelte` | Conflict indicator |
