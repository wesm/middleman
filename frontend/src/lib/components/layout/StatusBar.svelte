<script lang="ts">
  import { getStores } from "@middleman/ui";
  import BudgetBars from "./BudgetBars.svelte";
  import BudgetPopover from "./BudgetPopover.svelte";
  import { client } from "../../api/runtime.js";

  const { pulls, issues, sync } = getStores();

  let appVersion = $state("");

  $effect(() => {
    void client.GET("/version")
      .then(({ data }) => { if (data?.version) appVersion = data.version; })
      .catch(() => {});
  });

  let tick = $state(0);
  let tickHandle: ReturnType<typeof setInterval> | null = null;
  $effect(() => {
    tickHandle = setInterval(() => { tick++; }, 10_000);
    return () => { if (tickHandle !== null) clearInterval(tickHandle); };
  });

  function syncText(): string {
    void tick;
    const st = sync.getSyncState();
    if (st === null) return "";
    if (st.running) {
      if (st.progress) {
        return `syncing (${st.progress})`;
      }
      return "syncing\u2026";
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
    for (const pr of pulls.getPulls()) repos.add(`${pr.repo_owner}/${pr.repo_name}`);
    for (const issue of issues.getIssues()) repos.add(`${issue.repo_owner}/${issue.repo_name}`);
    return repos.size;
  }

  let popoverOpen = $state(false);

  function togglePopover() {
    popoverOpen = !popoverOpen;
  }

  function closePopover() {
    popoverOpen = false;
  }

  let rateLimitHosts = $derived.by(() => {
    void tick;
    return sync.getRateLimits();
  });
  let hasHosts = $derived(Object.keys(rateLimitHosts).length > 0);
</script>

<footer class="status-bar">
  <div class="status-left">
    <span class="status-item">{pulls.getPulls().length} PRs</span>
    <span class="status-sep">&middot;</span>
    <span class="status-item">{issues.getIssues().length} issues</span>
    <span class="status-sep">&middot;</span>
    <span class="status-item">{repoCount()} repos</span>
  </div>
  <div class="status-right">
    {#if hasHosts}
      <span class="budget-wrapper">
        <BudgetBars hosts={rateLimitHosts} onclick={togglePopover} expanded={popoverOpen} />
        {#if popoverOpen}
          <BudgetPopover hosts={rateLimitHosts} onclose={closePopover} />
        {/if}
      </span>
      <span class="status-sep">&middot;</span>
    {/if}
    {#if sync.getSyncState()?.last_error}
      <span class="status-item status-item--error" title={sync.getSyncState()?.last_error}>sync error</span>
      <span class="status-sep">&middot;</span>
    {/if}
    <span class="status-item" class:status-item--active={sync.getSyncState()?.running}>
      {#if sync.getSyncState()?.running}
        <span class="sync-dot"></span>
      {/if}
      {syncText()}
    </span>
    {#if appVersion}
      <span class="status-sep">&middot;</span>
      <span class="status-item status-item--version">{appVersion}</span>
    {/if}
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
  .budget-wrapper {
    position: relative;
    display: flex;
    align-items: center;
  }
  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }
</style>
