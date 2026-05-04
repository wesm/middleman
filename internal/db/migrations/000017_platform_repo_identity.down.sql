DROP INDEX IF EXISTS idx_repos_platform_repo_id;

DROP TRIGGER IF EXISTS middleman_repos_casefold_insert;
DROP TRIGGER IF EXISTS middleman_repos_casefold_update;

ALTER TABLE middleman_repos DROP COLUMN default_branch;
ALTER TABLE middleman_repos DROP COLUMN clone_url;
ALTER TABLE middleman_repos DROP COLUMN web_url;
ALTER TABLE middleman_repos DROP COLUMN repo_path_key;
ALTER TABLE middleman_repos DROP COLUMN name_key;
ALTER TABLE middleman_repos DROP COLUMN owner_key;
ALTER TABLE middleman_repos DROP COLUMN repo_path;
ALTER TABLE middleman_repos DROP COLUMN platform_repo_id;

CREATE TRIGGER middleman_repos_casefold_insert
BEFORE INSERT ON middleman_repos
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.owner <> lower(NEW.owner)
  OR NEW.name <> lower(NEW.name)
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be lowercase');
END;

CREATE TRIGGER middleman_repos_casefold_update
BEFORE UPDATE OF platform_host, owner, name ON middleman_repos
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.owner <> lower(NEW.owner)
  OR NEW.name <> lower(NEW.name)
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be lowercase');
END;
