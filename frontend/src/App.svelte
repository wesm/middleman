<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import {
    Provider,
    PRListView,
    IssueListView,
    ActivityFeedView,
    KanbanBoardView,
    ReviewsView,
    FocusListView,
  } from "@middleman/ui";
  import type { StoreInstances } from "@middleman/ui";
  import type { ActivityItem } from "@middleman/ui/api/types";
  import { client } from "./lib/api/runtime.js";

  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import StatusBar from "./lib/components/layout/StatusBar.svelte";
  import SettingsPage from "./lib/components/settings/SettingsPage.svelte";
  import WorkspaceTerminalView from "./lib/components/terminal/WorkspaceTerminalView.svelte";
  import DesignSystemPage from "./lib/components/design-system/DesignSystemPage.svelte";
  import FlashBanner from "./lib/components/FlashBanner.svelte";
  import { SpinnerIcon } from "./lib/icons.ts";
  import { showFlash } from "./lib/stores/flash.svelte.js";
  import { initItemRefHandler } from "./lib/utils/itemRefHandler.js";
  import { shouldIgnoreGlobalShortcutTarget } from "./lib/utils/keyboardShortcuts.js";
  import { runAppStartup } from "./lib/utils/appStartup.js";
  import {
    initTheme,
    cleanupTheme,
    reapplyTheme,
  } from "./lib/stores/theme.svelte.js";
  import {
    isSidebarCollapsed,
    getSidebarWidth,
    setSidebarWidth,
    toggleSidebar,
    isSidebarToggleEnabled,
    initSidebar,
    setNarrowOverride,
  } from "./lib/stores/sidebar.svelte.js";
  import {
    initContainerObserver,
    isNarrow,
  } from "./lib/stores/container.svelte.js";
  import {
    getRoute,
    getPage,
    getView,
    navigate,
    replaceUrl,
    getBasePath,
    isDiffView,
    getDetailTab,
    getSelectedPRFromRoute,
  } from "./lib/stores/router.svelte.ts";
  import {
    getGlobalRepo,
    applyConfigRepo,
  } from "./lib/stores/filter.svelte.js";
  import {
    getUIConfig,
    isEmbedded,
    getPullRequestActions,
    getIssueActions,
    getActiveWorktreeKey,
    invokeAction,
    emitWorkspaceCommand,
    isHeaderHidden,
    isStatusBarHidden,
    getInitialRoute,
    emitLayoutChanged,
    initWorkspaceBridge,
  } from "./lib/stores/embed-config.svelte.js";
  import { getSettings } from "./lib/api/settings.js";

  let stores = $state<StoreInstances | undefined>();
  let appReady = $state(false);

  onMount(() => {
    initTheme();
    initSidebar();
    initWorkspaceBridge();
    const initialRoute = getInitialRoute();
    if (initialRoute) {
      replaceUrl(initialRoute);
    }
    const ui = getUIConfig();
    applyConfigRepo(ui.repo, ui.hideRepoSelector);
    const appEl = document.getElementById("app")!;
    const cleanupContainer = initContainerObserver(appEl);
    const cleanupItemRefs = initItemRefHandler();
    const cancelStartup = runAppStartup({
      getSettings,
      getStores: () => stores,
      onReady: () => {
        appReady = true;
      },
    });
    const onBeforeUnload = () => {
      stores?.events.disconnect();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      cancelStartup();
      cleanupTheme();
      cleanupContainer();
      cleanupItemRefs();
      window.removeEventListener(
        "beforeunload",
        onBeforeUnload,
      );
    };
  });

  let lastRepo: string | undefined;

  onDestroy(() => {
    stores?.events.disconnect();
  });

  $effect(() => {
    const repo = getGlobalRepo();
    if (!appReady || !stores) {
      lastRepo = repo;
      return;
    }
    if (repo === lastRepo) return;
    lastRepo = repo;
    void stores.pulls.loadPulls(
      getView() === "board" ? { state: "open" } : undefined,
    );
    void stores.issues.loadIssues();
    void stores.activity.loadActivity();
  });

  $effect(() => {
    if (isSidebarToggleEnabled()) {
      setNarrowOverride(isNarrow());
    }
  });

  $effect(() => {
    reapplyTheme();
  });

  // Sync route state: restore drawer, select items, clear stale.
  $effect(() => {
    if (!stores) return;
    const route = getRoute();
    const page = route.page;

    if (page !== "activity") {
      drawerItem = null;
    } else if (!stores.settings.hasConfiguredRepos()) {
      drawerItem = null;
    } else {
      const sp = new URLSearchParams(window.location.search);
      const sel = sp.get("selected");
      if (sel) {
        const match = sel.match(
          /^(pr|issue):([^/]+)\/([^/]+)\/(\d+)$/,
        );
        if (match) {
          drawerItem = {
            itemType: match[1] as "pr" | "issue",
            owner: match[2]!,
            name: match[3]!,
            number: parseInt(match[4]!, 10),
          };
        }
      } else {
        drawerItem = null;
      }
    }

    if (route.page === "pulls") {
      if (
        "selected" in route &&
        route.selected &&
        stores.settings.hasConfiguredRepos()
      ) {
        stores.pulls.selectPR(
          route.selected.owner,
          route.selected.name,
          route.selected.number,
        );
      } else {
        stores.pulls.clearSelection();
      }
    } else if (route.page === "issues") {
      if (
        route.selected &&
        stores.settings.hasConfiguredRepos()
      ) {
        stores.issues.selectIssue(
          route.selected.owner,
          route.selected.name,
          route.selected.number,
          route.selected.platformHost,
        );
      } else {
        stores.issues.clearIssueSelection();
      }
    }
  });

  let drawerItem = $state<{
    itemType: "pr" | "issue";
    platformHost?: string | undefined;
    owner: string;
    name: string;
    number: number;
  } | null>(null);

  function updateDrawerURL(
    item: typeof drawerItem,
  ): void {
    const sp = new URLSearchParams(
      window.location.search,
    );
    if (item) {
      sp.set(
        "selected",
        `${item.itemType}:${item.owner}/${item.name}/${item.number}`,
      );
    } else {
      sp.delete("selected");
    }
    const qs = sp.toString();
    const base =
      getBasePath().replace(/\/$/, "") || "";
    history.replaceState(
      null,
      "",
      (base || "/") + (qs ? `?${qs}` : ""),
    );
  }

  function handleActivitySelect(
    item: ActivityItem,
  ): void {
    const itemType =
      item.item_type === "issue" ? "issue" : "pr";
    drawerItem = {
      itemType,
      platformHost: item.platform_host,
      owner: item.repo_owner,
      name: item.repo_name,
      number: item.item_number,
    };
    updateDrawerURL(drawerItem);
  }

  function closeDrawer(): void {
    drawerItem = null;
    updateDrawerURL(null);
  }

  function handleSidebarResize(width: number): void {
    setSidebarWidth(width);
    emitLayoutChanged({
      sidebar: { width },
      pinnedPanel: { width: 0, visible: false },
    });
  }

  function navigateToSelectedPR(): void {
    if (!stores) return;
    const sel = stores.pulls.getSelectedPR();
    if (!sel) return;
    const tab = getDetailTab();
    const path =
      tab === "files"
        ? `/pulls/${sel.owner}/${sel.name}/${sel.number}/files`
        : `/pulls/${sel.owner}/${sel.name}/${sel.number}`;
    if (getSelectedPRFromRoute()) {
      replaceUrl(path);
    } else {
      navigate(path);
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (!stores) return;
    const selectionAnchor =
      typeof window !== "undefined"
        ? window.getSelection()?.anchorNode ?? null
        : null;
    const focusedEditor =
      typeof document !== "undefined"
        ? document.querySelector(
            ".ProseMirror-focused, [contenteditable='true']:focus",
          )
        : null;
    if (focusedEditor) {
      return;
    }

    if (
      shouldIgnoreGlobalShortcutTarget(e.target) ||
      shouldIgnoreGlobalShortcutTarget(document.activeElement) ||
      shouldIgnoreGlobalShortcutTarget(selectionAnchor)
    ) {
      return;
    }

    if (
      e.key === "[" &&
      (e.metaKey || e.ctrlKey) &&
      isSidebarToggleEnabled()
    ) {
      e.preventDefault();
      toggleSidebar();
      return;
    }

    const page = getPage();
    if (page === "settings") return;
    if (page === "design-system") return;
    if (page === "reviews") return;
    if (page === "workspaces") return;

    if (page === "activity") {
      if (
        e.key === "Escape" &&
        drawerItem &&
        !e.defaultPrevented
      ) {
        e.preventDefault();
        closeDrawer();
      }
      return;
    }

    if (e.key === "f" && page === "pulls") {
      const sel = getSelectedPRFromRoute();
      if (sel) {
        e.preventDefault();
        const tab = getDetailTab();
        if (tab === "conversation") {
          navigate(
            `/pulls/${sel.owner}/${sel.name}/${sel.number}/files`,
          );
        } else {
          navigate(
            `/pulls/${sel.owner}/${sel.name}/${sel.number}`,
          );
        }
        return;
      }
    }

    const inDiffView = isDiffView();
    const currentRoute = getRoute();
    const isBoardView =
      currentRoute.page === "pulls" &&
      "view" in currentRoute &&
      currentRoute.view === "board";
    const isIssues = page === "issues";

    switch (e.key) {
      case "j":
        if (inDiffView || isBoardView) break;
        e.preventDefault();
        if (isIssues) {
          stores.issues.selectNextIssue();
        } else {
          stores.pulls.selectNextPR();
          navigateToSelectedPR();
        }
        break;
      case "k":
        if (inDiffView || isBoardView) break;
        e.preventDefault();
        if (isIssues) {
          stores.issues.selectPrevIssue();
        } else {
          stores.pulls.selectPrevPR();
          navigateToSelectedPR();
        }
        break;
      case "Escape":
        if (e.defaultPrevented || isBoardView) break;
        e.preventDefault();
        if (isIssues) navigate("/issues");
        else navigate("/pulls");
        break;
      case "1":
        e.preventDefault();
        navigate("/pulls");
        break;
      case "2":
        e.preventDefault();
        navigate("/pulls/board");
        break;
    }
  }

  $effect(() => {
    window.addEventListener("keydown", handleKeydown);
    return () =>
      window.removeEventListener(
        "keydown",
        handleKeydown,
      );
  });
</script>

<Provider
  {client}
  roborevBaseUrl="/api/roborev"
  onError={showFlash}
  onNavigate={(e) =>
    navigate(typeof e === "string" ? e : e.path)}
  onWorkspaceCommand={emitWorkspaceCommand}
  actions={{
    pull: getPullRequestActions().map((a) => ({
      id: a.id,
      label: a.label,
      handler: (ctx) => invokeAction(a, {
        surface: ctx.surface,
        owner: ctx.owner,
        name: ctx.name,
        number: ctx.number,
        ...ctx.meta != null && { meta: ctx.meta },
      }),
    })),
    issue: getIssueActions().map((a) => ({
      id: a.id,
      label: a.label,
      handler: (ctx) => invokeAction(a, {
        surface: ctx.surface,
        owner: ctx.owner,
        name: ctx.name,
        number: ctx.number,
        ...ctx.meta != null && { meta: ctx.meta },
      }),
    })),
  }}
  hostState={{
    getGlobalRepo,
    getGroupByRepo: () => stores?.grouping.getGroupByRepo() ?? true,
    getView,
    getActiveWorktreeKey,
  }}
  config={{
    hideStar: getUIConfig().hideStar,
    basePath: getBasePath(),
  }}
  {getPage}
  sidebar={{
    isEmbedded,
    isSidebarToggleEnabled,
    toggleSidebar,
  }}
  bind:stores
>
  {#if getPage() === "focus"}
    {@const r = getRoute()}
    {#if r.page === "focus"}
      <main class="focus-layout">
        {#if r.itemType === "mrs"}
          <FocusListView
            listType="mrs"
            {...r.repo ? { repo: r.repo } : {}}
          />
        {:else if r.itemType === "issues"}
          <FocusListView
            listType="issues"
            {...r.repo ? { repo: r.repo } : {}}
          />
        {:else if r.itemType === "pr"}
          <PRListView
            selectedPR={{
              owner: r.owner,
              name: r.name,
              number: r.number,
            }}
            detailTab="conversation"
            isSidebarCollapsed={true}
            hideSidebar={true}
          />
        {:else}
          <IssueListView
            selectedIssue={{
              owner: r.owner,
              name: r.name,
              number: r.number,
              platformHost: r.platformHost,
            }}
            isSidebarCollapsed={true}
            hideSidebar={true}
          />
        {/if}
      </main>
    {/if}
  {:else}
    {#if !isHeaderHidden()}
      <AppHeader />
    {/if}
    <FlashBanner />

    <main class="app-main">
      {#if getPage() === "design-system"}
        <DesignSystemPage />
      {:else if !appReady}
        <div class="loading-state">
          <SpinnerIcon
            class="loading-spinner"
            size="18"
            strokeWidth="2"
            aria-hidden="true"
          />
          Loading
        </div>
      {:else if getPage() === "settings"}
        <SettingsPage />
      {:else if getPage() === "activity"}
        <ActivityFeedView
          {drawerItem}
          onSelectItem={handleActivitySelect}
          onCloseDrawer={closeDrawer}
        />
      {:else if getPage() === "pulls"}
        {@const route = getRoute()}
        {#if route.page === "pulls" && route.view === "board"}
          <KanbanBoardView />
        {:else}
          {@const selectedPR =
            getSelectedPRFromRoute() ??
            stores?.pulls.getSelectedPR() ??
            null}
          {@const detailTab = getDetailTab()}
          <PRListView
            {selectedPR}
            {detailTab}
            isSidebarCollapsed={isSidebarCollapsed()}
            sidebarWidth={getSidebarWidth()}
            onSidebarResize={handleSidebarResize}
          />
        {/if}
      {:else if getPage() === "issues"}
        {@const selectedIssue =
          stores?.issues.getSelectedIssue() ?? null}
          <IssueListView
            {selectedIssue}
          isSidebarCollapsed={isSidebarCollapsed()}
          sidebarWidth={getSidebarWidth()}
          onSidebarResize={handleSidebarResize}
        />
      {:else if getPage() === "reviews"}
        {@const route = getRoute()}
        {#if route.page === "reviews" && route.jobId != null}
          <ReviewsView jobId={route.jobId} />
        {:else}
          <ReviewsView />
        {/if}
      {:else if getPage() === "workspaces"}
        <WorkspaceTerminalView workspaceId="" />
      {:else if getPage() === "terminal"}
        {@const r = getRoute()}
        {#if r.page === "terminal"}
          {#key r.workspaceId}
            <WorkspaceTerminalView workspaceId={r.workspaceId} />
          {/key}
        {/if}
      {/if}
    </main>

    {#if !isStatusBarHidden()}
      <StatusBar />
    {/if}
  {/if}
</Provider>

<style>
  .focus-layout {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .app-main {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    position: relative;
  }

  .loading-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    flex: 1;
    color: var(--text-muted);
    font-size: 13px;
    animation: fade-in 0.3s ease;
  }

  :global(.loading-spinner) {
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @keyframes fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
</style>
