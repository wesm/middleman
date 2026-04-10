# PR And Issue Labels Design

## Goal

Show GitHub labels for both pull requests and issues in middleman list rows and detail views, using GitHub label colors and a shared Svelte component to avoid duplicated rendering logic.

## Scope

- Add normalized, repo-scoped label storage in SQLite.
- Sync labels for both pull requests and issues from GitHub.
- Expose structured label arrays from the API instead of frontend-facing JSON blobs.
- Render labels in pull request and issue list rows.
- Render labels in pull request and issue detail views.
- Use one shared Svelte component for compact and full label rendering.

## Non-Goals

- Label filtering, searching, editing, or management UI.
- Global label deduplication across repositories.
- A separate label settings screen.

## Design Constraints

- Labels are configured per repository, so storage and joins must stay repo-scoped.
- The same label name may exist in multiple repos with different colors or descriptions.
- The frontend should not need to parse JSON label payloads inline in individual components.
- Rendering should look close to GitHub pills rather than generic badges.

## Architecture

### Database

Add a new normalized label model with association tables instead of storing label blobs on items.

New table:

- `middleman_labels`
  - `id INTEGER PRIMARY KEY AUTOINCREMENT`
  - `repo_id INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE`
  - `platform_id INTEGER NOT NULL`
  - `name TEXT NOT NULL DEFAULT ''`
  - `description TEXT NOT NULL DEFAULT ''`
  - `color TEXT NOT NULL DEFAULT ''`
  - `is_default INTEGER NOT NULL DEFAULT 0`
  - `updated_at DATETIME NOT NULL`
  - `UNIQUE(repo_id, platform_id)`
  - `UNIQUE(repo_id, name)`

Association tables:

- `middleman_merge_request_labels`
  - `merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE`
  - `label_id INTEGER NOT NULL REFERENCES middleman_labels(id) ON DELETE CASCADE`
  - `PRIMARY KEY(merge_request_id, label_id)`

- `middleman_issue_labels`
  - `issue_id INTEGER NOT NULL REFERENCES middleman_issues(id) ON DELETE CASCADE`
  - `label_id INTEGER NOT NULL REFERENCES middleman_labels(id) ON DELETE CASCADE`
  - `PRIMARY KEY(issue_id, label_id)`

Migration behavior:

- Create the new tables and supporting indexes.
- Backfill `middleman_issue_labels` and `middleman_labels` from existing `middleman_issues.labels_json` data so existing issue labels do not disappear after migration.
- Do not add any JSON label column to `middleman_merge_requests`.
- After the migration lands, application code stops reading and writing item-level JSON label blobs and uses the normalized tables exclusively.
- Keep the existing `middleman_issues.labels_json` column physically present for this change, but unused, to avoid a riskier table rebuild in the same feature branch.

This feature lands the normalized table design immediately for all runtime reads and writes, while limiting migration risk to additive schema changes plus issue-label backfill.

### Backend Domain Types

Add a shared label type in Go for API and query mapping, for example:

```go
type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color"`
	IsDefault   bool   `json:"is_default"`
}
```

Expose `Labels []Label` on:

- pull request list responses
- pull request detail responses
- issue list responses
- issue detail responses

The API contract should no longer require the frontend to read `LabelsJSON`.

### GitHub Sync

During sync for both pull requests and issues:

- collect labels returned by GitHub for the item
- upsert each label into `middleman_labels` for that repo
- replace that item's association rows with the latest set

Repo scoping rules:

- label identity is `(repo_id, platform_id)`
- item-to-label associations only point at labels from the same repo
- no cross-repo reuse by name

For issue sync, existing normalization logic in `internal/github/normalize.go` should move away from producing JSON payloads and instead return structured label data usable by the persistence layer.

For pull request sync, labels must be captured for the first time and persisted alongside the rest of the PR detail/list sync flow.

### Queries And API Assembly

Update list and detail queries so each PR and issue is returned with its labels already assembled.

Implementation options:

- join label rows directly in Go and aggregate them per item after scanning
- or query labels separately per result set and stitch them onto items by item ID

Recommendation: for list endpoints, load the page of items first, then fetch all matching labels for those item IDs in one additional query per item type and attach them in Go. This avoids row explosion in the main list queries and keeps ordering logic stable.

Detail endpoints can either reuse the same helper pattern or run a dedicated label query for the selected item.

### Frontend Rendering

Create a shared component in `packages/ui`, for example `packages/ui/src/components/shared/GitHubLabels.svelte`.

Props:

- `labels: Label[]`
- `mode: "compact" | "full"`
- `maxVisible?: number`

Behavior:

- `compact` mode is for `PullItem.svelte` and `IssueItem.svelte`
- `full` mode is for `PullDetail.svelte` and `IssueDetail.svelte`
- compact mode truncates to a small count and renders `+N` overflow
- full mode wraps all labels
- empty label arrays render nothing

Styling:

- use GitHub label color as the pill background
- compute readable foreground text from the background color instead of hardcoding white
- use a GitHub-like rounded pill, compact padding, semibold small text, and subtle border treatment
- preserve truncation in list rows and wrapping in detail views

This component replaces the duplicated inline issue label rendering and becomes the only label UI used by list/detail item components.

## Data Flow

1. GitHub API returns PR and issue labels for a repo item.
2. Sync layer upserts repo-scoped labels into `middleman_labels`.
3. Sync layer replaces association rows in `middleman_merge_request_labels` or `middleman_issue_labels`.
4. DB queries fetch items, then fetch labels for those item IDs.
5. Server returns structured `labels` arrays in list and detail responses.
6. `packages/ui` components pass those arrays to `GitHubLabels.svelte`.
7. `GitHubLabels.svelte` renders GitHub-style pills in compact or full mode.

## Error Handling

- If a label row is malformed or missing color, render a neutral fallback style rather than crashing.
- If color parsing fails on the frontend, fall back to a readable neutral chip.
- If label association sync fails for one item, the sync operation should return an error so the repo sync can be retried rather than silently serving partial label state.
- If old issue JSON backfill encounters malformed data, skip the bad entry, log it, and continue migrating other labels.

## Testing

### Database And Migration Tests

- migration test proving new tables are created successfully
- migration test proving existing `middleman_issues.labels_json` data backfills into `middleman_labels` and `middleman_issue_labels`
- query tests for listing PRs with labels
- query tests for listing issues with labels
- query tests for PR detail labels
- query tests for issue detail labels

### GitHub Sync Tests

- normalization or sync tests showing issue labels are persisted into normalized tables
- sync tests showing PR labels are persisted into normalized tables
- tests proving repo scoping keeps same-named labels in different repos independent
- tests proving removed labels are removed from association tables on resync

### API Tests

- list pulls endpoint returns `labels` arrays
- pull detail endpoint returns `labels` arrays
- list issues endpoint returns `labels` arrays
- issue detail endpoint returns `labels` arrays

### Frontend Tests

- component test for `GitHubLabels.svelte` compact mode with overflow
- component test for `GitHubLabels.svelte` full mode wrapping all labels
- component test proving readable foreground color selection for light and dark label colors
- integration-level component tests or view tests confirming `PullItem`, `PullDetail`, `IssueItem`, and `IssueDetail` render the shared component output

## Risks And Mitigations

- Query complexity increases when attaching labels to list responses.
  - Mitigation: fetch labels in a second batched query keyed by item IDs instead of inflating the main list query.

- Migration complexity is higher because issues already store labels as JSON while PRs do not.
  - Mitigation: write explicit backfill logic from issue JSON and land the normalized schema in one migration.

- GitHub-style pills can become unreadable for some colors.
  - Mitigation: compute foreground contrast instead of assuming white text.

## Implementation Notes

- `packages/ui/src/api/types.ts` should define a shared label type sourced from the generated schema instead of a hand-maintained issue-only interface.
- `internal/server/api_types.go` and the OpenAPI output need to reflect structured label arrays for both item types.
- `packages/ui/src/components/sidebar/IssueItem.svelte` and `packages/ui/src/components/detail/IssueDetail.svelte` should stop parsing JSON locally.
- `packages/ui/src/components/sidebar/PullItem.svelte` and `packages/ui/src/components/detail/PullDetail.svelte` should gain label rendering using the same component.
