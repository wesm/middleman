ALTER TABLE middleman_repos ADD COLUMN platform_repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN owner_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN name_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN repo_path_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN web_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN clone_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN default_branch TEXT NOT NULL DEFAULT '';

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
            THEN lower(trim(owner) || '/' || trim(name))
        ELSE trim(owner) || '/' || trim(name)
    END;

UPDATE middleman_repos
SET
    owner_key = lower(owner),
    name_key = lower(name),
    repo_path_key = lower(repo_path);

CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_platform_repo_id
    ON middleman_repos(platform, platform_host, platform_repo_id)
    WHERE platform_repo_id <> '';

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

UPDATE middleman_workspaces
SET
    platform_host = lower(trim(platform_host)),
    repo_owner_key = lower(trim(repo_owner)),
    repo_name_key = lower(trim(repo_name)),
    repo_path_key = lower(trim(repo_owner)) || '/' || lower(trim(repo_name));

UPDATE middleman_workspaces AS w
SET
    repo_owner_key = COALESCE((
        SELECT r.owner_key
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_owner_key),
    repo_name_key = COALESCE((
        SELECT r.name_key
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_name_key),
    repo_path_key = COALESCE((
        SELECT r.repo_path_key
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_path_key),
    repo_owner = COALESCE((
        SELECT r.owner
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_owner),
    repo_name = COALESCE((
        SELECT r.name
        FROM middleman_repos r
        WHERE r.platform_host = w.platform_host
          AND r.repo_path_key = w.repo_path_key
        ORDER BY CASE WHEN r.platform <> 'github' THEN 0 ELSE 1 END, r.id
        LIMIT 1
    ), repo_name);

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
