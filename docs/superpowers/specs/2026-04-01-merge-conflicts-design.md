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

In `syncOpenPR()`, two changes:

**1. Preserve `MergeableState` when full fetch is skipped.** The list endpoint does not return mergeable fields. When the full fetch is skipped (PR unchanged), the `else if existing != nil` branch must preserve `MergeableState` from the existing row alongside `Additions` and `Deletions`:

```go
} else if existing != nil {
    // Preserve fields the list endpoint doesn't return
    normalized.Additions = existing.Additions
    normalized.Deletions = existing.Deletions
    normalized.MergeableState = existing.MergeableState
}
```

**2. Trigger full fetch for empty/unknown state.** Treat both `""` (never fetched) and `"unknown"` (GitHub still computing) as triggers for the full PR fetch, alongside the existing zero-diff-stats trigger:

```go
needsFullFetch := needsTimeline ||
    (existing != nil && existing.Additions == 0 && existing.Deletions == 0) ||
    (existing != nil && existing.MergeableState == "") ||
    (existing != nil && existing.MergeableState == "unknown")
```

The individual `GetPullRequest` call (already made during full fetch) returns mergeable fields. If the full fetch still returns `"unknown"`, we store it as-is and retry on the next sync cycle.

## Merge Error Handling

In `mergePR()`, parse the error from `go-github` to detect merge-specific failures. GitHub's merge endpoint returns:

- **405**: "merge cannot be performed" (conflicts, branch protection, required checks, etc.)
- **409**: SHA mismatch

For 405/409 errors, extract the message from `go-github`'s `*github.ErrorResponse` and forward it as the error detail. Return HTTP 409 with the GitHub-provided message. This avoids relying on potentially stale cached `mergeable_state` to classify the error.

For non-405/409 GitHub errors (network, auth), return HTTP 502 with "GitHub merge error".

After any failed merge attempt, trigger `syncer.SyncPR()` in a goroutine with `context.WithoutCancel(ctx)` so it survives request completion (same pattern as the existing background syncs in `huma_routes.go` and `settings_handlers.go`). The frontend's next poll cycle picks up the updated state and renders the appropriate banner.

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
- The merge button remains enabled. The cached state may be stale (conflicts could have been resolved since last sync), so the user should always be able to attempt the merge. If the PR truly has conflicts, the API returns the 409 error with GitHub's message.

When `MergeableState` is `"blocked"`, `"behind"`, or `"unstable"`, show an appropriate informational message. The merge button stays enabled for all states.

## Frontend: Merge Modal

No changes needed to the modal itself. If opened with stale state (conflicts resolved between sync cycles), the merge attempt succeeds normally. If the merge fails, the API returns GitHub's error message which displays in the existing error area.

## Testing

- **DB migration**: Verify ALTER TABLE adds column, existing rows get default `""`.
- **Normalization**: Verify `NormalizePR()` captures all documented `MergeableState` values.
- **Sync preservation**: Verify `MergeableState` is preserved from the existing row when the full fetch is skipped (list-only path).
- **Sync full-fetch trigger**: Verify `""` and `"unknown"` both trigger a full fetch; `"clean"`, `"dirty"`, etc. do not.
- **Merge error classification**: Verify GitHub 405 and 409 both return HTTP 409 with GitHub's error message. Verify non-405/409 errors return 502.
- **API response**: Verify `MergeableState` appears in both list and detail endpoints.
- **Frontend banner**: Verify `"dirty"` shows conflict warning with "View on GitHub" link; `"blocked"`/`"behind"`/`"unstable"` show advisory messages; `"clean"`/`""` show no banner. Merge button enabled in all cases.
- **Frontend list indicator**: Verify conflict icon appears for `"dirty"` PRs only.

## Files Changed

| File | Change |
|------|--------|
| `internal/db/db.go` | ALTER TABLE migration |
| `internal/db/types.go` | Add `MergeableState` field |
| `internal/db/queries.go` | Include in upsert/select |
| `internal/github/normalize.go` | Capture `GetMergeableState()` |
| `internal/github/sync.go` | Preserve on skip, full-fetch trigger for empty/unknown |
| `internal/server/huma_routes.go` | Forward GitHub error messages, background re-sync |
| `internal/apiclient/spec/openapi.json` | Regenerated |
| `internal/apiclient/generated/` | Regenerated |
| `frontend/openapi/openapi.json` | Regenerated |
| `frontend/src/lib/api/generated/` | Regenerated |
| `frontend/src/lib/components/detail/PullDetail.svelte` | Conflict banner (merge button stays enabled) |
| `frontend/src/lib/components/sidebar/PullItem.svelte` | Conflict indicator |
