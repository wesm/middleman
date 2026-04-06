package github

import (
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateTrackerCounting(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	rt := NewRateTracker(d, "github.com")

	assert.Equal(0, rt.RequestsThisHour())
	assert.Equal(-1, rt.Remaining())

	rt.RecordRequest()
	rt.RecordRequest()
	rt.RecordRequest()

	assert.Equal(3, rt.RequestsThisHour())

	// Verify persisted to DB
	rl, err := d.GetRateLimit("github.com")
	require.NoError(err)
	require.NotNil(rl)
	assert.Equal(3, rl.RequestsHour)
	assert.Equal(-1, rl.RateRemaining)
}

func TestRateTrackerBackoff(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	rt := NewRateTracker(d, "github.com")

	// No backoff when remaining is -1 (unknown)
	backoff, wait := rt.ShouldBackoff()
	assert.False(backoff)
	assert.Zero(wait)

	// No backoff when remaining > 0
	futureReset := time.Now().Add(30 * time.Minute)
	rt.UpdateFromRate(gh.Rate{
		Remaining: 100,
		Reset:     gh.Timestamp{Time: futureReset},
	})
	backoff, wait = rt.ShouldBackoff()
	assert.False(backoff)
	assert.Zero(wait)

	// Backoff when remaining == 0 with future reset
	rt.UpdateFromRate(gh.Rate{
		Remaining: 0,
		Reset:     gh.Timestamp{Time: futureReset},
	})
	backoff, wait = rt.ShouldBackoff()
	assert.True(backoff)
	assert.Greater(wait, time.Duration(0))

	// Backoff with nil resetAt defaults to 60s
	rt.mu.Lock()
	rt.remaining = 0
	rt.resetAt = nil
	rt.mu.Unlock()
	backoff, wait = rt.ShouldBackoff()
	assert.True(backoff)
	assert.Equal(60*time.Second, wait)

	// No backoff when reset is in the past
	pastReset := time.Now().Add(-1 * time.Minute)
	rt.UpdateFromRate(gh.Rate{
		Remaining: 0,
		Reset:     gh.Timestamp{Time: pastReset},
	})
	backoff, wait = rt.ShouldBackoff()
	assert.False(backoff)
	assert.Zero(wait)
}

func TestRateTrackerHourRollover(t *testing.T) {
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com")

	for range 5 {
		rt.RecordRequest()
	}
	Assert.Equal(t, 5, rt.RequestsThisHour())

	// Simulate hour rollover by manipulating internal state.
	rt.mu.Lock()
	rt.hourStart = time.Now().Add(-2 * time.Hour)
	rt.mu.Unlock()

	rt.RecordRequest()
	Assert.Equal(t, 1, rt.RequestsThisHour(),
		"counter should reset after hour boundary")
}

func TestRateTrackerConcurrentAccess(t *testing.T) {
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com")

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			rt.RecordRequest()
			rt.RequestsThisHour()
			rt.Remaining()
			rt.ShouldBackoff()
		})
	}
	wg.Wait()
	Assert.GreaterOrEqual(t, rt.RequestsThisHour(), 1)
}
