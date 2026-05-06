-- name: UpsertIssue :exec
INSERT INTO middleman_issues (
    repo_id, platform_id, number, url, title, author, state,
    body, comment_count, labels_json, detail_fetched_at,
    created_at, updated_at, last_activity_at, closed_at
)
VALUES (
    sqlc.arg(repo_id), sqlc.arg(platform_id), sqlc.arg(number),
    sqlc.arg(url), sqlc.arg(title), sqlc.arg(author), sqlc.arg(state),
    sqlc.arg(body), sqlc.arg(comment_count), sqlc.arg(labels_json),
    sqlc.narg(detail_fetched_at), sqlc.arg(created_at),
    sqlc.arg(updated_at), sqlc.arg(last_activity_at), sqlc.narg(closed_at)
)
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
WHERE excluded.updated_at >= middleman_issues.updated_at;

-- name: GetIssueIDByRepoIDAndNumber :one
SELECT id
FROM middleman_issues
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: GetIssueByOwnerNameNumber :one
SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
       i.author, i.state, i.body, i.comment_count, i.labels_json,
       i.detail_fetched_at,
       i.created_at, i.updated_at, i.last_activity_at, i.closed_at,
       (s.number IS NOT NULL) AS starred
FROM middleman_issues i
JOIN middleman_repos r ON r.id = i.repo_id
LEFT JOIN middleman_starred_items s
    ON s.item_type = 'issue' AND s.repo_id = i.repo_id AND s.number = i.number
WHERE r.owner = sqlc.arg(owner) AND r.name = sqlc.arg(name) AND i.number = sqlc.arg(number);

-- name: GetIssueByRepoIDAndNumber :one
SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
       i.author, i.state, i.body, i.comment_count, i.labels_json,
       i.detail_fetched_at,
       i.created_at, i.updated_at, i.last_activity_at, i.closed_at,
       (s.number IS NOT NULL) AS starred
FROM middleman_issues i
LEFT JOIN middleman_starred_items s
    ON s.item_type = 'issue' AND s.repo_id = i.repo_id AND s.number = i.number
WHERE i.repo_id = sqlc.arg(repo_id) AND i.number = sqlc.arg(number);

-- name: GetIssueIDByOwnerNameNumber :one
SELECT i.id
FROM middleman_issues i
JOIN middleman_repos r ON r.id = i.repo_id
WHERE r.owner = sqlc.arg(owner) AND r.name = sqlc.arg(name) AND i.number = sqlc.arg(number);

-- name: ListOpenIssueNumbersByRepo :many
SELECT number
FROM middleman_issues
WHERE repo_id = sqlc.arg(repo_id) AND state = 'open';

-- name: UpdateIssueDerivedFields :exec
UPDATE middleman_issues
SET comment_count = sqlc.arg(comment_count),
    last_activity_at = sqlc.arg(last_activity_at)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdateIssueState :exec
UPDATE middleman_issues
SET state = sqlc.arg(state),
    closed_at = sqlc.narg(closed_at),
    updated_at = sqlc.arg(updated_at),
    last_activity_at = sqlc.arg(last_activity_at)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdateIssueDetailFetched :exec
UPDATE middleman_issues
SET detail_fetched_at = datetime('now')
WHERE repo_id = (
    SELECT id FROM middleman_repos
    WHERE platform_host = sqlc.arg(platform_host)
      AND owner = sqlc.arg(owner)
      AND name = sqlc.arg(name)
) AND number = sqlc.arg(number);

-- name: UpsertIssueEvent :exec
INSERT INTO middleman_issue_events (
    issue_id, platform_id, event_type, author, summary, body,
    metadata_json, created_at, dedupe_key
)
VALUES (
    sqlc.arg(issue_id), sqlc.narg(platform_id), sqlc.arg(event_type),
    sqlc.arg(author), sqlc.arg(summary), sqlc.arg(body), sqlc.arg(metadata_json),
    sqlc.arg(created_at), sqlc.arg(dedupe_key)
)
ON CONFLICT(dedupe_key) DO UPDATE SET
    issue_id       = excluded.issue_id,
    platform_id    = excluded.platform_id,
    event_type     = excluded.event_type,
    author         = excluded.author,
    summary        = excluded.summary,
    body           = excluded.body,
    metadata_json  = excluded.metadata_json,
    created_at     = excluded.created_at;

-- name: IssueCommentEventExists :one
SELECT EXISTS (
    SELECT 1
    FROM middleman_issue_events
    WHERE issue_id = sqlc.arg(issue_id)
      AND platform_id = sqlc.arg(platform_id)
      AND event_type = 'issue_comment'
);

-- name: DeleteAllIssueCommentEvents :exec
DELETE FROM middleman_issue_events
WHERE issue_id = sqlc.arg(issue_id)
  AND event_type = 'issue_comment';

-- name: DeleteMissingIssueCommentEvents :exec
DELETE FROM middleman_issue_events
WHERE issue_id = sqlc.arg(issue_id)
  AND event_type = 'issue_comment'
  AND dedupe_key NOT IN (sqlc.slice('dedupe_keys'));

-- name: ListIssueEvents :many
SELECT id, issue_id, platform_id, event_type, author, summary, body,
       metadata_json, CAST(created_at AS TEXT) AS created_at, dedupe_key
FROM middleman_issue_events
WHERE issue_id = sqlc.arg(issue_id)
ORDER BY created_at DESC;
