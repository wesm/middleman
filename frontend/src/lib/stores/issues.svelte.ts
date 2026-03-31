import { apiErrorMessage, client } from "../api/runtime.js";
import type { Issue, IssueDetail, IssuesParams } from "../api/types.js";

let issues = $state<Issue[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let filterRepo = $state<string | undefined>(undefined);
let filterStarred = $state(false);
let searchQuery = $state<string | undefined>(undefined);
let selectedIssue = $state<{ owner: string; name: string; number: number } | null>(null);

// Detail state
let issueDetail = $state<IssueDetail | null>(null);
let detailLoading = $state(false);
let detailError = $state<string | null>(null);
let detailPollHandle: ReturnType<typeof setInterval> | null = null;

// Read functions
export function getIssues(): Issue[] { return issues; }
export function isIssuesLoading(): boolean { return loading; }
export function getIssuesError(): string | null { return error; }
export function getSelectedIssue() { return selectedIssue; }
export function getIssueFilterRepo(): string | undefined { return filterRepo; }
export function getIssueFilterStarred(): boolean { return filterStarred; }
export function getIssueSearchQuery(): string | undefined { return searchQuery; }
export function getIssueDetail(): IssueDetail | null { return issueDetail; }
export function isIssueDetailLoading(): boolean { return detailLoading; }
export function getIssueDetailError(): string | null { return detailError; }

export function issuesByRepo(): Map<string, Issue[]> {
  const map = new Map<string, Issue[]>();
  for (const issue of issues) {
    const key = `${issue.repo_owner ?? ""}/${issue.repo_name ?? ""}`;
    const existing = map.get(key);
    if (existing) existing.push(issue);
    else map.set(key, [issue]);
  }
  return map;
}

// Write functions
export function setIssueFilterRepo(repo: string | undefined): void { filterRepo = repo; }
export function setIssueFilterStarred(v: boolean): void { filterStarred = v; }
export function setIssueSearchQuery(q: string | undefined): void { searchQuery = q; }
export function selectIssue(owner: string, name: string, number: number): void {
  selectedIssue = { owner, name, number };
}
export function clearIssueSelection(): void { selectedIssue = null; }

export async function loadIssues(params?: IssuesParams): Promise<void> {
  loading = true;
  error = null;
  try {
    const query = {
      repo: filterRepo,
      starred: filterStarred || undefined,
      q: searchQuery,
      ...params,
    };
    const { data, error: requestError } = await client.GET("/issues", {
      params: { query },
    });
    if (requestError) {
      throw new Error(apiErrorMessage(requestError, "failed to load issues"));
    }
    issues = (data ?? []) as Issue[];
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}

export async function loadIssueDetail(owner: string, name: string, number: number): Promise<void> {
  detailLoading = true;
  detailError = null;
  try {
    const { data, error: requestError } = await client.GET("/repos/{owner}/{name}/issues/{number}", {
      params: { path: { owner, name, number } },
    });
    if (requestError) {
      throw new Error(apiErrorMessage(requestError, "failed to load issue"));
    }
    issueDetail = data
      ? ({ ...data, events: data.events ?? [] } as IssueDetail)
      : null;
  } catch (err) {
    detailError = err instanceof Error ? err.message : String(err);
  } finally {
    detailLoading = false;
  }
}

async function refreshIssueDetail(owner: string, name: string, number: number): Promise<void> {
  try {
    const { data } = await client.GET("/repos/{owner}/{name}/issues/{number}", {
      params: { path: { owner, name, number } },
    });
    if (data !== undefined) {
      issueDetail = { ...data, events: data.events ?? [] } as IssueDetail;
    }
  } catch {
    /* silent */
  }
}

export function startIssueDetailPolling(owner: string, name: string, number: number): void {
  stopIssueDetailPolling();
  detailPollHandle = setInterval(() => { void refreshIssueDetail(owner, name, number); }, 60_000);
}

export function stopIssueDetailPolling(): void {
  if (detailPollHandle !== null) { clearInterval(detailPollHandle); detailPollHandle = null; }
}

export function clearIssueDetail(): void { issueDetail = null; detailError = null; }

export async function submitIssueComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<void> {
  detailError = null;
  try {
    const { error: requestError } = await client.POST("/repos/{owner}/{name}/issues/{number}/comments", {
      params: { path: { owner, name, number } },
      body: { body },
    });
    if (requestError) {
      throw new Error(apiErrorMessage(requestError, "failed to post comment"));
    }
  } catch (err) {
    detailError = err instanceof Error ? err.message : String(err);
    return;
  }
  await loadIssueDetail(owner, name, number);
}

export async function toggleIssueStar(
  owner: string,
  name: string,
  number: number,
  currentlyStarred: boolean,
): Promise<void> {
  try {
    if (currentlyStarred) {
      const { error: requestError } = await client.DELETE("/starred", {
        body: { item_type: "issue", owner, name, number },
      });
      if (requestError) {
        throw new Error(apiErrorMessage(requestError, "failed to unstar issue"));
      }
    } else {
      const { error: requestError } = await client.PUT("/starred", {
        body: { item_type: "issue", owner, name, number },
      });
      if (requestError) {
        throw new Error(apiErrorMessage(requestError, "failed to star issue"));
      }
    }
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    return;
  }
  // Refresh both the list and detail
  await loadIssues();
  if (issueDetail?.issue.Number === number) {
    await loadIssueDetail(owner, name, number);
  }
}

// Navigation — uses display order (grouped by repo)
function getDisplayOrderIssues(): Issue[] {
  const grouped = issuesByRepo();
  const ordered: Issue[] = [];
  for (const items of grouped.values()) {
    ordered.push(...items);
  }
  return ordered;
}

export function selectNextIssue(): void {
  const list = getDisplayOrderIssues();
  if (list.length === 0) return;
  if (selectedIssue === null) {
    const first = list[0]!;
    selectedIssue = { owner: first.repo_owner ?? "", name: first.repo_name ?? "", number: first.Number };
    return;
  }
  const idx = list.findIndex(
    (i) => (i.repo_owner ?? "") === selectedIssue!.owner &&
      (i.repo_name ?? "") === selectedIssue!.name &&
      i.Number === selectedIssue!.number,
  );
  if (idx < list.length - 1) {
    const next = list[idx + 1]!;
    selectedIssue = { owner: next.repo_owner ?? "", name: next.repo_name ?? "", number: next.Number };
  }
}

export function selectPrevIssue(): void {
  const list = getDisplayOrderIssues();
  if (list.length === 0) return;
  if (selectedIssue === null) {
    const last = list[list.length - 1]!;
    selectedIssue = { owner: last.repo_owner ?? "", name: last.repo_name ?? "", number: last.Number };
    return;
  }
  const idx = list.findIndex(
    (i) => (i.repo_owner ?? "") === selectedIssue!.owner &&
      (i.repo_name ?? "") === selectedIssue!.name &&
      i.Number === selectedIssue!.number,
  );
  if (idx > 0) {
    const prev = list[idx - 1]!;
    selectedIssue = { owner: prev.repo_owner ?? "", name: prev.repo_name ?? "", number: prev.Number };
  }
}
