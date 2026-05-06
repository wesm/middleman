-- name: UpsertMergeRequest :exec
INSERT INTO middleman_merge_requests (
    repo_id, platform_id, number, url, title, author, author_display_name,
    state, is_draft, body, head_branch, base_branch,
    platform_head_sha, platform_base_sha,
    head_repo_clone_url,
    additions, deletions, comment_count,
    review_decision, ci_status, ci_checks_json,
    detail_fetched_at, ci_had_pending,
    created_at, updated_at,
    last_activity_at, merged_at, closed_at, mergeable_state
)
VALUES (
    sqlc.arg(repo_id), sqlc.arg(platform_id), sqlc.arg(number), sqlc.arg(url),
    sqlc.arg(title), sqlc.arg(author), sqlc.arg(author_display_name),
    sqlc.arg(state), sqlc.arg(is_draft), sqlc.arg(body), sqlc.arg(head_branch),
    sqlc.arg(base_branch), sqlc.arg(platform_head_sha), sqlc.arg(platform_base_sha),
    sqlc.arg(head_repo_clone_url), sqlc.arg(additions), sqlc.arg(deletions),
    sqlc.arg(comment_count), sqlc.arg(review_decision), sqlc.arg(ci_status),
    sqlc.arg(ci_checks_json), sqlc.narg(detail_fetched_at), sqlc.arg(ci_had_pending),
    sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(last_activity_at),
    sqlc.narg(merged_at), sqlc.narg(closed_at), sqlc.arg(mergeable_state)
)
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
WHERE excluded.updated_at >= middleman_merge_requests.updated_at;

-- name: GetMergeRequestIDByRepoIDAndNumber :one
SELECT id
FROM middleman_merge_requests
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: GetMergeRequestByOwnerNameNumber :one
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
WHERE r.owner = sqlc.arg(owner) AND r.name = sqlc.arg(name) AND p.number = sqlc.arg(number);

-- name: GetMergeRequestByRepoIDAndNumber :one
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
WHERE p.repo_id = sqlc.arg(repo_id) AND p.number = sqlc.arg(number);

-- name: GetMRIDByOwnerNameNumber :one
SELECT p.id
FROM middleman_merge_requests p
JOIN middleman_repos r ON r.id = p.repo_id
WHERE r.owner = sqlc.arg(owner) AND r.name = sqlc.arg(name) AND p.number = sqlc.arg(number);

-- name: ListOpenMRNumbersByRepo :many
SELECT number
FROM middleman_merge_requests
WHERE repo_id = sqlc.arg(repo_id) AND state = 'open';

-- name: UpdateMRTitleBody :exec
UPDATE middleman_merge_requests
SET title = sqlc.arg(title),
    body = sqlc.arg(body),
    updated_at = sqlc.arg(updated_at),
    last_activity_at = MAX(last_activity_at, sqlc.arg(last_activity_at))
WHERE id = sqlc.arg(id) AND updated_at <= sqlc.arg(updated_at);

-- name: UpdateMRDerivedFields :exec
UPDATE middleman_merge_requests
SET review_decision = sqlc.arg(review_decision),
    comment_count = sqlc.arg(comment_count),
    last_activity_at = sqlc.arg(last_activity_at)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdateMRCIStatus :exec
UPDATE middleman_merge_requests
SET ci_status = sqlc.arg(ci_status),
    ci_checks_json = sqlc.arg(ci_checks_json)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: ClearMRCI :exec
UPDATE middleman_merge_requests
SET ci_status = '', ci_checks_json = '', ci_had_pending = 0
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdateClosedMRState :exec
UPDATE middleman_merge_requests
SET state = sqlc.arg(state),
    merged_at = sqlc.narg(merged_at),
    closed_at = sqlc.narg(closed_at),
    updated_at = sqlc.arg(updated_at),
    last_activity_at = sqlc.arg(last_activity_at),
    platform_head_sha = sqlc.arg(platform_head_sha),
    platform_base_sha = sqlc.arg(platform_base_sha)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdateDiffSHAs :exec
UPDATE middleman_merge_requests
SET diff_head_sha = sqlc.arg(diff_head_sha),
    diff_base_sha = sqlc.arg(diff_base_sha),
    merge_base_sha = sqlc.arg(merge_base_sha)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdatePlatformSHAs :exec
UPDATE middleman_merge_requests
SET platform_head_sha = sqlc.arg(platform_head_sha),
    platform_base_sha = sqlc.arg(platform_base_sha)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: GetDiffSHAs :one
SELECT p.platform_head_sha, p.platform_base_sha,
       p.diff_head_sha, p.diff_base_sha, p.merge_base_sha,
       p.state
FROM middleman_merge_requests p
JOIN middleman_repos r ON r.id = p.repo_id
WHERE r.owner = sqlc.arg(owner) AND r.name = sqlc.arg(name) AND p.number = sqlc.arg(number);

-- name: UpdateMRState :exec
UPDATE middleman_merge_requests
SET state = sqlc.arg(state),
    merged_at = sqlc.narg(merged_at),
    closed_at = sqlc.narg(closed_at),
    updated_at = sqlc.arg(updated_at),
    last_activity_at = sqlc.arg(last_activity_at)
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: UpdateMRDetailFetched :exec
UPDATE middleman_merge_requests
SET detail_fetched_at = datetime('now'),
    ci_had_pending = sqlc.arg(ci_had_pending)
WHERE repo_id = (
    SELECT id FROM middleman_repos
    WHERE platform_host = sqlc.arg(platform_host)
      AND owner = sqlc.arg(owner)
      AND name = sqlc.arg(name)
) AND number = sqlc.arg(number);
