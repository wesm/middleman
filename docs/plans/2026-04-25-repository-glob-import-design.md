# Repository Glob Import Design

## Context

The current Repositories settings workflow has one `owner/name` input. Submitting an exact name or glob immediately persists it to config via `POST /api/v1/repos`. Existing glob support tracks the pattern over time and the settings list only shows the glob plus matched count and refresh action.

That flow works for power users who want a live glob, but it is awkward when a user wants to discover repos from a glob, inspect matches, and add only a subset. The new workflow stages a glob in an import interface, previews matching repositories, lets the user filter/sort/select a subset, then saves the chosen repositories as exact config entries.

## Goals

- Let users enter `owner/pattern` and preview matching repositories before changing config.
- Exclude archived repositories from preview, matching current resolver behavior.
- Show repository name, description, visibility, and last code activity (`pushed_at`).
- Let users filter by name/description and selected/unselected/already-added state.
- Let users sort by name and last pushed time.
- Default all matching selectable repositories to selected.
- Support row checkbox selection, all/none shortcuts, and shift-click range selection/deselection.
- Add selected repositories as exact `owner/name` config entries, not as a glob.
- Preserve existing direct glob support for users who want ongoing glob tracking.

## Non-goals

- Do not change existing config schema for repositories.
- Do not add glob include/exclude persistence.
- Do not auto-add future repositories that match the staged glob.
- Do not show archived repositories in preview.
- Do not add pagination or server-side filtering beyond the submitted owner/pattern in the first iteration.

## Recommended approach

Add a modal-based repository import workflow backed by two API endpoints:

1. `POST /api/v1/repos/preview` lists non-archived repositories for an owner matching a glob pattern and returns display metadata plus already-configured state.
2. `POST /api/v1/repos/bulk` accepts selected exact repositories, validates them, appends new exact config entries in one save, updates the syncer, and triggers one sync run.

This keeps the existing exact/glob add path intact while providing a safer staged workflow for subset imports.

## User experience

### Entry point

The Repositories settings section shows `Add repositories…` as the primary action at the top of the section. Activating it opens the import modal. The existing direct add form remains below the primary action in a collapsed `Advanced: add exact repo or tracking glob directly` area so existing live-glob behavior is preserved without competing with the staged import flow.

In embedded mode, repository mutation controls remain unavailable. Hide or disable `Add repositories…`, the import modal entry point, and the advanced direct add form consistently with existing settings mutation disablement.

In non-embedded read-only settings mode where `GET /settings` works but repo mutation endpoints return `404` because `cfgPath == ""`, no proactive UI capability flag exists. Keep the controls visible and handle `404` inline with the server message (`settings not available`) for preview, bulk add, and direct add. Adding a settings capability field is out of scope.

### Modal flow

1. User enters an import pattern in strict `owner/pattern` format, for example `wesm/*` or `roborev-dev/glob-*`.
2. User clicks `Preview` or presses Enter.
3. The modal validates and parses the field, then loads matching repositories from GitHub.
4. Matching, selectable repositories are selected by default.
5. Repositories with an exact existing config entry are visible but disabled and marked with an `Already added` chip. Repositories that are only covered by an existing configured glob are still importable as exact entries.
6. User narrows the table with text search and one status quick filter.
7. User sorts by name or last pushed time.
8. User adjusts selection with checkboxes, all/none shortcuts, or shift-click ranges.
9. User clicks `Add selected repositories`. The button is disabled when zero selectable repositories are selected.
10. The modal submits selected rows in the current sort order across the full preview result set, ignoring active text/status filters for ordering so filtered-out selections are still included deterministically. On success, the frontend applies the returned `settingsResponse` directly; it does not perform a follow-up `GET /settings`. The modal closes, configured repos update in app state, sync status refreshes, and a sync run is triggered by the server.

If the user edits the pattern input after a successful preview or while a preview request is in flight, the modal immediately clears preview rows, selection, filters, sort, range anchor, and submit state. The edit also invalidates outstanding preview responses. The user must run Preview again before adding repositories. This prevents stale rows from being submitted under a different visible pattern.

### Pattern parsing and validation

The modal exposes one input, but the preview API accepts `owner` and `pattern` separately. Validation has two layers:

Frontend parser rules before calling the API:

- Trim leading/trailing whitespace from the full input before parsing.
- Input must contain exactly one `/` separator.
- Trim leading/trailing whitespace from parsed owner and pattern.
- Owner must be non-empty.
- Owner must not contain glob metacharacters (`*`, `?`, `[`, `]`) or another `/`.
- Pattern must be non-empty.
- Pattern must not contain `/`.
- Pattern may contain `path.Match` glob metacharacters.

Backend validation rules for `owner` and `pattern` fields:

- Trim leading/trailing whitespace from owner and pattern before validation.
- Owner and pattern must be non-empty.
- Owner must not contain `/` or glob metacharacters (`*`, `?`, `[`, `]`).
- Pattern must not contain `/`.
- Pattern must compile with `path.Match`; malformed glob syntax, such as an unmatched `[`, returns `400`.

Frontend-only validation errors show inline and do not call the preview API. Backend validation errors also show inline and clear stale preview rows/selection.

### Table

Columns:

- selection checkbox
- repository name (`owner/name`)
- description
- last pushed (`pushed_at`, rendered in local time by UI presentation utilities)
- visibility rendered from the `private` boolean as `Private` or `Public`
- status (`Already added` when disabled)

Default sort is last pushed descending so active repositories appear first. Sort controls support name ascending/descending and last pushed ascending/descending. Sorts are deterministic: ties always break by lowercase `owner/name` ascending, then original preview index. `pushed_at: null` sorts last in both ascending and descending recency modes so repositories with unknown activity never interrupt dated rows.

### Filtering

The text filter is a case-insensitive substring search. The query is trimmed and internal whitespace is matched literally. Empty query matches all rows. It searches lowercase `owner/name`, lowercase `name`, and lowercase description; `null` descriptions are treated as empty strings. A status quick filter allows the user to focus rows by one mode at a time:

- all rows
- selected
- unselected selectable rows
- already added

`Unselected` means selectable rows that are currently unchecked; disabled already-added rows appear only under `All rows` and `Already added`. Selection persists across filtering and sorting. A new preview request resets filter, sort, range anchor, and selection state for the new result set; the import pattern input stays unchanged. A failed preview keeps the modal open, clears the previous result set to avoid stale additions, and shows the error inline. Closing and reopening the modal resets pattern input, preview rows, filters, sort, selection, range anchor, and errors.

### Bulk selection semantics

- `All` selects all currently filtered, selectable rows.
- `None` deselects all currently filtered, selectable rows.
- Already-configured rows are never selectable.
- Shift-click uses the last checkbox interaction as the range anchor.
- If the range anchor is not present in the current visible sorted/filtered rows, shift-click behaves like a normal click and moves the anchor to the clicked row.
- If the range anchor is still visible after filtering or sorting, shift-click uses the anchor's current visible position.
- Shift-click applies the clicked checkbox target state to every selectable row in the visible sorted/filtered range.

Example: if rows 3 and 10 are visible and selectable, clicking row 3 then shift-clicking row 10 checked selects rows 3 through 10. Clicking row 3 then shift-clicking row 10 unchecked deselects rows 3 through 10.

## API design

### Preview repositories

`POST /api/v1/repos/preview`

Request:

```json
{
  "owner": "wesm",
  "pattern": "middleman-*"
}
```

Response status: `200 OK`.

Response:

```json
{
  "owner": "wesm",
  "pattern": "middleman-*",
  "repos": [
    {
      "owner": "wesm",
      "name": "middleman",
      "description": "Local-first PR dashboard",
      "private": false,
      "pushed_at": "2026-04-20T12:00:00Z",
      "already_configured": false
    },
    {
      "owner": "wesm",
      "name": "empty-repo",
      "description": null,
      "private": true,
      "pushed_at": null,
      "already_configured": true
    }
  ]
}
```

`description` and `pushed_at` are nullable.

Behavior:

- Validate owner and pattern according to the backend validation rules above.
- Use the default GitHub host behavior consistent with current settings endpoints. Host selection is out of scope for this iteration.
- Call `ListRepositoriesByOwner(ctx, owner)`.
- Exclude archived repositories.
- Match names with the same glob semantics as existing resolver (`path.Match`, case-insensitive canonical matching).
- Canonicalize owner/name consistently with existing resolver behavior.
- Mark `already_configured` when the exact repository already exists as an exact config entry by owner/name according to current string-based config validation behavior. Host-specific duplicate handling and historical alias/rename detection are out of scope because settings responses and UI do not expose enough data for that workflow. Repositories configured under an old alias are not treated as already added unless their stored owner/name matches the canonical preview row. Repositories only matched by a configured glob are not considered already configured.
- Return `pushed_at` from GitHub as UTC RFC3339 when available. If GitHub omits it, return `null` rather than substituting `updated_at`.

Errors:

- `400` invalid JSON or missing owner/pattern.
- `400` invalid glob pattern.
- `502` missing GitHub client for the default host or GitHub listing failure.
- `404` settings mutation workflow unavailable (`cfgPath == ""`), matching existing repo mutation endpoint availability rather than read-only settings availability.

### Bulk add repositories

`POST /api/v1/repos/bulk`

Request:

```json
{
  "repos": [
    { "owner": "wesm", "name": "middleman" },
    { "owner": "wesm", "name": "middleman-ui" }
  ]
}
```

Response status: `201 Created` when at least one new repository is added.

Response: existing `settingsResponse` shape.

Behavior:

- Validate body contains at least one repo.
- Trim leading/trailing whitespace from submitted owner/name values before validation and canonicalization.
- Reject submitted owner/name values containing `/` or glob metacharacters (`*`, `?`, `[`, `]`) with `400`; bulk add is exact-only.
- Deduplicate submitted repos by owner/name using current canonical rules while preserving first-seen request order. Host selection is out of scope for this iteration, so duplicate detection follows current config validation behavior and treats any existing same owner/name exact entry as blocking regardless of host.
- Pre-validate every non-duplicate, not-already-exact-configured candidate with existing exact repository resolution logic so missing or archived repositories cannot be persisted.
- After validation, deduplicate again by canonical owner/name while preserving first resolved occurrence. This handles GitHub rename/redirect cases where multiple submitted values in the same request resolve to the same canonical repository.
- Use the canonical owner/name returned by GitHub validation when constructing new config entries and resolved `RepoRef`s. Do not persist raw request casing or aliases when GitHub returns canonical values. Detecting that an existing stored config entry is an old alias for the same logical repo is out of scope; current string-based duplicate behavior is preserved.
- Treat validation failures as all-or-nothing: if any selected candidate is missing, archived, or fails GitHub validation, return an error and do not change config.
- Re-check exact config duplicates when applying the request. Repositories that became exact-configured concurrently are skipped.
- If every submitted repository is already exact-configured by apply time, return `400` with a clear message.
- Save config once after appending new exact repos in canonicalized request order.
- Merge resolved repo refs into syncer tracked repos.
- Trigger one sync run with `context.WithoutCancel(r.Context())`.
- Return updated settings response.

Concurrency:

- Follow current add-repo pattern: pre-validate outside `cfgMu`, then re-acquire `cfgMu`, re-check exact duplicates against current config, apply, save, update syncer.
- Preserve concurrent changes to other settings by mutating current in-memory config rather than replacing it with a stale snapshot.

Errors:

- `400` invalid JSON, empty repo list, missing owner/name, glob syntax in owner/name, all selected repos already configured, invalid config.
- `400` archived repository via existing `ErrConfiguredRepoArchived` classification.
- `502` GitHub validation failure.
- `500` config save failure.
- `404` settings unavailable.
- These manual POST routes inherit existing API middleware behavior such as CSRF/content-type handling; UI code should normally only surface the JSON error envelope returned by the handlers.

## Frontend design

### Components and boundaries

- `RepoImportModal.svelte`
  - Source of truth for pattern input, preview rows, filter text, status filter, sort state, selected keys, range anchor, loading/error states, and submit state.
  - Parses the `owner/pattern` input before preview.
  - Calls preview and bulk add API helpers.
  - Uses pure helper functions to compute visible rows and next selection state.
  - Notifies parent with updated repos on success.

- `RepoPreviewTable.svelte`
  - Presentational table component.
  - Receives visible rows, selected keys, sort state, filter values, disabled/already-added state, and counts as props.
  - Emits intent callbacks only: filter text change, status filter change, sort toggle, checkbox toggle with row key/index/shift flag, select visible, deselect visible.
  - Does not own filtering, sorting, selected keys, or range-anchor state.

- `repoImportSelection.ts`
  - Pure helper module and source of truth for row keying, parsing input, filtering, sorting, all/none selection, and shift-click range behavior.
  - Exports functions with deterministic inputs/outputs so selection behavior can be unit tested without Svelte.

- API helpers in `frontend/src/lib/api/settings.ts`
  - `previewRepos(owner, pattern)`
  - `bulkAddRepos(repos)`
  - Both helpers parse the existing `{ "error": "..." }` error response envelope when available so inline errors show the message text instead of raw JSON.

### State model

- Preview rows should use `$state.raw<RepoPreviewRow[]>([])` because API results are replaced as a whole.
- Selection should be represented as a set of stable row keys (`owner/name` for this iteration). Updates should replace the Set instance to keep Svelte updates predictable.
- `RepoImportModal.svelte` uses `$derived.by` to call `repoImportSelection.ts` helpers and produce visible rows plus counts.
- Avoid `$effect` for derived selection counts or table state.

### Accessibility and keyboard support

- Modal has a dialog role, labelled title, and focus management consistent with existing app patterns.
- Opening the modal moves initial focus to the pattern input.
- `Escape` closes the modal when not submitting.
- Closing the modal returns focus to the `Add repositories…` trigger.
- Preview input supports Enter to load preview.
- Table checkboxes have labels including full repository name.
- Sort buttons expose current sort state with `aria-sort` or equivalent column-header semantics.
- Disabled already-configured rows explain status with visible text/chip, not color alone.

### Visual design

The modal should follow middleman's dense maintainer-tool style: compact controls, strong hierarchy, no decorative treatment. Use shared UI primitives where they fit:

- `ActionButton` for repeated modal actions if the sizing/tone fits.
- `Chip` for `Already added` and visibility/status metadata.

Local CSS is acceptable for table layout and modal-specific geometry. If table styling becomes reused elsewhere, promote it to a shared component in a later change and update `context/ui-design-system.md` then.

## Backend design

Add new repository import code in a separate server file, for example `internal/server/repo_import_handlers.go`, rather than growing `settings_handlers.go` further. Register the two endpoints as manual `net/http` settings routes beside the existing settings repo mutation handlers; do not add Huma/OpenAPI/generated-client support in this iteration.

Suggested backend units:

- request/response types for preview and bulk add
- `validateRepoImportPattern(owner, pattern)` for backend validation
- `exactConfiguredRepoSet(repos []config.Repo)` for exact-entry duplicate detection only, matching current owner/name config validation behavior rather than introducing host-aware semantics
- `buildRepoPreviewRows(ctx, client, configuredExactSet, owner, pattern)` for GitHub listing, archived exclusion, glob matching, canonicalization, and nullable metadata extraction. It does not own UI sorting.
- `validateBulkExactRepos(ctx, clients, candidates)` for all-or-nothing exact repo validation using existing `ghclient.ResolveConfiguredRepo`; returns canonical exact config entries plus resolved `RepoRef`s for the apply step
- `applyBulkExactRepos(...)` for duplicate re-check, config append using canonical owner/name values, single save, syncer merge, and sync trigger

Reuse existing helpers where they match current behavior:

- `ghclient.ResolveConfiguredRepo` for exact repository validation and archived rejection.
- existing canonical owner/name behavior from `internal/github/repo_config_resolver.go`.
- existing `classifyResolveError` for archived/GitHub error mapping.
- existing syncer merge behavior from settings handlers, either by reusing `mergeTrackedRepos` or extracting a small shared helper.

## Data flow

```text
SettingsPage
  └─ RepoSettings
      ├─ existing direct add/remove/refresh flow
      └─ RepoImportModal
          ├─ parse owner/pattern locally
          ├─ POST /api/v1/repos/preview
          │   └─ GitHub ListRepositoriesByOwner
          ├─ local filter/sort/select via repoImportSelection.ts
          └─ POST /api/v1/repos/bulk
              ├─ GitHub GetRepository validation per selected repo
              ├─ config save
              ├─ syncer tracked repos update
              └─ sync trigger
```

## Edge cases

- No matches: show empty state with pattern and suggest trying `owner/*`.
- All matches already configured: table shows disabled rows; submit disabled with explanatory footer.
- User deselects every selectable row: submit button is disabled; if an empty bulk request still reaches the server, it returns `400` and changes nothing.
- Some selected repos become exact-configured concurrently: bulk endpoint skips newly-duplicated entries and adds remaining entries.
- A selected repo is deleted, archived, or otherwise fails validation during bulk add: bulk endpoint returns an error and adds none of the selected repos.
- Pattern invalid: preview returns `400`; UI keeps modal open, clears previous rows/selection, and displays inline error.
- GitHub error/rate limit during preview: UI keeps modal open, clears previous rows/selection, and displays inline error.
- GitHub error/rate limit during bulk submit: UI keeps modal open, preserves current rows/selection, and displays inline error so the user can retry.
- Concurrent preview requests: latest valid request wins. If an older preview response arrives after a newer request starts, or after the pattern input changed, or after the modal closed/reopened, the UI discards the older response/error.
- While preview is loading, the Preview button shows loading state for the active request. Editing the input invalidates the active request and returns the modal to an unpreviewed state.
- While bulk add is submitting, `Add selected repositories` is disabled to prevent double submit; server duplicate handling remains defensive.
- Missing `pushed_at`: render `Never pushed` or `—` and sort nulls last for descending recency.
- Large org: first iteration loads all matching owner repos from the owner listing response; client-side filter/sort remains acceptable for this project scope.

## Testing plan

### Backend e2e tests

- Preview returns only matching non-archived repositories.
- Preview includes `pushed_at`, description, visibility, and already-configured status.
- Preview rejects invalid patterns.
- Bulk add persists selected repos as exact entries, not a glob.
- Bulk add saves config once, updates syncer tracked repos, and triggers sync once.
- Bulk add handles duplicates and already-configured repos.
- Bulk add all-or-nothing validation failure leaves config and syncer unchanged when one selected repo is deleted, archived, or fails GitHub validation.
- Bulk add preserves concurrent non-repo settings changes.

### Full-stack browser e2e tests

- Add a Playwright full-stack flow under `frontend/tests/e2e-full`: open Settings, open repository import modal, preview a glob from the e2e GitHub test double, filter/sort, deselect one row, submit selected repos, and verify the settings repository list updates with exact entries only.
- Cover latest-preview-wins behavior in the full-stack browser flow: start two preview requests with controlled response ordering and verify only the newest result populates the table.
- Cover failed preview clearing stale rows/results in the full-stack browser flow.

### Frontend tests

- Modal previews repositories and defaults selectable rows to selected.
- Already-configured rows render disabled and are not submitted.
- Text filter matches `owner/name`, name, and description with the exact case-insensitive substring/null-description semantics from this design.
- Selected/unselected/already-added quick filters work.
- Sort by name and last pushed toggles direction.
- `All` and `None` apply to current filtered selectable rows.
- Shift-click selects and deselects visible ranges.
- Successful bulk add closes modal and updates repository settings.
- Pure helper tests cover submit ordering, hidden-anchor shift-click behavior, anchor behavior after sort/filter changes, and selection key generation scoped to the default host.

## Open decisions

None. This design intentionally keeps persistence simple by importing selected repositories as exact config entries and leaving live glob tracking to the existing direct glob workflow.
