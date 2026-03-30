<script lang="ts">
  import type { ActivityItem } from "../api/activity.js";

  interface Props {
    items: ActivityItem[];
    onSelectItem?: (item: ActivityItem) => void;
  }

  let { items, onSelectItem }: Props = $props();

  interface CollapsedCommits {
    kind: "collapsed";
    id: string;
    author: string;
    count: number;
    earliest: string;
    latest: string;
    representative: ActivityItem;
  }

  type EventRow = ActivityItem | CollapsedCommits;

  function isCollapsed(row: EventRow): row is CollapsedCommits {
    return "kind" in row && row.kind === "collapsed";
  }

  function collapseCommitRuns(events: ActivityItem[]): EventRow[] {
    const result: EventRow[] = [];
    let i = 0;
    while (i < events.length) {
      const ev = events[i]!;
      if (ev.activity_type !== "commit") {
        result.push(ev);
        i++;
        continue;
      }
      let j = i + 1;
      while (j < events.length) {
        const next = events[j]!;
        if (next.activity_type !== "commit" || next.author !== ev.author) break;
        j++;
      }
      const count = j - i;
      if (count < 3) {
        for (let k = i; k < j; k++) result.push(events[k]!);
      } else {
        const latest = events[i]!;
        const earliest = events[j - 1]!;
        result.push({
          kind: "collapsed",
          id: `collapsed-${latest.id}-${count}`,
          author: ev.author,
          count,
          earliest: earliest.created_at,
          latest: latest.created_at,
          representative: latest,
        });
      }
      i = j;
    }
    return result;
  }

  interface ItemGroup {
    itemType: string;
    itemNumber: number;
    itemTitle: string;
    itemUrl: string;
    itemState: string;
    repoOwner: string;
    repoName: string;
    latestTime: string;
    events: ActivityItem[];
    displayEvents: EventRow[];
  }

  interface RepoGroup {
    repo: string;
    itemCount: number;
    eventCount: number;
    latestTime: string;
    items: ItemGroup[];
  }

  const grouped = $derived.by(() => {
    const repoMap = new Map<string, Map<string, ActivityItem[]>>();

    for (const item of items) {
      const repoKey = `${item.repo_owner}/${item.repo_name}`;
      const itemKey = `${item.item_type}:${item.item_number}`;

      let itemMap = repoMap.get(repoKey);
      if (!itemMap) {
        itemMap = new Map();
        repoMap.set(repoKey, itemMap);
      }

      let events = itemMap.get(itemKey);
      if (!events) {
        events = [];
        itemMap.set(itemKey, events);
      }
      events.push(item);
    }

    const repoGroups: RepoGroup[] = [];

    for (const [repo, itemMap] of repoMap) {
      const itemGroups: ItemGroup[] = [];

      for (const [, events] of itemMap) {
        events.sort((a, b) =>
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

        const first = events[0]!;
        itemGroups.push({
          itemType: first.item_type,
          itemNumber: first.item_number,
          itemTitle: first.item_title,
          itemUrl: first.item_url,
          itemState: first.item_state,
          repoOwner: first.repo_owner,
          repoName: first.repo_name,
          latestTime: first.created_at,
          events,
          displayEvents: collapseCommitRuns(events),
        });
      }

      itemGroups.sort((a, b) =>
        new Date(b.latestTime).getTime() - new Date(a.latestTime).getTime());

      const allEvents = itemGroups.flatMap((g) => g.events);
      const latestTime = itemGroups[0]?.latestTime ?? "";

      repoGroups.push({
        repo,
        itemCount: itemGroups.length,
        eventCount: allEvents.length,
        latestTime,
        items: itemGroups,
      });
    }

    repoGroups.sort((a, b) =>
      new Date(b.latestTime).getTime() - new Date(a.latestTime).getTime());

    return repoGroups;
  });

  function eventLabel(type: string): string {
    switch (type) {
      case "new_pr": case "new_issue": return "Opened";
      case "comment": return "Comment";
      case "review": return "Review";
      case "commit": return "Commit";
      default: return type;
    }
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

  function handleItemClick(group: ItemGroup): void {
    if (group.events.length > 0) {
      onSelectItem?.(group.events[0]!);
    }
  }

  function handleEventClick(event: ActivityItem): void {
    onSelectItem?.(event);
  }
</script>

<div class="threaded-view">
  {#each grouped as repoGroup (repoGroup.repo)}
    <div class="repo-section">
      <div class="repo-header">
        <span class="repo-name">{repoGroup.repo}</span>
        <span class="repo-stats">{repoGroup.itemCount} items, {repoGroup.eventCount} events</span>
      </div>

      {#each repoGroup.items as itemGroup (`${itemGroup.itemType}:${itemGroup.itemNumber}`)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="item-row" onclick={() => handleItemClick(itemGroup)}>
          <span class="item-badge" class:badge-pr={itemGroup.itemType === "pr"} class:badge-issue={itemGroup.itemType === "issue"}>
            {itemGroup.itemType === "pr" ? "PR" : "Issue"}
          </span>
          {#if itemGroup.itemState === "merged"}
            <span class="state-tag state-merged">Merged</span>
          {:else if itemGroup.itemState === "closed"}
            <span class="state-tag state-closed">Closed</span>
          {/if}
          <span class="item-ref">#{itemGroup.itemNumber}</span>
          <span class="item-title">{itemGroup.itemTitle}</span>
          <span class="item-time">{relativeTime(itemGroup.latestTime)}</span>
        </div>

        {#each itemGroup.displayEvents as row (row.id)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          {#if isCollapsed(row)}
            <div class="event-row collapsed-event" onclick={() => handleEventClick(row.representative)}>
              <span class="event-type evt-commit">{row.count} commits</span>
              <span class="event-author">{row.author}</span>
              <span class="event-time">{relativeTime(row.latest)} - {relativeTime(row.earliest)}</span>
            </div>
          {:else}
            <div class="event-row" onclick={() => handleEventClick(row)}>
              <span class="event-type {eventClass(row.activity_type)}">{eventLabel(row.activity_type)}</span>
              <span class="event-author">{row.author}</span>
              <span class="event-time">{relativeTime(row.created_at)}</span>
            </div>
          {/if}
        {/each}
      {/each}
    </div>
  {/each}

  {#if grouped.length === 0}
    <div class="empty-state">No activity found</div>
  {/if}
</div>

<style>
  .threaded-view {
    flex: 1;
    overflow-y: auto;
    padding: 0 16px;
  }

  .repo-section {
    margin-bottom: 4px;
  }

  .repo-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0 4px;
    position: sticky;
    top: 0;
    background: var(--bg-primary);
    z-index: 2;
    border-bottom: 1px solid var(--border-default);
  }

  .repo-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .repo-stats {
    font-size: 10px;
    color: var(--text-muted);
  }

  .item-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 0 5px 24px;
    cursor: pointer;
    border-bottom: 1px solid var(--border-muted);
    transition: background 0.1s;
  }

  .item-row:hover {
    background: var(--bg-surface-hover);
  }

  .item-badge {
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    padding: 1px 4px;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .badge-pr {
    background: color-mix(in srgb, var(--accent-blue) 15%, transparent);
    color: var(--accent-blue);
  }
  .badge-issue {
    background: color-mix(in srgb, var(--accent-purple) 15%, transparent);
    color: var(--accent-purple);
  }

  .state-tag {
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    padding: 1px 4px;
    border-radius: 3px;
    flex-shrink: 0;
  }
  .state-merged {
    background: color-mix(in srgb, var(--accent-purple) 20%, transparent);
    color: var(--accent-purple);
  }
  .state-closed {
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
    color: var(--accent-red);
  }

  .item-ref {
    font-size: 12px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .item-title {
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
  }

  .item-time {
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
    margin-left: auto;
  }

  .event-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 0 3px 48px;
    cursor: pointer;
    border-bottom: 1px solid var(--border-muted);
    border-left: 2px solid var(--border-muted);
    margin-left: 24px;
    transition: background 0.1s;
  }

  .event-row:hover {
    background: var(--bg-surface-hover);
  }

  .collapsed-event {
    background: var(--bg-inset);
  }

  .event-type {
    font-size: 11px;
    font-weight: 500;
    flex-shrink: 0;
    color: var(--text-secondary);
  }

  .event-type.evt-comment { color: var(--accent-amber); }
  .event-type.evt-review { color: var(--accent-green); }
  .event-type.evt-commit { color: var(--accent-teal); }

  .event-author {
    font-size: 11px;
    color: var(--text-secondary);
  }

  .event-time {
    font-size: 11px;
    color: var(--text-muted);
    margin-left: auto;
    flex-shrink: 0;
  }

  .empty-state {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
