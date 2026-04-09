ALTER TABLE middleman_repos ADD COLUMN backfill_pr_page INTEGER NOT NULL DEFAULT 0;
ALTER TABLE middleman_repos ADD COLUMN backfill_pr_complete INTEGER NOT NULL DEFAULT 0;
ALTER TABLE middleman_repos ADD COLUMN backfill_pr_completed_at DATETIME;
ALTER TABLE middleman_repos ADD COLUMN backfill_issue_page INTEGER NOT NULL DEFAULT 0;
ALTER TABLE middleman_repos ADD COLUMN backfill_issue_complete INTEGER NOT NULL DEFAULT 0;
ALTER TABLE middleman_repos ADD COLUMN backfill_issue_completed_at DATETIME;

ALTER TABLE middleman_merge_requests ADD COLUMN detail_fetched_at DATETIME;
ALTER TABLE middleman_merge_requests ADD COLUMN ci_had_pending INTEGER NOT NULL DEFAULT 0;

ALTER TABLE middleman_issues ADD COLUMN detail_fetched_at DATETIME;

ALTER TABLE middleman_rate_limits ADD COLUMN rate_limit INTEGER NOT NULL DEFAULT -1;
