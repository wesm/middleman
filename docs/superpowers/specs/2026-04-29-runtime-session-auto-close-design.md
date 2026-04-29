# Runtime Session Auto-Close Design

## Problem

Launched workspace agent sessions, such as Claude or Codex, can exit naturally when the user sends EOF or otherwise quits the process. Today the terminal websocket reports the exit and the runtime manager marks the process as exited, but the workspace UI can continue showing the tab and the backend can continue advertising the session. For tmux-backed agent sessions, the persisted runtime tmux row can also remain until a later cleanup path runs.

This makes the session look reusable even though it is no longer a live process. A user can leave the workspace, return later, click the same agent, and land back in a terminated session instead of getting a clear fresh launch flow.

## Goals

- Treat natural process exit as the end of a launched runtime session.
- Remove exited agent and subprocess tabs automatically from the workspace UI.
- Keep backend runtime state aligned with what the UI shows.
- Forget persisted tmux-backed runtime rows when the backing tmux session has exited or disappeared.
- Preserve the existing workspace `tmux` tab behavior, which represents the base workspace terminal and intentionally reconnects.
- Preserve explicit close behavior, including confirmation for running sessions.

## Non-Goals

- Do not change workspace deletion or base workspace tmux cleanup behavior.
- Do not introduce a session history view or transcript retention.
- Do not restart agent sessions automatically after a normal exit.
- Do not change the configured launch target model.

## Recommended Approach

Use a combined backend and frontend cleanup flow.

The runtime manager should remove launched sessions from its active session maps when their process exits naturally. If the session was tmux-backed, the server should also forget the persisted runtime tmux row once the tmux session is gone. This makes `/workspaces/{id}/runtime` return only live or starting runtime sessions.

The frontend should still react immediately to the terminal websocket exit message. For agent session panes, `onExit` should unmount the pane, remove the tab from local UI state, select Home if the closed tab was active, and refresh runtime state in the background. This avoids leaving the user staring at a dead terminal while waiting for the next poll or runtime fetch.

The base workspace `tmux` tab should keep its current reconnect behavior because it is not a launched agent session. The shell drawer should follow the same user-facing principle as agent sessions: when the shell process exits, close or collapse the live terminal surface and refresh runtime state so reopening Shell starts a fresh shell.

## Backend Design

Add an exit cleanup path to `internal/workspace/localruntime.Manager`.

- The `session.watch` path already observes process exit, records status, exit time, and exit code, closes the PTY, and closes `done`.
- Extend session startup so the manager can register an exit callback for launched sessions and shell sessions.
- On natural exit, remove the session from `m.sessions` or `m.shells` only if the map still points at the same session pointer. This keeps explicit stop and concurrent replacement safe.
- If the session had a `tmuxSession`, invoke a manager-level callback supplied by the server after removal. The callback should be non-blocking relative to the session lock and should tolerate absent tmux sessions.
- Preserve shutdown behavior: runtime shutdown should still detach tmux-backed sessions so restart recovery can restore them, rather than treating server shutdown as natural user exit.

Add a server cleanup callback.

- When constructing `localruntime.Manager`, provide a callback that calls `workspace.Manager.ForgetRuntimeTmuxSession(ctx, workspaceID, tmuxSession)` once the tmux session is absent or after the runtime attach exits because the agent process ended.
- Use a bounded context for cleanup and log warnings without surfacing them to the websocket path.
- Explicit `DELETE /workspaces/{id}/runtime/sessions/{session_key}` should keep its existing stop-then-forget behavior.

`GET /workspaces/{id}/runtime` should return no entry for a naturally exited session after cleanup. This is the authoritative state the frontend reconciles against.

## Frontend Design

Update `WorkspaceTerminalView.svelte`.

- Replace the current agent pane `onExit={() => void fetchRuntime()}` with an exit handler that:
  - removes the session key from `mountedSessionKeys`;
  - selects Home if the active tab is that session;
  - clears the remembered active tab for that workspace to Home;
  - calls `fetchRuntime()` in the background.
- Keep `reconnectOnExit={false}` for launched agent sessions.
- Keep the base `tmux` tab using `reconnectOnExit={true}`.
- For the shell drawer, close the drawer on exit and refresh runtime state so reopening Shell calls `ensureWorkspaceShell` and starts a fresh process.

The UI should not show a special "exited" tab in the normal flow because the user action was to quit the process. The next natural action should be launching the target again from Home or the launch menu.

## Testing

Add backend E2E coverage with real SQLite and the existing runtime helper process.

- Natural non-tmux agent exit: launch a helper target that exits, wait for runtime cleanup, then assert `GET /workspaces/{id}/runtime` returns no sessions.
- Natural tmux-backed agent exit: launch a tmux-backed helper target that exits, attach through the websocket if needed to drive the process, wait for exit, then assert the runtime list is empty and `middleman_workspace_tmux_sessions` has no row for that workspace.
- Ensure explicit stop behavior still removes sessions and stored tmux rows.

Add frontend tests around `WorkspaceTerminalView.svelte` or the smallest suitable component boundary.

- When a mounted session terminal emits `onExit`, the tab is removed and Home becomes active.
- The base `tmux` tab remains mounted/reconnect-capable and is not auto-closed by the agent exit path.
- Shell drawer exit closes the drawer and refreshes runtime state.

Run focused Go tests with `-shuffle=on`, frontend tests with Bun, and regenerate API artifacts only if public API schemas change. This design should not require an API schema change.

## Risks

- Removing sessions immediately means users lose the short in-terminal `[Process exited]` marker. That is intentional for launched agents; the tab itself disappearing is clearer than retaining a dead pane.
- Tmux cleanup must not run during middleman server shutdown, or restart recovery would break. The cleanup path must distinguish natural process exit from manager shutdown/detach.
- The frontend should guard workspace-id transitions so an exit event from the old workspace cannot close a tab in the new workspace.
