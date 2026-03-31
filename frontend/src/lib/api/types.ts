import type { components, operations } from "./generated/schema.js";

export type Repo = components["schemas"]["Repo"];
export type PullRequest =
  components["schemas"]["PullRequest"] &
  Partial<Pick<components["schemas"]["PullResponse"], "repo_owner" | "repo_name">>;
export type Issue =
  components["schemas"]["Issue"] &
  Partial<Pick<components["schemas"]["IssueResponse"], "repo_owner" | "repo_name">>;
export type IssueEvent = components["schemas"]["IssueEvent"];
export type IssueDetail = components["schemas"]["IssueDetailResponse"];
export type PREvent = components["schemas"]["PREvent"];
export type PullDetail = components["schemas"]["PullDetailResponse"];
export type SyncStatus = components["schemas"]["SyncStatus"];
export type ActivityItem = components["schemas"]["ActivityItemResponse"];
export type ActivityResponse = components["schemas"]["ActivityResponse"];
export type ActivityParams = NonNullable<operations["get-activity"]["parameters"]["query"]>;
export type PullsParams = operations["list-pulls"]["parameters"]["query"];
export type IssuesParams = operations["list-issues"]["parameters"]["query"];
export type MergeParams = components["schemas"]["MergePRInputBody"];

export interface IssueLabel {
  name: string;
  color: string;
}

export type KanbanStatus = "new" | "reviewing" | "waiting" | "awaiting_merge";

export interface CICheck {
  name: string;
  status: string;
  conclusion: string;
  url: string;
  app: string;
}
