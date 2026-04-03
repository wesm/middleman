# Ungrouped View Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a shared "By Repo / All" toggle that switches PR, Issue, and Activity Threaded views between repo-grouped and flat chronological modes.

**Architecture:** A new shared Svelte store (`grouping.svelte.ts`) holds the boolean toggle, persisted in localStorage. Each list component reads the toggle and conditionally renders grouped (existing) or flat (new) layout. Flat mode adds a colored repo pill badge to each item. No backend changes needed.

**Tech Stack:** Svelte 5 (runes), TypeScript, Playwright (E2E tests)

**Spec:** `docs/superpowers/specs/2026-04-03-ungrouped-view-mode-design.md`

---

### Task 1: Create the shared grouping store

**Files:**
- Create: `frontend/src/lib/stores/grouping.svelte.ts`

- [ ] **Step 1: Create the grouping store**

```typescript
// frontend/src/lib/stores/grouping.svelte.ts
const STORAGE_KEY = "middleman:groupByRepo";

function readFromStorage(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== "false";
  } catch {
    return true;
  }
}

let groupByRepo = $state(readFromStorage());

export function getGroupByRepo(): boolean {
  return groupByRepo;
}

export function setGroupByRepo(value: boolean): void {
  groupByRepo = value;
  try {
    localStorage.setItem(STORAGE_KEY, String(value));
  } catch {
    // localStorage unavailable (e.g., private browsing quota).
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/stores/grouping.svelte.ts
git commit -m "feat: add shared grouping store with localStorage persistence"
```

---

### Task 2: Add repo color utility

A small helper that hashes a repo name to one of the accent colors. Used by PullItem, IssueItem, and ActivityThreaded.

**Files:**
- Create: `frontend/src/lib/utils/repo-color.ts`

- [ ] **Step 1: Create the utility**

```typescript
// frontend/src/lib/utils/repo-color.ts
const ACCENT_COLORS = [
  "var(--accent-blue)",
  "var(--accent-amber)",
  "var(--accent-green)",
  "var(--accent-red)",
  "var(--accent-purple)",
  "var(--accent-teal)",
] as const;

export function repoColor(repoName: string): string {
  let hash = 0;
  for (let i = 0; i < repoName.length; i++) {
    hash = ((hash << 5) - hash + repoName.charCodeAt(i)) | 0;
  }
  const idx = ((hash % ACCENT_COLORS.length) + ACCENT_COLORS.length)
    % ACCENT_COLORS.length;
  return ACCENT_COLORS[idx]!;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/utils/repo-color.ts
git commit -m "feat: add repo color hash utility"
```

---

### Task 3: Update pulls store — toggle-aware display order

Make `getDisplayOrderPRs()` return flat chronological order when the toggle is set to "All".

**Files:**
- Modify: `frontend/src/lib/stores/pulls.svelte.ts`

- [ ] **Step 1: Import grouping store and router, update getDisplayOrderPRs**

Add imports at top of file:

```typescript
import { getGroupByRepo } from "./grouping.svelte.js";
import { getView } from "./router.svelte.js";
```

Replace the existing `getDisplayOrderPRs()` function (around line 79-87):

```typescript
/** Returns PRs in display order: grouped by repo when groupByRepo is true
 *  or when in board view (board navigation is unaffected by toggle),
 *  flat chronological otherwise. */
export function getDisplayOrderPRs(): PullRequest[] {
  if (getGroupByRepo() || getView() === "board") {
    const grouped = pullsByRepo();
    const ordered: PullRequest[] = [];
    for (const prs of grouped.values()) {
      ordered.push(...prs);
    }
    return ordered;
  }
  return pulls;
}
```

Note: `pulls` is already sorted by `last_activity_at DESC` from the API. The `getView() === "board"` check ensures board mode always uses grouped order regardless of toggle state.

- [ ] **Step 2: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/stores/pulls.svelte.ts
git commit -m "feat: make PR display order toggle-aware"
```

---

### Task 4: Update issues store — toggle-aware display order

Same pattern as pulls.

**Files:**
- Modify: `frontend/src/lib/stores/issues.svelte.ts`

- [ ] **Step 1: Import grouping store and update getDisplayOrderIssues**

Add import at top of file:

```typescript
import { getGroupByRepo } from "./grouping.svelte.js";
```

Replace the existing `getDisplayOrderIssues()` function (around line 224-230):

```typescript
// Navigation — uses display order (grouped by repo, or flat if ungrouped)
function getDisplayOrderIssues(): Issue[] {
  if (getGroupByRepo()) {
    const grouped = issuesByRepo();
    const ordered: Issue[] = [];
    for (const items of grouped.values()) {
      ordered.push(...items);
    }
    return ordered;
  }
  return issues;
}
```

- [ ] **Step 2: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/stores/issues.svelte.ts
git commit -m "feat: make issue display order toggle-aware"
```

---

### Task 5: Add repo pill badge to PullItem

Add an optional `showRepo` prop. When true, render a colored repo badge in the meta row before the number/author.

**Files:**
- Modify: `frontend/src/lib/components/sidebar/PullItem.svelte`

- [ ] **Step 1: Add prop and import**

In the `<script>` section, add the import after the existing imports:

```typescript
import { repoColor } from "../../utils/repo-color.js";
```

Update the Props interface and destructuring:

```typescript
  interface Props {
    pr: PullRequest;
    selected: boolean;
    showRepo: boolean;
    onclick: () => void;
  }

  const { pr, selected, showRepo, onclick }: Props = $props();
```

Add a derived for the repo name:

```typescript
  const repoName = $derived(pr.repo_name ?? "");
```

- [ ] **Step 2: Add badge to the template**

Replace the existing meta-left span (line 46):

```svelte
    <span class="meta-left">
      {#if showRepo}
        <span
          class="repo-badge"
          style="color: {repoColor(repoName)}; background: color-mix(in srgb, {repoColor(repoName)} 15%, transparent);"
        >{repoName}</span>
      {/if}
      #{pr.Number} · {pr.Author}
    </span>
```

- [ ] **Step 3: Add CSS for the repo badge**

Add inside the `<style>` block:

```css
  .repo-badge {
    font-size: 9px;
    font-weight: 600;
    padding: 1px 5px;
    border-radius: 8px;
    max-width: 80px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    vertical-align: middle;
    line-height: 1.4;
  }
```

- [ ] **Step 4: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors (PullList will need updating in Task 7 to pass the new prop, but `check` may report this as a type error — proceed to Task 6 first if so)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/components/sidebar/PullItem.svelte
git commit -m "feat: add optional repo pill badge to PullItem"
```

---

### Task 6: Add repo pill badge to IssueItem

Same pattern as PullItem.

**Files:**
- Modify: `frontend/src/lib/components/sidebar/IssueItem.svelte`

- [ ] **Step 1: Add prop and import**

In the `<script>` section, add the import after the existing imports:

```typescript
import { repoColor } from "../../utils/repo-color.js";
```

Update the Props interface and destructuring:

```typescript
  interface Props {
    issue: Issue;
    selected: boolean;
    showRepo: boolean;
    onclick: () => void;
  }

  const { issue, selected, showRepo, onclick }: Props = $props();
```

Add a derived for the repo name:

```typescript
  const repoName = $derived(issue.repo_name ?? "");
```

- [ ] **Step 2: Add badge to the template**

Replace the existing meta-left span (line 71):

```svelte
    <span class="meta-left">
      {#if showRepo}
        <span
          class="repo-badge"
          style="color: {repoColor(repoName)}; background: color-mix(in srgb, {repoColor(repoName)} 15%, transparent);"
        >{repoName}</span>
      {/if}
      #{issue.Number} · {issue.Author}
    </span>
```

- [ ] **Step 3: Add CSS for the repo badge**

Add inside the `<style>` block (same CSS as PullItem):

```css
  .repo-badge {
    font-size: 9px;
    font-weight: 600;
    padding: 1px 5px;
    border-radius: 8px;
    max-width: 80px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    vertical-align: middle;
    line-height: 1.4;
  }
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/components/sidebar/IssueItem.svelte
git commit -m "feat: add optional repo pill badge to IssueItem"
```

---

### Task 7: Update PullList — toggle control + conditional rendering

Add the segmented control and switch between grouped and flat rendering.

**Files:**
- Modify: `frontend/src/lib/components/sidebar/PullList.svelte`

- [ ] **Step 1: Add imports**

Add to the existing imports in the `<script>` block:

```typescript
  import { getGroupByRepo, setGroupByRepo } from "../../stores/grouping.svelte.js";
```

- [ ] **Step 2: Add the segmented control to the filter bar**

Replace the existing filter-bar div (around line 75-85) with:

```svelte
  <div class="filter-bar">
    <span class="count-badge">{getPulls().length} PRs</span>
    <div class="state-toggle">
      {#each ["open", "closed", "all"] as s (s)}
        <button
          class="state-btn"
          class:state-btn--active={getFilterState() === s}
          onclick={() => { setFilterState(s); void loadPulls(); }}
        >{s === "open" ? "Open" : s === "closed" ? "Closed" : "All"}</button>
      {/each}
    </div>
    <div class="group-toggle">
      <button
        class="group-btn"
        class:group-btn--active={getGroupByRepo()}
        onclick={() => setGroupByRepo(true)}
      >By Repo</button>
      <button
        class="group-btn"
        class:group-btn--active={!getGroupByRepo()}
        onclick={() => setGroupByRepo(false)}
      >All</button>
    </div>
  </div>
```

- [ ] **Step 3: Update the list rendering**

Replace the existing `{#each [...pullsByRepo().entries()]}` block (the `{:else}` branch that renders the PR list, around line 133-144) with:

```svelte
    {:else}
      {#if getGroupByRepo()}
        {#each [...pullsByRepo().entries()] as [repo, prs] (repo)}
          <div class="repo-group">
            <h3 class="repo-header">{repo}</h3>
            {#each prs as pr (pr.ID)}
              <PullItem
                {pr}
                showRepo={false}
                selected={isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
                onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              />
            {/each}
          </div>
        {/each}
      {:else}
        {#each getPulls() as pr (pr.ID)}
          <PullItem
            {pr}
            showRepo={true}
            selected={isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
            onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
          />
        {/each}
      {/if}
```

- [ ] **Step 4: Add CSS for the group toggle**

Add inside the `<style>` block:

```css
  .group-toggle {
    display: flex;
    gap: 2px;
    background: var(--bg-inset);
    border-radius: 6px;
    padding: 2px;
    margin-left: auto;
  }
  .group-btn {
    font-size: 11px;
    padding: 2px 8px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    white-space: nowrap;
  }
  .group-btn--active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }
```

- [ ] **Step 5: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/components/sidebar/PullList.svelte
git commit -m "feat: add grouping toggle and flat rendering to PullList"
```

---

### Task 8: Update IssueList — toggle control + conditional rendering

Same pattern as PullList.

**Files:**
- Modify: `frontend/src/lib/components/sidebar/IssueList.svelte`

- [ ] **Step 1: Add imports**

Add to the existing imports in the `<script>` block:

```typescript
  import { getGroupByRepo, setGroupByRepo } from "../../stores/grouping.svelte.js";
```

- [ ] **Step 2: Add the segmented control to the filter bar**

Replace the existing filter-bar div (around line 67-78) with:

```svelte
  <div class="filter-bar">
    <span class="count-badge">{getIssues().length} issues</span>
    <div class="state-toggle">
      {#each ["open", "closed", "all"] as s (s)}
        <button
          class="state-btn"
          class:state-btn--active={getIssueFilterState() === s}
          onclick={() => { setIssueFilterState(s); void loadIssues(); }}
        >{s === "open" ? "Open" : s === "closed" ? "Closed" : "All"}</button>
      {/each}
    </div>
    <div class="group-toggle">
      <button
        class="group-btn"
        class:group-btn--active={getGroupByRepo()}
        onclick={() => setGroupByRepo(true)}
      >By Repo</button>
      <button
        class="group-btn"
        class:group-btn--active={!getGroupByRepo()}
        onclick={() => setGroupByRepo(false)}
      >All</button>
    </div>
  </div>
```

- [ ] **Step 3: Update the list rendering**

Replace the existing `{#each [...issuesByRepo().entries()]}` block (the `{:else}` branch that renders the issue list, around line 131-143) with:

```svelte
    {:else}
      {#if getGroupByRepo()}
        {#each [...issuesByRepo().entries()] as [repo, repoIssues] (repo)}
          <div class="repo-group">
            <h3 class="repo-header">{repo}</h3>
            {#each repoIssues as issue (issue.ID)}
              <IssueItem
                {issue}
                showRepo={false}
                selected={isSelected(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
                onclick={() => handleSelect(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
              />
            {/each}
          </div>
        {/each}
      {:else}
        {#each getIssues() as issue (issue.ID)}
          <IssueItem
            {issue}
            showRepo={true}
            selected={isSelected(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
            onclick={() => handleSelect(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
          />
        {/each}
      {/if}
```

- [ ] **Step 4: Add CSS for the group toggle**

Add inside the `<style>` block (same CSS as PullList):

```css
  .group-toggle {
    display: flex;
    gap: 2px;
    background: var(--bg-inset);
    border-radius: 6px;
    padding: 2px;
    margin-left: auto;
  }
  .group-btn {
    font-size: 11px;
    padding: 2px 8px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    white-space: nowrap;
  }
  .group-btn--active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }
```

- [ ] **Step 5: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/components/sidebar/IssueList.svelte
git commit -m "feat: add grouping toggle and flat rendering to IssueList"
```

---

### Task 9: Update ActivityThreaded — conditional repo grouping + repo badge

When ungrouped, skip the repo-level grouping. Use composite key `(repo_owner, repo_name, item_type, item_number)` for item grouping to avoid cross-repo collisions. Add repo badge to item rows.

**Files:**
- Modify: `frontend/src/lib/components/ActivityThreaded.svelte`

- [ ] **Step 1: Add imports and prop**

Add imports and update the Props interface:

```typescript
  import { getGroupByRepo } from "../stores/grouping.svelte.js";
  import { repoColor } from "../utils/repo-color.js";

  interface Props {
    items: ActivityItem[];
    onSelectItem: ((item: ActivityItem) => void) | undefined;
  }
```

- [ ] **Step 2: Replace the grouped derived computation**

Replace the entire `grouped` derived block (lines 85-149) with a new version that supports both modes. The key change: use a composite key `${repo_owner}/${repo_name}:${item_type}:${item_number}` for item grouping to prevent cross-repo collisions.

```typescript
  const grouped = $derived.by(() => {
    const byRepo = getGroupByRepo();

    // Phase 1: group events by item, using a composite key that
    // includes repo to prevent cross-repo collisions.
    const itemMap = new Map<string, ActivityItem[]>();

    for (const item of items) {
      const itemKey = `${item.repo_owner}/${item.repo_name}:${item.item_type}:${item.item_number}`;

      let events = itemMap.get(itemKey);
      if (!events) {
        events = [];
        itemMap.set(itemKey, events);
      }
      events.push(item);
    }

    // Phase 2: build ItemGroup array from the map.
    const allItemGroups: ItemGroup[] = [];

    for (const [, events] of itemMap) {
      events.sort((a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

      const first = events[0]!;
      allItemGroups.push({
        itemType: first.item_type,
        itemNumber: first.item_number,
        itemTitle: first.item_title,
        itemUrl: first.item_url,
        itemState: first.item_state,
        repoOwner: first.repo_owner,
        repoName: first.repo_name,
        latestTime: first.created_at,
        events,
        displayEvents: collapseCommitRuns(events),
      });
    }

    allItemGroups.sort((a, b) =>
      new Date(b.latestTime).getTime() - new Date(a.latestTime).getTime());

    if (!byRepo) {
      // Ungrouped: single synthetic RepoGroup containing all items.
      return [{
        repo: "",
        itemCount: allItemGroups.length,
        eventCount: allItemGroups.reduce((n, g) => n + g.events.length, 0),
        latestTime: allItemGroups[0]?.latestTime ?? "",
        items: allItemGroups,
      }];
    }

    // Grouped: bucket ItemGroups by repo.
    const repoMap = new Map<string, ItemGroup[]>();
    for (const ig of allItemGroups) {
      const repoKey = `${ig.repoOwner}/${ig.repoName}`;
      let bucket = repoMap.get(repoKey);
      if (!bucket) {
        bucket = [];
        repoMap.set(repoKey, bucket);
      }
      bucket.push(ig);
    }

    const repoGroups: RepoGroup[] = [];
    for (const [repo, itemGroups] of repoMap) {
      const allEvents = itemGroups.flatMap((g) => g.events);
      repoGroups.push({
        repo,
        itemCount: itemGroups.length,
        eventCount: allEvents.length,
        latestTime: itemGroups[0]?.latestTime ?? "",
        items: itemGroups,
      });
    }

    repoGroups.sort((a, b) =>
      new Date(b.latestTime).getTime() - new Date(a.latestTime).getTime());

    return repoGroups;
  });
```

- [ ] **Step 3: Update the template**

Replace the template (lines 193-242) with a version that conditionally shows repo headers and adds repo badges when ungrouped:

```svelte
<div class="threaded-view">
  {#each grouped as repoGroup (repoGroup.repo)}
    <div class="repo-section">
      {#if getGroupByRepo()}
        <div class="repo-header">
          <span class="repo-name">{repoGroup.repo}</span>
          <span class="repo-stats">{repoGroup.itemCount} items, {repoGroup.eventCount} events</span>
        </div>
      {/if}

      {#each repoGroup.items as itemGroup (`${itemGroup.repoOwner}/${itemGroup.repoName}:${itemGroup.itemType}:${itemGroup.itemNumber}`)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="item-row" onclick={() => handleItemClick(itemGroup)}>
          <span class="item-badge" class:badge-pr={itemGroup.itemType === "pr"} class:badge-issue={itemGroup.itemType === "issue"}>
            {itemGroup.itemType === "pr" ? "PR" : "Issue"}
          </span>
          {#if !getGroupByRepo()}
            <span
              class="repo-tag"
              style="color: {repoColor(itemGroup.repoName)}; background: color-mix(in srgb, {repoColor(itemGroup.repoName)} 15%, transparent);"
            >{itemGroup.repoName}</span>
          {/if}
          {#if itemGroup.itemState === "merged"}
            <span class="state-tag state-merged">Merged</span>
          {:else if itemGroup.itemState === "closed"}
            <span class="state-tag state-closed">Closed</span>
          {/if}
          <span class="item-ref">#{itemGroup.itemNumber}</span>
          <span class="item-title">{itemGroup.itemTitle}</span>
          <span class="item-time">{relativeTime(itemGroup.latestTime)}</span>
        </div>

        {#each itemGroup.displayEvents as row (row.id)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          {#if isCollapsed(row)}
            <div class="event-row collapsed-event" onclick={() => handleEventClick(row.representative)}>
              <span class="event-type evt-commit">{row.count} commits</span>
              <span class="event-author">{row.author}</span>
              <span class="event-time">{relativeTime(row.earliest)} - {relativeTime(row.latest)}</span>
            </div>
          {:else}
            <div class="event-row" onclick={() => handleEventClick(row)}>
              <span class="event-type {eventClass(row.activity_type)}">{eventLabel(row.activity_type)}</span>
              <span class="event-author">{row.author}</span>
              <span class="event-time">{relativeTime(row.created_at)}</span>
            </div>
          {/if}
        {/each}
      {/each}
    </div>
  {/each}

  {#if grouped.length === 0}
    <div class="empty-state">No activity found</div>
  {/if}
</div>
```

- [ ] **Step 4: Add CSS for the repo tag**

Add inside the `<style>` block:

```css
  .repo-tag {
    font-size: 9px;
    font-weight: 600;
    padding: 1px 4px;
    border-radius: 3px;
    flex-shrink: 0;
    max-width: 80px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
```

- [ ] **Step 5: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/components/ActivityThreaded.svelte
git commit -m "feat: add conditional repo grouping to ActivityThreaded"
```

---

### Task 10: Update ActivityFeed — toggle in controls bar

Add the "By Repo / All" segmented control to the activity controls bar, hidden when in flat view mode.

**Files:**
- Modify: `frontend/src/lib/components/ActivityFeed.svelte`

- [ ] **Step 1: Add imports**

Add to the existing imports in the `<script>` block:

```typescript
  import { getGroupByRepo, setGroupByRepo } from "../stores/grouping.svelte.js";
```

- [ ] **Step 2: Add the segmented control to the controls bar**

In the controls-bar template (around line 322-341), add the grouping toggle after the Flat/Threaded segmented control, inside the `.filter-group` div. It should only appear when the view mode is "threaded":

Find the Flat/Threaded segmented control block:

```svelte
      <div class="segmented-control">
        <button class="seg-btn" class:active={getViewMode() === "flat"} onclick={() => handleViewModeChange("flat")}>Flat</button>
        <button class="seg-btn" class:active={getViewMode() === "threaded"} onclick={() => handleViewModeChange("threaded")}>Threaded</button>
      </div>
```

Add immediately after it:

```svelte
      {#if getViewMode() === "threaded"}
        <div class="segmented-control">
          <button class="seg-btn" class:active={getGroupByRepo()} onclick={() => setGroupByRepo(true)}>By Repo</button>
          <button class="seg-btn" class:active={!getGroupByRepo()} onclick={() => setGroupByRepo(false)}>All</button>
        </div>
      {/if}
```

- [ ] **Step 3: Verify the build compiles**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/components/ActivityFeed.svelte
git commit -m "feat: add grouping toggle to activity controls bar"
```

---

### Task 11: E2E tests

Add full-stack E2E tests covering the toggle, rendering, persistence, sync, and keyboard navigation.

The test server seeds data with two repos: `acme/widgets` and `acme/tools`. Open PRs: widgets#1, #2, #6, #7, tools#1. Open issues: widgets#10, #11, #13, tools#5.

**Files:**
- Create: `frontend/tests/e2e-full/grouping-toggle.spec.ts`

- [ ] **Step 1: Write the E2E test file**

```typescript
// frontend/tests/e2e-full/grouping-toggle.spec.ts
import { expect, test, type Page } from "@playwright/test";

// Seed data repos: acme/widgets (most items) and acme/tools (fewer items).
// Open PRs (5): widgets#1, #2, #6, #7, tools#1
// Open issues (4): widgets#10, #11, #13, tools#5

async function waitForPullList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

async function waitForIssueList(page: Page): Promise<void> {
  await page.locator(".issue-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("grouping toggle", () => {
  test.beforeEach(async ({ page }) => {
    // Clear localStorage to start with default (By Repo).
    await page.goto("/pulls");
    await page.evaluate(() => localStorage.removeItem("middleman:groupByRepo"));
    await page.reload();
    await waitForPullList(page);
  });

  test("PR list defaults to grouped with repo headers", async ({ page }) => {
    await expect(page.locator(".repo-header").first()).toBeVisible();
    // No repo badges visible in grouped mode.
    await expect(page.locator(".repo-badge")).toHaveCount(0);
  });

  test("PR list ungrouped shows repo badges and no headers", async ({ page }) => {
    // Click "All" in group toggle.
    await page.locator(".group-btn", { hasText: "All" }).click();

    // Repo headers should disappear.
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Repo badges should appear on each item.
    const badges = page.locator(".repo-badge");
    await expect(badges.first()).toBeVisible();

    // Should have a badge for each PR.
    const items = page.locator(".pull-item");
    const itemCount = await items.count();
    await expect(badges).toHaveCount(itemCount);
  });

  test("toggle persists across page reload", async ({ page }) => {
    // Switch to ungrouped.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Reload the page.
    await page.reload();
    await waitForPullList(page);

    // Should still be ungrouped.
    await expect(page.locator(".repo-header")).toHaveCount(0);
    await expect(page.locator(".repo-badge").first()).toBeVisible();
  });

  test("toggle syncs from PRs to issues", async ({ page }) => {
    // Switch to ungrouped in PR list.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Navigate to issues.
    await page.goto("/issues");
    await waitForIssueList(page);

    // Issues should also be ungrouped.
    await expect(page.locator(".repo-header")).toHaveCount(0);
    await expect(page.locator(".repo-badge").first()).toBeVisible();
  });

  test("toggle syncs to activity threaded view", async ({ page }) => {
    // Switch to ungrouped in PR list.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Navigate to activity.
    await page.goto("/");

    // Switch to threaded mode.
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();

    // Wait for threaded view to render.
    await page.locator(".threaded-view .item-row").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Repo headers should not be visible (ungrouped).
    await expect(page.locator(".threaded-view .repo-header")).toHaveCount(0);

    // Repo tags should appear on item rows.
    await expect(page.locator(".repo-tag").first()).toBeVisible();
  });

  test("activity threaded ungrouped keeps cross-repo items separate", async ({ page }) => {
    // Seed data has both widgets#1 and tools#1 as PRs.
    // In ungrouped threaded mode, they must remain separate threads.
    await page.goto("/");

    // Switch to threaded + ungrouped.
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();
    await page.locator(".threaded-view .item-row").first()
      .waitFor({ state: "visible", timeout: 10_000 });
    await page.locator(".seg-btn", { hasText: "All" }).click();

    // Wait for repo tags to appear (ungrouped).
    await page.locator(".repo-tag").first()
      .waitFor({ state: "visible", timeout: 5_000 });

    // Find all item rows that show #1.
    const refOnes = page.locator(".item-row .item-ref", { hasText: "#1" });
    // There should be at least 2 (one for widgets, one for tools).
    const count = await refOnes.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });

  test("activity toggle hidden in flat mode, visible in threaded", async ({ page }) => {
    await page.goto("/");

    // In flat mode (default), the By Repo / All toggle should not exist.
    // The flat/threaded control is visible; check there's no "By Repo" button.
    await page.locator(".seg-btn", { hasText: "Flat" })
      .waitFor({ state: "visible", timeout: 10_000 });
    await expect(page.locator(".seg-btn", { hasText: "By Repo" }))
      .toHaveCount(0);

    // Switch to threaded mode.
    await page.locator(".seg-btn", { hasText: "Threaded" }).click();
    await page.locator(".threaded-view").waitFor({ state: "visible", timeout: 10_000 });

    // Now By Repo / All toggle should be visible.
    await expect(page.locator(".seg-btn", { hasText: "By Repo" }))
      .toBeVisible();
  });

  test("j/k navigation follows flat order in ungrouped mode", async ({ page }) => {
    // Switch to ungrouped.
    await page.locator(".group-btn", { hasText: "All" }).click();
    await expect(page.locator(".repo-header")).toHaveCount(0, { timeout: 5_000 });

    // Press j to select first item.
    await page.keyboard.press("j");
    await expect(page.locator(".pull-item.selected")).toHaveCount(1);

    // Get the first selected PR number.
    const first = await page.locator(".pull-item.selected .meta-left").textContent();

    // Press j again to move to second.
    await page.keyboard.press("j");
    const second = await page.locator(".pull-item.selected .meta-left").textContent();

    // They should be different items.
    expect(first).not.toEqual(second);
  });
});
```

- [ ] **Step 2: Run the tests**

Run: `cd frontend && bun run test:e2e -- grouping-toggle`
Expected: all tests pass

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/grouping-toggle.spec.ts
git commit -m "test: add E2E tests for grouping toggle"
```

---

### Task 12: Final verification

- [ ] **Step 1: Run full type check**

Run: `cd frontend && bun run check`
Expected: no errors

- [ ] **Step 2: Run all E2E tests**

Run: `cd frontend && bun run test:e2e`
Expected: all tests pass, including existing tests (no regressions)

- [ ] **Step 3: Build the full project**

Run: `make build`
Expected: clean build with no errors
