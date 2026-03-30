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
  import { listRepos } from "../api/client.js";
  import type { Repo } from "../api/types.js";

  interface Props {
    onSelectItem?: (item: ActivityItem) => void;
  }

  let { onSelectItem }: Props = $props();

  let repos = $state<Repo[]>([]);
  let searchInput = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  const ALL_TYPES = ["new_pr", "new_issue", "comment", "review", "commit"] as const;

  const TYPE_LABELS: Record<string, string> = {
    new_pr: "New PR",
    new_issue: "New Issue",
    comment: "Comment",
    review: "Review",
    commit: "Commit",
  };

  onMount(() => {
    syncFromURL();
    searchInput = getActivitySearch() ?? "";
    void loadActivity();
    startActivityPolling();
    void listRepos().then((r) => { repos = r; });
  });

  onDestroy(() => {
    stopActivityPolling();
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  function handleRepoChange(e: Event): void {
    const val = (e.target as HTMLSelectElement).value;
    setActivityFilterRepo(val || undefined);
    syncToURL();
    void loadActivity();
  }

  function toggleType(type: string): void {
    const current = getActivityFilterTypes();
    if (current.length === 0) {
      setActivityFilterTypes([type]);
    } else if (current.includes(type)) {
      const next = current.filter((t) => t !== type);
      setActivityFilterTypes(next);
    } else {
      setActivityFilterTypes([...current, type]);
    }
    syncToURL();
    void loadActivity();
  }

  function isTypeActive(type: string): boolean {
    const f = getActivityFilterTypes();
    if (f.length === 0) return true;
    return f.includes(type);
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

  function badgeClass(type: string): string {
    switch (type) {
      case "new_pr": return "badge-pr";
      case "new_issue": return "badge-issue";
      case "comment": return "badge-comment";
      case "review": return "badge-review";
      case "commit": return "badge-commit";
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
    <select class="repo-select" value={getActivityFilterRepo() ?? ""} onchange={handleRepoChange}>
      <option value="">All repositories</option>
      {#each repos as repo}
        <option value="{repo.Owner}/{repo.Name}">{repo.Owner}/{repo.Name}</option>
      {/each}
    </select>

    <div class="type-pills">
      {#each ALL_TYPES as type}
        <button
          class="type-pill"
          class:active={isTypeActive(type)}
          onclick={() => toggleType(type)}
        >
          <span class="pill-dot {badgeClass(type)}"></span>
          {TYPE_LABELS[type]}
        </button>
      {/each}
    </div>

    <input
      class="search-input"
      type="text"
      placeholder="Search titles and content..."
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
          <th class="col-type">Type</th>
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
            <td class="col-type">
              <span class="badge {badgeClass(item.activity_type)}">{TYPE_LABELS[item.activity_type]}</span>
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
    gap: 10px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .repo-select {
    font: inherit;
    font-size: 12px;
    padding: 4px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  .type-pills {
    display: flex;
    gap: 4px;
  }

  .type-pill {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    font-size: 11px;
    color: var(--text-muted);
    border: 1px solid var(--border-muted);
    background: transparent;
    transition: opacity 0.15s;
  }

  .type-pill.active {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-surface);
  }

  .type-pill:not(.active) {
    opacity: 0.5;
  }

  .pill-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .pill-dot.badge-pr { background: var(--accent-blue); }
  .pill-dot.badge-issue { background: var(--accent-purple); }
  .pill-dot.badge-comment { background: var(--accent-amber); }
  .pill-dot.badge-review { background: var(--accent-green); }
  .pill-dot.badge-commit { background: var(--accent-teal); }

  .search-input {
    margin-left: auto;
    width: 220px;
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
    table-layout: fixed;
  }

  .activity-table thead {
    position: sticky;
    top: 0;
    background: var(--bg-surface);
    z-index: 1;
  }

  .activity-table th {
    text-align: left;
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border-default);
  }

  .activity-table td {
    padding: 5px 12px;
    border-bottom: 1px solid var(--border-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .col-type { width: 90px; overflow: visible; }
  .col-repo { width: 160px; }
  .col-item { width: auto; }
  .col-author { width: 130px; }
  .col-when { width: 80px; text-align: right; }
  th.col-when { text-align: right; }
  .col-link { width: 36px; text-align: center; }

  .activity-row {
    cursor: pointer;
    transition: background 0.1s;
  }

  .activity-row:hover {
    background: var(--bg-surface-hover);
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }

  .badge-pr { background: color-mix(in srgb, var(--accent-blue) 18%, transparent); color: var(--accent-blue); }
  .badge-issue { background: color-mix(in srgb, var(--accent-purple) 18%, transparent); color: var(--accent-purple); }
  .badge-comment { background: color-mix(in srgb, var(--accent-amber) 18%, transparent); color: var(--accent-amber); }
  .badge-review { background: color-mix(in srgb, var(--accent-green) 18%, transparent); color: var(--accent-green); }
  .badge-commit { background: color-mix(in srgb, var(--accent-teal) 18%, transparent); color: var(--accent-teal); }

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
