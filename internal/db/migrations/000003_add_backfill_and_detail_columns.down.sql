ALTER TABLE middleman_rate_limits DROP COLUMN rate_limit;

ALTER TABLE middleman_issues DROP COLUMN detail_fetched_at;

ALTER TABLE middleman_merge_requests DROP COLUMN ci_had_pending;
ALTER TABLE middleman_merge_requests DROP COLUMN detail_fetched_at;

ALTER TABLE middleman_repos DROP COLUMN backfill_issue_completed_at;
ALTER TABLE middleman_repos DROP COLUMN backfill_issue_complete;
ALTER TABLE middleman_repos DROP COLUMN backfill_issue_page;
ALTER TABLE middleman_repos DROP COLUMN backfill_pr_completed_at;
ALTER TABLE middleman_repos DROP COLUMN backfill_pr_complete;
ALTER TABLE middleman_repos DROP COLUMN backfill_pr_page;
