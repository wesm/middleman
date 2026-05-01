# Sync Engine

The sync engine runs broad repository indexing first, then spends the per-host
detail budget on open items that need deeper refreshes.

## Detail Refresh Planner

`internal/github/detail_refresh_planner.go` owns the detail-drain planning
phase. It accepts typed snapshots of tracked repositories and watched merge
requests, applies the default GitHub host, filters stale database rows from
repos that are no longer tracked, and returns `QueueItem` values for the shared
priority queue.

This keeps `Syncer` orchestration focused on lifecycle, locking, budget
admission, and fetch execution. It also keeps host/key/defaulting rules in one
place so planner tests can exercise queue input construction without building a
GitHub `Client` mock or running the full sync loop.
