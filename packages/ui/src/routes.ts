export type RepositoryRouteRef = {
  owner: string;
  name: string;
};

export type NumberedRouteItemRef = RepositoryRouteRef & {
  number: number;
};

export type PullRequestRouteRef = NumberedRouteItemRef & {
  platformHost?: string | undefined;
};

export type IssueRouteRef = NumberedRouteItemRef & {
  platformHost?: string | undefined;
};

export type RoutedItemRef =
  | (PullRequestRouteRef & { itemType: "pr" })
  | (IssueRouteRef & { itemType: "issue" });

export type FocusListRouteRef = {
  itemType: "mrs" | "issues";
  repo?: string | undefined;
};

export function buildPullRequestRoute(ref: PullRequestRouteRef): string {
  return `/pulls/${ref.owner}/${ref.name}/${ref.number}`;
}

export function buildPullRequestFilesRoute(ref: PullRequestRouteRef): string {
  return `${buildPullRequestRoute(ref)}/files`;
}

export function buildIssueRoute(ref: IssueRouteRef): string {
  return `/issues/${ref.owner}/${ref.name}/${ref.number}${platformHostQuery(ref.platformHost)}`;
}

export function buildFocusPullRequestRoute(ref: PullRequestRouteRef): string {
  return `/focus/pr/${ref.owner}/${ref.name}/${ref.number}`;
}

export function buildFocusIssueRoute(ref: IssueRouteRef): string {
  return `/focus/issue/${ref.owner}/${ref.name}/${ref.number}${platformHostQuery(ref.platformHost)}`;
}

export function buildFocusListRoute(ref: FocusListRouteRef): string {
  const route = `/focus/${ref.itemType}`;
  return ref.repo ? `${route}?repo=${encodeURIComponent(ref.repo)}` : route;
}

export function buildRoutedItemRoute(
  ref: RoutedItemRef,
  options: { focus?: boolean } = {},
): string {
  if (ref.itemType === "pr") {
    return options.focus ? buildFocusPullRequestRoute(ref) : buildPullRequestRoute(ref);
  }
  return options.focus ? buildFocusIssueRoute(ref) : buildIssueRoute(ref);
}

function platformHostQuery(platformHost: string | undefined): string {
  return platformHost ? `?platform_host=${encodeURIComponent(platformHost)}` : "";
}
