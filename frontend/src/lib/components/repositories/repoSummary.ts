import type {
  RepoSummary,
  RepoSummaryAuthor,
  RepoSummaryCommitPointResponse,
  RepoSummaryIssue,
  RepoSummaryReleaseResponse,
} from "@middleman/ui/api/types";

export type RepoSummaryCard = Omit<
  RepoSummary,
  "active_authors" | "recent_issues" | "commit_timeline" | "releases"
> & {
  active_authors: RepoSummaryAuthor[];
  recent_issues: RepoSummaryIssue[];
  commit_timeline: RepoSummaryCommitPointResponse[];
  releases: RepoSummaryReleaseResponse[];
};

export interface RepoMetric {
  label: string;
  value: number;
  tone?: "neutral" | "blue" | "amber" | "green" | "red";
  onclick?: () => void;
}

export type RepoFilter = "all" | "prs" | "issues" | "stale";
export type RepoSort = "name" | "open-prs" | "open-issues" | "activity" | "stale";

export const staleReleaseCommitThreshold = 50;

export function repoKey(summary: {
  owner: string;
  name: string;
}): string {
  return `${summary.owner}/${summary.name}`;
}

export function repoStateKey(summary: {
  platform_host: string;
  owner: string;
  name: string;
}): string {
  return `${summary.platform_host}/${summary.owner}/${summary.name}`;
}

export function shouldShowPlatformHost(summary: {
  platform_host: string;
  default_platform_host?: string;
}): boolean {
  const host = (summary.platform_host || "github.com").toLowerCase();
  const defaultHost = (summary.default_platform_host || "github.com")
    .toLowerCase();
  return host !== defaultHost;
}

export function localDateTimeLabel(dateStr: string): string {
  return new Date(dateStr).toLocaleString();
}

export function shortDateLabel(dateStr: string): string {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
  }).format(new Date(dateStr));
}

export function displayReleaseName(
  release: RepoSummaryReleaseResponse | undefined,
): string {
  if (!release) return "No release";
  return release.tag_name || release.name || "Release";
}

export function isStaleRelease(summary: RepoSummaryCard): boolean {
  return (
    summary.latest_release !== undefined
    && (summary.commits_since_release ?? 0) >= staleReleaseCommitThreshold
  );
}

export function normalizeSummaries(
  data: RepoSummary[] | null | undefined,
): RepoSummaryCard[] {
  return (data ?? []).map((summary) => ({
    ...summary,
    default_platform_host: summary.default_platform_host || "github.com",
    active_authors: summary.active_authors ?? [],
    recent_issues: summary.recent_issues ?? [],
    commit_timeline: summary.commit_timeline ?? [],
    releases: summary.releases ?? [],
  }));
}
