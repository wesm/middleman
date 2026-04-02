package github

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
)

// SyncStatus holds the current state of the sync engine.
type SyncStatus struct {
	Running     bool      `json:"running"`
	CurrentRepo string    `json:"current_repo,omitempty"`
	Progress    string    `json:"progress,omitempty"`
	LastRunAt   time.Time `json:"last_run_at,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
}

// RepoRef identifies a GitHub repository.
type RepoRef struct {
	Owner string
	Name  string
}

// Syncer periodically pulls PR data from GitHub into SQLite.
type Syncer struct {
	client       Client
	db           *db.DB
	repos        []RepoRef
	reposMu      sync.Mutex
	interval     time.Duration
	running      atomic.Bool
	status       atomic.Value // stores *SyncStatus
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
	displayNames map[string]string // login -> display name, per sync run
}

// NewSyncer creates a Syncer that polls the given repos on the given interval.
func NewSyncer(client Client, database *db.DB, repos []RepoRef, interval time.Duration) *Syncer {
	s := &Syncer{
		client:   client,
		db:       database,
		repos:    repos,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	s.status.Store(&SyncStatus{})
	return s
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

// RunOnce performs a single sync pass across all configured repos.
// If a sync is already in progress it returns immediately (single-flight).
func (s *Syncer) RunOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	s.reposMu.Lock()
	repos := make([]RepoRef, len(s.repos))
	copy(repos, s.repos)
	s.reposMu.Unlock()

	s.status.Store(&SyncStatus{Running: true})
	s.displayNames = make(map[string]string)
	slog.Info("sync started", "repos", len(repos))

	var lastErr string
	for i, repo := range repos {
		progress := fmt.Sprintf("%d/%d", i+1, len(repos))
		repoName := repo.Owner + "/" + repo.Name
		s.status.Store(&SyncStatus{
			Running:     true,
			CurrentRepo: repoName,
			Progress:    progress,
		})
		slog.Info("syncing repo",
			"repo", repoName,
			"progress", progress,
		)
		if err := s.syncRepo(ctx, repo); err != nil {
			slog.Error("sync repo failed", "repo", repoName, "err", err)
			lastErr = err.Error()
		}
	}

	slog.Info("sync complete", "repos", len(repos))
	s.status.Store(&SyncStatus{
		Running:   false,
		LastRunAt: time.Now(),
		LastError: lastErr,
	})
}

// syncRepo syncs one repository: open PRs, timeline events, and stale closures.
func (s *Syncer) syncRepo(ctx context.Context, repo RepoRef) error {
	repoID, err := s.db.UpsertRepo(ctx, repo.Owner, repo.Name)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", repo.Owner, repo.Name, err)
	}

	ghRepo, err := s.client.GetRepository(ctx, repo.Owner, repo.Name)
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

	syncErr := s.doSyncRepo(ctx, repo, repoID)

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
func (s *Syncer) doSyncRepo(ctx context.Context, repo RepoRef, repoID int64) error {
	ghPRs, err := s.client.ListOpenPullRequests(ctx, repo.Owner, repo.Name)
	if err != nil {
		return fmt.Errorf("list open PRs: %w", err)
	}

	stillOpen := make(map[int]bool, len(ghPRs))
	for _, ghPR := range ghPRs {
		stillOpen[ghPR.GetNumber()] = true
	}

	for _, ghPR := range ghPRs {
		if err := s.syncOpenPR(ctx, repo, repoID, ghPR); err != nil {
			slog.Error("sync PR failed",
				"repo", repo.Owner+"/"+repo.Name,
				"number", ghPR.GetNumber(),
				"err", err,
			)
		}
	}

	// Handle PRs that were open in the DB but are no longer in the open list.
	closedNumbers, err := s.db.GetPreviouslyOpenPRNumbers(ctx, repoID, stillOpen)
	if err != nil {
		return fmt.Errorf("get previously open PRs: %w", err)
	}
	for _, number := range closedNumbers {
		if err := s.fetchAndUpdateClosed(ctx, repo, repoID, number); err != nil {
			slog.Error("update closed PR failed",
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

// syncOpenPR upserts a single open PR and, if the data has changed,
// refreshes its timeline events and derived fields.
func (s *Syncer) syncOpenPR(ctx context.Context, repo RepoRef, repoID int64, ghPR *gh.PullRequest) error {
	normalized := NormalizePR(repoID, ghPR)

	// Check whether we already have this PR and whether it has changed.
	existing, err := s.db.GetPullRequest(ctx, repo.Owner, repo.Name, ghPR.GetNumber())
	if err != nil {
		return fmt.Errorf("get existing PR #%d: %w", ghPR.GetNumber(), err)
	}

	needsTimeline := existing == nil || !existing.UpdatedAt.Equal(normalized.UpdatedAt)

	// Also fetch full PR details if we have stale zero diff stats or unknown mergeable state.
	needsFullFetch := needsTimeline ||
		(existing != nil && existing.Additions == 0 && existing.Deletions == 0) ||
		(existing != nil && existing.MergeableState == "") ||
		(existing != nil && existing.MergeableState == "unknown")

	// The list endpoint doesn't return diff stats. Fetch the individual
	// PR when data is new/changed or diff stats are missing.
	if needsFullFetch {
		fullPR, err := s.client.GetPullRequest(ctx, repo.Owner, repo.Name, ghPR.GetNumber())
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
		if name, ok := s.resolveDisplayName(ctx, normalized.Author); ok {
			normalized.AuthorDisplayName = name
		} else if existing != nil {
			normalized.AuthorDisplayName = existing.AuthorDisplayName
		}
	}

	prID, err := s.db.UpsertPullRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert PR #%d: %w", ghPR.GetNumber(), err)
	}

	if err := s.db.EnsureKanbanState(ctx, prID); err != nil {
		return fmt.Errorf("ensure kanban state for PR #%d: %w", ghPR.GetNumber(), err)
	}

	if needsTimeline {
		if err := s.refreshTimeline(ctx, repo, repoID, prID, ghPR); err != nil {
			return err
		}
	}

	// Always refresh CI status — check runs change independently of the
	// PR's updated_at field, so pending/in-progress checks would be missed
	// if we only fetched them when the PR itself changed.
	return s.refreshCIStatus(ctx, repo, repoID, ghPR)
}

// refreshTimeline fetches comments, reviews, and commits for a PR and
// updates its derived fields (ReviewDecision, CommentCount, LastActivityAt, CIStatus).
func (s *Syncer) refreshTimeline(
	ctx context.Context,
	repo RepoRef,
	repoID int64,
	prID int64,
	ghPR *gh.PullRequest,
) error {
	number := ghPR.GetNumber()

	comments, err := s.client.ListIssueComments(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list comments for PR #%d: %w", number, err)
	}

	reviews, err := s.client.ListReviews(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list reviews for PR #%d: %w", number, err)
	}

	commits, err := s.client.ListCommits(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		return fmt.Errorf("list commits for PR #%d: %w", number, err)
	}

	var events []db.PREvent
	for _, c := range comments {
		events = append(events, NormalizeCommentEvent(prID, c))
	}
	for _, r := range reviews {
		events = append(events, NormalizeReviewEvent(prID, r))
	}
	for _, c := range commits {
		events = append(events, NormalizeCommitEvent(prID, c))
	}

	if err := s.db.UpsertPREvents(ctx, events); err != nil {
		return fmt.Errorf("upsert events for PR #%d: %w", number, err)
	}

	reviewDecision := DeriveReviewDecision(reviews)
	lastActivityAt := computeLastActivity(ghPR, comments, reviews, commits)

	return s.db.UpdatePRDerivedFields(ctx, repoID, number, db.PRDerivedFields{
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
	combined, err := s.client.GetCombinedStatus(ctx, repo.Owner, repo.Name, headSHA)
	if err != nil {
		slog.Warn("get combined status failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number,
			"err", err,
		)
		return nil
	}

	checkRuns, err := s.client.ListCheckRunsForRef(ctx, repo.Owner, repo.Name, headSHA)
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

	return s.db.UpdatePRCIStatus(ctx, repoID, number, ciStatus, ciChecksJSON)
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
// callers can preserve existing data. Uses an in-memory cache to avoid
// duplicate API calls within a sync run.
func (s *Syncer) resolveDisplayName(
	ctx context.Context, login string,
) (string, bool) {
	if name, ok := s.displayNames[login]; ok {
		return name, true
	}
	user, err := s.client.GetUser(ctx, login)
	if err != nil {
		slog.Warn("get user display name failed",
			"login", login, "err", err,
		)
		return "", false
	}
	name := sanitizeDisplayName(user.GetName())
	s.displayNames[login] = name
	return name, true
}

// --- Issue sync ---

func (s *Syncer) syncIssues(
	ctx context.Context, repo RepoRef, repoID int64,
) error {
	ghIssues, err := s.client.ListOpenIssues(
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

	comments, err := s.client.ListIssueComments(
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
		`UPDATE issues SET comment_count = ?, last_activity_at = ?
		 WHERE id = ?`,
		len(comments), lastActivity, issueID,
	)
	return err
}

func (s *Syncer) fetchAndUpdateClosedIssue(
	ctx context.Context, repo RepoRef, repoID int64, number int,
) error {
	ghIssue, err := s.client.GetIssue(
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

// SyncPR fetches fresh data for a single PR from GitHub and updates the DB.
// Unlike the periodic sync, this always does a full fetch (details, timeline, CI).
// Returns an error if the repo is not in the configured repo list.
func (s *Syncer) SyncPR(ctx context.Context, owner, name string, number int) error {
	if !s.IsTrackedRepo(owner, name) {
		return fmt.Errorf("repo %s/%s is not tracked", owner, name)
	}

	repo := RepoRef{Owner: owner, Name: name}

	repoID, err := s.db.UpsertRepo(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", owner, name, err)
	}

	ghPR, err := s.client.GetPullRequest(ctx, owner, name, number)
	if err != nil {
		return fmt.Errorf("get PR %s/%s#%d: %w", owner, name, number, err)
	}

	normalized := NormalizePR(repoID, ghPR)

	if normalized.Author != "" && normalized.AuthorDisplayName == "" {
		existing, _ := s.db.GetPullRequest(ctx, owner, name, number)
		// Resolve directly instead of using s.resolveDisplayName to
		// avoid racing with the shared displayNames map in RunOnce.
		user, userErr := s.client.GetUser(ctx, normalized.Author)
		if userErr == nil {
			normalized.AuthorDisplayName = sanitizeDisplayName(user.GetName())
		} else if existing != nil {
			normalized.AuthorDisplayName = existing.AuthorDisplayName
		}
	}

	prID, err := s.db.UpsertPullRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert PR #%d: %w", number, err)
	}

	if err := s.db.EnsureKanbanState(ctx, prID); err != nil {
		return fmt.Errorf("ensure kanban state for PR #%d: %w", number, err)
	}

	if err := s.refreshTimeline(ctx, repo, repoID, prID, ghPR); err != nil {
		return fmt.Errorf("refresh timeline for PR #%d: %w", number, err)
	}

	return s.refreshCIStatus(ctx, repo, repoID, ghPR)
}

// SyncIssue fetches fresh data for a single issue from GitHub and updates the DB.
// Returns an error if the repo is not in the configured repo list.
func (s *Syncer) SyncIssue(ctx context.Context, owner, name string, number int) error {
	if !s.IsTrackedRepo(owner, name) {
		return fmt.Errorf("repo %s/%s is not tracked", owner, name)
	}

	repo := RepoRef{Owner: owner, Name: name}

	repoID, err := s.db.UpsertRepo(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", owner, name, err)
	}

	ghIssue, err := s.client.GetIssue(ctx, owner, name, number)
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

// fetchAndUpdateClosed retrieves the final state of a now-closed PR from GitHub.
func (s *Syncer) fetchAndUpdateClosed(ctx context.Context, repo RepoRef, repoID int64, number int) error {
	ghPR, err := s.client.GetPullRequest(ctx, repo.Owner, repo.Name, number)
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

	if err := s.db.UpdatePRState(ctx, repoID, number, state, mergedAt, closedAt); err != nil {
		return fmt.Errorf("update PR state for #%d: %w", number, err)
	}
	return nil
}
