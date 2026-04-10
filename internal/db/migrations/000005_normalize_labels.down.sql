DROP INDEX IF EXISTS idx_issue_labels_label_id;
DROP INDEX IF EXISTS idx_merge_request_labels_label_id;
DROP INDEX IF EXISTS idx_labels_repo_platform_id;
DROP INDEX IF EXISTS idx_labels_repo_name;

DROP TABLE IF EXISTS middleman_issue_labels;
DROP TABLE IF EXISTS middleman_merge_request_labels;
DROP TABLE IF EXISTS middleman_labels;
