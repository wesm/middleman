<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import type { ActivityItem } from "../api/types.js";
  import {
    getActivityItems,
    isActivityLoading,
    getActivityError,
    isActivityCapped,
    getActivitySearch,
    getTimeRange,
    getViewMode,
    getHideClosedMerged,
    getHideBots,
    getEnabledEvents,
    getItemFilter,
    setActivityFilterTypes,
    setActivitySearch,
    setTimeRange,
    setViewMode,
    setHideClosedMerged,
    setHideBots,
    setEnabledEvents,
    setItemFilter,
    loadActivity,
    startActivityPolling,
    stopActivityPolling,
    initializeFromMount,
    syncToURL,
  } from "../stores/activity.svelte.js";
  import type { TimeRange, ViewMode } from "../stores/activity.svelte.js";
  import ActivityThreaded from "./ActivityThreaded.svelte";
  import { hasConfiguredRepos, isSettingsLoaded } from "../stores/settings.svelte.js";
  import { navigate } from "../stores/router.svelte.js";
  import { subscribeSyncComplete } from "../stores/sync.svelte.js";
  import { getGroupByRepo, setGroupByRepo } from "../stores/grouping.svelte.js";
  import { isEmbedded } from "../stores/embed-config.svelte.js";

  interface Props {
    onSelectItem?: (item: ActivityItem) => void;
  }

  let { onSelectItem }: Props = $props();

  let searchInput = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let showFilterDropdown = $state(false);
  let filterBtnRef = $state<HTMLButtonElement>();
  let filterDropRef = $state<HTMLDivElement>();

  const EVENT_TYPES = ["comment", "review", "commit"] as const;

  const EVENT_LABELS: Record<string, string> = {
    comment: "Comments",
    review: "Reviews",
    commit: "Commits",
  };

  const EVENT_COLORS: Record<string, string> = {
    comment: "var(--accent-amber)",
    review: "var(--accent-green)",
    commit: "var(--accent-teal)",
  };

  const BOT_SUFFIXES = ["[bot]", "-bot", "bot"];

  function isBot(author: string): boolean {
    const lower = author.toLowerCase();
    return BOT_SUFFIXES.some((s) => lower.endsWith(s));
  }

  const hiddenFilterCount = $derived(
    (EVENT_TYPES.length - getEnabledEvents().size)
    + (getHideClosedMerged() ? 1 : 0)
    + (getHideBots() ? 1 : 0),
  );

  let unsubSync: (() => void) | undefined;

  onMount(() => {
    initializeFromMount();
    searchInput = getActivitySearch() ?? "";
    void loadActivity();
    startActivityPolling();
    unsubSync = subscribeSyncComplete(() => void loadActivity());
  });

  onDestroy(() => {
    stopActivityPolling();
    unsubSync?.();
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  // Close filter dropdown on outside click.
  $effect(() => {
    if (!showFilterDropdown) return;
    function handleClick(e: MouseEvent) {
      if (filterDropRef && !filterDropRef.contains(e.target as Node)
          && filterBtnRef && !filterBtnRef.contains(e.target as Node)) {
        showFilterDropdown = false;
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  });

  function applyFilters(): void {
    const types: string[] = [];
    const filter = getItemFilter();
    if (filter === "prs") types.push("new_pr");
    else if (filter === "issues") types.push("new_issue");
    else { types.push("new_pr", "new_issue"); }
    for (const evt of getEnabledEvents()) types.push(evt);
    const allSelected = filter === "all"
      && getEnabledEvents().size === EVENT_TYPES.length;
    setActivityFilterTypes(allSelected ? [] : types);
    syncToURL();
    void loadActivity();
  }

  function handleItemFilterChange(f: "all" | "prs" | "issues"): void {
    setItemFilter(f);
    applyFilters();
  }

  function toggleEvent(evt: string): void {
    const current = getEnabledEvents();
    const next = new Set(current);
    if (next.has(evt)) { if (next.size > 1) next.delete(evt); }
    else next.add(evt);
    setEnabledEvents(next);
    applyFilters();
  }

  function handleTimeRangeChange(range: TimeRange): void {
    setTimeRange(range);
    syncToURL();
    void loadActivity();
  }

  function handleViewModeChange(mode: ViewMode): void {
    setViewMode(mode);
    syncToURL();
  }

  const TIME_RANGES: { value: TimeRange; label: string }[] = [
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
    { value: "90d", label: "90d" },
  ];

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
    if (item.item_state === "merged") return "badge-merged";
    if (item.item_state === "closed") return "badge-closed";
    return item.item_type === "pr" ? "badge-pr" : "badge-issue";
  }

  function stateLabel(item: ActivityItem): string | null {
    if (item.item_state === "merged") return "Merged";
    if (item.item_state === "closed") return "Closed";
    return null;
  }

  interface CollapsedCommits {
    kind: "collapsed";
    id: string;
    author: string;
    count: number;
    repo_owner: string;
    repo_name: string;
    item_type: string;
    item_number: number;
    item_title: string;
    item_url: string;
    item_state: string;
    earliest: string;
    latest: string;
    representative: ActivityItem;
  }

  type DisplayRow = ActivityItem | CollapsedCommits;

  function isCollapsed(row: DisplayRow): row is CollapsedCommits {
    return "kind" in row && row.kind === "collapsed";
  }

  function collapseCommitRuns(items: ActivityItem[]): DisplayRow[] {
    const result: DisplayRow[] = [];
    let i = 0;
    while (i < items.length) {
      const item = items[i]!;
      if (item.activity_type !== "commit") {
        result.push(item);
        i++;
        continue;
      }
      // Collect consecutive commits by same author on same item.
      let j = i + 1;
      while (j < items.length) {
        const next = items[j]!;
        if (next.activity_type !== "commit"
            || next.author !== item.author
            || next.repo_owner !== item.repo_owner
            || next.repo_name !== item.repo_name
            || next.item_number !== item.item_number) break;
        j++;
      }
      const count = j - i;
      if (count < 3) {
        // Not worth collapsing fewer than 3.
        for (let k = i; k < j; k++) result.push(items[k]!);
      } else {
        // Items are newest-first, so earliest is last in the run.
        const latest = items[i]!;
        const earliest = items[j - 1]!;
        result.push({
          kind: "collapsed",
          id: `collapsed-${latest.id}-${count}`,
          author: item.author,
          count,
          repo_owner: item.repo_owner,
          repo_name: item.repo_name,
          item_type: item.item_type,
          item_number: item.item_number,
          item_title: item.item_title,
          item_url: item.item_url,
          item_state: item.item_state,
          earliest: earliest.created_at,
          latest: latest.created_at,
          representative: latest,
        });
      }
      i = j;
    }
    return result;
  }

  const displayItems = $derived.by(() => {
    let result = getActivityItems();
    const filter = getItemFilter();
    if (filter === "prs") {
      result = result.filter((it) => it.item_type === "pr");
    } else if (filter === "issues") {
      result = result.filter((it) => it.item_type === "issue");
    }
    if (getHideClosedMerged()) {
      result = result.filter((it) =>
        it.item_state !== "merged" && it.item_state !== "closed");
    }
    if (getHideBots()) {
      result = result.filter((it) => !isBot(it.author));
    }
    return result;
  });

  const flatRows = $derived(collapseCommitRuns(displayItems));

  function resetFilters(): void {
    setEnabledEvents(new Set(EVENT_TYPES));
    setHideClosedMerged(false);
    setHideBots(false);
    applyFilters();
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
    <div class="filter-group">
      <div class="segmented-control">
        <button class="seg-btn" class:active={getItemFilter() === "all"} onclick={() => handleItemFilterChange("all")}>All</button>
        <button class="seg-btn" class:active={getItemFilter() === "prs"} onclick={() => handleItemFilterChange("prs")}>PRs</button>
        <button class="seg-btn" class:active={getItemFilter() === "issues"} onclick={() => handleItemFilterChange("issues")}>Issues</button>
      </div>

      <div class="segmented-control">
        <button class="seg-btn" class:active={getViewMode() === "flat"} onclick={() => handleViewModeChange("flat")}>Flat</button>
        <button class="seg-btn" class:active={getViewMode() === "threaded"} onclick={() => handleViewModeChange("threaded")}>Threaded</button>
      </div>

      {#if getViewMode() === "threaded"}
        <div class="segmented-control">
          <button class="seg-btn" class:active={getGroupByRepo()} onclick={() => setGroupByRepo(true)}>By Repo</button>
          <button class="seg-btn" class:active={!getGroupByRepo()} onclick={() => setGroupByRepo(false)}>All</button>
        </div>
      {/if}

      <div class="segmented-control">
        {#each TIME_RANGES as r}
          <button class="seg-btn" class:active={getTimeRange() === r.value} onclick={() => handleTimeRangeChange(r.value)}>{r.label}</button>
        {/each}
      </div>
    </div>

    <div class="filter-wrap">
      <button
        class="filter-btn"
        class:filter-active={hiddenFilterCount > 0}
        bind:this={filterBtnRef}
        onclick={() => (showFilterDropdown = !showFilterDropdown)}
        title="Filter activity types"
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/>
        </svg>
        Filters
        {#if hiddenFilterCount > 0}
          <span class="filter-badge">{hiddenFilterCount}</span>
        {/if}
      </button>

      {#if showFilterDropdown}
        <div class="filter-dropdown" bind:this={filterDropRef}>
          <div class="filter-section-title">Event types</div>
          {#each EVENT_TYPES as evt}
            {@const visible = getEnabledEvents().has(evt)}
            <button
              class="filter-item"
              class:active={visible}
              onclick={() => toggleEvent(evt)}
            >
              <span
                class="filter-dot"
                style:background={visible ? EVENT_COLORS[evt] : "var(--border-muted)"}
              ></span>
              <span class="filter-label">{EVENT_LABELS[evt]}</span>
              <span class="filter-check" class:on={visible}>
                {#if visible}
                  <svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor">
                    <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
                  </svg>
                {/if}
              </span>
            </button>
          {/each}
          <div class="filter-divider"></div>
          <div class="filter-section-title">Visibility</div>
          <button
            class="filter-item"
            class:active={getHideClosedMerged()}
            onclick={() => { setHideClosedMerged(!getHideClosedMerged()); }}
          >
            <span class="filter-dot" style:background={getHideClosedMerged() ? "var(--accent-red)" : "var(--border-muted)"}></span>
            <span class="filter-label">Hide closed/merged</span>
            <span class="filter-check" class:on={getHideClosedMerged()}>
              {#if getHideClosedMerged()}
                <svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor">
                  <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
                </svg>
              {/if}
            </span>
          </button>
          <button
            class="filter-item"
            class:active={getHideBots()}
            onclick={() => { setHideBots(!getHideBots()); }}
          >
            <span class="filter-dot" style:background={getHideBots() ? "var(--accent-purple)" : "var(--border-muted)"}></span>
            <span class="filter-label">Hide bots</span>
            <span class="filter-check" class:on={getHideBots()}>
              {#if getHideBots()}
                <svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor">
                  <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
                </svg>
              {/if}
            </span>
          </button>
          {#if hiddenFilterCount > 0}
            <button class="filter-reset" onclick={resetFilters}>Show all</button>
          {/if}
        </div>
      {/if}
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

  {#if isSettingsLoaded() && !hasConfiguredRepos()}
    <div class="table-container">
      <div class="empty-state">No repositories configured.<br />
        {#if !isEmbedded()}<button class="settings-link" onclick={() => navigate("/settings")}>Add one in Settings</button>{/if}
      </div>
    </div>
  {:else if getViewMode() === "threaded"}
    {#if displayItems.length === 0 && isActivityLoading()}
      <div class="table-container"><div class="empty-state">Loading...</div></div>
    {:else}
      <ActivityThreaded items={displayItems} onSelectItem={onSelectItem} />
    {/if}
  {:else}
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
          {#each flatRows as row (row.id)}
            {#if isCollapsed(row)}
              <tr class="activity-row collapsed-row" onclick={() => handleRowClick(row.representative)}>
                <td class="col-kind">
                  <span class="badge {row.item_type === 'pr' ? 'badge-pr' : 'badge-issue'}">{row.item_type === "pr" ? "PR" : "Issue"}</span>
                </td>
                <td class="col-event">
                  <span class="evt-label evt-commit">{row.count} commits</span>
                </td>
                <td class="col-repo">{row.repo_owner}/{row.repo_name}</td>
                <td class="col-item">
                  <span class="item-number">#{row.item_number}</span>
                  <span class="item-title">{row.item_title}</span>
                </td>
                <td class="col-author">{row.author}</td>
                <td class="col-when">{relativeTime(row.earliest)} - {relativeTime(row.latest)}</td>
                <td class="col-link">
                  <button
                    class="link-btn"
                    title="Open on GitHub"
                    onclick={(e) => handleLinkClick(e, row.item_url)}
                  >&#x2197;</button>
                </td>
              </tr>
            {:else}
              <tr class="activity-row" onclick={() => handleRowClick(row)}>
                <td class="col-kind">
                  <span class="badge {badgeClass(row)}">{itemTypeLabel(row)}</span>
                  {#if stateLabel(row)}
                    <span class="state-badge state-{row.item_state}">{stateLabel(row)}</span>
                  {/if}
                </td>
                <td class="col-event">
                  <span class="evt-label {eventClass(row.activity_type)}">{eventLabel(row)}</span>
                </td>
                <td class="col-repo">{row.repo_owner}/{row.repo_name}</td>
                <td class="col-item">
                  <span class="item-number">#{row.item_number}</span>
                  <span class="item-title">{row.item_title}</span>
                </td>
                <td class="col-author">{row.author}</td>
                <td class="col-when">{relativeTime(row.created_at)}</td>
                <td class="col-link">
                  <button
                    class="link-btn"
                    title="Open on GitHub"
                    onclick={(e) => handleLinkClick(e, row.item_url)}
                  >&#x2197;</button>
                </td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>

      {#if flatRows.length === 0 && !isActivityLoading()}
        <div class="empty-state">No activity found</div>
      {/if}
    </div>
  {/if}

  {#if isActivityCapped()}
    <div class="capped-notice">
      Showing most recent 5,000 events. Narrow the time range or use filters to see more.
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

  .filter-wrap {
    position: relative;
  }

  .filter-btn {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 3px 10px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: border-color 0.12s, color 0.12s;
    position: relative;
  }

  .filter-btn:hover {
    border-color: var(--border-default);
    color: var(--text-secondary);
  }

  .filter-btn.filter-active {
    color: var(--accent-blue);
    border-color: var(--accent-blue);
  }

  .filter-badge {
    font-size: 9px;
    font-weight: 700;
    background: var(--accent-blue);
    color: white;
    border-radius: 6px;
    padding: 0 4px;
    min-width: 14px;
    text-align: center;
    line-height: 14px;
  }

  .filter-dropdown {
    position: absolute;
    top: 100%;
    left: 0;
    margin-top: 4px;
    min-width: 200px;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-md);
    z-index: 50;
    padding: 4px 0;
  }

  .filter-section-title {
    padding: 4px 12px 4px;
    font-size: 9px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .filter-divider {
    height: 1px;
    background: var(--border-muted);
    margin: 4px 8px;
  }

  .filter-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 4px 12px;
    font-size: 11px;
    color: var(--text-secondary);
    text-align: left;
    cursor: pointer;
    transition: background 0.08s;
  }

  .filter-item:hover {
    background: var(--bg-surface-hover);
  }

  .filter-item:not(.active) {
    opacity: 0.5;
  }

  .filter-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
    transition: background 0.1s;
  }

  .filter-label {
    flex: 1;
  }

  .filter-check {
    width: 14px;
    height: 14px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--accent-green);
    flex-shrink: 0;
  }

  .filter-reset {
    display: block;
    width: calc(100% - 16px);
    margin: 4px 8px 2px;
    padding: 4px 8px;
    font-size: 10px;
    color: var(--text-muted);
    text-align: center;
    border-top: 1px solid var(--border-muted);
    padding-top: 8px;
    cursor: pointer;
    transition: color 0.1s;
  }

  .filter-reset:hover {
    color: var(--text-primary);
  }

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

  .col-item {
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 0;
  }
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

  .collapsed-row {
    background: var(--bg-inset);
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
  .badge-merged {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }
  .badge-closed {
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
    color: var(--accent-red);
  }

  .state-badge {
    display: inline-block;
    padding: 1px 4px;
    border-radius: 3px;
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    margin-left: 3px;
  }
  .state-merged {
    background: color-mix(in srgb, var(--accent-purple) 20%, transparent);
    color: var(--accent-purple);
  }
  .state-closed {
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
    color: var(--accent-red);
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

  .settings-link {
    color: var(--accent-blue);
    cursor: pointer;
    font-size: 13px;
    margin-top: 4px;
    display: inline-block;
  }

  .settings-link:hover {
    text-decoration: underline;
  }

  .error-banner {
    padding: 8px 16px;
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 12px;
    border-bottom: 1px solid var(--border-default);
  }

  .capped-notice {
    padding: 6px 16px;
    font-size: 11px;
    color: var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 8%, transparent);
    border-top: 1px solid var(--border-default);
    text-align: center;
    flex-shrink: 0;
  }

</style>
