<script lang="ts">
  import type { PullRequest } from "../../api/types.js";

  interface Props {
    pr: PullRequest;
    selected: boolean;
    onclick: () => void;
  }

  const { pr, selected, onclick }: Props = $props();

  let el: HTMLButtonElement;

  $effect(() => {
    if (selected && el) {
      el.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  });

  const kanbanLabels: Record<string, string> = {
    new: "New",
    reviewing: "Reviewing",
    waiting: "Waiting",
    awaiting_merge: "Ready",
  };

  function timeAgo(dateStr: string): string {
    const diffMs = Date.now() - new Date(dateStr).getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return `${Math.floor(diffHr / 24)}d ago`;
  }

  const statusLabel = $derived(kanbanLabels[pr.KanbanStatus] ?? pr.KanbanStatus);
  const ago = $derived(timeAgo(pr.LastActivityAt));
</script>

<button class="pull-item" class:selected bind:this={el} onclick={onclick}>
  <p class="title">{pr.Title}</p>
  <div class="meta-row">
    <span class="meta-left">#{pr.Number} · {pr.Author}</span>
    <span class="meta-right">
      <span class="badge badge--{pr.KanbanStatus.replace('_', '-')}">{statusLabel}</span>
      <span class="time">{ago}</span>
    </span>
  </div>
</button>

<style>
  .pull-item {
    display: block;
    width: 100%;
    text-align: left;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border-muted);
    background: var(--bg-surface);
    cursor: pointer;
    transition: background 0.1s;
    border-left: 3px solid transparent;
  }

  .pull-item:hover {
    background: var(--bg-surface-hover);
  }

  .pull-item.selected {
    background: var(--bg-inset);
    border-left-color: var(--accent-blue);
  }

  .title {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin-bottom: 4px;
  }

  .meta-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .meta-left {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .meta-right {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .time {
    font-size: 11px;
    color: var(--text-muted);
  }

  .badge {
    font-size: 10px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 10px;
    white-space: nowrap;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: #fff;
  }

  .badge--new {
    background: var(--accent-blue);
  }

  .badge--reviewing {
    background: var(--accent-amber);
  }

  .badge--waiting {
    background: var(--accent-purple);
  }

  .badge--awaiting-merge {
    background: var(--accent-green);
  }
</style>
