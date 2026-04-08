# Collapsible Repo Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user click a repo header in the "By Repo" view of `PullList.svelte` and `IssueList.svelte` to collapse or expand that repo's items, with state persisted in localStorage and tracked independently per surface.

**Architecture:** A new `@middleman/ui` store (`collapsedRepos.svelte.ts`) holds two independent `$state Set<string>` instances — one per surface — serialized to separate localStorage keys. `PullList.svelte` and `IssueList.svelte` render their repo headers as `<button aria-expanded>` elements that call `collapsedRepos.toggle(...)` and wrap each repo's item loop in `{#if !collapsed}`.

**Tech Stack:** Svelte 5 (runes), TypeScript, Vite, Vitest + jsdom, `@testing-library/svelte`, Playwright (e2e), Bun (package manager).

**Spec:** `docs/superpowers/specs/2026-04-08-collapsible-repo-groups-design.md`

## Tooling

`bun`, `node`, and `playwright` are not on the default PATH in this nix-based environment. **Every** shell command that invokes `bun`, `node`, `npx`, or the playwright CLI must be wrapped in `nix shell`. Use this prefix for all test / build / lint / typecheck / playwright invocations:

```bash
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c '<command>'
```

Examples:

```bash
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c 'cd frontend && bun run test'
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c 'cd frontend && bun run check'
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c 'cd frontend && bun run lint'
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c 'cd frontend && bun run build'
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c 'cd frontend && bun run playwright test --config=playwright-e2e.config.ts'
```

If dependencies have never been installed in this worktree, run first:

```bash
nix shell nixpkgs#bun nixpkgs#nodejs_24 --command bash -c 'cd frontend && bun install --frozen-lockfile'
```

Git commands (`git add`, `git commit`, `git status`, `git log`, `git rev-parse`) do NOT need nix wrapping — they are on PATH.

---

## File Structure

**Create:**
- `packages/ui/src/stores/collapsedRepos.svelte.ts` — store factory + type
- `packages/ui/src/stores/collapsedRepos.test.ts` — Vitest unit tests for the store
- `frontend/tests/e2e-full/collapsible-repos.spec.ts` — Playwright e2e coverage

**Modify:**
- `frontend/vite.config.ts` — extend vitest test discovery to include `packages/ui`
- `packages/ui/package.json` — add `./stores/collapsedRepos` export
- `packages/ui/src/types.ts` — re-export type, import type, add to `StoreInstances`
- `packages/ui/src/index.ts` — re-export type, re-export factory
- `packages/ui/src/Provider.svelte` — import factory, instantiate, add to `StoreInstances` object
- `packages/ui/src/components/sidebar/PullList.svelte` — replace `<h3>` header with `<button>` + collapse wrapper, update CSS
- `packages/ui/src/components/sidebar/IssueList.svelte` — same treatment as `PullList.svelte`

---

## Task 1: Extend Vitest discovery to cover `packages/ui`

**Context:** `frontend/vite.config.ts` currently omits an explicit `test.include`, so Vitest defaults to searching only `frontend/**`. `packages/ui/src/components/diff/DiffFile.test.ts` already exists outside that root and the new store unit tests will live in `packages/ui/src/stores/`. A single config edit lets Vitest pick up both.

**Files:**
- Modify: `frontend/vite.config.ts`

- [ ] **Step 1: Read the current config**

Run: `cat frontend/vite.config.ts`

Confirm the current `test` block reads:

```ts
  test: {
    environment: "jsdom",
    exclude: ["tests/e2e/**", "tests/e2e-full/**", "node_modules/**"],
  },
```

- [ ] **Step 2: Add an explicit `include` pattern**

Edit `frontend/vite.config.ts`. Replace the `test` block shown above with:

```ts
  test: {
    environment: "jsdom",
    include: [
      "src/**/*.{test,spec}.?(c|m)[jt]s?(x)",
      "../packages/ui/src/**/*.{test,spec}.?(c|m)[jt]s?(x)",
    ],
    exclude: ["tests/e2e/**", "tests/e2e-full/**", "node_modules/**"],
  },
```

- [ ] **Step 3: Verify existing tests still run and discover `DiffFile.test.ts`**

Run: `cd frontend && bun run test`
Expected: Vitest reports the existing `frontend/src/**/*.test.ts` suites AND `packages/ui/src/components/diff/DiffFile.test.ts`. All tests pass.

- [ ] **Step 4: Commit**

```bash
git add frontend/vite.config.ts
git commit -m "test: include packages/ui tests in vitest discovery"
```

---

## Task 2: Create `collapsedRepos` store with TDD

**Context:** The store owns two independent `Set<string>` instances keyed as `"owner/name"`, persists each to its own localStorage key, and exposes a minimal `isCollapsed` / `toggle` API. Svelte 5 `$state` does not proxy plain `Set`s — mutations are invisible to the reactivity graph — so `toggle` must reassign the whole set. The unit tests are written first to lock the contract.

**Files:**
- Create: `packages/ui/src/stores/collapsedRepos.test.ts`
- Create: `packages/ui/src/stores/collapsedRepos.svelte.ts`

- [ ] **Step 1: Write the failing unit tests**

Create `packages/ui/src/stores/collapsedRepos.test.ts` with this exact content:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createCollapsedReposStore,
} from "./collapsedRepos.svelte.js";

const PULLS_KEY = "middleman:collapsedRepos:pulls";
const ISSUES_KEY = "middleman:collapsedRepos:issues";

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("createCollapsedReposStore — defaults", () => {
  it("reports every repo as expanded on a fresh store", () => {
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
    expect(store.isCollapsed("pulls", "acme/tools")).toBe(false);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(false);
    expect(store.isCollapsed("issues", "acme/tools")).toBe(false);
  });

  it("treats missing localStorage keys as empty sets", () => {
    expect(localStorage.getItem(PULLS_KEY)).toBeNull();
    expect(localStorage.getItem(ISSUES_KEY)).toBeNull();
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(false);
  });
});

describe("createCollapsedReposStore — toggle", () => {
  it("flips a repo's collapsed state on each call", () => {
    const store = createCollapsedReposStore();
    store.toggle("pulls", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
    store.toggle("pulls", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
  });

  it("keeps surfaces independent for the same repo key", () => {
    const store = createCollapsedReposStore();
    store.toggle("pulls", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(false);

    store.toggle("issues", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(true);

    store.toggle("pulls", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(true);
  });

  it("keeps repo keys within a surface independent", () => {
    const store = createCollapsedReposStore();
    store.toggle("pulls", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
    expect(store.isCollapsed("pulls", "acme/tools")).toBe(false);
  });
});

describe("createCollapsedReposStore — persistence", () => {
  it("reads pre-seeded pulls state from localStorage", () => {
    localStorage.setItem(PULLS_KEY, JSON.stringify(["acme/widgets"]));
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
    expect(store.isCollapsed("pulls", "acme/tools")).toBe(false);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(false);
  });

  it("reads pre-seeded issues state from localStorage", () => {
    localStorage.setItem(ISSUES_KEY, JSON.stringify(["acme/tools"]));
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("issues", "acme/tools")).toBe(true);
    expect(store.isCollapsed("pulls", "acme/tools")).toBe(false);
  });

  it("writes only to the surface's own storage key on toggle", () => {
    const store = createCollapsedReposStore();
    store.toggle("pulls", "acme/widgets");

    const pullsRaw = localStorage.getItem(PULLS_KEY);
    const issuesRaw = localStorage.getItem(ISSUES_KEY);
    expect(pullsRaw).not.toBeNull();
    expect(JSON.parse(pullsRaw!)).toEqual(["acme/widgets"]);
    expect(issuesRaw).toBeNull();
  });

  it("persists toggle across store instances via localStorage", () => {
    const first = createCollapsedReposStore();
    first.toggle("pulls", "acme/widgets");

    const second = createCollapsedReposStore();
    expect(second.isCollapsed("pulls", "acme/widgets")).toBe(true);
  });
});

describe("createCollapsedReposStore — error handling", () => {
  it("falls back to an empty set on malformed JSON", () => {
    localStorage.setItem(PULLS_KEY, "{not json");
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
    expect(store.isCollapsed("issues", "acme/widgets")).toBe(false);
  });

  it("falls back to an empty set on non-array JSON", () => {
    localStorage.setItem(PULLS_KEY, JSON.stringify({ bad: "shape" }));
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
  });

  it("keeps in-memory toggle working when setItem throws", () => {
    const spy = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(() => {
        throw new Error("QuotaExceededError");
      });
    const store = createCollapsedReposStore();
    expect(() => store.toggle("pulls", "acme/widgets")).not.toThrow();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
    store.toggle("pulls", "acme/widgets");
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
    spy.mockRestore();
  });

  it("keeps in-memory toggle working when getItem throws at construction", () => {
    const spy = vi
      .spyOn(Storage.prototype, "getItem")
      .mockImplementation(() => {
        throw new Error("SecurityError");
      });
    const store = createCollapsedReposStore();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(false);
    spy.mockRestore();
    expect(() => store.toggle("pulls", "acme/widgets")).not.toThrow();
    expect(store.isCollapsed("pulls", "acme/widgets")).toBe(true);
  });
});
```

- [ ] **Step 2: Run the new tests and verify they fail**

Run: `cd frontend && bun run test collapsedRepos`
Expected: FAIL with a module resolution error such as `Cannot find module './collapsedRepos.svelte.js'` or `Failed to resolve import`. No tests pass because the source file does not exist yet.

- [ ] **Step 3: Implement the store**

Create `packages/ui/src/stores/collapsedRepos.svelte.ts` with this exact content:

```ts
export type CollapseSurface = "pulls" | "issues";

const STORAGE_KEYS: Record<CollapseSurface, string> = {
  pulls: "middleman:collapsedRepos:pulls",
  issues: "middleman:collapsedRepos:issues",
};

function readFromStorage(surface: CollapseSurface): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS[surface]);
    if (raw === null) return new Set();
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((v): v is string => typeof v === "string"));
  } catch {
    // localStorage unavailable or corrupt JSON.
    return new Set();
  }
}

function writeToStorage(surface: CollapseSurface, value: Set<string>): void {
  try {
    localStorage.setItem(STORAGE_KEYS[surface], JSON.stringify([...value]));
  } catch {
    // localStorage unavailable (e.g., private browsing quota).
  }
}

export function createCollapsedReposStore() {
  let collapsedInPulls = $state<Set<string>>(readFromStorage("pulls"));
  let collapsedInIssues = $state<Set<string>>(readFromStorage("issues"));

  function isCollapsed(
    surface: CollapseSurface,
    repoKey: string,
  ): boolean {
    if (surface === "pulls") return collapsedInPulls.has(repoKey);
    return collapsedInIssues.has(repoKey);
  }

  function toggle(surface: CollapseSurface, repoKey: string): void {
    if (surface === "pulls") {
      const next = new Set(collapsedInPulls);
      if (next.has(repoKey)) next.delete(repoKey);
      else next.add(repoKey);
      collapsedInPulls = next;
      writeToStorage("pulls", next);
    } else {
      const next = new Set(collapsedInIssues);
      if (next.has(repoKey)) next.delete(repoKey);
      else next.add(repoKey);
      collapsedInIssues = next;
      writeToStorage("issues", next);
    }
  }

  return {
    isCollapsed,
    toggle,
  };
}

export type CollapsedReposStore = ReturnType<typeof createCollapsedReposStore>;
```

- [ ] **Step 4: Run the tests and verify they pass**

Run: `cd frontend && bun run test collapsedRepos`
Expected: PASS. Every `describe` block green, no failing assertions.

- [ ] **Step 5: Run the full frontend test suite**

Run: `cd frontend && bun run test`
Expected: PASS. Every suite — including `DiffFile.test.ts`, `embed-config.svelte.test.ts`, and the new `collapsedRepos.test.ts` — green.

- [ ] **Step 6: Commit**

```bash
git add packages/ui/src/stores/collapsedRepos.svelte.ts packages/ui/src/stores/collapsedRepos.test.ts
git commit -m "feat(ui): add collapsedRepos store with per-surface persistence"
```

---

## Task 3: Wire `collapsedRepos` into the `@middleman/ui` package surface

**Context:** The store only becomes reachable from components once it is registered in `StoreInstances`, re-exported from `index.ts`, wired into `Provider.svelte`, and declared in `package.json` exports. All four edits must land in a single commit or the intermediate state fails type-checking (`StoreInstances` requires the new field before `PullList`/`IssueList` can destructure it — but `Provider.svelte` must assign it at the same time).

**Files:**
- Modify: `packages/ui/src/types.ts`
- Modify: `packages/ui/src/index.ts`
- Modify: `packages/ui/src/Provider.svelte`
- Modify: `packages/ui/package.json`

- [ ] **Step 1: Add the type re-export and import in `types.ts`**

Edit `packages/ui/src/types.ts`. Replace this block (lines 85-86):

```ts
export type { GroupingStore } from "./stores/grouping.svelte.js";
export type { SettingsStore } from "./stores/settings.svelte.js";
```

with:

```ts
export type { GroupingStore } from "./stores/grouping.svelte.js";
export type { CollapsedReposStore } from "./stores/collapsedRepos.svelte.js";
export type { SettingsStore } from "./stores/settings.svelte.js";
```

Then replace this block (lines 94-95):

```ts
import type { GroupingStore } from "./stores/grouping.svelte.js";
import type { SettingsStore } from "./stores/settings.svelte.js";
```

with:

```ts
import type { GroupingStore } from "./stores/grouping.svelte.js";
import type { CollapsedReposStore } from "./stores/collapsedRepos.svelte.js";
import type { SettingsStore } from "./stores/settings.svelte.js";
```

- [ ] **Step 2: Add `collapsedRepos` to `StoreInstances` in `types.ts`**

In the same file, replace the `StoreInstances` interface (lines 97-106):

```ts
export interface StoreInstances {
  pulls: PullsStore;
  issues: IssuesStore;
  detail: DetailStore;
  activity: ActivityStore;
  sync: SyncStore;
  diff: DiffStore;
  grouping: GroupingStore;
  settings: SettingsStore;
}
```

with:

```ts
export interface StoreInstances {
  pulls: PullsStore;
  issues: IssuesStore;
  detail: DetailStore;
  activity: ActivityStore;
  sync: SyncStore;
  diff: DiffStore;
  grouping: GroupingStore;
  collapsedRepos: CollapsedReposStore;
  settings: SettingsStore;
}
```

- [ ] **Step 3: Add the type re-export in `index.ts`**

Edit `packages/ui/src/index.ts`. Replace this block (lines 1-23):

```ts
export type {
  MiddlemanClient,
  Action,
  ActionContext,
  ActionRegistry,
  NavigateEvent,
  NavigateCallback,
  MiddlemanEvent,
  EventCallback,
  PrepareRouteCallback,
  HostStateAccessors,
  StoreInstances,
  UIConfig,
  SidebarAccessors,
  PullsStore,
  IssuesStore,
  DetailStore,
  ActivityStore,
  SyncStore,
  DiffStore,
  GroupingStore,
  SettingsStore,
} from "./types.js";
```

with:

```ts
export type {
  MiddlemanClient,
  Action,
  ActionContext,
  ActionRegistry,
  NavigateEvent,
  NavigateCallback,
  MiddlemanEvent,
  EventCallback,
  PrepareRouteCallback,
  HostStateAccessors,
  StoreInstances,
  UIConfig,
  SidebarAccessors,
  PullsStore,
  IssuesStore,
  DetailStore,
  ActivityStore,
  SyncStore,
  DiffStore,
  GroupingStore,
  CollapsedReposStore,
  SettingsStore,
} from "./types.js";
```

- [ ] **Step 4: Add the factory re-export in `index.ts`**

In the same file, replace this block (lines 50-52):

```ts
export {
  createGroupingStore,
} from "./stores/grouping.svelte.js";
```

with:

```ts
export {
  createGroupingStore,
} from "./stores/grouping.svelte.js";
export {
  createCollapsedReposStore,
} from "./stores/collapsedRepos.svelte.js";
```

- [ ] **Step 5: Import the factory in `Provider.svelte`**

Edit `packages/ui/src/Provider.svelte`. Replace this block (lines 46-48):

```ts
  import {
    createGroupingStore,
  } from "./stores/grouping.svelte.js";
```

with:

```ts
  import {
    createGroupingStore,
  } from "./stores/grouping.svelte.js";
  import {
    createCollapsedReposStore,
  } from "./stores/collapsedRepos.svelte.js";
```

- [ ] **Step 6: Instantiate the store in `Provider.svelte`**

In the same file, replace line 99:

```ts
    const grouping = createGroupingStore();
```

with:

```ts
    const grouping = createGroupingStore();
    const collapsedRepos = createCollapsedReposStore();
```

- [ ] **Step 7: Add `collapsedRepos` to the returned `StoreInstances` object**

In the same file, replace the returned object (lines 159-168):

```ts
    const si: StoreInstances = {
      pulls: pullsStore,
      issues: issuesStore,
      detail: detailStore,
      activity: activityStore,
      sync: syncStore,
      diff: diffStore,
      grouping,
      settings: settingsStore,
    };
```

with:

```ts
    const si: StoreInstances = {
      pulls: pullsStore,
      issues: issuesStore,
      detail: detailStore,
      activity: activityStore,
      sync: syncStore,
      diff: diffStore,
      grouping,
      collapsedRepos,
      settings: settingsStore,
    };
```

- [ ] **Step 8: Register the package export in `package.json`**

Edit `packages/ui/package.json`. Replace this line:

```json
    "./stores/grouping": { "svelte": "./src/stores/grouping.svelte.ts", "default": "./src/stores/grouping.svelte.ts" },
```

with:

```json
    "./stores/grouping": { "svelte": "./src/stores/grouping.svelte.ts", "default": "./src/stores/grouping.svelte.ts" },
    "./stores/collapsedRepos": { "svelte": "./src/stores/collapsedRepos.svelte.ts", "default": "./src/stores/collapsedRepos.svelte.ts" },
```

- [ ] **Step 9: Typecheck the frontend (exercises the whole `@middleman/ui` surface)**

Run: `cd frontend && bun run check`
Expected: 0 errors, 0 warnings. `svelte-check --fail-on-warnings` passes.

- [ ] **Step 10: Commit**

```bash
git add packages/ui/src/types.ts packages/ui/src/index.ts packages/ui/src/Provider.svelte packages/ui/package.json
git commit -m "feat(ui): wire collapsedRepos store into Provider and exports"
```

---

## Task 4: Update `PullList.svelte` — markup and CSS

**Context:** The grouped branch currently renders `<h3 class="repo-header">{repo}</h3>` followed by an unconditional `{#each prs}`. Replace the `<h3>` with a `<button type="button" class="repo-header">` that owns the chevron, name, and count, then wrap the `{#each prs}` block in `{#if !collapsed}`. The CSS rule for `.repo-header` is merged in place (not duplicated) so the sticky/background/border-bottom properties are preserved and the new button styling lives alongside them. A `.repo-header[aria-expanded="false"]` rule suppresses the double border that would otherwise appear under a collapsed header.

**Files:**
- Modify: `packages/ui/src/components/sidebar/PullList.svelte`

- [ ] **Step 1: Add `collapsedRepos` to the destructured stores**

Edit `packages/ui/src/components/sidebar/PullList.svelte`. Replace line 6:

```svelte
  const { pulls, sync, diff, grouping, settings } = getStores();
```

with:

```svelte
  const { pulls, sync, diff, grouping, collapsedRepos, settings } = getStores();
```

- [ ] **Step 2: Replace the grouped-branch markup**

In the same file, replace the entire grouped branch (lines 199-239):

```svelte
      {#if grouping.getGroupByRepo()}
        {#each [...pulls.pullsByRepo().entries()] as [repo, prs] (repo)}
          <div class="repo-group">
            <h3 class="repo-header">{repo}</h3>
            {#each prs as pr (pr.ID)}
              {@const prSelected = isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              <PullItem
                {pr}
                showRepo={false}
                selected={prSelected}
                onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              />
              {#if prSelected && _getDetailTab() === "files"}
                <div class="diff-files">
                  {#if diff.isDiffLoading() && !diff.getDiff()}
                    <div class="diff-files-state diff-files-state--loading">Loading files</div>
                  {:else if diff.getDiff()}
                    {@const grouped = groupByDir(diff.getDiff()!.files)}
                    {#each grouped as group, gi (gi)}
                      {#if group.dir}
                        <div class="diff-dir-header">{group.dir}/</div>
                      {/if}
                      {#each group.files as f (f.path)}
                        <button
                          class="diff-file-row"
                          class:diff-file-row--active={diff.getActiveFile() === f.path}
                          class:diff-file-row--nested={!!group.dir}
                          onclick={() => diff.requestScrollToFile(f.path)}
                          title={f.path}
                        >
                          <span class="diff-file-status" style="color: {statusColor(f.status)}">{statusLetter(f.status)}</span>
                          <span class="diff-file-name" class:diff-file-name--deleted={f.status === "deleted"}>{filename(f.path)}</span>
                        </button>
                      {/each}
                    {/each}
                  {/if}
                </div>
              {/if}
            {/each}
          </div>
        {/each}
```

with:

```svelte
      {#if grouping.getGroupByRepo()}
        {#each [...pulls.pullsByRepo().entries()] as [repo, prs] (repo)}
          {@const collapsed = collapsedRepos.isCollapsed("pulls", repo)}
          <div class="repo-group">
            <button
              type="button"
              class="repo-header"
              aria-expanded={!collapsed}
              onclick={() => collapsedRepos.toggle("pulls", repo)}
            >
              <svg
                class="repo-header__chevron"
                class:repo-header__chevron--collapsed={collapsed}
                width="10" height="10" viewBox="0 0 10 10"
                fill="none" stroke="currentColor" stroke-width="1.5"
              >
                <polyline points="2,3 5,7 8,3" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <span class="repo-header__name">{repo}</span>
              <span class="repo-header__count">{prs.length}</span>
            </button>
            {#if !collapsed}
              {#each prs as pr (pr.ID)}
                {@const prSelected = isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
                <PullItem
                  {pr}
                  showRepo={false}
                  selected={prSelected}
                  onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
                />
                {#if prSelected && _getDetailTab() === "files"}
                  <div class="diff-files">
                    {#if diff.isDiffLoading() && !diff.getDiff()}
                      <div class="diff-files-state diff-files-state--loading">Loading files</div>
                    {:else if diff.getDiff()}
                      {@const grouped = groupByDir(diff.getDiff()!.files)}
                      {#each grouped as group, gi (gi)}
                        {#if group.dir}
                          <div class="diff-dir-header">{group.dir}/</div>
                        {/if}
                        {#each group.files as f (f.path)}
                          <button
                            class="diff-file-row"
                            class:diff-file-row--active={diff.getActiveFile() === f.path}
                            class:diff-file-row--nested={!!group.dir}
                            onclick={() => diff.requestScrollToFile(f.path)}
                            title={f.path}
                          >
                            <span class="diff-file-status" style="color: {statusColor(f.status)}">{statusLetter(f.status)}</span>
                            <span class="diff-file-name" class:diff-file-name--deleted={f.status === "deleted"}>{filename(f.path)}</span>
                          </button>
                        {/each}
                      {/each}
                    {/if}
                  </div>
                {/if}
              {/each}
            {/if}
          </div>
        {/each}
```

- [ ] **Step 3: Merge the `.repo-header` CSS rule**

In the same file, replace the existing `.repo-header` rule (lines 436-448):

```css
  .repo-header {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 6px 12px 4px;
    background: var(--bg-inset);
    border-bottom: 1px solid var(--border-muted);
    position: sticky;
    top: 0;
    z-index: 1;
  }
```

with:

```css
  .repo-header {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 6px 12px 4px;
    background: var(--bg-inset);
    border-bottom: 1px solid var(--border-muted);
    position: sticky;
    top: 0;
    z-index: 1;

    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    text-align: left;
    border-top: none;
    border-left: none;
    border-right: none;
    cursor: pointer;
    font-family: inherit;
  }

  .repo-header:hover {
    background: var(--bg-surface-hover);
  }

  .repo-header[aria-expanded="false"] {
    border-bottom: none;
  }

  .repo-header__chevron {
    color: var(--text-muted);
    transition: transform 120ms ease;
    flex-shrink: 0;
  }

  .repo-header__chevron--collapsed {
    transform: rotate(-90deg);
  }

  .repo-header__name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .repo-header__count {
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bun run check`
Expected: 0 errors, 0 warnings.

- [ ] **Step 5: Lint**

Run: `cd frontend && bun run lint`
Expected: no errors, no warnings.

- [ ] **Step 6: Build the frontend bundle**

Run: `cd frontend && bun run build`
Expected: Vite build succeeds with no errors. `dist/` is produced.

- [ ] **Step 7: Commit**

```bash
git add packages/ui/src/components/sidebar/PullList.svelte
git commit -m "feat(ui): make PullList repo headers collapsible"
```

---

## Task 5: Update `IssueList.svelte` — markup and CSS

**Context:** `IssueList.svelte` uses the same grouped-branch pattern as `PullList.svelte` but has no nested diff-files block, so the inner loop is simpler. The surface key is `"issues"`, the iterator is `issues.issuesByRepo()`, and the count is `repoIssues.length`. The CSS changes are identical to `PullList.svelte` — Svelte scoped styles keep the two blocks isolated so no cross-component leakage.

**Files:**
- Modify: `packages/ui/src/components/sidebar/IssueList.svelte`

- [ ] **Step 1: Add `collapsedRepos` to the destructured stores**

Edit `packages/ui/src/components/sidebar/IssueList.svelte`. Replace line 5:

```svelte
  const { issues, sync, grouping, settings } = getStores();
```

with:

```svelte
  const { issues, sync, grouping, collapsedRepos, settings } = getStores();
```

- [ ] **Step 2: Replace the grouped-branch markup**

In the same file, replace the grouped branch (lines 140-153):

```svelte
      {#if grouping.getGroupByRepo()}
        {#each [...issues.issuesByRepo().entries()] as [repo, repoIssues] (repo)}
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
```

with:

```svelte
      {#if grouping.getGroupByRepo()}
        {#each [...issues.issuesByRepo().entries()] as [repo, repoIssues] (repo)}
          {@const collapsed = collapsedRepos.isCollapsed("issues", repo)}
          <div class="repo-group">
            <button
              type="button"
              class="repo-header"
              aria-expanded={!collapsed}
              onclick={() => collapsedRepos.toggle("issues", repo)}
            >
              <svg
                class="repo-header__chevron"
                class:repo-header__chevron--collapsed={collapsed}
                width="10" height="10" viewBox="0 0 10 10"
                fill="none" stroke="currentColor" stroke-width="1.5"
              >
                <polyline points="2,3 5,7 8,3" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <span class="repo-header__name">{repo}</span>
              <span class="repo-header__count">{repoIssues.length}</span>
            </button>
            {#if !collapsed}
              {#each repoIssues as issue (issue.ID)}
                <IssueItem
                  {issue}
                  showRepo={false}
                  selected={isSelected(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
                  onclick={() => handleSelect(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
                />
              {/each}
            {/if}
          </div>
        {/each}
```

- [ ] **Step 3: Merge the `.repo-header` CSS rule**

In the same file, replace the existing `.repo-header` rule (lines 323-335):

```css
  .repo-header {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 6px 12px 4px;
    background: var(--bg-inset);
    border-bottom: 1px solid var(--border-muted);
    position: sticky;
    top: 0;
    z-index: 1;
  }
```

with:

```css
  .repo-header {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 6px 12px 4px;
    background: var(--bg-inset);
    border-bottom: 1px solid var(--border-muted);
    position: sticky;
    top: 0;
    z-index: 1;

    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    text-align: left;
    border-top: none;
    border-left: none;
    border-right: none;
    cursor: pointer;
    font-family: inherit;
  }

  .repo-header:hover {
    background: var(--bg-surface-hover);
  }

  .repo-header[aria-expanded="false"] {
    border-bottom: none;
  }

  .repo-header__chevron {
    color: var(--text-muted);
    transition: transform 120ms ease;
    flex-shrink: 0;
  }

  .repo-header__chevron--collapsed {
    transform: rotate(-90deg);
  }

  .repo-header__name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .repo-header__count {
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bun run check`
Expected: 0 errors, 0 warnings.

- [ ] **Step 5: Lint**

Run: `cd frontend && bun run lint`
Expected: no errors, no warnings.

- [ ] **Step 6: Build the frontend bundle**

Run: `cd frontend && bun run build`
Expected: Vite build succeeds with no errors.

- [ ] **Step 7: Commit**

```bash
git add packages/ui/src/components/sidebar/IssueList.svelte
git commit -m "feat(ui): make IssueList repo headers collapsible"
```

---

## Task 6: Playwright e2e coverage

**Context:** The e2e suite runs against a dedicated Go server (`cmd/e2e-server`) with deterministic seed data: `acme/widgets` has 4 open PRs (#1, #2, #6, #7) and 3 open issues (#10, #11, #13); `acme/tools` has 1 open PR (#1) and 1 open issue (#5). The `beforeEach` pattern mirrors `grouping-toggle.spec.ts` — you cannot call `localStorage` before the first `goto`, so the sequence is `goto → evaluate clear → reload → waitFor list`. Seven test cases enforce defaults, collapse/expand, count badge, keyboard activation, persistence across reload, independence across surfaces, and the parallel behavior on `/issues`.

**Files:**
- Create: `frontend/tests/e2e-full/collapsible-repos.spec.ts`

- [ ] **Step 1: Write the Playwright spec**

Create `frontend/tests/e2e-full/collapsible-repos.spec.ts` with this exact content:

```ts
import { expect, test, type Page } from "@playwright/test";

// Seed data repos: acme/widgets (most items) and acme/tools (fewer items).
// Open PRs (5): widgets#1, #2, #6, #7, tools#1   -> widgets 4, tools 1
// Open issues (4): widgets#10, #11, #13, tools#5 -> widgets 3, tools 1

async function waitForPullList(page: Page): Promise<void> {
  await page.locator(".pull-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

async function waitForIssueList(page: Page): Promise<void> {
  await page.locator(".issue-item").first()
    .waitFor({ state: "visible", timeout: 10_000 });
}

function widgetsHeader(page: Page) {
  return page.locator(".repo-header", { hasText: "acme/widgets" });
}

function toolsHeader(page: Page) {
  return page.locator(".repo-header", { hasText: "acme/tools" });
}

test.describe("collapsible repo groups", () => {
  test.beforeEach(async ({ page }) => {
    // Clear collapse state so every test starts expanded.
    // localStorage can only be touched after the first goto.
    await page.goto("/pulls");
    await page.evaluate(() => {
      localStorage.removeItem("middleman:collapsedRepos:pulls");
      localStorage.removeItem("middleman:collapsedRepos:issues");
    });
    await page.reload();
    await waitForPullList(page);
  });

  test("PR list — default expanded shows every PR and both headers", async ({ page }) => {
    await expect(widgetsHeader(page)).toBeVisible();
    await expect(toolsHeader(page)).toBeVisible();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(toolsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".pull-item")).toHaveCount(5);
  });

  test("PR list — collapsing acme/widgets hides its items, keeps header and count", async ({ page }) => {
    await widgetsHeader(page).click();

    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(widgetsHeader(page)).toBeVisible();
    await expect(
      widgetsHeader(page).locator(".repo-header__count"),
    ).toHaveText("4");

    // Only acme/tools' single PR remains visible.
    await expect(page.locator(".pull-item")).toHaveCount(1);
    // acme/tools stays expanded.
    await expect(toolsHeader(page)).toHaveAttribute("aria-expanded", "true");
  });

  test("PR list — expanding acme/widgets again restores its items", async ({ page }) => {
    await widgetsHeader(page).click();
    await expect(page.locator(".pull-item")).toHaveCount(1);

    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".pull-item")).toHaveCount(5);
  });

  test("PR list — keyboard activation via Enter and Space toggles collapse", async ({ page }) => {
    // Focus the widgets header directly.
    await widgetsHeader(page).focus();
    await page.keyboard.press("Enter");
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".pull-item")).toHaveCount(1);

    await widgetsHeader(page).focus();
    await page.keyboard.press("Space");
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".pull-item")).toHaveCount(5);
  });

  test("PR list — collapse state persists across reload", async ({ page }) => {
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");

    await page.reload();
    await waitForPullList(page);

    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".pull-item")).toHaveCount(1);
  });

  test("collapse is independent across pulls and issues surfaces", async ({ page }) => {
    // Collapse acme/widgets on /pulls.
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");

    // Navigate to /issues — acme/widgets must still be expanded there.
    await page.goto("/issues");
    await waitForIssueList(page);

    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    // Seed data: widgets has 3 open issues, tools has 1 — total 4.
    await expect(page.locator(".issue-item")).toHaveCount(4);
  });

  test("issue list — collapse, expand, and persist acme/widgets", async ({ page }) => {
    await page.goto("/issues");
    await waitForIssueList(page);

    // Default: 4 issues total (3 widgets + 1 tools).
    await expect(page.locator(".issue-item")).toHaveCount(4);

    // Collapse widgets: 1 issue remains (tools#5).
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(
      widgetsHeader(page).locator(".repo-header__count"),
    ).toHaveText("3");
    await expect(page.locator(".issue-item")).toHaveCount(1);

    // Expand again: back to 4.
    await widgetsHeader(page).click();
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".issue-item")).toHaveCount(4);

    // Collapse again and reload.
    await widgetsHeader(page).click();
    await page.reload();
    await waitForIssueList(page);
    await expect(widgetsHeader(page)).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".issue-item")).toHaveCount(1);
  });
});
```

- [ ] **Step 2: Typecheck the spec**

Run: `cd frontend && bun run check`
Expected: 0 errors, 0 warnings (the `tsconfig.json` picks up `tests/` too, so type mistakes in the spec show here).

- [ ] **Step 3: Lint the spec**

Run: `cd frontend && bun run lint`
Expected: no errors, no warnings.

- [ ] **Step 4: Run the Playwright e2e suite for this spec only**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts collapsible-repos.spec.ts`
Expected: 7 tests pass. The Go e2e server boots automatically via `webServer` in `playwright-e2e.config.ts`.

- [ ] **Step 5: Commit**

```bash
git add frontend/tests/e2e-full/collapsible-repos.spec.ts
git commit -m "test(e2e): add Playwright coverage for collapsible repo groups"
```

---

## Task 7: Final verification

**Context:** Everything should now be green in isolation. This task runs the full suite end-to-end to catch any lingering interaction with existing tests (keyboard-nav spec, grouping-toggle spec) before declaring the feature done.

**Files:** (none modified — verification only)

- [ ] **Step 1: Full typecheck**

Run: `cd frontend && bun run check`
Expected: 0 errors, 0 warnings.

- [ ] **Step 2: Full lint**

Run: `cd frontend && bun run lint`
Expected: no errors, no warnings.

- [ ] **Step 3: Full Vitest run**

Run: `cd frontend && bun run test`
Expected: every unit test passes, including `collapsedRepos.test.ts`, `DiffFile.test.ts`, and the existing `frontend/src/**` suites.

- [ ] **Step 4: Full frontend build**

Run: `cd frontend && bun run build`
Expected: Vite build succeeds; `dist/` is produced with no warnings.

- [ ] **Step 5: Full Playwright e2e run**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts`
Expected: every spec passes — including `grouping-toggle.spec.ts` (unchanged), `collapsible-repos.spec.ts` (new), and any other existing e2e specs.

- [ ] **Step 6: Confirm the grouped layout in the browser (manual sanity check)**

Run: `make dev` in one terminal and `make frontend-dev` in another. Open the app, switch to "By Repo", click a header, verify:
- Chevron rotates from down to right.
- Items disappear, header stays sticky.
- Count badge in the header still shows the full repo count.
- Reload preserves the collapsed state.
- Navigating to /issues shows independent collapse state.
- `j`/`k` navigation walks every PR regardless of collapse (the selected row may land inside a collapsed group — this is intentional per the spec).

- [ ] **Step 7: No commit required**

Verification only. If every step above passes, the feature is complete.
