-- middleman_projects is the registry for local repository checkouts.
-- Each row represents a local working directory the user (or an embedder)
-- has registered with middleman. Projects optionally link to a row in
-- middleman_repos via repo_id; that link is the sole source of truth for
-- platform identity (host/owner/name). Local-only projects with no
-- parseable remote have repo_id NULL.
--
-- ON DELETE SET NULL on the FK: if the linked middleman_repos row is
-- removed, the project becomes local-only rather than being deleted -
-- the on-disk checkout is the source of truth for the project record,
-- and unsyncing a repo should not strand the registered worktree.
CREATE TABLE IF NOT EXISTS middleman_projects (
    id              TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    local_path      TEXT NOT NULL UNIQUE,
    repo_id         INTEGER REFERENCES middleman_repos(id) ON DELETE SET NULL,
    default_branch  TEXT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS middleman_projects_repo_id_idx
    ON middleman_projects (repo_id) WHERE repo_id IS NOT NULL;

-- middleman_project_worktrees records the worktrees the embedder has
-- created on disk for a project. The caller performs the
-- `git worktree add`; middleman only persists the metadata.
CREATE TABLE IF NOT EXISTS middleman_project_worktrees (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES middleman_projects(id) ON DELETE CASCADE,
    branch      TEXT NOT NULL,
    path        TEXT NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS middleman_project_worktrees_project_id_idx
    ON middleman_project_worktrees (project_id);
