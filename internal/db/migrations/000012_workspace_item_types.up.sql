DROP TRIGGER IF EXISTS middleman_workspaces_casefold_update;
DROP TRIGGER IF EXISTS middleman_workspaces_casefold_insert;

CREATE TEMP TABLE middleman_workspace_setup_events_backup AS
SELECT id, workspace_id, stage, outcome, message, created_at
FROM middleman_workspace_setup_events;

DROP INDEX IF EXISTS middleman_workspace_setup_events_workspace_id_idx;
DROP TABLE IF EXISTS middleman_workspace_setup_events;

ALTER TABLE middleman_workspaces
    RENAME TO middleman_workspaces_v11;

CREATE TABLE middleman_workspaces (
    id               TEXT PRIMARY KEY,
    platform_host    TEXT NOT NULL,
    repo_owner       TEXT NOT NULL,
    repo_name        TEXT NOT NULL,
    item_type        TEXT NOT NULL DEFAULT 'pull_request',
    item_number      INTEGER NOT NULL,
    git_head_ref     TEXT NOT NULL,
    mr_head_repo     TEXT,
    worktree_path    TEXT NOT NULL,
    tmux_session     TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'creating',
    error_message    TEXT,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    workspace_branch TEXT NOT NULL DEFAULT '__middleman_unknown__',
    UNIQUE(platform_host, repo_owner, repo_name, item_type, item_number)
);

INSERT INTO middleman_workspaces (
    id, platform_host, repo_owner, repo_name,
    item_type, item_number, git_head_ref, mr_head_repo,
    worktree_path, tmux_session, status,
    error_message, created_at, workspace_branch
)
SELECT
    id, platform_host, repo_owner, repo_name,
    'pull_request', mr_number, mr_head_ref, mr_head_repo,
    worktree_path, tmux_session, status,
    error_message, created_at, workspace_branch
FROM middleman_workspaces_v11;

DROP TABLE middleman_workspaces_v11;

CREATE TRIGGER middleman_workspaces_casefold_insert
BEFORE INSERT ON middleman_workspaces
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_owner <> lower(NEW.repo_owner)
  OR NEW.repo_name <> lower(NEW.repo_name)
BEGIN
    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
END;

CREATE TRIGGER middleman_workspaces_casefold_update
BEFORE UPDATE OF platform_host, repo_owner, repo_name ON middleman_workspaces
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_owner <> lower(NEW.repo_owner)
  OR NEW.repo_name <> lower(NEW.repo_name)
BEGIN
    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
END;

CREATE TABLE middleman_workspace_setup_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
    stage        TEXT NOT NULL,
    outcome      TEXT NOT NULL,
    message      TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO middleman_workspace_setup_events (
    id, workspace_id, stage, outcome, message, created_at
)
SELECT
    id, workspace_id, stage, outcome, message, created_at
FROM middleman_workspace_setup_events_backup;

DROP TABLE middleman_workspace_setup_events_backup;

CREATE INDEX middleman_workspace_setup_events_workspace_id_idx
    ON middleman_workspace_setup_events (workspace_id, id);
