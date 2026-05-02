-- name: UpsertMREvent :exec
INSERT INTO middleman_mr_events (
    merge_request_id, platform_id, event_type, author, summary, body,
    metadata_json, created_at, dedupe_key
)
VALUES (
    sqlc.arg(merge_request_id), sqlc.narg(platform_id), sqlc.arg(event_type),
    sqlc.arg(author), sqlc.arg(summary), sqlc.arg(body), sqlc.arg(metadata_json),
    sqlc.arg(created_at), sqlc.arg(dedupe_key)
)
ON CONFLICT(merge_request_id, dedupe_key) DO UPDATE SET
    platform_id   = excluded.platform_id,
    event_type    = excluded.event_type,
    author        = excluded.author,
    summary       = excluded.summary,
    body          = excluded.body,
    metadata_json = excluded.metadata_json,
    created_at    = excluded.created_at;

-- name: MRCommentEventExists :one
SELECT EXISTS (
    SELECT 1
    FROM middleman_mr_events
    WHERE merge_request_id = sqlc.arg(merge_request_id)
      AND platform_id = sqlc.arg(platform_id)
      AND event_type = 'issue_comment'
);

-- name: DeleteAllMRCommentEvents :exec
DELETE FROM middleman_mr_events
WHERE merge_request_id = sqlc.arg(merge_request_id)
  AND event_type = 'issue_comment';

-- name: DeleteMissingMRCommentEvents :exec
DELETE FROM middleman_mr_events
WHERE merge_request_id = sqlc.arg(merge_request_id)
  AND event_type = 'issue_comment'
  AND dedupe_key NOT IN (sqlc.slice('dedupe_keys'));

-- name: GetMRLatestNonCommentEventTime :one
SELECT COALESCE(CAST(MAX(created_at) AS TEXT), '') AS created_at
FROM middleman_mr_events
WHERE merge_request_id = sqlc.arg(merge_request_id)
  AND event_type != 'issue_comment';

-- name: ListMREvents :many
SELECT id, merge_request_id, platform_id, event_type, author, summary, body,
       metadata_json, CAST(created_at AS TEXT) AS created_at, dedupe_key
FROM middleman_mr_events
WHERE merge_request_id = sqlc.arg(merge_request_id)
ORDER BY created_at DESC;
