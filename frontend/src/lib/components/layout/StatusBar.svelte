<script lang="ts">
  import { getPulls } from "../../stores/pulls.svelte.js";
  import { getIssues } from "../../stores/issues.svelte.js";
  import { getSyncState } from "../../stores/sync.svelte.js";

  // Force re-render every 10s so relative times stay fresh
  let tick = $state(0);
  let tickHandle: ReturnType<typeof setInterval> | null = null;
  $effect(() => {
    tickHandle = setInterval(() => { tick++; }, 10_000);
    return () => { if (tickHandle !== null) clearInterval(tickHandle); };
  });

  function syncText(): string {
    void tick; // reactive dependency
    const st = getSyncState();
    if (st === null) return "";
    if (st.running) {
      if (st.current_repo && st.progress) {
        return `syncing ${st.current_repo} (${st.progress})`;
      }
      return "syncing…";
    }
    if (!st.last_run_at) return "not synced";
    const diffMs = Date.now() - new Date(st.last_run_at).getTime();
    const mins = Math.floor(diffMs / 60_000);
    if (mins < 1) return "synced just now";
    if (mins < 60) return `synced ${mins}m ago`;
    return `synced ${Math.floor(mins / 60)}h ago`;
  }

  function repoCount(): number {
    const repos = new Set<string>();
    for (const pr of getPulls()) repos.add(`${pr.repo_owner}/${pr.repo_name}`);
    for (const issue of getIssues()) repos.add(`${issue.repo_owner}/${issue.repo_name}`);
    return repos.size;
  }
</script>

<footer class="status-bar">
  <div class="status-left">
    <span class="status-item">{getPulls().length} PRs</span>
    <span class="status-sep">·</span>
    <span class="status-item">{getIssues().length} issues</span>
    <span class="status-sep">·</span>
    <span class="status-item">{repoCount()} repos</span>
  </div>
  <div class="status-right">
    {#if getSyncState()?.last_error}
      <span class="status-item status-item--error" title={getSyncState()?.last_error}>sync error</span>
      <span class="status-sep">·</span>
    {/if}
    <span class="status-item" class:status-item--active={getSyncState()?.running}>
      {#if getSyncState()?.running}
        <span class="sync-dot"></span>
      {/if}
      {syncText()}
    </span>
  </div>
</footer>

<style>
  .status-bar {
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--bg-surface);
    border-top: 1px solid var(--border-muted);
    flex-shrink: 0;
    font-size: 10px;
    color: var(--text-muted);
  }
  .status-left, .status-right {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .status-sep {
    color: var(--border-default);
  }
  .status-item--error {
    color: var(--accent-red);
  }
  .status-item--active {
    color: var(--accent-green);
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .sync-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent-green);
    animation: pulse 1.5s ease-in-out infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }
</style>
