# SSE + ETag Live Sync

Replace frontend polling with server-sent events for instant UI updates, and add ETag-based conditional requests to reduce GitHub API rate limit consumption.

## Background

The current sync architecture has two latency sources:

1. **GitHub to middleman:** Syncer polls GitHub on a fixed timer (default 5m). No conditional requests, so every cycle makes full API calls even when nothing changed.
2. **middleman to browser:** Frontend polls multiple endpoints on independent timers (2s/30s sync status, 15s activity, 60s detail view). Changes detected by the syncer don't reach the browser until the next poll.

## Goals

- Browser sees sync results within seconds of sync completion, not up to 60s later
- GitHub API rate limit usage drops for unchanged data (304 responses cost 0 rate limit points)
- No new external infrastructure required (stays local-first)

## Non-goals

- Reducing the GitHub poll interval (that's a config knob, orthogonal to this work)
- WebSocket support (SSE is simpler, sufficient for server-to-client push)
- Caching response bodies (ETags are used as skip signals, not for response caching)

---

## Feature 1: Server-Sent Events

### Event Hub

New file: `internal/server/event_hub.go`

`EventHub` manages SSE subscribers with fan-out broadcasting.

```go
type Event struct {
    Type string
    Data any
}

type EventHub struct {
    mu             sync.Mutex
    subscribers    map[uint64]chan Event
    nextID         uint64
    lastSyncStatus *Event // most recent sync_status event, cached for new subscribers
    done           chan struct{} // closed by Close(); SSE handlers select on this to exit immediately
    closeOnce      sync.Once
}
```

Methods:
- `Subscribe(ctx context.Context) (<-chan Event, <-chan struct{})` -- under `mu`, creates a buffered channel (buffer size 16), pre-loads `lastSyncStatus` into the channel if non-nil, registers the channel under a fresh `id`, and releases the lock. Spawns a goroutine that calls the shared `unsubscribe(id)` helper when ctx is canceled. Returns the event channel and the hub's `done` channel. The SSE handler selects on both: `case <-done` fires immediately on shutdown, bypassing any buffered events. Note: only `sync_status` is cached; `data_changed` is not. Reconnect catch-up for missed `data_changed` events is handled client-side (the frontend fires a full refresh on every SSE `open` event — see Frontend SSE Client).
- `unsubscribeLocked(id uint64)` -- internal helper, caller must hold `mu`. Looks up the subscriber by id; if still present, deletes it from the map AND closes its channel; if already absent (because slow-consumer eviction removed it earlier), is a no-op. Called by `Broadcast` (which already holds `mu` during its iteration) and by `Close` (which holds `mu` during its cleanup sweep). The channel is closed exactly once and never double-closed.
- `unsubscribe(id uint64)` -- locking wrapper: acquires `mu`, calls `unsubscribeLocked(id)`, releases `mu`. Called by the context-cancel cleanup goroutine spawned in `Subscribe` (which does not hold `mu`). **Both the context-cancel goroutine (via `unsubscribe`) and the slow-consumer eviction in `Broadcast` (via `unsubscribeLocked`) go through the same locked delete+close logic**, so the channel is closed exactly once.
- `Close()` -- wraps the actual work in `closeOnce.Do`: closes `hub.done` (SSE handlers observe this in their two-level select and exit after at most one additional buffered event write), then under `mu` calls `unsubscribeLocked(id)` for every remaining subscriber to clean up channels. Called by `Server.Shutdown` before `httpSrv.Shutdown`. Handlers exit within at most `2 * writeDeadline` (10s) of the close — see shutdown bound analysis in SSE handler step 5. `Run`'s shutdown timeout is set to 15s to provide margin.
- `Broadcast(event Event)` -- under `mu`, updates `lastSyncStatus` if `event.Type == "sync_status"` (stored by value), then iterates subscribers with a non-blocking send to each channel. **If a non-blocking send fails (channel full), the hub does NOT silently drop the event; it calls `unsubscribeLocked(id)` for that subscriber (Broadcast already holds `mu`).** Combined with the per-write deadline on the SSE handler (see SSE Endpoint), this guarantees that a slow consumer is removed within bounded time: the channel close is observed by any handler currently parked in the `<-ch` select branch, and if the handler is parked in `Write`/`Flush` instead, the per-write deadline causes that I/O to error and the handler exits anyway. After the handler exits, the client's `EventSource` fires `onerror` and reconnects, and on reconnect the new subscription is pre-loaded with the current `lastSyncStatus`. Coalescing inside a fixed buffer would require either an unbounded backlog or per-type slots; closing the slow subscriber is simpler. **Bounded-time guarantee:** after a slow-consumer eviction closes the subscriber's channel, the handler exits within bounded time. The bound depends on where the handler is when the close happens: (a) if parked in the `<-ch` select branch, the closed channel is immediately ready, but the handler still drains up to `bufferSize` (16) buffered events before seeing `ok == false` — each drain iteration sets a 5s write deadline, so worst case is `bufferSize * writeDeadline` = 80s; (b) if parked in `Write`/`Flush`, the current per-write deadline (5s) fires, the handler returns on the I/O error, and no buffer drain occurs. The overall worst case is therefore `bufferSize * writeDeadline` (80s). This is acceptable for a loopback dashboard — the evicted subscriber cannot receive any NEW broadcasts (it was removed from the hub's map), and the buffered events being drained are stale but harmless. The client's `EventSource` reconnects after the handler exits, getting the current `lastSyncStatus` on the fresh subscription.

**Ordering guarantee:** `Subscribe` and `Broadcast` share the same `mu`, so the channel observed by a new subscriber always begins with the cached `lastSyncStatus` (if any) followed strictly by events broadcast after `Subscribe` returned. A transition broadcast that lands between a naive snapshot read and a subscribe can never sneak in ahead of the initial event, so the client cannot regress from a newer snapshot to an older buffered transition.

**Priming and startup order:** Broadcasting with no subscribers is cheap (the subscriber loop is empty) and still updates `lastSyncStatus`. Priming the hub on startup must happen **before** `syncer.Start(ctx)`, or a newer callback-driven broadcast from an already-running sync could be overwritten by a later prime reading an older `Syncer.Status()` value. The required ordering is:

1. Create the syncer (`NewSyncer`) — not yet started, `Status()` returns the zero value.
2. Construct the server (`server.NewWithConfig`) — this creates the `EventHub`, registers the `onStatusChange` callback on the syncer via `SetOnStatusChange`, and primes the hub with `Broadcast(Event{Type: "sync_status", Data: syncer.Status()})`. Because the syncer has not started yet, no callback broadcasts can race with this prime.
3. Call `syncer.Start(ctx)` — from this point forward, any status change flows through the already-wired callback, and the hub's `lastSyncStatus` stays monotonically current under the broadcast mutex.
4. Call `srv.Listen(addr)` (synchronously prepares `httpSrv`) and then `srv.Serve()` in a goroutine, so the server only begins accepting requests after the syncer is running.

This ordering also guarantees that the first HTTP request on `/api/v1/events` cannot land before the prime, because the server isn't listening yet.

To make this ordering testable and resistant to regression in `cmd/middleman/main.go`, extract the bootstrap logic into a shared helper with two distinct entry points:

```go
// cmd/middleman/app.go

type App struct {
    Server *server.Server
    Syncer *ghclient.Syncer
    DB     *db.DB
}

// Bootstrap performs steps 1–2 above (create syncer, construct server which
// primes the hub and wires the callback) without starting the syncer or
// serving HTTP. Tests use this directly to inspect hub state mid-sync.
// configPath is required because server.NewWithConfig uses it to persist
// PUT /api/v1/settings and repo add/remove writes back to the same file
// the CLI loaded.
func Bootstrap(cfg *config.Config, configPath string, ghClient ghclient.Client) (*App, error)

// Run is the only entry point `cmd/middleman/main.go` may use. It calls
// Bootstrap, starts the syncer, synchronously binds the listening
// socket via Server.Listen(addr) (returning any bind error to the
// caller directly), runs Server.Serve() in a goroutine, then selects
// on ctx.Done() (triggering a graceful shutdown) or a server error.
// Returns nil only if Serve returned http.ErrServerClosed (normal
// shutdown); any other error from Listen, Serve, or Shutdown is
// returned wrapped.
func Run(ctx context.Context, cfg *config.Config, configPath string, ghClient ghclient.Client, addr string) error {
    app, err := Bootstrap(cfg, configPath, ghClient)
    if err != nil {
        return err
    }
    // Defer order matters: Go runs deferred calls LIFO. We want
    // Syncer.Stop() (which now blocks until any in-flight RunOnce
    // returns and the goroutine exits) to run BEFORE DB.Close(),
    // otherwise a sync goroutine could still be touching the DB
    // after Run unwinds — most visibly on the bind-error and
    // server-error return paths, which would otherwise close the
    // DB while a RunOnce kicked off by app.Syncer.Start was still
    // mid-flight. Stop is registered LAST so it runs FIRST.
    defer app.DB.Close()
    // Use a dedicated cancel for the syncer so the deferred Stop
    // can both signal the goroutine and wait for it to finish,
    // independent of the parent ctx (which on the bind-error path
    // is still alive when Run returns).
    syncCtx, cancelSync := context.WithCancel(ctx)
    app.Syncer.Start(syncCtx)
    defer func() {
        cancelSync()
        app.Syncer.Stop() // blocks until the goroutine exits
    }()

    // Synchronously bind the TCP listener and prepare *http.Server
    // BEFORE spawning the serve goroutine. A bind error here is
    // returned directly and cannot be masked by a later ctx cancel,
    // and Shutdown is guaranteed to see a live httpSrv + listener
    // even if ctx fires before the serve goroutine is scheduled.
    if err := app.Server.Listen(addr); err != nil {
        return fmt.Errorf("listen: %w", err)
    }

    errCh := make(chan error, 1)
    go func() {
        err := app.Server.Serve()
        if errors.Is(err, http.ErrServerClosed) {
            errCh <- nil
            return
        }
        errCh <- err
    }()

    select {
    case <-ctx.Done():
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        if err := app.Server.Shutdown(shutdownCtx); err != nil {
            // Drain the serve goroutine to avoid leaking it, but
            // report the shutdown error which caused the failure.
            <-errCh
            return fmt.Errorf("server shutdown: %w", err)
        }
        // Wait for Serve to observe the shutdown and return.
        if err := <-errCh; err != nil {
            return fmt.Errorf("server: %w", err)
        }
        return nil
    case err := <-errCh:
        // Serve failed unexpectedly. Shut down the server to close
        // hub, drain SSE handlers, and release the listener before
        // the deferred Syncer.Stop() and DB.Close() run. Without
        // this, active handlers could still be touching the DB
        // when it closes.
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        _ = app.Server.Shutdown(shutdownCtx) // best-effort; original error takes priority
        if err != nil {
            return fmt.Errorf("server: %w", err)
        }
        return nil
    }
}
```

`Run` preserves graceful shutdown while fixing three holes the old inline `main.go` pattern had:

1. **Bind errors are surfaced synchronously.** `Server.Listen(addr)` actually calls `net.Listen("tcp", addr)` and returns any resulting error directly to the caller, before any goroutine is spawned and before any `ctx.Done()` branch can fire. This eliminates the race where a cancellation beats the serve goroutine into `ListenAndServe`, `Shutdown` marks the server closed, and the later `ListenAndServe` call returns `http.ErrServerClosed` without ever attempting the bind — silently masking "address already in use" and similar startup failures. With this split, `Serve()` calls `httpSrv.Serve(listener)` against the already-bound listener, so there is no `net.Listen` call inside the goroutine.
2. **No Shutdown-vs-Listen race on `httpSrv`:** because `Listen` is synchronous, by the time the select statement runs, both `s.httpSrv` and `s.listener` are guaranteed non-nil, so `Shutdown` can always reach a real server.
3. **Shutdown waits for Serve to exit:** after `Shutdown` returns, the select branch blocks on `errCh` until the serve goroutine has observed the shutdown and returned its final error. Any non-`ErrServerClosed` error from `Serve` is propagated instead of silently dropped. A `nil` return from `Run` truly implies `Serve` returned `http.ErrServerClosed`.

This requires the following changes to `*server.Server`:

- Add `httpSrv *http.Server` and `listener net.Listener` fields.
- Add `Listen(addr string) error`: creates the `*http.Server` (existing `WriteTimeout: 30s`, `ReadTimeout: 15s`, `IdleTimeout: 60s`, handler = `s`), then calls `net.Listen("tcp", addr)` and stores the resulting listener. Returns any error from `net.Listen` directly to the caller. Must be called exactly once before `Serve` or `Shutdown`.
- Add `Serve() error`: calls `s.httpSrv.Serve(s.listener)` and returns its result. Does **not** call `net.Listen` — the listener must already be bound by a prior `Listen` call, so `Serve` cannot encounter a bind error.
- Add `Shutdown(ctx context.Context) error`: calls `s.hub.Close()` first to close all SSE subscriber channels (causing SSE handlers to exit their select loops and return), then calls `s.httpSrv.Shutdown(ctx)` capturing its return value, then calls `s.listener.Close()` ignoring the returned error (closing an already-closed listener returns a harmless "use of closed network connection"), then returns the stored `httpSrv.Shutdown` error. The ordering matters: `http.Server.Shutdown` atomically sets its internal `inShutdown` flag before attempting to close its tracked listeners, and `http.Server.Serve` checks that flag via `trackListener` at its very first step. So regardless of whether `Serve` has started yet when `Shutdown` is called, `Shutdown` runs its normal path correctly:
  - **Post-`Serve` case:** `Serve` has already adopted `s.listener`. `httpSrv.Shutdown(ctx)` closes the tracked listener, which makes the running `Serve`'s `Accept` return, `Serve` returns `http.ErrServerClosed`, and then `Shutdown` drains active connections until `ctx` fires. Our subsequent explicit `s.listener.Close()` is a no-op on the already-closed listener.
  - **Pre-`Serve` case:** the serve goroutine has not yet entered `httpSrv.Serve(s.listener)`. `httpSrv.Shutdown(ctx)` has no tracked listeners to close but still sets `inShutdown`. Any subsequent `Serve()` call sees the flag in `trackListener` and returns `http.ErrServerClosed` immediately without touching `Accept`. `httpSrv.Shutdown` does NOT close the unadopted listener, so we must follow up with an explicit `s.listener.Close()` to release the bound socket — otherwise an early `ctx` cancellation between `Listen` and the serve goroutine being scheduled would leak the port. Closing the listener before the goroutine runs is safe because the flag already guarantees `Serve()` will return `ErrServerClosed` without racing on `Accept`.

  Previously-attempted orderings that are wrong and rejected: (a) close `s.listener` **before** calling `httpSrv.Shutdown` — in the post-Serve case, the running `Serve`'s `Accept` returns a raw listener-close error before `httpSrv` marks itself as shutting down, and `Serve` propagates that raw error instead of `http.ErrServerClosed`, making `Run`'s error branch treat a normal graceful shutdown as a server error. (b) Skip the explicit `s.listener.Close()` in the post-Serve case with an "already adopted" flag — correct but more state to track than the idempotent ordering above.

  For the test path where `*Server` is wired purely as an `http.Handler` via `httptest.NewServer` (no `Listen` call), both `httpSrv` and `listener` are nil and `Shutdown` is a no-op returning `nil`.
- The old `ListenAndServe(addr string) error` method is **removed** (not renamed or kept as a wrapper) to make the AST test enforceable: there must be no caller of the combined form anywhere in `cmd/middleman`.

`cmd/middleman/main.go`'s `run` function is reduced to loading config, constructing the GitHub client, setting up signal-cancellation, and calling `Run`. It does not reference `app.Syncer` or `app.Server` directly; the `Run` helper sequences them.

To prevent a future edit from reintroducing the bypass (calling `Bootstrap` and then wiring `syncer.Start` / `Server.Serve` inline in `main.go`, in a new helper file, or even in a new function inside `app.go` itself), add a type-aware regression test at `cmd/middleman/main_ast_test.go` that inspects the **entire `cmd/middleman` package including `app.go`**, but scoped by enclosing function:

```go
// TestOnlyAppRunStartsServerAndSyncer loads cmd/middleman with
// go/packages (with type info) and walks every *ast.SelectorExpr in
// every non-test source file — NOT just selectors that are immediate
// call-site callees. Walking every selector catches method-value
// aliases like `serve := app.Server.Serve` where the later `serve()`
// call is a plain *ast.Ident and the forbidden reference only shows
// up at the earlier selector. For each selector, it resolves via
// pkg.TypesInfo.Selections[sel] (NOT Uses: method references are
// recorded in Selections; Uses only holds plain identifiers) and
// accepts BOTH `types.MethodVal` (normal `x.M`) AND `types.MethodExpr`
// (the `(*server.Server).Serve` form) so that neither bypasses the
// guardrail.
//
// The forbidden set is a map[*types.Func]bool built by looking up
// the concrete lifecycle methods through types.NewMethodSet on
// *Syncer and *Server. This gives stable *types.Func identities for
// the canonical targets, so the second pass compares against them
// by pointer equality — the strongest local equivalence check.
// Interface indirection (e.g. `var s lifecycleIface = app.Server;
// s.Serve()`) is a documented known limitation; see "Known
// limitations" below.
//
// Enclosing-FuncDecl scope is tracked with an ast.Walk visitor
// struct carrying `enclosing` BY VALUE (not a mutable variable with
// ast.Inspect), so package-scope selectors after a FuncDecl cannot
// inherit the previous enclosing value and bypass the exemption.
// The test fails if any selector resolves to a method in the
// forbidden set UNLESS the enclosing FuncDecl is pointer-identical
// to the specific package-level `Run` function node found during a
// prior pass over app.go.
//
// Forbidden set (receiver type + method name):
//   - (*github.com/wesm/middleman/internal/github.Syncer).Start
//   - (*github.com/wesm/middleman/internal/server.Server).Listen
//   - (*github.com/wesm/middleman/internal/server.Server).Serve
//   - (*github.com/wesm/middleman/internal/server.Server).Shutdown
//
// No file in cmd/middleman may reference these methods, with ONE
// exception: the body of the single package-level `Run` function in
// app.go (FuncDecl.Recv == nil, name == "Run", declared at file scope
// in app.go, matching the expected signature). Bootstrap, App methods,
// any future helper inside app.go itself, and any method named `Run`
// on some other receiver type are all forbidden from touching this
// set — everything must go through the specific package-level Run.
// main.go is therefore forced to call Run.
func TestOnlyAppRunStartsServerAndSyncer(t *testing.T) {
    cfg := &packages.Config{
        Mode: packages.NeedName | packages.NeedFiles |
              packages.NeedSyntax | packages.NeedTypes |
              packages.NeedTypesInfo,
        Dir:  ".",
    }
    pkgs, err := packages.Load(cfg, ".")
    // First pass: locate the unique package-level Run FuncDecl.
    //   var runDecl *ast.FuncDecl
    //   for each non-test syntax file f in pkgs[0]:
    //     if filepath.Base(pkg.Fset.File(f.Pos()).Name()) != "app.go" { continue }
    //     for each top-level decl in f.Decls:
    //       fd, ok := decl.(*ast.FuncDecl); if !ok continue
    //       if fd.Recv != nil { continue }           // must not be a method
    //       if fd.Name.Name != "Run" { continue }
    //       if runDecl != nil { t.Fatalf("multiple Run decls") }
    //       runDecl = fd
    //   if runDecl == nil { t.Fatalf("no package-level Run in app.go") }
    //   // Validate signature: (ctx context.Context, cfg *config.Config,
    //   //   configPath string, ghClient ghclient.Client, addr string) error
    //   // using types info from runDecl.Name's *types.Func so a rename
    //   // or signature drift cannot silently disable the guardrail.
    //
    // Build the forbidden set as a map[*types.Func]bool keyed by
    // pointer identity. Exact string matching on
    // selection.Recv().String() is more fragile (pointer vs value
    // receiver drift, renames in the imported package), so we look
    // up the concrete lifecycle methods via types.NewMethodSet and
    // store the returned *types.Func pointers.
    //
    //   syncerPkg := pkg.Imports["github.com/wesm/middleman/internal/github"]
    //   serverPkg := pkg.Imports["github.com/wesm/middleman/internal/server"]
    //   syncerPtr := types.NewPointer(syncerPkg.Types.Scope().Lookup("Syncer").Type())
    //   serverPtr := types.NewPointer(serverPkg.Types.Scope().Lookup("Server").Type())
    //   forbidden := map[*types.Func]bool{}
    //   for _, tup := range []struct{ t types.Type; name string }{
    //       {syncerPtr, "Start"},
    //       {serverPtr, "Listen"},
    //       {serverPtr, "Serve"},
    //       {serverPtr, "Shutdown"},
    //   } {
    //       mset := types.NewMethodSet(tup.t)
    //       sel := mset.Lookup(nil, tup.name) // same-package lookup OK
    //       if sel == nil { t.Fatalf("method not found: %s", tup.name) }
    //       forbidden[sel.Obj().(*types.Func)] = true
    //   }
    //
    // Second pass: walk every non-test file with a scope-aware
    // visitor. We do NOT use ast.Inspect with a mutable enclosing
    // variable: ast.Inspect calls the callback with `n == nil` on
    // every subtree exit (not just FuncDecl exits), which makes
    // proper push/pop bookkeeping awkward. Instead, use ast.Walk
    // with a visitor struct that carries `enclosing` by value, and
    // return a NEW visitor scoped to each FuncDecl subtree:
    //
    //   type checker struct {
    //       enclosing *ast.FuncDecl  // nil at package scope
    //       runDecl   *ast.FuncDecl  // validated Run node
    //       info      *types.Info    // pkg.TypesInfo
    //       forbidden map[*types.Func]bool
    //       t         *testing.T
    //   }
    //   func (c *checker) Visit(n ast.Node) ast.Visitor {
    //       if n == nil { return nil }
    //       if fd, ok := n.(*ast.FuncDecl); ok {
    //           // Descend the FuncDecl subtree with a new checker
    //           // whose `enclosing` is this FuncDecl. When ast.Walk
    //           // pops back out, the parent's checker (unchanged)
    //           // resumes, so `enclosing` is naturally unset for any
    //           // siblings at package scope.
    //           child := *c
    //           child.enclosing = fd
    //           return &child
    //       }
    //       sel, ok := n.(*ast.SelectorExpr); if !ok { return c }
    //       selection := c.info.Selections[sel]
    //       if selection == nil { return c }
    //       if selection.Kind() != types.MethodVal &&
    //          selection.Kind() != types.MethodExpr {
    //           return c
    //       }
    //       fn, ok := selection.Obj().(*types.Func); if !ok { return c }
    //       if !c.forbidden[fn] { return c }
    //       // Pointer-identity check against the validated Run node.
    //       if c.enclosing != c.runDecl {
    //           c.t.Fatalf("forbidden %s reference outside Run: %s",
    //               fn.Name(), c.info.Fset.Position(sel.Pos()))
    //       }
    //       return c
    //   }
    //   for _, f := range pkg.Syntax {
    //       if isTestFile(f) { continue }
    //       ast.Walk(&checker{runDecl: runDecl, info: pkg.TypesInfo,
    //           forbidden: forbidden, t: t}, f)
    //   }
}
```

Why the `Run`-only exemption is pinned to a specific FuncDecl (not a file-level `app.go` exemption, and not a name-only check): exempting the entire file would let a future edit add a second helper function next to `Run` inside `app.go` that does the inline wiring, and have `main.go` call that helper instead. Scoping the exemption by bare name "Run" would let a future edit add a method like `func (h helper) Run(...)` anywhere in the package that inlines `Syncer.Start` / `Server.Listen` / `Server.Serve` / `Server.Shutdown` — the enclosing FuncDecl's name would still be "Run", defeating the guardrail. Pinning the exemption to the **specific AST node** identified by a first pass (file = `app.go`, `Recv == nil`, `Name == "Run"`, validated signature) forces all startup wiring through exactly that one package-level function. Signature validation guards against a rename or drift that would leave a same-named but wrong-shaped `Run` silently disabling the check.

Why the second pass walks every `*ast.SelectorExpr` rather than the callees of `*ast.CallExpr` only: a method-value alias like `serve := app.Server.Serve; serve()` separates the forbidden method reference (the `app.Server.Serve` selector) from the call site (a plain identifier `serve` that carries no `TypesInfo.Selections` entry). A call-only walk would see the identifier and skip it because it is not a selector; the forbidden reference would hide at the earlier selector expression. Walking every `SelectorExpr` node catches the reference regardless of whether it is the callee of an immediate call, taken as a method value, or used as the operand of a method expression — all three resolve via `TypesInfo.Selections` and all three are checked.

Why the second pass uses `ast.Walk` with a visitor struct rather than `ast.Inspect` with a mutable `enclosing` variable: `ast.Inspect` calls the callback with `n == nil` on every subtree exit (not just `FuncDecl` exits), which makes reliable push/pop of `enclosing` awkward. Updating `enclosing` on `FuncDecl` entry without resetting it on exit lets later package-scope selectors — e.g. a `var serve = (*server.Server).Serve` declared at file scope after `Run` — inherit the previous `enclosing` value and be misclassified as if they sat inside `Run`. Using `ast.Walk` with a visitor struct that carries `enclosing` **by value** and returns a NEW child visitor scoped to each `FuncDecl` subtree gives correct push/pop automatically: when `ast.Walk` pops back out of the subtree, the parent visitor (unchanged) resumes with its original `enclosing` (nil at package scope), so sibling nodes after the `FuncDecl` see `enclosing == nil` and fail the exemption as intended.

Why the forbidden set is keyed by `*types.Func` pointer rather than by `selection.Recv().String()`: exact string equality on the receiver is fragile. Pointer vs value receiver drift, a rename in the imported package, or a locally-defined wrapper type that embeds `*Server` will all silently disable a string-based check. Looking up the concrete methods via `types.NewMethodSet(*Syncer)` and `types.NewMethodSet(*Server)` gives a stable `*types.Func` identity for the canonical targets, and the second pass compares `selection.Obj().(*types.Func)` against the set by pointer equality — the strongest local equivalence check that does not require call-graph analysis.

#### Known limitations

The AST guardrail catches every **direct concrete-reference** path to a forbidden lifecycle method, but it does **not** follow interface indirection. A future edit that introduces a locally-declared interface enumerating the lifecycle method names (`type lifecycleIface interface { Serve() error; Shutdown(context.Context) error }`), assigns `app.Server` to a variable of that interface type, and calls `s.Serve()` on the variable will resolve via `TypesInfo.Selections` to the **interface method's** `*types.Func` (the one defined on `lifecycleIface.Serve`), not to the concrete `(*server.Server).Serve` `*types.Func` that the forbidden set was built from. Both are `*types.Func` values, but they have different pointer identities, so the `forbidden[fn]` check will not match. Detecting that case requires either (a) an overbroad expansion that walks every interface in the package and adds any method with a matching name and compatible signature — which would also flag unrelated future interfaces that happen to share a method name — or (b) interprocedural analysis that tracks which interface values are dynamically backed by `*Server` / `*Syncer`. Both are out of scope for an AST unit test. The mitigation is procedural, not test-based: the forbidden methods are not part of any existing interface in the `cmd/middleman` package, so introducing one would be a deliberate, reviewable action that should be caught at code review. `app_test.go` is **not** a backstop for this gap — it asserts the happy-path ordering of `Bootstrap` against a mock client, but `Syncer.Start` is fire-and-forget, so a hidden extra `Start`/`Listen`/`Serve` call routed through an interface alias inside `Bootstrap` could still satisfy the observed ordering and pass.

Combined with the Bootstrap regression test, this gives two complementary guardrails covering different concerns: `app_test.go` verifies the helper correctly orders prime and start on the happy path, and `main_ast_test.go` verifies every direct concrete reference to the forbidden method set in the entire `cmd/middleman` package is inside the body of `Run`. Neither covers interface-aliased lifecycle calls; that case is left to code review.

### Event Types

| Event | Payload | Trigger |
|-------|---------|---------|
| `sync_status` | `SyncStatus` JSON (same shape as `GET /sync/status`) | Every status change in Syncer (started, progress, complete) |
| `data_changed` | `{}` (empty object) | After `RunOnce` completes |

Note: `data_changed` fires after every sync completion, even when ETags caused all requests to return 304 and nothing in the DB actually changed. This is intentional -- the cost of an unnecessary local re-fetch (Go server to SQLite to browser) is negligible, and tracking dirty state to suppress the event adds complexity for no meaningful benefit.

### SSE Endpoint

`GET /api/v1/events` -- registered as a plain `mux.HandleFunc` on the inner mux (not Huma, since SSE doesn't fit request/response OpenAPI modeling). The existing base-path `StripPrefix` setup handles path routing automatically since this is on the inner mux like all other handlers.

Handler:
1. Set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
2. Obtain an `*http.ResponseController` via `http.NewResponseController(w)`. This exposes a `Flush() error` method (unlike the bare `http.Flusher` interface, whose `Flush()` returns no error), so the handler can detect flush failures and return promptly. **Immediately clear the connection-level write deadline** with `rc.SetWriteDeadline(time.Time{})` — this overrides the server's `WriteTimeout: 30s` for this response only, allowing the SSE connection to remain open indefinitely. (The server keeps its normal `WriteTimeout` for all non-SSE endpoints; see WriteTimeout section.) Call `rc.Flush()` to push headers; if it returns an error, return immediately (flushing unsupported or the underlying writer is broken).
3. Subscribe to hub with `r.Context()`, receiving both the event channel (`ch`) and the hub's `done` channel. Because the hub pre-loads the cached `lastSyncStatus` into the new channel under the broadcast lock (see Event Hub), the very first receive from the channel in step 5's select loop is a `sync_status` event carrying the current state. The handler doesn't need to fetch `Syncer.Status()` separately, and there is no window between snapshot read and subscribe.
4. Start a 30s keepalive ticker
5. Select loop with a **two-level select** pattern to bound post-shutdown work. Each iteration begins with a non-blocking `done` check: `select { case <-done: return; default: }`. Only if `done` is not yet closed does the handler enter the full four-branch select: `case <-done`, `case event, ok := <-ch`, `case <-ticker.C`, `case <-r.Context().Done()`. The `done` case is duplicated in the full select for the case where `done` closes while the handler is blocked waiting for an event. **Shutdown bound:** if `done` closes while the handler is in the full select and `ch` also has a buffered event ready, Go may pick `ch` once (non-deterministic select). The handler writes that one event (bounded by one `writeDeadline`, 5s), returns to the loop top, hits the non-blocking `done` check, and exits. Worst case: one extra event write after `done` closes, bounded by one `writeDeadline` (5s). The handler therefore exits within at most `2 * writeDeadline` (10s) of `hub.Close()` — one deadline if mid-write when `done` closes, one more if a buffered event is picked. This fits within a reasonable shutdown timeout (the current 5s in `Run` should be increased to 15s to provide margin). On channel receive, use the **two-value form** and return immediately when `ok` is false. The hub closes a subscriber's channel both when its context is canceled (the normal cleanup path) and when a non-blocking broadcast send finds the channel full (the slow-consumer path described in Event Hub). Treating both cases identically — exit the handler — ensures the loop never spins on a closed channel emitting zero-value `Event{}` frames as bogus empty SSE writes. When `ok` is true, set a per-write deadline before each event write via `rc.SetWriteDeadline(time.Now().Add(5 * time.Second))`, then marshal the event to SSE wire format (`event: <type>\ndata: <json>\n\n`), write, call `rc.Flush()`. If `Write` returns an error or `rc.Flush()` returns an error, return immediately (client disconnected, flush failed, or write deadline exceeded). **After a successful write+flush, clear the deadline** with `rc.SetWriteDeadline(time.Time{})` before returning to the select loop. This is required because an expired deadline is sticky — `SetWriteDeadline` with a past time causes subsequent writes to fail immediately. Without clearing, the 5s deadline set before a write would expire during the idle gap before the next event (up to 30s for keepalive), and the next write would fail on entry. The per-write deadline is required because the handler cleared the server's connection-level `WriteTimeout` at step 2 — without a per-write deadline, a stalled TCP receive window on a slow consumer could pin the handler in `Write` indefinitely, the handler would never observe a hub-side channel close, and the slow-consumer eviction described in Event Hub would not actually disconnect the client. On ticker, the same pattern: set 5s deadline, write SSE comment (`: keepalive\n\n`), call `rc.Flush()`, return on write or flush failure, clear deadline on success. On context cancel, return. The very first iteration of this loop will drain the cached `sync_status` from the channel and flush it out, so the client receives the current state immediately after the headers — no separate "write then flush" step outside the loop is required.
6. Keepalive ticker is stopped via defer

SSE is exempt from CSRF checks (GET request -- the existing CSRF check in `ServeHTTP` only applies to non-GET methods).

### Syncer Integration

The Syncer gets a callback field and a waitable shutdown:

```go
type Syncer struct {
    // ... existing fields ...
    onStatusChange func(*SyncStatus)
    done           chan struct{} // closed by the goroutine on exit
}
```

Set during server construction via a setter. No import cycle -- the github package doesn't import server.

```go
syncer.SetOnStatusChange(func(status *SyncStatus) {
    hub.Broadcast(Event{Type: "sync_status", Data: status})
    if !status.Running {
        hub.Broadcast(Event{Type: "data_changed", Data: struct{}{}})
    }
})
```

The callback fires at each `s.status.Store(...)` call site in `RunOnce`:
1. `SyncStatus{Running: true}` -- sync started (broadcasts `sync_status`)
2. `SyncStatus{Running: true, CurrentRepo: ..., Progress: ...}` -- per-repo progress (broadcasts `sync_status`)
3. `SyncStatus{Running: false, LastRunAt: ..., LastError: ...}` -- sync complete (broadcasts `sync_status` + `data_changed`)

#### Waitable shutdown

`Syncer.Stop()` is changed from fire-and-forget (close `stopCh` and return immediately) to **waitable**: it closes `stopCh`, then waits for the background ticker goroutine to exit, AND waits for every in-flight `RunOnce` invocation — including ones launched directly by API handlers, not only the one driven by `Start`'s ticker — to return.

The change has two parts:

1. **Background goroutine tracking.** `NewSyncer` creates `s.done = make(chan struct{})`. The goroutine spawned by `Start` does `defer close(s.done)` as its very first deferred call, so `done` is closed only after the ticker loop has returned.

2. **Manual RunOnce tracking via WaitGroup.** The current codebase launches manual sync runs from two API handlers (`internal/server/huma_routes.go:775` and `internal/server/settings_handlers.go:147`) with `go s.syncer.RunOnce(context.WithoutCancel(r.Context()))`. These goroutines are independent of `Start`'s ticker and would otherwise still be writing to SQLite after the ticker goroutine exits and `DB.Close()` runs — a real shutdown race the spec must close. The fix is to wrap **every** `RunOnce` call (the ticker-driven one inside the `Start` goroutine, and both API-handler call sites) in a `sync.WaitGroup` owned by the Syncer.

   `NewSyncer` allocates the lifetime state up front so it is valid before `Start` is ever called. The lifecycle is serialized by a single `sync.Mutex` (with a narrowly-scoped `sync.Once` wrapping `Stop`'s one-shot teardown work) rather than a constellation of `atomic.Bool` flags. The mutex makes the happens-before relationship between `Start`, `Stop`, and `TriggerRun` explicit, eliminating WaitGroup-misuse races where a `wg.Add(1)` could land after a concurrent `Stop`'s `wg.Wait()` had already returned with counter zero. The `sync.Once` is layered on top to make `Stop` shutdown-complete-idempotent across overlapping callers — see "Why the mutex (and where `sync.Once` still fits)" below for the full rationale:

   ```go
   type Syncer struct {
       // ... existing fields ...

       // lifecycleMu serializes the bookkeeping of Start, Stop, and
       // TriggerRun (registering wg slots, flipping started/stopped).
       // It is ONLY held during the short bookkeeping critical
       // sections — never across RunOnce calls or channel waits.
       lifecycleMu sync.Mutex
       started     bool // guarded by lifecycleMu
       stopped     bool // guarded by lifecycleMu

       // stopOnce wraps the one-shot state transition portion of
       // Stop (close stopCh, lifetimeCancel, possibly close done).
       // ALL Stop callers — first or otherwise — block on the
       // shared <-s.done and s.wg.Wait() outside the once, so every
       // caller observes shutdown completion, not just the first.
       stopOnce sync.Once

       done           chan struct{}
       stopCh         chan struct{}
       wg             sync.WaitGroup
       lifetimeCtx    context.Context
       lifetimeCancel context.CancelFunc
   }

   func NewSyncer(/*...*/) *Syncer {
       s := &Syncer{ /*...*/
           done:   make(chan struct{}),
           stopCh: make(chan struct{}),
       }
       s.lifetimeCtx, s.lifetimeCancel = context.WithCancel(context.Background())
       return s
   }
   ```

   `lifetimeCtx` is owned by the Syncer for its entire lifetime — it is created in `NewSyncer`, not in `Start`, so `TriggerRun` is valid before `Start` runs (and before `Stop`). `Start` simply links the caller's parent ctx into the cancellation chain by spawning a goroutine that calls `s.lifetimeCancel()` when the parent ctx is canceled, in addition to the existing stopCh / parent-ctx select inside the ticker loop.

   `TriggerRun` is the public wrapper that handler code calls instead of `go s.syncer.RunOnce(...)`. It uses `s.lifetimeCtx` (not the caller's request ctx) so the run survives request completion but is still canceled at syncer shutdown. **Critically, `TriggerRun` registers its `wg` slot under the lifecycle lock**, so a concurrent `Stop` cannot interleave between the `wg.Add(1)` and `wg.Wait()` and miss the new run:

   ```go
   func (s *Syncer) TriggerRun() {
       s.lifecycleMu.Lock()
       if s.stopped {
           // Shutdown already in progress; refuse new work so it
           // cannot escape Stop's wg.Wait().
           s.lifecycleMu.Unlock()
           return
       }
       s.wg.Add(1) // claim the slot BEFORE releasing the mutex
       s.lifecycleMu.Unlock()
       go func() {
           defer s.wg.Done()
           s.RunOnce(s.lifetimeCtx)
       }()
   }
   ```

   `Start` performs the same trick: it claims wg slots for the goroutines it is about to launch **inside** the lifecycle critical section, so a `Stop` that takes the mutex after `Start` releases it is guaranteed to see `wg` counter ≥ 2 when it eventually calls `wg.Wait()`. Adding `s.wg.Add(1) / s.wg.Done()` directly around each per-cycle `RunOnce` inside the existing goroutine — instead of routing through a helper method — keeps the AST guardrail simple (only the `Start` method's body needs to be on the exemption list, no extra helper to enumerate):

   ```go
   func (s *Syncer) Start(ctx context.Context) {
       s.lifecycleMu.Lock()
       if s.started || s.stopped {
           // Double-Start is a no-op; Start-after-Stop is rejected
           // because the syncer is terminal once Stop has run.
           s.lifecycleMu.Unlock()
           return
       }
       s.started = true
       // Reserve wg slots for the two goroutines we are about to
       // launch BEFORE releasing the mutex. Any concurrent Stop will
       // block on the mutex; once it acquires the mutex it sees
       // started=true, and when it later calls wg.Wait() the counter
       // is already at least 2. This closes the race where Stop
       // could otherwise reach wg.Wait() with counter=0 and return
       // before the ticker goroutine had a chance to register its
       // first wg.Add(1).
       s.wg.Add(2)
       s.lifecycleMu.Unlock()

       // Link the parent ctx into the lifetime cancellation chain so
       // a parent cancellation propagates to in-flight TriggerRun calls.
       go func() {
           defer s.wg.Done()
           select {
           case <-ctx.Done():
               s.lifetimeCancel()
           case <-s.done:
               // Stop already fired; nothing to do.
           }
       }()
       go func() {
           defer s.wg.Done()
           // Only this goroutine ever closes done in the started path.
           // The Stop-without-Start path closes done itself (see Stop).
           defer close(s.done)
           // Ticker-driven runs use lifetimeCtx (not the parent ctx)
           // so Stop's lifetimeCancel() can cancel in-flight RunOnce
           // calls without waiting for the parent ctx to be canceled.
           s.wg.Add(1)
           s.RunOnce(s.lifetimeCtx)
           s.wg.Done()
           ticker := time.NewTicker(s.interval)
           defer ticker.Stop()
           for {
               select {
               case <-ticker.C:
                   s.wg.Add(1)
                   s.RunOnce(s.lifetimeCtx)
                   s.wg.Done()
               case <-s.stopCh:
                   return
               case <-ctx.Done():
                   return
               }
           }
       }()
   }
   ```

   `Stop()` is then:

   ```go
   func (s *Syncer) Stop() {
       // The state transition (flip stopped, close stopCh, cancel
       // lifetimeCtx, possibly close done) runs exactly once via
       // stopOnce. Concurrent Stop callers serialize inside
       // stopOnce.Do — only the first runs the func body, the rest
       // block until that body returns and then fall through.
       s.stopOnce.Do(func() {
           s.lifecycleMu.Lock()
           s.stopped = true
           wasStarted := s.started
           close(s.stopCh)
           s.lifetimeCancel() // unblock any in-flight TriggerRun
           s.lifecycleMu.Unlock()

           if !wasStarted {
               // No ticker goroutine exists, so nobody else will
               // ever close done. Close it here so the shared
               // <-s.done wait below (and any future <-s.done in a
               // test) does not deadlock.
               close(s.done)
           }
       })

       // ALL Stop callers — first or otherwise — reach this point
       // and block on the shared completion signals. The first
       // caller has just released stopOnce; later callers were
       // parked inside stopOnce.Do and resume here. Both then wait
       // for the ticker goroutine to close done (in the started
       // path) and for every wg slot to drain. This is the
       // shutdown-complete idempotency guarantee: every Stop call
       // returns only after sync work has fully drained, not just
       // after the state transition has been scheduled.
       <-s.done
       s.wg.Wait() // wait for any in-flight RunOnce, ticker- or handler-driven
   }
   ```

   The mutex is held only during short bookkeeping windows; the long waits (`<-s.done`, `s.wg.Wait()`) happen **outside** both the critical section and the stopOnce, so a concurrent `TriggerRun` that runs before `Stop` is invoked can still be observed by `Stop`'s `wg.Wait()`. The mutex makes the start/stop/register transitions atomic with respect to each other; the `sync.Once` ensures the one-shot teardown work (closing channels, canceling the lifetime context) happens exactly once even when `Stop` is called concurrently from multiple goroutines; and the post-once waits ensure every caller observes shutdown completion, not just the one that won the race to run the transition.

   The two `internal/server/*.go` call sites change from `go s.syncer.RunOnce(...)` to `s.syncer.TriggerRun()`. After the change, no caller in the codebase launches a bare `go RunOnce`; the wrapper is the only way to fire-and-forget a run.

   **Why the mutex (and where `sync.Once` still fits):** earlier drafts of this design used `sync.Once` + `atomic.Bool` flags **alone** for the lifecycle bookkeeping, and the WaitGroup race rules subtly bit them. The pathology: `Start` spawns the ticker goroutine but the goroutine has not yet executed `wg.Add(1)` when a concurrent `Stop` reaches `wg.Wait()`. With counter zero, `Wait` returns immediately and `Stop` declares shutdown complete; meanwhile the delayed ticker goroutine still runs `RunOnce` against a possibly-closed DB. Symmetrically, `TriggerRun`'s unconditional `wg.Add(1)` could land after `Stop`'s `wg.Wait()` had returned, again letting a sync escape shutdown. The lifecycle mutex closes both holes by ensuring the wg-slot reservation for Start's goroutines and TriggerRun's goroutine happens-before any `Stop` that could observe `wasStarted=true` or `stopped=true`. `Stop` always sees `wg` counter at the value the mutex hand-off established, so `Wait` cannot return early.

   The final design retains `sync.Once` for a different, narrower job: wrapping the **one-shot state transition** at the top of `Stop` (flipping `stopped`, closing `stopCh`, calling `lifetimeCancel`, and conditionally closing `done`). The mutex alone is not sufficient for this because it would let the second `Stop` caller observe `s.stopped == true` at the entry check and return immediately while the first caller was still draining `<-s.done` and `wg.Wait()` outside the lock — overlapping `Stop` calls would no longer be waitable for shutdown completion. With `sync.Once` wrapping the transition and the long waits placed *outside* the once, every `Stop` caller — first or otherwise — blocks on the shared `<-s.done` and `s.wg.Wait()` signals before returning, so all callers observe shutdown completion. The mutex still serializes the `Start` / `Stop` / `TriggerRun` bookkeeping with respect to each other; the once layered on top serializes the teardown work specifically against repeat `Stop` invocations.

   **Interleaving cases:**
   - `Start` then `Stop`: Start takes the mutex, sets `started=true`, `wg.Add(2)`, releases the mutex, spawns goroutines. Stop's stopOnce runs the transition body: takes the mutex, sets `stopped=true`, sees `wasStarted=true`, closes `stopCh`, calls `lifetimeCancel`, releases. Stop then (outside the once) waits on `<-s.done` (closed by the ticker goroutine on exit) and `wg.Wait()` (drains all per-cycle and TriggerRun adds). Correct.
   - `Stop` then `Start`: Stop's stopOnce runs the transition body: takes the mutex, sets `stopped=true`, `wasStarted=false`, closes `stopCh`, calls `lifetimeCancel`, releases, then closes `s.done` itself (because no goroutine will). Stop then waits on `<-s.done` (already closed, returns immediately) and `wg.Wait()` (counter is 0, returns immediately). Start later takes the mutex, sees `stopped=true`, releases without launching goroutines. The syncer is terminal. Correct.
   - `Start` racing `Stop`: exactly one wins the mutex first, reducing to one of the above cases. There is no third interleaving.
   - `TriggerRun` racing `Stop`: if `TriggerRun` wins the mutex first, it does `wg.Add(1)` and spawns the goroutine; Stop's stopOnce body later takes the mutex, sets `stopped=true`, releases, and Stop's `wg.Wait()` (outside the once) blocks until the goroutine's `defer wg.Done()` runs. The goroutine sees `lifetimeCtx` already canceled (by Stop's `lifetimeCancel`) and returns quickly via `RunOnce`'s ctx checks. If `Stop` wins the mutex first, `TriggerRun` later sees `stopped=true` and returns without registering any work.
   - Double `Stop`: the second `Stop` caller enters `stopOnce.Do` and parks until the first caller's transition body returns; then it skips the body (once already fired) and falls through to the same shared `<-s.done` and `s.wg.Wait()` waits as the first caller. Both `Stop` calls return only after shutdown is fully drained — the second call cannot return early while the first is still waiting. Correct.
   - Double `Start`: the second call takes the mutex, sees `started=true`, releases without spawning. Correct.

`Stop` is therefore idempotent in the strong sense — every caller observes shutdown completion before returning, not just the one that won the race to run the transition. The combination of `sync.Once` (one-shot teardown) and the post-once `<-s.done` / `s.wg.Wait()` waits (shared by all callers) gives this guarantee. `<-s.done` and `s.wg.Wait()` are both safe to call multiple times in their own right.

Effective shutdown sequence in `Run`: the deferred `cancelSync()` (which cancels the `syncCtx` passed to `Start`) propagates through the linker goroutine to `lifetimeCancel`, which unblocks any in-flight HTTP call inside both ticker- and handler-driven `RunOnce` invocations. The deferred `Stop()` then closes `stopCh`, waits for the ticker goroutine on `done`, and waits for `wg` to drain. Only then does `defer DB.Close()` run.

### WriteTimeout

The server keeps its existing `WriteTimeout: 30s` on the `http.Server`, which protects all non-SSE endpoints against stalled writes. The SSE handler is the only endpoint that needs an unbounded response lifetime, so it overrides the deadline locally: immediately after obtaining the `*http.ResponseController`, it calls `rc.SetWriteDeadline(time.Time{})` to clear the connection-level write timeout for that single response. Subsequent per-write deadlines (5s before each event/keepalive write) provide the equivalent slow-consumer protection that the server-wide timeout provides for normal endpoints.

This per-handler override avoids the regression of setting `WriteTimeout: 0` server-wide, which would remove write protection for every non-SSE route. `ReadTimeout` (15s) still protects against slow request reads. `IdleTimeout` (60s) still cleans up idle HTTP/1.1 keep-alive connections (distinct from SSE connections, which are active).

---

## Feature 2: ETag Conditional Requests

### Target Endpoints

Two list endpoints benefit from ETags:

| Endpoint | Calls per cycle | ETag benefit |
|----------|----------------|-------------|
| `ListOpenPullRequests` | 1 per repo | Skip entire PR list processing when no PRs changed |
| `ListOpenIssues` | 1 per repo | Skip entire issue list processing when no issues changed |

`GetCombinedStatus` and `ListCheckRunsForRef` are excluded from ETag support. While they run unconditionally every cycle, their normalization functions (`NormalizeCIStatus`, `NormalizeCIChecks`) merge data from both sources into a single `ci_checks_json` column. Partial updates (one endpoint returns 304, the other returns 200) would require parsing and merging the existing JSON blob, adding complexity that outweighs the rate limit savings from these single-request endpoints.

All other read endpoints are already conditionally called (guarded by `UpdatedAt` comparison or in-memory caches).

### ETag Transport

New file: `internal/github/etag_transport.go`

```go
type etagTransport struct {
    base  http.RoundTripper
    cache sync.Map // URL string -> etagEntry
}

type etagEntry struct {
    etag     string
    cachedAt time.Time
}
```

**ETag TTL:** Cached ETags expire after `etagTTL` (constant, 30 minutes). Expired entries are treated as uncached — no `If-None-Match` sent, forcing an unconditional fetch. This bounds staleness for edge cases where a 304 hides changes that only affect later pages (see "Pagination and ETags" section). At the default 5-minute sync interval, this means ~6 ETag-accelerated cycles per unconditional refresh — still a large rate limit improvement over no caching.

```go
const etagTTL = 30 * time.Minute
```

`RoundTrip(req)`:
1. **Gate check:** if `req.Method` is not `GET`, or the request URL path does not match the ETag-eligible endpoint allowlist, pass through to the base transport and return. The allowlist matches the two target endpoints by path suffix pattern: `/repos/{owner}/{name}/pulls` and `/repos/{owner}/{name}/issues`. This prevents ETags from leaking onto excluded read endpoints (`GetCombinedStatus`, `ListCheckRunsForRef`) which cannot handle 304s correctly, and onto mutating requests (`POST`, `PATCH`, `PUT`, `DELETE`) that may share URL paths with read endpoints.
2. Check if this is a paginated later-page request: if the URL has a `page` query parameter with value > 1, skip ETag handling entirely — pass through to the base transport and return.
3. Look up `req.URL.String()` in cache. If found AND not expired (`time.Since(entry.cachedAt) < etagTTL`), clone the request and add `If-None-Match: <etag>` header (must clone to avoid mutating the original). If expired, delete the entry and proceed as uncached.
4. Call `base.RoundTrip(req)` with the (possibly modified) request.
5. On 200: if the response has an `ETag` header AND does NOT have a `Link` header containing `rel="next"` (indicating this is a single-page result), store the ETag in cache with `cachedAt: time.Now()`. If the response IS multi-page (has `Link: next`), delete any previously-cached ETag for this URL (`cache.Delete(url)`) — this evicts stale entries from when the endpoint was single-page and ensures multi-page endpoints always fetch fresh on the next cycle.
6. On 304: return response as-is (empty body, 304 status). Do NOT update `cachedAt` — the entry must age out so the TTL can eventually force an unconditional fetch.
7. On other status: return response as-is.

**Four safeguards:**
- Step 1 (gate check) restricts ETag handling to GET requests on the two explicitly targeted endpoints. All other endpoints — `GetCombinedStatus`, `ListCheckRunsForRef`, mutating requests — pass through the transport unmodified. This is the primary scope control.
- Step 2 (page parameter check) prevents later pages from caching or using stale ETags. go-github's first page request has no `page` parameter, while subsequent pages have `page=2`, `page=3`, etc.
- Step 5 (Link: next eviction) prevents the first page of multi-page results from being cached, AND actively evicts any previously-cached ETag for that URL. This handles the single-page → multi-page transition: if a repo was single-page (ETag cached), then grows past 100 items, the first 200 response with `Link: next` evicts the stale entry.
- Step 3 (TTL expiry) bounds staleness for the reverse edge case: a cached single-page ETag that 304s indefinitely, hiding a transition to multi-page. `ListOpenPullRequests` uses GitHub's default `created desc` sort, so page 1 content can remain unchanged even when later-page items are modified. The 30-minute TTL forces periodic unconditional fetches, bounding the window during which such changes are invisible.

All four are self-contained in the transport. No syncer, client, or collectPages changes are needed.

### Not-Modified Detection

go-github treats non-2xx responses as errors. When the transport returns a 304, go-github wraps it in `*github.ErrorResponse`. Detection helper in `internal/github/etag_transport.go`:

```go
func IsNotModified(err error) bool {
    var ghErr *github.ErrorResponse
    return errors.As(err, &ghErr) && ghErr.Response.StatusCode == http.StatusNotModified
}
```

### Client Changes

`NewClient` wraps the OAuth2 HTTP client's transport with `etagTransport`:

```go
func NewClient(token string) Client {
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    tc := oauth2.NewClient(context.Background(), ts)
    tc.Transport = &etagTransport{base: tc.Transport}
    return &liveClient{gh: gh.NewClient(tc)}
}
```

### Syncer Changes

#### Head SHA Cache

The Syncer needs a new in-memory cache to support CI refresh when `ListOpenPullRequests` returns 304:

```go
type Syncer struct {
    // ... existing fields ...
    headSHAs map[int64]map[int]string // repoID -> PR number -> head SHA
}
```

Populated during normal sync (when `ListOpenPullRequests` returns 200). On each 200 response, replace the entire inner map for this repoID with only the PRs from the current response -- this automatically evicts entries for closed/merged PRs that no longer appear in the open list. Lost on process restart, but so is the ETag cache, so the first sync always does a full fetch.

#### Two call sites with IsNotModified handling

**1. `doSyncRepo` -- ListOpenPullRequests:**

The 304 path must NOT return early -- `doSyncRepo` still needs to run `syncIssues` after PR handling. Structure:

```go
ghPRs, err := s.client.ListOpenPullRequests(ctx, repo.Owner, repo.Name)
if IsNotModified(err) {
    // PR list unchanged -- still refresh CI status for existing open PRs
    if ciErr := s.refreshCIForExistingPRs(ctx, repo, repoID); ciErr != nil {
        slog.Error("refresh CI for existing PRs", "repo", repoName, "err", ciErr)
    }
    // Fall through to syncIssues below
} else if err != nil {
    return fmt.Errorf("list open PRs: %w", err)
} else {
    // Normal path: process PRs, handle closures, populate headSHAs cache
    // ... existing PR processing logic ...
}

// Always sync issues regardless of PR list ETag result
if err := s.syncIssues(ctx, repo, repoID); err != nil { ... }
```

New helper `refreshCIForExistingPRs`: reads the head SHA cache for this repoID, calls `refreshCIStatus` for each entry (which still fetches CI data from the GitHub API unconditionally -- no ETags on CI endpoints). If the cache is empty (first sync after restart), returns nil (the 304 won't happen anyway since there's no cached ETag on first sync).

On normal 200 response, replace the headSHAs entry for this repoID with only the current open PRs' head SHAs.

**2. `syncIssues` -- ListOpenIssues:**
```go
ghIssues, err := s.client.ListOpenIssues(ctx, repo.Owner, repo.Name)
if IsNotModified(err) {
    return nil // issues unchanged, nothing to do
}
if err != nil {
    return fmt.Errorf("list open issues: %w", err)
}
```

Issues have no independent background updates (unlike CI), so 304 means skip entirely.

### Pagination and ETags

`ListOpenPullRequests` and `ListOpenIssues` use `collectPages`. The ETag transport handles pagination entirely on its own — no syncer, client, or `collectPages` changes needed.

**Later pages bypass ETags:** URLs with a `page` query parameter > 1 skip ETag handling entirely (no `If-None-Match` sent, no ETag stored). go-github's first-page request has no `page` parameter; subsequent pages have `page=2`, `page=3`, etc. This prevents a correctness bug: if page 1 returns 200 (data changed) but page 2 had a stale cached ETag and returned 304, `collectPages` would abort with a not-modified error, discarding the real page-1 changes.

**Multi-page first pages evict cached ETags:** The transport checks the `Link` response header for `rel="next"`. If present, the response spans multiple pages — the transport does NOT cache the ETag and explicitly deletes any previously-cached entry for that URL. This handles the single-page → multi-page transition: a repo that was under 100 items (ETag cached) then grows past 100 won't retain a stale ETag.

**TTL bounds hidden transitions:** A cached single-page ETag can legitimately 304 even after the list grows to multiple pages, because `ListOpenPullRequests` uses GitHub's `created desc` sort — page 1 content may not change when items shift to later pages. The 30-minute TTL on cached ETags forces periodic unconditional fetches, bounding the window during which such changes are invisible. At the default 5-minute sync interval, this means ~6 ETag-accelerated cycles per unconditional refresh.

When the first page returns 304, `collectPages` returns the not-modified error immediately. The caller treats the entire list as unchanged and skips processing. Single-page repos (the common case) get full ETag benefit. Multi-page repos always fetch fresh after the first unconditional cycle detects the transition.

### ETag Cache Lifetime

The `sync.Map` lives on the transport for the process lifetime. Entries are keyed by full URL (including query params). Only single-page first-page URLs are cached (later pages and multi-page first pages are excluded), so cache size is bounded by repos * 2 list endpoints. For a typical setup (5 repos), this is at most 10 entries. Entries expire after `etagTTL` (30 minutes) and are lazily deleted on lookup. No background eviction needed.

---

## Feature 3: Frontend SSE Client

### Events Store

New file: `frontend/src/lib/stores/events.svelte.ts`

Central SSE connection manager:

```typescript
let eventSource: EventSource | null = null;
let connected = $state(false);

export function connect(): void
export function disconnect(): void
export function isSSEConnected(): boolean
```

**`connect()` implementation:**
- Constructs URL using `getBasePath()` from router store: `${basePrefix}/api/v1/events`
- Creates `EventSource` at that URL
- Registers event listeners:
  - `sync_status`: parse JSON payload, call `updateSyncFromSSE(status)` on sync store
  - `data_changed`: call refresh functions based on current view (see below)
  - `open`: set `connected = true`, call `disablePolling()` on all stores with polling (sync, activity, detail, pulls, issues), then **immediately fire the same full refresh that `data_changed` triggers** (global `loadPulls()` + `loadIssues()` plus the view-aware refreshes described below). This catches up any `data_changed` events missed while disconnected — without it, a client that disconnects briefly around sync completion would reconnect into an idle `sync_status` with stale list/detail data and no trigger to refresh until the next sync. The cost of a redundant refresh on the very first connect (before any data exists to miss) is negligible.
  - `error`: set `connected = false`, call `enablePolling()` on all stores with polling (sync, activity, detail, pulls, issues). EventSource handles reconnection automatically.

**View-aware refresh on `data_changed`:**

The events store imports `getPage()` from the router store to determine which views are active:

| Current page | View | Actions on `data_changed` (in addition to global refreshes below) |
|-------------|------|--------------------------|
| `pulls` | list | If a PR detail is selected (`getSelectedPR()` is non-null), call `refreshDetail()` with that PR's owner/name/number. |
| `pulls` | board | If the board drawer is open, call `refreshDetail()` for the drawer's PR. |
| `issues` | - | If an issue is selected, call `refreshFromSSE()` on the issues store for the selected issue's detail. |
| `activity` | - | `loadActivity()` for full feed refresh (not `pollNewItems()` — incremental append wouldn't update existing rows whose title/state changed during sync). If an activity detail drawer is open, fire the registered refresh callback (see below). |
| `settings` | - | No additional refresh needed. |

**Global refreshes:** `loadPulls()` AND `loadIssues()` are always called on `data_changed` regardless of current page. The status bar (`StatusBar.svelte`) is part of the layout chrome and visible on every page; it reads `getPulls().length`, `getIssues().length`, and a `repoCount()` derived from both stores. Without global refreshes, the counts would go stale on `activity`, `pulls`, and `settings` after background syncs change issue state. When on the board view, the events store calls `loadPulls({ state: "open" })` instead of the generic `loadPulls()` to match the board's filter. The `getView()` helper from the router store distinguishes list from board.

**Board view specifics:** `KanbanBoard.svelte` has its own drawer state (`drawerPR`) that is local to the component, not in the pulls store. The events store cannot directly access this. Two options: (a) move `drawerPR` into the pulls store so the events store can check it, or (b) have the board component register a refresh callback with the events store on mount and unregister on destroy. Option (b) is simpler and doesn't require restructuring the board's local state.

**Activity drawer specifics:** Activity selection lives in `App.svelte` local state (plus the `?selected=...` query parameter), not in the router store. Activity items can be either PRs or issues, so the refresh path must branch: PR items call `detail.refreshFromSSE(owner, name, number)`, issue items call `issues.refreshFromSSE(owner, name, number)`. Same callback pattern as the board: `App.svelte` registers a refresh callback with the events store on mount that checks the current selection type and calls the appropriate store. Unregisters on destroy.

### Store Changes

**Prerequisite: move component-level polling into stores.** Currently, `PullList.svelte`, `IssueList.svelte`, and `KanbanBoard.svelte` each have their own 15s `setInterval` timers that call `loadPulls()`/`loadIssues()` directly. These must be moved into their respective stores so the SSE events store can centrally control them. Without this, SSE would disable store-level polling but component-level timers would keep firing.

**Polling gate rule (applies to all stores below):** Every store maintains a module-level `pollingEnabled` flag. `disablePolling()` sets the flag to false and clears any active interval. `enablePolling()` sets the flag to true and recreates any timer whose active state is recorded (see per-store sections). Critically, every `start*Polling()` function must also check this flag: if `pollingEnabled` is false (SSE is connected), the start function records its active state (flag + overrides/target) but skips creating the interval. When SSE later disconnects, `enablePolling()` walks the recorded state and creates the appropriate timers. This ensures mount-time `start*Polling()` calls from a newly-navigated-to view don't defeat SSE by creating fallback timers while SSE is connected.

**Stale-response guard (applies to all stores below):** Every independent async resource maintains its own monotonic `requestVersion` counter (a simple `let listVersion = 0`, `let detailVersion = 0`, etc.). Each load function increments its counter at call time, captures the value, and checks it after `await`: if the captured version no longer equals the current counter, the response is stale (a newer request was issued while this one was in flight) and the result is discarded silently. This prevents a slow in-flight poll or mount-triggered fetch from overwriting data returned by a newer SSE-triggered refresh. Counters are per-resource, not per-store, because a single store may serve independent async paths that fire concurrently — e.g., `issues.svelte.ts` fires both `loadIssues()` (list) and `refreshFromSSE()` (detail) on `data_changed` when an issue is selected; a single shared counter would cause the later call to invalidate the earlier, discarding a valid response. Concrete counters: `pulls.svelte.ts` has `listVersion`; `issues.svelte.ts` has `listVersion` and `detailVersion`; `activity.svelte.ts` has `listVersion`; `detail.svelte.ts` has `detailVersion`; `sync.svelte.ts` has `syncVersion`. **Increment timing:** every load function (e.g., `loadPulls`, `refreshSyncStatus`) increments its counter at call time and captures the value. After `await`, it compares captured vs current — if different, the response is stale. Non-HTTP writes (`updateSyncFromSSE`, `triggerSync` optimistic update) also increment the counter before applying state, which invalidates any in-flight HTTP polls. `applySyncState()` (see `sync.svelte.ts` section below) does NOT increment — it only applies state. The caller is responsible for incrementing before calling it. This ensures overlapping polls are correctly ordered: two concurrent `refreshSyncStatus()` calls capture different versions (N and N+1), so the earlier one is correctly recognized as stale when it resolves after the later one. No `AbortController` is needed — the redundant fetch completes and is simply ignored on arrival.

**`pulls.svelte.ts`:**
- New `startListPolling(overrides?)` / `stopListPolling()` functions managing a 15s timer that calls `loadPulls(overrides)`. The optional `overrides` parameter lets callers lock the timer to specific filters (e.g., `{ state: "open" }` for the board view). `startListPolling` stores the active overrides AND sets a boolean `listPollingActive` flag to true. `stopListPolling` clears the interval, the stored overrides, AND sets the flag to false (component unmount — timer should not revive). Replaces the `setInterval` in `PullList.svelte` and `KanbanBoard.svelte`.
- New `enablePolling()` / `disablePolling()` to gate polling on SSE connection state. These are distinct from start/stop: `disablePolling` clears the interval but preserves both the stored overrides and the `listPollingActive` flag. `enablePolling` checks the flag — if true, recreates the timer with the stored overrides (so board polling restarts with `{ state: "open" }`, and plain sidebar polling restarts with no overrides). If false (component unmounted via `stopListPolling`), `enablePolling` is a no-op for the list timer.
- Events store calls `loadPulls()` on `data_changed`.

**`issues.svelte.ts`:**
- New `startListPolling()` / `stopListPolling()` functions managing a 15s timer that calls `loadIssues()`. Same `listPollingActive` flag pattern as pulls store. Replaces the `setInterval` in `IssueList.svelte`.
- Existing `startIssueDetailPolling` / `stopIssueDetailPolling` gain the same lifecycle/toggle semantics as `detail.svelte.ts`: `startIssueDetailPolling` stores the current target (owner/name/number) and sets `issueDetailActive` flag. `stopIssueDetailPolling` clears the interval, stored target, and flag. `enablePolling` / `disablePolling` check the flag — `disablePolling` preserves target, `enablePolling` recreates the timer if the flag is true. This ensures issue detail polling restarts correctly after SSE reconnect (for both the issues page and the activity drawer's issue items).
- New `refreshFromSSE(owner: string, name: string, number: number)` that calls the existing issue detail refresh.
- Events store calls `loadIssues()` on `data_changed` when issues page is active.

**`sync.svelte.ts`:**
- Internal `applySyncState(status: SyncStatus)` helper: updates `syncState`, runs `adjustPollingSpeed`, and fires `onSyncComplete` on running-to-idle transitions. Does NOT increment `syncVersion` — callers are responsible for incrementing before calling. This separation ensures that overlapping `refreshSyncStatus()` calls each capture a distinct version at call time, so the earlier one is correctly recognized as stale regardless of resolution order.
- `refreshSyncStatus()`: increments `syncVersion`, captures it, awaits HTTP call, checks captured vs current, calls `applySyncState` only if version matches.
- New `updateSyncFromSSE(status: SyncStatus)` function: increments `syncVersion` (invalidating any in-flight `refreshSyncStatus` poll), then calls `applySyncState(status)`. Same effect as `refreshSyncStatus` but without the HTTP call.
- `triggerSync()`'s optimistic update: increments `syncVersion`, then calls `applySyncState({ running: true })`. In-flight polls see stale version on resolution.
- New `enablePolling()` / `disablePolling()` functions following the polling gate rule. `disablePolling` clears the interval but preserves the current `currentIntervalMs` (the adaptive 2s-while-syncing or 30s-idle value). `enablePolling` recreates the timer at the preserved `currentIntervalMs` so a sync that was running during SSE disconnect resumes at 2s, not the 30s default.
- `startPolling` and `stopPolling` still exist for lifecycle (mount/unmount). `startPolling` records the `syncPollingActive` flag but checks `pollingEnabled` before creating the timer. When `pollingEnabled` is false, `startPolling` must NOT touch `currentIntervalMs` — the preserved adaptive value must survive mount-during-SSE. When `pollingEnabled` is true, `startPolling` uses `currentIntervalMs` (defaulting it only if unset). `stopPolling` clears the interval, the flag, and resets `currentIntervalMs` to the default.

**`activity.svelte.ts`:**
- New `enablePolling()` / `disablePolling()` functions following the polling gate rule. `startActivityPolling` / `stopActivityPolling` maintain an `activityPollingActive` flag; start records state and checks the gate before creating the timer.
- New `refreshFromSSE()` that calls `loadActivity()` (full refresh, not incremental `pollNewItems()`).

**`detail.svelte.ts`:**
- New `enablePolling()` / `disablePolling()` functions. `startDetailPolling` already stores the current target (owner/name/number) in module-level state. `stopDetailPolling` clears both the interval AND the stored target (component unmount). `disablePolling` clears the interval but preserves the stored target. `enablePolling` recreates the timer for the stored target (so detail polling restarts for the correct PR/issue after SSE reconnect). If no target is stored (detail already closed via `stopDetailPolling`), `enablePolling` is a no-op for the detail timer.
- New `refreshFromSSE(owner: string, name: string, number: number)` that calls the existing `refreshDetail()`.

### Connection Lifecycle

```
App mount      -> connect()
SSE open       -> set connected, disable all polling timers,
                  full refresh (loadPulls + loadIssues + view-aware)
SSE error      -> clear connected, enable all polling timers
                  (EventSource auto-reconnects with backoff)
SSE reconnect  -> same as open: set connected, disable polling,
                  full refresh to catch up missed data_changed
App unmount    -> disconnect() (close EventSource)
```

`connect()` is called from `App.svelte`'s `onMount`. `disconnect()` is called from `onDestroy`.

**No event replay:** The SSE endpoint does not use event IDs or maintain a replay buffer. Events emitted during a brief SSE disconnection are lost. This is acceptable because: (a) polling re-enables immediately on disconnect and catches up within seconds for views whose polling timers are active (pull list, issue list, activity, detail — when their components are mounted), and (b) the `open` handler fires a full refresh on every (re)connect, so the client catches up with any `data_changed` events missed during the disconnection window without waiting for the next sync cycle. **Limitation:** status-bar counts (pull/issue totals, repo count) can go stale during an SSE outage on any view, because each view only polls its own store: `pulls` polls `loadPulls()` but not `loadIssues()`, `issues` polls `loadIssues()` but not `loadPulls()`, and `activity`/`settings` don't poll either list store. This is acceptable — the counts are layout chrome, the outage is local and brief, and the `open` handler's full refresh of both stores corrects them on reconnect. Adding a global background poller for two badge numbers would add complexity for negligible benefit.

### Fallback Behavior

When SSE is disconnected (fallback):
- Sync status polling: 2s while syncing, 30s idle (current behavior, unchanged)
- Pull/issue list polling: 15s (only when list/board components are mounted)
- Activity polling: 15s (only when activity view is mounted)
- Detail polling: 60s (only when detail panel is open)
- Status-bar counts: not independently polled — stale on any view during outage (pulls page misses issue changes, issues page misses pull changes, activity/settings miss both). Corrected on SSE reconnect (open handler calls both `loadPulls()` and `loadIssues()`)

When SSE is connected:
- All polling timers disabled
- `data_changed` triggers immediate refresh of active views
- `sync_status` updates sync state directly (no HTTP call)

---

## Testing

### Go Tests

**`internal/server/event_hub_test.go`:**
- Subscribe returns a channel that receives broadcast events
- Unsubscribe on context cancel (no goroutine leak)
- Concurrent broadcast safety (multiple goroutines broadcasting)
- Slow consumer (full channel) doesn't block other subscribers
- `Broadcast` with a `sync_status` event updates the cached `lastSyncStatus`; subsequent `Subscribe` receives that event as the first channel value
- `Broadcast` with non-`sync_status` events (e.g. `data_changed`) does NOT update `lastSyncStatus`
- With no prior broadcast, a new `Subscribe` returns a channel with no pre-loaded event (nil cache)
- Ordering: subscriber A connects, receives cached X, then broadcaster sends Y under the same lock → A's channel contains [X, Y] in that order
- Mid-sync connect: broadcast sync_status T1 (seeds cache), broadcast sync_status T2 (updates cache), Subscribe → new subscriber's first event is T2, never T1

**`internal/github/etag_transport_test.go`:**
- 200 response stores ETag from header
- Subsequent request to same URL includes `If-None-Match` header
- 304 response returned as-is (status preserved), does NOT refresh `cachedAt` timestamp
- Different URLs get independent ETag entries
- Request without cached ETag has no `If-None-Match` header
- Requests with `page` query parameter > 1 bypass ETag handling (no `If-None-Match` sent, no ETag stored)
- 200 response with `Link: rel="next"` evicts any previously-cached ETag for that URL
- Single-page → multi-page → single-page transition: ETag cached on single-page, evicted on multi-page detection, re-cached when back to single-page
- Expired ETag entries (older than `etagTTL`) are treated as uncached
- TTL-driven multi-page detection: cached single-page ETag, one or more 304s (which must NOT refresh `cachedAt`), then after `etagTTL` the next request omits `If-None-Match`, gets a 200 with `Link: rel="next"`, and evicts the cache entry
- `IsNotModified` returns true for 304 errors, false for other errors
- Gate: non-GET requests (POST, PATCH, DELETE) to allowlisted paths bypass cache entirely — no `If-None-Match` sent, no ETag stored, no cache eviction
- Gate: GET requests to non-allowlisted paths (`/repos/{owner}/{name}/commits/{sha}/status`, `/repos/{owner}/{name}/commits/{sha}/check-runs`) bypass cache entirely — no `If-None-Match` sent even if a matching URL is in cache, no ETag stored on 200
- Gate: GET request to allowlisted path with cached ETag gets `If-None-Match` (positive control, confirming the allowlist works)

**Integration tests:**
- SSE endpoint: returns `text/event-stream` content type, receives events after broadcast, connection closes cleanly on client disconnect
- SSE endpoint sends initial `sync_status` event: server startup primes the hub via `Broadcast(sync_status)` from `Syncer.Status()`; a new subscription's very first received event is a `sync_status` frame with that snapshot, flushed to the client before any transition-driven broadcast
- SSE endpoint mid-sync connect: prime the hub, simulate a sync start broadcast T1, simulate a mid-sync progress broadcast T2, then open a new subscription. The first frame the client receives is T2 (the most recent), and the client never receives T1 or any older snapshot afterward. Regression guard for the "subscribe then queued older event" race.
- SSE endpoint startup with in-progress sync (regression for priming race): this is guarded by **two complementary tests**. (1) `cmd/middleman/app_test.go` drives the production `Bootstrap(cfg, configPath, mockClient)` helper with a mock GitHub client whose first list call blocks on a channel so a `RunOnce` can be stopped mid-cycle. After `Bootstrap` returns, the test calls `app.Syncer.Start(ctx)`, lets `RunOnce` broadcast its initial `Running: true` state to the callback, then opens a new SSE subscription through `app.Server`. The first frame the client receives is `{running: true}` (the most recent cached broadcast), not the zero-value snapshot used during priming. (2) `cmd/middleman/main_ast_test.go` loads the entire `cmd/middleman` package with `go/packages` (type info enabled). A first pass over `app.go` locates the unique package-level `Run` FuncDecl (`Recv == nil`, `Name == "Run"`, validated signature), failing if none or more than one exists. A second pass uses an `ast.Walk` visitor struct that carries `enclosing *ast.FuncDecl` **by value** (returning a new child visitor inside each `FuncDecl` subtree so siblings at package scope correctly see `enclosing == nil`) and visits every `*ast.SelectorExpr` in every non-test file (including `app.go`) — not just the callees of `*ast.CallExpr` — so that a method-value alias like `serve := app.Server.Serve; serve()` is caught at the earlier selector reference where `TypesInfo.Selections` still resolves it. It accepts both `types.MethodVal` (covering normal calls, method-value assignments, and any other `x.M` reference) and `types.MethodExpr` (covering the method-expression form like `(*server.Server).Serve(app.Server)`). The forbidden set is a `map[*types.Func]bool` built by looking up the concrete lifecycle methods via `types.NewMethodSet` on `*Syncer` and `*Server`. It fails if any selector resolves to a `*types.Func` in the forbidden set from a `FuncDecl` that is NOT pointer-identical to the `Run` node. Pointer identity on the enclosing FuncDecl defeats spoofing via `func (h helper) Run(...)` defined elsewhere; handling `MethodExpr` defeats spoofing via `(*server.Server).Serve(app.Server)`; walking all `SelectorExpr` nodes defeats spoofing via `serve := app.Server.Serve; serve()`; and the visitor-struct push/pop defeats spoofing via a package-scope `var serve = (*server.Server).Serve` declared after `Run`. Indirection through a locally-declared interface (`var s lifecycleIface = app.Server; s.Serve()`) is a documented known limitation — see "Known limitations" in the AST guardrail rationale. The two tests cover **different concerns**, with some overlap on direct-concrete regressions: `app_test.go` verifies the happy-path ordering of `Bootstrap` against a mock client, and `main_ast_test.go` verifies every direct concrete reference to the forbidden lifecycle methods in the entire `cmd/middleman` package sits inside the body of exactly the validated package-level `Run` FuncDecl in `app.go`. A regression that adds a direct concrete lifecycle call inside `Bootstrap` would be caught by both. Neither covers interface-aliased lifecycle calls; that case is left to code review.

- `Run` bind-error propagation: the test creates an already-bound TCP listener on an ephemeral port, then calls `Run(ctx, cfg, cfgPath, mockClient, boundAddr)`. `Run` must return a wrapped bind error (the `"listen: …"` prefix from the synchronous `Server.Listen` call), not `nil`. Verifies that `Listen` actually calls `net.Listen` synchronously and that bind errors cannot be masked by a later `ctx.Done()`.
- `Run` serve-error propagation after cancel: in an environment where the listener can be programmatically closed mid-serve, trigger a `Serve()` error simultaneously with `ctx` cancellation. `Run` must wait for the serve goroutine to exit and propagate the non-`ErrServerClosed` error from `errCh` instead of silently returning `nil` from the shutdown branch.
- `Run` shutdown happy path: bind to `127.0.0.1:0`, cancel `ctx`, verify `Run` returns `nil` and the listener is closed (subsequent `net.Dial` to the bound address fails).
- Shutdown ordering — sync is in-flight when bind fails (Bootstrap-based, deterministic): this test exercises the same shutdown ordering as `Run`'s deferred chain but controls sequencing explicitly via `Bootstrap` to avoid goroutine scheduling races. Install a mock GitHub client whose first list call blocks on a test channel (`runOnceEnteredCh`) until its context is canceled. Sequence: (1) call `Bootstrap(cfg, cfgPath, mockClient)` to get an `*App`, (2) call `app.Syncer.Start(ctx)` to launch the ticker goroutine, (3) wait on `runOnceEnteredCh` — this proves `RunOnce` has entered the blocked list call, (4) pre-bind the target port with a sentinel listener, (5) call `app.Server.Listen(addr)` — fails with "address already in use", (6) cancel the context and call `app.Syncer.Stop()`, (7) close the DB. Assert: (a) `Listen` returns the bind error, (b) `runOnceEnteredCh` was reached (guaranteed by step 3), (c) `Stop()` blocks until the mock channel is released by context cancellation — proving the syncer goroutine was still in-flight when the bind error occurred, (d) DB close runs after `Stop()` returns. This is the regression guard for the shutdown race where `defer app.DB.Close()` would otherwise run while a `RunOnce` was still mid-flight after a bind error. The test does not go through `Run` because `Run` calls `Start` and `Listen` back-to-back in the same goroutine with no barrier between them — goroutine scheduling cannot guarantee `RunOnce` enters before `Listen` runs.
- `Run` bind-error deferred cleanup (end-to-end through `Run`): complementary to the Bootstrap-based test above. Pre-bind the target port, then call `Run(ctx, cfg, cfgPath, mockClient, boundAddr)`. `Run` returns the wrapped bind error. This test does NOT assert that a sync was in-flight (the scheduling race makes that nondeterministic) — it only verifies that `Run`'s deferred chain (`cancelSync`, `Stop`, `DB.Close`) executes cleanly on the bind-error path without panicking, double-closing, or leaking goroutines. Combined with the Bootstrap-based test, the two cover both the ordering invariant and the actual `Run` code path.
- `Run` shutdown ordering — sync is in-flight when serve errors: identical setup, but instead of pre-binding, force `Server.Serve()` to return a non-`ErrServerClosed` error mid-flight (e.g., by closing `s.listener` directly from the test once the serve goroutine has started). Assert the same four properties: the wrapped serve error is returned, the syncer goroutine actually began a `RunOnce` first, `Stop()` had returned by the time `Run` unwinds, and the DB close ran strictly after the syncer goroutine exited.
- Pre-`Serve` shutdown regression test (direct `*Server` lifecycle, not through `Run`): in `internal/server/server_test.go`, construct a `*Server`, call `Listen("127.0.0.1:0")` to bind the socket, capture the bound address via `s.listener.Addr().String()`, then call `Shutdown(ctx)` **before** `Serve()` is ever invoked. Assert (1) `Shutdown` returns `nil`, (2) the bound port is released — a fresh `net.Listen("tcp", boundAddr)` on the same address must succeed (fail fast if the port is still held), (3) a subsequent call to `Serve()` returns `http.ErrServerClosed` (not a raw listener-close error), proving the `httpSrv.Shutdown` step set `inShutdown` atomically even though no listener was adopted. This is the regression guard for the shutdown-ordering bug where closing the listener before `httpSrv.Shutdown` would leak the port (first version) or where closing the listener first in the post-`Serve` case would cause `Serve` to return a raw close error (second version). Note: because port reuse is subject to `TIME_WAIT` on some systems, the test may need to enable `SO_REUSEADDR` on the probing listener or use a dial-failure check instead (`net.DialTimeout` to the address returns an error within a short deadline).
- SSE handler flush-on-error: wrap an `httptest.ResponseRecorder` with a custom writer whose `FlushError() error` method (the Go 1.20+ hook used by `http.NewResponseController(w).Flush()`) succeeds for the first N calls and returns a synthetic error on a later call. Drive the handler so that the first `rc.Flush()` (header flush in step 2) and the cached initial `sync_status` flush in the first select-loop iteration both succeed, then trigger a broadcast that causes the NEXT flush (either an event flush from step 5 or a keepalive flush from the ticker) to fail. Verify the handler returns promptly on that later flush error rather than looping on stale state. This ensures implementations cannot ignore post-write flush failures and still pass the test.
- Syncer with `onStatusChange`: callback fires for started/progress/complete transitions
- Syncer with `IsNotModified`: verify PR processing is skipped on 304, CI refresh still runs with cached head SHAs, issue sync still runs on PR list 304
- `Syncer.Stop()` waits for in-flight `RunOnce`: install a mock client whose list call blocks on a test channel. Call `Start(ctx)`, wait until the mock confirms the goroutine entered the blocked list call, then call `Stop()` from the test goroutine and assert it does NOT return immediately — measure that `Stop()` only returns after the mock channel is released (or after the parent ctx is canceled, which the test does manually to drive the unblock). Without the `done` channel and the `<-s.done` wait, this test would race and `Stop()` would return immediately. Direct unit-level guard for the waitable-shutdown contract.
- `Syncer.Stop()` waits for handler-triggered `TriggerRun` **without ever calling `Start`**: same mock-blocking-channel setup, but the test never calls `Start(ctx)`. It calls `NewSyncer(...)` then `TriggerRun()` directly, simulating an API handler firing on a syncer that was just constructed. Wait until the mock confirms the goroutine entered the blocked list call, then call `Stop()` from a separate goroutine. Assert (1) `Stop()` initially blocks (the in-flight `RunOnce` is not done), (2) `Stop()`'s `lifetimeCancel()` actually unblocks the in-flight HTTP call inside `RunOnce` (because the mock observes `lifetimeCtx.Done()`), and (3) `Stop()` returns shortly after. This is the regression guard for the contradiction where `done` would otherwise never close (no ticker goroutine ever ran) and `Stop` would wait forever on `<-s.done`. The fix — `NewSyncer` allocating `lifetimeCtx`/`lifetimeCancel` up front, and `Stop` closing `done` itself when `wasStarted` is false — is verified by this test.
- `Syncer.Stop()` waits for handler-triggered `TriggerRun` **after `Start`**: same as above but the test calls `Start(ctx)` first. Verifies that the same `Stop` semantics hold when both ticker- and handler-driven runs are in flight.
- `Syncer.Stop()` is idempotent: call `Stop()` twice in sequence on a stopped syncer; the second call must return without panicking on a double-close of `stopCh`. Verifies that the `stopOnce.Do` wrapping makes the teardown body run exactly once even when called repeatedly.
- `Syncer.Stop()` is shutdown-complete-idempotent across overlapping callers: install a mock client whose list call blocks on a test channel. Call `Start(ctx)`, wait until the mock confirms the goroutine entered the blocked list call, then spawn TWO concurrent `Stop()` calls (call A and call B) from separate test goroutines. Assert that **neither** `Stop()` call returns until the mock channel is released — both must observe shutdown completion, not just the one that won the `stopOnce.Do` race. Once the mock is released, both calls return shortly after. Without the post-once `<-s.done` / `wg.Wait()` waits being shared by all callers, call B would return immediately at the once boundary while call A was still waiting on `wg.Wait()`, and the test would catch that as call B returning before the mock release.
- `Syncer.Start()` after `Stop()` is a no-op: call `NewSyncer(...)`, then `Stop()`, then `Start(ctx)`. Assert `Start` returns without launching any goroutine (no subsequent `RunOnce` is ever observed on the mock GitHub client, and `Status().Running` stays `false`). Verifies the `if s.started || s.stopped` mutex-guarded check inside `Start`.
- `Syncer.Start()` called twice is a no-op on the second call: call `Start(ctx)` twice in sequence. Assert the second call does not spawn a second ticker goroutine — the mock GitHub client should observe only one `RunOnce` per tick interval, not two. Verifies the same mutex-guarded check.
- **`Syncer.Stop()` does NOT race past a freshly-spawned ticker goroutine that has not yet reached `wg.Add(1)`**: this is the regression guard for the WaitGroup race where `Start` spawns the goroutine and `Stop` immediately runs `wg.Wait()` with counter zero. **Instrumentation must observe ticker-goroutine entry and `RunOnce` entry directly, not the first mock list invocation**: a correct implementation that bracketed `wg.Add(1)` around the per-cycle `RunOnce` could still satisfy shutdown ordering even if `RunOnce` returned early on canceled context before ever reaching `ListOpenPullRequests` — making "first list call" an unreliable signal that would deadlock the test. The test must instead capture event ordering via a **monotonic atomic sequence counter**, not via channel select races (which are nondeterministic — see below).

   **Why channel select races are insufficient:** an earlier draft tried to verify ordering with a single `select` over `tickerStartedCh` and `stopReturnedCh`, treating `stopReturnedCh` winning as failure. That recipe is unsound: if `Stop()` returns first and the test goroutine is slow to reach the `select`, the ticker goroutine can also complete its send to (the buffered) `tickerStartedCh` before the test arrives. By the time the `select` runs, BOTH cases are ready, and Go's `select` chooses one uniformly at random — so a broken implementation can still false-pass. Channels record *that* events occurred, not their relative *order*. The fix is to stamp each event with a monotonically-increasing sequence number captured at the send site itself, then compare the stamps after both events are known to have happened.

   **Hook plumbing.** Add a test-only hook struct to `NewSyncer` (gated behind a test-only constructor variant or a build tag, NOT exposed in the production API). The struct contains:
   - A shared `*atomic.Int64` sequence counter (test-allocated). All four stamps below are written via `seq.Add(1)` so they are strictly monotonic across all hook sites.
   - Four `*atomic.Int64` stamp fields the test reads after the events fire: `tickerEnterStamp`, `runOnceEnterStamp`, `tickerExitStamp`, `stopReturnStamp`.
   - Three buffered `chan struct{}` (capacity 1) so the test can `<-` for the *fact* that each event happened: `tickerEnteredCh`, `tickerExitedCh`, `stopReturnedCh`. These are wakeup signals only; ordering is determined exclusively by the stamps.

   The hook callbacks fire at exactly four source-level positions in the production code. **Critical: every stamp is taken at the actual event site, never in the helper goroutine that observes the event.** Capturing a stamp in a helper goroutine after observing an event leaves a window between the real event and the stamp during which other hooks can fire and acquire intermediate sequence numbers — which destroys the ordering guarantee.

   - **Ticker goroutine entry hook** (`tickerEnter`): the very first statement inside the ticker goroutine spawned by `Start`, *before* its `defer close(s.done)` registration and *before* its `wg.Add(1)` for the initial run. The hook does (atomically): `tickerEnterStamp.Store(seq.Add(1))`, then non-blocking send to `tickerEnteredCh`. (Non-blocking send means the test does not block the production goroutine even if it never reads.)
   - **`RunOnce` entry hook** (`runOnceEnter`): the very first statement inside `RunOnce` itself, before any DB or network work. The hook does (atomically): `runOnceEnterStamp.Store(seq.Add(1))`. No wakeup channel — the test does not need to wait specifically for this event because the post-stop assertion uses the quiescence barrier (see ticker exit hook below) to ensure all RunOnce entries that *could* happen have happened. Instrumenting at `RunOnce` entry rather than in the mock GitHub client is necessary because `RunOnce` may return early on canceled context before ever reaching `ListOpenPullRequests`, in which case a mock-side hook would never fire and the test could either deadlock or false-pass on a broken implementation.
   - **Ticker goroutine exit hook** (`tickerExit`): registered as the *first* `defer` inside the ticker goroutine (so it runs *last* by Go's LIFO defer order — i.e., after `wg.Done`, after `close(s.done)`, after every per-cycle `RunOnce`). The hook does (atomically): `tickerExitStamp.Store(seq.Add(1))`, then non-blocking send to `tickerExitedCh`. This is the **quiescence barrier**: when the test observes `tickerExitedCh`, the ticker goroutine has done every RunOnce it will ever do, so `runOnceEnterStamp.Load()` after this point is the final value.
   - **Stop return hook** (`stopReturn`): added inside `Stop()` itself, immediately after `<-s.done` and `s.wg.Wait()` complete and immediately before `Stop()` returns. The hook does (atomically): `stopReturnStamp.Store(seq.Add(1))`, then non-blocking send to `stopReturnedCh`. **The stamp must be taken inside `Stop()`, not in the test-side helper goroutine that calls `Stop()`** — otherwise the scheduler can run the ticker or RunOnce hook in the gap between the real return and a helper-side `seq.Add(1)`, which would let a broken implementation false-pass.

   **Recipe:**
   1. Allocate `seq := new(atomic.Int64)`, the four stamp fields, and the three wakeup channels.
   2. Construct the syncer via the test-only constructor that accepts the hook struct.
   3. Call `Start(ctx)`.
   4. Spawn a helper goroutine that calls `Stop()`. The helper's only job is to drive `Stop()` to completion — it does NOT take a stamp itself. The stamp is taken inside `Stop()` by the `stopReturn` hook before `Stop()` returns. After `Stop()` returns, the helper goroutine can exit (or signal a per-test `helperDone` channel for cleanup, but that signal is not part of the ordering check).
   5. The test goroutine waits, in this order, with generous timeouts:
      - `<-tickerEnteredCh` (the ticker goroutine has entered — confirms the test will not deadlock waiting for a goroutine that never spawned)
      - `<-stopReturnedCh` (the `stopReturn` hook fired inside `Stop()` — `stopReturnStamp` is now the canonical Stop-return event time)
      - `<-tickerExitedCh` (the ticker goroutine's deferred chain has run to completion — quiescence barrier, no further `RunOnce` or `tickerEnter` events can occur)
      
      The order of the `<-` ops in the test goroutine does not matter for the *ordering* assertion because the assertion reads stamps. The order matters only for *liveness*: waiting for `tickerExitedCh` last guarantees the post-stop snapshot is final.
   6. **Deterministic ordering assertion** (the entire reason for this test):
      ```go
      tickerStamp := tickerEnterStamp.Load()
      stopStamp := stopReturnStamp.Load()
      if tickerStamp == 0 {
          t.Fatalf("ticker goroutine never entered")
      }
      if stopStamp == 0 {
          t.Fatalf("Stop() never returned")
      }
      if tickerStamp >= stopStamp {
          t.Fatalf("Stop() returned before ticker entered "+
              "(tickerStamp=%d stopStamp=%d): wg.Add(2) reservation "+
              "race not closed", tickerStamp, stopStamp)
      }
      ```
   7. **No `RunOnce` after `Stop` returned** assertion. Snapshot `runOnceEnterStamp` AFTER `tickerExitedCh` has fired (the quiescence barrier guarantees no further `runOnceEnter` hook can run, so the snapshot is final):
      ```go
      // We have observed tickerExitedCh, so the ticker goroutine
      // has finished its entire deferred chain — no further RunOnce
      // calls are possible. The snapshot is final.
      runStamp := runOnceEnterStamp.Load()
      // RunOnce entry stamp must EITHER be unset (no RunOnce ran)
      // OR predate stopReturnStamp. A RunOnce entry stamped after
      // stopReturnStamp means a sync escaped shutdown.
      if runStamp != 0 && runStamp >= stopStamp {
          t.Fatalf("RunOnce entered after Stop returned "+
              "(runStamp=%d stopStamp=%d)", runStamp, stopStamp)
      }
      ```
      The quiescence barrier closes the false-pass window in step 7: without it, a broken implementation could let a `RunOnce` start *between* the test's `runOnceEnterStamp.Load()` and the actual ticker exit, and the test would silently miss the escape. With the barrier, the load happens only after every possible `RunOnce` has both started and finished.

   The atomic sequence counter is what makes both ordering checks **deterministic**: each `seq.Add(1)` returns a strictly-increasing value, and the stamp comparisons use those values rather than the order in which the test goroutine observes wakeup channels. A broken implementation that lets `Stop()` return before the ticker enters will produce `stopStamp < tickerStamp` — fails step 6 unconditionally. A broken implementation that lets a `RunOnce` start after `Stop()` returns will produce `runStamp >= stopStamp` after the quiescence barrier — fails step 7 unconditionally. There is no scheduler interleaving that can mask either violation.

   Run with `-race` enabled to catch concurrent map writes, etc. The test should be repeated 100+ times in a loop (or with `go test -count=N`) to flush out scheduling-dependent races. **Do NOT** insert a `runtime.Gosched()` between `Start` and spawning the Stop helper — that yields scheduling to the freshly-spawned ticker goroutine *before* the Stop helper exists, making the target race **less** likely to fire, not more. If the test needs to coax the race into appearing, the right place to yield is *inside* the Stop helper itself, immediately before calling `Stop()`, so the ticker goroutine and the Stop call have a fighting chance of interleaving.
- **`Syncer.TriggerRun()` racing `Stop()` does NOT let a sync escape shutdown**: test creates a Syncer, spawns `TriggerRun()` and `Stop()` concurrently (paired in tight loops with `runtime.Gosched()` between iterations to interleave them). After the join, assert that no `RunOnce` was launched after `Stop` returned: instrument the mock client to record the order of (Stop returned, RunOnce started) events and assert no RunOnce-start follows the Stop-returned timestamp. The lifecycle mutex on TriggerRun's check-and-Add path is what makes this safe — without it, TriggerRun could `wg.Add(1)` after Stop's `wg.Wait()` had returned. Run repeatedly under `-race`.
- `RunOnce` AST guardrail. The bare-`go RunOnce` regression check lives in **three coordinated AST tests**, because the call sites it must protect span three packages: `cmd/middleman` (where `Run` composes lifecycle calls), `internal/server` (where handlers fire manual syncs at `huma_routes.go:775` and `settings_handlers.go:147`), and `internal/github` itself (where `*Syncer.Start`'s inlined ticker-driven `RunOnce` call and the new `*Syncer.TriggerRun` wrapper live — any future bare `RunOnce` added anywhere else in `internal/github` would bypass a guard scoped only to the other two packages). The three tests share the same forbidden-set construction logic via a small helper, and each scopes its `go/packages` load to the package whose call sites it protects:
  - Extension to `cmd/middleman/main_ast_test.go`: `RunOnce` is added to the forbidden set built from `types.NewMethodSet(*Syncer)`. The only exempted FuncDecl in this package is the already-covered package-level `Run` in `app.go`. `*Syncer.Start` and `*Syncer.TriggerRun` cannot be exempted here because their bodies live in `internal/github`, not in `cmd/middleman`, so they are not visible to the `go/packages` load scoped to this package — their exemptions belong in the `internal/github` companion test instead.
  - New file `internal/server/server_ast_test.go`: loads the `internal/server` package with `go/packages` and runs the same selector-walking visitor against the same forbidden set. There are no exempted FuncDecls in this package — the only legal call to drive a sync from anywhere in `internal/server` is `s.syncer.TriggerRun()`, which is a different method (`TriggerRun`), not `RunOnce`, so it never appears in the AST as a `RunOnce` selector. Any future bare `go s.syncer.RunOnce(...)` inside `internal/server` fails this test.
  - New file `internal/github/sync_ast_test.go`: loads the `internal/github` package with `go/packages` and runs the same selector-walking visitor against the same forbidden set. This is the **only** test where `*Syncer.Start` and `*Syncer.TriggerRun` exemptions are meaningful, because those FuncDecls actually live in this package. The exempted FuncDecls are `*Syncer.Start` (so the inlined `wg.Add` / `s.RunOnce(ctx)` / `wg.Done` block inside `Start`'s ticker loop is allowed) AND `*Syncer.TriggerRun` (so the wrapper that handler code calls is allowed). Both are looked up by `*types.Func` pointer identity via `types.NewMethodSet(*Syncer)`, the same mechanism as the forbidden set, so a rename of either method updates both the forbidden set and the exemption set together. Any bare `RunOnce` reference added to `internal/github` from a FuncDecl other than `Start` or `TriggerRun` fails this test.
  - All three tests share the forbidden-set construction via a small helper in a new shared `internal/asttest` package (just enough to look up `*Syncer.RunOnce` via `types.NewMethodSet`), so a rename of `RunOnce` updates all three tests in lockstep. This is the **only** code that the AST tests share — the visitor logic and exemption logic stays per-test because the exempted call sites differ between the three packages.
- Hub slow-consumer disconnect: subscribe with a context that does not cancel, then call `Broadcast` 17 times in a row (one more than the buffer). Assert that the 17th broadcast removes the subscriber from the hub's map AND closes the channel (the test reads from the channel and observes `ok == false` once the buffered events are drained). Then broadcast a `data_changed` event and assert it is NOT delivered to the closed channel — proving the hub does not panic and the closed subscriber stays gone. Regression guard for the silent-drop bug where a slow consumer would lose terminal events forever.
- SSE handler exits cleanly on hub-side channel close: open a real SSE subscription via `httptest`, then from the test deliberately overrun the buffer to make the hub close the subscriber's channel. Read the response body and assert (a) the handler returns without writing any zero-value `event: \ndata: {}\n\n` frames, (b) the connection closes from the server side (subsequent reads return EOF), and (c) reconnecting yields the cached `lastSyncStatus` as the first frame on the new subscription. End-to-end check that the two-value receive plus hub close path is wired correctly.

### Frontend Tests

- Events store: `connect()` creates EventSource with correct URL, `disconnect()` closes it
- Polling toggle: `disablePolling()` clears timers, `enablePolling()` restarts them with preserved config
- Lifecycle vs toggle: `stopListPolling()` then `enablePolling()` does NOT revive list timer (flag cleared on stop). Same for `stopDetailPolling()` / `stopIssueDetailPolling()` then `enablePolling()`
- Plain `startListPolling()` (no overrides) survives `disablePolling()` / `enablePolling()` cycle — timer restarts because `listPollingActive` flag is true even though overrides are undefined
- Issue detail polling: `startIssueDetailPolling` through disable/enable preserves target, `stopIssueDetailPolling` then `enablePolling` does not revive
- Mount while SSE connected: after `disablePolling()`, calling `startListPolling()` / `startDetailPolling()` / `startIssueDetailPolling()` / `startActivityPolling()` / sync store `startPolling()` records active state but does NOT create a timer. Subsequent `enablePolling()` creates the timer from the recorded state
- Sync interval preservation through mount: sync is running at 2s, SSE connects (`disablePolling()` preserves `currentIntervalMs: 2s`), sync store `startPolling()` is called from a newly-mounted view (must NOT clobber `currentIntervalMs` back to 30s), then SSE errors (`enablePolling()` recreates the timer at 2s, not 30s)
- `updateSyncFromSSE` updates state and fires completion callback
- `updateSyncFromSSE` idle-to-running transition: `pollingEnabled = false` (SSE connected), `currentIntervalMs = 30000`, SSE delivers `{ running: true }` → `currentIntervalMs` updates to 2000. Calling `enablePolling()` after this creates the fallback timer at 2000ms, not 30000ms
- `updateSyncFromSSE` running-to-idle transition: `pollingEnabled = false`, `currentIntervalMs = 2000`, SSE delivers `{ running: false }` → `currentIntervalMs` updates to 30000 AND `onSyncComplete` callback fires. Calling `enablePolling()` after this creates the fallback timer at 30000ms
- Overlapping polls: start two `refreshSyncStatus()` calls (poll A then poll B, both delayed). Resolve poll A first with `{ running: false }`. Assert A is discarded (its captured version < current because B's start incremented the counter). Resolve poll B with `{ running: true }`. Assert B is applied (its captured version matches current). Regression guard for overlapping-poll ordering.
- Stale poll vs triggerSync: start a `refreshSyncStatus()` poll (mock the fetch to delay), then call `triggerSync()` which increments `syncVersion` and applies optimistic `{ running: true }` via `applySyncState()`, then resolve the delayed poll with `{ running: false }`. Assert the stale response is discarded (state remains `running: true`, `onSyncComplete` was NOT fired, `currentIntervalMs` is 2s not 30s). Regression guard for the poll-vs-optimistic-update race.
- Stale poll vs SSE update (idle→running): start a `refreshSyncStatus()` poll (mock delayed), then call `updateSyncFromSSE({ running: true, ... })`, then resolve the delayed poll with idle state. Assert stale response is discarded (`running` stays true, `currentIntervalMs` stays 2s). Same pattern as above but via SSE path instead of optimistic trigger.
- Stale poll vs SSE update (running→idle): start from `running: true` / `currentIntervalMs: 2s`, start a delayed `refreshSyncStatus()` poll, then call `updateSyncFromSSE({ running: false, ... })` which fires `onSyncComplete` and sets `currentIntervalMs: 30s`. Resolve the delayed poll with stale `{ running: true }`. Assert stale response is discarded (state remains idle, `currentIntervalMs` stays 30s, `onSyncComplete` was NOT fired a second time). Mirror of the idle→running test above.
- View-aware refresh: `data_changed` triggers correct store functions based on current page
- Global refresh: `data_changed` calls both `loadPulls()` AND `loadIssues()` on every page (pulls, issues, activity, settings) to keep status-bar counts current
- Initial sync prime: SSE opens while sync store has `syncState = null` and `pollingEnabled = false` (because `open` fires disablePolling on all stores); the events store receives a `sync_status` frame as the first message on the EventSource (that frame is the hub's cached snapshot) and calls `updateSyncFromSSE`, which populates `syncState` and updates `currentIntervalMs` even though polling stays disabled. Subsequent `enablePolling()` would then create the fallback timer at the correct cadence.

---

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/server/event_hub.go` | SSE subscriber management and fan-out |
| `internal/server/event_hub_test.go` | Hub unit tests |
| `internal/github/etag_transport.go` | HTTP transport with ETag injection + `IsNotModified` helper |
| `internal/github/etag_transport_test.go` | Transport unit tests |
| `frontend/src/lib/stores/events.svelte.ts` | SSE client and connection management |
| `cmd/middleman/app.go` | `App` struct, `Bootstrap(cfg, configPath, ghClient)` helper that creates syncer + constructs server (primes hub, wires callback) without starting the syncer or binding, and `Run(ctx, cfg, configPath, ghClient, addr)` helper that calls `Bootstrap`, starts the syncer, synchronously binds via `Server.Listen(addr)` (returning any bind error directly), runs `Server.Serve()` in a goroutine, and selects on `ctx.Done()` to invoke `Server.Shutdown` (15s deadline, waiting for the serve goroutine to exit afterward) or on a server-error channel. `Run` is the **only** function in the entire `cmd/middleman` package that may reference `Syncer.Start`, `Server.Listen`, `Server.Serve`, or `Server.Shutdown`; `Bootstrap` is exposed separately so tests can inspect hub state between construction and start but must not call any of those lifecycle methods. |
| `cmd/middleman/app_test.go` | Startup-ordering integration tests that drive `Bootstrap` directly with a mock GitHub client that blocks mid-`RunOnce`, asserting the cached `lastSyncStatus` reflects the in-progress state for new subscribers. Also contains the `Run` bind-error, serve-error, and shutdown happy-path tests described in the Testing section. |
| `cmd/middleman/main_ast_test.go` | Type-aware regression test using `go/packages`: loads the entire `cmd/middleman` package with type info. A first pass locates the unique package-level `Run` FuncDecl in `app.go` (`Recv == nil`, `Name == "Run"`, validated signature) — failing if none or more than one exists. A second pass uses `ast.Walk` with a visitor struct carrying `enclosing` **by value** (so child visitors only see a `FuncDecl` as enclosing inside its own subtree — package-scope selectors after a `FuncDecl` correctly see `enclosing == nil`), visits every `*ast.SelectorExpr` in every non-test file **including `app.go`**, resolves each selector via `TypesInfo.Selections[sel]`, and accepts both `types.MethodVal` and `types.MethodExpr` kinds. The forbidden set is a `map[*types.Func]bool` built by looking up the concrete lifecycle methods via `types.NewMethodSet(*Syncer)` / `types.NewMethodSet(*Server)`, **including `RunOnce`**. The only exempted FuncDecl in this package is `Run` (FuncDecl pointer identity). `*Syncer.Start` and `*Syncer.TriggerRun` are NOT in this test's exemption set because their bodies live in `internal/github`, not in `cmd/middleman`, so they are not visible to this package-scoped load — exempting them here would be meaningless. Their exemptions live in the `internal/github/sync_ast_test.go` companion test instead. The build fails if any selector resolves to a `*types.Func` in the forbidden set inside a `FuncDecl` that is not pointer-identical to the `Run` node. Prevents bypass via: a new sibling helper file; a new function added inside `app.go` itself; a same-named method on another receiver; method-expression call syntax; method-value alias assignment; or a package-scope `var x = ...` declared after `Run` (visitor push/pop). Indirection through a locally-declared interface (`var s lifecycleIface = app.Server; s.Serve()`) is a known limitation, documented in the AST guardrail rationale. |
| `internal/server/server_ast_test.go` | Companion AST guardrail loaded with `go/packages` against the `internal/server` package. Uses the same selector-walking visitor and the same forbidden-set construction (sharing the lookup helper from `internal/asttest`), but with an **empty** exemption set: no FuncDecl in `internal/server` is permitted to reference the forbidden methods directly. The only legal way for handler code to fire a sync is `s.syncer.TriggerRun()`, which selects `TriggerRun`, not `RunOnce`, so it does not match the forbidden set. Prevents a future bare `go s.syncer.RunOnce(context.WithoutCancel(...))` from being reintroduced in `huma_routes.go`, `settings_handlers.go`, or any other handler in `internal/server`. Without this companion test, the `cmd/middleman`-scoped guard would not see the handler files at all, leaving the actual race-prone call sites unprotected. |
| `internal/github/sync_ast_test.go` | Companion AST guardrail loaded with `go/packages` against the `internal/github` package. Uses the same selector-walking visitor and the same forbidden-set construction (sharing the lookup helper from `internal/asttest`). The exemption set is `{*Syncer.Start, *Syncer.TriggerRun}` — both looked up via `types.NewMethodSet(*Syncer)` by `*types.Func` pointer identity, so a rename updates the forbidden and exemption sets in lockstep. These exemptions live here (not in `cmd/middleman/main_ast_test.go`) because the `Start` and `TriggerRun` FuncDecl bodies are only visible when loading the `internal/github` package itself. Prevents a future bare `RunOnce` reference from being added to any other FuncDecl in `internal/github` — e.g., a new helper method on `Syncer` that forgets to go through `TriggerRun`'s `wg.Add` bracketing. Without this companion test, the other two AST guards would not see `internal/github` at all, leaving the package where `Syncer` itself lives without any regression protection. |
| `internal/asttest/forbidden.go` | New unexported package containing two helper functions: `SyncerForbiddenSet(syncerPkg *types.Package) map[*types.Func]bool` (looks up `RunOnce`, `Start`, `Stop` on `*Syncer`) and `ServerForbiddenSet(serverPkg *types.Package) map[*types.Func]bool` (looks up `Listen`, `Serve`, `Shutdown` on `*Server`). The `cmd/middleman/main_ast_test.go` and `internal/server/server_ast_test.go` tests call both helpers (they load both packages via `go/packages`). The `internal/github/sync_ast_test.go` test calls only `SyncerForbiddenSet` — it does not need `internal/server` types because the only forbidden methods it checks are on `*Syncer`. The helpers are split so `sync_ast_test.go` does not need to load or reference `internal/server`. The visitor and exemption logic stays per-test because the exempted call sites differ between packages. |

### Modified Files
| File | Change |
|------|--------|
| `internal/server/server.go` | Add `EventHub` field (with `done` channel and `closeOnce`), register `GET /api/v1/events` handler using `http.NewResponseController(w)` for flushable writes with per-handler `rc.SetWriteDeadline(time.Time{})` to clear the server-wide write timeout for SSE only, wire syncer callback via `SetOnStatusChange`, prime the hub with `Broadcast(sync_status)` from `syncer.Status()` during server construction. **Remove** the old `ListenAndServe(addr)` method and replace it with three explicit lifecycle methods: `Listen(addr string) error` synchronously creates the `*http.Server` (existing `WriteTimeout: 30s`, `ReadTimeout: 15s`, `IdleTimeout: 60s`) AND calls `net.Listen("tcp", addr)` AND stores the resulting listener into `s.listener` — returning any bind error directly; `Serve() error` calls `s.httpSrv.Serve(s.listener)` against the already-bound listener (never attempts a bind itself); `Shutdown(ctx context.Context) error` calls `s.hub.Close()` first (closing the `done` channel so SSE handlers exit within bounded time), then `s.httpSrv.Shutdown(ctx)`, then `s.listener.Close()` (idempotent, handles the pre-`Serve` port-release case). When `Listen` was never called (test path via `httptest.NewServer`), both `httpSrv` and `listener` are nil — `Shutdown` still calls `hub.Close()` but the `httpSrv.Shutdown` and `listener.Close` steps are no-ops. Binding in `Listen` and removing `ListenAndServe` entirely ensures bind errors surface synchronously to `Run` and cannot be masked by an early `ctx` cancellation. |
| `cmd/middleman/main.go` | Replace inline wiring with a single call to `Run(ctx, cfg, configPath, ghClient, addr)`. `main.go` does not reference `app.Syncer` or `app.Server` directly — `Run` sequences `Bootstrap` → `Syncer.Start` → `Server.Listen` → `Server.Serve` (goroutine) → `Server.Shutdown` on ctx cancel. Enforced by `main_ast_test.go`. |
| `internal/github/client.go` | Wrap OAuth2 transport with `etagTransport` in `NewClient` |
| `internal/github/sync.go` | Add `onStatusChange` callback + setter, `headSHAs` cache, `IsNotModified` checks at 2 call sites, `refreshCIForExistingPRs` helper, refactor `refreshCIStatus` to take `number int, headSHA string` instead of `*gh.PullRequest`. **Make `Stop()` waitable across both ticker- and handler-driven runs**: add `done chan struct{}`, `stopCh chan struct{}`, `lifecycleMu sync.Mutex`, `started bool` and `stopped bool` (both guarded by `lifecycleMu`), `stopOnce sync.Once` (wrapping the one-shot teardown work), `wg sync.WaitGroup`, and a `lifetimeCtx` / `lifetimeCancel` pair to the `Syncer` (all allocated in `NewSyncer` so the lifetime state is valid before `Start` runs). `Start` takes the mutex, refuses if already started or stopped, sets `started=true`, **calls `wg.Add(2)` for the linker and ticker goroutines BEFORE releasing the mutex** so a concurrent `Stop` cannot reach `wg.Wait()` with counter zero, then spawns the goroutines outside the lock. Each goroutine has `defer wg.Done()`. The ticker goroutine inlines `wg.Add(1) / s.RunOnce(ctx) / wg.Done()` directly around each per-cycle run (no helper, so the AST guardrail only needs to exempt `Start` by FuncDecl identity). The ticker goroutine's `defer close(s.done)` is unconditional (only this goroutine closes done in the started path). Add a public `TriggerRun()` wrapper (no ctx parameter) that handler code calls instead of `go s.syncer.RunOnce(...)`. `TriggerRun` takes the mutex, refuses if `stopped`, calls `wg.Add(1)` BEFORE releasing the mutex, then spawns a goroutine with `defer wg.Done()` that runs `RunOnce(s.lifetimeCtx)` so manual runs are bounded by the syncer's own lifetime, not the request lifecycle. `Stop()` wraps the one-shot teardown (mutex critical section that flips `stopped=true`, captures `wasStarted=s.started`, closes `stopCh`, calls `lifetimeCancel()`, releases the mutex, and conditionally closes `s.done` if `!wasStarted`) inside `stopOnce.Do(...)`. **Outside** the once, every `Stop` caller — first or otherwise — falls through to the shared `<-s.done` and `s.wg.Wait()` waits, so overlapping `Stop` calls are all idempotent in the shutdown-complete sense rather than just in the state-transition sense. The two server-side handlers (`huma_routes.go:775`, `settings_handlers.go:147`) change from `go s.syncer.RunOnce(context.WithoutCancel(...))` to `s.syncer.TriggerRun()`. After this change there are no bare `go syncer.RunOnce` call sites in the codebase. |
| `internal/server/huma_routes.go` | Replace the bare `go s.syncer.RunOnce(context.WithoutCancel(ctx))` at line 775 with `s.syncer.TriggerRun()`. The wrapper takes no ctx parameter — it uses the syncer's own `lifetimeCtx` internally, so the handler does not need to construct one. |
| `internal/server/settings_handlers.go` | Same change at line 147: replace the bare `go s.syncer.RunOnce(context.WithoutCancel(r.Context()))` with `s.syncer.TriggerRun()`. |
| `cmd/middleman/app.go` (already in New Files) | Note that `Run`'s deferred chain establishes the shutdown order with care: the deferred `app.DB.Close()` is registered first so it runs LAST, and the deferred `cancelSync(); app.Syncer.Stop()` is registered after it so it runs first, blocking on the sync goroutine's exit before the DB is closed. `Run` creates `syncCtx, cancelSync := context.WithCancel(ctx)` and passes `syncCtx` to `Syncer.Start` so the deferred `cancelSync()` actually unblocks any in-flight HTTP request inside `RunOnce` even on the bind-error path where the parent `ctx` was never canceled. |
| `frontend/src/lib/stores/sync.svelte.ts` | Add `updateSyncFromSSE`, `enablePolling`/`disablePolling`, `pollingEnabled` flag |
| `frontend/src/lib/stores/activity.svelte.ts` | Add `refreshFromSSE`, `enablePolling`/`disablePolling`, `pollingEnabled` flag |
| `frontend/src/lib/stores/detail.svelte.ts` | Add `refreshFromSSE`, `enablePolling`/`disablePolling`, `pollingEnabled` flag |
| `frontend/src/lib/stores/issues.svelte.ts` | Add `refreshFromSSE`, `enablePolling`/`disablePolling`, `pollingEnabled` flag |
| `frontend/src/lib/stores/pulls.svelte.ts` | Add `startListPolling`/`stopListPolling`, `enablePolling`/`disablePolling`, `pollingEnabled` flag |
| `frontend/src/lib/components/sidebar/PullList.svelte` | Remove 15s `setInterval`, call `startListPolling`/`stopListPolling` from pulls store |
| `frontend/src/lib/components/sidebar/IssueList.svelte` | Remove 15s `setInterval`, call `startListPolling`/`stopListPolling` from issues store |
| `frontend/src/lib/components/kanban/KanbanBoard.svelte` | Remove 15s `setInterval`, call `startListPolling({ state: "open" })`/`stopListPolling` from pulls store |
| `frontend/src/App.svelte` | Call `connect()` on mount, `disconnect()` on destroy |
