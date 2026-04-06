export type {
  MiddlemanClient,
  Action,
  ActionContext,
  ActionRegistry,
  NavigateEvent,
  NavigateCallback,
  MiddlemanEvent,
  EventCallback,
  PrepareRouteCallback,
  HostStateAccessors,
  StoreInstances,
  UIConfig,
  SidebarAccessors,
  PullsStore,
  IssuesStore,
  DetailStore,
  ActivityStore,
  SyncStore,
  DiffStore,
  GroupingStore,
  SettingsStore,
} from "./types.js";

export {
  getStores,
  getClient,
  getActions,
  getNavigate,
  getEventCallback,
  getPrepareRoute,
  getUIConfig,
  getSidebar,
  getHostState,
} from "./context.js";

// Store factories
export { createPullsStore } from "./stores/pulls.svelte.js";
export {
  createIssuesStore,
} from "./stores/issues.svelte.js";
export {
  createDetailStore,
} from "./stores/detail.svelte.js";
export {
  createActivityStore,
} from "./stores/activity.svelte.js";
export { createSyncStore } from "./stores/sync.svelte.js";
export { createDiffStore } from "./stores/diff.svelte.js";
export {
  createGroupingStore,
} from "./stores/grouping.svelte.js";
export {
  createSettingsStore,
} from "./stores/settings.svelte.js";

// Provider and views
export { default as Provider } from "./Provider.svelte";
export {
  default as PRListView,
} from "./views/PRListView.svelte";
export {
  default as IssueListView,
} from "./views/IssueListView.svelte";
export {
  default as ActivityFeedView,
} from "./views/ActivityFeedView.svelte";
export {
  default as KanbanBoardView,
} from "./views/KanbanBoardView.svelte";
export {
  default as DiffViewWrapper,
} from "./views/DiffViewWrapper.svelte";
