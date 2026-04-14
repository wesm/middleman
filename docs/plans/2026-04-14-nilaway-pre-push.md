# NilAway pre-push hook Implementation Plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Add dedicated `make nilaway` task and wire it into `prek` as `pre-push` hook that runs only for Go-related changes.

**Architecture:** Keep NilAway standalone from `golangci-lint`. Put command construction and missing-binary handling in `Makefile`, then let `prek.toml` trigger that target on `pre-push` using `types_or = ["go", "go-mod", "go-sum"]`. Update developer docs so local setup remains discoverable and consistent with repo workflow.

**Tech Stack:** Go toolchain, Uber NilAway, Make, prek, Markdown docs

---

## File map

- `Makefile` — add repo-local NilAway task, surface it in `.PHONY` and `help`, update hook-install wording.
- `prek.toml` — install `pre-push` shim and add NilAway local hook with Go-related type selectors.
- `README.md` — document NilAway task, hook behavior, and install flow.

### Task 1: Add dedicated NilAway Make target

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `Makefile`
- Verify: `Makefile` via `make nilaway --dry-run` is not available, so use direct target execution after edit

- [ ] **Step 1: Update `.PHONY` and helper variables in `Makefile`**

Add `nilaway` to `.PHONY`. Add helper variables near existing tool variables so module path and binary path are computed once.

```make
NILAWAY_BIN := $(shell if command -v nilaway >/dev/null 2>&1; then command -v nilaway; fi)
MODULE_PATH := $(shell go list -m)

.PHONY: ensure-embed-dir check-air air-install build build-release install \
        frontend frontend-dev frontend-dev-bun frontend-check api-generate roborev-api-generate \
        dev test test-short test-e2e test-e2e-roborev vet lint nilaway testify-helper-check tidy svelte-skills clean install-hooks help
```

- [ ] **Step 2: Add `nilaway` target in `Makefile`**

Insert target near `lint` / `vet` targets.

```make
# Run NilAway against first-party Go packages
nilaway: ensure-embed-dir
	@if [ -z "$(NILAWAY_BIN)" ]; then \
		echo "nilaway not found. Install with:" >&2; \
		echo "go install go.uber.org/nilaway/cmd/nilaway@latest" >&2; \
		exit 1; \
	fi
	$(NILAWAY_BIN) -include-pkgs="$(MODULE_PATH)" ./...
```

- [ ] **Step 3: Update `help` and install-hook wording in `Makefile`**

Adjust user-facing help text so repo surfaces new target and accurate hook scope.

```make
# Install pre-commit and pre-push hooks via prek
install-hooks:
	@if ! command -v prek >/dev/null 2>&1; then \
		echo "prek not found. Install with: brew install prek" >&2; \
		exit 1; \
	fi
	prek install -f

# Show help
help:
	@echo "  lint           - Run mise-managed golangci-lint (auto-fix)"
	@echo "  nilaway        - Run NilAway against first-party Go packages"
	@echo "  install-hooks  - Install pre-commit and pre-push hooks (prek)"
```

- [ ] **Step 4: Run target with missing binary path to verify install hint behavior**

Run:

```bash
PATH="/usr/bin:/bin" make nilaway
```

Expected:
- exit non-zero
- stderr contains:

```text
nilaway not found. Install with:
go install go.uber.org/nilaway/cmd/nilaway@latest
```

- [ ] **Step 5: Run Makefile formatting sanity check**

Run:

```bash
make help | rg "nilaway|install-hooks"
```

Expected:

```text
nilaway
install-hooks  - Install pre-commit and pre-push hooks (prek)
```

- [ ] **Step 6: Commit Makefile changes**

```bash
git add Makefile
git commit -m "build: add nilaway make target"
```

### Task 2: Add pre-push NilAway hook in prek

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `prek.toml`
- Verify: `.git/hooks/pre-push`, `.git/hooks/pre-commit`

- [ ] **Step 1: Update installed hook types in `prek.toml`**

Change top-level hook installation list so repo installs both shims.

```toml
default_install_hook_types = ["pre-commit", "pre-push"]
```

- [ ] **Step 2: Add local NilAway hook to `prek.toml`**

Insert NilAway hook in local hooks list near other Go hooks.

```toml
  {
    id = "nilaway",
    name = "nilaway",
    language = "system",
    entry = "make nilaway",
    stages = ["pre-push"],
    types_or = ["go", "go-mod", "go-sum"],
    pass_filenames = false,
    priority = 25,
  },
```

Use `priority = 25` so NilAway runs after existing Go pre-commit checks and remains ordered among local hooks.

- [ ] **Step 3: Install hooks through repo-supported command**

Run:

```bash
make install-hooks
```

Expected:
- command exits 0
- both hook files exist:

```bash
test -f .git/hooks/pre-commit && test -f .git/hooks/pre-push
```

- [ ] **Step 4: Verify pre-push hook selection logic**

Run dry-run selection check against Go-related changes.

```bash
prek run nilaway --stage pre-push --from-ref HEAD~1 --to-ref HEAD --dry-run
```

Expected:
- command shows `nilaway` selected when compared range includes `.go`, `go.mod`, or `go.sum`
- command does not append filenames to `make nilaway`

If `HEAD~1` does not contain suitable file types, create temporary local diff before running dry-run, then discard it after verification.

- [ ] **Step 5: Commit hook configuration changes**

```bash
git add prek.toml
git commit -m "build: run nilaway on pre-push"
```

### Task 3: Update developer docs and run end-to-end tooling verification

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `README.md`
- Verify: `README.md`, `Makefile`, `prek.toml`

- [ ] **Step 1: Update target list in `README.md`**

Add NilAway command to existing command list.

```md
make test           # All Go tests
make test-short     # Fast tests only
make lint           # golangci-lint
make nilaway        # NilAway nil analysis for first-party Go packages
make frontend-check # Svelte and TypeScript checks
make api-generate   # Regenerate OpenAPI spec and clients
make clean          # Remove build artifacts
```

- [ ] **Step 2: Rename and expand hook section in `README.md`**

Replace current pre-commit-only wording with both stages and setup commands.

```md
### Git hooks

Managed with [prek](https://github.com/j178/prek):

```sh
brew install prek
go install go.uber.org/nilaway/cmd/nilaway@latest
make install-hooks
```

NilAway runs on `pre-push` only when pushed changes include Go-tagged files (`go`, `go-mod`, `go-sum`).
```

- [ ] **Step 3: Run final verification commands**

Run:

```bash
make help | rg "nilaway|install-hooks"
make nilaway
prek run nilaway --stage pre-push --from-ref HEAD~1 --to-ref HEAD --dry-run
```

Expected:
- `make help` shows NilAway target and updated install-hooks text
- `make nilaway` exits 0 when NilAway installed and analyzes `./...` with `-include-pkgs="$(go list -m)"`
- dry-run shows NilAway only on qualifying Go-related changes

- [ ] **Step 4: Review final diff for scope control**

Run:

```bash
git diff --stat HEAD~3..HEAD
git diff -- Makefile prek.toml README.md
```

Expected:
- only `Makefile`, `prek.toml`, and `README.md` changed for implementation
- no unrelated formatting churn

- [ ] **Step 5: Commit documentation and verification-aligned changes**

```bash
git add README.md Makefile prek.toml
git commit -m "docs: document nilaway hook workflow"
```

## Self-review against spec

- Spec requires dedicated task: covered by Task 1.
- Spec requires dynamic module path via `go list -m`: covered by Task 1.
- Spec requires pre-push hook only: covered by Task 2.
- Spec requires Go-related trigger set with `types_or = ["go", "go-mod", "go-sum"]`: covered by Task 2.
- Spec requires installing both `pre-commit` and `pre-push` shims: covered by Task 2.
- Spec requires README + help + install-hooks wording updates: covered by Tasks 1 and 3.
- Spec requires missing-binary verification and working-binary verification: covered by Tasks 1 and 3.
- No placeholders remain; each step names exact files, commands, and expected outcomes.
