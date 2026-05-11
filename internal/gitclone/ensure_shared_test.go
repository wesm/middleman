package gitclone

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureCloneKey(t *testing.T) {
	assert := assert.New(t)

	a := ensureCloneKey("github.com", "acme", "widget")
	b := ensureCloneKey("github.com", "acme", "widget")
	assert.Equal(a, b, "same triple must hash to the same key")

	assert.NotEqual(a, ensureCloneKey("gitlab.com", "acme", "widget"))
	assert.NotEqual(a, ensureCloneKey("github.com", "other", "widget"))
	assert.NotEqual(a, ensureCloneKey("github.com", "acme", "other"))

	// Pathological owner/name combinations must not collide with each
	// other after concatenation. Without the null separator, owner=foo
	// name=barbaz would alias owner=foobar name=baz.
	x := ensureCloneKey("github.com", "foo", "barbaz")
	y := ensureCloneKey("github.com", "foobar", "baz")
	assert.NotEqual(x, y, "key must not be collision-prone on concat")
}

// TestEnsureCloneSharedDedupesConcurrentCallers verifies the singleflight
// invariant: callers that arrive while a fetch is in-flight attach to it
// instead of starting another fetch. The test uses a two-phase design so
// followers are guaranteed to reach DoChan while the leader is blocked
// inside fn, rather than racing the leader to acquire the slot.
func TestEnsureCloneSharedDedupesConcurrentCallers(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	mgr := New(t.TempDir(), nil)

	const followers = 15
	const callers = 1 + followers

	var calls atomic.Int32
	leaderStarted := make(chan struct{})
	release := make(chan struct{})

	fn := func(ctx context.Context) error {
		calls.Add(1)
		<-release
		return nil
	}

	// Phase 1: the leader takes the singleflight slot.
	errs := make([]error, callers)
	var wg sync.WaitGroup
	wg.Go(func() {
		errs[0] = mgr.ensureCloneShared(
			t.Context(), "github.com", "acme", "widget",
			func(ctx context.Context) error {
				close(leaderStarted)
				return fn(ctx)
			},
		)
	})
	<-leaderStarted

	// Phase 2: followers attach. Their fn must never run because the
	// leader still holds the slot.
	for i := 1; i < callers; i++ {
		wg.Go(func() {
			errs[i] = mgr.ensureCloneShared(
				t.Context(), "github.com", "acme", "widget", fn,
			)
		})
	}

	// Give followers time to reach DoChan and attach to the slot.
	// `go func()` to DoChan is microseconds in practice; 100ms is a
	// generous slack even on a loaded CI runner.
	time.Sleep(100 * time.Millisecond)

	close(release)
	wg.Wait()

	assert.Equal(int32(1), calls.Load(), "fn must run exactly once for concurrent same-key callers")
	for i, err := range errs {
		require.NoError(err, "caller %d", i)
	}
}

// TestEnsureCloneSharedSeparatesDistinctRepos verifies fn runs once per
// distinct repo when concurrent callers target different keys.
func TestEnsureCloneSharedSeparatesDistinctRepos(t *testing.T) {
	assert := assert.New(t)

	mgr := New(t.TempDir(), nil)

	var calls atomic.Int32
	fn := func(ctx context.Context) error {
		calls.Add(1)
		return nil
	}

	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c", "d"} {
		wg.Go(func() {
			_ = mgr.ensureCloneShared(
				t.Context(), "github.com", "acme", name, fn,
			)
		})
	}
	wg.Wait()

	assert.Equal(int32(4), calls.Load(), "distinct repos must not share a slot")
}

// TestEnsureCloneSharedShortCircuitsCanceledContext verifies that a
// caller with an already-canceled context does not enter the
// singleflight slot. Without the pre-check, EnsureClone would still
// kick off the underlying work and only the caller's select would
// observe the cancellation, leaving work running in the background
// the caller has logically abandoned.
func TestEnsureCloneSharedShortCircuitsCanceledContext(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	mgr := New(t.TempDir(), nil)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var called atomic.Bool
	err := mgr.ensureCloneShared(
		ctx, "github.com", "acme", "widget",
		func(ctx context.Context) error {
			called.Store(true)
			return nil
		},
	)

	require.ErrorIs(err, context.Canceled)
	assert.False(called.Load(), "fn must not run when caller ctx is already canceled")
}

// TestEnsureCloneSharedRunnerHasDeadline verifies that the runner
// context carries a deadline so a stuck git subprocess cannot hold
// the singleflight slot indefinitely.
func TestEnsureCloneSharedRunnerHasDeadline(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	mgr := New(t.TempDir(), nil)

	var observedDeadline time.Time
	var hadDeadline bool
	err := mgr.ensureCloneShared(
		t.Context(), "github.com", "acme", "widget",
		func(ctx context.Context) error {
			observedDeadline, hadDeadline = ctx.Deadline()
			return nil
		},
	)

	require.NoError(err)
	require.True(hadDeadline, "runner ctx must carry a deadline")
	assert.WithinDuration(
		time.Now().Add(ensureCloneTimeout), observedDeadline,
		time.Minute,
		"deadline should be ~ensureCloneTimeout from now",
	)
}

// TestEnsureCloneSharedDetachedContextSurvivesCancel verifies that a
// canceled caller does not abort the in-flight fetch for the other
// waiters sharing the slot.
func TestEnsureCloneSharedDetachedContextSurvivesCancel(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	mgr := New(t.TempDir(), nil)

	started := make(chan struct{})
	release := make(chan struct{})
	var fnErr atomic.Pointer[error]
	fn := func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			err := ctx.Err()
			fnErr.Store(&err)
			return err
		}
		return nil
	}

	cancelCtx, cancel := context.WithCancel(t.Context())

	doneCancel := make(chan error, 1)
	go func() {
		doneCancel <- mgr.ensureCloneShared(
			cancelCtx, "github.com", "acme", "widget", fn,
		)
	}()

	<-started

	// Cancel the first caller; the singleflight slot must continue so
	// other waiters can still receive the result.
	cancel()
	require.ErrorIs(<-doneCancel, context.Canceled)

	// fn must not have observed cancellation because we passed it a
	// detached context.
	assert.Nil(fnErr.Load(), "fn should not see cancellation from caller ctx")

	// Now let fn finish. (It is still running.) A second caller on the
	// same key should be served by the same in-flight slot.
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- mgr.ensureCloneShared(
			t.Context(), "github.com", "acme", "widget", fn,
		)
	}()

	// Give the second caller a moment to attach to the slot. Then
	// release fn.
	time.Sleep(10 * time.Millisecond)
	close(release)

	select {
	case err := <-secondDone:
		require.NoError(err)
	case <-time.After(2 * time.Second):
		assert.Fail("second caller did not complete after fn returned")
	}
}
