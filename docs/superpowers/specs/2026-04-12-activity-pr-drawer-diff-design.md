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

Earlier iteration proposed matching the PR detail pane width (`calc(100% - 340px)`). Full width is simpler (no width math, no narrow-container override) and gives the diff maximum horizontal room.

Conversation content keeps its `max-width: 800px`, but needs an explicit horizontal centering rule (`margin-inline: auto`) — without it, the column hugs the left edge inside a wide drawer, leaving a large blank right region. The current `.pull-detail` styles do not center.

**Outside-click dismissal is removed as a side effect.** Both current drawers close on exposed-backdrop click (`DetailDrawer.svelte` line 15, `KanbanBoard.svelte` line 104). At 100% width there is no exposed backdrop area, so this affordance disappears. Close affordances that remain: the drawer header's close button and the `Escape` key. Both are already wired up. This is an acceptable tradeoff because the user explicitly asked for full-width; preserving outside-click would require leaving a visible gutter that compromises the diff experience.

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
- `.pull-detail` keeps existing `max-width: 800px`, `padding: 20px 24px`, `display: flex; flex-direction: column; gap: 16px`, and gains:
  - `overflow-y: auto` and `flex: 1; min-height: 0` — scroll now lives here instead of in the drawer body, required so `DiffView`'s sticky toolbar works when switching tabs.
  - `width: 100%` and `margin-inline: auto` — without these, the 800px max-width cap leaves conversation content hugging the left edge of the wide full-width drawer. These center the column and make it span the full drawer width on narrow viewports.
  - `box-sizing: border-box` if not already applied globally — so padding is included in the 800px cap.

**Removed:**
- The `files-changed-btn` and its associated CSS — the tab replaces it.

**Tab bar CSS:**

Copy `.detail-tabs` / `.detail-tab` / `.detail-tab--active` styles from `PRListView.svelte` lines 146–171. Tab labels include inline `+N/-N` stats (which is new — PRListView's tabs don't show stats today, but PullDetail's existing files-changed-btn did).

#### `IssueDetail.svelte`

DetailDrawer renders both `PullDetail` and `IssueDetail`. Moving scroll ownership out of `.drawer-body` would break issue scrolling, since `IssueDetail` today relies on the drawer body being scrollable (`.issue-detail` has `max-width: 800px` with no internal scroll at `packages/ui/src/components/detail/IssueDetail.svelte:259`).

Give `IssueDetail` the same internal-scroll + centering treatment as `PullDetail`'s conversation container. IssueDetail has no tabs and no diff, so no wrap element or structural restructure is needed — just CSS changes on the existing `.issue-detail` block:

- Add `overflow-y: auto`, `flex: 1`, `min-height: 0` so it scrolls internally instead of relying on a scrollable parent.
- Add `width: 100%`, `margin-inline: auto`, `box-sizing: border-box` so the 800px cap stays centered inside the full-width drawer.

#### `DetailDrawer.svelte`

**Width change:**
- `.drawer-panel { width: 65%; min-width: 500px; }` → `.drawer-panel { width: 100%; }`
- Remove the `container-narrow`/`container-medium` override (no longer needed).

**Scroll change:**
- `.drawer-body { overflow-y: auto; }` → `.drawer-body { display: flex; flex-direction: column; min-height: 0; }`
- Scroll now lives inside PullDetail's `.pull-detail` (conversation), DiffView's `.diff-area` (files), or IssueDetail's `.issue-detail`. Both child components are updated to own their scroll before this drawer-body change lands.

**Backdrop:**
- Remove the `handleBackdropClick` handler and the `onclick` binding on `.drawer-backdrop` (currently `DetailDrawer.svelte` lines 15–19, 36). At 100% width there is no exposed backdrop to click. Close affordances that remain: the header close button (line 39) and the `Escape` key handler (lines 21–31), both already wired up.
- The `.drawer-backdrop` element itself can stay as a positional wrapper, or collapse into the panel directly. Keeping it is simpler — it still defines the `top: var(--header-height)` / `bottom: var(--status-bar-height)` bounds that position the drawer inside the app's content region.

#### `KanbanBoard.svelte` (its own drawer)

Same changes applied in place inside KanbanBoard:
- `.drawer { width: 65%; min-width: 500px; }` → `.drawer { width: 100%; }` (lines 181–195).
- `.drawer-body { overflow-y: auto; }` → `.drawer-body { display: flex; flex-direction: column; min-height: 0; }` (lines 227–230).
- Remove the `closeDrawer` call from the `.drawer-overlay` click handler and drop the overlay element entirely (lines 104–106) — it's a separate full-cover element that currently darkens the page behind the drawer, but with a 100%-wide drawer there is nothing behind to darken. `Escape` and the header close button (line 109) remain.
- Remove the `container-narrow`/`container-medium` override (lines 232–236).

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

**E2E test** (`frontend/tests/e2e-full/activity-drawer-diff.spec.ts`):

This belongs in `tests/e2e-full/` (managed backend + real SQLite per `playwright-e2e.config.ts`), not `tests/e2e/` (Vite dev server only). Per project convention the full-stack suite is the non-negotiable path for features that exercise real data flow.

1. Seed the e2e-full backend with a PR that has diff data (follow patterns in existing `tests/e2e-full/diff-view.spec.ts`).
2. Navigate to the activity view.
3. Click an activity row that references the seeded PR — drawer opens showing conversation tab.
4. Click the "Files changed" tab in the drawer — assert `DiffView` is rendered, the diff toolbar is visible, and at least one file block is present.
5. Click "Conversation" tab — assert conversation content returns and diff is unmounted.
6. Close drawer with `Escape` — assert drawer is gone and activity feed is still rendered.
7. Confirm backdrop click no longer closes the drawer (optional regression guard for the outside-click removal).

**Kanban drawer e2e coverage**: add a parallel case that opens the drawer from a kanban card and performs steps 3–6.

**Issue drawer regression guard**: add an e2e case that opens an issue from the activity view, seeds a long issue body or enough comments to overflow the drawer, and scrolls to the bottom. This catches any future change that re-breaks internal scroll in `IssueDetail`.

**Manual verification:**
- Activity PR drawer: open PR, switch tabs, confirm diff loads and scrolls independently, toolbar sticks.
- Activity issue drawer: open issue with long body, confirm scrolling works.
- Kanban drawer: same as activity PR drawer.
- PR list view: confirm no double tab bars, files tab still navigates via URL.
- Narrow viewport: confirm conversation/issue content still readable (800px cap centers in full-width drawer via `margin-inline: auto`).
- Wide viewport: confirm conversation/issue is centered (not hugging the left edge).

## Scope

**In scope:**
- PullDetail tab bar + DiffView integration.
- IssueDetail internal scroll + centering (required so DetailDrawer can stop owning scroll).
- Drawer width → 100% in DetailDrawer and KanbanBoard.
- Scroll ownership shift from drawer body to PullDetail/DiffView/IssueDetail.
- Centering rule on `.pull-detail` and `.issue-detail` so content stays centered in the wide drawer.
- Removal of outside-click dismissal (backdrop handlers) since the overlay has no exposed area at 100% width.
- Removal of KanbanBoard's visual overlay element (nothing to darken).
- PRListView `hideTabs` prop pass-through.
- E2E coverage via `tests/e2e-full/` (activity PR drawer, activity issue drawer, kanban drawer paths).

**Out of scope:**
- Unifying DetailDrawer and KanbanBoard's drawer implementations (they remain separate).
- Syncing the drawer's active tab with the URL (drawers remain URL-agnostic).
- Adding file-list navigation to the drawer context (users rely on scroll + `j`/`k`).
- Any changes to `DiffView` itself, `PullList`, or the diff store.
- Preserving outside-click dismissal via a partial-width gutter (explicitly rejected in favor of the uncompromised full-width diff).
- Tab switching in IssueDetail (issues have no diff; no tabs needed).
