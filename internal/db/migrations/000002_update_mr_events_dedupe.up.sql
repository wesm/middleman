CREATE TABLE middleman_mr_events_v2 (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
    platform_id      INTEGER,
    event_type       TEXT NOT NULL,
    author           TEXT NOT NULL DEFAULT '',
    summary          TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    metadata_json    TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    dedupe_key       TEXT NOT NULL,
    UNIQUE(merge_request_id, dedupe_key)
);

INSERT INTO middleman_mr_events_v2 (
    id,
    merge_request_id,
    platform_id,
    event_type,
    author,
    summary,
    body,
    metadata_json,
    created_at,
    dedupe_key
)
SELECT
    id,
    merge_request_id,
    platform_id,
    event_type,
    author,
    summary,
    body,
    metadata_json,
    created_at,
    dedupe_key
FROM middleman_mr_events;

DROP TABLE middleman_mr_events;
ALTER TABLE middleman_mr_events_v2 RENAME TO middleman_mr_events;
CREATE INDEX IF NOT EXISTS idx_mr_events_created
    ON middleman_mr_events(merge_request_id, created_at DESC);
