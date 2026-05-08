import {
  getDetailTab,
  getSelectedPRFromRoute,
  navigate,
  replaceUrl,
} from "../router.svelte.js";
import {
  isSidebarToggleEnabled,
  toggleSidebar,
} from "../sidebar.svelte.js";
import { toggleTheme } from "../theme.svelte.js";
import {
  buildPullRequestFilesRoute,
  buildPullRequestRoute,
} from "@middleman/ui/routes";
import type { StoreInstances } from "@middleman/ui";
import type { Action, Context } from "./types.js";

let storesGetter: (() => StoreInstances) | null = null;

export function setStoreInstances(getter: () => StoreInstances): void {
  storesGetter = getter;
}

function stores(): StoreInstances {
  if (!storesGetter) {
    throw new Error("setStoreInstances has not been called");
  }
  return storesGetter();
}

const always = (): boolean => true;

const isBoardRoute = (ctx: Context): boolean =>
  ctx.route.page === "pulls" &&
  "view" in ctx.route &&
  ctx.route.view === "board";

const onPullsListNotBoard = (ctx: Context): boolean =>
  ctx.page === "pulls" && !ctx.isDiffView && !isBoardRoute(ctx);

const onIssuesList = (ctx: Context): boolean => ctx.page === "issues";

// Mirror App.svelte's navigateToSelectedPR helper (replaceUrl when a PR is
// already selected in the URL, navigate otherwise).
function navigateToSelectedPR(): void {
  const sel = stores().pulls.getSelectedPR();
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

export const defaultActions: Action[] = [
  {
    id: "go.next",
    label: "Next pull request",
    scope: "view-pulls",
    binding: { key: "j" },
    priority: 0,
    when: onPullsListNotBoard,
    handler: () => {
      stores().pulls.selectNextPR();
      navigateToSelectedPR();
    },
  },
  {
    id: "go.prev",
    label: "Previous pull request",
    scope: "view-pulls",
    binding: { key: "k" },
    priority: 0,
    when: onPullsListNotBoard,
    handler: () => {
      stores().pulls.selectPrevPR();
      navigateToSelectedPR();
    },
  },
  {
    id: "go.next.issues",
    label: "Next issue",
    scope: "view-issues",
    binding: { key: "j" },
    priority: 0,
    when: onIssuesList,
    handler: () => {
      stores().issues.selectNextIssue();
    },
  },
  {
    id: "go.prev.issues",
    label: "Previous issue",
    scope: "view-issues",
    binding: { key: "k" },
    priority: 0,
    when: onIssuesList,
    handler: () => {
      stores().issues.selectPrevIssue();
    },
  },
  {
    id: "tab.toggle",
    label: "Toggle conversation/files tab",
    scope: "view-pulls",
    binding: { key: "f" },
    priority: 0,
    when: (ctx) => ctx.page === "pulls" && getSelectedPRFromRoute() !== null,
    handler: () => {
      const sel = getSelectedPRFromRoute();
      if (!sel) return;
      const tab = getDetailTab();
      if (tab === "conversation") {
        navigate(buildPullRequestFilesRoute(sel));
      } else {
        navigate(buildPullRequestRoute(sel));
      }
    },
  },
  {
    id: "escape.list",
    label: "Back to list",
    scope: "view-pulls",
    binding: { key: "Escape" },
    priority: 0,
    when: (ctx) =>
      (ctx.page === "pulls" || ctx.page === "issues") && !isBoardRoute(ctx),
    handler: (ctx) => {
      if (ctx.page === "issues") {
        navigate("/issues");
      } else {
        navigate("/pulls");
      }
    },
  },
  {
    id: "nav.pulls.list",
    label: "Pull requests (list)",
    scope: "global",
    binding: { key: "1" },
    priority: 0,
    when: always,
    handler: () => navigate("/pulls"),
  },
  {
    id: "nav.pulls.board",
    label: "Pull requests (board)",
    scope: "global",
    binding: { key: "2" },
    priority: 0,
    when: always,
    handler: () => navigate("/pulls/board"),
  },
  {
    id: "sidebar.toggle",
    label: "Toggle sidebar",
    scope: "global",
    binding: { key: "[", ctrlOrMeta: true },
    priority: 0,
    when: () => isSidebarToggleEnabled(),
    handler: () => toggleSidebar(),
  },
  {
    id: "palette.open",
    label: "Open command palette",
    scope: "global",
    binding: [
      { key: "k", ctrlOrMeta: true },
      { key: "p", ctrlOrMeta: true },
    ],
    priority: 0,
    when: always,
    // Stubbed until Task 17 lands palette-state. No-op rather than throw so
    // pressing the binding doesn't surface a flash toast every time during
    // the staged rollout. The real handler replaces this in stage 6.
    handler: () => {},
  },
  {
    id: "cheatsheet.open",
    label: "Show keyboard shortcuts",
    scope: "global",
    binding: { key: "?" },
    priority: 0,
    when: always,
    // Stubbed until Task 24 lands cheatsheet-state — see palette.open above.
    handler: () => {},
  },
  {
    id: "sync.repos",
    label: "Sync repositories",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => stores().sync.triggerSync(),
  },
  {
    id: "theme.toggle",
    label: "Toggle theme",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => toggleTheme(),
  },
  {
    id: "nav.settings",
    label: "Settings",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => navigate("/settings"),
  },
  {
    id: "nav.repos",
    label: "Repositories",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => navigate("/repos"),
  },
  {
    id: "nav.reviews",
    label: "Reviews",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => navigate("/reviews"),
  },
  {
    id: "nav.workspaces",
    label: "Workspaces",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => navigate("/workspaces"),
  },
  {
    id: "nav.design-system",
    label: "Design system",
    scope: "global",
    binding: null,
    priority: 0,
    when: always,
    handler: () => navigate("/design-system"),
  },
];
