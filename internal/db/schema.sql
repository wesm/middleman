CREATE TABLE IF NOT EXISTS middleman_repos (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    platform              TEXT NOT NULL DEFAULT 'github',
    platform_host         TEXT NOT NULL DEFAULT 'github.com',
    owner                 TEXT NOT NULL,
    name                  TEXT NOT NULL,
    last_sync_started_at  DATETIME,
    last_sync_completed_at DATETIME,
    last_sync_error       TEXT DEFAULT '',
    allow_squash_merge    INTEGER NOT NULL DEFAULT 1,
    allow_merge_commit    INTEGER NOT NULL DEFAULT 1,
    allow_rebase_merge    INTEGER NOT NULL DEFAULT 1,
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(platform, platform_host, owner, name)
);

CREATE TABLE IF NOT EXISTS middleman_merge_requests (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id          INTEGER NOT NULL REFERENCES middleman_repos(id),
    platform_id      INTEGER NOT NULL,
    number           INTEGER NOT NULL,
    url              TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    author           TEXT NOT NULL DEFAULT '',
    author_display_name TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT 'open',
    is_draft         INTEGER NOT NULL DEFAULT 0,
    body             TEXT NOT NULL DEFAULT '',
    head_branch      TEXT NOT NULL DEFAULT '',
    base_branch      TEXT NOT NULL DEFAULT '',
    additions        INTEGER NOT NULL DEFAULT 0,
    deletions        INTEGER NOT NULL DEFAULT 0,
    comment_count    INTEGER NOT NULL DEFAULT 0,
    review_decision  TEXT NOT NULL DEFAULT '',
    ci_status        TEXT NOT NULL DEFAULT '',
    ci_checks_json   TEXT NOT NULL DEFAULT '',
    platform_head_sha TEXT NOT NULL DEFAULT '',
    platform_base_sha TEXT NOT NULL DEFAULT '',
    diff_head_sha    TEXT NOT NULL DEFAULT '',
    diff_base_sha    TEXT NOT NULL DEFAULT '',
    merge_base_sha   TEXT NOT NULL DEFAULT '',
    head_repo_clone_url TEXT NOT NULL DEFAULT '',
    mergeable_state  TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    last_activity_at DATETIME NOT NULL,
    merged_at        DATETIME,
    closed_at        DATETIME,
    UNIQUE(repo_id, number),
    UNIQUE(repo_id, platform_id)
);

CREATE TABLE IF NOT EXISTS middleman_mr_events (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id),
    platform_id      INTEGER,
    event_type       TEXT NOT NULL,
    author           TEXT NOT NULL DEFAULT '',
    summary          TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    metadata_json    TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    dedupe_key       TEXT NOT NULL,
    UNIQUE(dedupe_key)
);

CREATE TABLE IF NOT EXISTS middleman_kanban_state (
    merge_request_id INTEGER PRIMARY KEY REFERENCES middleman_merge_requests(id),
    status           TEXT NOT NULL DEFAULT 'new',
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_mr_repo_state_activity
    ON middleman_merge_requests(repo_id, state, last_activity_at DESC);
CREATE INDEX IF NOT EXISTS idx_mr_state_activity
    ON middleman_merge_requests(state, last_activity_at DESC);
CREATE INDEX IF NOT EXISTS idx_kanban_status
    ON middleman_kanban_state(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_mr_events_created
    ON middleman_mr_events(merge_request_id, created_at DESC);

CREATE TABLE IF NOT EXISTS middleman_issues (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id          INTEGER NOT NULL REFERENCES middleman_repos(id),
    platform_id      INTEGER NOT NULL,
    number           INTEGER NOT NULL,
    url              TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    author           TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT 'open',
    body             TEXT NOT NULL DEFAULT '',
    comment_count    INTEGER NOT NULL DEFAULT 0,
    labels_json      TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    last_activity_at DATETIME NOT NULL,
    closed_at        DATETIME,
    UNIQUE(repo_id, number),
    UNIQUE(repo_id, platform_id)
);

CREATE TABLE IF NOT EXISTS middleman_issue_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id      INTEGER NOT NULL REFERENCES middleman_issues(id),
    platform_id   INTEGER,
    event_type    TEXT NOT NULL,
    author        TEXT NOT NULL DEFAULT '',
    summary       TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    dedupe_key    TEXT NOT NULL,
    UNIQUE(dedupe_key)
);

CREATE TABLE IF NOT EXISTS middleman_starred_items (
    item_type  TEXT NOT NULL,
    repo_id    INTEGER NOT NULL REFERENCES middleman_repos(id),
    number     INTEGER NOT NULL,
    starred_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(item_type, repo_id, number)
);

CREATE TABLE IF NOT EXISTS middleman_mr_worktree_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    merge_request_id INTEGER NOT NULL
        REFERENCES middleman_merge_requests(id)
        ON DELETE CASCADE,
    worktree_key TEXT NOT NULL,
    worktree_path TEXT,
    worktree_branch TEXT,
    linked_at TEXT NOT NULL,
    UNIQUE(merge_request_id, worktree_key),
    UNIQUE(worktree_key)
);

CREATE INDEX IF NOT EXISTS idx_mr_worktree_links_key
    ON middleman_mr_worktree_links(worktree_key);

CREATE TABLE IF NOT EXISTS middleman_rate_limits (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    platform_host  TEXT NOT NULL UNIQUE,
    requests_hour  INTEGER NOT NULL DEFAULT 0,
    hour_start     DATETIME NOT NULL,
    rate_remaining INTEGER NOT NULL DEFAULT -1,
    rate_reset_at  DATETIME,
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_issues_repo_state_activity
    ON middleman_issues(repo_id, state, last_activity_at DESC);
CREATE INDEX IF NOT EXISTS idx_issues_state_activity
    ON middleman_issues(state, last_activity_at DESC);
CREATE INDEX IF NOT EXISTS idx_issue_events_created
    ON middleman_issue_events(issue_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_starred_items_type
    ON middleman_starred_items(item_type, starred_at DESC);
