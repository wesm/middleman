import { client } from "../api/runtime.js";
import type { KanbanStatus, PullRequest } from "../api/types.js";

type PullsParams = {
  repo?: string;
  state?: string;
  kanban?: KanbanStatus;
  starred?: boolean;
  q?: string;
  limit?: number;
  offset?: number;
};

// --- state ---

let pulls = $state<PullRequest[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let filterRepo = $state<string | undefined>(undefined);
let filterKanban = $state<KanbanStatus | undefined>(undefined);
let filterStarred = $state(false);
let filterState = $state<string>("open");
let searchQuery = $state<string | undefined>(undefined);
let selectedPR = $state<{ owner: string; name: string; number: number } | null>(null);

// --- reads ---

export function getPulls(): PullRequest[] {
  return pulls;
}

export function isLoading(): boolean {
  return loading;
}

export function getError(): string | null {
  return error;
}

export function getSelectedPR(): { owner: string; name: string; number: number } | null {
  return selectedPR;
}

/** Groups pulls by "owner/name" into a Map. */
export function pullsByRepo(): Map<string, PullRequest[]> {
  const map = new Map<string, PullRequest[]>();
  for (const pr of pulls) {
    const key = `${pr.repo_owner ?? ""}/${pr.repo_name ?? ""}`;
    const existing = map.get(key);
    if (existing !== undefined) {
      existing.push(pr);
    } else {
      map.set(key, [pr]);
    }
  }
  return map;
}

export function getFilterRepo(): string | undefined {
  return filterRepo;
}

export function getFilterKanban(): KanbanStatus | undefined {
  return filterKanban;
}

export function getFilterStarred(): boolean {
  return filterStarred;
}

export function setFilterStarred(v: boolean): void {
  filterStarred = v;
}

export function getFilterState(): string {
  return filterState;
}

export function setFilterState(s: string): void {
  filterState = s;
}

/** Returns PRs in display order (grouped by repo, then by activity within group). */
export function getDisplayOrderPRs(): PullRequest[] {
  const grouped = pullsByRepo();
  const ordered: PullRequest[] = [];
  for (const prs of grouped.values()) {
    ordered.push(...prs);
  }
  return ordered;
}

export function selectNextPR(): void {
  const list = getDisplayOrderPRs();
  if (list.length === 0) return;
  const sel = selectedPR;
  if (sel === null) {
    const first = list[0];
    if (first !== undefined) {
      selectPR(first.repo_owner ?? "", first.repo_name ?? "", first.Number);
    }
    return;
  }
  const idx = list.findIndex(
    (pr) =>
      (pr.repo_owner ?? "") === sel.owner &&
      (pr.repo_name ?? "") === sel.name &&
      pr.Number === sel.number,
  );
  const next = list[idx + 1];
  if (next !== undefined) {
    selectPR(next.repo_owner ?? "", next.repo_name ?? "", next.Number);
  }
}

export function selectPrevPR(): void {
  const list = getDisplayOrderPRs();
  if (list.length === 0) return;
  const sel = selectedPR;
  if (sel === null) {
    const last = list[list.length - 1];
    if (last !== undefined) {
      selectPR(last.repo_owner ?? "", last.repo_name ?? "", last.Number);
    }
    return;
  }
  const idx = list.findIndex(
    (pr) =>
      (pr.repo_owner ?? "") === sel.owner &&
      (pr.repo_name ?? "") === sel.name &&
      pr.Number === sel.number,
  );
  if (idx > 0) {
    const prev = list[idx - 1];
    if (prev !== undefined) {
      selectPR(prev.repo_owner ?? "", prev.repo_name ?? "", prev.Number);
    }
  }
}

// --- writes ---

export function setFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}

export function setFilterKanban(kanban: KanbanStatus | undefined): void {
  filterKanban = kanban;
}

export function getSearchQuery(): string | undefined {
  return searchQuery;
}

export function setSearchQuery(q: string | undefined): void {
  searchQuery = q;
}

export function selectPR(owner: string, name: string, number: number): void {
  selectedPR = { owner, name, number };
}

export function clearSelection(): void {
  selectedPR = null;
}

/** Returns the current kanban status for a PR in the pulls list. */
export function getPullKanbanStatus(
  owner: string,
  name: string,
  number: number,
): KanbanStatus | undefined {
  const pr = pulls.find(
    (p) => p.repo_owner === owner && p.repo_name === name && p.Number === number,
  );
  return pr?.KanbanStatus as KanbanStatus | undefined;
}

/** Optimistically update a single PR's kanban status in the pulls list. */
export function optimisticKanbanUpdate(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): void {
  pulls = pulls.map((pr) =>
    pr.repo_owner === owner &&
    pr.repo_name === name &&
    pr.Number === number
      ? { ...pr, KanbanStatus: status }
      : pr,
  );
}

export async function togglePRStar(
  owner: string,
  name: string,
  number: number,
  currentlyStarred: boolean,
): Promise<void> {
  try {
    if (currentlyStarred) {
      const { error } = await client.DELETE("/starred", {
        body: { item_type: "pr", owner, name, number },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to unstar pull request");
      }
    } else {
      const { error } = await client.PUT("/starred", {
        body: { item_type: "pr", owner, name, number },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to star pull request");
      }
    }
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    return;
  }
  await loadPulls();
}

export async function loadPulls(params?: PullsParams): Promise<void> {
  loading = true;
  error = null;
  try {
    const merged = {
      state: filterState,
      ...(filterRepo !== undefined && { repo: filterRepo }),
      ...(filterKanban !== undefined && { kanban: filterKanban }),
      ...(filterStarred && { starred: true }),
      ...(searchQuery !== undefined && { q: searchQuery }),
      ...params,
    };
    const { data, error } = await client.GET("/pulls", {
      params: { query: merged },
    });
    if (error) {
      throw new Error(error.detail ?? error.title ?? "failed to load pulls");
    }
    pulls = data ?? [];
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}
