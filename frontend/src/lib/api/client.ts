import type { Issue, IssueDetail, KanbanStatus, PullDetail, PullRequest, Repo, SyncStatus } from "./types.js";

const BASE = "/api/v1";

export interface PullsParams {
  repo?: string;
  state?: string;
  kanban?: KanbanStatus;
  starred?: boolean;
  q?: string;
  limit?: number;
  offset?: number;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init);
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`${init?.method ?? "GET"} ${path} → ${res.status}: ${text}`);
  }
  if (res.status === 204 || res.status === 202) {
    return undefined as unknown as T;
  }
  return res.json() as Promise<T>;
}

export async function listPulls(params?: PullsParams): Promise<PullRequest[]> {
  const search = new URLSearchParams();
  if (params?.repo !== undefined) search.set("repo", params.repo);
  if (params?.state !== undefined) search.set("state", params.state);
  if (params?.kanban !== undefined) search.set("kanban", params.kanban);
  if (params?.starred) search.set("starred", "true");
  if (params?.q !== undefined) search.set("q", params.q);
  if (params?.limit !== undefined) search.set("limit", String(params.limit));
  if (params?.offset !== undefined) search.set("offset", String(params.offset));
  const qs = search.toString();
  return request<PullRequest[]>(`/pulls${qs ? `?${qs}` : ""}`);
}

export async function getPull(owner: string, name: string, number: number): Promise<PullDetail> {
  return request<PullDetail>(`/repos/${owner}/${name}/pulls/${number}`);
}

export async function setKanbanState(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): Promise<void> {
  await request<void>(`/repos/${owner}/${name}/pulls/${number}/state`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ status }),
  });
}

export async function postComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<{ id: number; body: string }> {
  return request<{ id: number; body: string }>(
    `/repos/${owner}/${name}/pulls/${number}/comments`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ body }),
    },
  );
}

export async function getRepo(
  owner: string,
  name: string,
): Promise<Repo> {
  return request<Repo>(`/repos/${owner}/${name}`);
}

export async function approvePR(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<void> {
  await request(
    `/repos/${owner}/${name}/pulls/${number}/approve`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ body }),
    },
  );
}

export interface MergeParams {
  commit_title: string;
  commit_message: string;
  method: "merge" | "squash" | "rebase";
}

export async function mergePR(
  owner: string,
  name: string,
  number: number,
  params: MergeParams,
): Promise<{ merged: boolean; sha: string; message: string }> {
  return request(
    `/repos/${owner}/${name}/pulls/${number}/merge`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params),
    },
  );
}

export async function listRepos(): Promise<Repo[]> {
  return request<Repo[]>("/repos");
}

export async function triggerSync(): Promise<void> {
  await request<void>("/sync", { method: "POST" });
}

export async function getSyncStatus(): Promise<SyncStatus> {
  return request<SyncStatus>("/sync/status");
}

export interface IssuesParams {
  repo?: string;
  state?: string;
  starred?: boolean;
  q?: string;
  limit?: number;
  offset?: number;
}

export async function listIssues(params?: IssuesParams): Promise<Issue[]> {
  const sp = new URLSearchParams();
  if (params?.repo) sp.set("repo", params.repo);
  if (params?.state) sp.set("state", params.state);
  if (params?.starred) sp.set("starred", "true");
  if (params?.q) sp.set("q", params.q);
  if (params?.limit) sp.set("limit", String(params.limit));
  if (params?.offset) sp.set("offset", String(params.offset));
  const qs = sp.toString();
  return request<Issue[]>(`/issues${qs ? "?" + qs : ""}`);
}

export async function getIssue(owner: string, name: string, number: number): Promise<IssueDetail> {
  return request<IssueDetail>(`/repos/${owner}/${name}/issues/${number}`);
}

export async function postIssueComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<{ id: number; body: string }> {
  return request(`/repos/${owner}/${name}/issues/${number}/comments`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ body }),
  });
}

export async function setStarred(
  itemType: "pr" | "issue",
  owner: string,
  name: string,
  number: number,
): Promise<void> {
  await request("/starred", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ item_type: itemType, owner, name, number }),
  });
}

export async function unsetStarred(
  itemType: "pr" | "issue",
  owner: string,
  name: string,
  number: number,
): Promise<void> {
  await request("/starred", {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ item_type: itemType, owner, name, number }),
  });
}
