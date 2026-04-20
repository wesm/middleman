# Issue-Backed Middleman Workspaces

## Summary

Issue detail should create a real middleman-managed workspace entry,
not route into the embedded host/worktree explorer and not emit an
embed-only workspace command.

The new issue workspace flow should:

- persist a row in `middleman_workspaces`
- create a real git worktree under middleman's worktree directory
- create a tmux session like PR-backed workspaces do today
- create a new managed branch from the repository's current
  `origin/HEAD`
- use an issue-specific right sidebar panel instead of the PR panel

This keeps the issue action inside middleman's existing workspace and
terminal model rather than delegating it to the unrelated embedded
workspace explorer.

## Problem

Middleman currently has two different concepts named "workspace":

1. An embedded host/project/worktree explorer rendered by
   `packages/ui/src/views/WorkspacesView.svelte`
2. A middleman-owned git worktree + tmux session system rendered by
   `frontend/src/lib/components/terminal/WorkspaceTerminalView.svelte`

The recent issue button work routed into the first system. That did
not create a middleman workspace entry, did not create a git worktree,
and did not show up in the middleman terminal/workspace list.

The intended behavior is for issue detail to use the second system.

## Goals

- Let an issue create a middleman-backed workspace entry.
- Reuse the existing workspace manager, lifecycle, terminal, and SSE
  status updates.
- Make issue workspaces appear in the middleman workspace list and
  terminal route.
- Keep PR-backed workspaces working exactly as they do today.
- Make issue workspaces render issue context in the right sidebar.
- Remove the repo-level embedded workspace panel entry points that
  were added for the detour path and are no longer needed.

## Non-Goals

- Unifying the embedded workspace explorer with middleman's own
  workspace model in this change
- Redesigning middleman's remaining terminal/workspace navigation
- Supporting reviews/roborev sidebars for issue workspaces
- Generalizing the entire workspace API vocabulary into a fully
  generic linked-item model in one pass

## Approaches Considered

### Option 1: Keep the embed command path and mirror results back

The issue button would continue to emit an embed workspace command and
some host-side integration would create a middleman row afterward.

Pros:

- smallest middleman code change on paper

Cons:

- keeps the wrong ownership boundary
- still depends on the embedded workspace explorer path
- does not solve the current architectural confusion between the two
  workspace systems
- makes middleman workspace creation indirect and harder to test

Rejected.

### Option 2: Add a separate issue-backed middleman workspace path

Issue detail would call a middleman API endpoint that creates an issue
workspace entry directly. The DB row would still live in
`middleman_workspaces`, but it would carry enough metadata to tell
issue-backed and PR-backed workspaces apart.

Pros:

- matches the intended product behavior
- reuses the existing workspace manager, list, terminal, and delete
  flows
- can be tested end-to-end through the existing SQLite + git setup

Cons:

- requires small DB model changes
- requires small presentation-layer changes where the terminal UI
  assumes all workspaces are PR-backed

Chosen.

### Option 3: Rebuild the workspace model as a fully generic linked
item system

This would go beyond the targeted schema cleanup here and fully
generalize the workspace APIs, types, and UI around a linked-item
abstraction in one pass.

Pros:

- clean long-term model

Cons:

- too large for the bug/feature scope
- unnecessary migration risk for existing PR workspaces

Rejected for this change.

## Proposed Design

### Data Model

Extend `middleman_workspaces` with a new `item_type` column:

- `pull_request` for existing PR-backed workspaces
- `issue` for new issue-backed workspaces

Rename `mr_number` to `item_number`.

`item_number` is the numeric slot for the linked item number:

- for PR workspaces: the pull request number
- for issue workspaces: the issue number

This rename is part of this change because keeping the old column name
would make the new issue-backed rows needlessly confusing, and there
is no cross-platform justification for preserving a PR-specific name
for mixed item types.

The existing `mr_head_ref` column remains the branch field rendered in
the terminal UI:

- for PR workspaces: the PR head branch, unchanged
- for issue workspaces: the synthetic middleman branch name, e.g.
  `middleman/issue-42`

This intentionally avoids a larger schema rewrite in the same change.

### Workspace Creation Semantics

Issue workspaces should be created on a synthetic branch:

- branch name: `middleman/issue-<number>`
- start ref: `origin/HEAD`

The user requirement is explicit that issue workspaces should create a
new branch from the current `origin/HEAD`, not attach directly to
`origin/HEAD`, not reuse a PR branch, and not depend on an embed-host
concept of the current worktree.

Setup flow for issue workspaces:

1. ensure/fetch the bare clone
2. create `middleman/issue-N` as a new local branch from `origin/HEAD`
3. run `git worktree add <path> middleman/issue-N`
4. create the tmux session
5. mark the workspace ready

Equivalent git invocation is acceptable so long as the result is the
same: a fresh managed branch whose initial commit matches the current
`origin/HEAD`.

### API Shape

Keep the existing PR workspace endpoint unchanged:

- `POST /api/v1/workspaces`

Add a dedicated issue workspace endpoint:

- `POST /api/v1/repos/{owner}/{name}/issues/{number}/workspace`

Request body:

- `platform_host`

The issue routes in middleman do not include host in the URL today, so
the issue-create endpoint must accept `platform_host` explicitly to
avoid recreating the same ambiguity that caused the recent issue-panel
navigation workaround.

Response:

- the standard workspace response body
- `202 Accepted`

If an issue workspace already exists, the endpoint should return the
existing workspace summary instead of failing with a duplicate error.
That keeps the button effectively idempotent and lets the UI navigate
to the existing terminal route.

### Issue Detail UI

Issue detail should:

1. call the new issue workspace endpoint
2. receive the workspace id immediately
3. navigate to `/terminal/{id}`

If the issue detail response already reports a linked workspace, the
button should read `Open Workspace` and navigate directly.
Otherwise it should read `Create Workspace`.

Issue detail should no longer:

- navigate to `/workspaces/panel/...`
- depend on the embedded workspace command bridge

### Workspace List / Terminal Presentation

The middleman workspace list and terminal view currently assume every
workspace is a PR-backed workspace.

Small presentation-layer adjustments are needed:

- workspace rows should route to PR detail for `pull_request` items
  and issue detail for `issue` items
- display text should use the issue title when `item_type = issue`
- issue workspaces should not render PR-only affordances

### Right Sidebar

Issue-backed workspaces should show an issue panel in the right
sidebar instead of the PR panel.

Behavior:

- PR workspaces keep the existing `PR` and `Reviews` sidebar behavior
- issue workspaces replace the PR panel with an issue panel
- issue workspaces do not expose the reviews panel

This keeps the terminal experience consistent with the linked item
type rather than pretending an issue workspace has PR metadata.

### Embedded Workspace Cleanup

The repo-level embedded workspace panel path should be removed from
the middleman frontend once issue workspaces stop using it.

Scope of cleanup:

- remove navigation to `/workspaces/panel/...` from issue detail and
  any repo detail affordances that only existed to support the embed
  explorer
- remove the route parsing and route rendering branches that exist
  only for the embedded panel flow
- remove the now-unused tests and demo screenshots that only exercise
  `/workspaces/panel/...`

The embedded explorer code under `packages/ui` does not need to be
deleted in this change if it is still used outside middleman's SPA.
The required cleanup is specifically middleman's own references and
entry points for that panel path.

### Testing

This change needs end-to-end coverage because it crosses database,
workspace manager, API, router, and terminal UI layers.

Required coverage:

- API/e2e test for creating an issue-backed workspace row and managed
  worktree on a new managed branch created from `origin/HEAD`
- API/e2e test proving the create endpoint is idempotent for an
  existing issue workspace
- frontend e2e test for the issue detail button creating a workspace
  and navigating to `/terminal/{id}`
- frontend e2e test for issue-backed workspaces showing the issue
  sidebar and not exposing the reviews tab
- router/frontend coverage that `/workspaces/panel/...` is no longer a
  supported middleman path

## Detailed Changes

### Database

- Add migration `000012_add_workspace_item_type`
- Rename `middleman_workspaces.mr_number` to `item_number`
- Add `Workspace.ItemType`
- Add DB query support for reading/writing `item_type` and
  `item_number`
- Add `GetWorkspaceByIssue(...)`
- Update workspace summary joins so issue-backed workspaces resolve an
  issue title/state instead of always joining merge requests

### Workspace Manager

- Add `CreateIssue(...)`
- Add `GetByIssue(...)`
- Teach setup to branch from `origin/HEAD` when `item_type = issue`
- Use `middleman/issue-<number>` as the new managed branch name

### Server

- Add issue workspace create endpoint
- Add issue detail workspace reference lookup
- Return `item_type` in workspace responses

### Frontend

- Change issue detail button to call the new middleman API endpoint
- Navigate to `/terminal/{id}` on success
- Teach workspace list/sidebar/terminal views about `item_type`
- Render `IssueDetail` in the right sidebar for issue workspaces
- Suppress PR-review sidebar behavior for issue workspaces
- Remove middleman-owned `/workspaces/panel/...` routing and its
  obsolete test/demo coverage

## Testing

### Backend

- DB tests for `item_type` persistence and `GetWorkspaceByIssue`
- E2E API test that creates an issue workspace, waits for readiness,
  and verifies:
  - item type is `issue`
  - branch is `middleman/issue-N`
  - worktree HEAD matches current `origin/HEAD`

### Frontend

- Full-stack browser test that clicking issue detail workspace button:
  - calls the issue workspace API
  - navigates to `/terminal/{id}`
- Terminal view test that issue-backed workspaces route to issue
  detail instead of PR detail
- Terminal sidebar test that issue workspaces render the issue panel
  and not the PR reviews panel

## Risks

- The workspace API will still contain some `mr_*` field names, such
  as `mr_head_ref`, even after renaming `mr_number`. That is an
  acceptable intermediate state, but the implementation should avoid
  introducing any new `mr_*` names for issue-backed data.
- The issue-create endpoint must use the provided `platform_host`
  carefully so it does not recreate owner/name ambiguity across hosts.
- `origin/HEAD` resolution must rely on the fetched bare clone state;
  tests should pin this behavior directly.

## Follow-Up Work

- Finish renaming remaining workspace API fields away from `mr_*`
  where the old names no longer fit the data model
- Decide whether the embedded workspace explorer and middleman's own
  workspace manager should continue to share the `/workspaces` route
  at all
- Consider whether issue workspaces should support additional sidebar
  tools beyond the issue panel
