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
            THEN lower(trim(CASE WHEN repo_path = '' THEN owner || '/' || name ELSE repo_path END))
        ELSE trim(CASE WHEN repo_path = '' THEN owner || '/' || name ELSE repo_path END)
    END;

UPDATE middleman_repos
SET
    owner_key = lower(owner),
    name_key = lower(name),
    repo_path_key = lower(repo_path);

CREATE TEMP TABLE middleman_repo_canonical_merge AS
WITH ranked_repos AS (
    SELECT
        id,
        MIN(id) OVER (
            PARTITION BY platform, platform_host, repo_path_key
        ) AS target_id
    FROM middleman_repos
    WHERE repo_path_key <> ''
)
SELECT id AS source_id, target_id
FROM ranked_repos
WHERE id <> target_id;

UPDATE middleman_repos
SET
    platform_repo_id = CASE
        WHEN platform_repo_id = ''
        THEN COALESCE((
            SELECT source.platform_repo_id
            FROM middleman_repos AS source
            JOIN middleman_repo_canonical_merge AS merge_map
                ON merge_map.source_id = source.id
            WHERE merge_map.target_id = middleman_repos.id
              AND source.platform_repo_id <> ''
            ORDER BY source.id
            LIMIT 1
        ), platform_repo_id)
        ELSE platform_repo_id
    END,
    web_url = CASE
        WHEN web_url = ''
        THEN COALESCE((
            SELECT source.web_url
            FROM middleman_repos AS source
            JOIN middleman_repo_canonical_merge AS merge_map
                ON merge_map.source_id = source.id
            WHERE merge_map.target_id = middleman_repos.id
              AND source.web_url <> ''
            ORDER BY source.id
            LIMIT 1
        ), web_url)
        ELSE web_url
    END,
    clone_url = CASE
        WHEN clone_url = ''
        THEN COALESCE((
            SELECT source.clone_url
            FROM middleman_repos AS source
            JOIN middleman_repo_canonical_merge AS merge_map
                ON merge_map.source_id = source.id
            WHERE merge_map.target_id = middleman_repos.id
              AND source.clone_url <> ''
            ORDER BY source.id
            LIMIT 1
        ), clone_url)
        ELSE clone_url
    END,
    default_branch = CASE
        WHEN default_branch = ''
        THEN COALESCE((
            SELECT source.default_branch
            FROM middleman_repos AS source
            JOIN middleman_repo_canonical_merge AS merge_map
                ON merge_map.source_id = source.id
            WHERE merge_map.target_id = middleman_repos.id
              AND source.default_branch <> ''
            ORDER BY source.id
            LIMIT 1
        ), default_branch)
        ELSE default_branch
    END
WHERE id IN (
    SELECT DISTINCT target_id
    FROM middleman_repo_canonical_merge
);

UPDATE middleman_merge_requests
SET repo_id = (
    SELECT target_id
    FROM middleman_repo_canonical_merge
    WHERE source_id = middleman_merge_requests.repo_id
)
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge)
  AND NOT EXISTS (
      SELECT 1
      FROM middleman_merge_requests AS target
      WHERE target.repo_id = (
          SELECT target_id
          FROM middleman_repo_canonical_merge
          WHERE source_id = middleman_merge_requests.repo_id
      )
        AND (
            target.number = middleman_merge_requests.number
            OR target.platform_id = middleman_merge_requests.platform_id
        )
  );

DELETE FROM middleman_merge_requests
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge);

UPDATE middleman_issues
SET repo_id = (
    SELECT target_id
    FROM middleman_repo_canonical_merge
    WHERE source_id = middleman_issues.repo_id
)
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge)
  AND NOT EXISTS (
      SELECT 1
      FROM middleman_issues AS target
      WHERE target.repo_id = (
          SELECT target_id
          FROM middleman_repo_canonical_merge
          WHERE source_id = middleman_issues.repo_id
      )
        AND (
            target.number = middleman_issues.number
            OR target.platform_id = middleman_issues.platform_id
        )
  );

DELETE FROM middleman_issues
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge);

UPDATE middleman_labels
SET repo_id = (
    SELECT target_id
    FROM middleman_repo_canonical_merge
    WHERE source_id = middleman_labels.repo_id
)
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge)
  AND NOT EXISTS (
      SELECT 1
      FROM middleman_labels AS target
      WHERE target.repo_id = (
          SELECT target_id
          FROM middleman_repo_canonical_merge
          WHERE source_id = middleman_labels.repo_id
      )
        AND (
            target.name = middleman_labels.name
            OR (
                target.platform_id IS NOT NULL
                AND middleman_labels.platform_id IS NOT NULL
                AND target.platform_id = middleman_labels.platform_id
            )
        )
  );

WITH source_label_targets AS (
    SELECT
        source.id AS source_label_id,
        target.id AS target_label_id,
        ROW_NUMBER() OVER (
            PARTITION BY source.id
            ORDER BY
                CASE
                    WHEN target.platform_id IS NOT NULL
                         AND source.platform_id IS NOT NULL
                         AND target.platform_id = source.platform_id
                    THEN 0
                    ELSE 1
                END,
                target.id
        ) AS target_rank
    FROM middleman_labels AS source
    JOIN middleman_repo_canonical_merge AS merge_map
        ON merge_map.source_id = source.repo_id
    JOIN middleman_labels AS target
        ON target.repo_id = merge_map.target_id
       AND (
           target.name = source.name
           OR (
               target.platform_id IS NOT NULL
               AND source.platform_id IS NOT NULL
               AND target.platform_id = source.platform_id
           )
       )
)
INSERT INTO middleman_issue_labels (issue_id, label_id)
SELECT il.issue_id, slt.target_label_id
FROM middleman_issue_labels AS il
JOIN source_label_targets AS slt
    ON slt.source_label_id = il.label_id
   AND slt.target_rank = 1
ON CONFLICT(issue_id, label_id) DO NOTHING;

WITH source_label_targets AS (
    SELECT
        source.id AS source_label_id,
        target.id AS target_label_id,
        ROW_NUMBER() OVER (
            PARTITION BY source.id
            ORDER BY
                CASE
                    WHEN target.platform_id IS NOT NULL
                         AND source.platform_id IS NOT NULL
                         AND target.platform_id = source.platform_id
                    THEN 0
                    ELSE 1
                END,
                target.id
        ) AS target_rank
    FROM middleman_labels AS source
    JOIN middleman_repo_canonical_merge AS merge_map
        ON merge_map.source_id = source.repo_id
    JOIN middleman_labels AS target
        ON target.repo_id = merge_map.target_id
       AND (
           target.name = source.name
           OR (
               target.platform_id IS NOT NULL
               AND source.platform_id IS NOT NULL
               AND target.platform_id = source.platform_id
           )
       )
)
INSERT INTO middleman_merge_request_labels (merge_request_id, label_id)
SELECT mrl.merge_request_id, slt.target_label_id
FROM middleman_merge_request_labels AS mrl
JOIN source_label_targets AS slt
    ON slt.source_label_id = mrl.label_id
   AND slt.target_rank = 1
ON CONFLICT(merge_request_id, label_id) DO NOTHING;

DELETE FROM middleman_labels
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge);

INSERT OR IGNORE INTO middleman_starred_items (
    item_type, repo_id, number, starred_at
)
SELECT item_type, merge_map.target_id, number, starred_at
FROM middleman_starred_items AS starred
JOIN middleman_repo_canonical_merge AS merge_map
    ON merge_map.source_id = starred.repo_id;

DELETE FROM middleman_starred_items
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge);

UPDATE middleman_stacks
SET repo_id = (
    SELECT target_id
    FROM middleman_repo_canonical_merge
    WHERE source_id = middleman_stacks.repo_id
)
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge)
  AND NOT EXISTS (
      SELECT 1
      FROM middleman_stacks AS target
      WHERE target.repo_id = (
          SELECT target_id
          FROM middleman_repo_canonical_merge
          WHERE source_id = middleman_stacks.repo_id
      )
        AND target.base_number = middleman_stacks.base_number
  );

DELETE FROM middleman_stacks
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge);

UPDATE middleman_repo_overviews
SET repo_id = (
    SELECT target_id
    FROM middleman_repo_canonical_merge
    WHERE source_id = middleman_repo_overviews.repo_id
)
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge)
  AND NOT EXISTS (
      SELECT 1
      FROM middleman_repo_overviews AS target
      WHERE target.repo_id = (
          SELECT target_id
          FROM middleman_repo_canonical_merge
          WHERE source_id = middleman_repo_overviews.repo_id
      )
  );

DELETE FROM middleman_repo_overviews
WHERE repo_id IN (SELECT source_id FROM middleman_repo_canonical_merge);

DELETE FROM middleman_repos
WHERE id IN (SELECT source_id FROM middleman_repo_canonical_merge);

DROP TABLE middleman_repo_canonical_merge;

CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_provider_path_key
    ON middleman_repos(platform, platform_host, repo_path_key)
    WHERE repo_path_key <> '';

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

ALTER TABLE middleman_workspaces
    ADD COLUMN repo_owner_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_workspaces
    ADD COLUMN repo_name_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_workspaces
    ADD COLUMN repo_path_key TEXT NOT NULL DEFAULT '';

DROP TRIGGER IF EXISTS middleman_workspaces_casefold_insert;
DROP TRIGGER IF EXISTS middleman_workspaces_casefold_update;

CREATE TEMP TABLE middleman_workspace_repo_match AS
WITH ranked_matches AS (
    SELECT
        w.id AS workspace_id,
        r.owner,
        r.name,
        r.owner_key,
        r.name_key,
        r.repo_path_key,
        ROW_NUMBER() OVER (
            PARTITION BY w.id
            ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        ) AS match_rank
    FROM middleman_workspaces AS w
    JOIN middleman_repos AS r
        ON r.platform_host = lower(trim(w.platform_host))
       AND r.repo_path_key = lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
)
SELECT
    workspace_id,
    owner,
    name,
    owner_key,
    name_key,
    repo_path_key
FROM ranked_matches
WHERE match_rank = 1;

CREATE TEMP TABLE middleman_workspace_canonical_values AS
SELECT
    w.id,
    lower(trim(w.platform_host)) AS platform_host,
    COALESCE(m.owner, lower(trim(w.repo_owner))) AS repo_owner,
    COALESCE(m.name, lower(trim(w.repo_name))) AS repo_name,
    COALESCE(m.owner_key, lower(trim(w.repo_owner))) AS repo_owner_key,
    COALESCE(m.name_key, lower(trim(w.repo_name))) AS repo_name_key,
    COALESCE(
        m.repo_path_key,
        lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
    ) AS repo_path_key,
    w.item_type,
    w.item_number,
    w.created_at,
    CASE
        WHEN trim(w.repo_owner) = COALESCE(m.owner, lower(trim(w.repo_owner)))
         AND trim(w.repo_name) = COALESCE(m.name, lower(trim(w.repo_name)))
        THEN 0
        ELSE 1
    END AS display_rank
FROM middleman_workspaces AS w
LEFT JOIN middleman_workspace_repo_match AS m
    ON m.workspace_id = w.id;

CREATE TEMP TABLE middleman_workspace_canonical_merge AS
WITH ranked_workspaces AS (
    SELECT
        id,
        FIRST_VALUE(id) OVER (
            PARTITION BY platform_host, repo_path_key, item_type, item_number
            ORDER BY display_rank, created_at, id
        ) AS target_id
    FROM middleman_workspace_canonical_values
)
SELECT id AS source_id, target_id
FROM ranked_workspaces
WHERE id <> target_id;

UPDATE middleman_workspace_setup_events
SET workspace_id = (
    SELECT target_id
    FROM middleman_workspace_canonical_merge
    WHERE source_id = middleman_workspace_setup_events.workspace_id
)
WHERE workspace_id IN (
    SELECT source_id
    FROM middleman_workspace_canonical_merge
);

CREATE TEMP TABLE middleman_workspace_tmux_sessions_backup AS
SELECT
    COALESCE((
        SELECT target_id
        FROM middleman_workspace_canonical_merge
        WHERE source_id = sessions.workspace_id
    ), sessions.workspace_id) AS workspace_id,
    sessions.session_name,
    sessions.target_key,
    sessions.created_at
FROM middleman_workspace_tmux_sessions AS sessions;

DROP INDEX IF EXISTS middleman_workspace_tmux_sessions_workspace_id_idx;
DROP TABLE middleman_workspace_tmux_sessions;

DELETE FROM middleman_workspaces
WHERE id IN (
    SELECT source_id
    FROM middleman_workspace_canonical_merge
);

CREATE TABLE middleman_workspace_tmux_sessions (
    workspace_id TEXT NOT NULL REFERENCES middleman_workspaces(id) ON DELETE CASCADE,
    session_name TEXT NOT NULL,
    target_key   TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_id, session_name),
    UNIQUE (session_name)
);

CREATE INDEX middleman_workspace_tmux_sessions_workspace_id_idx
    ON middleman_workspace_tmux_sessions(workspace_id);

INSERT OR IGNORE INTO middleman_workspace_tmux_sessions (
    workspace_id, session_name, target_key, created_at
)
SELECT workspace_id, session_name, target_key, created_at
FROM middleman_workspace_tmux_sessions_backup;

DROP TABLE middleman_workspace_tmux_sessions_backup;
DROP TABLE middleman_workspace_canonical_merge;
DROP TABLE middleman_workspace_canonical_values;
DROP TABLE middleman_workspace_repo_match;

UPDATE middleman_workspaces AS w
SET
    platform_host = lower(trim(platform_host)),
    repo_owner_key = COALESCE((
        SELECT r.owner_key
        FROM middleman_repos r
        WHERE r.platform_host = lower(trim(w.platform_host))
          AND r.repo_path_key = lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), lower(trim(repo_owner))),
    repo_name_key = COALESCE((
        SELECT r.name_key
        FROM middleman_repos r
        WHERE r.platform_host = lower(trim(w.platform_host))
          AND r.repo_path_key = lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), lower(trim(repo_name))),
    repo_path_key = COALESCE((
        SELECT r.repo_path_key
        FROM middleman_repos r
        WHERE r.platform_host = lower(trim(w.platform_host))
          AND r.repo_path_key = lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), lower(trim(repo_owner)) || '/' || lower(trim(repo_name))),
    repo_owner = COALESCE((
        SELECT r.owner
        FROM middleman_repos r
        WHERE r.platform_host = lower(trim(w.platform_host))
          AND r.repo_path_key = lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), lower(trim(repo_owner))),
    repo_name = COALESCE((
        SELECT r.name
        FROM middleman_repos r
        WHERE r.platform_host = lower(trim(w.platform_host))
          AND r.repo_path_key = lower(trim(w.repo_owner)) || '/' || lower(trim(w.repo_name))
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), lower(trim(repo_name)));

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_provider_item_key
    ON middleman_workspaces(platform_host, repo_path_key, item_type, item_number);

CREATE TRIGGER middleman_workspaces_casefold_insert
BEFORE INSERT ON middleman_workspaces
WHEN NEW.platform_host <> lower(NEW.platform_host)
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
          WHERE r.platform_host = NEW.platform_host
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
BEFORE UPDATE OF platform_host, repo_owner, repo_name, repo_owner_key, repo_name_key, repo_path_key ON middleman_workspaces
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_path_key = ''
  OR NEW.repo_owner_key <> lower(NEW.repo_owner_key)
  OR NEW.repo_name_key <> lower(NEW.repo_name_key)
  OR NEW.repo_path_key <> lower(NEW.repo_path_key)
  OR NEW.repo_path_key <> NEW.repo_owner_key || '/' || NEW.repo_name_key
  OR (
      NOT EXISTS (
          SELECT 1
          FROM middleman_repos r
          WHERE r.platform_host = NEW.platform_host
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
