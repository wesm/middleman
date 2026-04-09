package github

import (
	"context"
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
	clients          map[string]Client // host -> client
	db               *db.DB
	clones           *gitclone.Manager
	rateTrackers     map[string]*RateTracker // host -> tracker
	repos            []RepoRef
	reposMu          sync.Mutex
	interval         time.Duration
	watchInterval    time.Duration
	watchedMRs       []WatchedMR
	watchMu          sync.Mutex
	parallelism      atomic.Int32
	running          atomic.Bool
	status           atomic.Value // stores *SyncStatus
	stopCh           chan struct{}
	stopOnce         sync.Once
	wg               sync.WaitGroup
	displayNames     map[string]string // "host\x00login" -> display name, per sync run
	displayNamesMu   sync.Mutex
	displayNameGroup singleflight.Group // dedups concurrent GetUser calls
	onMRSynced       func(owner, name string, mr *db.MergeRequest)
	onSyncCompleted  func(results []RepoSyncResult)
}

// NewSyncer creates a Syncer that polls the given repos on the
// given interval. clients maps host -> Client; rateTrackers maps
// host -> RateTracker. Both may contain nil values. clones may
// be nil.
func NewSyncer(
	clients map[string]Client,
	database *db.DB,
	clones *gitclone.Manager,
	repos []RepoRef,
	interval time.Duration,
	rateTrackers map[string]*RateTracker,
) *Syncer {
	if clients == nil {
		clients = make(map[string]Client)
	}
	if rateTrackers == nil {
		rateTrackers = make(map[string]*RateTracker)
	}
	s := &Syncer{
		clients:      clients,
		db:           database,
		clones:       clones,
		rateTrackers: rateTrackers,
		repos:        repos,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
	s.parallelism.Store(defaultParallelism)
	s.status.Store(&SyncStatus{})
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
func (s *Syncer) Start(ctx context.Context) {
	s.wg.Go(func() {
		s.RunOnce(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RunOnce(ctx)
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	})

	watchInt := s.watchInterval
	if watchInt <= 0 {
		watchInt = 30 * time.Second
	}
	s.wg.Go(func() {
		ticker := time.NewTicker(watchInt)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.syncWatchedMRs(ctx)
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	})
}

// syncWatchedMRs syncs each MR on the watch list via SyncMR.
// Fires onMRSynced (inside SyncMR) but not onSyncCompleted.
// Checks per-host rate limits before issuing API calls.
func (s *Syncer) syncWatchedMRs(ctx context.Context) {
	s.watchMu.Lock()
	mrs := make([]WatchedMR, len(s.watchedMRs))
	copy(mrs, s.watchedMRs)
	s.watchMu.Unlock()

	if len(mrs) == 0 {
		return
	}

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
}

// Stop signals the background goroutine to exit. Safe to call multiple times.
func (s *Syncer) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

// Status returns a snapshot of the current sync state.
func (s *Syncer) Status() *SyncStatus {
	return s.status.Load().(*SyncStatus)
}

// runState holds the per-RunOnce mutable state shared by the
// worker pool. Extracted into a struct so runWorker can be a
// directly testable method instead of an inline closure.
type runState struct {
	completed *atomic.Int32
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
				s.status.Store(&SyncStatus{
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
		s.status.Store(&SyncStatus{
			Running:     true,
			CurrentRepo: repoName,
			Progress:    fmt.Sprintf("%d/%d", done, state.total),
		})
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
	s.status.Store(&SyncStatus{
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

	var (
		completed atomic.Int32
		errMu     sync.Mutex
		lastErr   string
		canceled  atomic.Bool
		wg        sync.WaitGroup
	)

	state := &runState{
		completed: &completed,
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
		s.status.Store(&SyncStatus{
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

	s.status.Store(&SyncStatus{
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

	syncErr := s.doSyncRepo(ctx, repo, repoID, cloneFetchOK)

	syncErrStr := ""
	if syncErr != nil {
		syncErrStr = syncErr.Error()
	}
	if err := s.db.UpdateRepoSyncCompleted(ctx, repoID, time.Now(), syncErrStr); err != nil {
		slog.Error("mark sync completed", "repo", repo.Owner+"/"+repo.Name, "err", err)
	}

	return syncErr
}

// doSyncRepo performs the actual GitHub API calls and DB writes for one repo.
func (s *Syncer) doSyncRepo(ctx context.Context, repo RepoRef, repoID int64, cloneFetchOK bool) error {
	client := s.clientFor(repo)
	ghPRs, err := client.ListOpenPullRequests(ctx, repo.Owner, repo.Name)
	if err != nil {
		return fmt.Errorf("list open PRs: %w", err)
	}

	stillOpen := make(map[int]bool, len(ghPRs))
	for _, ghPR := range ghPRs {
		stillOpen[ghPR.GetNumber()] = true
	}

	for _, ghPR := range ghPRs {
		if err := s.syncOpenMR(ctx, repo, repoID, ghPR, cloneFetchOK); err != nil {
			slog.Error("sync MR failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghPR.GetNumber(),
				"err", err,
			)
		}
	}

	// Handle MRs that were open in the DB but are no longer in the open list.
	closedNumbers, err := s.db.GetPreviouslyOpenMRNumbers(ctx, repoID, stillOpen)
	if err != nil {
		return fmt.Errorf("get previously open MRs: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosed(ctx, repo, repoID, number, cloneFetchOK); err != nil {
			slog.Error("update closed MR failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
		}
	}

	// Sync issues (non-fatal — issue failure should not block PR sync)
	if err := s.syncIssues(ctx, repo, repoID); err != nil {
		slog.Error("sync issues failed",
			"repo", repo.Owner+"/"+repo.Name, "err", err,
		)
	}

	return nil
}

// syncOpenMR upserts a single open MR and, if the data has changed,
// refreshes its timeline events and derived fields.
func (s *Syncer) syncOpenMR(ctx context.Context, repo RepoRef, repoID int64, ghPR *gh.PullRequest, cloneFetchOK bool) error {
	normalized := NormalizePR(repoID, ghPR)

	// Check whether we already have this MR and whether it has changed.
	existing, err := s.db.GetMergeRequest(ctx, repo.Owner, repo.Name, ghPR.GetNumber())
	if err != nil {
		return fmt.Errorf("get existing MR #%d: %w", ghPR.GetNumber(), err)
	}

	needsTimeline := existing == nil || !existing.UpdatedAt.Equal(normalized.UpdatedAt)

	// Also fetch full PR details if we have stale zero diff stats or unknown mergeable state.
	needsFullFetch := needsTimeline ||
		(existing != nil && existing.Additions == 0 && existing.Deletions == 0) ||
		(existing != nil && existing.MergeableState == "") ||
		(existing != nil && existing.MergeableState == "unknown")

	// The list endpoint doesn't return diff stats. Fetch the individual
	// PR when data is new/changed or diff stats are missing.
	client := s.clientFor(repo)
	if needsFullFetch {
		fullPR, err := client.GetPullRequest(ctx, repo.Owner, repo.Name, ghPR.GetNumber())
		// Treat (nil, nil) as a transient fetch failure rather
		// than a panic. A misbehaving Client returning nil
		// without an error is a contract violation, but the
		// periodic sync should keep going with the list-derived
		// data instead of crashing the worker.
		if err == nil && fullPR == nil {
			err = fmt.Errorf("client returned nil pull request")
		}
		if err != nil {
			slog.Warn("get full PR for diff stats failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghPR.GetNumber(),
				"err", err,
			)
			// Preserve fields the list endpoint doesn't return
			// so a transient fetch failure doesn't wipe cached data.
			if existing != nil {
				normalized.Additions = existing.Additions
				normalized.Deletions = existing.Deletions
				normalized.MergeableState = existing.MergeableState
			}
		} else {
			ghPR = fullPR
			normalized = NormalizePR(repoID, ghPR)
		}
	} else if existing != nil {
		// Preserve fields the list endpoint doesn't return
		normalized.Additions = existing.Additions
		normalized.Deletions = existing.Deletions
		normalized.MergeableState = existing.MergeableState
	}

	if normalized.Author != "" && normalized.AuthorDisplayName == "" {
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if name, ok := s.resolveDisplayName(ctx, client, host, normalized.Author); ok {
			normalized.AuthorDisplayName = name
		} else if existing != nil {
			normalized.AuthorDisplayName = existing.AuthorDisplayName
		}
	}

	mrID, err := s.db.UpsertMergeRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert MR #%d: %w", ghPR.GetNumber(), err)
	}

	if err := s.db.EnsureKanbanState(ctx, mrID); err != nil {
		return fmt.Errorf("ensure kanban state for MR #%d: %w", ghPR.GetNumber(), err)
	}

	// Compute diff SHAs if clone is available and fetch succeeded.
	repoHost := repo.PlatformHost
	if repoHost == "" {
		repoHost = "github.com"
	}
	if s.clones != nil && cloneFetchOK {
		headSHA := normalized.PlatformHeadSHA
		baseSHA := normalized.PlatformBaseSHA
		if headSHA != "" && baseSHA != "" {
			mb, err := s.clones.MergeBase(ctx, repoHost, repo.Owner, repo.Name, baseSHA, headSHA)
			if err != nil {
				slog.Warn("merge-base computation failed",
					"repo", repo.Owner+"/"+repo.Name,
					"number", ghPR.GetNumber(),
					"err", err,
				)
			} else {
				if err := s.db.UpdateDiffSHAs(ctx, repoID, ghPR.GetNumber(), headSHA, baseSHA, mb); err != nil {
					slog.Warn("update diff SHAs failed",
						"repo", repo.Owner+"/"+repo.Name,
						"number", ghPR.GetNumber(),
						"err", err,
					)
				}
			}
		}
	}

	if needsTimeline {
		if err := s.refreshTimeline(ctx, repo, repoID, mrID, ghPR); err != nil {
			return err
		}
	}

	// Always refresh CI status — check runs change independently of the
	// MR's updated_at field, so pending/in-progress checks would be missed
	// if we only fetched them when the MR itself changed.
	if err := s.refreshCIStatus(ctx, repo, repoID, ghPR); err != nil {
		return err
	}

	// Fire the hook after all derived fields (ReviewDecision, CIStatus)
	// are persisted so the callback receives up-to-date state.
	if s.onMRSynced != nil {
		fresh, err := s.db.GetMergeRequest(
			ctx, repo.Owner, repo.Name, ghPR.GetNumber(),
		)
		if err != nil {
			slog.Warn("get MR for onMRSynced hook failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghPR.GetNumber(),
				"err", err,
			)
		} else {
			s.onMRSynced(repo.Owner, repo.Name, fresh)
		}
	}

	return nil
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
		return fmt.Errorf("list force-push events for MR #%d: %w", number, err)
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
// of the PR's updated_at field.
func (s *Syncer) refreshCIStatus(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	ghPR *gh.PullRequest,
) error {
	headSHA := ""
	if ghPR.GetHead() != nil {
		headSHA = ghPR.GetHead().GetSHA()
	}
	if headSHA == "" {
		return nil
	}

	number := ghPR.GetNumber()

	// Fetch both sources. On failure, skip the DB write to preserve
	// existing data rather than wiping it with empty values.
	client := s.clientFor(repo)
	combined, err := client.GetCombinedStatus(ctx, repo.Owner, repo.Name, headSHA)
	if err != nil {
		slog.Warn("get combined status failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number,
			"err", err,
		)
		return nil
	}

	checkRuns, err := client.ListCheckRunsForRef(ctx, repo.Owner, repo.Name, headSHA)
	if err != nil {
		slog.Warn("list check runs failed",
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

func (s *Syncer) syncIssues(
	ctx context.Context, repo RepoRef, repoID int64,
) error {
	client := s.clientFor(repo)
	ghIssues, err := client.ListOpenIssues(
		ctx, repo.Owner, repo.Name,
	)
	if err != nil {
		return fmt.Errorf("list open issues: %w", err)
	}

	stillOpen := make(map[int]bool, len(ghIssues))
	for _, issue := range ghIssues {
		stillOpen[issue.GetNumber()] = true
	}

	for _, ghIssue := range ghIssues {
		if err := s.syncOpenIssue(ctx, repo, repoID, ghIssue); err != nil {
			slog.Error("sync issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghIssue.GetNumber(),
				"err", err,
			)
		}
	}

	closedNumbers, err := s.db.GetPreviouslyOpenIssueNumbers(
		ctx, repoID, stillOpen,
	)
	if err != nil {
		return fmt.Errorf("get previously open issues: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosedIssue(ctx, repo, repoID, number); err != nil {
			slog.Error("update closed issue failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", number,
				"err", err,
			)
		}
	}

	return nil
}

func (s *Syncer) syncOpenIssue(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	ghIssue *gh.Issue,
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

	needsTimeline := existing == nil ||
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
		// Resolve directly instead of using s.resolveDisplayName to
		// avoid racing with the shared displayNames map in RunOnce.
		user, userErr := client.GetUser(ctx, normalized.Author)
		if userErr == nil {
			normalized.AuthorDisplayName = sanitizeDisplayName(user.GetName())
		} else if existing != nil {
			normalized.AuthorDisplayName = existing.AuthorDisplayName
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

	if err := s.refreshCIStatus(ctx, repo, repoID, ghPR); err != nil {
		return err
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

	return s.refreshIssueTimeline(ctx, repo, issueID, ghIssue)
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
