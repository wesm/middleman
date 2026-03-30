export type Route =
  | { page: "activity" }
  | { page: "pulls"; view: "list" | "board"; selected?: { owner: string; name: string; number: number } }
  | { page: "issues"; selected?: { owner: string; name: string; number: number } };

function parseRoute(path: string): Route {
  if (path.startsWith("/pulls")) {
    const rest = path.slice("/pulls".length);
    if (rest === "/board") return { page: "pulls", view: "board" };
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

export function getPage(): "activity" | "pulls" | "issues" {
  return route.page;
}

export function navigate(path: string): void {
  history.pushState(null, "", path);
  route = parseRoute(path);
}

export function replaceUrl(path: string): void {
  history.replaceState(null, "", path);
  route = parseRoute(path);
}

// Listen for browser back/forward.
if (typeof window !== "undefined") {
  window.addEventListener("popstate", () => {
    route = parseRoute(window.location.pathname);
  });
}

// --- backward-compat helpers for existing components ---

export type View = "list" | "board";
export type Tab = "pulls" | "issues";

export function getView(): View {
  return route.page === "pulls" && route.view === "board" ? "board" : "list";
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
