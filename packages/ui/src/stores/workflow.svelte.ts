import type { PullRequest } from "../api/types.js";

export type WorkflowGroup =
  | "needsWorktree"
  | "blocked"
  | "readyToMerge"
  | "inProgress"
  | "needsReview";

export const workflowGroupOrder: WorkflowGroup[] = [
  "needsWorktree",
  "blocked",
  "readyToMerge",
  "inProgress",
  "needsReview",
];

export const workflowGroupLabels: Record<
  WorkflowGroup,
  string
> = {
  needsWorktree: "Needs Worktree",
  blocked: "Blocked",
  readyToMerge: "Ready to Merge",
  inProgress: "In Progress",
  needsReview: "Needs Review",
};

export interface WorkflowGroupEntry {
  group: WorkflowGroup;
  label: string;
  items: PullRequest[];
}

/**
 * Classify a PR into a workflow group.
 *
 * Precedence:
 * 1. Closed/merged -> fallback "needsReview"
 * 2. No worktree + open -> "needsWorktree"
 * 3. Linked to active worktree -> "inProgress"
 * 4. Failing CI -> "blocked"
 * 5. Changes requested -> "blocked"
 * 6. Merge conflicts or blocked merge state -> "blocked"
 * 7. Approved + not draft + CI not failing -> "readyToMerge"
 * 8. Review required -> "needsReview"
 * 9. Draft -> "needsReview"
 * 10. Fallback -> "needsReview"
 */
export function classifyPR(
  pr: PullRequest,
  activeWorktreeKey?: string,
): WorkflowGroup {
  if (pr.State === "merged" || pr.State === "closed") {
    return "needsReview";
  }

  const hasWorktree =
    (pr.worktree_links?.length ?? 0) > 0;

  if (!hasWorktree && pr.State === "open") {
    return "needsWorktree";
  }

  if (
    activeWorktreeKey &&
    pr.worktree_links?.some(
      (l) => l.worktree_key === activeWorktreeKey,
    )
  ) {
    return "inProgress";
  }

  if (pr.CIStatus === "failure") {
    return "blocked";
  }

  if (pr.ReviewDecision === "CHANGES_REQUESTED") {
    return "blocked";
  }

  if (
    pr.MergeableState === "dirty" ||
    pr.MergeableState === "blocked"
  ) {
    return "blocked";
  }

  if (
    pr.ReviewDecision === "APPROVED" &&
    !pr.IsDraft &&
    pr.CIStatus !== "failure"
  ) {
    return "readyToMerge";
  }

  if (pr.ReviewDecision === "REVIEW_REQUIRED") {
    return "needsReview";
  }

  if (pr.IsDraft) {
    return "needsReview";
  }

  return "needsReview";
}

/**
 * Group PRs by workflow classification.
 *
 * Returns groups in display order, omitting empty groups.
 * Items within each group are sorted by LastActivityAt
 * descending.
 */
export function groupByWorkflow(
  prs: PullRequest[],
  activeWorktreeKey?: string,
): WorkflowGroupEntry[] {
  const buckets = new Map<WorkflowGroup, PullRequest[]>();
  for (const g of workflowGroupOrder) {
    buckets.set(g, []);
  }

  for (const pr of prs) {
    const group = classifyPR(pr, activeWorktreeKey);
    buckets.get(group)!.push(pr);
  }

  const result: WorkflowGroupEntry[] = [];
  for (const group of workflowGroupOrder) {
    const items = buckets.get(group)!;
    if (items.length === 0) continue;
    items.sort(
      (a, b) =>
        new Date(b.LastActivityAt).getTime() -
        new Date(a.LastActivityAt).getTime(),
    );
    result.push({
      group,
      label: workflowGroupLabels[group],
      items,
    });
  }

  return result;
}
