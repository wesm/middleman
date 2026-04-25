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

	ch, _ := hub.Subscribe(t.Context())
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

	ctx, cancel := context.WithCancel(t.Context())
	ch, _ := hub.Subscribe(ctx)
	cancel()

	// Receive blocks until the cleanup goroutine closes the channel,
	// up to a generous safety timeout. No fixed sleep — the test
	// completes as soon as the channel is actually closed.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed after context cancel")
	case <-time.After(time.Second):
		require.FailNow(t, "channel was not closed within 1s of context cancel")
	}
}

func TestEventHub_ConcurrentBroadcastSafety(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	ch, _ := hub.Subscribe(t.Context())

	done := make(chan struct{})
	go func() {
		for i := range 100 {
			hub.Broadcast(Event{Type: "sync_status", Data: i})
		}
		close(done)
	}()
	go func() {
		for i := range 100 {
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

	ch, _ := hub.Subscribe(t.Context())

	// Fill buffer (16) + one more to trigger eviction
	for i := range 17 {
		hub.Broadcast(Event{Type: "data_changed", Data: i})
	}

	// Drain buffered events; channel should close
	count := 0
	for range ch {
		count++
	}
	assert.Equal(t, 16, count, "should receive exactly buffer-size events before close")
}

func TestEventHub_SyncStatusCachedForNewSubscribers(t *testing.T) {
	hub := NewEventHub()
	defer hub.Close()

	hub.Broadcast(Event{Type: "sync_status", Data: map[string]bool{"running": true}})

	ch, _ := hub.Subscribe(t.Context())

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

	ch, _ := hub.Subscribe(t.Context())

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

	ch, _ := hub.Subscribe(t.Context())

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

	ch, _ := hub.Subscribe(t.Context())

	ev := <-ch
	assert.Equal(t, "t2", ev.Data, "new subscriber should get the latest cached status")
}

func TestEventHub_SubscribeOrderingWithBroadcast(t *testing.T) {
	assert := assert.New(t)

	hub := NewEventHub()
	defer hub.Close()

	hub.Broadcast(Event{Type: "sync_status", Data: "cached"})

	ch, _ := hub.Subscribe(t.Context())
	hub.Broadcast(Event{Type: "data_changed", Data: "live"})

	ev1 := <-ch
	assert.Equal("sync_status", ev1.Type)
	assert.Equal("cached", ev1.Data)

	ev2 := <-ch
	assert.Equal("data_changed", ev2.Type)
	assert.Equal("live", ev2.Data)
}

func TestEventHub_CloseUnsubscribesAll(t *testing.T) {
	hub := NewEventHub()

	ch1, done := hub.Subscribe(t.Context())
	ch2, _ := hub.Subscribe(t.Context())

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

	// Subscribe slow consumer — never read
	_, _ = hub.Subscribe(t.Context())

	// Fill + overflow to evict
	for i := range 17 {
		hub.Broadcast(Event{Type: "data_changed", Data: i})
	}

	// New broadcast should not panic (evicted subscriber gone)
	hub.Broadcast(Event{Type: "data_changed", Data: "after-eviction"})
}
