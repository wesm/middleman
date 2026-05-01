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

  const match = selected.match(/^(pr|issue):([^/]+)\/([^/]+)\/(\d+)$/);
  if (!match) return null;

  const itemType = match[1] as ActivitySelectionItemType;
  const detailTab: ActivityDetailTab =
    itemType === "pr" && sp.get("selected_tab") === "files"
      ? "files"
      : "conversation";

  const platformHost =
    itemType === "issue" ? (sp.get("platform_host") ?? undefined) : undefined;

  return {
    itemType,
    owner: match[2]!,
    name: match[3]!,
    number: parseInt(match[4]!, 10),
    detailTab,
    ...(platformHost && { platformHost }),
  };
}

export function buildActivitySelectionSearch(
  currentSearch: string,
  selection: ActivitySelection | null,
): URLSearchParams {
  const sp = searchParams(currentSearch);
  sp.delete("selected");
  sp.delete("selected_tab");
  sp.delete("platform_host");

  if (!selection) return sp;

  sp.set(
    "selected",
    `${selection.itemType}:${selection.owner}/${selection.name}/${selection.number}`,
  );
  if (selection.itemType === "pr" && selection.detailTab === "files") {
    sp.set("selected_tab", "files");
  }
  if (selection.itemType === "issue" && selection.platformHost) {
    sp.set("platform_host", selection.platformHost);
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
