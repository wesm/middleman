CREATE TABLE IF NOT EXISTS middleman_stacks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE,
    base_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS middleman_stack_members (
    stack_id INTEGER NOT NULL REFERENCES middleman_stacks(id) ON DELETE CASCADE,
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    PRIMARY KEY (stack_id, merge_request_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_stack_members_mr
    ON middleman_stack_members(merge_request_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_stacks_repo_base
    ON middleman_stacks(repo_id, base_number);
CREATE INDEX IF NOT EXISTS idx_stacks_repo
    ON middleman_stacks(repo_id);
