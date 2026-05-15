<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import type { ActivityItem } from "../api/types.js";
  import { getStores } from "../context.js";
  import type { ItemFilter, TimeRange } from "../stores/activity.svelte.js";
  import { parseAPITimestamp } from "../utils/time.js";
  import ItemKindChip from "../components/shared/ItemKindChip.svelte";
  import ItemStateChip from "../components/shared/ItemStateChip.svelte";
  import {
    buildMobileActivityRepoOptions,
  } from "./mobileActivityRepoOptions.js";

  const { activity, settings, sync } = getStores();

  interface Props {
    selectedRepo?: string | undefined;
    onRepoChange?: ((repo: string | undefined) => void) | undefined;
    onSelectItem?: ((item: ActivityItem) => void) | undefined;
  }

  let { selectedRepo, onRepoChange, onSelectItem }: Props = $props();

  type ActivityGroup = {
    key: string;
    representative: ActivityItem;
    events: ActivityItem[];
    eventCount: number;
    latestTime: string;
  };

  const BOT_SUFFIXES = ["[bot]", "-bot", "bot"];
  const timeRanges: TimeRange[] = ["24h", "7d", "30d", "90d"];
  const itemFilters: { value: ItemFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "prs", label: "PRs" },
    { value: "issues", label: "Issues" },
  ];
  let searchInput = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let unsubSync: (() => void) | undefined;

  const repoOptions = $derived.by(() =>
    buildMobileActivityRepoOptions(settings.getConfiguredRepos()),
  );

  onMount(() => {
    activity.initializeFromMount();
    searchInput = activity.getActivitySearch() ?? "";
    void activity.loadActivity();
    activity.startActivityPolling();
    unsubSync = sync.subscribeSyncComplete(() => void activity.loadActivity());
  });

  onDestroy(() => {
    activity.stopActivityPolling();
    unsubSync?.();
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  function isBot(author: string): boolean {
    const lower = author.toLowerCase();
    return BOT_SUFFIXES.some((suffix) => lower.endsWith(suffix));
  }

  const displayItems = $derived.by(() => {
    let result = activity.getActivityItems();
    const filter = activity.getItemFilter();

    if (filter === "prs") {
      result = result.filter((item) => item.item_type === "pr");
    } else if (filter === "issues") {
      result = result.filter((item) => item.item_type === "issue");
    }

    if (activity.getHideClosedMerged()) {
      result = result.filter(
        (item) => item.item_state !== "merged" && item.item_state !== "closed",
      );
    }

    if (activity.getHideBots()) {
      result = result.filter((item) => !isBot(item.author));
    }

    return result;
  });

  const groups = $derived.by(() => {
    const map = new Map<string, ActivityItem[]>();

    for (const item of displayItems) {
      const key = `${item.repo.platform_host}|${item.repo.repo_path}:${item.item_type}:${item.item_number}`;
      const bucket = map.get(key);
      if (bucket) bucket.push(item);
      else map.set(key, [item]);
    }

    const result: ActivityGroup[] = [];
    for (const [key, events] of map) {
      events.sort(
        (a, b) =>
          parseAPITimestamp(b.created_at).getTime()
          - parseAPITimestamp(a.created_at).getTime(),
      );
      const representative = events[0];
      if (!representative) continue;
      result.push({
        key,
        representative,
        events,
        eventCount: events.length,
        latestTime: representative.created_at,
      });
    }

    result.sort(
      (a, b) =>
        parseAPITimestamp(b.latestTime).getTime()
        - parseAPITimestamp(a.latestTime).getTime(),
    );
    return result;
  });

  const activeThreadCount = $derived(groups.length);
  const eventCount = $derived(displayItems.length);
  const visibleGroups = $derived(groups.slice(0, 30));

  function applyFilters(): void {
    const types: string[] = [];
    const filter = activity.getItemFilter();
    if (filter === "prs") types.push("new_pr");
    else if (filter === "issues") types.push("new_issue");
    else types.push("new_pr", "new_issue");

    for (const eventType of activity.getEnabledEvents()) {
      types.push(eventType);
    }

    const allSelected = filter === "all"
      && activity.getEnabledEvents().size === 4;
    activity.setActivityFilterTypes(allSelected ? [] : types);
    activity.syncToURL();
    void activity.loadActivity();
  }

  function setItemFilter(filter: ItemFilter): void {
    activity.setItemFilter(filter);
    applyFilters();
  }

  function handleItemFilterChange(event: Event): void {
    setItemFilter((event.target as HTMLSelectElement).value as ItemFilter);
  }

  function setTimeRange(range: TimeRange): void {
    activity.setTimeRange(range);
    activity.syncToURL();
    void activity.loadActivity();
  }

  function handleTimeRangeChange(event: Event): void {
    setTimeRange((event.target as HTMLSelectElement).value as TimeRange);
  }

  function handleRepoChange(event: Event): void {
    const value = (event.target as HTMLSelectElement).value;
    onRepoChange?.(value || undefined);
    void activity.loadActivity();
  }

  function toggleHideBots(): void {
    activity.setHideBots(!activity.getHideBots());
    applyFilters();
  }

  function handleSearchInput(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    searchInput = value;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      activity.setActivitySearch(value || undefined);
      activity.syncToURL();
      void activity.loadActivity();
    }, 300);
  }

  function handleCardClick(group: ActivityGroup): void {
    onSelectItem?.(group.representative);
  }

  function eventLabel(type: string): string {
    switch (type) {
      case "new_pr":
      case "new_issue":
        return "Opened";
      case "comment":
        return "Comment";
      case "review":
        return "Review";
      case "commit":
        return "Commit";
      case "force_push":
        return "Force-pushed";
      default:
        return type;
    }
  }

  function relativeTime(iso: string): string {
    const diff = Date.now() - parseAPITimestamp(iso).getTime();
    const mins = Math.floor(diff / 60_000);
    if (mins < 1) return "just now";
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    if (days < 30) return `${Math.floor(days / 7)}w ago`;
    return `${Math.floor(days / 30)}mo ago`;
  }

  function eventTone(type: string): string {
    switch (type) {
      case "comment": return "comment";
      case "review": return "review";
      case "commit": return "commit";
      case "force_push": return "force-push";
      default: return "opened";
    }
  }

  function latestEvents(group: ActivityGroup): ActivityItem[] {
    return group.events.slice(0, 2);
  }

  function repoLabel(item: ActivityItem): string {
    return `${item.repo.platform_host}/${item.repo.repo_path}`;
  }
</script>

<section class="mobile-activity-inbox" aria-label="Mobile activity inbox">
  <div class="mobile-activity-scroll">
    <header class="mobile-activity-hero">
      <p class="mobile-activity-eyebrow">
        Activity inbox · {activity.getTimeRange()}
      </p>
      <h1>What needs attention?</h1>
    </header>

    <label class="mobile-activity-search">
      <span aria-hidden="true">⌕</span>
      <input
        class="search-input"
        type="search"
        placeholder="Search issues, PRs, authors"
        value={searchInput}
        oninput={handleSearchInput}
      />
    </label>

    <div class="mobile-activity-filter-grid" aria-label="Activity filters">
      <label class="mobile-filter-select">
        <span>Type</span>
        <select
          aria-label="Activity type"
          value={activity.getItemFilter()}
          onchange={handleItemFilterChange}
        >
          {#each itemFilters as option}
            <option value={option.value}>{option.label}</option>
          {/each}
        </select>
      </label>

      <label class="mobile-filter-select">
        <span>Range</span>
        <select
          aria-label="Time range"
          value={activity.getTimeRange()}
          onchange={handleTimeRangeChange}
        >
          {#each timeRanges as range}
            <option value={range}>{range}</option>
          {/each}
        </select>
      </label>

      <label class="mobile-filter-select mobile-filter-select--repo">
        <span>Repo</span>
        <select
          aria-label="Repository"
          value={selectedRepo ?? ""}
          onchange={handleRepoChange}
        >
          <option value="">All repos</option>
          {#each repoOptions as repo (repo.value)}
            <option value={repo.value}>{repo.label}</option>
          {/each}
        </select>
      </label>

      <button
        type="button"
        class="mobile-filter-toggle"
        class:active={activity.getHideBots()}
        aria-pressed={activity.getHideBots()}
        onclick={toggleHideBots}
      >Hide bots</button>
    </div>

    <section class="mobile-activity-summary" aria-label="Activity summary">
      <div class="mobile-activity-metric">
        <strong>{activeThreadCount}</strong>
        <span>active threads</span>
      </div>
      <div class="mobile-activity-metric">
        <strong>{eventCount}</strong>
        <span>events</span>
      </div>
    </section>

    {#if activity.getActivityError()}
      <div class="mobile-activity-error">{activity.getActivityError()}</div>
    {/if}

    {#if settings.isSettingsLoaded() && !settings.hasConfiguredRepos()}
      <div class="mobile-activity-empty">No repositories configured.</div>
    {:else if visibleGroups.length === 0 && activity.isActivityLoading()}
      <div class="mobile-activity-empty">Loading activity…</div>
    {:else if visibleGroups.length === 0}
      <div class="mobile-activity-empty">No activity found</div>
    {:else}
      <div class="mobile-activity-card-list">
        {#each visibleGroups as group (group.key)}
          {@const item = group.representative}
          <article class="mobile-activity-card">
            <button
              type="button"
              class="mobile-activity-card__button"
              onclick={() => handleCardClick(group)}
            >
              <span class="mobile-activity-card__top">
                <span class="mobile-activity-card__chips">
                  <ItemKindChip kind={item.item_type === "issue" ? "issue" : "pr"} size="md" />
                  <span class="mobile-activity-number">#{item.item_number}</span>
                  {#if item.item_state === "merged" || item.item_state === "closed"}
                    <ItemStateChip state={item.item_state} size="md" />
                  {/if}
                </span>
                <time>{relativeTime(group.latestTime)}</time>
              </span>

              <span class="mobile-activity-card__title">{item.item_title}</span>
              <span class="mobile-activity-card__meta">
                <span>{repoLabel(item)}</span>
                <span>{group.eventCount} {group.eventCount === 1 ? "event" : "events"}</span>
                <span>{item.author}</span>
              </span>
            </button>

            <div class="mobile-activity-events">
              {#each latestEvents(group) as event (event.id)}
                <button
                  type="button"
                  class="mobile-activity-event"
                  class:event-comment={eventTone(event.activity_type) === "comment"}
                  class:event-review={eventTone(event.activity_type) === "review"}
                  class:event-commit={eventTone(event.activity_type) === "commit"}
                  class:event-force-push={eventTone(event.activity_type) === "force-push"}
                  onclick={() => onSelectItem?.(event)}
                >
                  <span class="mobile-activity-event__dot" aria-hidden="true"></span>
                  <span class="mobile-activity-event__body">
                    <strong>{eventLabel(event.activity_type)}</strong>
                    <span>{event.author}</span>
                  </span>
                  <time>{relativeTime(event.created_at)}</time>
                </button>
              {/each}
            </div>

            <button
              type="button"
              class="mobile-activity-open"
              onclick={() => handleCardClick(group)}
            >Open thread</button>
          </article>
        {/each}
      </div>
    {/if}

    {#if activity.isActivityCapped()}
      <div class="mobile-activity-capped">
        Showing most recent 5,000 events. Narrow the range or filters to see more.
      </div>
    {/if}
  </div>
</section>

<style>
  .mobile-activity-inbox {
    --mobile-type-xs: var(--font-size-mobile-xs, 1.08rem);
    --mobile-type-sm: var(--font-size-mobile-sm, 1.17rem);
    --mobile-type-body: var(--font-size-mobile-body, 1.24rem);
    --mobile-type-title: var(--font-size-mobile-title, 1.54rem);
    --mobile-type-display: var(--font-size-mobile-display, 2.15rem);
    --mobile-type-metric: var(--font-size-mobile-metric, 1.97rem);
    --mobile-space-2xs: 0.36rem;
    --mobile-space-xs: 0.55rem;
    --mobile-space-sm: 0.75rem;
    --mobile-space-md: 1rem;
    --mobile-space-lg: 1.35rem;
    --mobile-radius-sm: 1rem;
    --mobile-radius-md: 1.25rem;
    --mobile-hit-target: 3.5rem;
    container-type: inline-size;
    font-size: var(--font-size-mobile-body);
    flex: 1;
    min-height: 0;
    overflow: hidden;
    background:
      radial-gradient(circle at 50% -20%, color-mix(in srgb, var(--accent-blue) 22%, transparent), transparent 34%),
      var(--bg-primary);
  }

  .mobile-activity-scroll {
    height: 100%;
    overflow-y: auto;
    padding:
      var(--mobile-space-md)
      var(--mobile-space-sm)
      max(var(--mobile-space-lg), env(safe-area-inset-bottom));
    font-size: var(--font-size-mobile-body);
  }

  .mobile-activity-hero {
    margin: var(--mobile-space-2xs) var(--mobile-space-2xs) var(--mobile-space-md);
  }

  .mobile-activity-eyebrow {
    margin: 0 0 var(--mobile-space-2xs);
    color: var(--text-muted);
    font-size: var(--font-size-mobile-sm);
    font-weight: 700;
    letter-spacing: 0.02em;
  }

  .mobile-activity-hero h1 {
    margin: 0;
    color: var(--text-primary);
    font-size: var(--font-size-mobile-display);
    line-height: 1;
    letter-spacing: -0.045em;
  }

  .mobile-activity-search {
    min-height: calc(var(--mobile-hit-target) + var(--mobile-space-xs));
    display: flex;
    align-items: center;
    gap: var(--mobile-space-sm);
    padding: 0 var(--mobile-space-md);
    border: thin solid var(--border-default);
    border-radius: var(--mobile-radius-sm);
    background: color-mix(in srgb, var(--bg-surface) 82%, transparent);
    color: var(--text-muted);
    margin-bottom: var(--mobile-space-sm);
  }

  .mobile-activity-search .search-input {
    flex: 1;
    min-width: 0;
    width: 100%;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--font-size-mobile-body);
  }

  .mobile-activity-search .search-input::placeholder {
    color: var(--text-muted);
  }

  .mobile-activity-filter-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--mobile-space-xs);
    margin-bottom: var(--mobile-space-sm);
  }

  .mobile-filter-select {
    min-width: 0;
    min-height: var(--mobile-hit-target);
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--mobile-space-xs);
    padding: 0 var(--mobile-space-sm);
    border: thin solid var(--border-default);
    border-radius: var(--mobile-radius-sm);
    color: var(--text-secondary);
    background: color-mix(in srgb, var(--bg-surface) 86%, transparent);
  }

  .mobile-filter-select--repo {
    grid-column: 1 / -1;
  }

  .mobile-filter-select span {
    color: var(--text-muted);
    font-size: var(--font-size-mobile-xs);
    font-weight: 750;
    letter-spacing: 0.01em;
  }

  .mobile-filter-select select {
    min-width: 0;
    width: 100%;
    border: 0;
    outline: 0;
    color: var(--text-primary);
    background: transparent;
    font: inherit;
    font-size: var(--font-size-mobile-sm);
    font-weight: 750;
  }

  .mobile-filter-toggle,
  .mobile-activity-open {
    min-height: var(--mobile-hit-target);
    flex: 0 0 auto;
    padding: var(--mobile-space-sm) var(--mobile-space-md);
    border: thin solid var(--border-default);
    border-radius: 999rem;
    color: var(--text-secondary);
    background: color-mix(in srgb, var(--bg-surface) 86%, transparent);
    font-size: var(--font-size-mobile-sm);
    font-weight: 750;
  }

  .mobile-filter-toggle.active {
    color: var(--bg-primary);
    background: var(--text-primary);
    border-color: transparent;
  }

  .mobile-activity-summary {
    display: grid;
    grid-template-columns: minmax(0, 1.2fr) minmax(0, 0.8fr);
    gap: var(--mobile-space-sm);
    margin: var(--mobile-space-2xs) 0 var(--mobile-space-md);
  }

  .mobile-activity-metric {
    min-height: calc(var(--mobile-hit-target) * 2);
    padding: var(--mobile-space-md);
    border: thin solid var(--border-default);
    border-radius: var(--mobile-radius-md);
    background: linear-gradient(
      145deg,
      color-mix(in srgb, var(--accent-blue) 17%, transparent),
      color-mix(in srgb, var(--bg-surface) 82%, transparent)
    );
  }

  .mobile-activity-metric strong {
    display: block;
    color: var(--text-primary);
    font-size: var(--font-size-mobile-metric);
    line-height: 1;
    letter-spacing: -0.04em;
  }

  .mobile-activity-metric span {
    display: block;
    margin-top: var(--mobile-space-2xs);
    color: var(--text-muted);
    font-size: var(--font-size-mobile-sm);
  }

  .mobile-activity-card-list {
    display: grid;
    gap: var(--mobile-space-md);
  }

  .mobile-activity-card {
    overflow: hidden;
    border: thin solid var(--border-default);
    border-radius: var(--mobile-radius-md);
    background: linear-gradient(
      180deg,
      color-mix(in srgb, var(--bg-surface) 96%, white 4%),
      color-mix(in srgb, var(--bg-surface) 88%, black 12%)
    );
    box-shadow: 0 0.65rem 2rem color-mix(in srgb, black 28%, transparent);
  }

  .mobile-activity-card__button {
    display: flex;
    flex-direction: column;
    gap: var(--mobile-space-sm);
    width: 100%;
    min-height: calc(var(--mobile-hit-target) * 3);
    padding: var(--mobile-space-md);
    border: 0;
    color: inherit;
    background: transparent;
    text-align: left;
  }

  .mobile-activity-card__top {
    display: flex;
    align-items: center;
    gap: var(--mobile-space-sm);
    min-width: 0;
  }

  .mobile-activity-card__chips {
    display: flex;
    align-items: center;
    gap: var(--mobile-space-xs);
    min-width: 0;
  }

  .mobile-activity-card__chips :global(.chip--md) {
    min-height: calc(var(--mobile-hit-target) * 0.55);
    padding: 0 var(--mobile-space-xs);
    border-radius: 999rem;
    font-size: var(--font-size-mobile-xs);
  }

  .mobile-activity-card__top time {
    margin-left: auto;
    flex-shrink: 0;
    color: var(--text-muted);
    font-size: var(--font-size-mobile-sm);
    font-weight: 700;
  }

  .mobile-activity-number {
    color: var(--text-muted);
    font-size: var(--font-size-mobile-sm);
    font-weight: 700;
  }

  .mobile-activity-card__title {
    display: -webkit-box;
    overflow: hidden;
    color: var(--text-primary);
    font-size: var(--font-size-mobile-title);
    font-weight: 800;
    line-height: 1.22;
    letter-spacing: -0.018em;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
  }

  .mobile-activity-card__meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--mobile-space-xs) var(--mobile-space-sm);
    color: var(--text-muted);
    font-size: var(--font-size-mobile-sm);
    line-height: 1.25;
  }

  .mobile-activity-card__meta span:not(:last-child)::after {
    content: "";
  }

  .mobile-activity-events {
    display: grid;
    gap: var(--mobile-space-xs);
    padding: 0 var(--mobile-space-sm) var(--mobile-space-sm);
  }

  .mobile-activity-event {
    min-height: var(--mobile-hit-target);
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--mobile-space-sm);
    padding: var(--mobile-space-sm);
    border: thin solid transparent;
    border-radius: var(--mobile-radius-sm);
    color: inherit;
    background: color-mix(in srgb, var(--bg-inset) 72%, transparent);
    text-align: left;
  }

  .mobile-activity-event__dot {
    width: 0.62rem;
    height: 0.62rem;
    border-radius: 999rem;
    background: var(--accent-blue);
    box-shadow: 0 0 0 0.32rem color-mix(in srgb, var(--accent-blue) 12%, transparent);
  }

  .mobile-activity-event.event-comment .mobile-activity-event__dot {
    background: var(--accent-amber);
    box-shadow: 0 0 0 0.32rem color-mix(in srgb, var(--accent-amber) 12%, transparent);
  }

  .mobile-activity-event.event-review .mobile-activity-event__dot,
  .mobile-activity-event.event-commit .mobile-activity-event__dot {
    background: var(--accent-green);
    box-shadow: 0 0 0 0.32rem color-mix(in srgb, var(--accent-green) 12%, transparent);
  }

  .mobile-activity-event.event-force-push .mobile-activity-event__dot {
    background: var(--accent-red);
    box-shadow: 0 0 0 0.32rem color-mix(in srgb, var(--accent-red) 12%, transparent);
  }

  .mobile-activity-event__body {
    min-width: 0;
  }

  .mobile-activity-event__body strong {
    display: block;
    color: var(--text-primary);
    font-size: var(--font-size-mobile-sm);
    font-weight: 750;
  }

  .mobile-activity-event__body span {
    display: block;
    overflow: hidden;
    color: var(--text-muted);
    font-size: var(--font-size-mobile-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mobile-activity-event time {
    color: var(--text-muted);
    font-size: var(--font-size-mobile-xs);
    font-weight: 750;
  }

  .mobile-activity-open {
    display: block;
    width: calc(100% - (var(--mobile-space-sm) * 2));
    margin: 0 var(--mobile-space-sm) var(--mobile-space-sm);
    color: var(--text-primary);
    text-align: center;
    background: color-mix(in srgb, var(--accent-blue) 13%, transparent);
  }

  .mobile-activity-empty,
  .mobile-activity-error,
  .mobile-activity-capped {
    padding: var(--mobile-space-lg);
    border: thin solid var(--border-default);
    border-radius: var(--mobile-radius-md);
    color: var(--text-muted);
    background: color-mix(in srgb, var(--bg-surface) 84%, transparent);
    font-size: var(--font-size-mobile-sm);
    text-align: center;
  }

  .mobile-activity-error {
    color: var(--accent-red);
  }

  .mobile-activity-capped {
    margin-top: var(--mobile-space-md);
    color: var(--accent-amber);
  }
</style>
