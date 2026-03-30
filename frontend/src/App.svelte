<script lang="ts">
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import StatusBar from "./lib/components/layout/StatusBar.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import PullDetail from "./lib/components/detail/PullDetail.svelte";
  import IssueList from "./lib/components/sidebar/IssueList.svelte";
  import IssueDetail from "./lib/components/detail/IssueDetail.svelte";
  import { getView, setView, getTab } from "./lib/stores/router.svelte.ts";
  import { startPolling } from "./lib/stores/sync.svelte.js";
  import {
    getSelectedPR,
    selectNextPR,
    selectPrevPR,
    clearSelection,
  } from "./lib/stores/pulls.svelte.js";
  import {
    getSelectedIssue,
    selectNextIssue,
    selectPrevIssue,
    clearIssueSelection,
  } from "./lib/stores/issues.svelte.js";
  import KanbanBoard from "./lib/components/kanban/KanbanBoard.svelte";

  $effect(() => {
    startPolling();
  });

  function handleKeydown(e: KeyboardEvent): void {
    const tag = (e.target as HTMLElement).tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

    const isIssues = getTab() === "issues";

    switch (e.key) {
      case "j":
        e.preventDefault();
        if (isIssues) selectNextIssue();
        else selectNextPR();
        break;
      case "k":
        e.preventDefault();
        if (isIssues) selectPrevIssue();
        else selectPrevPR();
        break;
      case "Escape":
        e.preventDefault();
        if (isIssues) clearIssueSelection();
        else clearSelection();
        break;
      case "1":
        e.preventDefault();
        setView("list");
        break;
      case "2":
        e.preventDefault();
        setView("board");
        break;
    }
  }

  $effect(() => {
    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

<AppHeader />

<main class="app-main">
  {#if getView() === "list"}
    <div class="list-layout">
      <aside class="sidebar">
        {#if getTab() === "pulls"}
          <PullList />
        {:else}
          <IssueList />
        {/if}
      </aside>
      <section class="detail-area" class:detail-area--empty={getTab() === "pulls" ? getSelectedPR() === null : getSelectedIssue() === null}>
        {#if getTab() === "pulls"}
          {#if getSelectedPR() !== null}
            {@const sel = getSelectedPR()!}
            <PullDetail owner={sel.owner} name={sel.name} number={sel.number} />
          {:else}
            <div class="placeholder-content">
              <p class="placeholder-text">Select a PR</p>
              <p class="placeholder-hint">j/k to navigate · Enter to open on GitHub · 1/2 to switch views</p>
            </div>
          {/if}
        {:else}
          {#if getSelectedIssue() !== null}
            {@const sel = getSelectedIssue()!}
            <IssueDetail owner={sel.owner} name={sel.name} number={sel.number} />
          {:else}
            <div class="placeholder-content">
              <p class="placeholder-text">Select an issue</p>
              <p class="placeholder-hint">j/k to navigate · Enter to open on GitHub · 1/2 to switch views</p>
            </div>
          {/if}
        {/if}
      </section>
    </div>
  {:else}
    <div class="board-layout">
      <KanbanBoard />
    </div>
  {/if}
</main>

<StatusBar />

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
    overflow: hidden;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
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
</style>
