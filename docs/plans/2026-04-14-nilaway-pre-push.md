# NilAway pre-push hook Implementation Plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Add dedicated `make nilaway` task, run it from `prek` on `pre-push` for Go-related changes only, and document setup for developers.

**Architecture:** Keep NilAway outside `golangci-lint` and expose it through `Makefile` so manual runs, hooks, and future CI can share one command path. Let `prek.toml` decide when hook fires using `pre-push` plus Go file selectors, then update `README.md` so install and hook behavior stay discoverable.

**Tech Stack:** Go toolchain, Uber NilAway, GNU Make, prek, Markdown

---

## File map

- `Makefile` — add repo-local `nilaway` target, expose it in `.PHONY` and `help`, and rename hook-install wording from pre-commit-only to both stages.
- `prek.toml` — install both hook shims and add `nilaway` local hook with `pre-push` stage plus Go-only selectors.
- `README.md` — document `make nilaway`, separate NilAway install requirement, and repo hook setup flow.

### Task 1: Add dedicated NilAway Make target

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `Makefile:24-25`
- Modify: `Makefile:147-203`
- Verify: command output from `make nilaway` and `make help`

- [ ] **Step 1: Extend `.PHONY` for new target**

Add `nilaway` to existing `.PHONY` block.

```make
.PHONY: ensure-embed-dir check-air air-install build build-release install \
        frontend frontend-dev frontend-dev-bun frontend-check api-generate roborev-api-generate \
        dev test test-short test-e2e test-e2e-roborev vet lint nilaway testify-helper-check tidy svelte-skills clean install-hooks help
```

- [ ] **Step 2: Add `nilaway` target near `lint`**

Use runtime `go list -m` lookup inside recipe so missing-binary verification can keep `go` available while hiding `nilaway` from `PATH`.

```make
# Run NilAway against first-party Go packages
nilaway: ensure-embed-dir
	@if ! command -v nilaway >/dev/null 2>&1; then \
		echo "nilaway not found. Install with:" >&2; \
		echo "go install go.uber.org/nilaway/cmd/nilaway@latest" >&2; \
		exit 1; \
	fi
	@module_path="$$(go list -m)"; \
		nilaway -include-pkgs="$$module_path" ./...
```

- [ ] **Step 3: Update hook-install comment and `help` output**

Change wording from pre-commit-only to both hook stages and surface `make nilaway` in help text.

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
	@echo "  testify-helper-check - Enforce Assert.New(t) in assertion-heavy Go tests"
	@echo ""
	@echo "  install-hooks  - Install pre-commit and pre-push hooks (prek)"
```

- [ ] **Step 4: Verify missing-binary path prints install hint**

Create temporary `PATH` with `go` available and `nilaway` absent.

Run:

```bash
tmpbin="$(mktemp -d)"
ln -s "$(command -v go)" "$tmpbin/go"
PATH="$tmpbin:/usr/bin:/bin" make nilaway
```

Expected:
- command exits non-zero
- output contains exactly:

```text
nilaway not found. Install with:
go install go.uber.org/nilaway/cmd/nilaway@latest
```

- [ ] **Step 5: Verify target passes module-scoped args to NilAway**

Use fake `nilaway` binary so command line can be asserted without requiring real analysis to succeed.

Run:

```bash
tmpdir="$(mktemp -d)"
cat >"$tmpdir/nilaway" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" >"$MM_NILAWAY_ARGS"
EOF
chmod +x "$tmpdir/nilaway"
MM_NILAWAY_ARGS="$tmpdir/args.txt" PATH="$tmpdir:$(dirname "$(command -v go)"):/usr/bin:/bin" make nilaway
module_path="$(go list -m)"
diff -u <(printf '%s\n' "-include-pkgs=$module_path" "./...") "$tmpdir/args.txt"
```

Expected:
- `make nilaway` exits 0
- `diff -u` prints no output
- recorded args are exactly module-scoped `-include-pkgs=<module>` plus `./...`

- [ ] **Step 6: Verify help output shows new target and updated hook text**

Run:

```bash
make help | rg "nilaway|install-hooks"
```

Expected:

```text
  nilaway        - Run NilAway against first-party Go packages
  install-hooks  - Install pre-commit and pre-push hooks (prek)
```

- [ ] **Step 7: Commit Makefile changes**

```bash
git add Makefile
git commit -m "build: add nilaway make target"
```

### Task 2: Add pre-push NilAway hook in prek

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `prek.toml:1`
- Modify: `prek.toml:39-77`
- Verify: `.git/hooks/pre-commit`, `.git/hooks/pre-push`, `prek run ... --dry-run`

- [ ] **Step 1: Install both Git hook shims by default**

Change top-level hook installation list.

```toml
default_install_hook_types = ["pre-commit", "pre-push"]
```

- [ ] **Step 2: Add NilAway local hook entry**

Insert hook after existing Go hooks so config reads cleanly with other repo-local checks.

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

- [ ] **Step 3: Install hooks with repo-supported command**

Run:

```bash
make install-hooks
test -f .git/hooks/pre-commit
test -f .git/hooks/pre-push
```

Expected:
- all commands exit 0
- both hook shims exist after install

- [ ] **Step 4: Verify positive and negative hook selection**

Run:

```bash
prek run nilaway --stage pre-push --files internal/config/config.go --dry-run
prek run nilaway --stage pre-push --files frontend/src/App.svelte --dry-run
```

Expected:
- first command lists `nilaway`
- second command does not list `nilaway`
- dry-run output shows `make nilaway` with no filenames appended

- [ ] **Step 5: Commit hook config changes**

```bash
git add prek.toml
git commit -m "build: run nilaway on pre-push"
```

### Task 3: Update docs and finish verification

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `README.md:210-229`
- Verify: `README.md`, `Makefile`, `prek.toml`

- [ ] **Step 1: Add `make nilaway` to development target list**

Update `Other targets:` block.

```md
make build          # Debug build with embedded frontend
make build-release  # Optimized, stripped release binary
make test           # All Go tests
make test-short     # Fast tests only
make lint           # golangci-lint
make nilaway        # NilAway nil analysis for first-party Go packages
make frontend-check # Svelte and TypeScript checks
make api-generate   # Regenerate OpenAPI spec and clients
make clean          # Remove build artifacts
```

- [ ] **Step 2: Rename hook section and document setup flow**

Replace pre-commit-only section with both hook stages, separate NilAway install step, and preferred repo command.

````md
### Git hooks

Managed with [prek](https://github.com/j178/prek):

```sh
brew install prek
go install go.uber.org/nilaway/cmd/nilaway@latest
make install-hooks
```

Repo installs both `pre-commit` and `pre-push` hooks. NilAway runs on `pre-push` only when changed files include `go`, `go-mod`, or `go-sum` types.
````

- [ ] **Step 3: Run final cross-file verification**

Run:

```bash
make help | rg "nilaway|install-hooks"
prek run nilaway --stage pre-push --files internal/config/config.go --dry-run
prek run nilaway --stage pre-push --files frontend/src/App.svelte --dry-run
git diff -- Makefile prek.toml README.md
```

Expected:
- help output includes NilAway and updated hook wording
- Go file dry-run selects `nilaway`
- frontend file dry-run skips `nilaway`
- diff contains only intended `Makefile`, `prek.toml`, and `README.md` edits before final commit

- [ ] **Step 4: Commit docs changes**

```bash
git add README.md
git commit -m "docs: document nilaway hook setup"
```

## Self-review against design artifact

- Dedicated `make nilaway` target: Task 1.
- Dynamic `go list -m` module scoping: Task 1 Step 2, verified in Task 1 Step 5.
- Clear missing-binary failure with install hint: Task 1 Step 4.
- Keep `make lint` unchanged: no task modifies lint behavior beyond nearby help text.
- `pre-push` only, not `pre-commit`: Task 2 Step 2.
- Skip pure frontend pushes via `types_or = ["go", "go-mod", "go-sum"]`: Task 2 Step 2, verified in Task 2 Step 4.
- Install both Git hook shims: Task 2 Step 1 and Step 3.
- README/setup updates: Task 3.
- No placeholders remain; every task names exact files, code, commands, and expected output.
