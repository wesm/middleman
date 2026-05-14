<script lang="ts">
  import { getStores, KbdBadge } from "@middleman/ui";
  import {
    getBasePath,
    getPage,
    getView,
    navigate,
  } from "../../stores/router.svelte.ts";
  import {
    activitySelectionToRoute,
    parseActivitySelection,
  } from "../../utils/activitySelection.js";
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

  const appIconSrc = `${getBasePath().replace(/\/$/, "")}/favicon.svg`;

  const hasSidebarStrip = $derived(
    getPage() === "issues"
    || (getPage() === "pulls" && getView() === "list")
    || getPage() === "workspaces"
    || getPage() === "terminal",
  );

  const stores = getStores();
  const { sync } = stores;

  async function handleSync(): Promise<void> {
    if (sync.getSyncState()?.running) return;
    await sync.triggerSync();
  }

  const syncing = $derived(sync.getSyncState()?.running ?? false);

  function routeForTab(
    destination: "pulls" | "issues",
  ): string {
    const selected = getPage() === "activity"
      ? parseActivitySelection(window.location.search)
      : null;
    return activitySelectionToRoute(selected, destination)
      ?? `/${destination}`;
  }

  function navigateTab(
    destination:
      | "activity"
      | "repos"
      | "pulls"
      | "issues"
      | "board"
      | "reviews"
      | "workspaces"
      | "settings"
      | "design-system",
  ): void {
    if (destination === "activity") navigate("/");
    else if (destination === "repos") navigate("/repos");
    else if (destination === "pulls" || destination === "issues") {
      navigate(routeForTab(destination));
    } else if (destination === "board") navigate("/pulls/board");
    else if (destination === "reviews") navigate("/reviews");
    else if (destination === "workspaces") navigate("/workspaces");
    else if (destination === "settings") navigate("/settings");
    else if (destination === "design-system") navigate("/design-system");
  }
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
        <KbdBadge binding={{ key: "[", ctrlOrMeta: true }} />
      </HeaderIconButton>
    {/if}
    <span class="brand">
      <img class="app-icon" src={appIconSrc} alt="" aria-hidden="true" />
      <span class="logo">middleman</span>
    </span>
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
          else if (v === "repos") navigateTab("repos");
          else if (v === "pulls") navigateTab("pulls");
          else if (v === "issues") navigateTab("issues");
          else if (v === "board") navigateTab("board");
          else if (v === "reviews") navigateTab("reviews");
          else if (v === "workspaces" || v === "terminal") navigateTab("workspaces");
          else if (v === "settings") navigateTab("settings");
          else if (v === "design-system") navigateTab("design-system");
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
        <button class="view-tab" class:active={getPage() === "activity"} onclick={() => { if (getPage() !== "activity") navigateTab("activity"); }}>
          Activity
        </button>
        <button class="view-tab" class:active={getPage() === "repos"} onclick={() => navigateTab("repos")}>
          Repos
        </button>
        <button class="view-tab" class:active={getPage() === "pulls"} onclick={() => navigateTab("pulls")}>
          PRs
        </button>
        <button class="view-tab" class:active={getPage() === "issues"} onclick={() => navigateTab("issues")}>
          Issues
        </button>
        <button class="view-tab" class:active={getView() === "board"} onclick={() => navigateTab("board")}>
          Board
        </button>
        <button class="view-tab"
          class:active={getPage() === "reviews"}
          onclick={() => navigateTab("reviews")}>
          Reviews
          {#if stores.roborevDaemon && !stores.roborevDaemon.isAvailable()}
            <span class="daemon-indicator" title="Daemon unavailable"></span>
          {/if}
        </button>
        <button
          class="view-tab"
          class:active={getPage() === "workspaces" || getPage() === "terminal"}
          onclick={() => navigateTab("workspaces")}
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
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    flex-shrink: 0;
  }

  .app-icon {
    display: block;
    width: 22px;
    height: 22px;
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

  :global(#app.container-narrow) .app-header {
    height: auto;
    min-height: 82px;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 8px;
    padding: 6px 10px;
  }

  :global(#app.container-narrow) .header-left {
    flex: 1 1 100%;
    gap: 8px;
    order: 1;
  }

  :global(#app.container-narrow) .brand {
    gap: 6px;
  }

  :global(#app.container-narrow) .app-icon {
    width: 20px;
    height: 20px;
  }

  :global(#app.container-narrow) .logo {
    font-size: 14px;
  }

  :global(#app.container-narrow) .header-left :global(.typeahead) {
    flex: 1 1 auto;
    min-width: 0;
    max-width: none;
  }

  :global(#app.container-narrow) .header-left :global(.typeahead-trigger),
  :global(#app.container-narrow) .header-left :global(.typeahead-input) {
    height: 30px;
  }

  :global(#app.container-narrow) .header-center {
    flex: 1 1 min(190px, 100%);
    min-width: 0;
    order: 2;
  }

  :global(#app.container-narrow) .nav-select {
    width: 100%;
    min-height: 32px;
    font-size: 13px;
  }

  :global(#app.container-narrow) .header-right {
    flex: 0 0 auto;
    order: 3;
    gap: 6px;
  }

  :global(#app.container-narrow) .action-btn {
    min-height: 32px;
    padding-inline: 10px;
  }
</style>
