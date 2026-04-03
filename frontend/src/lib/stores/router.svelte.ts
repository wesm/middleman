export type Route =
  | { page: "activity" }
  | { page: "pulls"; view: "list" | "board"; selected?: { owner: string; name: string; number: number } }
  | { page: "pulls"; view: "diff"; owner: string; name: string; number: number }
  | { page: "issues"; selected?: { owner: string; name: string; number: number } }
  | { page: "settings" };

// Runtime base path injected by the Go server (e.g., "/" or "/middleman/").
const rawBase = window.__BASE_PATH__ ?? "/";
const basePrefix = rawBase === "/" ? "" : rawBase.replace(/\/$/, "");
const embedded = typeof window !== "undefined" && window.__MIDDLEMAN_EMBEDDED__ === true;

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
  if (path === "/settings" && !embedded) return { page: "settings" };
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
  return { page: "activity" };
}

let route = $state<Route>(parseRoute(window.location.pathname));

export function getRoute(): Route {
  return route;
}

export function getPage(): "activity" | "pulls" | "issues" | "settings" {
  return route.page;
}

export function navigate(path: string, state?: Record<string, unknown>): void {
  const fullPath = basePrefix + path;
  history.pushState(state ?? null, "", fullPath);
  route = parseRoute(fullPath);
}

export function replaceUrl(path: string): void {
  const fullPath = basePrefix + path;
  history.replaceState(null, "", fullPath);
  route = parseRoute(fullPath);
}

// Listen for browser back/forward.
if (typeof window !== "undefined") {
  window.addEventListener("popstate", () => {
    route = parseRoute(window.location.pathname);
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
