<script lang="ts">
  import type { PullRequest, KanbanStatus } from "../../api/types.js";
  import { getPulls, loadPulls } from "../../stores/pulls.svelte.js";
  import { setKanbanState } from "../../api/client.js";
  import {
    loadDetail,
    getDetail,
    isDetailLoading,
    startDetailPolling,
    stopDetailPolling,
  } from "../../stores/detail.svelte.js";
  import PullDetail from "../detail/PullDetail.svelte";
  import KanbanColumn from "./KanbanColumn.svelte";

  let refreshHandle: ReturnType<typeof setInterval> | null = null;

  $effect(() => {
    void loadPulls();
    refreshHandle = setInterval(() => void loadPulls(), 15_000);
    return () => { if (refreshHandle !== null) clearInterval(refreshHandle); };
  });

  function pullsForStatus(status: string): PullRequest[] {
    return getPulls().filter((pr) => (pr.KanbanStatus || "new") === status);
  }

  const columns = [
    { id: "new", title: "New", color: "var(--kanban-new)" },
    { id: "reviewing", title: "Reviewing", color: "var(--accent-amber)" },
    { id: "waiting", title: "Waiting", color: "var(--accent-purple)" },
    { id: "awaiting_merge", title: "Awaiting Merge", color: "var(--accent-green)" },
  ] as const;

  // --- Drawer state ---
  let drawerPR = $state<{ owner: string; name: string; number: number } | null>(null);

  function handleSelect(pr: PullRequest): void {
    drawerPR = {
      owner: pr.repo_owner ?? "",
      name: pr.repo_name ?? "",
      number: pr.Number,
    };
  }

  function closeDrawer(): void {
    drawerPR = null;
    stopDetailPolling();
  }

  // Close drawer on Escape
  $effect(() => {
    if (drawerPR === null) return;
    function onKey(e: KeyboardEvent): void {
      if (e.key === "Escape") { closeDrawer(); e.preventDefault(); }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  // --- Drag and drop ---
  async function handleDrop(
    owner: string,
    name: string,
    number: number,
    status: KanbanStatus,
  ): Promise<void> {
    try {
      await setKanbanState(owner, name, number, status);
    } catch {
      // Card will snap back when pulls refresh
    }
    await loadPulls();
  }
</script>

<div class="kanban-wrap">
  <div class="kanban-board">
    {#each columns as col (col.id)}
      <KanbanColumn
        id={col.id}
        title={col.title}
        color={col.color}
        pulls={pullsForStatus(col.id)}
        onSelect={handleSelect}
        onDrop={handleDrop}
      />
    {/each}
  </div>

  {#if drawerPR !== null}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="drawer-overlay" onclick={closeDrawer} onkeydown={() => {}}></div>
    <aside class="drawer">
      <div class="drawer-header">
        <button class="drawer-close" onclick={closeDrawer} title="Close (Esc)">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
            <path d="M3.72 3.72a.75.75 0 011.06 0L8 6.94l3.22-3.22a.75.75 0 111.06 1.06L9.06 8l3.22 3.22a.75.75 0 11-1.06 1.06L8 9.06l-3.22 3.22a.75.75 0 01-1.06-1.06L6.94 8 3.72 4.78a.75.75 0 010-1.06z"/>
          </svg>
        </button>
      </div>
      <div class="drawer-body">
        <PullDetail
          owner={drawerPR.owner}
          name={drawerPR.name}
          number={drawerPR.number}
        />
      </div>
    </aside>
  {/if}
</div>

<style>
  .kanban-wrap {
    display: flex;
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .kanban-board {
    display: flex;
    flex: 1;
    gap: 12px;
    padding: 16px;
    overflow-x: auto;
    overflow-y: hidden;
    align-items: stretch;
  }

  .drawer-overlay {
    position: absolute;
    inset: 0;
    background: var(--overlay-bg, rgba(0, 0, 0, 0.3));
    z-index: 10;
    animation: fade-in 0.15s ease-out;
  }

  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .drawer {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(520px, 90%);
    background: var(--bg-primary);
    border-left: 1px solid var(--border-default);
    box-shadow: var(--shadow-lg);
    z-index: 11;
    display: flex;
    flex-direction: column;
    animation: slide-in 0.15s ease-out;
  }

  @keyframes slide-in {
    from { transform: translateX(100%); }
    to { transform: translateX(0); }
  }

  .drawer-header {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
  }

  .drawer-close {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    transition: background 0.1s, color 0.1s;
  }

  .drawer-close:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .drawer-body {
    flex: 1;
    overflow-y: auto;
  }
</style>
