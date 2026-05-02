-- name: UpsertRateLimit :exec
INSERT INTO middleman_rate_limits (
    platform_host, api_type, requests_hour, hour_start,
    rate_remaining, rate_limit, rate_reset_at, updated_at
)
VALUES (
    sqlc.arg(platform_host), sqlc.arg(api_type), sqlc.arg(requests_hour),
    sqlc.arg(hour_start), sqlc.arg(rate_remaining), sqlc.arg(rate_limit),
    sqlc.narg(rate_reset_at), datetime('now')
)
ON CONFLICT(platform_host, api_type) DO UPDATE SET
    requests_hour  = excluded.requests_hour,
    hour_start     = excluded.hour_start,
    rate_remaining = excluded.rate_remaining,
    rate_limit     = excluded.rate_limit,
    rate_reset_at  = excluded.rate_reset_at,
    updated_at     = datetime('now');

-- name: GetRateLimit :one
SELECT id, platform_host, api_type, requests_hour, hour_start,
       rate_remaining, rate_limit, rate_reset_at, updated_at
FROM middleman_rate_limits
WHERE platform_host = sqlc.arg(platform_host)
  AND api_type = sqlc.arg(api_type);
