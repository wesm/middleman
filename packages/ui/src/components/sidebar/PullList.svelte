<script lang="ts">
  import type { DiffFile } from "../../api/types.js";
  import { getStores, getNavigate, getSidebar, getActions, getHostState } from "../../context.js";
  import { groupByWorkflow } from "../../stores/workflow.svelte.js";
  import PullItem from "./PullItem.svelte";

  const { pulls, sync, diff, grouping, settings } = getStores();
  const navigate = getNavigate();
  const actions = getActions();
  const hostState = getHostState();
  const { isEmbedded, isSidebarToggleEnabled, toggleSidebar } = getSidebar();

  const importAction = $derived(
    (actions.pull ?? []).find(
      (a) => a.id === "import-worktree",
    ),
  );
  const activeWorktreeKey = $derived(
    hostState.getActiveWorktreeKey?.(),
  );
  const groupingMode = $derived(
    grouping.getGroupingMode(),
  );
  const workflowGroups = $derived(
    groupByWorkflow(pulls.getPulls(), activeWorktreeKey),
  );

  interface Props {
    getDetailTab?: () => string;
  }
  const { getDetailTab: _getDetailTab = () => "conversation" }: Props = $props();

  function filename(path: string): string {
    const i = path.lastIndexOf("/");
    return i >= 0 ? path.slice(i + 1) : path;
  }

  interface FileGroup { dir: string; files: DiffFile[] }

  function groupByDir(files: DiffFile[]): FileGroup[] {
    const result: FileGroup[] = [];
    let currentDir: string | null = null;
    let currentFiles: DiffFile[] = [];
    for (const f of files) {
      const i = f.path.lastIndexOf("/");
      const dir = i > 0 ? f.path.slice(0, i) : "";
      if (dir !== currentDir) {
        if (currentFiles.length > 0) result.push({ dir: currentDir ?? "", files: currentFiles });
        currentDir = dir;
        currentFiles = [f];
      } else {
        currentFiles.push(f);
      }
    }
    if (currentFiles.length > 0) result.push({ dir: currentDir ?? "", files: currentFiles });
    return result;
  }

  function statusLetter(s: string): string {
    switch (s) {
      case "modified": return "M";
      case "added": return "A";
      case "deleted": return "D";
      case "renamed": return "R";
      case "copied": return "C";
      default: return "?";
    }
  }

  function statusColor(s: string): string {
    switch (s) {
      case "modified": return "var(--accent-amber)";
      case "added": return "var(--accent-green)";
      case "deleted": return "var(--accent-red)";
      case "renamed":
      case "copied": return "var(--accent-blue)";
      default: return "var(--text-muted)";
    }
  }

  let searchInput = $state(pulls.getSearchQuery() ?? "");
  let debounceHandle: ReturnType<typeof setTimeout> | null = null;
  let refreshHandle: ReturnType<typeof setInterval> | null = null;

  $effect(() => {
    void pulls.loadPulls();

    refreshHandle = setInterval(() => {
      void pulls.loadPulls();
    }, 15_000);

    // If sync is currently running on first load, refresh when it completes
    if (sync.getSyncState()?.running) {
      sync.onNextSyncComplete(() => void pulls.loadPulls());
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
      pulls.setSearchQuery(value.trim() === "" ? undefined : value.trim());
      void pulls.loadPulls();
    }, 300);
  }

  function handleSelect(owner: string, name: string, number: number): void {
    pulls.selectPR(owner, name, number);
    if (_getDetailTab() === "files") {
      navigate(`/pulls/${owner}/${name}/${number}/files`);
    } else {
      navigate(`/pulls/${owner}/${name}/${number}`);
    }
  }

  function isSelected(owner: string, name: string, number: number): boolean {
    const sel = pulls.getSelectedPR();
    return sel !== null && sel.owner === owner && sel.name === name && sel.number === number;
  }
</script>

{#snippet diffFilesInline()}
  <div class="diff-files">
    {#if diff.isDiffLoading() && !diff.getDiff()}
      <div class="diff-files-state diff-files-state--loading">Loading files</div>
    {:else if diff.getDiff()}
      {@const grouped = groupByDir(diff.getDiff()!.files)}
      {#each grouped as group, gi (gi)}
        {#if group.dir}
          <div class="diff-dir-header">{group.dir}/</div>
        {/if}
        {#each group.files as f (f.path)}
          <button
            class="diff-file-row"
            class:diff-file-row--active={diff.getActiveFile() === f.path}
            class:diff-file-row--nested={!!group.dir}
            onclick={() => diff.requestScrollToFile(f.path)}
            title={f.path}
          >
            <span class="diff-file-status" style="color: {statusColor(f.status)}">{statusLetter(f.status)}</span>
            <span class="diff-file-name" class:diff-file-name--deleted={f.status === "deleted"}>{filename(f.path)}</span>
          </button>
        {/each}
      {/each}
    {/if}
  </div>
{/snippet}

<div class="pull-list">
  <div class="filter-bar">
    <span class="count-badge">{pulls.getPulls().length} PRs</span>
    <div class="state-toggle">
      {#each ["open", "closed", "all"] as s (s)}
        <button
          class="state-btn"
          class:state-btn--active={pulls.getFilterState() === s}
          onclick={() => { pulls.setFilterState(s); void pulls.loadPulls(); }}
        >{s === "open" ? "Open" : s === "closed" ? "Closed" : "All"}</button>
      {/each}
    </div>
    <div class="group-toggle">
      <button
        class="group-btn"
        class:group-btn--active={groupingMode === "byRepo"}
        onclick={() => grouping.setGroupingMode("byRepo")}
      >Repo</button>
      <button
        class="group-btn"
        class:group-btn--active={groupingMode === "byWorkflow"}
        onclick={() => grouping.setGroupingMode("byWorkflow")}
      >Status</button>
      <button
        class="group-btn"
        class:group-btn--active={groupingMode === "flat"}
        onclick={() => grouping.setGroupingMode("flat")}
      >All</button>
    </div>
    {#if isSidebarToggleEnabled()}
      <button class="sidebar-toggle" onclick={toggleSidebar} title="Collapse sidebar">
        <svg width="14" height="14" viewBox="0 0 16 16"
          fill="none" stroke="currentColor" stroke-width="1.5">
          <rect x="1" y="1" width="14" height="14" rx="2" />
          <line x1="6" y1="1" x2="6" y2="15" />
          <polyline points="10,6 8,8 10,10"
            stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    {/if}
  </div>
  <div class="search-bar">
    <div class="search-input-wrap">
      <svg class="search-icon" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
        <circle cx="6.5" cy="6.5" r="4.5" stroke="currentColor" stroke-width="1.5" />
        <path d="M10 10L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
      <input
        class="search-input"
        type="search"
        placeholder="Search PRs..."
        value={searchInput}
        oninput={onSearchInput}
      />
    </div>
    <button
      class="star-filter-btn"
      class:star-filter-btn--active={pulls.getFilterStarred()}
      onclick={() => { pulls.setFilterStarred(!pulls.getFilterStarred()); void pulls.loadPulls(); }}
      title={pulls.getFilterStarred() ? "Show all" : "Show starred only"}
    >
      {#if pulls.getFilterStarred()}
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
          <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25z"/>
        </svg>
      {:else}
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
          <path d="M8 .25a.75.75 0 01.673.418l1.882 3.815 4.21.612a.75.75 0 01.416 1.279l-3.046 2.97.719 4.192a.75.75 0 01-1.088.791L8 12.347l-3.766 1.98a.75.75 0 01-1.088-.79l.72-4.194L.818 6.374a.75.75 0 01.416-1.28l4.21-.611L7.327.668A.75.75 0 018 .25zm0 2.445L6.615 5.5a.75.75 0 01-.564.41l-3.097.45 2.24 2.184a.75.75 0 01.216.664l-.528 3.084 2.769-1.456a.75.75 0 01.698 0l2.77 1.456-.53-3.084a.75.75 0 01.216-.664l2.24-2.183-3.096-.45a.75.75 0 01-.564-.41L8 2.694z"/>
        </svg>
      {/if}
    </button>
  </div>

  {#if pulls.getFilterState() !== "open"}
    <p class="state-note">Showing items closed after middleman began tracking them</p>
  {/if}
  <div class="list-body">
    {#if settings.isSettingsLoaded() && !settings.hasConfiguredRepos()}
      <p class="state-message">No repositories configured.<br />
        {#if !isEmbedded()}<button class="settings-link" onclick={() => navigate("/settings")}>Add one in Settings</button>{/if}</p>
    {:else if pulls.isLoading() && pulls.getPulls().length === 0}
      <p class="state-message">Loading…</p>
    {:else if pulls.getError() !== null && pulls.getPulls().length === 0}
      <p class="state-message state-message--error">Error: {pulls.getError()}</p>
    {:else if pulls.getPulls().length === 0 && sync.getSyncState()?.running}
      <div class="state-message sync-message">
        <span class="sync-dot"></span>
        Syncing from GitHub…
      </div>
    {:else if pulls.getPulls().length === 0 && !sync.getSyncState()?.last_run_at}
      <p class="state-message">Waiting for first sync…</p>
    {:else if pulls.getPulls().length === 0}
      <p class="state-message">No pull requests found.</p>
    {:else}
      {#if groupingMode === "byRepo"}
        {#each [...pulls.pullsByRepo().entries()] as [repo, prs] (repo)}
          <div class="repo-group">
            <h3 class="repo-header">{repo}</h3>
            {#each prs as pr (pr.ID)}
              {@const prSelected = isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              <PullItem
                {pr}
                showRepo={false}
                selected={prSelected}
                {importAction}
                onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              />
              {#if prSelected && _getDetailTab() === "files"}
                {@render diffFilesInline()}
              {/if}
            {/each}
          </div>
        {/each}
      {:else if groupingMode === "byWorkflow"}
        {#each workflowGroups as wg (wg.group)}
          <div class="repo-group">
            <h3 class="repo-header">{wg.label}</h3>
            {#each wg.items as pr (pr.ID)}
              {@const prSelected = isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              <PullItem
                {pr}
                showRepo={true}
                selected={prSelected}
                {importAction}
                onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
              />
              {#if prSelected && _getDetailTab() === "files"}
                {@render diffFilesInline()}
              {/if}
            {/each}
          </div>
        {/each}
      {:else}
        {#each pulls.getPulls() as pr (pr.ID)}
          {@const prSelected = isSelected(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
          <PullItem
            {pr}
            showRepo={true}
            selected={prSelected}
            {importAction}
            onclick={() => handleSelect(pr.repo_owner ?? "", pr.repo_name ?? "", pr.Number)}
          />
          {#if prSelected && _getDetailTab() === "files"}
            {@render diffFilesInline()}
          {/if}
        {/each}
      {/if}
    {/if}
  </div>
  <div class="sidebar-footer">
    {#if !isEmbedded()}
      <button class="add-repo-link" onclick={() => navigate("/settings")}>
        + Add repository
      </button>
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

  .filter-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-muted);
    flex-shrink: 0;
    background: var(--bg-surface);
  }

  .filter-bar :global(.sidebar-toggle) {
    margin-left: auto;
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

  .sidebar-footer {
    padding: 8px 12px;
    border-top: 1px solid var(--border-muted);
    flex-shrink: 0;
  }

  .add-repo-link {
    font-size: 12px;
    color: var(--text-muted);
    cursor: pointer;
    transition: color 0.1s;
    padding: 0;
  }

  .add-repo-link:hover {
    color: var(--accent-blue);
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
  .group-toggle {
    display: flex;
    gap: 2px;
    background: var(--bg-inset);
    border-radius: 6px;
    padding: 2px;
    margin-left: auto;
  }
  .group-btn {
    font-size: 11px;
    padding: 2px 8px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    white-space: nowrap;
  }
  .group-btn--active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
  }

  .diff-files {
    border-bottom: 1px solid var(--border-muted);
    padding: 4px 0;
    max-height: 40vh;
    overflow-y: auto;
  }

  .diff-files-state {
    padding: 6px 24px;
    font-size: 11px;
    color: var(--text-muted);
  }

  .diff-files-state--loading {
    animation: pulse 1.5s ease-in-out infinite;
  }

  .diff-dir-header {
    padding: 5px 12px 2px 24px;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .diff-file-row {
    display: flex;
    align-items: center;
    gap: 5px;
    width: 100%;
    padding: 2px 12px 2px 24px;
    text-align: left;
    color: var(--text-secondary);
    transition: background 0.15s ease;
  }

  .diff-file-row--nested {
    padding-left: 36px;
  }

  .diff-file-row:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .diff-file-row--active {
    background: color-mix(in srgb, var(--accent-blue) 10%, transparent);
    color: var(--text-primary);
  }

  .diff-file-status {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    width: 12px;
    flex-shrink: 0;
    text-align: center;
  }

  .diff-file-name {
    font-family: var(--font-mono);
    font-size: 11px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .diff-file-name--deleted {
    text-decoration: line-through;
    opacity: 0.7;
  }
</style>
