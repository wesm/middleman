# Dev compose implementation plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Add a `mise` task that starts Docker compose with a host-fetched GitHub token and a mounted host middleman config file.

**Architecture:** Keep compose authentication on host side, then inject `MIDDLEMAN_GITHUB_TOKEN` into backend container. Mount host config file read-only into `/data/config.toml` and point middleman defaults there via `MIDDLEMAN_HOME=/data`, while preserving SQLite storage in Docker volume `/data`.

**Tech Stack:** Docker Compose, mise, Bash, Go app config, README docs

---

### Task 1: Update compose backend config source

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `compose.yml`

- [ ] **Step 1: Update backend environment and mounts**

Add:

```yaml
    environment:
      MIDDLEMAN_GITHUB_TOKEN: ${MIDDLEMAN_GITHUB_TOKEN}
      MIDDLEMAN_HOME: /data
```

and mount:

```yaml
      - ${HOME}/.config/middleman/config.toml:/data/config.toml:ro
```

- [ ] **Step 2: Remove repo-local config flag from backend command**

Change:

```yaml
        make dev ARGS="-config docker/dev-config.toml"
```

to:

```yaml
        make dev
```

- [ ] **Step 3: Verify rendered compose file**

Run:

```bash
MIDDLEMAN_GITHUB_TOKEN=dummy docker compose config
```

Expected: backend shows `/data/config.toml` read-only mount and `MIDDLEMAN_HOME=/data`.

### Task 2: Add mise task for host-side auth injection

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `mise.toml`

- [ ] **Step 1: Add `dev-compose` task**

Add task:

```toml
[tasks.dev-compose]
description = "Run docker compose with gh auth token injection"
run = 'MIDDLEMAN_GITHUB_TOKEN="$(gh auth token)" docker compose up --build'
```

- [ ] **Step 2: Verify mise task is visible**

Run:

```bash
mise tasks ls | rg dev-compose
```

Expected: task listed.

### Task 3: Document compose workflow

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `README.md`
- Optional cleanup: `docker/dev-config.toml`

- [ ] **Step 1: Add compose usage section**

Document:
- compose uses host `~/.config/middleman/config.toml`
- config file is mounted read-only
- SQLite stays in Docker volume at `/data/middleman.db`
- `dev-compose` fetches token from host `gh auth token`
- host config should not set `data_dir` away from `/data` for compose usage

- [ ] **Step 2: Decide handling of `docker/dev-config.toml`**

If compose no longer uses file, either:
- remove it, or
- keep it but note it is no longer compose source

Preferred: remove it if truly unused.

- [ ] **Step 3: Verify docs accuracy**

Run:

```bash
rg -n "dev-compose|docker compose|config.toml|MIDDLEMAN_HOME|gh auth token|/data/middleman.db" README.md compose.yml mise.toml docker/dev-config.toml -S
```

Expected: docs and config references agree.

### Task 4: Final verification and commit

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `docs/plans/2026-04-13-dev-compose-design.md`
- Modify: `docs/plans/2026-04-13-dev-compose.md`

- [ ] **Step 1: Run final verification**

Run:

```bash
MIDDLEMAN_GITHUB_TOKEN=dummy docker compose config >/tmp/middleman-compose.out
mise tasks ls | rg dev-compose
rg -n "dev-compose|MIDDLEMAN_HOME|/data/config.toml|/data/middleman.db" README.md compose.yml mise.toml -S
```

Expected: all commands succeed and references match.

- [ ] **Step 2: Commit**

Commit message:

```bash
feat: add dev compose mise task
```
