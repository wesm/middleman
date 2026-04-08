# SSE + ETag Live Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace frontend polling with server-sent events for instant UI updates, and add ETag-based conditional requests to reduce GitHub API rate limit consumption.

**Architecture:** EventHub manages SSE subscriber fan-out with bounded shutdown. ETag transport wraps the OAuth2 HTTP client to inject conditional headers. Frontend stores gain enable/disable polling gates controlled by the SSE events store. Server lifecycle splits into Listen/Serve/Shutdown with an App struct orchestrating startup in cmd/middleman/app.go.

**Tech Stack:** Go (stdlib net/http, go/packages for AST tests), Svelte 5 (runes, $state, $effect), go-github/v84, SQLite (unchanged)

---

## File Structure

### New Files (Go)
| File | Responsibility |
|------|---------------|
| `internal/server/event_hub.go` | EventHub struct, Subscribe/Broadcast/Close, slow-consumer eviction |
| `internal/server/event_hub_test.go` | Hub unit tests (subscribe, broadcast, slow consumer, cache, close) |
| `internal/github/etag_transport.go` | HTTP transport wrapper with ETag injection, IsNotModified helper |
| `internal/github/etag_transport_test.go` | Transport unit tests (cache, TTL, pagination, gate) |
| `cmd/middleman/app.go` | App struct, Bootstrap helper, Run orchestrator |
| `cmd/middleman/app_test.go` | Bootstrap/Run integration tests |
| `cmd/middleman/main_ast_test.go` | AST guardrail: lifecycle calls only in Run |
| `internal/server/server_ast_test.go` | AST guardrail: no bare RunOnce in server |
| `internal/github/sync_ast_test.go` | AST guardrail: RunOnce only in Start/TriggerRun |
| `internal/asttest/forbidden.go` | Shared forbidden-set construction helpers |

### New Files (Frontend)
| File | Responsibility |
|------|---------------|
| `frontend/src/lib/stores/events.svelte.ts` | SSE connection manager, view-aware refresh dispatch |

### Modified Files (Go)
| File | Changes |
|------|---------|
| `internal/server/server.go` | Add hub/httpSrv/listener fields, replace ListenAndServe with Listen/Serve/Shutdown, register SSE handler, wire callback, prime hub |
| `internal/github/sync.go` | Add onStatusChange callback, headSHAs cache, lifecycleMu/stopOnce/wg/done/lifetimeCtx, waitable Stop, TriggerRun, IsNotModified handling |
| `internal/github/client.go` | Wrap transport with etagTransport in NewClient |
| `internal/server/huma_routes.go` | Replace `go s.syncer.RunOnce(...)` with `s.syncer.TriggerRun()` at line 775 |
| `internal/server/settings_handlers.go` | Replace `go s.syncer.RunOnce(...)` with `s.syncer.TriggerRun()` at line 147 |
| `cmd/middleman/main.go` | Replace inline wiring with single `Run(ctx, cfg, configPath, ghClient, addr)` call |

### Modified Files (Frontend)
| File | Changes |
|------|---------|
| `frontend/src/lib/stores/sync.svelte.ts` | Add updateSyncFromSSE, applySyncState, requestVersion, enablePolling/disablePolling |
| `frontend/src/lib/stores/pulls.svelte.ts` | Add startListPolling/stopListPolling, enablePolling/disablePolling, listVersion |
| `frontend/src/lib/stores/issues.svelte.ts` | Add startListPolling/stopListPolling, enablePolling/disablePolling, listVersion/detailVersion |
| `frontend/src/lib/stores/activity.svelte.ts` | Add refreshFromSSE, enablePolling/disablePolling, listVersion |
| `frontend/src/lib/stores/detail.svelte.ts` | Add refreshFromSSE, enablePolling/disablePolling, detailVersion |
| `frontend/src/lib/components/sidebar/PullList.svelte` | Remove setInterval, use startListPolling/stopListPolling |
| `frontend/src/lib/components/sidebar/IssueList.svelte` | Remove setInterval, use startListPolling/stopListPolling |
| `frontend/src/lib/components/kanban/KanbanBoard.svelte` | Remove setInterval, use startListPolling/stopListPolling |
| `frontend/src/App.svelte` | Call connect()/disconnect(), register activity drawer refresh callback |

---

## Task 1: EventHub Core

**Files:**
- Create: `internal/server/event_hub.go`
- Test: `internal/server/event_hub_test.go`

- [ ] **Step 1: Write failing tests for Subscribe and Broadcast**

```go
// internal/server/event_hub_test.go
package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventHub_SubscribeReceivesBroadcast(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)
	hub.Broadcast(Event{Type: "data_changed", Data: struct{}{}})

	select {
	case ev := <-ch:
		assert.Equal(t, "data_changed", ev.Type)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for event")
	}
}

func TestEventHub_UnsubscribeOnContextCancel(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := hub.Subscribe(ctx)
	cancel()

	// Receive blocks until the cleanup goroutine closes the channel.
	// No sleep needed — a closed channel yields ok=false immediately.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed after context cancel")
	case <-time.After(time.Second):
		require.FailNow(t, "channel not closed after context cancel")
	}
}

func TestEventHub_ConcurrentBroadcastSafety(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			hub.Broadcast(Event{Type: "sync_status", Data: i})
		}
		close(done)
	}()
	go func() {
		for i := 0; i < 100; i++ {
			hub.Broadcast(Event{Type: "data_changed", Data: i})
		}
	}()

	<-done
	// Drain channel - should not panic
	for len(ch) > 0 {
		<-ch
	}
}

func TestEventHub_SlowConsumerEvicted(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)

	// Fill buffer (16) + one more to trigger eviction
	for i := 0; i < 17; i++ {
		hub.Broadcast(Event{Type: "data_changed", Data: i})
	}

	// Drain buffered events; channel should close
	count := 0
	for range ch {
		count++
	}
	assert.Equal(t, 16, count, "should receive exactly buffer-size events before close")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestEventHub -v`
Expected: compilation errors (types not defined)

- [ ] **Step 3: Implement EventHub**

```go
// internal/server/event_hub.go
package server

import (
	"context"
	"sync"
)

// Event represents an SSE event to broadcast.
type Event struct {
	Type string
	Data any
}

// EventHub manages SSE subscribers with fan-out broadcasting.
type EventHub struct {
	mu             sync.Mutex
	subscribers    map[uint64]chan Event
	nextID         uint64
	lastSyncStatus *Event
	done           chan struct{}
	closeOnce      sync.Once
}

// NewEventHub creates a ready-to-use hub.
func NewEventHub() *EventHub {
	return &EventHub{
		subscribers: make(map[uint64]chan Event),
		done:        make(chan struct{}),
	}
}

// Subscribe registers a new subscriber. Returns the event channel and
// the hub's done channel. The event channel is pre-loaded with the
// cached lastSyncStatus if available.
func (h *EventHub) Subscribe(ctx context.Context) (<-chan Event, <-chan struct{}) {
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	ch := make(chan Event, 16)
	if h.lastSyncStatus != nil {
		ch <- *h.lastSyncStatus
	}
	h.subscribers[id] = ch
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.unsubscribe(id)
	}()

	return ch, h.done
}

// unsubscribeLocked removes and closes the subscriber channel.
// Caller must hold mu.
func (h *EventHub) unsubscribeLocked(id uint64) {
	if ch, ok := h.subscribers[id]; ok {
		delete(h.subscribers, id)
		close(ch)
	}
}

// unsubscribe is the locking wrapper for context-cancel cleanup.
func (h *EventHub) unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unsubscribeLocked(id)
}

// Broadcast sends an event to all subscribers. If event.Type is
// "sync_status", the event is cached for future subscribers.
// Slow consumers (full channel) are evicted.
func (h *EventHub) Broadcast(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if event.Type == "sync_status" {
		e := event // copy
		h.lastSyncStatus = &e
	}

	for id, ch := range h.subscribers {
		select {
		case ch <- event:
		default:
			// Slow consumer — evict
			h.unsubscribeLocked(id)
		}
	}
}

// Close shuts down the hub: closes the done channel so SSE handlers
// exit, then cleans up all subscriber channels.
func (h *EventHub) Close() {
	h.closeOnce.Do(func() {
		close(h.done)
		h.mu.Lock()
		defer h.mu.Unlock()
		for id := range h.subscribers {
			h.unsubscribeLocked(id)
		}
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestEventHub -v`
Expected: all 4 tests PASS

- [ ] **Step 5: Write and run remaining hub tests**

Add to `event_hub_test.go`:

```go
func TestEventHub_SyncStatusCachedForNewSubscribers(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	hub.Broadcast(Event{Type: "sync_status", Data: map[string]bool{"running": true}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)

	select {
	case ev := <-ch:
		assert.Equal(t, "sync_status", ev.Type)
	case <-time.After(time.Second):
		require.FailNow(t, "expected cached sync_status")
	}
}

func TestEventHub_DataChangedNotCached(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	hub.Broadcast(Event{Type: "data_changed", Data: struct{}{}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)

	select {
	case <-ch:
		require.FailNow(t, "data_changed should not be cached for new subscribers")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestEventHub_NoCacheBeforeAnyBroadcast(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)

	select {
	case <-ch:
		require.FailNow(t, "expected no pre-loaded event")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestEventHub_CacheUpdatedOnLatestSyncStatus(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	hub.Broadcast(Event{Type: "sync_status", Data: "t1"})
	hub.Broadcast(Event{Type: "sync_status", Data: "t2"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)

	ev := <-ch
	assert.Equal(t, "t2", ev.Data, "new subscriber should get the latest cached status")
}

func TestEventHub_SubscribeOrderingWithBroadcast(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	hub.Broadcast(Event{Type: "sync_status", Data: "cached"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := hub.Subscribe(ctx)
	hub.Broadcast(Event{Type: "data_changed", Data: "live"})

	ev1 := <-ch
	assert.Equal(t, "sync_status", ev1.Type)
	assert.Equal(t, "cached", ev1.Data)

	ev2 := <-ch
	assert.Equal(t, "data_changed", ev2.Type)
	assert.Equal(t, "live", ev2.Data)
}

func TestEventHub_CloseUnsubscribesAll(t *testing.T) {
	hub := NewEventHub()

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	ch1, done := hub.Subscribe(ctx1)
	ch2, _ := hub.Subscribe(ctx2)

	hub.Close()

	// done channel should be closed
	select {
	case <-done:
	case <-time.After(time.Second):
		require.FailNow(t, "done channel should be closed")
	}

	// subscriber channels should be closed
	_, ok1 := <-ch1
	assert.False(t, ok1)
	_, ok2 := <-ch2
	assert.False(t, ok2)
}

func TestEventHub_BroadcastAfterSlowConsumerEviction(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe slow consumer
	hub.Subscribe(ctx)

	// Fill + overflow to evict
	for i := 0; i < 17; i++ {
		hub.Broadcast(Event{Type: "data_changed", Data: i})
	}

	// New broadcast should not panic (evicted subscriber gone)
	hub.Broadcast(Event{Type: "data_changed", Data: "after-eviction"})
}
```

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestEventHub -v`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/event_hub.go internal/server/event_hub_test.go
git commit -m "feat: add EventHub for SSE subscriber fan-out"
```

---

## Task 2: ETag Transport

**Files:**
- Create: `internal/github/etag_transport.go`
- Test: `internal/github/etag_transport_test.go`

- [ ] **Step 1: Write failing tests for core ETag behavior**

```go
// internal/github/etag_transport_test.go
package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTransport(handler http.HandlerFunc) (*etagTransport, *httptest.Server) {
	srv := httptest.NewServer(handler)
	return &etagTransport{base: http.DefaultTransport}, srv
}

func TestETagTransport_StoresETagOn200(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"abc123"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/owner/name/pulls", nil)
	resp, err := et.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Second request should include If-None-Match
	req2, _ := http.NewRequest("GET", "https://api.github.com/repos/owner/name/pulls", nil)
	et.base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, `"abc123"`, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.WriteHeader(304)
		return rec.Result(), nil
	})
	resp2, err := et.RoundTrip(req2)
	require.NoError(t, err)
	assert.Equal(t, 304, resp2.StatusCode)
}

func TestETagTransport_304DoesNotRefreshCachedAt(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"e1"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/n/pulls", nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	// Record initial cachedAt
	val, ok := et.cache.Load("https://api.github.com/repos/o/n/pulls")
	require.True(t, ok)
	initialCachedAt := val.(etagEntry).cachedAt

	// 304 should not update cachedAt
	et.base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(304)
		return rec.Result(), nil
	})
	req2, _ := http.NewRequest("GET", "https://api.github.com/repos/o/n/pulls", nil)
	_, err = et.RoundTrip(req2)
	require.NoError(t, err)

	val2, _ := et.cache.Load("https://api.github.com/repos/o/n/pulls")
	assert.Equal(t, initialCachedAt, val2.(etagEntry).cachedAt)
}

func TestETagTransport_DifferentURLsIndependent(t *testing.T) {
	callCount := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"etag`+r.URL.Path+`"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req1, _ := http.NewRequest("GET", "https://api.github.com/repos/a/b/pulls", nil)
	req2, _ := http.NewRequest("GET", "https://api.github.com/repos/c/d/issues", nil)
	_, _ = et.RoundTrip(req1)
	_, _ = et.RoundTrip(req2)

	val1, _ := et.cache.Load("https://api.github.com/repos/a/b/pulls")
	val2, _ := et.cache.Load("https://api.github.com/repos/c/d/issues")
	assert.NotEqual(t, val1.(etagEntry).etag, val2.(etagEntry).etag)
}

func TestETagTransport_PageGt1BypassesETag(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"), "page>1 should not send If-None-Match")
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"should-not-cache"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/n/pulls?page=2", nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	_, ok := et.cache.Load(req.URL.String())
	assert.False(t, ok, "page>1 response should not be cached")
}

func TestETagTransport_MultiPageEvictsCachedETag(t *testing.T) {
	// First request: single page, cache ETag
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"single"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	url := "https://api.github.com/repos/o/n/pulls"
	req, _ := http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	_, ok := et.cache.Load(url)
	assert.True(t, ok, "single-page ETag should be cached")

	// Second request: multi-page (Link: next), should evict
	et.base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"multi"`)
		rec.Header().Set("Link", `<https://api.github.com/repos/o/n/pulls?page=2>; rel="next"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})

	req2, _ := http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req2)
	_, ok = et.cache.Load(url)
	assert.False(t, ok, "multi-page response should evict cached ETag")
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestETagTransport -v`
Expected: compilation errors (types not defined)

- [ ] **Step 3: Implement etagTransport and IsNotModified**

```go
// internal/github/etag_transport.go
package github

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v84/github"
)

const etagTTL = 30 * time.Minute

// etagAllowedPaths lists URL path suffixes eligible for ETag handling.
var etagAllowedPaths = []string{"/pulls", "/issues"}

type etagEntry struct {
	etag     string
	cachedAt time.Time
}

type etagTransport struct {
	base  http.RoundTripper
	cache sync.Map // URL string -> etagEntry
}

func (t *etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Gate: only GET requests to allowlisted endpoints
	if req.Method != http.MethodGet || !isETagEligible(req.URL.Path) {
		return t.base.RoundTrip(req)
	}

	// Skip later pages
	if page := req.URL.Query().Get("page"); page != "" && page != "1" {
		return t.base.RoundTrip(req)
	}

	url := req.URL.String()

	// Check cache
	if val, ok := t.cache.Load(url); ok {
		entry := val.(etagEntry)
		if time.Since(entry.cachedAt) < etagTTL {
			req = req.Clone(req.Context())
			req.Header.Set("If-None-Match", entry.etag)
		} else {
			t.cache.Delete(url)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		etag := resp.Header.Get("ETag")
		if etag != "" && !hasLinkNext(resp) {
			t.cache.Store(url, etagEntry{etag: etag, cachedAt: time.Now()})
		} else if hasLinkNext(resp) {
			t.cache.Delete(url)
		}
	case http.StatusNotModified:
		// Do not update cachedAt — let TTL expire
	}

	return resp, nil
}

func isETagEligible(path string) bool {
	for _, suffix := range etagAllowedPaths {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func hasLinkNext(resp *http.Response) bool {
	for _, link := range resp.Header.Values("Link") {
		if strings.Contains(link, `rel="next"`) {
			return true
		}
	}
	return false
}

// IsNotModified returns true if the error represents a 304 Not Modified
// response from the GitHub API.
func IsNotModified(err error) bool {
	var ghErr *gh.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotModified
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestETagTransport -v`
Expected: all tests PASS

- [ ] **Step 5: Write and run gate and TTL tests**

Add to `etag_transport_test.go`:

```go
func TestETagTransport_NonGETBypassesCache(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"should-not-cache"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		req, _ := http.NewRequest(method, "https://api.github.com/repos/o/n/pulls", nil)
		_, err := et.RoundTrip(req)
		require.NoError(t, err)
	}

	// Nothing should be cached
	count := 0
	et.cache.Range(func(_, _ any) bool { count++; return true })
	assert.Equal(t, 0, count)
}

func TestETagTransport_NonAllowlistedPathBypassesCache(t *testing.T) {
	// Pre-populate cache with the URL to prove gate blocks it
	url := "https://api.github.com/repos/o/n/commits/abc123/status"
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"nope"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}
	et.cache.Store(url, etagEntry{etag: `"stale"`, cachedAt: time.Now()})

	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)
}

func TestETagTransport_AllowlistedPathPositiveControl(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"allowed"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/n/pulls", nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	_, ok := et.cache.Load("https://api.github.com/repos/o/n/pulls")
	assert.True(t, ok, "allowlisted path should be cached")
}

func TestETagTransport_SingleMultiSingleTransition(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	phase := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		switch phase {
		case 0: // single-page
			rec.Header().Set("ETag", `"v1"`)
			rec.WriteHeader(200)
		case 1: // multi-page
			rec.Header().Set("ETag", `"v2"`)
			rec.Header().Set("Link", `<https://api.github.com/repos/o/n/pulls?page=2>; rel="next"`)
			rec.WriteHeader(200)
		case 2: // back to single-page
			rec.Header().Set("ETag", `"v3"`)
			rec.WriteHeader(200)
		}
		return rec.Result(), nil
	})}

	// Phase 0: single-page, should cache
	phase = 0
	req, _ := http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	_, ok := et.cache.Load(url)
	assert.True(t, ok, "phase 0: single-page should cache")

	// Phase 1: multi-page, should evict
	phase = 1
	req, _ = http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	_, ok = et.cache.Load(url)
	assert.False(t, ok, "phase 1: multi-page should evict")

	// Phase 2: back to single-page, should re-cache
	phase = 2
	req, _ = http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	val, ok := et.cache.Load(url)
	assert.True(t, ok, "phase 2: single-page again should cache")
	assert.Equal(t, `"v3"`, val.(etagEntry).etag)
}

func TestETagTransport_TTLDrivenMultiPageDetection(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	requestCount := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		rec := httptest.NewRecorder()
		if r.Header.Get("If-None-Match") != "" {
			// 304 — do NOT refresh cachedAt
			rec.WriteHeader(304)
			return rec.Result(), nil
		}
		// Unconditional fetch after TTL — now multi-page
		rec.Header().Set("ETag", `"multi"`)
		rec.Header().Set("Link", `<https://api.github.com/repos/o/n/pulls?page=2>; rel="next"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	// Initial cache with artificially old cachedAt (just under TTL)
	et.cache.Store(url, etagEntry{
		etag:     `"old"`,
		cachedAt: time.Now().Add(-etagTTL + time.Minute),
	})

	// Request with valid cache — sends If-None-Match, gets 304
	req, _ := http.NewRequest("GET", url, nil)
	resp, _ := et.RoundTrip(req)
	assert.Equal(t, 304, resp.StatusCode)

	// Now expire the cache
	et.cache.Store(url, etagEntry{
		etag:     `"old"`,
		cachedAt: time.Now().Add(-etagTTL - time.Minute),
	})

	// Request with expired cache — no If-None-Match, gets 200 multi-page
	req, _ = http.NewRequest("GET", url, nil)
	resp, _ = et.RoundTrip(req)
	assert.Equal(t, 200, resp.StatusCode)

	// Cache should be evicted (multi-page detected)
	_, ok := et.cache.Load(url)
	assert.False(t, ok, "multi-page detection after TTL should evict")
}

func TestETagTransport_ExpiredEntryTreatedAsUncached(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"), "expired entry should not send If-None-Match")
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"fresh"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	// Store expired entry
	et.cache.Store(url, etagEntry{etag: `"old"`, cachedAt: time.Now().Add(-etagTTL - time.Minute)})

	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	val, _ := et.cache.Load(url)
	assert.Equal(t, `"fresh"`, val.(etagEntry).etag)
}

func TestIsNotModified(t *testing.T) {
	resp304 := &http.Response{StatusCode: 304}
	err304 := &gh.ErrorResponse{Response: resp304}
	assert.True(t, IsNotModified(err304))

	resp403 := &http.Response{StatusCode: 403}
	err403 := &gh.ErrorResponse{Response: resp403}
	assert.False(t, IsNotModified(err403))

	assert.False(t, IsNotModified(errors.New("random error")))

	errNilResp := &gh.ErrorResponse{Response: nil}
	assert.False(t, IsNotModified(errNilResp), "nil Response should not panic")
}
```

(Add `errors` import to the test file.)

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run "TestETagTransport|TestIsNotModified" -v`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/etag_transport.go internal/github/etag_transport_test.go
git commit -m "feat: add ETag transport with allowlist gate and TTL"
```

---

## Task 3: Syncer — onStatusChange Callback + headSHAs Cache

**Files:**
- Modify: `internal/github/sync.go`
- Test: `internal/github/sync_test.go`

- [ ] **Step 1: Write failing test for onStatusChange callback**

Add to `sync_test.go`:

```go
func TestSyncer_OnStatusChangeCallback(t *testing.T) {
	mock := &mockClient{
		openPRs:  []*gh.PullRequest{},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	_, err := d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})
	require.NoError(t, err)

	s := NewSyncer(mock, d, repos, time.Hour)

	var statuses []*SyncStatus
	var mu sync.Mutex
	s.SetOnStatusChange(func(status *SyncStatus) {
		mu.Lock()
		statuses = append(statuses, status)
		mu.Unlock()
	})

	s.RunOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(statuses), 2, "should fire at least started + completed")
	assert.True(t, statuses[0].Running, "first callback should be running=true")
	assert.False(t, statuses[len(statuses)-1].Running, "last callback should be running=false")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestSyncer_OnStatusChangeCallback -v`
Expected: FAIL — `SetOnStatusChange` not defined

- [ ] **Step 3: Add callback field and fire it at each status.Store site**

In `internal/github/sync.go`, add field to Syncer struct (after line 40):

```go
onStatusChange func(*SyncStatus)
```

Add setter method. **Must be called before `Start`** — the callback is not synchronized; `RunOnce` single-flight via `atomic.CompareAndSwap` guarantees only one `RunOnce` executes at a time, so `onStatusChange` and `headSHAs` are only accessed from within `RunOnce` and are safe without additional locking:

```go
// SetOnStatusChange registers a callback invoked on every status change.
// Must be called before Start — not safe to call concurrently with RunOnce.
func (s *Syncer) SetOnStatusChange(fn func(*SyncStatus)) {
	s.onStatusChange = fn
}
```

Add helper to fire callback:

```go
func (s *Syncer) updateStatus(st *SyncStatus) {
	s.status.Store(st)
	if s.onStatusChange != nil {
		s.onStatusChange(st)
	}
}
```

Replace every `s.status.Store(...)` call in `RunOnce` with `s.updateStatus(...)`. There are 3 sites:
1. Line ~102: `s.updateStatus(&SyncStatus{Running: true})`
2. Lines ~108-113 (in the loop): `s.updateStatus(&SyncStatus{Running: true, CurrentRepo: ..., Progress: ...})`
3. Lines ~131-135 (at end): `s.updateStatus(&SyncStatus{Running: false, LastRunAt: ..., LastError: ...})`

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestSyncer_OnStatusChangeCallback -v`
Expected: PASS

- [ ] **Step 5: Run all existing tests to check for regressions**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -v`
Expected: all tests PASS

- [ ] **Step 6: Add headSHAs cache and refreshCIForExistingPRs**

Add field to Syncer struct:

```go
headSHAs map[int64]map[int]string // repoID -> PR number -> head SHA
```

Initialize in `NewSyncer`:

```go
headSHAs: make(map[int64]map[int]string),
```

Refactor `refreshCIStatus` to accept `number int, headSHA string` instead of `*gh.PullRequest` — extract the two values at the call site in `syncOpenPR`.

Add `refreshCIForExistingPRs` helper:

```go
func (s *Syncer) refreshCIForExistingPRs(ctx context.Context, repo RepoRef, repoID int64) error {
	shas, ok := s.headSHAs[repoID]
	if !ok || len(shas) == 0 {
		return nil
	}
	for number, headSHA := range shas {
		if err := s.refreshCIStatus(ctx, repo, repoID, number, headSHA); err != nil {
			return err
		}
	}
	return nil
}
```

In `doSyncRepo`, wrap the `ListOpenPullRequests` call:

```go
ghPRs, err := s.client.ListOpenPullRequests(ctx, repo.Owner, repo.Name)
if IsNotModified(err) {
	if ciErr := s.refreshCIForExistingPRs(ctx, repo, repoID); ciErr != nil {
		slog.Error("refresh CI for existing PRs", "repo", repoName, "err", ciErr)
	}
	// Fall through to syncIssues below
} else if err != nil {
	return fmt.Errorf("list open PRs: %w", err)
} else {
	// Normal 200 path: existing PR processing logic...
	// After processing, replace headSHAs cache:
	shas := make(map[int]string, len(ghPRs))
	for _, pr := range ghPRs {
		if pr.GetHead() != nil && pr.GetHead().GetSHA() != "" {
			shas[pr.GetNumber()] = pr.GetHead().GetSHA()
		}
	}
	s.headSHAs[repoID] = shas
}
```

In `syncIssues`, wrap the `ListOpenIssues` call:

```go
ghIssues, err := s.client.ListOpenIssues(ctx, repo.Owner, repo.Name)
if IsNotModified(err) {
	return nil // issues unchanged, nothing to do
}
if err != nil {
	return fmt.Errorf("list open issues: %w", err)
}
```

Populate `headSHAs` on normal 200 path — replace entire inner map for this repoID.

- [ ] **Step 7: Write tests for IsNotModified handling**

```go
func TestSyncer_PRListNotModified_StillRefreshesCIAndIssues(t *testing.T) {
	ciCalled := atomic.Int32{}
	issueSyncCalled := atomic.Int32{}
	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 304},
			}
		},
		getCombinedStatusFunc: func(ctx context.Context, owner, name, ref string) (*gh.CombinedStatus, error) {
			ciCalled.Add(1)
			return &gh.CombinedStatus{State: gh.Ptr("success")}, nil
		},
		listCheckRunsFunc: func(ctx context.Context, owner, name, ref string) ([]*gh.CheckRun, error) {
			return nil, nil
		},
		listOpenIssuesFunc: func(ctx context.Context, owner, name string) ([]*gh.Issue, error) {
			issueSyncCalled.Add(1)
			return []*gh.Issue{}, nil
		},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	repoID, _ := d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})

	s := NewSyncer(mock, d, repos, time.Hour)
	// Pre-populate headSHAs so CI refresh has something to work with
	s.headSHAs[repoID] = map[int]string{1: "abc123"}

	s.RunOnce(context.Background())

	assert.Greater(t, ciCalled.Load(), int32(0), "CI refresh should run on 304")
	assert.Greater(t, issueSyncCalled.Load(), int32(0), "issue sync should still run")
}

func TestSyncer_IssueListNotModified_SkipsProcessing(t *testing.T) {
	mock := &mockClient{
		openPRs: []*gh.PullRequest{},
		listOpenIssuesFunc: func(ctx context.Context, owner, name string) ([]*gh.Issue, error) {
			return nil, &gh.ErrorResponse{
				Response: &http.Response{StatusCode: 304},
			}
		},
	}
	d := openTestDB(t)
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})

	s := NewSyncer(mock, d, []RepoRef{{Owner: "o", Name: "n"}}, time.Hour)
	s.RunOnce(context.Background())
	// No panic, no error — 304 path returns nil and skips issue processing
}
```

Note: The mock client needs `listOpenPRsFunc`, `listOpenIssuesFunc`, `getCombinedStatusFunc`, and `listCheckRunsFunc` optional func fields that override the default return values when non-nil (following the existing pattern in `sync_test.go`).

- [ ] **Step 8: Run all tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -v`
Expected: all tests PASS

- [ ] **Step 9: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: add sync status callback, headSHA cache, IsNotModified handling"
```

---

## Task 4: Syncer — Waitable Shutdown (lifecycleMu, wg, done, TriggerRun)

**Files:**
- Modify: `internal/github/sync.go`
- Test: `internal/github/sync_test.go`

This is the most complex Go task. The spec defines exact field layout and method signatures — follow them precisely.

- [ ] **Step 1: Write failing test for waitable Stop**

```go
func TestSyncer_StopWaitsForInflightRunOnce(t *testing.T) {
	entered := make(chan struct{}, 1)
	block := make(chan struct{})

	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			entered <- struct{}{}
			select {
			case <-block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, nil
		},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})

	s := NewSyncer(mock, d, repos, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Wait for RunOnce to enter, bounded so a regression fails the test
	// instead of hanging the suite.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for RunOnce")
	}

	// Stop should block until RunOnce returns
	stopDone := make(chan struct{})
	go func() {
		s.Stop()
		close(stopDone)
	}()

	// Verify Stop hasn't returned yet
	select {
	case <-stopDone:
		require.FailNow(t, "Stop returned before RunOnce finished")
	case <-time.After(100 * time.Millisecond):
	}

	// Unblock by canceling context (lifetimeCancel)
	cancel()

	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Stop did not return after context cancel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestSyncer_StopWaitsForInflightRunOnce -v`
Expected: FAIL (current Stop just closes stopCh, doesn't wait)

- [ ] **Step 3: Rewrite Syncer lifecycle fields**

Replace the Syncer struct fields per the spec. Add `lifecycleMu`, `started`, `stopped`, `stopOnce`, `done`, `stopCh`, `wg`, `lifetimeCtx`, `lifetimeCancel`. Update `NewSyncer` to allocate these. Rewrite `Start`, `Stop`, add `TriggerRun`. Follow the exact code in the spec's `sync.go` section.

Key changes:
- `NewSyncer`: create `done`, `stopCh`, `lifetimeCtx`/`lifetimeCancel`
- `Start`: mutex-guarded, `wg.Add(2)` before releasing lock, spawn linker + ticker goroutines
- `Stop`: `stopOnce.Do` wrapping teardown, post-once `<-s.done` + `s.wg.Wait()`
- `TriggerRun`: mutex-guarded, `wg.Add(1)` before releasing lock, `go RunOnce(s.lifetimeCtx)`

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestSyncer_StopWaitsForInflightRunOnce -v`
Expected: PASS

- [ ] **Step 5: Write and run additional lifecycle tests**

```go
func TestSyncer_StopIdempotent(t *testing.T) {
	d := openTestDB(t)
	s := NewSyncer(&mockClient{openIssues: []*gh.Issue{}}, d, nil, time.Hour)
	s.Stop()
	s.Stop() // must not panic on double-close
}

func TestSyncer_StartAfterStopIsNoop(t *testing.T) {
	callCount := atomic.Int32{}
	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			callCount.Add(1)
			return nil, nil
		},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	s := NewSyncer(mock, d, []RepoRef{{Owner: "o", Name: "n"}}, time.Hour)
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})
	s.Stop()
	ctx := context.Background()
	s.Start(ctx)
	// Give any stray RunOnce launch room to land, then assert none did.
	// require.Never keeps the probe running without pinning to a single
	// post-hoc sleep duration.
	require.Never(t, func() bool {
		return callCount.Load() > 0
	}, 200*time.Millisecond, 10*time.Millisecond,
		"Start after Stop should not launch RunOnce")
}

func TestSyncer_DoubleStartIsNoop(t *testing.T) {
	callCount := atomic.Int32{}
	entered := make(chan struct{}, 1)
	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			if callCount.Add(1) == 1 {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
			return nil, nil
		},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})
	s := NewSyncer(mock, d, repos, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	s.Start(ctx) // second call is no-op
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for RunOnce")
	}
	cancel()
	s.Stop()
	// With hour-long ticker, only one RunOnce should fire
	assert.Equal(t, int32(1), callCount.Load())
}

func TestSyncer_TriggerRunWithoutStart(t *testing.T) {
	entered := make(chan struct{}, 1)
	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})
	s := NewSyncer(mock, d, repos, time.Hour)
	s.TriggerRun()
	<-entered // proves RunOnce entered

	stopDone := make(chan struct{})
	go func() {
		s.Stop() // must not deadlock (done closed by Stop when !wasStarted)
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Stop deadlocked without Start")
	}
}

func TestSyncer_TriggerRunAfterStop(t *testing.T) {
	callCount := atomic.Int32{}
	entered := make(chan struct{}, 1)
	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			if callCount.Add(1) == 1 {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
			return nil, nil
		},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})
	s := NewSyncer(mock, d, repos, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	// Wait for the initial RunOnce to actually enter, with a timeout
	// so a regression that prevents the goroutine from starting fails
	// the test instead of hanging the whole suite.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for initial RunOnce")
	}
	cancel()
	s.Stop() // blocks until in-flight RunOnce returns
	before := callCount.Load()
	s.TriggerRun() // should be no-op (stopped=true)
	// Probe for a window long enough to catch any stray launch.
	require.Never(t, func() bool {
		return callCount.Load() != before
	}, 200*time.Millisecond, 10*time.Millisecond,
		"TriggerRun after Stop should not fire")
}

func TestSyncer_StopShutdownCompleteIdempotent(t *testing.T) {
	entered := make(chan struct{}, 1)
	mock := &mockClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		openIssues: []*gh.Issue{},
	}
	d := openTestDB(t)
	repos := []RepoRef{{Owner: "o", Name: "n"}}
	d.UpsertRepo(context.Background(), "o", "n", db.RepoMergeSettings{})
	s := NewSyncer(mock, d, repos, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for RunOnce")
	}

	// Two concurrent Stop calls — both must wait for shutdown
	stopA := make(chan struct{})
	stopB := make(chan struct{})
	go func() { s.Stop(); close(stopA) }()
	go func() { s.Stop(); close(stopB) }()

	// Neither should return yet (RunOnce blocked)
	select {
	case <-stopA:
		require.FailNow(t, "Stop A returned early")
	case <-stopB:
		require.FailNow(t, "Stop B returned early")
	case <-time.After(100 * time.Millisecond):
	}

	cancel() // unblock RunOnce via lifetimeCancel

	// Both must return
	select {
	case <-stopA:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Stop A did not return")
	}
	select {
	case <-stopB:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Stop B did not return")
	}
}
```

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -run TestSyncer -v -race`
Expected: all tests PASS, no race detected

- [ ] **Step 6: Run full test suite**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./... -race`
Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: make Syncer.Stop waitable, add TriggerRun wrapper"
```

---

## Task 5: Wire ETag Transport into Client

**Files:**
- Modify: `internal/github/client.go`

- [ ] **Step 1: Wrap transport in NewClient**

In `internal/github/client.go`, modify `NewClient` (line 33-37):

```go
func NewClient(token string) Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	tc.Transport = &etagTransport{base: tc.Transport}
	return &liveClient{gh: gh.NewClient(tc)}
}
```

- [ ] **Step 2: Run existing tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/github/ -v`
Expected: all tests PASS (etagTransport is transparent for tests using mockClient)

- [ ] **Step 3: Commit**

```bash
git add internal/github/client.go
git commit -m "feat: wrap GitHub HTTP client with ETag transport"
```

---

## Task 6: Server Lifecycle — Listen/Serve/Shutdown

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go` (if exists, otherwise create)

- [ ] **Step 1: Write failing tests for new lifecycle methods**

```go
// internal/server/server_test.go (add or create)
func TestServer_ListenBindsPort(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	err := s.Listen("127.0.0.1:0")
	require.NoError(t, err)
	defer s.Shutdown(context.Background())

	addr := s.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	conn.Close()
}

func TestServer_ShutdownBeforeServe(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	err := s.Listen("127.0.0.1:0")
	require.NoError(t, err)

	err = s.Shutdown(context.Background())
	assert.NoError(t, err)

	// Serve after Shutdown should return ErrServerClosed
	err = s.Serve()
	assert.ErrorIs(t, err, http.ErrServerClosed)
}

func TestServer_ListenBindError(t *testing.T) {
	// Bind a port first
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	s := New(openTestDB(t), nil, nil, nil, "/")
	err = s.Listen(ln.Addr().String())
	assert.Error(t, err) // address already in use
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run "TestServer_Listen|TestServer_Shutdown" -v`
Expected: compilation errors (methods not defined)

- [ ] **Step 3: Add hub, httpSrv, listener fields and lifecycle methods**

In `internal/server/server.go`:

Add fields to Server struct:
```go
hub      *EventHub
httpSrv  *http.Server
listener net.Listener
```

Add to `newServer`:
```go
s.hub = NewEventHub()
```

Add public accessor (needed by `cmd/middleman/app.go` to wire callback + prime):
```go
func (s *Server) Hub() *EventHub {
	return s.hub
}
```

Add `Listen` method:
```go
func (s *Server) Listen(addr string) error {
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln
	return nil
}
```

Add `Serve` method:
```go
func (s *Server) Serve() error {
	return s.httpSrv.Serve(s.listener)
}
```

Add `Shutdown` method:
```go
func (s *Server) Shutdown(ctx context.Context) error {
	s.hub.Close()
	if s.httpSrv == nil {
		return nil
	}
	err := s.httpSrv.Shutdown(ctx)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	return err
}
```

Remove old `ListenAndServe` method (lines 189-198).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run "TestServer_Listen|TestServer_Shutdown" -v`
Expected: all tests PASS

- [ ] **Step 5: Write pre-Serve shutdown regression test**

```go
func TestServer_ShutdownBeforeServe_ReleasesPort(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	err := s.Listen("127.0.0.1:0")
	require.NoError(t, err)
	boundAddr := s.listener.Addr().String()

	err = s.Shutdown(context.Background())
	assert.NoError(t, err)

	// Port should be released — a fresh listen must succeed
	ln, err := net.Listen("tcp", boundAddr)
	if err != nil {
		// TIME_WAIT on some systems — fall back to dial check
		conn, dialErr := net.DialTimeout("tcp", boundAddr, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			require.FailNow(t, "port still held after Shutdown")
		}
	} else {
		ln.Close()
	}

	// Subsequent Serve should return ErrServerClosed
	err = s.Serve()
	assert.ErrorIs(t, err, http.ErrServerClosed)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run "TestServer_" -v`
Expected: all PASS. Note: `cmd/middleman` tests will fail (main.go still calls ListenAndServe) — this is expected and fixed in Task 8

- [ ] **Step 7: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go internal/server/event_hub.go
git commit -m "feat: split Server into Listen/Serve/Shutdown, add EventHub field"
```

---

## Task 7: SSE Handler

**Files:**
- Modify: `internal/server/server.go` (register handler)
- Create or modify: handler code in server.go or a new file
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write failing test for SSE endpoint**

```go
func TestSSE_ReturnsEventStream(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
}

func TestSSE_ReceivesBroadcastEvent(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	s.hub.Broadcast(Event{Type: "data_changed", Data: struct{}{}})

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		}
		if line == "" && eventType != "" {
			break
		}
	}
	assert.Equal(t, "data_changed", eventType)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestSSE -v`
Expected: FAIL (404 — no handler registered)

- [ ] **Step 3: Implement SSE handler**

Register in `newServer` (after line 83):
```go
mux.HandleFunc("GET /api/v1/events", s.handleSSE)
```

Implement `handleSSE`:
```go
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	rc := http.NewResponseController(w)
	// Clear server-wide WriteTimeout for this SSE response. If the
	// ResponseWriter does not support deadline control, the server's
	// WriteTimeout (30s) would silently kill this long-lived response
	// mid-stream. Close the connection instead of proceeding blind.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Error("sse: clear write deadline", "err", err)
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	ch, done := s.hub.Subscribe(r.Context())
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		// Non-blocking done check
		select {
		case <-done:
			return
		default:
		}

		select {
		case <-done:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				slog.Error("sse: marshal event", "type", ev.Type, "err", err)
				continue
			}
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				slog.Error("sse: set write deadline", "err", err)
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				slog.Error("sse: clear write deadline", "err", err)
				return
			}
		case <-ticker.C:
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				slog.Error("sse: set write deadline", "err", err)
				return
			}
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				slog.Error("sse: clear write deadline", "err", err)
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestSSE -v`
Expected: all tests PASS

- [ ] **Step 5: Write and run initial sync_status cache test**

```go
func TestSSE_InitialSyncStatusFromCache(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	s.hub.Broadcast(Event{Type: "sync_status", Data: map[string]bool{"running": false}})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		}
		if line == "" && eventType != "" {
			break
		}
	}
	assert.Equal(t, "sync_status", eventType)
}
```

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestSSE -v`
Expected: PASS

- [ ] **Step 6: Write SSE handler hub-close and slow-consumer disconnect tests**

```go
func TestSSE_ExitsCleanlyOnHubClose(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Close the hub — handler should exit
	s.hub.Close()

	// Read until EOF — should not see zero-value frames
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "event: \ndata:")
}

func TestSSE_MarshalFailureContinuesServing(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	// Prime hub so first subscribe gets a sync_status — we read it
	// as proof the handler has subscribed before we broadcast test events.
	s.hub.Broadcast(Event{Type: "sync_status", Data: map[string]bool{"running": false}})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Single reader goroutine parses SSE frames and sends event types
	// over a channel. Avoids per-read goroutine leaks.
	events := make(chan string, 10)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(resp.Body)
		var evType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				evType = strings.TrimPrefix(line, "event: ")
			}
			if line == "" && evType != "" {
				events <- evType
				evType = ""
			}
		}
	}()

	// Read initial cached sync_status to confirm subscription is live
	select {
	case ev := <-events:
		assert.Equal(t, "sync_status", ev)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for initial sync_status")
	}

	// Now safe to broadcast — handler is subscribed
	s.hub.Broadcast(Event{Type: "bad", Data: make(chan int)})
	s.hub.Broadcast(Event{Type: "data_changed", Data: struct{}{}})

	select {
	case ev := <-events:
		assert.Equal(t, "data_changed", ev, "valid event should arrive after marshal failure")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for data_changed after marshal failure")
	}

	// Close body to unblock reader goroutine, then drain channel.
	// scanner.Err() after forced close returns a read-on-closed-body
	// error — that is expected cleanup, not a test failure. Stream
	// health is validated by successful receipt of both events above.
	resp.Body.Close()
	for range events {
	}
}

func TestSSE_SlowConsumerDisconnect(t *testing.T) {
	s := New(openTestDB(t), nil, nil, nil, "/")
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Overrun the buffer without reading — 17 broadcasts (buffer=16)
	for i := 0; i < 17; i++ {
		s.hub.Broadcast(Event{Type: "data_changed", Data: i})
	}

	// The handler should close the connection
	body := make([]byte, 1)
	_, err = resp.Body.Read(body)
	// Eventually hits EOF or connection reset
	// (may read buffered data first)
}
```

- [ ] **Step 7: Run all SSE tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./internal/server/ -run TestSSE -v`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: add SSE endpoint with two-level select and keepalive"
```

---

## Task 8: App Struct, Bootstrap, Run + main.go Refactor

**Files:**
- Create: `cmd/middleman/app.go`
- Modify: `cmd/middleman/main.go`
- Test: `cmd/middleman/app_test.go`

- [ ] **Step 1: Write failing test for Bootstrap**

```go
// cmd/middleman/app_test.go
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	ghclient "github.com/wesm/middleman/internal/github"
)

func TestBootstrap_CreatesAppWithSyncerAndServer(t *testing.T) {
	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "o", Name: "n"}},
	}
	mock := &mockBootstrapClient{}
	app, err := Bootstrap(cfg, "", mock)
	require.NoError(t, err)
	assert.NotNil(t, app.Server)
	assert.NotNil(t, app.Syncer)
	assert.NotNil(t, app.DB)
	defer app.DB.Close()
}

type mockBootstrapClient struct{}

// Implement ghclient.Client interface methods as no-ops...
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./cmd/middleman/ -run TestBootstrap -v`
Expected: compilation error (Bootstrap not defined)

- [ ] **Step 3: Implement Bootstrap and Run in app.go**

```go
// cmd/middleman/app.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/web"
)

// App holds the top-level components created by Bootstrap.
type App struct {
	Server *server.Server
	Syncer *ghclient.Syncer
	DB     *db.DB
}

// Bootstrap creates the syncer, wires the status callback, constructs
// the server, and primes the hub — without starting the syncer or
// serving HTTP.
func Bootstrap(cfg *config.Config, configPath string, ghClient ghclient.Client) (*App, error) {
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	repos := make([]ghclient.RepoRef, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = ghclient.RepoRef{Owner: r.Owner, Name: r.Name}
	}

	syncer := ghclient.NewSyncer(ghClient, database, repos, cfg.SyncDuration())

	assets, err := web.Assets()
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.NewWithConfig(database, ghClient, syncer, assets, cfg, configPath)

	// Wire status callback and prime the hub
	syncer.SetOnStatusChange(func(status *ghclient.SyncStatus) {
		srv.Hub().Broadcast(server.Event{Type: "sync_status", Data: status})
		if !status.Running {
			srv.Hub().Broadcast(server.Event{Type: "data_changed", Data: struct{}{}})
		}
	})
	srv.Hub().Broadcast(server.Event{
		Type: "sync_status",
		Data: syncer.Status(),
	})

	return &App{Server: srv, Syncer: syncer, DB: database}, nil
}

// Run is the only entry point main.go may use. It calls Bootstrap,
// starts the syncer, binds the socket, serves HTTP, and handles
// graceful shutdown.
func Run(ctx context.Context, cfg *config.Config, configPath string, ghClient ghclient.Client, addr string) error {
	app, err := Bootstrap(cfg, configPath, ghClient)
	if err != nil {
		return err
	}
	defer app.DB.Close()

	syncCtx, cancelSync := context.WithCancel(ctx)
	app.Syncer.Start(syncCtx)
	defer func() {
		cancelSync()
		app.Syncer.Stop()
	}()

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
			<-errCh
			return fmt.Errorf("server shutdown: %w", err)
		}
		if err := <-errCh; err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = app.Server.Shutdown(shutdownCtx)
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	}
}
```

This requires adding a `Hub()` accessor to `Server`:

```go
// In internal/server/server.go
func (s *Server) Hub() *EventHub {
	return s.hub
}
```

- [ ] **Step 4: Update main.go to use Run**

Replace the `run` function in `cmd/middleman/main.go` with:

```go
func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	token := cfg.GitHubToken()
	if token == "" {
		return fmt.Errorf("GitHub token not set: env var %q is empty", cfg.GitHubTokenEnv)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory %s: %w", cfg.DataDir, err)
	}

	ghClient := ghclient.NewClient(token)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := cfg.ListenAddr()
	slog.Info(fmt.Sprintf("starting server at http://%s", addr))

	return Run(ctx, cfg, configPath, ghClient, addr)
}
```

Remove unused imports (`db`, `server`, `web`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./... -v`
Expected: all tests PASS

- [ ] **Step 6: Write Run bind-error, shutdown, and serve-error tests**

```go
func TestRun_BindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "o", Name: "n"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = Run(ctx, cfg, "", &mockBootstrapClient{}, ln.Addr().String())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "listen:")
}

func TestRun_ShutdownHappyPath(t *testing.T) {
	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "o", Name: "n"}},
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Bind a probe listener on a concrete port, then close it so Run can
	// take it. We use the probe address to wait until Run's listener is
	// actually accepting connections before cancelling — no sleep needed.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := probe.Addr().String()
	require.NoError(t, probe.Close())

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, "", &mockBootstrapClient{}, addr)
	}()

	// Poll until the server is dialable, which proves Serve is running.
	require.Eventually(t, func() bool {
		c, dErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dErr != nil {
			return false
		}
		c.Close()
		return true
	}, 5*time.Second, 10*time.Millisecond, "server never started listening")

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "Run did not return after cancel")
	}
}

func TestRun_BindErrorDeferredCleanup(t *testing.T) {
	// Pre-bind the target port, then call Run.
	// Run returns the wrapped bind error.
	// This verifies the deferred chain (cancelSync, Stop, DB.Close)
	// executes cleanly without panicking or leaking goroutines.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "o", Name: "n"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = Run(ctx, cfg, "", &mockBootstrapClient{}, ln.Addr().String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listen:")
	// No goroutine leak — deferred chain ran cleanly
}

func TestBootstrap_ShutdownOrderingSyncInflight(t *testing.T) {
	// Deterministic test: controls sequencing via Bootstrap directly.
	// Mock blocks in ListOpenPullRequests until context is canceled.
	runOnceEntered := make(chan struct{}, 1)
	mock := &mockBootstrapClient{
		listOpenPRsFunc: func(ctx context.Context, owner, name string) ([]*gh.PullRequest, error) {
			select {
			case runOnceEntered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	cfg := &config.Config{
		Repos: []config.Repo{{Owner: "o", Name: "n"}},
	}
	app, err := Bootstrap(cfg, "", mock)
	require.NoError(t, err)
	defer app.DB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	app.Syncer.Start(ctx)

	// Wait for RunOnce to enter the blocked list call
	<-runOnceEntered

	// Pre-bind the target port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Listen fails with "address already in use"
	listenErr := app.Server.Listen(ln.Addr().String())
	assert.Error(t, listenErr)
	ln.Close()

	// Cancel ctx and Stop — Stop must block until RunOnce returns
	cancel()
	stopDone := make(chan struct{})
	go func() {
		app.Syncer.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Stop did not return — RunOnce still blocked")
	}
}
```

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./cmd/middleman/ -run "TestRun|TestBootstrap" -v`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/middleman/app.go cmd/middleman/app_test.go cmd/middleman/main.go internal/server/server.go
git commit -m "feat: extract App/Bootstrap/Run, refactor main.go to use Run"
```

---

## Task 9: Replace bare RunOnce calls with TriggerRun

**Files:**
- Modify: `internal/server/huma_routes.go` (line 775)
- Modify: `internal/server/settings_handlers.go` (line 147)

- [ ] **Step 1: Replace in huma_routes.go**

Change line 775 from:
```go
go s.syncer.RunOnce(context.WithoutCancel(ctx))
```
to:
```go
s.syncer.TriggerRun()
```

- [ ] **Step 2: Replace in settings_handlers.go**

Change line 147 from:
```go
go s.syncer.RunOnce(context.WithoutCancel(r.Context()))
```
to:
```go
s.syncer.TriggerRun()
```

- [ ] **Step 3: Run tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./... -v -race`
Expected: all tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/huma_routes.go internal/server/settings_handlers.go
git commit -m "refactor: replace bare RunOnce calls with TriggerRun"
```

---

## Task 10: AST Guardrail — Shared Helpers

**Files:**
- Create: `internal/asttest/forbidden.go`

- [ ] **Step 1: Write the shared forbidden-set helpers**

```go
// internal/asttest/forbidden.go
package asttest

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

// SyncerForbiddenSet returns a map of *types.Func for methods on *Syncer
// that must not be called directly outside approved locations.
// Methods: RunOnce, Start, Stop.
func SyncerForbiddenSet(t *testing.T, syncerPkg *types.Package) map[*types.Func]bool {
	t.Helper()
	syncerObj := syncerPkg.Scope().Lookup("Syncer")
	require.NotNil(t, syncerObj, "Syncer type not found in package")
	syncerPtr := types.NewPointer(syncerObj.Type())
	mset := types.NewMethodSet(syncerPtr)

	forbidden := map[*types.Func]bool{}
	for _, name := range []string{"RunOnce", "Start", "Stop"} {
		sel := mset.Lookup(nil, name)
		require.NotNilf(t, sel, "method %s not found on *Syncer", name)
		forbidden[sel.Obj().(*types.Func)] = true
	}
	return forbidden
}

// ServerForbiddenSet returns a map of *types.Func for methods on *Server
// that must not be called directly outside approved locations.
// Methods: Listen, Serve, Shutdown.
func ServerForbiddenSet(t *testing.T, serverPkg *types.Package) map[*types.Func]bool {
	t.Helper()
	serverObj := serverPkg.Scope().Lookup("Server")
	require.NotNil(t, serverObj, "Server type not found in package")
	serverPtr := types.NewPointer(serverObj.Type())
	mset := types.NewMethodSet(serverPtr)

	forbidden := map[*types.Func]bool{}
	for _, name := range []string{"Listen", "Serve", "Shutdown"} {
		sel := mset.Lookup(nil, name)
		require.NotNilf(t, sel, "method %s not found on *Server", name)
		forbidden[sel.Obj().(*types.Func)] = true
	}
	return forbidden
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go build ./internal/asttest/`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/asttest/forbidden.go
git commit -m "feat: add shared AST forbidden-set helpers for lifecycle guardrails"
```

---

## Task 11: AST Guardrail Tests

**Files:**
- Create: `cmd/middleman/main_ast_test.go`
- Create: `internal/server/server_ast_test.go`
- Create: `internal/github/sync_ast_test.go`

- [ ] **Step 1: Write main_ast_test.go**

Implement `TestOnlyAppRunStartsServerAndSyncer` per the spec: load `cmd/middleman` with `go/packages`, find the unique `Run` FuncDecl, build the forbidden set from both `SyncerForbiddenSet` and `ServerForbiddenSet`, walk every `*ast.SelectorExpr` with a by-value visitor, fail if any selector resolves to a forbidden method outside `Run`.

- [ ] **Step 2: Write server_ast_test.go**

Implement with empty exemption set — no FuncDecl in `internal/server` may call RunOnce.

- [ ] **Step 3: Write sync_ast_test.go**

Implement with `{*Syncer.Start, *Syncer.TriggerRun}` as exempted FuncDecls.

- [ ] **Step 4: Run all AST tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./cmd/middleman/ -run TestOnlyApp -v && go test ./internal/server/ -run TestAST -v && go test ./internal/github/ -run TestAST -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/middleman/main_ast_test.go internal/server/server_ast_test.go internal/github/sync_ast_test.go
git commit -m "feat: add AST guardrail tests for lifecycle method isolation"
```

---

## Task 12: Frontend — Polling Gates in Stores

**Files:**
- Modify: `frontend/src/lib/stores/pulls.svelte.ts`
- Modify: `frontend/src/lib/stores/issues.svelte.ts`
- Modify: `frontend/src/lib/stores/activity.svelte.ts`
- Modify: `frontend/src/lib/stores/sync.svelte.ts`
- Modify: `frontend/src/lib/stores/detail.svelte.ts`

- [ ] **Step 1: Add polling gate to pulls store**

In `pulls.svelte.ts`, add:

```typescript
let pollingEnabled = true;
let listPollingActive = false;
let listPollingOverrides: Record<string, string> | undefined;
let listPollHandle: ReturnType<typeof setInterval> | null = null;
let listVersion = 0;

export function startListPolling(overrides?: Record<string, string>): void {
  stopListPolling();
  listPollingActive = true;
  listPollingOverrides = overrides;
  if (!pollingEnabled) return;
  listPollHandle = setInterval(() => void loadPulls(overrides), 15_000);
}

export function stopListPolling(): void {
  listPollingActive = false;
  listPollingOverrides = undefined;
  if (listPollHandle !== null) {
    clearInterval(listPollHandle);
    listPollHandle = null;
  }
}

export function enablePolling(): void {
  pollingEnabled = true;
  if (listPollingActive && listPollHandle === null) {
    listPollHandle = setInterval(() => void loadPulls(listPollingOverrides), 15_000);
  }
}

export function disablePolling(): void {
  pollingEnabled = false;
  if (listPollHandle !== null) {
    clearInterval(listPollHandle);
    listPollHandle = null;
  }
}
```

Add stale-response guard to `loadPulls`:
```typescript
export async function loadPulls(overrides?: Record<string, string>): Promise<void> {
  const version = ++listVersion;
  // ... existing fetch logic ...
  // After await, check: if (version !== listVersion) return;
  // ... apply result ...
}
```

- [ ] **Step 2: Add polling gate to issues store**

In `issues.svelte.ts`, add module-level state:

```typescript
let pollingEnabled = true;
let listPollingActive = false;
let listPollHandle: ReturnType<typeof setInterval> | null = null;
let listVersion = 0;
let detailVersion = 0;
```

Add `startListPolling`/`stopListPolling` (same pattern as pulls — 15s interval calling `loadIssues()`). Add `enablePolling`/`disablePolling` following the polling gate rule: `disablePolling` clears intervals but preserves `listPollingActive` and `issueDetailActive` flags; `enablePolling` recreates timers if flags are true. Add `refreshFromSSE(owner, name, number)` that calls the existing issue detail load. Add stale-response guard (`listVersion` check) to `loadIssues` and `detailVersion` check to `loadIssueDetail`.

- [ ] **Step 3: Add polling gate to sync store**

In `sync.svelte.ts`, add:

```typescript
let pollingEnabled = true;
let syncPollingActive = false;
let syncVersion = 0;

function applySyncState(newStatus: SyncStatus): void {
  const isRunning = newStatus.running ?? false;
  status = newStatus;
  if (wasRunning && !isRunning && onSyncComplete) {
    const cb = onSyncComplete;
    onSyncComplete = null;
    cb();
  }
  wasRunning = isRunning;
  adjustPollingSpeed(isRunning);
}

export function updateSyncFromSSE(newStatus: SyncStatus): void {
  syncVersion++;
  applySyncState(newStatus);
}
```

Modify `refreshSyncStatus` to increment `syncVersion` at call time, capture version, check after `await`. Modify `triggerSync` to increment `syncVersion` and call `applySyncState({running: true})`. Modify `startPolling` to check `pollingEnabled` before creating timer, set `syncPollingActive` flag. Add `enablePolling`/`disablePolling` — `disablePolling` preserves `currentIntervalMs`, `enablePolling` recreates at preserved interval.

- [ ] **Step 4: Add polling gate to activity store**

In `activity.svelte.ts`, add:

```typescript
let pollingEnabled = true;
let activityPollingActive = false;
let listVersion = 0;
```

Add `enablePolling`/`disablePolling` following the gate rule. Add `refreshFromSSE()` that calls `loadActivity()` (full refresh, not incremental `pollNewItems`). Modify `startActivityPolling` to set `activityPollingActive` and check `pollingEnabled`. Add stale-response guard to `loadActivity`.

- [ ] **Step 5: Add polling gate to detail store**

In `detail.svelte.ts`, add:

```typescript
let pollingEnabled = true;
let detailTarget: { owner: string; name: string; number: number } | null = null;
let detailVersion = 0;
```

Modify `startDetailPolling` to store `detailTarget`. `stopDetailPolling` clears both interval AND `detailTarget`. `disablePolling` clears interval but preserves `detailTarget`. `enablePolling` recreates timer for stored target if present. Add `refreshFromSSE(owner, name, number)` that calls existing `refreshDetail()`. Add stale-response guard to `loadDetail` and `refreshDetail`.

- [ ] **Step 6: Run frontend type check**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river/frontend && bun run check`
Expected: no type errors

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/stores/pulls.svelte.ts frontend/src/lib/stores/issues.svelte.ts frontend/src/lib/stores/activity.svelte.ts frontend/src/lib/stores/sync.svelte.ts frontend/src/lib/stores/detail.svelte.ts
git commit -m "feat: add polling gates and stale-response guards to all stores"
```

---

## Task 13: Frontend — Move Component Polling into Stores

**Files:**
- Modify: `frontend/src/lib/components/sidebar/PullList.svelte`
- Modify: `frontend/src/lib/components/sidebar/IssueList.svelte`
- Modify: `frontend/src/lib/components/kanban/KanbanBoard.svelte`

- [ ] **Step 1: Update PullList.svelte**

Remove the `setInterval` from the `$effect` block. Replace with:

```svelte
<script>
  import { loadPulls, startListPolling, stopListPolling } from "$lib/stores/pulls.svelte.js";
  import { onDestroy } from "svelte";

  $effect(() => {
    void loadPulls();
    startListPolling();
    // ... keep existing onNextSyncComplete logic ...
    return () => {
      stopListPolling();
    };
  });
</script>
```

- [ ] **Step 2: Update IssueList.svelte**

Remove the `setInterval` from the `$effect` block. Replace with:

```svelte
<script>
  import { loadIssues, startListPolling, stopListPolling } from "$lib/stores/issues.svelte.js";

  $effect(() => {
    void loadIssues();
    startListPolling();
    return () => {
      stopListPolling();
    };
  });
</script>
```

- [ ] **Step 3: Update KanbanBoard.svelte**

Remove `setInterval` from `onMount`. Replace with:

```svelte
onMount(() => {
  void loadPulls({ state: "open" });
  startListPolling({ state: "open" });
});

onDestroy(() => {
  stopListPolling();
  void loadPulls();
});
```

- [ ] **Step 4: Run frontend type check**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river/frontend && bun run check`
Expected: no type errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/components/sidebar/PullList.svelte frontend/src/lib/components/sidebar/IssueList.svelte frontend/src/lib/components/kanban/KanbanBoard.svelte
git commit -m "refactor: move component polling into stores for SSE control"
```

---

## Task 14: Frontend — SSE Events Store

**Files:**
- Create: `frontend/src/lib/stores/events.svelte.ts`
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Create events store**

```typescript
// frontend/src/lib/stores/events.svelte.ts
import { getBasePath } from "./router.svelte.js";
import { getPage, getView, getSelectedPR } from "./router.svelte.js";
import { updateSyncFromSSE, enablePolling as enableSyncPolling, disablePolling as disableSyncPolling } from "./sync.svelte.js";
import { loadPulls, enablePolling as enablePullsPolling, disablePolling as disablePullsPolling } from "./pulls.svelte.js";
import { loadIssues, enablePolling as enableIssuesPolling, disablePolling as disableIssuesPolling } from "./issues.svelte.js";
import { enablePolling as enableActivityPolling, disablePolling as disableActivityPolling, refreshFromSSE as refreshActivity } from "./activity.svelte.js";
import { enablePolling as enableDetailPolling, disablePolling as disableDetailPolling, refreshFromSSE as refreshDetailSSE } from "./detail.svelte.js";
import type { SyncStatus } from "../api/types.js";

let eventSource: EventSource | null = null;
let connected = $state(false);
let drawerRefreshCallback: (() => void) | null = null;

export function isSSEConnected(): boolean {
  return connected;
}

export function registerDrawerRefresh(cb: () => void): void {
  drawerRefreshCallback = cb;
}

export function unregisterDrawerRefresh(): void {
  drawerRefreshCallback = null;
}

function disableAllPolling(): void {
  disableSyncPolling();
  disablePullsPolling();
  disableIssuesPolling();
  disableActivityPolling();
  disableDetailPolling();
}

function enableAllPolling(): void {
  enableSyncPolling();
  enablePullsPolling();
  enableIssuesPolling();
  enableActivityPolling();
  enableDetailPolling();
}

function fullRefresh(): void {
  void loadPulls();
  void loadIssues();

  const page = getPage();
  const view = getView();

  if (page === "pulls") {
    if (view === "board") {
      void loadPulls({ state: "open" });
    }
    const selected = getSelectedPR();
    if (selected) {
      void refreshDetailSSE(selected.owner, selected.name, selected.number);
    }
  }
  if (page === "activity") {
    void refreshActivity();
    if (drawerRefreshCallback) drawerRefreshCallback();
  }
  // Issues detail refresh handled via issues store
}

export function connect(): void {
  if (eventSource) return;

  const base = getBasePath().replace(/\/$/, "");
  eventSource = new EventSource(`${base}/api/v1/events`);

  eventSource.addEventListener("sync_status", (e: MessageEvent) => {
    const status: SyncStatus = JSON.parse(e.data);
    updateSyncFromSSE(status);
  });

  eventSource.addEventListener("data_changed", () => {
    fullRefresh();
  });

  eventSource.addEventListener("open", () => {
    connected = true;
    disableAllPolling();
    fullRefresh();
  });

  eventSource.addEventListener("error", () => {
    connected = false;
    enableAllPolling();
  });
}

export function disconnect(): void {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  connected = false;
}
```

- [ ] **Step 2: Wire into App.svelte**

In `App.svelte`, add:
```svelte
<script>
  import { onMount, onDestroy } from "svelte";
  import { connect, disconnect } from "$lib/stores/events.svelte.js";

  onMount(() => {
    connect();
  });

  onDestroy(() => {
    disconnect();
  });
</script>
```

- [ ] **Step 3: Run frontend type check**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river/frontend && bun run check`
Expected: no type errors

- [ ] **Step 4: Run frontend tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river/frontend && bun test`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/stores/events.svelte.ts frontend/src/App.svelte
git commit -m "feat: add SSE events store with polling toggle and view-aware refresh"
```

---

## Task 15: Frontend Tests

**Files:**
- Create or modify: `frontend/src/lib/stores/*.test.ts`

- [ ] **Step 1: Write polling gate tests**

Test each store's enable/disable/start/stop lifecycle:
- `disablePolling()` clears timers, `enablePolling()` restarts them
- `stopListPolling()` then `enablePolling()` does NOT revive timer
- Mount while SSE connected: records state but creates no timer
- Sync interval preservation through mount

- [ ] **Step 2: Write stale-response guard tests**

For `sync.svelte.ts`:
- Overlapping polls: start two refreshSyncStatus, resolve in reverse order, stale is discarded
- Stale poll vs triggerSync: poll resolves after optimistic update, discarded
- Stale poll vs SSE update: both idle→running and running→idle directions

- [ ] **Step 3: Write events store tests**

- `connect()` creates EventSource with correct URL
- `disconnect()` closes EventSource
- View-aware refresh dispatches correctly per page
- Global refresh always fires both `loadPulls` and `loadIssues`

- [ ] **Step 4: Run all frontend tests**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river/frontend && bun test`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/stores/
git commit -m "test: add polling gate, stale-response, and SSE event store tests"
```

---

## Task 16: Integration Smoke Test

**Files:**
- Existing test files

- [ ] **Step 1: Run full Go test suite with race detector**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && go test ./... -race -count=1`
Expected: all tests PASS, no race conditions

- [ ] **Step 2: Run frontend build**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && make frontend`
Expected: build succeeds

- [ ] **Step 3: Run full build**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && make build`
Expected: binary builds successfully

- [ ] **Step 4: Run lint**

Run: `cd /home/cloud/src/github.com/middleman/.claude/worktrees/sprightly-yawning-river && make lint`
Expected: no warnings

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "chore: fix any remaining lint/build issues"
```
