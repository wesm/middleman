<script lang="ts">
  import type { PREvent } from "../../api/types.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { timeAgo } from "../../utils/time.js";

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
          </div>
          {#if event.Summary}
            <p class="event-summary">{event.Summary}</p>
          {/if}
          {#if event.Body}
            <div class="event-body-wrap">
              <button
                class="copy-icon-btn"
                onclick={() => copyText(String(event.ID), event.Body)}
                title={copiedId === String(event.ID) ? "Copied!" : "Copy to clipboard"}
              >
                {#if copiedId === String(event.ID)}
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                    <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
                  </svg>
                {:else}
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                    <path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 010 1.5h-1.5a.25.25 0 00-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 00.25-.25v-1.5a.75.75 0 011.5 0v1.5A1.75 1.75 0 019.25 16h-7.5A1.75 1.75 0 010 14.25v-7.5z"/>
                    <path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0114.25 11h-7.5A1.75 1.75 0 015 9.25v-7.5zm1.75-.25a.25.25 0 00-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 00.25-.25v-7.5a.25.25 0 00-.25-.25h-7.5z"/>
                  </svg>
                {/if}
              </button>
              <div class="event-body {shouldRenderMarkdown(event.EventType) ? 'markdown-body' : ''}">
                {#if shouldRenderMarkdown(event.EventType)}
                  {@html renderMarkdown(event.Body)}
                {:else}
                  {event.Body}
                {/if}
              </div>
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
    bottom: 0;
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

  .event-body-wrap {
    position: relative;
  }

  .copy-icon-btn {
    position: absolute;
    top: 6px;
    right: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    opacity: 0;
    transition: opacity 0.15s, background 0.15s, color 0.15s;
    z-index: 1;
  }

  .event-body-wrap:hover .copy-icon-btn,
  .copy-icon-btn:focus-visible {
    opacity: 1;
  }

  .copy-icon-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-secondary);
  }

  .copy-icon-btn:active {
    transform: scale(0.92);
  }

  @media (hover: none) {
    .copy-icon-btn {
      opacity: 1;
    }
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
</style>
