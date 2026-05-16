DROP INDEX IF EXISTS idx_labels_repo_catalog_name;
ALTER TABLE middleman_repos DROP COLUMN label_catalog_sync_error;
ALTER TABLE middleman_repos DROP COLUMN label_catalog_checked_at;
ALTER TABLE middleman_repos DROP COLUMN label_catalog_synced_at;
ALTER TABLE middleman_labels DROP COLUMN catalog_seen_at;
ALTER TABLE middleman_labels DROP COLUMN catalog_present;
