package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// --- Repos ---

// UpsertRepo inserts a repo if it does not exist, then returns its ID.
func (d *DB) UpsertRepo(ctx context.Context, owner, name string) (int64, error) {
	_, err := d.rw.ExecContext(ctx,
		`INSERT INTO middleman_repos (platform, platform_host, owner, name)
		 VALUES ('github', 'github.com', ?, ?)
		 ON CONFLICT(platform, platform_host, owner, name) DO NOTHING`,
		owner, name,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert repo: %w", err)
	}
	var id int64
	err = d.ro.QueryRowContext(ctx,
		`SELECT id FROM middleman_repos WHERE platform = 'github' AND platform_host = 'github.com' AND owner = ? AND name = ?`, owner, name,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get repo id after upsert: %w", err)
	}
	return id, nil
}

// ListRepos returns all repos ordered by owner, name.
func (d *DB) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT id, platform, platform_host, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, allow_squash_merge, allow_merge_commit,
		        allow_rebase_merge, created_at
		 FROM middleman_repos ORDER BY owner, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(
			&r.ID, &r.Platform, &r.PlatformHost, &r.Owner, &r.Name,
			&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
			&r.LastSyncError,
			&r.AllowSquashMerge, &r.AllowMergeCommit, &r.AllowRebaseMerge,
			&r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// UpdateRepoSyncStarted records the time a sync began.
func (d *DB) UpdateRepoSyncStarted(ctx context.Context, id int64, t time.Time) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE middleman_repos SET last_sync_started_at = ? WHERE id = ?`, t, id,
	)
	if err != nil {
		return fmt.Errorf("update repo sync started: %w", err)
	}
	return nil
}

// UpdateRepoSyncCompleted records the time and optional error a sync finished.
func (d *DB) UpdateRepoSyncCompleted(ctx context.Context, id int64, t time.Time, syncErr string) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE middleman_repos SET last_sync_completed_at = ?, last_sync_error = ? WHERE id = ?`,
		t, syncErr, id,
	)
	if err != nil {
		return fmt.Errorf("update repo sync completed: %w", err)
	}
	return nil
}

// GetRepoByOwnerName returns the repo for the given owner/name, or nil if not found.
func (d *DB) GetRepoByOwnerName(ctx context.Context, owner, name string) (*Repo, error) {
	var r Repo
	err := d.ro.QueryRowContext(ctx,
		`SELECT id, platform, platform_host, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, allow_squash_merge, allow_merge_commit,
		        allow_rebase_merge, created_at
		 FROM middleman_repos WHERE platform = 'github' AND platform_host = 'github.com' AND owner = ? AND name = ?`, owner, name,
	).Scan(
		&r.ID, &r.Platform, &r.PlatformHost, &r.Owner, &r.Name,
		&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
		&r.LastSyncError,
		&r.AllowSquashMerge, &r.AllowMergeCommit, &r.AllowRebaseMerge,
		&r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo by owner/name: %w", err)
	}
	return &r, nil
}

// UpdateRepoSettings updates the merge method settings for a repo.
func (d *DB) UpdateRepoSettings(
	ctx context.Context,
	id int64,
	allowSquash, allowMerge, allowRebase bool,
) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE middleman_repos SET allow_squash_merge = ?, allow_merge_commit = ?, allow_rebase_merge = ? WHERE id = ?`,
		allowSquash, allowMerge, allowRebase, id,
	)
	return err
}

// --- Merge Requests ---

// UpsertMergeRequest inserts or updates a merge request, returning its internal ID.
// On conflict (repo_id, number) all fields except created_at are updated.
func (d *DB) UpsertMergeRequest(ctx context.Context, mr *MergeRequest) (int64, error) {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_merge_requests
		    (repo_id, platform_id, number, url, title, author, author_display_name,
		     state, is_draft, body, head_branch, base_branch,
		     platform_head_sha, platform_base_sha,
		     head_repo_clone_url,
		     additions, deletions, comment_count,
		     review_decision, ci_status, ci_checks_json, created_at, updated_at,
		     last_activity_at, merged_at, closed_at, mergeable_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
		    platform_id          = excluded.platform_id,
		    url                  = excluded.url,
		    title                = excluded.title,
		    author               = excluded.author,
		    author_display_name  = excluded.author_display_name,
		    state                = excluded.state,
		    is_draft             = excluded.is_draft,
		    body                 = excluded.body,
		    head_branch          = excluded.head_branch,
		    base_branch          = excluded.base_branch,
		    platform_head_sha    = excluded.platform_head_sha,
		    platform_base_sha    = excluded.platform_base_sha,
		    head_repo_clone_url  = excluded.head_repo_clone_url,
		    additions            = excluded.additions,
		    deletions            = excluded.deletions,
		    comment_count        = excluded.comment_count,
		    review_decision      = excluded.review_decision,
		    ci_status            = excluded.ci_status,
		    ci_checks_json       = excluded.ci_checks_json,
		    updated_at           = excluded.updated_at,
		    last_activity_at     = excluded.last_activity_at,
		    merged_at            = excluded.merged_at,
		    closed_at            = excluded.closed_at,
		    mergeable_state      = excluded.mergeable_state`,
		mr.RepoID, mr.PlatformID, mr.Number, mr.URL, mr.Title,
		mr.Author, mr.AuthorDisplayName,
		mr.State, mr.IsDraft, mr.Body, mr.HeadBranch, mr.BaseBranch,
		mr.PlatformHeadSHA, mr.PlatformBaseSHA,
		mr.HeadRepoCloneURL,
		mr.Additions, mr.Deletions, mr.CommentCount, mr.ReviewDecision,
		mr.CIStatus, mr.CIChecksJSON, mr.CreatedAt, mr.UpdatedAt,
		mr.LastActivityAt, mr.MergedAt, mr.ClosedAt, mr.MergeableState,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert merge request: %w", err)
	}
	var id int64
	err = d.ro.QueryRowContext(ctx,
		`SELECT id FROM middleman_merge_requests WHERE repo_id = ? AND number = ?`,
		mr.RepoID, mr.Number,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get mr id after upsert: %w", err)
	}
	return id, nil
}

// GetMergeRequest returns a merge request by repo owner/name and MR number, or nil if not found.
func (d *DB) GetMergeRequest(ctx context.Context, owner, name string, number int) (*MergeRequest, error) {
	var mr MergeRequest
	err := d.ro.QueryRowContext(ctx, `
		SELECT p.id, p.repo_id, p.platform_id, p.number, p.url, p.title,
		       p.author, p.author_display_name, p.state, p.is_draft,
		       p.body, p.head_branch, p.base_branch,
		       p.platform_head_sha, p.platform_base_sha,
		       p.diff_head_sha, p.diff_base_sha, p.merge_base_sha,
		       p.head_repo_clone_url,
		       p.additions, p.deletions, p.comment_count, p.review_decision,
		       p.ci_status, p.ci_checks_json,
		       p.created_at, p.updated_at, p.last_activity_at,
		       p.merged_at, p.closed_at, p.mergeable_state,
		       COALESCE(k.status, '') AS kanban_status,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_merge_requests p
		JOIN middleman_repos r ON r.id = p.repo_id
		LEFT JOIN middleman_kanban_state k ON k.merge_request_id = p.id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'pr' AND s.repo_id = p.repo_id AND s.number = p.number
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(
		&mr.ID, &mr.RepoID, &mr.PlatformID, &mr.Number, &mr.URL, &mr.Title,
		&mr.Author, &mr.AuthorDisplayName, &mr.State, &mr.IsDraft,
		&mr.Body, &mr.HeadBranch, &mr.BaseBranch,
		&mr.PlatformHeadSHA, &mr.PlatformBaseSHA,
		&mr.DiffHeadSHA, &mr.DiffBaseSHA, &mr.MergeBaseSHA,
		&mr.HeadRepoCloneURL,
		&mr.Additions, &mr.Deletions, &mr.CommentCount, &mr.ReviewDecision,
		&mr.CIStatus, &mr.CIChecksJSON,
		&mr.CreatedAt, &mr.UpdatedAt, &mr.LastActivityAt,
		&mr.MergedAt, &mr.ClosedAt, &mr.MergeableState, &mr.KanbanStatus, &mr.Starred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}
	return &mr, nil
}

// ListMergeRequests returns merge requests matching the given options.
// Results are ordered by last_activity_at DESC.
func (d *DB) ListMergeRequests(ctx context.Context, opts ListMergeRequestsOpts) ([]MergeRequest, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	var conds []string
	var args []any

	switch state {
	case "all":
		// no state filter
	case "closed":
		conds = append(conds, "p.state IN ('closed', 'merged')")
	default:
		conds = append(conds, "p.state = ?")
		args = append(args, state)
	}

	if opts.RepoOwner != "" && opts.RepoName != "" {
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, opts.RepoOwner, opts.RepoName)
	}
	if opts.KanbanState != "" {
		conds = append(conds, "COALESCE(k.status, '') = ?")
		args = append(args, opts.KanbanState)
	}
	if opts.Starred {
		conds = append(conds, "s.number IS NOT NULL")
	}
	if opts.Search != "" {
		conds = append(conds, "(p.title LIKE ? OR p.author LIKE ?)")
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.repo_id, p.platform_id, p.number, p.url, p.title,
		       p.author, p.author_display_name, p.state, p.is_draft,
		       p.body, p.head_branch, p.base_branch,
		       p.platform_head_sha, p.platform_base_sha,
		       p.diff_head_sha, p.diff_base_sha, p.merge_base_sha,
		       p.head_repo_clone_url,
		       p.additions, p.deletions, p.comment_count, p.review_decision,
		       p.ci_status, p.ci_checks_json,
		       p.created_at, p.updated_at, p.last_activity_at,
		       p.merged_at, p.closed_at, p.mergeable_state,
		       COALESCE(k.status, '') AS kanban_status,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_merge_requests p
		JOIN middleman_repos r ON r.id = p.repo_id
		LEFT JOIN middleman_kanban_state k ON k.merge_request_id = p.id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'pr' AND s.repo_id = p.repo_id AND s.number = p.number
		%s
		ORDER BY p.last_activity_at DESC`, where)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list merge requests: %w", err)
	}
	defer rows.Close()

	var mrs []MergeRequest
	for rows.Next() {
		var mr MergeRequest
		if err := rows.Scan(
			&mr.ID, &mr.RepoID, &mr.PlatformID, &mr.Number, &mr.URL, &mr.Title,
			&mr.Author, &mr.AuthorDisplayName, &mr.State, &mr.IsDraft,
			&mr.Body, &mr.HeadBranch, &mr.BaseBranch,
			&mr.PlatformHeadSHA, &mr.PlatformBaseSHA,
			&mr.DiffHeadSHA, &mr.DiffBaseSHA, &mr.MergeBaseSHA,
			&mr.HeadRepoCloneURL,
			&mr.Additions, &mr.Deletions, &mr.CommentCount, &mr.ReviewDecision,
			&mr.CIStatus, &mr.CIChecksJSON,
			&mr.CreatedAt, &mr.UpdatedAt, &mr.LastActivityAt,
			&mr.MergedAt, &mr.ClosedAt, &mr.MergeableState, &mr.KanbanStatus, &mr.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan merge request: %w", err)
		}
		mrs = append(mrs, mr)
	}
	return mrs, rows.Err()
}

// --- Events ---

// UpsertMREvents bulk-inserts events, ignoring duplicates by dedupe_key.
func (d *DB) UpsertMREvents(ctx context.Context, events []MREvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO middleman_mr_events
			    (merge_request_id, platform_id, event_type, author, summary, body,
			     metadata_json, created_at, dedupe_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(dedupe_key) DO NOTHING`)
		if err != nil {
			return fmt.Errorf("prepare upsert mr events: %w", err)
		}
		defer stmt.Close()

		for i := range events {
			e := &events[i]
			if _, err := stmt.ExecContext(ctx,
				e.MergeRequestID, e.PlatformID, e.EventType, e.Author, e.Summary, e.Body,
				e.MetadataJSON, e.CreatedAt, e.DedupeKey,
			); err != nil {
				return fmt.Errorf("insert mr event (dedupe_key=%s): %w", e.DedupeKey, err)
			}
		}
		return nil
	})
}

// ListMREvents returns all events for a merge request ordered by created_at DESC.
func (d *DB) ListMREvents(ctx context.Context, mrID int64) ([]MREvent, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, merge_request_id, platform_id, event_type, author, summary, body,
		       metadata_json, created_at, dedupe_key
		FROM middleman_mr_events
		WHERE merge_request_id = ?
		ORDER BY created_at DESC`, mrID,
	)
	if err != nil {
		return nil, fmt.Errorf("list mr events: %w", err)
	}
	defer rows.Close()

	var events []MREvent
	for rows.Next() {
		var e MREvent
		if err := rows.Scan(
			&e.ID, &e.MergeRequestID, &e.PlatformID, &e.EventType, &e.Author, &e.Summary,
			&e.Body, &e.MetadataJSON, &e.CreatedAt, &e.DedupeKey,
		); err != nil {
			return nil, fmt.Errorf("scan mr event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Kanban ---

// EnsureKanbanState creates a kanban row with status "new" if one does not exist.
func (d *DB) EnsureKanbanState(ctx context.Context, mrID int64) error {
	_, err := d.rw.ExecContext(ctx,
		`INSERT INTO middleman_kanban_state (merge_request_id, status) VALUES (?, 'new')
		 ON CONFLICT(merge_request_id) DO NOTHING`,
		mrID,
	)
	if err != nil {
		return fmt.Errorf("ensure kanban state: %w", err)
	}
	return nil
}

// SetKanbanState sets the kanban status for a merge request (upsert).
func (d *DB) SetKanbanState(ctx context.Context, mrID int64, status string) error {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_kanban_state (merge_request_id, status, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(merge_request_id) DO UPDATE SET
		    status     = excluded.status,
		    updated_at = excluded.updated_at`,
		mrID, status,
	)
	if err != nil {
		return fmt.Errorf("set kanban state: %w", err)
	}
	return nil
}

// GetKanbanState returns the kanban state for a merge request, or nil if not found.
func (d *DB) GetKanbanState(ctx context.Context, mrID int64) (*KanbanState, error) {
	var k KanbanState
	err := d.ro.QueryRowContext(ctx,
		`SELECT merge_request_id, status, updated_at FROM middleman_kanban_state WHERE merge_request_id = ?`, mrID,
	).Scan(&k.MergeRequestID, &k.Status, &k.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get kanban state: %w", err)
	}
	return &k, nil
}

// --- Helpers ---

// GetMRIDByRepoAndNumber returns the internal MR ID for a given repo+number.
func (d *DB) GetMRIDByRepoAndNumber(ctx context.Context, owner, name string, number int) (int64, error) {
	var id int64
	err := d.ro.QueryRowContext(ctx, `
		SELECT p.id FROM middleman_merge_requests p
		JOIN middleman_repos r ON r.id = p.repo_id
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("MR %s/%s#%d not found", owner, name, number)
	}
	if err != nil {
		return 0, fmt.Errorf("get mr id by repo and number: %w", err)
	}
	return id, nil
}

// GetPreviouslyOpenMRNumbers returns MR numbers that are open in the DB but
// not in the stillOpen set — i.e. MRs that were closed/merged since the last sync.
func (d *DB) GetPreviouslyOpenMRNumbers(
	ctx context.Context,
	repoID int64,
	stillOpen map[int]bool,
) ([]int, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT number FROM middleman_merge_requests WHERE repo_id = ? AND state = 'open'`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("get previously open mrs: %w", err)
	}
	defer rows.Close()

	var closed []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan mr number: %w", err)
		}
		if !stillOpen[n] {
			closed = append(closed, n)
		}
	}
	return closed, rows.Err()
}

// MRDerivedFields holds computed fields that are refreshed after fetching timeline events.
type MRDerivedFields struct {
	ReviewDecision string
	CommentCount   int
	LastActivityAt time.Time
}

// UpdateMRDerivedFields writes computed fields back to the merge_requests row.
func (d *DB) UpdateMRDerivedFields(
	ctx context.Context,
	repoID int64,
	number int,
	fields MRDerivedFields,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET review_decision = ?, comment_count = ?, last_activity_at = ?
		WHERE repo_id = ? AND number = ?`,
		fields.ReviewDecision, fields.CommentCount, fields.LastActivityAt,
		repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update mr derived fields: %w", err)
	}
	return nil
}

// UpdateMRCIStatus writes CI status and check runs JSON for a merge request.
func (d *DB) UpdateMRCIStatus(
	ctx context.Context,
	repoID int64,
	number int,
	ciStatus string,
	ciChecksJSON string,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET ci_status = ?, ci_checks_json = ?
		WHERE repo_id = ? AND number = ?`,
		ciStatus, ciChecksJSON,
		repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update mr ci status: %w", err)
	}
	return nil
}

// UpdateClosedMRState atomically updates the state, timestamps, and final
// platform head/base SHAs for a MR that has transitioned to closed or merged.
// updatedAt should be the MR's UpdatedAt timestamp from the platform.
func (d *DB) UpdateClosedMRState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	updatedAt time.Time,
	mergedAt, closedAt *time.Time,
	platformHeadSHA, platformBaseSHA string,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET state = ?, merged_at = ?, closed_at = ?,
		    updated_at = ?, last_activity_at = ?,
		    platform_head_sha = ?, platform_base_sha = ?
		WHERE repo_id = ? AND number = ?`,
		state, mergedAt, closedAt, updatedAt, updatedAt,
		platformHeadSHA, platformBaseSHA, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update closed MR state: %w", err)
	}
	return nil
}

// UpdateDiffSHAs stores the locally-verified diff SHAs for a merge request.
// Called after a successful bare clone fetch and merge-base computation.
func (d *DB) UpdateDiffSHAs(ctx context.Context, repoID int64, number int, diffHead, diffBase, mergeBase string) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE middleman_merge_requests
		 SET diff_head_sha = ?, diff_base_sha = ?, merge_base_sha = ?
		 WHERE repo_id = ? AND number = ?`,
		diffHead, diffBase, mergeBase, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update diff SHAs for MR %d: %w", number, err)
	}
	return nil
}

// DiffSHAs holds the SHA columns needed by the diff endpoint.
type DiffSHAs struct {
	PlatformHeadSHA string
	PlatformBaseSHA string
	DiffHeadSHA     string
	DiffBaseSHA     string
	MergeBaseSHA    string
	State           string
}

// GetDiffSHAs returns the diff-related SHAs for a merge request.
func (d *DB) GetDiffSHAs(ctx context.Context, owner, name string, number int) (*DiffSHAs, error) {
	var s DiffSHAs
	err := d.ro.QueryRowContext(ctx, `
		SELECT p.platform_head_sha, p.platform_base_sha,
		       p.diff_head_sha, p.diff_base_sha, p.merge_base_sha,
		       p.state
		FROM middleman_merge_requests p
		JOIN middleman_repos r ON r.id = p.repo_id
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(&s.PlatformHeadSHA, &s.PlatformBaseSHA,
		&s.DiffHeadSHA, &s.DiffBaseSHA, &s.MergeBaseSHA,
		&s.State)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get diff SHAs: %w", err)
	}
	return &s, nil
}

// UpdateMRState sets the final state and timestamps for a MR after it is closed or merged.
func (d *DB) UpdateMRState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	mergedAt, closedAt *time.Time,
) error {
	now := time.Now().UTC()
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET state = ?, merged_at = ?, closed_at = ?,
		    updated_at = ?, last_activity_at = ?
		WHERE repo_id = ? AND number = ?`,
		state, mergedAt, closedAt, now, now, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update mr state: %w", err)
	}
	return nil
}

// --- Issues ---

// UpsertIssue inserts or updates an issue, returning its internal ID.
func (d *DB) UpsertIssue(ctx context.Context, issue *Issue) (int64, error) {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_issues
		    (repo_id, platform_id, number, url, title, author, state,
		     body, comment_count, labels_json,
		     created_at, updated_at, last_activity_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
		    platform_id      = excluded.platform_id,
		    url              = excluded.url,
		    title            = excluded.title,
		    author           = excluded.author,
		    state            = excluded.state,
		    body             = excluded.body,
		    comment_count    = excluded.comment_count,
		    labels_json      = excluded.labels_json,
		    updated_at       = excluded.updated_at,
		    last_activity_at = excluded.last_activity_at,
		    closed_at        = excluded.closed_at`,
		issue.RepoID, issue.PlatformID, issue.Number, issue.URL,
		issue.Title, issue.Author, issue.State,
		issue.Body, issue.CommentCount, issue.LabelsJSON,
		issue.CreatedAt, issue.UpdatedAt, issue.LastActivityAt, issue.ClosedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert issue: %w", err)
	}
	var id int64
	err = d.ro.QueryRowContext(ctx,
		`SELECT id FROM middleman_issues WHERE repo_id = ? AND number = ?`,
		issue.RepoID, issue.Number,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get issue id after upsert: %w", err)
	}
	return id, nil
}

// GetIssue returns an issue by repo owner/name and issue number, or nil if not found.
func (d *DB) GetIssue(
	ctx context.Context, owner, name string, number int,
) (*Issue, error) {
	var issue Issue
	err := d.ro.QueryRowContext(ctx, `
		SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
		       i.author, i.state, i.body, i.comment_count, i.labels_json,
		       i.created_at, i.updated_at, i.last_activity_at, i.closed_at,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_issues i
		JOIN middleman_repos r ON r.id = i.repo_id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'issue' AND s.repo_id = i.repo_id AND s.number = i.number
		WHERE r.owner = ? AND r.name = ? AND i.number = ?`,
		owner, name, number,
	).Scan(
		&issue.ID, &issue.RepoID, &issue.PlatformID, &issue.Number,
		&issue.URL, &issue.Title, &issue.Author, &issue.State,
		&issue.Body, &issue.CommentCount, &issue.LabelsJSON,
		&issue.CreatedAt, &issue.UpdatedAt, &issue.LastActivityAt,
		&issue.ClosedAt, &issue.Starred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	return &issue, nil
}

// ListIssues returns issues matching the given options.
func (d *DB) ListIssues(
	ctx context.Context, opts ListIssuesOpts,
) ([]Issue, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	var conds []string
	var args []any

	switch state {
	case "all":
		// no state filter
	case "closed":
		conds = append(conds, "i.state = 'closed'")
	default:
		conds = append(conds, "i.state = ?")
		args = append(args, state)
	}

	if opts.RepoOwner != "" && opts.RepoName != "" {
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, opts.RepoOwner, opts.RepoName)
	}
	if opts.Starred {
		conds = append(conds, "s.number IS NOT NULL")
	}
	if opts.Search != "" {
		conds = append(conds, "(i.title LIKE ? OR i.author LIKE ?)")
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
		       i.author, i.state, i.body, i.comment_count, i.labels_json,
		       i.created_at, i.updated_at, i.last_activity_at, i.closed_at,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_issues i
		JOIN middleman_repos r ON r.id = i.repo_id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'issue' AND s.repo_id = i.repo_id AND s.number = i.number
		%s
		ORDER BY i.last_activity_at DESC`, where)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()

	var issues []Issue
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(
			&issue.ID, &issue.RepoID, &issue.PlatformID, &issue.Number,
			&issue.URL, &issue.Title, &issue.Author, &issue.State,
			&issue.Body, &issue.CommentCount, &issue.LabelsJSON,
			&issue.CreatedAt, &issue.UpdatedAt, &issue.LastActivityAt,
			&issue.ClosedAt, &issue.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetIssueIDByRepoAndNumber returns the internal issue ID for a given repo+number.
func (d *DB) GetIssueIDByRepoAndNumber(
	ctx context.Context, owner, name string, number int,
) (int64, error) {
	var id int64
	err := d.ro.QueryRowContext(ctx, `
		SELECT i.id FROM middleman_issues i
		JOIN middleman_repos r ON r.id = i.repo_id
		WHERE r.owner = ? AND r.name = ? AND i.number = ?`,
		owner, name, number,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("issue %s/%s#%d not found", owner, name, number)
	}
	if err != nil {
		return 0, fmt.Errorf("get issue id by repo and number: %w", err)
	}
	return id, nil
}

// ResolveItemNumber checks whether the given number in a repo is a MR
// or issue. Returns the item type ("pr" or "issue") and whether it was
// found. MRs take precedence if both somehow exist.
func (d *DB) ResolveItemNumber(
	ctx context.Context, repoID int64, number int,
) (itemType string, found bool, err error) {
	var exists int
	err = d.ro.QueryRowContext(ctx,
		`SELECT 1 FROM middleman_merge_requests WHERE repo_id = ? AND number = ?`,
		repoID, number,
	).Scan(&exists)
	if err == nil {
		return "pr", true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("check merge_requests: %w", err)
	}

	err = d.ro.QueryRowContext(ctx,
		`SELECT 1 FROM middleman_issues WHERE repo_id = ? AND number = ?`,
		repoID, number,
	).Scan(&exists)
	if err == nil {
		return "issue", true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("check issues: %w", err)
	}

	return "", false, nil
}

// UpdateIssueState sets the state and closed_at for an issue.
func (d *DB) UpdateIssueState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	closedAt *time.Time,
) error {
	now := time.Now().UTC()
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_issues SET state = ?, closed_at = ?,
		    updated_at = ?, last_activity_at = ?
		WHERE repo_id = ? AND number = ?`,
		state, closedAt, now, now, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update issue state: %w", err)
	}
	return nil
}

// GetPreviouslyOpenIssueNumbers returns issue numbers that are open in the DB
// but not in the stillOpen set.
func (d *DB) GetPreviouslyOpenIssueNumbers(
	ctx context.Context,
	repoID int64,
	stillOpen map[int]bool,
) ([]int, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT number FROM middleman_issues WHERE repo_id = ? AND state = 'open'`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("get previously open issues: %w", err)
	}
	defer rows.Close()

	var closed []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan issue number: %w", err)
		}
		if !stillOpen[n] {
			closed = append(closed, n)
		}
	}
	return closed, rows.Err()
}

// --- Issue Events ---

// UpsertIssueEvents bulk-inserts issue events, ignoring duplicates by dedupe_key.
func (d *DB) UpsertIssueEvents(ctx context.Context, events []IssueEvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO middleman_issue_events
			    (issue_id, platform_id, event_type, author, summary, body,
			     metadata_json, created_at, dedupe_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(dedupe_key) DO NOTHING`)
		if err != nil {
			return fmt.Errorf("prepare upsert issue events: %w", err)
		}
		defer stmt.Close()

		for i := range events {
			e := &events[i]
			if _, err := stmt.ExecContext(ctx,
				e.IssueID, e.PlatformID, e.EventType, e.Author,
				e.Summary, e.Body, e.MetadataJSON, e.CreatedAt,
				e.DedupeKey,
			); err != nil {
				return fmt.Errorf("insert issue event (dedupe_key=%s): %w", e.DedupeKey, err)
			}
		}
		return nil
	})
}

// ListIssueEvents returns all events for an issue ordered by created_at DESC.
func (d *DB) ListIssueEvents(ctx context.Context, issueID int64) ([]IssueEvent, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, issue_id, platform_id, event_type, author, summary, body,
		       metadata_json, created_at, dedupe_key
		FROM middleman_issue_events
		WHERE issue_id = ?
		ORDER BY created_at DESC`, issueID,
	)
	if err != nil {
		return nil, fmt.Errorf("list issue events: %w", err)
	}
	defer rows.Close()

	var events []IssueEvent
	for rows.Next() {
		var e IssueEvent
		if err := rows.Scan(
			&e.ID, &e.IssueID, &e.PlatformID, &e.EventType, &e.Author,
			&e.Summary, &e.Body, &e.MetadataJSON, &e.CreatedAt, &e.DedupeKey,
		); err != nil {
			return nil, fmt.Errorf("scan issue event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Starring ---

// SetStarred stars an item (MR or issue).
func (d *DB) SetStarred(
	ctx context.Context, itemType string, repoID int64, number int,
) error {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_starred_items (item_type, repo_id, number)
		VALUES (?, ?, ?)
		ON CONFLICT(item_type, repo_id, number) DO NOTHING`,
		itemType, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("set starred: %w", err)
	}
	return nil
}

// UnsetStarred removes a star from an item.
func (d *DB) UnsetStarred(
	ctx context.Context, itemType string, repoID int64, number int,
) error {
	_, err := d.rw.ExecContext(ctx, `
		DELETE FROM middleman_starred_items
		WHERE item_type = ? AND repo_id = ? AND number = ?`,
		itemType, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("unset starred: %w", err)
	}
	return nil
}

// IsStarred checks whether an item is starred.
func (d *DB) IsStarred(
	ctx context.Context, itemType string, repoID int64, number int,
) (bool, error) {
	var count int
	err := d.ro.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM middleman_starred_items
		WHERE item_type = ? AND repo_id = ? AND number = ?`,
		itemType, repoID, number,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("is starred: %w", err)
	}
	return count > 0, nil
}

// --- Rate Limits ---

// UpsertRateLimit inserts or updates a rate limit row by platform_host.
func (d *DB) UpsertRateLimit(
	platformHost string,
	requestsHour int,
	hourStart time.Time,
	rateRemaining int,
	rateResetAt *time.Time,
) error {
	_, err := d.rw.Exec(`
		INSERT INTO middleman_rate_limits
		    (platform_host, requests_hour, hour_start,
		     rate_remaining, rate_reset_at, updated_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(platform_host) DO UPDATE SET
		    requests_hour  = excluded.requests_hour,
		    hour_start     = excluded.hour_start,
		    rate_remaining = excluded.rate_remaining,
		    rate_reset_at  = excluded.rate_reset_at,
		    updated_at     = datetime('now')`,
		platformHost, requestsHour, hourStart,
		rateRemaining, rateResetAt,
	)
	if err != nil {
		return fmt.Errorf("upsert rate limit: %w", err)
	}
	return nil
}

// GetRateLimit returns the rate limit row for a platform host,
// or nil,nil if not found.
func (d *DB) GetRateLimit(
	platformHost string,
) (*RateLimit, error) {
	var r RateLimit
	err := d.ro.QueryRow(`
		SELECT id, platform_host, requests_hour, hour_start,
		       rate_remaining, rate_reset_at, updated_at
		FROM middleman_rate_limits
		WHERE platform_host = ?`,
		platformHost,
	).Scan(
		&r.ID, &r.PlatformHost, &r.RequestsHour, &r.HourStart,
		&r.RateRemaining, &r.RateResetAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rate limit: %w", err)
	}
	return &r, nil
}

// --- Worktree Links ---

// SetWorktreeLinks replaces all worktree links atomically.
// The existing rows are deleted and the provided links are
// inserted in a single transaction.
func (d *DB) SetWorktreeLinks(links []WorktreeLink) error {
	return d.Tx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`DELETE FROM middleman_mr_worktree_links`,
		); err != nil {
			return fmt.Errorf("delete worktree links: %w", err)
		}
		if len(links) == 0 {
			return nil
		}
		stmt, err := tx.Prepare(`
			INSERT INTO middleman_mr_worktree_links
			    (merge_request_id, worktree_key,
			     worktree_path, worktree_branch, linked_at)
			VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf(
				"prepare insert worktree link: %w", err,
			)
		}
		defer stmt.Close()
		for i := range links {
			l := &links[i]
			if _, err := stmt.Exec(
				l.MergeRequestID, l.WorktreeKey,
				l.WorktreePath, l.WorktreeBranch,
				l.LinkedAt.UTC().Format(time.RFC3339),
			); err != nil {
				return fmt.Errorf(
					"insert worktree link %s: %w",
					l.WorktreeKey, err,
				)
			}
		}
		return nil
	})
}

// GetWorktreeLinksForMR returns worktree links for a
// specific merge request.
func (d *DB) GetWorktreeLinksForMR(
	mergeRequestID int64,
) ([]WorktreeLink, error) {
	rows, err := d.ro.Query(`
		SELECT id, merge_request_id, worktree_key,
		       worktree_path, worktree_branch, linked_at
		FROM middleman_mr_worktree_links
		WHERE merge_request_id = ?
		ORDER BY linked_at DESC`,
		mergeRequestID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get worktree links for MR: %w", err,
		)
	}
	defer rows.Close()
	return scanWorktreeLinks(rows)
}

// GetWorktreeLinksForMRs returns worktree links for the
// given merge request IDs. IDs are batched to stay within
// SQLite's bind-parameter limit.
func (d *DB) GetWorktreeLinksForMRs(
	mrIDs []int64,
) ([]WorktreeLink, error) {
	if len(mrIDs) == 0 {
		return nil, nil
	}
	const batchSize = 500
	var all []WorktreeLink
	for start := 0; start < len(mrIDs); start += batchSize {
		end := min(start+batchSize, len(mrIDs))
		batch := mrIDs[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			placeholders[i] = "?"
			args[i] = id
		}
		query := `
			SELECT id, merge_request_id, worktree_key,
			       worktree_path, worktree_branch, linked_at
			FROM middleman_mr_worktree_links
			WHERE merge_request_id IN (` +
			strings.Join(placeholders, ",") + `)
			ORDER BY linked_at DESC`
		rows, err := d.ro.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf(
				"get worktree links for MRs: %w", err,
			)
		}
		links, err := scanWorktreeLinks(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		all = append(all, links...)
	}
	return all, nil
}

// GetAllWorktreeLinks returns all worktree links ordered
// by linked_at DESC.
func (d *DB) GetAllWorktreeLinks() ([]WorktreeLink, error) {
	rows, err := d.ro.Query(`
		SELECT id, merge_request_id, worktree_key,
		       worktree_path, worktree_branch, linked_at
		FROM middleman_mr_worktree_links
		ORDER BY linked_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get all worktree links: %w", err,
		)
	}
	defer rows.Close()
	return scanWorktreeLinks(rows)
}

func scanWorktreeLinks(
	rows *sql.Rows,
) ([]WorktreeLink, error) {
	var links []WorktreeLink
	for rows.Next() {
		var l WorktreeLink
		var path, branch sql.NullString
		var linkedAtStr string
		if err := rows.Scan(
			&l.ID, &l.MergeRequestID, &l.WorktreeKey,
			&path, &branch, &linkedAtStr,
		); err != nil {
			return nil, fmt.Errorf(
				"scan worktree link: %w", err,
			)
		}
		t, err := time.Parse(time.RFC3339, linkedAtStr)
		if err != nil {
			return nil, fmt.Errorf(
				"parse linked_at %q: %w", linkedAtStr, err,
			)
		}
		l.LinkedAt = t
		l.WorktreePath = path.String
		l.WorktreeBranch = branch.String
		links = append(links, l)
	}
	return links, rows.Err()
}
