<script lang="ts">
  import { onMount } from "svelte";
  import { getPage, getView, navigate } from "../../stores/router.svelte.ts";
  import { getSyncState, triggerSync } from "../../stores/sync.svelte.js";

  const THEME_KEY = "middleman-theme";

  function storedTheme(): string | null {
    const v = localStorage.getItem(THEME_KEY);
    if (v === "dark" || v === "light") return v;
    if (v !== null) localStorage.removeItem(THEME_KEY);
    return null;
  }

  function loadInitialTheme(): boolean {
    const stored = storedTheme();
    if (stored !== null) return stored === "dark";
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }

  let dark = $state(loadInitialTheme());

  onMount(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");

    const handleChange = (event: MediaQueryListEvent) => {
      if (storedTheme() === null) {
        dark = event.matches;
      }
    };

    mediaQuery.addEventListener("change", handleChange);

    return () => {
      mediaQuery.removeEventListener("change", handleChange);
    };
  });

  $effect(() => {
    document.documentElement.classList.toggle("dark", dark);
  });

  function toggleTheme(): void {
    dark = !dark;
    localStorage.setItem(THEME_KEY, dark ? "dark" : "light");
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
      <button class="view-tab" class:active={getView() === "board"} onclick={() => navigate("/pulls/board")}>
        Board
      </button>
    </div>
  </nav>

  <div class="header-right">
    <button class="action-btn" onclick={handleSync} disabled={syncing}>
      {syncing ? "Syncing..." : "Sync"}
    </button>
    <button class="action-btn icon-btn" onclick={toggleTheme} title="Toggle theme">
      {dark ? "☀" : "☾"}
    </button>
    <button
      class="action-btn icon-btn"
      class:active={getPage() === "settings"}
      onclick={() => navigate("/settings")}
      title="Settings"
    >
      <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
        <path d="M8 4.754a3.246 3.246 0 100 6.492 3.246 3.246 0 000-6.492zM5.754 8a2.246 2.246 0 114.492 0 2.246 2.246 0 01-4.492 0z"/>
        <path d="M9.796 1.343c-.527-1.79-3.065-1.79-3.592 0l-.094.319a.873.873 0 01-1.255.52l-.292-.16c-1.64-.892-3.433.902-2.54 2.541l.159.292a.873.873 0 01-.52 1.255l-.319.094c-1.79.527-1.79 3.065 0 3.592l.319.094a.873.873 0 01.52 1.255l-.16.292c-.892 1.64.901 3.434 2.541 2.54l.292-.159a.873.873 0 011.255.52l.094.319c.527 1.79 3.065 1.79 3.592 0l.094-.319a.873.873 0 011.255-.52l.292.16c1.64.893 3.434-.902 2.54-2.541l-.159-.292a.873.873 0 01.52-1.255l.319-.094c1.79-.527 1.79-3.065 0-3.592l-.319-.094a.873.873 0 01-.52-1.255l.16-.292c.893-1.64-.902-3.433-2.541-2.54l-.292.159a.873.873 0 01-1.255-.52l-.094-.319zm-2.633.283c.246-.835 1.428-.835 1.674 0l.094.319a1.873 1.873 0 002.693 1.115l.291-.16c.764-.415 1.6.42 1.184 1.185l-.159.292a1.873 1.873 0 001.116 2.692l.318.094c.835.246.835 1.428 0 1.674l-.319.094a1.873 1.873 0 00-1.115 2.693l.16.291c.415.764-.421 1.6-1.185 1.184l-.291-.159a1.873 1.873 0 00-2.693 1.116l-.094.318c-.246.835-1.428.835-1.674 0l-.094-.319a1.873 1.873 0 00-2.692-1.115l-.292.16c-.764.415-1.6-.421-1.184-1.185l.159-.291A1.873 1.873 0 001.945 8.93l-.319-.094c-.835-.246-.835-1.428 0-1.674l.319-.094A1.873 1.873 0 003.06 4.377l-.16-.292c-.415-.764.42-1.6 1.185-1.184l.292.159a1.873 1.873 0 002.692-1.115l.094-.319z"/>
      </svg>
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
