ALTER TABLE middleman_workspaces
    RENAME COLUMN mr_number TO item_number;

ALTER TABLE middleman_workspaces
    RENAME COLUMN mr_head_ref TO git_head_ref;

ALTER TABLE middleman_workspaces
    ADD COLUMN item_type TEXT NOT NULL DEFAULT 'pull_request';
