<script lang="ts">
  import { getStores } from "@middleman/ui";
  import { getPage, getView, navigate } from "../../stores/router.svelte.ts";
  import RepoTypeahead from "../RepoTypeahead.svelte";
  import HeaderIconButton from "./HeaderIconButton.svelte";
  import {
    MoonIcon,
    SettingsIcon,
    SidebarToggleIcon,
    SunIcon,
  } from "../../icons.ts";
  import { getGlobalRepo, setGlobalRepo } from "../../stores/filter.svelte.js";
  import { isEmbedded, getUIConfig } from "../../stores/embed-config.svelte.js";
  import { isNarrow } from "../../stores/container.svelte.js";
  import {
    isDark, toggleTheme, isThemeToggleVisible,
  } from "../../stores/theme.svelte.js";
  import {
    isSidebarCollapsed,
    toggleSidebar,
    isSidebarToggleEnabled,
  } from "../../stores/sidebar.svelte.js";

  const hasSidebarStrip = $derived(
    getPage() === "issues"
    || (getPage() === "pulls" && getView() === "list"),
  );

  const stores = getStores();
  const { sync } = stores;

  async function handleSync(): Promise<void> {
    if (sync.getSyncState()?.running) return;
    await sync.triggerSync();
  }

  const syncing = $derived(sync.getSyncState()?.running ?? false);
</script>

<header class="app-header">
  <div class="header-left">
    {#if isSidebarCollapsed() && isSidebarToggleEnabled() && !hasSidebarStrip}
      <HeaderIconButton
        onclick={toggleSidebar}
        title="Expand sidebar"
      >
        <SidebarToggleIcon
          size="14"
          strokeWidth="1.5"
          aria-hidden="true"
        />
      </HeaderIconButton>
    {/if}
    <span class="logo">middleman</span>
    {#if !getUIConfig().hideRepoSelector}
      <RepoTypeahead
        selected={getGlobalRepo()}
        onchange={setGlobalRepo}
      />
    {/if}
  </div>

  <nav class="header-center">
    {#if isNarrow()}
      <select
        class="nav-select"
        value={getPage() === "pulls" && getView() === "board" ? "board" : getPage()}
        onchange={(e) => {
          const v = (e.target as HTMLSelectElement).value;
          if (v === "activity") navigate("/");
          else if (v === "repos") navigate("/repos");
          else if (v === "pulls") navigate("/pulls");
          else if (v === "issues") navigate("/issues");
          else if (v === "board") navigate("/pulls/board");
          else if (v === "reviews") navigate("/reviews");
          else if (v === "workspaces" || v === "terminal") navigate("/workspaces");
          else if (v === "settings") navigate("/settings");
          else if (v === "design-system") navigate("/design-system");
        }}
      >
        <option value="activity">Activity</option>
        <option value="repos">Repos</option>
        <option value="pulls">PRs</option>
        <option value="issues">Issues</option>
        <option value="board">Board</option>
        <option value="reviews">Reviews</option>
        <option value="workspaces">Workspaces</option>
        {#if getPage() === "design-system"}
          <option value="design-system">Design system</option>
        {/if}
        {#if getPage() === "terminal"}
          <option value="terminal">Workspaces</option>
        {/if}
        {#if !isEmbedded() && getPage() === "settings"}
          <option value="settings">Settings</option>
        {/if}
      </select>
    {:else}
      <div class="tab-group">
        <button class="view-tab" class:active={getPage() === "activity"} onclick={() => { if (getPage() !== "activity") navigate("/"); }}>
          Activity
        </button>
        <button class="view-tab" class:active={getPage() === "repos"} onclick={() => navigate("/repos")}>
          Repos
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
        <button class="view-tab"
          class:active={getPage() === "reviews"}
          onclick={() => navigate("/reviews")}>
          Reviews
          {#if stores.roborevDaemon && !stores.roborevDaemon.isAvailable()}
            <span class="daemon-indicator" title="Daemon unavailable"></span>
          {/if}
        </button>
        <button
          class="view-tab"
          class:active={getPage() === "workspaces" || getPage() === "terminal"}
          onclick={() => navigate("/workspaces")}
        >Workspaces</button>
      </div>
    {/if}
  </nav>

  <div class="header-right">
    {#if !getUIConfig().hideSync}
      <button class="action-btn" onclick={handleSync} disabled={syncing}>
        {syncing ? "Syncing..." : "Sync"}
      </button>
    {/if}
    {#if isThemeToggleVisible()}
      <HeaderIconButton onclick={toggleTheme} title="Toggle theme">
        {#if isDark()}
          <SunIcon size="14" aria-hidden="true" />
        {:else}
          <span data-filled-icon="moon">
            <MoonIcon size="14" aria-hidden="true" />
          </span>
        {/if}
      </HeaderIconButton>
    {/if}
    {#if !isEmbedded()}
      <HeaderIconButton
        active={getPage() === "settings"}
        onclick={() => navigate("/settings")}
        title="Settings"
      >
        <SettingsIcon size="14" strokeWidth="1.75" aria-hidden="true" />
      </HeaderIconButton>
    {/if}
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
    gap: 12px;
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

  .nav-select {
    font-size: 12px;
    padding: 4px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  .daemon-indicator {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text-muted);
    margin-left: 4px;
    vertical-align: middle;
    opacity: 0.6;
  }

  [data-filled-icon="moon"] :global(svg path) {
    fill: currentColor;
    stroke: none;
  }
</style>
