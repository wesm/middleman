ALTER TABLE middleman_repos ADD COLUMN platform_repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN owner_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN name_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN repo_path_key TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN web_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN clone_url TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_repos ADD COLUMN default_branch TEXT NOT NULL DEFAULT '';

UPDATE middleman_repos
SET repo_path = owner || '/' || name,
    owner_key = lower(owner),
    name_key = lower(name),
    repo_path_key = lower(owner) || '/' || lower(name)
WHERE repo_path = ''
  AND owner_key = ''
  AND name_key = ''
  AND repo_path_key = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_platform_repo_id
    ON middleman_repos(platform, platform_host, platform_repo_id)
    WHERE platform_repo_id <> '';

DROP TRIGGER IF EXISTS middleman_repos_casefold_insert;
DROP TRIGGER IF EXISTS middleman_repos_casefold_update;

CREATE TRIGGER middleman_repos_casefold_insert
BEFORE INSERT ON middleman_repos
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR (
      lower(NEW.platform) = 'github'
      AND (
          NEW.owner <> lower(NEW.owner)
          OR NEW.name <> lower(NEW.name)
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be lowercase');
END;

CREATE TRIGGER middleman_repos_casefold_update
BEFORE UPDATE OF platform_host, owner, name ON middleman_repos
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR (
      lower(NEW.platform) = 'github'
      AND (
          NEW.owner <> lower(NEW.owner)
          OR NEW.name <> lower(NEW.name)
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be lowercase');
END;
