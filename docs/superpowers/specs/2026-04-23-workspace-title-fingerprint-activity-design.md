# Workspace Tmux Title And Fingerprint Activity Design

## Goal

Show a `Working` indicator in the workspace list when an agent in the workspace tmux session appears active, without requiring middleman to own every agent's hook/plugin configuration.

The signal should work passively for terminals middleman spawns today and should avoid language-specific prompt detection. In particular, the implementation must not infer waiting or idle states from English UI phrases such as "Allow once", "Reject", "Do you want", or similar text. Agents can render in any language and can change UI wording at any time.

## Observed Agent Behavior

Real tmux tests were run against local interactive agents by launching each CLI in a fresh tmux session and submitting prompts with `tmux send-keys`.

Codex interactive TUI changes `#{pane_title}` while it is processing a submitted prompt. The observed busy title uses a leading Braille spinner frame, for example `⠴ t3code-b5014b03` and `⠦ t3code-b5014b03`, then settles back to `t3code-b5014b03`.

OpenCode 1.14.19 does not appear to use Codex-style spinner frames. It starts with `pane_title=OpenCode`, then changes the title to a conversation/topic value such as `OC | Run sleep 8 and report OPENCODE_SLEEP...`. The title remained unchanged while OpenCode waited for permission and while an approved `sleep 8 && echo OPENCODE_SLEEP_DONE` command ran.

Pi 0.68.0 does not appear to encode activity in the tmux title. It kept a static title such as `π - tmp.HdeVVrxzNh` while processing a prompt and while running `sleep 8 && echo PI_SLEEP_DONE`.

These tests imply title detection is strong for Codex, but not enough for Pi or OpenCode. For those agents, pane-output activity is the best passive fallback.

## Non-Goals

Do not implement agent hook installation in this feature. Hooks and plugins remain a future high-accuracy mode.

Do not implement natural-language waiting detection from pane text.

Do not depend on child-process activity for the core signal. Process trees are too weak for model/network activity and are platform-specific across macOS and Linux.

Do not persist activity samples in SQLite. The state is short-lived UI state and can safely reset on server restart.

## State Model

Add an in-memory activity tracker keyed by tmux session name.

Each tracked session stores:

```go
type TmuxActivitySample struct {
    Session        string
    PaneTitle      string
    Fingerprint    string
    HasFingerprint bool
    LastChangedAt  time.Time
    LastSampledAt  time.Time
    Source         string // "title", "output", "none", "unknown"
    Working        bool
}
```

`Fingerprint` is a hash of recent captured pane output, not the raw output. The raw pane contents should not be stored in memory longer than needed to compute the hash.

Default timing constants:

```go
const tmuxActivityTTL = 30 * time.Second
const tmuxSampleMinInterval = 4 * time.Second
const tmuxCaptureScrollbackLines = 160
```

`tmuxActivityTTL` is the window during which recent pane output changes keep the workspace marked working. `tmuxSampleMinInterval` prevents duplicate clients or rapid refreshes from repeatedly spawning tmux commands for the same session.

## Signal Precedence

The server derives `Working` in this order:

1. Explicit title protocol says working.
2. Recent pane-output fingerprint changed within `tmuxActivityTTL`.
3. Otherwise not working.

Title protocol detection should be narrow and based on observed or configured protocols, not generic language matching.

Initial built-in title protocols:

```go
// Codex interactive spinner frames observed at the start of pane_title.
"⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏" followed by a space
```

The current generic English substring detector (`working`, `busy`, `thinking`, etc.) should be removed or moved behind explicit user configuration. It is not suitable as a default protocol detector.

## Sampling Algorithm

When `/api/v1/workspaces` or `/api/v1/workspaces/{id}` returns a ready workspace with a tmux session, the server samples that session.

Sampling steps:

1. If the session was sampled less than `tmuxSampleMinInterval` ago, reuse the cached sample.
2. Read title with `tmux display-message -p -t <session> "#{pane_title}"`.
3. Capture recent pane output with `tmux capture-pane -p -t <session> -S -160`.
4. Normalize the captured text by converting CRLF to LF and trimming trailing spaces from each line.
5. Hash the normalized text with SHA-256.
6. If this is the first fingerprint for the session, establish the baseline but do not mark output as recently changed.
7. If the fingerprint differs from the previous fingerprint, update `LastChangedAt = now`.
8. Set `Working = true` if the title protocol is working or `now - LastChangedAt <= tmuxActivityTTL`.
9. Set `Source = "title"` for explicit title working, `Source = "output"` for recent output activity, and `Source = "none"` otherwise.

First sample behavior is intentionally conservative. After server restart, a quiet pane should not show `Working` for 30 seconds just because it has captured output. It becomes output-active only after the server observes a change.

If tmux title or capture fails, return the workspace without a working signal and log at debug level with workspace id and tmux session. A transient tmux read failure must not make `/workspaces` fail.

## API Shape

Keep the existing compatibility fields:

```json
{
  "tmux_pane_title": "string | null",
  "tmux_working": true
}
```

Add optional diagnostic fields:

```json
{
  "tmux_activity_source": "title | output | none | unknown",
  "tmux_last_output_at": "RFC3339 timestamp | null"
}
```

`tmux_working` remains the UI boolean. It is true when either explicit title activity or recent output activity is present.

`tmux_activity_source` helps debugging and future UI copy without exposing implementation internals.

Do not expose the fingerprint hash over the API.

## Frontend Behavior

The workspace sidebar should poll `/api/v1/workspaces` while mounted. A 5 second interval is sufficient and aligns with the server throttle.

The existing SSE `workspace_status` listener should remain for create/error/ready transitions. Polling is required because output activity is not currently event-driven and will not emit `workspace_status`.

The row should show the existing spinner badge when `tmux_working` is true. The badge title can include the activity source for debugging, for example `Working: title` or `Working: output`.

The UI should not show a separate `Waiting` state from pane text. Waiting can be added later only from explicit hooks/plugins/status files.

## Tmux Manager API

Extend `internal/workspace.Manager` with a method that can read both title and recent pane output through the configured tmux command wrapper:

```go
type TmuxPaneSnapshot struct {
    Title  string
    Output string
}

func (m *Manager) TmuxPaneSnapshot(ctx context.Context, session string) (TmuxPaneSnapshot, error)
```

Implementation can use two tmux invocations initially:

```sh
tmux display-message -p -t "$session" "#{pane_title}"
tmux capture-pane -p -t "$session" -S -160
```

This preserves the existing configurable tmux wrapper behavior. Combining calls is not necessary for the first implementation.

## Concurrency

The activity tracker should guard its map with a mutex.

Sampling should avoid holding the mutex while running tmux commands. The flow should be:

1. Lock, check cache freshness, unlock.
2. Run tmux commands.
3. Lock, compare and update session state, unlock.

If two requests sample the same session concurrently, duplicate tmux calls are acceptable but should be rare. Avoid complicated singleflight unless profiling shows a problem.

## Tests

Add Go unit coverage for title protocol detection:

```text
Codex spinner frame => working
Settled Codex title => not working
OpenCode topic title => not working by title
Pi static title => not working by title
Generic English "working" title => not working by default
```

Add Go unit coverage for the activity tracker:

```text
First output fingerprint establishes baseline and is not working
Changed fingerprint marks output working
Unchanged fingerprint remains working until TTL expires
Unchanged fingerprint after TTL clears working
Explicit title working overrides quiet output
```

Extend server tests with a fake tmux wrapper that returns different `capture-pane` output across calls and verifies:

```text
GET /workspaces transitions tmux_working false -> true when output changes
GET /workspaces clears tmux_working after TTL
tmux read failures do not fail the endpoint
```

Add a frontend test or component-level test that verifies the badge appears for `tmux_working=true` and disappears after a later polled response with `tmux_working=false`.

Manual verification should include real tmux runs for:

```text
Codex prompt: title spinner sets Working
OpenCode prompt: output changes set Working, no title spinner assumed
Pi prompt: output changes set Working, no title spinner assumed
Quiet pane: Working clears after the configured TTL
```

## Implementation Plan

1. Add the in-memory activity tracker to `internal/server` or `internal/workspace`. Prefer `internal/server` if it only enriches API responses; prefer `internal/workspace` if future terminal APIs will also consume it.
2. Add `Manager.TmuxPaneSnapshot` using configured tmux command wrappers.
3. Replace `isWorkingTmuxTitle` generic token matching with narrow title protocol detection.
4. Enrich workspace responses with `tmux_working`, `tmux_activity_source`, and `tmux_last_output_at`.
5. Update OpenAPI-generated clients and frontend schema.
6. Add sidebar polling at a 5 second cadence.
7. Add backend and frontend tests.
8. Run focused Go tests, frontend checks, and relevant E2E coverage.

## Risks

Pane-output fingerprinting reports recent terminal activity, not true semantic model state. A model can be working silently for more than 30 seconds and the indicator will clear. This is acceptable for the passive fallback because it avoids false certainty.

Animated terminal UI spinners may keep output changing even when an agent is not semantically doing useful work. This is acceptable because it still reflects active terminal UI activity.

Large numbers of workspaces increase tmux command volume. The sample throttle and capture-line limit should keep the overhead bounded. If this becomes expensive, move sampling to a shared background ticker or add per-session singleflight.

Multiple browser clients can cause duplicate sampling. The minimum sample interval prevents most duplicate work.

Server restarts lose activity baselines. The UI will recover after the next observed output change.
