<script lang="ts">
  import { setContext } from "svelte";
  import {
    API_CLIENT_KEY, ACTIONS_KEY, NAVIGATE_KEY, EVENT_KEY,
    PREPARE_ROUTE_KEY, STORES_KEY, UI_CONFIG_KEY, SIDEBAR_KEY,
    HOST_STATE_KEY,
  } from "./context.js";
  import type {
    MiddlemanClient, ActionRegistry, NavigateCallback,
    EventCallback, PrepareRouteCallback, HostStateAccessors,
    StoreInstances, UIConfig, SidebarAccessors,
  } from "./types.js";
  import type {
    PullsStoreOptions,
  } from "./stores/pulls.svelte.js";
  import type {
    IssuesStoreOptions,
  } from "./stores/issues.svelte.js";
  import type {
    DetailStoreOptions,
  } from "./stores/detail.svelte.js";
  import type {
    ActivityStoreOptions,
  } from "./stores/activity.svelte.js";
  import type {
    DiffStoreOptions,
  } from "./stores/diff.svelte.js";
  import {
    createPullsStore,
  } from "./stores/pulls.svelte.js";
  import {
    createIssuesStore,
  } from "./stores/issues.svelte.js";
  import {
    createDetailStore,
  } from "./stores/detail.svelte.js";
  import {
    createActivityStore,
  } from "./stores/activity.svelte.js";
  import {
    createSyncStore,
  } from "./stores/sync.svelte.js";
  import {
    createDiffStore,
  } from "./stores/diff.svelte.js";
  import {
    createGroupingStore,
  } from "./stores/grouping.svelte.js";
  import {
    createSettingsStore,
  } from "./stores/settings.svelte.js";

  interface Props {
    client: MiddlemanClient;
    actions?: ActionRegistry;
    onNavigate?: NavigateCallback;
    onEvent?: EventCallback;
    prepareRoute?: PrepareRouteCallback;
    hostState?: HostStateAccessors;
    config?: UIConfig;
    sidebar?: SidebarAccessors;
    getPage?: () => string;
    stores?: StoreInstances | undefined;
    children?: import("svelte").Snippet;
  }

  let {
    client,
    actions = {},
    onNavigate = () => {},
    onEvent = () => {},
    prepareRoute = undefined,
    hostState = {},
    config = {},
    sidebar = {
      isEmbedded: () => false,
      isSidebarToggleEnabled: () => true,
      toggleSidebar: () => {},
    },
    getPage = () => "",
    stores = $bindable(),
    children,
  }: Props = $props();

  // All initialization is in this function so its
  // parameters are plain values, not reactive proxies.
  // This avoids state_referenced_locally warnings.
  function init(
    cl: MiddlemanClient,
    hs: HostStateAccessors,
    cfg: UIConfig,
    act: ActionRegistry,
    nav: NavigateCallback,
    evt: EventCallback,
    prep: PrepareRouteCallback | undefined,
    sb: SidebarAccessors,
    gp: () => string,
  ): StoreInstances {
    const grouping = createGroupingStore();
    const settingsStore = createSettingsStore();

    const pullsOpts: PullsStoreOptions = { client: cl };
    if (hs.getGlobalRepo) {
      pullsOpts.getGlobalRepo = hs.getGlobalRepo;
    }
    pullsOpts.getGroupByRepo =
      hs.getGroupByRepo ?? grouping.getGroupByRepo;
    if (hs.getView) {
      pullsOpts.getView = hs.getView;
    }
    const pullsStore = createPullsStore(pullsOpts);

    const syncStore = createSyncStore({ client: cl });

    const detailOpts: DetailStoreOptions = {
      client: cl,
      getPage: gp,
      pulls: {
        loadPulls: (p?: unknown) => pullsStore.loadPulls(
          p as Parameters<typeof pullsStore.loadPulls>[0],
        ),
        optimisticKanbanUpdate:
          pullsStore.optimisticKanbanUpdate,
        getPullKanbanStatus:
          pullsStore.getPullKanbanStatus,
      },
      sync: syncStore,
    };
    const detailStore = createDetailStore(detailOpts);

    const issuesOpts: IssuesStoreOptions = { client: cl };
    if (hs.getGlobalRepo) {
      issuesOpts.getGlobalRepo = hs.getGlobalRepo;
    }
    issuesOpts.getGroupByRepo =
      hs.getGroupByRepo ?? grouping.getGroupByRepo;
    const issuesStore = createIssuesStore(issuesOpts);

    const activityOpts: ActivityStoreOptions = {
      client: cl,
    };
    if (hs.getGlobalRepo) {
      activityOpts.getGlobalRepo = hs.getGlobalRepo;
    }
    if (cfg.basePath != null) {
      const bp = cfg.basePath;
      activityOpts.getBasePath = () => bp;
    }
    const activityStore =
      createActivityStore(activityOpts);

    const diffOpts: DiffStoreOptions = {};
    if (cfg.basePath != null) {
      const bp = cfg.basePath;
      diffOpts.getBasePath = () => bp;
    }
    const diffStore = createDiffStore(diffOpts);

    const si: StoreInstances = {
      pulls: pullsStore,
      issues: issuesStore,
      detail: detailStore,
      activity: activityStore,
      sync: syncStore,
      diff: diffStore,
      grouping,
      settings: settingsStore,
    };

    setContext(API_CLIENT_KEY, cl);
    setContext(ACTIONS_KEY, act);
    setContext(NAVIGATE_KEY, nav);
    setContext(EVENT_KEY, evt);
    setContext(PREPARE_ROUTE_KEY, prep ?? null);
    setContext(STORES_KEY, si);
    setContext(UI_CONFIG_KEY, cfg);
    setContext(SIDEBAR_KEY, sb);
    setContext(HOST_STATE_KEY, hs);

    return si;
  }

  // svelte-ignore state_referenced_locally
  stores = init(
    client, hostState, config, actions,
    onNavigate, onEvent, prepareRoute,
    sidebar, getPage,
  );
</script>

{#if children}
  {@render children()}
{/if}
