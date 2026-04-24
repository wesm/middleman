package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoErrorf(t, err, "open db")
	t.Cleanup(func() { database.Close() })
	return database
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(openTestDB(t), nil, nil, "/", nil, ServerOptions{})
}

func TestNewBoundsStartupTmuxOrphanCleanup(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "slow-tmux")
	require.NoError(os.WriteFile(script, []byte(
		"#!/bin/sh\nsleep 0.25\nexit 0\n",
	), 0o755))

	origTimeout := startupTmuxCleanupTimeout
	startupTmuxCleanupTimeout = 20 * time.Millisecond
	t.Cleanup(func() { startupTmuxCleanupTimeout = origTimeout })

	cfg := &config.Config{
		Tmux: config.Tmux{
			Command: []string{script, "wrap"},
		},
	}

	start := time.Now()
	s := New(
		openTestDB(t), nil, nil, "/", cfg,
		ServerOptions{WorktreeDir: t.TempDir()},
	)
	elapsed := time.Since(start)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	assert.Less(elapsed, 150*time.Millisecond)
}

func TestHealthzAndLivez_ReturnOK(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	for _, path := range []string{"/healthz", "/livez"} {
		resp, err := http.Get(ts.URL + path)
		require.NoError(err)
		assert.Equal(http.StatusOK, resp.StatusCode, path)
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var body struct {
			Status string `json:"status"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		require.NoError(err)

		assert.Equal("ok", body.Status, path)
	}
}

func TestHealthz_ReturnsServiceUnavailableAfterDBClose(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	database := openTestDB(t)
	s := New(database, nil, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(s)
	defer ts.Close()

	t.Cleanup(func() { gracefulShutdown(t, s) })

	require.NoError(database.Close())

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(err)

	assert.Equal(http.StatusServiceUnavailable, resp.StatusCode)
	assert.Contains(string(body), "database unavailable")
}

func TestSSE_ReturnsEventStream(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
}

func TestSSE_ReceivesBroadcastEvent(t *testing.T) {
	s := newTestServer(t)
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
		if rest, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = rest
		}
		if line == "" && eventType != "" {
			break
		}
	}
	assert.Equal(t, "data_changed", eventType)
}

func TestSSE_InitialSyncStatusFromCache(t *testing.T) {
	s := newTestServer(t)
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
		if rest, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = rest
		}
		if line == "" && eventType != "" {
			break
		}
	}
	assert.Equal(t, "sync_status", eventType)
}

func TestSSE_ExitsCleanlyOnHubClose(t *testing.T) {
	s := newTestServer(t)
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
	assert := assert.New(t)
	require := require.New(t)

	s := newTestServer(t)
	// Prime hub so first subscribe gets a sync_status — we read it
	// as proof the handler has subscribed before we broadcast test events.
	s.hub.Broadcast(Event{Type: "sync_status", Data: map[string]bool{"running": false}})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(err)
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
			if rest, ok := strings.CutPrefix(line, "event: "); ok {
				evType = rest
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
		assert.Equal("sync_status", ev)
	case <-time.After(5 * time.Second):
		require.FailNow("timed out waiting for initial sync_status")
	}

	// Now safe to broadcast — handler is subscribed
	s.hub.Broadcast(Event{Type: "bad", Data: make(chan int)})
	s.hub.Broadcast(Event{Type: "data_changed", Data: struct{}{}})

	select {
	case ev := <-events:
		assert.Equal("data_changed", ev, "valid event should arrive after marshal failure")
	case <-time.After(5 * time.Second):
		require.FailNow("timed out waiting for data_changed after marshal failure")
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
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Overrun the buffer without reading — 17 broadcasts (buffer=16)
	for i := range 17 {
		s.hub.Broadcast(Event{Type: "data_changed", Data: i})
	}

	// The handler should close the connection
	body := make([]byte, 1)
	_, err = resp.Body.Read(body)
	_ = err // may be EOF or connection reset; we just want to ensure no hang
}

// deadlineControlWriter wraps a ResponseWriter with a controllable
// SetWriteDeadline for testing SSE error paths. failAfter controls how
// many successful calls are allowed before returning an error.
type deadlineControlWriter struct {
	http.ResponseWriter
	mu        sync.Mutex
	failAfter int // calls succeed up to this count; 0 = fail immediately
	calls     int
}

func (w *deadlineControlWriter) SetWriteDeadline(_ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.calls > w.failAfter {
		return errors.New("deadline not supported")
	}
	return nil
}

// Unwrap lets ResponseController find Flush on the inner writer.
func (w *deadlineControlWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func TestSSE_TerminatesOnInitialDeadlineFailure(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	w := &deadlineControlWriter{ResponseWriter: rec, failAfter: 0}
	r := httptest.NewRequest("GET", "/api/v1/events", nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSSE(w, r)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "handler did not exit on initial deadline failure")
	}
}

func TestSSE_TerminatesOnMidStreamDeadlineFailure(t *testing.T) {
	s := newTestServer(t)
	// Cached sync_status delivered on subscribe triggers mid-stream write
	s.hub.Broadcast(Event{Type: "sync_status", Data: map[string]bool{"running": false}})

	rec := httptest.NewRecorder()
	// First call (initial clear) succeeds; second (pre-write deadline) fails
	w := &deadlineControlWriter{ResponseWriter: rec, failAfter: 1}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSSE(w, r)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "handler did not exit on mid-stream deadline failure")
	}

	// Cancel context so Subscribe's cleanup goroutine unsubscribes
	cancel()
	require.Eventually(t, func() bool {
		s.hub.mu.Lock()
		defer s.hub.mu.Unlock()
		return len(s.hub.subscribers) == 0
	}, 2*time.Second, 10*time.Millisecond, "subscriber should be cleaned up after context cancel")

	// Deadline failed before event write — body must be empty
	assert.Empty(t, rec.Body.String(), "no output should be written after deadline failure")
}
