<script lang="ts">
  import type { PullRequest } from "../../api/types.js";
  import { getPulls, loadPulls, selectPR } from "../../stores/pulls.svelte.js";
  import { setView } from "../../stores/router.svelte.js";
  import KanbanColumn from "./KanbanColumn.svelte";

  $effect(() => {
    loadPulls();
  });

  function pullsForStatus(status: string): PullRequest[] {
    return getPulls().filter((pr) => {
      const s = pr.KanbanStatus || "new";
      return s === status;
    });
  }

  const columns = [
    { id: "new", title: "New", color: "var(--kanban-new)" },
    { id: "reviewing", title: "Reviewing", color: "var(--accent-amber)" },
    { id: "waiting", title: "Waiting", color: "var(--accent-purple)" },
    { id: "awaiting_merge", title: "Awaiting Merge", color: "var(--accent-green)" },
  ] as const;

  function handleSelect(pr: PullRequest): void {
    selectPR(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number);
    setView("list");
  }
</script>

<div class="kanban-board">
  {#each columns as col (col.id)}
    <KanbanColumn
      title={col.title}
      color={col.color}
      pulls={pullsForStatus(col.id)}
      onSelect={handleSelect}
    />
  {/each}
</div>

<style>
  .kanban-board {
    display: flex;
    flex: 1;
    gap: 12px;
    padding: 16px;
    overflow-x: auto;
    overflow-y: hidden;
    align-items: stretch;
  }
</style>
