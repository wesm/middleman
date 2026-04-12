# Inline Diff in PR Drawer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users view a PR's diff inline in the drawer when opening a PR from the activity or kanban view, and give the drawer full width so the diff experience is uncompromised.

**Architecture:** Add a local tab bar (`Conversation` / `Files changed`) inside `PullDetail.svelte`. When the files tab is active, render `DiffView` inline. Make drawers full width (100%). `IssueDetail` gets matching internal-scroll + centering CSS so shared `DetailDrawer` keeps working for issues. `PRListView` passes `hideTabs={true}` to avoid double tab bars against its router-driven external tabs.

**Tech Stack:** Svelte 5 (runes), TypeScript, Playwright (`tests/e2e-full/` — managed backend + real SQLite), existing `diffStore`.

**Spec reference:** `docs/superpowers/specs/2026-04-12-activity-pr-drawer-diff-design.md`

---

## File Map

**Modify:**
- `packages/ui/src/components/detail/PullDetail.svelte` — add `hideTabs` prop, `activeTab` state, tab bar, restructure layout to `.pull-detail-wrap` → (tabs | DiffView | `.pull-detail`), update CSS.
- `packages/ui/src/components/detail/IssueDetail.svelte` — add internal-scroll + centering CSS to `.issue-detail`.
- `packages/ui/src/components/DetailDrawer.svelte` — width 100%, drawer-body flex, remove backdrop click handler.
- `packages/ui/src/components/kanban/KanbanBoard.svelte` — drawer width 100%, drawer-body flex, remove overlay element and its handler.
- `packages/ui/src/views/PRListView.svelte` — pass `hideTabs={true}` to `<PullDetail>`.

**Create:**
- `frontend/tests/e2e-full/activity-drawer.spec.ts` — drawer e2e tests (activity PR diff tab, activity issue scroll regression, kanban PR diff tab, PR-list single-tab-bar guard).

---

## Execution Notes

- Phase 1 writes failing e2e tests up front (TDD). Tests 1 and 4 pass today, tests 2 and 3 fail until Phase 2 lands the implementation.
- Phase 2 tasks are ordered so each commit leaves the tree buildable, even if some e2e tests stay red until later tasks.
- Commit after every task. Never amend. Never skip hooks (per project CLAUDE.md).
- Use `bun` for all frontend ops. Never use `npm`.
- Run only targeted tests during a task; full-suite verification happens in the final task.
- **E2E build step:** `playwright-e2e.config.ts` runs the Go `cmd/e2e-server`, which serves the frontend embedded in `internal/web/dist/`. Any frontend change requires `make frontend` (which runs `bun run build` and copies into the embed dir) before running playwright, otherwise tests run against stale bundles. Each implementation task's test step includes this rebuild.

---

## Phase 1: Write Failing E2E Tests (TDD)

**Phase 1 precondition:** Run `make frontend` once before starting Phase 1. This guarantees the embedded bundle in `internal/web/dist/` reflects the current (pre-change) source. Tests that describe "today's behavior" will then give accurate pass/fail signals.

```bash
make frontend
```

### Task 1: Create e2e test file with activity PR diff test

**Files:**
- Create: `frontend/tests/e2e-full/activity-drawer.spec.ts`

**Context:** The e2e-full suite runs against a managed backend with seeded fixtures (`acme/widgets` repo, PRs including #1). The existing `frontend/tests/e2e-full/diff-view.spec.ts` shows the pattern for mocking diff API responses via `page.route()` — reuse that pattern.

The activity feed is seeded via `/` and uses `.activity-row` selectors (see `frontend/tests/e2e-full/activity-filters.spec.ts`).

- [ ] **Step 1: Create test file with PR diff tab switch test**

Create `frontend/tests/e2e-full/activity-drawer.spec.ts`:

```ts
import { expect, test, type Page } from "@playwright/test";
import type { DiffResult, FilesResult } from "@middleman/ui/api/types";

// Minimal diff fixture: one modified file.
const tinyDiff: DiffResult = {
  stale: false,
  whitespace_only_count: 0,
  files: [
    {
      path: "src/handler.go",
      old_path: "src/handler.go",
      status: "modified",
      is_binary: false,
      is_whitespace_only: false,
      additions: 2,
      deletions: 1,
      hunks: [
        {
          old_start: 1,
          old_count: 3,
          new_start: 1,
          new_count: 4,
          lines: [
            { type: "context", content: "package main", old_num: 1, new_num: 1 },
            { type: "delete", content: "// old", old_num: 2 },
            { type: "add", content: "// new", new_num: 2 },
            { type: "add", content: "// added", new_num: 3 },
            { type: "context", content: "", old_num: 3, new_num: 4 },
          ],
        },
      ],
    },
  ],
};

function filesFromDiff(fixture: DiffResult): FilesResult {
  return {
    stale: fixture.stale,
    files: fixture.files.map((f) => ({
      ...f,
      additions: 0,
      deletions: 0,
      hunks: [],
    })),
  };
}

// Broad wildcard mock: any PR in any repo returns the same tiny diff.
// The activity feed test clicks "the first PR row", which could be any
// PR from the seeded fixtures; a wildcard mock keeps the test
// deterministic regardless of which PR is clicked.
async function mockDiffForAllPRs(
  page: Page, fixture: DiffResult,
): Promise<void> {
  await page.route("**/api/v1/repos/*/*/pulls/*/files", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(filesFromDiff(fixture)),
    });
  });
  await page.route("**/api/v1/repos/*/*/pulls/*/diff*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(fixture),
    });
  });
}

async function waitForActivityTable(page: Page): Promise<void> {
  await page.locator(".activity-table tbody .activity-row").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

test.describe("activity drawer", () => {
  test("PR drawer shows diff when switching to Files tab", async ({ page }) => {
    // Route-level mocks must be installed before navigation so the
    // diff store never sees a real backend response.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/");
    await waitForActivityTable(page);

    // Click the first PR activity row. The seeded activity feed contains
    // both PRs and issues; pick the first row tagged "PR". The wildcard
    // mock covers whichever PR this turns out to be.
    const prRow = page
      .locator(".activity-row")
      .filter({ has: page.locator(".badge", { hasText: "PR" }) })
      .first();
    await prRow.click();

    // Drawer opens with the conversation tab by default.
    const drawer = page.locator(".drawer-panel");
    await expect(drawer).toBeVisible();

    // Click the "Files changed" tab inside the drawer.
    await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

    // Diff is rendered inside the drawer.
    await expect(drawer.locator(".diff-view")).toBeVisible();
    await expect(drawer.locator(".diff-toolbar")).toBeVisible();
    await expect(drawer.locator(".diff-file")).toHaveCount(1);

    // Switching back to Conversation unmounts the diff.
    await drawer.locator(".detail-tab", { hasText: "Conversation" }).click();
    await expect(drawer.locator(".diff-view")).toHaveCount(0);

    // Escape closes the drawer.
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
  });
});
```

- [ ] **Step 2: Run the test — expect failure**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "PR drawer shows diff"
```

Expected: FAIL — `.detail-tab` locator times out because `PullDetail` currently has no tab bar. The drawer opens (with the old `.files-changed-btn`), but there is no tab to click.

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/activity-drawer.spec.ts
git commit -m "$(cat <<'EOF'
test: add failing e2e for activity drawer PR diff tab

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Add kanban drawer PR diff test

**Files:**
- Modify: `frontend/tests/e2e-full/activity-drawer.spec.ts`

**Context:** Kanban board is at `/pulls/board`. Cards render as `.kanban-card`. Kanban opens its own inline drawer (separate from `DetailDrawer`); its panel selector is `.drawer` (not `.drawer-panel`).

- [ ] **Step 1: Add kanban drawer test to the same file**

Append inside the existing `test.describe("activity drawer", ...)` block (or add a second `describe`):

```ts
test("kanban drawer shows diff when switching to Files tab", async ({ page }) => {
  await mockDiffForAllPRs(page, tinyDiff);

  await page.goto("/pulls/board");
  await page.locator(".kanban-card").first()
    .waitFor({ state: "visible", timeout: 10_000 });

  // Click the first kanban card in any column. The wildcard diff mock
  // covers whichever PR number this card represents.
  await page.locator(".kanban-card").first().click();

  // Kanban drawer (distinct from DetailDrawer) uses .drawer as its panel.
  const drawer = page.locator(".kanban-wrap .drawer");
  await expect(drawer).toBeVisible();

  await drawer.locator(".detail-tab", { hasText: "Files changed" }).click();

  await expect(drawer.locator(".diff-view")).toBeVisible();
  await expect(drawer.locator(".diff-file")).toHaveCount(1);

  await page.keyboard.press("Escape");
  await expect(drawer).toHaveCount(0);
});
```

- [ ] **Step 2: Run the test — expect failure**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "kanban drawer shows diff"
```

Expected: FAIL — no `.detail-tab` inside the kanban drawer (tabs don't exist yet).

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/activity-drawer.spec.ts
git commit -m "$(cat <<'EOF'
test: add failing e2e for kanban drawer PR diff tab

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Add issue drawer scroll regression test

**Files:**
- Modify: `frontend/tests/e2e-full/activity-drawer.spec.ts`

**Context:** This test passes today (drawer-body scrolls) and must continue to pass after Phase 2 moves scroll ownership into `IssueDetail`. It is a regression guard, not a failing-first TDD case.

- [ ] **Step 1: Add issue scroll test**

Append:

```ts
test("issue drawer scrolls internally to bottom of content", async ({ page }) => {
  await page.goto("/");
  await waitForActivityTable(page);

  // Pick the first issue activity row.
  const issueRow = page
    .locator(".activity-row")
    .filter({ has: page.locator(".badge", { hasText: "Issue" }) })
    .first();
  await issueRow.click();

  const drawer = page.locator(".drawer-panel");
  await expect(drawer).toBeVisible();

  // The issue-detail element exists inside the drawer.
  const issueDetail = drawer.locator(".issue-detail");
  await expect(issueDetail).toBeVisible();

  // Force a scroll to the bottom. If scroll ownership is broken, this
  // either no-ops or scrolls the wrong container.
  await issueDetail.evaluate((el) => {
    // Walk up to find the scroll container (could be the element itself
    // or an ancestor).
    let target: HTMLElement | null = el;
    while (target) {
      const style = getComputedStyle(target);
      if (style.overflowY === "auto" || style.overflowY === "scroll") {
        target.scrollTop = target.scrollHeight;
        return;
      }
      target = target.parentElement;
    }
  });

  // The drawer itself should still be visible after the scroll action.
  await expect(drawer).toBeVisible();
});
```

- [ ] **Step 2: Run the test — expect pass**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "issue drawer scrolls"
```

Expected: PASS (today's behavior).

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/activity-drawer.spec.ts
git commit -m "$(cat <<'EOF'
test: add issue drawer scroll regression guard

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Add PR-list single-tab-bar guard test

**Files:**
- Modify: `frontend/tests/e2e-full/activity-drawer.spec.ts` (or new `pr-list-tabs.spec.ts` — keep in the same file for cohesion since all four tests share the drawer/tabs theme).

**Context:** This test passes today (PRListView has exactly one tab bar). After Task 6 adds tabs to PullDetail, this test would fail if hideTabs were not respected. Task 7 restores the single-tab-bar state.

- [ ] **Step 1: Add PR-list single-tab-bar test**

Append:

```ts
test.describe("PR list tabs", () => {
  test("only one Conversation/Files tab bar is present", async ({ page }) => {
    // Mock the diff so navigating to /files does not depend on real data.
    await mockDiffForAllPRs(page, tinyDiff);

    await page.goto("/pulls/acme/widgets/1");

    // Wait for the PR detail area to render.
    await page.locator(".detail-tabs").first()
      .waitFor({ state: "visible", timeout: 10_000 });

    // Assert exactly one "Conversation" button and one "Files changed"
    // button on the PR list page. If PullDetail ever stops respecting
    // hideTabs in this context, both counts become 2.
    await expect(
      page.getByRole("button", { name: "Conversation" }),
    ).toHaveCount(1);
    await expect(
      page.getByRole("button", { name: /Files changed/ }),
    ).toHaveCount(1);

    // Same guard for the files route.
    await page.goto("/pulls/acme/widgets/1/files");
    await page.locator(".diff-view").waitFor({ state: "visible", timeout: 10_000 });
    await expect(
      page.getByRole("button", { name: "Conversation" }),
    ).toHaveCount(1);
    await expect(
      page.getByRole("button", { name: /Files changed/ }),
    ).toHaveCount(1);
  });
});
```

- [ ] **Step 2: Run the test — expect pass**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "only one"
```

Expected: PASS (today's behavior — only one external tab bar).

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/activity-drawer.spec.ts
git commit -m "$(cat <<'EOF'
test: add PR list single-tab-bar regression guard

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 2: Implementation

### Task 5: IssueDetail internal scroll + centering CSS

**Files:**
- Modify: `packages/ui/src/components/detail/IssueDetail.svelte` (the `.issue-detail` block in the `<style>` section, around line 259).

**Context:** `box-sizing: border-box` is already globally applied in `frontend/src/app.css`, so no need to repeat it.

At this point, Task 6 (DetailDrawer drawer-body change) has not happened yet. `flex: 1` on `.issue-detail` without a flex parent is ignored, so this task is a no-op visually until Task 7 lands. The change is harmless in isolation and prepares the component for the drawer-body flip.

- [ ] **Step 1: Update `.issue-detail` CSS**

Find the `.issue-detail` block in `packages/ui/src/components/detail/IssueDetail.svelte`:

```css
.issue-detail {
  padding: 20px 24px;
  max-width: 800px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
```

Replace with:

```css
.issue-detail {
  padding: 20px 24px;
  max-width: 800px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  width: 100%;
  margin-inline: auto;
}
```

- [ ] **Step 2: Build the UI package to catch type/syntax errors**

```bash
cd packages/ui && bun run build
```

Expected: success, no errors.

- [ ] **Step 3: Rebuild the frontend bundle**

```bash
make frontend
```

Expected: success. Required so the e2e server serves the updated `IssueDetail.svelte`.

- [ ] **Step 4: Run the issue scroll regression test to confirm nothing broke**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "issue drawer scrolls"
```

Expected: PASS (still passing because drawer-body still owns scroll — Task 6 will shift it).

- [ ] **Step 5: Commit**

```bash
git add packages/ui/src/components/detail/IssueDetail.svelte
git commit -m "$(cat <<'EOF'
feat(ui): give IssueDetail internal scroll and centering

Prepares IssueDetail for the drawer-body flex change by making it
own its own scroll and centering within its container. No visual
change until DetailDrawer stops owning scroll.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: PullDetail tabs + DiffView integration + DetailDrawer width/scroll/backdrop (atomic)

**Files:**
- Modify: `packages/ui/src/components/detail/PullDetail.svelte`
- Modify: `packages/ui/src/components/DetailDrawer.svelte`

**Context:** These two files are coupled. PullDetail's new `.pull-detail-wrap` uses `flex: 1; min-height: 0` which requires DetailDrawer's `.drawer-body` to be a flex column. Landing only PullDetail would leave the conversation content sized incorrectly inside a non-flex drawer-body. Land both in one commit.

The task has two parts: (a) PullDetail structural rewrite, (b) DetailDrawer CSS + backdrop.

- [ ] **Step 1: Import DiffView in PullDetail**

Add to the imports block at the top of `PullDetail.svelte` (after existing imports):

```ts
import DiffView from "../diff/DiffView.svelte";
```

- [ ] **Step 2: Add `hideTabs` prop and `activeTab` state in PullDetail**

In the Props interface:

```ts
interface Props {
  owner: string;
  name: string;
  number: number;
  onPullsRefresh?: () => Promise<void>;
  hideTabs?: boolean;
}

const {
  owner, name, number, onPullsRefresh, hideTabs = false,
}: Props = $props();
```

Near the top of the `<script>` (after the existing state declarations like `copied`, `stateSubmitting`, etc., somewhere around the other `$state` calls), add:

```ts
let activeTab = $state<"conversation" | "files">("conversation");
```

- [ ] **Step 3: Restructure PullDetail markup**

Find the existing structure:

```svelte
{#if detailStore.isDetailLoading()}
  ...
{:else}
  {@const detail = detailStore.getDetail()}
  {#if detail !== null}
    {@const pr = detail.merge_request}
    <div class="pull-detail">
      <!-- header, meta, chips, CI, kanban, actions, body, files-changed-btn, comment, activity -->
    </div>
  {/if}
{/if}
```

Wrap the content inside `{#if detail !== null}` with `.pull-detail-wrap` and a tab bar:

```svelte
{#if detailStore.isDetailLoading()}
  <div class="state-center"><p class="state-msg">Loading…</p></div>
{:else if detailStore.getDetailError() !== null && detailStore.getDetail() === null}
  <div class="state-center"><p class="state-msg state-msg--error">Error: {detailStore.getDetailError()}</p></div>
{:else}
  {@const detail = detailStore.getDetail()}
  {#if detail !== null}
    {@const pr = detail.merge_request}
    <div class="pull-detail-wrap">
      {#if !hideTabs}
        <div class="detail-tabs">
          <button
            type="button"
            class="detail-tab"
            class:detail-tab--active={activeTab === "conversation"}
            onclick={() => { activeTab = "conversation"; }}
          >
            Conversation
          </button>
          <button
            type="button"
            class="detail-tab"
            class:detail-tab--active={activeTab === "files"}
            onclick={() => { activeTab = "files"; }}
          >
            Files changed
            {#if pr.Additions > 0}
              <span class="files-stat files-stat--add">+{pr.Additions}</span>
            {/if}
            {#if pr.Deletions > 0}
              <span class="files-stat files-stat--del">-{pr.Deletions}</span>
            {/if}
          </button>
        </div>
      {/if}
      {#if !hideTabs && activeTab === "files"}
        <DiffView {owner} {name} {number} />
      {:else}
        <div class="pull-detail">
          <!-- KEEP the existing conversation content exactly as it is:
               refresh-banner, detail-header, meta-row, chips-row, labels,
               CI expanded, kanban-row, merge warnings, diff sync warnings,
               actions-row blocks, MergeModal, PR body section, comment box,
               activity section. Do NOT touch any of those inner blocks. -->
        </div>
      {/if}
    </div>
  {/if}
{/if}
```

**Important:** Inside the `<div class="pull-detail">` block, leave all existing child content unchanged (detail header, meta row, chips, CI, kanban, actions, body, comment box, activity) EXCEPT for the `files-changed-btn` block, which is removed in the next step.

- [ ] **Step 4: Remove the old files-changed-btn from PullDetail**

Inside the conversation block (the `<div class="pull-detail">` branch), find and delete the entire `<!-- Files changed -->` button block (currently at approximately lines 533–548):

```svelte
<!-- Files changed -->
<button
  class="files-changed-btn"
  onclick={() => navigate(`/pulls/${owner}/${name}/${number}/files`)}
>
  <span class="files-changed-label">Files changed</span>
  <span class="files-changed-stats">
    {#if pr.Additions > 0}
      <span class="files-stat files-stat--add">+{pr.Additions}</span>
    {/if}
    {#if pr.Deletions > 0}
      <span class="files-stat files-stat--del">-{pr.Deletions}</span>
    {/if}
  </span>
  <span class="files-changed-arrow">&#8594;</span>
</button>
```

Delete this entire block. The files tab replaces it.

Also delete the corresponding CSS in the `<style>` section:

```css
.files-changed-btn { ... }
.files-changed-btn:hover { ... }
.files-changed-label { ... }
.files-changed-stats { ... }
.files-changed-arrow { ... }
.files-changed-btn:hover .files-changed-arrow { ... }
```

**Keep** the `.files-stat`, `.files-stat--add`, `.files-stat--del` rules — the new tab bar reuses them for the `+N/-N` inline stats.

- [ ] **Step 5: Update PullDetail CSS — add `.pull-detail-wrap` and update `.pull-detail`**

Find the existing `.pull-detail` block:

```css
.pull-detail {
  padding: 20px 24px;
  max-width: 800px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
```

Replace with:

```css
.pull-detail-wrap {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.pull-detail {
  padding: 20px 24px;
  max-width: 800px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  width: 100%;
  margin-inline: auto;
}
```

- [ ] **Step 6: Add `.detail-tabs` / `.detail-tab` CSS to PullDetail**

Copy the styles from `PRListView.svelte` lines 146–171 into PullDetail's `<style>` section (append near the bottom, before the closing tag):

```css
.detail-tabs {
  display: flex;
  gap: 0;
  border-bottom: 1px solid var(--border-default);
  background: var(--bg-surface);
  flex-shrink: 0;
}

.detail-tab {
  font-size: 12px;
  font-weight: 500;
  padding: 8px 16px;
  color: var(--text-secondary);
  border-bottom: 2px solid transparent;
  transition: color 0.1s, border-color 0.1s;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border-top: none;
  border-left: none;
  border-right: none;
  cursor: pointer;
  font-family: inherit;
}

.detail-tab:hover {
  color: var(--text-primary);
  background: var(--bg-surface-hover);
}

.detail-tab--active {
  color: var(--text-primary);
  border-bottom-color: var(--accent-blue);
}
```

- [ ] **Step 7: Update DetailDrawer — width, scroll, backdrop**

In `packages/ui/src/components/DetailDrawer.svelte`, remove the backdrop click handler. Change the `<script>` block:

Delete this function:

```ts
function handleBackdropClick(e: MouseEvent): void {
  if (e.target === e.currentTarget) {
    onClose();
  }
}
```

Change the markup from:

```svelte
<div class="drawer-backdrop" onclick={handleBackdropClick}>
  <aside class="drawer-panel">
```

to:

```svelte
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="drawer-backdrop">
  <aside class="drawer-panel">
```

(Keep the `svelte-ignore` directive only if the linter complains about the element having no interaction — Svelte 5 may or may not require it once the click handler is removed. Add it if the build fails.)

Update CSS. Find:

```css
.drawer-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 65%;
  min-width: 500px;
  background: var(--bg-surface);
  border-left: 1px solid var(--border-default);
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
```

Replace with:

```css
.drawer-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 100%;
  background: var(--bg-surface);
  border-left: 1px solid var(--border-default);
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
```

Find:

```css
.drawer-body {
  flex: 1;
  overflow-y: auto;
}
```

Replace with:

```css
.drawer-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
```

Delete the container-narrow/medium override entirely:

```css
:global(#app.container-narrow) .drawer-panel,
:global(#app.container-medium) .drawer-panel {
  width: 100%;
  min-width: 0;
}
```

- [ ] **Step 8: Remove the now-unused `handleBackdropClick` import or reference (if any)**

Double-check the `<script>` section of DetailDrawer — no references to the deleted function should remain.

- [ ] **Step 9: Build the UI package**

```bash
cd packages/ui && bun run build
```

Expected: success.

- [ ] **Step 10: Rebuild the embedded frontend**

```bash
make frontend
```

Expected: success. This runs `bun run build` in `frontend/` and copies the output to `internal/web/dist/` so the e2e server serves the new PullDetail and DetailDrawer.

- [ ] **Step 11: Run the activity drawer PR diff test**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "PR drawer shows diff"
```

Expected: PASS.

- [ ] **Step 12: Run the issue drawer scroll regression test**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "issue drawer scrolls"
```

Expected: PASS (IssueDetail now owns scroll inside a flex drawer-body).

- [ ] **Step 13: Run the PR list single-tab-bar test**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "only one"
```

Expected: FAIL — PullDetail now renders its own tab bar in PRListView, creating a duplicate. Task 7 fixes this.

- [ ] **Step 14: Commit**

```bash
git add \
  packages/ui/src/components/detail/PullDetail.svelte \
  packages/ui/src/components/DetailDrawer.svelte
git commit -m "$(cat <<'EOF'
feat(ui): inline diff tabs in PullDetail, full-width activity drawer

Adds Conversation / Files changed tabs inside PullDetail with inline
DiffView rendering. Widens DetailDrawer to 100% so the diff gets full
horizontal space. Scroll ownership moves from drawer-body into
.pull-detail / .diff-area / .issue-detail. Backdrop click-to-close is
removed since there is no exposed backdrop at full width; Escape and
the header close button remain.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: PRListView pass hideTabs

**Files:**
- Modify: `packages/ui/src/views/PRListView.svelte`

**Context:** PRListView keeps its router-driven external tab bar (for URL state). It renders PullDetail only for conversation mode. After Task 6, PullDetail has its own tabs — we suppress them here to avoid duplication.

- [ ] **Step 1: Add `hideTabs={true}` to the PullDetail invocation**

Find the existing block in `PRListView.svelte` (around lines 75–79):

```svelte
<PullDetail
  owner={selectedPR.owner}
  name={selectedPR.name}
  number={selectedPR.number}
/>
```

Change to:

```svelte
<PullDetail
  owner={selectedPR.owner}
  name={selectedPR.name}
  number={selectedPR.number}
  hideTabs={true}
/>
```

- [ ] **Step 2: Build the UI package**

```bash
cd packages/ui && bun run build
```

Expected: success.

- [ ] **Step 3: Rebuild the embedded frontend**

```bash
make frontend
```

Expected: success.

- [ ] **Step 4: Run the PR list single-tab-bar test**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "only one"
```

Expected: PASS (PullDetail's internal tabs suppressed; only PRListView's external tab bar renders).

- [ ] **Step 5: Commit**

```bash
git add packages/ui/src/views/PRListView.svelte
git commit -m "$(cat <<'EOF'
feat(ui): suppress PullDetail internal tabs in PRListView

PRListView has its own router-driven tab bar for URL state. Pass
hideTabs={true} so PullDetail's new internal tabs do not stack on
top of the external ones.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: KanbanBoard drawer width, scroll, overlay

**Files:**
- Modify: `packages/ui/src/components/kanban/KanbanBoard.svelte`

**Context:** Kanban has its own inline drawer (not shared with DetailDrawer). It needs the same width, scroll, and overlay treatment. After Task 6, PullDetail already has tabs, so kanban's drawer will show them automatically once the width allows the diff to be usable.

- [ ] **Step 1: Remove the overlay element**

Find the drawer block in KanbanBoard.svelte (around line 104):

```svelte
{#if drawerPR !== null}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="drawer-overlay" onclick={closeDrawer} onkeydown={() => {}}></div>
  <aside class="drawer">
    ...
```

Remove the overlay div. Replace with:

```svelte
{#if drawerPR !== null}
  <aside class="drawer">
    ...
```

- [ ] **Step 2: Update `.drawer` CSS**

Find (around lines 181–195):

```css
.drawer {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 65%;
  min-width: 500px;
  background: var(--bg-primary);
  border-left: 1px solid var(--border-default);
  box-shadow: var(--shadow-lg);
  z-index: 11;
  display: flex;
  flex-direction: column;
  animation: slide-in 0.15s ease-out;
}
```

Replace with:

```css
.drawer {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 100%;
  background: var(--bg-primary);
  border-left: 1px solid var(--border-default);
  box-shadow: var(--shadow-lg);
  z-index: 11;
  display: flex;
  flex-direction: column;
  animation: slide-in 0.15s ease-out;
}
```

- [ ] **Step 3: Update `.drawer-body` CSS**

Find (around lines 227–230):

```css
.drawer-body {
  flex: 1;
  overflow-y: auto;
}
```

Replace with:

```css
.drawer-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
```

- [ ] **Step 4: Delete the now-unused `.drawer-overlay` CSS and narrow-container override**

Find and remove:

```css
.drawer-overlay {
  position: absolute;
  inset: 0;
  background: var(--overlay-bg, rgba(0, 0, 0, 0.3));
  z-index: 10;
  animation: fade-in 0.15s ease-out;
}

@keyframes fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
```

Also remove the narrow-container override (around lines 232–236):

```css
:global(#app.container-narrow) .drawer,
:global(#app.container-medium) .drawer {
  width: 100%;
  min-width: 0;
}
```

- [ ] **Step 5: Build the UI package**

```bash
cd packages/ui && bun run build
```

Expected: success.

- [ ] **Step 6: Rebuild the embedded frontend**

```bash
make frontend
```

Expected: success.

- [ ] **Step 7: Run the kanban drawer diff test**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer -g "kanban drawer shows diff"
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add packages/ui/src/components/kanban/KanbanBoard.svelte
git commit -m "$(cat <<'EOF'
feat(ui): full-width kanban drawer with inline diff tabs

Widens the kanban board's inline drawer to 100% so the DiffView
(now reachable via PullDetail's Files tab) has full horizontal
space. Removes the visual overlay element since there is no
exposed area behind a full-width drawer.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Full test sweep and manual verification

**Files:** none

- [ ] **Step 1: Rebuild the embedded frontend one more time**

```bash
make frontend
```

Expected: success. Safety step in case any later task changed files without rebuilding.

- [ ] **Step 2: Run the full activity-drawer e2e file**

```bash
cd frontend && bun x playwright test --config playwright-e2e.config.ts activity-drawer
```

Expected: all tests in `activity-drawer.spec.ts` PASS (PR diff tab, kanban diff tab, issue scroll, PR-list single tab bar).

- [ ] **Step 3: Run the full e2e-full suite to check for regressions**

```bash
make test-e2e
```

Expected: all tests PASS. `make test-e2e` re-runs `make frontend` and then `bun run playwright test --config=playwright-e2e.config.ts --project=chromium`. Watch especially for `diff-view.spec.ts`, `pull-list.spec.ts`, and `activity-filters.spec.ts` — these touch related code paths.

- [ ] **Step 4: Run the package UI tests**

```bash
cd packages/ui && bun test
```

Expected: all tests PASS.

- [ ] **Step 5: Run `go test ./...` for backend regression**

```bash
go test ./...
```

Expected: all tests PASS. No backend changes in this plan, but the project convention runs the suite post-merge.

- [ ] **Step 6: Build the full binary and confirm embedded frontend builds**

```bash
make build
```

Expected: success. Produces the middleman binary.

- [ ] **Step 7: Manual smoke test checklist**

Run `make dev` in one shell and `make frontend-dev` in another. Open the dev URL. Verify:

- Activity view → click a PR row → drawer opens at full width → switch to Files changed → diff renders with a sticky toolbar → switch back to Conversation → escape closes drawer → activity scroll position preserved.
- Activity view → click an issue row → issue content scrolls to bottom inside the drawer.
- Kanban view → click a card → drawer opens at full width → switch to Files changed → diff renders.
- PR list view → select a PR → only one Conversation/Files tab bar is visible → Files tab still updates the URL to `/files`.
- Wide viewport → conversation content is centered (not hugging the left edge).
- Narrow viewport → drawer fits within the container, content still readable.

- [ ] **Step 8: If everything passes, there is nothing to commit in this task.** Otherwise, create follow-up commits for any regressions discovered.

---

## Done

All spec requirements implemented:

- PullDetail tabs + DiffView integration (Task 6).
- Full-width drawers in DetailDrawer and KanbanBoard (Tasks 6, 8).
- Scroll ownership shift into PullDetail/DiffView/IssueDetail (Tasks 5, 6).
- Centering rule on `.pull-detail` and `.issue-detail` (Tasks 5, 6).
- Outside-click dismissal removed (Tasks 6, 8).
- KanbanBoard overlay removed (Task 8).
- PRListView `hideTabs` pass-through (Task 7).
- E2E coverage for all four drawer scenarios (Tasks 1–4).
