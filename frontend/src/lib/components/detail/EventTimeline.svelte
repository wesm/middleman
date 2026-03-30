<script lang="ts">
  import type { PREvent } from "../../api/types.js";
  import { renderMarkdown } from "../../utils/markdown.js";

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

  function shouldRenderMarkdown(eventType: string): boolean {
    return eventType === "issue_comment" || eventType === "review" || eventType === "review_comment";
  }

  let copiedId = $state<string | null>(null);
  let copyTimeout: ReturnType<typeof setTimeout> | null = null;

  function copyText(id: string, text: string): void {
    void navigator.clipboard.writeText(text).then(() => {
      copiedId = id;
      if (copyTimeout !== null) clearTimeout(copyTimeout);
      copyTimeout = setTimeout(() => {
        copiedId = null;
        copyTimeout = null;
      }, 1500);
    });
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
            {#if event.Body}
              <button
                class="copy-btn"
                onclick={() => copyText(event.ID, event.Body)}
                title="Copy to clipboard"
              >
                {copiedId === event.ID ? "Copied!" : "Copy"}
              </button>
            {/if}
          </div>
          {#if event.Summary}
            <p class="event-summary">{event.Summary}</p>
          {/if}
          {#if event.Body}
            <div class="event-body {shouldRenderMarkdown(event.EventType) ? 'markdown-body' : ''}">
              {#if shouldRenderMarkdown(event.EventType)}
                {@html renderMarkdown(event.Body)}
              {:else}
                {event.Body}
              {/if}
            </div>
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

  .copy-btn {
    font-size: 11px;
    font-weight: 500;
    padding: 1px 6px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-default);
    background: var(--bg-surface);
    color: var(--text-secondary);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s, color 0.1s, border-color 0.1s;
  }

  .event:hover .copy-btn {
    opacity: 1;
  }

  .copy-btn:hover {
    color: var(--accent-blue);
    border-color: var(--accent-blue);
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

  .event-body.markdown-body {
    white-space: normal;
  }

  .markdown-body :global(p) {
    margin-bottom: 0.5em;
  }
  .markdown-body :global(p:last-child) {
    margin-bottom: 0;
  }
  .markdown-body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.9em;
    background: var(--bg-surface-hover);
    padding: 1px 4px;
    border-radius: 3px;
  }
  .markdown-body :global(pre) {
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    overflow-x: auto;
    margin: 6px 0;
  }
  .markdown-body :global(pre code) {
    background: none;
    padding: 0;
  }
  .markdown-body :global(a) {
    color: var(--accent-blue);
  }
  .markdown-body :global(ul), .markdown-body :global(ol) {
    padding-left: 1.5em;
    margin-bottom: 0.5em;
  }
  .markdown-body :global(blockquote) {
    border-left: 3px solid var(--border-default);
    padding-left: 10px;
    color: var(--text-secondary);
    margin: 6px 0;
  }
  .markdown-body :global(h1), .markdown-body :global(h2), .markdown-body :global(h3) {
    font-size: 1em;
    font-weight: 600;
    margin: 8px 0 4px;
  }
  .markdown-body :global(img) {
    max-width: 100%;
    border-radius: var(--radius-sm);
  }
</style>
