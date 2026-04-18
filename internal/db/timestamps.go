package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// canonicalUTCTime converts application timestamps to UTC before they cross
// the SQLite write boundary. This keeps raw text storage stable so SQL
// ordering/filtering on DATETIME columns reflects the actual instant.
func canonicalUTCTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC()
}

func canonicalUTCTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := canonicalUTCTime(*t)
	return &utc
}

// canonicalizeMergeRequestTimestamps enforces the repo-wide contract that PR
// timestamps are stored in UTC, even if the caller constructed local-zone
// fixtures or legacy values upstream.
func canonicalizeMergeRequestTimestamps(mr *MergeRequest) {
	if mr == nil {
		return
	}
	mr.CreatedAt = canonicalUTCTime(mr.CreatedAt)
	mr.UpdatedAt = canonicalUTCTime(mr.UpdatedAt)
	mr.LastActivityAt = canonicalUTCTime(mr.LastActivityAt)
	mr.MergedAt = canonicalUTCTimePtr(mr.MergedAt)
	mr.ClosedAt = canonicalUTCTimePtr(mr.ClosedAt)
	mr.DetailFetchedAt = canonicalUTCTimePtr(mr.DetailFetchedAt)
}

// canonicalizeIssueTimestamps applies the same UTC storage contract to issue
// timestamps before insert/update statements execute.
func canonicalizeIssueTimestamps(issue *Issue) {
	if issue == nil {
		return
	}
	issue.CreatedAt = canonicalUTCTime(issue.CreatedAt)
	issue.UpdatedAt = canonicalUTCTime(issue.UpdatedAt)
	issue.LastActivityAt = canonicalUTCTime(issue.LastActivityAt)
	issue.ClosedAt = canonicalUTCTimePtr(issue.ClosedAt)
	issue.DetailFetchedAt = canonicalUTCTimePtr(issue.DetailFetchedAt)
}

// canonicalizeMREventTimestamps normalizes event times before activity rows
// reach SQLite so raw created_at text stays chronologically sortable.
func canonicalizeMREventTimestamps(event *MREvent) {
	if event == nil {
		return
	}
	event.CreatedAt = canonicalUTCTime(event.CreatedAt)
}

// canonicalizeIssueEventTimestamps normalizes issue activity timestamps before
// they are persisted.
func canonicalizeIssueEventTimestamps(event *IssueEvent) {
	if event == nil {
		return
	}
	event.CreatedAt = canonicalUTCTime(event.CreatedAt)
}

// repairLegacyTimestampStorage rewrites legacy offset-based timestamps into
// UTC on startup. This is intentionally idempotent: reopening a database keeps
// the same instants but refreshes the raw DATETIME text so SQL comparisons no
// longer depend on mixed local/UTC encodings.
func (d *DB) repairLegacyTimestampStorage(ctx context.Context) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		repairs := []struct {
			table  string
			column string
		}{
			{table: "middleman_merge_requests", column: "created_at"},
			{table: "middleman_merge_requests", column: "updated_at"},
			{table: "middleman_merge_requests", column: "last_activity_at"},
			{table: "middleman_merge_requests", column: "merged_at"},
			{table: "middleman_merge_requests", column: "closed_at"},
			{table: "middleman_merge_requests", column: "detail_fetched_at"},
			{table: "middleman_issues", column: "created_at"},
			{table: "middleman_issues", column: "updated_at"},
			{table: "middleman_issues", column: "last_activity_at"},
			{table: "middleman_issues", column: "closed_at"},
			{table: "middleman_issues", column: "detail_fetched_at"},
			{table: "middleman_mr_events", column: "created_at"},
			{table: "middleman_issue_events", column: "created_at"},
		}

		for _, repair := range repairs {
			if err := repairTimestampColumn(ctx, tx, repair.table, repair.column); err != nil {
				return err
			}
		}
		return nil
	})
}

// repairTimestampColumn reparses one timestamp column and writes the same
// instant back in UTC. It operates on rowid so the helper works across all
// timestamp-bearing tables without needing per-table primary-key logic.
func repairTimestampColumn(
	ctx context.Context, tx *sql.Tx, table, column string,
) error {
	query := fmt.Sprintf(
		`SELECT rowid, %s FROM %s WHERE %s IS NOT NULL`,
		column, table, column,
	)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query %s.%s timestamps: %w", table, column, err)
	}
	defer rows.Close()

	type repairRow struct {
		rowID int64
		value time.Time
	}

	var repairs []repairRow
	for rows.Next() {
		var rowID int64
		var raw string
		if err := rows.Scan(&rowID, &raw); err != nil {
			return fmt.Errorf("scan %s.%s timestamp: %w", table, column, err)
		}
		parsed, err := parseDBTime(raw)
		if err != nil {
			return fmt.Errorf("parse %s.%s row %d timestamp %q: %w", table, column, rowID, raw, err)
		}
		repairs = append(repairs, repairRow{rowID: rowID, value: parsed.UTC()})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s.%s timestamps: %w", table, column, err)
	}

	update := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, table, column)
	for _, repair := range repairs {
		if _, err := tx.ExecContext(ctx, update, repair.value, repair.rowID); err != nil {
			return fmt.Errorf("update %s.%s row %d timestamp: %w", table, column, repair.rowID, err)
		}
	}
	return nil
}
