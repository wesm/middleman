<script lang="ts">
  import { getStores } from "@middleman/ui";

  const { pulls, issues, sync } = getStores();

  const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");

  let appVersion = $state("");

  $effect(() => {
    fetch(`${basePath}/api/v1/version`)
      .then((r) => r.ok ? r.json() : null)
      .then((data) => { if (data?.version) appVersion = data.version; })
      .catch(() => {});
  });

  // Force re-render every 10s so relative times stay fresh
  let tick = $state(0);
  let tickHandle: ReturnType<typeof setInterval> | null = null;
  $effect(() => {
    tickHandle = setInterval(() => { tick++; }, 10_000);
    return () => { if (tickHandle !== null) clearInterval(tickHandle); };
  });

  function syncText(): string {
    void tick; // reactive dependency
    const st = sync.getSyncState();
    if (st === null) return "";
    if (st.running) {
      if (st.progress) {
        return `syncing (${st.progress})`;
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
    for (const pr of pulls.getPulls()) repos.add(`${pr.repo_owner}/${pr.repo_name}`);
    for (const issue of issues.getIssues()) repos.add(`${issue.repo_owner}/${issue.repo_name}`);
    return repos.size;
  }

  function rateLimitText(): {
    text: string;
    level: "normal" | "warning" | "critical";
  } {
    void tick; // reactive dependency
    const hosts = sync.getRateLimits();
    const entries = Object.values(hosts);
    if (entries.length === 0) {
      return { text: "", level: "normal" };
    }

    // Prefer known hosts; fall back to all if none are known yet.
    const knownEntries = entries.filter(h => h.known);
    const pool = knownEntries.length > 0 ? knownEntries : entries;

    // Pick worst-status host from the pool
    let worst = pool[0]!;
    for (const h of pool) {
      if (h.sync_paused && !worst.sync_paused) {
        worst = h;
      } else if (h.sync_throttle_factor > worst.sync_throttle_factor) {
        worst = h;
      }
    }

    if (!worst.known) {
      return { text: "GitHub: --", level: "normal" };
    }

    const globalUsed = worst.rate_limit - worst.rate_remaining;
    const budgetLimit = worst.budget_limit ?? 0;
    const budgetSpent = worst.budget_spent ?? 0;
    let text = `GitHub: ${globalUsed}/${worst.rate_limit} global`;
    if (budgetLimit > 0) {
      text += `, ${budgetSpent}/${budgetLimit} budget`;
    }

    // Reset time
    if (worst.rate_reset_at) {
      const resetMs = new Date(worst.rate_reset_at).getTime() - Date.now();
      if (resetMs > 0) {
        const resetMin = Math.ceil(resetMs / 60_000);
        text += ` · resets ${resetMin}m`;
      }
    }

    // Throttle status
    if (worst.sync_paused) {
      text += " · sync paused";
    } else if (worst.sync_throttle_factor > 1) {
      text += ` · sync ${worst.sync_throttle_factor}x slower`;
    }

    // Color level
    const pct = worst.rate_remaining / worst.rate_limit;
    let level: "normal" | "warning" | "critical" = "normal";
    if (worst.sync_paused || pct < 0.1) {
      level = "critical";
    } else if (pct < 0.5) {
      level = "warning";
    }

    return { text, level };
  }

  let rateInfo = $derived(rateLimitText());
</script>

<footer class="status-bar">
  <div class="status-left">
    <span class="status-item">{pulls.getPulls().length} PRs</span>
    <span class="status-sep">·</span>
    <span class="status-item">{issues.getIssues().length} issues</span>
    <span class="status-sep">·</span>
    <span class="status-item">{repoCount()} repos</span>
  </div>
  <div class="status-right">
    {#if rateInfo.text}
      <span
        class="status-item"
        class:status-item--warning={rateInfo.level === "warning"}
        class:status-item--critical={rateInfo.level === "critical"}
      >
        {rateInfo.text}
      </span>
      <span class="status-sep">·</span>
    {/if}
    {#if sync.getSyncState()?.last_error}
      <span class="status-item status-item--error" title={sync.getSyncState()?.last_error}>sync error</span>
      <span class="status-sep">·</span>
    {/if}
    <span class="status-item" class:status-item--active={sync.getSyncState()?.running}>
      {#if sync.getSyncState()?.running}
        <span class="sync-dot"></span>
      {/if}
      {syncText()}
    </span>
    {#if appVersion}
      <span class="status-sep">·</span>
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
  .status-item--warning {
    color: var(--accent-amber);
  }
  .status-item--critical {
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
