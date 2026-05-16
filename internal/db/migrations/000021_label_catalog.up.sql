ALTER TABLE middleman_labels ADD COLUMN catalog_present INTEGER NOT NULL DEFAULT 0;
ALTER TABLE middleman_labels ADD COLUMN catalog_seen_at DATETIME;

ALTER TABLE middleman_repos ADD COLUMN label_catalog_synced_at DATETIME;
ALTER TABLE middleman_repos ADD COLUMN label_catalog_checked_at DATETIME;
ALTER TABLE middleman_repos ADD COLUMN label_catalog_sync_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_labels_repo_catalog_name
    ON middleman_labels(repo_id, catalog_present, name);
