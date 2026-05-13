# Retry/Backoff Consolidation Implementation Plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Consolidate transient retry orchestration under a new `internal/retry` package while leaving rate-limit gating code unchanged.

**Architecture:** Extract generic `cenkalti/backoff/v5` execution into `internal/retry`, keep git-specific transient classification in `internal/gitclone`, and update docs to point contributors at one canonical transient retry package. Rate-limit gates in `internal/ratelimit` and `internal/github` stay as-is and are documented as separate from transient retry.

**Tech Stack:** Go, `github.com/cenkalti/backoff/v5`, testify, existing `gitclone` and context docs

---

## File map

- Create: `internal/retry/retry.go` — shared transient retry helper and default production schedule
- Create: `internal/retry/retry_test.go` — unit tests for retry classification, permanent wrapping, and try budget
- Modify: `internal/gitclone/retry.go` — keep git-specific matcher and delegate to `internal/retry`, or remove file if delegation is clearer
- Modify: `internal/gitclone/retry_test.go` — keep matcher tests; move shared orchestration assertions to `internal/retry`
- Modify: `context/retries-and-backoffs.md` — classify all retry-related sites by path as transient retry, rate-limit gate, or scheduling cadence

### Task 1: Shared transient retry package

**TDD scenario:** New feature — full TDD cycle

**Files:**
- Create: `internal/retry/retry.go`
- Create: `internal/retry/retry_test.go`

- [ ] **Step 1: Write failing tests for generic transient retry behavior**

```go
package retry

import (
    "errors"
    "testing"
    "time"

    "github.com/cenkalti/backoff/v5"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func fastBackOff() *backoff.ExponentialBackOff {
    bo := backoff.NewExponentialBackOff()
    bo.InitialInterval = time.Microsecond
    bo.MaxInterval = time.Microsecond
    bo.RandomizationFactor = 0
    return bo
}

func TestDoStopsOnPermanentError(t *testing.T) {
    require := require.New(t)
    assert := assert.New(t)

    calls := 0
    permanent := errors.New("permanent")

    _, err := DoWithBackOff(t.Context(), Config[string]{
        Label:      "test",
        BackOff:    fastBackOff(),
        MaxTries:   3,
        IsTransient: func(err error) bool { return false },
        Op: func() (string, error) {
            calls++
            return "", permanent
        },
    })

    require.ErrorIs(err, permanent)
    assert.Equal(1, calls)
}

func TestDoRetriesTransientErrorUntilBudgetExhausted(t *testing.T) {
    require := require.New(t)
    assert := assert.New(t)

    calls := 0
    transient := errors.New("transient")

    _, err := DoWithBackOff(t.Context(), Config[string]{
        Label:      "test",
        BackOff:    fastBackOff(),
        MaxTries:   3,
        IsTransient: func(err error) bool { return true },
        Op: func() (string, error) {
            calls++
            return "", transient
        },
    })

    require.ErrorIs(err, transient)
    assert.Equal(3, calls)
}

func TestDoRetriesTransientErrorUntilSuccess(t *testing.T) {
    require := require.New(t)
    assert := assert.New(t)

    calls := 0

    got, err := DoWithBackOff(t.Context(), Config[string]{
        Label:      "test",
        BackOff:    fastBackOff(),
        MaxTries:   3,
        IsTransient: func(err error) bool { return true },
        Op: func() (string, error) {
            calls++
            if calls < 3 {
                return "", errors.New("transient")
            }
            return "ok", nil
        },
    })

    require.NoError(err)
    assert.Equal("ok", got)
    assert.Equal(3, calls)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/retry -run 'TestDo' -shuffle=on`
Expected: FAIL with undefined `DoWithBackOff`, `Config`, or missing package errors

- [ ] **Step 3: Write minimal shared retry implementation**

```go
package retry

import (
    "context"
    "log/slog"
    "time"

    "github.com/cenkalti/backoff/v5"
)

const DefaultMaxTries = 3

type Config[T any] struct {
    Label       string
    BackOff     backoff.BackOff
    MaxTries    uint
    IsTransient func(error) bool
    Op          func() (T, error)
}

func DefaultBackOff() *backoff.ExponentialBackOff {
    expo := backoff.NewExponentialBackOff()
    expo.InitialInterval = 500 * time.Millisecond
    expo.MaxInterval = 4 * time.Second
    expo.RandomizationFactor = 0.3
    return expo
}

func Do[T any](ctx context.Context, cfg Config[T]) (T, error) {
    cfg.BackOff = DefaultBackOff()
    if cfg.MaxTries == 0 {
        cfg.MaxTries = DefaultMaxTries
    }
    return DoWithBackOff(ctx, cfg)
}

func DoWithBackOff[T any](ctx context.Context, cfg Config[T]) (T, error) {
    wrapped := func() (T, error) {
        v, err := cfg.Op()
        if err == nil {
            return v, nil
        }
        if cfg.IsTransient != nil && cfg.IsTransient(err) {
            return v, err
        }
        return v, backoff.Permanent(err)
    }

    notify := func(err error, next time.Duration) {
        slog.Debug("retrying transient failure", "op", cfg.Label, "next", next, "err", err)
    }

    return backoff.Retry(ctx, wrapped,
        backoff.WithBackOff(cfg.BackOff),
        backoff.WithMaxTries(cfg.MaxTries),
        backoff.WithNotify(notify),
    )
}
```

- [ ] **Step 4: Run package tests to verify green**

Run: `go test ./internal/retry -shuffle=on`
Expected: PASS

- [ ] **Step 5: Commit shared helper extraction**

```bash
git add internal/retry/retry.go internal/retry/retry_test.go
git commit -m "refactor: centralize transient retry orchestration" \
  -m "Issue #299 needs one obvious package for cenkalti/backoff/v5 usage so future network callers do not grow ad hoc retry wrappers.\n\nMove the generic retry schedule, permanent-error wrapping policy, and debug notify hook into internal/retry while leaving package-specific transient classification at call sites."
```

### Task 2: Migrate gitclone to shared helper

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `internal/gitclone/retry.go`
- Modify: `internal/gitclone/retry_test.go`
- Test: `internal/gitclone/retry_test.go`

- [ ] **Step 1: Run existing gitclone retry tests first**

Run: `go test ./internal/gitclone -run 'Test(IsTransientGitError|RetryTransient)' -shuffle=on`
Expected: PASS

- [ ] **Step 2: Add or update failing gitclone test for delegation shape if needed**

```go
func TestRetryTransientUsesSharedHelperBudget(t *testing.T) {
    require := require.New(t)
    assert := assert.New(t)

    calls := 0
    _, err := retryTransientWithBackOff(t.Context(), "test", fastBackOff(), func() (string, error) {
        calls++
        return "", errors.New("remote: Internal Server Error")
    })

    require.Error(err)
    assert.Equal(retry.DefaultMaxTries, uint(calls))
}
```

If this test becomes redundant after delegation, remove the old budget assertions from `internal/gitclone/retry_test.go` instead of duplicating `internal/retry` coverage.

- [ ] **Step 3: Replace gitclone-owned orchestration with shared helper call**

```go
func retryTransient[T any](ctx context.Context, label string, op func() (T, error)) (T, error) {
    return retry.Do(ctx, retry.Config[T]{
        Label:       label,
        MaxTries:    retry.DefaultMaxTries,
        IsTransient: isTransientGitError,
        Op:          op,
    })
}

func retryTransientWithBackOff[T any](ctx context.Context, label string, bo backoff.BackOff, op func() (T, error)) (T, error) {
    return retry.DoWithBackOff(ctx, retry.Config[T]{
        Label:       label,
        BackOff:     bo,
        MaxTries:    retry.DefaultMaxTries,
        IsTransient: isTransientGitError,
        Op:          op,
    })
}
```

If `internal/gitclone/retry.go` becomes a near-empty adapter, keep only matcher plus adapter functions. If clearer, delete the file and move matcher into a small `internal/gitclone/transient.go` file.

- [ ] **Step 4: Run gitclone tests and targeted shared tests**

Run: `go test ./internal/retry ./internal/gitclone -shuffle=on`
Expected: PASS

- [ ] **Step 5: Commit gitclone migration**

```bash
git add internal/gitclone/retry.go internal/gitclone/retry_test.go internal/retry/retry.go internal/retry/retry_test.go
git commit -m "refactor: point git clone retries at shared helper" \
  -m "Git operations were still the only package owning retry orchestration after PR #298, which made the extraction target for issue #299 unclear.\n\nKeep git-specific transient matching local, but route execution through internal/retry so future callers can share the same cenkalti/backoff/v5 policy without depending on gitclone internals."
```

### Task 3: Update retry pattern documentation

**TDD scenario:** Trivial change — use judgment

**Files:**
- Modify: `context/retries-and-backoffs.md`

- [ ] **Step 1: Update doc classification by path**

Add sections or bullets that classify each site explicitly:

```md
## Transient retry

- `internal/retry/retry.go` — canonical shared transient retry wrapper over `cenkalti/backoff/v5`
- `internal/gitclone/clone.go` — clone, fetch, and `remote set-head` operations routed through `internal/retry`
- `internal/gitclone/retry.go` (if retained) — git-specific transient matcher and thin adapter only

## Rate-limit gates

- `internal/ratelimit/rate.go` — `ShouldBackoff()` reports quota-window waits; not transient retry
- `internal/github/graphql.go` — `GraphQLFetcher.ShouldBackoff()` passthrough for quota gating
- `internal/github/sync.go` — worker and GraphQL gating sites that wait on provider reset windows

## Scheduling cadence (out of scope)

- `internal/server/server.go` — periodic SSE refresh tickers
- `internal/github/sync.go` — periodic sync cadence tickers
```

- [ ] **Step 2: Verify doc paths against code**

Run: `grep -RIn 'internal/retry\|ShouldBackoff\|time.NewTicker\|retryTransient' context/retries-and-backoffs.md internal/gitclone internal/github internal/ratelimit internal/server`
Expected: output references align with current code paths

- [ ] **Step 3: Commit doc update**

```bash
git add context/retries-and-backoffs.md
git commit -m "docs: clarify retry and rate-limit boundaries" \
  -m "Issue #299 is easy to over-interpret into quota gating and cadence loops.\n\nClassify each path as transient retry, rate-limit gate, or scheduling cadence so contributors can extend the shared retry package without changing unrelated wait policies."
```

### Task 4: Final verification and cleanup

**TDD scenario:** Modifying tested code — run affected tests after change

**Files:**
- Verify only: `internal/retry/retry.go`, `internal/retry/retry_test.go`, `internal/gitclone/retry.go`, `internal/gitclone/retry_test.go`, `context/retries-and-backoffs.md`

- [ ] **Step 1: Run focused verification suite**

Run: `go test ./internal/retry ./internal/gitclone -shuffle=on`
Expected: PASS

- [ ] **Step 2: Run broader regression check for clone/sync packages if retry extraction touched shared imports**

Run: `go test ./internal/gitclone ./internal/github -shuffle=on`
Expected: PASS

- [ ] **Step 3: Run diff hygiene checks**

Run: `git diff --check && git diff --cached --check`
Expected: no output

- [ ] **Step 4: Create final commit if verification work changed files**

```bash
git add internal/retry/retry.go internal/retry/retry_test.go internal/gitclone/retry.go internal/gitclone/retry_test.go context/retries-and-backoffs.md
git commit -m "test: lock retry consolidation behavior" \
  -m "Finish issue #299 verification after the retry extraction so future edits keep the shared helper contract, git transient matcher, and documentation boundaries aligned."
```

If no files changed during verification, skip this commit.
