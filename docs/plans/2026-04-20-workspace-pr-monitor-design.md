# Workspace PR Monitor Design

**Date:** 2026-04-20
**Status:** Proposed

## Goal

Add background monitoring for active workspaces so an issue-backed workspace can detect when its local branch becomes associated with a synced pull request, then persist that PR association without changing the workspace's original issue association.

## Problem

Middleman workspaces currently model only one linked item:

- PR workspaces are identified by `item_type='pull_request'` and `item_number=<pr number>`.
- Issue workspaces are identified by `item_type='issue'` and `item_number=<issue number>`.

That shape is enough to create either kind of workspace, but it cannot represent common issue workflow progression:

1. user creates workspace from issue
2. user starts branch locally
3. user opens PR later
4. workspace should keep issue identity and also gain PR-specific UI/context

Today there is no persisted place to store "this issue workspace is now also associated with PR #N", so terminal workspace UI cannot reliably decide when to expose a PR view.

## Non-Goals

- Changing a workspace's primary owner after creation
- Replacing issue association with PR association
- Automatically clearing or reassigning a PR association after it has been set
- General multi-item linking beyond issue + associated PR
- Reacting instantly to branch changes via hooks or tmux integration

## User-Facing Outcome

- PR-backed workspaces continue to behave as they do today
- Issue-backed workspaces continue to open with issue context
- Once an issue-backed workspace has a detectable synced PR, it gains PR context as well
- Issue-backed workspace terminal UI shows both `Issue` and `PR` tabs after association is known
- Default tab remains `Issue`

## Design Principles

1. Preserve original workspace identity
2. Store PR capability explicitly in database
3. Use one PR-view rule for all workspaces
4. Keep detection best-effort and background-only
5. First detected PR wins; no automatic churn

## Data Model

### Existing meaning kept

- `item_type` and `item_number` remain workspace primary owner fields
- PR workspace:
  - `item_type='pull_request'`
  - `item_number=<pr number>`
- Issue workspace:
  - `item_type='issue'`
  - `item_number=<issue number>`

### New field

Add nullable `associated_pr_number` to `middleman_workspaces`.

Meaning:

- PR workspace: `associated_pr_number = item_number`
- Issue workspace before PR exists: `associated_pr_number = NULL`
- Issue workspace after PR detected: `associated_pr_number = <detected pr number>`

This field does not replace issue ownership. It only expresses whether workspace has PR context available.

### Migration and backfill

Migration adds nullable column and backfills existing PR workspaces:

- `ALTER TABLE middleman_workspaces ADD COLUMN associated_pr_number INTEGER;`
- `UPDATE middleman_workspaces SET associated_pr_number = item_number WHERE item_type = 'pull_request';`

No backfill for issue workspaces.

## API Model

Workspace API responses should expose associated PR metadata directly so UI can render without extra lookup choreography.

Owner-derived workspace metadata remains unchanged:

- workspace list row title/state continue to come from owner item
- issue workspace list/header continues to use issue-owned fields and branch pill
- PR workspace list/header continues to use PR-owned fields

### Workspace response additions

Add nullable `associated_pr` object to every `workspaceResponse`-shaped HTTP payload, including create/list/get responses:

- `number`
- `title`
- `state`
- `is_draft`
- `ci_status`
- `review_decision`

Semantics:

- `item_type` + `item_number` describe owner
- `associated_pr` describes PR capability
- PR tab visibility is driven by `associated_pr != null`

For PR workspaces, `associated_pr` is always present once migration/backfill or creation path has run.

For issue workspaces, `associated_pr` appears only after monitor sets `associated_pr_number`.

### SSE contract

`workspace_status` remains an ID-only invalidation event, not a full workspace payload.

Meaning:

- create/list/get HTTP responses use expanded `workspaceResponse` with `associated_pr`
- `workspace_status` event continues to carry workspace ID only
- clients receiving `workspace_status` refetch workspace/list data over HTTP

## Background Monitor

## Ownership

Server owns monitor lifecycle because it already has:

- database handle
- workspace manager
- background context tied to shutdown
- startup path where durable loops belong

Monitor starts with server and stops on server shutdown.

### Schedule

- server starts monitor loop immediately during startup
- loop executes one `RunOnce` pass immediately after startup wiring completes
- loop then executes every minute

Implementation must expose deterministic test seam:

- monitor logic lives in a type with `RunOnce(ctx)`
- periodic loop only calls `RunOnce` on ticker
- tests call `RunOnce` directly instead of waiting for real time

This satisfies "every minute or so" while keeping tests deterministic.

### Eligibility

Monitor only inspects workspaces where all are true:

- workspace exists in database
- `item_type='issue'`
- `associated_pr_number IS NULL`
- `status='ready'`
- worktree path is non-empty

PR workspaces are excluded because they already have `associated_pr_number`. Non-ready workspaces are skipped completely.
### Detection inputs

For each eligible workspace, monitor reads git state from workspace directory.

Detection order:

1. Resolve upstream branch / tracking ref
2. Fallback to current local branch name

The monitor should skip association if branch state is not meaningful, including:

- detached HEAD
- no current branch
- synthetic issue branch still active (for example `middleman/issue-7`)
- git command failure

### Matching rule

Match detected branch to synced pull requests in same repository only.

#### Upstream-first matching

If git reports an upstream tracking ref for current branch, monitor must collect:

- local branch name
- upstream branch name
- upstream remote name
- upstream remote URL from git config, if readable

Monitor then searches synced open PRs in same repo using these rules:

1. Candidate PR must have `head_branch == upstream branch name`
2. If upstream remote URL is available, candidate PR must also have matching normalized head-repo identity
3. If exactly one candidate remains, select it
4. If zero or multiple candidates remain, treat upstream match as unresolved and continue to fallback rules only when allowed below

Normalization for remote identity comparison:

- convert upstream remote URL and PR `head_repo_clone_url` into canonical `owner/repo` identity
- parser must accept HTTPS and SSH/SCP clone URL forms
- ignore scheme, credentials, optional `.git` suffix, and trailing slash
- compare case-insensitively
- if either side cannot be parsed into `owner/repo`, treat remote-identity comparison as unavailable

#### Local-name fallback

Fallback is allowed only when no upstream tracking ref exists or upstream metadata is unreadable.
Fallback rule:

1. Candidate PR must be open and in same repo
2. Candidate PR must have `head_branch == current local branch name`
3. If exactly one candidate exists, select it
4. If multiple candidates exist, skip as ambiguous
5. If zero candidates exist, leave workspace unlinked

This intentionally avoids guessing when multiple PRs share branch names across forks.

### Persistence rule

When monitor finds exactly one selected PR match:

- set `associated_pr_number` once
- update must be atomic and conditional: write only when current value is still `NULL`
- DB write path must return whether row changed so caller can distinguish first association from no-op
- never overwrite automatically later
- never clear automatically later
- never change `item_type` or `item_number`

This makes workspace behavior stable after PR association appears.
## Workspace Creation Rules

### PR workspace creation

PR-backed creation path should persist:

- `item_type='pull_request'`
- `item_number=<pr number>`
- `associated_pr_number=<same pr number>`

### Issue workspace creation

Issue-backed creation path should persist:

- `item_type='issue'`
- `item_number=<issue number>`
- `associated_pr_number=NULL`

## UI Behavior

## Long-term rule

PR presentation should be capability-based, not owner-type-only.

Rules:

- show `PR` tab when `associated_pr != null`
- show `Issue` tab when `item_type == 'issue'`
- show `Reviews` tab only for PR-owned workspaces in this feature
- default tab comes from owner:
  - PR workspace defaults to `PR`
  - issue workspace defaults to `Issue`
- persisted sidebar-tab localStorage restore applies only on initial load of current workspace ID and only when it does not violate owner default
- specifically, issue-owned workspaces must open on `Issue` on initial load, even if localStorage previously stored `pr`
- after initial load, if user manually switches tabs, subsequent workspace refetch/SSE updates for that same workspace ID must preserve current tab while it remains supported
- when navigating to different workspace ID, tab selection is recomputed from owner default for newly opened workspace
- reset to owner default only when current tab becomes unsupported for latest workspace payload of current workspace

This keeps current PR workspace behavior visible to users while giving both workspace kinds one internal rule for PR support.

### Frontend boundary

`WorkspaceTerminalView` remains owner of terminal-route workspace payload fetching and tab selection.

It should pass two distinct item contexts into sidebar rendering:

- owner context:
  - `ownerItemType`
  - `ownerItemNumber`
- associated PR context:
  - `associatedPRNumber` nullable

`WorkspaceRightSidebar` should stop inferring PR-vs-issue solely from owner item type. Instead:

- `Issue` tab renders owner issue detail using owner context
- `PR` tab renders PR detail using associated PR context
- `Reviews` tab keeps current PR-owned behavior and continues to use workspace branch
- PR detail rendered inside workspace sidebar must suppress workspace create/open actions so an issue-owned workspace with `associated_pr` never offers redundant `Create Workspace` or self-referential `Open Workspace` actions

This boundary makes dual-context issue workspaces explicit and testable.
### Terminal workspace behavior

#### PR workspace

- tabs: `PR`, `Reviews`
- PR tab uses `associated_pr.number`
- visible behavior remains same as current product, except embedded workspace sidebar PR detail suppresses workspace create/open actions

#### Issue workspace without associated PR

- tabs: `Issue`
- no PR tab
- no Reviews tab change in this feature

#### Issue workspace with associated PR

- tabs: `Issue`, `PR`
- default remains `Issue` on initial load
- localStorage must not reopen this workspace on `PR` by default
- after user manually selects `PR`, refetches keep `PR` selected while still supported
- PR tab loads associated PR detail using `associated_pr.number`
- embedded PR detail suppresses workspace create/open actions

## Server and Query Changes

Required query/model changes:

- add `associated_pr_number` to `db.Workspace`
- add joined associated PR metadata to `db.WorkspaceSummary`
- update insert/get/list/summary scans
- add focused update method for setting `associated_pr_number` conditionally when null and returning whether row changed
- add query helper for listing monitor candidates, or filter from workspace list if sufficient

Summary joins should pull associated PR fields from `middleman_merge_requests` using:

- repo resolved from workspace host/owner/name
- merge request number resolved from `associated_pr_number`

This join must be separate from owner join because issue workspaces need both issue-owned metadata and PR-associated metadata.

Because API schema changes, implementation must also regenerate API artifacts:

- `make api-generate`
- checked-in generated client/schema outputs updated with source change

## Error Handling

Monitor failures are non-fatal.

Behavior:

- log git inspection failures with workspace ID/path context
- skip unreadable or missing worktrees
- skip ambiguous branch states
- skip if no synced PR match exists yet
- continue scanning remaining workspaces
- do not move or rewrite existing association
- after successful first association write where DB method reports row changed, server broadcasts:
  - `workspace_status` event with workspace ID so open terminal views refetch
  - `data_changed` event so broader UI can refresh lists/detail caches

On restart, monitor resumes scanning unresolved issue workspaces.

## Testing Strategy

E2E coverage required because feature crosses persistence, server lifecycle, git state inspection, and UI behavior.

### Database tests

- migration adds `associated_pr_number`
- migration backfills PR-owned workspaces
- summary query returns owner issue metadata plus associated PR metadata correctly

### Workspace/server tests

- PR workspace creation sets `associated_pr_number` immediately
- issue workspace creation leaves `associated_pr_number` null
- monitor resolves PR from upstream branch
- monitor falls back to local branch name when upstream missing
- monitor skips synthetic issue branch
- monitor does not overwrite existing association

### API e2e tests

- create issue workspace
- seed synced PR in same repo
- move workspace branch to match PR branch
- invoke monitor `RunOnce` directly through test seam
- verify `GET /workspaces/{id}` returns:
  - `item_type='issue'`
  - original issue `item_number`
  - populated `associated_pr`
- verify issue detail still references workspace

### Frontend e2e tests

- issue workspace without `associated_pr` shows only `Issue`
- issue workspace with `associated_pr` shows `Issue` and `PR`
- issue workspace still defaults to `Issue`
- stored `pr` sidebar-tab state does not override issue-owned default after association appears
- after user manually switches to `PR`, association-driven or later refetches keep `PR` selected while supported
- embedded PR detail inside workspace sidebar hides workspace create/open actions
- PR workspace still behaves as before while sourcing PR detail from `associated_pr`
- open terminal view gains PR tab after association-triggered SSE/refetch

## Implementation Notes for Planning

Likely files touched during implementation:

- `internal/db/migrations/`
- `internal/db/types.go`
- `internal/db/queries.go`
- `internal/workspace/manager.go`
- `internal/server/server.go`
- `internal/server/api_types.go`
- `frontend/src/lib/components/terminal/WorkspaceTerminalView.svelte`
- `packages/ui/src/components/workspace/WorkspaceRightSidebar.svelte`
- relevant Go e2e tests and Playwright tests

## Open Questions Resolved

- Detection source: upstream ref first, branch name fallback
- Upstream remote identity comparison: canonical case-insensitive `owner/repo`
- Issue association: never replaced by PR association
- PR association churn: first match wins, no automatic overwrite
- List/detail payloads: include associated PR metadata directly
- PR workspace behavior: keep visible UX same, but drive PR support from `associated_pr`

## Acceptance Criteria

1. Existing PR workspaces persist and expose `associated_pr`
2. Issue workspaces keep original issue ownership
3. Background monitor runs periodically under server lifecycle
4. Unlinked issue workspaces gain `associated_pr` once local git state matches synced PR branch
5. Association is persisted and stable across restart
6. Terminal UI shows PR tab whenever `associated_pr` exists
7. Issue workspace still shows issue tab and defaults to it
8. Tests cover migration, API behavior, monitor detection, and terminal UI behavior
9. Embedded PR detail inside workspace sidebar never shows redundant workspace create/open actions
