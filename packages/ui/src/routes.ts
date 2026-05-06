export type RepositoryRouteRef = {
  provider: string;
  platformHost?: string | undefined;
  owner: string;
  name: string;
  repoPath: string;
};

export type ProviderRouteRef = {
  provider: string;
  platformHost?: string | undefined;
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
  return `/focus/pr${providerItemQuery(providerRouteRef(ref))}`;
}

export function buildFocusIssueRoute(ref: IssueRouteRef): string {
  return `/focus/issue${providerItemQuery(providerRouteRef(ref))}`;
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

function requireRouteText(value: string, field: string): string {
  if (!value) {
    throw new Error(`missing route ${field}`);
  }
  return value;
}

function providerItemQuery(ref: ProviderRouteRef & { number: number }): string {
  const params: Array<[string, string]> = [
    ["provider", requireRouteText(ref.provider, "provider")],
    ["repo_path", requireRouteText(ref.repoPath, "repoPath")],
    ["number", ref.number.toString()],
  ];
  if (ref.platformHost) {
    params.splice(1, 0, ["platform_host", ref.platformHost]);
  }
  return `?${params.map(([key, value]) => `${key}=${encodeURIComponent(value)}`).join("&")}`;
}

function providerRouteRef(ref: NumberedRouteItemRef): ProviderRouteRef & { number: number } {
  return {
    provider: ref.provider,
    platformHost: ref.platformHost,
    repoPath: ref.repoPath,
    number: ref.number,
  };
}
