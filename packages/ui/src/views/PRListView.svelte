<script lang="ts">
  import {
    getNavigate, getSidebar,
  } from "../context.js";
  import CollapsibleResizableSidebar from "../components/shared/CollapsibleResizableSidebar.svelte";
  import PullList from "../components/sidebar/PullList.svelte";
  import PullDetail
    from "../components/detail/PullDetail.svelte";
  import DiffSidebar from "../components/diff/DiffSidebar.svelte";
  import DiffView from "../components/diff/DiffView.svelte";
  import StackSidebar
    from "../components/detail/StackSidebar.svelte";

  const { isSidebarToggleEnabled, toggleSidebar } = getSidebar();
  const navigate = getNavigate();

  interface Props {
    selectedPR?: {
      owner: string;
      name: string;
      number: number;
    } | null;
    detailTab?: "conversation" | "files";
    isSidebarCollapsed?: boolean;
    hideSidebar?: boolean;
    sidebarWidth?: number;
    showStackSidebar?: boolean;
    autoSyncDetail?: boolean;
    onSidebarResize?: (width: number) => void;
    onDetailTabChange?: (tab: "conversation" | "files") => void;
  }

  let {
    selectedPR = null,
    detailTab = "conversation",
    isSidebarCollapsed = false,
    hideSidebar = false,
    sidebarWidth = 340,
    showStackSidebar = true,
    autoSyncDetail = true,
    onSidebarResize,
    onDetailTabChange,
  }: Props = $props();

  function selectDetailTab(tab: "conversation" | "files"): void {
    if (onDetailTabChange) {
      onDetailTabChange(tab);
      return;
    }
    if (selectedPR === null) return;
    navigate(
      tab === "files"
        ? `/pulls/${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}/files`
        : `/pulls/${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}`,
    );
  }
</script>

<CollapsibleResizableSidebar
  isCollapsed={isSidebarCollapsed}
  {hideSidebar}
  {sidebarWidth}
  {onSidebarResize}
  showCollapsedStrip={isSidebarToggleEnabled()}
  onExpand={toggleSidebar}
  mainEmpty={selectedPR === null}
>
  {#snippet sidebar()}
    <PullList
      getDetailTab={() => detailTab}
      showSelectedDiffSidebar={false}
    />
  {/snippet}

  {#if selectedPR !== null}
    <div class="detail-tabs">
      <button
        class="detail-tab"
        class:detail-tab--active={detailTab === "conversation"}
        onclick={() => selectDetailTab("conversation")}
      >
        Conversation
      </button>
      <button
        class="detail-tab"
        class:detail-tab--active={detailTab === "files"}
        onclick={() => selectDetailTab("files")}
      >
        Files changed
      </button>
    </div>
    {#if detailTab === "files"}
      {#key `${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}`}
        <div class="files-layout">
          <aside class="files-sidebar">
            <DiffSidebar />
          </aside>
          <div class="files-main">
            <DiffView
              owner={selectedPR.owner}
              name={selectedPR.name}
              number={selectedPR.number}
            />
          </div>
        </div>
      {/key}
    {:else}
      <PullDetail
        owner={selectedPR.owner}
        name={selectedPR.name}
        number={selectedPR.number}
        autoSync={autoSyncDetail}
        hideTabs={true}
      />
    {/if}
  {:else}
    <div class="placeholder-content">
      <p class="placeholder-text">Select a PR</p>
      <p class="placeholder-hint">
        j/k to navigate &middot; 1/2 to switch views
      </p>
    </div>
  {/if}

  {#snippet trailing()}
    {#if showStackSidebar && selectedPR !== null}
      <StackSidebar
        owner={selectedPR.owner}
        name={selectedPR.name}
        number={selectedPR.number}
      />
    {/if}
  {/snippet}
</CollapsibleResizableSidebar>

<style>
  .detail-tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .files-layout {
    display: flex;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .files-sidebar {
    width: 280px;
    flex-shrink: 0;
    border-right: 1px solid var(--border-default);
    background: var(--bg-surface);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }

  .files-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  @media (max-width: 720px) {
    .files-layout {
      flex-direction: column;
    }

    .files-sidebar {
      width: 100%;
      max-height: 35vh;
      border-right: none;
      border-bottom: 1px solid var(--border-default);
    }

    .files-main {
      flex: 1;
      min-height: 0;
    }
  }

  .placeholder-content {
    text-align: center;
  }

  .placeholder-text {
    color: var(--text-muted);
    font-size: 13px;
  }

  .placeholder-hint {
    color: var(--text-muted);
    font-size: 11px;
    margin-top: 8px;
    opacity: 0.7;
  }

  .detail-tab {
    font-size: 12px;
    font-weight: 500;
    padding: 8px 16px;
    color: var(--text-secondary);
    border-bottom: 2px solid transparent;
    transition: color 0.1s, border-color 0.1s;
  }

  .detail-tab:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .detail-tab--active {
    color: var(--text-primary);
    border-bottom-color: var(--accent-blue);
  }
</style>
