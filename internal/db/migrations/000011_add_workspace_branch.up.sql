ALTER TABLE middleman_workspaces
    ADD COLUMN workspace_branch TEXT NOT NULL DEFAULT '__middleman_unknown__';
