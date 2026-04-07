<script lang="ts">
  import { getStores, getNavigate, getSidebar } from "../context.js";
  import PullItem from "../components/sidebar/PullItem.svelte";
  import IssueItem from "../components/sidebar/IssueItem.svelte";

  const { pulls, issues, sync, settings } = getStores();
  const navigate = getNavigate();
  const { isEmbedded } = getSidebar();

  interface Props {
    listType: "mrs" | "issues";
    repo?: string;
  }

  const { listType, repo }: Props = $props();

  let searchInput = $state("");
  let debounceHandle: ReturnType<typeof setTimeout> | null =
    null;
  let refreshHandle: ReturnType<typeof setInterval> | null =
    null;

  const repoLabel = $derived(repo ?? "All repositories");

  $effect(() => {
    if (listType === "mrs") {
      void pulls.loadPulls();
    } else {
      void issues.loadIssues();
    }

    refreshHandle = setInterval(() => {
      if (listType === "mrs") {
        void pulls.loadPulls();
      } else {
        void issues.loadIssues();
      }
    }, 15_000);

    if (sync.getSyncState()?.running) {
      sync.onNextSyncComplete(() => {
        if (listType === "mrs") {
          void pulls.loadPulls();
        } else {
          void issues.loadIssues();
        }
      });
    }

    return () => {
      if (refreshHandle !== null) clearInterval(refreshHandle);
    };
  });

  function onSearchInput(e: Event): void {
    const value = (e.target as HTMLInputElement).value;
    searchInput = value;

    if (debounceHandle !== null) clearTimeout(debounceHandle);
    debounceHandle = setTimeout(() => {
      const q =
        value.trim() === "" ? undefined : value.trim();
      if (listType === "mrs") {
        pulls.setSearchQuery(q);
        void pulls.loadPulls();
      } else {
        issues.setIssueSearchQuery(q);
        void issues.loadIssues();
      }
    }, 300);
  }

  function handlePRSelect(
    owner: string,
    name: string,
    number: number,
  ): void {
    navigate(`/focus/pr/${owner}/${name}/${number}`);
  }

  function handleIssueSelect(
    owner: string,
    name: string,
    number: number,
  ): void {
    navigate(`/focus/issue/${owner}/${name}/${number}`);
  }

  // Filter state accessors for PRs.
  const prFilterState = $derived(pulls.getFilterState());
  const prItems = $derived(pulls.getPulls());
  const prLoading = $derived(pulls.isLoading());
  const prError = $derived(pulls.getError());

  // Filter state accessors for issues.
  const issueFilterState = $derived(
    issues.getIssueFilterState(),
  );
  const issueItems = $derived(issues.getIssues());
  const issueLoading = $derived(issues.isIssuesLoading());
  const issueError = $derived(issues.getIssuesError());

  const itemCount = $derived(
    listType === "mrs" ? prItems.length : issueItems.length,
  );
  const itemLabel = $derived(
    listType === "mrs" ? "PRs" : "issues",
  );
</script>

<div class="focus-list">
  <div class="header">
    <span class="header-label">{repoLabel}</span>
    <span class="count-badge">{itemCount} {itemLabel}</span>
  </div>
  <div class="filter-bar">
    <div class="state-toggle">
      {#if listType === "mrs"}
        {#each ["open", "closed", "all"] as s (s)}
          <button
            class="state-btn"
            class:state-btn--active={prFilterState === s}
            onclick={() => {
              pulls.setFilterState(s);
              void pulls.loadPulls();
            }}
          >
            {s === "open"
              ? "Open"
              : s === "closed"
                ? "Closed"
                : "All"}
          </button>
        {/each}
      {:else}
        {#each ["open", "closed", "all"] as s (s)}
          <button
            class="state-btn"
            class:state-btn--active={issueFilterState === s}
            onclick={() => {
              issues.setIssueFilterState(s);
              void issues.loadIssues();
            }}
          >
            {s === "open"
              ? "Open"
              : s === "closed"
                ? "Closed"
                : "All"}
          </button>
        {/each}
      {/if}
    </div>
  </div>
  <div class="search-bar">
    <div class="search-input-wrap">
      <svg
        class="search-icon"
        viewBox="0 0 16 16"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <circle
          cx="6.5"
          cy="6.5"
          r="4.5"
          stroke="currentColor"
          stroke-width="1.5"
        />
        <path
          d="M10 10L14 14"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
        />
      </svg>
      <input
        class="search-input"
        type="search"
        placeholder="Search {itemLabel}..."
        value={searchInput}
        oninput={onSearchInput}
      />
    </div>
  </div>

  {#if listType === "mrs" && prFilterState !== "open"}
    <p class="state-note">
      Showing items closed after tracking began
    </p>
  {:else if listType === "issues" && issueFilterState !== "open"}
    <p class="state-note">
      Showing items closed after tracking began
    </p>
  {/if}

  <div class="list-body">
    {#if settings.isSettingsLoaded() && !settings.hasConfiguredRepos()}
      <p class="state-message">No repositories configured.</p>
    {:else if listType === "mrs"}
      {#if prLoading && prItems.length === 0}
        <p class="state-message">Loading...</p>
      {:else if prError !== null && prItems.length === 0}
        <p class="state-message state-message--error">
          Error: {prError}
        </p>
      {:else if prItems.length === 0 && sync.getSyncState()?.running}
        <div class="state-message sync-message">
          <span class="sync-dot"></span>
          Syncing...
        </div>
      {:else if prItems.length === 0 && !sync.getSyncState()?.last_run_at}
        <p class="state-message">Waiting for first sync...</p>
      {:else if prItems.length === 0}
        <p class="state-message">No pull requests found.</p>
      {:else}
        {#each prItems as pr (pr.ID)}
          <PullItem
            {pr}
            showRepo={!repo}
            selected={false}
            onclick={() =>
              handlePRSelect(
                pr.repo_owner ?? "",
                pr.repo_name ?? "",
                pr.Number,
              )}
          />
        {/each}
      {/if}
    {:else}
      {#if issueLoading && issueItems.length === 0}
        <p class="state-message">Loading...</p>
      {:else if issueError !== null && issueItems.length === 0}
        <p class="state-message state-message--error">
          Error: {issueError}
        </p>
      {:else if issueItems.length === 0 && sync.getSyncState()?.running}
        <div class="state-message sync-message">
          <span class="sync-dot"></span>
          Syncing...
        </div>
      {:else if issueItems.length === 0 && !sync.getSyncState()?.last_run_at}
        <p class="state-message">Waiting for first sync...</p>
      {:else if issueItems.length === 0}
        <p class="state-message">No issues found.</p>
      {:else}
        {#each issueItems as issue (issue.ID)}
          <IssueItem
            {issue}
            showRepo={!repo}
            selected={false}
            onclick={() =>
              handleIssueSelect(
                issue.repo_owner ?? "",
                issue.repo_name ?? "",
                issue.Number,
              )}
          />
        {/each}
      {/if}
    {/if}
  </div>
</div>

<style>
  .focus-list {
    display: flex;
    flex-direction: column;
    height: 100%;
    width: 100%;
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .header-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .count-badge {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: 10px;
    padding: 2px 7px;
    flex-shrink: 0;
  }

  .filter-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
    background: var(--bg-surface);
  }

  .search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-default);
    flex-shrink: 0;
    background: var(--bg-surface);
  }

  .search-input-wrap {
    position: relative;
    flex: 1;
    min-width: 0;
  }

  .search-icon {
    position: absolute;
    left: 8px;
    top: 50%;
    transform: translateY(-50%);
    width: 13px;
    height: 13px;
    color: var(--text-muted);
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    font-size: 12px;
    padding: 5px 8px 5px 28px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
  }

  .search-input:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .state-toggle {
    display: flex;
    gap: 2px;
    background: var(--bg-inset);
    border-radius: 6px;
    padding: 2px;
  }

  .state-btn {
    font-size: 11px;
    padding: 2px 8px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    white-space: nowrap;
  }

  .state-btn--active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }

  .state-note {
    font-size: 11px;
    color: var(--text-muted);
    padding: 4px 10px;
    margin: 0;
    border-bottom: 1px solid var(--border-muted);
  }

  .list-body {
    flex: 1;
    overflow-y: auto;
  }

  .state-message {
    padding: 24px 16px;
    font-size: 13px;
    color: var(--text-muted);
    text-align: center;
  }

  .state-message--error {
    color: var(--accent-red);
  }

  .sync-message {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }

  .sync-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green);
    animation: pulse 1.5s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.4;
    }
    50% {
      opacity: 1;
    }
  }
</style>
