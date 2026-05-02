import type {
  KanbanStatus,
  PullDetail,
} from "../api/types.js";
import type { MiddlemanClient } from "../types.js";

export type DetailSyncMode = boolean | "background";

export interface DetailStoreOptions {
  client: MiddlemanClient;
  getPage?: () => string;
  pulls?: {
    loadPulls: (params?: unknown) => Promise<void>;
    optimisticKanbanUpdate?: (
      owner: string,
      name: string,
      number: number,
      platformHost: string | undefined,
      status: KanbanStatus,
    ) => void;
    getPullKanbanStatus?: (
      owner: string,
      name: string,
      number: number,
      platformHost?: string | undefined,
    ) => KanbanStatus | undefined;
  };
  sync?: {
    subscribeSyncComplete: (
      cb: () => void,
    ) => () => void;
    refreshSyncStatus?: () => Promise<void>;
  };
}

function apiErrorMessage(
  error: { detail?: string; title?: string },
  fallback: string,
): string {
  return error.detail ?? error.title ?? fallback;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function syncIntentRank(mode: DetailSyncMode): number {
  if (mode === true) return 2;
  if (mode === "background") return 1;
  return 0;
}

function strongerSyncMode(
  a: DetailSyncMode,
  b: DetailSyncMode,
): DetailSyncMode {
  return syncIntentRank(b) > syncIntentRank(a) ? b : a;
}

export function createDetailStore(
  opts: DetailStoreOptions,
) {
  const apiClient = opts.client;
  const getPage = opts.getPage ?? (() => "");
  const pullsDep = opts.pulls;
  const syncDep = opts.sync;

  // --- state ---

  let detail = $state<PullDetail | null>(null);
  let loading = $state(false);
  let syncing = $state(false);
  let storeError = $state<string | null>(null);
  let detailLoaded = $state(false);
  let syncGeneration = 0;
  let activeLoad: {
    key: string;
    promise: Promise<void> | null;
    syncMode: DetailSyncMode;
  } | null = null;

  // Per-PR monotonic counters for kanban updates.
  const kanbanSeqByPR = new Map<string, number>();

  // --- polling ---

  let detailPollHandle: ReturnType<
    typeof setInterval
  > | null = null;
  let unsubSyncComplete: (() => void) | null = null;

  // --- reads ---

  function getDetail(): PullDetail | null {
    return detail;
  }

  function isDetailLoading(): boolean {
    return loading;
  }

  function isDetailSyncing(): boolean {
    return syncing;
  }

  function getDetailError(): string | null {
    return storeError;
  }

  function getDetailLoaded(): boolean {
    return detailLoaded;
  }

  function isStaleRefreshing(): boolean {
    if (!detail || !syncing) return false;
    const fetchedAt = detail.detail_fetched_at;
    if (!fetchedAt) return false;
    const fetchedMs = new Date(fetchedAt).getTime();
    const updatedMs = new Date(
      detail.merge_request.UpdatedAt,
    ).getTime();
    const hourAgo = Date.now() - 3_600_000;
    return fetchedMs < hourAgo && updatedMs > fetchedMs;
  }

  // --- internal helpers ---

  function prKey(
    owner: string,
    name: string,
    number: number,
    platformHost?: string,
  ): string {
    return `${platformHost ?? ""}:${owner}/${name}/${number}`;
  }

  function isDetailShowing(
    owner: string,
    name: string,
    number: number,
    platformHost?: string,
  ): boolean {
    return (
      detail !== null &&
      detail.repo_owner === owner &&
      detail.repo_name === name &&
      detail.merge_request.Number === number &&
      (platformHost === undefined || detail.platform_host === platformHost)
    );
  }

  async function refreshPullsIfActive(): Promise<void> {
    if (getPage() === "pulls" && pullsDep) {
      await pullsDep.loadPulls();
    }
  }

  async function refreshDetail(
    owner: string,
    name: string,
    number: number,
    platformHost?: string,
    expectedGen: number = syncGeneration,
  ): Promise<void> {
    try {
      const { data } = await apiClient.GET(
        "/repos/{owner}/{name}/pulls/{number}",
        {
          params: {
            path: { owner, name, number },
            ...(platformHost ? { query: { platform_host: platformHost } } : {}),
          },
        },
      );
      // Re-check the generation after the awaited request: if the
      // selected PR changed mid-flight, dropping the assignment keeps
      // the new selection's data from being clobbered.
      if (expectedGen !== syncGeneration) return;
      if (data !== undefined) {
        detail = {
          ...data,
          events: data.events ?? [],
        } as PullDetail;
        detailLoaded = data.detail_loaded ?? detailLoaded;
      }
    } catch {
      // Silent refresh
    }
  }

  async function syncDetail(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    gen: number,
  ): Promise<void> {
    syncing = true;
    try {
      const { data, error: requestError } =
        await apiClient.POST(
          "/repos/{owner}/{name}/pulls/{number}/sync",
          {
            params: {
              path: { owner, name, number },
              ...(platformHost ? { query: { platform_host: platformHost } } : {}),
            },
          },
        );
      if (gen !== syncGeneration) return;
      if (requestError) {
        throw new Error(
          apiErrorMessage(requestError, "sync failed"),
        );
      }
      if (data) {
        storeError = null;
        detail = {
          ...data,
          events: data.events ?? [],
        } as PullDetail;
        detailLoaded =
          data.detail_loaded ?? detailLoaded;
      }
    } catch {
      // Sync failure is non-fatal.
    } finally {
      if (gen === syncGeneration) syncing = false;
    }
    // Always refresh rate limits -- the API calls happened
    // regardless of whether user navigated away.
    void syncDep?.refreshSyncStatus?.();
    if (gen === syncGeneration) {
      await refreshPullsIfActive();
    }
  }

  // --- writes ---

  function clearDetail(): void {
    ++syncGeneration;
    activeLoad = null;
    detail = null;
    loading = false;
    syncing = false;
    storeError = null;
    detailLoaded = false;
  }

  async function loadDetail(
    owner: string,
    name: string,
    number: number,
    options?: { sync?: DetailSyncMode; platformHost?: string | undefined },
  ): Promise<void> {
    const syncMode = options?.sync ?? true;
    const platformHost = options?.platformHost;
    // Dedup by item identity only. A second caller with a different
    // sync mode joins the in-flight load and may promote the sync
    // intent if its requested mode is stronger.
    const key = prKey(owner, name, number, platformHost);
    if (
      loading &&
      activeLoad?.key === key &&
      activeLoad.promise !== null
    ) {
      activeLoad.syncMode = strongerSyncMode(
        activeLoad.syncMode,
        syncMode,
      );
      return activeLoad.promise;
    }

    const gen = ++syncGeneration;
    const currentLoad: {
      key: string;
      promise: Promise<void> | null;
      syncMode: DetailSyncMode;
    } = { key, promise: null, syncMode };
    activeLoad = currentLoad;

    // Keep the previously loaded detail visible while the new one
    // is in flight. Nulling `detail` here flipped consumers to a
    // "Loading…" empty state for every prop change, which produced
    // a visible flash when, for example, the workspace right
    // sidebar updates from one PR to another. Consumers that need
    // a "first load" empty state should check `detail === null`
    // alongside `loading`.
    loading = true;
    syncing = false;
    storeError = null;
    detailLoaded = false;
    const promise = (async () => {
      try {
        const { data, error: requestError } =
          await apiClient.GET(
            "/repos/{owner}/{name}/pulls/{number}",
            {
              params: {
                path: { owner, name, number },
                ...(platformHost ? { query: { platform_host: platformHost } } : {}),
              },
            },
          );
        if (gen !== syncGeneration) return;
        if (requestError) {
          throw new Error(
            requestError.detail ??
              requestError.title ??
              "failed to load pull request",
          );
        }
        detail = data
          ? ({
              ...data,
              events: data.events ?? [],
            } as PullDetail)
          : null;
        detailLoaded = data?.detail_loaded ?? false;
      } catch (err) {
        if (gen !== syncGeneration) return;
        storeError =
          err instanceof Error ? err.message : String(err);
      } finally {
        if (gen === syncGeneration) loading = false;
        if (activeLoad === currentLoad) activeLoad = null;
      }

      // Use the latest promoted sync intent so a stronger caller's
      // request isn't lost when it joined an in-flight load.
      const finalSyncMode = currentLoad.syncMode;
      if (gen === syncGeneration && finalSyncMode === true) {
        void syncDetail(owner, name, number, platformHost, gen);
      } else if (gen === syncGeneration && finalSyncMode === "background") {
        void enqueueBackgroundDetailSync(
          owner,
          name,
          number,
          platformHost,
          gen,
          detail?.detail_fetched_at,
        );
      }
    })();
    currentLoad.promise = promise;
    return promise;
  }

  async function enqueueBackgroundDetailSync(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    gen: number,
    previousFetchedAt?: string,
  ): Promise<void> {
    syncing = true;
    try {
      const { error: requestError } = await apiClient.POST(
        "/repos/{owner}/{name}/pulls/{number}/sync/async",
        {
          params: {
            path: { owner, name, number },
            ...(platformHost ? { query: { platform_host: platformHost } } : {}),
          },
        },
      );
      if (requestError) return;
      await refreshAfterBackgroundDetailSync(
        owner,
        name,
        number,
        platformHost,
        gen,
        previousFetchedAt,
      );
    } finally {
      if (gen === syncGeneration) syncing = false;
      void syncDep?.refreshSyncStatus?.();
    }
  }

  async function refreshAfterBackgroundDetailSync(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    gen: number,
    previousFetchedAt?: string,
  ): Promise<void> {
    for (const ms of [300, 700, 1_500, 3_000, 5_000]) {
      await delay(ms);
      if (gen !== syncGeneration) return;
      await refreshDetail(owner, name, number, platformHost, gen);
      if (gen !== syncGeneration) return;
      const fetchedAt = detail?.detail_fetched_at;
      if (fetchedAt && fetchedAt !== previousFetchedAt) {
        return;
      }
    }
  }

  async function refreshDetailOnly(
    owner: string,
    name: string,
    number: number,
    platformHost?: string,
  ): Promise<void> {
    await refreshDetail(owner, name, number, platformHost);
  }

  async function updateKanbanState(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    status: KanbanStatus,
  ): Promise<void> {
    const key = prKey(owner, name, number, platformHost);
    const seq = (kanbanSeqByPR.get(key) ?? 0) + 1;
    kanbanSeqByPR.set(key, seq);

    const prevDetailStatus = isDetailShowing(
      owner,
      name,
      number,
      platformHost,
    )
      ? (detail!.merge_request
          .KanbanStatus as KanbanStatus)
      : undefined;
    const prevPullsStatus =
      pullsDep?.getPullKanbanStatus?.(
        owner,
        name,
        number,
        platformHost,
      );

    if (prevDetailStatus !== undefined) {
      detail = {
        ...detail!,
        merge_request: {
          ...detail!.merge_request,
          KanbanStatus: status,
        },
      };
    }
    pullsDep?.optimisticKanbanUpdate?.(
      owner,
      name,
      number,
      platformHost,
      status,
    );

    try {
      const { error: requestError } =
        await apiClient.PUT(
          "/repos/{owner}/{name}/pulls/{number}/state",
            {
              params: {
                path: { owner, name, number },
                ...(platformHost ? { query: { platform_host: platformHost } } : {}),
              },
              body: { status },
            },
          );
      if (requestError) {
        throw new Error(
          requestError.detail ??
            requestError.title ??
            "failed to update kanban state",
        );
      }
    } catch (err) {
      if (seq === kanbanSeqByPR.get(key)) {
        storeError =
          err instanceof Error
            ? err.message
            : String(err);
        if (
          prevDetailStatus !== undefined &&
          isDetailShowing(owner, name, number, platformHost)
        ) {
          detail = {
            ...detail!,
            merge_request: {
              ...detail!.merge_request,
              KanbanStatus: prevDetailStatus,
            },
          };
        }
        if (prevPullsStatus !== undefined) {
          pullsDep?.optimisticKanbanUpdate?.(
            owner,
            name,
            number,
            platformHost,
            prevPullsStatus,
          );
        }
        const reloads: Promise<void>[] = [];
        if (pullsDep) reloads.push(pullsDep.loadPulls());
        if (isDetailShowing(owner, name, number, platformHost)) {
          reloads.push(
            loadDetail(owner, name, number, { platformHost }),
          );
        }
        await Promise.all(reloads);
        if (seq === kanbanSeqByPR.get(key)) {
          kanbanSeqByPR.set(key, seq - 1);
        }
      }
      return;
    }

    if (seq === kanbanSeqByPR.get(key)) {
      const refreshes: Promise<void>[] = [
        refreshPullsIfActive(),
      ];
      if (isDetailShowing(owner, name, number, platformHost)) {
        refreshes.push(
          loadDetail(owner, name, number, { platformHost }),
        );
      }
      await Promise.all(refreshes);
    }
  }

  async function updatePRContent(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    fields: { title?: string; body?: string },
  ): Promise<void> {
    if (!detail || !isDetailShowing(owner, name, number, platformHost))
      return;

    const prevTitle = detail.merge_request.Title;
    const prevBody = detail.merge_request.Body;

    // Optimistic update.
    detail = {
      ...detail,
      merge_request: {
        ...detail.merge_request,
        ...(fields.title !== undefined && {
          Title: fields.title,
        }),
        ...(fields.body !== undefined && {
          Body: fields.body,
        }),
      },
    };

    try {
      const { data, error: requestError } =
        await apiClient.PATCH(
          "/repos/{owner}/{name}/pulls/{number}",
          {
            params: {
              path: { owner, name, number },
              ...(platformHost ? { query: { platform_host: platformHost } } : {}),
            },
            body: fields,
          },
        );
      if (requestError) {
        throw new Error(
          apiErrorMessage(
            requestError,
            "failed to update PR",
          ),
        );
      }
      // Apply server-canonical response.
      if (data && isDetailShowing(owner, name, number, platformHost)) {
        detail = data as PullDetail;
      }
    } catch (err) {
      storeError =
        err instanceof Error ? err.message : String(err);
      // Revert optimistic update.
      if (
        isDetailShowing(owner, name, number, platformHost) &&
        detail
      ) {
        detail = {
          ...detail,
          merge_request: {
            ...detail.merge_request,
            Title: prevTitle,
            Body: prevBody,
          },
        };
      }
      throw err;
    }
    // Refresh pulls list independently -- don't let a
    // refresh failure revert a successful edit.
    refreshPullsIfActive().catch(() => {});
  }

  function startDetailPolling(
    owner: string,
    name: string,
    number: number,
    platformHost?: string,
  ): void {
    stopDetailPolling();
    detailPollHandle = setInterval(() => {
      void refreshDetail(owner, name, number, platformHost);
    }, 60_000);
    if (syncDep) {
      unsubSyncComplete =
        syncDep.subscribeSyncComplete(() => {
          void refreshDetail(owner, name, number, platformHost);
        });
    }
  }

  function stopDetailPolling(): void {
    if (detailPollHandle !== null) {
      clearInterval(detailPollHandle);
      detailPollHandle = null;
    }
    if (unsubSyncComplete !== null) {
      unsubSyncComplete();
      unsubSyncComplete = null;
    }
  }

  async function toggleDetailPRStar(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    currentlyStarred: boolean,
  ): Promise<void> {
    if (detail !== null) {
      detail = {
        ...detail,
        merge_request: {
          ...detail.merge_request,
          Starred: !currentlyStarred,
        },
      };
    }
    try {
      if (currentlyStarred) {
        const { error: requestError } =
          await apiClient.DELETE("/starred", {
            body: {
              item_type: "pr",
              owner,
              name,
              number,
              ...(platformHost ? { platform_host: platformHost } : {}),
            },
          });
        if (requestError) {
          throw new Error(
            requestError.detail ??
              requestError.title ??
              "failed to unstar pull request",
          );
        }
      } else {
        const { error: requestError } =
          await apiClient.PUT("/starred", {
            body: {
              item_type: "pr",
              owner,
              name,
              number,
              ...(platformHost ? { platform_host: platformHost } : {}),
            },
          });
        if (requestError) {
          throw new Error(
            requestError.detail ??
              requestError.title ??
              "failed to star pull request",
          );
        }
      }
    } catch (err) {
      storeError =
        err instanceof Error ? err.message : String(err);
      if (detail !== null) {
        detail = {
          ...detail,
          merge_request: {
            ...detail.merge_request,
            Starred: currentlyStarred,
          },
        };
      }
      return;
    }
    await refreshPullsIfActive();
  }

  async function submitComment(
    owner: string,
    name: string,
    number: number,
    platformHost: string | undefined,
    body: string,
  ): Promise<void> {
    storeError = null;
    try {
      const { error: requestError } =
        await apiClient.POST(
          "/repos/{owner}/{name}/pulls/{number}/comments",
          {
            params: {
              path: { owner, name, number },
              ...(platformHost ? { query: { platform_host: platformHost } } : {}),
            },
            body: { body },
          },
        );
      if (requestError) {
        throw new Error(
          requestError.detail ??
            requestError.title ??
            "failed to post comment",
        );
      }
    } catch (err) {
      storeError =
        err instanceof Error ? err.message : String(err);
      return;
    }
    // Supersede any in-flight syncDetail so its stale response
    // cannot overwrite the detail we are about to fetch.
    const gen = ++syncGeneration;
    syncing = false;
    // Silent refresh: avoid flipping loading flag, which would
    // unmount the detail tree and reset scroll position.
    await refreshDetail(owner, name, number, platformHost);
    // Pull authoritative state from GitHub so PR row metadata
    // (last_activity_at, comment_count) and the pulls list catch
    // up. Skip if the user navigated away mid-refresh.
    if (gen === syncGeneration) {
      void syncDetail(owner, name, number, platformHost, gen);
    }
  }

  async function editComment(
    owner: string,
    name: string,
    number: number,
    commentID: number,
    platformHost: string | undefined,
    body: string,
  ): Promise<boolean> {
    storeError = null;
    try {
      const { error: requestError } = await apiClient.PATCH(
        "/repos/{owner}/{name}/pulls/{number}/comments/{comment_id}",
        {
          params: {
            path: {
              owner,
              name,
              number,
              comment_id: commentID,
            },
            ...(platformHost ? { query: { platform_host: platformHost } } : {}),
          },
          body: { body },
        },
      );
      if (requestError) {
        throw new Error(
          requestError.detail ??
            requestError.title ??
            "failed to edit comment",
        );
      }
    } catch (err) {
      storeError = err instanceof Error ? err.message : String(err);
      return false;
    }
    await refreshDetail(owner, name, number, platformHost);
    return true;
  }

  return {
    getDetail,
    isDetailLoading,
    isDetailSyncing,
    getDetailError,
    getDetailLoaded,
    isStaleRefreshing,
    clearDetail,
    loadDetail,
    refreshDetailOnly,
    updateKanbanState,
    updatePRContent,
    startDetailPolling,
    stopDetailPolling,
    toggleDetailPRStar,
    submitComment,
    editComment,
  };
}

export type DetailStore = ReturnType<
  typeof createDetailStore
>;
