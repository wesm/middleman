CREATE TABLE IF NOT EXISTS middleman_repo_overviews (
    repo_id                  INTEGER PRIMARY KEY REFERENCES middleman_repos(id) ON DELETE CASCADE,
    latest_release_tag       TEXT NOT NULL DEFAULT '',
    latest_release_name      TEXT NOT NULL DEFAULT '',
    latest_release_url       TEXT NOT NULL DEFAULT '',
    latest_release_target    TEXT NOT NULL DEFAULT '',
    latest_release_prerelease INTEGER NOT NULL DEFAULT 0,
    latest_release_published_at DATETIME,
    commits_since_release    INTEGER,
    commit_timeline_json     TEXT NOT NULL DEFAULT '[]',
    timeline_updated_at      DATETIME,
    updated_at               DATETIME NOT NULL DEFAULT (datetime('now'))
);
