# Retries, Backoff, and Single-Flight Dedup

middleman shells out to flaky upstreams (GitHub smart-HTTP returns sporadic
5xx on `/info/refs`). Two patterns live in
[`internal/gitclone`](../internal/gitclone/) and are reusable elsewhere.

## Retry: `retryTransient`

[`retry.go`](../internal/gitclone/retry.go). Wraps an idempotent op in
`cenkalti/backoff/v5`. Retries only when `isTransientGitError` matches
(5xx, `connection reset`, `could not resolve host`, `early EOF`). Permanent
errors short-circuit via `backoff.Permanent`. Schedule: 500ms → ~4s, jitter
±30%, 3 attempts. Per-attempt errors log at `DEBUG`; only the final
failure surfaces.

Use it for idempotent shell-outs that hit a flaky network upstream. Do
not stack it on top of paths that already retry (e.g. `internal/github`
REST client). Test against `retryTransientWithBackOff` with a
microsecond `ExponentialBackOff` so tests stay fast.

To extend matchers, add a substring to the slice in `retry.go` and a row
to `retry_test.go`. Keep the matcher conservative — false positives turn
permanent failures into multi-second hangs.

## Single-flight: `Manager.EnsureClone`

[`clone.go`](../internal/gitclone/clone.go). `EnsureClone` opens a
`singleflight` slot keyed on `(host, owner, name)` so concurrent
callers (periodic syncer, per-PR detail syncs, workspace setup) share
one underlying clone/fetch instead of stampeding GitHub.

Invariants to preserve:

- **Pre-check `ctx.Err()`**. A caller whose ctx is already canceled
  must not enter the slot, or the runner does work for nobody.
- **Key shape** `host \x00 owner \x00 name`. The null separator
  prevents `foo/barbaz` colliding with `foobar/baz`.
- **Detached, bounded runner ctx**. The slot runs with
  `context.WithTimeout(context.WithoutCancel(ctx), ensureCloneTimeout)`.
  Detached so one canceled waiter cannot abort work for others;
  bounded so a stuck git subprocess cannot hold the slot forever.
- **`DoChan`, not `Do`**, so each caller still observes its own
  `ctx.Done()` via the outer `select`.

Reach for a singleflight slot whenever multiple in-process call sites
hit the same upstream resource. Prefer dedup over retry — it removes
the cause, retry just absorbs the effect.

## Tests

Test the policy decisions, not the library. For retry that means the
matcher, the `backoff.Permanent` wrap, and the budget constant. For
dedup that means the key shape and the integration paths that
exercise the real cloneBare/fetch. Skip tests that assert "the library
loops" or "DoChan delivers to every waiter" — those are upstream's
contract.
