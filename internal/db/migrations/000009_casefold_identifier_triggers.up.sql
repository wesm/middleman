CREATE TRIGGER IF NOT EXISTS middleman_repos_casefold_insert
BEFORE INSERT ON middleman_repos
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.owner <> lower(NEW.owner)
  OR NEW.name <> lower(NEW.name)
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be lowercase');
END;

CREATE TRIGGER IF NOT EXISTS middleman_repos_casefold_update
BEFORE UPDATE OF platform_host, owner, name ON middleman_repos
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.owner <> lower(NEW.owner)
  OR NEW.name <> lower(NEW.name)
BEGIN
    SELECT RAISE(ABORT, 'repo identifiers must be lowercase');
END;

CREATE TRIGGER IF NOT EXISTS middleman_workspaces_casefold_insert
BEFORE INSERT ON middleman_workspaces
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_owner <> lower(NEW.repo_owner)
  OR NEW.repo_name <> lower(NEW.repo_name)
BEGIN
    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
END;

CREATE TRIGGER IF NOT EXISTS middleman_workspaces_casefold_update
BEFORE UPDATE OF platform_host, repo_owner, repo_name ON middleman_workspaces
WHEN NEW.platform_host <> lower(NEW.platform_host)
  OR NEW.repo_owner <> lower(NEW.repo_owner)
  OR NEW.repo_name <> lower(NEW.repo_name)
BEGIN
    SELECT RAISE(ABORT, 'workspace repo identifiers must be lowercase');
END;
