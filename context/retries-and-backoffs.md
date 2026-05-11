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

## Single-flight: `Manager.ensureCloneShared`

[`clone.go`](../internal/gitclone/clone.go). Wraps `EnsureClone` so
concurrent callers for the same `(host, owner, name)` collapse onto one
in-flight call via `golang.org/x/sync/singleflight`. Without this, the
periodic syncer, per-PR detail syncs, and workspace setup stampeded
GitHub and triggered the 5xx burst the retry above absorbs.

Three invariants to preserve:

- **Key shape**: `host \x00 owner \x00 name`. Null separator prevents
  `foo/barbaz` colliding with `foobar/baz`.
- **Detached runner context**: the slot uses `context.WithoutCancel`. A
  canceled leader must not abort the in-flight call for followers
  attached to the slot; each caller still observes its own ctx via the
  outer `select`.
- **`DoChan`, not `Do`**: lets each caller wait on its own
  `ctx.Done()`.

Reach for a singleflight slot whenever multiple in-process call sites
hit the same upstream resource. Prefer dedup over retry — it removes
the cause, retry just absorbs the effect.

## Tests

- Retry: inject a fake op; cover success-after-recovery, permanent
  short-circuit, budget exhaustion, ctx cancellation. See
  `TestRetryTransient*`.
- Singleflight: two-phase. Leader takes the slot and blocks inside `fn`;
  signal "started"; only then spawn followers. Run with `-race` —
  singleflight bugs show up as slot-map data races. See
  `TestEnsureCloneShared*`.
