import { getSyncStatus, triggerSync as apiTriggerSync } from "../api/client.js";
import type { SyncStatus } from "../api/types.js";

// --- state ---

let status = $state<SyncStatus | null>(null);
let pollingHandle: ReturnType<typeof setInterval> | null = null;

// --- reads ---

export function getSyncState(): SyncStatus | null {
  return status;
}

// --- writes ---

export async function refreshSyncStatus(): Promise<void> {
  try {
    status = await getSyncStatus();
  } catch {
    // Silently ignore polling errors — the UI will show stale data.
  }
}

export async function triggerSync(): Promise<void> {
  await apiTriggerSync();
  await refreshSyncStatus();
}

export function startPolling(intervalMs = 30_000): void {
  if (pollingHandle !== null) return;
  void refreshSyncStatus();
  pollingHandle = setInterval(() => {
    void refreshSyncStatus();
  }, intervalMs);
}

export function stopPolling(): void {
  if (pollingHandle === null) return;
  clearInterval(pollingHandle);
  pollingHandle = null;
}
