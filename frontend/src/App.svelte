<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import PullDetail from "./lib/components/detail/PullDetail.svelte";
  import { getView } from "./lib/stores/router.svelte.ts";
  import { startPolling } from "./lib/stores/sync.svelte.js";
  import { getSelectedPR } from "./lib/stores/pulls.svelte.js";

  $effect(() => {
    startPolling();
  });
</script>

<AppHeader />

<main class="app-main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        <PullList />
      </aside>
      <section class="detail-area" class:detail-area--empty={getSelectedPR() === null}>
        {#if getSelectedPR() !== null}
          {@const sel = getSelectedPR()!}
          <PullDetail owner={sel.owner} name={sel.name} number={sel.number} />
        {:else}
          <p class="placeholder-text">Select a PR</p>
        {/if}
      </section>
    </div>
  {:else}
    <div class="board-layout">
      <p class="placeholder-text">Kanban board — coming soon</p>
    </div>
  {/if}
</main>

<style>
  .app-main {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

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

  .board-layout {
    flex: 1;
    overflow-x: auto;
    overflow-y: hidden;
    background: var(--bg-primary);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .placeholder-text {
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
