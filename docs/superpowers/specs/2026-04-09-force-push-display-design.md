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

Use a synthetic merge-request event to mark branch-history rewrites.

- Keep existing `commit` events unchanged.
- Add a new merge-request event type: `force_push`.
- Detect rewrites in the syncer while it has both the newly fetched GitHub commit list and the previously persisted commit events for the PR.
- Persist one `force_push` event when the new commit list does not represent a normal append-only continuation of the previously known visible history.
- Render that event anywhere the UI already shows commit activity.

This keeps the change explicit, minimal, and backend-driven.

## Detection

Detection happens in `refreshTimeline` during pull request sync.

### Inputs

- the newly fetched GitHub commit list for the PR
- the previously stored merge-request events for the PR

### Previous commit snapshot

The syncer should derive the previous visible commit snapshot from stored `commit` events for the PR.

- Use the stored commit SHA from each commit event's `Summary`.
- Ignore non-commit events when building the previous snapshot.
- If there are no previously stored commit events, do not emit a `force_push` event.

### Rewrite rule

Treat the update as a normal push when the old visible commit history is a prefix of the new visible commit history.

Treat the update as a rewritten history event when:

- there were previously stored commit events, and
- the new visible commit sequence is not a prefix-preserving extension of the previous visible commit sequence

This covers squash-and-force-push and rebase-and-force-push cases without redesigning commit storage.

### Failure guardrails

Do not emit a `force_push` event when:

- this is the first observed commit snapshot for the PR
- the GitHub commit fetch failed
- the GitHub commit fetch returned an empty list unexpectedly for a PR that previously had commits

In those cases the sync should preserve prior state instead of inferring a rewrite.

## Persisted Event Shape

Persist a synthetic `db.MREvent` with:

- `EventType = "force_push"`
- `Summary` containing a compact transition string such as `<old-tip-short> -> <new-tip-short>`
- `MetadataJSON` containing the full old tip SHA, full new tip SHA, and optional dropped/added commit counts
- `DedupeKey` derived only from the two tip SHAs

The dedupe key format should be:

- `force-push-<old-tip-sha>-<new-tip-sha>`

This ensures repeated syncs of the same rewrite do not insert duplicate events.

The event should not claim an actor. GitHub's commit-list snapshot does not tell us who force-pushed the branch, so `Author` should remain empty in this design.

## Append-Only History

Existing merge-request events remain append-only.

- Do not delete older `commit` events after a force push.
- The new `force_push` event is the marker that explains why older commit events no longer match the current branch tip.

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
- summary line: the compact SHA transition from `Summary`
- no actor line unless a reliable actor exists

The body should stay minimal. This design only requires the event label and the compact SHA transition summary.

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

- a normal append-only commit update does not emit `force_push`
- a rewritten commit sequence emits exactly one `force_push` event
- repeated syncs of the same rewrite do not duplicate the event because dedupe uses only the old/new tip SHAs
- empty or failed commit fetches do not produce false rewrite events

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

- identifying the actor who performed the force push
- deleting or hiding older commit events after a rewrite
- redesigning the activity feed around grouped push batches
