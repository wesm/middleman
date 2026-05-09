import {
  buildRoutedItemRoute,
  type IssueRouteRef,
  type NumberedRouteItemRef,
  type RepositoryRouteRef,
  type RoutedItemRef,
} from "@middleman/ui/routes";
import { canonicalProvider } from "@middleman/ui/api/provider-routes";

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

const defaultPlatformHosts: Record<string, string> = {
  github: "github.com",
  gitlab: "gitlab.com",
};

function defaultPlatformHost(provider: string): string | undefined {
  return defaultPlatformHosts[canonicalProvider(provider)];
}

function decodeRouteSegment(segment: string): string | undefined {
  try {
    return decodeURIComponent(segment);
  } catch {
    return undefined;
  }
}

function parseProviderNumberedPath(
  parts: string[],
  start: number,
  platformHost?: string | undefined,
): NumberedItemRef | undefined {
  if (parts.length < start + 4) return undefined;
  const provider = decodeRouteSegment(parts[start] ?? "")?.trim();
  const owner = decodeRouteSegment(parts[start + 1] ?? "")?.replace(/^\/+|\/+$/g, "");
  const name = decodeRouteSegment(parts[start + 2] ?? "")?.replace(/^\/+|\/+$/g, "");
  const numberText = decodeRouteSegment(parts[start + 3] ?? "");
  if (!provider || !owner || !name || !numberText) return undefined;

  const number = parseInt(numberText, 10);
  if (!Number.isFinite(number) || number <= 0) return undefined;

  const resolvedPlatformHost = platformHost ?? defaultPlatformHost(provider);
  const ref: NumberedItemRef = {
    provider,
    owner,
    name,
    number,
    repoPath: `${owner}/${name}`,
    ...(resolvedPlatformHost && { platformHost: resolvedPlatformHost }),
  };
  return ref;
}

function parseHostProviderNumberedPath(
  parts: string[],
  kind: "pulls" | "issues",
  start = 0,
): NumberedItemRef | undefined {
  if (parts[start] === kind) {
    return parseProviderNumberedPath(parts, start + 1);
  }
  if (parts[start] === "host" && parts[start + 2] === kind) {
    const platformHost = decodeRouteSegment(parts[start + 1] ?? "")?.trim();
    if (!platformHost) return undefined;
    return parseProviderNumberedPath(parts, start + 3, platformHost);
  }
  return undefined;
}

function parseRoute(fullPath: string): Route {
  const qIdx = fullPath.indexOf("?");
  const pathname = qIdx >= 0 ? fullPath.slice(0, qIdx) : fullPath;
  const search = qIdx >= 0 ? fullPath.slice(qIdx + 1) : "";
  const path = stripBase(pathname).replace(/\/+$/, "") || "/";
  const parts = path.split("/").filter(Boolean);
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
    const pull = parseHostProviderNumberedPath(parts, "pulls", 1);
    if (pull && parts.length === (parts[1] === "host" ? 8 : 6)) {
      return {
        page: "focus",
        itemType: "pr",
        ...pull,
      };
    }
    const issue = parseHostProviderNumberedPath(parts, "issues", 1);
    if (issue && parts.length === (parts[1] === "host" ? 8 : 6)) {
      return {
        page: "focus",
        itemType: "issue",
        ...issue,
      };
    }
  }
  if (path === "/design-system") {
    return { page: "design-system" };
  }
  if (path.startsWith("/pulls")) {
    const rest = path.slice("/pulls".length);
    if (rest !== "" && rest !== "/board") {
      const selected = parseHostProviderNumberedPath(parts, "pulls");
      const isFiles = parts[parts.length - 1] === "files";
      if (selected && (parts.length === 5 || (parts.length === 6 && isFiles))) {
        return {
          page: "pulls",
          view: "list",
          selected,
          ...(isFiles && { tab: "files" as const }),
        };
      }
    }
    if (rest === "/board") return { page: "pulls", view: "board" };
    return { page: "pulls", view: "list" };
  }
  if (path === "/repos") {
    return { page: "repos" };
  }
  if (path === "/settings" && !isEmbedded()) return { page: "settings" };
  if (path.startsWith("/issues")) {
    if (path !== "/issues") {
      const selected = parseHostProviderNumberedPath(parts, "issues");
      if (selected && parts.length === 5) {
        return {
          page: "issues",
          selected,
        };
      }
    }
    return { page: "issues" };
  }
  if (path.startsWith("/host/")) {
    const pull = parseHostProviderNumberedPath(parts, "pulls");
    const isPullFiles = parts[parts.length - 1] === "files";
    if (pull && (parts.length === 7 || (parts.length === 8 && isPullFiles))) {
      return {
        page: "pulls",
        view: "list",
        selected: pull,
        ...(isPullFiles && { tab: "files" as const }),
      };
    }
    const issue = parseHostProviderNumberedPath(parts, "issues");
    if (issue && parts.length === 7) {
      return {
        page: "issues",
        selected: issue,
      };
    }
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

export function buildItemRoute(ref: RoutableItemRef): string {
  return buildRoutedItemRoute(ref, { focus: isFocusMode() });
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
