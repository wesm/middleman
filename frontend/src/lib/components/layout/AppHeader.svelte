<script lang="ts">
  import { getPage, getView, navigate } from "../../stores/router.svelte.ts";
  import { getSyncState, triggerSync } from "../../stores/sync.svelte.js";

  let dark = $state(false);

  $effect(() => {
    dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    applyTheme(dark);
  });

  function applyTheme(isDark: boolean): void {
    document.documentElement.classList.toggle("dark", isDark);
  }

  function toggleTheme(): void {
    dark = !dark;
    applyTheme(dark);
  }

  async function handleSync(): Promise<void> {
    if (getSyncState()?.running) return;
    await triggerSync();
  }

  const syncing = $derived(getSyncState()?.running ?? false);
</script>

<header class="app-header">
  <div class="header-left">
    <span class="logo">middleman</span>
  </div>

  <nav class="header-center">
    <div class="tab-group">
      <button class="view-tab" class:active={getPage() === "activity"} onclick={() => { if (getPage() !== "activity") navigate("/"); }}>
        Activity
      </button>
      <button class="view-tab" class:active={getPage() === "pulls"} onclick={() => navigate("/pulls")}>
        PRs
      </button>
      <button class="view-tab" class:active={getPage() === "issues"} onclick={() => navigate("/issues")}>
        Issues
      </button>
    </div>
    {#if getPage() === "pulls"}
      <div class="tab-group">
        <button class="view-tab" class:active={getView() === "list"} onclick={() => navigate("/pulls")}>
          List
        </button>
        <button class="view-tab" class:active={getView() === "board"} onclick={() => navigate("/pulls/board")}>
          Board
        </button>
      </div>
    {/if}
  </nav>

  <div class="header-right">
    <button class="action-btn" onclick={handleSync} disabled={syncing}>
      {syncing ? "Syncing..." : "Sync"}
    </button>
    <button class="action-btn icon-btn" onclick={toggleTheme} title="Toggle theme">
      {dark ? "☀" : "☾"}
    </button>
  </div>
</header>

<style>
  .app-header {
    height: var(--header-height);
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-default);
    display: flex;
    align-items: center;
    padding: 0 16px;
    gap: 16px;
    flex-shrink: 0;
    box-shadow: var(--shadow-sm);
  }

  .header-left {
    flex: 1;
    display: flex;
    align-items: center;
  }

  .logo {
    font-weight: 600;
    font-size: 15px;
    color: var(--text-primary);
    letter-spacing: -0.01em;
  }

  .header-center {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .tab-group {
    display: flex;
    align-items: center;
    gap: 2px;
    background: var(--bg-inset);
    border-radius: var(--radius-md);
    padding: 2px;
  }

  .view-tab {
    padding: 4px 14px;
    border-radius: calc(var(--radius-md) - 2px);
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
  }

  .view-tab:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .view-tab.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    box-shadow: var(--shadow-sm);
  }

  .header-right {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }

  .action-btn {
    padding: 5px 12px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
    background: var(--bg-surface);
    transition: background 0.15s, color 0.15s, border-color 0.15s;
  }

  .action-btn:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
    border-color: var(--border-muted);
  }

  .action-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .icon-btn {
    padding: 5px 10px;
  }
</style>
