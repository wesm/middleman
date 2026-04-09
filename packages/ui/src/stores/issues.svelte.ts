import type {
  Issue,
  IssueDetail,
  IssuesParams,
} from "../api/types.js";
import type { MiddlemanClient } from "../types.js";

export interface IssuesStoreOptions {
  client: MiddlemanClient;
  getGlobalRepo?: () => string | undefined;
  getGroupByRepo?: () => boolean;
  getPage?: () => string;
  sync?: {
    refreshSyncStatus?: () => Promise<void>;
  };
}

function apiErrorMessage(
  error: { detail?: string; title?: string },
  fallback: string,
): string {
  return error.detail ?? error.title ?? fallback;
}

export function createIssuesStore(
  opts: IssuesStoreOptions,
) {
  const apiClient = opts.client;
  const getGlobalRepo =
    opts.getGlobalRepo ?? (() => undefined);
  const getGroupByRepo =
    opts.getGroupByRepo ?? (() => false);
  const getPage = opts.getPage ?? (() => "");
  const syncDep = opts.sync;

  async function refreshIssuesIfActive(): Promise<void> {
    if (getPage() === "issues") {
      await loadIssues();
    }
  }

  // --- list state ---

  let issues = $state<Issue[]>([]);
  let loading = $state(false);
  let storeError = $state<string | null>(null);
  let filterStarred = $state(false);
  let filterState = $state<string>("open");
  let searchQuery = $state<string | undefined>(undefined);
  let selectedIssue = $state<{
    owner: string;
    name: string;
    number: number;
  } | null>(null);

  // --- detail state ---

  let issueDetail = $state<IssueDetail | null>(null);
  let detailLoading = $state(false);
  let detailSyncing = $state(false);
  let detailError = $state<string | null>(null);
  let issueDetailLoaded = $state(false);
  let detailPollHandle: ReturnType<
    typeof setInterval
  > | null = null;
  let issueSyncGeneration = 0;

  // --- list reads ---

  function getIssues(): Issue[] {
    return issues;
  }
  function isIssuesLoading(): boolean {
    return loading;
  }
  function getIssuesError(): string | null {
    return storeError;
  }
  function getSelectedIssue(): {
    owner: string;
    name: string;
    number: number;
  } | null {
    return selectedIssue;
  }
  function getIssueFilterStarred(): boolean {
    return filterStarred;
  }
  function getIssueSearchQuery(): string | undefined {
    return searchQuery;
  }

  function issuesByRepo(): Map<string, Issue[]> {
    const map = new Map<string, Issue[]>();
    for (const issue of issues) {
      const key = `${issue.repo_owner ?? ""}/${issue.repo_name ?? ""}`;
      const existing = map.get(key);
      if (existing) existing.push(issue);
      else map.set(key, [issue]);
    }
    return map;
  }

  // --- detail reads ---

  function getIssueDetail(): IssueDetail | null {
    return issueDetail;
  }
  function isIssueDetailLoading(): boolean {
    return detailLoading;
  }
  function isIssueDetailSyncing(): boolean {
    return detailSyncing;
  }
  function getIssueDetailError(): string | null {
    return detailError;
  }

  function getIssueDetailLoaded(): boolean {
    return issueDetailLoaded;
  }

  function isIssueStaleRefreshing(): boolean {
    if (!issueDetail || !detailSyncing) return false;
    const fetchedAt = issueDetail.detail_fetched_at;
    if (!fetchedAt) return false;
    const fetchedMs = new Date(fetchedAt).getTime();
    const updatedMs = new Date(
      issueDetail.issue.UpdatedAt,
    ).getTime();
    const hourAgo = Date.now() - 3_600_000;
    return fetchedMs < hourAgo && updatedMs > fetchedMs;
  }

  // --- list writes ---

  function setIssueFilterStarred(v: boolean): void {
    filterStarred = v;
  }
  function setIssueSearchQuery(
    q: string | undefined,
  ): void {
    searchQuery = q;
  }
  function getIssueFilterState(): string {
    return filterState;
  }
  function setIssueFilterState(s: string): void {
    filterState = s;
  }

  function selectIssue(
    owner: string,
    name: string,
    number: number,
  ): void {
    selectedIssue = { owner, name, number };
  }
  function clearIssueSelection(): void {
    selectedIssue = null;
  }

  async function loadIssues(
    params?: IssuesParams,
  ): Promise<void> {
    loading = true;
    storeError = null;
    try {
      const globalRepo = getGlobalRepo();
      const { data, error: requestError } =
        await apiClient.GET("/issues", {
          params: {
            query: {
              state: filterState,
              ...(globalRepo !== undefined && {
                repo: globalRepo,
              }),
              ...(filterStarred && { starred: true }),
              ...(searchQuery !== undefined && {
                q: searchQuery,
              }),
              ...params,
            },
          },
        });
      if (requestError) {
        throw new Error(
          apiErrorMessage(
            requestError,
            "failed to load issues",
          ),
        );
      }
      issues = (data ?? []) as Issue[];
    } catch (err) {
      storeError =
        err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  // --- detail writes ---

  async function loadIssueDetail(
    owner: string,
    name: string,
    number: number,
  ): Promise<void> {
    const gen = ++issueSyncGeneration;

    detailLoading = true;
    detailSyncing = false;
    detailError = null;
    try {
      const { data, error: requestError } =
        await apiClient.GET(
          "/repos/{owner}/{name}/issues/{number}",
          { params: { path: { owner, name, number } } },
        );
      if (gen !== issueSyncGeneration) return;
      if (requestError) {
        throw new Error(
          apiErrorMessage(
            requestError,
            "failed to load issue",
          ),
        );
      }
      issueDetail = data
        ? ({
            ...data,
            events: data.events ?? [],
          } as IssueDetail)
        : null;
      issueDetailLoaded = data?.detail_loaded ?? false;
    } catch (err) {
      if (gen !== issueSyncGeneration) return;
      detailError =
        err instanceof Error ? err.message : String(err);
    } finally {
      if (gen === issueSyncGeneration)
        detailLoading = false;
    }

    if (gen === issueSyncGeneration) {
      void syncIssueDetail(owner, name, number, gen);
    }
  }

  async function syncIssueDetail(
    owner: string,
    name: string,
    number: number,
    gen: number,
  ): Promise<void> {
    detailSyncing = true;
    try {
      const { data, error: requestError } =
        await apiClient.POST(
          "/repos/{owner}/{name}/issues/{number}/sync",
          { params: { path: { owner, name, number } } },
        );
      if (gen !== issueSyncGeneration) return;
      if (requestError) {
        throw new Error(
          apiErrorMessage(requestError, "sync failed"),
        );
      }
      if (data) {
        detailError = null;
        issueDetail = {
          ...data,
          events: data.events ?? [],
        } as IssueDetail;
        issueDetailLoaded =
          data.detail_loaded ?? issueDetailLoaded;
      }
    } catch {
      // Sync failure is non-fatal.
    } finally {
      if (gen === issueSyncGeneration)
        detailSyncing = false;
    }
    // Always refresh rate limits -- the API calls happened
    // regardless of whether user navigated away.
    void syncDep?.refreshSyncStatus?.();
    if (gen === issueSyncGeneration) {
      await refreshIssuesIfActive();
    }
  }

  async function refreshIssueDetail(
    owner: string,
    name: string,
    number: number,
  ): Promise<void> {
    const gen = issueSyncGeneration;
    try {
      const { data } = await apiClient.GET(
        "/repos/{owner}/{name}/issues/{number}",
        { params: { path: { owner, name, number } } },
      );
      if (gen !== issueSyncGeneration) return;
      if (data !== undefined) {
        issueDetail = {
          ...data,
          events: data.events ?? [],
        } as IssueDetail;
        issueDetailLoaded = data.detail_loaded ?? issueDetailLoaded;
      }
    } catch {
      /* silent */
    }
  }

  function startIssueDetailPolling(
    owner: string,
    name: string,
    number: number,
  ): void {
    stopIssueDetailPolling();
    detailPollHandle = setInterval(() => {
      void refreshIssueDetail(owner, name, number);
    }, 60_000);
  }

  function stopIssueDetailPolling(): void {
    if (detailPollHandle !== null) {
      clearInterval(detailPollHandle);
      detailPollHandle = null;
    }
  }

  function clearIssueDetail(): void {
    ++issueSyncGeneration;
    issueDetail = null;
    detailLoading = false;
    detailSyncing = false;
    detailError = null;
    issueDetailLoaded = false;
  }

  async function submitIssueComment(
    owner: string,
    name: string,
    number: number,
    body: string,
  ): Promise<void> {
    detailError = null;
    try {
      const { error: requestError } = await apiClient.POST(
        "/repos/{owner}/{name}/issues/{number}/comments",
        {
          params: { path: { owner, name, number } },
          body: { body },
        },
      );
      if (requestError) {
        throw new Error(
          apiErrorMessage(
            requestError,
            "failed to post comment",
          ),
        );
      }
    } catch (err) {
      detailError =
        err instanceof Error ? err.message : String(err);
      return;
    }
    // Supersede any in-flight syncIssueDetail so its stale response
    // cannot overwrite the detail we are about to fetch.
    const gen = ++issueSyncGeneration;
    detailSyncing = false;
    // Silent refresh: avoid flipping loading flag, which would
    // unmount the detail tree and reset scroll position.
    await refreshIssueDetail(owner, name, number);
    // Pull authoritative state from GitHub so issue row metadata
    // catches up. Skip if the user navigated away mid-refresh.
    if (gen === issueSyncGeneration) {
      void syncIssueDetail(owner, name, number, gen);
    }
  }

  async function toggleIssueStar(
    owner: string,
    name: string,
    number: number,
    currentlyStarred: boolean,
  ): Promise<void> {
    try {
      if (currentlyStarred) {
        const { error: requestError } =
          await apiClient.DELETE("/starred", {
            body: {
              item_type: "issue",
              owner,
              name,
              number,
            },
          });
        if (requestError) {
          throw new Error(
            apiErrorMessage(
              requestError,
              "failed to unstar issue",
            ),
          );
        }
      } else {
        const { error: requestError } =
          await apiClient.PUT("/starred", {
            body: {
              item_type: "issue",
              owner,
              name,
              number,
            },
          });
        if (requestError) {
          throw new Error(
            apiErrorMessage(
              requestError,
              "failed to star issue",
            ),
          );
        }
      }
    } catch (err) {
      storeError =
        err instanceof Error ? err.message : String(err);
      return;
    }
    await loadIssues();
    if (
      issueDetail &&
      issueDetail.repo_owner === owner &&
      issueDetail.repo_name === name &&
      issueDetail.issue.Number === number
    ) {
      await loadIssueDetail(owner, name, number);
    }
  }

  // --- navigation ---

  function getDisplayOrderIssues(): Issue[] {
    if (getGroupByRepo()) {
      const grouped = issuesByRepo();
      const ordered: Issue[] = [];
      for (const items of grouped.values()) {
        ordered.push(...items);
      }
      return ordered;
    }
    return issues;
  }

  function selectNextIssue(): void {
    const list = getDisplayOrderIssues();
    if (list.length === 0) return;
    if (selectedIssue === null) {
      const first = list[0];
      if (first !== undefined) {
        selectedIssue = {
          owner: first.repo_owner ?? "",
          name: first.repo_name ?? "",
          number: first.Number,
        };
      }
      return;
    }
    const idx = list.findIndex(
      (i) =>
        (i.repo_owner ?? "") === selectedIssue!.owner &&
        (i.repo_name ?? "") === selectedIssue!.name &&
        i.Number === selectedIssue!.number,
    );
    if (idx < list.length - 1) {
      const next = list[idx + 1];
      if (next !== undefined) {
        selectedIssue = {
          owner: next.repo_owner ?? "",
          name: next.repo_name ?? "",
          number: next.Number,
        };
      }
    }
  }

  function selectPrevIssue(): void {
    const list = getDisplayOrderIssues();
    if (list.length === 0) return;
    if (selectedIssue === null) {
      const last = list[list.length - 1];
      if (last !== undefined) {
        selectedIssue = {
          owner: last.repo_owner ?? "",
          name: last.repo_name ?? "",
          number: last.Number,
        };
      }
      return;
    }
    const idx = list.findIndex(
      (i) =>
        (i.repo_owner ?? "") === selectedIssue!.owner &&
        (i.repo_name ?? "") === selectedIssue!.name &&
        i.Number === selectedIssue!.number,
    );
    if (idx > 0) {
      const prev = list[idx - 1];
      if (prev !== undefined) {
        selectedIssue = {
          owner: prev.repo_owner ?? "",
          name: prev.repo_name ?? "",
          number: prev.Number,
        };
      }
    }
  }

  return {
    getIssues,
    isIssuesLoading,
    getIssuesError,
    getSelectedIssue,
    getIssueFilterStarred,
    setIssueFilterStarred,
    getIssueSearchQuery,
    setIssueSearchQuery,
    getIssueFilterState,
    setIssueFilterState,
    issuesByRepo,
    selectIssue,
    clearIssueSelection,
    loadIssues,
    getIssueDetail,
    isIssueDetailLoading,
    isIssueDetailSyncing,
    getIssueDetailError,
    getIssueDetailLoaded,
    isIssueStaleRefreshing,
    loadIssueDetail,
    startIssueDetailPolling,
    stopIssueDetailPolling,
    clearIssueDetail,
    submitIssueComment,
    toggleIssueStar,
    selectNextIssue,
    selectPrevIssue,
  };
}

export type IssuesStore = ReturnType<
  typeof createIssuesStore
>;
