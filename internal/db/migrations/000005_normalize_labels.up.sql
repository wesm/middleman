CREATE TABLE IF NOT EXISTS middleman_labels (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE,
    platform_id INTEGER,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '',
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
        COALESCE(json_extract(label.value, '$.color'), '') AS color
    FROM middleman_issues AS i
    JOIN json_each(
        CASE
            WHEN json_valid(i.labels_json) THEN i.labels_json
            ELSE '[]'
        END
    ) AS label
    WHERE TRIM(COALESCE(json_extract(label.value, '$.name'), '')) <> ''
)
INSERT OR IGNORE INTO middleman_labels (repo_id, name, color)
SELECT
    repo_id,
    name,
    MIN(color) AS color
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
