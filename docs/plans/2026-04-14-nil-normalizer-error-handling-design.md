# Nil normalizer error handling design

**Problem**
- `internal/github/normalize.go` returns partially populated DB structs when GitHub payload pointer is nil.
- Callers can upsert zero-value rows if GitHub client returns `(nil, nil)`.
- Recent sync and API error-handling changes lack end-to-end coverage for missing clients and nil payloads.

**Goal**
- Treat nil GitHub PR/issue payloads as error conditions.
- Prevent silent bad-data writes.
- Add regression coverage for sync and HTTP mutation flows.

## Approach options

### Option 1: Keep normalizer signatures, return zero-value structs
- Minimal change.
- Bad because callers cannot distinguish nil payload from legitimate zero-value data.
- Reject.

### Option 2: Return `nil` from normalizers on nil input, require caller guards
- Small API change.
- Makes nil payload explicit at call sites.
- Fits existing pointer return type.
- Recommended.

### Option 3: Change normalizers to return `(*T, error)`
- Strongest contract.
- Larger churn across many callers for little extra value versus explicit nil checks.
- Not needed now.

## Chosen design
- `NormalizePR` returns `nil` when `ghPR == nil`.
- `NormalizeIssue` returns `nil` when `ghIssue == nil`.
- Every caller that uses normalized result before upsert/label replacement must guard against nil.
- Detail-fetch and mutation fallback paths return explicit errors on nil GitHub payloads.
- List/bulk/backfill paths treat nil payload as sync error for current operation, not silent continue.

## Impacted files
- `internal/github/normalize.go`
- `internal/github/sync.go`
- `internal/server/huma_routes.go`
- `internal/github/normalize_test.go`
- `internal/github/sync_test.go`
- `internal/server/api_test.go`

## Error-handling rules
- Sync detail fetches: return errors like `client returned nil pull request` / `client returned nil issue`.
- API mutation fallback re-fetches: if nil payload returned, do not update DB from fallback data; return gateway error.
- Missing client resolution in HTTP mutation paths remains explicit 404 and gets e2e coverage.

## Testing
- Unit tests for nil normalizer behavior.
- Sync tests using real SQLite helpers to prove nil payloads do not create corrupted rows.
- API tests through real HTTP server + SQLite to cover missing clients and nil payload fallback handling.
