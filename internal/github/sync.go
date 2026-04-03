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
	"github.com/wesm/middleman/internal/gitclone"
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
	clones       *gitclone.Manager
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
// clones may be nil if bare clone management is not configured.
func NewSyncer(client Client, database *db.DB, clones *gitclone.Manager, repos []RepoRef, interval time.Duration) *Syncer {
	s := &Syncer{
		client:   client,
		db:       database,
		clones:   clones,
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

	// Fetch bare clone before PR data so refs are available for merge-base.
	cloneFetchOK := false
	if s.clones != nil {
		remoteURL := fmt.Sprintf("https://github.com/%s/%s.git", repo.Owner, repo.Name)
		if err := s.clones.EnsureClone(ctx, repo.Owner, repo.Name, remoteURL); err != nil {
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
	ghPRs, err := s.client.ListOpenPullRequests(ctx, repo.Owner, repo.Name)
	if err != nil {
		return fmt.Errorf("list open PRs: %w", err)
	}

	stillOpen := make(map[int]bool, len(ghPRs))
	for _, ghPR := range ghPRs {
		stillOpen[ghPR.GetNumber()] = true
	}

	for _, ghPR := range ghPRs {
		if err := s.syncOpenPR(ctx, repo, repoID, ghPR, cloneFetchOK); err != nil {
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
		if err := s.fetchAndUpdateClosed(ctx, repo, repoID, number, cloneFetchOK); err != nil {
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
func (s *Syncer) syncOpenPR(ctx context.Context, repo RepoRef, repoID int64, ghPR *gh.PullRequest, cloneFetchOK bool) error {
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

	// Compute diff SHAs if clone is available and fetch succeeded.
	if s.clones != nil && cloneFetchOK {
		headSHA := normalized.GitHubHeadSHA
		baseSHA := normalized.GitHubBaseSHA
		if headSHA != "" && baseSHA != "" {
			mb, err := s.clones.MergeBase(ctx, repo.Owner, repo.Name, baseSHA, headSHA)
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
	ghIssue, err := s.client.GetIssue(ctx, owner, name, number)
	if err != nil {
		return "", fmt.Errorf(
			"get item %s/%s#%d: %w", owner, name, number, err,
		)
	}

	if ghIssue.PullRequestLinks != nil {
		if err := s.SyncPR(ctx, owner, name, number); err != nil {
			return "", fmt.Errorf(
				"sync PR %s/%s#%d: %w", owner, name, number, err,
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

	if err := s.db.UpdateClosedPRState(
		ctx, repoID, number, state,
		ghPR.GetUpdatedAt().Time,
		mergedAt, closedAt,
		ghPR.GetHead().GetSHA(), ghPR.GetBase().GetSHA(),
	); err != nil {
		return fmt.Errorf("update closed PR #%d: %w", number, err)
	}

	// Compute diff SHAs so the diff endpoint works.
	// For closed-but-not-merged PRs, use GitHub's head/base SHAs directly.
	// For merged PRs, use merge-base(merge_commit^1, refs/pull/<number>/head)
	// to find the fork point. This works for all merge strategies because ^1
	// is always a pre-merge commit on the base branch lineage, and the pull
	// ref always points to the original PR head. We only do this when no diff
	// SHAs exist yet; PRs synced while open already have valid diff SHAs.
	if s.clones != nil && cloneFetchOK {
		headSHA := ghPR.GetHead().GetSHA()
		baseSHA := ghPR.GetBase().GetSHA()

		if ghPR.GetMerged() {
			s.computeMergedPRDiffSHAs(ctx, repo, repoID, number, ghPR.GetMergeCommitSHA())
		} else if headSHA != "" && baseSHA != "" {
			mb, err := s.clones.MergeBase(ctx, repo.Owner, repo.Name, baseSHA, headSHA)
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

// computeMergedPRDiffSHAs computes diff SHAs for a merged PR that lacks them.
// Uses merge-base(merge_commit^1, refs/pull/<number>/head) which works for all
// GitHub merge strategies:
//   - Merge commit: ^1 is the pre-merge base tip
//   - Squash: ^1 is the pre-squash base tip
//   - Rebase: ^1 is the previous rebased commit
//
// In all cases, merge-base with the original PR head (from the pull ref)
// correctly identifies the fork point.
func (s *Syncer) computeMergedPRDiffSHAs(
	ctx context.Context, repo RepoRef, repoID int64, number int, mergeCommitSHA string,
) {
	if mergeCommitSHA == "" {
		return
	}

	existing, err := s.db.GetDiffSHAs(ctx, repo.Owner, repo.Name, number)
	if err != nil {
		slog.Warn("get diff SHAs for merged PR failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number, "err", err,
		)
		return
	}
	if existing == nil || existing.DiffHeadSHA != "" {
		return // already has diff SHAs or PR not found
	}

	// Resolve the PR head from the pull ref. GitHub keeps these refs
	// indefinitely, pointing to the original PR head commit regardless
	// of merge strategy.
	pullRef := fmt.Sprintf("refs/pull/%d/head", number)
	prHead, err := s.clones.RevParse(ctx, repo.Owner, repo.Name, pullRef)
	if err != nil {
		slog.Warn("rev-parse pull ref for merged PR failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number, "ref", pullRef, "err", err,
		)
		return
	}

	// Use the merge commit's first parent as the base for merge-base.
	// This avoids the post-merge ancestor problem where prHead is reachable
	// from the current base branch tip (making merge-base return prHead).
	preMergeBase, err := s.clones.RevParse(ctx, repo.Owner, repo.Name, mergeCommitSHA+"^1")
	if err != nil {
		slog.Warn("rev-parse merge commit parent failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number, "merge_commit", mergeCommitSHA, "err", err,
		)
		return
	}

	mb, err := s.clones.MergeBase(ctx, repo.Owner, repo.Name, preMergeBase, prHead)
	if err != nil {
		slog.Warn("merge-base for merged PR failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number, "err", err,
		)
		return
	}

	if prHead == "" || mb == "" {
		return
	}

	if err := s.db.UpdateDiffSHAs(ctx, repoID, number, prHead, mb, mb); err != nil {
		slog.Warn("update diff SHAs for merged PR failed",
			"repo", repo.Owner+"/"+repo.Name,
			"number", number, "err", err,
		)
	}
}
