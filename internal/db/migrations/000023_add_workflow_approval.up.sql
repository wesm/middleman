ALTER TABLE middleman_merge_requests
ADD COLUMN workflow_approval_checked_at DATETIME;
ALTER TABLE middleman_merge_requests
ADD COLUMN workflow_approval_head_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE middleman_merge_requests
ADD COLUMN workflow_approval_required INTEGER NOT NULL DEFAULT 0;
ALTER TABLE middleman_merge_requests
ADD COLUMN workflow_approval_count INTEGER NOT NULL DEFAULT 0;
