<script lang="ts">
  import type { PREvent } from "../../api/types.js";

  interface Props {
    events: PREvent[];
  }

  const { events }: Props = $props();

  const typeLabels: Record<string, string> = {
    issue_comment: "Comment",
    review: "Review",
    commit: "Commit",
    review_comment: "Review Comment",
  };

  function dotColor(eventType: string): string {
    if (eventType === "issue_comment") return "var(--accent-blue)";
    if (eventType === "review" || eventType === "review_comment") return "var(--accent-purple)";
    if (eventType === "commit") return "var(--accent-green)";
    return "var(--text-muted)";
  }

  function timeAgo(dateStr: string): string {
    const diffMs = Date.now() - new Date(dateStr).getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return `${Math.floor(diffHr / 24)}d ago`;
  }
</script>

{#if events.length === 0}
  <p class="empty">No activity yet</p>
{:else}
  <ol class="timeline">
    {#each events as event (event.ID)}
      <li class="event">
        <span class="dot" style="background: {dotColor(event.EventType)}"></span>
        <div class="event-content">
          <div class="event-header">
            <span class="event-type">{typeLabels[event.EventType] ?? event.EventType}</span>
            {#if event.Author}
              <span class="event-author">{event.Author}</span>
            {/if}
            <span class="event-time">{timeAgo(event.CreatedAt)}</span>
          </div>
          {#if event.Summary}
            <p class="event-summary">{event.Summary}</p>
          {/if}
          {#if event.Body}
            <div class="event-body">{event.Body}</div>
          {/if}
        </div>
      </li>
    {/each}
  </ol>
{/if}

<style>
  .empty {
    font-size: 13px;
    color: var(--text-muted);
    padding: 16px 0;
  }

  .timeline {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0;
  }

  .event {
    display: flex;
    gap: 12px;
    padding: 10px 0;
    position: relative;
  }

  .event:not(:last-child)::after {
    content: "";
    position: absolute;
    left: 5px;
    top: 26px;
    bottom: -2px;
    width: 2px;
    background: var(--border-muted);
  }

  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    flex-shrink: 0;
    margin-top: 3px;
  }

  .event-content {
    flex: 1;
    min-width: 0;
  }

  .event-header {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 4px;
  }

  .event-type {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-secondary);
  }

  .event-author {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-primary);
  }

  .event-time {
    font-size: 11px;
    color: var(--text-muted);
    margin-left: auto;
  }

  .event-summary {
    font-size: 12px;
    color: var(--text-secondary);
    margin-bottom: 4px;
  }

  .event-body {
    font-size: 12px;
    color: var(--text-primary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.5;
  }
</style>
