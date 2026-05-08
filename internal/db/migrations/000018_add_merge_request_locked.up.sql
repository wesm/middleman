ALTER TABLE middleman_merge_requests
ADD COLUMN is_locked INTEGER NOT NULL DEFAULT 0;
