<script lang="ts">
  import { onMount } from "svelte";
  import AppHeader from "./lib/components/layout/AppHeader.svelte";
  import StatusBar from "./lib/components/layout/StatusBar.svelte";
  import PullList from "./lib/components/sidebar/PullList.svelte";
  import PullDetail from "./lib/components/detail/PullDetail.svelte";
  import IssueList from "./lib/components/sidebar/IssueList.svelte";
  import IssueDetail from "./lib/components/detail/IssueDetail.svelte";
  import KanbanBoard from "./lib/components/kanban/KanbanBoard.svelte";
  import ActivityFeed from "./lib/components/ActivityFeed.svelte";
  import DetailDrawer from "./lib/components/DetailDrawer.svelte";
  import SettingsPage from "./lib/components/settings/SettingsPage.svelte";
  import FlashBanner from "./lib/components/FlashBanner.svelte";
  import DiffView from "./lib/components/diff/DiffView.svelte";
  import { initItemRefHandler } from "./lib/utils/itemRefHandler.js";
  import { getRoute, getPage, getView, navigate, replaceUrl, getBasePath, isDiffView, getDetailTab, getSelectedPRFromRoute } from "./lib/stores/router.svelte.ts";
  import { startPolling } from "./lib/stores/sync.svelte.js";
  import { getSettings } from "./lib/api/settings.js";
  import { hydrateActivityDefaults } from "./lib/stores/activity.svelte.js";
  import { setConfiguredRepos, hasConfiguredRepos } from "./lib/stores/settings.svelte.js";
  import {
    getSelectedPR,
    selectNextPR,
    selectPrevPR,
    clearSelection,
    selectPR,
  } from "./lib/stores/pulls.svelte.js";
  import {
    getSelectedIssue,
    selectNextIssue,
    selectPrevIssue,
    clearIssueSelection,
    selectIssue,
  } from "./lib/stores/issues.svelte.js";
  import type { ActivityItem } from "./lib/api/types.js";

  let drawerItem = $state<{
    itemType: "pr" | "issue";
    owner: string;
    name: string;
    number: number;
  } | null>(null);

  import { loadPulls } from "./lib/stores/pulls.svelte.js";
  import { loadIssues } from "./lib/stores/issues.svelte.js";
  import { getGlobalRepo } from "./lib/stores/filter.svelte.js";
  import { loadActivity } from "./lib/stores/activity.svelte.js";

  let appReady = $state(false);

  onMount(() => {
    const cleanupItemRefs = initItemRefHandler();
    void (async () => {
      try {
        const settings = await getSettings();
        setConfiguredRepos(settings.repos);
        hydrateActivityDefaults(settings.activity);
      } catch (err) {
        console.warn("Failed to load settings, using defaults:", err);
      }
      appReady = true;
      startPolling();
      void loadPulls();
      void loadIssues();
    })();
    return () => {
      cleanupItemRefs();
    };
  });

  let lastRepo: string | undefined;
  $effect(() => {
    const repo = getGlobalRepo();
    if (!appReady) {
      lastRepo = repo;
      return;
    }
    if (repo === lastRepo) return;
    lastRepo = repo;
    void loadPulls(getView() === "board" ? { state: "open" } : undefined);
    void loadIssues();
    void loadActivity();
  });

  // Sync route state: restore drawer, select items, clear stale state.
  $effect(() => {
    const route = getRoute();
    const page = route.page;

    // Clear drawer when leaving activity page.
    if (page !== "activity") {
      drawerItem = null;
    } else if (!hasConfiguredRepos()) {
      drawerItem = null;
    } else {
      // Restore drawer from URL (/?selected=pr:owner/name/42).
      const sp = new URLSearchParams(window.location.search);
      const sel = sp.get("selected");
      if (sel) {
        const match = sel.match(/^(pr|issue):([^/]+)\/([^/]+)\/(\d+)$/);
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

    // Sync selection from route, clear when no item selected.
    if (route.page === "pulls") {
      if ("view" in route && route.view === "diff") {
        selectPR(route.owner, route.name, route.number);
      } else if ("selected" in route && route.selected && hasConfiguredRepos()) {
        selectPR(route.selected.owner, route.selected.name, route.selected.number);
      } else {
        clearSelection();
      }
    } else if (route.page === "issues") {
      if (route.selected && hasConfiguredRepos()) {
        selectIssue(route.selected.owner, route.selected.name, route.selected.number);
      } else {
        clearIssueSelection();
      }
    }
  });

  function updateDrawerURL(item: typeof drawerItem): void {
    const sp = new URLSearchParams(window.location.search);
    if (item) {
      sp.set("selected", `${item.itemType}:${item.owner}/${item.name}/${item.number}`);
    } else {
      sp.delete("selected");
    }
    const qs = sp.toString();
    const base = getBasePath().replace(/\/$/, "") || "";
    history.replaceState(null, "", (base || "/") + (qs ? `?${qs}` : ""));
  }

  function handleActivitySelect(item: ActivityItem): void {
    const itemType = item.item_type === "issue" ? "issue" : "pr";
    drawerItem = {
      itemType,
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

  function navigateToSelectedPR(): void {
    const sel = getSelectedPR();
    if (!sel) return;
    const tab = getDetailTab();
    const path = tab === "files"
      ? `/pulls/${sel.owner}/${sel.name}/${sel.number}/files`
      : `/pulls/${sel.owner}/${sel.name}/${sel.number}`;
    // Push on first selection so Back returns to the list; replace after.
    if (getSelectedPRFromRoute()) {
      replaceUrl(path);
    } else {
      navigate(path);
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    const tag = (e.target as HTMLElement).tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

    const page = getPage();
    if (page === "settings") return;

    if (page === "activity") {
      if (e.key === "Escape" && drawerItem && !e.defaultPrevented) {
        e.preventDefault();
        closeDrawer();
      }
      return;
    }

    // Toggle between conversation and files tabs.
    if (e.key === "f" && page === "pulls") {
      const sel = getSelectedPRFromRoute();
      if (sel) {
        e.preventDefault();
        const tab = getDetailTab();
        if (tab === "conversation") {
          navigate(`/pulls/${sel.owner}/${sel.name}/${sel.number}/files`);
        } else {
          navigate(`/pulls/${sel.owner}/${sel.name}/${sel.number}`);
        }
        return;
      }
    }

    // DiffView handles its own j/k for file-level navigation.
    const inDiffView = isDiffView();
    const currentRoute = getRoute();
    const isBoardView = currentRoute.page === "pulls" && "view" in currentRoute && currentRoute.view === "board";
    const isIssues = page === "issues";

    switch (e.key) {
      case "j":
        if (inDiffView || isBoardView) break;
        e.preventDefault();
        if (isIssues) {
          selectNextIssue();
        } else {
          selectNextPR();
          navigateToSelectedPR();
        }
        break;
      case "k":
        if (inDiffView || isBoardView) break;
        e.preventDefault();
        if (isIssues) {
          selectPrevIssue();
        } else {
          selectPrevPR();
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
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

<AppHeader />
<FlashBanner />

<main class="app-main">
  {#if !appReady}
    <div class="loading-state">Loading...</div>
  {:else if getPage() === "settings"}
    <SettingsPage />
  {:else if getPage() === "activity"}
    <ActivityFeed onSelectItem={handleActivitySelect} />
    {#if drawerItem}
      <DetailDrawer
        itemType={drawerItem.itemType}
        owner={drawerItem.owner}
        name={drawerItem.name}
        number={drawerItem.number}
        onClose={closeDrawer}
      />
    {/if}
  {:else if getPage() === "pulls"}
    {@const route = getRoute()}
    {#if route.page === "pulls" && route.view === "board"}
      <div class="board-layout">
        <KanbanBoard />
      </div>
    {:else}
      {@const selectedPR = getSelectedPRFromRoute() ?? getSelectedPR()}
      {@const detailTab = getDetailTab()}
      <div class="list-layout">
        <aside class="sidebar">
          <PullList />
        </aside>
        <section class="detail-area" class:detail-area--empty={selectedPR === null}>
          {#if selectedPR !== null}
            <div class="detail-tabs">
              <button
                class="detail-tab"
                class:detail-tab--active={detailTab === "conversation"}
                onclick={() => navigate(`/pulls/${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}`)}
              >
                Conversation
              </button>
              <button
                class="detail-tab"
                class:detail-tab--active={detailTab === "files"}
                onclick={() => navigate(`/pulls/${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}/files`)}
              >
                Files changed
              </button>
            </div>
            {#if detailTab === "files"}
              {#key `${selectedPR.owner}/${selectedPR.name}/${selectedPR.number}`}
                <DiffView owner={selectedPR.owner} name={selectedPR.name} number={selectedPR.number} inline />
              {/key}
            {:else}
              <PullDetail owner={selectedPR.owner} name={selectedPR.name} number={selectedPR.number} />
            {/if}
          {:else}
            <div class="placeholder-content">
              <p class="placeholder-text">Select a PR</p>
              <p class="placeholder-hint">j/k to navigate · 1/2 to switch views</p>
            </div>
          {/if}
        </section>
      </div>
    {/if}
  {:else}
    <div class="list-layout">
      <aside class="sidebar">
        <IssueList />
      </aside>
      <section class="detail-area" class:detail-area--empty={getSelectedIssue() === null}>
        {#if getSelectedIssue() !== null}
          {@const sel = getSelectedIssue()!}
          <IssueDetail owner={sel.owner} name={sel.name} number={sel.number} />
        {:else}
          <div class="placeholder-content">
            <p class="placeholder-text">Select an issue</p>
            <p class="placeholder-hint">j/k to navigate</p>
          </div>
        {/if}
      </section>
    </div>
  {/if}
</main>

<StatusBar />

<style>
  .app-main {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    position: relative;
  }

  .list-layout {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .sidebar {
    width: 340px;
    flex-shrink: 0;
    background: var(--bg-surface);
    border-right: 1px solid var(--border-default);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .detail-area {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .detail-area--empty {
    align-items: center;
    justify-content: center;
  }

  .board-layout {
    flex: 1;
    overflow: hidden;
    background: var(--bg-primary);
    display: flex;
    flex-direction: column;
  }

  .placeholder-content {
    text-align: center;
  }

  .placeholder-text {
    color: var(--text-muted);
    font-size: 13px;
  }

  .placeholder-hint {
    color: var(--text-muted);
    font-size: 11px;
    margin-top: 8px;
    opacity: 0.7;
  }

  .loading-state {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 1;
    color: var(--text-muted);
    font-size: 13px;
  }

  .detail-tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border-default);
    background: var(--bg-surface);
    flex-shrink: 0;
  }

  .detail-tab {
    font-size: 12px;
    font-weight: 500;
    padding: 8px 16px;
    color: var(--text-secondary);
    border-bottom: 2px solid transparent;
    transition: color 0.1s, border-color 0.1s;
  }

  .detail-tab:hover {
    color: var(--text-primary);
    background: var(--bg-surface-hover);
  }

  .detail-tab--active {
    color: var(--text-primary);
    border-bottom-color: var(--accent-blue);
  }
</style>
