# Collapsible Repo Groups in PR and Issue Lists

## Summary

In the "By Repo" grouping of the PR list and Issue list, let the user click a
repo header to collapse or expand that repo's items. State persists across
reloads, and PR-list and Issue-list collapse state are tracked independently.

## Motivation

When "By Repo" grouping is enabled and several repos are configured, the
sidebar becomes a long scroll of every open PR or issue across every repo. A
maintainer who wants to focus on one or two repos at a time has no way to
temporarily hide the others. Collapsing a repo group removes its items from
view while keeping the header visible as a signpost.

## Scope

- Applies to `PullList.svelte` and `IssueList.svelte` when `groupByRepo` is
  `true`.
- Out of scope: the "All" (flat) view — there are no repo headers to collapse.
- Out of scope: bulk expand/collapse-all controls.
- Out of scope: collapse state per-user on the server. Client-local only.

## Architecture

### New store: `collapsedRepos.svelte.ts`

File: `packages/ui/src/stores/collapsedRepos.svelte.ts`.

Two independent `$state` `Set<string>` instances, one per surface:

- `collapsedInPulls: Set<string>`
- `collapsedInIssues: Set<string>`

Repo key format: `"owner/name"` (matches the key already used by
`pulls.pullsByRepo()` and `issues.issuesByRepo()`).

Storage keys:

- `middleman:collapsedRepos:pulls`
- `middleman:collapsedRepos:issues`

Serialized as a JSON array of strings. Read on store construction. Written on
every mutation. On parse error or missing storage, fall back to an empty set
(same defensive pattern as `grouping.svelte.ts`).

Public API:

```ts
export type CollapseSurface = "pulls" | "issues";

export function createCollapsedReposStore() {
  function isCollapsed(surface: CollapseSurface, repoKey: string): boolean;
  function toggle(surface: CollapseSurface, repoKey: string): void;
  return { isCollapsed, toggle };
}

export type CollapsedReposStore = ReturnType<typeof createCollapsedReposStore>;
```

Default state: every repo expanded (not in either set).

### Store wiring

`StoreInstances` and the store type imports live in `packages/ui/src/types.ts`,
not `index.ts`. `index.ts` only re-exports them. Both files must change.

- **`packages/ui/src/types.ts`**:
  - Line 85: add `export type { CollapsedReposStore } from "./stores/collapsedRepos.svelte.js";` next to the `GroupingStore` re-export.
  - Line 94: add `import type { CollapsedReposStore } from "./stores/collapsedRepos.svelte.js";` next to the `GroupingStore` import.
  - Around line 104: add `collapsedRepos: CollapsedReposStore;` on the `StoreInstances` interface, after `grouping`.
- **`packages/ui/src/index.ts`**:
  - Around line 21: add `CollapsedReposStore` to the `export type { ... } from "./types.js"` block, next to `GroupingStore`.
  - Around line 51: add a factory re-export: `export { createCollapsedReposStore } from "./stores/collapsedRepos.svelte.js";` next to `createGroupingStore`.
- **`packages/ui/src/Provider.svelte`**:
  - Import `createCollapsedReposStore` from `./stores/collapsedRepos.svelte.js` next to the existing `createGroupingStore` import.
  - Instantiate it in the store factory next to `const grouping = createGroupingStore();`.
  - Add `collapsedRepos` to the returned `StoreInstances` object next to `grouping`.
- **`packages/ui/package.json`**: add the exact literal entry to the
  `exports` map next to `./stores/grouping`:

  ```json
  "./stores/collapsedRepos": {
    "svelte": "./src/stores/collapsedRepos.svelte.ts",
    "default": "./src/stores/collapsedRepos.svelte.ts"
  }
  ```
- `getStores()` in `packages/ui/src/context.ts` requires no change — it
  returns the full `StoreInstances` typed object.

## Component Changes

### `PullList.svelte`

1. Pull `collapsedRepos` from `getStores()`.
2. Replace the current `<h3 class="repo-header">{repo}</h3>` + `{#each prs}`
   pattern with this exact structure:

   ```svelte
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
             <!-- existing diff-files block unchanged -->
           {/if}
         {/each}
       {/if}
     </div>
   {/each}
   ```

   Key points:
   - `type="button"` prevents default form-submit behavior if an ancestor
     ever becomes a `<form>`.
   - The `.repo-group` `<div>` always renders, so the sticky header stays
     visible when collapsed.
   - Wrapping the entire `{#each prs}` in `{#if !collapsed}` automatically
     suppresses the nested diff-files block (no additional guard needed).

### `IssueList.svelte`

Identical treatment, with `"pulls"` → `"issues"`, `pullsByRepo` →
`issuesByRepo`, `prs.length` → `repoIssues.length`. No diff-files block to
worry about.

### Styling

`.repo-header` becomes a `<button>` styled as a header. Replace the existing
rule with the merged rule below (not a second rule — edit the existing one).
Svelte scoped styles isolate these from `ActivityThreaded.svelte`'s own
`.repo-header`.

Merged `.repo-header` rule (applies to both `PullList.svelte` and
`IssueList.svelte`):

```css
.repo-header {
  /* preserved from existing rule */
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

  /* new: button-as-header */
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  text-align: left;
  border-top: none;
  border-left: none;
  border-right: none;
  cursor: pointer;
  font-family: inherit; /* override UA default on <button> */
}

.repo-header:hover {
  background: var(--bg-surface-hover);
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

The `border-bottom` from the existing rule is preserved. The UA default
`<button>` borders on top/left/right are explicitly cleared. `font-family:
inherit` is required because `<button>` elements do not inherit the page
font family by default. The hover background is identical whether the group
is collapsed or expanded — this is intentional (hover state reflects the
header as a whole, not the nested content).

### Accessibility

- `<button>` element gives native keyboard focus and Enter/Space activation.
- `aria-expanded` reflects the inverse of `isCollapsed`.
- No `aria-controls` is needed — the `.repo-group` div sits directly below and
  is identified by its position.

### Interaction with existing features

- **Selection**: if the currently selected PR or issue belongs to a
  newly-collapsed repo, the selection remains in the store but visually
  disappears. This is acceptable — re-expanding the repo restores the visible
  selection. No automatic deselect.
- **Keyboard nav (`j`/`k`)**: `selectNextPR` / `selectPrevPR` use
  `getDisplayOrderPRs()` (see `packages/ui/src/stores/pulls.svelte.ts:110`),
  which flattens `pullsByRepo()` when grouped. This iterator does NOT skip
  collapsed groups — it walks every PR in display order regardless of
  collapse state. Consequence: pressing `j` or `k` may land the selection
  inside a collapsed repo, where the selected row is not visible. The user
  can re-expand the repo to see it. This is acceptable: collapse hides,
  nav does not change. Not pruning the selection avoids a footgun where
  collapsing would silently move the caret.
- **Sticky headers**: unchanged. Buttons sticky just as well as `<h3>`.
- **"All" view**: unaffected. No repo headers exist there.
- **Star filter, search, state toggle**: unaffected. Collapse state is
  independent of what is loaded into the list.

## Data Flow

```
User clicks repo header
  -> onclick handler -> collapsedRepos.toggle(surface, repoKey)
  -> Set mutated, localStorage written
  -> $state triggers reactive re-render
  -> {#if !collapsed} branch flips
  -> {#each} block unmounts/mounts PR/issue rows
```

No network calls, no store reloads, no side effects beyond localStorage.

## Error Handling

- `localStorage.getItem` throws (private mode quota): catch, return empty set.
- `JSON.parse` fails on corrupted value: catch, return empty set.
- `localStorage.setItem` throws: catch, swallow. State still works in-memory
  for the session.

All failure modes match the existing `grouping.svelte.ts` pattern.

## Testing

No component-level tests. `PullList` and `IssueList` depend on `getStores()`,
`getNavigate()`, `getSidebar()`, and other contexts, and the `@middleman/ui`
package has no existing precedent for mocking Svelte context in tests
(the only existing test, `DiffFile.test.ts`, mocks a module import, not
context). Building that harness is out of proportion for this feature.
Coverage is split between a unit test for the store and a Playwright e2e
test that exercises the rendered components.

### Vitest: store unit tests

File: `packages/ui/src/stores/collapsedRepos.test.ts`.

- Fresh store: `isCollapsed("pulls", "a/b")` is `false`, same for
  `"issues"`.
- `toggle("pulls", "a/b")` once → `isCollapsed("pulls", "a/b")` is `true`.
  Again → `false`.
- Toggling `"pulls"` does not affect `"issues"` for the same repo key, and
  vice versa.
- Pre-seeded localStorage: set `middleman:collapsedRepos:pulls` to
  `'["a/b"]'` before constructing the store; assert `a/b` is collapsed in
  `pulls` and not in `issues`.
- Malformed JSON in localStorage (e.g., `"{not json"`): new store falls
  back to empty set, no throw.
- Missing keys in localStorage: both sets empty.
- `localStorage.setItem` throws (stub `Storage.prototype.setItem` to throw):
  `toggle` still flips the in-memory state, no throw propagates, and a
  subsequent `isCollapsed` call reflects the toggle. This verifies the
  "Error Handling" contract.
- Mutation writes to the correct storage key. Toggling `pulls` writes only
  to `middleman:collapsedRepos:pulls`, not `middleman:collapsedRepos:issues`.

### Playwright: e2e

File: `frontend/tests/e2e-full/collapsible-repos.spec.ts`.

Model on `frontend/tests/e2e-full/grouping-toggle.spec.ts`. Seed data per
the comment at the top of that file: `acme/widgets` has 4 open PRs (#1, #2,
#6, #7) and 3 open issues (#10, #11, #13); `acme/tools` has 1 open PR (#1)
and 1 open issue (#5). Totals: 5 open PRs, 4 open issues.

**`beforeEach` pattern** (must match the existing pattern exactly — you
cannot call `localStorage` before a `goto`, which is why the reload is
required):

```ts
await page.goto("/pulls");
await page.evaluate(() => {
  localStorage.removeItem("middleman:collapsedRepos:pulls");
  localStorage.removeItem("middleman:collapsedRepos:issues");
});
await page.reload();
await waitForPullList(page);
```

Test cases:

1. **PR list — default expanded**: both repo headers visible, all 5 open
   PRs visible (`.pull-item` count === 5).
2. **PR list — collapse one repo**: click `.repo-header` for `acme/widgets`
   (matched by `hasText: "acme/widgets"`). Assert `.pull-item` count
   becomes 1 (only `acme/tools`). Assert the `.repo-header` for
   `acme/widgets` is still visible. Assert the count badge inside the
   collapsed header still shows `4`. Assert `aria-expanded="false"` on
   the collapsed header.
3. **PR list — expand again**: click the same header. `.pull-item` count
   back to 5. `aria-expanded="true"`.
4. **PR list — keyboard activation**: focus the `acme/widgets` header
   (via `.focus()`), press `Enter`, assert the group collapses. Press
   `Space`, assert it expands. This enforces the accessibility claim.
5. **PR list — persistence**: collapse `acme/widgets`, reload the page,
   assert still collapsed on reload and `.pull-item` count is still 1.
6. **Independence across surfaces**: on `/pulls`, collapse `acme/widgets`.
   Navigate to `/issues`. Assert the `acme/widgets` header on `/issues` is
   expanded (`aria-expanded="true"`) and all 4 open issues are visible.
7. **Issue list — collapse and persist**: mirror test 2, 3, 5 on `/issues`
   for `acme/widgets` (expected issue counts: 4 total → 1 after collapse).

## Out of Scope / Non-Goals

- Collapse-all / expand-all button.
- Server-side persistence or sync across devices.
- Collapse state in the "All" (flat) grouping view.
- Changing the default (remains expanded).
- Animating the collapse transition (only the chevron rotates — items
  appear/disappear without height animation).
- Pruning stale entries for repos that have been removed from config or
  renamed on GitHub. Stale keys in the `Set<string>` are harmless: they
  simply never match a rendered repo. The set will grow by at most one
  entry per ever-seen repo, which is bounded in practice. If this becomes
  a concern later, a startup step can intersect the set with the current
  configured repos.
