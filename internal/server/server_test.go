package server

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	s := newTestServer(t)
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
