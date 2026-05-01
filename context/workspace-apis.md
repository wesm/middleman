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

## Non-Goals

- Represent arbitrary worktrees discovered on a host machine.
- Mirror an external workspace tree or host inventory.
- Serve as a generic Git automation API outside middleman's workspace lifecycle.

## Related context

- [`context/workspace-runtime-lifecycle.md`](./workspace-runtime-lifecycle.md)
  documents runtime-session exit, tmux persistence, and destructive ordering
  rules that sit underneath these APIs.
