<script lang="ts">
  import PanelLeftCloseIcon from "@lucide/svelte/icons/panel-left-close";
  import PanelLeftOpenIcon from "@lucide/svelte/icons/panel-left-open";
  import { onDestroy } from "svelte";
  import type { ActivityItem } from "../api/types.js";
  import ActivityFeed
    from "../components/ActivityFeed.svelte";
  import PRListView from "./PRListView.svelte";
  import IssueListView from "./IssueListView.svelte";

  type DrawerItem = {
    itemType: "pr" | "issue";
    platformHost?: string | undefined;
    owner: string;
    name: string;
    number: number;
    detailTab?: "conversation" | "files";
  };

  interface Props {
    drawerItem?: DrawerItem | null;
    detailTab?: "conversation" | "files";
    onSelectItem?: (item: ActivityItem) => void;
    onCloseDrawer?: () => void;
    onDetailTabChange?: (tab: "conversation" | "files") => void;
  }

  let {
    drawerItem: controlledDrawer,
    detailTab = "conversation",
    onSelectItem,
    onCloseDrawer,
    onDetailTabChange,
  }: Props = $props();

  // Internal state used when no controlled props are
  // provided (standalone usage).
  let internalDrawer = $state<DrawerItem | null>(null);
  let activityPaneWidth = $state(360);
  let activityPaneCollapsed = $state(false);

  const minActivityPaneWidth = 280;
  const maxActivityPaneWidth = 560;
  let resizeCleanup: (() => void) | null = null;

  const controlled = $derived(
    controlledDrawer !== undefined || onCloseDrawer !== undefined,
  );
  const activeDrawer = $derived(
    controlled ? (controlledDrawer ?? null) : internalDrawer,
  );

  function handleSelect(item: ActivityItem): void {
    const itemType =
      item.item_type === "issue" ? "issue" : "pr";
    const entry: DrawerItem = {
      itemType,
      platformHost: item.platform_host,
      owner: item.repo_owner,
      name: item.repo_name,
      number: item.item_number,
      detailTab: "conversation",
    };
    if (!controlled) {
      internalDrawer = entry;
    }
    onSelectItem?.(item);
  }

  function handleClose(): void {
    activityPaneCollapsed = false;
    stopActivityPaneResize();
    if (!controlled) {
      internalDrawer = null;
    }
    onCloseDrawer?.();
  }

  function clampActivityPaneWidth(width: number): number {
    return Math.max(
      minActivityPaneWidth,
      Math.min(maxActivityPaneWidth, width),
    );
  }

  function stopActivityPaneResize(): void {
    resizeCleanup?.();
    resizeCleanup = null;
  }

  function startActivityPaneResize(event: MouseEvent): void {
    event.preventDefault();
    stopActivityPaneResize();
    const startX = event.clientX;
    const startWidth = activityPaneWidth;

    function onMove(moveEvent: MouseEvent): void {
      activityPaneWidth = clampActivityPaneWidth(
        startWidth + moveEvent.clientX - startX,
      );
    }

    function onUp(): void {
      stopActivityPaneResize();
    }

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    resizeCleanup = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }

  function handleActivityPaneResizeKeydown(event: KeyboardEvent): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      activityPaneWidth = clampActivityPaneWidth(activityPaneWidth - 24);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      activityPaneWidth = clampActivityPaneWidth(activityPaneWidth + 24);
    }
  }

  function collapseActivityPane(): void {
    activityPaneCollapsed = true;
  }

  function expandActivityPane(): void {
    activityPaneCollapsed = false;
  }

  onDestroy(() => {
    stopActivityPaneResize();
  });
</script>

<div
  class="activity-shell"
  class:activity-shell--split={activeDrawer !== null}
  class:activity-shell--full={activeDrawer === null}
>
  <section
    class="activity-pane"
    class:activity-pane--collapsed={activeDrawer !== null && activityPaneCollapsed}
    style:--activity-pane-width={`${activityPaneWidth}px`}
  >
    {#if activeDrawer && activityPaneCollapsed}
      <div class="activity-collapsed-strip">
        <button
          class="activity-sidebar-toggle"
          onclick={expandActivityPane}
          title="Expand Activity sidebar"
          type="button"
        >
          <PanelLeftOpenIcon
            size="14"
            strokeWidth="1.5"
            aria-hidden="true"
          />
        </button>
      </div>
    {:else if activeDrawer}
      <div class="activity-rail-header">
        <span>Activity</span>
        <button
          class="activity-sidebar-toggle"
          onclick={collapseActivityPane}
          title="Collapse Activity sidebar"
          type="button"
        >
          <PanelLeftCloseIcon
            size="14"
            strokeWidth="1.5"
            aria-hidden="true"
          />
        </button>
      </div>
    {/if}
    <div class="activity-feed-wrap">
      <ActivityFeed
        compact={activeDrawer !== null}
        selectedItem={activeDrawer}
        onSelectItem={handleSelect}
      />
    </div>
  </section>

  {#if activeDrawer && !activityPaneCollapsed}
    <button
      class="activity-split-resize-handle"
      aria-label="Resize Activity rail"
      type="button"
      onkeydown={handleActivityPaneResizeKeydown}
      onmousedown={startActivityPaneResize}
    ></button>
  {/if}

  {#if activeDrawer}
    <section class="activity-detail">
      <div class="activity-detail-header">
        <span>
          {activeDrawer.owner}/{activeDrawer.name}#{activeDrawer.number}
        </span>
        <button
          class="activity-rail-close"
          onclick={handleClose}
          title="Close Activity selection"
          type="button"
        >
          &times;
        </button>
      </div>

      {#if activeDrawer.itemType === "pr"}
        <PRListView
          selectedPR={{
            owner: activeDrawer.owner,
            name: activeDrawer.name,
            number: activeDrawer.number,
          }}
          {detailTab}
          isSidebarCollapsed={true}
          hideSidebar={true}
          showStackSidebar={false}
          {...onDetailTabChange ? { onDetailTabChange } : {}}
        />
      {:else}
        <IssueListView
          selectedIssue={{
            owner: activeDrawer.owner,
            name: activeDrawer.name,
            number: activeDrawer.number,
            platformHost: activeDrawer.platformHost,
          }}
          isSidebarCollapsed={true}
          hideSidebar={true}
        />
      {/if}
    </section>
  {/if}
</div>

<style>
  .activity-shell {
    flex: 1;
    overflow: hidden;
    display: flex;
    min-height: 0;
    container-type: inline-size;
  }

  .activity-pane {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .activity-shell--split .activity-pane {
    width: var(--activity-pane-width, 360px);
    flex: 0 0 var(--activity-pane-width, 360px);
    border-right: 1px solid var(--border-default);
  }

  .activity-shell--split .activity-pane--collapsed {
    width: 28px;
    flex-basis: 28px;
  }

  .activity-feed-wrap {
    min-height: 0;
    flex: 1;
    display: flex;
    flex-direction: column;
  }

  .activity-shell--split .activity-pane--collapsed .activity-feed-wrap {
    display: none;
  }

  .activity-rail-header,
  .activity-detail-header {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-height: 34px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 12px;
    font-weight: 600;
  }

  .activity-collapsed-strip {
    width: 28px;
    flex: 1;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 6px;
    background: var(--bg-surface);
  }

  .activity-sidebar-toggle {
    width: 22px;
    height: 22px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }

  .activity-sidebar-toggle:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .activity-detail-header span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .activity-rail-close {
    width: 22px;
    height: 22px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    background: var(--bg-inset);
  }

  .activity-rail-close:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-surface-hover);
  }

  .activity-split-resize-handle {
    width: 4px;
    cursor: col-resize;
    background: var(--border-muted);
    appearance: none;
    border: 0;
    padding: 0;
    flex-shrink: 0;
  }

  .activity-split-resize-handle:hover,
  .activity-split-resize-handle:focus-visible {
    background: var(--accent-blue);
    outline: none;
  }

  .activity-detail {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  @container (max-width: 760px) {
    .activity-shell--split .activity-pane {
      display: none;
    }

    .activity-shell--split .activity-split-resize-handle {
      display: none;
    }
  }
</style>
