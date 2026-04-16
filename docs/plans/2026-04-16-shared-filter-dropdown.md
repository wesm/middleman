# Shared Filter Dropdown Implementation Plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Reuse same popover-style filter control for roborev status filtering that activity feed already uses by extracting shared UI and wiring both surfaces to it.

**Architecture:** Extract dropdown trigger, popover, section, and item-row rendering into focused shared Svelte component in `packages/ui/src/components/shared/`. Keep filtering state and store updates in callers. Activity feed uses shared component in multi-select mode with reset action. Roborev filter bar uses same component in single-select mode for statuses.

**Tech Stack:** Svelte 5, TypeScript, Playwright e2e, Vitest where useful.

---

### Task 1: Capture baseline behavior

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `docs/plans/2026-04-16-shared-filter-dropdown.md`
- Test: `frontend/tests/e2e-full/activity-filters.spec.ts`
- Test: `frontend/tests/e2e-full/roborev-e2e.spec.ts`

- [ ] **Step 1: Run activity filter e2e baseline**

```bash
bunx playwright test frontend/tests/e2e-full/activity-filters.spec.ts
```

Expected: PASS. Confirms existing event filter popover behavior before refactor.

- [ ] **Step 2: Run roborev filter e2e baseline**

```bash
bunx playwright test frontend/tests/e2e-full/roborev-e2e.spec.ts
```

Expected: PASS. Confirms status filtering works before replacing `<select>`.

### Task 2: Extract shared dropdown filter component

**TDD scenario:** New feature — full TDD cycle

**Files:**
- Create: `packages/ui/src/components/shared/FilterDropdown.svelte`
- Modify: `packages/ui/src/index.ts`
- Test: `frontend/tests/e2e-full/activity-filters.spec.ts`

- [ ] **Step 1: Write failing interaction test indirectly through existing activity filter e2e**

```ts
await page.locator(".filter-btn").click();
await expect(page.locator(".filter-dropdown")).toBeVisible();
await page.locator(".filter-item", { hasText: "Comments" }).click();
await expect(page.locator(".evt-label.evt-comment")).toHaveCount(0);
```

This already exists in `frontend/tests/e2e-full/activity-filters.spec.ts`. Re-run after extraction to verify no regressions.

- [ ] **Step 2: Create minimal shared component**

```svelte
<script lang="ts">
  export interface FilterDropdownSection {
    title?: string;
    items: {
      id: string;
      label: string;
      active: boolean;
      color?: string;
      disabled?: boolean;
      onSelect: () => void;
    }[];
  }
</script>
```

Render trigger button, badge, outside-click closing, section headings, item rows, optional reset action, and slot-free prop-driven layout matching current activity filter DOM classes.

- [ ] **Step 3: Export shared component**

```ts
export { default as FilterDropdown } from "./components/shared/FilterDropdown.svelte";
```

- [ ] **Step 4: Re-run activity filter e2e**

```bash
bunx playwright test frontend/tests/e2e-full/activity-filters.spec.ts
```

Expected: PASS.

### Task 3: Refactor activity feed to use shared component

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `packages/ui/src/components/ActivityFeed.svelte`
- Modify: `packages/ui/src/components/shared/FilterDropdown.svelte`
- Test: `frontend/tests/e2e-full/activity-filters.spec.ts`

- [ ] **Step 1: Replace inline dropdown markup with shared component props**

```svelte
<FilterDropdown
  label="Filters"
  activeCount={hiddenFilterCount}
  sections={activitySections}
  resetLabel="Show all"
  onReset={resetFilters}
/>
```

Move activity-specific state updates into computed section/item data while preserving existing labels, colors, and reset behavior.

- [ ] **Step 2: Remove duplicated dropdown state and styles no longer needed in `ActivityFeed.svelte`**

Delete local outside-click handling, inline dropdown markup, and dropdown-specific CSS that now belongs in shared component.

- [ ] **Step 3: Run activity filter e2e again**

```bash
bunx playwright test frontend/tests/e2e-full/activity-filters.spec.ts
```

Expected: PASS.

### Task 4: Refactor roborev status filter to use shared component

**TDD scenario:** New feature — full TDD cycle

**Files:**
- Modify: `packages/ui/src/components/roborev/FilterBar.svelte`
- Modify: `packages/ui/src/components/shared/FilterDropdown.svelte`
- Test: `frontend/tests/e2e-full/roborev-e2e.spec.ts`

- [ ] **Step 1: Update e2e test to drive popover instead of `<select>`**

```ts
await page.locator(".filter-btn", { hasText: "Status" }).click();
await page.locator(".filter-item", { hasText: "Done" }).click();
await waitForJobRows(page, 1);
```

Expected before implementation: FAIL because roborev still renders `.status-select`.

- [ ] **Step 2: Replace `<select>` with shared dropdown in single-select mode**

```svelte
<FilterDropdown
  label="Status"
  selectedLabel={statusLabel}
  activeCount={jobsStore?.getFilterStatus() ? 1 : 0}
  sections={[statusSection]}
/>
```

Include `All statuses`, status colors where useful, and close popover after selection.

- [ ] **Step 3: Keep existing store contract**

```ts
jobsStore?.setFilter("status", value || undefined);
```

Only view changes. Query/store behavior stays unchanged.

- [ ] **Step 4: Run roborev e2e**

```bash
bunx playwright test frontend/tests/e2e-full/roborev-e2e.spec.ts
```

Expected: PASS.

### Task 5: Final verification and commit

**TDD scenario:** Modifying tested code — run existing tests after change

**Files:**
- Modify: `packages/ui/src/components/ActivityFeed.svelte`
- Modify: `packages/ui/src/components/roborev/FilterBar.svelte`
- Modify: `packages/ui/src/components/shared/FilterDropdown.svelte`
- Modify: `packages/ui/src/index.ts`
- Modify: `frontend/tests/e2e-full/roborev-e2e.spec.ts`

- [ ] **Step 1: Run focused frontend typecheck if component API changed**

```bash
cd packages/ui && bun run typecheck
```

Expected: PASS.

- [ ] **Step 2: Run both affected e2e specs together**

```bash
bunx playwright test frontend/tests/e2e-full/activity-filters.spec.ts frontend/tests/e2e-full/roborev-e2e.spec.ts
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add packages/ui/src/components/shared/FilterDropdown.svelte \
  packages/ui/src/components/ActivityFeed.svelte \
  packages/ui/src/components/roborev/FilterBar.svelte \
  packages/ui/src/index.ts \
  frontend/tests/e2e-full/roborev-e2e.spec.ts \
  docs/plans/2026-04-16-shared-filter-dropdown.md

git commit -m "refactor: share filter dropdown UI" -m $'🤖 Generated with [OpenAI Codex](https://openai.com/codex)\nCo-authored-by: OpenAI Codex <noreply@openai.com>'
```

Expected: commit created with only relevant files staged.
