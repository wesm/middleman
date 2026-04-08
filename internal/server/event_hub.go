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
	closed         bool // guarded by mu
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
// cached lastSyncStatus if available. If the hub is already closed,
// returns an immediately-closed channel and the closed done channel
// so callers can exit cleanly without leaking a goroutine.
func (h *EventHub) Subscribe(ctx context.Context) (<-chan Event, <-chan struct{}) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		ch := make(chan Event)
		close(ch)
		return ch, h.done
	}
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
// exit, marks the hub closed so future Subscribe calls fail fast,
// then cleans up all subscriber channels.
func (h *EventHub) Close() {
	h.closeOnce.Do(func() {
		close(h.done)
		h.mu.Lock()
		defer h.mu.Unlock()
		h.closed = true
		for id := range h.subscribers {
			h.unsubscribeLocked(id)
		}
	})
}
