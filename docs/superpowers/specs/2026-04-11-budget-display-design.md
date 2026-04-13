# Budget Display Design

## Overview

Add a layered budget/rate-limit display to the middleman UI. A compact view lives in the existing status bar; clicking it opens a popover with per-host detail. Three budget types are displayed: GitHub REST API rate limits, GitHub GraphQL API rate limits, and middleman's own sync budget.

## Current State

The backend infrastructure largely exists:

- **`GET /api/v1/rate-limits`** endpoint returns per-host status including REST rate limits, sync budget, throttle state, and `known` flag
- **`RateTracker`** supports `apiType` (rest/graphql), stores both `remaining` and `limit`, hydrates from DB on startup
- **`SyncBudget`** tracks middleman's own hourly request spend per host with `Limit()`, `Spent()`, `Remaining()` methods
- **Migration 000005** restructured `middleman_rate_limits` with `api_type` column and `UNIQUE(platform_host, api_type)`
- **E2E tests** exist for the rate-limits endpoint (default state, request counting, budget spend)

What's **missing**:

- GraphQL rate limit data in the endpoint response. GraphQL `RateTracker` instances are already created in `main.go`/`middleman.go` and wired into `GraphQLFetcher` transports — they're actively tracking. But `Syncer.RateTrackers()` only returns the REST trackers, so the `getRateLimits` handler has no access to GraphQL rate data. The GraphQL trackers need to be exposed.
- Visual budget display (mini progress bars + popover). A text-based rate limit display already exists in `StatusBar.svelte` using `sync.getRateLimits()` — it shows worst-host text like `GitHub: 188/5000 global, 42/500 budget · resets 12m`. This text display will be replaced by the visual mini-bar + popover design. The sync store already polls `/api/v1/rate-limits` and exposes `getRateLimits()` — no new polling store needed.

## Data Model

### Budget Types

| Type | Source | Unit | Already Tracked? |
|------|--------|------|-----------------|
| REST | `RateTracker` (apiType "rest") via go-github `Rate` struct | requests | Yes |
| GraphQL | `RateTracker` (apiType "graphql") via `graphqlRateTransport` parsing `X-RateLimit-*` headers | points | Tracker active, not exposed to endpoint |
| Middleman | `SyncBudget` per host | requests | Yes |

### Existing API Response Shape

`GET /api/v1/rate-limits` currently returns:

```json
{
  "hosts": {
    "github.com": {
      "requests_hour": 188,
      "rate_remaining": 4812,
      "rate_limit": 5000,
      "rate_reset_at": "2026-04-11T15:00:00Z",
      "hour_start": "2026-04-11T14:00:00Z",
      "sync_throttle_factor": 1,
      "sync_paused": false,
      "reserve_buffer": 200,
      "known": true,
      "budget_limit": 500,
      "budget_spent": 42,
      "budget_remaining": 458
    }
  }
}
```

This covers REST rate limits and sync budget. GraphQL fields need to be added.

## Backend Changes

### GraphQL Rate Limit Extension

Add GraphQL rate limit fields to `rateLimitHostStatus`:

- `gql_remaining` (int): GraphQL points remaining (-1 if unknown)
- `gql_limit` (int): GraphQL point limit (-1 if unknown)
- `gql_reset_at` (string): GraphQL rate limit reset time (empty if unknown)
- `gql_known` (bool): whether GraphQL rate data has been observed

GraphQL `RateTracker` instances already exist and are actively recording data via `graphqlRateTransport`. They're created in `main.go` and passed to `NewGraphQLFetcher`, but stored only inside the `GraphQLFetcher` — not accessible via the syncer's public API. The work is exposing them to the `getRateLimits` handler (e.g., via a `GQLRateTrackers()` accessor on the syncer, or a `RateTracker()` accessor on `GraphQLFetcher`).

The endpoint already returns an entry for every configured host via `RateTrackers()`. If a host has no observed REST data, `known` will be false and `rate_remaining` will be -1. The same pattern applies to the new GraphQL fields.

## Frontend

### Compact View (Status Bar)

Add a clickable budget section to the right side of the existing `StatusBar.svelte`, between the existing content and the sync status.

Layout: `REST [===] GQL [===] 188 req/hr`

- Two mini progress bars (32px wide, 4px tall, rounded) for REST and GQL
- Bars show fill proportional to `remaining / limit`
- Labels (`REST`, `GQL`) inherit the bar's threshold color when in warning/red
- Middleman budget displayed as `spent/limit` or just spent count with `req/hr` unit in blue. When sync budget is disabled (`budget_limit == 0`) on all configured hosts, hide the middleman count entirely from the compact view. When some hosts have budget enabled and others don't, sum only the enabled hosts.
- When multiple hosts are configured, bars reflect the worst-case host (lowest `remaining / limit` ratio). Worst-case selection is per-bar-type: REST bar uses `known` to filter hosts, GQL bar uses `gql_known`. Only hosts with observed data for that specific budget type participate. If all hosts are unknown for a given budget type, show the unknown state (gray bar).
- Middleman count shows the sum across hosts. If all hosts are unknown (no data yet), show `--` instead of `0 req/hr` to match the unknown bar state. If some hosts are known and some unknown, sum only the known hosts.

### Color Thresholds

Based on `remaining / limit` percentage:

| State | Condition | Color |
|-------|-----------|-------|
| Healthy | >20% remaining | Green (`#4ade80`) |
| Warning | 5-20% remaining | Yellow (`#fbbf24`) |
| Critical | <5% remaining | Red (`#f87171`) |
| Middleman | Always | Blue (`#60a5fa`) |

Colors should use new CSS variables in `app.css` (`--budget-green`, `--budget-yellow`, `--budget-red`, `--budget-blue`). These are intentionally distinct from existing `--accent-*` variables — the budget palette uses lighter/more saturated Tailwind-style values suited to progress bar fills, while accent colors are tuned for text and icons.

### Expanded View (Popover)

Clicking the budget section in the status bar opens a popover anchored to the bottom-right of the budget area. The popover:

- Has a small "API Budget" header in muted uppercase
- Lists each host as a section, separated by a subtle border
- Each host section contains:
  - Host name with a colored health dot (color = worst of REST/GQL for that host)
  - REST progress bar with `remaining / limit requests` label
  - GraphQL progress bar with `remaining / limit points` label
  - Middleman budget as `spent / limit req/hr` (omit row for hosts where `budget_limit == 0`)
  - Per-type reset times as relative text (`resets in Xm`). REST and GQL may reset at different times; show each inline next to its bar. Middleman resets on the hour.
  - Throttle indicator when `sync_throttle_factor > 1` or `sync_paused`
- Dismisses on click-outside or Escape key
- Does not block interaction with the rest of the app (no modal overlay)

### Popover Positioning

- Anchored to bottom-right of the budget area in the status bar
- Opens upward (above the status bar)
- Max height constrained to avoid overflowing the viewport
- If content overflows, scroll within the popover

### Data Fetching

- Use existing `sync.getRateLimits()` from `packages/ui/src/stores/sync.svelte.ts` — it already polls `/api/v1/rate-limits` at 30s intervals and exposes reactive `RateLimitHostStatus` per host. No new polling store needed.

### Unknown State

When `known` is false (no data yet), the compact bar should render as an empty/gray bar with a `--` label instead of numbers. The popover should show "not yet observed" for that budget type.

## Interaction Details

### Status Bar Integration

The budget section sits between the left stats (`42 PRs · 12 issues · 8 repos`) and the right sync/version info. It is visually grouped as a single clickable area but does not have a visible border/background in its default state.

### Rate-Limited / Paused State

When `sync_paused` is true, the sync status text already shows rate-limited state. The budget bars reinforce this by showing the critical state in red. When `sync_throttle_factor > 1`, the popover should indicate throttling is active. No additional status bar treatment needed beyond color changes.

### Single Host

When only one host is configured (common case), the popover shows one section without a host header — just the budget rows directly.

## Changes Summary

### Backend (small)

- **Syncer/Fetcher**: Expose existing GraphQL `RateTracker` instances (currently private inside `GraphQLFetcher`)
- **Handler**: Add `gql_remaining`, `gql_limit`, `gql_reset_at`, `gql_known` fields to `rateLimitHostStatus` and populate from GraphQL tracker
- **API types**: Update `rateLimitHostStatus` struct
- **API client**: Regenerate via `make api-generate`

### Frontend (bulk of work)

- **Components**: New `BudgetBars.svelte` (compact mini-bars for status bar) and `BudgetPopover.svelte` (expanded per-host detail)
- **Integration**: Replace existing text-based `rateLimitText()` in `StatusBar.svelte` with `BudgetBars` component and wire popover toggle. Data comes from existing `sync.getRateLimits()`.
- **Styling**: Add budget CSS variables to `app.css`

## Testing

### E2E Tests (non-negotiable)

E2E tests must exercise the full stack (HTTP API with real SQLite) for:

- **GraphQL fields in rate-limits response**: Verify `gql_remaining`, `gql_limit`, `gql_reset_at`, `gql_known` fields appear and update correctly when a GraphQL rate tracker records data
- **Mixed known/unknown hosts**: Multiple configured hosts where some have observed rate data and others don't — endpoint returns entries for all
- **Budget spend tracking**: Verify `budget_spent`, `budget_remaining`, `budget_limit` update after sync budget spend (extends existing `TestAPIRateLimitsWithBudget`)

### Playwright E2E Tests

The project has two Playwright configs: mock-based (`frontend/playwright.config.ts` with `frontend/tests/e2e/`) and full-stack (`frontend/playwright-e2e.config.ts` with `frontend/tests/e2e-full/` and `cmd/e2e-server`). Budget display needs coverage in both:

**Mock-based e2e** (extend `frontend/tests/e2e/support/mockApi.ts` to serve `/api/v1/rate-limits`):

- Status bar renders budget bars with mocked rate-limit data (known hosts show colored bars, budget text)
- Status bar shows unknown state (`--`, gray bars) when all hosts have `known: false`
- Clicking budget area opens popover with per-host detail
- Popover dismisses on click-outside and Escape
- Mixed known/unknown hosts show worst-case from known hosts only
- Budget row hidden when all hosts have `budget_limit == 0`

**Full-stack e2e** (real Go server with seeded SQLite — the e2e server does not run sync, so test against seeded/injected rate-limit data):

- Budget bars display rate-limit data from seeded DB
- Popover shows correct per-host breakdown with seeded data

### Frontend Unit Tests (vitest)

- Worst-case host selection logic across multiple known hosts
- Color threshold computation (green/yellow/red boundaries)
- Middleman count aggregation with mixed enabled/disabled hosts
