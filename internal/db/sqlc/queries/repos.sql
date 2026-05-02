-- name: UpsertRepo :one
INSERT INTO middleman_repos (platform, platform_host, owner, name)
VALUES ('github', ?, ?, ?)
ON CONFLICT(platform, platform_host, owner, name) DO UPDATE SET
    platform = excluded.platform
RETURNING id;

-- name: ListRepos :many
SELECT id, platform, platform_host, owner, name,
       last_sync_started_at, last_sync_completed_at,
       last_sync_error, allow_squash_merge, allow_merge_commit,
       allow_rebase_merge,
       backfill_pr_page, backfill_pr_complete,
       backfill_pr_completed_at,
       backfill_issue_page, backfill_issue_complete,
       backfill_issue_completed_at,
       created_at
FROM middleman_repos
ORDER BY owner, name;

-- name: GetRepoByOwnerName :one
SELECT id, platform, platform_host, owner, name,
       last_sync_started_at, last_sync_completed_at,
       last_sync_error, allow_squash_merge, allow_merge_commit,
       allow_rebase_merge,
       backfill_pr_page, backfill_pr_complete,
       backfill_pr_completed_at,
       backfill_issue_page, backfill_issue_complete,
       backfill_issue_completed_at,
       created_at
FROM middleman_repos
WHERE owner = ? AND name = ?
ORDER BY platform_host ASC
LIMIT 1;

-- name: GetRepoByID :one
SELECT id, platform, platform_host, owner, name,
       last_sync_started_at, last_sync_completed_at,
       last_sync_error, allow_squash_merge, allow_merge_commit,
       allow_rebase_merge,
       backfill_pr_page, backfill_pr_complete,
       backfill_pr_completed_at,
       backfill_issue_page, backfill_issue_complete,
       backfill_issue_completed_at,
       created_at
FROM middleman_repos
WHERE id = ?;

-- name: GetRepoByHostOwnerName :one
SELECT id, platform, platform_host, owner, name,
       last_sync_started_at, last_sync_completed_at,
       last_sync_error, allow_squash_merge, allow_merge_commit,
       allow_rebase_merge,
       backfill_pr_page, backfill_pr_complete,
       backfill_pr_completed_at,
       backfill_issue_page, backfill_issue_complete,
       backfill_issue_completed_at,
       created_at
FROM middleman_repos
WHERE platform_host = ? AND owner = ? AND name = ?;

-- name: UpdateRepoSyncStarted :exec
UPDATE middleman_repos
SET last_sync_started_at = ?
WHERE id = ?;

-- name: UpdateRepoSyncCompleted :exec
UPDATE middleman_repos
SET last_sync_completed_at = ?, last_sync_error = ?
WHERE id = ?;

-- name: UpdateRepoSettings :exec
UPDATE middleman_repos
SET allow_squash_merge = ?,
    allow_merge_commit = ?,
    allow_rebase_merge = ?
WHERE id = ?;

-- name: UpdateBackfillCursor :exec
UPDATE middleman_repos
SET backfill_pr_page = ?,
    backfill_pr_complete = ?,
    backfill_pr_completed_at = ?,
    backfill_issue_page = ?,
    backfill_issue_complete = ?,
    backfill_issue_completed_at = ?
WHERE id = ?;

-- name: PurgeOtherHostStarredItems :exec
DELETE FROM middleman_starred_items
WHERE repo_id IN (
    SELECT id FROM middleman_repos WHERE platform_host != ?
);

-- name: PurgeOtherHostWorktreeLinks :exec
DELETE FROM middleman_mr_worktree_links
WHERE merge_request_id IN (
    SELECT id FROM middleman_merge_requests
    WHERE repo_id IN (
        SELECT id FROM middleman_repos WHERE platform_host != ?
    )
);

-- name: PurgeOtherHostKanbanState :exec
DELETE FROM middleman_kanban_state
WHERE merge_request_id IN (
    SELECT id FROM middleman_merge_requests
    WHERE repo_id IN (
        SELECT id FROM middleman_repos WHERE platform_host != ?
    )
);

-- name: PurgeOtherHostMergeRequestEvents :exec
DELETE FROM middleman_mr_events
WHERE merge_request_id IN (
    SELECT id FROM middleman_merge_requests
    WHERE repo_id IN (
        SELECT id FROM middleman_repos WHERE platform_host != ?
    )
);

-- name: PurgeOtherHostMergeRequests :exec
DELETE FROM middleman_merge_requests
WHERE repo_id IN (
    SELECT id FROM middleman_repos WHERE platform_host != ?
);

-- name: PurgeOtherHostIssueEvents :exec
DELETE FROM middleman_issue_events
WHERE issue_id IN (
    SELECT id FROM middleman_issues
    WHERE repo_id IN (
        SELECT id FROM middleman_repos WHERE platform_host != ?
    )
);

-- name: PurgeOtherHostIssues :exec
DELETE FROM middleman_issues
WHERE repo_id IN (
    SELECT id FROM middleman_repos WHERE platform_host != ?
);

-- name: PurgeOtherHostRepos :exec
DELETE FROM middleman_repos
WHERE platform_host != ?;

-- name: PurgeOtherHostRateLimits :exec
DELETE FROM middleman_rate_limits
WHERE platform_host != ?;
