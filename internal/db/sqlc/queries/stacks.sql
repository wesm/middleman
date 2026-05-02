-- name: ListPRsForStackDetection :many
SELECT id, number, title, head_branch, base_branch, state, ci_status, review_decision
FROM middleman_merge_requests
WHERE repo_id = sqlc.arg(repo_id) AND state IN ('open', 'merged')
ORDER BY number;

-- name: UpsertStack :one
INSERT INTO middleman_stacks (repo_id, base_number, name)
VALUES (sqlc.arg(repo_id), sqlc.arg(base_number), sqlc.arg(name))
ON CONFLICT(repo_id, base_number) DO UPDATE SET
    name = excluded.name,
    updated_at = datetime('now')
RETURNING id;

-- name: DeleteStackMembersByStack :exec
DELETE FROM middleman_stack_members
WHERE stack_id = sqlc.arg(stack_id);

-- name: DeleteStackMemberByMR :exec
DELETE FROM middleman_stack_members
WHERE merge_request_id = sqlc.arg(merge_request_id);

-- name: InsertStackMember :exec
INSERT INTO middleman_stack_members (stack_id, merge_request_id, position)
VALUES (sqlc.arg(stack_id), sqlc.arg(merge_request_id), sqlc.arg(position));

-- name: ListStacksWithOpenMembers :many
SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at,
       r.owner, r.name AS repo_name
FROM middleman_stacks s
JOIN middleman_repos r ON r.id = s.repo_id
WHERE EXISTS (
    SELECT 1 FROM middleman_stack_members sm2
    JOIN middleman_merge_requests p2 ON p2.id = sm2.merge_request_id
    WHERE sm2.stack_id = s.id AND p2.state = 'open'
)
ORDER BY s.updated_at DESC;

-- name: ListStacksWithOpenMembersByRepo :many
SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at,
       r.owner, r.name AS repo_name
FROM middleman_stacks s
JOIN middleman_repos r ON r.id = s.repo_id
WHERE r.owner = sqlc.arg(owner)
  AND r.name = sqlc.arg(name)
  AND EXISTS (
      SELECT 1 FROM middleman_stack_members sm2
      JOIN middleman_merge_requests p2 ON p2.id = sm2.merge_request_id
      WHERE sm2.stack_id = s.id AND p2.state = 'open'
  )
ORDER BY s.updated_at DESC;

-- name: ListStackMembersByStackIDs :many
SELECT sm.stack_id, sm.merge_request_id, sm.position,
       p.number, p.title, p.state, p.ci_status, p.review_decision,
       p.is_draft, p.base_branch
FROM middleman_stack_members sm
JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
WHERE sm.stack_id IN (sqlc.slice('stack_ids'))
ORDER BY sm.stack_id, sm.position;

-- name: DeleteAllStacksForRepo :exec
DELETE FROM middleman_stacks
WHERE repo_id = sqlc.arg(repo_id);

-- name: DeleteStaleStacksForRepo :exec
DELETE FROM middleman_stacks
WHERE repo_id = sqlc.arg(repo_id)
  AND id NOT IN (sqlc.slice('active_stack_ids'));

-- name: GetStackForPR :one
SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at
FROM middleman_stacks s
JOIN middleman_stack_members sm ON sm.stack_id = s.id
JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
JOIN middleman_repos r ON r.id = p.repo_id
WHERE r.owner = sqlc.arg(owner)
  AND r.name = sqlc.arg(name)
  AND p.number = sqlc.arg(number);

-- name: ListStackMembersByStack :many
SELECT sm.stack_id, sm.merge_request_id, sm.position,
       p.number, p.title, p.state, p.ci_status, p.review_decision,
       p.is_draft, p.base_branch
FROM middleman_stack_members sm
JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
WHERE sm.stack_id = sqlc.arg(stack_id)
ORDER BY sm.position;
