export type Route =
  | { page: "activity" }
  | { page: "pulls"; view: "list" | "board"; selected?: { owner: string; name: string; number: number } }
  | { page: "pulls"; view: "diff"; owner: string; name: string; number: number }
  | { page: "issues"; selected?: { owner: string; name: string; number: number } }
  | { page: "settings" }
  | { page: "focus"; itemType: "pr" | "issue"; owner: string; name: string; number: number }
  | { page: "reviews"; jobId?: number };

import { isEmbedded, getOnNavigate, getOnRouteChange } from "./embed-config.svelte.js";

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

function parseRoute(fullPath: string): Route {
  const path = stripBase(fullPath);
  if (path.startsWith("/focus/")) {
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
      return {
        page: "focus",
        itemType: "issue",
        owner: issueMatch[1]!,
        name: issueMatch[2]!,
        number: parseInt(issueMatch[3]!, 10),
      };
    }
  }
  if (path.startsWith("/pulls")) {
    const rest = path.slice("/pulls".length);
    if (rest === "/board") return { page: "pulls", view: "board" };
    const diffMatch = rest.match(/^\/([^/]+)\/([^/]+)\/(\d+)\/files$/);
    if (diffMatch) {
      return {
        page: "pulls",
        view: "diff",
        owner: diffMatch[1]!,
        name: diffMatch[2]!,
        number: parseInt(diffMatch[3]!, 10),
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
  if (path === "/settings" && !isEmbedded()) return { page: "settings" };
  if (path.startsWith("/issues")) {
    const match = path.slice("/issues".length).match(/^\/([^/]+)\/([^/]+)\/(\d+)$/);
    if (match) {
      return {
        page: "issues",
        selected: { owner: match[1]!, name: match[2]!, number: parseInt(match[3]!, 10) },
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
  return { page: "activity" };
}

let route = $state<Route>(parseRoute(window.location.pathname));

// Fire onRouteChange for the initial route after the module loads.
// Deferred so the embedder has time to set up the callback.
if (typeof window !== "undefined") {
  queueMicrotask(() => fireRouteChange(route));
}

export function getRoute(): Route {
  return route;
}

export function getPage():
  "activity" | "pulls" | "issues" | "settings" | "focus" | "reviews" {
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
): string {
  if (isFocusMode()) {
    return `/focus/${type}/${owner}/${name}/${number}`;
  }
  return type === "pr"
    ? `/pulls/${owner}/${name}/${number}`
    : `/issues/${owner}/${name}/${number}`;
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
    navType = r.itemType === "pr" ? "pull" : "issue";
  } else if (r.page === "pulls") {
    navType = r.view === "board" ? "board" : "pull";
  } else if (r.page === "issues") {
    navType = "issue";
  } else if (r.page === "reviews") {
    navType = "reviews";
  } else {
    navType = r.page as "activity";
  }

  const event: MiddlemanNavigateEvent = {
    type: navType,
    focus,
    view: stripBase(window.location.pathname),
  };

  if (r.page === "focus") {
    event.owner = r.owner;
    event.name = r.name;
    event.number = r.number;
  } else if (r.page === "pulls" && "view" in r && r.view === "diff") {
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
    route = parseRoute(window.location.pathname);
    fireRouteChange(route);
  });
}

// --- detail tab derived from route ---

export type DetailTab = "conversation" | "files";

export function getDetailTab(): DetailTab {
  if (route.page === "pulls" && "view" in route && route.view === "diff") return "files";
  return "conversation";
}

/** Returns the selected PR info regardless of whether the route is list or diff view. */
export function getSelectedPRFromRoute(): { owner: string; name: string; number: number } | null {
  if (route.page !== "pulls") return null;
  if ("view" in route && route.view === "diff") {
    return { owner: route.owner, name: route.name, number: route.number };
  }
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
  return route.page === "pulls" && "view" in route && route.view === "diff";
}
