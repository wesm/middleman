<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import { getView } from "./lib/stores/router.svelte.ts";
</script>

<AppHeader />

<main class="app-main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        <p class="placeholder-text">PR list — coming soon</p>
      </aside>
      <section class="detail-area">
        <p class="placeholder-text">PR detail — coming soon</p>
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
    overflow-y: auto;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .detail-area {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
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
