# PR Timeline Filter Design

## Goal

Add a PR detail activity filter that remembers browser-local preferences and lets maintainers focus the feed on human-authored content while still being able to inspect commits, force pushes, and PR system events when needed.

## Scope

This feature covers the pull request detail Activity section only. The Activity page defaults remain server-backed settings, and the PR detail filter uses `localStorage` so each browser remembers its own PR feed preferences.

The feature also expands the stored PR timeline events to include the first set of GitHub system events that maintainers need for triage:

- Cross references from issues or pull requests.
- PR title changes.
- Base branch changes.

The existing stored event types remain supported:

- `issue_comment`
- `review`
- `commit`
- `force_push`

Review comments are UI-aware today but are not currently fetched by the sync path. This design does not add review comment fetching unless it can be done within the same GitHub timeline query without adding a separate REST path.

## User Experience

The PR detail Activity section gets a compact filter control above the feed, using the existing shared `FilterDropdown` component. Fresh browsers default to showing everything.

The dropdown has these sections:

- Content
  - Messages: comments and reviews.
  - Commit details: compact commit rows.
  - Events: cross references, title changes, and base branch changes.
  - Force pushes: force-push rows.
- Visibility
  - Hide bot activity.

The filter state persists in `localStorage` and applies to every PR detail feed in that browser. It does not write URL parameters and does not change server config.

When no events remain after filtering, the timeline shows a short empty state that makes it clear filters are hiding activity.

## Rendering

Messages keep the current card treatment and markdown rendering.

Commit events render as compact one-line commit detail rows. Each row shows:

- Event date/time using the existing time formatting style.
- Short commit hash derived from the event summary SHA.
- Commit title, taken from the first line of the commit message body.

The commit title stays on one line and truncates with browser CSS overflow handling.

System events render as compact rows rather than full message cards:

- Cross reference: shows the source issue or pull request if available.
- Title change: shows previous title and current title.
- Base branch change: shows previous branch and current branch.
- Force push: continues to show the short before/after SHA summary.

Bot filtering uses the same author heuristic as the Activity page: lowercased author names ending in `[bot]`, `-bot`, or `bot` are considered bot-authored.

## Data Model

No database migration is required. `middleman_mr_events` already stores:

- `event_type`
- `author`
- `summary`
- `body`
- `metadata_json`
- `created_at`
- `dedupe_key`

New system events use new `event_type` values:

- `cross_referenced`
- `renamed_title`
- `base_ref_changed`

Each new event stores a human-readable `summary` for simple rendering and structured details in `metadata_json` for precise UI output.

Stable dedupe keys should be based on the GitHub GraphQL node ID when available. If a node ID is unavailable, the key should combine event type, timestamp, actor, and event-specific values such as branch names or titles.

## GitHub Sync

The existing GraphQL timeline path currently fetches `HEAD_REF_FORCE_PUSHED_EVENT`. It should be extended to fetch:

- `CROSS_REFERENCED_EVENT`
- `RENAMED_TITLE_EVENT`
- `BASE_REF_CHANGED_EVENT`
- `HEAD_REF_FORCE_PUSHED_EVENT`

The GraphQL query should request only the fields needed for normalization:

- Shared fields: `id`, `actor { login }`, `createdAt`
- `CrossReferencedEvent`: `source`, `isCrossRepository`, `willCloseTarget`
- `RenamedTitleEvent`: previous title and current title
- `BaseRefChangedEvent`: previous ref name and current ref name
- `HeadRefForcePushedEvent`: before commit, after commit, and ref

Timeline fetch failures for these system events should follow the current force-push behavior: log a warning, keep the rest of the detail sync usable, and avoid failing the whole PR detail refresh solely because the optional timeline event query failed.

## Components And Boundaries

Backend:

- `internal/github/client.go`: extend the GraphQL timeline event fetch shape.
- `internal/github/normalize.go`: add normalizers for the new system events.
- `internal/github/sync.go`: upsert the new events during PR timeline refresh.
- `internal/db/queries.go`: keep generic event storage unchanged unless query helpers need small adjustments.

Frontend:

- `packages/ui/src/components/detail/PullDetail.svelte`: add the filter row above Activity and pass filtered events into the timeline.
- `packages/ui/src/components/detail/EventTimeline.svelte`: render commit/system event rows compactly and keep message rows as cards.
- `packages/ui/src/components/shared/FilterDropdown.svelte`: reuse as-is unless a small generic accessibility or layout improvement is needed.
- A small helper module may be added under `packages/ui/src/components/detail/` for PR timeline filter state and event classification so component code stays readable.

## Testing

Backend tests should cover:

- GraphQL timeline parsing for cross reference, title change, base change, and force push events.
- Normalization into stable event types, summaries, metadata, authors, timestamps, and dedupe keys.
- PR detail sync upserts new timeline events without dropping existing comments, reviews, commits, or force pushes.
- Optional timeline fetch failures do not fail the entire PR detail refresh.

Frontend tests should cover:

- Default localStorage state shows everything.
- Toggling Messages, Commit details, Events, Force pushes, and Hide bot activity filters the displayed rows.
- Preferences persist across component remounts.
- Commit rows show date/time, short SHA, and one-line truncated title.
- The PR filter uses the shared `FilterDropdown` component rather than a new dropdown implementation.

Verification should run the Svelte autofixer for edited `.svelte` files, relevant frontend tests, relevant Go tests with `-shuffle=on`, and the standard build/test target needed for the touched layers.

## Non-Goals

- Do not add server-backed PR detail filter settings.
- Do not add URL query parameters for PR detail feed filters.
- Do not redesign the Activity page filters.
- Do not migrate existing databases.
- Do not add every GitHub timeline event type in this slice.
