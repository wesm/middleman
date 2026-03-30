<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import type { ActivityItem } from "../api/activity.js";
  import {
    getActivityItems,
    isActivityLoading,
    getActivityError,
    hasMoreActivity,
    getActivityFilterRepo,
    getActivityFilterTypes,
    getActivitySearch,
    setActivityFilterRepo,
    setActivityFilterTypes,
    setActivitySearch,
    loadActivity,
    loadMoreActivity,
    startActivityPolling,
    stopActivityPolling,
    syncFromURL,
    syncToURL,
  } from "../stores/activity.svelte.js";
  import RepoSelector from "./sidebar/RepoSelector.svelte";

  interface Props {
    onSelectItem?: (item: ActivityItem) => void;
  }

  let { onSelectItem }: Props = $props();

  let searchInput = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  type ItemFilter = "all" | "prs" | "issues";
  let itemFilter = $state<ItemFilter>("all");

  const EVENT_TYPES = ["comment", "review", "commit"] as const;

  const EVENT_LABELS: Record<string, string> = {
    comment: "Comments",
    review: "Reviews",
    commit: "Commits",
  };

  let enabledEvents = $state<Set<string>>(new Set(EVENT_TYPES));

  onMount(() => {
    syncFromURL();
    searchInput = getActivitySearch() ?? "";
    restoreFiltersFromStore();
    void loadActivity();
    startActivityPolling();
  });

  onDestroy(() => {
    stopActivityPolling();
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  function restoreFiltersFromStore(): void {
    const types = getActivityFilterTypes();
    if (types.length === 0) {
      itemFilter = "all";
      enabledEvents = new Set(EVENT_TYPES);
      return;
    }
    const hasPR = types.includes("new_pr");
    const hasIssue = types.includes("new_issue");
    if (hasPR && !hasIssue) itemFilter = "prs";
    else if (hasIssue && !hasPR) itemFilter = "issues";
    else itemFilter = "all";
    enabledEvents = new Set(EVENT_TYPES.filter((t) => types.includes(t)));
  }

  function applyFilters(): void {
    const types: string[] = [];
    if (itemFilter === "prs") {
      types.push("new_pr");
    } else if (itemFilter === "issues") {
      types.push("new_issue");
    } else {
      types.push("new_pr", "new_issue");
    }
    for (const evt of enabledEvents) {
      types.push(evt);
    }
    const allSelected = itemFilter === "all"
      && enabledEvents.size === EVENT_TYPES.length;
    setActivityFilterTypes(allSelected ? [] : types);
    syncToURL();
    void loadActivity();
  }

  function setItemFilter(f: ItemFilter): void {
    itemFilter = f;
    applyFilters();
  }

  function toggleEvent(evt: string): void {
    const next = new Set(enabledEvents);
    if (next.has(evt)) {
      if (next.size > 1) next.delete(evt);
    } else {
      next.add(evt);
    }
    enabledEvents = next;
    applyFilters();
  }

  function handleRepoChange(repo: string | undefined): void {
    setActivityFilterRepo(repo);
    syncToURL();
    void loadActivity();
  }

  function handleSearchInput(e: Event): void {
    const val = (e.target as HTMLInputElement).value;
    searchInput = val;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      setActivitySearch(val || undefined);
      syncToURL();
      void loadActivity();
    }, 300);
  }

  function eventLabel(item: ActivityItem): string {
    switch (item.activity_type) {
      case "new_pr": return "Opened";
      case "new_issue": return "Opened";
      case "comment": return "Comment";
      case "review": return "Review";
      case "commit": return "Commit";
      default: return item.activity_type;
    }
  }

  function itemTypeLabel(item: ActivityItem): string {
    return item.item_type === "pr" ? "PR" : "Issue";
  }

  function badgeClass(item: ActivityItem): string {
    return item.item_type === "pr" ? "badge-pr" : "badge-issue";
  }

  function eventClass(type: string): string {
    switch (type) {
      case "comment": return "evt-comment";
      case "review": return "evt-review";
      case "commit": return "evt-commit";
      default: return "";
    }
  }

  function relativeTime(iso: string): string {
    const diff = Date.now() - new Date(iso).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return "just now";
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    return new Date(iso).toLocaleDateString();
  }

  function handleRowClick(item: ActivityItem): void {
    onSelectItem?.(item);
  }

  function handleLinkClick(e: Event, url: string): void {
    e.stopPropagation();
    window.open(url, "_blank", "noopener");
  }
</script>

<div class="activity-feed">
  <div class="controls-bar">
    <RepoSelector
      selected={getActivityFilterRepo()}
      onchange={handleRepoChange}
    />

    <div class="filter-group">
      <div class="segmented-control">
        <button class="seg-btn" class:active={itemFilter === "all"} onclick={() => setItemFilter("all")}>All</button>
        <button class="seg-btn" class:active={itemFilter === "prs"} onclick={() => setItemFilter("prs")}>PRs</button>
        <button class="seg-btn" class:active={itemFilter === "issues"} onclick={() => setItemFilter("issues")}>Issues</button>
      </div>

      <div class="event-toggles">
        {#each EVENT_TYPES as evt}
          <button
            class="evt-toggle"
            class:active={enabledEvents.has(evt)}
            onclick={() => toggleEvent(evt)}
          >
            <span class="evt-dot {eventClass(evt)}"></span>
            {EVENT_LABELS[evt]}
          </button>
        {/each}
      </div>
    </div>

    <input
      class="search-input"
      type="text"
      placeholder="Search..."
      value={searchInput}
      oninput={handleSearchInput}
    />
  </div>

  {#if getActivityError()}
    <div class="error-banner">{getActivityError()}</div>
  {/if}

  <div class="table-container">
    <table class="activity-table">
      <thead>
        <tr>
          <th class="col-kind">Kind</th>
          <th class="col-event">Event</th>
          <th class="col-repo">Repository</th>
          <th class="col-item">Item</th>
          <th class="col-author">Author</th>
          <th class="col-when">When</th>
          <th class="col-link"></th>
        </tr>
      </thead>
      <tbody>
        {#each getActivityItems() as item (item.id)}
          <tr class="activity-row" onclick={() => handleRowClick(item)}>
            <td class="col-kind">
              <span class="badge {badgeClass(item)}">{itemTypeLabel(item)}</span>
            </td>
            <td class="col-event">
              <span class="evt-label {eventClass(item.activity_type)}">{eventLabel(item)}</span>
            </td>
            <td class="col-repo">{item.repo_owner}/{item.repo_name}</td>
            <td class="col-item">
              <span class="item-number">#{item.item_number}</span>
              <span class="item-title">{item.item_title}</span>
            </td>
            <td class="col-author">{item.author}</td>
            <td class="col-when">{relativeTime(item.created_at)}</td>
            <td class="col-link">
              <button
                class="link-btn"
                title="Open on GitHub"
                onclick={(e) => handleLinkClick(e, item.item_url)}
              >&#x2197;</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if getActivityItems().length === 0 && !isActivityLoading()}
      <div class="empty-state">No activity found</div>
    {/if}
  </div>

  {#if hasMoreActivity()}
    <div class="load-more">
      <button class="load-more-btn" onclick={() => void loadMoreActivity()} disabled={isActivityLoading()}>
        {isActivityLoading() ? "Loading..." : "Load more"}
      </button>
    </div>
  {/if}
</div>

<style>
  .activity-feed {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .controls-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .filter-group {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .segmented-control {
    display: flex;
    align-items: center;
    gap: 1px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    padding: 2px;
  }

  .seg-btn {
    padding: 3px 10px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    border-radius: calc(var(--radius-sm) - 1px);
    transition: background 0.12s, color 0.12s;
  }

  .seg-btn.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: var(--shadow-sm);
  }

  .seg-btn:hover:not(.active) {
    color: var(--text-secondary);
  }

  .event-toggles {
    display: flex;
    gap: 2px;
  }

  .evt-toggle {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    font-size: 11px;
    color: var(--text-muted);
    transition: color 0.12s, opacity 0.12s;
  }

  .evt-toggle.active {
    color: var(--text-primary);
  }

  .evt-toggle:not(.active) {
    opacity: 0.4;
  }

  .evt-toggle:hover {
    opacity: 1;
  }

  .evt-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .evt-dot.evt-comment { background: var(--accent-amber); }
  .evt-dot.evt-review { background: var(--accent-green); }
  .evt-dot.evt-commit { background: var(--accent-teal); }

  .search-input {
    margin-left: auto;
    width: 180px;
    font-size: 12px;
    padding: 4px 8px;
  }

  .table-container {
    flex: 1;
    overflow-y: auto;
    padding: 0 16px;
  }

  .activity-table {
    width: 100%;
    border-collapse: collapse;
  }

  .activity-table thead {
    position: sticky;
    top: 0;
    background: var(--bg-primary);
    z-index: 1;
  }

  .activity-table th {
    text-align: left;
    padding: 6px 10px;
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border-default);
    white-space: nowrap;
  }

  .activity-table td {
    padding: 5px 10px;
    border-bottom: 1px solid var(--border-muted);
    white-space: nowrap;
  }

  .col-kind { }
  .col-event { }
  .col-repo { }
  .col-item {
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 0;
  }
  .col-author { }
  .col-when { text-align: right; }
  th.col-when { text-align: right; }
  .col-link { text-align: center; }

  .activity-row {
    cursor: pointer;
    transition: background 0.1s;
  }

  .activity-row:hover {
    background: var(--bg-surface-hover);
  }

  .badge {
    display: inline-block;
    padding: 1px 5px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
    white-space: nowrap;
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }

  .badge-pr {
    background: color-mix(in srgb, var(--accent-blue) 15%, transparent);
    color: var(--accent-blue);
  }
  .badge-issue {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }

  .evt-label {
    font-size: 12px;
    color: var(--text-secondary);
  }

  .evt-label.evt-comment { color: var(--accent-amber); }
  .evt-label.evt-review { color: var(--accent-green); }
  .evt-label.evt-commit { color: var(--accent-teal); }

  .col-repo {
    color: var(--text-muted);
    font-size: 12px;
  }

  .item-number {
    color: var(--text-muted);
    margin-right: 4px;
  }

  .item-title {
    color: var(--text-primary);
  }

  .col-author {
    color: var(--text-secondary);
    font-size: 12px;
  }

  .col-when {
    color: var(--text-muted);
    font-size: 12px;
  }

  .link-btn {
    color: var(--text-muted);
    font-size: 13px;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }

  .link-btn:hover {
    color: var(--accent-blue);
    background: var(--bg-surface-hover);
  }

  .empty-state {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }

  .error-banner {
    padding: 8px 16px;
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 12px;
    border-bottom: 1px solid var(--border-default);
  }

  .load-more {
    padding: 8px 16px;
    text-align: center;
    border-top: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .load-more-btn {
    padding: 5px 16px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    font-size: 12px;
    color: var(--text-secondary);
    background: var(--bg-surface);
  }

  .load-more-btn:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .load-more-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
