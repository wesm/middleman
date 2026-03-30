CREATE TABLE IF NOT EXISTS repos (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    owner                 TEXT NOT NULL,
    name                  TEXT NOT NULL,
    last_sync_started_at  DATETIME,
    last_sync_completed_at DATETIME,
    last_sync_error       TEXT DEFAULT '',
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(owner, name)
);

CREATE TABLE IF NOT EXISTS pull_requests (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id          INTEGER NOT NULL REFERENCES repos(id),
    github_id        INTEGER NOT NULL,
    number           INTEGER NOT NULL,
    url              TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    author           TEXT NOT NULL DEFAULT '',
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
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    last_activity_at DATETIME NOT NULL,
    merged_at        DATETIME,
    closed_at        DATETIME,
    UNIQUE(repo_id, number),
    UNIQUE(github_id)
);

CREATE TABLE IF NOT EXISTS pr_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id         INTEGER NOT NULL REFERENCES pull_requests(id),
    github_id     INTEGER,
    event_type    TEXT NOT NULL,
    author        TEXT NOT NULL DEFAULT '',
    summary       TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    dedupe_key    TEXT NOT NULL,
    UNIQUE(dedupe_key)
);

CREATE TABLE IF NOT EXISTS kanban_state (
    pr_id      INTEGER PRIMARY KEY REFERENCES pull_requests(id),
    status     TEXT NOT NULL DEFAULT 'new',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_pr_repo_state_activity
    ON pull_requests(repo_id, state, last_activity_at DESC);
CREATE INDEX IF NOT EXISTS idx_pr_state_activity
    ON pull_requests(state, last_activity_at DESC);
CREATE INDEX IF NOT EXISTS idx_kanban_status
    ON kanban_state(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_pr_created
    ON pr_events(pr_id, created_at DESC);
