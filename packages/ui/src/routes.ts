export type RepositoryRouteRef = {
  owner: string;
  name: string;
  provider?: string | undefined;
  platformHost?: string | undefined;
  repoPath?: string | undefined;
};

export type ProviderRouteRef = {
  provider: string;
  platformHost: string;
  repoPath: string;
};

export type NumberedRouteItemRef = RepositoryRouteRef & {
  number: number;
};

export type PullRequestRouteRef = NumberedRouteItemRef;

export type IssueRouteRef = NumberedRouteItemRef;

export type RoutedItemRef =
  | (PullRequestRouteRef & { itemType: "pr" })
  | (IssueRouteRef & { itemType: "issue" });

export type FocusListRouteRef = {
  itemType: "mrs" | "issues";
  repo?: string | undefined;
};

export function buildPullRequestRoute(ref: PullRequestRouteRef): string {
  return buildProviderPullRequestRoute(providerRouteRef(ref));
}

export function buildPullRequestFilesRoute(ref: PullRequestRouteRef): string {
  return buildProviderPullRequestFilesRoute(providerRouteRef(ref));
}

export function buildProviderPullRequestRoute(ref: ProviderRouteRef & { number: number }): string {
  return `/pulls/detail${providerItemQuery(ref)}`;
}

export function buildProviderPullRequestFilesRoute(
  ref: ProviderRouteRef & { number: number },
): string {
  return `/pulls/detail/files${providerItemQuery(ref)}`;
}

export function buildIssueRoute(ref: IssueRouteRef): string {
  return buildProviderIssueRoute(providerRouteRef(ref));
}

export function buildProviderIssueRoute(ref: ProviderRouteRef & { number: number }): string {
  return `/issues/detail${providerItemQuery(ref)}`;
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

function providerItemQuery(ref: ProviderRouteRef & { number: number }): string {
  const params: Array<[string, string]> = [
    ["provider", ref.provider],
    ["platform_host", ref.platformHost],
    ["repo_path", ref.repoPath],
    ["number", ref.number.toString()],
  ];
  return `?${params.map(([key, value]) => `${key}=${encodeURIComponent(value)}`).join("&")}`;
}

function platformHostQuery(platformHost: string | undefined): string {
  return platformHost ? `?platform_host=${encodeURIComponent(platformHost)}` : "";
}

function providerRouteRef(ref: NumberedRouteItemRef): ProviderRouteRef & { number: number } {
  return {
    provider: ref.provider ?? "github",
    platformHost: ref.platformHost ?? "github.com",
    repoPath: ref.repoPath ?? `${ref.owner}/${ref.name}`,
    number: ref.number,
  };
}
