import type {
  RateLimitHostStatus,
  SyncStatus,
} from "../api/types.js";
import type { MiddlemanClient } from "../types.js";

export interface SyncStoreOptions {
  client: MiddlemanClient;
}

export function createSyncStore(opts: SyncStoreOptions) {
  const apiClient = opts.client;

  // --- state ---

  let status = $state<SyncStatus | null>(null);
  let rateLimits = $state<
    Record<string, RateLimitHostStatus>
  >({});
  let pollingHandle: ReturnType<typeof setInterval> | null =
    null;
  let wasRunning = false;
  let onSyncCompleteOnce: (() => void) | null = null;
  const syncCompleteListeners = new Set<() => void>();
  let currentIntervalMs = 30_000;

  // --- reads ---

  function getSyncState(): SyncStatus | null {
    return status;
  }

  function getRateLimits(): Record<
    string,
    RateLimitHostStatus
  > {
    return rateLimits;
  }

  // --- writes ---

  function onNextSyncComplete(fn: () => void): void {
    onSyncCompleteOnce = fn;
  }

  function subscribeSyncComplete(
    fn: () => void,
  ): () => void {
    syncCompleteListeners.add(fn);
    return () => {
      syncCompleteListeners.delete(fn);
    };
  }

  async function refreshSyncStatus(): Promise<void> {
    const [syncResult, rateResult] =
      await Promise.allSettled([
        apiClient.GET("/sync/status"),
        apiClient.GET("/rate-limits"),
      ]);

    if (syncResult.status === "fulfilled") {
      const { data, error } = syncResult.value;
      if (!error && data) {
        status = data;
      }
    }

    if (rateResult.status === "fulfilled") {
      const { data, error } = rateResult.value;
      if (!error && data) {
        rateLimits = data.hosts ?? {};
      }
    }

    const isRunning = status?.running ?? false;

    if (wasRunning && !isRunning) {
      if (onSyncCompleteOnce) {
        const cb = onSyncCompleteOnce;
        onSyncCompleteOnce = null;
        cb();
      }
      for (const fn of syncCompleteListeners) fn();
    }
    wasRunning = isRunning;

    adjustPollingSpeed(isRunning);
  }

  async function triggerSync(): Promise<void> {
    const previous = status;

    status = {
      running: true,
      last_run_at: previous?.last_run_at ?? "",
      last_error: "",
    };
    wasRunning = true;
    adjustPollingSpeed(true);

    try {
      const { error } = await apiClient.POST("/sync");
      if (error) {
        throw new Error(
          error.detail ??
            error.title ??
            "failed to trigger sync",
        );
      }
      await refreshSyncStatus();
    } catch (err) {
      status = {
        running: false,
        last_run_at: previous?.last_run_at ?? "",
        last_error:
          err instanceof Error
            ? err.message
            : "failed to trigger sync",
      };
      wasRunning = false;
      adjustPollingSpeed(false);
      throw err;
    }
  }

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

  function startPolling(intervalMs = 30_000): void {
    if (pollingHandle !== null) return;
    currentIntervalMs = intervalMs;
    void refreshSyncStatus();
    pollingHandle = setInterval(() => {
      void refreshSyncStatus();
    }, currentIntervalMs);
  }

  function stopPolling(): void {
    if (pollingHandle === null) return;
    clearInterval(pollingHandle);
    pollingHandle = null;
  }

  return {
    getSyncState,
    getRateLimits,
    onNextSyncComplete,
    subscribeSyncComplete,
    refreshSyncStatus,
    triggerSync,
    startPolling,
    stopPolling,
  };
}

export type SyncStore = ReturnType<typeof createSyncStore>;
