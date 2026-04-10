CREATE TABLE middleman_rate_limits_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    platform_host  TEXT NOT NULL,
    api_type       TEXT NOT NULL DEFAULT 'rest',
    requests_hour  INTEGER NOT NULL DEFAULT 0,
    hour_start     DATETIME NOT NULL,
    rate_remaining INTEGER NOT NULL DEFAULT -1,
    rate_limit     INTEGER NOT NULL DEFAULT -1,
    rate_reset_at  DATETIME,
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(platform_host, api_type)
);

INSERT INTO middleman_rate_limits_new
    (id, platform_host, api_type, requests_hour, hour_start,
     rate_remaining, rate_limit, rate_reset_at, updated_at)
SELECT id, platform_host, 'rest', requests_hour, hour_start,
       rate_remaining, rate_limit, rate_reset_at, updated_at
FROM middleman_rate_limits;

DROP TABLE middleman_rate_limits;

ALTER TABLE middleman_rate_limits_new RENAME TO middleman_rate_limits;
