<script lang="ts">
  import {
    issuesByRepo,
    isIssuesLoading,
    getIssuesError,
    loadIssues,
    getIssues,
    getSelectedIssue,
    selectIssue,
    getIssueSearchQuery,
    setIssueSearchQuery,
    getIssueFilterRepo,
    setIssueFilterRepo,
    getIssueFilterStarred,
    setIssueFilterStarred,
  } from "../../stores/issues.svelte.js";
  import { getSyncState, onNextSyncComplete } from "../../stores/sync.svelte.js";
  import { listRepos } from "../../api/client.js";
  import IssueItem from "./IssueItem.svelte";

  let searchInput = $state(getIssueSearchQuery() ?? "");
  let debounceHandle: ReturnType<typeof setTimeout> | null = null;
  let repos = $state<string[]>([]);
  let refreshHandle: ReturnType<typeof setInterval> | null = null;

  $effect(() => {
    void loadIssues();
    listRepos().then((r) => {
      repos = r.map((repo) => `${repo.Owner}/${repo.Name}`);
    });

    refreshHandle = setInterval(() => {
      void loadIssues();
      listRepos().then((r) => {
        repos = r.map((repo) => `${repo.Owner}/${repo.Name}`);
      });
    }, 15_000);

    if (getSyncState()?.running) {
      onNextSyncComplete(() => void loadIssues());
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
      setIssueSearchQuery(value.trim() === "" ? undefined : value.trim());
      void loadIssues();
    }, 300);
  }

  function handleSelect(owner: string, name: string, number: number): void {
    selectIssue(owner, name, number);
  }

  function isSelected(owner: string, name: string, number: number): boolean {
    const sel = getSelectedIssue();
    return sel !== null && sel.owner === owner && sel.name === name && sel.number === number;
  }
</script>

<div class="issue-list">
  <div class="search-bar">
    <div class="search-input-wrap">
      <svg class="search-icon" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
        <circle cx="6.5" cy="6.5" r="4.5" stroke="currentColor" stroke-width="1.5" />
        <path d="M10 10L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
      <input
        class="search-input"
        type="search"
        placeholder="Search issues..."
        value={searchInput}
        oninput={onSearchInput}
      />
    </div>
    <button
      class="star-filter-btn"
      class:star-filter-btn--active={getIssueFilterStarred()}
      onclick={() => { setIssueFilterStarred(!getIssueFilterStarred()); void loadIssues(); }}
      title={getIssueFilterStarred() ? "Show all" : "Show starred only"}
    >
      {#if getIssueFilterStarred()}
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
          <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25z"/>
        </svg>
      {:else}
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
          <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25zm0 2.445L6.615 5.5a.75.75 0 01-.564.41l-3.097.45 2.24 2.184a.75.75 0 01.216.664l-.528 3.084 2.769-1.456a.75.75 0 01.698 0l2.77 1.456-.53-3.084a.75.75 0 01.216-.664l2.24-2.183-3.096-.45a.75.75 0 01-.564-.41L8 2.694z"/>
        </svg>
      {/if}
    </button>
    <span class="count-badge">{getIssues().length}</span>
  </div>

  {#if repos.length > 1}
    <div class="filter-chips">
      <button
        class="filter-chip"
        class:active={getIssueFilterRepo() === undefined}
        onclick={() => { setIssueFilterRepo(undefined); void loadIssues(); }}
      >
        All
      </button>
      {#each repos as repo}
        <button
          class="filter-chip"
          class:active={getIssueFilterRepo() === repo}
          onclick={() => {
            setIssueFilterRepo(getIssueFilterRepo() === repo ? undefined : repo);
            void loadIssues();
          }}
        >
          {repo.split("/")[1]}
        </button>
      {/each}
    </div>
  {/if}

  <div class="list-body">
    {#if isIssuesLoading() && getIssues().length === 0}
      <p class="state-message">Loading…</p>
    {:else if getIssuesError() !== null && getIssues().length === 0}
      <p class="state-message state-message--error">Error: {getIssuesError()}</p>
    {:else if getIssues().length === 0 && getSyncState()?.running}
      <div class="state-message sync-message">
        <span class="sync-dot"></span>
        Syncing from GitHub…
      </div>
    {:else if getIssues().length === 0 && !getSyncState()?.last_run_at}
      <p class="state-message">Waiting for first sync…</p>
    {:else if getIssues().length === 0}
      <p class="state-message">No issues found.</p>
    {:else}
      {#each [...issuesByRepo().entries()] as [repo, repoIssues] (repo)}
        <div class="repo-group">
          <h3 class="repo-header">{repo}</h3>
          {#each repoIssues as issue (issue.ID)}
            <IssueItem
              {issue}
              selected={isSelected(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
              onclick={() => handleSelect(issue.repo_owner ?? "", issue.repo_name ?? "", issue.Number)}
            />
          {/each}
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .issue-list {
    display: flex;
    flex-direction: column;
    height: 100%;
    width: 100%;
  }

  .search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
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

  .star-filter-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    cursor: pointer;
    flex-shrink: 0;
    transition: color 0.1s, background 0.1s;
  }

  .star-filter-btn:hover {
    color: var(--accent-amber);
    background: var(--bg-surface-hover);
  }

  .star-filter-btn--active {
    color: var(--accent-amber);
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

  .filter-chips {
    display: flex;
    gap: 4px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-muted);
    overflow-x: auto;
    flex-shrink: 0;
  }

  .filter-chips::-webkit-scrollbar {
    display: none;
  }

  .filter-chip {
    font-size: 11px;
    font-weight: 500;
    padding: 3px 10px;
    border-radius: 10px;
    white-space: nowrap;
    color: var(--text-secondary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    transition: all 0.1s;
    flex-shrink: 0;
    cursor: pointer;
  }

  .filter-chip:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .filter-chip.active {
    background: var(--accent-blue);
    color: #fff;
    border-color: var(--accent-blue);
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
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }

  .repo-group {
    border-bottom: 1px solid var(--border-default);
  }

  .repo-header {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 6px 12px 4px;
    background: var(--bg-inset);
    border-bottom: 1px solid var(--border-muted);
    position: sticky;
    top: 0;
    z-index: 1;
  }
</style>
