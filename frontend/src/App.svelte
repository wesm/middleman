<script lang="ts">
  import { onDestroy } from "svelte";
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
  import type { RoutedItemRef } from "@middleman/ui/routes";
  import { client } from "./lib/api/runtime.js";

  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import StatusBar from "./lib/components/layout/StatusBar.svelte";
  import Palette from "./lib/components/keyboard/Palette.svelte";
  import Cheatsheet from "./lib/components/keyboard/Cheatsheet.svelte";
  import RepoSummaryPage from "./lib/components/repositories/RepoSummaryPage.svelte";
  import SettingsPage from "./lib/components/settings/SettingsPage.svelte";
  import WorkspaceTerminalView from "./lib/components/terminal/WorkspaceTerminalView.svelte";
  import WorkspaceEmbedShell from "./lib/components/terminal/WorkspaceEmbedShell.svelte";
  import DesignSystemPage from "./lib/components/design-system/DesignSystemPage.svelte";
  import FlashBanner from "./lib/components/FlashBanner.svelte";
  import { SpinnerIcon } from "./lib/icons.ts";
  import { showFlash } from "./lib/stores/flash.svelte.js";
  import { initItemRefHandler } from "./lib/utils/itemRefHandler.js";
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
    getDetailTab,
    getSelectedPRFromRoute,
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
    emitLayoutChanged,
    initWorkspaceBridge,
  } from "./lib/stores/embed-config.svelte.js";
  import { getSettings } from "./lib/api/settings.js";
  import { shouldUseFullAppShell } from "./lib/utils/appShell.js";
  import { registerScopedActions } from "./lib/stores/keyboard/registry.svelte.js";
  import {
    defaultActions,
    setStoreInstances,
  } from "./lib/stores/keyboard/actions.js";
  import { dispatchKeydown } from "./lib/stores/keyboard/dispatch.svelte.js";
  import { buildContext } from "./lib/stores/keyboard/context.svelte.js";
  import { registerPRDetailActions } from "./lib/stores/keyboard/pr-detail-actions.js";
  import type { PRDetailActionInput } from "../../packages/ui/src/components/detail/keyboard-actions.js";
  import type { Context } from "./lib/stores/keyboard/types.js";

  let stores = $state<StoreInstances | undefined>();
  let appReady = $state(false);
  let cleanupFullAppShell: (() => void) | undefined;
  let fullShellStores: StoreInstances | undefined;

  function stopFullAppShell() {
    fullShellStores?.events.disconnect();
    cleanupFullAppShell?.();
    cleanupFullAppShell = undefined;
    fullShellStores = undefined;
    appReady = false;
  }

  function startFullAppShell(startupStores: StoreInstances) {
    if (cleanupFullAppShell) return;
    fullShellStores = startupStores;
    appReady = false;
    initTheme();
    initSidebar();
    initWorkspaceBridge();
    const ui = getUIConfig();
    applyConfigRepo(ui.repo, ui.hideRepoSelector);
    const appEl = document.getElementById("app")!;
    const cleanupContainer = initContainerObserver(appEl);
    const cleanupItemRefs = initItemRefHandler();
    const cancelStartup = runAppStartup({
      getSettings,
      getStores: () => startupStores,
      onReady: () => {
        appReady = true;
      },
    });
    const onBeforeUnload = () => {
      stores?.events.disconnect();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    cleanupFullAppShell = () => {
      cancelStartup();
      cleanupTheme();
      cleanupContainer();
      cleanupItemRefs();
      window.removeEventListener(
        "beforeunload",
        onBeforeUnload,
      );
    };
  }

  $effect(() => {
    if (!shouldUseFullAppShell(getPage())) {
      stopFullAppShell();
      return;
    }
    if (stores && stores !== fullShellStores) {
      stopFullAppShell();
      startFullAppShell(stores);
    }
  });

  let lastRepo: string | undefined;

  onDestroy(() => {
    stopFullAppShell();
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
    if (!shouldUseFullAppShell(getPage())) return;
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

  $effect(() => {
    if (!shouldUseFullAppShell(getPage())) return;
    if (!stores) return;
    setStoreInstances(() => stores!);
    const cleanupDefaults = registerScopedActions("app:defaults", defaultActions);
    // Activity-page drawer close is owned by App.svelte because drawerItem and
    // closeDrawer are local to this component. Mirrors the pre-migration
    // behavior where Escape on the activity page closed the open PR drawer.
    const cleanupActivity = registerScopedActions("app:activity-drawer", [
      {
        id: "activity.drawer.close",
        label: "Close activity drawer",
        scope: "global",
        binding: { key: "Escape" },
        priority: 50,
        when: (ctx) => ctx.page === "activity" && drawerItem !== null,
        handler: () => closeDrawer(),
      },
    ]);
    const onKeydown = (e: KeyboardEvent) =>
      dispatchKeydown(e, () => buildContext(stores!));
    window.addEventListener("keydown", onKeydown);
    return () => {
      window.removeEventListener("keydown", onKeydown);
      cleanupActivity();
      cleanupDefaults();
    };
  });

  // PR-detail palette commands (pr.approve, pr.ready, pr.approveWorkflows).
  // Lives here in the app shell because the keyboard registry can't be
  // imported from inside @middleman/ui. The buildPRDetailInput closure
  // assembles the action input from the active PR detail, the loaded
  // capabilities, and the app stores; it returns null when nothing is
  // ready, in which case every action's `when` returns false. pr.merge
  // is intentionally NOT wired (see pr-detail-actions.ts).
  function buildPRDetailInput(ctx: Context): PRDetailActionInput | null {
    if (!stores) return null;
    if (ctx.selectedPR === null) return null;
    const detail = stores.detail.getDetail();
    if (detail === null) return null;
    const sel = ctx.selectedPR;
    // Palette actions only apply to the PR that is actually loaded in
    // the detail pane. If the route-derived selection is for a different
    // PR (mid-route-change, deep link not yet resolved), we treat the
    // input as not ready so `when` returns false.
    const stale =
      detail.repo_owner !== sel.owner
      || detail.repo_name !== sel.name
      || (detail.merge_request?.Number ?? -1) !== sel.number
      || detail.repo?.provider !== sel.provider
      || detail.repo?.platform_host !== sel.platformHost
      || detail.repo?.repo_path !== sel.repoPath;
    if (stale) return null;
    const pr = detail.merge_request;
    const capabilities = detail.repo?.capabilities;
    if (!pr || !capabilities) return null;
    const wfa = detail.workflow_approval;
    const workflowApprovalReady = Boolean(
      capabilities.workflow_approval && wfa?.checked && wfa.required,
    );
    return {
      pr: {
        State: pr.State,
        IsDraft: pr.IsDraft,
        MergeableState: pr.MergeableState,
      },
      ref: {
        provider: sel.provider,
        platformHost: sel.platformHost,
        owner: sel.owner,
        name: sel.name,
        repoPath: sel.repoPath,
      },
      number: sel.number,
      viewerCan: {
        approve: capabilities.review_mutation,
        merge: capabilities.merge_mutation,
        markReady: capabilities.ready_for_review,
        approveWorkflows: workflowApprovalReady,
      },
      // pr.merge is not registered, so repoSettings is not consulted.
      repoSettings: null,
      // Same identity check feeds `stale`; reaching this return means
      // selection and detail agree, so the action is fresh.
      stale: false,
      stores: { pulls: stores.pulls, detail: stores.detail },
      client,
      approveCommentBody: "",
      onError: (msg: string) => showFlash(msg),
    };
  }

  $effect(() => {
    if (!stores) return;
    return registerPRDetailActions(buildPRDetailInput);
  });
</script>

{#if !shouldUseFullAppShell(getPage())}
  <WorkspaceEmbedShell />
{:else}
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

    <Palette />
    <Cheatsheet />
  </Provider>
{/if}

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
