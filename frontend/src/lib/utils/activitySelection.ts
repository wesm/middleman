import {
  buildIssueRoute,
  buildPullRequestFilesRoute,
  buildPullRequestRoute,
  type RoutedItemRef,
} from "@middleman/ui/routes";

export type ActivitySelectionItemType = "pr" | "issue";
export type ActivityDetailTab = "conversation" | "files";

export type ActivitySelection = RoutedItemRef & {
  detailTab: ActivityDetailTab;
};

type Destination = "pulls" | "issues";

function searchParams(search: string): URLSearchParams {
  return new URLSearchParams(search.startsWith("?") ? search.slice(1) : search);
}

export function parseActivitySelection(search: string): ActivitySelection | null {
  const sp = searchParams(search);
  const selected = sp.get("selected");
  if (!selected) return null;

  let itemType: ActivitySelectionItemType;
  let owner: string;
  let name: string;
  let number: number;
  const provider = sp.get("provider") ?? undefined;
  const platformHost = sp.get("platform_host") ?? undefined;
  const repoPath = sp.get("repo_path")?.replace(/^\/+|\/+$/g, "") || undefined;

  const providerMatch = selected.match(/^(pr|issue):(\d+)$/);
  if (providerMatch && repoPath) {
    const pathParts = repoPath.split("/").filter(Boolean);
    if (pathParts.length < 2) return null;
    itemType = providerMatch[1] as ActivitySelectionItemType;
    number = parseInt(providerMatch[2]!, 10);
    name = pathParts[pathParts.length - 1]!;
    owner = pathParts.slice(0, -1).join("/");
  } else {
    const match = selected.match(/^(pr|issue):([^/]+)\/([^/]+)\/(\d+)$/);
    if (!match) return null;
    itemType = match[1] as ActivitySelectionItemType;
    owner = match[2]!;
    name = match[3]!;
    number = parseInt(match[4]!, 10);
  }

  const detailTab: ActivityDetailTab =
    itemType === "pr" && sp.get("selected_tab") === "files"
      ? "files"
      : "conversation";

  return {
    itemType,
    owner,
    name,
    number,
    detailTab,
    ...(provider && { provider }),
    ...(platformHost && { platformHost }),
    ...(repoPath && { repoPath }),
  };
}

export function buildActivitySelectionSearch(
  currentSearch: string,
  selection: ActivitySelection | null,
): URLSearchParams {
  const sp = searchParams(currentSearch);
  sp.delete("selected");
  sp.delete("selected_tab");
  sp.delete("provider");
  sp.delete("platform_host");
  sp.delete("repo_path");

  if (!selection) return sp;

  if (selection.repoPath) {
    sp.set("selected", `${selection.itemType}:${selection.number}`);
    if (selection.provider) sp.set("provider", selection.provider);
    if (selection.platformHost) sp.set("platform_host", selection.platformHost);
    if (selection.repoPath) sp.set("repo_path", selection.repoPath);
  } else {
    sp.set(
      "selected",
      `${selection.itemType}:${selection.owner}/${selection.name}/${selection.number}`,
    );
    if (selection.platformHost) sp.set("platform_host", selection.platformHost);
  }
  if (selection.itemType === "pr" && selection.detailTab === "files") {
    sp.set("selected_tab", "files");
  }
  return sp;
}

export function activitySelectionToRoute(
  selection: ActivitySelection | null,
  destination: Destination,
): string | null {
  if (!selection) return null;
  if (destination === "pulls") {
    if (selection.itemType !== "pr") return null;
    return selection.detailTab === "files"
      ? buildPullRequestFilesRoute(selection)
      : buildPullRequestRoute(selection);
  }

  if (selection.itemType !== "issue") return null;
  return buildIssueRoute(selection);
}
