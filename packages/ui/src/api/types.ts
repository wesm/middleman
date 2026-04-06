import type { components, operations } from "./generated/schema.js";

export type Repo = components["schemas"]["Repo"];
export type PullRequest =
  components["schemas"]["MergeRequest"] &
  Partial<Pick<components["schemas"]["MergeRequestResponse"], "repo_owner" | "repo_name">>;
export type Issue =
  components["schemas"]["Issue"] &
  Partial<Pick<components["schemas"]["IssueResponse"], "repo_owner" | "repo_name">>;
export type IssueEvent = components["schemas"]["IssueEvent"];
export type IssueDetail = components["schemas"]["IssueDetailResponse"];
export type PREvent = components["schemas"]["MREvent"];
export type PullDetail = components["schemas"]["MergeRequestDetailResponse"];
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

export interface ActivitySettings {
  view_mode: "flat" | "threaded";
  time_range: "24h" | "7d" | "30d" | "90d";
  hide_closed: boolean;
  hide_bots: boolean;
}

export interface ConfigRepo {
  owner: string;
  name: string;
}

export interface Settings {
  repos: ConfigRepo[];
  activity: ActivitySettings;
}

export interface DiffResult {
  stale: boolean;
  whitespace_only_count: number;
  files: DiffFile[];
}

export interface DiffFile {
  path: string;
  old_path: string;
  status: "added" | "modified" | "deleted" | "renamed" | "copied";
  is_binary: boolean;
  is_whitespace_only: boolean;
  additions: number;
  deletions: number;
  hunks: DiffHunk[];
}

export interface DiffHunk {
  old_start: number;
  old_count: number;
  new_start: number;
  new_count: number;
  section?: string;
  lines: DiffLine[];
}

export interface DiffLine {
  type: "context" | "add" | "delete";
  content: string;
  old_num?: number;
  new_num?: number;
  no_newline?: boolean;
}
