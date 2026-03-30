import { listPulls } from "../api/client.js";
import type { KanbanStatus, PullRequest } from "../api/types.js";
import type { PullsParams } from "../api/client.js";

// --- state ---

let pulls = $state<PullRequest[]>([]);
let loading = $state(false);
let error = $state<string | null>(null);
let filterRepo = $state<string | undefined>(undefined);
let filterKanban = $state<KanbanStatus | undefined>(undefined);
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

// --- writes ---

export function setFilterRepo(repo: string | undefined): void {
  filterRepo = repo;
}

export function setFilterKanban(kanban: KanbanStatus | undefined): void {
  filterKanban = kanban;
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

export async function loadPulls(params?: PullsParams): Promise<void> {
  loading = true;
  error = null;
  try {
    const merged: PullsParams = {
      repo: filterRepo,
      kanban: filterKanban,
      q: searchQuery,
      ...params,
    };
    pulls = await listPulls(merged);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}
