# Workspace APIs

These APIs exist to manage **middleman-owned workspaces**.

They are not a generic Git worktree browser, and they are not a
host/embed protocol for inspecting arbitrary external worktrees.
Their job is narrower: create, list, inspect, and delete the
local workspaces that middleman uses as durable execution
contexts for tracked review items.

## Purpose

- Give a tracked PR or issue a stable local workspace entry in
  middleman's own database.
- Materialize that workspace as a local Git worktree plus tmux
  session managed by middleman.
- Let the UI reopen that workspace later from `/workspaces` or
  `/terminal/:id` without recomputing anything from Git state.
- Keep enough item metadata on the workspace record for the UI
  to present the correct sidebar and routing behavior.

## Endpoint Roles

### `POST /workspaces`

Creates a workspace for a pull request.

This is the PR-backed workspace flow. The caller identifies the
tracked PR, and middleman creates or reuses the local workspace
for that PR.

### `POST /repos/{owner}/{name}/issues/{number}/workspace`

Creates a workspace for an issue.

This is the issue-backed workspace flow. Unlike PR workspaces,
these do not start from a PR head branch. They create a new local
branch from the repository's current `origin/HEAD` and register a
middleman workspace entry for the issue.

### `GET /workspaces`

Lists middleman-managed workspaces.

This is the source for the workspaces page and terminal workspace
picker. It should return middleman's own persisted workspace
records, not an external host's worktree inventory.

### `GET /workspaces/{id}`

Returns one persisted workspace record.

The UI uses this to load the terminal view for an existing
workspace and decide whether that workspace is PR-backed or
issue-backed.

### `DELETE /workspaces/{id}`

Deletes a persisted workspace and its managed local resources.

This is the teardown path for a middleman workspace entry. It is
not a generic "delete any worktree on disk" API.

## Data Model Intent

The workspace record is keyed to a tracked item, not only to a
branch name.

- `item_type` says whether the workspace belongs to a
  `pull_request` or an `issue`.
- `item_number` is the tracked item number within the repo.
- `git_head_ref` is the Git branch name middleman should open in
  the worktree.

Those fields exist so the UI can render the correct detail panel:

- PR-backed workspaces show PR and Reviews sidebars.
- Issue-backed workspaces show the issue sidebar and disable the
  PR/reviews path.

## Non-Goals

- Representing arbitrary worktrees discovered on a host machine.
- Mirroring an external embedder's workspace tree.
- Acting as a generic Git automation API outside middleman's own
  tracked workspace lifecycle.
