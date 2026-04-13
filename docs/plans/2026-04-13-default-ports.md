# Default ports implementation plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Move middleman default app and Vite dev ports away from agentsview defaults to avoid local collisions.

**Architecture:** Update port defaults at source-of-truth config points, then align docs and tests with new values. Keep runtime behavior unchanged apart from defaults: explicit config, env overrides, Docker overrides, and ephemeral test listeners still behave same.

**Tech Stack:** Go, Svelte 5, Vite, Playwright, testify

---

### Task 1: Update backend default port sources

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `internal/config/config.go`
- Modify: `middleman.go`
- Test: `internal/config/config_test.go`
- Test: `cmd/middleman/main_test.go`

- [ ] **Step 1: Run existing backend default-port tests**

Run: `go test ./internal/config ./cmd/middleman -shuffle=on`
Expected: PASS with current `8090` defaults.

- [ ] **Step 2: Update default port constants and embedded/library fallback**

Set backend defaults from `8090` to `8091` in:

```go
const (
    defaultPort = 8091
)
```

and

```go
return &config.Config{
    Host: "127.0.0.1",
    Port: 8091,
}
```

Also update default config template string in `internal/config/config.go` from:

```toml
port = 8090
```

to:

```toml
port = 8091
```

- [ ] **Step 3: Update backend tests that assert defaults**

Change expected values from `8090` to `8091` in config and startup tests.

- [ ] **Step 4: Run backend tests again**

Run: `go test ./internal/config ./cmd/middleman -shuffle=on`
Expected: PASS with `8091` defaults.

- [ ] **Step 5: Commit**

Commit message:

```bash
fix: change default backend port to 8091
```

### Task 2: Update server/config fixture expectations

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `internal/server/settings_test.go`
- Modify: `internal/server/roborev_proxy_test.go`
- Modify: `internal/server/sync_cooldown_e2e_test.go`

- [ ] **Step 1: Run existing server tests**

Run: `go test ./internal/server -run 'TestHandleGetSettings|TestRoborevProxyForwarding|TestTriggerSyncE2EBypassesCooldown|TestAddRepoE2ETriggersImmediateSyncDuringCooldown|TestRefreshRepoE2ETriggersImmediateSyncDuringCooldown' -shuffle=on`
Expected: PASS with current fixture configs.

- [ ] **Step 2: Update fixture config snippets**

Change inline TOML fixture snippets from:

```toml
port = 8090
```

to:

```toml
port = 8091
```

and any direct struct literal expectations from `8090` to `8091`.

- [ ] **Step 3: Run server tests again**

Run: `go test ./internal/server -run 'TestHandleGetSettings|TestRoborevProxyForwarding|TestTriggerSyncE2EBypassesCooldown|TestAddRepoE2ETriggersImmediateSyncDuringCooldown|TestRefreshRepoE2ETriggersImmediateSyncDuringCooldown' -shuffle=on`
Expected: PASS with updated fixtures.

- [ ] **Step 4: Commit**

Commit message:

```bash
test: align server fixtures with new default port
```

### Task 3: Update frontend dev defaults and docs

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `frontend/vite.config.ts`
- Modify: `README.md`
- Modify: `config.example.toml`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update Vite defaults**

Change proxy fallback target and explicit dev port to:

```ts
const apiUrl = process.env.MIDDLEMAN_API_URL ?? "http://127.0.0.1:8091";

server: {
  port: 5174,
  proxy: {
    "/api": {
      target: apiUrl,
    },
  },
}
```

- [ ] **Step 2: Update user-facing docs and sample config**

Change docs and examples from `8090` to `8091`, and Vite dev docs from `5173` to `5174`.

- [ ] **Step 3: Verify frontend config builds**

Run: `cd frontend && bun run build`
Expected: PASS.

- [ ] **Step 4: Commit**

Commit message:

```bash
docs: update default app and dev ports
```

### Task 4: Final verification and PR prep

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `docs/plans/2026-04-13-default-ports-design.md`
- Modify: `docs/plans/2026-04-13-default-ports.md`

- [ ] **Step 1: Run focused full verification**

Run:

```bash
go test ./internal/config ./cmd/middleman ./internal/server -shuffle=on
cd frontend && bun run build
```

Expected: all PASS.

- [ ] **Step 2: Review diff**

Run:

```bash
git diff --stat
```

Expected: only port-default, docs, and test-alignment changes.

- [ ] **Step 3: Create final commit**

Commit message:

```bash
fix: change middleman default ports
```

- [ ] **Step 4: Push branch and open PR**

Run:

```bash
git push origin change-default-ports
gh pr create --fill
```

Expected: branch pushed and PR URL returned.
