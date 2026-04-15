# NilAway pre-push hook design

## Summary

Add dedicated `make nilaway` task that runs Uber's NilAway against first-party packages in this repo. Wire task into `prek` as `pre-push` hook so potential nil conditions surface before code leaves local machine.

## Goals

- Add explicit repo task for NilAway.
- Run NilAway before push, not on every commit.
- Skip NilAway on pure frontend pushes.
- Limit analysis to repo module package prefix.
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
- Determine current module path from `go list -m`.
- Check `nilaway` exists in `PATH`.
- If missing, print install command:
  - `go install go.uber.org/nilaway/cmd/nilaway@latest`
- Run:
  - `nilaway -include-pkgs="$(go list -m)" ./...`

Reasoning:
- Keeps NilAway scope aligned with `go.mod` automatically.
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
- `types_or = ["go", "go-mod", "go-sum"]`
- `pass_filenames = false`

Reasoning:
- Hook should run only when pushed changes include Go source or Go module metadata.
- `types_or` uses `identify` tags for Go source plus `go.mod` and `go.sum`.
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
- NilAway runs on `pre-push` only when pushed changes include Go-tagged files (`go`, `go-mod`, `go-sum`)
- NilAway must be installed separately
- install command for NilAway
- `make install-hooks` is preferred setup command

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
- Confirm command derives module path from `go list -m` and scopes `-include-pkgs` to that value.
- With `nilaway` absent from `PATH`, confirm `make nilaway` exits non-zero and prints install hint.
- Run `make install-hooks` and confirm both `.git/hooks/pre-commit` and `.git/hooks/pre-push` exist.
- Verify `prek.toml` uses `types_or = ["go", "go-mod", "go-sum"]` for hook selection.
- Run `prek run nilaway --stage pre-push --from-ref <old> --to-ref <new> --dry-run` to confirm pre-push selection.

### Scope rationale

No new app behavior, API behavior, or DB behavior. E2E test not needed for this tooling-only change.

## Acceptance details

### Makefile

- Add `nilaway` to `.PHONY`.
- Add `nilaway` target.
- Add `nilaway` to `help` output.
- Update `install-hooks` comments and help text to mention pre-commit and pre-push.

### prek.toml

- Install both `pre-commit` and `pre-push` hook shims.
- Add `nilaway` local hook with `types_or = ["go", "go-mod", "go-sum"]` and `stages = ["pre-push"]`.

### README

- Add `make nilaway` to target list.
- Rename hook section to cover both pre-commit and pre-push.
- Document `go install go.uber.org/nilaway/cmd/nilaway@latest`.
- Document `make install-hooks` as recommended hook installation command.

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
