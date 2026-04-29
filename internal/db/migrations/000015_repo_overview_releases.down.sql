CREATE TABLE middleman_repo_overviews_old (
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

INSERT INTO middleman_repo_overviews_old (
    repo_id,
    latest_release_tag,
    latest_release_name,
    latest_release_url,
    latest_release_target,
    latest_release_prerelease,
    latest_release_published_at,
    commits_since_release,
    commit_timeline_json,
    timeline_updated_at,
    updated_at
)
SELECT
    repo_id,
    latest_release_tag,
    latest_release_name,
    latest_release_url,
    latest_release_target,
    latest_release_prerelease,
    latest_release_published_at,
    commits_since_release,
    commit_timeline_json,
    timeline_updated_at,
    updated_at
FROM middleman_repo_overviews;

DROP TABLE middleman_repo_overviews;
ALTER TABLE middleman_repo_overviews_old RENAME TO middleman_repo_overviews;
