package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	"golang.org/x/sync/singleflight"
)

// SyncStatus holds the current state of the sync engine.
type SyncStatus struct {
	Running     bool      `json:"running"`
	CurrentRepo string    `json:"current_repo,omitempty"`
	Progress    string    `json:"progress,omitempty"`
	LastRunAt   time.Time `json:"last_run_at,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
}

// DiffSyncErrorCode categorizes the reason a diff sync failed. The frontend
// uses this category to render a user-facing message that does not leak local
// clone paths, refs, SHAs, or git stderr.
type DiffSyncErrorCode string

const (
	// DiffSyncCodeCloneUnavailable means the local bare clone could not be
	// created or updated (network failure, disk full, permission denied).
	DiffSyncCodeCloneUnavailable DiffSyncErrorCode = "clone_unavailable"
	// DiffSyncCodeCommitUnreachable means a commit needed to compute the diff
	// (PR head, merge commit, or its first parent) is not present in the local
	// clone and could not be fetched.
	DiffSyncCodeCommitUnreachable DiffSyncErrorCode = "commit_unreachable"
	// DiffSyncCodeMergeBaseFailed means git merge-base could not compute the
	// fork point between the PR head and the base.
	DiffSyncCodeMergeBaseFailed DiffSyncErrorCode = "merge_base_failed"
	// DiffSyncCodeInternal covers database failures and other unexpected
	// internal errors during diff computation.
	DiffSyncCodeInternal DiffSyncErrorCode = "internal"
)

// DiffSyncError reports a non-fatal failure to compute or update the diff SHAs
// for a PR. SyncMR returns this when only the diff portion of the sync failed:
// the PR row, timeline, and CI status were updated successfully, so callers
// should still treat the PR data as fresh, but the diff view will be stale or
// missing until the underlying problem is fixed.
//
// Code categorizes the failure for client-facing messaging via UserMessage.
// Err preserves the underlying detail for server-side logging only — never
// expose Err.Error() to API clients, since it can contain clone paths, refs,
// SHAs, and git stderr.
type DiffSyncError struct {
	Code DiffSyncErrorCode
	Err  error
}

func (e *DiffSyncError) Error() string {
	return fmt.Sprintf("diff sync failed (%s): %v", e.Code, e.Err)
}

func (e *DiffSyncError) Unwrap() error {
	return e.Err
}

// UserMessage returns a sanitized message safe to surface to API clients.
// It never includes clone paths, refs, SHAs, or other internal details from
// the underlying error.
func (e *DiffSyncError) UserMessage() string {
	switch e.Code {
	case DiffSyncCodeCloneUnavailable:
		return "Diff data is unavailable: the local repository clone could not be prepared."
	case DiffSyncCodeCommitUnreachable:
		return "Diff data is unavailable: a required commit is missing from the local clone."
	case DiffSyncCodeMergeBaseFailed:
		return "Diff data is unavailable: could not determine the merge base for this pull request."
	case DiffSyncCodeInternal:
		return "Diff data is unavailable: internal error while updating diff data."
	default:
		return "Diff data is unavailable."
	}
}

// RepoRef identifies a GitHub repository.
type RepoRef struct {
	Owner        string
	Name         string
	PlatformHost string // "github.com" or GHE hostname
}

// RepoSyncResult holds the outcome of syncing a single repo.
type RepoSyncResult struct {
	Owner        string
	Name         string
	PlatformHost string
	Error        string // empty on success
}

// WatchedMR identifies a merge request to sync on a fast interval.
type WatchedMR struct {
	Owner        string
	Name         string
	Number       int
	PlatformHost string // "github.com" or GHE hostname
}

// defaultParallelism is the worker pool size used by RunOnce when
// SetParallelism has not been called. Bounded so we don't burst the
// per-host GitHub rate limit / abuse-detection thresholds.
const defaultParallelism = 4

// Syncer periodically pulls PR data from GitHub into SQLite.
type Syncer struct {
	clients            map[string]Client // host -> client
	db                 *db.DB
	clones             *gitclone.Manager
	rateTrackers       map[string]*RateTracker // host -> tracker
	budgets            map[string]*SyncBudget  // host -> budget
	budgetPerHour      int
	repos              []RepoRef
	reposMu            sync.Mutex
	interval           time.Duration
	watchInterval      time.Duration
	watchedMRs         []WatchedMR
	watchMu            sync.Mutex
	parallelism        atomic.Int32
	running            atomic.Bool
	status             atomic.Value // stores *SyncStatus
	stopCh             chan struct{}
	stopOnce           sync.Once
	wg                 sync.WaitGroup
	// lifecycleMu serializes TriggerRun registration with Stop so
	// no wg.Add can happen after Stop begins wg.Wait.
	lifecycleMu sync.Mutex
	stopped     bool // guarded by lifecycleMu
	nextSyncAfter      map[string]time.Time // host -> next eligible background sync time
	nextWatchSyncAfter map[string]time.Time // host -> next eligible watch-sync time
	displayNames       map[string]string    // "host\x00login" -> display name, per sync run
	displayNamesMu     sync.Mutex
	displayNameGroup   singleflight.Group // dedups concurrent GetUser calls
	onMRSynced         func(owner, name string, mr *db.MergeRequest)
	onSyncCompleted    func(results []RepoSyncResult)
	onStatusChange     func(status *SyncStatus)
	// statusMu serializes publishStatus so worker goroutines
	// can't interleave updates and deliver out-of-order snapshots
	// to SSE subscribers.
	statusMu sync.Mutex

	// failedRepos tracks repos whose last sync had a partial failure
	// (a per-PR, per-issue, or closure-detection step failed after
	// the ETag cache was populated by a successful 200 list fetch).
	// Values are failScope bitmasks indicating which path(s) failed.
	// The next sync cycle consults this set at the top of doSyncRepo
	// and forces an unconditional refetch of the list endpoints so
	// the failed items get re-applied from a fresh 200 response
	// instead of being skipped by a silent 304. Keyed by
	// "host/owner/name". Cleared on the next successful sync.
	failedRepos sync.Map

	// runCtx is the syncer's lifetime context. It is canceled in
	// Stop so in-flight RunOnce / TriggerRun goroutines observe
	// cancellation and unblock any long-running GitHub calls. Both
	// Start and TriggerRun derive their goroutine context from
	// runCtx (merged with any caller context), so Stop can unblock
	// the work it spawned regardless of whether the caller's ctx
	// is still live. runCtxMu guards lazy init and the Stop
	// handoff.
	runCtx    context.Context
	runCancel context.CancelFunc
	runCtxMu  sync.Mutex
}

// ensureRunCtx lazily initializes runCtx/runCancel. Safe to call
// multiple times; the first caller wins and later calls are no-ops.
func (s *Syncer) ensureRunCtx() context.Context {
	s.runCtxMu.Lock()
	defer s.runCtxMu.Unlock()
	if s.runCtx == nil {
		s.runCtx, s.runCancel = context.WithCancel(context.Background())
	}
	return s.runCtx
}

// mergeWithRunCtx returns a context that is canceled when either the
// caller's ctx or the syncer's lifetime ctx is canceled. The returned
// cancel function must be called to release resources. Used by
// TriggerRun so ad-hoc runs respect both the caller's deadline and
// Stop's global cancellation signal.
func (s *Syncer) mergeWithRunCtx(caller context.Context) (context.Context, context.CancelFunc) {
	runCtx := s.ensureRunCtx()
	merged, cancel := context.WithCancel(caller)
	go func() {
		select {
		case <-runCtx.Done():
			cancel()
		case <-merged.Done():
		}
	}()
	return merged, cancel
}

// failScope is a bitmask indicating which sync paths failed.
type failScope uint8

const (
	failMR     failScope = 1 << iota // PR/MR sync path failed
	failIssues                       // issue sync path failed
)

// markRepoFailed records that the most recent sync of this repo hit
// a partial failure after the ETag cache may have been populated, so
// the next cycle must force an unconditional refetch of the affected
// list endpoints. Matched by clearRepoFailed on a clean cycle.
func (s *Syncer) markRepoFailed(repo RepoRef, scope failScope) {
	key := repoFailKey(repo)
	for {
		prev, ok := s.failedRepos.Load(key)
		merged := scope
		if ok {
			merged |= prev.(failScope)
		}
		if ok {
			if s.failedRepos.CompareAndSwap(key, prev, merged) {
				return
			}
		} else {
			if _, loaded := s.failedRepos.LoadOrStore(key, merged); !loaded {
				return
			}
		}
		// Another goroutine raced us; retry.
	}
}

// clearRepoFailed clears the partial-failure flag after a clean
// doSyncRepo pass.
func (s *Syncer) clearRepoFailed(repo RepoRef) {
	s.failedRepos.Delete(repoFailKey(repo))
}

// repoFailKey returns the sync.Map key for a repo. Includes the host
// so multi-host setups don't cross-invalidate.
func repoFailKey(repo RepoRef) string {
	host := repo.PlatformHost
	if host == "" {
		host = "github.com"
	}
	return host + "/" + repo.Owner + "/" + repo.Name
}

// consumeRepoFailed returns the failScope bitmask for a previously
// failed repo. Returns 0 if the repo had no failure. The flag remains
// set until a subsequent successful sync explicitly clears it.
func (s *Syncer) consumeRepoFailed(repo RepoRef) failScope {
	v, ok := s.failedRepos.Load(repoFailKey(repo))
	if !ok {
		return 0
	}
	return v.(failScope)
}

// publishStatus stores a status snapshot and invokes the
// onStatusChange callback if one is registered. Used in place of
// s.status.Store so SSE subscribers see every state transition.
func (s *Syncer) publishStatus(status *SyncStatus) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status.Store(status)
	if s.onStatusChange != nil {
		s.onStatusChange(status)
	}
}

// NewSyncer creates a Syncer that polls the given repos on the
// given interval. clients maps host -> Client; rateTrackers maps
// host -> RateTracker. Both may contain nil values. clones may
// be nil. budgetPerHour controls how many API calls the detail
// drain may spend per host per hour (0 disables detail drain).
func NewSyncer(
	clients map[string]Client,
	database *db.DB,
	clones *gitclone.Manager,
	repos []RepoRef,
	interval time.Duration,
	rateTrackers map[string]*RateTracker,
	budgetPerHour int,
) *Syncer {
	if clients == nil {
		clients = make(map[string]Client)
	}
	if rateTrackers == nil {
		rateTrackers = make(map[string]*RateTracker)
	}

	// Collect unique hosts from repos and clients.
	hostSet := make(map[string]struct{})
	for _, r := range repos {
		h := r.PlatformHost
		if h == "" {
			h = "github.com"
		}
		hostSet[h] = struct{}{}
	}
	for h := range clients {
		hostSet[h] = struct{}{}
	}
	budgets := make(map[string]*SyncBudget, len(hostSet))
	if budgetPerHour > 0 {
		for h := range hostSet {
			budgets[h] = NewSyncBudget(budgetPerHour)
		}
	}

	s := &Syncer{
		clients:            clients,
		db:                 database,
		clones:             clones,
		rateTrackers:       rateTrackers,
		budgets:            budgets,
		budgetPerHour:      budgetPerHour,
		repos:              repos,
		interval:           interval,
		nextSyncAfter:      make(map[string]time.Time),
		nextWatchSyncAfter: make(map[string]time.Time),
		stopCh:             make(chan struct{}),
	}
	s.parallelism.Store(defaultParallelism)
	s.status.Store(&SyncStatus{})

	// Wire budget reset to rate tracker window resets.
	for h, rt := range rateTrackers {
		if b, ok := budgets[h]; ok && rt != nil {
			rt.SetOnWindowReset(b.Reset)
		}
	}

	return s
}

// SetWatchInterval sets the fast-sync interval for watched MRs.
// Must be called before Start.
func (s *Syncer) SetWatchInterval(d time.Duration) {
	s.watchInterval = d
}

// HasDiffSync reports whether the syncer has a clone manager configured
// and is therefore expected to populate diff SHAs for tracked PRs. The
// HTTP layer uses this to decide whether a missing diff is a sync issue
// worth warning about, or simply a deployment that opted out of diffs.
func (s *Syncer) HasDiffSync() bool {
	return s.clones != nil
}

// SetWatchedMRs replaces the fast-sync watch list. Each watched
// MR is synced on the watch interval via SyncMR, independent of
// the bulk sync cycle.
func (s *Syncer) SetWatchedMRs(mrs []WatchedMR) {
	s.watchMu.Lock()
	s.watchedMRs = make([]WatchedMR, len(mrs))
	copy(s.watchedMRs, mrs)
	s.watchMu.Unlock()
}

// SetOnMRSynced registers a callback invoked after each MR
// is upserted during a sync pass.
//
// Concurrency: RunOnce processes repos in parallel (see
// SetParallelism), so the callback may be invoked from up to
// `parallelism` goroutines concurrently. Implementations must
// be safe for concurrent use. The callback also runs on the
// goroutine that is mid-sync for a repo, so it must not block
// indefinitely or it will stall sync progress.
//
// Call SetOnMRSynced before Start/RunOnce. Mutating the hook
// while a sync is in flight is not safe.
func (s *Syncer) SetOnMRSynced(
	fn func(owner, name string, mr *db.MergeRequest),
) {
	s.onMRSynced = fn
}

// SetOnSyncCompleted registers a callback invoked at the end
// of each RunOnce pass with per-repo sync results.
//
// Concurrency: this hook fires once per RunOnce pass on the
// goroutine that drives RunOnce, so it is not invoked
// concurrently with itself. Call SetOnSyncCompleted before
// Start/RunOnce; mutating the hook while a sync is in flight
// is not safe.
func (s *Syncer) SetOnSyncCompleted(
	fn func(results []RepoSyncResult),
) {
	s.onSyncCompleted = fn
}

// SetParallelism sets the maximum number of repos synced
// concurrently in RunOnce. Values <= 0 are clamped to 1
// (sequential).
func (s *Syncer) SetParallelism(n int) {
	if n < 1 {
		n = 1
	}
	s.parallelism.Store(int32(n))
}

// SetOnStatusChange registers a callback invoked whenever the
// sync status transitions (start, per-repo progress, rate-limit
// wait, completion). Used by the server to broadcast live sync
// state over SSE.
func (s *Syncer) SetOnStatusChange(fn func(status *SyncStatus)) {
	s.onStatusChange = fn
}

// TriggerRun kicks off a non-blocking RunOnce on the Syncer's
// wait group so callers can request an ad-hoc sync without
// blocking the caller. The run participates in the Syncer's
// lifecycle: Stop cancels the merged context so any in-flight
// GitHub call unblocks, then waits for the goroutine to exit.
// The caller's ctx is honored too, so per-request deadlines still
// apply.
func (s *Syncer) TriggerRun(ctx context.Context) {
	s.lifecycleMu.Lock()
	if s.stopped {
		s.lifecycleMu.Unlock()
		return
	}
	merged, cancel := s.mergeWithRunCtx(ctx)
	s.wg.Add(1)
	s.lifecycleMu.Unlock()

	go func() {
		defer s.wg.Done()
		defer cancel()
		s.RunOnce(merged)
	}()
}

// clientFor returns the Client for the given repo's host,
// falling back to "github.com" if no match is found.
func (s *Syncer) clientFor(repo RepoRef) Client {
	host := repo.PlatformHost
	if host == "" {
		host = "github.com"
	}
	if c, ok := s.clients[host]; ok {
		return c
	}
	return s.clients["github.com"]
}

// ClientForRepo returns the Client for a tracked repo by
// owner/name, or an error if the repo is not tracked.
func (s *Syncer) ClientForRepo(
	owner, name string,
) (Client, error) {
	s.reposMu.Lock()
	defer s.reposMu.Unlock()
	for _, r := range s.repos {
		if r.Owner == owner && r.Name == name {
			return s.clientFor(r), nil
		}
	}
	return nil, fmt.Errorf(
		"repo %s/%s is not tracked", owner, name,
	)
}

// ClientForHost returns the Client for a specific host,
// or an error if no client is configured for that host.
func (s *Syncer) ClientForHost(
	host string,
) (Client, error) {
	if c, ok := s.clients[host]; ok {
		return c, nil
	}
	return nil, fmt.Errorf(
		"no client configured for host %s", host,
	)
}

// hostFor returns the platform host for a repo identified by
// owner/name. Returns "github.com" if not found.
func (s *Syncer) hostFor(owner, name string) string {
	for _, r := range s.repos {
		if r.Owner == owner && r.Name == name {
			if r.PlatformHost != "" {
				return r.PlatformHost
			}
			return "github.com"
		}
	}
	return "github.com"
}

// HostForRepo returns the platform host for a tracked repo.
// Thread-safe.
func (s *Syncer) HostForRepo(owner, name string) string {
	s.reposMu.Lock()
	defer s.reposMu.Unlock()
	return s.hostFor(owner, name)
}

// SetRepos atomically replaces the list of repositories to sync.
func (s *Syncer) SetRepos(repos []RepoRef) {
	s.reposMu.Lock()
	s.repos = make([]RepoRef, len(repos))
	copy(s.repos, repos)
	s.reposMu.Unlock()
}

// Start runs an immediate sync then launches a background ticker.
// It returns as soon as the goroutine is started; call Stop to shut it down.
// A second goroutine runs watched-MR fast-syncs on a shorter interval.
//
// The caller's ctx and the syncer's internal lifetime ctx (canceled
// by Stop) are both honored: either one unblocks any in-flight work.
func (s *Syncer) Start(ctx context.Context) {
	startMerged, startCancel := s.mergeWithRunCtx(ctx)
	s.wg.Go(func() {
		defer startCancel()
		s.RunOnce(startMerged)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RunOnce(startMerged)
			case <-s.stopCh:
				return
			case <-startMerged.Done():
				return
			}
		}
	})

	watchInt := s.watchInterval
	if watchInt <= 0 {
		watchInt = 30 * time.Second
	}
	watchMerged, watchCancel := s.mergeWithRunCtx(ctx)
	s.wg.Go(func() {
		defer watchCancel()
		ticker := time.NewTicker(watchInt)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.syncWatchedMRs(watchMerged)
			case <-s.stopCh:
				return
			case <-watchMerged.Done():
				return
			}
		}
	})
}

// syncWatchedMRs syncs each MR on the watch list via SyncMR.
// Fires onMRSynced (inside SyncMR) but not onSyncCompleted.
// Checks per-host rate limits before issuing API calls.
// hostEligibility computes which hosts are eligible for sync
// based on rate tracker state and the next-sync-after gate.
// hosts may contain duplicates; they are deduplicated internally.
func (s *Syncer) hostEligibility(
	hosts []string,
	nextAfter map[string]time.Time,
) map[string]bool {
	now := time.Now()
	eligible := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		if _, checked := eligible[host]; checked {
			continue
		}
		rt := s.rateTrackers[host]
		if rt == nil {
			eligible[host] = true
			continue
		}
		if rt.IsPaused() {
			eligible[host] = false
			continue
		}
		if after, ok := nextAfter[host]; ok && now.Before(after) {
			eligible[host] = false
			continue
		}
		eligible[host] = true
	}
	return eligible
}

// advanceNextSync updates the next-sync-after gate for hosts
// that were eligible, using each host's current throttle factor.
func (s *Syncer) advanceNextSync(
	eligible map[string]bool,
	nextAfter map[string]time.Time,
	interval time.Duration,
) {
	now := time.Now()
	for host, ok := range eligible {
		if !ok {
			continue
		}
		rt := s.rateTrackers[host]
		if rt == nil {
			continue
		}
		nextAfter[host] = now.Add(
			interval * time.Duration(rt.ThrottleFactor()),
		)
	}
}

func (s *Syncer) syncWatchedMRs(ctx context.Context) {
	s.watchMu.Lock()
	mrs := make([]WatchedMR, len(s.watchedMRs))
	copy(mrs, s.watchedMRs)
	s.watchMu.Unlock()

	if len(mrs) == 0 {
		return
	}

	watchInt := s.watchInterval
	if watchInt <= 0 {
		watchInt = 30 * time.Second
	}
	watchHosts := make([]string, len(mrs))
	for i, mr := range mrs {
		host := mr.PlatformHost
		if host == "" {
			host = "github.com"
		}
		watchHosts[i] = host
	}
	eligibleHosts := s.hostEligibility(
		watchHosts, s.nextWatchSyncAfter,
	)

	// Check backoff once per host to avoid redundant checks.
	blockedHosts := make(map[string]bool)
	for _, mr := range mrs {
		host := mr.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if _, checked := blockedHosts[host]; checked {
			continue
		}
		if rt := s.rateTrackers[host]; rt != nil {
			if backoff, _ := rt.ShouldBackoff(); backoff {
				blockedHosts[host] = true
				continue
			}
		}
		blockedHosts[host] = false
	}

	for _, mr := range mrs {
		host := mr.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if !eligibleHosts[host] {
			slog.Debug("skipping fast-sync for throttled host",
				"host", host,
				"owner", mr.Owner,
				"name", mr.Name,
				"number", mr.Number,
			)
			continue
		}
		if blockedHosts[host] {
			slog.Debug("skipping fast-sync for rate-limited host",
				"host", host,
				"owner", mr.Owner,
				"name", mr.Name,
				"number", mr.Number,
			)
			continue
		}
		if err := s.syncMRWithHost(ctx, mr.Owner, mr.Name, mr.Number, host); err != nil {
			slog.Warn("fast-sync watched MR failed",
				"owner", mr.Owner,
				"name", mr.Name,
				"number", mr.Number,
				"err", err,
			)
		}
	}

	s.advanceNextSync(
		eligibleHosts, s.nextWatchSyncAfter, watchInt,
	)
}

// stopGracePeriod bounds how long Stop will wait for in-flight work
// to exit after the syncer's lifetime context is canceled. If a
// misbehaving dependency ignores ctx, Stop gives up and logs a
// warning rather than deadlocking the caller.
const stopGracePeriod = 30 * time.Second

// Stop signals the background goroutine to exit. Safe to call
// multiple times. Cancels the syncer's lifetime context first so
// blocked RunOnce and TriggerRun goroutines can observe the
// cancellation and unwind their GitHub calls, then waits for the
// wait group up to stopGracePeriod. The bounded wait prevents Stop
// from hanging the process in pathological cases where a client
// ignores ctx.
func (s *Syncer) Stop() {
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.stopped = true
		s.lifecycleMu.Unlock()

		close(s.stopCh)
		s.runCtxMu.Lock()
		cancel := s.runCancel
		s.runCtxMu.Unlock()
		if cancel != nil {
			cancel()
		}
	})

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopGracePeriod):
		slog.Warn("syncer stop timed out; returning while work is still in flight",
			"grace", stopGracePeriod)
	}
}

// Status returns a snapshot of the current sync state.
func (s *Syncer) Status() *SyncStatus {
	return s.status.Load().(*SyncStatus)
}

// RateTrackers returns the per-host rate trackers map.
func (s *Syncer) RateTrackers() map[string]*RateTracker {
	return s.rateTrackers
}

// Budgets returns the per-host sync budgets map.
func (s *Syncer) Budgets() map[string]*SyncBudget {
	return s.budgets
}

// runState holds the per-RunOnce mutable state shared by the
// worker pool. Extracted into a struct so runWorker can be a
// directly testable method instead of an inline closure.
type runState struct {
	completed *atomic.Int32
	maxShown  *atomic.Int32
	errMu     *sync.Mutex
	lastErr   *string
	// canceled is latched to true at the moment any goroutine
	// observes ctx cancellation while work is still outstanding.
	// RunOnce uses this flag (rather than a completed-count
	// heuristic) to decide whether the run was canceled, so a
	// misbehaving syncRepo that ignores ctx and returns success
	// cannot mask cancellation.
	canceled *atomic.Bool
	total    int
	// results is a preallocated slice indexed by repo position so
	// OnSyncCompleted receives results in the configured repo order
	// regardless of worker completion order. Each index is written
	// by exactly one worker, so no mutex is needed.
	results []RepoSyncResult
}

// repoWork pairs a repo with its index in the configured repo list
// so workers can write results to the correct preallocated slot.
type repoWork struct {
	index int
	repo  RepoRef
}

// runWorker drains the work channel until it is closed or ctx
// is canceled. It is the body of each goroutine spawned by
// RunOnce. Extracted from the inline closure so cancellation
// behavior can be unit-tested directly without racing against
// the dispatch loop.
func (s *Syncer) runWorker(
	ctx context.Context,
	work <-chan repoWork,
	state *runState,
) {
	for item := range work {
		repo := item.repo
		// Defense-in-depth against the dispatch race: the
		// dispatch loop pre-checks ctx before its select, but
		// a cancel can still land in the micro-window between
		// the pre-check and the select, in which case Go's
		// select may pick the send branch and hand this worker
		// a repo that should never have been enqueued. Bail
		// here before logging or starting any work, and latch
		// the canceled flag so RunOnce reports the run as
		// canceled regardless of how many repos happened to
		// finish in parallel.
		if ctx.Err() != nil {
			state.canceled.Store(true)
			return
		}
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if rt := s.rateTrackers[host]; rt != nil {
			if backoff, wait := rt.ShouldBackoff(); backoff {
				s.publishStatus(&SyncStatus{
					Running: true,
					Progress: fmt.Sprintf(
						"rate limited, waiting %s", wait,
					),
				})
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					state.canceled.Store(true)
					return
				}
			}
		}
		repoName := repo.Owner + "/" + repo.Name
		slog.Info("syncing repo", "repo", repoName)
		if err := s.syncRepo(ctx, repo); err != nil {
			// Bail without counting this repo only when the
			// *run* context itself is canceled and the error
			// reflects that. Per-request timeouts also come
			// back as wrapped context.DeadlineExceeded but
			// must reach the normal error path so they're
			// captured in lastErr instead of being silently
			// dropped.
			if ctx.Err() != nil &&
				(errors.Is(err, context.Canceled) ||
					errors.Is(err, context.DeadlineExceeded)) {
				state.canceled.Store(true)
				return
			}
			errStr := err.Error()
			slog.Error("sync repo failed",
				"repo", repoName, "err", err,
			)
			state.errMu.Lock()
			*state.lastErr = errStr
			state.errMu.Unlock()
			// Each index is written by exactly one worker.
			state.results[item.index].Error = errStr
		}
		// Latch the canceled flag if ctx was canceled during
		// syncRepo. A misbehaving Client implementation can
		// ignore ctx and return nil (or a non-context error)
		// even after cancellation; without this check the run
		// would fall through to the success path and fire
		// onSyncCompleted for what the user asked to cancel.
		if ctx.Err() != nil {
			state.canceled.Store(true)
			return
		}
		done := state.completed.Add(1)
		for {
			cur := state.maxShown.Load()
			if done <= cur {
				break
			}
			if state.maxShown.CompareAndSwap(cur, done) {
				s.publishStatus(&SyncStatus{
					Running:  true,
					Progress: fmt.Sprintf("%d/%d", done, state.total),
				})
				break
			}
		}
	}
}

// RunOnce performs a single sync pass across all configured repos.
// If a sync is already in progress it returns immediately (single-flight).
//
// Repos are synced in parallel using a bounded worker pool sized by
// SetParallelism (default defaultParallelism). The bound keeps the
// per-host GitHub rate limit and abuse-detection thresholds happy
// while still capturing most of the wall-clock win on network I/O.
func (s *Syncer) RunOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	s.reposMu.Lock()
	repos := make([]RepoRef, len(s.repos))
	copy(repos, s.repos)
	s.reposMu.Unlock()

	total := len(repos)
	s.publishStatus(&SyncStatus{
		Running:  true,
		Progress: fmt.Sprintf("0/%d", total),
	})
	s.displayNamesMu.Lock()
	s.displayNames = make(map[string]string)
	s.displayNamesMu.Unlock()
	slog.Info("sync started", "repos", total)

	workers := min(max(int(s.parallelism.Load()), 1), total)

	work := make(chan repoWork)
	results := make([]RepoSyncResult, total)
	for i, r := range repos {
		host := r.PlatformHost
		if host == "" {
			host = "github.com"
		}
		results[i] = RepoSyncResult{
			Owner:        r.Owner,
			Name:         r.Name,
			PlatformHost: host,
		}
	}

	repoHosts := make([]string, len(repos))
	for i, r := range repos {
		host := r.PlatformHost
		if host == "" {
			host = "github.com"
		}
		repoHosts[i] = host
	}
	eligibleHosts := s.hostEligibility(
		repoHosts, s.nextSyncAfter,
	)

	var (
		completed atomic.Int32
		maxShown  atomic.Int32
		errMu     sync.Mutex
		lastErr   string
		canceled  atomic.Bool
		wg        sync.WaitGroup
	)

	state := &runState{
		completed: &completed,
		maxShown:  &maxShown,
		errMu:     &errMu,
		lastErr:   &lastErr,
		canceled:  &canceled,
		total:     total,
		results:   results,
	}
	for range workers {
		wg.Go(func() {
			s.runWorker(ctx, work, state)
		})
	}

dispatch:
	for i, r := range repos {
		host := r.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if !eligibleHosts[host] {
			results[i].Error = "skipped: rate limit throttled"
			completed.Add(1)
			s.status.Store(&SyncStatus{
				Running:  true,
				Progress: fmt.Sprintf("%d/%d", completed.Load(), total),
			})
			continue
		}
		// Check ctx before entering the select. Go's select picks
		// pseudo-randomly when both branches are ready, so a naked
		// `select { case work <- r: case <-ctx.Done(): }` can still
		// hand a repo to a ready worker after the run has been
		// canceled. The pre-check biases the loop toward cancel so
		// the dispatch reliably stops once ctx is done.
		if ctx.Err() != nil {
			canceled.Store(true)
			break dispatch
		}
		item := repoWork{index: i, repo: r}
		select {
		case work <- item:
		case <-ctx.Done():
			canceled.Store(true)
			break dispatch
		}
	}
	close(work)
	wg.Wait()

	s.advanceNextSync(
		eligibleHosts, s.nextSyncAfter, s.interval,
	)

	// Detail drain: fetch full details for highest-priority items
	// within the per-host budget. Runs after index scan completes.
	if !canceled.Load() && ctx.Err() == nil {
		s.drainDetailQueue(ctx, eligibleHosts)
	}

	// Backfill discovery: fetch closed items if budget allows.
	if !canceled.Load() && ctx.Err() == nil {
		for host, ok := range eligibleHosts {
			if !ok {
				continue
			}
			s.runBackfillDiscovery(ctx, host, repos)
		}
	}

	// Use a latched flag (set by the dispatch loop and workers at
	// the moment they observe ctx cancellation) rather than a
	// completed-count heuristic. A misbehaving syncRepo that
	// ignores ctx and returns success would otherwise let the
	// run fall through to onSyncCompleted even though the user
	// asked to cancel. A cancel that races in strictly *after*
	// every worker finished and returned never latches the flag,
	// so the late-cancel-after-clean-sync case still reports
	// success.
	if canceled.Load() {
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		slog.Info("sync canceled", "repos", total, "err", err)
		s.publishStatus(&SyncStatus{
			Running:   false,
			LastRunAt: time.Now(),
			LastError: err.Error(),
		})
		return
	}

	slog.Info("sync complete", "repos", total)

	if s.onSyncCompleted != nil {
		s.onSyncCompleted(results)
	}

	s.publishStatus(&SyncStatus{
		Running:   false,
		LastRunAt: time.Now(),
		LastError: lastErr,
	})
}

// syncRepo syncs one repository: open PRs, timeline events, and stale closures.
func (s *Syncer) syncRepo(ctx context.Context, repo RepoRef) error {
	repoID, err := s.db.UpsertRepo(ctx, repo.PlatformHost, repo.Owner, repo.Name)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", repo.Owner, repo.Name, err)
	}

	client := s.clientFor(repo)

	ghRepo, err := client.GetRepository(ctx, repo.Owner, repo.Name)
	if err != nil {
		slog.Warn("get repo settings failed",
			"repo", repo.Owner+"/"+repo.Name, "err", err,
		)
	} else {
		_ = s.db.UpdateRepoSettings(ctx, repoID,
			ghRepo.GetAllowSquashMerge(),
			ghRepo.GetAllowMergeCommit(),
			ghRepo.GetAllowRebaseMerge(),
		)
	}

	if err := s.db.UpdateRepoSyncStarted(ctx, repoID, time.Now()); err != nil {
		return fmt.Errorf("mark sync started for %s/%s: %w", repo.Owner, repo.Name, err)
	}

	// Fetch bare clone before PR data so refs are available for merge-base.
	host := repo.PlatformHost
	if host == "" {
		host = "github.com"
	}
	cloneFetchOK := false
	if s.clones != nil {
		remoteURL := fmt.Sprintf("https://%s/%s/%s.git", host, repo.Owner, repo.Name)
		if err := s.clones.EnsureClone(ctx, host, repo.Owner, repo.Name, remoteURL); err != nil {
			slog.Warn("bare clone fetch failed",
				"repo", repo.Owner+"/"+repo.Name, "err", err,
			)
		} else {
			cloneFetchOK = true
		}
	}

	syncErr := s.indexSyncRepo(ctx, repo, repoID, cloneFetchOK)

	syncErrStr := ""
	if syncErr != nil {
		syncErrStr = syncErr.Error()
	}
	if err := s.db.UpdateRepoSyncCompleted(ctx, repoID, time.Now(), syncErrStr); err != nil {
		slog.Error("mark sync completed", "repo", repo.Owner+"/"+repo.Name, "err", err)
	}

	return syncErr
}

// indexSyncRepo performs the cheap index scan: list endpoints only,
// upserting basic data without detail fetches. This runs every cycle.
func (s *Syncer) indexSyncRepo(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	cloneFetchOK bool,
) error {
	client := s.clientFor(repo)

	// If the previous sync of this repo partially failed after the
	// ETag cache was populated by a 200 list response, a naive next
	// cycle would see 304 and skip the per-item upserts that failed
	// last time, leaving the DB stale until the TTL expired. Evict
	// the repo's list ETags so the following calls are
	// unconditional, forcing a fresh 200 that we can re-apply.
	priorFail := s.consumeRepoFailed(repo)
	forceMR := priorFail&failMR != 0
	forceIssues := priorFail&failIssues != 0
	if priorFail != 0 {
		var endpoints []string
		if forceMR {
			endpoints = append(endpoints, "pulls")
		}
		if forceIssues {
			endpoints = append(endpoints, "issues")
		}
		client.InvalidateListETagsForRepo(repo.Owner, repo.Name, endpoints...)
	}

	// Track partial-failure signals per path so the next cycle only
	// forces refresh on the paths that actually failed.
	var failedScope failScope

	ghPRs, err := client.ListOpenPullRequests(
		ctx, repo.Owner, repo.Name,
	)
	prListUnchanged := false
	if err != nil {
		// 304 Not Modified means the open-PR list is byte-identical
		// to the previous fetch. No PR opened, no PR closed, no
		// metadata on any open PR changed. Skip per-PR upserts and
		// closure detection — both ran on the previous sync that
		// produced the cached etag.
		if IsNotModified(err) {
			prListUnchanged = true
		} else {
			s.markRepoFailed(repo, failMR)
			return fmt.Errorf("list open PRs: %w", err)
		}
	}

	if prListUnchanged {
		// 304 means the open-PR *list* (numbers, head SHAs, metadata the
		// list endpoint returns) is unchanged, but check runs and combined
		// status change independently of anything the PR list reflects.
		// Iterate the DB's current open PRs and refresh CI so pending or
		// failed checks still surface within a sync cycle instead of
		// lagging until some list-visible field changes.
		//
		// Each refreshCIStatus costs 2 API calls (check runs + combined
		// status), so gate on the per-host budget to avoid consuming
		// unbounded calls in the index phase.
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		budget := s.budgets[host]

		openMRs, err := s.db.ListMergeRequests(ctx, db.ListMergeRequestsOpts{
			RepoOwner: repo.Owner,
			RepoName:  repo.Name,
			State:     "open",
		})
		if err != nil {
			slog.Error("list open MRs for 304 CI refresh failed",
				"repo", repo.Owner+"/"+repo.Name, "err", err,
			)
			failedScope |= failMR
		} else {
			for _, mr := range openMRs {
				if budget != nil && !budget.TrySpend(2) {
					slog.Debug("304 CI refresh skipped: budget exhausted",
						"repo", repo.Owner+"/"+repo.Name,
						"number", mr.Number,
					)
					break
				}
				if err := s.refreshCIStatus(ctx, repo, repoID, mr.Number, mr.PlatformHeadSHA); err != nil {
					slog.Error("refresh CI status on 304 failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", mr.Number,
						"err", err,
					)
					failedScope |= failMR
					// No refund: both API calls (ListCheckRunsForRef +
					// GetCombinedStatus) already executed before the
					// error — only the DB write failed.
				}
			}
		}
	} else {
		stillOpen := make(map[int]bool, len(ghPRs))
		for _, ghPR := range ghPRs {
			stillOpen[ghPR.GetNumber()] = true
		}

		for _, ghPR := range ghPRs {
			if err := s.indexUpsertMR(
				ctx, repo, repoID, ghPR,
			); err != nil {
				slog.Error("index upsert MR failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", ghPR.GetNumber(),
					"err", err,
				)
				failedScope |= failMR
			}
		}

		// Detect closed PRs and fetch final state (1 API call each,
		// outside budget -- needed for accurate closed state).
		closedNumbers, err := s.db.GetPreviouslyOpenMRNumbers(
			ctx, repoID, stillOpen,
		)
		if err != nil {
			s.markRepoFailed(repo, failMR)
			return fmt.Errorf("get previously open MRs: %w", err)
		}
		for _, number := range closedNumbers {
			if err := s.fetchAndUpdateClosed(
				ctx, repo, repoID, number, cloneFetchOK,
			); err != nil {
				slog.Error("update closed MR failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", number,
					"err", err,
				)
				failedScope |= failMR
			}
		}
	}

	// Index issues (list-only, no detail).
	// Issues have an independent etag, so this still runs even when the
	// PR list returned 304.
	if err := s.indexSyncIssues(
		ctx, repo, repoID, forceIssues,
	); err != nil {
		slog.Error("index sync issues failed",
			"repo", repo.Owner+"/"+repo.Name, "err", err,
		)
		failedScope |= failIssues
	}

	if failedScope != 0 {
		// One or more per-item steps failed. Record which paths
		// failed so the next cycle forces an unconditional refetch
		// only for the affected list endpoints.
		s.markRepoFailed(repo, failedScope)
	} else {
		// Clean pass — drop any leftover flag from a prior cycle.
		s.clearRepoFailed(repo)
	}

	return nil
}

// indexUpsertMR upserts a PR from list endpoint data only. No
// GetPullRequest, no timeline, no CI. Preserves fields that the
// list endpoint does not return (additions, deletions,
// mergeable_state) from the existing DB row.
func (s *Syncer) indexUpsertMR(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	ghPR *gh.PullRequest,
) error {
	normalized := NormalizePR(repoID, ghPR)

	existing, err := s.db.GetMergeRequest(
		ctx, repo.Owner, repo.Name, ghPR.GetNumber(),
	)
	if err != nil {
		return fmt.Errorf(
			"get existing MR #%d: %w", ghPR.GetNumber(), err,
		)
	}

	// Preserve fields the list endpoint doesn't return.
	if existing != nil {
		normalized.Additions = existing.Additions
		normalized.Deletions = existing.Deletions
		normalized.MergeableState = existing.MergeableState
	}

	if normalized.Author != "" &&
		normalized.AuthorDisplayName == "" {
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		client := s.clientFor(repo)
		if name, ok := s.resolveDisplayName(
			ctx, client, host, normalized.Author,
		); ok {
			normalized.AuthorDisplayName = name
		} else if existing != nil {
			normalized.AuthorDisplayName =
				existing.AuthorDisplayName
		}
	}

	mrID, err := s.db.UpsertMergeRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf(
			"upsert MR #%d: %w", ghPR.GetNumber(), err,
		)
	}

	if err := s.db.EnsureKanbanState(ctx, mrID); err != nil {
		return fmt.Errorf(
			"ensure kanban state for MR #%d: %w",
			ghPR.GetNumber(), err,
		)
	}

	return nil
}

// fetchMRDetail performs a full detail fetch for a single MR:
// GetPullRequest, refreshTimeline, refreshCIStatus. Returns the
// number of API calls made.
func (s *Syncer) fetchMRDetail(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	number int,
	cloneFetchOK bool,
) (int, error) {
	calls := 0
	client := s.clientFor(repo)

	fullPR, err := client.GetPullRequest(
		ctx, repo.Owner, repo.Name, number,
	)
	calls++
	if err == nil && fullPR == nil {
		err = fmt.Errorf("client returned nil pull request")
	}
	if err != nil {
		return calls, fmt.Errorf(
			"get full PR #%d: %w", number, err,
		)
	}

	normalized := NormalizePR(repoID, fullPR)

	if normalized.Author != "" &&
		normalized.AuthorDisplayName == "" {
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if name, ok := s.resolveDisplayName(
			ctx, client, host, normalized.Author,
		); ok {
			normalized.AuthorDisplayName = name
		}
		calls++ // GetUser
	}

	mrID, err := s.db.UpsertMergeRequest(ctx, normalized)
	if err != nil {
		return calls, fmt.Errorf(
			"upsert MR #%d: %w", number, err,
		)
	}

	if err := s.db.EnsureKanbanState(ctx, mrID); err != nil {
		return calls, fmt.Errorf(
			"ensure kanban state for MR #%d: %w", number, err,
		)
	}

	// Diff SHAs if clone available.
	repoHost := repo.PlatformHost
	if repoHost == "" {
		repoHost = "github.com"
	}
	if s.clones != nil && cloneFetchOK {
		headSHA := normalized.PlatformHeadSHA
		baseSHA := normalized.PlatformBaseSHA
		if headSHA != "" && baseSHA != "" {
			mb, mbErr := s.clones.MergeBase(
				ctx, repoHost, repo.Owner,
				repo.Name, baseSHA, headSHA,
			)
			if mbErr != nil {
				slog.Warn("merge-base computation failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", number, "err", mbErr,
				)
			} else {
				if dbErr := s.db.UpdateDiffSHAs(
					ctx, repoID, number,
					headSHA, baseSHA, mb,
				); dbErr != nil {
					slog.Warn("update diff SHAs failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", number, "err", dbErr,
					)
				}
			}
		}
	}

	if err := s.refreshTimeline(
		ctx, repo, repoID, mrID, fullPR,
	); err != nil {
		// Timeline = 4 calls (comments + reviews + commits + force-push).
		calls += 4
		return calls, err
	}
	calls += 4

	ciHeadSHA := ""
	if fullPR.GetHead() != nil {
		ciHeadSHA = fullPR.GetHead().GetSHA()
	}
	if err := s.refreshCIStatus(
		ctx, repo, repoID, number, ciHeadSHA,
	); err != nil {
		// CI = 2 calls (combined status + check runs).
		calls += 2
		return calls, err
	}
	calls += 2

	// Determine whether CI had pending checks for scoring by
	// reading the DB row that refreshCIStatus just wrote. Use
	// ciHasPending (checks individual statuses) rather than the
	// aggregate CIStatus, which becomes "failure" when any check
	// fails even if others are still running.
	pending := false
	freshMR, freshErr := s.db.GetMergeRequest(
		ctx, repo.Owner, repo.Name, number,
	)
	if freshErr == nil && freshMR != nil {
		pending = ciHasPending(freshMR.CIChecksJSON)
	}

	detailHost := repo.PlatformHost
	if detailHost == "" {
		detailHost = "github.com"
	}
	if err := s.db.UpdateMRDetailFetched(
		ctx, detailHost, repo.Owner, repo.Name, number, pending,
	); err != nil {
		return calls, fmt.Errorf(
			"mark detail fetched for MR #%d: %w", number, err,
		)
	}

	// Fire onMRSynced hook.
	if s.onMRSynced != nil {
		fresh, fErr := s.db.GetMergeRequest(
			ctx, repo.Owner, repo.Name, number,
		)
		if fErr != nil {
			slog.Warn("get MR for onMRSynced hook failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number, "err", fErr,
			)
		} else {
			s.onMRSynced(repo.Owner, repo.Name, fresh)
		}
	}

	return calls, nil
}

// fetchIssueDetail performs a full detail fetch for a single
// issue: GetIssue + refreshIssueTimeline. Returns the number
// of API calls made.
func (s *Syncer) fetchIssueDetail(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	number int,
) (int, error) {
	calls := 0
	client := s.clientFor(repo)

	ghIssue, err := client.GetIssue(
		ctx, repo.Owner, repo.Name, number,
	)
	calls++
	if err != nil {
		return calls, fmt.Errorf(
			"get issue #%d: %w", number, err,
		)
	}

	normalized := NormalizeIssue(repoID, ghIssue)
	issueID, err := s.db.UpsertIssue(ctx, normalized)
	if err != nil {
		return calls, fmt.Errorf(
			"upsert issue #%d: %w", number, err,
		)
	}

	if err := s.refreshIssueTimeline(
		ctx, repo, issueID, ghIssue,
	); err != nil {
		calls++ // comments
		return calls, err
	}
	calls++ // comments

	issueHost := repo.PlatformHost
	if issueHost == "" {
		issueHost = "github.com"
	}
	if err := s.db.UpdateIssueDetailFetched(
		ctx, issueHost, repo.Owner, repo.Name, number,
	); err != nil {
		return calls, fmt.Errorf(
			"mark detail fetched for issue #%d: %w", number, err,
		)
	}

	return calls, nil
}

// refreshTimeline fetches comments, reviews, and commits for a PR and
// updates its derived fields (ReviewDecision, CommentCount, LastActivityAt, CIStatus).
func (s *Syncer) refreshTimeline(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	mrID int64,
	ghPR *gh.PullRequest,
) error {
	number := ghPR.GetNumber()
	client := s.clientFor(repo)

	comments, err := client.ListIssueComments(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list comments for MR #%d: %w", number, err)
	}

	reviews, err := client.ListReviews(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list reviews for MR #%d: %w", number, err)
	}

	commits, err := client.ListCommits(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list commits for MR #%d: %w", number, err)
	}

	forcePushEvents, err := client.ListForcePushEvents(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		slog.Warn("force-push fetch failed during timeline refresh",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number,
			"err", err,
		)
		forcePushEvents = nil
	}

	var events []db.MREvent
	for _, c := range comments {
		events = append(events, NormalizeCommentEvent(mrID, c))
	}
	for _, r := range reviews {
		events = append(events, NormalizeReviewEvent(mrID, r))
	}
	for _, c := range commits {
		events = append(events, NormalizeCommitEvent(mrID, c))
	}
	for _, fp := range forcePushEvents {
		events = append(events, NormalizeForcePushEvent(mrID, fp))
	}

	if err := s.db.UpsertMREvents(ctx, events); err != nil {
		return fmt.Errorf("upsert events for MR #%d: %w", number, err)
	}

	reviewDecision := DeriveReviewDecision(reviews)
	lastActivityAt := computeLastActivity(ghPR, comments, reviews, commits)

	return s.db.UpdateMRDerivedFields(ctx, repoID, number, db.MRDerivedFields{
		ReviewDecision: reviewDecision,
		CommentCount:   len(comments),
		LastActivityAt: lastActivityAt,
	})
}

// refreshCIStatus fetches combined status and check runs for a PR's head SHA.
// Called on every sync cycle for open PRs, since check runs change independently
// of the PR's updated_at field. Takes headSHA and number directly so it can be
// invoked from the 304 code path, where the caller holds DB rows rather than
// a *gh.PullRequest.
func (s *Syncer) refreshCIStatus(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	number int,
	headSHA string,
) error {
	if headSHA == "" {
		return nil
	}

	// Fetch both sources. On failure, skip the DB write to preserve
	// existing data rather than wiping it with empty values.
	client := s.clientFor(repo)
	checkRuns, err := client.ListCheckRunsForRef(ctx, repo.Owner, repo.Name, headSHA)
	if err != nil {
		slog.Warn("list check runs failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number,
			"err", err,
		)
		return nil
	}

	combined, err := client.GetCombinedStatus(ctx, repo.Owner, repo.Name, headSHA)
	if err != nil {
		slog.Warn("get combined status failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number,
			"err", err,
		)
		return nil
	}

	ciStatus := DeriveOverallCIStatus(checkRuns, combined)
	ciChecksJSON := NormalizeCIChecks(checkRuns, combined)

	return s.db.UpdateMRCIStatus(ctx, repoID, number, ciStatus, ciChecksJSON)
}

// ciHasPending parses the CI checks JSON and returns true if any
// check has a status other than "completed".
func ciHasPending(ciChecksJSON string) bool {
	if ciChecksJSON == "" {
		return false
	}
	var checks []db.CICheck
	if err := json.Unmarshal([]byte(ciChecksJSON), &checks); err != nil {
		return false
	}
	for _, c := range checks {
		if c.Status != "completed" {
			return true
		}
	}
	return false
}

// computeLastActivity returns the most recent timestamp across the PR and its events.
func computeLastActivity(
	ghPR *gh.PullRequest,
	comments []*gh.IssueComment,
	reviews []*gh.PullRequestReview,
	commits []*gh.RepositoryCommit,
) time.Time {
	latest := time.Time{}
	if ghPR.UpdatedAt != nil {
		latest = ghPR.UpdatedAt.Time
	}

	for _, c := range comments {
		if c.UpdatedAt != nil && c.UpdatedAt.After(latest) {
			latest = c.UpdatedAt.Time
		}
	}
	for _, r := range reviews {
		if r.SubmittedAt != nil && r.SubmittedAt.After(latest) {
			latest = r.SubmittedAt.Time
		}
	}
	for _, c := range commits {
		if c.GetCommit() != nil && c.GetCommit().Author != nil &&
			c.GetCommit().Author.Date != nil &&
			c.GetCommit().Author.Date.After(latest) {
			latest = c.GetCommit().Author.Date.Time
		}
	}
	return latest
}

// resolveDisplayName returns the GitHub display name for a login and
// whether the lookup succeeded. Returns ("", false) on API failure so
// callers can preserve existing data. Uses an in-memory cache across
// a sync run plus singleflight dedup so concurrent workers racing on
// the same author only trigger one GetUser call.
func (s *Syncer) resolveDisplayName(
	ctx context.Context, client Client, host, login string,
) (string, bool) {
	key := host + "\x00" + login
	s.displayNamesMu.Lock()
	name, ok := s.displayNames[key]
	s.displayNamesMu.Unlock()
	if ok {
		return name, true
	}

	v, err, _ := s.displayNameGroup.Do(key, func() (any, error) {
		// Re-check the cache inside the singleflight slot: another
		// caller may have populated it while this one was waiting
		// for its turn to run.
		s.displayNamesMu.Lock()
		if cached, ok := s.displayNames[key]; ok {
			s.displayNamesMu.Unlock()
			return cached, nil
		}
		s.displayNamesMu.Unlock()

		user, err := client.GetUser(ctx, login)
		if err != nil {
			return "", err
		}
		resolved := sanitizeDisplayName(user.GetName())
		s.displayNamesMu.Lock()
		s.displayNames[key] = resolved
		s.displayNamesMu.Unlock()
		return resolved, nil
	})
	if err != nil {
		slog.Warn("get user display name failed",
			"login", login, "err", err,
		)
		return "", false
	}
	return v.(string), true
}

// --- Issue sync ---

// indexSyncIssues syncs issues from list endpoint data and
// refreshes timeline when data changed or forceRefresh is set.
// Issues have no separate detail phase, so timeline refresh
// happens inline here via syncOpenIssue.
func (s *Syncer) indexSyncIssues(
	ctx context.Context, repo RepoRef, repoID int64, forceRefresh bool,
) error {
	client := s.clientFor(repo)
	ghIssues, err := client.ListOpenIssues(
		ctx, repo.Owner, repo.Name,
	)
	if err != nil {
		// 304: open issue list unchanged since the previous sync.
		// No issue opened, closed, or modified. Skip per-issue
		// upserts and closure detection.
		if IsNotModified(err) {
			return nil
		}
		return fmt.Errorf("list open issues: %w", err)
	}

	stillOpen := make(map[int]bool, len(ghIssues))
	for _, issue := range ghIssues {
		stillOpen[issue.GetNumber()] = true
	}

	var hadItemFailure bool
	for _, ghIssue := range ghIssues {
		if err := s.syncOpenIssue(ctx, repo, repoID, ghIssue, forceRefresh); err != nil {
			slog.Error("sync issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghIssue.GetNumber(),
				"err", err,
			)
			hadItemFailure = true
		}
	}

	// Detect closed issues and fetch final state.
	closedNumbers, err := s.db.GetPreviouslyOpenIssueNumbers(
		ctx, repoID, stillOpen,
	)
	if err != nil {
		return fmt.Errorf("get previously open issues: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosedIssue(
			ctx, repo, repoID, number,
		); err != nil {
			slog.Error("update closed issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
			hadItemFailure = true
		}
	}

	if hadItemFailure {
		return fmt.Errorf("one or more issue sync items failed")
	}
	return nil
}

func (s *Syncer) syncOpenIssue(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	ghIssue *gh.Issue,
	forceRefresh bool,
) error {
	normalized := NormalizeIssue(repoID, ghIssue)

	existing, err := s.db.GetIssue(
		ctx, repo.Owner, repo.Name, ghIssue.GetNumber(),
	)
	if err != nil {
		return fmt.Errorf(
			"get existing issue #%d: %w", ghIssue.GetNumber(), err,
		)
	}

	needsTimeline := forceRefresh || existing == nil ||
		!existing.UpdatedAt.Equal(normalized.UpdatedAt)

	issueID, err := s.db.UpsertIssue(ctx, normalized)
	if err != nil {
		return fmt.Errorf(
			"upsert issue #%d: %w", ghIssue.GetNumber(), err,
		)
	}

	if !needsTimeline {
		return nil
	}

	return s.refreshIssueTimeline(ctx, repo, issueID, ghIssue)
}

func (s *Syncer) refreshIssueTimeline(
	ctx context.Context,
	repo RepoRef,
	issueID int64,
	ghIssue *gh.Issue,
) error {
	number := ghIssue.GetNumber()
	client := s.clientFor(repo)

	comments, err := client.ListIssueComments(
		ctx, repo.Owner, repo.Name, number,
	)
	if err != nil {
		return fmt.Errorf(
			"list comments for issue #%d: %w", number, err,
		)
	}

	var events []db.IssueEvent
	for _, c := range comments {
		events = append(events, NormalizeIssueCommentEvent(issueID, c))
	}

	if err := s.db.UpsertIssueEvents(ctx, events); err != nil {
		return fmt.Errorf(
			"upsert issue events for #%d: %w", number, err,
		)
	}

	lastActivity := ghIssue.UpdatedAt.Time
	for _, c := range comments {
		if c.UpdatedAt != nil && c.UpdatedAt.After(lastActivity) {
			lastActivity = c.UpdatedAt.Time
		}
	}

	_, err = s.db.WriteDB().ExecContext(ctx,
		`UPDATE middleman_issues SET comment_count = ?, last_activity_at = ?
		 WHERE id = ?`,
		len(comments), lastActivity, issueID,
	)
	return err
}

func (s *Syncer) fetchAndUpdateClosedIssue(
	ctx context.Context, repo RepoRef, repoID int64, number int,
) error {
	client := s.clientFor(repo)
	ghIssue, err := client.GetIssue(
		ctx, repo.Owner, repo.Name, number,
	)
	if err != nil {
		return fmt.Errorf("get closed issue #%d: %w", number, err)
	}

	var closedAt *time.Time
	if ghIssue.ClosedAt != nil {
		t := ghIssue.ClosedAt.Time
		closedAt = &t
	}

	return s.db.UpdateIssueState(
		ctx, repoID, number, ghIssue.GetState(), closedAt,
	)
}

// --- Detail Drain ---

// drainDetailQueue builds a priority queue of items needing detail
// fetches and processes them within the per-host budget.
func (s *Syncer) drainDetailQueue(
	ctx context.Context,
	eligibleHosts map[string]bool,
) {
	if s.budgetPerHour <= 0 {
		return
	}

	items := s.buildDetailQueueItems(ctx)
	if len(items) == 0 {
		return
	}

	queue := BuildQueue(items, time.Now())
	if len(queue) == 0 {
		return
	}

	// Track which hosts are exhausted so we skip quickly.
	exhausted := make(map[string]bool)

	for i := range queue {
		if ctx.Err() != nil {
			return
		}
		qi := &queue[i]
		host := qi.PlatformHost
		if host == "" {
			host = "github.com"
		}

		if !eligibleHosts[host] {
			continue
		}
		if exhausted[host] {
			continue
		}

		budget := s.budgets[host]
		if budget == nil {
			continue
		}

		worstCase := qi.WorstCaseCost()
		if !budget.TrySpend(worstCase) {
			exhausted[host] = true
			continue
		}

		repo := RepoRef{
			Owner:        qi.RepoOwner,
			Name:         qi.RepoName,
			PlatformHost: qi.PlatformHost,
		}
		repoID, err := s.db.UpsertRepo(
			ctx, host, qi.RepoOwner, qi.RepoName,
		)
		if err != nil {
			slog.Warn("detail drain: upsert repo failed",
				"repo", qi.RepoOwner+"/"+qi.RepoName,
				"err", err,
			)
			budget.Refund(worstCase)
			continue
		}

		// Compute diff SHAs if clone available.
		cloneFetchOK := false
		if s.clones != nil {
			remoteURL := fmt.Sprintf(
				"https://%s/%s/%s.git",
				host, qi.RepoOwner, qi.RepoName,
			)
			if cloneErr := s.clones.EnsureClone(
				ctx, host, qi.RepoOwner, qi.RepoName,
				remoteURL,
			); cloneErr != nil {
				slog.Warn("detail drain: bare clone failed",
					"repo", qi.RepoOwner+"/"+qi.RepoName,
					"err", cloneErr,
				)
			} else {
				cloneFetchOK = true
			}
		}

		var actualCalls int
		if qi.Type == QueueItemPR {
			actualCalls, err = s.fetchMRDetail(
				ctx, repo, repoID, qi.Number, cloneFetchOK,
			)
		} else {
			actualCalls, err = s.fetchIssueDetail(
				ctx, repo, repoID, qi.Number,
			)
		}

		if actualCalls < worstCase {
			budget.Refund(worstCase - actualCalls)
		}

		if err != nil {
			slog.Warn("detail drain: fetch failed",
				"repo", qi.RepoOwner+"/"+qi.RepoName,
				"number", qi.Number,
				"type", qi.Type,
				"err", err,
			)
		}
	}
}

// buildDetailQueueItems queries the DB for open PRs and issues
// that may need a detail fetch, combining with starred/watched
// state to build queue items for scoring.
func (s *Syncer) buildDetailQueueItems(
	ctx context.Context,
) []QueueItem {
	// Build set of tracked repos to filter out stale DB rows
	// from removed repos.
	s.reposMu.Lock()
	trackedRepos := make(map[string]bool, len(s.repos))
	for _, r := range s.repos {
		host := r.PlatformHost
		if host == "" {
			host = "github.com"
		}
		trackedRepos[host+"\x00"+r.Owner+"/"+r.Name] = true
	}
	s.reposMu.Unlock()

	// Gather watched MR numbers for matching.
	s.watchMu.Lock()
	watched := make(map[string]bool, len(s.watchedMRs))
	for _, w := range s.watchedMRs {
		key := fmt.Sprintf(
			"%s/%s#%d", w.Owner, w.Name, w.Number,
		)
		watched[key] = true
	}
	s.watchMu.Unlock()

	var items []QueueItem

	// Open PRs.
	prs, err := s.db.ListMergeRequests(
		ctx, db.ListMergeRequestsOpts{State: "open"},
	)
	if err != nil {
		slog.Warn("detail drain: list open PRs failed",
			"err", err,
		)
		return nil
	}
	for _, pr := range prs {
		repo, rErr := s.db.GetRepoByID(ctx, pr.RepoID)
		if rErr != nil || repo == nil {
			continue
		}
		repoKey := repo.PlatformHost + "\x00" + repo.Owner + "/" + repo.Name
		if !trackedRepos[repoKey] {
			continue
		}
		watchKey := fmt.Sprintf(
			"%s/%s#%d", repo.Owner, repo.Name, pr.Number,
		)
		items = append(items, QueueItem{
			Type:            QueueItemPR,
			RepoOwner:       repo.Owner,
			RepoName:        repo.Name,
			Number:          pr.Number,
			PlatformHost:    repo.PlatformHost,
			UpdatedAt:       pr.UpdatedAt,
			DetailFetchedAt: pr.DetailFetchedAt,
			CIHadPending:    pr.CIHadPending,
			Starred:         pr.Starred,
			Watched:         watched[watchKey],
			IsOpen:          true,
		})
	}

	// Open issues.
	issues, err := s.db.ListIssues(
		ctx, db.ListIssuesOpts{State: "open"},
	)
	if err != nil {
		slog.Warn("detail drain: list open issues failed",
			"err", err,
		)
		return items
	}
	for _, issue := range issues {
		repo, rErr := s.db.GetRepoByID(ctx, issue.RepoID)
		if rErr != nil || repo == nil {
			continue
		}
		repoKey := repo.PlatformHost + "\x00" + repo.Owner + "/" + repo.Name
		if !trackedRepos[repoKey] {
			continue
		}
		items = append(items, QueueItem{
			Type:            QueueItemIssue,
			RepoOwner:       repo.Owner,
			RepoName:        repo.Name,
			Number:          issue.Number,
			PlatformHost:    repo.PlatformHost,
			UpdatedAt:       issue.UpdatedAt,
			DetailFetchedAt: issue.DetailFetchedAt,
			Starred:         issue.Starred,
			IsOpen:          true,
		})
	}

	return items
}

// --- Backfill Discovery ---

// backfillMaxPagesPerRepo limits how many closed-item pages
// we fetch per repo per cycle to stay gentle on the API.
const backfillMaxPagesPerRepo = 2

// runBackfillDiscovery fetches closed PRs/issues for repos on
// the given host, advancing backfill cursors stored in the DB.
// Only runs when >50% of the host's budget remains.
func (s *Syncer) runBackfillDiscovery(
	ctx context.Context,
	host string,
	repos []RepoRef,
) {
	budget := s.budgets[host]
	if budget == nil {
		return
	}
	if budget.Remaining() < budget.Limit()/2 {
		return
	}

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		rHost := repo.PlatformHost
		if rHost == "" {
			rHost = "github.com"
		}
		if rHost != host {
			continue
		}

		repoRow, err := s.db.GetRepoByOwnerName(
			ctx, repo.Owner, repo.Name,
		)
		if err != nil || repoRow == nil {
			continue
		}

		s.backfillRepo(ctx, repo, repoRow, budget)
	}
}

func (s *Syncer) backfillRepo(
	ctx context.Context,
	repo RepoRef,
	repoRow *db.Repo,
	budget *SyncBudget,
) {
	client := s.clientFor(repo)
	repoID := repoRow.ID
	now := time.Now()

	// PR backfill.
	prPage := repoRow.BackfillPRPage
	prComplete := repoRow.BackfillPRComplete
	prCompletedAt := repoRow.BackfillPRCompletedAt

	if prComplete && prCompletedAt != nil &&
		now.Sub(*prCompletedAt) < 24*time.Hour {
		// Skip -- completed recently.
	} else {
		if prComplete {
			// Reset for re-scan.
			prPage = 0
			prComplete = false
			prCompletedAt = nil
		}
		for range backfillMaxPagesPerRepo {
			if ctx.Err() != nil || !budget.CanSpend(1) {
				break
			}
			prPage++
			prs, hasMore, err := client.ListPullRequestsPage(
				ctx, repo.Owner, repo.Name,
				"closed", prPage,
			)
			budget.Spend(1)
			if err != nil {
				slog.Warn("backfill PRs failed",
					"repo", repo.Owner+"/"+repo.Name,
					"page", prPage, "err", err,
				)
				break
			}
			for _, ghPR := range prs {
				normalized := NormalizePR(repoID, ghPR)
				if _, uErr := s.db.UpsertMergeRequest(
					ctx, normalized,
				); uErr != nil {
					slog.Warn("backfill upsert PR failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", ghPR.GetNumber(),
						"err", uErr,
					)
				}
			}
			if !hasMore {
				prComplete = true
				t := now
				prCompletedAt = &t
				break
			}
		}
	}

	// Issue backfill.
	issuePage := repoRow.BackfillIssuePage
	issueComplete := repoRow.BackfillIssueComplete
	issueCompletedAt := repoRow.BackfillIssueCompletedAt

	if issueComplete && issueCompletedAt != nil &&
		now.Sub(*issueCompletedAt) < 24*time.Hour {
		// Skip.
	} else {
		if issueComplete {
			issuePage = 0
			issueComplete = false
			issueCompletedAt = nil
		}
		for range backfillMaxPagesPerRepo {
			if ctx.Err() != nil || !budget.CanSpend(1) {
				break
			}
			issuePage++
			issues, hasMore, err := client.ListIssuesPage(
				ctx, repo.Owner, repo.Name,
				"closed", issuePage,
			)
			budget.Spend(1)
			if err != nil {
				slog.Warn("backfill issues failed",
					"repo", repo.Owner+"/"+repo.Name,
					"page", issuePage, "err", err,
				)
				break
			}
			for _, ghIssue := range issues {
				normalized := NormalizeIssue(repoID, ghIssue)
				if _, uErr := s.db.UpsertIssue(
					ctx, normalized,
				); uErr != nil {
					slog.Warn("backfill upsert issue failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", ghIssue.GetNumber(),
						"err", uErr,
					)
				}
			}
			if !hasMore {
				issueComplete = true
				t := now
				issueCompletedAt = &t
				break
			}
		}
	}

	// Persist cursor state.
	if err := s.db.UpdateBackfillCursor(
		ctx, repoID,
		prPage, prComplete, prCompletedAt,
		issuePage, issueComplete, issueCompletedAt,
	); err != nil {
		slog.Warn("update backfill cursor failed",
			"repo", repo.Owner+"/"+repo.Name, "err", err,
		)
	}
}

// IsTrackedRepo checks whether the given repo is in the configured list.
func (s *Syncer) IsTrackedRepo(owner, name string) bool {
	s.reposMu.Lock()
	repos := s.repos
	s.reposMu.Unlock()
	for _, r := range repos {
		if r.Owner == owner && r.Name == name {
			return true
		}
	}
	return false
}

// isTrackedRepoOnHost checks whether the given repo on a specific host
// is in the configured list. Used by the watched-MR path where the
// host is known and must match exactly.
func (s *Syncer) isTrackedRepoOnHost(owner, name, host string) bool {
	if host == "" {
		host = "github.com"
	}
	s.reposMu.Lock()
	repos := s.repos
	s.reposMu.Unlock()
	for _, r := range repos {
		rHost := r.PlatformHost
		if rHost == "" {
			rHost = "github.com"
		}
		if r.Owner == owner && r.Name == name && rHost == host {
			return true
		}
	}
	return false
}

// SyncMR fetches fresh data for a single MR from GitHub and updates the DB.
// Unlike the periodic sync, this always does a full fetch (details, timeline, CI).
// Returns an error if the repo is not in the configured repo list.
func (s *Syncer) SyncMR(ctx context.Context, owner, name string, number int) error {
	return s.syncMRWithHost(ctx, owner, name, number, "")
}

// syncMRWithHost is the internal implementation of SyncMR.
// When hostHint is non-empty it is used instead of resolving via
// s.hostFor, avoiding ambiguity when the same owner/name exists on
// multiple hosts.
func (s *Syncer) syncMRWithHost(
	ctx context.Context,
	owner, name string,
	number int,
	hostHint string,
) error {
	host := hostHint
	if host == "" {
		host = s.hostFor(owner, name)
	}

	if !s.isTrackedRepoOnHost(owner, name, host) {
		return fmt.Errorf(
			"repo %s/%s on %s is not tracked", owner, name, host,
		)
	}

	repo := RepoRef{Owner: owner, Name: name, PlatformHost: host}
	client := s.clientFor(repo)

	repoID, err := s.db.UpsertRepo(ctx, host, owner, name)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", owner, name, err)
	}

	ghPR, err := client.GetPullRequest(ctx, owner, name, number)
	if err != nil {
		return fmt.Errorf("get MR %s/%s#%d: %w", owner, name, number, err)
	}
	if ghPR == nil {
		return fmt.Errorf(
			"get MR %s/%s#%d: client returned nil pull request",
			owner, name, number,
		)
	}

	normalized := NormalizePR(repoID, ghPR)

	if normalized.Author != "" && normalized.AuthorDisplayName == "" {
		existing, _ := s.db.GetMergeRequest(ctx, owner, name, number)
		if existing != nil && existing.AuthorDisplayName != "" {
			normalized.AuthorDisplayName = existing.AuthorDisplayName
		} else {
			user, userErr := client.GetUser(ctx, normalized.Author)
			if userErr == nil {
				normalized.AuthorDisplayName = sanitizeDisplayName(user.GetName())
			}
		}
	}

	mrID, err := s.db.UpsertMergeRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert MR #%d: %w", number, err)
	}

	if err := s.db.EnsureKanbanState(ctx, mrID); err != nil {
		return fmt.Errorf("ensure kanban state for MR #%d: %w", number, err)
	}

	// Run the diff sync, but don't let its failure abort the rest of SyncMR:
	// timeline and CI status are independent and the user still wants them
	// fresh. Capture the error and surface it via DiffSyncError at the end.
	diffErr := s.syncMRDiff(ctx, repo, repoID, number, ghPR, normalized)

	if err := s.refreshTimeline(ctx, repo, repoID, mrID, ghPR); err != nil {
		return fmt.Errorf("refresh timeline for MR #%d: %w", number, err)
	}

	syncMRHeadSHA := ""
	if ghPR.GetHead() != nil {
		syncMRHeadSHA = ghPR.GetHead().GetSHA()
	}
	if err := s.refreshCIStatus(ctx, repo, repoID, number, syncMRHeadSHA); err != nil {
		return err
	}

	// Update ci_had_pending after refreshing CI status.
	fresh, freshErr := s.db.GetMergeRequest(ctx, owner, name, number)
	if freshErr == nil && fresh != nil {
		pending := ciHasPending(fresh.CIChecksJSON)
		_ = s.db.UpdateMRDetailFetched(ctx, host, owner, name, number, pending)
	}

	if s.onMRSynced != nil {
		fresh, err := s.db.GetMergeRequest(ctx, owner, name, number)
		if err != nil {
			slog.Warn("get MR for onMRSynced hook in SyncMR",
				"repo", owner+"/"+name,
				"number", number,
				"err", err,
			)
		} else {
			s.onMRSynced(owner, name, fresh)
		}
	}

	if diffErr != nil {
		return diffErr
	}
	return nil
}

// syncMRDiff fetches the bare clone and computes diff SHAs for a single PR.
// Returns nil when there is no clone manager (the caller has already opted
// out of diff support); otherwise returns an error wrapping a
// *DiffSyncError that describes the first failure encountered along the
// clone or diff path. Callers can recover the structured categorization via
// errors.As.
func (s *Syncer) syncMRDiff(
	ctx context.Context, repo RepoRef, repoID int64, number int,
	ghPR *gh.PullRequest, normalized *db.MergeRequest,
) error {
	if s.clones == nil {
		return nil
	}
	host := repo.PlatformHost
	if host == "" {
		host = "github.com"
	}
	remoteURL := fmt.Sprintf("https://%s/%s/%s.git", host, repo.Owner, repo.Name)
	if err := s.clones.EnsureClone(ctx, host, repo.Owner, repo.Name, remoteURL); err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeCloneUnavailable,
			Err:  fmt.Errorf("ensure bare clone for #%d: %w", number, err),
		}
	}

	if ghPR.GetMerged() {
		// Merged MRs need special merge-base logic via the pull ref.
		// Force recomputation to repair any previously incorrect SHAs.
		return s.computeMergedMRDiffSHAs(ctx, repo, repoID, number, ghPR.GetMergeCommitSHA(), true)
	}

	if normalized.PlatformHeadSHA == "" || normalized.PlatformBaseSHA == "" {
		return nil
	}
	mb, err := s.clones.MergeBase(ctx, host, repo.Owner, repo.Name, normalized.PlatformBaseSHA, normalized.PlatformHeadSHA)
	if err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeMergeBaseFailed,
			Err:  fmt.Errorf("merge-base for #%d: %w", number, err),
		}
	}
	if err := s.db.UpdateDiffSHAs(ctx, repoID, number, normalized.PlatformHeadSHA, normalized.PlatformBaseSHA, mb); err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeInternal,
			Err:  fmt.Errorf("update diff SHAs for #%d: %w", number, err),
		}
	}
	return nil
}

// SyncIssue fetches fresh data for a single issue from GitHub and updates the DB.
// Returns an error if the repo is not in the configured repo list.
func (s *Syncer) SyncIssue(ctx context.Context, owner, name string, number int) error {
	if !s.IsTrackedRepo(owner, name) {
		return fmt.Errorf("repo %s/%s is not tracked", owner, name)
	}

	host := s.hostFor(owner, name)
	repo := RepoRef{Owner: owner, Name: name, PlatformHost: host}
	client := s.clientFor(repo)

	repoID, err := s.db.UpsertRepo(ctx, host, owner, name)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", owner, name, err)
	}

	ghIssue, err := client.GetIssue(ctx, owner, name, number)
	if err != nil {
		return fmt.Errorf("get issue %s/%s#%d: %w", owner, name, number, err)
	}

	normalized := NormalizeIssue(repoID, ghIssue)
	issueID, err := s.db.UpsertIssue(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert issue #%d: %w", number, err)
	}

	if err := s.refreshIssueTimeline(ctx, repo, issueID, ghIssue); err != nil {
		return err
	}

	_ = s.db.UpdateIssueDetailFetched(ctx, host, owner, name, number)
	return nil
}

// SyncItemByNumber fetches an item by number from GitHub, determines
// whether it is a PR or issue, syncs it into the DB, and returns the
// item type ("pr" or "issue").
// Returns an error if the repo is not in the configured repo list.
func (s *Syncer) SyncItemByNumber(
	ctx context.Context, owner, name string, number int,
) (string, error) {
	if !s.IsTrackedRepo(owner, name) {
		return "", fmt.Errorf("repo %s/%s is not tracked", owner, name)
	}

	// GitHub's Issues API returns both issues and PRs. If the
	// response has PullRequestLinks, it's a PR.
	host := s.hostFor(owner, name)
	repo := RepoRef{Owner: owner, Name: name, PlatformHost: host}
	client := s.clientFor(repo)
	ghIssue, err := client.GetIssue(ctx, owner, name, number)
	if err != nil {
		return "", fmt.Errorf(
			"get item %s/%s#%d: %w", owner, name, number, err,
		)
	}

	if ghIssue.PullRequestLinks != nil {
		if err := s.SyncMR(ctx, owner, name, number); err != nil {
			// A DiffSyncError means the PR row, timeline, and CI status
			// were upserted successfully and only the diff computation
			// failed. The item type is known, so resolution can still
			// succeed; surface the error so callers that care about diff
			// freshness can react, but report itemType so callers that
			// just need to route the user (e.g. /items/{n}/resolve) can
			// proceed.
			var diffErr *DiffSyncError
			if errors.As(err, &diffErr) {
				return "pr", err
			}
			return "", fmt.Errorf(
				"sync MR %s/%s#%d: %w", owner, name, number, err,
			)
		}
		return "pr", nil
	}

	if err := s.SyncIssue(ctx, owner, name, number); err != nil {
		return "", fmt.Errorf(
			"sync issue %s/%s#%d: %w", owner, name, number, err,
		)
	}
	return "issue", nil
}

// fetchAndUpdateClosed retrieves the final state of a now-closed PR from GitHub.
func (s *Syncer) fetchAndUpdateClosed(ctx context.Context, repo RepoRef, repoID int64, number int, cloneFetchOK bool) error {
	client := s.clientFor(repo)
	ghPR, err := client.GetPullRequest(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("get closed PR #%d: %w", number, err)
	}

	state := ghPR.GetState()
	if ghPR.GetMerged() {
		state = "merged"
	}

	var mergedAt, closedAt *time.Time
	if ghPR.MergedAt != nil {
		t := ghPR.MergedAt.Time
		mergedAt = &t
	}
	if ghPR.ClosedAt != nil {
		t := ghPR.ClosedAt.Time
		closedAt = &t
	}

	if err := s.db.UpdateClosedMRState(
		ctx, repoID, number, state,
		ghPR.GetUpdatedAt().Time,
		mergedAt, closedAt,
		ghPR.GetHead().GetSHA(), ghPR.GetBase().GetSHA(),
	); err != nil {
		return fmt.Errorf("update closed MR #%d: %w", number, err)
	}

	// Compute diff SHAs so the diff endpoint works.
	// For closed-but-not-merged PRs, use GitHub's head/base SHAs directly.
	// For merged PRs, use merge-base(merge_commit^1, refs/pull/<number>/head)
	// to find the fork point. This works for all merge strategies because ^1
	// is always a pre-merge commit on the base branch lineage, and the pull
	// ref always points to the original PR head. We only do this when no diff
	// SHAs exist yet; PRs synced while open already have valid diff SHAs.
	closedHost := repo.PlatformHost
	if closedHost == "" {
		closedHost = "github.com"
	}
	if s.clones != nil && cloneFetchOK {
		headSHA := ghPR.GetHead().GetSHA()
		baseSHA := ghPR.GetBase().GetSHA()

		if ghPR.GetMerged() {
			if err := s.computeMergedMRDiffSHAs(ctx, repo, repoID, number, ghPR.GetMergeCommitSHA(), false); err != nil {
				slog.Warn("compute merged PR diff SHAs failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", number, "err", err,
				)
			}
		} else if headSHA != "" && baseSHA != "" {
			mb, err := s.clones.MergeBase(ctx, closedHost, repo.Owner, repo.Name, baseSHA, headSHA)
			if err != nil {
				slog.Warn("merge-base for closed PR failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", number, "err", err,
				)
			} else {
				if err := s.db.UpdateDiffSHAs(ctx, repoID, number, headSHA, baseSHA, mb); err != nil {
					slog.Warn("update diff SHAs for closed PR failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", number, "err", err,
					)
				}
			}
		}
	}
	return nil
}

// computeMergedMRDiffSHAs computes diff SHAs for a merged PR.
// Uses merge-base(merge_commit^1, refs/pull/<number>/head) which works for all
// GitHub merge strategies:
//   - Merge commit: ^1 is the pre-merge base tip
//   - Squash: ^1 is the pre-squash base tip
//   - Rebase: ^1 is the previous rebased commit
//
// In all cases, merge-base with the original PR head (from the pull ref)
// correctly identifies the fork point.
//
// When force is false, skips PRs that already have diff SHAs (periodic sync).
// When force is true, always recomputes (on-demand SyncMR).
//
// Returns a *DiffSyncError (wrapped as an error) describing the failure when
// any git or DB operation fails. A nil return covers both success and the
// no-op skip cases (empty merge SHA, existing valid diff SHAs without force).
func (s *Syncer) computeMergedMRDiffSHAs(
	ctx context.Context, repo RepoRef, repoID int64, number int, mergeCommitSHA string,
	force bool,
) error {
	if mergeCommitSHA == "" {
		return nil
	}

	if !force {
		existing, err := s.db.GetDiffSHAs(ctx, repo.Owner, repo.Name, number)
		if err != nil {
			return &DiffSyncError{
				Code: DiffSyncCodeInternal,
				Err:  fmt.Errorf("get diff SHAs for merged PR #%d: %w", number, err),
			}
		}
		if existing == nil || existing.DiffHeadSHA != "" {
			return nil // already has diff SHAs or PR not found
		}
	}

	// Resolve the PR head from the pull ref. GitHub keeps these refs
	// indefinitely, pointing to the original PR head commit regardless
	// of merge strategy.
	mergedHost := repo.PlatformHost
	if mergedHost == "" {
		mergedHost = "github.com"
	}
	pullRef := fmt.Sprintf("refs/pull/%d/head", number)
	prHead, err := s.clones.RevParse(ctx, mergedHost, repo.Owner, repo.Name, pullRef)
	if err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeCommitUnreachable,
			Err:  fmt.Errorf("rev-parse %s for merged PR #%d: %w", pullRef, number, err),
		}
	}

	// Use the merge commit's first parent as the base for merge-base.
	// This avoids the post-merge ancestor problem where prHead is reachable
	// from the current base branch tip (making merge-base return prHead).
	preMergeBase, err := s.clones.RevParse(ctx, mergedHost, repo.Owner, repo.Name, mergeCommitSHA+"^1")
	if err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeCommitUnreachable,
			Err:  fmt.Errorf("rev-parse %s^1 for merged PR #%d: %w", mergeCommitSHA, number, err),
		}
	}

	mb, err := s.clones.MergeBase(ctx, mergedHost, repo.Owner, repo.Name, preMergeBase, prHead)
	if err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeMergeBaseFailed,
			Err:  fmt.Errorf("merge-base for merged PR #%d: %w", number, err),
		}
	}

	if prHead == "" || mb == "" {
		return nil
	}

	if err := s.db.UpdateDiffSHAs(ctx, repoID, number, prHead, mb, mb); err != nil {
		return &DiffSyncError{
			Code: DiffSyncCodeInternal,
			Err:  fmt.Errorf("update diff SHAs for merged PR #%d: %w", number, err),
		}
	}
	return nil
}
