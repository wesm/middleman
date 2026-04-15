CREATE TEMP TABLE repo_casefold_duplicates AS
SELECT
    r.id AS duplicate_id,
    keep.keep_id
FROM middleman_repos AS r
JOIN (
    SELECT
        platform,
        lower(platform_host) AS platform_host,
        lower(owner) AS owner,
        lower(name) AS name,
        min(id) AS keep_id,
        count(*) AS repo_count
    FROM middleman_repos
    GROUP BY platform, lower(platform_host), lower(owner), lower(name)
    HAVING repo_count > 1
) AS keep
    ON keep.platform = r.platform
   AND keep.platform_host = lower(r.platform_host)
   AND keep.owner = lower(r.owner)
   AND keep.name = lower(r.name)
WHERE r.id <> keep.keep_id;

CREATE TEMP TABLE repo_casefold_targets AS
SELECT
    r.id AS repo_id,
    COALESCE(d.keep_id, r.id) AS keep_id
FROM middleman_repos AS r
LEFT JOIN repo_casefold_duplicates AS d
    ON d.duplicate_id = r.id;

CREATE TEMP TABLE label_casefold_duplicates AS
SELECT id AS duplicate_id, keep_id
FROM (
    SELECT
        l.id,
        first_value(l.id) OVER (
            PARTITION BY t.keep_id, l.name
            ORDER BY CASE WHEN l.repo_id = t.keep_id THEN 0 ELSE 1 END, l.id
        ) AS keep_id,
        row_number() OVER (
            PARTITION BY t.keep_id, l.name
            ORDER BY CASE WHEN l.repo_id = t.keep_id THEN 0 ELSE 1 END, l.id
        ) AS rn
    FROM middleman_labels AS l
    JOIN repo_casefold_targets AS t
        ON t.repo_id = l.repo_id
)
WHERE rn > 1;

INSERT OR IGNORE INTO middleman_issue_labels (issue_id, label_id)
SELECT il.issue_id, d.keep_id
FROM middleman_issue_labels AS il
JOIN label_casefold_duplicates AS d
    ON d.duplicate_id = il.label_id;

INSERT OR IGNORE INTO middleman_merge_request_labels (merge_request_id, label_id)
SELECT mrl.merge_request_id, d.keep_id
FROM middleman_merge_request_labels AS mrl
JOIN label_casefold_duplicates AS d
    ON d.duplicate_id = mrl.label_id;

DELETE FROM middleman_issue_labels
WHERE label_id IN (
    SELECT duplicate_id FROM label_casefold_duplicates
);

DELETE FROM middleman_merge_request_labels
WHERE label_id IN (
    SELECT duplicate_id FROM label_casefold_duplicates
);

DELETE FROM middleman_labels
WHERE id IN (
    SELECT duplicate_id FROM label_casefold_duplicates
);

CREATE TEMP TABLE issue_casefold_duplicates AS
SELECT id AS duplicate_id, keep_id
FROM (
    SELECT
        i.id,
        first_value(i.id) OVER (
            PARTITION BY t.keep_id, i.number
            ORDER BY CASE WHEN i.repo_id = t.keep_id THEN 0 ELSE 1 END, i.id
        ) AS keep_id,
        row_number() OVER (
            PARTITION BY t.keep_id, i.number
            ORDER BY CASE WHEN i.repo_id = t.keep_id THEN 0 ELSE 1 END, i.id
        ) AS rn
    FROM middleman_issues AS i
    JOIN repo_casefold_targets AS t
        ON t.repo_id = i.repo_id
)
WHERE rn > 1;

UPDATE middleman_issue_events
SET issue_id = (
    SELECT keep_id
    FROM issue_casefold_duplicates
    WHERE duplicate_id = middleman_issue_events.issue_id
)
WHERE issue_id IN (
    SELECT duplicate_id FROM issue_casefold_duplicates
);

INSERT OR IGNORE INTO middleman_issue_labels (
    issue_id, label_id
)
SELECT d.keep_id, il.label_id
FROM middleman_issue_labels AS il
JOIN issue_casefold_duplicates AS d
    ON d.duplicate_id = il.issue_id;

DELETE FROM middleman_issue_events
WHERE issue_id IN (
    SELECT duplicate_id FROM issue_casefold_duplicates
);

DELETE FROM middleman_issue_labels
WHERE issue_id IN (
    SELECT duplicate_id FROM issue_casefold_duplicates
);

DELETE FROM middleman_issues
WHERE id IN (
    SELECT duplicate_id FROM issue_casefold_duplicates
);

CREATE TEMP TABLE mr_casefold_duplicates AS
SELECT id AS duplicate_id, keep_id
FROM (
    SELECT
        mr.id,
        first_value(mr.id) OVER (
            PARTITION BY t.keep_id, mr.number
            ORDER BY CASE WHEN mr.repo_id = t.keep_id THEN 0 ELSE 1 END, mr.id
        ) AS keep_id,
        row_number() OVER (
            PARTITION BY t.keep_id, mr.number
            ORDER BY CASE WHEN mr.repo_id = t.keep_id THEN 0 ELSE 1 END, mr.id
        ) AS rn
    FROM middleman_merge_requests AS mr
    JOIN repo_casefold_targets AS t
        ON t.repo_id = mr.repo_id
)
WHERE rn > 1;

UPDATE middleman_mr_events
SET merge_request_id = (
    SELECT keep_id
    FROM mr_casefold_duplicates
    WHERE duplicate_id = middleman_mr_events.merge_request_id
)
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

CREATE TEMP TABLE mr_casefold_kanban_winners AS
SELECT keep_id, status, updated_at
FROM (
    SELECT
        candidates.keep_id,
        ks.status,
        ks.updated_at,
        row_number() OVER (
            PARTITION BY candidates.keep_id
            ORDER BY
                CASE WHEN ks.status <> 'new' THEN 0 ELSE 1 END,
                julianday(ks.updated_at) DESC,
                ks.updated_at DESC,
                ks.status ASC
        ) AS rn
    FROM (
        SELECT keep_id, keep_id AS merge_request_id
        FROM mr_casefold_duplicates
        UNION
        SELECT keep_id, duplicate_id AS merge_request_id
        FROM mr_casefold_duplicates
    ) AS candidates
    JOIN middleman_kanban_state AS ks
        ON ks.merge_request_id = candidates.merge_request_id
)
WHERE rn = 1;

UPDATE middleman_kanban_state
SET
    status = (
        SELECT status
        FROM mr_casefold_kanban_winners
        WHERE keep_id = middleman_kanban_state.merge_request_id
    ),
    updated_at = (
        SELECT updated_at
        FROM mr_casefold_kanban_winners
        WHERE keep_id = middleman_kanban_state.merge_request_id
    )
WHERE merge_request_id IN (
    SELECT keep_id FROM mr_casefold_kanban_winners
);

INSERT OR IGNORE INTO middleman_kanban_state (
    merge_request_id, status, updated_at
)
SELECT keep_id, status, updated_at
FROM mr_casefold_kanban_winners;

INSERT OR IGNORE INTO middleman_merge_request_labels (
    merge_request_id, label_id
)
SELECT d.keep_id, mrl.label_id
FROM middleman_merge_request_labels AS mrl
JOIN mr_casefold_duplicates AS d
    ON d.duplicate_id = mrl.merge_request_id;

UPDATE middleman_mr_worktree_links
SET merge_request_id = (
    SELECT keep_id
    FROM mr_casefold_duplicates
    WHERE duplicate_id = middleman_mr_worktree_links.merge_request_id
)
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
)
AND NOT EXISTS (
    SELECT 1
    FROM middleman_mr_worktree_links AS existing
    JOIN mr_casefold_duplicates AS d
        ON d.duplicate_id = middleman_mr_worktree_links.merge_request_id
    WHERE existing.merge_request_id = d.keep_id
      AND existing.worktree_key = middleman_mr_worktree_links.worktree_key
);

DELETE FROM middleman_mr_worktree_links
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

DELETE FROM middleman_stack_members
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
)
AND EXISTS (
    SELECT 1
    FROM middleman_stack_members AS existing
    JOIN mr_casefold_duplicates AS d
        ON d.duplicate_id = middleman_stack_members.merge_request_id
    WHERE existing.merge_request_id = d.keep_id
);

UPDATE middleman_stack_members
SET merge_request_id = (
    SELECT keep_id
    FROM mr_casefold_duplicates
    WHERE duplicate_id = middleman_stack_members.merge_request_id
)
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

DELETE FROM middleman_mr_events
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

DELETE FROM middleman_kanban_state
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

DELETE FROM middleman_merge_request_labels
WHERE merge_request_id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

DELETE FROM middleman_merge_requests
WHERE id IN (
    SELECT duplicate_id FROM mr_casefold_duplicates
);

DELETE FROM middleman_issues
WHERE id IN (
    SELECT id
    FROM (
        SELECT
            i.id,
            row_number() OVER (
                PARTITION BY t.keep_id, i.platform_id
                ORDER BY CASE WHEN i.repo_id = t.keep_id THEN 0 ELSE 1 END, i.id
            ) AS rn
        FROM middleman_issues AS i
        JOIN repo_casefold_targets AS t
            ON t.repo_id = i.repo_id
    )
    WHERE rn > 1
);

DELETE FROM middleman_merge_requests
WHERE id IN (
    SELECT id
    FROM (
        SELECT
            mr.id,
            row_number() OVER (
                PARTITION BY t.keep_id, mr.platform_id
                ORDER BY CASE WHEN mr.repo_id = t.keep_id THEN 0 ELSE 1 END, mr.id
            ) AS rn
        FROM middleman_merge_requests AS mr
        JOIN repo_casefold_targets AS t
            ON t.repo_id = mr.repo_id
    )
    WHERE rn > 1
);

CREATE TEMP TABLE label_platform_casefold_duplicates AS
SELECT id AS duplicate_id, keep_id
FROM (
    SELECT
        l.id,
        first_value(l.id) OVER (
            PARTITION BY t.keep_id, l.platform_id
            ORDER BY CASE WHEN l.repo_id = t.keep_id THEN 0 ELSE 1 END, l.id
        ) AS keep_id,
        row_number() OVER (
            PARTITION BY t.keep_id, l.platform_id
            ORDER BY CASE WHEN l.repo_id = t.keep_id THEN 0 ELSE 1 END, l.id
        ) AS rn
    FROM middleman_labels AS l
    JOIN repo_casefold_targets AS t
        ON t.repo_id = l.repo_id
    WHERE l.platform_id IS NOT NULL
)
WHERE rn > 1;

INSERT OR IGNORE INTO middleman_issue_labels (issue_id, label_id)
SELECT il.issue_id, d.keep_id
FROM middleman_issue_labels AS il
JOIN label_platform_casefold_duplicates AS d
    ON d.duplicate_id = il.label_id;

INSERT OR IGNORE INTO middleman_merge_request_labels (
    merge_request_id, label_id
)
SELECT mrl.merge_request_id, d.keep_id
FROM middleman_merge_request_labels AS mrl
JOIN label_platform_casefold_duplicates AS d
    ON d.duplicate_id = mrl.label_id;

DELETE FROM middleman_issue_labels
WHERE label_id IN (
    SELECT duplicate_id FROM label_platform_casefold_duplicates
);

DELETE FROM middleman_merge_request_labels
WHERE label_id IN (
    SELECT duplicate_id FROM label_platform_casefold_duplicates
);

DELETE FROM middleman_labels
WHERE id IN (
    SELECT duplicate_id FROM label_platform_casefold_duplicates
);

DELETE FROM middleman_starred_items
WHERE rowid IN (
    SELECT rowid
    FROM (
        SELECT
            si.rowid,
            row_number() OVER (
                PARTITION BY t.keep_id, si.item_type, si.number
                ORDER BY CASE WHEN si.repo_id = t.keep_id THEN 0 ELSE 1 END, si.rowid
            ) AS rn
        FROM middleman_starred_items AS si
        JOIN repo_casefold_targets AS t
            ON t.repo_id = si.repo_id
    )
    WHERE rn > 1
);

CREATE TEMP TABLE stack_casefold_duplicates AS
SELECT id AS duplicate_id, keep_id
FROM (
    SELECT
        s.id,
        first_value(s.id) OVER (
            PARTITION BY t.keep_id, s.base_number
            ORDER BY CASE WHEN s.repo_id = t.keep_id THEN 0 ELSE 1 END, s.id
        ) AS keep_id,
        row_number() OVER (
            PARTITION BY t.keep_id, s.base_number
            ORDER BY CASE WHEN s.repo_id = t.keep_id THEN 0 ELSE 1 END, s.id
        ) AS rn
    FROM middleman_stacks AS s
    JOIN repo_casefold_targets AS t
        ON t.repo_id = s.repo_id
)
WHERE rn > 1;

INSERT OR IGNORE INTO middleman_stack_members (
    stack_id, merge_request_id, position
)
SELECT d.keep_id, sm.merge_request_id, sm.position
FROM middleman_stack_members AS sm
JOIN stack_casefold_duplicates AS d
    ON d.duplicate_id = sm.stack_id;

DELETE FROM middleman_stack_members
WHERE stack_id IN (
    SELECT duplicate_id FROM stack_casefold_duplicates
);

DELETE FROM middleman_stacks
WHERE id IN (
    SELECT duplicate_id FROM stack_casefold_duplicates
);

UPDATE middleman_merge_requests
SET repo_id = (
    SELECT keep_id
    FROM repo_casefold_targets
    WHERE repo_id = middleman_merge_requests.repo_id
)
WHERE repo_id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

UPDATE middleman_issues
SET repo_id = (
    SELECT keep_id
    FROM repo_casefold_targets
    WHERE repo_id = middleman_issues.repo_id
)
WHERE repo_id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

UPDATE middleman_labels
SET repo_id = (
    SELECT keep_id
    FROM repo_casefold_targets
    WHERE repo_id = middleman_labels.repo_id
)
WHERE repo_id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

UPDATE middleman_starred_items
SET repo_id = (
    SELECT keep_id
    FROM repo_casefold_targets
    WHERE repo_id = middleman_starred_items.repo_id
)
WHERE repo_id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

UPDATE middleman_stacks
SET repo_id = (
    SELECT keep_id
    FROM repo_casefold_targets
    WHERE repo_id = middleman_stacks.repo_id
)
WHERE repo_id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

DELETE FROM middleman_workspaces
WHERE rowid NOT IN (
    SELECT min(rowid)
    FROM middleman_workspaces
    GROUP BY lower(platform_host), lower(repo_owner), lower(repo_name), mr_number
);

DELETE FROM middleman_repos
WHERE id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

UPDATE middleman_repos
SET
    platform_host = lower(platform_host),
    owner = lower(owner),
    name = lower(name);

UPDATE middleman_workspaces
SET
    platform_host = lower(platform_host),
    repo_owner = lower(repo_owner),
    repo_name = lower(repo_name);

DROP TABLE stack_casefold_duplicates;
DROP TABLE mr_casefold_duplicates;
DROP TABLE issue_casefold_duplicates;
DROP TABLE label_casefold_duplicates;
DROP TABLE repo_casefold_targets;
DROP TABLE repo_casefold_duplicates;
