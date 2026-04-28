export type ActivitySelectionItemType = "pr" | "issue";
export type ActivityDetailTab = "conversation" | "files";

export interface ActivitySelection {
  itemType: ActivitySelectionItemType;
  owner: string;
  name: string;
  number: number;
  platformHost?: string | undefined;
  detailTab: ActivityDetailTab;
}

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
    const suffix = selection.detailTab === "files" ? "/files" : "";
    return `/pulls/${selection.owner}/${selection.name}/${selection.number}${suffix}`;
  }

  if (selection.itemType !== "issue") return null;
  const qs = selection.platformHost
    ? `?platform_host=${encodeURIComponent(selection.platformHost)}`
    : "";
  return `/issues/${selection.owner}/${selection.name}/${selection.number}${qs}`;
}
