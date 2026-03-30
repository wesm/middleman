<script lang="ts">
  import type { Issue, IssueLabel } from "../../api/types.js";
  import { toggleIssueStar } from "../../stores/issues.svelte.js";

  interface Props {
    issue: Issue;
    selected: boolean;
    onclick: () => void;
  }

  const { issue, selected, onclick }: Props = $props();

  let el: HTMLButtonElement;

  $effect(() => {
    if (selected && el) {
      el.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  });

  function handleStarClick(e: MouseEvent): void {
    e.stopPropagation();
    void toggleIssueStar(
      issue.repo_owner ?? "",
      issue.repo_name ?? "",
      issue.Number,
      issue.Starred,
    );
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

  function timeAgo(dateStr: string): string {
    const diffMs = Date.now() - new Date(dateStr).getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return `${Math.floor(diffHr / 24)}d ago`;
  }

  const labels = $derived(parseLabels(issue.LabelsJSON));
  const visibleLabels = $derived(labels.slice(0, 2));
  const extraCount = $derived(Math.max(0, labels.length - 2));
  const ago = $derived(timeAgo(issue.LastActivityAt));
  const stateLabel = $derived(
    issue.State === "open" ? "Open" : "Closed",
  );
</script>

<button class="issue-item" class:selected bind:this={el} onclick={onclick}>
  <p class="title">{issue.Title}</p>
  {#if visibleLabels.length > 0}
    <div class="labels-row">
      {#each visibleLabels as label}
        <span
          class="label-pill"
          style="background: {labelColor(label.color)}; color: #fff;"
        >{label.name}</span>
      {/each}
      {#if extraCount > 0}
        <span class="label-more">+{extraCount}</span>
      {/if}
    </div>
  {/if}
  <div class="meta-row">
    <span class="meta-left">#{issue.Number} · {issue.Author}</span>
    <span class="meta-right">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      {#if issue.Starred}
        <svg class="star-icon star-icon--active" width="12" height="12" viewBox="0 0 16 16" fill="currentColor" onclick={handleStarClick} role="button" tabindex="-1">
          <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25z"/>
        </svg>
      {:else}
        <svg class="star-icon" width="12" height="12" viewBox="0 0 16 16" fill="currentColor" onclick={handleStarClick} role="button" tabindex="-1">
          <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25zm0 2.445L6.615 5.5a.75.75 0 01-.564.41l-3.097.45 2.24 2.184a.75.75 0 01.216.664l-.528 3.084 2.769-1.456a.75.75 0 01.698 0l2.77 1.456-.53-3.084a.75.75 0 01.216-.664l2.24-2.183-3.096-.45a.75.75 0 01-.564-.41L8 2.694z"/>
        </svg>
      {/if}
      <span class="badge badge--{issue.State}">{stateLabel}</span>
      <span class="time">{ago}</span>
    </span>
  </div>
</button>

<style>
  .issue-item {
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

  .issue-item:hover {
    background: var(--bg-surface-hover);
  }

  .issue-item.selected {
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

  .labels-row {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 4px;
    overflow: hidden;
  }

  .label-pill {
    font-size: 10px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 10px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 120px;
    line-height: 1.5;
  }

  .label-more {
    font-size: 10px;
    color: var(--text-muted);
    flex-shrink: 0;
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
  }

  .badge--open {
    background: color-mix(in srgb, var(--accent-green) 18%, transparent);
    color: var(--accent-green);
  }

  .badge--closed {
    background: color-mix(in srgb, var(--accent-purple) 18%, transparent);
    color: var(--accent-purple);
  }

  .star-icon {
    color: var(--text-muted);
    opacity: 0;
    transition: opacity 0.15s, color 0.1s;
    cursor: pointer;
    flex-shrink: 0;
  }

  .issue-item:hover .star-icon {
    opacity: 0.6;
  }

  .star-icon:hover {
    opacity: 1 !important;
    color: var(--accent-amber);
  }

  .star-icon--active {
    color: var(--accent-amber);
    opacity: 1;
  }
</style>
