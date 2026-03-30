// TypeScript interfaces matching Go JSON output.
// Go's default encoding uses PascalCase for struct fields without json tags.
// Fields with explicit json tags use lowercase (e.g., repo_owner, repo_name).

export interface Repo {
  ID: number;
  Owner: string;
  Name: string;
  LastSyncStartedAt: string | null;
  LastSyncCompletedAt: string | null;
  LastSyncError: string;
  CreatedAt: string;
}

export interface PullRequest {
  ID: number;
  RepoID: number;
  GitHubID: number;
  Number: number;
  URL: string;
  Title: string;
  Author: string;
  State: string;
  IsDraft: boolean;
  Body: string;
  HeadBranch: string;
  BaseBranch: string;
  Additions: number;
  Deletions: number;
  CommentCount: number;
  ReviewDecision: string;
  CIStatus: string;
  CIChecksJSON: string;
  CreatedAt: string;
  UpdatedAt: string;
  LastActivityAt: string;
  MergedAt: string | null;
  ClosedAt: string | null;
  KanbanStatus: string;
  Starred: boolean;
  // Enrichment fields from list endpoint (json-tagged, lowercase)
  repo_owner?: string;
  repo_name?: string;
}

export interface Issue {
  ID: number;
  RepoID: number;
  GitHubID: number;
  Number: number;
  URL: string;
  Title: string;
  Author: string;
  State: string;
  Body: string;
  CommentCount: number;
  LabelsJSON: string;
  CreatedAt: string;
  UpdatedAt: string;
  LastActivityAt: string;
  ClosedAt: string | null;
  Starred: boolean;
  repo_owner?: string;
  repo_name?: string;
}

export interface IssueEvent {
  ID: number;
  IssueID: number;
  GitHubID: number | null;
  EventType: string;
  Author: string;
  Summary: string;
  Body: string;
  MetadataJSON: string;
  CreatedAt: string;
  DedupeKey: string;
}

export interface IssueDetail {
  issue: Issue;
  events: IssueEvent[];
  repo_owner: string;
  repo_name: string;
}

export interface IssueLabel {
  name: string;
  color: string;
}

export interface PREvent {
  ID: number;
  PRID: number;
  GitHubID: number | null;
  EventType: string;
  Author: string;
  Summary: string;
  Body: string;
  MetadataJSON: string;
  CreatedAt: string;
  DedupeKey: string;
}

export interface PullDetail {
  pull_request: PullRequest;
  events: PREvent[];
  repo_owner: string;
  repo_name: string;
}

export interface SyncStatus {
  running: boolean;
  last_run_at: string;
  last_error: string;
}

export type KanbanStatus = "new" | "reviewing" | "waiting" | "awaiting_merge";

export interface CICheck {
  name: string;
  status: string;
  conclusion: string;
  url: string;
  app: string;
}
