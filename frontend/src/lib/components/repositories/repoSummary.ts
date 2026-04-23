import type {
  RepoSummary,
  RepoSummaryAuthor,
  RepoSummaryIssue,
} from "@middleman/ui/api/types";

export type RepoSummaryCard = Omit<
  RepoSummary,
  "active_authors" | "recent_issues"
> & {
  active_authors: RepoSummaryAuthor[];
  recent_issues: RepoSummaryIssue[];
};

export interface RepoMetric {
  label: string;
  value: number;
  tone?: "neutral" | "blue" | "amber" | "green";
  onclick?: () => void;
}

export function repoKey(summary: {
  owner: string;
  name: string;
}): string {
  return `${summary.owner}/${summary.name}`;
}

export function localDateTimeLabel(dateStr: string): string {
  return new Date(dateStr).toLocaleString();
}

export function normalizeSummaries(
  data: RepoSummary[] | null | undefined,
): RepoSummaryCard[] {
  return (data ?? []).map((summary) => ({
    ...summary,
    active_authors: summary.active_authors ?? [],
    recent_issues: summary.recent_issues ?? [],
  }));
}
