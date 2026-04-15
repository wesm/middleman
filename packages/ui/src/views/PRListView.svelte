<script lang="ts">
  import {
    getNavigate, getSidebar,
  } from "../context.js";
  import CollapsibleResizableSidebar from "../components/shared/CollapsibleResizableSidebar.svelte";
  import PullList from "../components/sidebar/PullList.svelte";
  import PullDetail
    from "../components/detail/PullDetail.svelte";
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
    onSidebarResize?: (width: number) => void;
  }

  let {
    selectedPR = null,
    detailTab = "conversation",
    isSidebarCollapsed = false,
    hideSidebar = false,
    sidebarWidth = 340,
    onSidebarResize,
  }: Props = $props();
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
    <PullList getDetailTab={() => detailTab} />
  {/snippet}

  {#if selectedPR !== null}
    <div class="detail-tabs">
      <button
        class="detail-tab"
        class:detail-tab--active={detailTab === "conversation"}
        onclick={() => navigate(
          `/pulls/${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}`,
        )}
      >
        Conversation
      </button>
      <button
        class="detail-tab"
        class:detail-tab--active={detailTab === "files"}
        onclick={() => navigate(
          `/pulls/${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}/files`,
        )}
      >
        Files changed
      </button>
    </div>
    {#if detailTab === "files"}
      {#key `${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}`}
        <DiffView
          owner={selectedPR.owner}
          name={selectedPR.name}
          number={selectedPR.number}
        />
      {/key}
    {:else}
      <PullDetail
        owner={selectedPR.owner}
        name={selectedPR.name}
        number={selectedPR.number}
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
    {#if selectedPR !== null}
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
