<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import type { PullRequest, KanbanStatus } from "../../api/types.js";
  import { providerItemPath, providerRouteParams } from "../../api/provider-routes.js";
  import { getStores, getClient, getNavigate, getSidebar } from "../../context.js";
  import DetailDrawer from "../DetailDrawer.svelte";
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

  // --- Drag and drop ---
  async function handleDrop(
    owner: string,
    name: string,
    number: number,
    status: KanbanStatus,
  ): Promise<void> {
    const ref = { provider: "github", platformHost: "github.com", owner, name };
    try {
      const { error } = await client.PUT(providerItemPath("pulls", ref, "/state"), {
        params: { path: { ...providerRouteParams(ref), number } },
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
    <DetailDrawer
      itemType="pr"
      owner={drawerPR.owner}
      name={drawerPR.name}
      number={drawerPR.number}
      onClose={closeDrawer}
      onPullsRefresh={() => pulls.loadPulls({ state: "open" })}
    />
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
</style>
