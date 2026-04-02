# Merge Conflict Visibility

GitHub issue: #42

## Problem

When a PR has merge conflicts, attempting to merge from middleman shows a generic "GitHub merge error" with no explanation. There is also no proactive indication that a PR has conflicts before the user tries to merge.

## Solution

Capture the `mergeable_state` field from the GitHub API during sync, store it in the database, surface it in the PR list and detail views, and return actionable error messages when a merge fails due to conflicts.

## Data Layer

Add column to `pull_requests` table:

```sql
ALTER TABLE pull_requests ADD COLUMN mergeable_state TEXT NOT NULL DEFAULT '';
```

Values: `""` (not yet computed), `"clean"`, `"dirty"` (conflicts), `"unstable"`, `"blocked"`, `"behind"`, `"draft"`.

Add `MergeableState string` to `db.PullRequest`. Include in upsert and select queries.

Since `pullResponse` embeds `db.PullRequest`, the field appears in all API responses automatically.

## Sync and Normalization

`NormalizePR()` captures `ghPR.GetMergeableState()`.

In `syncOpenPR()`, add empty mergeable state as a trigger for the full PR fetch (same pattern as zero diff stats):

```go
needsFullFetch := needsTimeline ||
    (existing != nil && existing.Additions == 0 && existing.Deletions == 0) ||
    (existing != nil && existing.MergeableState == "")
```

The list endpoint does not return mergeable fields; the individual `GetPullRequest` call (already made during full fetch) does.

## Merge Error Handling

In `mergePR()`, parse the error from `go-github` to detect merge-specific failures. The library returns `*github.ErrorResponse` with HTTP 405 for non-mergeable PRs.

- Return HTTP 409 with message "This pull request has merge conflicts that must be resolved before merging" for 405 responses from GitHub.
- Return HTTP 502 for other GitHub errors (network, auth, etc.).
- After a failed merge attempt, trigger `syncer.SyncPR()` to refresh the cached `mergeable_state`.

## Frontend: PR List

Show a conflict indicator on PRs where `mergeable_state === "dirty"`:

- Small icon or colored indicator inline with existing PR metadata (CI status, review state).
- Tooltip: "Has merge conflicts".
- Subtle treatment, similar to CI failure indicators.

## Frontend: PR Detail

When `mergeable_state === "dirty"`, show a warning banner near the merge button area:

- Yellow/orange background.
- Text: "This branch has conflicts that must be resolved before merging".
- Link: "View on GitHub" pointing to the PR URL where the user can see conflict details and use GitHub's resolution UI.
- Disable the merge button.

When `mergeable_state` is `"blocked"`, `"behind"`, or `"unstable"`, show an appropriate message but do not disable the merge button (these are advisory; GitHub may still allow the merge depending on repo settings).

## Frontend: Merge Modal

No changes needed to the modal itself. If opened with stale state (conflicts resolved between sync cycles), the merge attempt succeeds normally. If conflicts exist, the API returns the 409 error which displays in the existing error area with the conflict-specific message.

## Files Changed

| File | Change |
|------|--------|
| `internal/db/db.go` | ALTER TABLE migration |
| `internal/db/types.go` | Add `MergeableState` field |
| `internal/db/queries.go` | Include in upsert/select |
| `internal/github/normalize.go` | Capture `GetMergeableState()` |
| `internal/github/sync.go` | Full-fetch trigger for empty mergeable state |
| `internal/server/huma_routes.go` | Parse merge errors, trigger re-sync |
| `frontend/openapi/openapi.json` | Regenerated (new field) |
| `frontend/src/lib/api/generated/` | Regenerated |
| `frontend/src/lib/components/detail/PullDetail.svelte` | Conflict banner, disable merge button |
| `frontend/src/lib/components/sidebar/PullListItem.svelte` | Conflict indicator |
