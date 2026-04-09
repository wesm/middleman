# Force-Push Display Design

## Goal

Make it clear when a pull request branch has been force-pushed or otherwise had visible history rewritten, while keeping the UI minimal and reusing the existing commit-activity surfaces.

## Scope

This design covers:

- detecting rewritten pull request history during sync
- persisting a minimal synthetic event that marks the rewrite
- rendering that event in the pull request detail timeline
- rendering that event in activity-feed views that already show commit activity

This design does not replace the current per-commit event model with a higher-level push model.

## Current State

Pull request timeline sync currently fetches comments, reviews, and commits and persists them as append-only `middleman_mr_events` rows. Commit events are stored with `event_type = "commit"`, the commit SHA in `Summary`, and the commit message in `Body`.

The frontend already renders those commit events in:

- the pull request detail timeline
- the flat activity feed
- the threaded activity feed

The activity feed already includes regular PR commit activity as `commit` items.

## Proposed Approach

Use GitHub's native force-push timeline event as the source of truth.

- Keep existing `commit` events unchanged.
- Fetch pull request force-push timeline events during sync in addition to comments, reviews, and commits.
- Persist GitHub's `head_ref_force_pushed` event as a merge-request event with `EventType = "force_push"`.
- Carry the GitHub actor through so the UI can show who force-pushed the branch.
- Render that event anywhere the UI already shows commit activity.

This keeps the signal explicit, avoids heuristic-only detection, and lets the product show the force-push actor instead of leaving it blank.

## Source Event

Detection should be driven by GitHub timeline events, not inferred from commit-list shape alone.

### Source API

During pull request timeline refresh, continue fetching comments, reviews, and commits as today.

Fetch force-push events from GitHub GraphQL using `HeadRefForcePushedEvent` timeline items for the pull request.

This event gives the design everything it needs:

- actor
- before commit
- after commit
- created-at timestamp
- ref

Using GraphQL for this specific event keeps the feature precise and preserves the SHA-pair dedupe rule.

### Failure guardrails

Do not synthesize a `force_push` event from commit-list heuristics when the timeline request fails.

If the GraphQL timeline fetch fails, preserve prior state and skip force-push event updates for that sync cycle.

## Persisted Event Shape

Persist each GitHub `HeadRefForcePushedEvent` as a `db.MREvent` with:

- `EventType = "force_push"`
- `Author` set to the GitHub actor login when present
- `Summary` containing the compact SHA transition string `<old-tip-short> -> <new-tip-short>`
- `PlatformID` set from the GitHub timeline event ID when available
- `MetadataJSON` containing the full before SHA, full after SHA, and ref name
- `DedupeKey` derived only from the two SHAs

- `force-push-<old-tip-sha>-<new-tip-sha>`

This keeps the dedupe behavior aligned with the actual rewritten transition.

## Append-Only History

Existing merge-request events remain append-only.

- Do not delete older `commit` events after a force push.
- The new `force_push` event is the marker that explains why older commit events are separated by a history rewrite.

This preserves history while still making the rewrite explicit.

## API And Query Changes

The existing merge-request detail API can continue returning `[]db.MREvent`; it only needs to include the new `force_push` event type.

The activity feed should start including `force_push` events from `middleman_mr_events` alongside `issue_comment`, `review`, and `commit`.

`force_push` should map through the activity pipeline as its own activity type so the frontend can label it clearly instead of pretending it is a normal commit.

## Frontend Rendering

### Pull Request Detail Timeline

Render `force_push` as a distinct timeline card in `EventTimeline.svelte`.

- label: `Force-pushed`
- color family: near commit activity, but visually distinct enough that it reads as a rewrite marker rather than another commit
- actor line: show the GitHub actor login when present
- summary line: the compact SHA transition from the before and after commits

The body should stay minimal. This design requires the event label, the actor, and the compact SHA transition summary.

### Flat Activity Feed

Render `force_push` as a normal activity row with label `Force-pushed`.

- It should appear anywhere commit activity already appears.
- It should break collapsed commit runs so the rewrite boundary remains visible.

### Threaded Activity Feed

Render `force_push` as a normal event row with label `Force-pushed`.

- It should appear in the per-item event list.
- It should break collapsed commit runs for the same reason as the flat view.

### Filters

Add `force_push` to the activity event filter list.

This keeps the event discoverable and consistent with the existing comment/review/commit filtering model.

## Testing

### Backend

Add sync tests that verify:

- a `head_ref_force_pushed` GitHub timeline event is persisted as `force_push`
- the GitHub actor is carried into the stored event
- repeated syncs of the same force-push event do not duplicate it
- timeline fetch failure does not produce false force-push events

Add activity-query coverage that verifies `force_push` events appear in the unified activity feed.

### Frontend

Add component coverage that verifies:

- `EventTimeline.svelte` renders the new event label and summary correctly
- the flat activity feed renders `Force-pushed`
- the threaded activity feed renders `Force-pushed`
- collapsed commit grouping stops at a `force_push` event instead of collapsing across it

## Implementation Notes

- Keep the change additive.
- Reuse the existing event pipeline rather than introducing a new push-domain model.
- Prefer small query and rendering updates over broader data reshaping.

## Out Of Scope

- deleting or hiding older commit events after a rewrite
- redesigning the activity feed around grouped push batches
