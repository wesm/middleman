# Workspace APIs

These APIs manage **middleman-owned workspaces**: durable local execution
contexts for tracked PRs and issues. They are not a generic Git worktree
browser and not an embedder protocol for arbitrary host state.

## Purpose

- Persist a middleman workspace entry for a tracked item.
- Materialize that entry as a local Git worktree plus tmux session.
- Let the UI reopen the same workspace from `/workspaces` or `/terminal/:id`.
- Carry enough item metadata to render the correct sidebar behavior.

## Endpoint Intent

- `POST /workspaces`: create or reuse a PR-backed workspace.
- `POST /repos/{owner}/{name}/issues/{number}/workspace`: create or reuse an
  issue-backed workspace; these start from the repo's current `origin/HEAD`,
  not from a PR head branch.
- `GET /workspaces`: list middleman's persisted workspaces for the workspaces
  page and terminal picker.
- `GET /workspaces/{id}`: load one persisted workspace for terminal view.
- `DELETE /workspaces/{id}`: tear down a middleman-managed workspace and its
  local resources.

## Data Model Intent

- `item_type`: whether the workspace belongs to a `pull_request` or `issue`.
- `item_number`: the tracked item number within the repo.
- `git_head_ref`: the Git branch name middleman opens in the worktree.

These fields exist so PR-backed workspaces show PR/Reviews sidebars, while
issue-backed workspaces show the issue sidebar and disable the PR/reviews path.

## Runtime Lifecycle Seam

Workspace runtime lifecycle coordination belongs below the HTTP server in
`internal/workspace.RuntimeLifecycle`.

- `internal/workspace/localruntime.Manager` is the process adapter. It starts,
  stops, lists, restores, and attaches local runtime processes and tmux-backed
  runtime sessions.
- `internal/workspace.Manager` is the persistence/worktree adapter. It records
  durable runtime tmux ownership rows and owns workspace deletion of persisted
  rows, worktrees, branches, and base workspace tmux sessions.
- `RuntimeLifecycle` composes those adapters for cross-adapter ordering:
  launch + record + rollback, explicit stop + stored tmux fallback, natural
  exit cleanup, and delete-time stopping marker + stop-before-destructive
  cleanup.
- Server handlers should call lifecycle methods for launching runtime sessions,
  stopping runtime sessions, and deleting workspaces. They should not directly
  interleave process cleanup with runtime tmux persistence.

The delete ordering is intentional: dirty preflight must run before runtime
sessions are stopped, but the runtime stopping marker must span the whole
delete call so a concurrent launch cannot start in a worktree after sessions
were stopped and before the workspace row is removed.

## Non-Goals

- Represent arbitrary worktrees discovered on a host machine.
- Mirror an external workspace tree or host inventory.
- Serve as a generic Git automation API outside middleman's workspace lifecycle.
