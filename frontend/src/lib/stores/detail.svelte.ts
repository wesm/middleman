import { client, apiErrorMessage } from "../api/runtime.js";
import type { KanbanStatus, PullDetail } from "../api/types.js";
import { getPage } from "./router.svelte.js";
import { loadPulls, optimisticKanbanUpdate, getPullKanbanStatus } from "./pulls.svelte.js";

// --- state ---

let detail = $state<PullDetail | null>(null);
let loading = $state(false);
let syncing = $state(false);
let error = $state<string | null>(null);

// Monotonic counter to detect stale sync responses after item switches.
let syncGeneration = 0;

// --- reads ---

export function getDetail(): PullDetail | null {
  return detail;
}

export function isDetailLoading(): boolean {
  return loading;
}

export function isDetailSyncing(): boolean {
  return syncing;
}

export function getDetailError(): string | null {
  return error;
}

// --- writes ---

export function clearDetail(): void {
  ++syncGeneration;
  detail = null;
  loading = false;
  syncing = false;
  error = null;
}

export async function loadDetail(owner: string, name: string, number: number): Promise<void> {
  // Bump generation so any in-flight sync for a previous item is ignored.
  const gen = ++syncGeneration;

  loading = true;
  syncing = false;
  error = null;
  try {
    const { data, error: requestError } = await client.GET("/repos/{owner}/{name}/pulls/{number}", {
      params: { path: { owner, name, number } },
    });
    if (gen !== syncGeneration) return;
    if (requestError) {
      throw new Error(requestError.detail ?? requestError.title ?? "failed to load pull request");
    }
    detail = data ? ({ ...data, events: data.events ?? [] } as PullDetail) : null;
  } catch (err) {
    if (gen !== syncGeneration) return;
    error = err instanceof Error ? err.message : String(err);
  } finally {
    if (gen === syncGeneration) loading = false;
  }

  // Only sync if this load is still the active one.
  if (gen === syncGeneration) {
    void syncDetail(owner, name, number, gen);
  }
}

async function syncDetail(owner: string, name: string, number: number, gen: number): Promise<void> {
  syncing = true;
  try {
    const { data, error: requestError } = await client.POST("/repos/{owner}/{name}/pulls/{number}/sync", {
      params: { path: { owner, name, number } },
    });
    if (gen !== syncGeneration) return;
    if (requestError) {
      throw new Error(apiErrorMessage(requestError, "sync failed"));
    }
    if (data) {
      error = null;
      detail = { ...data, events: data.events ?? [] } as PullDetail;
    }
  } catch {
    // Sync failure is non-fatal — we already have cached data.
  } finally {
    if (gen === syncGeneration) syncing = false;
  }
  if (gen === syncGeneration) await refreshPullsIfActive();
}

/** Refreshes the pulls list only when the pulls list view is active. */
async function refreshPullsIfActive(): Promise<void> {
  if (getPage() === "pulls") {
    await loadPulls();
  }
}

// Per-PR monotonic counters so stale request handlers become no-ops
// without interfering with updates to other PRs.
const kanbanSeqByPR = new Map<string, number>();

function prKey(owner: string, name: string, number: number): string {
  return `${owner}/${name}/${number}`;
}

function isDetailShowing(
  owner: string,
  name: string,
  number: number,
): boolean {
  return (
    detail !== null &&
    detail.repo_owner === owner &&
    detail.repo_name === name &&
    detail.pull_request.Number === number
  );
}

/** Optimistically updates the kanban state, then refreshes the pulls list. */
export async function updateKanbanState(
  owner: string,
  name: string,
  number: number,
  status: KanbanStatus,
): Promise<void> {
  const key = prKey(owner, name, number);
  const seq = (kanbanSeqByPR.get(key) ?? 0) + 1;
  kanbanSeqByPR.set(key, seq);

  // Capture previous status for local rollback on failure.
  const prevDetailStatus = isDetailShowing(owner, name, number)
    ? detail!.pull_request.KanbanStatus as KanbanStatus
    : undefined;
  const prevPullsStatus = getPullKanbanStatus(owner, name, number);

  // Optimistic update: detail only if it shows this PR, pulls always.
  if (prevDetailStatus !== undefined) {
    detail = {
      ...detail!,
      pull_request: { ...detail!.pull_request, KanbanStatus: status },
    };
  }
  optimisticKanbanUpdate(owner, name, number, status);
  try {
    const { error: requestError } = await client.PUT("/repos/{owner}/{name}/pulls/{number}/state", {
      params: { path: { owner, name, number } },
      body: { status },
    });
    if (requestError) {
      throw new Error(requestError.detail ?? requestError.title ?? "failed to update kanban state");
    }
  } catch (err) {
    // Only reconcile if no newer request for this PR has superseded.
    if (seq === kanbanSeqByPR.get(key)) {
      error = err instanceof Error ? err.message : String(err);
      // Restore previous state locally first so the UI is correct
      // even if the server reload below also fails.
      if (prevDetailStatus !== undefined && isDetailShowing(owner, name, number)) {
        detail = {
          ...detail!,
          pull_request: { ...detail!.pull_request, KanbanStatus: prevDetailStatus },
        };
      }
      if (prevPullsStatus !== undefined) {
        optimisticKanbanUpdate(owner, name, number, prevPullsStatus);
      }
      // Best-effort server reload to get authoritative state.
      const reloads: Promise<void>[] = [loadPulls()];
      if (isDetailShowing(owner, name, number)) {
        reloads.push(loadDetail(owner, name, number));
      }
      await Promise.all(reloads);
      // Allow older in-flight requests to still reconcile, but
      // only if no newer request started during the reload.
      if (seq === kanbanSeqByPR.get(key)) {
        kanbanSeqByPR.set(key, seq - 1);
      }
    }
    return;
  }
  // Only refresh if still the latest request for this PR, so an
  // older success doesn't overwrite a newer optimistic value.
  if (seq === kanbanSeqByPR.get(key)) {
    const refreshes: Promise<void>[] = [refreshPullsIfActive()];
    if (isDetailShowing(owner, name, number)) {
      refreshes.push(loadDetail(owner, name, number));
    }
    await Promise.all(refreshes);
  }
}

// --- polling ---

let detailPollHandle: ReturnType<typeof setInterval> | null = null;

async function refreshDetail(owner: string, name: string, number: number): Promise<void> {
  try {
    const { data } = await client.GET("/repos/{owner}/{name}/pulls/{number}", {
      params: { path: { owner, name, number } },
    });
    if (data !== undefined) {
      detail = { ...data, events: data.events ?? [] } as PullDetail;
    }
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
    if (currentlyStarred) {
      const { error: requestError } = await client.DELETE("/starred", {
        body: { item_type: "pr", owner, name, number },
      });
      if (requestError) {
        throw new Error(requestError.detail ?? requestError.title ?? "failed to unstar pull request");
      }
    } else {
      const { error: requestError } = await client.PUT("/starred", {
        body: { item_type: "pr", owner, name, number },
      });
      if (requestError) {
        throw new Error(requestError.detail ?? requestError.title ?? "failed to star pull request");
      }
    }
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    if (detail !== null) {
      detail = { ...detail, pull_request: { ...detail.pull_request, Starred: currentlyStarred } };
    }
    return;
  }
  await refreshPullsIfActive();
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
    const { error: requestError } = await client.POST("/repos/{owner}/{name}/pulls/{number}/comments", {
      params: { path: { owner, name, number } },
      body: { body },
    });
    if (requestError) {
      throw new Error(requestError.detail ?? requestError.title ?? "failed to post comment");
    }
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    return;
  }
  await loadDetail(owner, name, number);
}
