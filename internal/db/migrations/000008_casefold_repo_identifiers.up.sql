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

DELETE FROM middleman_repos
WHERE id IN (
    SELECT duplicate_id FROM repo_casefold_duplicates
);

DROP TABLE repo_casefold_duplicates;

DELETE FROM middleman_workspaces
WHERE rowid NOT IN (
    SELECT min(rowid)
    FROM middleman_workspaces
    GROUP BY lower(platform_host), lower(repo_owner), lower(repo_name), mr_number
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
