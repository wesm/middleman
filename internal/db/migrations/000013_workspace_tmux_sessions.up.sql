CREATE TABLE IF NOT EXISTS middleman_workspace_tmux_sessions (
    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
    session_name TEXT NOT NULL,
    target_key   TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_id, session_name),
    UNIQUE (session_name)
);

CREATE INDEX IF NOT EXISTS middleman_workspace_tmux_sessions_workspace_id_idx
    ON middleman_workspace_tmux_sessions(workspace_id);
