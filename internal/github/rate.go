package github

import (
	"log/slog"
	"sync"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
)

// RateTracker records per-host API request counts and rate limit
// state, persisting to SQLite for cross-restart visibility.
type RateTracker struct {
	mu           sync.Mutex
	db           *db.DB
	platformHost string
	count        int
	hourStart    time.Time
	remaining    int
	resetAt      *time.Time
}

// NewRateTracker creates a tracker for the given platform host.
// remaining is initialized to -1 (unknown) until the first API
// response updates it.
func NewRateTracker(
	database *db.DB, platformHost string,
) *RateTracker {
	return &RateTracker{
		db:           database,
		platformHost: platformHost,
		remaining:    -1,
		hourStart:    truncateHour(time.Now().UTC()),
	}
}

// RecordRequest increments the hourly request counter and
// persists to DB.
func (rt *RateTracker) RecordRequest() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.rollIfNeeded()
	rt.count++
	rt.persist()
}

// UpdateFromRate updates remaining/reset from a go-github Rate.
func (rt *RateTracker) UpdateFromRate(rate gh.Rate) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.remaining = rate.Remaining
	resetTime := rate.Reset.UTC()
	rt.resetAt = &resetTime
	rt.persist()
}

// ShouldBackoff returns true and the wait duration if the rate
// limit is exhausted (remaining==0). If resetAt is nil, defaults
// to 60s. Returns false if remaining is >0 or unknown (-1).
func (rt *RateTracker) ShouldBackoff() (bool, time.Duration) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.remaining != 0 {
		return false, 0
	}
	if rt.resetAt == nil {
		return true, 60 * time.Second
	}
	wait := time.Until(*rt.resetAt)
	if wait <= 0 {
		return false, 0
	}
	return true, wait
}

// Remaining returns the last known remaining request count.
func (rt *RateTracker) Remaining() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.remaining
}

// RequestsThisHour returns the number of requests recorded in
// the current hour window.
func (rt *RateTracker) RequestsThisHour() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.rollIfNeeded()
	return rt.count
}

// rollIfNeeded resets the counter if the hour boundary has passed.
// Must be called with mu held.
func (rt *RateTracker) rollIfNeeded() {
	now := truncateHour(time.Now().UTC())
	if now.After(rt.hourStart) {
		rt.count = 0
		rt.hourStart = now
	}
}

// persist writes current state to DB. Must be called with mu held.
func (rt *RateTracker) persist() {
	err := rt.db.UpsertRateLimit(
		rt.platformHost,
		rt.count,
		rt.hourStart,
		rt.remaining,
		rt.resetAt,
	)
	if err != nil {
		slog.Warn("persist rate limit failed",
			"host", rt.platformHost, "err", err,
		)
	}
}

// truncateHour returns t truncated to the start of its hour.
func truncateHour(t time.Time) time.Time {
	return t.Truncate(time.Hour)
}
