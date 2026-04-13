# Budget Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the text-based rate limit display in the status bar with visual mini progress bars and a click-to-expand popover showing per-host budget detail.

**Architecture:** Backend exposes existing GraphQL rate trackers to the rate-limits endpoint (4 new JSON fields). Frontend replaces `rateLimitText()` in StatusBar with two new Svelte components (`BudgetBars` for compact bars, `BudgetPopover` for detail), consuming data from the existing `sync.getRateLimits()` store.

**Tech Stack:** Go (huma, go-github), Svelte 5 (runes), Playwright, vitest, @testing-library/svelte

**Spec:** `docs/superpowers/specs/2026-04-11-budget-display-design.md`

---

## File Map

### Backend (modify)

| File | Change |
|------|--------|
| `internal/github/graphql.go` | Add `RateTracker()` accessor to `GraphQLFetcher` |
| `internal/github/sync.go` | Add `GQLRateTrackers()` method to `Syncer` |
| `internal/server/api_types.go` | Add GQL fields to `rateLimitHostStatus` |
| `internal/server/huma_routes.go` | Populate GQL fields in `getRateLimits` handler |

### Backend (test)

| File | Change |
|------|--------|
| `internal/github/graphql_test.go` | Test `RateTracker()` accessor |
| `internal/server/api_test.go` | E2E test for GQL fields in rate-limits response |

### Frontend (create)

| File | Purpose |
|------|---------|
| `frontend/src/lib/components/layout/BudgetBars.svelte` | Compact mini-bar display for status bar |
| `frontend/src/lib/components/layout/BudgetPopover.svelte` | Expanded per-host detail popover |
| `frontend/src/lib/components/layout/budget-utils.ts` | Computation helpers (worst-case, thresholds, aggregation) |
| `frontend/src/lib/components/layout/budget-utils.test.ts` | Unit tests for computation logic |
| `frontend/tests/e2e/budget-display.spec.ts` | Playwright mock-based e2e tests |

### Frontend (modify)

| File | Change |
|------|--------|
| `frontend/src/lib/components/layout/StatusBar.svelte` | Replace `rateLimitText()` with `BudgetBars` + popover |
| `frontend/src/app.css` | Add `--budget-*` CSS variables |
| `frontend/tests/e2e/support/mockApi.ts` | Add `/api/v1/rate-limits` mock response |

### Generated (regenerate)

| File | How |
|------|-----|
| `internal/apiclient/generated/client.gen.go` | `make api-generate` |
| `packages/ui/src/api/generated/schema.ts` | `make api-generate` |

---

## Task 1: Add RateTracker accessor to GraphQLFetcher

**Files:**
- Modify: `internal/github/graphql.go`
- Test: `internal/github/graphql_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/github/graphql_test.go`:

```go
func TestGraphQLFetcherRateTracker(t *testing.T) {
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com", "graphql")
	f := NewGraphQLFetcher("fake-token", "github.com", rt, nil)
	require.Same(t, rt, f.RateTracker())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestGraphQLFetcherRateTracker -v`
Expected: FAIL — `f.RateTracker undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/github/graphql.go` after the `GraphQLFetcher` struct:

```go
// RateTracker returns the GraphQL rate tracker, or nil if none.
func (f *GraphQLFetcher) RateTracker() *RateTracker {
	return f.rateTracker
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestGraphQLFetcherRateTracker -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/github/graphql.go internal/github/graphql_test.go
git commit -m "feat: add RateTracker accessor to GraphQLFetcher"
```

---

## Task 2: Add GQLRateTrackers to Syncer

**Files:**
- Modify: `internal/github/sync.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/github/sync_test.go`:

```go
func TestSyncerGQLRateTrackers(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)

	rt := NewRateTracker(d, "github.com", "rest")
	gqlRT := NewRateTracker(d, "github.com", "graphql")

	syncer := NewSyncer(
		map[string]Client{"github.com": &mockClient{}},
		d, nil,
		[]RepoRef{{Owner: "acme", Name: "widget", PlatformHost: "github.com"}},
		time.Minute,
		map[string]*RateTracker{"github.com": rt},
		nil,
	)

	fetcher := NewGraphQLFetcher("token", "github.com", gqlRT, nil)
	syncer.SetFetchers(map[string]*GraphQLFetcher{"github.com": fetcher})

	gqlTrackers := syncer.GQLRateTrackers()
	assert.Len(gqlTrackers, 1)
	assert.Same(gqlRT, gqlTrackers["github.com"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncerGQLRateTrackers -v`
Expected: FAIL — `syncer.GQLRateTrackers undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/github/sync.go` after the `Budgets()` method:

```go
// GQLRateTrackers returns per-host GraphQL rate trackers
// extracted from the registered GraphQL fetchers.
func (s *Syncer) GQLRateTrackers() map[string]*RateTracker {
	result := make(map[string]*RateTracker, len(s.fetchers))
	for host, f := range s.fetchers {
		if rt := f.RateTracker(); rt != nil {
			result[host] = rt
		}
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix shell nixpkgs#go --command go test ./internal/github/ -run TestSyncerGQLRateTrackers -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: add GQLRateTrackers accessor to Syncer"
```

---

## Task 3: Add GQL fields to API response

**Files:**
- Modify: `internal/server/api_types.go`
- Modify: `internal/server/huma_routes.go`
- Test: `internal/server/api_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/api_test.go`:

```go
func TestAPIRateLimitsWithGQL(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	restRT := ghclient.NewRateTracker(database, "github.com", "rest")
	gqlRT := ghclient.NewRateTracker(database, "github.com", "graphql")

	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": restRT},
		nil,
	)

	fetcher := ghclient.NewGraphQLFetcher("token", "github.com", gqlRT, nil)
	syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com": fetcher,
	})

	// Simulate GraphQL rate data.
	gqlRT.UpdateFromRate(gh.Rate{
		Limit:     5000,
		Remaining: 4800,
		Reset:     gh.Timestamp{Time: time.Now().Add(30 * time.Minute)},
	})

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	host, ok := body.Hosts["github.com"]
	assert.True(ok)

	// GQL fields should be populated.
	assert.Equal(4800, host.GQLRemaining)
	assert.Equal(5000, host.GQLLimit)
	assert.True(host.GQLKnown)
	assert.NotEmpty(host.GQLResetAt)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix shell nixpkgs#go --command go test ./internal/server/ -run TestAPIRateLimitsWithGQL -v`
Expected: FAIL — `host.GQLRemaining undefined` (fields don't exist yet)

- [ ] **Step 3: Add GQL fields to rateLimitHostStatus**

Edit `internal/server/api_types.go`, add to `rateLimitHostStatus`:

```go
type rateLimitHostStatus struct {
	RequestsHour       int    `json:"requests_hour"`
	RateRemaining      int    `json:"rate_remaining"`
	RateLimit          int    `json:"rate_limit"`
	RateResetAt        string `json:"rate_reset_at"`
	HourStart          string `json:"hour_start"`
	SyncThrottleFactor int    `json:"sync_throttle_factor"`
	SyncPaused         bool   `json:"sync_paused"`
	ReserveBuffer      int    `json:"reserve_buffer"`
	Known              bool   `json:"known"`
	BudgetLimit        int    `json:"budget_limit"`
	BudgetSpent        int    `json:"budget_spent"`
	BudgetRemaining    int    `json:"budget_remaining"`
	GQLRemaining       int    `json:"gql_remaining"`
	GQLLimit           int    `json:"gql_limit"`
	GQLResetAt         string `json:"gql_reset_at"`
	GQLKnown           bool   `json:"gql_known"`
}
```

- [ ] **Step 4: Update getRateLimits handler**

Edit `internal/server/huma_routes.go`, update `getRateLimits` to populate GQL fields:

```go
func (s *Server) getRateLimits(
	_ context.Context, _ *struct{},
) (*rateLimitsOutput, error) {
	trackers := s.syncer.RateTrackers()
	gqlTrackers := s.syncer.GQLRateTrackers()
	budgets := s.syncer.Budgets()
	hosts := make(map[string]rateLimitHostStatus, len(trackers))
	for host, rt := range trackers {
		resetStr := ""
		if resetAt := rt.ResetAt(); resetAt != nil {
			resetStr = resetAt.UTC().Format(time.RFC3339)
		}
		status := rateLimitHostStatus{
			RequestsHour:       rt.RequestsThisHour(),
			RateRemaining:      rt.Remaining(),
			RateLimit:          rt.RateLimit(),
			RateResetAt:        resetStr,
			HourStart:          rt.HourStart().UTC().Format(time.RFC3339),
			SyncThrottleFactor: rt.ThrottleFactor(),
			SyncPaused:         rt.IsPaused(),
			ReserveBuffer:      ghclient.RateReserveBuffer,
			Known:              rt.Known(),
			GQLRemaining:       -1,
			GQLLimit:           -1,
		}
		if gqlRT := gqlTrackers[host]; gqlRT != nil {
			status.GQLRemaining = gqlRT.Remaining()
			status.GQLLimit = gqlRT.RateLimit()
			status.GQLKnown = gqlRT.Known()
			if resetAt := gqlRT.ResetAt(); resetAt != nil {
				status.GQLResetAt = resetAt.UTC().Format(time.RFC3339)
			}
		}
		if b := budgets[host]; b != nil {
			status.BudgetLimit = b.Limit()
			status.BudgetSpent = b.Spent()
			status.BudgetRemaining = b.Remaining()
		}
		hosts[host] = status
	}
	return &rateLimitsOutput{
		Body: rateLimitsResponse{Hosts: hosts},
	}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `nix shell nixpkgs#go --command go test ./internal/server/ -run TestAPIRateLimitsWithGQL -v`
Expected: PASS

- [ ] **Step 6: Run full test suite**

Run: `nix shell nixpkgs#go --command go test ./...`
Expected: All tests pass (existing tests should still work — new fields default to zero values)

- [ ] **Step 7: Commit**

```bash
git add internal/server/api_types.go internal/server/huma_routes.go internal/server/api_test.go
git commit -m "feat: add GraphQL rate limit fields to rate-limits endpoint"
```

---

## Task 4: Add E2E test for GQL unknown state

**Files:**
- Test: `internal/server/api_test.go`

- [ ] **Step 1: Write test for GQL defaults when no fetcher is set**

Add to `internal/server/api_test.go`:

```go
func TestAPIRateLimitsGQLDefaultsUnknown(t *testing.T) {
	assert := Assert.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	rt := ghclient.NewRateTracker(database, "github.com", "rest")
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil,
		[]ghclient.RepoRef{{
			Owner: "acme", Name: "widget",
			PlatformHost: "github.com",
		}},
		time.Minute,
		map[string]*ghclient.RateTracker{"github.com": rt},
		nil,
	)
	// No SetFetchers call — GQL data should be unknown.

	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/rate-limits")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(200, resp.StatusCode)

	var body rateLimitsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	host := body.Hosts["github.com"]
	assert.Equal(-1, host.GQLRemaining)
	assert.Equal(-1, host.GQLLimit)
	assert.False(host.GQLKnown)
	assert.Empty(host.GQLResetAt)
}
```

- [ ] **Step 2: Run test**

Run: `nix shell nixpkgs#go --command go test ./internal/server/ -run TestAPIRateLimitsGQLDefaultsUnknown -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/api_test.go
git commit -m "test: verify GQL rate limit defaults to unknown when no fetcher"
```

---

## Task 5: Regenerate API client

**Files:**
- Regenerate: `internal/apiclient/generated/client.gen.go`
- Regenerate: `packages/ui/src/api/generated/schema.ts`

- [ ] **Step 1: Regenerate**

Run: `nix shell nixpkgs#go --command make api-generate`

- [ ] **Step 2: Verify GQL fields appear in generated TypeScript**

Check that `packages/ui/src/api/generated/schema.ts` now includes `gql_remaining`, `gql_limit`, `gql_reset_at`, `gql_known` in `RateLimitHostStatus`.

- [ ] **Step 3: Verify Go client updated**

Check that `internal/apiclient/generated/client.gen.go` `RateLimitHostStatus` includes `GqlRemaining`, `GqlLimit`, `GqlResetAt`, `GqlKnown`.

- [ ] **Step 4: Run full test suite**

Run: `nix shell nixpkgs#go --command go test ./...`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add internal/apiclient/generated/ packages/ui/src/api/generated/ api/openapi.json
git commit -m "chore: regenerate API client with GQL rate limit fields"
```

---

## Task 6: Add budget CSS variables

**Files:**
- Modify: `frontend/src/app.css`

- [ ] **Step 1: Add variables to light theme**

Add to `frontend/src/app.css` inside the `:root` block, after existing accent variables:

```css
  --budget-green: #4ade80;
  --budget-yellow: #fbbf24;
  --budget-red: #f87171;
  --budget-blue: #60a5fa;
  --budget-bar-bg: #e5e7eb;
```

- [ ] **Step 2: Add variables to dark theme**

Add to `frontend/src/app.css` inside the `:root.dark` block:

```css
  --budget-green: #4ade80;
  --budget-yellow: #fbbf24;
  --budget-red: #f87171;
  --budget-blue: #60a5fa;
  --budget-bar-bg: #2a2a3e;
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/app.css
git commit -m "feat: add budget display CSS variables"
```

---

## Task 7: Create budget computation utilities

**Files:**
- Create: `frontend/src/lib/components/layout/budget-utils.ts`
- Create: `frontend/src/lib/components/layout/budget-utils.test.ts`

- [ ] **Step 1: Write failing tests**

Create `frontend/src/lib/components/layout/budget-utils.test.ts`:

```typescript
import { describe, expect, it } from "vitest";
import {
  budgetColor,
  worstCaseRatio,
  aggregateBudget,
  type HostBudgetEntry,
} from "./budget-utils";

describe("budgetColor", () => {
  it("returns green when above 20%", () => {
    expect(budgetColor(0.5)).toBe("var(--budget-green)");
  });
  it("returns yellow between 5% and 20%", () => {
    expect(budgetColor(0.15)).toBe("var(--budget-yellow)");
  });
  it("returns red below 5%", () => {
    expect(budgetColor(0.03)).toBe("var(--budget-red)");
  });
  it("returns yellow at exactly 20% boundary", () => {
    expect(budgetColor(0.2)).toBe("var(--budget-yellow)");
  });
  it("returns red at exactly 5% boundary", () => {
    expect(budgetColor(0.05)).toBe("var(--budget-red)");
  });
});

describe("worstCaseRatio", () => {
  it("picks lowest ratio from known hosts", () => {
    const entries: HostBudgetEntry[] = [
      { remaining: 4000, limit: 5000, known: true },
      { remaining: 500, limit: 5000, known: true },
    ];
    expect(worstCaseRatio(entries)).toBeCloseTo(0.1);
  });

  it("excludes unknown hosts", () => {
    const entries: HostBudgetEntry[] = [
      { remaining: 4000, limit: 5000, known: true },
      { remaining: -1, limit: -1, known: false },
    ];
    expect(worstCaseRatio(entries)).toBeCloseTo(0.8);
  });

  it("returns -1 when all hosts unknown", () => {
    const entries: HostBudgetEntry[] = [
      { remaining: -1, limit: -1, known: false },
    ];
    expect(worstCaseRatio(entries)).toBe(-1);
  });

  it("returns -1 for empty array", () => {
    expect(worstCaseRatio([])).toBe(-1);
  });
});

describe("aggregateBudget", () => {
  it("sums budget from enabled hosts", () => {
    const result = aggregateBudget([
      { budget_limit: 500, budget_spent: 42 },
      { budget_limit: 300, budget_spent: 10 },
    ]);
    expect(result).toEqual({ spent: 52, limit: 800, hasAny: true });
  });

  it("excludes disabled hosts", () => {
    const result = aggregateBudget([
      { budget_limit: 500, budget_spent: 42 },
      { budget_limit: 0, budget_spent: 0 },
    ]);
    expect(result).toEqual({ spent: 42, limit: 500, hasAny: true });
  });

  it("returns hasAny false when all disabled", () => {
    const result = aggregateBudget([
      { budget_limit: 0, budget_spent: 0 },
    ]);
    expect(result).toEqual({ spent: 0, limit: 0, hasAny: false });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && bun run test -- --run budget-utils`
Expected: FAIL — module not found

- [ ] **Step 3: Write implementation**

Create `frontend/src/lib/components/layout/budget-utils.ts`:

```typescript
export interface HostBudgetEntry {
  remaining: number;
  limit: number;
  known: boolean;
}

export function budgetColor(ratio: number): string {
  if (ratio > 0.2) return "var(--budget-green)";
  if (ratio > 0.05) return "var(--budget-yellow)";
  return "var(--budget-red)";
}

/**
 * Returns the lowest remaining/limit ratio across known hosts.
 * Returns -1 if no hosts have known data.
 */
export function worstCaseRatio(entries: HostBudgetEntry[]): number {
  let worst = Infinity;
  let hasKnown = false;
  for (const e of entries) {
    if (!e.known || e.limit <= 0) continue;
    hasKnown = true;
    const ratio = e.remaining / e.limit;
    if (ratio < worst) worst = ratio;
  }
  return hasKnown ? worst : -1;
}

/**
 * Aggregates budget across hosts, excluding disabled ones (limit == 0).
 */
export function aggregateBudget(
  entries: { budget_limit: number; budget_spent: number }[],
): { spent: number; limit: number; hasAny: boolean } {
  let spent = 0;
  let limit = 0;
  let hasAny = false;
  for (const e of entries) {
    if (e.budget_limit <= 0) continue;
    hasAny = true;
    spent += e.budget_spent;
    limit += e.budget_limit;
  }
  return { spent, limit, hasAny };
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && bun run test -- --run budget-utils`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/components/layout/budget-utils.ts frontend/src/lib/components/layout/budget-utils.test.ts
git commit -m "feat: add budget computation utilities with tests"
```

---

## Task 8: Create BudgetBars component

**Files:**
- Create: `frontend/src/lib/components/layout/BudgetBars.svelte`

- [ ] **Step 1: Create the component**

Create `frontend/src/lib/components/layout/BudgetBars.svelte`:

```svelte
<script lang="ts">
  import type { RateLimitHostStatus } from "@middleman/ui/api/types";
  import { budgetColor, worstCaseRatio, aggregateBudget } from "./budget-utils";

  interface Props {
    hosts: Record<string, RateLimitHostStatus>;
    onclick?: () => void;
  }

  let { hosts, onclick }: Props = $props();

  function anyPaused(): boolean {
    return Object.values(hosts).some((h) => h.sync_paused);
  }

  function restEntries() {
    return Object.values(hosts).map((h) => ({
      remaining: h.rate_remaining,
      limit: h.rate_limit,
      known: h.known,
    }));
  }

  function gqlEntries() {
    return Object.values(hosts).map((h) => ({
      remaining: h.gql_remaining ?? -1,
      limit: h.gql_limit ?? -1,
      known: h.gql_known ?? false,
    }));
  }

  function restRatio() {
    return worstCaseRatio(restEntries());
  }

  function gqlRatio() {
    return worstCaseRatio(gqlEntries());
  }

  function barColor(ratio: number): string {
    if (anyPaused()) return "var(--budget-red)";
    return budgetColor(ratio);
  }

  function budget() {
    return aggregateBudget(Object.values(hosts));
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<span class="budget-bars" {onclick}>
  {@const rr = restRatio()}
  {@const gr = gqlRatio()}
  {@const b = budget()}
  {@const paused = anyPaused()}

  <span class="budget-bar-group">
    <span
      class="budget-label"
      style:color={rr >= 0 ? barColor(rr) : paused ? "var(--budget-red)" : "var(--text-muted)"}
    >{rr >= 0 ? "REST" : "--"}</span>
    <span class="budget-track">
      {#if rr >= 0}
        <span
          class="budget-fill"
          style:width="{Math.max(rr * 100, 2)}%"
          style:background={barColor(rr)}
        ></span>
      {/if}
    </span>
  </span>

  <span class="budget-bar-group">
    <span
      class="budget-label"
      style:color={gr >= 0 ? barColor(gr) : paused ? "var(--budget-red)" : "var(--text-muted)"}
    >{gr >= 0 ? "GQL" : "--"}</span>
    <span class="budget-track">
      {#if gr >= 0}
        <span
          class="budget-fill"
          style:width="{Math.max(gr * 100, 2)}%"
          style:background={barColor(gr)}
        ></span>
      {/if}
    </span>
  </span>

  {#if b.hasAny}
    <span class="budget-count">{b.spent} req/hr</span>
  {/if}
</span>

<style>
  .budget-bars {
    display: flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
    padding: 1px 4px;
    border-radius: 3px;
  }
  .budget-bars:hover {
    background: var(--bg-hover, rgba(255, 255, 255, 0.05));
  }
  .budget-bar-group {
    display: flex;
    align-items: center;
    gap: 3px;
  }
  .budget-label {
    font-size: 9px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  .budget-track {
    display: inline-block;
    width: 32px;
    height: 4px;
    background: var(--budget-bar-bg);
    border-radius: 2px;
    overflow: hidden;
  }
  .budget-fill {
    display: block;
    height: 100%;
    border-radius: 2px;
    transition: width 0.5s ease;
  }
  .budget-count {
    color: var(--budget-blue);
    font-size: 10px;
  }
</style>
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && bun run build`
Expected: Build succeeds (component isn't wired yet, just needs to compile)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/components/layout/BudgetBars.svelte
git commit -m "feat: add BudgetBars compact status bar component"
```

---

## Task 9: Create BudgetPopover component

**Files:**
- Create: `frontend/src/lib/components/layout/BudgetPopover.svelte`

- [ ] **Step 1: Create the component**

Create `frontend/src/lib/components/layout/BudgetPopover.svelte`:

```svelte
<script lang="ts">
  import type { RateLimitHostStatus } from "@middleman/ui/api/types";
  import { budgetColor } from "./budget-utils";

  interface Props {
    hosts: Record<string, RateLimitHostStatus>;
    onclose: () => void;
  }

  let { hosts, onclose }: Props = $props();

  let popoverEl: HTMLDivElement | undefined = $state();

  function handleClickOutside(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) {
      onclose();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onclose();
    }
  }

  $effect(() => {
    // Delay registration to avoid catching the click that opened the popover.
    const id = setTimeout(() => {
      document.addEventListener("click", handleClickOutside);
    }, 0);
    document.addEventListener("keydown", handleKeydown);
    return () => {
      clearTimeout(id);
      document.removeEventListener("click", handleClickOutside);
      document.removeEventListener("keydown", handleKeydown);
    };
  });

  function hostEntries() {
    return Object.entries(hosts);
  }

  function ratio(remaining: number, limit: number): number {
    if (limit <= 0) return -1;
    return remaining / limit;
  }

  function resetText(resetAt: string): string {
    if (!resetAt) return "";
    const ms = new Date(resetAt).getTime() - Date.now();
    if (ms <= 0) return "";
    const min = Math.ceil(ms / 60_000);
    return `resets ${min}m`;
  }

  function hostHealthColor(h: RateLimitHostStatus): string {
    const rr = h.known && h.rate_limit > 0 ? h.rate_remaining / h.rate_limit : 1;
    const gr = (h.gql_known ?? false) && (h.gql_limit ?? 0) > 0
      ? (h.gql_remaining ?? 0) / (h.gql_limit ?? 1)
      : 1;
    return budgetColor(Math.min(rr, gr));
  }

  const singleHost = $derived(hostEntries().length === 1);
</script>

<div class="budget-popover" bind:this={popoverEl}>
  <div class="popover-header">API Budget</div>

  {#each hostEntries() as [hostname, h], i}
    {#if i > 0}
      <div class="popover-divider"></div>
    {/if}

    <div class="host-section">
      {#if !singleHost}
        <div class="host-name">
          <span class="health-dot" style:background={hostHealthColor(h)}></span>
          {hostname}
        </div>
      {/if}

      <!-- REST -->
      <div class="budget-row">
        <span class="row-label">REST</span>
        <span class="row-bar">
          {@const rr = ratio(h.rate_remaining, h.rate_limit)}
          {#if h.known && rr >= 0}
            <span class="bar-track">
              <span
                class="bar-fill"
                style:width="{Math.max(rr * 100, 2)}%"
                style:background={budgetColor(rr)}
              ></span>
            </span>
            <span class="row-detail">
              {h.rate_remaining.toLocaleString()} / {h.rate_limit.toLocaleString()} requests
            </span>
            {#if resetText(h.rate_reset_at)}
              <span class="row-reset">{resetText(h.rate_reset_at)}</span>
            {/if}
          {:else}
            <span class="row-unknown">not yet observed</span>
          {/if}
        </span>
      </div>

      <!-- GraphQL -->
      <div class="budget-row">
        <span class="row-label">GraphQL</span>
        <span class="row-bar">
          {@const gr = ratio(h.gql_remaining ?? -1, h.gql_limit ?? -1)}
          {#if (h.gql_known ?? false) && gr >= 0}
            <span class="bar-track">
              <span
                class="bar-fill"
                style:width="{Math.max(gr * 100, 2)}%"
                style:background={budgetColor(gr)}
              ></span>
            </span>
            <span class="row-detail">
              {(h.gql_remaining ?? 0).toLocaleString()} / {(h.gql_limit ?? 0).toLocaleString()} points
            </span>
            {#if resetText(h.gql_reset_at ?? "")}
              <span class="row-reset">{resetText(h.gql_reset_at ?? "")}</span>
            {/if}
          {:else}
            <span class="row-unknown">not yet observed</span>
          {/if}
        </span>
      </div>

      <!-- Middleman Budget -->
      {#if h.budget_limit > 0}
        <div class="budget-row">
          <span class="row-label">Middleman</span>
          <span class="row-bar">
            <span class="budget-spent">{h.budget_spent}</span>
            <span class="row-detail"> / {h.budget_limit} req/hr</span>
          </span>
        </div>
      {/if}

      <!-- Throttle -->
      {#if h.sync_paused}
        <div class="throttle-indicator throttle-paused">sync paused</div>
      {:else if h.sync_throttle_factor > 1}
        <div class="throttle-indicator">sync {h.sync_throttle_factor}x slower</div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .budget-popover {
    position: absolute;
    bottom: calc(100% + 4px);
    right: 0;
    width: 320px;
    max-height: 400px;
    overflow-y: auto;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: 8px;
    padding: 12px 16px;
    box-shadow: 0 -4px 24px rgba(0, 0, 0, 0.2);
    z-index: 100;
    font-size: 11px;
  }
  .popover-header {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.8px;
    color: var(--text-muted);
    margin-bottom: 10px;
  }
  .popover-divider {
    border-top: 1px solid var(--border-muted);
    margin: 10px 0;
  }
  .host-name {
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 8px;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .health-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .budget-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
  }
  .row-label {
    color: var(--text-muted);
    font-size: 10px;
    width: 52px;
    flex-shrink: 0;
  }
  .row-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 0;
  }
  .bar-track {
    width: 60px;
    height: 5px;
    background: var(--budget-bar-bg);
    border-radius: 3px;
    overflow: hidden;
    flex-shrink: 0;
  }
  .bar-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.5s ease;
  }
  .row-detail {
    color: var(--text-muted);
    font-size: 10px;
    white-space: nowrap;
  }
  .row-reset {
    color: var(--text-muted);
    font-size: 9px;
    opacity: 0.7;
  }
  .row-unknown {
    color: var(--text-muted);
    font-size: 10px;
    font-style: italic;
  }
  .budget-spent {
    color: var(--budget-blue);
    font-weight: 600;
  }
  .throttle-indicator {
    font-size: 10px;
    color: var(--accent-amber);
    margin-top: 4px;
  }
  .throttle-paused {
    color: var(--accent-red);
  }
</style>
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && bun run build`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/components/layout/BudgetPopover.svelte
git commit -m "feat: add BudgetPopover expanded detail component"
```

---

## Task 10: Wire BudgetBars into StatusBar

**Files:**
- Modify: `frontend/src/lib/components/layout/StatusBar.svelte`

- [ ] **Step 1: Replace rateLimitText with BudgetBars**

Rewrite `frontend/src/lib/components/layout/StatusBar.svelte`. Remove the `rateLimitText()` function and `rateInfo` derived. Import and use `BudgetBars` and `BudgetPopover` instead:

```svelte
<script lang="ts">
  import { getStores } from "@middleman/ui";
  import BudgetBars from "./BudgetBars.svelte";
  import BudgetPopover from "./BudgetPopover.svelte";

  const { pulls, issues, sync } = getStores();

  const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");

  let appVersion = $state("");

  $effect(() => {
    fetch(`${basePath}/api/v1/version`)
      .then((r) => r.ok ? r.json() : null)
      .then((data) => { if (data?.version) appVersion = data.version; })
      .catch(() => {});
  });

  let tick = $state(0);
  let tickHandle: ReturnType<typeof setInterval> | null = null;
  $effect(() => {
    tickHandle = setInterval(() => { tick++; }, 10_000);
    return () => { if (tickHandle !== null) clearInterval(tickHandle); };
  });

  function syncText(): string {
    void tick;
    const st = sync.getSyncState();
    if (st === null) return "";
    if (st.running) {
      if (st.progress) {
        return `syncing (${st.progress})`;
      }
      return "syncing\u2026";
    }
    if (!st.last_run_at) return "not synced";
    const diffMs = Date.now() - new Date(st.last_run_at).getTime();
    const mins = Math.floor(diffMs / 60_000);
    if (mins < 1) return "synced just now";
    if (mins < 60) return `synced ${mins}m ago`;
    return `synced ${Math.floor(mins / 60)}h ago`;
  }

  function repoCount(): number {
    const repos = new Set<string>();
    for (const pr of pulls.getPulls()) repos.add(`${pr.repo_owner}/${pr.repo_name}`);
    for (const issue of issues.getIssues()) repos.add(`${issue.repo_owner}/${issue.repo_name}`);
    return repos.size;
  }

  let popoverOpen = $state(false);

  function togglePopover() {
    popoverOpen = !popoverOpen;
  }

  function closePopover() {
    popoverOpen = false;
  }

  let rateLimitHosts = $derived.by(() => {
    void tick;
    return sync.getRateLimits();
  });
  let hasHosts = $derived(Object.keys(rateLimitHosts).length > 0);
</script>

<footer class="status-bar">
  <div class="status-left">
    <span class="status-item">{pulls.getPulls().length} PRs</span>
    <span class="status-sep">&middot;</span>
    <span class="status-item">{issues.getIssues().length} issues</span>
    <span class="status-sep">&middot;</span>
    <span class="status-item">{repoCount()} repos</span>
  </div>
  <div class="status-right">
    {#if hasHosts}
      <span class="budget-wrapper">
        <BudgetBars hosts={rateLimitHosts} onclick={togglePopover} />
        {#if popoverOpen}
          <BudgetPopover hosts={rateLimitHosts} onclose={closePopover} />
        {/if}
      </span>
      <span class="status-sep">&middot;</span>
    {/if}
    {#if sync.getSyncState()?.last_error}
      <span class="status-item status-item--error" title={sync.getSyncState()?.last_error}>sync error</span>
      <span class="status-sep">&middot;</span>
    {/if}
    <span class="status-item" class:status-item--active={sync.getSyncState()?.running}>
      {#if sync.getSyncState()?.running}
        <span class="sync-dot"></span>
      {/if}
      {syncText()}
    </span>
    {#if appVersion}
      <span class="status-sep">&middot;</span>
      <span class="status-item status-item--version">{appVersion}</span>
    {/if}
  </div>
</footer>

<style>
  .status-bar {
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--bg-surface);
    border-top: 1px solid var(--border-muted);
    flex-shrink: 0;
    font-size: 10px;
    color: var(--text-muted);
  }
  .status-left, .status-right {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .status-sep {
    color: var(--border-default);
  }
  .status-item--error {
    color: var(--accent-red);
  }
  .status-item--active {
    color: var(--accent-green);
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .sync-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent-green);
    animation: pulse 1.5s ease-in-out infinite;
  }
  .budget-wrapper {
    position: relative;
    display: flex;
    align-items: center;
  }
  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }
</style>
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && bun run build`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/components/layout/StatusBar.svelte
git commit -m "feat: replace text rate-limit display with BudgetBars and popover"
```

---

## Task 11: Extend mockApi with rate-limits data

**Files:**
- Modify: `frontend/tests/e2e/support/mockApi.ts`

- [ ] **Step 1: Add rate-limits mock data and route**

Add rate-limits fixture data and handler to `frontend/tests/e2e/support/mockApi.ts`:

```typescript
const rateLimits = {
  hosts: {
    "github.com": {
      requests_hour: 188,
      rate_remaining: 4812,
      rate_limit: 5000,
      rate_reset_at: new Date(Date.now() + 42 * 60_000).toISOString(),
      hour_start: new Date(Date.now() - 18 * 60_000).toISOString(),
      sync_throttle_factor: 1,
      sync_paused: false,
      reserve_buffer: 200,
      known: true,
      budget_limit: 500,
      budget_spent: 42,
      budget_remaining: 458,
      gql_remaining: 4950,
      gql_limit: 5000,
      gql_reset_at: new Date(Date.now() + 38 * 60_000).toISOString(),
      gql_known: true,
    },
  },
};
```

Add this handler inside the `mockApi` route callback, after the sync/status handler:

```typescript
    if (method === "GET" && pathname === "/api/v1/rate-limits") {
      await fulfillJson(route, rateLimits);
      return;
    }
```

- [ ] **Step 2: Commit**

```bash
git add frontend/tests/e2e/support/mockApi.ts
git commit -m "test: add rate-limits mock data for Playwright e2e"
```

---

## Task 12: Playwright mock-based e2e tests

**Files:**
- Create: `frontend/tests/e2e/budget-display.spec.ts`

- [ ] **Step 1: Write e2e tests**

Create `frontend/tests/e2e/budget-display.spec.ts`:

```typescript
import { expect, test } from "@playwright/test";

import { mockApi } from "./support/mockApi";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("status bar shows budget bars with known data", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await expect(bars).toBeVisible();
  await expect(bars.getByText("REST")).toBeVisible();
  await expect(bars.getByText("GQL")).toBeVisible();
});

test("budget bars show middleman count when budget enabled", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await expect(bars.getByText("42 req/hr")).toBeVisible();
});

test("clicking budget area opens popover", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await bars.click();

  const popover = page.locator(".budget-popover");
  await expect(popover).toBeVisible();
  await expect(popover.getByText("API Budget")).toBeVisible();
  await expect(popover.getByText(/requests/)).toBeVisible();
  await expect(popover.getByText(/points/)).toBeVisible();
});

test("popover dismisses on Escape", async ({ page }) => {
  await page.goto("/pulls");

  await page.locator(".budget-bars").click();
  await expect(page.locator(".budget-popover")).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(page.locator(".budget-popover")).not.toBeVisible();
});

test("popover dismisses on click outside", async ({ page }) => {
  await page.goto("/pulls");

  await page.locator(".budget-bars").click();
  await expect(page.locator(".budget-popover")).toBeVisible();

  // Click outside the popover (on the main content area)
  await page.locator(".app-main").click();
  await expect(page.locator(".budget-popover")).not.toBeVisible();
});

test("mixed known/unknown hosts show worst-case from known only", async ({ page }) => {
  await page.route("**/api/v1/rate-limits", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        hosts: {
          "github.com": {
            requests_hour: 100,
            rate_remaining: 4500,
            rate_limit: 5000,
            rate_reset_at: new Date(Date.now() + 30 * 60_000).toISOString(),
            hour_start: new Date().toISOString(),
            sync_throttle_factor: 1,
            sync_paused: false,
            reserve_buffer: 200,
            known: true,
            budget_limit: 500,
            budget_spent: 100,
            budget_remaining: 400,
            gql_remaining: 4900,
            gql_limit: 5000,
            gql_reset_at: new Date(Date.now() + 25 * 60_000).toISOString(),
            gql_known: true,
          },
          "ghe.corp.example.com": {
            requests_hour: 0,
            rate_remaining: -1,
            rate_limit: -1,
            rate_reset_at: "",
            hour_start: new Date().toISOString(),
            sync_throttle_factor: 1,
            sync_paused: false,
            reserve_buffer: 200,
            known: false,
            budget_limit: 0,
            budget_spent: 0,
            budget_remaining: 0,
            gql_remaining: -1,
            gql_limit: -1,
            gql_reset_at: "",
            gql_known: false,
          },
        },
      }),
    });
  });

  await page.goto("/pulls");

  // Should show REST/GQL labels (not --) because github.com is known
  const bars = page.locator(".budget-bars");
  await expect(bars.getByText("REST")).toBeVisible();
  await expect(bars.getByText("GQL")).toBeVisible();

  // REST bar fill should reflect github.com's 90% ratio (green)
  const restFill = bars.locator(".budget-fill").first();
  await expect(restFill).toBeVisible();

  // Popover should show both hosts
  await bars.click();
  const popover = page.locator(".budget-popover");
  await expect(popover.getByText("github.com")).toBeVisible();
  await expect(popover.getByText("ghe.corp.example.com")).toBeVisible();
  // Known host shows requests, unknown shows "not yet observed"
  await expect(popover.getByText(/4,500.*5,000.*requests/)).toBeVisible();
  await expect(popover.getByText("not yet observed").first()).toBeVisible();
});

test("budget bars show unknown state when host not known", async ({ page }) => {
  await page.route("**/api/v1/rate-limits", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        hosts: {
          "github.com": {
            requests_hour: 0,
            rate_remaining: -1,
            rate_limit: -1,
            rate_reset_at: "",
            hour_start: new Date().toISOString(),
            sync_throttle_factor: 1,
            sync_paused: false,
            reserve_buffer: 200,
            known: false,
            budget_limit: 0,
            budget_spent: 0,
            budget_remaining: 0,
            gql_remaining: -1,
            gql_limit: -1,
            gql_reset_at: "",
            gql_known: false,
          },
        },
      }),
    });
  });

  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await expect(bars).toBeVisible();
  // Unknown state: labels show -- instead of REST/GQL
  await expect(bars.getByText("--").first()).toBeVisible();
  await expect(bars.getByText("REST")).not.toBeVisible();
  await expect(bars.getByText("GQL")).not.toBeVisible();
  // No budget count when budget disabled
  await expect(bars.getByText("req/hr")).not.toBeVisible();
});
```

- [ ] **Step 2: Run e2e tests**

Run: `cd frontend && bun run test:e2e -- --grep "budget"`
Expected: All pass

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e/budget-display.spec.ts
git commit -m "test: add Playwright e2e tests for budget display"
```

---

## Task 13: Full-stack Playwright e2e test

**Files:**
- Modify: `cmd/e2e-server/main.go`
- Create: `frontend/tests/e2e-full/budget-display.spec.ts`

The e2e server currently creates the syncer with `nil` rate trackers and `nil` budgets, so the rate-limits endpoint returns an empty hosts map. To test budget bars with real data, the e2e server needs rate trackers seeded with known values.

- [ ] **Step 1: Add rate tracker to e2e server**

Edit `cmd/e2e-server/main.go` to create a rate tracker and budget before constructing the syncer:

```go
rt := ghclient.NewRateTracker(database, "github.com", "rest")
// Seed with known values so the budget bars render.
rt.UpdateFromRate(gh.Rate{
	Limit:     5000,
	Remaining: 4200,
	Reset:     gh.Timestamp{Time: time.Now().Add(45 * time.Minute)},
})

gqlRT := ghclient.NewRateTracker(database, "github.com", "graphql")
gqlRT.UpdateFromRate(gh.Rate{
	Limit:     5000,
	Remaining: 4800,
	Reset:     gh.Timestamp{Time: time.Now().Add(40 * time.Minute)},
})

budget := ghclient.NewSyncBudget(500)
budget.Spend(75)

syncer := ghclient.NewSyncer(
	map[string]ghclient.Client{"github.com": fc},
	database, diffRepo.Manager, repos, time.Hour,
	map[string]*ghclient.RateTracker{"github.com": rt},
	map[string]*ghclient.SyncBudget{"github.com": budget},
)

// Wire GraphQL fetcher so GQL rate data appears in the endpoint.
gqlFetcher := ghclient.NewGraphQLFetcher("fake-token", "github.com", gqlRT, budget)
syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{"github.com": gqlFetcher})
```

- [ ] **Step 2: Write the full-stack e2e test**

Create `frontend/tests/e2e-full/budget-display.spec.ts`:

```typescript
import { expect, test } from "@playwright/test";

test("budget bars render with seeded rate-limit data", async ({ page }) => {
  await page.goto("/pulls");

  const bars = page.locator(".budget-bars");
  await expect(bars).toBeVisible();
  await expect(bars.getByText("REST")).toBeVisible();
});

test("popover shows per-host breakdown from seeded data", async ({ page }) => {
  await page.goto("/pulls");

  await page.locator(".budget-bars").click();

  const popover = page.locator(".budget-popover");
  await expect(popover).toBeVisible();
  await expect(popover.getByText(/requests/)).toBeVisible();
  await expect(popover.getByText(/points/)).toBeVisible(); // GQL data
  await expect(popover.getByText(/75/)).toBeVisible(); // budget_spent
});
```

- [ ] **Step 3: Build the e2e server**

Run: `nix shell nixpkgs#go --command go build -o frontend/cmd/e2e-server/e2e-server ./cmd/e2e-server`

- [ ] **Step 4: Run the full-stack e2e tests**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts --grep "budget"`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add cmd/e2e-server/main.go frontend/tests/e2e-full/budget-display.spec.ts
git commit -m "test: add full-stack Playwright e2e test for budget display"
```

---

## Task 14: Full test suite verification

- [ ] **Step 1: Run Go tests**

Run: `nix shell nixpkgs#go --command go test ./...`
Expected: All pass

- [ ] **Step 2: Run frontend unit tests**

Run: `cd frontend && bun run test`
Expected: All pass

- [ ] **Step 3: Run mock-based Playwright tests**

Run: `cd frontend && bun run test:e2e`
Expected: All pass

- [ ] **Step 4: Build full binary**

Run: `nix shell nixpkgs#go --command make build`
Expected: Build succeeds with embedded frontend
