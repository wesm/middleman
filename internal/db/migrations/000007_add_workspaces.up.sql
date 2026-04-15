CREATE TABLE IF NOT EXISTS middleman_workspaces (
    id            TEXT PRIMARY KEY,
    platform_host TEXT NOT NULL,
    repo_owner    TEXT NOT NULL,
    repo_name     TEXT NOT NULL,
    mr_number     INTEGER NOT NULL,
    mr_head_ref   TEXT NOT NULL,
    mr_head_repo  TEXT,
    worktree_path TEXT NOT NULL,
    tmux_session  TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'creating',
    error_message TEXT,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(platform_host, repo_owner, repo_name, mr_number)
);
