import {
  buildRoutedItemRoute,
  type IssueRouteRef,
  type NumberedRouteItemRef,
  type RepositoryRouteRef,
  type RoutedItemRef,
} from "@middleman/ui/routes";

export type RepoRef = RepositoryRouteRef;
export type NumberedItemRef = NumberedRouteItemRef;
export type HostedItemRef = IssueRouteRef;
export type RoutableItemRef = RoutedItemRef;

export type Route =
  | { page: "activity" }
  | { page: "design-system" }
  | { page: "repos" }
  | { page: "workspaces" }
  | { page: "pulls"; view: "list" | "board"; selected?: NumberedItemRef; tab?: "files" }
  | { page: "issues"; selected?: HostedItemRef }
  | { page: "settings" }
  | ({ page: "focus" } & RoutableItemRef)
  | { page: "focus"; itemType: "mrs"; repo?: string }
  | { page: "focus"; itemType: "issues"; repo?: string }
  | { page: "reviews"; jobId?: number }
  | { page: "terminal"; workspaceId: string };

import {
  isEmbedded,
  getOnNavigate,
  getOnRouteChange,
  getUIConfig as getEmbedUIConfig,
  getHost,
} from "./embed-config.svelte.js";

// Runtime base path injected by the Go server (e.g., "/" or "/middleman/").
const rawBase = window.__BASE_PATH__ ?? "/";
const basePrefix = rawBase === "/" ? "" : rawBase.replace(/\/$/, "");

export function getBasePath(): string {
  return rawBase;
}

function stripBase(path: string): string {
  if (basePrefix && path.startsWith(basePrefix)) {
    return path.slice(basePrefix.length) || "/";
  }
  return path;
}

function currentLocationPath(): string {
  return window.location.pathname + window.location.search;
}

function parseRoute(fullPath: string): Route {
  const qIdx = fullPath.indexOf("?");
  const pathname = qIdx >= 0 ? fullPath.slice(0, qIdx) : fullPath;
  const search = qIdx >= 0 ? fullPath.slice(qIdx + 1) : "";
  const path = stripBase(pathname).replace(/\/+$/, "") || "/";
  if (path.startsWith("/focus/")) {
    if (path === "/focus/mrs") {
      const sp = new URLSearchParams(search);
      const repo = sp.get("repo");
      const r: Route = { page: "focus", itemType: "mrs" };
      if (repo) r.repo = repo;
      return r;
    }
    if (path === "/focus/issues") {
      const sp = new URLSearchParams(search);
      const repo = sp.get("repo");
      const r: Route = { page: "focus", itemType: "issues" };
      if (repo) r.repo = repo;
      return r;
    }
    const prMatch = path.match(
      /^\/focus\/pr\/([^/]+)\/([^/]+)\/(\d+)$/,
    );
    if (prMatch) {
      return {
        page: "focus",
        itemType: "pr",
        owner: prMatch[1]!,
        name: prMatch[2]!,
        number: parseInt(prMatch[3]!, 10),
      };
    }
    const issueMatch = path.match(
      /^\/focus\/issue\/([^/]+)\/([^/]+)\/(\d+)$/,
    );
    if (issueMatch) {
      const sp = new URLSearchParams(search);
      const platformHost = sp.get("platform_host") ?? undefined;
      return {
        page: "focus",
        itemType: "issue",
        owner: issueMatch[1]!,
        name: issueMatch[2]!,
        number: parseInt(issueMatch[3]!, 10),
        ...(platformHost && { platformHost }),
      };
    }
  }
  if (path === "/design-system") {
    return { page: "design-system" };
  }
  if (path.startsWith("/pulls")) {
    const rest = path.slice("/pulls".length);
    if (rest === "/board") return { page: "pulls", view: "board" };
    const filesMatch = rest.match(/^\/([^/]+)\/([^/]+)\/(\d+)\/files$/);
    if (filesMatch) {
      return {
        page: "pulls",
        view: "list",
        selected: {
          owner: filesMatch[1]!,
          name: filesMatch[2]!,
          number: parseInt(filesMatch[3]!, 10),
        },
        tab: "files",
      };
    }
    const match = rest.match(/^\/([^/]+)\/([^/]+)\/(\d+)$/);
    if (match) {
      return {
        page: "pulls",
        view: "list",
        selected: { owner: match[1]!, name: match[2]!, number: parseInt(match[3]!, 10) },
      };
    }
    return { page: "pulls", view: "list" };
  }
  if (path === "/repos") {
    return { page: "repos" };
  }
  if (path === "/settings" && !isEmbedded()) return { page: "settings" };
  if (path.startsWith("/issues")) {
    const match = path.slice("/issues".length).match(/^\/([^/]+)\/([^/]+)\/(\d+)$/);
    if (match) {
      const sp = new URLSearchParams(search);
      const platformHost = sp.get("platform_host") ?? undefined;
      return {
        page: "issues",
        selected: {
          owner: match[1]!,
          name: match[2]!,
          number: parseInt(match[3]!, 10),
          ...(platformHost && { platformHost }),
        },
      };
    }
    return { page: "issues" };
  }
  const reviewsMatch = path.match(/^\/reviews(?:\/(\d+))?$/);
  if (reviewsMatch) {
    if (reviewsMatch[1]) {
      return {
        page: "reviews",
        jobId: parseInt(reviewsMatch[1], 10),
      };
    }
    return { page: "reviews" };
  }
  const terminalMatch = path.match(/^\/terminal\/([^/]+)$/);
  if (terminalMatch) {
    return {
      page: "terminal",
      workspaceId: terminalMatch[1]!,
    };
  }
  if (path === "/workspaces" || path.startsWith("/workspaces/")) {
    return { page: "workspaces" };
  }
  return { page: "activity" };
}

let route = $state<Route>(
  parseRoute(currentLocationPath()),
);

// Fire onRouteChange for the initial route after the module loads.
// Deferred so the embedder has time to set up the callback.
if (typeof window !== "undefined") {
  queueMicrotask(() => fireRouteChange(route));
}

export function getRoute(): Route {
  return route;
}

export function getPage():
  "activity" | "design-system" | "repos" | "pulls" | "issues" | "settings"
  | "focus" | "reviews" | "workspaces" | "terminal" {
  return route.page;
}

export function isFocusMode(): boolean {
  return route.page === "focus";
}

export function buildItemRoute(
  type: "pr" | "issue",
  owner: string,
  name: string,
  number: number,
  platformHost?: string,
): string {
  return buildRoutedItemRoute(
    {
      itemType: type,
      owner,
      name,
      number,
      ...(platformHost && { platformHost }),
    },
    { focus: isFocusMode() },
  );
}

export function navigate(path: string, state?: Record<string, unknown>): void {
  const fullPath = basePrefix + path;
  history.pushState(state ?? null, "", fullPath);
  route = parseRoute(fullPath);
  fireMiddlemanNavigateEvent(route);
  fireRouteChange(route);
}

function buildRouteEvent(r: Route): MiddlemanNavigateEvent {
  const focus = r.page === "focus";
  let navType: MiddlemanNavigateEvent["type"];
  if (r.page === "focus") {
    if (r.itemType === "mrs") {
      navType = "pull";
    } else if (r.itemType === "issues") {
      navType = "issue";
    } else {
      navType = r.itemType === "pr" ? "pull" : "issue";
    }
  } else if (r.page === "pulls") {
    navType = r.view === "board" ? "board" : "pull";
  } else if (r.page === "issues") {
    navType = "issue";
  } else if (r.page === "repos") {
    navType = "repos";
  } else if (r.page === "reviews") {
    navType = "reviews";
  } else if (
    r.page === "workspaces" ||
    r.page === "terminal"
  ) {
    navType = "workspaces";
  } else if (r.page === "design-system") {
    navType = "activity";
  } else {
    navType = "activity";
  }

  const event: MiddlemanNavigateEvent = {
    type: navType,
    focus,
    view: stripBase(window.location.pathname) + window.location.search,
  };

  if (r.page === "focus" && "owner" in r) {
    event.owner = r.owner;
    event.name = r.name;
    event.number = r.number;
  } else if (r.page === "pulls" && "selected" in r && r.selected) {
    event.owner = r.selected.owner;
    event.name = r.selected.name;
    event.number = r.selected.number;
  } else if (r.page === "issues" && "selected" in r && r.selected) {
    event.owner = r.selected.owner;
    event.name = r.selected.name;
    event.number = r.selected.number;
  }

  // Populate repo from focus list route or global config.
  if (r.page === "focus" && "repo" in r && r.repo) {
    event.repo = r.repo;
  } else {
    const cfgRepo = getEmbedUIConfig().repo;
    if (cfgRepo) {
      event.repo = `${cfgRepo.owner}/${cfgRepo.name}`;
    }
  }

  const host = getHost();
  if (host) {
    event.host = host;
  }

  return event;
}

function fireMiddlemanNavigateEvent(r: Route): void {
  const cb = getOnNavigate();
  if (cb) cb(buildRouteEvent(r));
}

function fireRouteChange(r: Route): void {
  const cb = getOnRouteChange();
  if (cb) cb(buildRouteEvent(r));
}

export function replaceUrl(path: string): void {
  const fullPath = basePrefix + path;
  history.replaceState(null, "", fullPath);
  route = parseRoute(fullPath);
  fireRouteChange(route);
}

// Listen for browser back/forward.
if (typeof window !== "undefined") {
  window.addEventListener("popstate", () => {
    route = parseRoute(currentLocationPath());
    fireRouteChange(route);
  });
}

// Expose imperative navigation for the host embedder.
if (typeof window !== "undefined") {
  window.__middleman_navigate_to_route = (route: string) => {
    navigate(route);
  };
}

// --- detail tab derived from route ---

export type DetailTab = "conversation" | "files";

export function getDetailTab(): DetailTab {
  if (route.page === "pulls" && "tab" in route && route.tab === "files") return "files";
  return "conversation";
}

export function getSelectedPRFromRoute(): NumberedItemRef | null {
  if (route.page !== "pulls") return null;
  if ("selected" in route && route.selected) {
    return route.selected;
  }
  return null;
}

// --- backward-compat helpers for existing components ---

export type View = "list" | "board";
export type Tab = "pulls" | "issues";

export function getView(): View {
  return route.page === "pulls" && "view" in route && route.view === "board" ? "board" : "list";
}

export function setView(v: View): void {
  if (route.page === "pulls") {
    navigate(v === "board" ? "/pulls/board" : "/pulls");
  }
}

export function getTab(): Tab {
  if (route.page === "pulls") return "pulls";
  if (route.page === "issues") return "issues";
  return "pulls";
}

export function setTab(t: Tab): void {
  navigate(t === "pulls" ? "/pulls" : "/issues");
}

export function isDiffView(): boolean {
  return route.page === "pulls" && "tab" in route && route.tab === "files";
}
