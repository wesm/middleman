<script lang="ts">
  import {
    getIssueDetail,
    isIssueDetailLoading,
    getIssueDetailError,
    loadIssueDetail,
    startIssueDetailPolling,
    stopIssueDetailPolling,
    toggleIssueStar,
  } from "../../stores/issues.svelte.js";
  import type { IssueLabel } from "../../api/types.js";
  import { renderMarkdown } from "../../utils/markdown.js";
  import { timeAgo } from "../../utils/time.js";
  import EventTimeline from "./EventTimeline.svelte";
  import IssueCommentBox from "./IssueCommentBox.svelte";

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  $effect(() => {
    void loadIssueDetail(owner, name, number);
    startIssueDetailPolling(owner, name, number);
    return () => stopIssueDetailPolling();
  });

  let copied = $state(false);
  let copyTimeout: ReturnType<typeof setTimeout> | null = null;

  function copyBody(text: string): void {
    void navigator.clipboard.writeText(text).then(() => {
      copied = true;
      if (copyTimeout !== null) clearTimeout(copyTimeout);
      copyTimeout = setTimeout(() => {
        copied = false;
        copyTimeout = null;
      }, 1500);
    });
  }

  function parseLabels(json: string): IssueLabel[] {
    if (!json) return [];
    try {
      return JSON.parse(json) as IssueLabel[];
    } catch {
      return [];
    }
  }

  function labelColor(color: string): string {
    if (!color) return "#666";
    return color.startsWith("#") ? color : `#${color}`;
  }

  function handleStarClick(): void {
    const detail = getIssueDetail();
    if (!detail) return;
    void toggleIssueStar(
      owner,
      name,
      number,
      detail.issue.Starred,
    );
  }
</script>

{#if isIssueDetailLoading()}
  <div class="state-center"><p class="state-msg">Loading...</p></div>
{:else if getIssueDetailError() !== null && getIssueDetail() === null}
  <div class="state-center"><p class="state-msg state-msg--error">Error: {getIssueDetailError()}</p></div>
{:else}
  {@const detail = getIssueDetail()}
  {#if detail !== null}
    {@const issue = detail.issue}
    {@const labels = parseLabels(issue.LabelsJSON)}
    <div class="issue-detail">
      <!-- Header -->
      <div class="detail-header">
        <h2 class="detail-title">{issue.Title}</h2>
        <button
          class="star-btn"
          onclick={handleStarClick}
          title={issue.Starred ? "Unstar" : "Star"}
        >
          {#if issue.Starred}
            <svg class="star-detail-icon star-detail-icon--active" width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
              <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25z"/>
            </svg>
          {:else}
            <svg class="star-detail-icon" width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
              <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25zm0 2.445L6.615 5.5a.75.75 0 01-.564.41l-3.097.45 2.24 2.184a.75.75 0 01.216.664l-.528 3.084 2.769-1.456a.75.75 0 01.698 0l2.77 1.456-.53-3.084a.75.75 0 01.216-.664l2.24-2.183-3.096-.45a.75.75 0 01-.564-.41L8 2.694z"/>
            </svg>
          {/if}
        </button>
        <a class="gh-link" href={issue.URL} target="_blank" rel="noopener noreferrer" title="Open on GitHub">
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M6 3H3a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
            <path d="M10 2h4v4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M8 8L14 2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
          </svg>
        </a>
      </div>

      <!-- Meta row -->
      <div class="meta-row">
        <span class="meta-item">{detail.repo_owner}/{detail.repo_name}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">#{issue.Number}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">{issue.Author}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item">{timeAgo(issue.CreatedAt)}</span>
        <span class="meta-sep">·</span>
        <span class="meta-item chip chip--{issue.State}">
          {issue.State === "open" ? "Open" : "Closed"}
        </span>
      </div>

      <!-- Labels -->
      {#if labels.length > 0}
        <div class="labels-row">
          {#each labels as label}
            <span
              class="label-pill"
              style="background: {labelColor(label.color)}; color: #fff;"
            >{label.name}</span>
          {/each}
        </div>
      {/if}

      <!-- Issue body -->
      {#if issue.Body}
        <div class="section body-section">
          <div class="section-header">
            <span class="section-title-inline">Description</span>
          </div>
          <div class="inset-box-wrap">
            <button
              class="copy-icon-btn"
              onclick={() => copyBody(issue.Body)}
              title={copied ? "Copied!" : "Copy to clipboard"}
            >
              {#if copied}
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
            <div class="inset-box markdown-body">{@html renderMarkdown(issue.Body)}</div>
          </div>
        </div>
      {/if}

      <!-- Comment box -->
      <div class="section">
        <IssueCommentBox {owner} {name} {number} />
      </div>

      <!-- Activity -->
      <div class="section">
        <h3 class="section-title">Activity</h3>
        <EventTimeline events={detail.events as any} />
      </div>
    </div>
  {/if}
{/if}

<style>
  .state-center {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
  }

  .state-msg {
    font-size: 13px;
    color: var(--text-muted);
  }

  .state-msg--error {
    color: var(--accent-red);
  }

  .issue-detail {
    padding: 20px 24px;
    max-width: 800px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .detail-header {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }

  .detail-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    line-height: 1.35;
    flex: 1;
    min-width: 0;
  }

  .star-btn {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    margin-top: 3px;
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
  }

  .star-detail-icon {
    color: var(--text-muted);
    transition: color 0.1s;
  }

  .star-detail-icon:hover {
    color: var(--accent-amber);
  }

  .star-detail-icon--active {
    color: var(--accent-amber);
  }

  .gh-link {
    flex-shrink: 0;
    color: var(--text-muted);
    display: flex;
    align-items: center;
    margin-top: 3px;
    transition: color 0.1s;
  }

  .gh-link:hover {
    color: var(--accent-blue);
    text-decoration: none;
  }

  .meta-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
  }

  .meta-item {
    font-size: 12px;
    color: var(--text-secondary);
  }

  .meta-sep {
    font-size: 12px;
    color: var(--text-muted);
  }

  .chip {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 10px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    white-space: nowrap;
  }

  .chip--open {
    background: color-mix(in srgb, var(--accent-green) 15%, transparent);
    color: var(--accent-green);
  }

  .chip--closed {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }

  .labels-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .label-pill {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 10px;
    white-space: nowrap;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .section-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .section-title-inline {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .inset-box-wrap {
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

  .inset-box-wrap:hover .copy-icon-btn,
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

  .inset-box {
    font-size: 13px;
    color: var(--text-primary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    word-break: break-word;
    line-height: 1.6;
  }

</style>
