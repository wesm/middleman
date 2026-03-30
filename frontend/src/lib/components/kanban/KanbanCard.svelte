<script lang="ts">
  import type { PullRequest } from "../../api/types.js";

  interface Props {
    pr: PullRequest;
    onclick: () => void;
  }

  const { pr, onclick }: Props = $props();

  function timeAgo(dateStr: string): string {
    const diffMs = Date.now() - new Date(dateStr).getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return `${Math.floor(diffHr / 24)}d ago`;
  }

  const ago = $derived(timeAgo(pr.LastActivityAt));
  const repoLabel = $derived(
    pr.repo_owner && pr.repo_name
      ? `${pr.repo_owner}/${pr.repo_name}`
      : `#${pr.Number}`
  );
</script>

<button class="kanban-card" {onclick}>
  <p class="card-title">{pr.Title}</p>
  <p class="card-meta">{repoLabel} #{pr.Number}</p>
  <div class="card-footer">
    <span class="card-author">{pr.Author}</span>
    <span class="card-time">{ago}</span>
  </div>
</button>

<style>
  .kanban-card {
    display: block;
    width: 100%;
    text-align: left;
    padding: 10px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: box-shadow 0.15s, border-color 0.15s;
    box-shadow: var(--shadow-sm);
  }

  .kanban-card:hover {
    box-shadow: var(--shadow-md);
    border-color: var(--border-default);
  }

  .card-title {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
    line-height: 1.4;
    margin-bottom: 4px;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .card-meta {
    font-size: 11px;
    color: var(--text-muted);
    margin-bottom: 8px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .card-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .card-author {
    font-size: 11px;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .card-time {
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
</style>
