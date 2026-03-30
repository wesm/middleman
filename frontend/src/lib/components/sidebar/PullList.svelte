<script lang="ts">
  import {
    pullsByRepo,
    isLoading,
    getError,
    loadPulls,
    getPulls,
    getSelectedPR,
    selectPR,
    getSearchQuery,
    setSearchQuery,
    getFilterRepo,
    setFilterRepo,
  } from "../../stores/pulls.svelte.js";
  import { listRepos } from "../../api/client.js";
  import PullItem from "./PullItem.svelte";

  let searchInput = $state(getSearchQuery() ?? "");
  let debounceHandle: ReturnType<typeof setTimeout> | null = null;
  let repos = $state<string[]>([]);

  $effect(() => {
    void loadPulls();
    listRepos().then((r) => {
      repos = r.map((repo) => `${repo.Owner}/${repo.Name}`);
    });
  });

  function onSearchInput(e: Event): void {
    const value = (e.target as HTMLInputElement).value;
    searchInput = value;

    if (debounceHandle !== null) clearTimeout(debounceHandle);
    debounceHandle = setTimeout(() => {
      setSearchQuery(value.trim() === "" ? undefined : value.trim());
      void loadPulls();
    }, 300);
  }

  function handleSelect(owner: string, name: string, number: number): void {
    selectPR(owner, name, number);
  }

  function isSelected(owner: string, name: string, number: number): boolean {
    const sel = getSelectedPR();
    return sel !== null && sel.owner === owner && sel.name === name && sel.number === number;
  }
</script>

<div class="pull-list">
  <div class="search-bar">
    <div class="search-input-wrap">
      <svg class="search-icon" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
        <circle cx="6.5" cy="6.5" r="4.5" stroke="currentColor" stroke-width="1.5" />
        <path d="M10 10L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
      <input
        class="search-input"
        type="search"
        placeholder="Search PRs…"
        value={searchInput}
        oninput={onSearchInput}
      />
    </div>
    <span class="count-badge">{getPulls().length}</span>
  </div>

  {#if repos.length > 1}
    <div class="filter-chips">
      <button
        class="filter-chip"
        class:active={getFilterRepo() === undefined}
        onclick={() => { setFilterRepo(undefined); void loadPulls(); }}
      >
        All
      </button>
      {#each repos as repo}
        <button
          class="filter-chip"
          class:active={getFilterRepo() === repo}
          onclick={() => {
            setFilterRepo(getFilterRepo() === repo ? undefined : repo);
            void loadPulls();
          }}
        >
          {repo.split("/")[1]}
        </button>
      {/each}
    </div>
  {/if}

  <div class="list-body">
    {#if isLoading()}
      <p class="state-message">Loading…</p>
    {:else if getError() !== null}
      <p class="state-message state-message--error">Error: {getError()}</p>
    {:else if getPulls().length === 0}
      <p class="state-message">No pull requests found.</p>
    {:else}
      {#each [...pullsByRepo().entries()] as [repo, prs] (repo)}
        <div class="repo-group">
          <h3 class="repo-header">{repo}</h3>
          {#each prs as pr (pr.ID)}
            <PullItem
              {pr}
              selected={isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
            />
          {/each}
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .pull-list {
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
