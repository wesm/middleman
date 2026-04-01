# Global Project Selector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the repo selector from per-view filter bars into the AppHeader so a single selection persists across all views and page reloads.

**Architecture:** Create a global filter store (`filter.svelte.ts`) that owns `filterRepo` state persisted to localStorage. The AppHeader renders a `RepoTypeahead` bound to this store. Each view store (`pulls`, `issues`, `activity`) reads from the global filter instead of maintaining its own. `App.svelte` watches the global filter and triggers reloads when it changes.

**Tech Stack:** Svelte 5 (runes), TypeScript, Vitest

---

### Task 1: Create the global filter store

**Files:**
- Create: `frontend/src/lib/stores/filter.svelte.ts`

- [ ] **Step 1: Create `filter.svelte.ts`**

```ts
const STORAGE_KEY = "middleman-filter-repo";

function loadPersistedRepo(): string | undefined {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v ?? undefined;
  } catch {
    return undefined;
  }
}

let filterRepo = $state<string | undefined>(loadPersistedRepo());

export function getGlobalRepo(): string | undefined {
  return filterRepo;
}

export function setGlobalRepo(repo: string | undefined): void {
  filterRepo = repo;
  try {
    if (repo !== undefined) {
      localStorage.setItem(STORAGE_KEY, repo);
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  } catch {
    // Storage blocked — filter still works for this session
  }
}
```

- [ ] **Step 2: Verify frontend builds**

Run: `cd frontend && bun run build`
Expected: Build succeeds with no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/stores/filter.svelte.ts
git commit -m "feat: add global filter store with localStorage persistence"
```

---

### Task 2: Add RepoTypeahead to AppHeader

**Files:**
- Modify: `frontend/src/lib/components/layout/AppHeader.svelte`

- [ ] **Step 1: Import the global filter store and RepoTypeahead in AppHeader**

Add to the `<script>` block:

```ts
import RepoTypeahead from "../RepoTypeahead.svelte";
import { getGlobalRepo, setGlobalRepo } from "../../stores/filter.svelte.js";
```

- [ ] **Step 2: Add RepoTypeahead to the header-left section**

Replace the `header-left` div contents:

```svelte
<div class="header-left">
  <span class="logo">middleman</span>
  <RepoTypeahead
    selected={getGlobalRepo()}
    onchange={setGlobalRepo}
  />
</div>
```

- [ ] **Step 3: Add a gap between the logo and the typeahead**

In the `.header-left` CSS rule, add:

```css
.header-left {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 12px;
}
```

- [ ] **Step 4: Verify frontend builds**

Run: `cd frontend && bun run build`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/components/layout/AppHeader.svelte
git commit -m "feat: add repo selector to app header"
```

---

### Task 3: Wire pulls store to global filter

**Files:**
- Modify: `frontend/src/lib/stores/pulls.svelte.ts`

- [ ] **Step 1: Replace local filterRepo with global store import**

Remove these lines from `pulls.svelte.ts`:

```ts
let filterRepo = $state<string | undefined>(undefined);
```

Add import at the top:

```ts
import { getGlobalRepo } from "./filter.svelte.js";
```

- [ ] **Step 2: Update `loadPulls` to read from global store**

In the `loadPulls` function, change the merged params from:

```ts
...(filterRepo !== undefined && { repo: filterRepo }),
```

to:

```ts
...(getGlobalRepo() !== undefined && { repo: getGlobalRepo() }),
```

- [ ] **Step 3: Remove the per-view filter functions**

Remove these functions entirely:

```ts
export function getFilterRepo(): string | undefined {
  return filterRepo;
}

export function setFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}
```

- [ ] **Step 4: Verify frontend builds**

Run: `cd frontend && bun run build`
Expected: Build succeeds (callers will be fixed in later tasks)

If there are build errors from components still importing `getFilterRepo`/`setFilterRepo`, that's expected and will be fixed in Task 6.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/stores/pulls.svelte.ts
git commit -m "refactor: pulls store reads repo filter from global store"
```

---

### Task 4: Wire issues store to global filter

**Files:**
- Modify: `frontend/src/lib/stores/issues.svelte.ts`

- [ ] **Step 1: Replace local filterRepo with global store import**

Remove this line:

```ts
let filterRepo = $state<string | undefined>(undefined);
```

Add import at the top:

```ts
import { getGlobalRepo } from "./filter.svelte.js";
```

- [ ] **Step 2: Update `loadIssues` to read from global store**

In the `loadIssues` function, change:

```ts
...(filterRepo !== undefined && { repo: filterRepo }),
```

to:

```ts
...(getGlobalRepo() !== undefined && { repo: getGlobalRepo() }),
```

- [ ] **Step 3: Remove the per-view filter functions**

Remove these:

```ts
export function getIssueFilterRepo(): string | undefined { return filterRepo; }
export function setIssueFilterRepo(repo: string | undefined): void { filterRepo = repo; }
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/stores/issues.svelte.ts
git commit -m "refactor: issues store reads repo filter from global store"
```

---

### Task 5: Wire activity store to global filter

**Files:**
- Modify: `frontend/src/lib/stores/activity.svelte.ts`

- [ ] **Step 1: Replace local filterRepo with global store import**

Remove this line:

```ts
let filterRepo = $state<string | undefined>(undefined);
```

Add import at the top:

```ts
import { getGlobalRepo } from "./filter.svelte.js";
```

- [ ] **Step 2: Update `buildParams` to read from global store**

In `buildParams()`, change:

```ts
if (filterRepo) p.repo = filterRepo;
```

to:

```ts
const repo = getGlobalRepo();
if (repo) p.repo = repo;
```

- [ ] **Step 3: Remove per-view filter functions and URL sync for repo**

Remove these functions:

```ts
export function getActivityFilterRepo(): string | undefined {
  return filterRepo;
}

export function setActivityFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}
```

In `syncFromURL()`, remove the repo handling:

```ts
if (sp.has("repo")) filterRepo = sp.get("repo") ?? undefined;
```

In `syncToURL()`, remove the repo handling:

```ts
if (filterRepo) sp.set("repo", filterRepo);
else sp.delete("repo");
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/stores/activity.svelte.ts
git commit -m "refactor: activity store reads repo filter from global store"
```

---

### Task 6: Remove per-view RepoTypeahead from all view components

**Files:**
- Modify: `frontend/src/lib/components/sidebar/PullList.svelte`
- Modify: `frontend/src/lib/components/sidebar/IssueList.svelte`
- Modify: `frontend/src/lib/components/kanban/KanbanBoard.svelte`
- Modify: `frontend/src/lib/components/ActivityFeed.svelte`

- [ ] **Step 1: Update PullList.svelte**

Remove these imports:

```ts
import RepoTypeahead from "../RepoTypeahead.svelte";
```

And remove from the imports list:

```ts
getFilterRepo,
setFilterRepo,
```

Remove the `<RepoTypeahead>` from the filter-bar:

```svelte
<RepoTypeahead
  selected={getFilterRepo()}
  onchange={(repo) => { setFilterRepo(repo); void loadPulls(); }}
/>
```

The filter-bar should now just contain the count badge and state toggle.

- [ ] **Step 2: Update IssueList.svelte**

Remove these imports:

```ts
import RepoTypeahead from "../RepoTypeahead.svelte";
```

And remove from the imports list:

```ts
getIssueFilterRepo,
setIssueFilterRepo,
```

Remove the `<RepoTypeahead>` from the filter-bar:

```svelte
<RepoTypeahead
  selected={getIssueFilterRepo()}
  onchange={(repo) => { setIssueFilterRepo(repo); void loadIssues(); }}
/>
```

- [ ] **Step 3: Update KanbanBoard.svelte**

Remove these imports:

```ts
import RepoTypeahead from "../RepoTypeahead.svelte";
```

And remove from the imports list:

```ts
getFilterRepo,
setFilterRepo,
```

Remove the `handleRepoChange` function:

```ts
function handleRepoChange(repo: string | undefined): void {
  setFilterRepo(repo);
  void loadPulls({ state: "open" });
}
```

Remove the entire controls-bar div:

```svelte
<div class="controls-bar">
  <RepoTypeahead
    selected={getFilterRepo()}
    onchange={handleRepoChange}
  />
</div>
```

Also remove the `.controls-bar` CSS rule.

- [ ] **Step 4: Update ActivityFeed.svelte**

Remove these imports:

```ts
import RepoTypeahead from "./RepoTypeahead.svelte";
```

And remove from the imports list:

```ts
getActivityFilterRepo,
setActivityFilterRepo,
```

Remove the `handleRepoChange` function:

```ts
function handleRepoChange(repo: string | undefined): void {
  setActivityFilterRepo(repo);
  syncToURL();
  void loadActivity();
}
```

Remove the `<RepoTypeahead>` from the controls-bar:

```svelte
<RepoTypeahead
  selected={getActivityFilterRepo()}
  onchange={handleRepoChange}
/>
```

- [ ] **Step 5: Verify frontend builds**

Run: `cd frontend && bun run build`
Expected: Build succeeds with no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/components/sidebar/PullList.svelte \
  frontend/src/lib/components/sidebar/IssueList.svelte \
  frontend/src/lib/components/kanban/KanbanBoard.svelte \
  frontend/src/lib/components/ActivityFeed.svelte
git commit -m "refactor: remove per-view repo selectors"
```

---

### Task 7: Add global filter change watcher in App.svelte

**Files:**
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Import the global filter store**

Add to the imports in App.svelte:

```ts
import { getGlobalRepo } from "./lib/stores/filter.svelte.js";
import { loadActivity } from "./lib/stores/activity.svelte.js";
```

(`loadPulls` and `loadIssues` are already imported.)

- [ ] **Step 2: Add an effect that reloads data when the global repo changes**

After the existing `onMount` block, add:

```ts
let lastRepo: string | undefined;
$effect(() => {
  const repo = getGlobalRepo();
  if (!appReady) {
    lastRepo = repo;
    return;
  }
  if (repo === lastRepo) return;
  lastRepo = repo;
  void loadPulls();
  void loadIssues();
  void loadActivity();
});
```

This skips the first run (before appReady) and only fires on subsequent changes.

- [ ] **Step 3: Verify frontend builds**

Run: `cd frontend && bun run build`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.svelte
git commit -m "feat: reload all views when global repo filter changes"
```

---

### Task 8: Clean up unused RepoSelector component

**Files:**
- Delete: `frontend/src/lib/components/sidebar/RepoSelector.svelte` (if unused)

- [ ] **Step 1: Check if RepoSelector is imported anywhere**

Run: `cd frontend && grep -r "RepoSelector" src/`

If no results (or only the file itself), the component is unused.

- [ ] **Step 2: Delete the unused file**

If unused, delete `frontend/src/lib/components/sidebar/RepoSelector.svelte`.

- [ ] **Step 3: Commit**

```bash
git add -A frontend/src/lib/components/sidebar/RepoSelector.svelte
git commit -m "chore: remove unused RepoSelector component"
```

---

### Task 9: Run tests and verify

**Files:** None (verification only)

- [ ] **Step 1: Run the full frontend test suite**

Run: `cd frontend && bun run test`
Expected: All tests pass

- [ ] **Step 2: Run the frontend build**

Run: `cd frontend && bun run build`
Expected: Build succeeds with no errors or warnings

- [ ] **Step 3: Run the full Go test suite**

Run: `make test`
Expected: All tests pass (Go backend is unaffected by this change)

- [ ] **Step 4: Fix any test failures**

If existing tests fail due to removed exports (`getFilterRepo`, `setFilterRepo`, etc.), update those tests to use the global filter store instead.

For `detail.svelte.test.ts`: the test calls `loadPulls()` which now reads from `getGlobalRepo()`. The mock setup may need to also mock `filter.svelte.js`:

```ts
vi.mock("../stores/filter.svelte.js", () => ({
  getGlobalRepo: () => undefined,
}));
```

- [ ] **Step 5: Commit any test fixes**

```bash
git add -A
git commit -m "fix: update tests for global filter store"
```
