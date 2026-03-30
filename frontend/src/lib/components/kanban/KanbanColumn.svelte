<script lang="ts">
  import type { PullRequest } from "../../api/types.js";
  import KanbanCard from "./KanbanCard.svelte";

  interface Props {
    title: string;
    color: string;
    pulls: PullRequest[];
    onSelect: (pr: PullRequest) => void;
  }

  const { title, color, pulls, onSelect }: Props = $props();
</script>

<div class="kanban-column">
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
    overflow: hidden;
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
