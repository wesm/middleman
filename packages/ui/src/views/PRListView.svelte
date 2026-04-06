<script lang="ts">
  import {
    getStores, getNavigate, getActions, getSidebar,
  } from "../context.js";
  import PullList from "../components/sidebar/PullList.svelte";
  import PullDetail
    from "../components/detail/PullDetail.svelte";
  import DiffView from "../components/diff/DiffView.svelte";

  const { pulls, detail, sync } = getStores();
  const navigate = getNavigate();
  const actions = getActions();
  const { isSidebarToggleEnabled, toggleSidebar } =
    getSidebar();

  interface Props {
    selectedPR?: {
      owner: string;
      name: string;
      number: number;
    } | null;
    detailTab?: "conversation" | "files";
    isSidebarCollapsed?: boolean;
    hideSidebar?: boolean;
  }

  let {
    selectedPR = null,
    detailTab = "conversation",
    isSidebarCollapsed = false,
    hideSidebar = false,
  }: Props = $props();
</script>

<div class="list-layout">
  {#if !isSidebarCollapsed}
    <aside class="sidebar">
      <PullList getDetailTab={() => detailTab} />
    </aside>
  {:else if !hideSidebar}
    <aside class="sidebar sidebar--collapsed"></aside>
  {/if}
  <section
    class="detail-area"
    class:detail-area--empty={selectedPR === null}
  >
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
            inline
          />
        {/key}
      {:else}
        <PullDetail
          owner={selectedPR.owner}
          name={selectedPR.name}
          number={selectedPR.number}
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
  </section>
</div>

<style>
  .list-layout {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .sidebar {
    width: 340px;
    flex-shrink: 0;
    background: var(--bg-surface);
    border-right: 1px solid var(--border-default);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .sidebar--collapsed {
    width: 0;
    overflow: hidden;
    border-right: none;
  }

  .detail-area {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .detail-area--empty {
    align-items: center;
    justify-content: center;
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
