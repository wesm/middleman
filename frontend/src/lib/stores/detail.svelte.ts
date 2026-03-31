import { client, apiErrorMessage } from "../api/runtime.js";
import type { KanbanStatus, PullDetail } from "../api/types.js";
import { getPage } from "./router.svelte.js";
import { loadPulls } from "./pulls.svelte.js";

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
    const { error: requestError } = await client.PUT("/repos/{owner}/{name}/pulls/{number}/state", {
      params: { path: { owner, name, number } },
      body: { status },
    });
    if (requestError) {
      throw new Error(requestError.detail ?? requestError.title ?? "failed to update kanban state");
    }
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    // Reload to restore accurate server state on failure.
    await loadDetail(owner, name, number);
    return;
  }
  await refreshPullsIfActive();
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
