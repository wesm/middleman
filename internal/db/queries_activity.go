package db

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ListActivity returns a unified, reverse-chronological feed of
// activity across all repos. It merges new PRs, new issues, PR
// events, and issue events into a single stream with cursor-based
// keyset pagination.
func (d *DB) ListActivity(
	ctx context.Context, opts ListActivityOpts,
) ([]ActivityItem, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	var whereClauses []string
	var args []any

	if opts.Repo != "" {
		whereClauses = append(whereClauses,
			"repo_owner || '/' || repo_name = ?")
		args = append(args, opts.Repo)
	}

	if len(opts.Types) > 0 {
		placeholders := make([]string, len(opts.Types))
		for i, t := range opts.Types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		whereClauses = append(whereClauses,
			"activity_type IN ("+strings.Join(placeholders, ",")+")")
	}

	if opts.Search != "" {
		pattern := "%" + opts.Search + "%"
		whereClauses = append(whereClauses,
			"(item_title LIKE ? OR body_preview LIKE ?)")
		args = append(args, pattern, pattern)
	}

	// Time window filter.
	if opts.Since != nil {
		whereClauses = append(whereClauses, "created_at >= ?")
		args = append(args, *opts.Since)
	}

	if opts.BeforeTime != nil {
		whereClauses = append(whereClauses,
			"(created_at < ? OR (created_at = ? AND "+
				"(source < ? OR (source = ? AND source_id < ?))))")
		args = append(args,
			*opts.BeforeTime, *opts.BeforeTime,
			opts.BeforeSource, opts.BeforeSource,
			opts.BeforeSourceID)
	}

	if opts.AfterTime != nil {
		whereClauses = append(whereClauses,
			"(created_at > ? OR (created_at = ? AND "+
				"(source > ? OR (source = ? AND source_id > ?))))")
		args = append(args,
			*opts.AfterTime, *opts.AfterTime,
			opts.AfterSource, opts.AfterSource,
			opts.AfterSourceID)
	}

	where := ""
	if len(whereClauses) > 0 {
		where = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT activity_type, source, source_id,
		       repo_owner, repo_name,
		       item_type, item_number, item_title,
		       item_url, item_state, author,
		       created_at, body_preview
		FROM (
			SELECT 'new_pr' AS activity_type,
			       'pr' AS source, p.id AS source_id,
			       r.owner AS repo_owner, r.name AS repo_name,
			       'pr' AS item_type, p.number AS item_number,
			       p.title AS item_title,
			       p.url AS item_url, p.state AS item_state,
			       p.author, p.created_at,
			       '' AS body_preview
			FROM pull_requests p
			JOIN repos r ON p.repo_id = r.id
			UNION ALL
			SELECT 'new_issue', 'issue', i.id,
			       r.owner, r.name,
			       'issue', i.number, i.title,
			       i.url, i.state,
			       i.author, i.created_at,
			       ''
			FROM issues i
			JOIN repos r ON i.repo_id = r.id
			UNION ALL
			SELECT CASE e.event_type
			           WHEN 'issue_comment' THEN 'comment'
			           ELSE e.event_type
			       END,
			       'pre', e.id,
			       r.owner, r.name,
			       'pr', p.number, p.title,
			       p.url, p.state,
			       e.author, e.created_at,
			       substr(COALESCE(e.body, ''), 1, 200)
			FROM pr_events e
			JOIN pull_requests p ON e.pr_id = p.id
			JOIN repos r ON p.repo_id = r.id
			WHERE e.event_type IN (
				'issue_comment', 'review', 'commit')
			UNION ALL
			SELECT 'comment', 'ise', e.id,
			       r.owner, r.name,
			       'issue', i.number, i.title,
			       i.url, i.state,
			       e.author, e.created_at,
			       substr(COALESCE(e.body, ''), 1, 200)
			FROM issue_events e
			JOIN issues i ON e.issue_id = i.id
			JOIN repos r ON i.repo_id = r.id
			WHERE e.event_type = 'issue_comment'
		) unified
		%s
		ORDER BY created_at DESC, source DESC, source_id DESC
		LIMIT ?`, where)

	args = append(args, limit)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var items []ActivityItem
	for rows.Next() {
		var it ActivityItem
		if err := rows.Scan(
			&it.ActivityType, &it.Source, &it.SourceID,
			&it.RepoOwner, &it.RepoName,
			&it.ItemType, &it.ItemNumber, &it.ItemTitle,
			&it.ItemURL, &it.ItemState, &it.Author,
			&it.CreatedAt, &it.BodyPreview,
		); err != nil {
			return nil, fmt.Errorf("scan activity item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// EncodeCursor encodes a sort position into an opaque cursor string.
func EncodeCursor(
	createdAt time.Time, source string, sourceID int64,
) string {
	raw := fmt.Sprintf("%d:%s:%d",
		createdAt.UnixMilli(), source, sourceID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses an opaque cursor string into its components.
func DecodeCursor(cursor string) (
	time.Time, string, int64, error,
) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", 0,
			fmt.Errorf("decode cursor base64: %w", err)
	}
	parts := strings.SplitN(string(raw), ":", 3)
	if len(parts) != 3 {
		return time.Time{}, "", 0,
			fmt.Errorf("invalid cursor: expected 3 parts, got %d",
				len(parts))
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", 0,
			fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	sourceID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, "", 0,
			fmt.Errorf("invalid cursor source_id: %w", err)
	}
	return time.UnixMilli(ms).UTC(), parts[1], sourceID, nil
}
