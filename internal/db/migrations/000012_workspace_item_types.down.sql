ALTER TABLE middleman_workspaces
    DROP COLUMN item_type;

ALTER TABLE middleman_workspaces
    RENAME COLUMN git_head_ref TO mr_head_ref;

ALTER TABLE middleman_workspaces
    RENAME COLUMN item_number TO mr_number;
