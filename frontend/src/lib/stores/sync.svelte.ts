import { client } from "../api/runtime.js";
import type { SyncStatus } from "../api/types.js";

// --- state ---

let status = $state<SyncStatus | null>(null);
let pollingHandle: ReturnType<typeof setInterval> | null = null;
let wasRunning = false;
let onSyncCompleteOnce: (() => void) | null = null;
const syncCompleteListeners = new Set<() => void>();

// --- reads ---

export function getSyncState(): SyncStatus | null {
  return status;
}

// --- writes ---

/** Register a callback to fire once when a running sync finishes. */
export function onNextSyncComplete(fn: () => void): void {
  onSyncCompleteOnce = fn;
}

/** Subscribe to every sync completion. Returns an unsubscribe function. */
export function subscribeSyncComplete(fn: () => void): () => void {
  syncCompleteListeners.add(fn);
  return () => { syncCompleteListeners.delete(fn); };
}

export async function refreshSyncStatus(): Promise<void> {
  try {
    const { data, error } = await client.GET("/sync/status");
    if (error) {
      throw new Error(error.detail ?? error.title ?? "failed to load sync status");
    }
    status = data ?? null;
  } catch {
    return;
  }

  const isRunning = status?.running ?? false;

  // Detect sync completion transition
  if (wasRunning && !isRunning) {
    if (onSyncCompleteOnce) {
      const cb = onSyncCompleteOnce;
      onSyncCompleteOnce = null;
      cb();
    }
    for (const fn of syncCompleteListeners) fn();
  }
  wasRunning = isRunning;

  // Adjust polling speed: 2s while syncing, 30s idle
  adjustPollingSpeed(isRunning);
}

export async function triggerSync(): Promise<void> {
  const previous = status;

  status = {
    running: true,
    last_run_at: previous?.last_run_at ?? "",
    last_error: "",
  };
  wasRunning = true;
  adjustPollingSpeed(true);

  try {
    const { error } = await client.POST("/sync");
    if (error) {
      throw new Error(error.detail ?? error.title ?? "failed to trigger sync");
    }
    await refreshSyncStatus();
  } catch (err) {
    status = {
      running: false,
      last_run_at: previous?.last_run_at ?? "",
      last_error: err instanceof Error ? err.message : "failed to trigger sync",
    };
    wasRunning = false;
    adjustPollingSpeed(false);
    throw err;
  }
}

let currentIntervalMs = 30_000;

function adjustPollingSpeed(running: boolean): void {
  const targetMs = running ? 2_000 : 30_000;
  if (targetMs === currentIntervalMs) return;
  currentIntervalMs = targetMs;
  if (pollingHandle !== null) {
    clearInterval(pollingHandle);
    pollingHandle = setInterval(() => {
      void refreshSyncStatus();
    }, currentIntervalMs);
  }
}

export function startPolling(intervalMs = 30_000): void {
  if (pollingHandle !== null) return;
  currentIntervalMs = intervalMs;
  void refreshSyncStatus();
  pollingHandle = setInterval(() => {
    void refreshSyncStatus();
  }, currentIntervalMs);
}

export function stopPolling(): void {
  if (pollingHandle === null) return;
  clearInterval(pollingHandle);
  pollingHandle = null;
}
