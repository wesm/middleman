# Workspace Feature — Test Coverage Gaps

Identified by roborev reviews during implementation. All underlying code
bugs are fixed; these are missing test coverage for regression prevention.

## Go / E2E

- [ ] Terminal WebSocket e2e: open real session, force exit, assert client
  receives `"exited"` frame (validates the background-context write fix)
- [ ] Dirty-delete e2e: simulate missing/corrupt worktree, assert first
  DELETE returns 409, assert `?force=true` succeeds
- [ ] Multi-host platform_host e2e: seed two repos with same owner/name
  on different hosts, assert PR detail returns correct `platform_host`
  from the repo row (not syncer default)
- [ ] Duplicate workspace create e2e: POST twice for same MR, assert
  second returns 409 Conflict

## Frontend / Playwright

- [ ] Terminal route switching: navigate `/terminal/a` then `/terminal/b`,
  verify component remounts (keyed by workspaceId)
- [ ] Delete from terminal view: exercise the CSRF Content-Type header
  fix and 409 → force-delete flow
- [ ] Process exit stops reconnect: verify `exited` event suppresses
  WebSocket reconnection
- [ ] Standalone `/workspaces` placeholder renders correctly
- [ ] Sidebar → terminal navigation flow
