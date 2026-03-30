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
		`INSERT INTO repos (owner, name) VALUES (?, ?) ON CONFLICT(owner, name) DO NOTHING`,
		owner, name,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert repo: %w", err)
	}
	var id int64
	err = d.ro.QueryRowContext(ctx,
		`SELECT id FROM repos WHERE owner = ? AND name = ?`, owner, name,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get repo id after upsert: %w", err)
	}
	return id, nil
}

// ListRepos returns all repos ordered by owner, name.
func (d *DB) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT id, owner, name, last_sync_started_at, last_sync_completed_at,
		        last_sync_error, created_at
		 FROM repos ORDER BY owner, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(
			&r.ID, &r.Owner, &r.Name,
			&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
			&r.LastSyncError, &r.CreatedAt,
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
		`UPDATE repos SET last_sync_started_at = ? WHERE id = ?`, t, id,
	)
	if err != nil {
		return fmt.Errorf("update repo sync started: %w", err)
	}
	return nil
}

// UpdateRepoSyncCompleted records the time and optional error a sync finished.
func (d *DB) UpdateRepoSyncCompleted(ctx context.Context, id int64, t time.Time, syncErr string) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE repos SET last_sync_completed_at = ?, last_sync_error = ? WHERE id = ?`,
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
		`SELECT id, owner, name, last_sync_started_at, last_sync_completed_at,
		        last_sync_error, created_at
		 FROM repos WHERE owner = ? AND name = ?`, owner, name,
	).Scan(
		&r.ID, &r.Owner, &r.Name,
		&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
		&r.LastSyncError, &r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo by owner/name: %w", err)
	}
	return &r, nil
}

// --- Pull Requests ---

// UpsertPullRequest inserts or updates a pull request, returning its internal ID.
// On conflict (repo_id, number) all fields except created_at are updated.
func (d *DB) UpsertPullRequest(ctx context.Context, pr *PullRequest) (int64, error) {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO pull_requests
		    (repo_id, github_id, number, url, title, author, state, is_draft,
		     body, head_branch, base_branch, additions, deletions, comment_count,
		     review_decision, ci_status, created_at, updated_at, last_activity_at,
		     merged_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
		    github_id        = excluded.github_id,
		    url              = excluded.url,
		    title            = excluded.title,
		    author           = excluded.author,
		    state            = excluded.state,
		    is_draft         = excluded.is_draft,
		    body             = excluded.body,
		    head_branch      = excluded.head_branch,
		    base_branch      = excluded.base_branch,
		    additions        = excluded.additions,
		    deletions        = excluded.deletions,
		    comment_count    = excluded.comment_count,
		    review_decision  = excluded.review_decision,
		    ci_status        = excluded.ci_status,
		    updated_at       = excluded.updated_at,
		    last_activity_at = excluded.last_activity_at,
		    merged_at        = excluded.merged_at,
		    closed_at        = excluded.closed_at`,
		pr.RepoID, pr.GitHubID, pr.Number, pr.URL, pr.Title, pr.Author,
		pr.State, pr.IsDraft, pr.Body, pr.HeadBranch, pr.BaseBranch,
		pr.Additions, pr.Deletions, pr.CommentCount, pr.ReviewDecision,
		pr.CIStatus, pr.CreatedAt, pr.UpdatedAt, pr.LastActivityAt,
		pr.MergedAt, pr.ClosedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert pull request: %w", err)
	}
	var id int64
	err = d.ro.QueryRowContext(ctx,
		`SELECT id FROM pull_requests WHERE repo_id = ? AND number = ?`,
		pr.RepoID, pr.Number,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get pr id after upsert: %w", err)
	}
	return id, nil
}

// GetPullRequest returns a pull request by repo owner/name and PR number, or nil if not found.
func (d *DB) GetPullRequest(ctx context.Context, owner, name string, number int) (*PullRequest, error) {
	var pr PullRequest
	err := d.ro.QueryRowContext(ctx, `
		SELECT p.id, p.repo_id, p.github_id, p.number, p.url, p.title,
		       p.author, p.state, p.is_draft, p.body, p.head_branch, p.base_branch,
		       p.additions, p.deletions, p.comment_count, p.review_decision,
		       p.ci_status, p.created_at, p.updated_at, p.last_activity_at,
		       p.merged_at, p.closed_at,
		       COALESCE(k.status, '') AS kanban_status
		FROM pull_requests p
		JOIN repos r ON r.id = p.repo_id
		LEFT JOIN kanban_state k ON k.pr_id = p.id
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(
		&pr.ID, &pr.RepoID, &pr.GitHubID, &pr.Number, &pr.URL, &pr.Title,
		&pr.Author, &pr.State, &pr.IsDraft, &pr.Body, &pr.HeadBranch, &pr.BaseBranch,
		&pr.Additions, &pr.Deletions, &pr.CommentCount, &pr.ReviewDecision,
		&pr.CIStatus, &pr.CreatedAt, &pr.UpdatedAt, &pr.LastActivityAt,
		&pr.MergedAt, &pr.ClosedAt, &pr.KanbanStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	return &pr, nil
}

// ListPullRequests returns pull requests matching the given options.
// Results are ordered by last_activity_at DESC.
func (d *DB) ListPullRequests(ctx context.Context, opts ListPullsOpts) ([]PullRequest, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	var conds []string
	var args []any

	conds = append(conds, "p.state = ?")
	args = append(args, state)

	if opts.RepoOwner != "" && opts.RepoName != "" {
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, opts.RepoOwner, opts.RepoName)
	}
	if opts.KanbanState != "" {
		conds = append(conds, "COALESCE(k.status, '') = ?")
		args = append(args, opts.KanbanState)
	}
	if opts.Search != "" {
		conds = append(conds, "(p.title LIKE ? OR p.author LIKE ?)")
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}

	where := "WHERE " + strings.Join(conds, " AND ")
	args = append(args, limit, opts.Offset)

	query := fmt.Sprintf(`
		SELECT p.id, p.repo_id, p.github_id, p.number, p.url, p.title,
		       p.author, p.state, p.is_draft, p.body, p.head_branch, p.base_branch,
		       p.additions, p.deletions, p.comment_count, p.review_decision,
		       p.ci_status, p.created_at, p.updated_at, p.last_activity_at,
		       p.merged_at, p.closed_at,
		       COALESCE(k.status, '') AS kanban_status
		FROM pull_requests p
		JOIN repos r ON r.id = p.repo_id
		LEFT JOIN kanban_state k ON k.pr_id = p.id
		%s
		ORDER BY p.last_activity_at DESC
		LIMIT ? OFFSET ?`, where)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	defer rows.Close()

	var prs []PullRequest
	for rows.Next() {
		var pr PullRequest
		if err := rows.Scan(
			&pr.ID, &pr.RepoID, &pr.GitHubID, &pr.Number, &pr.URL, &pr.Title,
			&pr.Author, &pr.State, &pr.IsDraft, &pr.Body, &pr.HeadBranch, &pr.BaseBranch,
			&pr.Additions, &pr.Deletions, &pr.CommentCount, &pr.ReviewDecision,
			&pr.CIStatus, &pr.CreatedAt, &pr.UpdatedAt, &pr.LastActivityAt,
			&pr.MergedAt, &pr.ClosedAt, &pr.KanbanStatus,
		); err != nil {
			return nil, fmt.Errorf("scan pull request: %w", err)
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

// --- Events ---

// UpsertPREvents bulk-inserts events, ignoring duplicates by dedupe_key.
func (d *DB) UpsertPREvents(ctx context.Context, events []PREvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO pr_events
			    (pr_id, github_id, event_type, author, summary, body,
			     metadata_json, created_at, dedupe_key)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(dedupe_key) DO NOTHING`)
		if err != nil {
			return fmt.Errorf("prepare upsert pr events: %w", err)
		}
		defer stmt.Close()

		for i := range events {
			e := &events[i]
			if _, err := stmt.ExecContext(ctx,
				e.PRID, e.GitHubID, e.EventType, e.Author, e.Summary, e.Body,
				e.MetadataJSON, e.CreatedAt, e.DedupeKey,
			); err != nil {
				return fmt.Errorf("insert pr event (dedupe_key=%s): %w", e.DedupeKey, err)
			}
		}
		return nil
	})
}

// ListPREvents returns all events for a PR ordered by created_at DESC.
func (d *DB) ListPREvents(ctx context.Context, prID int64) ([]PREvent, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, pr_id, github_id, event_type, author, summary, body,
		       metadata_json, created_at, dedupe_key
		FROM pr_events
		WHERE pr_id = ?
		ORDER BY created_at DESC`, prID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pr events: %w", err)
	}
	defer rows.Close()

	var events []PREvent
	for rows.Next() {
		var e PREvent
		if err := rows.Scan(
			&e.ID, &e.PRID, &e.GitHubID, &e.EventType, &e.Author, &e.Summary,
			&e.Body, &e.MetadataJSON, &e.CreatedAt, &e.DedupeKey,
		); err != nil {
			return nil, fmt.Errorf("scan pr event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Kanban ---

// EnsureKanbanState creates a kanban row with status "new" if one does not exist.
func (d *DB) EnsureKanbanState(ctx context.Context, prID int64) error {
	_, err := d.rw.ExecContext(ctx,
		`INSERT INTO kanban_state (pr_id, status) VALUES (?, 'new') ON CONFLICT(pr_id) DO NOTHING`,
		prID,
	)
	if err != nil {
		return fmt.Errorf("ensure kanban state: %w", err)
	}
	return nil
}

// SetKanbanState sets the kanban status for a PR (upsert).
func (d *DB) SetKanbanState(ctx context.Context, prID int64, status string) error {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO kanban_state (pr_id, status, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(pr_id) DO UPDATE SET
		    status     = excluded.status,
		    updated_at = excluded.updated_at`,
		prID, status,
	)
	if err != nil {
		return fmt.Errorf("set kanban state: %w", err)
	}
	return nil
}

// GetKanbanState returns the kanban state for a PR, or nil if not found.
func (d *DB) GetKanbanState(ctx context.Context, prID int64) (*KanbanState, error) {
	var k KanbanState
	err := d.ro.QueryRowContext(ctx,
		`SELECT pr_id, status, updated_at FROM kanban_state WHERE pr_id = ?`, prID,
	).Scan(&k.PRID, &k.Status, &k.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get kanban state: %w", err)
	}
	return &k, nil
}

// --- Helpers ---

// GetPRIDByRepoAndNumber returns the internal PR ID for a given repo+number.
func (d *DB) GetPRIDByRepoAndNumber(ctx context.Context, owner, name string, number int) (int64, error) {
	var id int64
	err := d.ro.QueryRowContext(ctx, `
		SELECT p.id FROM pull_requests p
		JOIN repos r ON r.id = p.repo_id
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("PR %s/%s#%d not found", owner, name, number)
	}
	if err != nil {
		return 0, fmt.Errorf("get pr id by repo and number: %w", err)
	}
	return id, nil
}

// GetPreviouslyOpenPRNumbers returns PR numbers that are open in the DB but
// not in the stillOpen set — i.e. PRs that were closed/merged since the last sync.
func (d *DB) GetPreviouslyOpenPRNumbers(
	ctx context.Context,
	repoID int64,
	stillOpen map[int]bool,
) ([]int, error) {
	rows, err := d.ro.QueryContext(ctx,
		`SELECT number FROM pull_requests WHERE repo_id = ? AND state = 'open'`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("get previously open prs: %w", err)
	}
	defer rows.Close()

	var closed []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan pr number: %w", err)
		}
		if !stillOpen[n] {
			closed = append(closed, n)
		}
	}
	return closed, rows.Err()
}

// PRDerivedFields holds computed fields that are refreshed after fetching timeline events.
type PRDerivedFields struct {
	ReviewDecision string
	CommentCount   int
	LastActivityAt time.Time
	CIStatus       string
}

// UpdatePRDerivedFields writes computed fields back to the pull_requests row.
func (d *DB) UpdatePRDerivedFields(
	ctx context.Context,
	repoID int64,
	number int,
	fields PRDerivedFields,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE pull_requests
		SET review_decision = ?, comment_count = ?, last_activity_at = ?, ci_status = ?
		WHERE repo_id = ? AND number = ?`,
		fields.ReviewDecision, fields.CommentCount, fields.LastActivityAt, fields.CIStatus,
		repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update pr derived fields: %w", err)
	}
	return nil
}

// UpdatePRState sets the final state and timestamps for a PR after it is closed or merged.
func (d *DB) UpdatePRState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	mergedAt, closedAt *time.Time,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE pull_requests
		SET state = ?, merged_at = ?, closed_at = ?
		WHERE repo_id = ? AND number = ?`,
		state, mergedAt, closedAt, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update pr state: %w", err)
	}
	return nil
}
