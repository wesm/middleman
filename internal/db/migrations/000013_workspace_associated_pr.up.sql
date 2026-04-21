ALTER TABLE middleman_workspaces
    ADD COLUMN associated_pr_number INTEGER;

UPDATE middleman_workspaces
SET associated_pr_number = item_number
WHERE item_type = 'pull_request';
