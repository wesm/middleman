-- Rate limits: add api_type column, change UNIQUE constraint
CREATE TABLE middleman_rate_limits_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    platform_host  TEXT NOT NULL,
    api_type       TEXT NOT NULL DEFAULT 'rest',
    requests_hour  INTEGER NOT NULL DEFAULT 0,
    hour_start     DATETIME NOT NULL,
    rate_remaining INTEGER NOT NULL DEFAULT -1,
    rate_limit     INTEGER NOT NULL DEFAULT -1,
    rate_reset_at  DATETIME,
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(platform_host, api_type)
);

INSERT INTO middleman_rate_limits_new
    (id, platform_host, api_type, requests_hour, hour_start,
     rate_remaining, rate_limit, rate_reset_at, updated_at)
SELECT id, platform_host, 'rest', requests_hour, hour_start,
       rate_remaining, rate_limit, rate_reset_at, updated_at
FROM middleman_rate_limits;

DROP TABLE middleman_rate_limits;

ALTER TABLE middleman_rate_limits_new RENAME TO middleman_rate_limits;

-- Normalized labels
CREATE TABLE IF NOT EXISTS middleman_labels (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE,
    platform_id INTEGER,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '',
    is_default  INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL,
    UNIQUE(repo_id, name),
    UNIQUE(repo_id, platform_id)
);

CREATE TABLE IF NOT EXISTS middleman_merge_request_labels (
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
    label_id         INTEGER NOT NULL REFERENCES middleman_labels(id) ON DELETE CASCADE,
    PRIMARY KEY (merge_request_id, label_id)
);

CREATE TABLE IF NOT EXISTS middleman_issue_labels (
    issue_id  INTEGER NOT NULL REFERENCES middleman_issues(id) ON DELETE CASCADE,
    label_id  INTEGER NOT NULL REFERENCES middleman_labels(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

CREATE INDEX IF NOT EXISTS idx_labels_repo_name
    ON middleman_labels(repo_id, name);
CREATE INDEX IF NOT EXISTS idx_labels_repo_platform_id
    ON middleman_labels(repo_id, platform_id)
    WHERE platform_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_merge_request_labels_label_id
    ON middleman_merge_request_labels(label_id);
CREATE INDEX IF NOT EXISTS idx_issue_labels_label_id
    ON middleman_issue_labels(label_id);

WITH parsed_issue_labels AS (
    SELECT
        i.id AS issue_id,
        i.repo_id,
        json_extract(label.value, '$.name') AS name,
        COALESCE(json_extract(label.value, '$.color'), '') AS color,
        COALESCE(i.updated_at, i.created_at, datetime('now')) AS updated_at
    FROM middleman_issues AS i
    JOIN json_each(
        CASE
            WHEN json_valid(i.labels_json) THEN i.labels_json
            ELSE '[]'
        END
    ) AS label
    WHERE TRIM(COALESCE(json_extract(label.value, '$.name'), '')) <> ''
)
INSERT OR IGNORE INTO middleman_labels (repo_id, platform_id, name, description, color, is_default, updated_at)
SELECT
    repo_id,
    NULL,
    name,
    '',
    MIN(color) AS color,
    0,
    MAX(updated_at) AS updated_at
FROM parsed_issue_labels
GROUP BY repo_id, name;

WITH parsed_issue_labels AS (
    SELECT
        i.id AS issue_id,
        i.repo_id,
        json_extract(label.value, '$.name') AS name
    FROM middleman_issues AS i
    JOIN json_each(
        CASE
            WHEN json_valid(i.labels_json) THEN i.labels_json
            ELSE '[]'
        END
    ) AS label
    WHERE TRIM(COALESCE(json_extract(label.value, '$.name'), '')) <> ''
)
INSERT OR IGNORE INTO middleman_issue_labels (issue_id, label_id)
SELECT
    pil.issue_id,
    l.id
FROM parsed_issue_labels AS pil
JOIN middleman_labels AS l
    ON l.repo_id = pil.repo_id
   AND l.name = pil.name;
