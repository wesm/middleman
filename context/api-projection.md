# API Projection

PR and issue route handlers should keep data loading separate from API response
assembly. The tracked item projection module in
`internal/server/tracked_item_projection.go` owns the shared response shape for
PR and issue list, detail, sync, and create responses.

Keep these concerns in the projection module instead of repeating them in route
handlers:

- repository decoration (`platform_host`, `repo_owner`, `repo_name`)
- `detail_loaded` and `detail_fetched_at`
- UTC RFC3339 formatting for API timestamps, following ADR 0001
- non-nil empty event and worktree link slices
- workspace references for tracked item detail responses
- worktree link response mapping for PR list and detail responses

Handlers may still fetch rows, events, worktree links, workflow state, and
workspace records. Once those inputs are loaded, call a projection helper so
list/detail/sync/create responses stay uniform.
