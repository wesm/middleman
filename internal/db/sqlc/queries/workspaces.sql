-- name: DeleteAllWorktreeLinks :exec
DELETE FROM middleman_mr_worktree_links;

-- name: InsertWorktreeLink :exec
INSERT INTO middleman_mr_worktree_links (
    merge_request_id, worktree_key, worktree_path, worktree_branch, linked_at
)
VALUES (
    sqlc.arg(merge_request_id),
    sqlc.arg(worktree_key),
    sqlc.arg(worktree_path),
    sqlc.arg(worktree_branch),
    sqlc.arg(linked_at)
);

-- name: ListWorktreeLinksForMR :many
SELECT id, merge_request_id, worktree_key,
       worktree_path, worktree_branch, linked_at
FROM middleman_mr_worktree_links
WHERE merge_request_id = sqlc.arg(merge_request_id)
ORDER BY linked_at DESC;

-- name: ListWorktreeLinksForMRIDs :many
SELECT id, merge_request_id, worktree_key,
       worktree_path, worktree_branch, linked_at
FROM middleman_mr_worktree_links
WHERE merge_request_id IN (sqlc.slice('merge_request_ids'))
ORDER BY linked_at DESC;

-- name: ListAllWorktreeLinks :many
SELECT id, merge_request_id, worktree_key,
       worktree_path, worktree_branch, linked_at
FROM middleman_mr_worktree_links
ORDER BY linked_at DESC;

-- name: InsertWorkspace :exec
INSERT INTO middleman_workspaces (
    id, platform_host, repo_owner, repo_name,
    item_type, item_number, associated_pr_number,
    git_head_ref, mr_head_repo, workspace_branch,
    worktree_path, tmux_session, status,
    error_message
)
VALUES (
    sqlc.arg(id), sqlc.arg(platform_host), sqlc.arg(repo_owner), sqlc.arg(repo_name),
    sqlc.arg(item_type), sqlc.arg(item_number), sqlc.narg(associated_pr_number),
    sqlc.arg(git_head_ref), sqlc.narg(mr_head_repo), sqlc.arg(workspace_branch),
    sqlc.arg(worktree_path), sqlc.arg(tmux_session), sqlc.arg(status),
    sqlc.narg(error_message)
);

-- name: GetWorkspace :one
SELECT id, platform_host, repo_owner, repo_name,
       item_type, item_number, associated_pr_number,
       git_head_ref, mr_head_repo, workspace_branch,
       worktree_path, tmux_session, status,
       error_message, created_at
FROM middleman_workspaces
WHERE id = sqlc.arg(id);

-- name: GetWorkspaceByItem :one
SELECT id, platform_host, repo_owner, repo_name,
       item_type, item_number, associated_pr_number,
       git_head_ref, mr_head_repo, workspace_branch,
       worktree_path, tmux_session, status,
       error_message, created_at
FROM middleman_workspaces
WHERE platform_host = sqlc.arg(platform_host)
  AND repo_owner = sqlc.arg(repo_owner)
  AND repo_name = sqlc.arg(repo_name)
  AND item_type = sqlc.arg(item_type)
  AND item_number = sqlc.arg(item_number);

-- name: ListWorkspaces :many
SELECT id, platform_host, repo_owner, repo_name,
       item_type, item_number, associated_pr_number,
       git_head_ref, mr_head_repo, workspace_branch,
       worktree_path, tmux_session, status,
       error_message, created_at
FROM middleman_workspaces
ORDER BY created_at DESC;

-- name: UpdateWorkspaceStatus :exec
UPDATE middleman_workspaces
SET status = sqlc.arg(status),
    error_message = sqlc.narg(error_message)
WHERE id = sqlc.arg(id);

-- name: UpdateWorkspaceBranch :exec
UPDATE middleman_workspaces
SET workspace_branch = sqlc.arg(workspace_branch)
WHERE id = sqlc.arg(id);

-- name: StartWorkspaceRetry :execrows
UPDATE middleman_workspaces
SET status = 'creating',
    error_message = NULL
WHERE id = sqlc.arg(id) AND status = 'error';

-- name: SetWorkspaceAssociatedPRNumberIfNull :execrows
UPDATE middleman_workspaces
SET associated_pr_number = sqlc.arg(associated_pr_number)
WHERE id = sqlc.arg(id) AND associated_pr_number IS NULL;

-- name: InsertWorkspaceSetupEvent :exec
INSERT INTO middleman_workspace_setup_events (
    workspace_id, stage, outcome, message
)
VALUES (
    sqlc.arg(workspace_id), sqlc.arg(stage), sqlc.arg(outcome), sqlc.arg(message)
);

-- name: ListWorkspaceSetupEvents :many
SELECT id, workspace_id, stage, outcome, message, created_at
FROM middleman_workspace_setup_events
WHERE workspace_id = sqlc.arg(workspace_id)
ORDER BY id;

-- name: UpsertWorkspaceTmuxSession :exec
INSERT INTO middleman_workspace_tmux_sessions (
    workspace_id, session_name, target_key, created_at
)
VALUES (
    sqlc.arg(workspace_id), sqlc.arg(session_name), sqlc.arg(target_key), sqlc.arg(created_at)
)
ON CONFLICT(workspace_id, session_name) DO UPDATE SET
    target_key = excluded.target_key,
    created_at = excluded.created_at;

-- name: ListWorkspaceTmuxSessions :many
SELECT workspace_id, session_name, target_key, created_at
FROM middleman_workspace_tmux_sessions
WHERE workspace_id = sqlc.arg(workspace_id)
ORDER BY target_key, created_at, session_name;

-- name: ListAllWorkspaceTmuxSessions :many
SELECT workspace_id, session_name, target_key, created_at
FROM middleman_workspace_tmux_sessions
ORDER BY workspace_id, target_key, created_at, session_name;

-- name: DeleteWorkspaceTmuxSession :exec
DELETE FROM middleman_workspace_tmux_sessions
WHERE workspace_id = sqlc.arg(workspace_id)
  AND session_name = sqlc.arg(session_name);

-- name: DeleteWorkspaceTmuxSessionCreatedAt :execrows
DELETE FROM middleman_workspace_tmux_sessions
WHERE workspace_id = sqlc.arg(workspace_id)
  AND session_name = sqlc.arg(session_name)
  AND created_at = sqlc.arg(created_at);

-- name: DeleteWorkspaceTmuxSessions :exec
DELETE FROM middleman_workspace_tmux_sessions
WHERE workspace_id = sqlc.arg(workspace_id);

-- name: DeleteWorkspace :exec
DELETE FROM middleman_workspaces
WHERE id = sqlc.arg(id);

-- name: ListWorkspaceSummaries :many
SELECT
    w.id, w.platform_host, w.repo_owner, w.repo_name,
    w.item_type, w.item_number, w.associated_pr_number,
    w.git_head_ref, w.mr_head_repo, w.workspace_branch,
    w.worktree_path, w.tmux_session, w.status,
    w.error_message, w.created_at,
    CASE
        WHEN w.item_type = 'issue' THEN i.title
        ELSE m.title
    END AS mr_title,
    CASE
        WHEN w.item_type = 'issue' THEN i.state
        ELSE m.state
    END AS mr_state,
    m.is_draft AS mr_is_draft,
    m.ci_status AS mr_ci_status,
    m.review_decision AS mr_review_decision,
    m.additions AS mr_additions,
    m.deletions AS mr_deletions
FROM middleman_workspaces w
LEFT JOIN middleman_repos r
    ON r.platform_host = w.platform_host
   AND r.owner = w.repo_owner
   AND r.name = w.repo_name
LEFT JOIN middleman_merge_requests m
    ON m.repo_id = r.id
   AND m.number = w.item_number
   AND w.item_type = 'pull_request'
LEFT JOIN middleman_issues i
    ON i.repo_id = r.id
   AND i.number = w.item_number
   AND w.item_type = 'issue'
ORDER BY w.created_at DESC;

-- name: GetWorkspaceSummary :one
SELECT
    w.id, w.platform_host, w.repo_owner, w.repo_name,
    w.item_type, w.item_number, w.associated_pr_number,
    w.git_head_ref, w.mr_head_repo, w.workspace_branch,
    w.worktree_path, w.tmux_session, w.status,
    w.error_message, w.created_at,
    CASE
        WHEN w.item_type = 'issue' THEN i.title
        ELSE m.title
    END AS mr_title,
    CASE
        WHEN w.item_type = 'issue' THEN i.state
        ELSE m.state
    END AS mr_state,
    m.is_draft AS mr_is_draft,
    m.ci_status AS mr_ci_status,
    m.review_decision AS mr_review_decision,
    m.additions AS mr_additions,
    m.deletions AS mr_deletions
FROM middleman_workspaces w
LEFT JOIN middleman_repos r
    ON r.platform_host = w.platform_host
   AND r.owner = w.repo_owner
   AND r.name = w.repo_name
LEFT JOIN middleman_merge_requests m
    ON m.repo_id = r.id
   AND m.number = w.item_number
   AND w.item_type = 'pull_request'
LEFT JOIN middleman_issues i
    ON i.repo_id = r.id
   AND i.number = w.item_number
   AND w.item_type = 'issue'
WHERE w.id = sqlc.arg(id);
