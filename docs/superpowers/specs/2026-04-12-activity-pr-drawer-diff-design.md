# Inline Diff in PR Drawer Design

## Problem

When opening a PR from the activity view, the drawer shows conversation content but has no way to view the diff. The "Files changed" button attempts to navigate to `/pulls/{owner}/{name}/{number}/files` but this leaves the activity view entirely — users lose their place in the feed. The same gap exists in the kanban view's drawer.

Result: inconsistent UX. From PR list view, files are reachable via the conversation/files tab bar. From activity and kanban views, they aren't.

## Goal

Make the diff accessible from any drawer context (activity, kanban) without leaving the parent view. Give the diff enough horizontal space to be usable.

## Approach

Add tab switching (`Conversation` / `Files changed`) inside `PullDetail.svelte` itself. When the files tab is active, render `DiffView` inline. Make drawers full width (100%) so the diff gets an uncompromised experience.

PRListView already has its own external tab bar that drives URL state via the router. To avoid double tab bars in that view, PullDetail accepts a `hideTabs` prop that PRListView passes as `true`.

### Why not navigate away from the drawer

Navigation loses the user's scroll position in the activity feed, closes the drawer context, and mixes routed and non-routed state. Inline rendering keeps the activity view intact — close the drawer and you're back where you were.

### Why full width instead of matching the PR detail pane

Earlier iteration proposed matching the PR detail pane width (`calc(100% - 340px)`). Full width is simpler (no width math, no narrow-container override) and gives the diff maximum horizontal room. Conversation content is unaffected visually because `.pull-detail` already has `max-width: 800px` — it just centers in a wider container. The backdrop element stays (full-cover click-to-close blocker) but has no visible area anymore.

### Why inside PullDetail rather than at the drawer level

PullDetail is the shared unit rendered by both DetailDrawer (activity) and KanbanBoard's inline drawer. Putting tabs inside PullDetail means one implementation works in both places (and in PRListView when not suppressed). Alternative — tabs at the drawer level — would duplicate tab markup between DetailDrawer and KanbanBoard's drawer.

## Architecture

### Current state after rebase

Three places render `PullDetail`:

1. **`PRListView.svelte`** — renders either `<PullDetail>` or `<DiffView>` based on `detailTab` prop (driven by router). Has its own tab bar above the detail area.
2. **`DetailDrawer.svelte`** — used by `ActivityFeedView`. Renders `<PullDetail>` inside `.drawer-body`.
3. **`KanbanBoard.svelte`** — has its own inline drawer (not shared with DetailDrawer). Renders `<PullDetail>` inside `.drawer-body`.

`DiffView` is self-contained after PR #111 removed `DiffSidebar`. File navigation from the `PullList` sidebar applies only to PRListView context. In drawers, users navigate files by scrolling or using `j`/`k` keyboard shortcuts.

### Component changes

#### `PullDetail.svelte`

Add internal tab state and optional tab bar.

**New props:**
- `hideTabs?: boolean` (default `false`) — PRListView passes `true` to suppress PullDetail's internal tabs.

**New state:**
- `activeTab: "conversation" | "files"` — defaults to `"conversation"`.

**Layout restructure:**

Wrap the content inside the existing `{#if detail !== null}` branch (which has access to `pr = detail.merge_request`) with a new `.pull-detail-wrap` container. The top-level loading/error states remain unchanged — tabs only appear once detail is loaded, because the "Files changed" tab needs `pr.Additions`/`pr.Deletions` for its inline stats.

```svelte
{#if detailStore.isDetailLoading()}
  <div class="state-center">...</div>
{:else if detailStore.getDetailError() !== null && detailStore.getDetail() === null}
  <div class="state-center">...</div>
{:else}
  {@const detail = detailStore.getDetail()}
  {#if detail !== null}
    {@const pr = detail.merge_request}
    <div class="pull-detail-wrap">
      {#if !hideTabs}
        <div class="detail-tabs">
          <button class="detail-tab" class:detail-tab--active={activeTab === "conversation"}
                  onclick={() => activeTab = "conversation"}>
            Conversation
          </button>
          <button class="detail-tab" class:detail-tab--active={activeTab === "files"}
                  onclick={() => activeTab = "files"}>
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
          <!-- existing conversation content: header, meta row, chips, CI,
               kanban, actions, body, comment box, activity timeline -->
        </div>
      {/if}
    </div>
  {/if}
{/if}
```

The `!hideTabs && activeTab === "files"` guard means PRListView (which passes `hideTabs={true}`) always renders the conversation variant through PullDetail — PRListView renders `DiffView` itself at its own level when its router-driven tab is `"files"`. PullDetail's internal `activeTab` is dead state in that context, which is fine.

**New CSS:**

- `.pull-detail-wrap { display: flex; flex-direction: column; flex: 1; min-height: 0; overflow: hidden; }` — new scroll container boundary.
- `.pull-detail` keeps existing `max-width: 800px`, `padding: 20px 24px`, `display: flex; flex-direction: column; gap: 16px`, and gains `overflow-y: auto; flex: 1; min-height: 0`. Scroll now lives here instead of in the drawer body. This is required so that when switching to the files tab, `DiffView`'s sticky toolbar works correctly.

**Removed:**
- The `files-changed-btn` and its associated CSS — the tab replaces it.

**Tab bar CSS:**

Copy `.detail-tabs` / `.detail-tab` / `.detail-tab--active` styles from `PRListView.svelte` lines 146–171. Tab labels include inline `+N/-N` stats (which is new — PRListView's tabs don't show stats today, but PullDetail's existing files-changed-btn did).

#### `DetailDrawer.svelte`

**Width change:**
- `.drawer-panel { width: 65%; min-width: 500px; }` → `.drawer-panel { width: 100%; }`
- Remove the `container-narrow`/`container-medium` override (no longer needed).

**Scroll change:**
- `.drawer-body { overflow-y: auto; }` → `.drawer-body { display: flex; flex-direction: column; min-height: 0; }`
- Scroll now lives inside PullDetail's `.pull-detail` (conversation) or DiffView's `.diff-area` (files).

Backdrop element stays as a full-cover click-to-close blocker.

#### `KanbanBoard.svelte` (its own drawer)

Same two changes applied to `.drawer` and `.drawer-body` inside KanbanBoard (lines 181–230). No shared component, so edit in place.

#### `PRListView.svelte`

One change: pass `hideTabs={true}` to `<PullDetail>` (line 75–79) so PullDetail's internal tab bar doesn't stack on top of PRListView's router-driven tab bar.

```svelte
<PullDetail
  owner={selectedPR.owner}
  name={selectedPR.name}
  number={selectedPR.number}
  hideTabs={true}
/>
```

PRListView still renders `<DiffView>` directly when `detailTab === "files"`. PullDetail's internal files tab is only used in drawer contexts.

### Data flow

No API or store changes. `DiffView` uses the existing `diffStore` which already loads via `onMount`. Mounting `DiffView` inside a drawer causes it to fetch the diff when the user clicks the files tab.

When switching tabs back from files to conversation, `DiffView` unmounts and calls `diffStore.clearDiff()` in its cleanup (existing behavior). No stale state.

## Testing

**E2E test** (`frontend/tests/e2e/activity-drawer-diff.spec.ts` or similar):

1. Seed the fixture server with a PR that has diff data.
2. Navigate to the activity view.
3. Click an activity row that references the PR — drawer opens showing conversation tab.
4. Click the "Files changed" tab in the drawer — assert `DiffView` is rendered, toolbar is visible, at least one file block is present.
5. Click "Conversation" tab — assert conversation content returns.
6. Close drawer with Escape — assert drawer is gone and activity feed is still rendered (scroll preserved if possible).

**Unit test** (`packages/ui/src/components/detail/PullDetail.test.ts`):
- `hideTabs={true}` hides the tab bar.
- Default tab is `"conversation"`.

**Manual verification:**
- Activity drawer: open PR, switch tabs, confirm diff loads and scrolls independently, toolbar sticks.
- Kanban drawer: same.
- PR list view: confirm no double tab bars, files tab still navigates via URL.
- Narrow viewport: confirm conversation content still readable (800px cap centers in full-width drawer).

## Scope

**In scope:**
- PullDetail tab bar + DiffView integration.
- Drawer width → 100% in DetailDrawer and KanbanBoard.
- Scroll ownership shift from drawer body to PullDetail/DiffView.
- PRListView `hideTabs` prop pass-through.
- E2E coverage.

**Out of scope:**
- Unifying DetailDrawer and KanbanBoard's drawer implementations (they remain separate).
- Syncing the drawer's active tab with the URL (drawers remain URL-agnostic).
- Adding file-list navigation to the drawer context (users rely on scroll + `j`/`k`).
- Any changes to `DiffView` itself, `PullList`, or the diff store.
