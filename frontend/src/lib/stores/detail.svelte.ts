import { getPull, postComment, setKanbanState, setStarred, unsetStarred } from "../api/client.js";
import type { KanbanStatus, PullDetail } from "../api/types.js";
import { loadPulls } from "./pulls.svelte.js";

// --- state ---

let detail = $state<PullDetail | null>(null);
let loading = $state(false);
let error = $state<string | null>(null);

// --- reads ---

export function getDetail(): PullDetail | null {
  return detail;
}

export function isDetailLoading(): boolean {
  return loading;
}

export function getDetailError(): string | null {
  return error;
}

// --- writes ---

export function clearDetail(): void {
  detail = null;
  error = null;
}

export async function loadDetail(owner: string, name: string, number: number): Promise<void> {
  loading = true;
  error = null;
  try {
    detail = await getPull(owner, name, number);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  } finally {
    loading = false;
  }
}

/** Optimistically updates the kanban state, then refreshes the pulls list. */
export async function updateKanbanState(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): Promise<void> {
  // Optimistic update on the cached detail.
  if (detail !== null) {
    detail = {
      ...detail,
      pull_request: { ...detail.pull_request, KanbanStatus: status },
    };
  }
  try {
    await setKanbanState(owner, name, number, status);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    // Reload to restore accurate server state on failure.
    await loadDetail(owner, name, number);
    return;
  }
  // Refresh the pulls list so the board/list view stays current.
  await loadPulls();
}

// --- polling ---

let detailPollHandle: ReturnType<typeof setInterval> | null = null;

async function refreshDetail(owner: string, name: string, number: number): Promise<void> {
  try {
    detail = await getPull(owner, name, number);
  } catch {
    // Silent refresh - don't overwrite error state
  }
}

export function startDetailPolling(owner: string, name: string, number: number): void {
  stopDetailPolling();
  detailPollHandle = setInterval(() => {
    void refreshDetail(owner, name, number);
  }, 60_000);
}

export function stopDetailPolling(): void {
  if (detailPollHandle !== null) {
    clearInterval(detailPollHandle);
    detailPollHandle = null;
  }
}

export async function toggleDetailPRStar(
  owner: string,
  name: string,
  number: number,
  currentlyStarred: boolean,
): Promise<void> {
  // Optimistic update
  if (detail !== null) {
    detail = { ...detail, pull_request: { ...detail.pull_request, Starred: !currentlyStarred } };
  }
  try {
    if (currentlyStarred) await unsetStarred("pr", owner, name, number);
    else await setStarred("pr", owner, name, number);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    if (detail !== null) {
      detail = { ...detail, pull_request: { ...detail.pull_request, Starred: currentlyStarred } };
    }
    return;
  }
  await loadPulls();
}

/** Posts a comment to GitHub, then reloads the detail to show the new event. */
export async function submitComment(
  owner: string,
  name: string,
  number: number,
  body: string,
): Promise<void> {
  error = null;
  try {
    await postComment(owner, name, number, body);
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    return;
  }
  await loadDetail(owner, name, number);
}
