# UTC Datetime Policy Design

## Summary

Middleman should treat datetime handling as a strict cross-layer invariant:

- Store datetimes in UTC.
- Serialize API datetimes in UTC RFC3339.
- Convert to local timezone only in the Svelte UI presentation layer.

This change formalizes behavior that is already mostly present in the codebase, closes gaps where the rule is only implicit, and adds enforcement and tests without requiring a risky migration of existing rows.

## Problem

The repository currently has an implicit convention rather than a documented rule.

- Go code frequently normalizes timestamps with `.UTC()`.
- API handlers often emit RFC3339 strings.
- The frontend parses API timestamps with `new Date(...)` and renders relative or locale-formatted output.
- Some database paths still tolerate multiple timestamp layouts because SQLite and the current driver can surface `DATETIME` values in different serialized forms.

That leaves three problems:

1. Contributors do not have one canonical policy to follow.
2. Storage and API behavior are not enforced uniformly.
3. Future changes could accidentally move timezone logic into the wrong layer.

## Decision

Adopt a single datetime policy for all application-owned timestamps.

### Storage

- All application-owned timestamps persisted by middleman are UTC.
- New writes should use RFC3339 UTC with a trailing `Z` where the application controls serialization.
- Existing tolerant read paths may remain in place for backward compatibility, but they must normalize parsed values to UTC immediately.

### API

- All datetimes returned by the HTTP API are UTC RFC3339 strings.
- API handlers should not emit local offsets, timezone-dependent formatting, or ad hoc layouts.

### Frontend

- API timestamps are treated as canonical UTC instants.
- Conversion to local timezone is allowed only when rendering for the user in Svelte components or UI utilities.
- Filtering, ordering, and comparisons should operate on those canonical instants rather than on locale-formatted strings.

## Scope

This policy applies to:

- SQLite persistence and SQL defaults owned by middleman
- Go model normalization and query parsing
- HTTP API request and response timestamp fields
- OpenAPI-generated frontend types and frontend consumers of those fields
- Contributor guidance in `CLAUDE.md`

This policy does not require:

- rewriting historical rows purely to change their textual representation
- changing end-user display from local/relative time back to UTC text in the UI
- changing third-party payloads before they are first normalized into middleman-owned types

## Recommended Approach

Use strict edge normalization with backward-compatible reads.

### Why this approach

- It gives the project a clear invariant at every boundary that matters.
- It avoids a migration whose only purpose would be rewriting old UTC values into a new textual shape.
- It lets us fix contributor behavior, API behavior, and new writes immediately while keeping existing databases readable.

### Rejected alternatives

#### Full storage migration to one exact on-disk format

This is stronger on paper but provides little user value relative to the migration risk and implementation churn.

#### API-only rule

This leaves storage underspecified and does not prevent timezone bugs from entering through database write paths.

## Design

## 1. Canonical invariants

The implementation should enforce these invariants:

1. A persisted middleman timestamp represents a UTC instant.
2. A timestamp crossing the HTTP API boundary is serialized as UTC RFC3339.
3. Local timezone conversion is presentation-only.
4. Parsing code may accept multiple legacy layouts, but all resulting `time.Time` values are normalized to UTC before further use.

## 2. Storage rules

Database writes should stop relying on ambiguous implicit serialization.

- Where Go writes timestamps explicitly, normalize with `.UTC()` before persistence.
- Where SQL currently generates timestamps, prefer an explicit RFC3339 UTC representation rather than mixed implicit layouts.
- If a timestamp is stored as text by application code, use RFC3339 UTC.
- If a `DATETIME` column remains in place for compatibility, treat the column contract as "UTC instant", not "database-local wall clock time".

Existing parsing helpers should stay tolerant enough to read current databases and driver-produced formats, but that tolerance is a compatibility shim, not the canonical policy.

## 3. API rules

Server responses should have one policy for every timestamp field:

- emit UTC
- emit RFC3339
- avoid custom one-off layouts when `time.RFC3339` is sufficient

Any handler currently returning a UTC-looking string via a hard-coded format should be reviewed and standardized onto the same explicit RFC3339 rule used elsewhere.

Request parsing should also expect RFC3339 when accepting timestamps from clients.

## 4. Frontend rules

The UI should continue to parse API timestamps as absolute instants.

- `new Date(apiValue)` is acceptable when `apiValue` is an RFC3339 UTC string.
- Relative time and locale rendering are presentation concerns.
- Stores and components should not reinterpret timestamps as if they were already local wall-clock values.
- Shared UI helpers are preferred over duplicating timestamp formatting logic across components.

## 5. Documentation updates

Three documentation layers should exist after this change:

1. This design spec in `docs/superpowers/specs/...`
2. An ADR that records the architectural decision and rationale
3. A short contributor-facing rule in `CLAUDE.md`

The ADR should be the long-lived reference for why the policy exists. `CLAUDE.md` should be the short operational reminder contributors see before editing code.

## 6. Testing

Add or update tests for the policy at the boundaries where it matters.

- Database tests: parsed timestamps normalize to UTC even when legacy layouts are accepted.
- API tests: timestamp fields are returned as UTC RFC3339.
- Frontend tests: UI consumers accept UTC API values and localize only at render time.

The goal is not to snapshot every timestamp field in the system. The goal is to verify the policy once per important boundary and catch regressions early.

## Implementation Outline

Implementation should proceed in this order:

1. Add the ADR and `CLAUDE.md` guidance.
2. Identify all application-owned timestamp write paths in Go and SQL.
3. Normalize those writes to explicit UTC behavior.
4. Standardize API serialization on UTC RFC3339.
5. Keep tolerant DB reads, but document them as backward-compatibility only.
6. Add database, API, and frontend tests for the invariant.

## Risks And Mitigations

### Risk: legacy databases contain mixed layouts

Mitigation: keep tolerant parsing for reads and normalize immediately to UTC in memory.

### Risk: contributors add new local-time logic in the backend

Mitigation: document the rule in `CLAUDE.md`, codify it in the ADR, and add tests at persistence and API boundaries.

### Risk: frontend formatting remains duplicated

Mitigation: keep the architecture rule clear that duplication is acceptable only in the presentation layer; follow-up cleanup can centralize helpers without changing the policy.

## Deliverables

- `docs/superpowers/specs/2026-04-11-utc-datetime-policy-design.md`
- `docs/adr/0001-utc-datetime-policy.md`
- a short datetime policy rule in `CLAUDE.md`
- follow-up implementation and tests that enforce the policy

## Success Criteria

This work is successful when:

- the repository has a documented UTC datetime policy
- new storage and API code has one unambiguous rule to follow
- the frontend is explicitly the only layer allowed to localize timestamps for display
- tests verify the invariant at DB and API boundaries
