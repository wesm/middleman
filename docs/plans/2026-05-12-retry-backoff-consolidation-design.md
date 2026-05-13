# Retry/Backoff Consolidation Design

## Context

Issue #299 follows PR #298, which introduced `github.com/cenkalti/backoff/v5` for transient git fetch failures in `internal/gitclone/retry.go`. Other code paths still express retry and wait policy ad hoc. Goal: make transient retry policy converge on one obvious package while keeping rate-limit gating separate.

## Goals

- Introduce one shared package for transient retry policy: `internal/retry`.
- Keep `cenkalti/backoff/v5` as only retry primitive for transient retries.
- Migrate existing transient retry wrapper logic out of `internal/gitclone` ownership.
- Leave `internal/ratelimit` public API and rate-limit gating semantics unchanged.
- Do not wrap rate-limit gates in `backoff.Retry`, `RetryAfterError`, or any new retry abstraction in this issue.
- Update retry documentation to list each retry/backoff site by path and classify it as transient retry, rate-limit gate, or scheduling cadence.
- Provide tests for shared retry helper and migrated callers.

## Non-goals

- Do not redesign `internal/ratelimit.RateTracker` APIs.
- Do not change rate-limit gate code in `internal/ratelimit/rate.go`, `internal/github/graphql.go`, or quota-gate call sites in `internal/github/sync.go` except comments/docs if needed.
- Do not change sync cadence loops backed by `time.NewTicker`, including out-of-scope scheduling paths in `internal/server/server.go` and cadence loops in `internal/github/sync.go`.
- Do not introduce exponential backoff for provider reset windows.
- Do not broaden provider-specific behavior beyond giving future providers one shared transient retry package.

## Recommended approach

Create `internal/retry` as canonical home for transient retry execution. Move generic `backoff/v5` orchestration there and let callers provide classification logic for which errors are transient.

For git clone/fetch work, keep git-specific transient matching but stop coupling that policy to `internal/gitclone` package ownership. `internal/gitclone` should call into `internal/retry` with its matcher.

Leave rate-limit checks in `internal/github/sync.go`, `internal/github/graphql.go`, and `internal/ratelimit/rate.go` unchanged in code for this issue. Those sites are quota gates, not transient retry schedules.

## Architecture

### New package: `internal/retry`

Responsibilities:

- Own `cenkalti/backoff/v5` usage for transient retries.
- Provide generic helper(s) that:
  - accept `context.Context`
  - accept label for logging
  - accept `backoff.BackOff`
  - accept max tries
  - accept transient-error classifier
  - wrap permanent errors with `backoff.Permanent`
  - log per-attempt retry notifications at debug level
- Expose production default schedule suitable for short transient upstream failures.
- Expose test seam for injected backoff schedule.

Non-responsibilities:

- No provider rate-limit logic.
- No ticker/scheduling loops.
- No package-specific error matching.

### `internal/gitclone`

Responsibilities after migration:

- Keep git-specific transient error matcher (`isTransientGitError`) unless later reused elsewhere.
- Call shared `internal/retry` helper from clone/fetch paths in `internal/gitclone/clone.go`.
- Keep existing retry budget and behavior unless migration requires trivial naming adjustments.
- If `internal/gitclone/retry.go` remains, it is thin git-specific adapter only. It must not own `backoff/v5` orchestration, production schedule, or separate test seam.

### `internal/ratelimit`

No API change. `ShouldBackoff()` remains public and tuple-shaped. It still represents quota exhaustion wait state, not retry schedule policy.

### `internal/github`

No semantic rewrite of rate-limit flow in this issue. Existing `ShouldBackoff()` call sites remain quota gates. If any transient retry helper already exists outside `gitclone`, migrate it to `internal/retry`; otherwise these files only need documentation touch points or tiny call-site cleanup.

## Site audit and migration intent

### `internal/gitclone/retry.go`

Current state:

- Owns `backoff/v5` orchestration.
- Owns default exponential schedule.
- Owns debug notify hook.
- Owns git transient classifier.

Target state:

- Shared orchestration moves to `internal/retry`.
- Git classifier stays local or moves only if clearly reusable.
- File either becomes thin adapter or disappears if call sites can invoke `internal/retry` directly without hurting clarity.

### `internal/ratelimit/rate.go`

Current state:

- `ShouldBackoff()` returns `(bool, time.Duration)` for exhausted quota windows.

Target state:

- No API or semantic change.
- Documentation should explicitly mark this as rate-limit gating, not transient retry/backoff schedule.

### `internal/github/graphql.go`

Current state:

- `GraphQLFetcher.ShouldBackoff()` is passthrough to rate tracker.

Target state:

- Unchanged in code.
- Documentation should call out that this path is quota gating and intentionally not migrated into `internal/retry`.

### `internal/github/sync.go`

Current state:

- Worker and GraphQL paths gate work with `ShouldBackoff()`.
- No evidence in current audit of separate transient `backoff/v5` helper here.

Target state:

- Unchanged in code and behavior for this issue.
- Update docs to classify these as quota-gate sites, not retry schedule sites.

## Data flow

### Transient retry flow

```text
caller
  -> classify error as transient or permanent
  -> internal/retry helper
       -> backoff.Retry(...)
       -> debug notify on each retry
       -> stop immediately on backoff.Permanent(err)
  -> caller receives final value/error
```

### Rate-limit gate flow

```text
caller
  -> RateTracker.ShouldBackoff()
  -> if false: proceed immediately
  -> if true: wait until provider reset window
```

These flows stay distinct by design.

## Testing plan

### Unit tests

- New `internal/retry` tests for:
  - retries transient errors until success
  - wraps non-transient errors as permanent and stops immediately
  - respects max tries
  - honors injected fast backoff schedule
- `internal/gitclone` tests updated to verify git matcher and integration with shared helper without re-testing `backoff/v5` internals.

### Integration / package-level tests

- Run existing `internal/gitclone` tests covering transient fetch retry behavior.
- If implementation touches `internal/github` call sites mechanically, add focused tests only for changed behavior or wiring.

### E2E expectation

- No new browser e2e expected because user-visible UI behavior should not change.
- If sync API behavior changes in a user-noticeable way during implementation audit, add server/e2e coverage then.

## Documentation plan

Update `context/retries-and-backoffs.md` to:

- point to `internal/retry` as canonical transient retry package
- classify each listed path as transient retry, rate-limit gate, or scheduling cadence
- list `internal/gitclone/retry.go` or replacement path plus `internal/gitclone/clone.go` as transient retry sites
- note `internal/ratelimit/rate.go`, `internal/github/graphql.go`, and quota-gate paths in `internal/github/sync.go` remain rate-limit gate sites outside transient retry policy
- note out-of-scope scheduling cadence paths in `internal/server/server.go` and `internal/github/sync.go`

## Risks and constraints

- Over-migrating rate-limit code would expand scope and mix two policies. Avoid.
- Moving helper code without preserving test seam could slow tests. Keep injectable backoff schedule.
- Duplicating transient matchers across packages would weaken consolidation. Keep shared execution wrapper central even if classifiers stay package-local.

## Open decisions

- Whether `internal/gitclone/retry.go` remains as thin git-specific adapter or disappears entirely should be decided during implementation based on whichever result is clearer with less churn.
