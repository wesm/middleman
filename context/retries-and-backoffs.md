# Retries, Backoff, and Single-Flight Dedup

middleman talks to upstreams that misbehave under load — GitHub's smart-HTTP
endpoint returns sporadic `500 Internal Server Error` on `/info/refs` when the
same repo is fetched in a tight burst, and concurrent callers inside the
process amplify that burst. The clone manager combines two patterns to absorb
both classes of failure: a bounded retry around transient errors, and a
single-flight slot that collapses concurrent identical work into one
underlying call.

## When to retry vs. when to dedup

- Use **retry** when the failure is transient on the upstream side and the
  same operation will likely succeed if you re-issue it shortly. The retry
  shape is exponential backoff with a small per-attempt cap and a fixed
  total budget.
- Use **single-flight** when multiple in-process callers are racing on the
  same external resource. Dedup is preferable to retry: it removes the
  cause (the burst) instead of papering over the effect. The two work
  together — the leader's call still gets retry coverage, followers piggy-
  back on its result.
- Both patterns assume the operation is idempotent over the relevant
  window. `git fetch --prune` is; an `INSERT` is not.

## Retry: `gitclone.retryTransient`

Defined in [`internal/gitclone/retry.go`](../internal/gitclone/retry.go).
Built on `github.com/cenkalti/backoff/v5` (already a direct dependency).

```go
out, err := retryTransient(ctx, "git fetch", func() ([]byte, error) {
    return m.git(ctx, host, clonePath, "fetch", "--prune", "origin")
})
```

What this does:

1. Calls the operation.
2. On error, checks `isTransientGitError(err)`:
   - **Transient** (`500/502/503/504`, `connection reset`, `could not
     resolve host`, `early EOF`, etc.) → retry with backoff.
   - **Permanent** (auth, not-found, malformed remote) → wrap in
     `backoff.Permanent` so the retry loop short-circuits immediately.
3. Schedule: `500ms → ~750ms → ~1.1s`, randomized ±30%, capped at 4s.
4. Maximum 3 attempts total. Configurable in tests via
   `retryTransientWithBackOff` (the test seam).
5. Per-attempt failures are logged at `DEBUG`; only the final failure
   surfaces to the caller. `slog.Warn` should fire once per persistent
   failure, not once per retry.

Use `retryTransientWithBackOff` with a microsecond-grain `ExponentialBackOff`
in tests so they stay fast (see
[`retry_test.go`](../internal/gitclone/retry_test.go) for the `fastBackOff`
helper).

### Extending `isTransientGitError`

Edit the slice in `retry.go` and add a row to the table in
`retry_test.go`. Keep the matcher conservative — false positives turn
permanent failures into multi-second hangs. Prefer matching substrings of
git stderr (e.g. `"returned error: 503"`) over guessing at exit codes.

### When NOT to use this helper

- HTTP REST calls already covered by `internal/github` rate limiting and
  budget logic — those have their own retry and budget tracker; do not
  layer another retry on top.
- Database operations. SQLite errors are surfaced directly; retrying
  blindly can hide schema or migration bugs.
- Anything with side effects beyond `git fetch`. The helper assumes
  re-running is safe.

## Single-flight: `Manager.ensureCloneShared`

Defined in [`internal/gitclone/clone.go`](../internal/gitclone/clone.go).
Built on `golang.org/x/sync/singleflight` (same package already used by
the syncer for `displayName` dedup).

```go
func (m *Manager) EnsureClone(
    ctx context.Context, host, owner, name, remoteURL string,
) error {
    return m.ensureCloneShared(ctx, host, owner, name, func(ctx context.Context) error {
        return m.ensureCloneLocked(ctx, host, owner, name, remoteURL)
    })
}
```

Key behaviors to preserve when modifying this path:

1. **Key shape**: `host \x00 owner \x00 name`. The null separator is
   load-bearing — without it, `owner=foo name=barbaz` collides with
   `owner=foobar name=baz`. There's a test (`TestEnsureCloneKey`) that
   pins this.
2. **Detached context for the runner**: the slot runs with
   `context.WithoutCancel(ctx)`. A canceled leader must not abort the
   in-flight work for followers attached to the same slot — they have
   their own contexts and expect the result. Each caller still observes
   its own cancellation via the surrounding `select`.
3. **DoChan, not Do**: `DoChan` lets each caller wait on its own
   `ctx.Done()` while the slot runs. `Do` would block all callers on the
   leader's call without context awareness.

### When to add new single-flight slots

Add one whenever multiple in-process call sites perform the same upstream
operation for the same logical resource. The clone manager case had three
callers (periodic syncer, per-PR detail sync, workspace setup) all calling
`EnsureClone(host, owner, name)` within seconds. Symptoms before the fix:
upstream 5xx bursts, lock contention on `.git/`, redundant network traffic.
Symptoms a dedup slot will hide: deadlocks, slow operations holding the
slot for too long, callers observing one-off stale state. Make the slot
small enough that holding it is cheap.

## Testing patterns

### Retry tests

Inject a fake operation that fails N times then succeeds, asserts:

- Success after recovery returns the value and reports the correct call
  count.
- Permanent errors short-circuit (one call, original error wrapped).
- Transient errors exhaust the retry budget (exactly `retryAttempts`
  calls).
- A canceled context cuts the loop short and surfaces `context.Canceled`.

See `TestRetryTransient*` in
[`retry_test.go`](../internal/gitclone/retry_test.go).

### Single-flight tests

Two-phase pattern: a **leader** goroutine takes the slot first and blocks
inside the operation. Once the leader signals it has started, the test
spawns **followers**. Followers are guaranteed to attach to the leader's
slot because the slot is held until the test closes the release channel.

Verify:

- Concurrent same-key callers result in exactly one operation invocation.
- Distinct keys do not share a slot.
- Cancelling the leader does not abort the operation for followers.
- Operation errors propagate to all callers attached to the slot.

See `TestEnsureCloneShared*` in
[`ensure_shared_test.go`](../internal/gitclone/ensure_shared_test.go).
Always run these with `-race`; singleflight bugs typically manifest as
data races on the slot map.

## Related context

- [`context/github-sync-invariants.md`](./github-sync-invariants.md) for
  how the periodic syncer composes with the clone manager.
- [`context/workspace-runtime-lifecycle.md`](./workspace-runtime-lifecycle.md)
  for the workspace setup flow that also calls `EnsureClone`.
