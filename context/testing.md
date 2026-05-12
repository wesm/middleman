# Testing

## Live GraphQL validation

GraphQL query shape changes must be validated against GitHub's live GraphQL API before they are merged. The local test suite includes a gated live test:

```sh
MIDDLEMAN_LIVE_GITHUB_TESTS=1 go test ./internal/github -run TestLiveGraphQLQueriesValidateAgainstGitHub -shuffle=on
```

The test uses `MIDDLEMAN_GITHUB_TOKEN` first, then `GITHUB_TOKEN`. It intentionally skips unless `MIDDLEMAN_LIVE_GITHUB_TESTS=1` is set because live validation consumes GitHub GraphQL rate limit and requires network access.

When changing structs, fields, aliases, fragments, pagination arguments, or nested selections used by `internal/github/graphql.go`, enable `MIDDLEMAN_LIVE_GITHUB_TESTS=1` and run the live validation test in addition to the normal Go tests.

CI runs the live GraphQL validation as a separate Go test step using the workflow `GITHUB_TOKEN` only in trusted contexts, such as pushes to `main`, manual `workflow_dispatch` runs, and same-repository pull requests. The general pull request Go test step does not receive a GitHub token.

## Provider work

When adding or changing a provider, pick tests at the boundary where users would
notice the regression:

- provider package tests for API normalization, pagination, auth/header shape,
  typed platform errors, and capability flags;
- config tests for provider defaults, host normalization, nested paths,
  duplicate detection, and token selection;
- DB/query tests for provider-aware identity and provider ID reconciliation;
- server e2e tests with real SQLite for route payloads, settings/import flows,
  and capability-gated actions;
- frontend store/component tests for provider refs and generated route helpers;
- optional live/container tests when fakes cannot validate provider API drift.

Regenerate OpenAPI and generated clients with `make api-generate` after Huma
route or API type changes.

## Race test runtime

Use `make race-times` and CI's race timing artifact before guessing at slow
packages. Keep `go test -race` fast by splitting large black-box flows into
separate packages and leaving only unexported-internal coverage in the source
package.

Current split targets:

- `internal/server/apitest`: generated-client HTTP API behavior;
- `internal/server/workspacetest`: workspace, runtime, terminal, and tmux HTTP
  flows;
- `internal/github/syncertest`: exported syncer contracts;
- `internal/db/projecttest`: DB behavior that can avoid core `internal/db`.

Use migrated SQLite template fixtures for non-migration DB tests:
`internal/testutil/dbtest.Open(t)` outside `internal/db`, and package-local
`openTestDB(t)` inside `internal/db`. Keep migration, legacy repair, dirty
migration, and schema-history tests behind `dbtest.OpenWithMigrationsAt(t, path)`
or package-local `openDBWithMigrations(t)`.

Do not use sleeps for synchronization. Prefer explicit events, callbacks,
readiness channels, or immediate-check polling with a bounded ticker.
`testing/synctest` is only for pure in-process goroutines and timers created
inside the bubble; do not use it with HTTP servers, WebSockets, tmux, PTYs, git,
shell commands, external-process filesystem polling, or nested `t.Run`,
`t.Parallel`, or `t.Deadline`.

## Related context

- [`context/provider-architecture.md`](./provider-architecture.md) documents the
  provider package split and checklist for adding providers.
- [`context/platform-sync-invariants.md`](./platform-sync-invariants.md)
  documents provider identity and capability rules for GitHub, GitLab, and
  future providers.
- [`context/github-sync-invariants.md`](./github-sync-invariants.md) documents
  timeline freshness, SHA-sensitive CI, and fallback rules that usually
  determine which tests belong on a GitHub-specific sync change.
