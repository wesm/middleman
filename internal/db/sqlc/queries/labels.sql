-- name: LookupLabelIDByName :one
SELECT id
FROM middleman_labels
WHERE repo_id = sqlc.arg(repo_id) AND name = sqlc.arg(name);

-- name: LookupLabelIDByPlatformID :one
SELECT id
FROM middleman_labels
WHERE repo_id = sqlc.arg(repo_id) AND platform_id = sqlc.arg(platform_id);

-- name: GetLabelPlatformID :one
SELECT platform_id
FROM middleman_labels
WHERE id = sqlc.arg(id);

-- name: MoveIssueLabelAssociations :exec
INSERT INTO middleman_issue_labels (issue_id, label_id)
SELECT issue_id, sqlc.arg(to_label_id)
FROM middleman_issue_labels
WHERE middleman_issue_labels.label_id = sqlc.arg(from_label_id)
ON CONFLICT(issue_id, label_id) DO NOTHING;

-- name: DeleteIssueLabelAssociationsByLabel :exec
DELETE FROM middleman_issue_labels
WHERE label_id = sqlc.arg(id);

-- name: MoveMergeRequestLabelAssociations :exec
INSERT INTO middleman_merge_request_labels (merge_request_id, label_id)
SELECT merge_request_id, sqlc.arg(to_label_id)
FROM middleman_merge_request_labels
WHERE middleman_merge_request_labels.label_id = sqlc.arg(from_label_id)
ON CONFLICT(merge_request_id, label_id) DO NOTHING;

-- name: DeleteMergeRequestLabelAssociationsByLabel :exec
DELETE FROM middleman_merge_request_labels
WHERE label_id = sqlc.arg(id);

-- name: DeleteLabelByID :exec
DELETE FROM middleman_labels
WHERE id = sqlc.arg(id);

-- name: InsertLabel :execlastid
INSERT INTO middleman_labels (
    repo_id, platform_id, name, description, color, is_default, updated_at
)
VALUES (
    sqlc.arg(repo_id),
    NULLIF(sqlc.arg(platform_id), 0),
    sqlc.arg(name),
    sqlc.arg(description),
    sqlc.arg(color),
    sqlc.arg(is_default),
    sqlc.arg(updated_at)
);

-- name: UpdateLabel :exec
UPDATE middleman_labels
SET platform_id = COALESCE(NULLIF(sqlc.arg(platform_id), 0), platform_id),
    name = sqlc.arg(name),
    description = sqlc.arg(description),
    color = sqlc.arg(color),
    is_default = sqlc.arg(is_default),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: GetIssueRepoID :one
SELECT repo_id
FROM middleman_issues
WHERE id = sqlc.arg(id);

-- name: GetMergeRequestRepoID :one
SELECT repo_id
FROM middleman_merge_requests
WHERE id = sqlc.arg(id);

-- name: DeleteIssueLabelsByIssueID :exec
DELETE FROM middleman_issue_labels
WHERE issue_id = sqlc.arg(issue_id);

-- name: InsertIssueLabel :exec
INSERT INTO middleman_issue_labels (issue_id, label_id)
VALUES (sqlc.arg(issue_id), sqlc.arg(label_id))
ON CONFLICT(issue_id, label_id) DO NOTHING;

-- name: DeleteMergeRequestLabelsByMRID :exec
DELETE FROM middleman_merge_request_labels
WHERE merge_request_id = sqlc.arg(merge_request_id);

-- name: InsertMergeRequestLabel :exec
INSERT INTO middleman_merge_request_labels (merge_request_id, label_id)
VALUES (sqlc.arg(merge_request_id), sqlc.arg(label_id))
ON CONFLICT(merge_request_id, label_id) DO NOTHING;

-- name: ListLabelsForMergeRequestIDs :many
SELECT ml.merge_request_id, l.id, l.repo_id, COALESCE(l.platform_id, 0) AS platform_id,
       l.name, l.description, l.color, l.is_default, l.updated_at
FROM middleman_merge_request_labels ml
JOIN middleman_labels l ON l.id = ml.label_id
WHERE ml.merge_request_id IN (sqlc.slice('merge_request_ids'))
ORDER BY l.name, l.id;

-- name: ListLabelsForIssueIDs :many
SELECT il.issue_id, l.id, l.repo_id, COALESCE(l.platform_id, 0) AS platform_id,
       l.name, l.description, l.color, l.is_default, l.updated_at
FROM middleman_issue_labels il
JOIN middleman_labels l ON l.id = il.label_id
WHERE il.issue_id IN (sqlc.slice('issue_ids'))
ORDER BY l.name, l.id;
