# NilAway pre-push hook design

## Summary

Add dedicated `make nilaway` task that runs Uber's NilAway against first-party packages in this repo. Wire task into `prek` as `pre-push` hook so potential nil conditions surface before code leaves local machine.

## Goals

- Add explicit repo task for NilAway.
- Add explicit repo task for NilAway.
- Run NilAway before push, not on every commit.
- Skip NilAway on pure frontend pushes.
- Limit analysis to repo package prefix `github.com/wesm/middleman`.
- Fail with clear install hint when `nilaway` binary is missing.
- Keep existing `make lint` flow unchanged.

## Non-goals

- Do not merge NilAway into `golangci-lint` config.
- Do not auto-install NilAway from hook.
- Do not add CI wiring in this change.
- Do not change existing pre-commit hook behavior beyond installing `pre-push` shim too.

## Current state

- `Makefile` has `lint`, `test-short`, `frontend-check`, `install-hooks`.
- `prek.toml` installs only `pre-commit` hook shim via `default_install_hook_types = ["pre-commit"]`.
- Repo already uses local `prek` hooks for Go formatting, lint, tests, and frontend checks.
- NilAway not yet referenced in repo.

## Chosen approach

### 1. Add dedicated Make target

Add `nilaway` target to `Makefile`.

Behavior:
- Check `nilaway` exists in `PATH`.
- If missing, print install command:
  - `go install go.uber.org/nilaway/cmd/nilaway@latest`
- Run:
  - `nilaway -include-pkgs="github.com/wesm/middleman" ./...`

Reasoning:
- Gives developers explicit local command.
- Keeps hook config thin.
- Makes later CI reuse trivial.

### 2. Add `pre-push` hook in `prek.toml`

Add local hook:
- `id = "nilaway"`
- `name = "nilaway"`
- `language = "system"`
- `entry = "make nilaway"`
- `stages = ["pre-push"]`
- `files = "^(.*\\.go|go\\.mod|go\\.sum)$"`
- `pass_filenames = false`

Reasoning:
- Hook should run only when pushed changes include Go source or Go module metadata.
- `pass_filenames = false` still lets NilAway analyze repo-wide package graph once triggered.
- `pre-push` timing keeps heavier static analysis off `pre-commit` path.

### 3. Install both hook shims

Update `default_install_hook_types` to:
- `pre-commit`
- `pre-push`

Reasoning:
- In `prek`, hook stage selection alone does not install Git shim.
- Repo should install both shims with existing `make install-hooks` flow.

### 4. Document developer setup

Update README hook section to mention:
- repo uses `prek` for `pre-commit` and `pre-push`
- NilAway runs on `pre-push` only when pushed changes include `*.go`, `go.mod`, or `go.sum`
- NilAway must be installed separately
- install command for NilAway

## Alternatives considered

### A. Add NilAway under `make lint`

Pros:
- single lint entrypoint

Cons:
- user asked for dedicated task
- makes routine lint heavier
- less clear separation between `golangci-lint` and NilAway

Rejected.

### B. Put NilAway command directly in `prek.toml`

Pros:
- fewer `Makefile` edits

Cons:
- command duplicated or hidden in hook config
- weaker local discoverability
- less reusable for CI or manual runs

Rejected.

### C. Integrate NilAway as custom `golangci-lint` linter

Pros:
- single reporting surface

Cons:
- larger config change
- not requested
- more moving parts than needed

Rejected.

## Error handling

### Missing binary

`make nilaway` exits non-zero with concise install hint.

Example message:

```text
nilaway not found. Install with:
go install go.uber.org/nilaway/cmd/nilaway@latest
```

### Analysis failures

NilAway output passes through unchanged. Hook blocks push on non-zero exit.

## Testing and verification

### Functional verification

- Run `make nilaway` with binary available.
- Confirm command targets package prefix `github.com/wesm/middleman`.
- Run `prek install -f` and confirm both `pre-commit` and `pre-push` hooks install.
- Verify `prek.toml` hook selector matches only `*.go`, `go.mod`, and `go.sum` changes.
- If supported, run `prek run nilaway --hook-stage pre-push --files <go-file>` to confirm hook selection. Otherwise inspect installed hook behavior plus direct `make nilaway` execution.

### Scope rationale

No new app behavior, API behavior, or DB behavior. E2E test not needed for this tooling-only change.

## Files to change

- `Makefile`
- `prek.toml`
- `README.md`

## Risks

- Developers may have `prek` installed but not `nilaway`; clear install hint reduces confusion.
- File selector must stay aligned with intended Go-side triggers; too broad adds friction, too narrow can skip needed analysis.
- NilAway may report existing issues and block first push after rollout; expected outcome for static analysis adoption.

## Rollout notes

After merge, developers should run:

```sh
go install go.uber.org/nilaway/cmd/nilaway@latest
make install-hooks
```
