<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import type { PullRequest, KanbanStatus } from "../../api/types.js";
  import { getStores, getClient, getNavigate, getSidebar } from "../../context.js";
  import PullDetail from "../detail/PullDetail.svelte";
  import KanbanColumn from "./KanbanColumn.svelte";

  const { pulls, detail, settings } = getStores();
  const client = getClient();
  const navigate = getNavigate();
  const { isEmbedded } = getSidebar();

  let refreshHandle: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    void pulls.loadPulls({ state: "open" });
    refreshHandle = setInterval(() => void pulls.loadPulls({ state: "open" }), 15_000);
  });

  onDestroy(() => {
    if (refreshHandle !== null) clearInterval(refreshHandle);
    void pulls.loadPulls();
  });

  function pullsForStatus(status: string): PullRequest[] {
    return pulls.getPulls().filter((pr) => (pr.KanbanStatus || "new") === status);
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
    detail.stopDetailPolling();
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
      const { error } = await client.PUT("/repos/{owner}/{name}/pulls/{number}/state", {
        params: { path: { owner, name, number } },
        body: { status },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to update kanban state");
      }
    } catch {
      // Card will snap back when pulls refresh
    }
    await pulls.loadPulls({ state: "open" });
  }
</script>

<div class="kanban-wrap">
  {#if settings.isSettingsLoaded() && !settings.hasConfiguredRepos()}
    <div class="empty-state">No repositories configured.<br />
      {#if !isEmbedded()}<button class="settings-link" onclick={() => navigate("/settings")}>Add one in Settings</button>{/if}
    </div>
  {:else}
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
  {/if}

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
          onPullsRefresh={() => pulls.loadPulls({ state: "open" })}
        />
      </div>
    </aside>
  {/if}
</div>

<style>
  .kanban-wrap {
    display: flex;
    flex-direction: column;
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

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    flex: 1;
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
  }

  .settings-link {
    color: var(--accent-blue);
    cursor: pointer;
    font-size: 13px;
    margin-top: 4px;
  }

  .settings-link:hover {
    text-decoration: underline;
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
    width: 65%;
    min-width: 500px;
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

  :global(#app.container-narrow) .drawer,
  :global(#app.container-medium) .drawer {
    width: 100%;
    min-width: 0;
  }
</style>
