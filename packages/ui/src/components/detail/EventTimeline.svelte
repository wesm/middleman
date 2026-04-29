<script lang="ts">
  import type { IssueEvent, PREvent } from "../../api/types.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { localDateTimeLabel, timeAgo } from "../../utils/time.js";
  import { copyToClipboard } from "../../utils/clipboard.js";

  interface Props {
    events: Array<PREvent | IssueEvent>;
    repoOwner?: string;
    repoName?: string;
    filtered?: boolean;
  }

  const { events, repoOwner, repoName, filtered = false }: Props = $props();

  const typeLabels: Record<string, string> = {
    issue_comment: "Comment",
    review: "Review",
    commit: "Commit",
    force_push: "Force-pushed",
    review_comment: "Review Comment",
  };

  const typeColors: Record<string, string> = {
    issue_comment: "var(--accent-blue)",
    review: "var(--accent-purple)",
    review_comment: "var(--accent-purple)",
    commit: "var(--accent-green)",
    force_push: "var(--accent-red)",
  };

  function shouldRenderMarkdown(eventType: string): boolean {
    return eventType === "issue_comment" || eventType === "review" || eventType === "review_comment";
  }

  function isCompactEvent(eventType: string): boolean {
    return (
      eventType === "commit" ||
      eventType === "force_push" ||
      eventType === "cross_referenced" ||
      eventType === "renamed_title" ||
      eventType === "base_ref_changed"
    );
  }

  function shortCommit(summary: string): string {
    return summary.length > 7 ? summary.slice(0, 7) : summary;
  }

  function commitTitle(body: string): string {
    return body.split(/\r?\n/, 1)[0] ?? "";
  }

  function systemEventLabel(eventType: string): string {
    switch (eventType) {
      case "cross_referenced":
        return "Referenced";
      case "renamed_title":
        return "Title changed";
      case "base_ref_changed":
        return "Base changed";
      case "force_push":
        return "Force-pushed";
      default:
        return typeLabels[eventType] ?? eventType;
    }
  }

  function parseMetadata(event: PREvent | IssueEvent): Record<string, unknown> {
    if (!event.MetadataJSON) return {};
    try {
      const parsed = JSON.parse(event.MetadataJSON) as unknown;
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return {};
      return parsed as Record<string, unknown>;
    } catch {
      return {};
    }
  }

  function metadataString(metadata: Record<string, unknown>, key: string): string | null {
    const value = metadata[key];
    return typeof value === "string" && value.length > 0 ? value : null;
  }

  let copiedId = $state<string | null>(null);
  let copyTimeout: ReturnType<typeof setTimeout> | null = null;

  function copyText(id: string, text: string): void {
    void copyToClipboard(text).then((ok) => {
      if (!ok) return;
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
  <p class="empty">{filtered ? "No activity matches the current filters" : "No activity yet"}</p>
{:else}
  <ol class="timeline">
    {#each events as event (event.ID)}
      <li class={isCompactEvent(event.EventType) ? "event event--compact" : "event"}>
        <div class="event-rail">
          <span
            class="dot"
            style="background: {typeColors[event.EventType] ?? 'var(--text-muted)'}"
          ></span>
          <span class="rail-line"></span>
        </div>
        {#if isCompactEvent(event.EventType)}
          {@const metadata = parseMetadata(event)}
          <div class="event-card event-card--compact">
            <div class="event-header event-header--compact">
              <span
                class="event-type"
                style="color: {typeColors[event.EventType] ?? 'var(--text-muted)'}"
              >
                {systemEventLabel(event.EventType)}
              </span>
              {#if event.Author}
                <span class="event-author">{event.Author}</span>
              {/if}
              <span class="event-time">{localDateTimeLabel(event.CreatedAt)}</span>
              {#if event.EventType === "commit"}
                <span class="commit-sha">{shortCommit(event.Summary)}</span>
                <span class="commit-title">{commitTitle(event.Body)}</span>
              {:else if event.EventType === "cross_referenced"}
                {@const sourceUrl = metadataString(metadata, "source_url")}
                {@const sourceTitle = metadataString(metadata, "source_title") ?? event.Summary}
                {#if sourceUrl}
                  <a
                    class="system-event-link"
                    href={sourceUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {sourceTitle}
                  </a>
                {:else}
                  <span class="system-event-summary">{sourceTitle}</span>
                {/if}
              {:else}
                <span class="system-event-summary">{event.Summary}</span>
              {/if}
            </div>
          </div>
        {:else}
          <div class="event-card">
            <div class="event-header">
              <span
                class="event-type"
                style="color: {typeColors[event.EventType] ?? 'var(--text-muted)'}"
              >
                {typeLabels[event.EventType] ?? event.EventType}
              </span>
              {#if event.Author}
                <span class="event-author">{event.Author}</span>
              {/if}
              <span class="event-time">{timeAgo(event.CreatedAt)}</span>
            </div>
            {#if event.Summary && (event.EventType === "commit" || event.EventType === "force_push")}
              <p class="event-summary">{event.Summary}</p>
            {/if}
            {#if event.Body}
              <div class="event-body-wrap">
                <button
                  class="copy-icon-btn"
                  class:copied={copiedId === String(event.ID)}
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
                    {@html renderMarkdown(event.Body, repoOwner && repoName ? { owner: repoOwner, name: repoName } : undefined)}
                  {:else}
                    {event.Body}
                  {/if}
                </div>
              </div>
            {/if}
          </div>
        {/if}
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
    gap: 0;
  }

  /* Left rail: dot + connector line */
  .event-rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 24px;
    flex-shrink: 0;
    padding-top: 14px;
  }

  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
    z-index: 1;
    box-shadow: 0 0 0 3px var(--bg-primary);
  }

  .rail-line {
    width: 2px;
    flex: 1;
    background: var(--border-default);
    margin-top: 2px;
  }

  .event:last-child .rail-line {
    display: none;
  }

  /* Right side: card */
  .event-card {
    flex: 1;
    min-width: 0;
    background: var(--bg-surface);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-md);
    padding: 10px 12px;
    margin: 4px 0 4px 8px;
  }

  .event-card--compact {
    padding: 7px 10px;
  }

  .event-header {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .event-header--compact {
    min-width: 0;
    flex-wrap: nowrap;
  }

  .event-header--compact .event-time {
    margin-left: 0;
  }

  .event-type {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
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
    margin-top: 4px;
    font-family: var(--font-mono);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .commit-sha {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text-secondary);
  }

  .commit-title,
  .system-event-summary,
  .system-event-link {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .commit-title {
    flex: 1;
    color: var(--text-primary);
  }

  .system-event-summary,
  .system-event-link {
    flex: 1;
    font-size: 12px;
  }

  .system-event-summary {
    color: var(--text-secondary);
  }

  .system-event-link {
    color: var(--accent-blue);
    text-decoration: none;
  }

  .system-event-link:hover {
    text-decoration: underline;
  }

  /* Body wrap for copy button positioning */
  .event-body-wrap {
    position: relative;
    margin-top: 8px;
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

  .copy-icon-btn.copied {
    opacity: 1;
    color: var(--accent-green);
    background: color-mix(in srgb, var(--accent-green) 12%, transparent);
  }

  @media (hover: none) {
    .copy-icon-btn {
      opacity: 1;
    }
  }

  .event-body {
    font-size: 12px;
    color: var(--text-primary);
    padding: 8px 36px 8px 10px;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.6;
  }

  .event-body.markdown-body {
    white-space: normal;
  }
</style>
