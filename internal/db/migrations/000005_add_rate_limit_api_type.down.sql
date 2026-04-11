CREATE TABLE middleman_rate_limits_old (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    platform_host  TEXT NOT NULL UNIQUE,
    requests_hour  INTEGER NOT NULL DEFAULT 0,
    hour_start     DATETIME NOT NULL,
    rate_remaining INTEGER NOT NULL DEFAULT -1,
    rate_limit     INTEGER NOT NULL DEFAULT -1,
    rate_reset_at  DATETIME,
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO middleman_rate_limits_old
    (id, platform_host, requests_hour, hour_start,
     rate_remaining, rate_limit, rate_reset_at, updated_at)
SELECT id, platform_host, requests_hour, hour_start,
       rate_remaining, rate_limit, rate_reset_at, updated_at
FROM middleman_rate_limits
WHERE api_type = 'rest';

DROP TABLE middleman_rate_limits;

ALTER TABLE middleman_rate_limits_old RENAME TO middleman_rate_limits;
