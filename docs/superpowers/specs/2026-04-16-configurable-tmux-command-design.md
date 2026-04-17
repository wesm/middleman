# Configurable tmux Launch Command

## Problem

middleman shells out to `tmux` at four call sites to create, query,
attach to, and kill per-workspace tmux sessions. The command name is
hard-coded to the literal string `"tmux"` everywhere. Operators who
run middleman under a hardened systemd unit (tight sandboxing,
restricted namespaces, read-only paths, etc.) have no way to launch
the tmux session under a different set of permissions than the
middleman service itself.

The motivating use case is `systemd-run --user --scope tmux ...`,
which moves the tmux server into its own transient scope so it is
not bound by the middleman service's sandboxing. Without this, the
only options are to loosen sandboxing on the whole service or to
disable the workspace terminal feature.

## Goals

- Let operators prefix every `tmux` invocation with an arbitrary
  command + argv prefix (e.g. `systemd-run --user --scope tmux`).
- Keep existing behavior bit-for-bit when the setting is unset.
- Apply consistently across all four call sites. No per-site knobs.

## Non-goals

- No per-workspace override.
- No templating, no env-var interpolation, no shell parsing.
- No change to the arguments middleman passes after the tmux binary
  (e.g. `new-session -d -s ... -c ...`). Only the command + argv
  prefix is configurable.
- No runtime reconfiguration. The command is read once at startup;
  editing `tmux.command` in `config.toml` requires a middleman
  restart to take effect on the live process. Settings-API writes
  (`PUT /settings`, `POST /repos`, `DELETE /repos`) preserve the
  `[tmux]` section on disk but do not reload the running server.
- No changes to the settings API or UI. `tmux.command` is a
  file-only setting, edited by operators who are configuring a
  hardened systemd unit. A misconfigured wrapper breaks the
  workspace feature and should require a deliberate config-file
  edit + restart.
- No README entry, no `middleman config` subcommand output, no CLI
  help text beyond what the sample stanza below provides. The
  feature is a last-resort escape hatch for a specific sandboxing
  scenario, not a general-purpose knob — matching how the
  internal-only `[roborev]` section is handled today.

## Operator reference

The one supported configuration shape, to be copy-pasted into
`~/.config/middleman/config.toml` (or wherever the operator keeps
it):

```toml
# tmux launch command wrapper (optional). Configure ONLY when
# middleman runs under a sandbox that prevents tmux from creating
# its server, e.g. a hardened systemd unit. The first element is
# exec'd; remaining elements are its leading arguments. Include the
# tmux binary somewhere in this list (conventionally last).
# middleman does NOT append "tmux" — the config must provide it.
#
# Changes take effect on middleman restart.
[tmux]
command = ["systemd-run", "--user", "--scope", "tmux"]
```

This spec is the operator-facing reference. The project has no
shipped CHANGELOG or release-notes artifact; discovery is through
the PR description on merge and this spec in the repo, which are
the same surface operators already use to find the `[roborev]`
section.

## Trust model

`tmux.command` is arbitrary local process execution: whatever the
first element of the array names gets exec'd on every workspace
operation. The configuration is trusted operator input on the same
footing as `github_token_env` or `data_dir` — it must come from a
`config.toml` the operator controls. Environments that ingest a
`config.toml` from an untrusted source (e.g. a shared multi-tenant
directory or a user-uploaded file) must not enable this setting,
because a malicious value turns every workspace create, terminal
attach, and workspace delete into arbitrary code execution under
the middleman process's credentials. Middleman makes no defense
against this case — the validation only checks structural shape,
not identity or safety of the binary.

## Design

### Config

A new optional `[tmux]` section in `config.toml`:

```toml
[tmux]
command = ["systemd-run", "--user", "--scope", "tmux"]
```

- `command` is a TOML array of strings representing the **full
  argv** that replaces middleman's conceptual `"tmux"` slot. The
  first element is exec'd; the remaining elements are that
  process's leading arguments. middleman then appends its own
  subcommand argv (`new-session ...`, `has-session ...`, etc.)
  after the configured array.
- The operator is required to include the `tmux` binary somewhere
  in `command` (typically as the last element). middleman does
  **not** append `tmux` on its own. A configuration like
  `["systemd-run", "--user", "--scope"]` (without `tmux`) is
  unsupported — the wrapper would see `new-session` as its
  subcommand and fail to exec anything useful. Validation does
  not detect this form because there is no reliable heuristic; the
  failure surfaces at the first workspace create.
- When `[tmux]` is omitted or `command` is an empty array, the
  effective value is `["tmux"]`, preserving today's behavior.

### Go types

In `internal/config/config.go`:

```go
type Tmux struct {
    Command []string `toml:"command,omitempty"`
}

type Config struct {
    // ... existing fields ...
    Tmux Tmux `toml:"tmux"`
}
```

Only a `toml` tag — `Tmux.Command` is not exposed via the settings
API, matching how the internal-only `Roborev` subsection is tagged.

Add a helper on `*Config`:

```go
// TmuxCommand returns the command + argv prefix used to invoke
// tmux. Defaults to ["tmux"] when c is nil or the setting is
// unconfigured.
func (c *Config) TmuxCommand() []string {
    if c == nil || len(c.Tmux.Command) == 0 {
        return []string{"tmux"}
    }
    return append([]string(nil), c.Tmux.Command...)
}
```

The helper is nil-safe because the server constructor chain
(`server.New` and `server.NewWithConfig` both route through
`newServer`) commonly runs with `cfg == nil` in tests — many
existing tests in `internal/server` pass `nil` for cfg while still
exercising the workspace/terminal paths (`api_test.go:4887`
wires up workspaces with `cfg == nil`). The helper returns a copy
so callers can safely `append` without mutating the config.

### Validation

In `Config.Validate`:

- If `Tmux.Command` is set (non-nil) and non-empty, the first
  element must contain non-whitespace after `strings.TrimSpace`.
  Reject both `""` and whitespace-only values (`"   "`) with a
  clear error. A whitespace-only executable would otherwise sneak
  past a plain `== ""` check and fail with a confusing shell-level
  error at first workspace create.
- An empty or missing array is valid (uses default).

Config validation only checks structure. It does not stat the
executable or confirm it is on PATH — a missing binary surfaces
at runtime when middleman first tries to create a workspace, with
the existing exec error path. This matches how middleman currently
treats `tmux` itself.

### Call-site changes

All four call sites that currently hard-code `"tmux"` switch to
using the configured prefix. The arguments after the prefix are
unchanged.

1. `internal/workspace/manager.go` `newTmuxSession` — creates the
   detached session.
2. `internal/workspace/manager.go` `TmuxSessionExists` — runs
   `has-session`.
3. `internal/workspace/manager.go` `Delete` — runs `kill-session`.
4. `internal/terminal/handler.go` `ServeHTTP` — runs
   `attach-session` under a PTY.

`workspace.Manager` gains a `tmuxCmd []string` field set at
construction. `terminal.Handler` gains an equivalent field. Both
are populated from `cfg.TmuxCommand()` inside `newServer`
(`internal/server/server.go`), which both `server.New` and
`server.NewWithConfig` route through and which is where the
`workspace.Manager` and `terminal.Handler` are constructed today.
`cfg` is already available there, so no plumbing through
`ServerOptions` is required. Because `TmuxCommand()` is nil-safe,
the call is `cfg.TmuxCommand()` without a guard — tests that pass
`cfg == nil` will receive the default `["tmux"]` prefix.
Field visibility should mirror each type's existing construction
convention rather than being forced to match across types:
`workspace.Manager` already takes dependencies via setter methods
(`SetClones`), so the tmux field is unexported with a
`SetTmuxCommand` setter; `terminal.Handler` is populated via a
struct literal in `newServer` (`&terminal.Handler{Workspaces: ...}`),
so the tmux field is exported. The asymmetry is load-bearing — it
keeps both types consistent with their existing call sites — and
is not something the implementer needs to "pick."

Because `EnsureTmux` and `TmuxSessionExists` are currently
package-level functions, they become methods on `*Manager` (or take
an explicit command prefix) so they can reach the configured
command. The terminal handler calls `Manager.EnsureTmux` through
its existing `*workspace.Manager` dependency rather than the
package-level function.

### has-session failure handling

Wrapping tmux adds a second exec layer to every call, which forces
a contract the unwrapped code could leave implicit: when
`has-session` fails, is it tmux saying "session missing" or the
wrapper saying "I could not run tmux at all"? The answer decides
whether `EnsureTmux` falls through to `new-session` (correct for
session-missing) or surfaces the error (correct for every other
failure — a broken wrapper will fail new-session the same way and
the user sees a misleading error about the second call instead of
the first).

The heuristic `tmuxSessionExists` uses, top to bottom:

- If the command runs and exits 0, the session exists — return
  `(true, nil)`.
- If exec itself fails (binary missing, ENOENT, context cancelled),
  the error is not an `*exec.ExitError`. Propagate it — the caller
  cannot safely assume "session missing" when the wrapper or binary
  could not run.
- If it is an `*exec.ExitError` with exit code != 1, propagate the
  error. tmux's `has-session` exits 1 specifically when the target
  session is absent; other exit codes (127 "command not found", 203
  "exec failed", 126 "permission denied", etc.) signal wrapper
  misconfiguration, not a missing tmux session.
- If it is an `*exec.ExitError` with exit code 1, inspect **stderr
  only** (stdout is not load-bearing — a wrapper may emit anything
  there). If stderr contains either `can't find session` or
  `no server running`, the session is genuinely absent — return
  `(false, nil)`. Otherwise propagate as an error.

Consequences:

- `tmuxSessionExists` returns `(bool, error)`. The old
  `runCmd(...) == nil` boolean collapses the distinction above and
  is insufficient.
- `cmd.Stdout` and `cmd.Stderr` must be captured into separate
  buffers so the heuristic can look at stderr alone. `CombinedOutput`
  merges them and lets stdout content spoof the session-absent
  signal.
- `EnsureTmux` wraps the non-absent error as `"tmux has-session: %w"`
  so the error message identifies which call surfaced the
  misconfiguration.

Assumption: tmux's stderr phrases `can't find session` and
`no server running` have been stable across multiple major tmux
releases and are the observable contract we pin against.
Environments running a heavily patched tmux fork that renames
these messages must either reconfigure the wrapper to translate
them, or accept that middleman will surface the non-match as a
wrapper-failure error and refuse to create sessions. This
trade-off sits on the operator's side; middleman does not try to
recover from an unknown tmux.

### Wrapper requirements

Operators writing a wrapper must ensure it:

- Forwards argv unchanged from the wrapper's own arguments onward.
  middleman appends tmux's subcommand (`new-session`, `has-session`,
  etc.) after the configured argv, and a wrapper that rewrites
  these loses the tmux verb.
- Preserves stdio for `attach-session`. The terminal handler runs
  `attach-session` under a PTY via `creack/pty.StartWithSize`; a
  wrapper that daemonizes, redirects stdio, or detaches before
  tmux takes over breaks interactive attach even when the
  non-interactive verbs (`new-session -d`, `has-session`,
  `kill-session`) still work.
- Exits with the child process's status for non-attach verbs, or at
  least preserves tmux's exit-1-for-missing-session behavior.
  Wrappers that remap exit codes will confuse the has-session
  heuristic.
- Does not buffer or rewrite stderr. The session-absent heuristic
  matches tmux's stderr phrases verbatim; a wrapper that captures
  stderr and re-emits it on stdout (or translates the message)
  defeats the match.

`systemd-run --user --scope tmux` is the motivating example and
meets all four requirements on paper: it inherits stdio, runs tmux
in the foreground, and forwards exit codes. The automated tests
exercise synthetic shell wrappers and a recording binary, not
`systemd-run` itself — operators deploying this wrapper should
validate the full attach path manually on their target host before
relying on it. Small `sh` wrappers that only `exec tmux "$@"`
(optionally after `exec systemd-run --user --scope`) are equally
fine and are the simplest way to narrow the scope of what the
wrapper does before handing off to tmux.

### Operator-facing failure surface

When the wrapper is misconfigured, the failure needs to be visible
to the operator without a deep dive into source. The surfaces:

- `EnsureTmux` errors are logged via `slog.Error("ensure tmux",
  "err", ...)` inside `terminal.Handler.ServeHTTP` before the
  connection closes.
- The WebSocket terminal connection closes with
  `websocket.StatusInternalError` (1011) and reason `"failed to
  start tmux"`. A browser client observing the close frame knows
  the session could not start.
- Workspace setup (`new-session` through the wrapper) surfaces the
  same `exec.ExitError` wrapping through the existing workspace
  `error` status; the UI already shows that state.

These are existing channels; no new logging or error types are
introduced. The design ensures misconfiguration reaches them
promptly rather than being silently retried as "session absent."

Assembling the exec call is a one-liner — prefix + args, pass to
`exec.CommandContext`. Either inline it at each call site or
extract an exported helper that both `workspace` and `terminal`
can import. Sketch of an exported form:

```go
// TmuxExec returns an *exec.Cmd for `<prefix...> <args...>`.
// The helper makes a defensive copy of prefix so the caller may
// reuse it.
func TmuxExec(ctx context.Context, prefix []string, args ...string) *exec.Cmd {
    full := append(append([]string(nil), prefix...), args...)
    return exec.CommandContext(ctx, full[0], full[1:]...)
}
```

If inlined instead, the terminal handler still needs to set
`cmd.Env` after construction — the helper only builds the command,
it does not manage env or attach PTYs.

### Behavior when wrapper is set

Example with `command = ["systemd-run", "--user", "--scope", "tmux"]`:

| site | existing argv | new argv |
|------|---------------|----------|
| new-session | `tmux new-session -d -s S -c C shell -l` | `systemd-run --user --scope tmux new-session -d -s S -c C shell -l` |
| has-session | `tmux has-session -t S` | `systemd-run --user --scope tmux has-session -t S` |
| kill-session | `tmux kill-session -t S` | `systemd-run --user --scope tmux kill-session -t S` |
| attach-session | `tmux attach-session -t S` | `systemd-run --user --scope tmux attach-session -t S` |

Operators who only want the server (new-session) wrapped can point
`command` at a small script that discriminates on `$1 == "new-session"`
and execs `tmux` directly otherwise. This keeps middleman's wiring
simple.

## Testing

E2E and unit coverage live in the existing packages:

1. `internal/config` — table-driven test: omitted `[tmux]`,
   explicit empty array, single-element `["tmux"]`, multi-element
   wrapper, invalid empty-first-element (expect error), invalid
   whitespace-only first-element (expect error).
2. `internal/config` — `TmuxCommand()` returns a defensive copy
   (mutating the result must not affect the config). Nil-receiver
   returns the default `["tmux"]`.
3. `internal/config` — `Save` round-trip: a config with a
   populated `[tmux]` section reloads with the same slice.
4. `internal/workspace` — with a fake prefix that points at a test
   helper binary (or a shell script written to `t.TempDir()`),
   assert that `newTmuxSession`, `tmuxSessionExists`, and
   `Delete`'s kill-session path invoke the prefix + expected argv.
   Additionally, pin down the `has-session` failure contract with
   four cases: binary missing (non-ExitError propagates), exit code
   != 1 (propagates), exit 1 with non-tmux stderr (propagates),
   exit 1 with the tmux phrase on stdout only (propagates — not
   session-absent). Verify argv via a recording binary that writes
   NUL-delimited argv to a file.
5. `internal/server` e2e — spin up the full HTTP server with a
   config that sets `tmux.command` to a recording script. Cover:
   - Create a workspace via the API, assert the recorded argv
     matches the prefix + `new-session ...`.
   - Dial the `/api/v1/workspaces/{id}/terminal` WebSocket and
     assert the recorded argv for the `attach-session` invocation
     matches `prefix + attach-session -t <session>`.
   - Delete the workspace via the API, assert the recorded argv
     matches the prefix + `kill-session -t <session>`.
   - Wrapper-failure surface: a `has-session` script that fails
     (exit != 1, exit 1 with non-tmux stderr, or exit 1 with the
     tmux phrase on stdout only) must close the attach WebSocket
     with `websocket.StatusInternalError` rather than silently
     proceeding to `new-session` through the same broken wrapper.
   - One settings mutation path (`PUT /api/v1/settings` is the
     cheapest) must preserve the `[tmux]` section across the
     handler's `(*Config).Save` round-trip. All three mutation
     routes (`PUT /settings`, `POST /repos`, `DELETE /repos`) use
     the same `Save` implementation; one e2e is enough to regression-
     gate the mechanism.
6. Default-path coverage — existing workspace/terminal tests
   continue to pass without any config change, confirming the
   no-wrapper default is unchanged.

The recording helper must record argv unambiguously — a naive
`echo "$@"` collapses whitespace and cannot distinguish empty
arguments from absent ones. Use NUL-delimited output so the test
can recover the exact argv:

```sh
#!/bin/sh
# Appends "<argc>\0<arg0>\0<arg1>\0...\0" per invocation.
printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"
```

The test reads `$TMUX_RECORD`, splits on NUL, and compares against
the expected argv slice. A tiny Go helper built with `go test -c`
is equally acceptable (and avoids the shell entirely), but the
shell form is enough and matches existing test doubles in the
project.

## Success criteria

The work is complete when all of the following hold:

- Default-path parity: with no `[tmux]` section in `config.toml`,
  every tmux exec middleman issues is byte-for-byte identical to
  today's invocation. Existing tests pass unchanged.
- Wrapper-path coverage at every call site: with
  `tmux.command = [prefix...]` set, `new-session`, `has-session`,
  `kill-session`, and `attach-session` all run as
  `<prefix...> <original argv...>`.
- has-session failure contract (see "has-session failure handling"
  above) holds: only `exit code 1 + stderr containing "can't find
  session" or "no server running"` is treated as session-absent.
  All other failure modes (non-ExitError, other exit codes, exit 1
  without the stderr match) propagate so wrapper misconfiguration
  surfaces on the first call.
- Save-round-trip: `(*Config).Save` preserves the `[tmux]` section
  rather than silently erasing it on the next write. All three
  settings-UI mutation routes (`PUT /api/v1/settings`,
  `POST /api/v1/repos`, `DELETE /api/v1/repos`) call the same
  `Save`, so one e2e on any of them regression-gates the
  mechanism.
- Validation: a `tmux.command` whose first element is empty or
  whitespace-only fails config load with a clear error.
- Nil-cfg safety: server constructors (`server.New` with `cfg ==
  nil`, common in tests) continue to work with the default
  `["tmux"]` prefix.
- Required e2e coverage: `internal/server` has end-to-end tests
  that drive the real HTTP stack with a recording script wired as
  `tmux.command` and assert the prefix reaches `new-session`
  (via `POST /api/v1/workspaces`), `attach-session` (via
  `GET /api/v1/workspaces/{id}/terminal` WebSocket), and
  `kill-session` (via `DELETE /api/v1/workspaces/{id}`). The
  attach path also has coverage for the wrapper-failure surface
  (WebSocket closes with `StatusInternalError` when has-session
  fails for non-absent reasons).

## Rollout

No migration, no feature flag. Behavior is identical for anyone
who does not set `tmux.command`.

The change is staged across several bisectable commits — one per
package layer (config, workspace manager, terminal handler,
server wiring) plus the e2e suite — so each commit builds and
tests cleanly in isolation. The commit count is an implementation
detail of the staged plan; see `docs/superpowers/plans/` for the
concrete sequencing.
