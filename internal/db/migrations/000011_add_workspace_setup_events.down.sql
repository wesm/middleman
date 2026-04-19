ALTER TABLE middleman_workspaces
    DROP COLUMN workspace_branch;

DROP INDEX IF EXISTS middleman_workspace_setup_events_workspace_id_idx;
DROP TABLE IF EXISTS middleman_workspace_setup_events;
