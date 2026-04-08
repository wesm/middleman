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

- Add `CollapsedReposStore` type export and import in `packages/ui/src/index.ts`
  next to the existing `GroupingStore` entry (lines 85 and 94).
- Add `collapsedRepos: CollapsedReposStore` field on the `StoreInstances`
  interface in `packages/ui/src/index.ts` (after `grouping`, around line 104).
- In `packages/ui/src/Provider.svelte`, import `createCollapsedReposStore`
  from `./stores/collapsedRepos.svelte.js` (next to the grouping import),
  instantiate it in the store factory (next to `const grouping = ...`), and
  add `collapsedRepos` to the returned `StoreInstances` object (next to
  `grouping`).
- Add a `./stores/collapsedRepos` entry to the `exports` map in
  `packages/ui/package.json`, mirroring the existing `./stores/grouping`
  entry.
- `getStores()` in `packages/ui/src/context.ts` requires no change — it
  returns the full `StoreInstances` typed object.

## Component Changes

### `PullList.svelte`

1. Pull `collapsedRepos` from `getStores()`.
2. In the `{#if grouping.getGroupByRepo()}` branch, for each `[repo, prs]`
   entry from `pulls.pullsByRepo()`, compute
   `const collapsed = collapsedRepos.isCollapsed("pulls", repo)`.
3. Replace the existing `<h3 class="repo-header">{repo}</h3>` with a
   `<button class="repo-header" aria-expanded={!collapsed}
   onclick={() => collapsedRepos.toggle("pulls", repo)}>`. Content:

   ```html
   <svg class="repo-header__chevron" class:repo-header__chevron--collapsed={collapsed}
        width="10" height="10" viewBox="0 0 10 10"
        fill="none" stroke="currentColor" stroke-width="1.5">
     <polyline points="2,3 5,7 8,3" stroke-linecap="round" stroke-linejoin="round" />
   </svg>
   <span class="repo-header__name">{repo}</span>
   <span class="repo-header__count">{prs.length}</span>
   ```

4. Wrap the existing `{#each prs ...}` block in `{#if !collapsed} ... {/if}`.
   The `.repo-group` `<div>` still renders so the header stays visible.
5. When `collapsed` is `true`, also suppress the `{#if prSelected && ... "files"}`
   diff-files block inside that group (it lives inside the `{#each}` body, so
   hiding the `{#each}` covers it automatically — nothing extra to do).

### `IssueList.svelte`

Identical treatment, with `"pulls"` → `"issues"`, `pullsByRepo` →
`issuesByRepo`, `prs.length` → `repoIssues.length`. No diff-files block to
worry about.

### Styling

`.repo-header` becomes a `<button>` styled as a header. Adjust the existing
CSS rule and add new rules. Current values preserved: padding, font size,
text-transform, color, background, border, sticky positioning, z-index.

New rules:

```css
.repo-header {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  text-align: left;
  border: none;
  cursor: pointer;
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
- **Keyboard nav (`j`/`k`)**: operates on the flat `pulls.getPulls()` /
  `issues.getIssues()` arrays. It already does not consult the rendered
  repo-group structure, so collapse does not break it. The selection may
  advance into a collapsed group; the user can re-expand to see what was
  selected.
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

### Vitest: store unit tests

File: `packages/ui/src/stores/collapsedRepos.test.ts`.

- Fresh store: `isCollapsed("pulls", "a/b")` is `false`.
- `toggle` once: `isCollapsed` becomes `true`. Again: `false`.
- Toggling `"pulls"` does not affect `"issues"` for the same key.
- Pre-seeded localStorage (`middleman:collapsedRepos:pulls` = `["a/b"]`):
  new store reports `a/b` as collapsed in `pulls`, not in `issues`.
- Malformed JSON in localStorage: new store falls back to empty, no throw.
- Mutation writes to the correct key.

### Vitest: component tests

Files: `packages/ui/src/components/sidebar/PullList.test.ts`,
`packages/ui/src/components/sidebar/IssueList.test.ts`.

Use `@testing-library/svelte` and mock the store context the same way
`DiffFile.test.ts` mocks its dependencies.

- Render with two repo groups. Both expanded by default. Items visible.
- Click chevron on first group. Items for that group removed from DOM.
  Header still visible. Second group untouched.
- Click again. Items restored.
- Store seeded as collapsed on mount: items absent on first render.

### Playwright: e2e

File: `frontend/tests/e2e-full/collapsible-repos.spec.ts`.

Model on `frontend/tests/e2e-full/grouping-toggle.spec.ts`. Uses seed repos
`acme/widgets` and `acme/tools`.

- Clear `middleman:collapsedRepos:pulls` and `:issues` in `beforeEach`.
- **PR list**: collapse `acme/widgets` by clicking its `.repo-header`. Assert
  `.pull-item` count equals only the `acme/tools` PRs. `.repo-header` for
  `acme/widgets` still visible. Count badge visible.
- Click again, all items back.
- **Persistence**: collapse, reload page, still collapsed.
- **Independence across surfaces**: on `/pulls`, collapse `acme/widgets`.
  Navigate to `/issues`. `acme/widgets` issues still visible.
- **Issue list**: parallel test — collapse `acme/widgets` on `/issues`,
  assert only `acme/tools` issues visible, reload, still collapsed.

## Out of Scope / Non-Goals

- Collapse-all / expand-all button.
- Server-side persistence or sync across devices.
- Collapse state in the "All" (flat) grouping view.
- Changing the default (remains expanded).
- Animating the collapse transition (only the chevron rotates — items
  appear/disappear without height animation).
