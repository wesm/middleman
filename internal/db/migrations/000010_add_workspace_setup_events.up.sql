CREATE TABLE IF NOT EXISTS middleman_workspace_setup_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    outcome     TEXT NOT NULL,
    message     TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS middleman_workspace_setup_events_workspace_id_idx
    ON middleman_workspace_setup_events (workspace_id, id);
