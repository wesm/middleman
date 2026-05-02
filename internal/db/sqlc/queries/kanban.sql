-- name: EnsureKanbanState :exec
INSERT INTO middleman_kanban_state (merge_request_id, status)
VALUES (sqlc.arg(merge_request_id), 'new')
ON CONFLICT(merge_request_id) DO NOTHING;

-- name: SetKanbanState :exec
INSERT INTO middleman_kanban_state (merge_request_id, status, updated_at)
VALUES (sqlc.arg(merge_request_id), sqlc.arg(status), datetime('now'))
ON CONFLICT(merge_request_id) DO UPDATE SET
    status = excluded.status,
    updated_at = excluded.updated_at;

-- name: GetKanbanState :one
SELECT merge_request_id, status, updated_at
FROM middleman_kanban_state
WHERE merge_request_id = sqlc.arg(merge_request_id);
