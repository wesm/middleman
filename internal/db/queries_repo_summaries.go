package db

import (
	"context"
	"fmt"
)

// ListRepoSummaries returns one summary per tracked repo. The summaries are
// assembled from cached database state only; no live GitHub calls are made.
func (d *DB) ListRepoSummaries(ctx context.Context) ([]RepoSummary, error) {
	repos, err := d.ListRepos(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]RepoSummary, 0, len(repos))
	summaryByRepoID := make(map[int64]*RepoSummary, len(repos))
	for _, repo := range repos {
		summaries = append(summaries, RepoSummary{Repo: repo})
		summaryByRepoID[repo.ID] = &summaries[len(summaries)-1]
	}

	if err := d.loadRepoSummaryStats(ctx, summaryByRepoID); err != nil {
		return nil, err
	}
	if err := d.loadRepoSummaryAuthors(ctx, summaryByRepoID); err != nil {
		return nil, err
	}
	if err := d.loadRepoSummaryIssues(ctx, summaryByRepoID); err != nil {
		return nil, err
	}

	return summaries, nil
}

func (d *DB) loadRepoSummaryStats(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.ro.QueryContext(ctx, `
		WITH pr_stats AS (
			SELECT repo_id,
			       COUNT(*) AS cached_pr_count,
			       SUM(CASE WHEN state = 'open' THEN 1 ELSE 0 END) AS open_pr_count,
			       SUM(CASE WHEN state = 'open' AND is_draft THEN 1 ELSE 0 END) AS draft_pr_count,
			       MAX(last_activity_at) AS last_pr_activity_at
			FROM middleman_merge_requests
			GROUP BY repo_id
		),
		issue_stats AS (
			SELECT repo_id,
			       COUNT(*) AS cached_issue_count,
			       SUM(CASE WHEN state = 'open' THEN 1 ELSE 0 END) AS open_issue_count,
			       MAX(last_activity_at) AS last_issue_activity_at
			FROM middleman_issues
			GROUP BY repo_id
		)
		SELECT r.id,
		       COALESCE(pr.cached_pr_count, 0),
		       COALESCE(pr.open_pr_count, 0),
		       COALESCE(pr.draft_pr_count, 0),
		       COALESCE(i.cached_issue_count, 0),
		       COALESCE(i.open_issue_count, 0),
		       CASE
		           WHEN pr.last_pr_activity_at IS NULL THEN i.last_issue_activity_at
		           WHEN i.last_issue_activity_at IS NULL THEN pr.last_pr_activity_at
		           WHEN pr.last_pr_activity_at >= i.last_issue_activity_at THEN pr.last_pr_activity_at
		           ELSE i.last_issue_activity_at
		       END AS most_recent_activity_at
		FROM middleman_repos r
		LEFT JOIN pr_stats pr ON pr.repo_id = r.id
		LEFT JOIN issue_stats i ON i.repo_id = r.id
		ORDER BY r.owner, r.name`,
	)
	if err != nil {
		return fmt.Errorf("list repo summary stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			repoID             int64
			cachedPRCount      int
			openPRCount        int
			draftPRCount       int
			cachedIssueCount   int
			openIssueCount     int
			mostRecentActivity *string
		)
		if err := rows.Scan(
			&repoID,
			&cachedPRCount,
			&openPRCount,
			&draftPRCount,
			&cachedIssueCount,
			&openIssueCount,
			&mostRecentActivity,
		); err != nil {
			return fmt.Errorf("scan repo summary stats: %w", err)
		}

		summary := summaryByRepoID[repoID]
		if summary == nil {
			continue
		}
		summary.CachedPRCount = cachedPRCount
		summary.OpenPRCount = openPRCount
		summary.DraftPRCount = draftPRCount
		summary.CachedIssueCount = cachedIssueCount
		summary.OpenIssueCount = openIssueCount
		if mostRecentActivity != nil {
			t, err := parseDBTime(*mostRecentActivity)
			if err != nil {
				return fmt.Errorf("parse repo summary activity %q: %w", *mostRecentActivity, err)
			}
			summary.MostRecentActivityAt = &t
		}
	}

	return rows.Err()
}

func (d *DB) loadRepoSummaryAuthors(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.ro.QueryContext(ctx, `
		WITH author_items AS (
			SELECT repo_id, author, last_activity_at
			FROM middleman_merge_requests
			WHERE author <> ''
			UNION ALL
			SELECT repo_id, author, last_activity_at
			FROM middleman_issues
			WHERE author <> ''
		),
		author_totals AS (
			SELECT repo_id,
			       author,
			       COUNT(*) AS item_count,
			       MAX(last_activity_at) AS most_recent_activity_at
			FROM author_items
			GROUP BY repo_id, author
		),
		ranked AS (
			SELECT repo_id,
			       author,
			       item_count,
			       ROW_NUMBER() OVER (
			           PARTITION BY repo_id
			           ORDER BY item_count DESC, most_recent_activity_at DESC, author ASC
			       ) AS rank
			FROM author_totals
		)
		SELECT repo_id, author, item_count
		FROM ranked
		WHERE rank <= 3
		ORDER BY repo_id, rank`,
	)
	if err != nil {
		return fmt.Errorf("list repo summary authors: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			repoID    int64
			login     string
			itemCount int
		)
		if err := rows.Scan(&repoID, &login, &itemCount); err != nil {
			return fmt.Errorf("scan repo summary author: %w", err)
		}

		summary := summaryByRepoID[repoID]
		if summary == nil {
			continue
		}
		summary.ActiveAuthors = append(summary.ActiveAuthors, RepoActivityAuthor{
			Login:     login,
			ItemCount: itemCount,
		})
	}

	return rows.Err()
}

func (d *DB) loadRepoSummaryIssues(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.ro.QueryContext(ctx, `
		WITH ranked AS (
			SELECT repo_id,
			       number,
			       title,
			       author,
			       state,
			       url,
			       last_activity_at,
			       ROW_NUMBER() OVER (
			           PARTITION BY repo_id
			           ORDER BY last_activity_at DESC, number DESC
			       ) AS rank
			FROM middleman_issues
			WHERE state = 'open'
		)
		SELECT repo_id, number, title, author, state, url, last_activity_at
		FROM ranked
		WHERE rank <= 3
		ORDER BY repo_id, rank`,
	)
	if err != nil {
		return fmt.Errorf("list repo summary issues: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			repoID         int64
			issue          RepoIssueHeadline
			lastActivityAt string
		)
		if err := rows.Scan(
			&repoID,
			&issue.Number,
			&issue.Title,
			&issue.Author,
			&issue.State,
			&issue.URL,
			&lastActivityAt,
		); err != nil {
			return fmt.Errorf("scan repo summary issue: %w", err)
		}
		t, err := parseDBTime(lastActivityAt)
		if err != nil {
			return fmt.Errorf("parse repo summary issue activity %q: %w", lastActivityAt, err)
		}
		issue.LastActivityAt = t

		summary := summaryByRepoID[repoID]
		if summary == nil {
			continue
		}
		summary.RecentIssues = append(summary.RecentIssues, issue)
	}

	return rows.Err()
}
