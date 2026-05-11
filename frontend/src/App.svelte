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
  import {
    buildPullRequestFilesRoute,
    buildPullRequestRoute,
    type RoutedItemRef,
  } from "@middleman/ui/routes";
  import { client } from "./lib/api/runtime.js";

  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import StatusBar from "./lib/components/layout/StatusBar.svelte";
  import RepoSummaryPage from "./lib/components/repositories/RepoSummaryPage.svelte";
  import SettingsPage from "./lib/components/settings/SettingsPage.svelte";
  import WorkspaceTerminalView from "./lib/components/terminal/WorkspaceTerminalView.svelte";
  import WorkspaceListSidebar from "./lib/components/terminal/WorkspaceListSidebar.svelte";
  import WorkspaceEmbedEmptyState from "./lib/components/terminal/WorkspaceEmbedEmptyState.svelte";
  import WorkspaceFirstRunPanel from "./lib/components/terminal/WorkspaceFirstRunPanel.svelte";
  import WorkspaceProjectCard from "./lib/components/terminal/WorkspaceProjectCard.svelte";
  import { WorkspaceRightSidebar } from "@middleman/ui";
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
    isWorkspaceEmbedPage,
  } from "./lib/stores/router.svelte.ts";
  import {
    buildActivitySelectionSearch,
    parseActivitySelection,
    type ActivityDetailTab,
  } from "./lib/utils/activitySelection.js";
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
      const nextDrawer = parseActivitySelection(
        window.location.search,
      );
      if (!sameActivitySelection(drawerItem, nextDrawer)) {
        drawerItem = nextDrawer;
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
          route.selected.provider,
          route.selected.platformHost,
          route.selected.repoPath,
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
          route.selected.provider,
          route.selected.platformHost,
          route.selected.repoPath,
        );
      } else {
        stores.issues.clearIssueSelection();
      }
    }
  });

  type DrawerItem = RoutedItemRef & {
    detailTab: ActivityDetailTab;
  };

  let drawerItem = $state<DrawerItem | null>(null);

  function sameActivitySelection(
    left: DrawerItem | null,
    right: DrawerItem | null,
  ): boolean {
    if (left === right) return true;
    if (left === null || right === null) return false;
    return left.itemType === right.itemType
      && left.provider === right.provider
      && left.platformHost === right.platformHost
      && left.repoPath === right.repoPath
      && left.owner === right.owner
      && left.name === right.name
      && left.number === right.number
      && left.detailTab === right.detailTab;
  }

  function updateDrawerURL(
    item: DrawerItem | null,
  ): void {
    if (getPage() !== "activity") return;
    const sp = buildActivitySelectionSearch(
      window.location.search,
      item,
    );
    const qs = sp.toString();
    replaceUrl(qs ? `/?${qs}` : "/");
  }

  function handleActivitySelect(
    item: ActivityItem,
  ): void {
    if (!item.repo) {
      throw new Error("activity item missing provider repo identity");
    }
    const itemType =
      item.item_type === "issue" ? "issue" : "pr";
    drawerItem = {
      itemType,
      provider: item.repo.provider,
      platformHost: item.repo.platform_host,
      repoPath: item.repo.repo_path,
      owner: item.repo.owner,
      name: item.repo.name,
      number: item.item_number,
      detailTab: "conversation",
    };
    updateDrawerURL(drawerItem);
  }

  function handleActivityDetailTabChange(
    tab: "conversation" | "files",
  ): void {
    if (!drawerItem || drawerItem.itemType !== "pr") return;
    drawerItem = { ...drawerItem, detailTab: tab };
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
        ? buildPullRequestFilesRoute(sel)
        : buildPullRequestRoute(sel);
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
    if (page === "repos") return;
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
          navigate(buildPullRequestFilesRoute(sel));
        } else {
          navigate(buildPullRequestRoute(sel));
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
  {#if isWorkspaceEmbedPage(getPage())}
    {@const r = getRoute()}
    <main class="embed-layout">
      {#if r.page === "embed-workspace-list"}
        <WorkspaceListSidebar selectedId="" />
      {:else if r.page === "embed-workspace-terminal"}
        <WorkspaceTerminalView
          workspaceId={r.workspaceId}
          hideWorkspaceList={true}
          hideRightSidebar={true}
        />
      {:else if r.page === "embed-workspace-detail"}
        <WorkspaceRightSidebar
          activeTab={r.tab ??
            (r.itemType === "issue" ? "issue" : "pr")}
          workspaceID=""
          provider={r.platformHost.toLowerCase().includes("gitlab")
            ? "gitlab"
            : "github"}
          platformHost={r.platformHost}
          repoOwner={r.owner}
          repoName={r.name}
          repoPath={`${r.owner}/${r.name}`}
          ownerItemType={r.itemType === "issue" ? "issue" : "pull_request"}
          ownerItemNumber={r.number}
          associatedPRNumber={r.itemType === "pr" ? r.number : null}
          branch={r.branch ?? ""}
          roborevBaseUrl={getBasePath().replace(/\/$/, "") +
            "/api/roborev"}
        />
      {:else if r.page === "embed-workspace-empty"}
        <WorkspaceEmbedEmptyState reason={r.reason} />
      {:else if r.page === "embed-workspace-first-run"}
        <WorkspaceFirstRunPanel />
      {:else if r.page === "embed-workspace-project"}
        <WorkspaceProjectCard projectId={r.projectId} />
      {/if}
    </main>
  {:else if getPage() === "focus"}
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
              provider: r.provider,
              platformHost: r.platformHost,
              repoPath: r.repoPath,
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
              provider: r.provider,
              platformHost: r.platformHost,
              repoPath: r.repoPath,
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
          detailTab={drawerItem?.detailTab ?? "conversation"}
          onDetailTabChange={handleActivityDetailTabChange}
        />
      {:else if getPage() === "repos"}
        <RepoSummaryPage />
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
      {:else if getPage() === "workspaces" || getPage() === "terminal"}
        {@const r = getRoute()}
        {@const wsId =
          r.page === "terminal" ? r.workspaceId : ""}
        <!-- Single mount across /workspaces and /terminal/{id};
             WorkspaceTerminalView reacts to workspaceId changes
             internally so the page doesn't flash on navigation. -->
        <WorkspaceTerminalView
          workspaceId={wsId}
          isSidebarCollapsed={isSidebarCollapsed()}
          sidebarWidth={getSidebarWidth()}
          onSidebarResize={handleSidebarResize}
          isSidebarToggleEnabled={isSidebarToggleEnabled()}
          onToggleSidebar={toggleSidebar}
        />
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

  /* Embed routes render a single workspace component at full
     bleed. The host (e.g. a WKWebView) provides the surrounding
     chrome. Hidden overflow lets the inner component manage its
     own scrolling without leaking onto the host. */
  .embed-layout {
    flex: 1;
    overflow: hidden;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
    min-height: 0;
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
