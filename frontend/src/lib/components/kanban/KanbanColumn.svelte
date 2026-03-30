<script lang="ts">
  import type { PullRequest, KanbanStatus } from "../../api/types.js";
  import KanbanCard from "./KanbanCard.svelte";

  interface Props {
    id: string;
    title: string;
    color: string;
    pulls: PullRequest[];
    onSelect: (pr: PullRequest) => void;
    onDrop: (owner: string, name: string, number: number, status: KanbanStatus) => void;
  }

  const { id, title, color, pulls, onSelect, onDrop }: Props = $props();

  let dragOver = $state(false);

  function handleDragOver(e: DragEvent): void {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dragOver = true;
  }

  function handleDragLeave(): void {
    dragOver = false;
  }

  function handleDrop(e: DragEvent): void {
    e.preventDefault();
    dragOver = false;
    if (!e.dataTransfer) return;
    try {
      const data = JSON.parse(e.dataTransfer.getData("text/plain")) as {
        owner: string;
        name: string;
        number: number;
      };
      onDrop(data.owner, data.name, data.number, id as KanbanStatus);
    } catch {
      // Invalid drag data
    }
  }
</script>

<div
  class="kanban-column"
  class:drag-over={dragOver}
  ondragover={handleDragOver}
  ondragleave={handleDragLeave}
  ondrop={handleDrop}
  role="group"
  aria-label="{title} column"
>
  <div class="column-header">
    <span class="column-title" style="color: {color}">{title}</span>
    <span class="column-count">{pulls.length}</span>
  </div>
  <div class="column-body">
    {#if pulls.length === 0}
      <p class="empty-state">No PRs</p>
    {:else}
      {#each pulls as pr (pr.ID)}
        <KanbanCard {pr} onclick={() => onSelect(pr)} />
      {/each}
    {/if}
  </div>
</div>

<style>
  .kanban-column {
    min-width: 260px;
    max-width: 360px;
    flex: 1;
    display: flex;
    flex-direction: column;
    background: var(--bg-inset);
    border-radius: var(--radius-lg);
    border: 2px solid transparent;
    overflow: hidden;
    transition: border-color 0.15s, background 0.15s;
  }

  .kanban-column.drag-over {
    border-color: var(--accent-blue);
    background: color-mix(in srgb, var(--accent-blue) 5%, var(--bg-inset));
  }

  .column-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border-muted);
  }

  .column-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .column-count {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    background: var(--bg-surface);
    border: 1px solid var(--border-muted);
    border-radius: 10px;
    padding: 1px 7px;
    min-width: 22px;
    text-align: center;
  }

  .column-body {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .empty-state {
    font-size: 12px;
    color: var(--text-muted);
    text-align: center;
    padding: 24px 0;
  }
</style>
