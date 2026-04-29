package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func sqlPlaceholders(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func canonicalRepoIdentifier(host, owner, name string) (string, string, string) {
	if host == "" {
		host = "github.com"
	}
	return strings.ToLower(host), strings.ToLower(owner), strings.ToLower(name)
}

func lookupLabelIDByNameTx(ctx context.Context, tx *sql.Tx, repoID int64, name string) (int64, bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM middleman_labels WHERE repo_id = ? AND name = ?`,
		repoID, name,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func labelPlatformIDTx(ctx context.Context, tx *sql.Tx, labelID int64) (sql.NullInt64, error) {
	var platformID sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT platform_id FROM middleman_labels WHERE id = ?`,
		labelID,
	).Scan(&platformID)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return platformID, nil
}

func mergeLabelRowAssociationsTx(ctx context.Context, tx *sql.Tx, fromLabelID, toLabelID int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO middleman_issue_labels (issue_id, label_id)
		SELECT issue_id, ? FROM middleman_issue_labels WHERE label_id = ?
		ON CONFLICT(issue_id, label_id) DO NOTHING`,
		toLabelID, fromLabelID,
	); err != nil {
		return fmt.Errorf("move issue label associations: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM middleman_issue_labels WHERE label_id = ?`,
		fromLabelID,
	); err != nil {
		return fmt.Errorf("delete old issue label associations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO middleman_merge_request_labels (merge_request_id, label_id)
		SELECT merge_request_id, ? FROM middleman_merge_request_labels WHERE label_id = ?
		ON CONFLICT(merge_request_id, label_id) DO NOTHING`,
		toLabelID, fromLabelID,
	); err != nil {
		return fmt.Errorf("move merge request label associations: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM middleman_merge_request_labels WHERE label_id = ?`,
		fromLabelID,
	); err != nil {
		return fmt.Errorf("delete old merge request label associations: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM middleman_labels WHERE id = ?`,
		fromLabelID,
	); err != nil {
		return fmt.Errorf("delete old label row: %w", err)
	}
	return nil
}

func lookupLabelIDByPlatformIDTx(ctx context.Context, tx *sql.Tx, repoID, platformID int64) (int64, bool, error) {
	if platformID == 0 {
		return 0, false, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM middleman_labels WHERE repo_id = ? AND platform_id = ?`,
		repoID, platformID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func labelIDForUpsertTx(ctx context.Context, tx *sql.Tx, repoID int64, label Label) (int64, bool, error) {
	platformID, foundByPlatform, err := lookupLabelIDByPlatformIDTx(ctx, tx, repoID, label.PlatformID)
	if err != nil {
		return 0, false, fmt.Errorf("lookup label %s by platform id: %w", label.Name, err)
	}
	nameID, foundByName, err := lookupLabelIDByNameTx(ctx, tx, repoID, label.Name)
	if err != nil {
		return 0, false, fmt.Errorf("lookup label %s by name: %w", label.Name, err)
	}
	if foundByPlatform && foundByName && platformID != nameID {
		namePlatformID, err := labelPlatformIDTx(ctx, tx, nameID)
		if err != nil {
			return 0, false, fmt.Errorf("lookup label %s platform id: %w", label.Name, err)
		}
		if !namePlatformID.Valid {
			if err := mergeLabelRowAssociationsTx(ctx, tx, nameID, platformID); err != nil {
				return 0, false, fmt.Errorf("merge stale label %s into platform row: %w", label.Name, err)
			}
			return platformID, true, nil
		}
		return 0, false, fmt.Errorf("label %s in repo %d matches different rows by name and platform id", label.Name, repoID)
	}
	if foundByPlatform {
		return platformID, true, nil
	}
	if foundByName {
		return nameID, true, nil
	}
	return 0, false, nil
}

func repoIDForIssueTx(ctx context.Context, tx *sql.Tx, issueID int64) (int64, error) {
	var repoID int64
	err := tx.QueryRowContext(ctx,
		`SELECT repo_id FROM middleman_issues WHERE id = ?`,
		issueID,
	).Scan(&repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("issue %d not found", issueID)
	}
	if err != nil {
		return 0, fmt.Errorf("lookup issue repo: %w", err)
	}
	return repoID, nil
}

func repoIDForMergeRequestTx(ctx context.Context, tx *sql.Tx, mrID int64) (int64, error) {
	var repoID int64
	err := tx.QueryRowContext(ctx,
		`SELECT repo_id FROM middleman_merge_requests WHERE id = ?`,
		mrID,
	).Scan(&repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("merge request %d not found", mrID)
	}
	if err != nil {
		return 0, fmt.Errorf("lookup merge request repo: %w", err)
	}
	return repoID, nil
}

func upsertLabelsTx(ctx context.Context, tx *sql.Tx, repoID int64, labels []Label) (map[string]int64, error) {
	ids := make(map[string]int64, len(labels))
	for _, label := range labels {
		id, found, err := labelIDForUpsertTx(ctx, tx, repoID, label)
		if err != nil {
			return nil, err
		}
		if !found {
			result, err := tx.ExecContext(ctx, `
				INSERT INTO middleman_labels (repo_id, platform_id, name, description, color, is_default, updated_at)
				VALUES (?, NULLIF(?, 0), ?, ?, ?, ?, ?)`,
				repoID, label.PlatformID, label.Name, label.Description, label.Color, label.IsDefault, label.UpdatedAt,
			)
			if err != nil {
				return nil, fmt.Errorf("insert label %s: %w", label.Name, err)
			}
			id, err = result.LastInsertId()
			if err != nil {
				return nil, fmt.Errorf("label insert id %s: %w", label.Name, err)
			}
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE middleman_labels
				SET platform_id = COALESCE(NULLIF(?, 0), platform_id),
				    name = ?,
				    description = ?,
				    color = ?,
				    is_default = ?,
				    updated_at = ?
				WHERE id = ?`,
				label.PlatformID, label.Name, label.Description, label.Color, label.IsDefault, label.UpdatedAt, id,
			)
			if err != nil {
				return nil, fmt.Errorf("update label %s: %w", label.Name, err)
			}
		}
		ids[label.Name] = id
	}
	return ids, nil
}

func replaceIssueLabelsTx(ctx context.Context, tx *sql.Tx, repoID, issueID int64, labels []Label) error {
	actualRepoID, err := repoIDForIssueTx(ctx, tx, issueID)
	if err != nil {
		return err
	}
	if actualRepoID != repoID {
		return fmt.Errorf("issue %d belongs to repo %d, not repo %d", issueID, actualRepoID, repoID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM middleman_issue_labels WHERE issue_id = ?`, issueID); err != nil {
		return fmt.Errorf("delete issue labels: %w", err)
	}
	if len(labels) == 0 {
		return nil
	}
	ids, err := upsertLabelsTx(ctx, tx, actualRepoID, labels)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO middleman_issue_labels (issue_id, label_id) VALUES (?, ?) ON CONFLICT(issue_id, label_id) DO NOTHING`,
			issueID, ids[label.Name],
		); err != nil {
			return fmt.Errorf("insert issue label %s: %w", label.Name, err)
		}
	}
	return nil
}

func replaceMergeRequestLabelsTx(ctx context.Context, tx *sql.Tx, repoID, mrID int64, labels []Label) error {
	actualRepoID, err := repoIDForMergeRequestTx(ctx, tx, mrID)
	if err != nil {
		return err
	}
	if actualRepoID != repoID {
		return fmt.Errorf("merge request %d belongs to repo %d, not repo %d", mrID, actualRepoID, repoID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM middleman_merge_request_labels WHERE merge_request_id = ?`, mrID); err != nil {
		return fmt.Errorf("delete merge request labels: %w", err)
	}
	if len(labels) == 0 {
		return nil
	}
	ids, err := upsertLabelsTx(ctx, tx, actualRepoID, labels)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO middleman_merge_request_labels (merge_request_id, label_id) VALUES (?, ?) ON CONFLICT(merge_request_id, label_id) DO NOTHING`,
			mrID, ids[label.Name],
		); err != nil {
			return fmt.Errorf("insert merge request label %s: %w", label.Name, err)
		}
	}
	return nil
}

func (d *DB) UpsertLabels(ctx context.Context, repoID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		_, err := upsertLabelsTx(ctx, tx, repoID, labels)
		return err
	})
}

func (d *DB) ReplaceIssueLabels(ctx context.Context, repoID, issueID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		return replaceIssueLabelsTx(ctx, tx, repoID, issueID, labels)
	})
}

func (d *DB) ReplaceMergeRequestLabels(ctx context.Context, repoID, mrID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		return replaceMergeRequestLabelsTx(ctx, tx, repoID, mrID, labels)
	})
}

func (d *DB) loadLabelsForMergeRequests(ctx context.Context, ids []int64) (map[int64][]Label, error) {
	if len(ids) == 0 {
		return map[int64][]Label{}, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT ml.merge_request_id, l.id, l.repo_id, COALESCE(l.platform_id, 0), l.name, l.description, l.color, l.is_default, l.updated_at
		FROM middleman_merge_request_labels ml
		JOIN middleman_labels l ON l.id = ml.label_id
		WHERE ml.merge_request_id IN (%s)
		ORDER BY l.name, l.id`, sqlPlaceholders(len(ids)))
	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query merge request labels: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]Label, len(ids))
	for rows.Next() {
		var ownerID int64
		var label Label
		if err := rows.Scan(&ownerID, &label.ID, &label.RepoID, &label.PlatformID, &label.Name, &label.Description, &label.Color, &label.IsDefault, &label.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan merge request label: %w", err)
		}
		out[ownerID] = append(out[ownerID], label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merge request labels: %w", err)
	}
	return out, nil
}

func (d *DB) loadLabelsForIssues(ctx context.Context, ids []int64) (map[int64][]Label, error) {
	if len(ids) == 0 {
		return map[int64][]Label{}, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT il.issue_id, l.id, l.repo_id, COALESCE(l.platform_id, 0), l.name, l.description, l.color, l.is_default, l.updated_at
		FROM middleman_issue_labels il
		JOIN middleman_labels l ON l.id = il.label_id
		WHERE il.issue_id IN (%s)
		ORDER BY l.name, l.id`, sqlPlaceholders(len(ids)))
	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query issue labels: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]Label, len(ids))
	for rows.Next() {
		var ownerID int64
		var label Label
		if err := rows.Scan(&ownerID, &label.ID, &label.RepoID, &label.PlatformID, &label.Name, &label.Description, &label.Color, &label.IsDefault, &label.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan issue label: %w", err)
		}
		out[ownerID] = append(out[ownerID], label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue labels: %w", err)
	}
	return out, nil
}

// PurgeOtherHosts deletes all data for platform hosts other
// than keepHost. Deletes in FK-dependency order so it works
// on existing DBs where CASCADE may not be retrofitted.
func (d *DB) PurgeOtherHosts(ctx context.Context, keepHost string) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		queries := []string{
			`DELETE FROM middleman_starred_items WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?)`,
			`DELETE FROM middleman_mr_worktree_links WHERE merge_request_id IN (SELECT id FROM middleman_merge_requests WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?))`,
			`DELETE FROM middleman_kanban_state WHERE merge_request_id IN (SELECT id FROM middleman_merge_requests WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?))`,
			`DELETE FROM middleman_mr_events WHERE merge_request_id IN (SELECT id FROM middleman_merge_requests WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?))`,
			`DELETE FROM middleman_merge_requests WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?)`,
			`DELETE FROM middleman_issue_events WHERE issue_id IN (SELECT id FROM middleman_issues WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?))`,
			`DELETE FROM middleman_issues WHERE repo_id IN (SELECT id FROM middleman_repos WHERE platform_host != ?)`,
			`DELETE FROM middleman_repos WHERE platform_host != ?`,
			`DELETE FROM middleman_rate_limits WHERE platform_host != ?`,
		}
		for _, q := range queries {
			if _, err := tx.ExecContext(ctx, q, keepHost); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Repos ---

// UpsertRepo inserts a repo if it does not exist, then returns its ID.
// host is the platform hostname (e.g. "github.com" or a GHE hostname).
func (d *DB) UpsertRepo(ctx context.Context, host, owner, name string) (int64, error) {
	host, owner, name = canonicalRepoIdentifier(host, owner, name)
	_, err := d.rw.ExecContext(ctx,
		`INSERT INTO middleman_repos (platform, platform_host, owner, name)
		 VALUES ('github', ?, ?, ?)
		 ON CONFLICT(platform, platform_host, owner, name) DO NOTHING`,
		host, owner, name,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert repo: %w", err)
	}
	var id int64
	err = d.ro.QueryRowContext(ctx,
		`SELECT id FROM middleman_repos
		 WHERE platform = 'github' AND platform_host = ?
		   AND owner = ? AND name = ?`,
		host, owner, name,
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
		        allow_rebase_merge,
		        backfill_pr_page, backfill_pr_complete,
		        backfill_pr_completed_at,
		        backfill_issue_page, backfill_issue_complete,
		        backfill_issue_completed_at,
		        created_at
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
			&r.BackfillPRPage, &r.BackfillPRComplete,
			&r.BackfillPRCompletedAt,
			&r.BackfillIssuePage, &r.BackfillIssueComplete,
			&r.BackfillIssueCompletedAt,
			&r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		normalizeRepoTimestamps(&r)
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// UpdateRepoSyncStarted records the time a sync began.
func (d *DB) UpdateRepoSyncStarted(ctx context.Context, id int64, t time.Time) error {
	t = canonicalUTCTime(t)
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
	t = canonicalUTCTime(t)
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
// Config validation rejects duplicate owner/name across hosts, so this should
// always be unambiguous. The ORDER BY provides deterministic results as a
// safety net if stale data from a previous config exists in the database.
func (d *DB) GetRepoByOwnerName(ctx context.Context, owner, name string) (*Repo, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	var r Repo
	err := d.ro.QueryRowContext(ctx,
		`SELECT id, platform, platform_host, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, allow_squash_merge, allow_merge_commit,
		        allow_rebase_merge,
		        backfill_pr_page, backfill_pr_complete,
		        backfill_pr_completed_at,
		        backfill_issue_page, backfill_issue_complete,
		        backfill_issue_completed_at,
		        created_at
		 FROM middleman_repos WHERE owner = ? AND name = ?
		 ORDER BY platform_host ASC LIMIT 1`, owner, name,
	).Scan(
		&r.ID, &r.Platform, &r.PlatformHost, &r.Owner, &r.Name,
		&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
		&r.LastSyncError,
		&r.AllowSquashMerge, &r.AllowMergeCommit, &r.AllowRebaseMerge,
		&r.BackfillPRPage, &r.BackfillPRComplete,
		&r.BackfillPRCompletedAt,
		&r.BackfillIssuePage, &r.BackfillIssueComplete,
		&r.BackfillIssueCompletedAt,
		&r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo by owner/name: %w", err)
	}
	normalizeRepoTimestamps(&r)
	return &r, nil
}

// GetRepoByID returns the repo with the given ID, or nil if not found.
func (d *DB) GetRepoByID(ctx context.Context, id int64) (*Repo, error) {
	var r Repo
	err := d.ro.QueryRowContext(ctx,
		`SELECT id, platform, platform_host, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, allow_squash_merge, allow_merge_commit,
		        allow_rebase_merge,
		        backfill_pr_page, backfill_pr_complete,
		        backfill_pr_completed_at,
		        backfill_issue_page, backfill_issue_complete,
		        backfill_issue_completed_at,
		        created_at
		 FROM middleman_repos WHERE id = ?`, id,
	).Scan(
		&r.ID, &r.Platform, &r.PlatformHost, &r.Owner, &r.Name,
		&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
		&r.LastSyncError,
		&r.AllowSquashMerge, &r.AllowMergeCommit, &r.AllowRebaseMerge,
		&r.BackfillPRPage, &r.BackfillPRComplete,
		&r.BackfillPRCompletedAt,
		&r.BackfillIssuePage, &r.BackfillIssueComplete,
		&r.BackfillIssueCompletedAt,
		&r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo by id: %w", err)
	}
	normalizeRepoTimestamps(&r)
	return &r, nil
}

func normalizeRepoTimestamps(r *Repo) {
	if r == nil {
		return
	}
	r.CreatedAt = r.CreatedAt.UTC()
	if r.LastSyncStartedAt != nil {
		t := r.LastSyncStartedAt.UTC()
		r.LastSyncStartedAt = &t
	}
	if r.LastSyncCompletedAt != nil {
		t := r.LastSyncCompletedAt.UTC()
		r.LastSyncCompletedAt = &t
	}
	if r.BackfillPRCompletedAt != nil {
		t := r.BackfillPRCompletedAt.UTC()
		r.BackfillPRCompletedAt = &t
	}
	if r.BackfillIssueCompletedAt != nil {
		t := r.BackfillIssueCompletedAt.UTC()
		r.BackfillIssueCompletedAt = &t
	}
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

// UpsertMergeRequest inserts or updates a merge request, returning its internal
// ID. Before writing, all timestamp fields are normalized to UTC so the raw
// SQLite DATETIME text stays comparable in SQL.
// On conflict (repo_id, number), stale snapshots are ignored wholesale.
func (d *DB) UpsertMergeRequest(ctx context.Context, mr *MergeRequest) (int64, error) {
	canonicalizeMergeRequestTimestamps(mr)
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_merge_requests
		    (repo_id, platform_id, number, url, title, author, author_display_name,
		     state, is_draft, body, head_branch, base_branch,
		     platform_head_sha, platform_base_sha,
		     head_repo_clone_url,
		     additions, deletions, comment_count,
		     review_decision, ci_status, ci_checks_json,
		     detail_fetched_at, ci_had_pending,
		     created_at, updated_at,
		     last_activity_at, merged_at, closed_at, mergeable_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		    detail_fetched_at    = COALESCE(middleman_merge_requests.detail_fetched_at, excluded.detail_fetched_at),
		    ci_had_pending       = middleman_merge_requests.ci_had_pending,
		    updated_at           = excluded.updated_at,
		    last_activity_at     = excluded.last_activity_at,
		    merged_at            = excluded.merged_at,
		    closed_at            = excluded.closed_at,
		    mergeable_state      = excluded.mergeable_state
		WHERE excluded.updated_at >= middleman_merge_requests.updated_at`,
		mr.RepoID, mr.PlatformID, mr.Number, mr.URL, mr.Title,
		mr.Author, mr.AuthorDisplayName,
		mr.State, mr.IsDraft, mr.Body, mr.HeadBranch, mr.BaseBranch,
		mr.PlatformHeadSHA, mr.PlatformBaseSHA,
		mr.HeadRepoCloneURL,
		mr.Additions, mr.Deletions, mr.CommentCount, mr.ReviewDecision,
		mr.CIStatus, mr.CIChecksJSON,
		mr.DetailFetchedAt, mr.CIHadPending,
		mr.CreatedAt, mr.UpdatedAt,
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
	_, owner, name = canonicalRepoIdentifier("", owner, name)
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
		       p.detail_fetched_at, p.ci_had_pending,
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
		&mr.MergedAt, &mr.ClosedAt, &mr.MergeableState,
		&mr.DetailFetchedAt, &mr.CIHadPending,
		&mr.KanbanStatus, &mr.Starred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, []int64{mr.ID})
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	mr.Labels = labelsByMR[mr.ID]
	return &mr, nil
}

// GetMergeRequestByRepoIDAndNumber returns a merge request by repo ID and number.
func (d *DB) GetMergeRequestByRepoIDAndNumber(ctx context.Context, repoID int64, number int) (*MergeRequest, error) {
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
		       p.detail_fetched_at, p.ci_had_pending,
		       COALESCE(k.status, '') AS kanban_status,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_merge_requests p
		LEFT JOIN middleman_kanban_state k ON k.merge_request_id = p.id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'pr' AND s.repo_id = p.repo_id AND s.number = p.number
		WHERE p.repo_id = ? AND p.number = ?`,
		repoID, number,
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
		&mr.MergedAt, &mr.ClosedAt, &mr.MergeableState,
		&mr.DetailFetchedAt, &mr.CIHadPending,
		&mr.KanbanStatus, &mr.Starred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merge request by repo id: %w", err)
	}
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, []int64{mr.ID})
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	mr.Labels = labelsByMR[mr.ID]
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
		host, owner, name := canonicalRepoIdentifier(opts.PlatformHost, opts.RepoOwner, opts.RepoName)
		if host != "" {
			conds = append(conds, "r.platform_host = ?")
			args = append(args, host)
		}
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, owner, name)
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
		       p.detail_fetched_at, p.ci_had_pending,
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
	var mrIDs []int64
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
			&mr.MergedAt, &mr.ClosedAt, &mr.MergeableState,
			&mr.DetailFetchedAt, &mr.CIHadPending,
			&mr.KanbanStatus, &mr.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan merge request: %w", err)
		}
		mrs = append(mrs, mr)
		mrIDs = append(mrIDs, mr.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, mrIDs)
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	for i := range mrs {
		mrs[i].Labels = labelsByMR[mrs[i].ID]
	}
	return mrs, nil
}

// --- Events ---

// UpsertMREvents bulk-inserts events after normalizing CreatedAt to UTC.
// When a duplicate dedupe key is seen again, the conflict path refreshes
// mutable fields so edited events and legacy local-offset timestamps are
// repaired during normal sync.
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
			ON CONFLICT(merge_request_id, dedupe_key) DO UPDATE SET
			    platform_id   = excluded.platform_id,
			    event_type    = excluded.event_type,
			    author        = excluded.author,
			    summary       = excluded.summary,
			    body          = excluded.body,
			    metadata_json = excluded.metadata_json,
			    created_at    = excluded.created_at`)
		if err != nil {
			return fmt.Errorf("prepare upsert mr events: %w", err)
		}
		defer stmt.Close()

		for i := range events {
			e := &events[i]
			canonicalizeMREventTimestamps(e)
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

// DeleteMissingMRCommentEvents removes issue_comment rows for a PR whose
// dedupe keys are absent from the latest GitHub comment list.
func (d *DB) DeleteMissingMRCommentEvents(
	ctx context.Context,
	mrID int64,
	dedupeKeys []string,
) error {
	query := `DELETE FROM middleman_mr_events
		WHERE merge_request_id = ? AND event_type = 'issue_comment'`
	args := []any{mrID}
	if len(dedupeKeys) > 0 {
		query += ` AND dedupe_key NOT IN (` + sqlPlaceholders(len(dedupeKeys)) + `)`
		for _, key := range dedupeKeys {
			args = append(args, key)
		}
	}
	if _, err := d.rw.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete missing mr comment events: %w", err)
	}
	return nil
}

// GetMRLatestNonCommentEventTime returns the most recent created_at across
// non-comment events (reviews, commits, force pushes) for a merge request.
// Returns zero time when no such events exist. The comment-only refresh
// paths use this to avoid regressing last_activity_at to a comment-derived
// value when reviews or commits with a newer timestamp are already stored.
func (d *DB) GetMRLatestNonCommentEventTime(ctx context.Context, mrID int64) (time.Time, error) {
	var createdAt sql.NullString
	err := d.ro.QueryRowContext(ctx, `
		SELECT MAX(created_at) FROM middleman_mr_events
		WHERE merge_request_id = ? AND event_type != 'issue_comment'`,
		mrID,
	).Scan(&createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("query latest non-comment mr event: %w", err)
	}
	if !createdAt.Valid {
		return time.Time{}, nil
	}
	t, err := parseDBTime(createdAt.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse latest non-comment mr event time %q: %w", createdAt.String, err)
	}
	return t, nil
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
		var createdAtStr string
		if err := rows.Scan(
			&e.ID, &e.MergeRequestID, &e.PlatformID, &e.EventType, &e.Author, &e.Summary,
			&e.Body, &e.MetadataJSON, &createdAtStr, &e.DedupeKey,
		); err != nil {
			return nil, fmt.Errorf("scan mr event: %w", err)
		}
		t, err := parseDBTime(createdAtStr)
		if err != nil {
			return nil, fmt.Errorf(
				"parse mr event created_at %q: %w",
				createdAtStr, err)
		}
		e.CreatedAt = t
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
	_, owner, name = canonicalRepoIdentifier("", owner, name)
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

// IssueDerivedFields holds computed fields that are refreshed after fetching issue events.
type IssueDerivedFields struct {
	CommentCount   int
	LastActivityAt time.Time
}

// UpdateMRTitleBody updates only the title, body, updated_at, and
// last_activity_at fields. last_activity_at is set to
// MAX(existing, updatedAt) to preserve correct list ordering.
// Derived fields (CommentCount, CIStatus, etc.) are untouched.
func (d *DB) UpdateMRTitleBody(
	ctx context.Context,
	id int64,
	title, body string,
	updatedAt time.Time,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET title = ?, body = ?, updated_at = ?,
		    last_activity_at = MAX(last_activity_at, ?)
		WHERE id = ? AND updated_at <= ?`,
		title, body, updatedAt, updatedAt, id, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("update mr title/body: %w", err)
	}
	return nil
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

// UpdateIssueDerivedFields writes computed fields back to the issues row.
func (d *DB) UpdateIssueDerivedFields(
	ctx context.Context,
	repoID int64,
	number int,
	fields IssueDerivedFields,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_issues
		SET comment_count = ?, last_activity_at = ?
		WHERE repo_id = ? AND number = ?`,
		fields.CommentCount, fields.LastActivityAt,
		repoID, number,
	)
	if err != nil {
		return fmt.Errorf("update issue derived fields: %w", err)
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

// ClearMRCI resets ci_status, ci_checks_json, and ci_had_pending for a
// merge request. UpsertMergeRequest preserves ci_had_pending across
// upserts, so callers that observe a head SHA change need this to drop
// the stale pending flag along with the rest of the CI fields.
func (d *DB) ClearMRCI(
	ctx context.Context,
	repoID int64,
	number int,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET ci_status = '', ci_checks_json = '', ci_had_pending = 0
		WHERE repo_id = ? AND number = ?`,
		repoID, number,
	)
	if err != nil {
		return fmt.Errorf("clear mr ci: %w", err)
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

// UpdatePlatformSHAs stores the platform head/base SHAs for a merge
// request. Called after normalizing GitHub API data or in test setup.
func (d *DB) UpdatePlatformSHAs(
	ctx context.Context,
	repoID int64, number int,
	platformHead, platformBase string,
) error {
	_, err := d.rw.ExecContext(ctx,
		`UPDATE middleman_merge_requests
		 SET platform_head_sha = ?, platform_base_sha = ?
		 WHERE repo_id = ? AND number = ?`,
		platformHead, platformBase, repoID, number,
	)
	if err != nil {
		return fmt.Errorf(
			"update platform SHAs for MR %d: %w", number, err)
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

// Stale reports whether the recorded diff SHAs have drifted from the
// platform SHAs. For merged PRs only head drift matters (the base
// never advances after merge). For open/closed PRs both sides can
// advance and invalidate the diff.
func (s *DiffSHAs) Stale() bool {
	if s.State == "merged" {
		return s.DiffHeadSHA != s.PlatformHeadSHA
	}
	return s.DiffHeadSHA != s.PlatformHeadSHA || s.DiffBaseSHA != s.PlatformBaseSHA
}

// GetDiffSHAs returns the diff-related SHAs for a merge request.
func (d *DB) GetDiffSHAs(ctx context.Context, owner, name string, number int) (*DiffSHAs, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
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

// UpsertIssue inserts or updates an issue, returning its internal ID. Before
// writing, all timestamp fields are normalized to UTC so SQL ordering/filtering
// operates on a consistent storage representation.
// On conflict (repo_id, number), stale snapshots are ignored wholesale.
func (d *DB) UpsertIssue(ctx context.Context, issue *Issue) (int64, error) {
	canonicalizeIssueTimestamps(issue)
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_issues
		    (repo_id, platform_id, number, url, title, author, state,
		     body, comment_count, labels_json, detail_fetched_at,
		     created_at, updated_at, last_activity_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
		    platform_id       = excluded.platform_id,
		    url               = excluded.url,
		    title             = excluded.title,
		    author            = excluded.author,
		    state             = excluded.state,
		    body              = excluded.body,
		    comment_count     = excluded.comment_count,
		    labels_json       = excluded.labels_json,
		    detail_fetched_at = COALESCE(middleman_issues.detail_fetched_at, excluded.detail_fetched_at),
		    updated_at        = excluded.updated_at,
		    last_activity_at  = excluded.last_activity_at,
		    closed_at         = excluded.closed_at
		WHERE excluded.updated_at >= middleman_issues.updated_at`,
		issue.RepoID, issue.PlatformID, issue.Number, issue.URL,
		issue.Title, issue.Author, issue.State,
		issue.Body, issue.CommentCount, issue.LabelsJSON,
		issue.DetailFetchedAt,
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
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	var issue Issue
	err := d.ro.QueryRowContext(ctx, `
		SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
		       i.author, i.state, i.body, i.comment_count, i.labels_json,
		       i.detail_fetched_at,
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
		&issue.DetailFetchedAt,
		&issue.CreatedAt, &issue.UpdatedAt, &issue.LastActivityAt,
		&issue.ClosedAt, &issue.Starred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	labelsByIssue, err := d.loadLabelsForIssues(ctx, []int64{issue.ID})
	if err != nil {
		return nil, fmt.Errorf("load issue labels: %w", err)
	}
	issue.Labels = labelsByIssue[issue.ID]
	return &issue, nil
}

// GetIssueByRepoIDAndNumber returns an issue by repo ID and number.
func (d *DB) GetIssueByRepoIDAndNumber(ctx context.Context, repoID int64, number int) (*Issue, error) {
	var issue Issue
	err := d.ro.QueryRowContext(ctx, `
		SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
		       i.author, i.state, i.body, i.comment_count, i.labels_json,
		       i.detail_fetched_at,
		       i.created_at, i.updated_at, i.last_activity_at, i.closed_at,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_issues i
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'issue' AND s.repo_id = i.repo_id AND s.number = i.number
		WHERE i.repo_id = ? AND i.number = ?`,
		repoID, number,
	).Scan(
		&issue.ID, &issue.RepoID, &issue.PlatformID, &issue.Number,
		&issue.URL, &issue.Title, &issue.Author, &issue.State,
		&issue.Body, &issue.CommentCount, &issue.LabelsJSON,
		&issue.DetailFetchedAt,
		&issue.CreatedAt, &issue.UpdatedAt, &issue.LastActivityAt,
		&issue.ClosedAt, &issue.Starred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue by repo id: %w", err)
	}
	labelsByIssue, err := d.loadLabelsForIssues(ctx, []int64{issue.ID})
	if err != nil {
		return nil, fmt.Errorf("load issue labels: %w", err)
	}
	issue.Labels = labelsByIssue[issue.ID]
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
		host, owner, name := canonicalRepoIdentifier(opts.PlatformHost, opts.RepoOwner, opts.RepoName)
		if host != "" {
			conds = append(conds, "r.platform_host = ?")
			args = append(args, host)
		}
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, owner, name)
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
		       i.detail_fetched_at,
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
	var issueIDs []int64
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(
			&issue.ID, &issue.RepoID, &issue.PlatformID, &issue.Number,
			&issue.URL, &issue.Title, &issue.Author, &issue.State,
			&issue.Body, &issue.CommentCount, &issue.LabelsJSON,
			&issue.DetailFetchedAt,
			&issue.CreatedAt, &issue.UpdatedAt, &issue.LastActivityAt,
			&issue.ClosedAt, &issue.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		issues = append(issues, issue)
		issueIDs = append(issueIDs, issue.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	labelsByIssue, err := d.loadLabelsForIssues(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("load issue labels: %w", err)
	}
	for i := range issues {
		issues[i].Labels = labelsByIssue[issues[i].ID]
	}
	return issues, nil
}

// GetIssueIDByRepoAndNumber returns the internal issue ID for a given repo+number.
func (d *DB) GetIssueIDByRepoAndNumber(
	ctx context.Context, owner, name string, number int,
) (int64, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
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

// --- Detail Fetch Tracking ---

// UpdateMRDetailFetched marks a merge request as having had its
// detail fetched and records whether CI had pending checks.
func (d *DB) UpdateMRDetailFetched(
	ctx context.Context,
	platformHost, repoOwner, repoName string,
	number int, ciHadPending bool,
) error {
	platformHost, repoOwner, repoName = canonicalRepoIdentifier(
		platformHost, repoOwner, repoName,
	)
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_merge_requests
		SET detail_fetched_at = datetime('now'),
		    ci_had_pending = ?
		WHERE repo_id = (
		    SELECT id FROM middleman_repos
		    WHERE platform_host = ? AND owner = ? AND name = ?
		) AND number = ?`,
		ciHadPending, platformHost, repoOwner, repoName, number,
	)
	if err != nil {
		return fmt.Errorf("update mr detail fetched: %w", err)
	}
	return nil
}

// UpdateIssueDetailFetched marks an issue as having had its
// detail fetched.
func (d *DB) UpdateIssueDetailFetched(
	ctx context.Context,
	platformHost, repoOwner, repoName string, number int,
) error {
	platformHost, repoOwner, repoName = canonicalRepoIdentifier(
		platformHost, repoOwner, repoName,
	)
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_issues
		SET detail_fetched_at = datetime('now')
		WHERE repo_id = (
		    SELECT id FROM middleman_repos
		    WHERE platform_host = ? AND owner = ? AND name = ?
		) AND number = ?`,
		platformHost, repoOwner, repoName, number,
	)
	if err != nil {
		return fmt.Errorf("update issue detail fetched: %w", err)
	}
	return nil
}

// UpdateBackfillCursor updates the backfill pagination state for a repo.
func (d *DB) UpdateBackfillCursor(
	ctx context.Context, repoID int64,
	prPage int, prComplete bool, prCompletedAt *time.Time,
	issuePage int, issueComplete bool,
	issueCompletedAt *time.Time,
) error {
	repo := &Repo{
		BackfillPRCompletedAt:    prCompletedAt,
		BackfillIssueCompletedAt: issueCompletedAt,
	}
	canonicalizeRepoTimestamps(repo)
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_repos
		SET backfill_pr_page = ?,
		    backfill_pr_complete = ?,
		    backfill_pr_completed_at = ?,
		    backfill_issue_page = ?,
		    backfill_issue_complete = ?,
		    backfill_issue_completed_at = ?
		WHERE id = ?`,
		prPage, prComplete, repo.BackfillPRCompletedAt,
		issuePage, issueComplete, repo.BackfillIssueCompletedAt,
		repoID,
	)
	if err != nil {
		return fmt.Errorf("update backfill cursor: %w", err)
	}
	return nil
}

// --- Issue Events ---

// UpsertIssueEvents bulk-inserts issue events after normalizing CreatedAt to
// UTC. Duplicate keys refresh mutable fields so edited events and older local
// timestamp encodings are repaired during normal sync.
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
			ON CONFLICT(dedupe_key) DO UPDATE SET
			    issue_id       = excluded.issue_id,
			    platform_id    = excluded.platform_id,
			    event_type     = excluded.event_type,
			    author         = excluded.author,
			    summary        = excluded.summary,
			    body           = excluded.body,
			    metadata_json  = excluded.metadata_json,
			    created_at     = excluded.created_at`)
		if err != nil {
			return fmt.Errorf("prepare upsert issue events: %w", err)
		}
		defer stmt.Close()

		for i := range events {
			e := &events[i]
			canonicalizeIssueEventTimestamps(e)
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

// DeleteMissingIssueCommentEvents removes issue_comment rows for an issue whose
// dedupe keys are absent from the latest GitHub comment list.
func (d *DB) DeleteMissingIssueCommentEvents(
	ctx context.Context,
	issueID int64,
	dedupeKeys []string,
) error {
	query := `DELETE FROM middleman_issue_events
		WHERE issue_id = ? AND event_type = 'issue_comment'`
	args := []any{issueID}
	if len(dedupeKeys) > 0 {
		query += ` AND dedupe_key NOT IN (` + sqlPlaceholders(len(dedupeKeys)) + `)`
		for _, key := range dedupeKeys {
			args = append(args, key)
		}
	}
	if _, err := d.rw.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete missing issue comment events: %w", err)
	}
	return nil
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
		var createdAtStr string
		if err := rows.Scan(
			&e.ID, &e.IssueID, &e.PlatformID, &e.EventType, &e.Author,
			&e.Summary, &e.Body, &e.MetadataJSON, &createdAtStr, &e.DedupeKey,
		); err != nil {
			return nil, fmt.Errorf("scan issue event: %w", err)
		}
		t, err := parseDBTime(createdAtStr)
		if err != nil {
			return nil, fmt.Errorf(
				"parse issue event created_at %q: %w",
				createdAtStr, err)
		}
		e.CreatedAt = t
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListCommentAutocompleteUsers returns repo-scoped username suggestions for comment mentions.
func (d *DB) ListCommentAutocompleteUsers(
	ctx context.Context,
	platformHost, owner, name, query string,
	limit int,
) ([]string, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	containsQuery := "%" + strings.ToLower(query) + "%"
	prefixQuery := strings.ToLower(query) + "%"

	rows, err := d.ro.QueryContext(ctx, `
		WITH repo AS (
			SELECT id
			FROM middleman_repos
			WHERE platform_host = ? AND owner = ? AND name = ?
		), candidates AS (
			SELECT mr.author AS login, mr.last_activity_at AS last_seen
			FROM middleman_merge_requests mr
			WHERE mr.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT i.author AS login, i.last_activity_at AS last_seen
			FROM middleman_issues i
			WHERE i.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT e.author AS login, e.created_at AS last_seen
			FROM middleman_mr_events e
			JOIN middleman_merge_requests mr ON mr.id = e.merge_request_id
			WHERE mr.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT e.author AS login, e.created_at AS last_seen
			FROM middleman_issue_events e
			JOIN middleman_issues i ON i.id = e.issue_id
			WHERE i.repo_id = (SELECT id FROM repo)
		), ranked AS (
			SELECT login, MAX(last_seen) AS last_seen
			FROM candidates
			WHERE login <> ''
			  AND (? = '' OR LOWER(login) LIKE ?)
			GROUP BY login
		)
		SELECT login
		FROM ranked
		ORDER BY
			CASE WHEN ? <> '' AND LOWER(login) LIKE ? THEN 0 ELSE 1 END,
			last_seen DESC,
			login ASC
		LIMIT ?`,
		platformHost, owner, name,
		query, containsQuery,
		query, prefixQuery,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list comment autocomplete users: %w", err)
	}
	defer rows.Close()

	users := make([]string, 0, limit)
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, fmt.Errorf("scan comment autocomplete user: %w", err)
		}
		users = append(users, login)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment autocomplete users: %w", err)
	}
	return users, nil
}

// ListCommentAutocompleteReferences returns repo-scoped # suggestions for pulls and issues.
func (d *DB) ListCommentAutocompleteReferences(
	ctx context.Context,
	platformHost, owner, name, query string,
	limit int,
) ([]CommentAutocompleteReference, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	titleQuery := "%" + strings.ToLower(query) + "%"
	numberPrefix := query + "%"

	rows, err := d.ro.QueryContext(ctx, `
		WITH repo AS (
			SELECT id
			FROM middleman_repos
			WHERE platform_host = ? AND owner = ? AND name = ?
		), candidates AS (
			SELECT 'pull' AS kind, mr.number, mr.title, mr.state, mr.last_activity_at
			FROM middleman_merge_requests mr
			WHERE mr.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT 'issue' AS kind, i.number, i.title, i.state, i.last_activity_at
			FROM middleman_issues i
			WHERE i.repo_id = (SELECT id FROM repo)
		)
		SELECT kind, number, title, state
		FROM candidates
		WHERE ? = ''
		   OR CAST(number AS TEXT) LIKE ?
		   OR LOWER(title) LIKE ?
		ORDER BY
			CASE WHEN ? <> '' AND CAST(number AS TEXT) LIKE ? THEN 0 ELSE 1 END,
			CASE WHEN ? <> '' AND LOWER(title) LIKE ? THEN 0 ELSE 1 END,
			last_activity_at DESC,
			number DESC
		LIMIT ?`,
		platformHost, owner, name,
		query, numberPrefix, titleQuery,
		query, numberPrefix,
		query, titleQuery,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list comment autocomplete references: %w", err)
	}
	defer rows.Close()

	references := make([]CommentAutocompleteReference, 0, limit)
	for rows.Next() {
		var ref CommentAutocompleteReference
		if err := rows.Scan(&ref.Kind, &ref.Number, &ref.Title, &ref.State); err != nil {
			return nil, fmt.Errorf("scan comment autocomplete reference: %w", err)
		}
		references = append(references, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment autocomplete references: %w", err)
	}
	return references, nil
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

// UpsertRateLimit inserts or updates a rate limit row by (platform_host, api_type).
func (d *DB) UpsertRateLimit(
	platformHost string,
	apiType string,
	requestsHour int,
	hourStart time.Time,
	rateRemaining int,
	rateLimit int,
	rateResetAt *time.Time,
) error {
	_, err := d.rw.Exec(`
		INSERT INTO middleman_rate_limits
		    (platform_host, api_type, requests_hour, hour_start,
		     rate_remaining, rate_limit, rate_reset_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(platform_host, api_type) DO UPDATE SET
		    requests_hour  = excluded.requests_hour,
		    hour_start     = excluded.hour_start,
		    rate_remaining = excluded.rate_remaining,
		    rate_limit     = excluded.rate_limit,
		    rate_reset_at  = excluded.rate_reset_at,
		    updated_at     = datetime('now')`,
		platformHost, apiType, requestsHour, hourStart,
		rateRemaining, rateLimit, rateResetAt,
	)
	if err != nil {
		return fmt.Errorf("upsert rate limit: %w", err)
	}
	return nil
}

// GetRateLimit returns the rate limit row for a (platform_host, api_type) pair,
// or nil,nil if not found.
func (d *DB) GetRateLimit(
	platformHost string,
	apiType string,
) (*RateLimit, error) {
	var r RateLimit
	err := d.ro.QueryRow(`
		SELECT id, platform_host, api_type, requests_hour, hour_start,
		       rate_remaining, rate_limit, rate_reset_at, updated_at
		FROM middleman_rate_limits
		WHERE platform_host = ? AND api_type = ?`,
		platformHost, apiType,
	).Scan(
		&r.ID, &r.PlatformHost, &r.APIType, &r.RequestsHour, &r.HourStart,
		&r.RateRemaining, &r.RateLimit, &r.RateResetAt, &r.UpdatedAt,
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
func (d *DB) SetWorktreeLinks(
	ctx context.Context, links []WorktreeLink,
) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM middleman_mr_worktree_links`,
		); err != nil {
			return fmt.Errorf("delete worktree links: %w", err)
		}
		if len(links) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
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
			if _, err := stmt.ExecContext(ctx,
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
	ctx context.Context, mergeRequestID int64,
) ([]WorktreeLink, error) {
	rows, err := d.ro.QueryContext(ctx, `
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
	ctx context.Context, mrIDs []int64,
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
		rows, err := d.ro.QueryContext(ctx, query, args...)
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
func (d *DB) GetAllWorktreeLinks(
	ctx context.Context,
) ([]WorktreeLink, error) {
	rows, err := d.ro.QueryContext(ctx, `
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

// GetRepoByHostOwnerName returns the repo for the given
// host/owner/name triple, or nil if not found.
func (d *DB) GetRepoByHostOwnerName(
	ctx context.Context,
	host, owner, name string,
) (*Repo, error) {
	host, owner, name = canonicalRepoIdentifier(host, owner, name)
	var r Repo
	err := d.ro.QueryRowContext(ctx,
		`SELECT id, platform, platform_host, owner, name,
		        last_sync_started_at, last_sync_completed_at,
		        last_sync_error, allow_squash_merge, allow_merge_commit,
		        allow_rebase_merge,
		        backfill_pr_page, backfill_pr_complete,
		        backfill_pr_completed_at,
		        backfill_issue_page, backfill_issue_complete,
		        backfill_issue_completed_at,
		        created_at
		 FROM middleman_repos
		 WHERE platform_host = ? AND owner = ? AND name = ?`,
		host, owner, name,
	).Scan(
		&r.ID, &r.Platform, &r.PlatformHost, &r.Owner, &r.Name,
		&r.LastSyncStartedAt, &r.LastSyncCompletedAt,
		&r.LastSyncError,
		&r.AllowSquashMerge, &r.AllowMergeCommit, &r.AllowRebaseMerge,
		&r.BackfillPRPage, &r.BackfillPRComplete,
		&r.BackfillPRCompletedAt,
		&r.BackfillIssuePage, &r.BackfillIssueComplete,
		&r.BackfillIssueCompletedAt,
		&r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"get repo by host/owner/name: %w", err,
		)
	}
	normalizeRepoTimestamps(&r)
	return &r, nil
}

// --- Workspaces ---

// InsertWorkspace inserts a new workspace row.
func (d *DB) InsertWorkspace(
	ctx context.Context, ws *Workspace,
) error {
	ws.PlatformHost, ws.RepoOwner, ws.RepoName = canonicalRepoIdentifier(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_workspaces
		    (id, platform_host, repo_owner, repo_name,
		     item_type, item_number, git_head_ref, mr_head_repo,
		     workspace_branch,
		     worktree_path, tmux_session, status,
		     error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ws.ID, ws.PlatformHost, ws.RepoOwner, ws.RepoName,
		ws.ItemType, ws.ItemNumber, ws.GitHeadRef, ws.MRHeadRepo,
		ws.WorkspaceBranch,
		ws.WorktreePath, ws.TmuxSession, ws.Status,
		ws.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert workspace: %w", err)
	}
	return nil
}

// GetWorkspace returns a workspace by ID, or nil if not found.
func (d *DB) GetWorkspace(
	ctx context.Context, id string,
) (*Workspace, error) {
	var ws Workspace
	err := d.ro.QueryRowContext(ctx, `
		SELECT id, platform_host, repo_owner, repo_name,
		       item_type, item_number, git_head_ref, mr_head_repo,
		       workspace_branch,
		       worktree_path, tmux_session, status,
		       error_message, created_at
		FROM middleman_workspaces WHERE id = ?`, id,
	).Scan(
		&ws.ID, &ws.PlatformHost, &ws.RepoOwner, &ws.RepoName,
		&ws.ItemType, &ws.ItemNumber, &ws.GitHeadRef, &ws.MRHeadRepo,
		&ws.WorkspaceBranch,
		&ws.WorktreePath, &ws.TmuxSession, &ws.Status,
		&ws.ErrorMessage, &ws.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	ws.CreatedAt = ws.CreatedAt.UTC()
	return &ws, nil
}

// GetWorkspaceByMR returns the workspace for a specific MR,
// or nil if not found.
func (d *DB) GetWorkspaceByMR(
	ctx context.Context,
	platformHost, owner, name string,
	mrNumber int,
) (*Workspace, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	var ws Workspace
	err := d.ro.QueryRowContext(ctx, `
		SELECT id, platform_host, repo_owner, repo_name,
		       item_type, item_number, git_head_ref, mr_head_repo,
		       workspace_branch,
		       worktree_path, tmux_session, status,
		       error_message, created_at
		FROM middleman_workspaces
		WHERE platform_host = ? AND repo_owner = ?
		  AND repo_name = ? AND item_type = ? AND item_number = ?`,
		platformHost, owner, name, WorkspaceItemTypePullRequest, mrNumber,
	).Scan(
		&ws.ID, &ws.PlatformHost, &ws.RepoOwner, &ws.RepoName,
		&ws.ItemType, &ws.ItemNumber, &ws.GitHeadRef, &ws.MRHeadRepo,
		&ws.WorkspaceBranch,
		&ws.WorktreePath, &ws.TmuxSession, &ws.Status,
		&ws.ErrorMessage, &ws.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace by MR: %w", err)
	}
	ws.CreatedAt = ws.CreatedAt.UTC()
	return &ws, nil
}

// GetWorkspaceByIssue returns the workspace for a specific issue,
// or nil if not found.
func (d *DB) GetWorkspaceByIssue(
	ctx context.Context,
	platformHost, owner, name string,
	issueNumber int,
) (*Workspace, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	var ws Workspace
	err := d.ro.QueryRowContext(ctx, `
		SELECT id, platform_host, repo_owner, repo_name,
		       item_type, item_number, git_head_ref, mr_head_repo,
		       workspace_branch,
		       worktree_path, tmux_session, status,
		       error_message, created_at
		FROM middleman_workspaces
		WHERE platform_host = ? AND repo_owner = ?
		  AND repo_name = ? AND item_type = ? AND item_number = ?`,
		platformHost, owner, name, WorkspaceItemTypeIssue, issueNumber,
	).Scan(
		&ws.ID, &ws.PlatformHost, &ws.RepoOwner, &ws.RepoName,
		&ws.ItemType, &ws.ItemNumber, &ws.GitHeadRef, &ws.MRHeadRepo,
		&ws.WorkspaceBranch,
		&ws.WorktreePath, &ws.TmuxSession, &ws.Status,
		&ws.ErrorMessage, &ws.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace by issue: %w", err)
	}
	ws.CreatedAt = ws.CreatedAt.UTC()
	return &ws, nil
}

// ListWorkspaces returns all workspaces ordered by
// created_at DESC.
func (d *DB) ListWorkspaces(
	ctx context.Context,
) ([]Workspace, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, platform_host, repo_owner, repo_name,
		       item_type, item_number, git_head_ref, mr_head_repo,
		       workspace_branch,
		       worktree_path, tmux_session, status,
		       error_message, created_at
		FROM middleman_workspaces
		ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var out []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(
			&ws.ID, &ws.PlatformHost, &ws.RepoOwner,
			&ws.RepoName, &ws.ItemType, &ws.ItemNumber,
			&ws.GitHeadRef, &ws.MRHeadRepo,
			&ws.WorkspaceBranch,
			&ws.WorktreePath, &ws.TmuxSession,
			&ws.Status, &ws.ErrorMessage, &ws.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		ws.CreatedAt = ws.CreatedAt.UTC()
		out = append(out, ws)
	}
	return out, rows.Err()
}

// UpdateWorkspaceStatus sets the status and optional error
// message for a workspace.
func (d *DB) UpdateWorkspaceStatus(
	ctx context.Context,
	id, status string,
	errMsg *string,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_workspaces
		SET status = ?, error_message = ?
		WHERE id = ?`,
		status, errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("update workspace status: %w", err)
	}
	return nil
}

// UpdateWorkspaceBranch stores the exact branch middleman created
// for a workspace. Empty means setup reused a pre-existing local
// branch and therefore does not own it.
func (d *DB) UpdateWorkspaceBranch(
	ctx context.Context, id, branch string,
) error {
	_, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_workspaces
		SET workspace_branch = ?
		WHERE id = ?`,
		branch, id,
	)
	if err != nil {
		return fmt.Errorf("update workspace branch: %w", err)
	}
	return nil
}

// StartWorkspaceRetry atomically transitions an errored workspace
// into setup state. It returns false when the workspace exists but
// was not in error status at the instant of the update.
func (d *DB) StartWorkspaceRetry(
	ctx context.Context, id string,
) (bool, error) {
	res, err := d.rw.ExecContext(ctx, `
		UPDATE middleman_workspaces
		SET status = 'creating',
		    error_message = NULL
		WHERE id = ? AND status = 'error'`, id,
	)
	if err != nil {
		return false, fmt.Errorf("start workspace retry: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf(
			"start workspace retry rows affected: %w", err,
		)
	}
	return affected == 1, nil
}

// InsertWorkspaceSetupEvent appends an audit event for workspace
// setup activity.
func (d *DB) InsertWorkspaceSetupEvent(
	ctx context.Context, event *WorkspaceSetupEvent,
) error {
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_workspace_setup_events
		    (workspace_id, stage, outcome, message)
		VALUES (?, ?, ?, ?)`,
		event.WorkspaceID, event.Stage, event.Outcome,
		event.Message,
	)
	if err != nil {
		return fmt.Errorf(
			"insert workspace setup event: %w", err,
		)
	}
	return nil
}

// ListWorkspaceSetupEvents returns the audit trail for a single
// workspace setup, ordered by insertion.
func (d *DB) ListWorkspaceSetupEvents(
	ctx context.Context, workspaceID string,
) ([]WorkspaceSetupEvent, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, workspace_id, stage, outcome, message,
		       created_at
		FROM middleman_workspace_setup_events
		WHERE workspace_id = ?
		ORDER BY id`, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list workspace setup events: %w", err,
		)
	}
	defer rows.Close()

	var out []WorkspaceSetupEvent
	for rows.Next() {
		var event WorkspaceSetupEvent
		if err := rows.Scan(
			&event.ID, &event.WorkspaceID, &event.Stage,
			&event.Outcome, &event.Message, &event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan workspace setup event: %w", err,
			)
		}
		event.CreatedAt = event.CreatedAt.UTC()
		out = append(out, event)
	}
	return out, rows.Err()
}

// UpsertWorkspaceTmuxSession records a tmux session owned by a
// runtime launch inside a workspace. Re-launching the same target
// keeps the original row fresh without duplicating it.
func (d *DB) UpsertWorkspaceTmuxSession(
	ctx context.Context,
	session *WorkspaceTmuxSession,
) error {
	createdAt := canonicalUTCTime(session.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_workspace_tmux_sessions
		    (workspace_id, session_name, target_key, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(workspace_id, session_name) DO UPDATE SET
		    target_key = excluded.target_key,
		    created_at = excluded.created_at`,
		session.WorkspaceID, session.SessionName, session.TargetKey,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("upsert workspace tmux session: %w", err)
	}
	return nil
}

// ListWorkspaceTmuxSessions returns stored runtime tmux sessions for
// a workspace ordered by target key and creation time.
func (d *DB) ListWorkspaceTmuxSessions(
	ctx context.Context,
	workspaceID string,
) ([]WorkspaceTmuxSession, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT workspace_id, session_name, target_key, created_at
		FROM middleman_workspace_tmux_sessions
		WHERE workspace_id = ?
		ORDER BY target_key, created_at, session_name`, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspace tmux sessions: %w", err)
	}
	defer rows.Close()

	var out []WorkspaceTmuxSession
	for rows.Next() {
		var session WorkspaceTmuxSession
		if err := rows.Scan(
			&session.WorkspaceID, &session.SessionName,
			&session.TargetKey, &session.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan workspace tmux session: %w", err)
		}
		session.CreatedAt = session.CreatedAt.UTC()
		out = append(out, session)
	}
	return out, rows.Err()
}

// ListAllWorkspaceTmuxSessions returns every stored runtime tmux
// session. It is used by startup cleanup to distinguish live owned
// sessions from stale managed sessions left behind by crashes.
func (d *DB) ListAllWorkspaceTmuxSessions(
	ctx context.Context,
) ([]WorkspaceTmuxSession, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT workspace_id, session_name, target_key, created_at
		FROM middleman_workspace_tmux_sessions
		ORDER BY workspace_id, target_key, created_at, session_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all workspace tmux sessions: %w", err)
	}
	defer rows.Close()

	var out []WorkspaceTmuxSession
	for rows.Next() {
		var session WorkspaceTmuxSession
		if err := rows.Scan(
			&session.WorkspaceID, &session.SessionName,
			&session.TargetKey, &session.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan workspace tmux session: %w", err)
		}
		session.CreatedAt = session.CreatedAt.UTC()
		out = append(out, session)
	}
	return out, rows.Err()
}

// DeleteWorkspaceTmuxSession removes one stored runtime tmux session.
func (d *DB) DeleteWorkspaceTmuxSession(
	ctx context.Context,
	workspaceID string,
	sessionName string,
) error {
	_, err := d.rw.ExecContext(ctx, `
		DELETE FROM middleman_workspace_tmux_sessions
		WHERE workspace_id = ? AND session_name = ?`,
		workspaceID, sessionName,
	)
	if err != nil {
		return fmt.Errorf("delete workspace tmux session: %w", err)
	}
	return nil
}

// DeleteWorkspaceTmuxSessionCreatedAt removes one stored runtime tmux session
// only if it still belongs to the same runtime session generation.
func (d *DB) DeleteWorkspaceTmuxSessionCreatedAt(
	ctx context.Context,
	workspaceID string,
	sessionName string,
	createdAt time.Time,
) (bool, error) {
	result, err := d.rw.ExecContext(ctx, `
		DELETE FROM middleman_workspace_tmux_sessions
		WHERE workspace_id = ? AND session_name = ? AND created_at = ?`,
		workspaceID, sessionName, canonicalUTCTime(createdAt),
	)
	if err != nil {
		return false, fmt.Errorf("delete workspace tmux session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete workspace tmux session rows: %w", err)
	}
	return rows > 0, nil
}

// DeleteWorkspaceTmuxSessions removes every stored runtime tmux
// session for a workspace.
func (d *DB) DeleteWorkspaceTmuxSessions(
	ctx context.Context,
	workspaceID string,
) error {
	_, err := d.rw.ExecContext(ctx, `
		DELETE FROM middleman_workspace_tmux_sessions
		WHERE workspace_id = ?`, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("delete workspace tmux sessions: %w", err)
	}
	return nil
}

// DeleteWorkspace removes a workspace by ID.
func (d *DB) DeleteWorkspace(
	ctx context.Context, id string,
) error {
	_, err := d.rw.ExecContext(ctx,
		`DELETE FROM middleman_workspaces WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}

// workspaceSummaryColumns is the SELECT list shared by
// ListWorkspaceSummaries and GetWorkspaceSummary.
const workspaceSummaryColumns = `
	w.id, w.platform_host, w.repo_owner, w.repo_name,
	w.item_type, w.item_number, w.git_head_ref, w.mr_head_repo,
	w.workspace_branch,
	w.worktree_path, w.tmux_session, w.status,
	w.error_message, w.created_at,
	CASE
	    WHEN w.item_type = 'issue' THEN i.title
	    ELSE m.title
	END,
	CASE
	    WHEN w.item_type = 'issue' THEN i.state
	    ELSE m.state
	END,
	m.is_draft, m.ci_status,
	m.review_decision, m.additions, m.deletions`

// workspaceSummaryJoins is the FROM/JOIN clause shared by
// ListWorkspaceSummaries and GetWorkspaceSummary.
const workspaceSummaryJoins = `
	FROM middleman_workspaces w
	LEFT JOIN middleman_repos r
	    ON r.platform_host = w.platform_host
	   AND r.owner = w.repo_owner
	   AND r.name = w.repo_name
	LEFT JOIN middleman_merge_requests m
	    ON m.repo_id = r.id
	   AND m.number = w.item_number
	   AND w.item_type = 'pull_request'
	LEFT JOIN middleman_issues i
	    ON i.repo_id = r.id
	   AND i.number = w.item_number
	   AND w.item_type = 'issue'`

func scanWorkspaceSummary(
	scanner interface{ Scan(...any) error },
) (*WorkspaceSummary, error) {
	var s WorkspaceSummary
	err := scanner.Scan(
		&s.ID, &s.PlatformHost, &s.RepoOwner, &s.RepoName,
		&s.ItemType, &s.ItemNumber, &s.GitHeadRef, &s.MRHeadRepo,
		&s.WorkspaceBranch,
		&s.WorktreePath, &s.TmuxSession, &s.Status,
		&s.ErrorMessage, &s.CreatedAt,
		&s.MRTitle, &s.MRState, &s.MRIsDraft, &s.MRCIStatus,
		&s.MRReviewDecision, &s.MRAdditions, &s.MRDeletions,
	)
	if err != nil {
		return nil, err
	}
	s.CreatedAt = s.CreatedAt.UTC()
	return &s, nil
}

// ListWorkspaceSummaries returns all workspaces with joined MR
// metadata, ordered by created_at DESC.
func (d *DB) ListWorkspaceSummaries(
	ctx context.Context,
) ([]WorkspaceSummary, error) {
	query := "SELECT " + workspaceSummaryColumns +
		workspaceSummaryJoins +
		"\nORDER BY w.created_at DESC"
	rows, err := d.ro.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf(
			"list workspace summaries: %w", err,
		)
	}
	defer rows.Close()

	var out []WorkspaceSummary
	for rows.Next() {
		s, err := scanWorkspaceSummary(rows)
		if err != nil {
			return nil, fmt.Errorf(
				"scan workspace summary: %w", err,
			)
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetWorkspaceSummary returns a single workspace with joined
// MR metadata, or nil if not found.
func (d *DB) GetWorkspaceSummary(
	ctx context.Context, id string,
) (*WorkspaceSummary, error) {
	query := "SELECT " + workspaceSummaryColumns +
		workspaceSummaryJoins +
		"\nWHERE w.id = ?"
	s, err := scanWorkspaceSummary(
		d.ro.QueryRowContext(ctx, query, id),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"get workspace summary: %w", err,
		)
	}
	return s, nil
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
