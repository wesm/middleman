ALTER TABLE middleman_repos ADD COLUMN platform_repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN owner_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN name_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN repo_path_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN web_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN clone_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN default_branch TEXT NOT NULL DEFAULT '';

UPDATE middleman_repos
SET
    platform = lower(trim(platform)),
    platform_host = lower(trim(platform_host)),
    owner = CASE
        WHEN lower(trim(platform)) = 'github' THEN lower(trim(owner))
        ELSE trim(owner)
    END,
    name = CASE
        WHEN lower(trim(platform)) = 'github' THEN lower(trim(name))
        ELSE trim(name)
    END,
    repo_path = CASE
        WHEN lower(trim(platform)) = 'github'
            THEN lower(trim(owner) || '/' || trim(name))
        ELSE trim(owner) || '/' || trim(name)
    END;

UPDATE middleman_repos
SET
    owner_key = lower(owner),
    name_key = lower(name),
    repo_path_key = lower(repo_path);

CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_platform_repo_id
    ON middleman_repos(platform, platform_host, platform_repo_id)
    WHERE platform_repo_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_provider_path_key
    ON middleman_repos(platform, platform_host, repo_path_key)
    WHERE repo_path_key <> '';

ALTER TABLE middleman_merge_requests ADD COLUMN platform_external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_issues ADD COLUMN platform_external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_labels ADD COLUMN platform_external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_mr_events ADD COLUMN platform_external_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_merge_requests_repo_platform_external_id
    ON middleman_merge_requests(repo_id, platform_external_id)
    WHERE platform_external_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_repo_platform_external_id
    ON middleman_issues(repo_id, platform_external_id)
    WHERE platform_external_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_labels_repo_platform_external_id
    ON middleman_labels(repo_id, platform_external_id)
    WHERE platform_external_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_mr_events_platform_external_id
    ON middleman_mr_events(merge_request_id, event_type, platform_external_id)
    WHERE platform_external_id <> '';

DROP INDEX IF EXISTS idx_issue_events_created;

CREATE TABLE middleman_issue_events_v17 (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id             INTEGER NOT NULL REFERENCES middleman_issues(id) ON DELETE CASCADE,
    platform_id          INTEGER,
    platform_external_id TEXT NOT NULL DEFAULT '',
    event_type           TEXT NOT NULL,
    author               TEXT NOT NULL DEFAULT '',
    summary              TEXT NOT NULL DEFAULT '',
    body                 TEXT NOT NULL DEFAULT '',
    metadata_json        TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL,
    dedupe_key           TEXT NOT NULL,
    UNIQUE(issue_id, dedupe_key)
);

INSERT INTO middleman_issue_events_v17 (
    id, issue_id, platform_id, platform_external_id, event_type,
    author, summary, body, metadata_json, created_at, dedupe_key
)
SELECT
    id, issue_id, platform_id, '', event_type,
    COALESCE(author, ''), COALESCE(summary, ''), COALESCE(body, ''),
    COALESCE(metadata_json, ''), created_at, dedupe_key
FROM middleman_issue_events;

DROP TABLE middleman_issue_events;
ALTER TABLE middleman_issue_events_v17 RENAME TO middleman_issue_events;

CREATE INDEX IF NOT EXISTS idx_issue_events_created
    ON middleman_issue_events(issue_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_issue_events_platform_external_id
    ON middleman_issue_events(issue_id, event_type, platform_external_id)
    WHERE platform_external_id <> '';

DROP TRIGGER IF EXISTS middleman_repos_casefold_insert;
DROP TRIGGER IF EXISTS middleman_repos_casefold_update;

CREATE TRIGGER middleman_repos_casefold_insert
BEFORE INSERT ON middleman_repos
WHEN NEW.platform <> lower(NEW.platform)
  OR NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_path = ''
  OR NEW.owner_key <> lower(NEW.owner)
  OR NEW.name_key <> lower(NEW.name)
  OR NEW.repo_path_key <> lower(NEW.repo_path)
  OR (
      lower(NEW.platform) = 'github'
      AND (
          NEW.owner <> lower(NEW.owner)
          OR NEW.name <> lower(NEW.name)
          OR NEW.repo_path <> lower(NEW.repo_path)
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be provider-canonical');
END;

CREATE TRIGGER middleman_repos_casefold_update
BEFORE UPDATE OF platform, platform_host, owner, name, repo_path, owner_key, name_key, repo_path_key ON middleman_repos
WHEN NEW.platform <> lower(NEW.platform)
  OR NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_path = ''
  OR NEW.owner_key <> lower(NEW.owner)
  OR NEW.name_key <> lower(NEW.name)
  OR NEW.repo_path_key <> lower(NEW.repo_path)
  OR (
      lower(NEW.platform) = 'github'
      AND (
          NEW.owner <> lower(NEW.owner)
          OR NEW.name <> lower(NEW.name)
          OR NEW.repo_path <> lower(NEW.repo_path)
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be provider-canonical');
END;

DROP TRIGGER IF EXISTS middleman_workspaces_casefold_insert;
DROP TRIGGER IF EXISTS middleman_workspaces_casefold_update;

CREATE TEMP TABLE middleman_workspace_setup_events_backup AS
SELECT id, workspace_id, stage, outcome, message, created_at
FROM middleman_workspace_setup_events;

CREATE TEMP TABLE middleman_workspace_tmux_sessions_backup AS
SELECT workspace_id, session_name, target_key, created_at
FROM middleman_workspace_tmux_sessions;

DROP INDEX IF EXISTS middleman_workspace_setup_events_workspace_id_idx;
DROP TABLE IF EXISTS middleman_workspace_setup_events;
DROP INDEX IF EXISTS middleman_workspace_tmux_sessions_workspace_id_idx;
DROP TABLE IF EXISTS middleman_workspace_tmux_sessions;

ALTER TABLE middleman_workspaces
    RENAME TO middleman_workspaces_v16;

CREATE TABLE middleman_workspaces (
    id                   TEXT PRIMARY KEY,
    platform             TEXT NOT NULL DEFAULT 'github',
    platform_host        TEXT NOT NULL,
    repo_owner           TEXT NOT NULL,
    repo_name            TEXT NOT NULL,
    item_type            TEXT NOT NULL DEFAULT 'pull_request',
    item_number          INTEGER NOT NULL,
    git_head_ref         TEXT NOT NULL,
    mr_head_repo         TEXT,
    worktree_path        TEXT NOT NULL,
    tmux_session         TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'creating',
    error_message        TEXT,
    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    workspace_branch     TEXT NOT NULL DEFAULT '__middleman_unknown__',
    associated_pr_number INTEGER,
    repo_owner_key       TEXT NOT NULL DEFAULT '',
    repo_name_key        TEXT NOT NULL DEFAULT '',
    repo_path_key        TEXT NOT NULL DEFAULT ''
);

INSERT INTO middleman_workspaces (
    id, platform_host, repo_owner, repo_name,
    item_type, item_number, git_head_ref, mr_head_repo,
    worktree_path, tmux_session, status, error_message, created_at,
    workspace_branch, associated_pr_number,
    repo_owner_key, repo_name_key, repo_path_key
)
SELECT
    id, lower(trim(platform_host)), lower(trim(repo_owner)), lower(trim(repo_name)),
    item_type, item_number, git_head_ref, mr_head_repo,
    worktree_path, tmux_session, status, error_message, created_at,
    workspace_branch, associated_pr_number,
    lower(trim(repo_owner)), lower(trim(repo_name)),
    lower(trim(repo_owner)) || '/' || lower(trim(repo_name))
FROM middleman_workspaces_v16;

DROP TABLE middleman_workspaces_v16;

UPDATE middleman_workspaces
SET
    repo_owner_key = lower(trim(repo_owner)),
    repo_name_key = lower(trim(repo_name)),
    repo_path_key = lower(trim(repo_owner)) || '/' || lower(trim(repo_name));

UPDATE middleman_workspaces AS w
SET
    platform = COALESCE((
        SELECT r.platform
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), platform),
    repo_owner_key = COALESCE((
        SELECT r.owner_key
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_owner_key),
    repo_name_key = COALESCE((
        SELECT r.name_key
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_name_key),
    repo_path_key = COALESCE((
        SELECT r.repo_path_key
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_path_key),
    repo_owner = COALESCE((
        SELECT r.owner
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_owner),
    repo_name = COALESCE((
        SELECT r.name
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_name);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_provider_item_key
    ON middleman_workspaces(platform, platform_host, repo_path_key, item_type, item_number);

CREATE TRIGGER middleman_workspaces_casefold_insert
BEFORE INSERT ON middleman_workspaces
WHEN NEW.platform <> lower(NEW.platform)
  OR NEW.platform_host <> lower(NEW.platform_host)
  OR (
      NEW.repo_path_key = ''
      AND (
          NEW.repo_owner <> lower(NEW.repo_owner)
          OR NEW.repo_name <> lower(NEW.repo_name)
      )
  )
  OR (
      NEW.repo_path_key <> ''
      AND (
          NEW.repo_owner_key <> lower(NEW.repo_owner_key)
          OR NEW.repo_name_key <> lower(NEW.repo_name_key)
          OR NEW.repo_path_key <> lower(NEW.repo_path_key)
          OR NEW.repo_path_key <> NEW.repo_owner_key || '/' || NEW.repo_name_key
      )
  )
  OR (
      NEW.repo_path_key <> ''
      AND
      NOT EXISTS (
          SELECT 1
          FROM middleman_repos r
          WHERE r.platform = NEW.platform
            AND r.platform_host = NEW.platform_host
            AND r.repo_path_key = NEW.repo_path_key
            AND r.platform <> 'github'
      )
      AND (
          NEW.repo_owner <> NEW.repo_owner_key
          OR NEW.repo_name <> NEW.repo_name_key
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'workspace repo identifiers must be provider-canonical');
END;

CREATE TRIGGER middleman_workspaces_key_fill_insert
AFTER INSERT ON middleman_workspaces
WHEN NEW.repo_path_key = ''
BEGIN
    UPDATE middleman_workspaces
    SET repo_owner_key = lower(repo_owner),
        repo_name_key = lower(repo_name),
        repo_path_key = lower(repo_owner) || '/' || lower(repo_name)
    WHERE id = NEW.id;
END;

CREATE TRIGGER middleman_workspaces_casefold_update
BEFORE UPDATE OF platform, platform_host, repo_owner, repo_name, repo_owner_key, repo_name_key, repo_path_key ON middleman_workspaces
WHEN NEW.platform <> lower(NEW.platform)
  OR NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_path_key = ''
  OR NEW.repo_owner_key <> lower(NEW.repo_owner_key)
  OR NEW.repo_name_key <> lower(NEW.repo_name_key)
  OR NEW.repo_path_key <> lower(NEW.repo_path_key)
  OR NEW.repo_path_key <> NEW.repo_owner_key || '/' || NEW.repo_name_key
  OR (
      NOT EXISTS (
          SELECT 1
          FROM middleman_repos r
          WHERE r.platform = NEW.platform
            AND r.platform_host = NEW.platform_host
            AND r.repo_path_key = NEW.repo_path_key
            AND r.platform <> 'github'
      )
      AND (
          NEW.repo_owner <> NEW.repo_owner_key
          OR NEW.repo_name <> NEW.repo_name_key
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'workspace repo identifiers must be provider-canonical');
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
SELECT id, workspace_id, stage, outcome, message, created_at
FROM middleman_workspace_setup_events_backup;

DROP TABLE middleman_workspace_setup_events_backup;

CREATE INDEX middleman_workspace_setup_events_workspace_id_idx
    ON middleman_workspace_setup_events (workspace_id, id);

CREATE TABLE middleman_workspace_tmux_sessions (
    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
    session_name TEXT NOT NULL,
    target_key   TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_id, session_name),
    UNIQUE (session_name)
);

INSERT INTO middleman_workspace_tmux_sessions (
    workspace_id, session_name, target_key, created_at
)
SELECT workspace_id, session_name, target_key, created_at
FROM middleman_workspace_tmux_sessions_backup;

DROP TABLE middleman_workspace_tmux_sessions_backup;

CREATE INDEX middleman_workspace_tmux_sessions_workspace_id_idx
    ON middleman_workspace_tmux_sessions(workspace_id);
