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
  CollapsedReposStore,
  SettingsStore,
  EventsStore,
  DaemonStore,
  JobsStore,
  ReviewStore,
  LogStore,
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
  getRoborevClient,
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
  classifyPR,
  groupByWorkflow,
  workflowGroupOrder,
  workflowGroupLabels,
} from "./stores/workflow.svelte.js";
export type {
  WorkflowGroup,
  WorkflowGroupEntry,
} from "./stores/workflow.svelte.js";
export {
  createCollapsedReposStore,
} from "./stores/collapsedRepos.svelte.js";
export {
  createSettingsStore,
} from "./stores/settings.svelte.js";
export {
  createEventsStore,
} from "./stores/events.svelte.js";
export {
  createDaemonStore,
} from "./stores/roborev/daemon.svelte.js";
export {
  createJobsStore,
} from "./stores/roborev/jobs.svelte.js";
export {
  createReviewStore,
} from "./stores/roborev/review.svelte.js";
export {
  createLogStore,
} from "./stores/roborev/log.svelte.js";

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
  default as ReviewsView,
} from "./views/ReviewsView.svelte";
export {
  default as FocusListView,
} from "./views/FocusListView.svelte";
export {
  default as WorkspacesView,
} from "./views/WorkspacesView.svelte";
export {
  default as WorkspacePanelView,
} from "./views/WorkspacePanelView.svelte";
export {
  default as ActionButton,
} from "./components/shared/ActionButton.svelte";
export {
  default as FilterDropdown,
} from "./components/shared/FilterDropdown.svelte";
export {
  default as WorkspaceRightSidebar,
} from "./components/workspace/WorkspaceRightSidebar.svelte";
